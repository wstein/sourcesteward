//! Carry-over check (M4): may the review of an old pull request head be carried over to a rewritten head?
//!
//! Read-only: nothing is posted and no review state is read (`from` must be CLEAR; `stw` does not check that here).
//! The rule is the carry-over rule of the dotagents skill `agent-workflow`; all three conditions must hold:
//! 1. equal patch-id of the cumulative diff `BASE...HEAD`, old and new (each against its own base);
//! 2. no merge commit in `NEW_BASE..NEW_HEAD`;
//! 3. `git range-diff OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD` shows `=` for every commit, at the same position.
//!
//! A step that could not run is `unknown` and the answer is no: a read failure never becomes a yes. Only for a yes
//! the output ends with the exact `Carried-over:` line (the grammar of `review.rs`, 40-hex patch-id, `pr=<n|none>`).
//!
//! Git boundary: patch-id and merge commits are read in process (gix); `range-diff` and the fetch of the pull
//! request head (`--fetch`, `origin` only) go through the installed git (`git_runner`, `"via": "git"`).
//! Ported from `legacy/sourcesteward/internal/carryover`; deviations from the Go behavior are listed in the header of
//! `tests/it/carryover.rs`.
use std::path::{Path, PathBuf};

use serde_json::{json, Value};

use crate::git_runner::{self, GitError, Hex40, Op, Runner, PULL_HEAD_NAMESPACE};
use crate::patch_id;
use crate::review::PrRef;

/// `schema_version` of the JSON document (Go: 1; this one has `pr` as a number or `"none"` and a `via` per step).
pub const SCHEMA_VERSION: u32 = 2;
/// Bounds the listed range-diff rows that are not `=`.
pub const MAX_DEVIATIONS: usize = 200;
/// Commits per side that are compared (a higher position in the range-diff rows is a read failure).
pub const MAX_COMMITS: usize = 500;
/// Commits visited when looking for merge commits (a longer range is a read failure).
pub const WALK_LIMIT: usize = 5000;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum State {
    Equal,
    Differs,
    Unknown,
    None,
    Found,
    Match,
    Mismatch,
    NotChecked,
}

impl State {
    pub fn label(self) -> &'static str {
        match self {
            State::Equal => "equal",
            State::Differs => "differs",
            State::Unknown => "unknown",
            State::None => "none",
            State::Found => "found",
            State::Match => "match",
            State::Mismatch => "mismatch",
            State::NotChecked => "not-checked",
        }
    }
}

/// Why a carry-over is refused; every reason that applies is listed.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Reason {
    ObjectMissing,
    ReadFailed,
    PatchIdDiffers,
    PatchIdEmpty,
    MergeCommits,
    RangeDiffNotEqual,
    RangeEmpty,
    HeadMismatch,
}

impl Reason {
    pub fn label(self) -> &'static str {
        match self {
            Reason::ObjectMissing => "object-missing",
            Reason::ReadFailed => "read-failed",
            Reason::PatchIdDiffers => "patch-id-differs",
            Reason::PatchIdEmpty => "patch-id-empty",
            Reason::MergeCommits => "merge-commits",
            Reason::RangeDiffNotEqual => "range-diff-not-equal",
            Reason::RangeEmpty => "range-empty",
            Reason::HeadMismatch => "pr-head-mismatch",
        }
    }
}

/// A validated command line.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Request {
    pub pr: PrRef,
    pub old_base: String,
    pub old_head: String,
    pub new_base: String,
    pub new_head: String,
    /// Fetch the head of the pull request from `origin` into the private namespace and require it to equal `new_head`.
    pub fetch: bool,
}

/// An invalid command line (exit 2).
#[derive(Debug, PartialEq, Eq)]
pub struct UsageError(pub String);

impl std::fmt::Display for UsageError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

/// Validate the four positional arguments (full lower-case 40-hex commit ids: a ref name, a leading dash, `..` or a
/// space never reaches an argument vector) and `--pr`: a number from 1 without leading zeros, or `none` for a
/// local review. Go takes 1 to 999999999 and no `none`.
pub fn parse_request(
    args: &[String],
    pr: Option<&str>,
    fetch: bool,
) -> Result<Request, UsageError> {
    if args.len() != 4 {
        return Err(UsageError(
            "want OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD, four full 40-hex commit ids".into(),
        ));
    }
    for (name, a) in ["OLD_BASE", "OLD_HEAD", "NEW_BASE", "NEW_HEAD"]
        .iter()
        .zip(args)
    {
        if Hex40::parse(a).is_none() {
            return Err(UsageError(format!(
                "{name} must be a full 40-hex commit id in lowercase"
            )));
        }
    }
    let pr = match pr {
        Some(p) => parse_pr(p)?,
        None => {
            return Err(UsageError(
                "--pr NUMBER|none is required (a number from 1, or none for a local review)".into(),
            ))
        }
    };
    if fetch && pr == PrRef::None {
        return Err(UsageError(
            "--fetch needs a pull request number (--pr none has no pull request head)".into(),
        ));
    }
    Ok(Request {
        pr,
        old_base: args[0].clone(),
        old_head: args[1].clone(),
        new_base: args[2].clone(),
        new_head: args[3].clone(),
        fetch,
    })
}

/// A pull request number from 1 without leading zeros, or `none` for a local review. Shared with `stw prove`.
pub fn parse_pr(text: &str) -> Result<PrRef, UsageError> {
    match text {
        "none" => Ok(PrRef::None),
        n if !n.is_empty()
            && !n.starts_with('0')
            && n.bytes().all(|b| b.is_ascii_digit())
            && n.parse::<u64>().is_ok() =>
        {
            Ok(PrRef::Number(n.parse().expect("checked")))
        }
        _ => Err(UsageError(
            "--pr NUMBER|none is required (a number from 1, or none for a local review)".into(),
        )),
    }
}

/// One row of `git range-diff --no-patch`: positions (1-based) and the mark.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Row {
    pub old: Option<u32>,
    pub mark: char,
    pub new: Option<u32>,
}

/// A range-diff row that is not `=` at the same position. `mark` is `!` also for an equal commit at another
/// position, `<` for a commit only in the old range, `>` for one only in the new range.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Deviation {
    pub old: Option<u32>,
    pub new: Option<u32>,
    pub mark: char,
}

/// Parse the output of `git range-diff --no-color --no-patch`. Every line is a row
/// (`1:  abc1234 = 1:  def5678 subject`, `-:  ------- > 2:  def5678 subject`); anything else is an error.
pub fn parse_range_diff(out: &str) -> Result<Vec<Row>, String> {
    let mut rows = Vec::new();
    for line in out.lines().filter(|l| !l.trim().is_empty()) {
        let bad = || {
            format!(
                "unexpected range-diff line: {}",
                line.chars().take(80).collect::<String>()
            )
        };
        let t: Vec<&str> = line.split_whitespace().collect();
        if t.len() < 5 {
            return Err(bad());
        }
        let pos = |s: &str| -> Result<Option<u32>, String> {
            let n = s.strip_suffix(':').ok_or_else(bad)?;
            if n == "-" {
                return Ok(None);
            }
            if n.is_empty() || n.starts_with('0') || !n.bytes().all(|b| b.is_ascii_digit()) {
                return Err(bad());
            }
            n.parse().map(Some).map_err(|_| bad())
        };
        let (old, new) = (pos(t[0])?, pos(t[3])?);
        let mark = match t[2] {
            "=" => '=',
            "!" => '!',
            "<" => '<',
            ">" => '>',
            _ => return Err(bad()),
        };
        let shape_ok = match mark {
            '=' | '!' => old.is_some() && new.is_some(),
            '<' => old.is_some() && new.is_none(),
            _ => old.is_none() && new.is_some(),
        };
        if !shape_ok {
            return Err(bad());
        }
        rows.push(Row { old, mark, new });
    }
    if rows
        .iter()
        .any(|r| r.old.max(r.new).is_some_and(|n| n as usize > MAX_COMMITS))
    {
        return Err(format!("more than {MAX_COMMITS} commits per side"));
    }
    Ok(rows)
}

/// Why a step of the check could not run.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ReadError {
    pub step: &'static str,
    pub via: &'static str,
    /// `GitError::kind()` for a git step, `gix` for an in-process one.
    pub kind: String,
    /// The step cannot run on this machine (no git, too old, refused configuration).
    pub unsupported: bool,
    pub message: String,
}

impl ReadError {
    fn gix(step: &'static str, message: impl ToString) -> ReadError {
        ReadError {
            step,
            via: "gix",
            kind: "gix".into(),
            unsupported: false,
            message: message.to_string(),
        }
    }
    fn git(step: &'static str, e: &GitError) -> ReadError {
        ReadError {
            step,
            via: git_runner::VIA,
            kind: e.kind().into(),
            unsupported: e.is_unsupported(),
            message: e.to_string(),
        }
    }
}

/// The reads of the check. Real implementation: `RepoGit`.
pub trait Git {
    /// Fetch the head of pull request `pr` into the private namespace and return its commit id.
    fn fetch_pull(&self, pr: u64) -> Result<String, ReadError>;
    /// Whether `id` names a commit that exists.
    fn has_commit(&self, id: &str) -> Result<bool, ReadError>;
    /// Patch-id of `base...head`; empty when the diff is empty.
    fn patch_id(&self, base: &str, head: &str) -> Result<String, ReadError>;
    /// Merge commits of `base..head`.
    fn merges(&self, base: &str, head: &str) -> Result<Vec<String>, ReadError>;
    /// Rows of `git range-diff old_base old_head new_base new_head`.
    fn range_rows(&self, r: &Request) -> Result<Vec<Row>, ReadError>;
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct PatchIdCheck {
    pub state: State,
    pub old: String,
    pub new: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct MergeCheck {
    pub state: State,
    pub commits: Vec<String>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RangeCheck {
    pub state: State,
    pub total: usize,
    pub not_equal: usize,
    pub deviations: Vec<Deviation>,
    pub truncated: bool,
}

/// The result: the source of the JSON and the human output.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CarryOver {
    pub pr: PrRef,
    pub old_base: String,
    pub old_head: String,
    pub new_base: String,
    pub new_head: String,
    pub patch_id: PatchIdCheck,
    pub merge_commits: MergeCheck,
    pub range_diff: RangeCheck,
    pub pr_head: State,
    pub carry_over: bool,
    pub reasons: Vec<Reason>,
    pub read_errors: Vec<ReadError>,
    /// The `Carried-over:` line, only for a yes.
    pub line: Option<String>,
}

/// The `Carried-over:` line. Parses with `review::parse_strict`.
pub fn line(from: &str, to: &str, patch_id: &str, pr: PrRef) -> String {
    let pr = match pr {
        PrRef::Number(n) => n.to_string(),
        PrRef::None => "none".into(),
    };
    format!("Carried-over: from={from} to={to} patch-id={patch_id} pr={pr}")
}

/// Run the three conditions and the optional comparison of the pull request head.
pub fn check(g: &dyn Git, req: &Request) -> CarryOver {
    let mut res = CarryOver {
        pr: req.pr,
        old_base: req.old_base.clone(),
        old_head: req.old_head.clone(),
        new_base: req.new_base.clone(),
        new_head: req.new_head.clone(),
        patch_id: PatchIdCheck {
            state: State::Unknown,
            old: String::new(),
            new: String::new(),
        },
        merge_commits: MergeCheck {
            state: State::Unknown,
            commits: vec![],
        },
        range_diff: RangeCheck {
            state: State::Unknown,
            total: 0,
            not_equal: 0,
            deviations: vec![],
            truncated: false,
        },
        pr_head: State::NotChecked,
        carry_over: false,
        reasons: vec![],
        read_errors: vec![],
        line: None,
    };
    if req.fetch {
        match req.pr {
            PrRef::Number(n) => match g.fetch_pull(n) {
                Err(e) => {
                    res.pr_head = State::Unknown;
                    res.fail(Reason::ReadFailed, Some(e));
                }
                Ok(tip) if tip == req.new_head => res.pr_head = State::Match,
                Ok(_) => {
                    res.pr_head = State::Mismatch;
                    res.fail(Reason::HeadMismatch, None);
                }
            },
            PrRef::None => {
                res.pr_head = State::Unknown;
                res.fail(Reason::ReadFailed, None);
            }
        }
    }
    for id in [&req.old_base, &req.old_head, &req.new_base, &req.new_head] {
        match g.has_commit(id) {
            Err(e) => {
                res.fail(Reason::ReadFailed, Some(e));
                return res.finish();
            }
            Ok(false) => {
                res.fail(Reason::ObjectMissing, None);
                return res.finish();
            }
            Ok(true) => {}
        }
    }
    res.check_patch_id(g, req);
    res.check_merges(g, req);
    res.check_range(g, req);
    res.finish()
}

impl CarryOver {
    fn fail(&mut self, reason: Reason, err: Option<ReadError>) {
        if !self.reasons.contains(&reason) {
            self.reasons.push(reason);
        }
        self.read_errors.extend(err);
    }

    fn finish(mut self) -> CarryOver {
        self.carry_over = self.reasons.is_empty()
            && self.patch_id.state == State::Equal
            && self.merge_commits.state == State::None
            && self.range_diff.state == State::Equal
            && !matches!(self.pr_head, State::Mismatch | State::Unknown);
        if self.carry_over {
            self.line = Some(line(
                &self.old_head,
                &self.new_head,
                &self.patch_id.new,
                self.pr,
            ));
        }
        self
    }

    fn check_patch_id(&mut self, g: &dyn Git, req: &Request) {
        let old = g.patch_id(&req.old_base, &req.old_head);
        let new = g.patch_id(&req.new_base, &req.new_head);
        let (old, new) = match (old, new) {
            (Ok(o), Ok(n)) => (o, n),
            (o, n) => {
                self.fail(Reason::ReadFailed, None);
                self.read_errors.extend(o.err());
                self.read_errors.extend(n.err());
                return;
            }
        };
        self.patch_id.old = old.clone();
        self.patch_id.new = new.clone();
        if old.is_empty() || new.is_empty() {
            self.fail(Reason::PatchIdEmpty, None);
        } else if old != new {
            self.patch_id.state = State::Differs;
            self.fail(Reason::PatchIdDiffers, None);
        } else {
            self.patch_id.state = State::Equal;
        }
    }

    fn check_merges(&mut self, g: &dyn Git, req: &Request) {
        match g.merges(&req.new_base, &req.new_head) {
            Err(e) => self.fail(Reason::ReadFailed, Some(e)),
            Ok(m) if m.is_empty() => self.merge_commits.state = State::None,
            Ok(m) => {
                self.merge_commits = MergeCheck {
                    state: State::Found,
                    commits: m,
                };
                self.fail(Reason::MergeCommits, None);
            }
        }
    }

    fn check_range(&mut self, g: &dyn Git, req: &Request) {
        let rows = match g.range_rows(req) {
            Ok(r) => r,
            Err(e) => {
                self.fail(Reason::ReadFailed, Some(e));
                return;
            }
        };
        if rows.is_empty() {
            self.range_diff.state = State::Differs;
            self.fail(Reason::RangeEmpty, None);
            return;
        }
        self.range_diff.total = rows.len();
        for r in &rows {
            if r.mark == '=' && r.old == r.new {
                continue;
            }
            let mark = if r.mark == '=' { '!' } else { r.mark };
            self.range_diff.not_equal += 1;
            if self.range_diff.deviations.len() < MAX_DEVIATIONS {
                self.range_diff.deviations.push(Deviation {
                    old: r.old,
                    new: r.new,
                    mark,
                });
            } else {
                self.range_diff.truncated = true;
            }
        }
        if self.range_diff.not_equal > 0 {
            self.range_diff.state = State::Differs;
            self.fail(Reason::RangeDiffNotEqual, None);
        } else {
            self.range_diff.state = State::Equal;
        }
    }

    pub fn to_json(&self) -> Value {
        let pr = match self.pr {
            PrRef::Number(n) => json!(n),
            PrRef::None => json!("none"),
        };
        let pos = |p: Option<u32>| p.map_or(Value::Null, |n| json!(n));
        json!({
            "schema_version": SCHEMA_VERSION,
            "command": "review carry-over",
            "pr": pr,
            "from": {"base": self.old_base, "head": self.old_head},
            "to": {"base": self.new_base, "head": self.new_head},
            "patch_id": {"state": self.patch_id.state.label(), "old": self.patch_id.old,
                "new": self.patch_id.new, "via": "gix"},
            "merge_commits": {"state": self.merge_commits.state.label(),
                "count": self.merge_commits.commits.len(), "commits": self.merge_commits.commits,
                "via": "gix"},
            "range_diff": {"state": self.range_diff.state.label(), "total": self.range_diff.total,
                "not_equal": self.range_diff.not_equal,
                "deviations": self.range_diff.deviations.iter()
                    .map(|d| json!({"old": pos(d.old), "new": pos(d.new), "mark": d.mark.to_string()}))
                    .collect::<Vec<_>>(),
                "truncated": self.range_diff.truncated, "via": git_runner::VIA},
            "pr_head": {"state": self.pr_head.label(), "via": git_runner::VIA},
            "carry_over": self.carry_over,
            "reasons": self.reasons.iter().map(|r| r.label()).collect::<Vec<_>>(),
            "read_errors": self.read_errors.iter().map(|e| json!({
                "step": e.step, "via": e.via, "kind": e.kind, "unsupported": e.unsupported,
                "message": e.message})).collect::<Vec<_>>(),
            "line": self.line,
        })
    }

    /// Per condition, the verdict and, only for a yes, the line to post as the last line.
    pub fn render_human(&self) -> String {
        use std::fmt::Write;
        let mark = |n: Option<u32>| n.map_or("-".to_string(), |n| n.to_string());
        let or_none = |s: &str| {
            if s.is_empty() {
                "none".to_string()
            } else {
                s.to_string()
            }
        };
        let pr = match self.pr {
            PrRef::Number(n) => format!("#{n}"),
            PrRef::None => "(local review)".into(),
        };
        let mut b = String::new();
        let _ = writeln!(b, "stw review carry-over, pull request {pr}");
        let _ = writeln!(b, "from  base {}  head {}", self.old_base, self.old_head);
        let _ = writeln!(b, "to    base {}  head {}", self.new_base, self.new_head);
        let _ = writeln!(
            b,
            "1 patch-id      {:<8} old {} new {}",
            self.patch_id.state.label(),
            or_none(&self.patch_id.old),
            or_none(&self.patch_id.new)
        );
        let _ = writeln!(
            b,
            "2 merge commits {:<8} {}",
            self.merge_commits.state.label(),
            self.merge_commits.commits.len()
        );
        let _ = writeln!(
            b,
            "3 range-diff    {:<8} {} commits, {} not \"=\" or at another position (via {})",
            self.range_diff.state.label(),
            self.range_diff.total,
            self.range_diff.not_equal,
            git_runner::VIA
        );
        for d in &self.range_diff.deviations {
            let _ = writeln!(b, "    {} {} {}", mark(d.old), d.mark, mark(d.new));
        }
        if self.range_diff.truncated {
            let _ = writeln!(b, "    (more rows not listed)");
        }
        let _ = writeln!(
            b,
            "  pr head       {} (via {})",
            self.pr_head.label(),
            git_runner::VIA
        );
        for e in &self.read_errors {
            let _ = writeln!(
                b,
                "  read error    {} via {}: {}{}",
                e.step,
                e.via,
                e.message,
                if e.unsupported {
                    " (unsupported here)"
                } else {
                    ""
                }
            );
        }
        match &self.line {
            Some(l) if self.carry_over => {
                let _ = writeln!(b, "carry-over: yes\n{l}");
            }
            _ => {
                let why: Vec<&str> = self.reasons.iter().map(|r| r.label()).collect();
                let _ = writeln!(
                    b,
                    "carry-over: no ({})\nDelta check instead of a carry-over.",
                    why.join(", ")
                );
            }
        }
        b
    }
}

/// The checkout the check reads: objects through gix, `range-diff` and the fetch through the runner.
pub struct RepoGit<'a> {
    dir: PathBuf,
    runner: &'a Runner,
}

impl<'a> RepoGit<'a> {
    /// `dir` is the root of a repository or worktree (absolute). The runner refuses an empty or relative path.
    pub fn new(dir: &Path, runner: &'a Runner) -> RepoGit<'a> {
        RepoGit {
            dir: dir.to_path_buf(),
            runner,
        }
    }

    /// Fresh handle each time: a fetch between two reads is seen.
    fn open(&self, step: &'static str) -> Result<gix::Repository, ReadError> {
        crate::repo::open_isolated(&self.dir).map_err(|e| ReadError::gix(step, e))
    }
}

fn oid(step: &'static str, s: &str) -> Result<gix::ObjectId, ReadError> {
    gix::ObjectId::from_hex(s.as_bytes()).map_err(|e| ReadError::gix(step, e))
}

impl Git for RepoGit<'_> {
    fn fetch_pull(&self, pr: u64) -> Result<String, ReadError> {
        self.runner
            .run(&self.dir, Op::FetchPullHead { pr })
            .map_err(|e| ReadError::git("fetch-pr-head", &e))?;
        let repo = self.open("fetch-pr-head")?;
        let name = format!("{PULL_HEAD_NAMESPACE}pr-{pr}");
        let mut r = repo
            .find_reference(name.as_str())
            .map_err(|e| ReadError::gix("fetch-pr-head", e))?;
        let id = r
            .peel_to_id()
            .map_err(|e| ReadError::gix("fetch-pr-head", e))?;
        Ok(id.to_string())
    }

    fn has_commit(&self, id: &str) -> Result<bool, ReadError> {
        let repo = self.open("objects")?;
        let found = repo
            .try_find_object(oid("objects", id)?)
            .map_err(|e| ReadError::gix("objects", e))?;
        Ok(found.is_some_and(|o| o.kind == gix::object::Kind::Commit))
    }

    fn patch_id(&self, base: &str, head: &str) -> Result<String, ReadError> {
        let step = "patch-id";
        let repo = self.open(step)?;
        let (base, head) = (oid(step, base)?, oid(step, head)?);
        // `git diff base...head`: from the merge base to the head
        let mb = repo
            .merge_base(base, head)
            .map_err(|e| ReadError::gix(step, e))?;
        let tree = |id: gix::ObjectId| -> Result<gix::Tree<'_>, ReadError> {
            repo.find_commit(id)
                .map_err(|e| ReadError::gix(step, e))?
                .tree()
                .map_err(|e| ReadError::gix(step, e))
        };
        let (old_tree, new_tree) = (
            tree(
                mb.ok_or_else(|| ReadError::gix(step, "no merge base"))?
                    .detach(),
            )?,
            tree(head)?,
        );
        if old_tree.id == new_tree.id {
            return Ok(String::new());
        }
        patch_id::tree_diff_patch_id(&repo, &old_tree, &new_tree)
            .map(|id| id.to_string())
            .map_err(|e| ReadError::gix(step, e))
    }

    fn merges(&self, base: &str, head: &str) -> Result<Vec<String>, ReadError> {
        let step = "merge-commits";
        let repo = self.open(step)?;
        let walk = repo
            .rev_walk([oid(step, head)?])
            .with_hidden([oid(step, base)?])
            .all()
            .map_err(|e| ReadError::gix(step, e))?;
        let mut merges = Vec::new();
        for (n, info) in walk.enumerate() {
            if n >= WALK_LIMIT {
                return Err(ReadError::gix(
                    step,
                    format!("more than {WALK_LIMIT} commits in the range"),
                ));
            }
            let info = info.map_err(|e| ReadError::gix(step, e))?;
            if info.parent_ids.len() > 1 {
                merges.push(info.id.to_string());
            }
        }
        Ok(merges)
    }

    fn range_rows(&self, r: &Request) -> Result<Vec<Row>, ReadError> {
        let step = "range-diff";
        let h =
            |s: &str| Hex40::parse(s).ok_or_else(|| ReadError::gix(step, "not a 40-hex commit id"));
        let out = self
            .runner
            .run(
                &self.dir,
                Op::RangeDiff {
                    old_base: h(&r.old_base)?,
                    old_head: h(&r.old_head)?,
                    new_base: h(&r.new_base)?,
                    new_head: h(&r.new_head)?,
                },
            )
            .map_err(|e| ReadError::git(step, &e))?;
        parse_range_diff(&String::from_utf8_lossy(&out.stdout)).map_err(|m| ReadError {
            step,
            via: git_runner::VIA,
            kind: "output".into(),
            unsupported: false,
            message: m,
        })
    }
}
