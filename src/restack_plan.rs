//! `stw restack plan [BRANCH...] [--onto BASIS] [--old-parent REV]`: what a rebase of a stack of branches onto a new
//! base would do, level by level, before anything is changed. Read-only: no ref, no file, no object on disk, no
//! network. Two parts:
//!
//! * the port of the single-branch prediction of the Go command (`internal/restack`, hidden `restack plan --branch
//!   --old-parent --new-parent`): one branch, an old parent, a new base, the in-memory replay (`replay.rs`, M1) of the
//!   branch's commits with the predicted conflicts, the commits that become empty, a merge or root commit refused.
//!   Here that is `stw restack plan BRANCH --old-parent REV --onto BASIS`;
//! * the whole-stack preview (new, spike A): `A B C` bottom to top, every branch sits on the one before; level n is
//!   replayed onto the new tip of level n-1. A level above a conflict only inherits it: its own result depends on how
//!   the conflict below is resolved and is not computed (`inherited-conflict`).
//!
//! Input, either explicit (`A B C`) or derived from the local branch topology (no branch named): starting at the
//! checked-out branch, the chain of local branches below it, each the nearest one below the previous; anything
//! ambiguous (two candidates, two names for one commit, a detached head, no common history with the base) is refused
//! with the explicit spelling as a suggestion; stw never guesses.
//!
//! The engine is an approximation of `git rebase` (see the header of `replay.rs`: directory renames, rename detection
//! pinned, no merge driver, no rerere). The plan never says `clean` when that approximation is not safe: a level whose
//! result would be clean but whose repository has a `merge` attribute, a rename or diff setting that differs from the
//! pinned one, or a commit that changes a submodule is `unknown` with the reason. A conflict that is predicted is
//! reported as it is (the conservative direction); a conflict that rerere could resolve is not known to the plan.
//!
//! Git boundary: no `git` call at all. Refs, objects, merge base, commit walks, trees, the merge itself (gix),
//! patch-ids (`patch_id.rs`) and the configuration are read in process; `git range-diff` and `git rebase` are not used.
use std::collections::BTreeSet;
use std::path::Path;

use gix::bstr::{BStr, ByteSlice};
use gix::ObjectId;
use serde_json::{json, Value};

use crate::github::reads::escape_untrusted;
use crate::output::short;
use crate::{patch_id, replay, repo};

/// `schema_version` of the JSON document.
pub const SCHEMA_VERSION: u32 = 2;
/// Levels of one plan.
pub const MAX_LEVELS: usize = 64;
/// Commits of one level (more is `unknown`, a bound, never a guess).
pub const MAX_COMMITS: usize = 500;
/// Commits of the new base that are searched for an equal patch when a commit becomes empty.
const UPSTREAM_SCAN: usize = 5000;
/// Conflicted paths and steps listed per level.
const MAX_LISTED: usize = 50;

/// A validated command line.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Request {
    /// Local branches, bottom to top; empty: derive the chain from the topology.
    pub branches: Vec<String>,
    /// The new base (a revision); `None`: the default branch of the repository (`origin/HEAD`).
    pub onto: Option<String>,
    /// The old parent of the bottom branch (the Go `--old-parent`); `None`: the merge base of the bottom branch and the base.
    pub old_parent: Option<String>,
}

/// An invalid command line or an input that names nothing (exit 2).
#[derive(Debug, PartialEq, Eq)]
pub struct UsageError(pub String);

impl std::fmt::Display for UsageError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

fn check_name(what: &str, s: &str) -> Result<(), UsageError> {
    if s.is_empty() {
        return Err(UsageError(format!("{what} must not be empty")));
    }
    if s.starts_with('-') {
        return Err(UsageError(format!("{what} must not start with a dash")));
    }
    if s.len() > 255 || s.chars().any(char::is_control) {
        return Err(UsageError(format!(
            "{what} is too long or holds a control character"
        )));
    }
    if s.contains("..") {
        return Err(UsageError(format!(
            "{what} must be one name or revision, not a range"
        )));
    }
    Ok(())
}

pub fn parse_request(
    branches: &[String],
    onto: Option<&str>,
    old_parent: Option<&str>,
) -> Result<Request, UsageError> {
    for b in branches {
        check_name("a branch", b)?;
    }
    let mut seen = BTreeSet::new();
    if let Some(d) = branches.iter().find(|b| !seen.insert(b.as_str())) {
        return Err(UsageError(format!(
            "branch {} is named twice",
            escape_untrusted(d, 100).0
        )));
    }
    if branches.len() > MAX_LEVELS {
        return Err(UsageError(format!("more than {MAX_LEVELS} branches")));
    }
    if let Some(o) = onto {
        check_name("--onto", o)?;
    }
    if let Some(o) = old_parent {
        check_name("--old-parent", o)?;
        if branches.len() != 1 {
            return Err(UsageError(
                "--old-parent needs exactly one branch (it is the old parent of the bottom branch)"
                    .into(),
            ));
        }
    }
    Ok(Request {
        branches: branches.to_vec(),
        onto: onto.map(str::to_string),
        old_parent: old_parent.map(str::to_string),
    })
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Source {
    Explicit,
    AutoDetected,
}

impl Source {
    pub fn label(self) -> &'static str {
        match self {
            Source::Explicit => "explicit",
            Source::AutoDetected => "auto-detected",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum LevelState {
    Clean,
    Conflict,
    InheritedConflict,
    BecameEmpty,
    AlreadyUpstream,
    Refused,
    Unknown,
    /// Above a level that is refused or unknown: nothing is known.
    NotComputed,
}

impl LevelState {
    pub fn label(self) -> &'static str {
        match self {
            LevelState::Clean => "clean",
            LevelState::Conflict => "conflict",
            LevelState::InheritedConflict => "inherited-conflict",
            LevelState::BecameEmpty => "became-empty",
            LevelState::AlreadyUpstream => "already-upstream",
            LevelState::Refused => "refused",
            LevelState::Unknown => "unknown",
            LevelState::NotComputed => "not-computed",
        }
    }
    fn fine(self) -> bool {
        matches!(
            self,
            LevelState::Clean | LevelState::BecameEmpty | LevelState::AlreadyUpstream
        )
    }
}

/// What one commit of a level did (the names of `git rebase`).
#[derive(Clone, Debug)]
pub struct StepInfo {
    pub old: ObjectId,
    /// `clean`, `unchanged`, `conflict`, `dropped` (empty after the replay) or `empty-start`.
    pub status: &'static str,
    pub subject: String,
}

#[derive(Clone, Debug)]
pub struct ConflictInfo {
    pub commit: ObjectId,
    pub path: String,
    pub kind: &'static str,
}

#[derive(Clone, Debug)]
pub struct Level {
    pub branch: String,
    pub tip: ObjectId,
    pub old_base: ObjectId,
    /// The tip the level is replayed onto; `None` when it is not computed.
    pub new_base: Option<ObjectId>,
    pub commits: Option<usize>,
    pub state: LevelState,
    pub reasons: Vec<String>,
    pub conflicts: Vec<ConflictInfo>,
    pub steps: Vec<StepInfo>,
    pub dropped: usize,
}

/// An input that stops the plan before any level is replayed.
#[derive(Clone, Debug)]
pub struct Refusal {
    pub code: &'static str,
    pub message: String,
    /// The explicit spelling to use instead, when there is one.
    pub suggestion: Option<String>,
}

#[derive(Clone, Debug)]
pub struct Plan {
    pub source: Source,
    pub onto_rev: String,
    pub onto: ObjectId,
    pub levels: Vec<Level>,
    pub refusal: Option<Refusal>,
    pub notes: Vec<String>,
}

fn esc(s: &str) -> String {
    escape_untrusted(s, 200).0
}

fn lossy(b: &BStr) -> String {
    b.to_str_lossy().into_owned()
}

pub(crate) fn resolve(repo: &gix::Repository, rev: &str) -> Result<ObjectId, String> {
    let id = repo.rev_parse_single(rev).map_err(|e| e.to_string())?;
    Ok(id
        .object()
        .map_err(|e| e.to_string())?
        .peel_to_commit()
        .map_err(|_| "is not a commit".to_string())?
        .id)
}

fn merge_base(repo: &gix::Repository, a: ObjectId, b: ObjectId) -> Option<ObjectId> {
    repo.merge_base(a, b).ok().flatten().map(|i| i.detach())
}

fn subject_of(repo: &gix::Repository, id: ObjectId) -> String {
    repo.find_commit(id)
        .ok()
        .and_then(|c| {
            c.message_raw()
                .ok()
                .map(|m| lossy(m.lines().next().unwrap_or_default().as_bstr()))
        })
        .unwrap_or_default()
}

fn suggestion(names: &[String], onto: &str) -> String {
    let list: Vec<String> = names.iter().map(|n| esc(n)).collect();
    format!("stw restack plan {} --onto {}", list.join(" "), esc(onto))
}

/// The chain (branch, tip; bottom first) and the notes.
type Detected = (Vec<(String, ObjectId)>, Vec<String>);

/// The chain below the checked-out branch, bottom first, or why it is not clear.
fn detect(repo: &gix::Repository, onto: ObjectId, onto_rev: &str) -> Result<Detected, Refusal> {
    let refuse = |code, message: String, suggestion| Refusal {
        code,
        message,
        suggestion,
    };
    let head = repo
        .head()
        .map_err(|e| refuse("read-failed", esc(&e.to_string()), None))?;
    let Some(name) = head.referent_name().and_then(repo::branch_short) else {
        return Err(refuse(
            "detached-head",
            "HEAD is not on a branch: there is no starting point for the chain".into(),
            Some(format!(
                "stw restack plan BRANCH... --onto {}",
                esc(onto_rev)
            )),
        ));
    };
    let all =
        repo::local_branches(repo).map_err(|e| refuse("read-failed", esc(&e.to_string()), None))?;
    let ancestor = |a: ObjectId, b: ObjectId| {
        repo::is_ancestor(repo, a, b).map_err(|e| refuse("read-failed", esc(&e.to_string()), None))
    };
    // the local default branch (main) is the trunk, never a level, also when it is ahead of origin/main
    let default_name = repo::base_ref(repo).and_then(|full| {
        full.strip_prefix("refs/remotes/")
            .and_then(|r| r.split_once('/'))
            .map(|(_, b)| b.to_string())
    });
    if default_name.as_deref() == Some(name.as_str()) {
        return Err(refuse(
            "default-branch",
            format!(
                "the checked-out branch {} is the default branch: it is the trunk, not a stack",
                esc(&name)
            ),
            Some(format!(
                "stw restack plan BRANCH... --onto {}",
                esc(onto_rev)
            )),
        ));
    }
    // branches that already are part of the base are not part of a stack
    let mut open = Vec::new();
    let mut notes = Vec::new();
    for (n, t) in all {
        if ancestor(t, onto)? {
            continue;
        }
        if default_name.as_deref() == Some(n.as_str()) {
            notes.push(format!(
                "the local default branch {} is ahead of the base and is not part of the chain",
                esc(&n)
            ));
            continue;
        }
        open.push((n, t));
    }
    let Some(top) = open.iter().find(|(n, _)| *n == name).cloned() else {
        return Err(refuse(
            "nothing-to-restack",
            format!(
                "the checked-out branch {} is already contained in {}: there is nothing to restack",
                esc(&name),
                esc(onto_rev)
            ),
            None,
        ));
    };
    let mut chain = vec![top];
    loop {
        if chain.len() > MAX_LEVELS {
            return Err(refuse(
                "too-many-levels",
                format!("more than {MAX_LEVELS} branches in the chain"),
                None,
            ));
        }
        let last = chain.last().expect("not empty").clone();
        let mut same = Vec::new();
        let mut below = Vec::new();
        for (n, t) in &open {
            if chain.iter().any(|(c, _)| c == n) {
                continue;
            }
            if *t == last.1 {
                same.push(n.clone());
            } else if ancestor(*t, last.1)? {
                below.push((n.clone(), *t));
            }
        }
        if !same.is_empty() {
            let mut names = vec![last.0.clone()];
            names.extend(same);
            return Err(refuse(
                "ambiguous",
                format!(
                    "the branches {} point at the same commit: which one is below the other is not known",
                    names.iter().map(|n| esc(n)).collect::<Vec<_>>().join(", ")
                ),
                Some(suggestion(&names, onto_rev)),
            ));
        }
        // the nearest ones: no other candidate lies above them
        let mut nearest = Vec::new();
        for (n, t) in &below {
            let mut covered = false;
            for (m, u) in &below {
                if m != n && *t != *u && ancestor(*t, *u)? {
                    covered = true;
                    break;
                }
            }
            if !covered {
                nearest.push((n.clone(), *t));
            }
        }
        // two names at one commit below
        let mut tips = BTreeSet::new();
        if nearest.iter().any(|(_, t)| !tips.insert(*t)) {
            let names: Vec<String> = nearest.iter().map(|(n, _)| n.clone()).collect();
            return Err(refuse(
                "ambiguous",
                format!(
                    "below {} several branches point at the same commit: {}",
                    esc(&last.0),
                    names.iter().map(|n| esc(n)).collect::<Vec<_>>().join(", ")
                ),
                Some(suggestion(&names, onto_rev)),
            ));
        }
        match nearest.len() {
            0 => break,
            1 => chain.push(nearest.remove(0)),
            _ => {
                let names: Vec<String> = nearest.iter().map(|(n, _)| n.clone()).collect();
                return Err(refuse(
                    "ambiguous",
                    format!(
                        "below {} the stack forks: {} are candidates and none lies below another",
                        esc(&last.0),
                        names.iter().map(|n| esc(n)).collect::<Vec<_>>().join(", ")
                    ),
                    Some(suggestion(&names, onto_rev)),
                ));
            }
        }
    }
    chain.reverse();
    // branches above the starting point are not part of the plan
    let top = chain.last().expect("not empty").clone();
    let mut above = Vec::new();
    for (n, t) in &open {
        if chain.iter().any(|(c, _)| c == n) {
            continue;
        }
        if *t != top.1 && ancestor(top.1, *t)? {
            above.push(esc(n));
        }
    }
    if !above.is_empty() {
        above.truncate(10);
        notes.push(format!(
            "not in the plan, they sit above {}: {} (name the whole stack to include them)",
            esc(&top.0),
            above.join(", ")
        ));
    }
    Ok((chain, notes))
}

/// Whether the text of an attributes file sets, unsets or resets the `merge` attribute, or the built-in macro
/// `binary` (which sets `-merge`), on some pattern, or defines a macro (`[attr]name ...`) that does. A macro definition
/// that does is reported where it is defined, whether or not it is used: the conservative direction.
fn mentions_merge(text: &[u8]) -> bool {
    text.lines().any(|l| {
        let l = l.trim();
        if l.starts_with(b"#") {
            return false;
        }
        l.fields().skip(1).any(|t| {
            matches!(
                t,
                b"merge" | b"-merge" | b"!merge" | b"binary" | b"-binary" | b"!binary"
            ) || t.starts_with(b"merge=")
        })
    })
}

/// The blobs of the `.gitattributes` files that `commit` adds or changes (the version after the commit).
fn changed_attribute_blobs(
    repo: &gix::Repository,
    commit: ObjectId,
) -> Result<Vec<ObjectId>, String> {
    use gix::diff::tree_with_rewrites::Change;
    let c = repo.find_commit(commit).map_err(|e| e.to_string())?;
    let Some(parent) = c.parent_ids().next() else {
        return Ok(vec![]);
    };
    let parent = repo
        .find_commit(parent.detach())
        .map_err(|e| e.to_string())?
        .tree()
        .map_err(|e| e.to_string())?;
    let tree = c.tree().map_err(|e| e.to_string())?;
    let options = gix::diff::Options::default().with_rewrites(None);
    let changes = repo
        .diff_tree_to_tree(&parent, &tree, options)
        .map_err(|e| e.to_string())?;
    let is_attr = |l: &gix::bstr::BStr| l.rsplit(|b| *b == b'/').next() == Some(b".gitattributes");
    Ok(changes
        .iter()
        .filter_map(|ch| match ch {
            Change::Addition { location, id, .. } | Change::Modification { location, id, .. }
                if is_attr(location.as_ref()) =>
            {
                Some(*id)
            }
            _ => None,
        })
        .collect())
}

/// Settings and files that make the replay differ from `git rebase` in the dangerous direction (the plan says clean,
/// git conflicts): the reasons, empty when there are none. The pinned values of the engine are the defaults.
fn merge_uncertainty(repo: &gix::Repository, trees: &[ObjectId]) -> Vec<String> {
    let mut out = Vec::new();
    let snapshot = repo.config_snapshot();
    let cfg = snapshot.plumbing();
    for (key, defaults) in [
        ("merge.renames", &["true"][..]),
        ("diff.renames", &["true"][..]),
        ("merge.renameLimit", &["7000"][..]),
        ("diff.renameLimit", &["7000"][..]),
        ("diff.algorithm", &["myers", "default"][..]),
    ] {
        if let Some(v) = cfg.string(key) {
            let v = lossy(v.as_ref());
            if !defaults.contains(&v.as_str()) {
                out.push(format!(
                    "{key}={} differs from the pinned {} of the replay",
                    esc(&v),
                    defaults[0]
                ));
            }
        }
    }
    // `merge` attribute of any .gitattributes of the trees involved or of info/attributes
    let mut found = false;
    if let Ok(text) = std::fs::read(repo.common_dir().join("info/attributes")) {
        found |= mentions_merge(&text);
    }
    for tree in trees {
        let Ok(tree) = repo.find_tree(*tree) else {
            continue;
        };
        let mut rec = gix::traverse::tree::Recorder::default();
        if tree.traverse().breadthfirst(&mut rec).is_err() {
            out.push("a tree could not be scanned for merge attributes".into());
            continue;
        }
        for e in rec.records {
            if e.mode.is_blob()
                && e.filepath.rsplit(|b| *b == b'/').next() == Some(b".gitattributes")
            {
                match repo.find_blob(e.oid) {
                    Ok(b) => found |= mentions_merge(&b.data),
                    Err(_) => out.push("a .gitattributes file could not be read".into()),
                }
            }
        }
    }
    if found {
        out.push(
            "a merge attribute is set in a .gitattributes file or info/attributes (merge drivers are not run)".into(),
        );
    }
    out.sort();
    out.dedup();
    out
}

/// A commit that changes a submodule (a gitlink) is merged by rules the replay does not share with git.
fn touches_gitlink(repo: &gix::Repository, commit: ObjectId) -> Result<bool, String> {
    use gix::diff::tree_with_rewrites::Change;
    let c = repo.find_commit(commit).map_err(|e| e.to_string())?;
    let Some(parent) = c.parent_ids().next() else {
        return Ok(false);
    };
    let parent = repo
        .find_commit(parent.detach())
        .map_err(|e| e.to_string())?
        .tree()
        .map_err(|e| e.to_string())?;
    let tree = c.tree().map_err(|e| e.to_string())?;
    let options = gix::diff::Options::default().with_rewrites(None);
    let changes = repo
        .diff_tree_to_tree(&parent, &tree, options)
        .map_err(|e| e.to_string())?;
    Ok(changes.iter().any(|ch| match ch {
        Change::Addition { entry_mode, .. } | Change::Deletion { entry_mode, .. } => {
            entry_mode.is_commit()
        }
        Change::Modification {
            entry_mode,
            previous_entry_mode,
            ..
        } => entry_mode.is_commit() || previous_entry_mode.is_commit(),
        Change::Rewrite { .. } => false,
    }))
}

fn count_range(repo: &gix::Repository, tip: ObjectId, base: ObjectId) -> Result<usize, String> {
    let walk = repo
        .rev_walk([tip])
        .with_hidden([base])
        .all()
        .map_err(|e| e.to_string())?;
    let mut n = 0;
    for info in walk {
        info.map_err(|e| e.to_string())?;
        n += 1;
        if n > MAX_COMMITS {
            break;
        }
    }
    Ok(n)
}

/// Patch-ids of the commits `onto` has and `old_base` has not, or `None` when there are too many to look through.
fn upstream_patch_ids(
    repo: &gix::Repository,
    onto: ObjectId,
    old_base: ObjectId,
) -> Option<BTreeSet<ObjectId>> {
    let walk = repo.rev_walk([onto]).with_hidden([old_base]).all().ok()?;
    let mut set = BTreeSet::new();
    for (n, info) in walk.enumerate() {
        if n >= UPSTREAM_SCAN {
            return None;
        }
        let info = info.ok()?;
        if let Ok(Some(id)) = patch_id::commit_patch_id(repo, info.id) {
            set.insert(id);
        }
    }
    Some(set)
}

fn level(branch: &str, tip: ObjectId, old_base: ObjectId) -> Level {
    Level {
        branch: branch.to_string(),
        tip,
        old_base,
        new_base: None,
        commits: None,
        state: LevelState::NotComputed,
        reasons: vec![],
        conflicts: vec![],
        steps: vec![],
        dropped: 0,
    }
}

fn input_refusal(
    source: Source,
    onto_rev: &str,
    onto: ObjectId,
    r: Refusal,
    notes: Vec<String>,
) -> Plan {
    Plan {
        source,
        onto_rev: onto_rev.to_string(),
        onto,
        levels: vec![],
        refusal: Some(r),
        notes,
    }
}

/// Plan in the repository at `dir` (the root of a repository or worktree). `Err` is a usage problem (a name that
/// resolves to nothing, no base); `Ok` is the plan, including a refusal.
pub fn plan(dir: &Path, req: &Request) -> Result<Plan, crate::error::Error> {
    plan_inner(dir, req, None)
}

type ConflictRecorder<'a> =
    dyn FnMut(&gix::Repository, &str, &replay::Step) -> crate::error::Result<()> + 'a;

pub(crate) fn plan_recording_conflict(
    dir: &Path,
    req: &Request,
    record: &mut ConflictRecorder<'_>,
) -> crate::error::Result<Plan> {
    plan_inner(dir, req, Some(record))
}

fn plan_inner(
    dir: &Path,
    req: &Request,
    mut record: Option<&mut ConflictRecorder<'_>>,
) -> crate::error::Result<Plan> {
    use crate::error::Error;
    let repo = repo::open(dir)?;
    let usage = |m: String| Error::usage(m);
    // the new base
    let (onto_rev, onto) = match &req.onto {
        Some(rev) => (
            rev.clone(),
            resolve(&repo, rev).map_err(|m| usage(format!("--onto {}: {}", esc(rev), esc(&m))))?,
        ),
        None => {
            let full = repo::base_ref(&repo).ok_or_else(|| {
                usage(
                    "no --onto and the repository has no default branch (origin/HEAD, origin/main or origin/master): pass --onto BASIS".into(),
                )
            })?;
            let id = repo::resolve(&repo, &full).ok_or_else(|| {
                usage(format!(
                    "the default branch {} does not resolve: pass --onto BASIS",
                    esc(&full)
                ))
            })?;
            (
                full.strip_prefix("refs/remotes/")
                    .unwrap_or(&full)
                    .to_string(),
                id,
            )
        }
    };
    // the branches
    let (source, branches, mut notes) = if req.branches.is_empty() {
        match detect(&repo, onto, &onto_rev) {
            Ok((chain, notes)) => (Source::AutoDetected, chain, notes),
            Err(r) => {
                return Ok(input_refusal(
                    Source::AutoDetected,
                    &onto_rev,
                    onto,
                    r,
                    vec![],
                ))
            }
        }
    } else {
        let mut v = Vec::new();
        for b in &req.branches {
            let full = format!("refs/heads/{b}");
            let id = repo::resolve(&repo, &full)
                .ok_or_else(|| usage(format!("unknown local branch {}", esc(b))))?;
            v.push((b.clone(), id));
        }
        (Source::Explicit, v, vec![])
    };
    // old bases
    let mut levels: Vec<Level> = Vec::new();
    for (i, (name, tip)) in branches.iter().enumerate() {
        let old_base = if i == 0 {
            match &req.old_parent {
                Some(rev) => {
                    let p = resolve(&repo, rev)
                        .map_err(|m| usage(format!("--old-parent {}: {}", esc(rev), esc(&m))))?;
                    let ok = repo::is_ancestor(&repo, p, *tip)?;
                    if !ok {
                        return Ok(input_refusal(
                            source,
                            &onto_rev,
                            onto,
                            Refusal {
                                code: "old-parent-not-ancestor",
                                message: format!(
                                    "the old parent {} is not an ancestor of {}",
                                    esc(rev),
                                    esc(name)
                                ),
                                suggestion: None,
                            },
                            notes,
                        ));
                    }
                    p
                }
                None => match merge_base(&repo, *tip, onto) {
                    Some(b) => b,
                    None => {
                        return Ok(input_refusal(
                            source,
                            &onto_rev,
                            onto,
                            Refusal {
                                code: "no-merge-base",
                                message: format!(
                                    "{} has no common history with {}",
                                    esc(name),
                                    esc(&onto_rev)
                                ),
                                suggestion: None,
                            },
                            notes,
                        ))
                    }
                },
            }
        } else {
            let prev = &branches[i - 1];
            if !repo::is_ancestor(&repo, prev.1, *tip)? {
                return Ok(input_refusal(
                    source,
                    &onto_rev,
                    onto,
                    Refusal {
                        code: "not-stacked",
                        message: format!(
                            "{} is not based on {}: the tip of {} is not an ancestor of {}",
                            esc(name),
                            esc(&prev.0),
                            esc(&prev.0),
                            esc(name)
                        ),
                        suggestion: None,
                    },
                    notes,
                ));
            }
            prev.1
        };
        levels.push(level(name, *tip, old_base));
    }
    // the replay
    let mem = replay::sandbox(&repo).map_err(|e| Error::repository(e.to_string()))?;
    let committer = replay::configured_committer(&repo, gix::date::Time::new(0, 0)).unwrap_or(
        gix::actor::Signature {
            name: "stw plan".into(),
            email: "stw-plan@localhost".into(),
            time: gix::date::Time::new(0, 0),
        },
    );
    let mut trees = Vec::new();
    for t in std::iter::once(onto).chain(levels.iter().map(|l| l.tip)) {
        if let Ok(c) = repo.find_commit(t) {
            if let Ok(id) = c.tree_id() {
                trees.push(id.detach());
            }
        }
    }
    let uncertainty = merge_uncertainty(&repo, &trees);
    #[derive(PartialEq)]
    enum Block {
        None,
        Conflict,
        Other,
    }
    let mut block = Block::None;
    let mut new_tip = onto;
    for l in levels.iter_mut() {
        l.commits = count_range(&repo, l.tip, l.old_base).ok();
        match block {
            Block::Conflict => {
                l.state = LevelState::InheritedConflict;
                l.reasons.push(
                    "a level below has a conflict; this level depends on how it is resolved".into(),
                );
                continue;
            }
            Block::Other => {
                l.state = LevelState::NotComputed;
                l.reasons.push("a level below is refused or unknown".into());
                continue;
            }
            Block::None => {}
        }
        l.new_base = Some(new_tip);
        if l.commits.is_none_or(|n| n > MAX_COMMITS) {
            l.state = LevelState::Unknown;
            l.reasons.push(format!(
                "more than {MAX_COMMITS} commits in the level (or the range could not be read)"
            ));
            block = Block::Other;
            continue;
        }
        let replay = if record.is_some() {
            replay::replay_range_with_stages(&mem, new_tip, l.old_base, l.tip, &committer)
        } else {
            replay::replay_range_in(&mem, new_tip, l.old_base, l.tip, &committer)
        };
        let (steps, tip_after) = match replay {
            Ok(x) => x,
            Err(e @ (replay::ReplayError::MergeCommit(_) | replay::ReplayError::RootCommit(_))) => {
                l.state = LevelState::Refused;
                l.reasons.push(esc(&e.to_string()));
                block = Block::Other;
                continue;
            }
            Err(e) => {
                l.state = LevelState::Unknown;
                l.reasons.push(esc(&e.to_string()));
                block = Block::Other;
                continue;
            }
        };
        for s in &steps {
            if s.status == replay::Status::Conflicted {
                if let Some(record) = &mut record {
                    record(&mem, &l.branch, s)?;
                }
            }
            let status = match s.status {
                replay::Status::Clean => "clean",
                replay::Status::Unchanged => "unchanged",
                replay::Status::Conflicted => "conflict",
                replay::Status::Empty => "dropped",
                replay::Status::EmptyStart => "empty-start",
            };
            l.steps.push(StepInfo {
                old: s.old,
                status,
                subject: subject_of(&repo, s.old),
            });
            for c in &s.conflicts {
                l.conflicts.push(ConflictInfo {
                    commit: s.old,
                    path: lossy(c.path.as_ref()),
                    kind: c.kind,
                });
            }
        }
        l.dropped = steps
            .iter()
            .filter(|s| s.status == replay::Status::Empty)
            .count();
        if steps.iter().any(|s| s.status == replay::Status::Conflicted) {
            l.state = LevelState::Conflict;
            block = Block::Conflict;
            continue;
        }
        // the result would be clean: is the replay to be trusted here?
        let mut why = uncertainty.clone();
        for s in &steps {
            match changed_attribute_blobs(&repo, s.old) {
                Ok(blobs) => {
                    for b in blobs {
                        match repo.find_blob(b) {
                            Ok(blob) if mentions_merge(&blob.data) => why.push(format!(
                                "commit {} changes a .gitattributes file that sets a merge attribute",
                                s.old
                            )),
                            Ok(_) => {}
                            Err(e) => why.push(format!(
                                "a .gitattributes file of commit {} could not be read: {}",
                                s.old,
                                esc(&e.to_string())
                            )),
                        }
                    }
                }
                Err(m) => why.push(format!("commit {} could not be read: {}", s.old, esc(&m))),
            }
            match touches_gitlink(&repo, s.old) {
                Ok(true) => why.push(format!(
                    "commit {} changes a submodule; the replay does not share git's rules for those",
                    s.old
                )),
                Ok(false) => {}
                Err(m) => why.push(format!("commit {} could not be read: {}", s.old, esc(&m))),
            }
        }
        if !why.is_empty() {
            l.state = LevelState::Unknown;
            l.reasons.extend(why);
            block = Block::Other;
            continue;
        }
        l.state = if steps.is_empty() {
            if repo::is_ancestor(&repo, l.tip, onto)? {
                LevelState::AlreadyUpstream
            } else {
                LevelState::Clean
            }
        } else if l.dropped == steps.len() {
            let theirs = upstream_patch_ids(&repo, onto, l.old_base);
            let all_found = theirs.is_some_and(|set| {
                steps.iter().all(|s| {
                    patch_id::commit_patch_id(&repo, s.old)
                        .ok()
                        .flatten()
                        .is_some_and(|id| set.contains(&id))
                })
            });
            if all_found {
                LevelState::AlreadyUpstream
            } else {
                LevelState::BecameEmpty
            }
        } else {
            LevelState::Clean
        };
        new_tip = tip_after;
    }
    if levels.len() > 1 && source == Source::Explicit {
        notes.push("each level is replayed onto the new tip of the level below".into());
    }
    Ok(Plan {
        source,
        onto_rev,
        onto,
        levels,
        refusal: None,
        notes,
    })
}

impl Plan {
    /// The first level that is not fine, and what is wrong with it.
    fn first_problem(&self) -> Option<&Level> {
        self.levels.iter().find(|l| {
            !l.state.fine()
                && l.state != LevelState::InheritedConflict
                && l.state != LevelState::NotComputed
        })
    }

    /// `clean`, `conflicts`, `refused` or `unknown`.
    pub fn verdict(&self) -> &'static str {
        if self.refusal.is_some() {
            return "refused";
        }
        match self.first_problem().map(|l| l.state) {
            None => "clean",
            Some(LevelState::Conflict) => "conflicts",
            Some(LevelState::Refused) => "refused",
            _ => "unknown",
        }
    }

    pub fn is_clean(&self) -> bool {
        self.verdict() == "clean"
    }

    fn files(l: &Level) -> Vec<String> {
        let mut v: Vec<String> = l.conflicts.iter().map(|c| esc(&c.path)).collect();
        v.sort();
        v.dedup();
        v
    }

    /// The first line of the text output.
    pub fn headline(&self) -> String {
        if let Some(r) = &self.refusal {
            return format!("plan: refused ({}: {})", r.code, r.message);
        }
        match self.first_problem() {
            None => "plan: clean".into(),
            Some(l) => match l.state {
                LevelState::Conflict => {
                    let files = Plan::files(l);
                    let shown: Vec<String> = files.iter().take(10).cloned().collect();
                    let more = if files.len() > shown.len() {
                        format!(", and {} more", files.len() - shown.len())
                    } else {
                        String::new()
                    };
                    format!(
                        "plan: conflicts at {} (files {}{})",
                        esc(&l.branch),
                        shown.join(", "),
                        more
                    )
                }
                LevelState::Refused => format!(
                    "plan: refused at {} ({})",
                    esc(&l.branch),
                    l.reasons.join("; ")
                ),
                _ => format!(
                    "plan: unknown at {} ({})",
                    esc(&l.branch),
                    l.reasons.join("; ")
                ),
            },
        }
    }

    pub fn to_json(&self) -> Value {
        let id = |o: Option<ObjectId>| o.map_or(Value::Null, |i| json!(i.to_string()));
        let levels: Vec<Value> = self
            .levels
            .iter()
            .map(|l| {
                json!({
                    "branch": esc(&l.branch),
                    "tip": l.tip.to_string(),
                    "old_base": l.old_base.to_string(),
                    "new_base": id(l.new_base),
                    "commits": l.commits,
                    "state": l.state.label(),
                    "reasons": l.reasons,
                    "files": Plan::files(l),
                    "conflicts": l.conflicts.iter().map(|c| json!({
                        "commit": c.commit.to_string(), "path": esc(&c.path), "kind": c.kind})).collect::<Vec<_>>(),
                    "dropped": l.dropped,
                    "steps": l.steps.iter().map(|s| json!({
                        "commit": s.old.to_string(), "status": s.status, "subject": esc(&s.subject)})).collect::<Vec<_>>(),
                })
            })
            .collect();
        let count = |st: LevelState| self.levels.iter().filter(|l| l.state == st).count();
        json!({
            "schema_version": SCHEMA_VERSION,
            "command": "restack-plan",
            "read_only": true,
            "plan": self.headline(),
            "verdict": self.verdict(),
            "source": self.source.label(),
            "onto": {"rev": esc(&self.onto_rev), "commit": self.onto.to_string()},
            "levels": levels,
            "summary": {
                "levels": self.levels.len(),
                "clean": count(LevelState::Clean),
                "conflict": count(LevelState::Conflict),
                "inherited_conflict": count(LevelState::InheritedConflict),
                "became_empty": count(LevelState::BecameEmpty),
                "already_upstream": count(LevelState::AlreadyUpstream),
                "refused": count(LevelState::Refused),
                "unknown": count(LevelState::Unknown),
                "not_computed": count(LevelState::NotComputed),
            },
            "refusal": self.refusal.as_ref().map(|r| json!({
                "code": r.code, "message": r.message, "suggestion": r.suggestion})),
            "notes": self.notes,
        })
    }

    pub fn render_human(&self) -> String {
        use std::fmt::Write;
        let mut b = String::new();
        let _ = writeln!(b, "{}", self.headline());
        // an input that stopped the plan has no levels: no count then
        let count = if self.levels.is_empty() {
            String::new()
        } else {
            let n = self.levels.len();
            format!(", {n} {}", if n == 1 { "level" } else { "levels" })
        };
        let _ = writeln!(
            b,
            "onto {} ({}){count}, stack {}",
            esc(&self.onto_rev),
            short(&self.onto.to_string()),
            self.source.label()
        );
        if let Some(r) = &self.refusal {
            if let Some(s) = &r.suggestion {
                let _ = writeln!(b, "instead: {s}");
            }
        }
        for (i, l) in self.levels.iter().enumerate() {
            let base = |o: Option<ObjectId>| {
                o.map_or("-".to_string(), |i| short(&i.to_string()).to_string())
            };
            let state = match l.state {
                LevelState::Conflict => {
                    format!("conflict in {}", Plan::files(l).join(", "))
                }
                LevelState::InheritedConflict => {
                    "inherited conflict (depends on the level below)".to_string()
                }
                s => s.label().to_string(),
            };
            let n = l.commits.map_or("?".to_string(), |n| n.to_string());
            let _ = writeln!(
                b,
                "level {}  {}  base {} -> {}  {} {}  {}",
                i + 1,
                esc(&l.branch),
                short(&l.old_base.to_string()),
                base(l.new_base),
                n,
                if l.commits == Some(1) {
                    "commit"
                } else {
                    "commits"
                },
                state
            );
            if !matches!(
                l.state,
                LevelState::InheritedConflict | LevelState::NotComputed
            ) {
                for r in &l.reasons {
                    let _ = writeln!(b, "    reason: {r}");
                }
            }
            for c in l.conflicts.iter().take(MAX_LISTED) {
                let _ = writeln!(
                    b,
                    "    {} conflict ({}) {}",
                    short(&c.commit.to_string()),
                    c.kind,
                    esc(&c.path)
                );
            }
            if l.state != LevelState::Conflict && l.dropped > 0 {
                let _ = writeln!(
                    b,
                    "    {} {} dropped (empty after the replay)",
                    l.dropped,
                    if l.dropped == 1 { "commit" } else { "commits" }
                );
            }
        }
        for n in &self.notes {
            let _ = writeln!(b, "note: {n}");
        }
        let _ = writeln!(
            b,
            "note: a prediction by an in-memory replay, not a run of git rebase; nothing was written"
        );
        b
    }
}
