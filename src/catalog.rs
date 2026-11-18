use crate::{source::github_tree, workspace::*};
use anyhow::{Context, Result, ensure};
use serde_json::{Value, json};
use std::{
    collections::{BTreeMap, BTreeSet},
    fs,
    path::{Path, PathBuf},
    sync::Arc,
    time::Duration,
};

#[derive(Default, Clone)]
pub struct Catalog {
    pub skills: BTreeMap<String, Value>,
    pub technologies: BTreeMap<String, Value>,
    pub mappings: BTreeMap<String, Value>,
    pub roots: BTreeMap<String, PathBuf>,
    pub warnings: Vec<String>,
    /// Source trees fetched during this operation, keyed by (uri, ref).
    pub trees: BTreeMap<(String, String), Arc<tempfile::TempDir>>,
}
impl Catalog {
    pub fn load(ws: &Workspace, refresh: bool) -> Result<Self> {
        let settings = ws.settings()?;
        let mut c = Self::default();
        if let Some(path) = &ws.registry {
            c.add(&path.canonicalize()?)?;
        } else {
            let mut sources = BTreeMap::from([(
                "nosey:official".to_owned(),
                json!({"enabled":true,"priority":0,"source":{"kind":"git","url":"https://github.com/rigerc/nosey-registry"}}),
            )]);
            for r in settings["registries"].as_array().into_iter().flatten() {
                sources.insert(field(r, "id").into(), r.clone());
            }
            let mut refs: Vec<_> = sources
                .into_values()
                .filter(|r| r["enabled"] != false)
                .collect();
            refs.sort_by_key(|r| r["priority"].as_i64().unwrap_or(0));
            for r in refs {
                let src = &r["source"];
                let path = if src["kind"] == "local" {
                    PathBuf::from(field(src, "path")).canonicalize()?
                } else {
                    c.snapshot(
                        ws,
                        field(src, "url"),
                        field(src, "ref"),
                        settings["updates"]["ttlHours"].as_u64().unwrap_or(24),
                        refresh,
                    )?
                };
                c.add(&path)?;
            }
            let prop = confined(&ws.root, ".nosey/config.json")?;
            if prop.exists() && read_json(&prop)?.get("registries").is_some() {
                c.warnings.push(
                    "Project registry proposals require explicit selection with --registry.".into(),
                );
            }
        }
        c.skills
            .retain(|_, v| v["disabled"] != true && v["quarantine"].is_null());
        c.technologies.retain(|_, v| v["disabled"] != true);
        c.mappings.retain(|_, v| v["disabled"] != true);
        Ok(c)
    }
    fn snapshot(
        &mut self,
        ws: &Workspace,
        uri: &str,
        rev: &str,
        ttl: u64,
        refresh: bool,
    ) -> Result<PathBuf> {
        use sha2::{Digest, Sha256};
        let base = ws
            .cache
            .join(format!("{:x}", Sha256::digest(format!("{uri}\0{rev}"))));
        let index = base.join("current.json");
        let cached = read_json(&index)
            .ok()
            .and_then(|v| confined(&base, v.as_str()?).ok())
            .filter(|p| p.is_dir());
        if !refresh
            && index
                .metadata()
                .and_then(|m| m.modified())
                .ok()
                .and_then(|t| t.elapsed().ok())
                .is_some_and(|d| d < Duration::from_secs(ttl.saturating_mul(3600)))
            && let Some(p) = &cached
        {
            return Ok(p.clone());
        }
        let result = (|| -> Result<PathBuf> {
            let tree = github_tree(uri, rev)?;
            Self::default().add(tree.path())?;
            fs::create_dir_all(&base)?;
            let dir = tempfile::Builder::new()
                .prefix("registry-")
                .tempdir_in(&base)?;
            // tempdir may be on a different volume from the cache.
            for item in walkdir::WalkDir::new(tree.path()).min_depth(1) {
                let e = item?;
                let dest = dir.path().join(e.path().strip_prefix(tree.path())?);
                if e.file_type().is_dir() {
                    fs::create_dir(dest)?;
                } else {
                    fs::copy(e.path(), dest)?;
                }
            }
            let name = dir
                .path()
                .file_name()
                .unwrap()
                .to_string_lossy()
                .to_string();
            let path = dir.keep();
            atomic_json(&index, &json!(name))?;
            // Older snapshots are unreachable once current.json moves on.
            for old in fs::read_dir(&base)?.flatten() {
                let n = old.file_name();
                if n != *name.as_str() && n.to_string_lossy().starts_with("registry-") {
                    let _ = fs::remove_dir_all(old.path());
                }
            }
            Ok(path)
        })();
        match (result, cached) {
            (Err(e), Some(p)) => {
                self.warnings.push(format!("Using cached registry. {e:#}"));
                Ok(p)
            }
            (r, _) => r,
        }
    }
    fn add(&mut self, root: &Path) -> Result<()> {
        let manifest = read_json(&confined(root, "nosey.registry.json")?)?;
        ensure!(
            manifest["schemaVersion"] == 1,
            "Unsupported registry manifest"
        );
        for kind in ["technologies", "skills", "mappings"] {
            let name = manifest["files"][kind]
                .as_str()
                .with_context(|| format!("Registry missing {kind} document"))?;
            let doc = read_json(&confined(root, name)?)?;
            let target = match kind {
                "skills" => &mut self.skills,
                "technologies" => &mut self.technologies,
                _ => &mut self.mappings,
            };
            let mut seen = BTreeSet::new();
            for row in doc[kind].as_array().context("Invalid catalog collection")? {
                let id = row["id"].as_str().context("Missing catalog ID")?;
                ensure!(seen.insert(id), "Duplicate {kind} ID {id}");
                let entry = target.entry(id.to_owned()).or_insert_with(|| json!({}));
                for (k, v) in row.as_object().context("Expected catalog object")? {
                    if (k == "rules" || k == "recommendations")
                        && entry[k].is_array()
                        && v.as_array().is_some_and(|a| !a.is_empty())
                    {
                        let key = if k == "rules" { "id" } else { "skillId" };
                        let list = entry[k].as_array_mut().unwrap();
                        for child in v.as_array().unwrap() {
                            if let Some(old) = list.iter_mut().find(|o| o[key] == child[key]) {
                                old.as_object_mut()
                                    .context("Invalid old rule")?
                                    .extend(child.as_object().context("Invalid rule")?.clone());
                            } else {
                                list.push(child.clone());
                            }
                        }
                    } else {
                        entry[k] = v.clone();
                    }
                }
                if kind == "skills" && row.get("source").is_some() {
                    self.roots.insert(id.to_owned(), root.to_owned());
                    entry["sourceRegistryId"] = manifest["id"].clone();
                }
            }
        }
        Ok(())
    }
    pub fn search(&self, q: &str) -> Vec<&Value> {
        let q = q.trim().to_lowercase();
        self.skills
            .values()
            .filter(|v| {
                ["id", "name", "description", "repositoryId"]
                    .iter()
                    .map(|k| field(v, k))
                    .chain(strings(&v["technologies"]))
                    .chain(strings(&v["tags"]))
                    .any(|s| s.to_lowercase().contains(&q))
            })
            .collect()
    }
}
