use crate::{catalog::Catalog, providers, workspace::*};
use anyhow::{Context, Result, bail};
use globset::{GlobBuilder, GlobMatcher, GlobSetBuilder};
use ignore::WalkBuilder;
use regex::{Regex, RegexBuilder};
use serde::Serialize;
use serde_json::Value;
use std::{
    collections::{BTreeMap, BTreeSet},
    fs,
    io::Read,
};

#[derive(Default, Clone, Serialize)]
pub struct Scan {
    pub technologies: BTreeMap<String, BTreeSet<String>>,
    pub recommendations: BTreeMap<String, BTreeSet<String>>,
    pub warnings: BTreeSet<String>,
    pub files: usize,
}
enum Matcher {
    Exact(String),
    Pattern(Regex),
}
impl Matcher {
    fn new(v: &Value, key: &str, re: &str) -> Result<Self> {
        if let Some(p) = v[re].as_str() {
            Ok(Self::Pattern(
                RegexBuilder::new(p)
                    .case_insensitive(v["regexFlags"] == "i")
                    .build()?,
            ))
        } else {
            Ok(Self::Exact(
                v[key].as_str().context("Missing rule predicate")?.into(),
            ))
        }
    }
    fn matches(&self, s: &str) -> bool {
        match self {
            Self::Exact(x) => x == s,
            Self::Pattern(r) => r.is_match(s),
        }
    }
}
enum Rule {
    Path(GlobMatcher),
    Filename(Regex, bool),
    Extension(String),
    Content(GlobMatcher, Matcher),
    Env(Option<GlobMatcher>, Matcher),
    Dependency(String, Matcher),
}
fn glob(s: &str) -> Result<GlobMatcher> {
    Ok(GlobBuilder::new(s)
        .literal_separator(true)
        .build()?
        .compile_matcher())
}
impl Rule {
    fn compile(v: &Value) -> Result<Self> {
        Ok(match field(v, "kind") {
            "file" if v["glob"].is_string() => Self::Path(glob(field(v, "glob"))?),
            "file" => Self::Filename(
                Regex::new(field(v, "pathRegex"))?,
                v["pathScope"] == "basename",
            ),
            "extension" => Self::Extension(field(v, "extension").into()),
            "content" => Self::Content(
                glob(field(v, "glob"))?,
                Matcher::new(v, "contains", "regex")?,
            ),
            "dotenv" => Self::Env(
                v["glob"].as_str().map(glob).transpose()?,
                Matcher::new(v, "key", "keyRegex")?,
            ),
            "dependency" => Self::Dependency(
                field(v, "ecosystem").into(),
                Matcher::new(v, "name", "nameRegex")?,
            ),
            "mcp_package" => Self::Dependency("mcp".into(), Matcher::new(v, "name", "nameRegex")?),
            "detector" => bail!("Adapter {} unavailable", field(v, "detector")),
            k => bail!("Unsupported rule kind {k}"),
        })
    }
}

pub fn scan(ws: &Workspace, c: &Catalog) -> Result<Scan> {
    let s = ws.settings()?;
    let opts = &s["detection"];
    let mut result = Scan::default();
    let mut unknown = BTreeSet::new();
    let mut rules = Vec::new();
    let mut dependencies: BTreeMap<String, Vec<(&str, Matcher)>> = BTreeMap::new();
    for (id, t) in &c.technologies {
        for row in t["rules"].as_array().into_iter().flatten() {
            if row["disabled"] == true
                || row["kind"] == "mcp_package" && opts["enableMcpPackage"] != true
            {
                continue;
            }
            match Rule::compile(row) {
                Ok(Rule::Dependency(eco, m)) => dependencies.entry(eco).or_default().push((id, m)),
                Ok(r) => rules.push((id, r)),
                Err(e) => {
                    unknown.insert(id.clone());
                    result
                        .warnings
                        .insert(format!("{id} / {}: {e}", field(row, "id")));
                }
            }
        }
    }
    let mut exclude = GlobSetBuilder::new();
    for p in strings(&opts["exclude"]) {
        exclude.add(GlobBuilder::new(p).literal_separator(true).build()?);
    }
    let exclude = exclude.build()?;
    let root = ws.root.clone();
    let walk = WalkBuilder::new(&root)
        .hidden(false)
        .parents(false)
        .git_global(false)
        .require_git(false)
        .follow_links(false)
        .filter_entry(move |e| {
            if e.depth() == 0 {
                return true;
            }
            let p = e.path().strip_prefix(&root).unwrap();
            e.file_name() != ".git"
                && !exclude.is_match(p)
                && !(e.file_type().is_some_and(|v| v.is_dir())
                    && exclude.is_match(p.join("__nosey__")))
        })
        .build();
    let limit = opts["maxFileBytes"].as_u64().unwrap_or(1 << 20);
    for item in walk {
        let entry = match item {
            Ok(e) => e,
            Err(e) => {
                result.warnings.insert(e.to_string());
                unknown.extend(c.technologies.keys().cloned());
                continue;
            }
        };
        let path = entry.path();
        if path == ws.root {
            continue;
        }
        let rel = path
            .strip_prefix(&ws.root)?
            .to_string_lossy()
            .replace('\\', "/");
        let base = entry.file_name().to_string_lossy();
        let file = entry.file_type().is_some_and(|t| t.is_file());
        if entry.file_type().is_some_and(|t| t.is_symlink()) {
            continue;
        }
        if file {
            result.files += 1;
        }
        let eco = providers::ecosystem(&base, &rel, opts["enableMcpPackage"] == true);
        let mut contents = None;
        let read = || -> Result<String> {
            let mut data = Vec::new();
            fs::File::open(path)?
                .take(limit + 1)
                .read_to_end(&mut data)?;
            anyhow::ensure!(data.len() as u64 <= limit, "File exceeds scan limit");
            Ok(String::from_utf8(data)?)
        };
        if let Some(eco) = eco.filter(|_| file) {
            contents = Some(read());
            let found = contents
                .as_ref()
                .unwrap()
                .as_ref()
                .ok()
                .map(|s| providers::dependencies(eco, &base, s));
            match found {
                Some(Ok(names)) => {
                    for (kind, name) in names {
                        for (id, m) in dependencies.get(&kind).into_iter().flatten() {
                            if m.matches(&name) {
                                result
                                    .technologies
                                    .entry((*id).into())
                                    .or_default()
                                    .insert(rel.clone());
                            }
                        }
                    }
                }
                _ => {
                    result
                        .warnings
                        .insert(format!("Could not evaluate {eco} manifest {rel}"));
                    for (id, _) in dependencies.get(eco).into_iter().flatten() {
                        unknown.insert((*id).into());
                    }
                }
            }
        }
        for (id, r) in &rules {
            let hit = match r {
                Rule::Path(g) => g.is_match(&rel),
                Rule::Filename(r, b) => r.is_match(if *b { &base } else { &rel }),
                Rule::Extension(ext) => file && rel.ends_with(ext),
                Rule::Content(g, m) if file && g.is_match(&rel) => {
                    match contents.get_or_insert_with(read) {
                        Ok(s) => match m {
                            Matcher::Exact(x) => s.contains(x.as_str()),
                            Matcher::Pattern(r) => r.is_match(s),
                        },
                        Err(_) => {
                            result.warnings.insert(format!("Could not read {rel}"));
                            unknown.insert((*id).clone());
                            false
                        }
                    }
                }
                Rule::Env(g, m)
                    if file
                        && g.as_ref().map_or_else(
                            || {
                                base.starts_with(".env")
                                    && !base.contains("example")
                                    && !base.contains("sample")
                            },
                            |g| g.is_match(&rel),
                        ) =>
                {
                    match contents.get_or_insert_with(read) {
                        Ok(s) => s
                            .lines()
                            .filter_map(|l| {
                                l.trim()
                                    .trim_start_matches("export ")
                                    .split_once('=')
                                    .map(|(k, _)| k.trim())
                            })
                            .any(|k| m.matches(k)),
                        Err(_) => {
                            result.warnings.insert(format!("Could not read {rel}"));
                            unknown.insert((*id).clone());
                            false
                        }
                    }
                }
                _ => false,
            };
            if hit {
                result
                    .technologies
                    .entry((*id).clone())
                    .or_default()
                    .insert(rel.clone());
            }
        }
    }
    let detected = |id: &str| result.technologies.contains_key(id);
    for map in c
        .mappings
        .values()
        .filter(|m| detected(field(m, "technologyId")))
    {
        for r in map["recommendations"].as_array().into_iter().flatten() {
            let id = field(r, "skillId");
            let w = &r["when"];
            if !c.skills.contains_key(id)
                || r["disabled"] == true
                || !strings(&w["allTechnologies"]).all(detected)
                || strings(&w["noneTechnologies"]).any(|t| detected(t) || unknown.contains(t))
                || (strings(&w["anyTechnologies"]).count() > 0
                    && !strings(&w["anyTechnologies"]).any(detected))
            {
                continue;
            }
            let reason = r["reason"].as_str().unwrap_or(field(map, "technologyId"));
            result
                .recommendations
                .entry(id.into())
                .or_default()
                .insert(reason.into());
        }
    }
    result.warnings.extend(c.warnings.iter().cloned());
    Ok(result)
}
