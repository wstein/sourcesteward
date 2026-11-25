// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! `stw absorb` (new behavior, no Go counterpart): the read-only PLAN of folding the staged changes into the unpushed
//! commits that own the changed lines. It computes and writes nothing: no add, commit, rebase, ref, file or index refresh.
//!
//! The method is that of git-absorb 0.9.0 (BSD 3-Clause, Copyright (c) 2018 Stephen Jung; see `absorb_commute`): the stack
//! is the first-parent chain of HEAD that no remote-tracking branch (and, without `--base`, no other local branch)
//! contains, up to a merge commit, a commit by another author or the limit; a staged hunk belongs to the newest stack
//! commit whose changes do not commute with it. The stack and target rules follow `src/stack.rs` and `src/lib.rs` of
//! git-absorb through the accepted Python spike `spikes/stw/absorb/stw-absorb-plan`, which this module is compared with
//! (`tests/it/absorb_oracle.rs`); deviations are listed in README.md, section `stw absorb plan`.
//!
//! Git boundary: everything is read through gix (refs, commits, trees, the index, blobs, the hunks of a zero-context
//! Myers diff). The only `git` call is `blame` (`Op::BlameLines`, `via: git`) for the lines a hunk removes: gix has no
//! blame. A failing blame is an error, never an empty evidence list. A partial clone is not served: a read that fails in
//! one is `partial-clone-unsupported`, and nothing is ever fetched.
use std::collections::{BTreeMap, HashMap, HashSet};
use std::os::unix::ffi::OsStrExt;
use std::path::Path;

use gix::ObjectId;
use serde_json::{json, Value};

use crate::absorb_commute::{commute_diff_before, lines_match, Hunk};
use crate::git_runner::{BlamePath, Hex40, Op, Runner};
use crate::{repo, sha256};

/// Commits in the stack without `--base`.
pub const DEFAULT_MAX_STACK: u32 = 10;
/// `schema_version` of the JSON document.
pub const SCHEMA_VERSION: u32 = 1;
/// Variables that would make the repository another one than the one the command works in.
const OVERRIDES: [&str; 5] = [
    "GIT_DIR",
    "GIT_WORK_TREE",
    "GIT_INDEX_FILE",
    "GIT_COMMON_DIR",
    "GIT_OBJECT_DIRECTORY",
];

pub struct Options {
    pub base: Option<String>,
    pub max_stack: usize,
    pub ambiguous_newest: bool,
}

/// A refusal with a typed code.
#[derive(Debug)]
pub struct Fail {
    pub code: &'static str,
    pub message: String,
}

impl Fail {
    pub fn to_json(&self) -> Value {
        json!({"schema_version": SCHEMA_VERSION, "command": "absorb",
            "error": {"code": self.code, "message": self.message}})
    }
}

fn fail<T>(code: &'static str, message: impl Into<String>) -> Result<T, Fail> {
    Err(Fail {
        code,
        message: message.into(),
    })
}

pub struct Plan {
    doc: Value,
    pub(crate) absorbed: Vec<AbsorbedHunk>,
    pub(crate) head: ObjectId,
    pub(crate) reference: Vec<u8>,
    pub(crate) rewrite_commits: Vec<ObjectId>,
}

pub(crate) struct AbsorbedHunk {
    pub path: Vec<u8>,
    pub target: ObjectId,
    pub staged: Hunk,
    pub historical: Hunk,
}

impl Plan {
    pub fn to_json(&self) -> &Value {
        &self.doc
    }
    /// One line per fact, deterministic, no command for the caller to type; derived from the document, so that text and
    /// JSON cannot disagree.
    pub fn render_human(&self) -> String {
        use std::fmt::Write;
        let d = &self.doc;
        let text = |v: &Value| v.as_str().unwrap_or_default().to_string();
        let short = |v: &Value| text(v).chars().take(7).collect::<String>();
        let mut out = String::new();
        let _ = writeln!(
            out,
            "absorb plan (read-only): head {} on {}, stack {} commit(s), ends at {}",
            short(&d["head"]),
            text(&d["branch"]),
            d["stack"].as_array().map_or(0, Vec::len),
            text(&d["stack_end"])
        );
        for h in d["hunks"].as_array().into_iter().flatten() {
            let range = format!(
                "-{},{} +{},{}",
                h["old"]["start"], h["old"]["count"], h["new"]["start"], h["new"]["count"]
            );
            if h["assignment"] == "absorbed" {
                let _ = writeln!(
                    out,
                    "{} {} {range} -> {} {} ({})",
                    text(&h["id"]),
                    text(&h["path"]),
                    short(&h["target"]["sha"]),
                    text(&h["target"]["subject"]),
                    text(&h["basis"])
                );
            } else {
                let _ = writeln!(
                    out,
                    "{} {} {range} skipped: {}",
                    text(&h["id"]),
                    text(&h["path"]),
                    text(&h["reason"])
                );
            }
        }
        for f in d["files_skipped"].as_array().into_iter().flatten() {
            let _ = writeln!(
                out,
                "file {} skipped: {}",
                text(&f["path"]),
                text(&f["reason"])
            );
        }
        for f in d["fixups"].as_array().into_iter().flatten() {
            let ids: Vec<String> = f["hunks"]
                .as_array()
                .into_iter()
                .flatten()
                .map(text)
                .collect();
            let _ = writeln!(out, "{} <- {}", text(&f["subject"]), ids.join(", "));
        }
        let rw = &d["rewrite"];
        let oldest = rw["commits"]
            .as_array()
            .and_then(|c| c.first())
            .map(|c| format!(" (oldest {})", short(c)))
            .unwrap_or_default();
        let _ = writeln!(
            out,
            "rewrite: {} commit(s) get new SHAs{oldest}; nothing was written",
            rw["count"]
        );
        out
    }
}

/// The variable that makes the repository ambiguous, if one is set (an empty value counts as unset).
pub fn environment_override() -> Option<Fail> {
    OVERRIDES
        .iter()
        .find(|v| std::env::var_os(v).is_some_and(|x| !x.is_empty()))
        .map(|v| Fail {
            code: "environment-override",
            message: format!(
                "{v} is set: the repository would not be the one of the current directory; unset it"
            ),
        })
}

/// One path of the index or of the tree of HEAD: the mode (`0o100644`, `0o100755`, `0o120000`, `0o160000`) and the blob.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
struct Item {
    mode: u32,
    id: ObjectId,
}

const GITLINK: u32 = 0o160000;
const SYMLINK: u32 = 0o120000;

/// What kind of object a mode names: a regular file (either mode), a link or a gitlink.
fn class(mode: u32) -> u32 {
    match mode {
        GITLINK | SYMLINK => mode,
        _ => 0o100000,
    }
}

/// One staged change of a path (no rename detection): the status letters of `git diff-index --raw`.
struct Change {
    path: Vec<u8>,
    status: char,
    old: Option<Item>,
    new: Option<Item>,
}

/// A commit of the stack, or one a hunk's lines come from.
#[derive(Clone)]
struct Rec {
    id: ObjectId,
    parents: Vec<ObjectId>,
    name: Vec<u8>,
    email: Vec<u8>,
    subject: Vec<u8>,
}

/// What one stack commit did to one path.
#[derive(Clone)]
struct FileDiff {
    /// The commit created the path (or changed its type): every hunk of the path belongs to it.
    added: bool,
    binary: bool,
    hunks: Vec<Hunk>,
}

/// One staged hunk and what became of it.
struct Entry {
    path: Vec<u8>,
    old: (i64, usize),
    new: (i64, usize),
    /// The stack index of the target commit (newest is 0), also for a skipped hunk whose target is named.
    target: Option<usize>,
    basis: Option<&'static str>,
    absorbed: bool,
    apply: Option<(Hunk, Hunk)>,
    reason: Option<&'static str>,
    origins: Vec<(ObjectId, usize)>,
}

struct Planner<'a> {
    repo: gix::Repository,
    dir: &'a Path,
    runner: &'a Runner,
    opts: &'a Options,
    /// The repository is a partial clone: a failing read means a missing object.
    partial: bool,
    head: ObjectId,
    ref_name: Vec<u8>,
    ident: (Vec<u8>, Vec<u8>),
    base: Option<ObjectId>,
    index_digest: String,
    /// Every commit a remote-tracking branch contains.
    remote: HashSet<ObjectId>,
    stack: Vec<Rec>,
    stack_end: &'static str,
    records: HashMap<ObjectId, Rec>,
    diffs: HashMap<(usize, Vec<u8>), Option<FileDiff>>,
    entries: Vec<Entry>,
    files_skipped: Vec<(Vec<u8>, &'static str)>,
}

/// The plan of the staged changes of the repository at `dir` (its work tree root). `start` is the directory the command
/// runs in (the search for a `.jj` directory starts there and ends at `dir`).
pub fn plan(dir: &Path, start: &Path, runner: &Runner, opts: &Options) -> Result<Plan, Fail> {
    plan_in(open_repository(dir, start)?, dir, runner, opts)
}

pub(crate) fn open_repository(dir: &Path, start: &Path) -> Result<gix::Repository, Fail> {
    let repo = repo::open(dir).map_err(|e| Fail {
        code: "git-failed",
        message: format!("cannot open the repository: {e}"),
    })?;
    detect_backend(&repo, dir, start)?;
    Ok(repo)
}

pub(crate) fn plan_in(
    repo: gix::Repository,
    dir: &Path,
    runner: &Runner,
    opts: &Options,
) -> Result<Plan, Fail> {
    let partial = is_partial(&repo);
    let mut p = Planner {
        repo,
        dir,
        runner,
        opts,
        partial,
        head: ObjectId::null(gix::hash::Kind::Sha1),
        ref_name: Vec::new(),
        ident: (Vec::new(), Vec::new()),
        base: None,
        index_digest: String::new(),
        remote: HashSet::new(),
        stack: Vec::new(),
        stack_end: "root",
        records: HashMap::new(),
        diffs: HashMap::new(),
        entries: Vec::new(),
        files_skipped: Vec::new(),
    };
    p.run()
}

fn exists(path: &Path) -> bool {
    std::fs::symlink_metadata(path).is_ok()
}

/// Backend metadata is read before anything else: `.jj` in the directory or above it up to the work tree root, GitButler
/// markers in the git directories or the refs. Git is the only supported backend; there is no fallback to Git writes.
fn detect_backend(repo: &gix::Repository, top: &Path, start: &Path) -> Result<(), Fail> {
    let mut jj = false;
    let mut d = if start.starts_with(top) { start } else { top };
    loop {
        let marker = d.join(".jj");
        if let Ok(md) = std::fs::symlink_metadata(&marker) {
            if md.file_type().is_symlink() || !md.is_dir() {
                return fail(
                    "invalid-metadata",
                    format!(
                        "{} is not a directory",
                        sanitize_bytes(marker.as_os_str().as_bytes())
                    ),
                );
            }
            jj = true;
            break;
        }
        match d.parent() {
            Some(up) if d != top => d = up,
            _ => break,
        }
    }
    let butler = exists(&repo.git_dir().join("gitbutler"))
        || exists(&repo.common_dir().join("gitbutler"))
        || has_butler_refs(repo);
    match (jj, butler) {
        (true, true) => fail(
            "ambiguous-backend",
            "jj and GitButler metadata are both present; ownership is ambiguous and nothing was read",
        ),
        (true, false) => fail(
            "unsupported-backend",
            "jj workspace (.jj found): stw absorb supports Git only and never falls back to Git writes",
        ),
        (false, true) => fail(
            "unsupported-backend",
            "GitButler-managed repository: stw absorb supports Git only and never falls back to Git writes",
        ),
        (false, false) => Ok(()),
    }
}

/// Any `refs/gitbutler/*` or the exact workspace branch `refs/heads/gitbutler/workspace`.
fn has_butler_refs(repo: &gix::Repository) -> bool {
    let any = repo
        .references()
        .and_then(|p| {
            p.prefixed("refs/gitbutler/")
                .map(|mut i| i.next().is_some())
        })
        .unwrap_or(false);
    any || matches!(
        repo.try_find_reference("refs/heads/gitbutler/workspace"),
        Ok(Some(_))
    )
}

/// A promisor remote or `extensions.partialclone`: objects may be missing and nothing is fetched.
fn is_partial(repo: &gix::Repository) -> bool {
    let snap = repo.config_snapshot();
    let cfg = snap.plumbing();
    let promisor = cfg
        .sections_by_name("remote")
        .into_iter()
        .flatten()
        .any(|s| s.header().subsection_name().is_some() && s.body().value("promisor").is_some());
    promisor || cfg.string("extensions.partialclone").is_some()
}

fn mode_of(kind: gix::objs::tree::EntryKind) -> u32 {
    u32::from_str_radix(crate::patch_id::mode_str(kind), 8).unwrap_or(0)
}

/// The zero-context hunks of `before` -> `after`: a Myers diff without the indent heuristic, as `git diff -U0
/// --diff-algorithm=myers --no-indent-heuristic` prints them (the numbers are those of the hunk header).
fn diff_hunks(before: &[u8], after: &[u8]) -> Vec<Hunk> {
    use gix::diff::blob::{Algorithm, Diff, InternedInput};
    let input = InternedInput::new(before, after);
    let mut diff = Diff::compute(Algorithm::Myers, &input);
    diff.postprocess_no_heuristic(&input);
    diff.hunks()
        .map(|h| {
            let rem: Vec<Vec<u8>> = h
                .before
                .clone()
                .map(|k| input.interner[input.before[k as usize]].to_vec())
                .collect();
            let add: Vec<Vec<u8>> = h
                .after
                .clone()
                .map(|k| input.interner[input.after[k as usize]].to_vec())
                .collect();
            // a side without lines is numbered by the line before it
            Hunk {
                rem_start: i64::from(h.before.start) + i64::from(!rem.is_empty()),
                add_start: i64::from(h.after.start) + i64::from(!add.is_empty()),
                rem,
                add,
            }
        })
        .collect()
}

/// The first paragraph of a commit message as one line (`%s` of `git log`): blank lines before it are skipped, its lines
/// lose their trailing white space and are joined by a space.
fn subject_of(message: &[u8]) -> Vec<u8> {
    let mut out: Vec<u8> = Vec::new();
    let mut started = false;
    for line in message.split(|b| *b == b'\n') {
        let line = line.trim_ascii_end();
        if line.is_empty() {
            if started {
                break;
            }
            continue;
        }
        if started {
            out.push(b' ');
        }
        started = true;
        out.extend_from_slice(line);
    }
    out
}

impl Planner<'_> {
    /// A failed read: in a partial clone an object is missing, else the repository is unreadable.
    fn read_err(&self, what: &str, e: impl std::fmt::Display) -> Fail {
        if self.partial {
            Fail {
                code: "partial-clone-unsupported",
                message: format!("{what} failed in a partial clone (objects may be missing; no lazy fetch is allowed): {e}"),
            }
        } else {
            Fail {
                code: "git-failed",
                message: format!("{what} failed: {e}"),
            }
        }
    }

    fn rd<T, E: std::fmt::Display>(&self, what: &str, r: Result<T, E>) -> Result<T, Fail> {
        r.map_err(|e| self.read_err(what, e))
    }

    fn run(&mut self) -> Result<Plan, Fail> {
        let head = self.rd("reading HEAD", self.repo.head())?;
        if head.is_unborn() {
            return fail("no-commits", "the repository has no commit yet");
        }
        let Some(name) = head.referent_name() else {
            return fail("detached-head", "HEAD is not on a branch");
        };
        self.ref_name = name.as_bstr().to_vec();
        self.head = self.rd("reading HEAD", self.repo.head_id())?.detach();
        self.ident = self.identity()?;
        if let Some(rev) = &self.opts.base {
            self.base = Some(self.resolve_base(rev)?);
        }
        let changes = self.staged()?;
        if changes.is_empty() {
            return fail("nothing-staged", "nothing is staged: the index equals HEAD");
        }
        self.walk()?;
        for c in &changes {
            self.change(c)?;
        }
        Ok(self.document())
    }

    fn identity(&self) -> Result<(Vec<u8>, Vec<u8>), Fail> {
        let snap = self.repo.config_snapshot();
        let mut out = Vec::new();
        for key in ["user.name", "user.email"] {
            match snap.string(key).filter(|v| !v.is_empty()) {
                Some(v) => out.push(v.to_vec()),
                None => {
                    return fail(
                        "no-identity",
                        format!("{key} is not set in the Git configuration"),
                    )
                }
            }
        }
        let email = out.pop().unwrap_or_default();
        Ok((out.pop().unwrap_or_default(), email))
    }

    fn resolve_base(&self, rev: &str) -> Result<ObjectId, Fail> {
        let bad = || Fail {
            code: "bad-base",
            message: format!("--base {} is not a commit", sanitize(rev)),
        };
        let id = self
            .repo
            .rev_parse_single(format!("{rev}^{{commit}}").as_str())
            .map_err(|_| bad())?
            .detach();
        if !repo::is_ancestor(&self.repo, id, self.head).unwrap_or(false) {
            return fail(
                "bad-base",
                format!("--base {} is not an ancestor of HEAD", sanitize(rev)),
            );
        }
        Ok(id)
    }

    /// The staged changes, sorted by path: the index against the tree of HEAD. An unmerged path is a refusal. The digest of
    /// the whole index (`git ls-files -s -z`: mode, id, stage, path of every entry) is kept for the document.
    fn staged(&mut self) -> Result<Vec<Change>, Fail> {
        let index = self.rd("reading the index", self.repo.index_or_empty())?;
        let mut new: BTreeMap<Vec<u8>, Item> = BTreeMap::new();
        let mut listing: Vec<u8> = Vec::new();
        for e in index.entries() {
            if e.stage_raw() != 0 {
                return fail(
                    "unmerged-index",
                    "the index has unmerged paths (a merge is in progress)",
                );
            }
            let path = e.path(&index);
            listing.extend(format!("{:06o} {} 0\t", e.mode.bits(), e.id).bytes());
            listing.extend_from_slice(path);
            listing.push(0);
            new.insert(
                path.to_vec(),
                Item {
                    mode: e.mode.bits(),
                    id: e.id,
                },
            );
        }
        self.index_digest = sha256::hex(&listing);
        let commit = self.rd("reading HEAD", self.repo.find_commit(self.head))?;
        let tree = self.rd("reading the tree of HEAD", commit.tree())?;
        let mut rec = gix::traverse::tree::Recorder::default();
        self.rd(
            "reading the tree of HEAD",
            tree.traverse().breadthfirst(&mut rec),
        )?;
        let mut old: BTreeMap<Vec<u8>, Item> = BTreeMap::new();
        for e in rec.records {
            if !e.mode.is_tree() {
                old.insert(
                    e.filepath.to_vec(),
                    Item {
                        mode: u32::from(e.mode.value()),
                        id: e.oid,
                    },
                );
            }
        }
        let mut out = Vec::new();
        for path in old
            .keys()
            .chain(new.keys())
            .collect::<std::collections::BTreeSet<_>>()
        {
            let (o, n) = (old.get(path).copied(), new.get(path).copied());
            let status = match (o, n) {
                (Some(a), Some(b)) if a == b => continue,
                (Some(a), Some(b)) if class(a.mode) != class(b.mode) => 'T',
                (Some(_), Some(_)) => 'M',
                (None, Some(_)) => 'A',
                (Some(_), None) => 'D',
                (None, None) => continue,
            };
            out.push(Change {
                path: path.clone(),
                status,
                old: o,
                new: n,
            });
        }
        Ok(out)
    }

    // --- the stack --------------------------------------------------------------------------------------------------

    fn rec(&mut self, id: ObjectId) -> Result<Rec, Fail> {
        if let Some(r) = self.records.get(&id) {
            return Ok(r.clone());
        }
        let c = self.rd("reading a commit", self.repo.find_commit(id))?;
        let author = self.rd("reading a commit", c.author())?;
        let r = Rec {
            id,
            parents: c.parent_ids().map(|p| p.detach()).collect(),
            name: author.name.to_vec(),
            email: author.email.to_vec(),
            subject: subject_of(self.rd("reading a commit", c.message_raw())?),
        };
        self.records.insert(id, r.clone());
        Ok(r)
    }

    fn foreign(&self, r: &Rec) -> bool {
        (&r.name, &r.email) != (&self.ident.0, &self.ident.1)
    }

    /// The tips of the refs below `prefix` that are commits (tags are peeled), except the ref named `skip`.
    fn tips(&self, prefix: &str, skip: Option<&[u8]>) -> Result<Vec<ObjectId>, Fail> {
        let platform = self.rd("reading the refs", self.repo.references())?;
        let iter = self.rd("reading the refs", platform.prefixed(prefix))?;
        let mut out = Vec::new();
        for r in iter {
            let mut r = self.rd("reading the refs", r)?;
            if skip == Some(r.name().as_bstr().as_ref()) {
                continue;
            }
            if let Ok(id) = r.peel_to_id() {
                if self.repo.find_commit(id.detach()).is_ok() {
                    out.push(id.detach());
                }
            }
        }
        Ok(out)
    }

    /// Every commit reachable from `tips`.
    fn closure(&self, tips: Vec<ObjectId>) -> Result<HashSet<ObjectId>, Fail> {
        let mut out = HashSet::new();
        if tips.is_empty() {
            return Ok(out);
        }
        let walk = self.rd("walking the history", self.repo.rev_walk(tips).all())?;
        for info in walk {
            out.insert(self.rd("walking the history", info)?.id);
        }
        Ok(out)
    }

    /// The stack: the first-parent commits of HEAD that no remote-tracking ref (and, without `--base`, no other local
    /// branch) contains, newest first, up to the first merge commit, foreign commit or the limit (the stack rules of
    /// git-absorb).
    fn walk(&mut self) -> Result<(), Fail> {
        self.remote = self.closure(self.tips("refs/remotes/", None)?)?;
        let hidden = match self.base {
            Some(b) => self.closure(vec![b])?,
            None => {
                let heads = self.tips("refs/heads/", Some(&self.ref_name))?;
                self.closure(heads)?
            }
        };
        let limit = if self.base.is_some() {
            usize::MAX
        } else {
            self.opts.max_stack + 1
        };
        let mut recs: Vec<Rec> = Vec::new();
        let mut cur = Some(self.head);
        while let Some(id) = cur {
            if recs.len() >= limit || self.remote.contains(&id) || hidden.contains(&id) {
                break;
            }
            let r = self.rec(id)?;
            cur = r.parents.first().copied();
            recs.push(r);
        }
        let end = |base: bool| if base { "base" } else { "branch" };
        let Some(first) = recs.first() else {
            if self.remote.contains(&self.head) {
                return fail(
                    "published",
                    "HEAD is contained in a remote-tracking branch: nothing to rewrite",
                );
            }
            self.stack_end = end(self.base.is_some());
            return Ok(());
        };
        if first.parents.len() <= 1 && self.foreign(first) {
            return fail(
                "not-owned",
                format!(
                    "HEAD is by another author ({} <{}>)",
                    sanitize_bytes(&first.name),
                    sanitize_bytes(&first.email)
                ),
            );
        }
        let mut stack: Vec<Rec> = Vec::new();
        for r in recs {
            if r.parents.len() > 1 {
                self.stack_end = "merge";
                self.stack = stack;
                return Ok(());
            }
            if self.foreign(&r) {
                self.stack_end = "foreign";
                self.stack = stack;
                return Ok(());
            }
            if self.base.is_none() && stack.len() == self.opts.max_stack {
                self.stack_end = "limit";
                self.stack = stack;
                return Ok(());
            }
            stack.push(r);
        }
        self.stack_end = match stack.last().and_then(|r| r.parents.first().copied()) {
            None => "root",
            Some(next) if self.remote.contains(&next) => "published",
            Some(next) => {
                let r = self.rec(next)?;
                if r.parents.len() > 1 {
                    "merge"
                } else if self.foreign(&r) {
                    "foreign"
                } else {
                    end(self.base.is_some())
                }
            }
        };
        self.stack = stack;
        Ok(())
    }

    // --- reading trees and blobs ------------------------------------------------------------------------------------

    fn tree_of(&self, id: ObjectId) -> Result<ObjectId, Fail> {
        let c = self.rd("reading a commit", self.repo.find_commit(id))?;
        Ok(self.rd("reading a commit", c.tree_id())?.detach())
    }

    fn lookup(&self, tree: ObjectId, path: &[u8]) -> Result<Option<Item>, Fail> {
        let t = self.rd("reading a tree", self.repo.find_tree(tree))?;
        let rel = Path::new(std::ffi::OsStr::from_bytes(path));
        let e = self.rd("reading a tree", t.lookup_entry_by_path(rel))?;
        Ok(e.map(|e| Item {
            mode: mode_of(e.mode().kind()),
            id: e.object_id(),
        }))
    }

    fn blob(&self, id: ObjectId) -> Result<Vec<u8>, Fail> {
        Ok(self
            .rd("reading a blob", self.repo.find_object(id))?
            .data
            .clone())
    }

    /// What stack commit `i` did to `path`; `None` when it left the path alone (or touched a gitlink, which the diff of
    /// the plan ignores).
    fn diff_of(&mut self, i: usize, path: &[u8]) -> Result<Option<FileDiff>, Fail> {
        let key = (i, path.to_vec());
        if let Some(d) = self.diffs.get(&key) {
            return Ok(d.clone());
        }
        let rec = self.stack[i].clone();
        let new = self.lookup(self.tree_of(rec.id)?, path)?;
        let old = match rec.parents.first() {
            Some(p) => self.lookup(self.tree_of(*p)?, path)?,
            None => None,
        };
        let d = self.file_diff(old, new)?;
        self.diffs.insert(key, d.clone());
        Ok(d)
    }

    fn file_diff(&self, old: Option<Item>, new: Option<Item>) -> Result<Option<FileDiff>, Fail> {
        if old == new || [old, new].iter().flatten().any(|x| x.mode == GITLINK) {
            return Ok(None);
        }
        let added = match (old, new) {
            (None, Some(_)) => true,
            (Some(a), Some(b)) => class(a.mode) != class(b.mode),
            _ => false,
        };
        if added {
            return Ok(Some(FileDiff {
                added,
                binary: false,
                hunks: Vec::new(),
            }));
        }
        let read = |x: Option<Item>| x.map_or(Ok(Vec::new()), |x| self.blob(x.id));
        let (before, after) = (read(old)?, read(new)?);
        let binary = crate::patch_id::is_binary(&before) || crate::patch_id::is_binary(&after);
        Ok(Some(FileDiff {
            added,
            binary,
            hunks: if binary {
                Vec::new()
            } else {
                diff_hunks(&before, &after)
            },
        }))
    }

    // --- the staged changes -----------------------------------------------------------------------------------------

    fn change(&mut self, c: &Change) -> Result<(), Fail> {
        let skip = |p: &mut Self, why: &'static str| {
            p.files_skipped.push((c.path.clone(), why));
            Ok(())
        };
        let linkish = |x: Option<Item>| x.is_some_and(|x| x.mode == SYMLINK);
        if [c.old, c.new].iter().flatten().any(|x| x.mode == GITLINK) {
            return skip(self, "submodule");
        }
        match c.status {
            'A' => return skip(self, "added"),
            'D' => return skip(self, "deleted"),
            'T' => return skip(self, "typechange"),
            'M' => {}
            _ => return skip(self, "other"),
        }
        if linkish(c.old) || linkish(c.new) {
            return skip(self, "symlink");
        }
        let Some(d) = self.file_diff(c.old, c.new)? else {
            return skip(self, "mode-only");
        };
        if d.binary {
            return skip(self, "binary");
        }
        if d.hunks.is_empty() {
            return skip(self, "mode-only");
        }
        let mut preceding = 0;
        for h in d.hunks {
            let isolated = h.shifted(-preceding, 0);
            preceding += h.offset();
            self.hunk(&c.path, &h, &isolated)?;
        }
        Ok(())
    }

    /// Place one hunk. `h` has the numbers of the staged diff, `isolated` those it would have without the hunks before it
    /// in the same file (the commutation test works on one hunk at a time).
    fn hunk(&mut self, path: &[u8], h: &Hunk, isolated: &Hunk) -> Result<(), Fail> {
        let mut e = Entry {
            path: path.to_vec(),
            old: (h.rem_start, h.rem.len()),
            new: (h.add_start, h.add.len()),
            target: None,
            basis: None,
            absorbed: false,
            apply: None,
            reason: None,
            origins: Vec::new(),
        };
        let found = self.locate(path, isolated)?;
        if !h.rem.is_empty() {
            e.origins = self.blame(path, h)?;
            for (id, _) in &e.origins {
                self.rec(*id)?;
            }
        }
        match found {
            None => {
                let shas: Vec<ObjectId> = e.origins.iter().map(|(s, _)| *s).collect();
                e.reason = Some(self.outside_reason(&shas)?);
            }
            Some((i, commuted, basis)) => {
                e.target = Some(i);
                e.basis = Some(basis);
                let ok = self.consistent(i, path, &commuted)?;
                match decide(ok, e.origins.len(), self.opts.ambiguous_newest) {
                    Ok(()) => {
                        e.absorbed = true;
                        e.apply = Some((isolated.clone(), commuted));
                    }
                    Err(why) => e.reason = Some(why),
                }
            }
        }
        self.entries.push(e);
        Ok(())
    }

    /// The newest stack commit whose changes do not commute with the hunk, with the hunk as it is in that commit.
    fn locate(
        &mut self,
        path: &[u8],
        hunk: &Hunk,
    ) -> Result<Option<(usize, Hunk, &'static str)>, Fail> {
        let mut cur = hunk.clone();
        for i in 0..self.stack.len() {
            let Some(d) = self.diff_of(i, path)? else {
                continue;
            };
            if d.added {
                return Ok(Some((i, cur, "file-added")));
            }
            match commute_diff_before(&cur, &d.hunks) {
                None => return Ok(Some((i, cur, "overlaps-lines"))),
                Some(next) => cur = next,
            }
        }
        Ok(None)
    }

    /// The removed lines of the commuted hunk are where it says in the blob of the target commit.
    fn consistent(&self, i: usize, path: &[u8], hunk: &Hunk) -> Result<bool, Fail> {
        let tree = self.tree_of(self.stack[i].id)?;
        let item = match self.lookup(tree, path) {
            Ok(Some(item)) if item.mode != GITLINK => item,
            Ok(_) => return Ok(false),
            Err(f) if f.code == "git-failed" => return Ok(false),
            Err(f) => return Err(f),
        };
        match self.blob(item.id) {
            Ok(blob) => Ok(lines_match(&blob, hunk)),
            Err(f) if f.code == "git-failed" => Ok(false),
            Err(f) => Err(f),
        }
    }

    /// The commits that last changed the removed lines of `hunk` in HEAD, with the number of lines of each, in order of
    /// first appearance (evidence, not the decision: the decision is the commutation test).
    fn blame(&self, path: &[u8], hunk: &Hunk) -> Result<Vec<(ObjectId, usize)>, Fail> {
        let shown = sanitize_bytes(path);
        let no_evidence = |why: String| Fail {
            code: "blame-failed",
            message: format!("no evidence for the lines removed in {shown}: {why}"),
        };
        let first =
            u32::try_from(hunk.rem_start).map_err(|_| no_evidence("bad line number".into()))?;
        let last = first + hunk.rem.len() as u32 - 1;
        let bp = BlamePath::parse(path)
            .ok_or_else(|| no_evidence("the path is not UTF-8 text".into()))?;
        let rev =
            Hex40::parse(&self.head.to_string()).ok_or_else(|| no_evidence("bad head".into()))?;
        let out = self
            .runner
            .run(
                self.dir,
                Op::BlameLines {
                    rev,
                    path: bp,
                    first,
                    last,
                },
            )
            .map_err(|e| {
                if self.partial {
                    self.read_err("blame", e)
                } else {
                    no_evidence(e.to_string())
                }
            })?;
        let mut counts: Vec<(ObjectId, usize)> = Vec::new();
        for line in out.stdout.split(|b| *b == b'\n') {
            let Some(id) = porcelain_header(line) else {
                continue;
            };
            match counts.iter_mut().find(|(s, _)| *s == id) {
                Some((_, n)) => *n += 1,
                None => counts.push((id, 1)),
            }
        }
        Ok(counts)
    }

    /// A hunk that commutes with the whole stack: no-candidate, or why the lines it removes cannot be reached.
    fn outside_reason(&mut self, shas: &[ObjectId]) -> Result<&'static str, Fail> {
        let outside: Vec<ObjectId> = shas
            .iter()
            .filter(|s| !self.stack.iter().any(|r| r.id == **s))
            .copied()
            .collect();
        if outside.is_empty() {
            return Ok("no-candidate");
        }
        if outside.iter().any(|s| self.remote.contains(s)) {
            return Ok("published");
        }
        for s in &outside {
            let r = self.rec(*s)?;
            if self.foreign(&r) {
                return Ok("foreign");
            }
        }
        Ok("outside-range")
    }

    // --- the document -----------------------------------------------------------------------------------------------

    fn document(&mut self) -> Plan {
        let mut counts: HashMap<&[u8], usize> = HashMap::new();
        for r in &self.stack {
            *counts.entry(&r.subject).or_default() += 1;
        }
        let mut fixups: Vec<(usize, Vec<String>)> = Vec::new();
        for (n, e) in self.entries.iter().enumerate() {
            let Some(t) = e.target.filter(|_| e.absorbed) else {
                continue;
            };
            let id = format!("h{}", n + 1);
            match fixups.iter_mut().find(|(i, _)| *i == t) {
                Some((_, hunks)) => hunks.push(id),
                None => fixups.push((t, vec![id])),
            }
        }
        let rewritten: Vec<ObjectId> = match fixups.iter().map(|(i, _)| *i).max() {
            Some(oldest) => self.stack[..=oldest].iter().rev().map(|r| r.id).collect(),
            None => Vec::new(),
        };
        let target = |i: usize| {
            let r = &self.stack[i];
            json!({"sha": r.id.to_string(), "subject": sanitize_bytes(&r.subject)})
        };
        let hunks: Vec<Value> = self
            .entries
            .iter()
            .enumerate()
            .map(|(n, e)| {
                let origins: Vec<Value> = e
                    .origins
                    .iter()
                    .map(|(s, lines)| {
                        let subject = self
                            .records
                            .get(s)
                            .map(|r| sanitize_bytes(&r.subject))
                            .unwrap_or_default();
                        json!({"sha": s.to_string(), "subject": subject, "lines": lines})
                    })
                    .collect();
                json!({"id": format!("h{}", n + 1), "path": sanitize_bytes(&e.path),
                    "old": {"start": e.old.0, "count": e.old.1},
                    "new": {"start": e.new.0, "count": e.new.1},
                    "assignment": if e.absorbed { "absorbed" } else { "skipped" },
                    "target": e.target.map(target),
                    "basis": e.basis, "reason": e.reason, "origins": origins})
            })
            .collect();
        let fixups_json: Vec<Value> = fixups
            .iter()
            .map(|(i, ids)| {
                let r = &self.stack[*i];
                let subject = if counts[r.subject.as_slice()] == 1 {
                    sanitize_bytes(&r.subject)
                } else {
                    r.id.to_string()
                };
                json!({"target": r.id.to_string(), "subject": format!("fixup! {subject}"), "hunks": ids})
            })
            .collect();
        let absorbed = self.entries.iter().filter(|e| e.absorbed).count();
        let ref_name = sanitize_bytes(&self.ref_name);
        let branch = ref_name
            .strip_prefix("refs/heads/")
            .unwrap_or(&ref_name)
            .to_string();
        let base = self.opts.base.as_deref().map(sanitize);
        let mut doc = json!({
            "schema_version": SCHEMA_VERSION, "command": "absorb", "mode": "plan",
            "head": self.head.to_string(), "branch": branch,
            "index_digest": self.index_digest,
            "base": base, "max_stack": self.opts.max_stack,
            "stack": self.stack.iter().enumerate().map(|(i, _)| target(i)).collect::<Vec<_>>(),
            "stack_end": self.stack_end,
            "hunks": hunks,
            "files_skipped": self.files_skipped.iter()
                .map(|(p, why)| json!({"path": sanitize_bytes(p), "reason": why})).collect::<Vec<_>>(),
            "fixups": fixups_json,
            "rewrite": {"count": rewritten.len(), "commits": rewritten.iter().map(ToString::to_string).collect::<Vec<_>>(), "ref": ref_name, "new_shas": null},
            "summary": {"hunks": self.entries.len(), "absorbed": absorbed,
                "skipped": self.entries.len() - absorbed, "fixups": fixups.len(), "rewritten": rewritten.len()},
            "effects": {"writes": [], "index_refresh": false, "network": false, "hooks": false, "prompts": false},
        });
        let options = json!({"base": self.opts.base, "max_stack": self.opts.max_stack,
            "ambiguous_newest": self.opts.ambiguous_newest});
        let raw: Vec<Vec<u8>> = self.entries.iter().map(|e| e.path.clone()).collect();
        let sum = plan_sum(&options, &raw, &doc);
        if let Value::Object(m) = &mut doc {
            m.insert("plan_sum".into(), Value::String(sum));
        }
        let absorbed = self
            .entries
            .iter()
            .filter_map(|entry| {
                let (staged, historical) = entry.apply.as_ref()?;
                Some(AbsorbedHunk {
                    path: entry.path.clone(),
                    target: self.stack[entry.target?].id,
                    staged: staged.clone(),
                    historical: historical.clone(),
                })
            })
            .collect();
        Plan {
            doc,
            absorbed,
            head: self.head,
            reference: self.ref_name.clone(),
            rewrite_commits: rewritten,
        }
    }
}

/// The commit of a `git blame --porcelain` header line (`<40 hex> <orig line> <final line>[ <group size>]`).
fn porcelain_header(line: &[u8]) -> Option<ObjectId> {
    let text = std::str::from_utf8(line).ok()?;
    let mut parts = text.split(' ');
    let (sha, a, b) = (parts.next()?, parts.next()?, parts.next()?);
    let number = |x: &str| !x.is_empty() && x.bytes().all(|c| c.is_ascii_digit());
    let group = parts.next();
    let ok = sha.len() == 40
        && sha.bytes().all(|c| matches!(c, b'0'..=b'9' | b'a'..=b'f'))
        && number(a)
        && number(b)
        && group.is_none_or(number)
        && parts.next().is_none();
    ok.then(|| ObjectId::from_hex(sha.as_bytes()).ok())
        .flatten()
}

/// Bidi controls, zero-width characters and line and paragraph separators (the C0 and C1 controls are tested by range).
const MASKED: [char; 17] = [
    '\u{202a}', '\u{202b}', '\u{202c}', '\u{202d}', '\u{202e}', '\u{2066}', '\u{2067}', '\u{2068}',
    '\u{2069}', '\u{200e}', '\u{200f}', '\u{061c}', '\u{200b}', '\u{2060}', '\u{feff}', '\u{2028}',
    '\u{2029}',
];

/// Control, bidi and zero-width characters and line and paragraph separators become `?`.
pub fn sanitize(text: &str) -> String {
    text.chars()
        .map(|c| {
            let n = c as u32;
            if n < 0x20 || (0x7f..0xa0).contains(&n) || MASKED.contains(&c) {
                '?'
            } else {
                c
            }
        })
        .collect()
}

/// `sanitize` for bytes: every byte of an invalid UTF-8 sequence becomes `?`.
pub fn sanitize_bytes(bytes: &[u8]) -> String {
    let mut out = String::new();
    for chunk in bytes.utf8_chunks() {
        out.push_str(&sanitize(chunk.valid()));
        out.extend(std::iter::repeat_n('?', chunk.invalid().len()));
    }
    out
}

/// Verdict of one placed hunk: `Ok` when it is absorbed, else the reason it is skipped. `consistent` is the check that
/// the removed lines are where the commuted hunk says in the target's blob; `origin_commits` the number of commits the
/// removed lines come from. A hunk that cannot be applied is a conflict before it is ambiguous.
pub fn decide(
    consistent: bool,
    origin_commits: usize,
    ambiguous_newest: bool,
) -> Result<(), &'static str> {
    if !consistent {
        return Err("conflict");
    }
    if origin_commits >= 2 && !ambiguous_newest {
        return Err("ambiguous");
    }
    Ok(())
}

/// The JSON text that the plan checksum covers: object keys sorted, no white space, every character outside space..~ as
/// `\uXXXX` (a character above the BMP as a surrogate pair). The encoding of the Python spike (`json.dumps(sort_keys=True,
/// separators=(",", ":"))`), so that the two programs agree on the checksum.
pub fn canonical_json(value: &Value) -> String {
    let mut out = String::new();
    canon(value, &mut out);
    out
}

fn canon(v: &Value, out: &mut String) {
    match v {
        Value::Null => out.push_str("null"),
        Value::Bool(b) => out.push_str(if *b { "true" } else { "false" }),
        Value::Number(n) => out.push_str(&n.to_string()),
        Value::String(s) => {
            out.push('"');
            escape_into(s, out);
            out.push('"');
        }
        Value::Array(a) => {
            out.push('[');
            for (i, x) in a.iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                canon(x, out);
            }
            out.push(']');
        }
        Value::Object(m) => {
            let mut keys: Vec<&String> = m.keys().collect();
            keys.sort();
            out.push('{');
            for (i, k) in keys.into_iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                out.push('"');
                escape_into(k, out);
                out.push_str("\":");
                canon(&m[k.as_str()], out);
            }
            out.push('}');
        }
    }
}

fn escape_into(s: &str, out: &mut String) {
    use std::fmt::Write;
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            '\u{8}' => out.push_str("\\b"),
            '\u{c}' => out.push_str("\\f"),
            ' '..='~' => out.push(c),
            _ => {
                for unit in c.encode_utf16(&mut [0; 2]) {
                    let _ = write!(out, "\\u{unit:04x}");
                }
            }
        }
    }
}

/// SHA-256 over `{"options":..,"plan":..,"raw_paths":[..]}` in the encoding of `canonical_json`. `raw_paths` are the
/// paths as bytes, unmasked; a byte of an invalid UTF-8 sequence is `\udcXX`.
pub fn plan_sum(options: &Value, raw_paths: &[Vec<u8>], plan: &Value) -> String {
    use std::fmt::Write;
    let mut text = String::from("{\"options\":");
    canon(options, &mut text);
    text.push_str(",\"plan\":");
    canon(plan, &mut text);
    text.push_str(",\"raw_paths\":[");
    for (i, p) in raw_paths.iter().enumerate() {
        if i > 0 {
            text.push(',');
        }
        text.push('"');
        for chunk in p.utf8_chunks() {
            escape_into(chunk.valid(), &mut text);
            for b in chunk.invalid() {
                let _ = write!(text, "\\udc{b:02x}");
            }
        }
        text.push('"');
    }
    text.push_str("]}");
    sha256::hex(text.as_bytes())
}
