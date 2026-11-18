use crate::{
    catalog::Catalog,
    source::{document_name, github_tree},
    workspace::*,
};
use anyhow::{Context, Result, bail, ensure};
use icu_collator::{Collator, options::CollatorOptions};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    collections::{BTreeMap, BTreeSet},
    fs,
    io::{ErrorKind, Read},
    path::{Path, PathBuf},
    sync::Arc,
};
use tempfile::TempDir;
use walkdir::WalkDir;

const UNIVERSAL: &str = ".agents/skills";
const LOCK: &str = "skills-lock.json";

fn agents(settings: &Value) -> Result<BTreeMap<String, String>> {
    // Project destinations from internal/skillkit/agents_table.go (see the upstream MIT notice).
    let mut map: BTreeMap<String, String> = serde_json::from_str(include_str!("agents.json"))?;
    for a in settings["installation"]["customAgents"]
        .as_array()
        .into_iter()
        .flatten()
    {
        let dir = field(a, "skillsDir");
        relative(dir)?;
        ensure!(
            map.insert(field(a, "id").into(), dir.into()).is_none(),
            "Duplicate agent ID"
        );
    }
    Ok(map)
}
fn validate_name(s: &str) -> Result<()> {
    ensure!(
        s.len() <= 128
            && s.as_bytes().first().is_some_and(u8::is_ascii_alphanumeric)
            && s.bytes()
                .all(|c| c.is_ascii_alphanumeric() || b"._-".contains(&c)),
        "Invalid skill name {s:?}"
    );
    Ok(())
}
pub fn skill_name(s: &Value) -> &str {
    s["source"]["skillName"]
        .as_str()
        .unwrap_or(field(s, "name"))
}
fn read_lock(ws: &Workspace) -> Result<Value> {
    let path = confined(&ws.root, LOCK)?;
    if !path.try_exists()? {
        return Ok(json!({"version":1,"skills":{}}));
    }
    let v = read_json(&path)?;
    ensure!(
        v["version"] == 1 && v["skills"].is_object(),
        "Unsupported or invalid skills-lock.json"
    );
    for (name, entry) in v["skills"].as_object().unwrap() {
        validate_name(name)?;
        ensure!(entry.is_object(), "Invalid lock entry for {name}");
    }
    Ok(v)
}
fn preflight(ws: &Workspace) -> Result<()> {
    if let Some(home) = directories::BaseDirs::new().and_then(|d| d.home_dir().canonicalize().ok())
    {
        ensure!(
            home != ws.root,
            "Refusing to install into the home directory; run from a project directory"
        );
    }
    Ok(())
}
fn operation_lock(ws: &Workspace) -> Result<fs::File> {
    let dir = ws.data.join("locks");
    fs::create_dir_all(&dir)?;
    let hash = format!("{:x}", Sha256::digest(ws.root.to_string_lossy().as_bytes()));
    let f = fs::OpenOptions::new()
        .create(true)
        .truncate(false)
        .read(true)
        .write(true)
        .open(dir.join(format!("{}.lock", &hash[..32])))?;
    f.try_lock()
        .context("Another Nosey operation holds this workspace")?;
    Ok(f)
}
fn recorded(v: &Value) -> Result<Vec<String>> {
    let Some(loc) = v.get("noseyInstallation") else {
        return Ok(vec![]);
    };
    let dirs = loc["directories"]
        .as_array()
        .context("Invalid lock locations")?;
    ensure!(!dirs.is_empty(), "Empty lock locations");
    dirs.iter()
        .map(|v| {
            let s = v.as_str().context("Invalid location")?;
            relative(s)?;
            Ok(s.into())
        })
        .collect()
}
fn normalized_source(s: &str) -> &str {
    s.trim_end_matches('/')
        .trim_end_matches(".git")
        .trim_start_matches("https://github.com/")
}
fn belongs(c: &Catalog, s: &Value, e: &Value, ws: &Workspace) -> bool {
    let src = &s["source"];
    if src["kind"] == "local" {
        let Some(root) = c.roots.get(field(s, "id")) else {
            return false;
        };
        let old = ws.root.join(field(e, "source")).canonicalize();
        return old.is_ok() && old.ok() == root.join(field(src, "path")).canonicalize().ok();
    }
    normalized_source(field(e, "source")) == normalized_source(field(src, "uri"))
}
#[derive(Default, Clone, Serialize, Deserialize)]
pub struct Installed {
    pub name: String,
    pub tracked: bool,
    pub locations: BTreeSet<String>,
    pub source: String,
    pub catalog_id: Option<String>,
    pub outdated: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub skipped: Option<String>,
}

pub fn inventory(ws: &Workspace, c: &Catalog) -> Result<Vec<Installed>> {
    let settings = ws.settings()?;
    let lock = read_lock(ws)?;
    let agent_dirs: BTreeSet<_> = agents(&settings)?.into_values().collect();
    let mut dirs = agent_dirs.clone();
    if let Some(d) = settings["installation"]["directory"]
        .as_str()
        .filter(|s| !s.is_empty())
    {
        dirs.insert(d.into());
    }
    let mut map = BTreeMap::new();
    for (name, e) in lock["skills"].as_object().unwrap() {
        dirs.extend(recorded(e)?);
        let skill = c
            .skills
            .values()
            .find(|s| skill_name(s) == name && belongs(c, s, e, ws));
        map.insert(
            name.clone(),
            Installed {
                name: name.clone(),
                tracked: true,
                source: field(e, "source").into(),
                catalog_id: skill.map(|s| field(s, "id").into()),
                outdated: skill.is_some_and(|s| {
                    s["source"]["ref"].is_string() && s["source"]["ref"] != e["ref"]
                }),
                ..Default::default()
            },
        );
    }
    for d in dirs {
        let path = confined(&ws.root, &d)?;
        if !path.try_exists()? {
            continue;
        }
        for item in path.read_dir()? {
            let item = item?;
            let name = item.file_name().to_string_lossy().into_owned();
            if validate_name(&name).is_err() {
                continue;
            }
            let Ok(real) = item.path().canonicalize() else {
                continue;
            };
            if !real.starts_with(&ws.root) || !real.join("SKILL.md").is_file() {
                continue;
            }
            let e = &lock["skills"][&name];
            if !e.is_null()
                && !(recorded(e)?.contains(&d)
                    || (e["noseyInstallation"].is_null()
                        || e["noseyInstallation"]["agentDirectories"] == true)
                        && agent_dirs.contains(&d))
            {
                continue;
            }
            map.entry(name.clone())
                .or_insert_with(|| Installed {
                    name,
                    ..Default::default()
                })
                .locations
                .insert(d.clone());
        }
    }
    Ok(map.into_values().collect())
}
fn visit<'a>(
    c: &'a Catalog,
    id: &'a str,
    order: &mut Vec<String>,
    stack: &mut BTreeSet<&'a str>,
) -> Result<()> {
    if order.iter().any(|s| s == id) {
        return Ok(());
    }
    ensure!(stack.insert(id), "Dependency cycle at {id}");
    let s = c
        .skills
        .get(id)
        .with_context(|| format!("Unknown or unavailable skill {id}"))?;
    for dep in strings(&s["requires"]) {
        visit(c, dep, order, stack)?;
    }
    stack.remove(id);
    order.push(id.into());
    Ok(())
}
pub fn selection(c: &Catalog, ids: &[String], installed: &[Installed]) -> Result<Vec<String>> {
    ensure!(!ids.is_empty(), "Select at least one skill");
    let mut order = vec![];
    for id in ids {
        visit(c, id, &mut order, &mut BTreeSet::new())?;
    }
    let present: BTreeSet<&str> = installed
        .iter()
        .filter_map(|i| i.catalog_id.as_deref())
        .collect();
    // Only a skill that is not installed yet may introduce a name clash or a
    // conflict; updating two already-coexisting skills must stay possible.
    let fresh = |id: &str| !present.contains(id);
    let conflicts = |id: &str| -> Vec<&str> {
        c.skills
            .get(id)
            .map(|s| strings(&s["conflictsWith"]).collect())
            .unwrap_or_default()
    };
    let mut names = BTreeSet::new();
    for id in &order {
        let name = skill_name(&c.skills[id]);
        validate_name(name)?;
        ensure!(
            names.insert(name.to_lowercase()),
            "Multiple selected skills install as {name}"
        );
        if fresh(id)
            && let Some(i) = installed.iter().find(|i| {
                i.name.eq_ignore_ascii_case(name) && i.catalog_id.as_deref() != Some(id.as_str())
            })
        {
            bail!(
                "Installing {id} would replace the installed skill {}",
                i.name
            );
        }
    }
    let mut problems = BTreeSet::new();
    for id in &order {
        for other in conflicts(id) {
            if (fresh(id) || fresh(other))
                && (order.iter().any(|x| x == other) || present.contains(other))
            {
                problems.insert(format!("{id} conflicts with {other}"));
            }
        }
        if fresh(id) {
            for other in &present {
                if conflicts(other).contains(&id.as_str()) {
                    problems.insert(format!("{other} conflicts with {id}"));
                }
            }
        }
    }
    ensure!(
        problems.is_empty(),
        "{}",
        problems.into_iter().collect::<Vec<_>>().join("; ")
    );
    Ok(order)
}

fn files(root: &Path) -> Result<Vec<PathBuf>> {
    let mut out: Vec<PathBuf> = vec![];
    for item in WalkDir::new(root)
        .follow_links(false)
        .into_iter()
        .filter_entry(|e| !matches!(e.file_name().to_str(), Some(".git" | "node_modules")))
    {
        let e = item?;
        if e.file_type().is_file() {
            out.push(e.path().strip_prefix(root)?.into());
        }
    }
    let collator = Collator::try_new(Default::default(), CollatorOptions::default())
        .map_err(|e| anyhow::anyhow!("{e}"))?;
    out.sort_by(|a, b| {
        collator
            .compare(
                &a.to_string_lossy().replace('\\', "/"),
                &b.to_string_lossy().replace('\\', "/"),
            )
            .then(a.cmp(b))
    });
    Ok(out)
}
fn hash(root: &Path, files: &[PathBuf]) -> Result<String> {
    let mut sum = Sha256::new();
    let mut buf = [0; 16384];
    for p in files {
        sum.update(p.to_string_lossy().replace('\\', "/"));
        let mut f = fs::File::open(root.join(p))?;
        loop {
            let n = f.read(&mut buf)?;
            if n == 0 {
                break;
            }
            sum.update(&buf[..n]);
        }
    }
    Ok(format!("{:x}", sum.finalize()))
}
fn find_skill(root: &Path, name: &str) -> Result<PathBuf> {
    let mut found = None;
    for entry in WalkDir::new(root)
        .follow_links(false)
        .into_iter()
        .filter_entry(|e| {
            !matches!(
                e.file_name().to_str(),
                Some(".git" | "node_modules" | "target" | "dist" | "build")
            )
        })
    {
        let e = entry?;
        if e.file_type().is_file()
            && e.file_name() == "SKILL.md"
            && document_name(e.path()).is_ok_and(|n| n == name)
        {
            ensure!(found.is_none(), "Ambiguous upstream skill {name}");
            found = Some(e.path().parent().unwrap().to_owned());
        }
    }
    found.with_context(|| format!("Source contains no SKILL.md named {name}"))
}
#[derive(Default)]
struct SourceCache(BTreeMap<(String, String), Arc<TempDir>>);
impl SourceCache {
    fn root(&mut self, c: &Catalog, s: &Value) -> Result<PathBuf> {
        let src = &s["source"];
        if src["kind"] == "local" {
            return confined(
                c.roots
                    .get(field(s, "id"))
                    .context("Missing registry origin")?,
                field(src, "path"),
            );
        }
        ensure!(src["kind"] == "git", "Unsupported skill source");
        let key = (field(src, "uri").to_owned(), field(src, "ref").to_owned());
        // Reuse trees fetched while resolving targets or checking updates so
        // one operation installs exactly the revision it inspected.
        if let Some(tree) = c.trees.get(&key) {
            return Ok(tree.path().into());
        }
        if !self.0.contains_key(&key) {
            self.0
                .insert(key.clone(), Arc::new(github_tree(&key.0, &key.1)?));
        }
        Ok(self.0[&key].path().into())
    }
}

#[derive(Clone)]
struct Destination {
    dir: String,
    link: bool,
}

fn destinations(
    ws: &Workspace,
    settings: &Value,
    selected: &[String],
    copy: bool,
) -> Result<Vec<Destination>> {
    let opts = &settings["installation"];
    if let Some(d) = opts["directory"].as_str().filter(|s| !s.is_empty()) {
        ensure!(
            selected.is_empty(),
            "Custom install directory cannot be combined with explicit agents"
        );
        confined(&ws.root, d)?;
        return Ok(vec![Destination {
            dir: d.into(),
            link: false,
        }]);
    }
    let agents = agents(settings)?;
    let targets: Vec<&str> = if !selected.is_empty() {
        selected.iter().map(String::as_str).collect()
    } else {
        strings(&opts["agents"]).collect()
    };
    ensure!(
        !targets.is_empty(),
        "Select target agents in Settings or pass --agent"
    );
    let mut dirs = BTreeMap::new();
    for id in targets {
        let dir = agents
            .get(id)
            .with_context(|| format!("Unknown agent {id}"))?;
        let link = dir != UNIVERSAL && !copy && opts["method"] != "copy";
        if link {
            let parent = Path::new(dir).parent().unwrap();
            if id != "claude-code" && !ws.root.join(parent).is_dir() {
                continue;
            }
            dirs.insert(UNIVERSAL.to_string(), false);
        }
        dirs.insert(dir.clone(), link);
    }
    ensure!(
        !dirs.is_empty(),
        "No selected agents have a project directory. Use copy mode to create them."
    );
    Ok(dirs
        .into_iter()
        .map(|(dir, link)| Destination { dir, link })
        .collect())
}

// Each replacement is staged on the destination filesystem. Existing trees
// remain recoverable until the matching lock-file write succeeds.
struct Change {
    dest: PathBuf,
    staging: TempDir,
    old: bool,
    applied: bool,
}
impl Change {
    fn prepare(ws: &Workspace, dir: &str, name: &str) -> Result<Self> {
        validate_name(name)?;
        let parent = confined(&ws.root, dir)?;
        fs::create_dir_all(&parent)?;
        let dest = parent.join(name);
        let old = match fs::symlink_metadata(&dest) {
            Ok(m) => {
                ensure!(
                    m.is_dir() || m.is_symlink(),
                    "Refusing to replace a regular file at {}",
                    dest.display()
                );
                let resolved = dest.canonicalize()?;
                ensure!(
                    resolved.starts_with(&ws.root) && resolved.join("SKILL.md").is_file(),
                    "Refusing to replace a non-skill directory at {}",
                    dest.display()
                );
                true
            }
            Err(e) if e.kind() == ErrorKind::NotFound => false,
            Err(e) => return Err(e.into()),
        };
        Ok(Self {
            dest,
            staging: tempfile::Builder::new()
                .prefix(".nosey-")
                .tempdir_in(parent)?,
            old,
            applied: false,
        })
    }
    fn stage(&self, root: &Path, files: &[PathBuf], link: Option<&Path>) -> Result<()> {
        let new = self.staging.path().join("new");
        if let Some(target) = link {
            let target = pathdiff::diff_paths(target, self.dest.parent().unwrap())
                .context("Invalid link target")?;
            #[cfg(unix)]
            std::os::unix::fs::symlink(target, new)?;
            #[cfg(windows)]
            std::os::windows::fs::symlink_dir(target, new)?;
        } else {
            fs::create_dir(&new)?;
            for p in files {
                let out = new.join(p);
                fs::create_dir_all(out.parent().unwrap())?;
                fs::copy(root.join(p), out)?;
            }
        }
        Ok(())
    }
    fn apply(&mut self) -> Result<()> {
        if self.old {
            fs::rename(&self.dest, self.staging.path().join("old"))?;
        }
        self.applied = true;
        let new = self.staging.path().join("new");
        if fs::symlink_metadata(&new).is_ok() {
            fs::rename(new, &self.dest)?;
        }
        Ok(())
    }
    fn rollback(&mut self) -> Result<()> {
        if self.applied {
            if fs::symlink_metadata(&self.dest).is_ok() {
                fs::rename(&self.dest, self.staging.path().join("reverted"))?;
            }
            if self.old {
                fs::rename(self.staging.path().join("old"), &self.dest)?;
            }
        }
        Ok(())
    }
}
fn commit(ws: &Workspace, mut changes: Vec<Change>, lock: &Value) -> Result<()> {
    let result = changes
        .iter_mut()
        .try_for_each(Change::apply)
        .and_then(|_| atomic_json(&confined(&ws.root, LOCK)?, lock));
    if let Err(error) = result {
        for mut change in changes.into_iter().rev() {
            if let Err(e) = change.rollback() {
                let path = change.staging.keep();
                bail!(
                    "{error:#}. Rollback failed: {e:#}. Backup: {}",
                    path.display()
                );
            }
        }
        return Err(error);
    }
    Ok(())
}

pub fn install(
    ws: &Workspace,
    c: &Catalog,
    ids: &[String],
    selected: &[String],
    copy: bool,
    update: bool,
) -> Result<Vec<String>> {
    preflight(ws)?;
    let _op = operation_lock(ws)?;
    let settings = ws.settings()?;
    let items = inventory(ws, c)?;
    let mut lock = read_lock(ws)?;
    let order = selection(c, ids, &items)?;
    let mut cache = SourceCache::default();
    let mut done = vec![];
    let mut base = vec![];
    if update {
        for i in items
            .iter()
            .filter(|i| i.catalog_id.as_ref().is_some_and(|id| ids.contains(id)))
        {
            for d in &i.locations {
                if !base.iter().any(|x: &Destination| x.dir == *d) {
                    base.push(Destination {
                        dir: d.clone(),
                        link: ws.root.join(d).join(&i.name).is_symlink(),
                    });
                }
            }
        }
    } else {
        base = destinations(ws, &settings, selected, copy)?;
    }
    for id in order {
        let s = &c.skills[&id];
        let name = skill_name(s);
        let result = (|| -> Result<()> {
            let mut dest = base.clone();
            if update && let Some(i) = items.iter().find(|i| i.catalog_id.as_deref() == Some(&id)) {
                dest = i
                    .locations
                    .iter()
                    .map(|d| Destination {
                        dir: d.clone(),
                        link: ws.root.join(d).join(name).is_symlink(),
                    })
                    .collect();
            }
            ensure!(!dest.is_empty(), "No installed destinations for {name}");
            if lock["skills"].get(name).is_some() {
                ensure!(
                    belongs(c, s, &lock["skills"][name], ws),
                    "A different source owns the name {name}"
                );
            }
            let root = cache.root(c, s)?;
            let skill = find_skill(&root, name)?;
            let paths = files(&skill)?;
            let canonical = ws.root.join(UNIVERSAL).join(name);
            let mut changes = vec![];
            for d in &dest {
                let ch = Change::prepare(ws, &d.dir, name)?;
                ensure!(
                    !ch.dest.starts_with(&skill) && !skill.starts_with(&ch.dest),
                    "Install source overlaps destination"
                );
                ch.stage(&skill, &paths, d.link.then_some(&canonical))?;
                changes.push(ch);
            }
            let entry = &mut lock["skills"][name];
            let src = &s["source"];
            if src["kind"] == "local" {
                entry["source"] = json!(
                    pathdiff::diff_paths(&root, &ws.root)
                        .unwrap_or(root.clone())
                        .to_string_lossy()
                        .replace('\\', "/")
                );
                entry["sourceType"] = json!("local");
            } else {
                entry["source"] = json!(normalized_source(field(src, "uri")));
                entry["sourceUrl"] = src["uri"].clone();
                entry["sourceType"] = json!("github");
                entry["ref"] = src["ref"].clone();
            }
            entry["computedHash"] = json!(hash(&skill, &paths)?);
            entry["skillPath"] = json!(
                skill
                    .join("SKILL.md")
                    .strip_prefix(root)?
                    .to_string_lossy()
                    .replace('\\', "/")
            );
            let d = field(&settings["installation"], "directory");
            if !update && !d.is_empty() {
                let mut dirs: BTreeSet<_> = recorded(entry)?.into_iter().collect();
                dirs.insert(d.to_owned());
                let agent = entry["noseyInstallation"]["agentDirectories"] == true
                    || items.iter().any(|i| {
                        i.tracked
                            && i.name == name
                            && !i.locations.is_empty()
                            && entry["noseyInstallation"].is_null()
                    });
                entry["noseyInstallation"] = json!({"directories":dirs,"agentDirectories":agent});
            } else if !update && !entry["noseyInstallation"].is_null() {
                entry["noseyInstallation"]["agentDirectories"] = true.into();
            }
            commit(ws, changes, &lock)
        })();
        result
            .with_context(|| format!("Installing {id}. Already completed: {}", done.join(", ")))?;
        done.push(id);
    }
    Ok(done)
}

pub fn uninstall(ws: &Workspace, c: &Catalog, targets: &[String]) -> Result<Vec<String>> {
    let _op = operation_lock(ws)?;
    let items = inventory(ws, c)?;
    let mut lock = read_lock(ws)?;
    let mut chosen = BTreeSet::new();
    for key in targets {
        let matches: Vec<_> = items
            .iter()
            .filter(|i| {
                i.tracked
                    && (i.name == *key || i.catalog_id.as_ref() == Some(key) || i.source == *key)
            })
            .collect();
        ensure!(!matches.is_empty(), "No tracked skill matches {key}");
        for i in matches {
            chosen.insert(i.name.clone());
        }
    }
    let ids: BTreeSet<_> = items
        .iter()
        .filter(|i| chosen.contains(&i.name))
        .filter_map(|i| i.catalog_id.as_deref())
        .collect();
    for i in &items {
        if !chosen.contains(&i.name)
            && let Some(s) = i.catalog_id.as_ref().and_then(|id| c.skills.get(id))
        {
            ensure!(
                !strings(&s["requires"]).any(|r| ids.contains(r)),
                "{} requires a selected skill",
                i.name
            );
        }
    }
    let mut changes = vec![];
    for i in items.iter().filter(|i| chosen.contains(&i.name)) {
        for d in &i.locations {
            changes.push(Change::prepare(ws, d, &i.name)?);
        }
        lock["skills"].as_object_mut().unwrap().remove(&i.name);
    }
    commit(ws, changes, &lock)?;
    Ok(chosen.into_iter().collect())
}

fn custom(c: &mut Catalog, source: Value, root: PathBuf, name: &str) -> Result<String> {
    validate_name(name)?;
    let id = format!("{}@{}", field(&source, "uri"), name);
    let id = if source["kind"] == "local" {
        format!("{}@{name}", root.join(field(&source, "path")).display())
    } else {
        id
    };
    c.skills.insert(
        id.clone(),
        json!({"id":id,"name":name,"source":source,"sourceRegistryId":"user:direct"}),
    );
    c.roots.insert(id.clone(), root);
    Ok(id)
}

pub fn resolve_targets(ws: &Workspace, c: &mut Catalog, args: &[String]) -> Result<Vec<String>> {
    let mut ids = vec![];
    for arg in args {
        if c.skills.contains_key(arg) {
            ids.push(arg.clone());
            continue;
        }
        let local = ws.root.join(arg);
        let (root, src) = if local.is_dir() {
            let p = local.canonicalize()?;
            let name = p
                .file_name()
                .context("Cannot install from filesystem root")?
                .to_string_lossy();
            (p.clone(), json!({"kind":"local","path":name}))
        } else {
            let (uri, rev) = arg.rsplit_once('@').unwrap_or((arg, ""));
            let tree = Arc::new(github_tree(uri, rev)?);
            let root = tree.path().to_owned();
            c.trees.insert((uri.to_owned(), rev.to_owned()), tree);
            (root, json!({"kind":"git","uri":uri,"ref":rev}))
        };
        let mut names = BTreeSet::new();
        for e in WalkDir::new(&root)
            .follow_links(false)
            .into_iter()
            .filter_entry(|e| {
                !matches!(
                    e.file_name().to_str(),
                    Some(".git" | "node_modules" | "target")
                )
            })
        {
            let e = e?;
            if e.file_type().is_file()
                && e.file_name() == "SKILL.md"
                && let Ok(n) = document_name(e.path())
            {
                ensure!(names.insert(n), "Duplicate upstream skill name");
            }
        }
        ensure!(!names.is_empty(), "No skills in {arg}");
        for n in names {
            let mut s = src.clone();
            s["skillName"] = json!(n);
            ids.push(custom(c, s, root.parent().unwrap_or(&root).into(), &n)?);
        }
    }
    Ok(ids)
}
/// Compares every tracked skill with its source. A skill that cannot be
/// checked records the reason in `skipped` instead of failing the others.
pub fn check_updates(ws: &Workspace, c: &mut Catalog, items: &mut [Installed]) -> Result<()> {
    let lock = read_lock(ws)?;
    let mut cache = SourceCache::default();
    for i in items.iter_mut().filter(|i| i.tracked) {
        let entry = &lock["skills"][&i.name];
        if i.catalog_id.is_none() {
            let custom_source = match field(entry, "sourceType") {
                "local" => ws
                    .root
                    .join(&i.source)
                    .canonicalize()
                    .ok()
                    .and_then(|path| {
                        Some((
                            json!({"kind":"local","path":path.file_name()?.to_str()?,"skillName":i.name}),
                            path.parent()?.to_owned(),
                        ))
                    })
                    .ok_or("its local source no longer exists"),
                "github" => Ok((
                    json!({"kind":"git","uri":i.source,"ref":field(entry,"ref"),"skillName":i.name}),
                    ws.root.clone(),
                )),
                _ => Err("its source cannot be checked automatically; reinstall to enable updates"),
            };
            match custom_source {
                Ok((src, root)) => i.catalog_id = Some(custom(c, src, root, &i.name)?),
                Err(reason) => {
                    i.skipped = Some(reason.into());
                    continue;
                }
            }
        }
        let s = &c.skills[i.catalog_id.as_ref().unwrap()];
        let result = (|| -> Result<bool> {
            let root = cache.root(c, s)?;
            let source = find_skill(&root, &i.name)?;
            let expected = hash(&source, &files(&source)?)?;
            let mut outdated = false;
            for d in &i.locations {
                let path = confined(&ws.root, d)?.join(&i.name).canonicalize()?;
                ensure!(path.starts_with(&ws.root), "Skill points outside workspace");
                outdated |= hash(&path, &files(&path)?)? != expected;
            }
            Ok(outdated)
        })();
        match result {
            Ok(outdated) => i.outdated |= outdated,
            Err(e) => i.skipped = Some(format!("{e:#}")),
        }
    }
    for (key, tree) in cache.0 {
        c.trees.entry(key).or_insert(tree);
    }
    Ok(())
}

pub fn preview_destinations(ws: &Workspace, agents: &[String], copy: bool) -> Result<Vec<String>> {
    Ok(destinations(ws, &ws.settings()?, agents, copy)?
        .into_iter()
        .map(|d| d.dir)
        .collect())
}
