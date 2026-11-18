use anyhow::{Context, Result, ensure};
use flate2::read::GzDecoder;
use std::{
    fs,
    io::{self, Read},
    path::{Component, Path},
    time::Duration,
};
use tempfile::TempDir;

/// Download data only. Links, device nodes, permissions and Git metadata from
/// an upstream archive must never affect the destination filesystem.
pub fn github_tree(uri: &str, revision: &str) -> Result<TempDir> {
    let slug = uri
        .trim_end_matches('/')
        .trim_end_matches(".git")
        .trim_start_matches("https://github.com/");
    let mut parts = slug.split('/');
    let valid = |s: &str| {
        !s.is_empty()
            && s != "."
            && s != ".."
            && s.bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"._-".contains(&b))
    };
    ensure!(
        parts.next().is_some_and(valid)
            && parts.next().is_some_and(valid)
            && parts.next().is_none(),
        "Expected a GitHub repository, got {uri:?}"
    );
    let revision = if revision.is_empty() {
        "HEAD"
    } else {
        revision
    };
    ensure!(revision.split('/').all(valid), "Invalid Git ref");
    let client = reqwest::blocking::Client::builder()
        .timeout(Duration::from_secs(120))
        .build()?;
    let response = client
        .get(format!(
            "https://codeload.github.com/{slug}/tar.gz/{revision}"
        ))
        .send()
        .and_then(reqwest::blocking::Response::error_for_status)
        .with_context(|| format!("Fetching {slug} at {revision}"))?;
    let dir = tempfile::tempdir()?;
    let reader = response.take(128 << 20);
    let mut archive = tar::Archive::new(GzDecoder::new(reader).take(512 << 20));
    for (i, entry) in archive.entries()?.enumerate() {
        ensure!(i < 100_000, "Archive contains too many files");
        let mut entry = entry?;
        if !entry.header().entry_type().is_file() {
            continue;
        }
        let path = entry.path()?.into_owned();
        let mut parts = path.components();
        ensure!(
            matches!(parts.next(), Some(Component::Normal(_))),
            "Invalid archive prefix"
        );
        let rel = parts.as_path();
        ensure!(
            !rel.as_os_str().is_empty()
                && rel.components().all(|c| matches!(c, Component::Normal(_))),
            "Invalid archive path"
        );
        if rel
            .components()
            .any(|c| c.as_os_str().eq_ignore_ascii_case(".git"))
        {
            continue;
        }
        let dest = dir.path().join(rel);
        fs::create_dir_all(dest.parent().unwrap())?;
        let mut file = fs::File::create_new(dest)?;
        io::copy(&mut entry, &mut file)?;
        #[cfg(unix)]
        if entry.header().mode()? & 0o111 != 0 {
            use std::os::unix::fs::PermissionsExt;
            file.set_permissions(fs::Permissions::from_mode(0o755))?;
        }
    }
    Ok(dir)
}
pub fn document_name(path: &Path) -> Result<String> {
    let text = fs::read_to_string(path)?;
    let mut lines = text.lines();
    ensure!(lines.next() == Some("---"), "Missing front matter");
    let mut front = String::new();
    for line in lines {
        if line == "---" {
            let doc: serde_json::Value = serde_yaml_ng::from_str(&front)?;
            return Ok(doc["name"]
                .as_str()
                .context("Missing skill name")?
                .to_owned());
        }
        front.push_str(line);
        front.push('\n');
    }
    anyhow::bail!("Unterminated front matter")
}
