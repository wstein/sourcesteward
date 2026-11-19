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
use std::collections::BTreeMap;
use std::os::unix::ffi::OsStrExt;
use std::path::Path;

use gix::ObjectId;
use serde_json::{json, Value};

use crate::git_runner::Runner;
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
}

impl Plan {
    pub fn to_json(&self) -> &Value {
        &self.doc
    }
    pub fn render_human(&self) -> String {
        String::new()
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

/// One staged change of a path (no rename detection): the status letters of `git diff-index --raw`. The fields are read by
/// the plan of the next commit; the guards only ask whether there is one.
#[allow(dead_code)]
struct Change {
    path: Vec<u8>,
    status: char,
    old: Option<Item>,
    new: Option<Item>,
}

struct Planner<'a> {
    repo: gix::Repository,
    opts: &'a Options,
    /// The repository is a partial clone: a failing read means a missing object.
    partial: bool,
    head: ObjectId,
    ref_name: Vec<u8>,
    ident: (Vec<u8>, Vec<u8>),
    base: Option<ObjectId>,
}

/// The plan of the staged changes of the repository at `dir` (its work tree root). `start` is the directory the command
/// runs in (the search for a `.jj` directory starts there and ends at `dir`).
pub fn plan(dir: &Path, start: &Path, _runner: &Runner, opts: &Options) -> Result<Plan, Fail> {
    let repo = repo::open(dir).map_err(|e| Fail {
        code: "git-failed",
        message: format!("cannot open the repository: {e}"),
    })?;
    detect_backend(&repo, dir, start)?;
    let partial = is_partial(&repo);
    let mut p = Planner {
        repo,
        opts,
        partial,
        head: ObjectId::null(gix::hash::Kind::Sha1),
        ref_name: Vec::new(),
        ident: (Vec::new(), Vec::new()),
        base: None,
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
        fail("not-implemented", "stw absorb is not implemented yet")
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

    /// The staged changes, sorted by path: the index against the tree of HEAD. An unmerged path is a refusal.
    fn staged(&self) -> Result<Vec<Change>, Fail> {
        let index = self.rd("reading the index", self.repo.index_or_empty())?;
        let mut new: BTreeMap<Vec<u8>, Item> = BTreeMap::new();
        for e in index.entries() {
            if e.stage_raw() != 0 {
                return fail(
                    "unmerged-index",
                    "the index has unmerged paths (a merge is in progress)",
                );
            }
            new.insert(
                e.path(&index).to_vec(),
                Item {
                    mode: e.mode.bits(),
                    id: e.id,
                },
            );
        }
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
