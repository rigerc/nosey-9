use anyhow::{Context, Result, bail, ensure};
use directories::BaseDirs;
use serde_json::{Value, json};
use std::{
    fs,
    io::{ErrorKind, Write},
    path::{Component, Path, PathBuf},
};
use tempfile::NamedTempFile;

pub fn relative(value: &str) -> Result<&Path> {
    let path = Path::new(value);
    ensure!(
        !value.is_empty()
            && !value.contains(['\\', ':'])
            && !value.chars().any(char::is_control)
            && path
                .components()
                .all(|p| matches!(p, Component::Normal(n) if !n.eq_ignore_ascii_case(".git"))),
        "Expected a project-relative path outside .git, got {value:?}"
    );
    Ok(path)
}

pub fn confined(root: &Path, rel: &str) -> Result<PathBuf> {
    let mut path = root.to_path_buf();
    for part in relative(rel)?.components() {
        path.push(part);
        match fs::symlink_metadata(&path) {
            Ok(m) => ensure!(!m.is_symlink(), "Refusing symlink {}", path.display()),
            Err(e) if e.kind() == ErrorKind::NotFound => (),
            Err(e) => return Err(e.into()),
        }
    }
    Ok(path)
}

pub fn read_json(path: &Path) -> Result<Value> {
    ensure!(
        path.metadata()?.len() <= 16 << 20,
        "{} exceeds the size limit",
        path.display()
    );
    serde_json::from_reader(fs::File::open(path)?)
        .with_context(|| format!("Reading {}", path.display()))
}

pub fn atomic_json(path: &Path, value: &Value) -> Result<()> {
    let parent = path.parent().context("Missing parent directory")?;
    fs::create_dir_all(parent)?;
    let mut file = NamedTempFile::new_in(parent)?;
    serde_json::to_writer_pretty(&mut file, value)?;
    writeln!(file)?;
    file.as_file().sync_all()?;
    file.persist(path)?;
    Ok(())
}

pub fn strings(v: &Value) -> impl Iterator<Item = &str> {
    v.as_array().into_iter().flatten().filter_map(Value::as_str)
}
pub fn field<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key].as_str().unwrap_or_default()
}

fn merge(base: &mut Value, overlay: Value) {
    if let (Some(b), Some(o)) = (base.as_object_mut(), overlay.as_object()) {
        for (k, v) in o {
            merge(b.entry(k).or_insert(Value::Null), v.clone());
        }
    } else {
        *base = overlay;
    }
}

#[derive(Clone)]
pub struct Workspace {
    pub root: PathBuf,
    pub config: PathBuf,
    pub cache: PathBuf,
    pub data: PathBuf,
    pub registry: Option<PathBuf>,
}

impl Workspace {
    pub fn open(root: &Path, config: Option<PathBuf>, registry: Option<PathBuf>) -> Result<Self> {
        let root = root.canonicalize().context("Opening workspace")?;
        ensure!(root.is_dir(), "Workspace must be a directory");
        let dirs = BaseDirs::new().context("Cannot locate user directories")?;
        Ok(Self {
            root,
            config: config.unwrap_or_else(|| dirs.config_dir().join("nosey/config.json")),
            cache: dirs.cache_dir().join("nosey/rust"),
            data: std::env::var_os("XDG_DATA_HOME")
                .map(PathBuf::from)
                .unwrap_or_else(|| dirs.home_dir().join(".local/share"))
                .join("nosey"),
            registry,
        })
    }

    fn layer(&self, project: bool) -> Result<(PathBuf, Value)> {
        let path = if project {
            confined(&self.root, ".nosey/config.json")?
        } else {
            self.config.clone()
        };
        let mut v = match fs::metadata(&path) {
            Err(e) if e.kind() == ErrorKind::NotFound => json!({"schemaVersion":3}),
            Ok(_) => read_json(&path)?,
            Err(e) => return Err(e.into()),
        };
        if !matches!(v["schemaVersion"].as_u64(), Some(1..=3)) {
            bail!("Unsupported configuration in {}", path.display());
        }
        if v["schemaVersion"] != 3 {
            if let Some(obj) = v["installation"].as_object_mut() {
                for k in ["defaultMode", "cliVersion", "upstreamTelemetry"] {
                    obj.remove(k);
                }
            }
            v["schemaVersion"] = 3.into();
        }
        validate(&v, project)?;
        Ok((path, v))
    }

    pub fn settings(&self) -> Result<Value> {
        let mut v = json!({"schemaVersion":3,"installation":{"directory":"","agents":[],"method":"symlink","customAgents":[]},"detection":{"exclude":["**/node_modules/**","**/vendor/**","**/.git/**","**/dist/**","**/build/**","**/target/**"],"maxFileBytes":1048576,"enableMcpPackage":false},"updates":{"ttlHours":24},"registries":[]});
        merge(&mut v, self.layer(false)?.1);
        let mut project = self.layer(true)?.1;
        // Workspace-proposed sources never gain trust from opening a folder.
        project.as_object_mut().unwrap().remove("registries");
        merge(&mut v, project);
        Ok(v)
    }

    pub fn set(&self, key: &str, value: Value, project: bool) -> Result<()> {
        let (path, mut v) = self.layer(project)?;
        let mut slot = &mut v;
        for k in key.split('.') {
            ensure!(!k.is_empty(), "Empty setting name");
            ensure!(slot.is_object() || slot.is_null(), "Invalid setting {key}");
            slot = &mut slot[k];
        }
        *slot = value;
        validate(&v, project)?;
        atomic_json(&path, &v)
    }
}

fn validate(v: &Value, project: bool) -> Result<()> {
    use std::sync::LazyLock;
    static SCHEMA: LazyLock<jsonschema::Validator> = LazyLock::new(|| {
        jsonschema::validator_for(
            &serde_json::from_str(include_str!(
                "../internal/contracts/schemas/settings.v3.schema.json"
            ))
            .unwrap(),
        )
        .unwrap()
    });
    SCHEMA
        .validate(v)
        .map_err(|e| anyhow::anyhow!("Invalid configuration at {}", e.instance_path()))?;
    if project {
        ensure!(
            v.pointer("/installation/customAgents").is_none()
                && v.pointer("/detection/adapters").is_none(),
            "Project settings cannot configure custom agents or detector adapters"
        );
    }
    if let Some(d) = v
        .pointer("/installation/directory")
        .and_then(Value::as_str)
        .filter(|s| !s.is_empty())
    {
        relative(d)?;
    }
    Ok(())
}
impl Workspace {
    #[cfg(feature = "desktop")]
    pub fn save_installation(&self, agents: &[String], dir: &str, copy: bool) -> Result<()> {
        let (path, mut v) = self.layer(true)?;
        v["installation"]["directory"] = json!(dir);
        v["installation"]["method"] = json!(if copy { "copy" } else { "symlink" });
        v["installation"]["agents"] = json!(agents);
        validate(&v, true)?;
        atomic_json(&path, &v)
    }
}
