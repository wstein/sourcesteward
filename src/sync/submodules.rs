//! `stw sync --submodules`: bring each pinned (detached) submodule to the commit the superproject records, when
//! that is a fast-forward of its HEAD. Port of `internal/stwsync/submodules.go` (`submoduleStep`).
//!
//! Reads (recorded commit, object, ancestry, local changes, URLs) go through gix. The two writes are
//! `git submodule sync -- <path>` and `git submodule update --no-fetch --checkout -- <path>` in the superproject,
//! through the runner (`Op::SubmoduleSync`, `Op::SubmoduleUpdate`): never a clone, init, fetch or recursion, and
//! no gitlink, index or superproject commit is written. A move is reported with the old and the new commit; it is
//! not undone by anything. The pass runs after every repository was synced (so the superproject's own
//! fast-forward is done and its new gitlinks count), one submodule after the other.
//!
//! The submodules of every level are visited (the gitlinks of each checked-out repository's index, nested ones too):
//! a nested pinned submodule is stepped against the submodule that holds its gitlink (`holder_of`).
use std::path::{Path, PathBuf};

use serde_json::{json, Value};

use super::{Opts, SyncRec};
use crate::add::SubPath;
use crate::git_runner::{self, Op};
use crate::github::reads::escape_untrusted;
use crate::repo;
use crate::status;

/// What `--submodules` found or did for one submodule.
#[derive(Clone, Debug, Default)]
pub struct SubRec {
    /// `moved`, `would-move`, `up-to-date`, `skipped-dirty`, `not-fast-forward`, `unreachable`, `failed`.
    pub state: &'static str,
    pub old: Option<String>,
    pub new: Option<String>,
    /// URL rewrites by `git submodule sync` (escaped).
    pub note: Option<String>,
}

impl SubRec {
    pub fn to_json(&self) -> Value {
        json!({"state": self.state, "old": self.old, "new": self.new, "note": self.note})
    }
}

fn esc(s: &str) -> String {
    escape_untrusted(s, 200).0
}

fn sh(id: &str) -> &str {
    id.get(..7).unwrap_or(id)
}

/// Set the outcome of the submodule step on the record.
fn put(rec: &mut SyncRec, result: &str, reason: String, sub: SubRec) {
    rec.result = result.to_string();
    rec.reason = reason;
    rec.submodule = Some(sub);
}

fn unreachable_rec(rec: &mut SyncRec, o: &Opts, why: &str, sub: SubRec) {
    // a dry run only predicts; the fetch of a run may still bring a missing commit
    let (result, reason) = if o.apply {
        ("error", format!("unreachable: {why}"))
    } else {
        ("skip", format!("would be unreachable: {why}"))
    };
    put(
        rec,
        result,
        reason,
        SubRec {
            state: "unreachable",
            ..sub
        },
    );
}

/// Whether a component of `rel` below `root` is a symbolic link.
fn symlinked_path(root: &Path, rel: &str) -> bool {
    let mut cur = root.to_path_buf();
    for seg in rel.split('/') {
        cur.push(seg);
        if std::fs::symlink_metadata(&cur).is_ok_and(|m| m.file_type().is_symlink()) {
            return true;
        }
    }
    false
}

/// The commit the superproject's index records for the submodule at `rel` (stage 0 only).
fn recorded_commit(root: &gix::Repository, rel: &str) -> Option<gix::ObjectId> {
    let index = root.index_or_empty().ok()?;
    let e = index.entry_by_path_and_stage(
        gix::bstr::BStr::new(rel.as_bytes()),
        gix::index::entry::Stage::Unconflicted,
    )?;
    e.mode.is_submodule().then_some(e.id)
}

/// The name `.gitmodules` gives the submodule at `rel` (usually the path itself).
fn submodule_name(root: &Path, rel: &str) -> Option<String> {
    let cfg = gix::config::File::from_path_no_includes(
        root.join(".gitmodules"),
        gix::config::Source::Worktree,
    )
    .ok()?;
    for s in cfg.sections_by_name("submodule")? {
        if s.value("path").is_some_and(|p| p == rel) {
            return s.header().subsection_name().map(|n| n.to_string());
        }
    }
    None
}

fn config_url(path: &Path, key: &str) -> Option<String> {
    let r = repo::open(path).ok()?;
    let v = r.config_snapshot().string(key).map(|v| v.to_string());
    v
}

/// The URL rewrites of `git submodule sync`: the submodule's own `remote.origin.url` and the superproject's
/// `submodule.<name>.url`, once when both changed the same way.
fn url_note(
    own: (Option<String>, Option<String>),
    sup: (Option<String>, Option<String>),
    name: Option<&str>,
) -> Option<String> {
    let show = |u: &Option<String>| esc(&git_runner::redact(u.as_deref().unwrap_or(""), &[]));
    let (own_changed, sup_changed) = (own.0 != own.1, name.is_some() && sup.0 != sup.1);
    let mut parts = vec![];
    if own_changed && sup_changed && own == sup {
        parts.push(format!(
            "URL rewritten by git submodule sync: {} -> {}",
            show(&own.0),
            show(&own.1)
        ));
    } else {
        if own_changed {
            parts.push(format!(
                "URL rewritten by git submodule sync: {} -> {}",
                show(&own.0),
                show(&own.1)
            ));
        }
        if sup_changed {
            parts.push(format!(
                "submodule.{}.url rewritten: {} -> {}",
                esc(name.unwrap_or("")),
                show(&sup.0),
                show(&sup.1)
            ));
        }
    }
    (!parts.is_empty()).then(|| parts.join("; "))
}

/// The step on one pinned submodule `rec` (detached HEAD) at `rel` below the superproject at `root`: the
/// workspace root or, for a nested submodule, the submodule that holds the gitlink.
fn step(root: &Path, rel: String, rec: &mut SyncRec, o: &Opts) {
    let none = SubRec::default;
    let Ok(path) = SubPath::parse(&rel) else {
        return unreachable_rec(
            rec,
            o,
            "the submodule path is not a plain directory path; stw leaves it alone",
            none(),
        );
    };
    if symlinked_path(root, &rel) {
        return unreachable_rec(
            rec,
            o,
            "the submodule path goes through a symbolic link; stw leaves it alone",
            none(),
        );
    }
    let sub_path = root.join(&rel);
    let (root_repo, sub) = match (repo::open(root), repo::open(&sub_path)) {
        (Ok(a), Ok(b)) => (a, b),
        (a, b) => {
            let e = a
                .err()
                .or(b.err())
                .map(|e| e.to_string())
                .unwrap_or_default();
            return put(
                rec,
                "error",
                format!("cannot read the submodule: {}", esc(&e)),
                SubRec {
                    state: "failed",
                    ..none()
                },
            );
        }
    };
    let Some(recorded) = recorded_commit(&root_repo, &rel) else {
        return put(
            rec,
            "error",
            "the superproject records no commit for the submodule".into(),
            SubRec {
                state: "failed",
                ..none()
            },
        );
    };
    let rec_s = recorded.to_string();
    let Ok(head) = sub.head_id().map(|h| h.detach()) else {
        return put(
            rec,
            "skip",
            "the submodule has no commit yet".into(),
            SubRec {
                state: "not-fast-forward",
                ..none()
            },
        );
    };
    let head_s = head.to_string();
    let base = SubRec {
        old: Some(head_s.clone()),
        ..none()
    };
    let have = sub
        .find_object(recorded)
        .is_ok_and(|ob| ob.kind == gix::object::Kind::Commit);
    if !have {
        return unreachable_rec(
            rec,
            o,
            &format!(
                "the recorded commit {} is not in the submodule; fetch it with plain git{}",
                sh(&rec_s),
                if o.apply {
                    ""
                } else {
                    " (a run fetches origin first)"
                }
            ),
            base,
        );
    }
    if status::changes(&sub, false).map_or(true, |c| c.dirty_paths > 0) {
        return put(
            rec,
            "skip",
            "the submodule has local changes".into(),
            SubRec {
                state: "skipped-dirty",
                ..base
            },
        );
    }
    if head == recorded {
        return put(
            rec,
            "none",
            "pinned submodule is at the recorded commit".into(),
            SubRec {
                state: "up-to-date",
                ..base
            },
        );
    }
    let ff = sub
        .merge_base(head, recorded)
        .is_ok_and(|b| b.is_some_and(|b| b.detach() == head));
    if !ff {
        return put(
            rec,
            "skip",
            format!(
                "the recorded commit {} is not a fast-forward of HEAD {}",
                sh(&rec_s),
                sh(&head_s)
            ),
            SubRec {
                state: "not-fast-forward",
                new: Some(rec_s),
                ..base
            },
        );
    }
    let moved = SubRec {
        new: Some(rec_s.clone()),
        ..base
    };
    if !o.apply {
        return put(
            rec,
            "submodule-move",
            format!(
                "would move the pinned submodule {}..{} to the commit the superproject records",
                sh(&head_s),
                sh(&rec_s)
            ),
            SubRec {
                state: "would-move",
                ..moved
            },
        );
    }
    // the move: `git submodule sync` rewrites URLs (reported), `update --no-fetch --checkout` moves HEAD
    let name = submodule_name(root, &rel);
    let own_old = config_url(&sub_path, "remote.origin.url");
    let sup_old = name
        .as_deref()
        .and_then(|n| config_url(root, &format!("submodule.{n}.url")));
    let fail = |rec: &mut SyncRec, what: &str, e: &git_runner::GitError, sub: SubRec| {
        rec.git_error = Some(e.kind().to_string());
        rec.err = Some(esc(&e.to_string()));
        put(
            rec,
            "error",
            what.to_string(),
            SubRec {
                state: "failed",
                ..sub
            },
        );
    };
    let runner = git_runner::shared();
    if let Err(e) = runner.run(root, Op::SubmoduleSync { path: path.clone() }) {
        return fail(rec, "git submodule sync failed", &e, moved);
    }
    let own_new = config_url(&sub_path, "remote.origin.url");
    let sup_new = name
        .as_deref()
        .and_then(|n| config_url(root, &format!("submodule.{n}.url")));
    let note = url_note((own_old, own_new), (sup_old, sup_new), name.as_deref());
    let moved = SubRec {
        note: note.clone(),
        ..moved
    };
    rec.via = Some(git_runner::VIA.to_string());
    if let Err(e) = runner.run(root, Op::SubmoduleUpdate { path }) {
        return fail(rec, "git submodule update failed", &e, moved);
    }
    let landed = repo::open(&sub_path)
        .ok()
        .and_then(|r| r.head_id().ok().map(|h| h.detach()));
    let suffix = note.map(|n| format!("; {n}")).unwrap_or_default();
    if landed != Some(recorded) {
        return put(
            rec,
            "error",
            format!(
                "the submodule did not land on the recorded commit {}{suffix}",
                sh(&rec_s)
            ),
            SubRec {
                state: "failed",
                ..moved
            },
        );
    }
    put(
        rec,
        "submodule-moved",
        format!(
            "moved the pinned submodule {}..{} to the commit the superproject records{suffix}",
            sh(&head_s),
            sh(&rec_s)
        ),
        SubRec {
            state: "moved",
            ..moved
        },
    );
}

/// Adds one record per gitlink path that is not accepted (`repo::rejected_paths`), never synced, whatever
/// the flags. Only a run with `--submodules` makes it an error (Go's `unreachable` arises only there; a dry run
/// predicts it as a skip); a run without `--submodules` shows a skip with the same text, a hint that does not fail the run.
/// The path is untrusted: no file system access, escaped in the text.
pub fn rejected_pass(rejected: &[(String, String)], list: &mut Vec<SyncRec>, o: &Opts) {
    for (p, why) in rejected {
        let mut rec = SyncRec {
            name: p.clone(),
            path: String::new(),
            fetch: "not-run".into(),
            ..Default::default()
        };
        let why = format!("submodule path not accepted: {}: {why}", esc(p));
        if o.submodules {
            unreachable_rec(&mut rec, o, &why, SubRec::default());
        } else {
            put(&mut rec, "skip", why, SubRec::default());
            rec.submodule = None;
        }
        list.push(rec);
    }
}

/// The `--submodules` pass over the synced `list` of the superproject at `root`: each pinned submodule (checked
/// out, detached HEAD, nothing else wrong with it) gets its step; with `include_uninitialized`, a submodule of
/// a gitlink without a checkout is added as unreachable (never cloned). `skip` is the rules checkout, which
/// only `--rules` handles.
pub fn submodule_pass(
    root: &Path,
    list: &mut Vec<SyncRec>,
    o: &Opts,
    include_uninitialized: bool,
    skip: &Path,
) {
    // The holder of a gitlink is found in the whole walk of the workspace, not in `list`: `--repo` and the
    // agent-rules checkout leave repositories out of it, and a nested one is still held by its own parent.
    let all = repo::list_repos(root);
    let holders: Vec<(PathBuf, String)> = list
        .iter()
        .map(|rec| holder_of(root, &all, &rec.name))
        .collect();
    for (rec, (parent, rel)) in list.iter_mut().zip(holders) {
        let is_root = std::path::Path::new(&rec.path) == root;
        if !is_root && rec.pinned && rec.result == "skip" {
            step(&parent, rel, rec, o);
        }
    }
    if !include_uninitialized {
        return;
    }
    // every checked-out repository of the workspace (the root, nested ones too) may hold gitlinks without a checkout
    let mut holders: Vec<(PathBuf, String)> = vec![(root.to_path_buf(), String::new())];
    holders.extend(
        all.iter()
            .filter(|r| r.path != root)
            .map(|r| (r.path.clone(), r.name.clone())),
    );
    for (holder, prefix) in holders {
        for p in repo::submodule_paths(&holder) {
            // a refused path is reported by `rejected_pass`; the name is the normalized path, as in the list
            let Some(p) = repo::accepted_name(&holder, &p) else {
                continue;
            };
            let dir = holder.join(&p);
            let name = if prefix.is_empty() {
                p.clone()
            } else {
                format!("{prefix}/{p}")
            };
            let canon = std::fs::canonicalize(&dir).unwrap_or(dir.clone());
            if dir.join(".git").exists()
                || canon == skip
                || list.iter().any(|r| Path::new(&r.path) == dir)
            {
                continue;
            }
            let mut rec = SyncRec {
                name,
                path: dir.to_string_lossy().into_owned(),
                fetch: "not-run".into(),
                ..Default::default()
            };
            unreachable_rec(
                &mut rec,
                o,
                "not initialized; init it with plain git, stw never clones",
                SubRec::default(),
            );
            list.push(rec);
        }
    }
}

/// The repository that holds the gitlink of the repository `name`, and the gitlink path inside it: the repository
/// of the walk `all` whose name is the longest path prefix of `name`, else the workspace root (`root`, the whole name).
fn holder_of(root: &Path, all: &[repo::RepoRef], name: &str) -> (PathBuf, String) {
    let mut best: Option<(&repo::RepoRef, &str)> = None;
    for r in all {
        if r.path == root {
            continue;
        }
        if let Some(rest) = name
            .strip_prefix(r.name.as_str())
            .and_then(|t| t.strip_prefix('/'))
        {
            if best.is_none_or(|(b, _)| r.name.len() > b.name.len()) {
                best = Some((r, rest));
            }
        }
    }
    match best {
        Some((r, rest)) => (r.path.clone(), rest.to_string()),
        None => (root.to_path_buf(), name.to_string()),
    }
}
