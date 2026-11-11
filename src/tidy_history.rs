//! `stw tidy history`: list the stored tidy manifests (the journals of `tidy apply`), newest first, read-only.
//!
//! Ported from the Go `stw tidy history` (`internal/workspace/tidy_v3_history.go`, Spec 031 FR-047): one row per stored
//! journal with its counts, then every bound as a visible truncation marker or a journal status, never a silent omission.
//! The journal is the manifest of `tidy apply` (`<DIR>/<run>.jsonl`, README "tidy apply and tidy restore"); its format is
//! not changed, and `tidy restore` reads the same files. A row says how many branches the run recorded (`deleted`) and how
//! many of them `tidy restore` could create again now (`restorable`: the branch does not exist, the commit is in the
//! repository). Nothing is written, no ref is moved, no repository is changed.
//!
//! Bounds (as in Go): at most 4096 manifests are scanned, `--limit` (default 256, at most 1024) are shown, at most 65536
//! entries are checked against the repositories, and the check stops after 30 seconds. A manifest that cannot be read is
//! listed as `unreadable`, one that was not checked because of a bound as `incomplete`.
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use serde_json::{json, Value};

use crate::error::{Error, Result};
use crate::git_runner::{BranchRef, Hex40};
use crate::github::reads::escape_untrusted;
use crate::output::*;
use crate::tidy_apply::ManifestEntry;

pub const DEFAULT_LIMIT: usize = 256;
pub const MAX_LIMIT: usize = 1024;
const SCAN_LIMIT: usize = 4096;
const OBJECT_LINES: usize = 65_536;
const BUDGET: Duration = Duration::from_secs(30);
const MAX_MANIFEST_BYTES: u64 = 4 << 20;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HistoryEntry {
    /// The run ID (the file name without `.jsonl`).
    pub run: String,
    /// Modification time of the file, `YYYY-MM-DDTHH:MM:SSZ`.
    pub time: String,
    /// `local-refs` (a manifest records deleted local branches), `unknown` when it was not read.
    pub mode: &'static str,
    /// Entries of the manifest: the branches the run recorded before it deleted them.
    pub deleted: usize,
    /// Of those, the ones `tidy restore` could create now.
    pub restorable: usize,
    /// `complete` (read), `incomplete` (not checked because of a bound), `unreadable`.
    pub status: &'static str,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Truncation {
    pub unit: &'static str,
    pub shown: usize,
    pub total: usize,
}

#[derive(Clone, Debug)]
pub struct History {
    pub manifest_dir: PathBuf,
    pub entries: Vec<HistoryEntry>,
    pub total: usize,
    pub scan_truncated: bool,
    pub truncated: Vec<Truncation>,
}

/// A run ID as a file stem: `[A-Za-z0-9._-]`, 1 to 128 characters, not starting with a dot.
fn valid_id(s: &str) -> bool {
    !s.is_empty()
        && s.len() <= 128
        && !s.starts_with('.')
        && s.bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'_' | b'-'))
}

struct Row {
    entry: HistoryEntry,
    /// Entries to check against the repositories.
    candidates: Vec<ManifestEntry>,
}

/// Read a manifest: every line must be a JSON entry of the `tidy apply` format.
fn read_entries(path: &Path) -> std::result::Result<Vec<ManifestEntry>, String> {
    use std::io::Read;
    let mut text = String::new();
    std::fs::File::open(path)
        .and_then(|f| f.take(MAX_MANIFEST_BYTES + 1).read_to_string(&mut text))
        .map_err(|e| e.to_string())?;
    if text.len() as u64 > MAX_MANIFEST_BYTES {
        return Err("larger than 4 MiB".into());
    }
    let mut out = Vec::new();
    for (n, line) in text
        .lines()
        .enumerate()
        .filter(|(_, l)| !l.trim().is_empty())
    {
        let v: Value = serde_json::from_str(line).map_err(|e| format!("line {}: {e}", n + 1))?;
        out.push(
            ManifestEntry::from_value(&v)
                .ok_or_else(|| format!("line {}: not a manifest entry", n + 1))?,
        );
    }
    Ok(out)
}

/// Whether `tidy restore` could create the branch of `e` now: the repository opens, the branch does not exist, the commit
/// is there. Everything in the entry is data: the path must be absolute, the branch a valid `refs/heads/` name, the commit
/// 40 lower-case hex digits.
fn restorable(e: &ManifestEntry, cache: &mut HashMap<String, Option<gix::Repository>>) -> bool {
    let (Some(branch), Some(sha)) = (
        BranchRef::parse(&format!("refs/heads/{}", e.branch)),
        Hex40::parse(&e.sha),
    ) else {
        return false;
    };
    let repo = cache.entry(e.repo.clone()).or_insert_with(|| {
        let p = Path::new(&e.repo);
        (p.is_absolute() && p.is_dir())
            .then(|| crate::repo::open(p).ok())
            .flatten()
    });
    let Some(repo) = repo else { return false };
    let free = matches!(repo.try_find_reference(branch.as_str()), Ok(None));
    let id = gix::ObjectId::from_hex(sha.as_str().as_bytes()).expect("40 hex digits");
    free && repo.has_object(id)
}

/// The history of the manifests in `dir`. A directory that does not exist is an empty history (nothing was applied yet).
pub fn history(dir: &Path, limit: usize) -> Result<History> {
    history_with(dir, limit, SCAN_LIMIT, OBJECT_LINES, BUDGET)
}

/// `history` with the bounds as parameters (tests lower them).
pub fn history_with(
    dir: &Path,
    limit: usize,
    scan_limit: usize,
    object_lines: usize,
    budget: Duration,
) -> Result<History> {
    let started = Instant::now();
    let mut found: Vec<(String, i64)> = Vec::new();
    let mut scan_truncated = false;
    match std::fs::read_dir(dir) {
        Ok(rd) => {
            for e in rd {
                let e = e.map_err(|e| {
                    Error::repository(format!("cannot read {}: {e}", dir.display()))
                })?;
                if found.len() >= scan_limit {
                    scan_truncated = true;
                    break;
                }
                let name = e.file_name();
                let Some(id) = name
                    .to_str()
                    .and_then(|n| n.strip_suffix(".jsonl"))
                    .filter(|s| valid_id(s))
                else {
                    continue;
                };
                // a link or a directory under a manifest's name is not a manifest
                let Ok(m) = std::fs::symlink_metadata(e.path()) else {
                    continue;
                };
                if !m.is_file() {
                    continue;
                }
                let secs = m
                    .modified()
                    .ok()
                    .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
                    .map_or(0, |d| d.as_secs() as i64);
                found.push((id.to_string(), secs));
            }
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
        Err(e) => {
            return Err(Error::repository(format!(
                "cannot read {}: {e}",
                dir.display()
            )))
        }
    }
    // newest run first: run IDs sort by time, so the first row is the run `tidy restore` takes without `--run`
    found.sort_by(|a, b| b.0.cmp(&a.0));
    let total = found.len();
    let shown = &found[..total.min(limit)];
    let mut rows: Vec<Row> = Vec::new();
    let (mut checked, mut wanted, mut counted) = (0usize, 0usize, 0usize);
    for (id, secs) in shown {
        let mut row = Row {
            entry: HistoryEntry {
                run: id.clone(),
                time: crate::lint_pr_cmd::rfc3339_utc(*secs),
                mode: "unknown",
                deleted: 0,
                restorable: 0,
                status: "incomplete",
            },
            candidates: Vec::new(),
        };
        if started.elapsed() > budget {
            rows.push(row);
            continue;
        }
        checked += 1;
        match read_entries(&dir.join(format!("{id}.jsonl"))) {
            Err(_) => row.entry.status = "unreadable",
            Ok(entries) => {
                row.entry.mode = "local-refs";
                row.entry.deleted = entries.len();
                wanted += entries.len();
                if counted + entries.len() > object_lines {
                    // over the bound: listed, not checked
                } else {
                    counted += entries.len();
                    row.entry.status = "complete";
                    row.candidates = entries;
                }
            }
        }
        rows.push(row);
    }
    for row in &mut rows {
        if row.candidates.is_empty() {
            continue;
        }
        if started.elapsed() > budget {
            row.entry.status = "incomplete";
            continue;
        }
        let mut cache: HashMap<String, Option<gix::Repository>> = HashMap::new();
        row.entry.restorable = row
            .candidates
            .iter()
            .filter(|e| restorable(e, &mut cache))
            .count();
    }
    let mut truncated = Vec::new();
    if shown.len() < total || scan_truncated {
        truncated.push(Truncation {
            unit: "journals",
            shown: shown.len(),
            total,
        });
    }
    if wanted > counted {
        truncated.push(Truncation {
            unit: "objects",
            shown: counted,
            total: wanted,
        });
    }
    if checked < shown.len() {
        truncated.push(Truncation {
            unit: "checked-journals",
            shown: checked,
            total: shown.len(),
        });
    }
    Ok(History {
        manifest_dir: dir.to_path_buf(),
        entries: rows.into_iter().map(|r| r.entry).collect(),
        total,
        scan_truncated,
        truncated,
    })
}

pub fn to_json(h: &History) -> Value {
    json!({"schema_version": 2, "command": "tidy-history", "read_only": true, "manifest_dir": p2s(&h.manifest_dir),
           "engine": engine(), "status": "ok", "total": h.total, "scan_truncated": h.scan_truncated,
           "entries": h.entries.iter().map(|e| json!({"run": e.run, "time": e.time, "mode": e.mode,
                         "deleted": e.deleted, "restorable": e.restorable, "status": e.status})).collect::<Vec<_>>(),
           "truncated": h.truncated.iter().map(|t| json!({"unit": t.unit, "shown": t.shown, "total": t.total})).collect::<Vec<_>>()})
}

pub fn render_human(h: &History) -> String {
    let dir = escape_untrusted(&p2s(&h.manifest_dir), 300).0;
    let mut out = format!(
        "{} journal(s) in {dir}, newest first{}\n",
        h.total,
        if h.scan_truncated {
            " (scan stopped at its bound)"
        } else {
            ""
        }
    );
    for e in &h.entries {
        out += &format!(
            "  {:<32} {}  deleted {:<4} restorable {:<4} {}\n",
            escape_untrusted(&e.run, 128).0,
            e.time,
            e.deleted,
            e.restorable,
            e.status
        );
    }
    for t in &h.truncated {
        out += &format!("truncated: {} {} of {}\n", t.unit, t.shown, t.total);
    }
    out
}
