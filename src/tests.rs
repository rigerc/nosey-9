use crate::{
    catalog::Catalog,
    install, scan,
    workspace::{Workspace, atomic_json},
};
use serde_json::json;
use std::{fs, path::PathBuf};
use tempfile::TempDir;

pub struct Fixture {
    pub _temp: TempDir,
    pub ws: Workspace,
    pub registry: PathBuf,
}
impl Fixture {
    pub fn new() -> Self {
        let t = tempfile::tempdir().unwrap();
        let base = t.path().canonicalize().unwrap();
        let reg = base.join("registry");
        let root = base.join("project");
        fs::create_dir_all(&reg).unwrap();
        fs::create_dir_all(&root).unwrap();
        atomic_json(&reg.join("nosey.registry.json"),&json!({"schemaVersion":1,"id":"test:local","name":"Test","files":{"skills":"skills.json","mappings":"mappings.json","technologies":"technologies.json","repositories":"repositories.json"}})).unwrap();
        atomic_json(
            &reg.join("repositories.json"),
            &json!({"schemaVersion":1,"repositories":[]}),
        )
        .unwrap();
        let mut skills = vec![];
        for (name, deps) in [("alpha", vec!["test:beta"]), ("beta", vec![])] {
            let dir = reg.join("local").join(name);
            fs::create_dir_all(&dir).unwrap();
            fs::write(
                dir.join("SKILL.md"),
                format!("---\nname: {name}\ndescription: Example\n---\nOriginal"),
            )
            .unwrap();
            skills.push(json!({"id":format!("test:{name}"),"name":name,"requires":deps,"technologies":["react"],"source":{"kind":"local","path":"local","skillName":name},"repositoryId":"test:examples"}));
        }
        atomic_json(
            &reg.join("skills.json"),
            &json!({"schemaVersion":2,"skills":skills}),
        )
        .unwrap();
        atomic_json(&reg.join("technologies.json"),&json!({"schemaVersion":2,"technologies":[{"id":"react","name":"React","category":"framework","rules":[{"kind":"dependency","id":"dep","name":"react","ecosystem":"npm"}]}]})).unwrap();
        atomic_json(&reg.join("mappings.json"),&json!({"schemaVersion":2,"mappings":[{"id":"react-skills","technologyId":"react","recommendations":[{"skillId":"test:alpha"}]}]})).unwrap();
        let ws = Workspace {
            root,
            registry: Some(reg.clone()),
            data: base.join("data"),
            config: base.join("user/config.json"),
            cache: base.join("cache"),
        };
        ws.set("installation.agents", json!(["codex"]), true)
            .unwrap();
        Self {
            _temp: t,
            registry: reg,
            ws,
        }
    }
}
#[test]
fn scans_nested_manifests_and_honors_ignores() {
    let f = Fixture::new();
    let c = Catalog::load(&f.ws, false).unwrap();
    assert_eq!(c.search("ALPHA").len(), 1);
    for d in ["web", "hidden", "node_modules/fake"] {
        let path = f.ws.root.join(d);
        fs::create_dir_all(&path).unwrap();
        fs::write(
            path.join("package.json"),
            r#"{"dependencies":{"react":"19"}}"#,
        )
        .unwrap();
    }
    fs::write(f.ws.root.join(".gitignore"), "hidden/\n").unwrap();
    let scan = scan::scan(&f.ws, &c).unwrap();
    assert_eq!(
        scan.technologies["react"].iter().collect::<Vec<_>>(),
        vec!["web/package.json"]
    );
    assert!(scan.recommendations.contains_key("test:alpha"));
}
#[test]
fn dependency_order_update_locations_and_safe_removal() {
    let f = Fixture::new();
    let ws = &f.ws;
    let mut c = Catalog::load(ws, false).unwrap();
    let ids = vec!["test:alpha".into()];
    assert_eq!(
        install::install(ws, &c, &ids, &[], false, false).unwrap(),
        vec!["test:beta", "test:alpha"]
    );
    assert!(install::uninstall(ws, &c, &["beta".into()]).is_err());
    ws.set("installation.directory", json!("tools/skills"), true)
        .unwrap();
    install::install(ws, &c, &ids, &[], false, false).unwrap();
    ws.set("installation.directory", json!(""), true).unwrap();
    fs::write(f.registry.join("local/alpha/notes.txt"), "New content").unwrap();
    let mut items = install::inventory(ws, &c).unwrap();
    install::check_updates(ws, &mut c, &mut items).unwrap();
    assert!(items.iter().find(|i| i.name == "alpha").unwrap().outdated);
    install::install(ws, &c, &ids, &[], false, true).unwrap();
    for d in [".agents/skills", "tools/skills"] {
        assert_eq!(
            fs::read_to_string(ws.root.join(d).join("alpha/notes.txt")).unwrap(),
            "New content"
        );
    }
    install::uninstall(ws, &c, &["alpha".into(), "beta".into()]).unwrap();
    assert!(install::inventory(ws, &c).unwrap().is_empty());
}
#[test]
fn invalid_lock_untracked_files_and_outside_links_are_preserved() {
    let f = Fixture::new();
    let c = Catalog::load(&f.ws, false).unwrap();
    let keep = f.ws.root.join(".agents/skills/beta");
    fs::create_dir_all(&keep).unwrap();
    fs::write(keep.join("user.txt"), "keep").unwrap();
    assert!(install::install(&f.ws, &c, &["test:beta".into()], &[], false, false).is_err());
    assert!(keep.join("user.txt").exists());
    let lock = f.ws.root.join("skills-lock.json");
    fs::write(&lock, "bad lock").unwrap();
    assert!(install::uninstall(&f.ws, &c, &["beta".into()]).is_err());
    assert_eq!(fs::read_to_string(lock).unwrap(), "bad lock");
    #[cfg(unix)]
    {
        std::os::unix::fs::symlink(&f.registry, f.ws.root.join("outside")).unwrap();
        assert!(crate::workspace::confined(&f.ws.root, "outside/skill").is_err());
    }
}
#[test]
fn dependency_cycles_and_conflicts_are_rejected_before_writes() {
    let f = Fixture::new();
    let mut c = Catalog::load(&f.ws, false).unwrap();
    c.skills.get_mut("test:beta").unwrap()["requires"] = json!(["test:alpha"]);
    assert!(install::selection(&c, &["test:alpha".into()], &[]).is_err());
    c.skills.get_mut("test:beta").unwrap()["requires"] = json!([]);
    c.skills.get_mut("test:beta").unwrap()["conflictsWith"] = json!(["test:alpha"]);
    assert!(install::install(&f.ws, &c, &["test:alpha".into()], &[], false, false).is_err());
    assert!(!f.ws.root.join("skills-lock.json").exists());
}
