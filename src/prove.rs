//! `stw prove ALT NEU [--base BASIS]` (new behavior, no Go counterpart): proves locally that a rewritten branch (`NEU`)
//! is the old branch (`ALT`) moved by a rebase and nothing else. No pull request, no network, nothing is written.
//!
//! The three conditions of `stw review carry-over` are not reimplemented: this module builds a `carryover::Request`
//! from the resolved revisions and runs `carryover::check` (equal patch-id of `BASE...HEAD`, no merge commit in the
//! new range, `git range-diff` shows `=` for every commit at the same position). Four checks are added here:
//! 4. the same set of changed paths (name-only, no rename detection, on both sides the same way);
//! 5. author and committer of every new commit are the `user.name <user.email>` of the Git configuration (read only),
//!    and every `Co-Authored-By` trailer line of an old commit is still in the new commit at the same position;
//! 6. no session link in the text of a new commit (the rule of `lint pr`, `lint_pr::has_session_link`).
//!
//! Git boundary: everything is read through gix (revisions, merge base, commit walk, trees, patch-id in process via
//! `patch_id.rs`, identity from the configuration); the only `git` call is `range-diff` (`Op::RangeDiff`, `via: git`),
//! which gix does not have. A step that cannot run is `unknown` and the verdict is `not proved`. Only for `proved`
//! the output ends with the exact `Carried-over:` line.
//!
//! Bases: the old base is `merge-base(ALT, NEU)`. Without `--base` the new base is the same commit (a rewrite on the
//! same base: amend, reorder, squash, edit); with `--base BASIS` it is `BASIS`, which must be an ancestor of `NEU`
//! (a rebase onto `BASIS`). `ALT == NEU` proves nothing and is refused.
use std::collections::BTreeSet;
use std::path::Path;

use gix::bstr::{BStr, ByteSlice};
use gix::ObjectId;
use serde_json::{json, Value};

use crate::carryover::{self, CarryOver, Reason, State, UsageError};
use crate::git_runner::Runner;
use crate::github::reads::escape_untrusted;
use crate::review::PrRef;
use crate::{lint_pr, replay, repo};

/// `schema_version` of the JSON document.
pub const SCHEMA_VERSION: u32 = 2;
/// Commits per side that are examined (more is `unknown`, as for carry-over).
pub const MAX_COMMITS: usize = carryover::MAX_COMMITS;
/// Changed paths listed per side of the path comparison.
const MAX_LISTED: usize = 50;

/// A validated command line.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Request {
    pub old_rev: String,
    pub new_rev: String,
    pub base_rev: Option<String>,
    /// The old parent of ALT (`--old-parent`), the old base; `None`: the merge base of ALT and NEU.
    pub old_parent_rev: Option<String>,
    pub pr: PrRef,
}

fn check_rev(name: &str, rev: &str) -> Result<(), UsageError> {
    if rev.is_empty() {
        return Err(UsageError(format!("{name} must not be empty")));
    }
    if rev.starts_with('-') {
        return Err(UsageError(format!("{name} must not start with a dash")));
    }
    if rev.len() > 255 || rev.chars().any(char::is_control) {
        return Err(UsageError(format!(
            "{name} is too long or holds a control character"
        )));
    }
    if rev.contains("..") {
        return Err(UsageError(format!(
            "{name} must be one revision, not a range (use --base for the base)"
        )));
    }
    Ok(())
}

/// Validate `ALT NEU`, `--base` and `--pr` (a number from 1, or `none`; default `none`, a local proof).
pub fn parse_request(
    args: &[String],
    base: Option<&str>,
    old_parent: Option<&str>,
    pr: Option<&str>,
) -> Result<Request, UsageError> {
    let [old, new] = args else {
        return Err(UsageError(
            "want ALT NEU: the old branch or head and the new one".into(),
        ));
    };
    check_rev("ALT", old)?;
    check_rev("NEU", new)?;
    if let Some(b) = base {
        check_rev("--base", b)?;
    }
    if let Some(b) = old_parent {
        check_rev("--old-parent", b)?;
    }
    let pr = match pr {
        Some(p) => carryover::parse_pr(p)?,
        None => PrRef::None,
    };
    Ok(Request {
        old_rev: old.clone(),
        new_rev: new.clone(),
        base_rev: base.map(str::to_string),
        old_parent_rev: old_parent.map(str::to_string),
        pr,
    })
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CheckState {
    Ok,
    Failed,
    Unknown,
    /// Not asked because an earlier check already failed.
    NotRun,
}

impl CheckState {
    pub fn label(self) -> &'static str {
        match self {
            CheckState::Ok => "ok",
            CheckState::Failed => "failed",
            CheckState::Unknown => "unknown",
            CheckState::NotRun => "not-run",
        }
    }
}

/// One check: the verdict, why, and the items behind it (all text already escaped).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Check {
    pub id: &'static str,
    pub via: &'static str,
    pub state: CheckState,
    /// Reason labels when not `ok`.
    pub reasons: Vec<&'static str>,
    pub detail: String,
    pub items: Vec<String>,
}

impl Check {
    fn new(id: &'static str, via: &'static str) -> Check {
        Check {
            id,
            via,
            state: CheckState::Unknown,
            reasons: vec![],
            detail: String::new(),
            items: vec![],
        }
    }
    fn ok(mut self, detail: impl Into<String>) -> Check {
        self.state = CheckState::Ok;
        self.detail = detail.into();
        self
    }
    fn fail(mut self, reason: &'static str, detail: impl Into<String>) -> Check {
        self.state = CheckState::Failed;
        self.reasons.push(reason);
        self.detail = detail.into();
        self
    }
    fn unknown(mut self, reason: &'static str, detail: impl Into<String>) -> Check {
        self.state = CheckState::Unknown;
        self.reasons.push(reason);
        self.detail = detail.into();
        self
    }
}

/// The resolved ends of the proof.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Ends {
    pub old_head: String,
    pub new_head: String,
    pub old_base: String,
    pub new_base: String,
    /// `merge-base` or `flag`: where the new base came from.
    pub base_source: &'static str,
    /// `merge-base` or `flag` (`--old-parent`): where the old base came from.
    pub old_base_source: &'static str,
}

/// The result: the source of the JSON and the text.
#[derive(Clone, Debug)]
pub struct Proof {
    pub req: Request,
    pub ends: Option<Ends>,
    /// Problems before any check could run (unknown revision, no merge base, same commit, base not below NEU).
    pub input: Vec<Check>,
    /// Range-diff, patch-id, merge commits, files, identity, trailers, session links; empty when `input` failed.
    pub checks: Vec<Check>,
    pub proved: bool,
    pub reasons: Vec<&'static str>,
    pub line: Option<String>,
    /// The `--base` hint for a rebase onto a new base.
    pub hint: Option<String>,
}

fn esc(s: &str) -> String {
    escape_untrusted(s, 200).0
}

fn lossy(b: &BStr) -> String {
    b.to_str_lossy().into_owned()
}

/// Resolve one revision to a commit id (peeled), in process.
fn resolve(repo: &gix::Repository, rev: &str) -> Result<ObjectId, String> {
    let id = repo.rev_parse_single(rev).map_err(|e| e.to_string())?;
    let commit = id
        .object()
        .map_err(|e| e.to_string())?
        .peel_to_commit()
        .map_err(|_| "is not a commit".to_string())?;
    Ok(commit.id)
}

/// The commits of `base..head`, oldest first (a linear range is the only one that pairs by position).
fn commits(
    repo: &gix::Repository,
    base: ObjectId,
    head: ObjectId,
) -> Result<Vec<ObjectId>, String> {
    let walk = repo
        .rev_walk([head])
        .with_hidden([base])
        .sorting(gix::revision::walk::Sorting::ByCommitTime(
            Default::default(),
        ))
        .all()
        .map_err(|e| e.to_string())?;
    let mut out = Vec::new();
    for info in walk {
        if out.len() >= MAX_COMMITS {
            return Err(format!("more than {MAX_COMMITS} commits in the range"));
        }
        out.push(info.map_err(|e| e.to_string())?.id);
    }
    out.reverse();
    Ok(out)
}

/// The paths changed by `base...head` (from the merge base to the head), no rename detection.
fn changed_paths(
    repo: &gix::Repository,
    base: ObjectId,
    head: ObjectId,
) -> Result<BTreeSet<String>, String> {
    let mb = repo
        .merge_base(base, head)
        .map_err(|e| e.to_string())?
        .ok_or("no common history")?
        .detach();
    let tree = |id: ObjectId| -> Result<gix::Tree<'_>, String> {
        repo.find_commit(id)
            .map_err(|e| e.to_string())?
            .tree()
            .map_err(|e| e.to_string())
    };
    let (a, b) = (tree(mb)?, tree(head)?);
    let options = gix::diff::Options::default().with_rewrites(None);
    let changes = repo
        .diff_tree_to_tree(&a, &b, options)
        .map_err(|e| e.to_string())?;
    Ok(changes
        .iter()
        .map(|c| lossy(c.location()))
        .collect::<BTreeSet<_>>())
}

struct Msg {
    id: ObjectId,
    parents: usize,
    subject: String,
    message: String,
    author: (String, String),
    committer: (String, String),
}

fn read_commit(repo: &gix::Repository, id: ObjectId) -> Result<Msg, String> {
    let c = repo.find_commit(id).map_err(|e| e.to_string())?;
    let decoded = c.decode().map_err(|e| e.to_string())?;
    let message = decoded.message.to_str_lossy().into_owned();
    let sig = |s: gix::actor::SignatureRef<'_>| (lossy(s.name), lossy(s.email));
    Ok(Msg {
        id,
        parents: c.parent_ids().count(),
        subject: message.lines().next().unwrap_or("").to_string(),
        message,
        author: sig(decoded.author().map_err(|e| e.to_string())?),
        committer: sig(decoded.committer().map_err(|e| e.to_string())?),
    })
}

fn label(m: &Msg) -> String {
    format!("{} {}", m.id, esc(&m.subject))
}

fn same_person(want: &(String, String), got: &(String, String)) -> bool {
    want.0 == got.0 && lint_pr::fold(&want.1) == lint_pr::fold(&got.1)
}

fn files_check(repo: &gix::Repository, e: &Ends) -> Check {
    let c = Check::new("files", "gix");
    let both = changed_paths(repo, parse(&e.old_base), parse(&e.old_head))
        .and_then(|o| changed_paths(repo, parse(&e.new_base), parse(&e.new_head)).map(|n| (o, n)));
    let (old, new) = match both {
        Ok(x) => x,
        Err(m) => return c.unknown("read-failed", esc(&m)),
    };
    let only_old: Vec<&String> = old.difference(&new).collect();
    let only_new: Vec<&String> = new.difference(&old).collect();
    if only_old.is_empty() && only_new.is_empty() {
        return c.ok(format!("{} files on both sides", old.len()));
    }
    let mut c = c.fail(
        "files-differ",
        format!(
            "{} only in the old range, {} only in the new range",
            only_old.len(),
            only_new.len()
        ),
    );
    for (mark, list) in [('<', &only_old), ('>', &only_new)] {
        for p in list.iter().take(MAX_LISTED) {
            c.items.push(format!("{mark} {}", esc(p)));
        }
        if list.len() > MAX_LISTED {
            c.items.push(format!("{mark} (more paths not listed)"));
        }
    }
    c
}

fn parse(hex: &str) -> ObjectId {
    ObjectId::from_hex(hex.as_bytes()).expect("a full hex id")
}

fn identity_check(repo: &gix::Repository, new: &[Msg]) -> Check {
    let c = Check::new("identity", "gix");
    let Some((name, email)) = replay::configured_user(repo) else {
        return c.unknown(
            "identity-unknown",
            "no user.name and user.email in the Git configuration (stw never sets them)",
        );
    };
    let want = (lossy(name.as_ref()), lossy(email.as_ref()));
    let mut bad = Vec::new();
    for m in new {
        for (role, who) in [("author", &m.author), ("committer", &m.committer)] {
            if !same_person(&want, who) {
                bad.push(format!("{} {role} {} <{}>", m.id, esc(&who.0), esc(&who.1)));
            }
        }
    }
    if bad.is_empty() {
        return c.ok(format!(
            "{} commits, author and committer {} <{}>",
            new.len(),
            esc(&want.0),
            esc(&want.1)
        ));
    }
    let mut c = c.fail(
        "identity-mismatch",
        format!(
            "{} of {} identities differ from {} <{}>",
            bad.len(),
            new.len() * 2,
            esc(&want.0),
            esc(&want.1)
        ),
    );
    c.items = bad;
    c
}

fn trailer_check(old: &[Msg], new: &[Msg]) -> Check {
    let c = Check::new("trailers", "gix");
    // range-diff and the per-commit patch-ids leave merge commits out; so does the pairing by position. A
    // linearized NEU of an old range with merges can be proved, and the check says how many were left out.
    let old_merges = old.iter().filter(|m| m.parents > 1).count();
    let old: Vec<&Msg> = old.iter().filter(|m| m.parents <= 1).collect();
    let skipped = if old_merges == 0 {
        String::new()
    } else {
        format!("; the old range had {old_merges} merge commits, left out of the pairing")
    };
    let mut lost = Vec::new();
    let mut seen = 0usize;
    for (o, n) in old.iter().zip(new) {
        let have: Vec<String> = lint_pr::co_authored_lines(&n.message)
            .iter()
            .map(|l| l.trim_end().to_string())
            .collect();
        for line in lint_pr::co_authored_lines(&o.message) {
            seen += 1;
            if !have.contains(&line.trim_end().to_string()) {
                lost.push(format!("{} lost: {}", n.id, esc(line.trim_end())));
            }
        }
    }
    if lost.is_empty() {
        return c.ok(format!(
            "{seen} Co-Authored-By lines of the old commits are in the new commits{skipped}"
        ));
    }
    let mut c = c.fail(
        "trailer-missing",
        format!(
            "{} Co-Authored-By lines of the old commits are gone{skipped}",
            lost.len()
        ),
    );
    c.items = lost;
    c
}

fn session_check(new: &[Msg]) -> Check {
    let c = Check::new("session-links", "gix");
    let hits: Vec<String> = new
        .iter()
        .filter(|m| lint_pr::has_session_link(&m.message))
        .map(label)
        .collect();
    if hits.is_empty() {
        return c.ok(format!("none in {} commit texts", new.len()));
    }
    let mut c = c.fail(
        "session-link",
        format!("{} new commit texts hold a session link", hits.len()),
    );
    c.items = hits;
    c
}

/// A check of the shared carry-over machine, in the words of this command.
fn carry_checks(c: &CarryOver) -> Vec<Check> {
    let mut out = Vec::new();
    let reason_of = |r: Reason| c.reasons.contains(&r);

    let mut rd = Check::new("range-diff", crate::git_runner::VIA);
    rd.state = match c.range_diff.state {
        State::Equal => CheckState::Ok,
        State::Differs => CheckState::Failed,
        _ => CheckState::Unknown,
    };
    rd.detail = match c.range_diff.state {
        State::Equal => format!(
            "{} commits, every one \"=\" at the same position",
            c.range_diff.total
        ),
        State::Differs if reason_of(Reason::RangeEmpty) => "the range-diff lists no commit".into(),
        State::Differs => format!(
            "{} of {} commits are not \"=\" at the same position",
            c.range_diff.not_equal, c.range_diff.total
        ),
        _ => "the range-diff did not run".into(),
    };
    for r in [Reason::RangeDiffNotEqual, Reason::RangeEmpty] {
        if reason_of(r) {
            rd.reasons.push(r.label());
        }
    }
    let mark = |n: Option<u32>| n.map_or("-".to_string(), |n| n.to_string());
    for d in &c.range_diff.deviations {
        rd.items
            .push(format!("{} {} {}", mark(d.old), d.mark, mark(d.new)));
    }
    if c.range_diff.truncated {
        rd.items.push("(more rows not listed)".into());
    }
    out.push(rd);

    let mut cp = Check::new("commit-patches", "gix");
    cp.state = match c.commit_patches.state {
        State::Equal => CheckState::Ok,
        State::Differs => CheckState::Failed,
        State::NotChecked => CheckState::NotRun,
        _ => CheckState::Unknown,
    };
    cp.detail = match c.commit_patches.state {
        State::Equal => "the patch-id of every commit is equal, old against new".into(),
        State::Differs => format!(
            "{} commit positions differ in their patch-id (binary contents show only as \"Binary files differ\" in range-diff)",
            c.commit_patches.differing.len()
        ),
        State::NotChecked => "not asked: an earlier check failed".into(),
        _ => "the per-commit patch-ids could not be read".into(),
    };
    if reason_of(Reason::CommitPatchDiffers) {
        cp.reasons.push(Reason::CommitPatchDiffers.label());
    }
    cp.items = c
        .commit_patches
        .differing
        .iter()
        .map(|n| format!("position {n}"))
        .collect();
    out.push(cp);

    let mut pid = Check::new("patch-id", "gix");
    pid.state = match c.patch_id.state {
        State::Equal => CheckState::Ok,
        State::Differs => CheckState::Failed,
        _ => CheckState::Unknown,
    };
    pid.detail = match c.patch_id.state {
        State::Equal => c.patch_id.new.clone(),
        State::Differs => format!("old {} new {}", c.patch_id.old, c.patch_id.new),
        _ if reason_of(Reason::PatchIdEmpty) => "a diff is empty: there is no patch-id".into(),
        _ => "the patch-id could not be read".into(),
    };
    for r in [Reason::PatchIdDiffers, Reason::PatchIdEmpty] {
        if reason_of(r) {
            pid.reasons.push(r.label());
        }
    }
    out.push(pid);

    let mut mc = Check::new("merge-commits", "gix");
    mc.state = match c.merge_commits.state {
        State::None => CheckState::Ok,
        State::Found => CheckState::Failed,
        _ => CheckState::Unknown,
    };
    mc.detail = match c.merge_commits.state {
        State::None => "none in the new range".into(),
        State::Found => format!("{} in the new range", c.merge_commits.commits.len()),
        _ => "the new range could not be read".into(),
    };
    if reason_of(Reason::MergeCommits) {
        mc.reasons.push(Reason::MergeCommits.label());
    }
    mc.items = c.merge_commits.commits.clone();
    out.push(mc);

    // a read failure belongs to the step that ran
    for e in &c.read_errors {
        let target = match e.step {
            "range-diff" => "range-diff",
            "commit-patch-ids" => "commit-patches",
            "patch-id" => "patch-id",
            "merge-commits" => "merge-commits",
            _ => continue,
        };
        if let Some(ch) = out.iter_mut().find(|ch| ch.id == target) {
            if !ch.reasons.contains(&Reason::ReadFailed.label()) {
                ch.reasons.push(Reason::ReadFailed.label());
            }
            ch.items.push(format!(
                "read error via {}: {}{}",
                e.via,
                esc(&e.message),
                if e.unsupported {
                    " (unsupported here)"
                } else {
                    ""
                }
            ));
        }
    }
    out
}

fn input_problem(id: &'static str, reason: &'static str, detail: String) -> Check {
    Check::new(id, "gix").fail(reason, detail)
}

/// Run the proof in the repository at `dir` (the root of a repository or worktree, absolute).
pub fn prove(dir: &Path, runner: &Runner, req: &Request) -> Proof {
    let mut p = Proof {
        req: req.clone(),
        ends: None,
        input: vec![],
        checks: vec![],
        proved: false,
        reasons: vec![],
        line: None,
        hint: None,
    };
    let repo = match repo::open(dir) {
        Ok(r) => r,
        Err(e) => {
            p.input
                .push(Check::new("repository", "gix").unknown("read-failed", esc(&e.to_string())));
            return finish(p);
        }
    };
    let mut resolve_one = |what: &'static str, rev: &str| match resolve(&repo, rev) {
        Ok(id) => Some(id),
        Err(m) => {
            p.input.push(input_problem(
                "revision",
                "unknown-revision",
                format!("{what} {}: {}", esc(rev), esc(&m)),
            ));
            None
        }
    };
    let old = resolve_one("ALT", &req.old_rev);
    let new = resolve_one("NEU", &req.new_rev);
    let base = req.base_rev.as_deref().map(|b| resolve_one("--base", b));
    let old_parent = req
        .old_parent_rev
        .as_deref()
        .map(|b| resolve_one("--old-parent", b));
    let (Some(old), Some(new)) = (old, new) else {
        return finish(p);
    };
    if matches!(base, Some(None)) || matches!(old_parent, Some(None)) {
        return finish(p);
    }
    if old == new {
        p.input.push(input_problem(
            "revision",
            "same-commit",
            "ALT and NEU are the same commit: there is nothing to prove".into(),
        ));
        return finish(p);
    }
    let (old_base, old_source) = match old_parent {
        // the old parent of ALT: REV..ALT is the old range (a child branch whose parent was rebased)
        Some(Some(par)) => {
            match repo::is_ancestor(&repo, par, old) {
                Ok(true) if par == old => {
                    p.input.push(input_problem(
                        "revision",
                        "old-range-empty",
                        "--old-parent is ALT itself: the old range is empty".into(),
                    ));
                    return finish(p);
                }
                Ok(true) => {}
                Ok(false) => {
                    p.input.push(input_problem(
                        "revision",
                        "old-parent-not-ancestor",
                        "--old-parent is not an ancestor of ALT".into(),
                    ));
                    return finish(p);
                }
                Err(e) => {
                    p.input.push(
                        Check::new("revision", "gix").unknown("read-failed", esc(&e.to_string())),
                    );
                    return finish(p);
                }
            }
            (par, "flag")
        }
        _ => match repo.merge_base(old, new) {
            Ok(Some(id)) => (id.detach(), "merge-base"),
            Ok(None) | Err(_) => {
                p.input.push(input_problem(
                    "revision",
                    "no-merge-base",
                    "ALT and NEU have no common history".into(),
                ));
                return finish(p);
            }
        },
    };
    let (new_base, source) = match base {
        Some(Some(b)) => (b, "flag"),
        // no --base: the old base is the new base too; named by where it came from
        _ => (
            old_base,
            if old_source == "flag" {
                "old-parent"
            } else {
                "merge-base"
            },
        ),
    };
    match repo::is_ancestor(&repo, new_base, new) {
        Ok(true) => {}
        Ok(false) => {
            let mut why =
                String::from("the base is not an ancestor of NEU: NEU is not a rebase onto it");
            if source == "old-parent" {
                why.push_str(" (without --base the old parent is the new base too: name the new parent with --base)");
            }
            p.input
                .push(input_problem("revision", "base-not-below-new", why));
            return finish(p);
        }
        Err(e) => {
            p.input
                .push(Check::new("revision", "gix").unknown("read-failed", esc(&e.to_string())));
            return finish(p);
        }
    }
    // git range-diff needs two non-empty ranges; an empty one proves nothing
    for (rng, from, to) in [("old", old_base, old), ("new", new_base, new)] {
        if from == to {
            p.input.push(input_problem(
                "revision",
                "range-empty",
                format!("the {rng} range is empty: there is no commit to compare"),
            ));
            return finish(p);
        }
    }
    let ends = Ends {
        old_head: old.to_string(),
        new_head: new.to_string(),
        old_base: old_base.to_string(),
        new_base: new_base.to_string(),
        base_source: source,
        old_base_source: old_source,
    };
    // 1 to 3: the machine of `stw review carry-over`
    let creq = carryover::Request {
        pr: req.pr,
        old_base: ends.old_base.clone(),
        old_head: ends.old_head.clone(),
        new_base: ends.new_base.clone(),
        new_head: ends.new_head.clone(),
        fetch: false,
    };
    let carry = carryover::check(&carryover::RepoGit::new(dir, runner), &creq);
    p.checks.extend(carry_checks(&carry));
    // 4 to 6: in process
    p.checks.push(files_check(&repo, &ends));
    let read = |base: ObjectId, head: ObjectId| -> Result<Vec<Msg>, String> {
        commits(&repo, base, head)?
            .into_iter()
            .map(|id| read_commit(&repo, id))
            .collect()
    };
    match (read(old_base, old), read(new_base, new)) {
        (Ok(o), Ok(n)) => {
            p.checks.push(identity_check(&repo, &n));
            p.checks.push(trailer_check(&o, &n));
            p.checks.push(session_check(&n));
        }
        (a, b) => {
            let why = a.err().or(b.err()).unwrap_or_default();
            for id in ["identity", "trailers", "session-links"] {
                p.checks
                    .push(Check::new(id, "gix").unknown("read-failed", esc(&why)));
            }
        }
    }
    if (source == "merge-base" || old_source == "merge-base")
        && p.checks
            .iter()
            .any(|c| c.id == "range-diff" && c.state != CheckState::Ok)
    {
        p.hint = Some(
            "if NEU was rebased onto a new base, name it with --base BASE; if ALT sat on a parent that was rebased (git rebase --onto NEWPARENT OLDPARENT ALT), name the old parent with --old-parent OLDPARENT (without them the base is the merge base of ALT and NEU)"
                .into(),
        );
    }
    p.ends = Some(ends);
    finish(p)
}

fn finish(mut p: Proof) -> Proof {
    let mut reasons: Vec<&'static str> = Vec::new();
    for c in p.input.iter().chain(&p.checks) {
        if c.state != CheckState::Ok {
            if c.reasons.is_empty() && c.state != CheckState::NotRun {
                reasons.push("read-failed");
            }
            for r in &c.reasons {
                if !reasons.contains(r) {
                    reasons.push(r);
                }
            }
        }
    }
    p.proved = p.input.is_empty()
        && p.checks.len() == 8
        && p.checks.iter().all(|c| c.state == CheckState::Ok);
    p.reasons = reasons;
    if p.proved {
        if let (Some(e), Some(pid)) = (&p.ends, p.checks.iter().find(|c| c.id == "patch-id")) {
            p.line = Some(carryover::line(
                &e.old_head,
                &e.new_head,
                &pid.detail,
                p.req.pr,
            ));
        }
    }
    p
}

impl Proof {
    pub fn verdict(&self) -> &'static str {
        if self.proved {
            "proved"
        } else {
            "not proved"
        }
    }

    pub fn to_json(&self) -> Value {
        let pr = match self.req.pr {
            PrRef::Number(n) => json!(n),
            PrRef::None => json!("none"),
        };
        let check = |c: &Check| {
            json!({"id": c.id, "state": c.state.label(), "via": c.via, "reasons": c.reasons,
                "detail": c.detail, "items": c.items})
        };
        let ends = self.ends.as_ref().map_or(Value::Null, |e| {
            json!({"old_head": e.old_head, "new_head": e.new_head, "old_base": e.old_base,
                "new_base": e.new_base, "base_source": e.base_source,
                "old_base_source": e.old_base_source})
        });
        json!({
            "schema_version": SCHEMA_VERSION,
            "command": "prove",
            "verdict": self.verdict(),
            "proved": self.proved,
            "pr": pr,
            "request": {"old": esc(&self.req.old_rev), "new": esc(&self.req.new_rev),
                "base": self.req.base_rev.as_deref().map(esc),
                "old_parent": self.req.old_parent_rev.as_deref().map(esc)},
            "ends": ends,
            "input": self.input.iter().map(check).collect::<Vec<_>>(),
            "checks": self.checks.iter().map(check).collect::<Vec<_>>(),
            "reasons": self.reasons,
            "hint": self.hint,
            "line": self.line,
        })
    }

    /// The verdict first, then one line per check with its reason, then (only for `proved`) the `Carried-over:` line.
    pub fn render_human(&self) -> String {
        use std::fmt::Write;
        let mut b = String::new();
        let _ = writeln!(b, "{}", self.verdict());
        let _ = writeln!(
            b,
            "stw prove {} -> {}{}{}",
            esc(&self.req.old_rev),
            esc(&self.req.new_rev),
            self.req
                .old_parent_rev
                .as_deref()
                .map_or(String::new(), |r| format!(" --old-parent {}", esc(r))),
            self.req
                .base_rev
                .as_deref()
                .map_or(String::new(), |r| format!(" --base {}", esc(r)))
        );
        if let Some(e) = &self.ends {
            let _ = writeln!(
                b,
                "from  base {}  head {}  (base from {})",
                e.old_base, e.old_head, e.old_base_source
            );
            let _ = writeln!(
                b,
                "to    base {}  head {}  (base from {})",
                e.new_base, e.new_head, e.base_source
            );
        }
        for c in self.input.iter().chain(&self.checks) {
            let n = if c.id == "revision" || c.id == "repository" {
                String::new()
            } else {
                let order = [
                    "range-diff",
                    "commit-patches",
                    "patch-id",
                    "merge-commits",
                    "files",
                    "identity",
                    "trailers",
                    "session-links",
                ];
                order
                    .iter()
                    .position(|i| *i == c.id)
                    .map_or(String::new(), |i| format!("{} ", i + 1))
            };
            let why = if c.reasons.is_empty() {
                String::new()
            } else {
                format!(" [{}]", c.reasons.join(", "))
            };
            let _ = writeln!(
                b,
                "{n}{:<14} {:<8} {}{} (via {})",
                c.id,
                c.state.label(),
                c.detail,
                why,
                c.via
            );
            for i in &c.items {
                let _ = writeln!(b, "    {i}");
            }
        }
        if let Some(h) = &self.hint {
            let _ = writeln!(b, "hint: {h}");
        }
        if let Some(l) = &self.line {
            let _ = writeln!(b, "{l}");
        }
        b
    }
}
