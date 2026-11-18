use anyhow::Result;
use regex::Regex;
use serde_json::Value;
use std::collections::BTreeSet;
use std::sync::LazyLock;

pub fn ecosystem(name: &str, path: &str, mcp: bool) -> Option<&'static str> {
    Some(match name {
        "package.json" => "npm",
        "composer.json" => "composer",
        "deno.json" | "deno.jsonc" => "deno",
        "Cargo.toml" => "cargo",
        "pyproject.toml" | "Pipfile" => "python",
        "go.mod" => "go",
        "Gemfile" | "Gemfile.lock" => "ruby",
        "mix.exs" | "mix.lock" => "hex",
        "compose.yaml" | "compose.yml" | "docker-compose.yaml" | "docker-compose.yml" => "docker",
        _ if name.starts_with("Dockerfile") => "docker",
        _ if name.starts_with("requirements") && name.ends_with(".txt") => "python",
        _ if name.ends_with(".tf") => "terraform",
        _ if path.contains(".github/workflows/")
            && (name.ends_with(".yaml") || name.ends_with(".yml")) =>
        {
            "githubAction"
        }
        _ if mcp
            && (name == ".mcp.json"
                || name == "mcp.json"
                || path.ends_with(".codex/config.toml")) =>
        {
            "mcp"
        }
        _ => return None,
    })
}

// Like the Go engine, only PEP 508 list entries are normalized; requirement
// files and TOML table keys keep the authored spelling that registry rules use.
fn python_name(s: &str) -> String {
    s.trim()
        .split(['[', '<', '>', '=', '!', '~', ';', ' ', '@'])
        .next()
        .unwrap_or("")
        .into()
}
fn python_list_name(s: &str) -> String {
    python_name(s).replace('_', "-").to_lowercase()
}
fn toml_packages(doc: &Value, eco: &str, out: &mut BTreeSet<String>) {
    let Some(map) = doc.as_object() else {
        return;
    };
    for (key, value) in map {
        if matches!(
            key.as_str(),
            "dependencies"
                | "dev-dependencies"
                | "build-dependencies"
                | "packages"
                | "dev-packages"
        ) {
            if let Some(obj) = value.as_object() {
                for (name, spec) in obj {
                    out.insert(if eco == "cargo" {
                        spec["package"].as_str().unwrap_or(name).into()
                    } else {
                        name.clone()
                    });
                }
            }
            for item in value
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(Value::as_str)
            {
                out.insert(python_list_name(item));
            }
        }
        if matches!(key.as_str(), "optional-dependencies" | "dependency-groups") {
            for group in value.as_object().into_iter().flat_map(|obj| obj.values()) {
                for item in group
                    .as_array()
                    .into_iter()
                    .flatten()
                    .filter_map(Value::as_str)
                {
                    out.insert(python_list_name(item));
                }
            }
        }
        toml_packages(value, eco, out);
    }
}
fn image(s: &str) -> String {
    let s = s.split('@').next().unwrap_or(s);
    match s.rsplit_once(':') {
        Some((p, t)) if !t.contains('/') => p.into(),
        _ => s.into(),
    }
}

pub fn dependencies(eco: &str, name: &str, text: &str) -> Result<Vec<(String, String)>> {
    let mut names = BTreeSet::new();
    let mut other = Vec::new();
    match eco {
        "npm" | "composer" | "deno" => {
            let doc: Value = if name == "deno.jsonc" {
                json5::from_str(text)?
            } else {
                serde_json::from_str(text)?
            };
            for k in [
                "dependencies",
                "devDependencies",
                "peerDependencies",
                "optionalDependencies",
                "require",
                "require-dev",
                "imports",
            ] {
                for (key, value) in doc[k].as_object().into_iter().flatten() {
                    names.insert(
                        if eco == "deno" {
                            value.as_str().unwrap_or(key)
                        } else {
                            key
                        }
                        .into(),
                    );
                }
            }
        }
        "cargo" | "python" if !name.starts_with("requirements") => {
            let doc: toml::Value = toml::from_str(text)?;
            toml_packages(&serde_json::to_value(doc)?, eco, &mut names);
        }
        "python" => {
            for line in text
                .lines()
                .map(str::trim)
                .filter(|l| !l.starts_with(['#', '-']))
            {
                names.insert(python_name(line));
            }
        }
        "go" | "ruby" | "hex" => {
            static GO: LazyLock<Regex> = LazyLock::new(|| {
                Regex::new(r"(?m)^\s*(?:require\s+)?([\w.+-]+\.[\w./+-]+)\s+v\d").unwrap()
            });
            static RUBY: LazyLock<Regex> = LazyLock::new(|| {
                Regex::new(r#"(?m)^(?:\s*gem\s+["']([\w.-]+)["']|    ([\w.-]+) \()"#).unwrap()
            });
            static HEX: LazyLock<Regex> =
                LazyLock::new(|| Regex::new(r"\{\s*:([\w]+)\s*,").unwrap());
            for c in (match eco {
                "go" => &*GO,
                "ruby" => &*RUBY,
                _ => &*HEX,
            })
            .captures_iter(text)
            {
                if let Some(m) = c.iter().skip(1).flatten().next() {
                    names.insert(m.as_str().into());
                }
            }
        }
        "docker" if name.starts_with("Dockerfile") => {
            for line in text.lines() {
                let mut words = line.split_whitespace();
                if words.next().is_some_and(|s| s.eq_ignore_ascii_case("FROM"))
                    && let Some(i) = words.find(|s| !s.starts_with("--"))
                {
                    anyhow::ensure!(!i.contains('$'), "Dynamic Docker base image");
                    names.insert(image(i));
                }
            }
        }
        "docker" | "githubAction" => {
            let doc: Value = serde_yaml_ng::from_str(text)?;
            if eco == "docker" {
                for s in doc["services"]
                    .as_object()
                    .into_iter()
                    .flat_map(|v| v.values())
                {
                    if let Some(i) = s["image"].as_str() {
                        anyhow::ensure!(!i.contains('$'), "Dynamic Compose image");
                        names.insert(image(i));
                    }
                }
            } else {
                for job in doc["jobs"]
                    .as_object()
                    .into_iter()
                    .flat_map(|v| v.values())
                    .chain(std::iter::once(&doc["runs"]))
                {
                    for s in
                        std::iter::once(job).chain(job["steps"].as_array().into_iter().flatten())
                    {
                        if let Some(i) = s["uses"].as_str() {
                            anyhow::ensure!(!i.contains('$'), "Dynamic workflow reference");
                            names.insert(i.split('@').next().unwrap_or(i).into());
                        }
                    }
                }
            }
        }
        "terraform" => {
            static RESOURCE: LazyLock<Regex> =
                LazyLock::new(|| Regex::new(r#"(?m)^\s*resource\s+"([\w-]+)""#).unwrap());
            static PROVIDER: LazyLock<Regex> = LazyLock::new(|| {
                Regex::new(r#"\bsource\s*=\s*"([\w.-]+(?:/[\w.-]+){1,2})""#).unwrap()
            });
            for c in RESOURCE.captures_iter(text) {
                other.push(("terraform.resource".into(), c[1].into()));
            }
            for c in PROVIDER.captures_iter(text) {
                let s = &c[1];
                names.insert(if s.matches('/').count() == 1 {
                    format!("registry.terraform.io/{s}")
                } else {
                    s.into()
                });
            }
        }
        "mcp" => {
            let doc: Value = if name.ends_with(".toml") {
                serde_json::to_value(toml::from_str::<toml::Value>(text)?)?
            } else {
                serde_json::from_str(text)?
            };
            for k in ["mcpServers", "servers", "mcp_servers"] {
                for server in doc[k].as_object().into_iter().flat_map(|o| o.values()) {
                    if server["disabled"] == true || server["enabled"] == false {
                        continue;
                    }
                    let args: Vec<_> = crate::workspace::strings(&server["args"]).collect();
                    let cmd = std::path::Path::new(server["command"].as_str().unwrap_or(""))
                        .file_name()
                        .and_then(|n| n.to_str());
                    let args = match (cmd, args.as_slice()) {
                        (Some("npx" | "bunx" | "uvx"), a) => a,
                        (Some("npm"), ["exec", a @ ..])
                        | (Some("pnpm" | "yarn"), ["dlx", a @ ..])
                        | (Some("uv"), ["tool", "run", a @ ..]) => a,
                        _ => continue,
                    };
                    if let Some(s) = args.iter().find(|s| !s.starts_with('-')) {
                        let n = s
                            .rfind('@')
                            .filter(|i| *i > 0)
                            .map(|i| &s[..i])
                            .unwrap_or(s);
                        names.insert(n.into());
                    }
                }
            }
        }
        _ => (),
    }
    other.extend(
        names
            .into_iter()
            .filter(|s| !s.is_empty())
            .map(|s| (eco.into(), s)),
    );
    Ok(other)
}
