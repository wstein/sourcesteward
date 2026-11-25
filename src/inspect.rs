//! Read-only comparison for published PRs and local Git objects.
use std::collections::HashSet;
use std::path::Path;

use serde::Serialize;
use serde_json::{json, Value};

use crate::error::{Error, Result};
use crate::github::{Client, GithubState};

#[derive(Serialize)]
struct File {
    path: String,
    status: String,
    previous_path: Option<String>,
    rules: Vec<Value>,
    test_suggestions: Vec<Value>,
}

impl File {
    fn new(path: String, status: &str, previous_path: Option<String>) -> Self {
        Self {
            path,
            status: status.into(),
            previous_path,
            rules: vec![],
            test_suggestions: vec![],
        }
    }

    fn context(&mut self, old: impl FnMut(&str) -> bool, mut new: impl FnMut(&str) -> bool) {
        if let Some(path) = self
            .previous_path
            .as_deref()
            .or_else(|| (self.status == "removed").then_some(self.path.as_str()))
        {
            rules(&mut self.rules, path, "base", old);
        }
        if self.status == "removed" {
            return;
        }
        rules(&mut self.rules, &self.path, "head", &mut new);
        let path = Path::new(&self.path);
        let Some(stem) = path
            .file_stem()
            .and_then(|name| name.to_str())
            .filter(|stem| {
                !stem.is_empty()
                    && stem
                        .bytes()
                        .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'-'))
            })
        else {
            return;
        };
        if path.extension().is_none_or(|ext| ext != "rs") {
            return;
        }
        for parent in path.ancestors().skip(1) {
            let manifest = parent.join("Cargo.toml").to_string_lossy().into_owned();
            if !new(&manifest) {
                continue;
            }
            let Some(relative) = path.strip_prefix(parent).ok().and_then(Path::to_str) else {
                break;
            };
            let parts: Vec<_> = relative.split('/').collect();
            let mut argv = vec!["cargo", "test", "--locked", "--test"];
            let filter = format!("{stem}::");
            match parts.as_slice() {
                ["tests", _] => argv.push(stem),
                ["tests", "it", _]
                    if stem != "main"
                        && new(&parent.join("tests/it/main.rs").to_string_lossy()) =>
                {
                    argv.extend(["it", &filter]);
                }
                _ => break,
            }
            let dir = parent.to_str().filter(|p| !p.is_empty()).unwrap_or(".");
            self.test_suggestions
                .push(json!({"directory": dir, "argv": argv, "sources": [manifest, self.path]}));
            break;
        }
    }
}

fn rules(out: &mut Vec<Value>, path: &str, side: &str, mut has: impl FnMut(&str) -> bool) {
    let ancestors: Vec<_> = Path::new(path).ancestors().skip(1).collect();
    for parent in ancestors.into_iter().rev() {
        let path = parent.join("AGENTS.md").to_string_lossy().into_owned();
        if has(&path) {
            out.push(json!({"side": side, "path": path}));
        }
    }
}

#[derive(Serialize)]
pub struct Report {
    schema_version: u8,
    command: &'static str,
    source: &'static str,
    requested: Value,
    base: Option<String>,
    head: Option<String>,
    comparison_base: Option<String>,
    files: Vec<File>,
    incomplete: Vec<String>,
}

impl Report {
    fn new(source: &'static str, requested: Value) -> Self {
        Self {
            schema_version: 1,
            command: "inspect",
            source,
            requested,
            base: None,
            head: None,
            comparison_base: None,
            files: vec![],
            incomplete: vec![],
        }
    }

    pub fn complete(&self) -> bool {
        self.incomplete.is_empty()
    }

    fn pin(&mut self, base: String, head: String) {
        self.base = Some(base);
        self.head = Some(head);
    }
}

pub fn local(path: &Path, base: &str, head: &str) -> Report {
    let mut report = Report::new("git", json!({"base": base, "head": head}));
    if let Err(error) = read_local(&mut report, path, base, head) {
        report
            .incomplete
            .push(format!("comparison-unavailable: {error}"));
    }
    report
}

fn read_local(report: &mut Report, path: &Path, base: &str, head: &str) -> Result<()> {
    use gix::object::tree::diff::{Action, Change};
    let discovered = gix::discover(path)?;
    let repo = crate::repo::open(discovered.workdir().unwrap_or_else(|| discovered.git_dir()))?;
    let resolve = |revision: &str| -> Result<_> {
        Ok(repo
            .rev_parse_single(format!("{revision}^{{commit}}").as_str())?
            .detach())
    };
    let base = resolve(base)?;
    let head = resolve(head)?;
    report.pin(base.to_string(), head.to_string());
    let merge = repo
        .merge_base(base, head)?
        .ok_or_else(|| Error::repository("no merge base"))?;
    report.comparison_base = Some(merge.to_string());
    let old = repo.find_commit(merge.detach())?.tree()?;
    let new = repo.find_commit(head)?.tree()?;
    let outcome = old
        .changes()?
        .options(|options| {
            options
                .track_path()
                .track_rewrites(Some(Default::default()));
        })
        .for_each_to_obtain_tree(&new, |event| {
            let (path, status, previous) = match event {
                Change::Addition {
                    location,
                    entry_mode,
                    ..
                } if !entry_mode.is_tree() => (location, "added", None),
                Change::Deletion {
                    location,
                    entry_mode,
                    ..
                } if !entry_mode.is_tree() => (location, "removed", None),
                Change::Modification {
                    location,
                    entry_mode,
                    previous_entry_mode,
                    ..
                } => match (previous_entry_mode.is_tree(), entry_mode.is_tree()) {
                    (false, false) => (location, "modified", None),
                    (true, false) => (location, "added", None),
                    (false, true) => (location, "removed", None),
                    (true, true) => return Ok(Action::Continue(())),
                },
                Change::Rewrite {
                    location,
                    source_location,
                    entry_mode,
                    copy,
                    ..
                } if !entry_mode.is_tree() => (
                    location,
                    if copy { "copied" } else { "renamed" },
                    Some(source_location),
                ),
                _ => return Ok(Action::Continue(())),
            };
            match (
                std::str::from_utf8(path),
                previous.map(|p| std::str::from_utf8(p)).transpose(),
            ) {
                (Ok(path), Ok(previous)) => {
                    report
                        .files
                        .push(File::new(path.into(), status, previous.map(str::to_owned)))
                }
                _ => {
                    if !report
                        .incomplete
                        .iter()
                        .any(|reason| reason == "non-utf8-path")
                    {
                        report.incomplete.push("non-utf8-path".into());
                    }
                }
            }
            Ok(Action::Continue(()))
        })
        .map_err(|error| Error::repository(error.to_string()))?;
    if outcome.is_some_and(|o| o.num_similarity_checks_skipped_for_rename_tracking_due_to_limit > 0)
    {
        report.incomplete.push("rename-detection-limit".into());
    }
    let mut missing_context = false;
    for file in &mut report.files {
        let mut old_failed = false;
        let mut new_failed = false;
        file.context(
            |path| tracked_locally(&old, path, &mut old_failed),
            |path| tracked_locally(&new, path, &mut new_failed),
        );
        missing_context |= old_failed || new_failed;
    }
    if missing_context {
        report.incomplete.push("local-context-unavailable".into());
    }
    report.files.sort_by(|a, b| a.path.cmp(&b.path));
    Ok(())
}

fn tracked_locally(tree: &gix::Tree<'_>, path: &str, failed: &mut bool) -> bool {
    match tree.lookup_entry_by_path(path) {
        Ok(entry) => entry.is_some_and(|entry| entry.mode().is_blob()),
        Err(_) => {
            *failed = true;
            false
        }
    }
}

pub fn published(slug: &str, number: u64) -> Report {
    let mut report = Report::new("github", json!({"repo": slug, "pr": number}));
    let github = GithubState::discover(false);
    let result = github
        .client()
        .ok_or_else(|| Error::github("no GitHub token"))
        .and_then(|client| read_published(&mut report, client, slug, number));
    if let Err(error) = result {
        report
            .incomplete
            .push(format!("comparison-unavailable: {error}"));
    }
    report
}

fn sha(value: &Value) -> Result<String> {
    value
        .as_str()
        .filter(|sha| {
            sha.len() == 40 && sha.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
        })
        .map(str::to_owned)
        .ok_or_else(|| Error::github("missing or invalid commit SHA"))
}

fn read_published(report: &mut Report, client: &Client, slug: &str, number: u64) -> Result<()> {
    client.arm_deadline(std::time::Duration::from_secs(90));
    let read_pull = || -> Result<_> {
        let doc = client.lint_pull(slug, number)?.body;
        Ok((sha(&doc["base"]["sha"])?, sha(&doc["head"]["sha"])?))
    };
    let (base, head) = read_pull()?;
    report.pin(base.clone(), head.clone());
    let comparison =
        client.inspection_get(slug, &format!("compare/{base}...{head}?per_page=1&page=1"))?;
    let merge = sha(&comparison["merge_base_commit"]["sha"])?;
    report.comparison_base = Some(merge.clone());
    let files = comparison["files"]
        .as_array()
        .ok_or_else(|| Error::github("comparison has no file list"))?;
    if files.len() >= 300 {
        report
            .incomplete
            .push("github-comparison-file-limit".into());
    }
    for row in files {
        let (Some(path), Some(status)) = (row["filename"].as_str(), row["status"].as_str()) else {
            report.incomplete.push("invalid-github-file".into());
            continue;
        };
        let previous = row["previous_filename"].as_str().map(str::to_owned);
        if matches!(status, "renamed" | "copied") && previous.is_none() {
            report
                .incomplete
                .push("missing-github-previous-path".into());
        }
        report.files.push(File::new(path.into(), status, previous));
    }
    if !report.files.is_empty() {
        let old = github_paths(report, client, slug, &merge);
        let new = if head == merge {
            old.clone()
        } else {
            github_paths(report, client, slug, &head)
        };
        for file in &mut report.files {
            file.context(|p| old.contains(p), |p| new.contains(p));
        }
    }
    match read_pull() {
        Ok(current) if current == (base, head) => {}
        Ok(_) => report.incomplete.push("pull-request-moved".into()),
        Err(error) => report
            .incomplete
            .push(format!("pull-request-recheck-unavailable: {error}")),
    }
    Ok(())
}

fn github_paths(
    report: &mut Report,
    client: &Client,
    slug: &str,
    revision: &str,
) -> HashSet<String> {
    let read = client.inspection_get(slug, &format!("git/trees/{revision}?recursive=1"));
    if let Ok(doc) = read {
        if let Some(entries) = doc["tree"].as_array() {
            if doc["truncated"].as_bool() != Some(false) {
                report
                    .incomplete
                    .push(format!("github-tree-incomplete:{revision}"));
            }
            return entries
                .iter()
                .filter(|node| node["type"] == "blob")
                .filter_map(|node| node["path"].as_str().map(str::to_owned))
                .collect();
        }
    }
    report
        .incomplete
        .push(format!("github-context-unavailable:{revision}"));
    HashSet::new()
}
