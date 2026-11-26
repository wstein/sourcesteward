//! What the observer hands to the core of `stw watch` (port of `internal/watch/observer.go`): one snapshot per
//! repository and cycle, the hints the accepted state gives the observer, and the classified read failure. The texts
//! of a snapshot (titles, names, descriptions) are raw GitHub text; they are escaped where an event is made.
use std::collections::BTreeMap;

use super::cond_store::CondStore;
use super::http::Code;

/// Check classes of one PR head. The empty class means "not observed yet".
pub const CLASS_NONE: &str = "none";
pub const CLASS_PENDING: &str = "pending";
pub const CLASS_GREEN: &str = "green";
pub const CLASS_RED: &str = "red";

/// What the state makes necessary, so that the observer reads only when needed: details for PRs marked due, checks
/// for pending or unobserved PRs.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Hints {
    pub cycle: u64,
    pub detail_every: u64,
    /// The open PRs of the accepted state, ascending by number.
    pub known: Vec<KnownPr>,
}

/// The accepted view of one open PR.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct KnownPr {
    pub number: u64,
    pub head: String,
    /// `none`, `pending`, `green`, `red`, or empty when unobserved.
    pub class: String,
    /// A checks class seen once and not yet accepted (empty when none).
    pub class_candidate: String,
    pub detail_due: bool,
}

/// One snapshot of a repository.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct RepoObservation {
    pub branches: Option<Result<BTreeMap<String, String>, String>>,
    /// The PR list was used only for reviews, withheld after refusal, or not selected.
    pub skip_pr_events: bool,
    pub pr_read_error: Option<String>,
    pub issues: Option<crate::issue_list::Listed>,
    pub activity: Vec<(
        ActivityScope,
        Result<Vec<crate::watch::event::Activity>, String>,
    )>,
    pub workflows: Option<Result<Vec<crate::watch::event::WorkflowCompleted>, String>>,
    pub active_workflows: usize,
    pub default_branch: String,
    /// The complete open list.
    pub prs: Vec<PrObs>,
    /// `pulls/N` of the PRs that left the open list; absent when unreadable.
    pub gone: BTreeMap<u64, GoneObs>,
    /// The replacement store of validators; the caller keeps it for the next cycle.
    pub cond: Option<CondStore>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ActivityScope {
    Reviews(u64),
    Discussions,
    Wiki,
}

/// One open PR. `detail` and `checks` are `None` when this cycle did not read them: the accepted values are kept.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PrObs {
    pub number: u64,
    pub title: String,
    pub author: String,
    pub labels: Vec<String>,
    pub draft: bool,
    pub head: String,
    pub head_ref: String,
    pub base: String,
    /// The head repository differs from the base repository.
    pub fork: bool,
    /// True only when `pulls/N` was read in this observation; otherwise `fork` is no answer and the accepted value
    /// is kept.
    pub fork_known: bool,
    pub detail: Option<DetailObs>,
    pub checks: Option<ChecksObs>,
}

/// `pulls/N`: a `mergeable` of `None` is "not computed" and keeps the previous value.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct DetailObs {
    pub mergeable: Option<bool>,
    /// GitHub `mergeable_state`.
    pub state: String,
}

/// The bounded data of one check run: no log is ever read.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CheckRun {
    pub name: String,
    /// `queued`, `in_progress`, `completed`.
    pub status: String,
    pub conclusion: String,
    pub title: String,
    pub summary: String,
    pub text: String,
}

/// One commit status context.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct StatusContext {
    pub context: String,
    /// `pending`, `success`, `failure`, `error`.
    pub state: String,
    pub description: String,
}

/// Where the runs of a head came from.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum ChecksVia {
    #[default]
    CheckRuns,
    Graphql,
    /// The token cannot read check runs (403): the newest Actions run per workflow and event stands in, without any
    /// output text (new against Go, D11).
    ActionsRuns,
}

/// The raw check data of a PR head; the core classifies it.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ChecksObs {
    pub runs: Vec<CheckRun>,
    pub statuses: Vec<StatusContext>,
    pub via: ChecksVia,
}

/// How the PR was merged, from the parents of the merge commit.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum MethodHint {
    Merge,
    RebaseOrSquash,
    Unknown,
}

impl MethodHint {
    pub fn as_str(self) -> &'static str {
        match self {
            MethodHint::Merge => "merge",
            MethodHint::RebaseOrSquash => "rebase-or-squash",
            MethodHint::Unknown => "unknown",
        }
    }
}

/// `pulls/N` of a PR that left the open list.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct GoneObs {
    pub merged: bool,
    /// `None` when unreadable.
    pub merge_commit: Option<String>,
    pub merged_at: String,
    pub base: String,
    pub head: String,
    pub head_ref: String,
    pub method_hint: MethodHint,
}

/// A classified read failure. `code` is one of the watch-error codes; run-wide failures (a rate limit) belong to the
/// run, not to the repository.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ObserveError {
    pub code: Code,
    pub message: String,
    pub run_wide: bool,
    /// Seconds to wait; 0: use the repository backoff.
    pub retry_in: u64,
}

impl std::fmt::Display for ObserveError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}: {}", self.code.as_str(), self.message)
    }
}

impl std::error::Error for ObserveError {}

/// Why an observation did not happen: a read failed, or the run was canceled (not a repository failure).
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum ObserveFailure {
    Error(ObserveError),
    Canceled,
}

fn is_failure_conclusion(c: &str) -> bool {
    matches!(
        c,
        "failure" | "cancelled" | "timed_out" | "action_required" | "startup_failure"
    )
}

/// The class of a head: `red` when a completed run failed or a status is `failure` or `error`, else `pending` when a
/// run is not completed or a status is `pending`, else `green` when anything was reported, else `none`. The shape of
/// a red head and the failing names belong to the core (`ClassifyChecks`); the observer needs the class only to
/// notice a debounce candidate in flight.
pub fn checks_class(runs: &[CheckRun], statuses: &[StatusContext]) -> &'static str {
    let failing = runs
        .iter()
        .any(|r| r.status == "completed" && is_failure_conclusion(&r.conclusion))
        || statuses
            .iter()
            .any(|s| s.state == "failure" || s.state == "error");
    if failing {
        return CLASS_RED;
    }
    let pending = runs.iter().any(|r| r.status != "completed")
        || statuses.iter().any(|s| s.state == "pending");
    if pending {
        CLASS_PENDING
    } else if runs.len() + statuses.len() > 0 {
        CLASS_GREEN
    } else {
        CLASS_NONE
    }
}
