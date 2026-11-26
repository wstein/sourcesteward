//! The accepted state of the watcher: what was last accepted about every repository and pull request, the counters that keep
//! event ids unique, and the failure bookkeeping. It is `state.json` (format `stw-watch-state`, `schema_version` 1; the Rust
//! format of the port, not the Go one). Maps are ordered (`BTreeMap`), so the file is deterministic.
//!
//! The state changes in two ways: `State::apply` (the transition of an event, identical for a live cycle and for the replay
//! after a crash) and the cycle's own bookkeeping that is not an event (the cycle counter, `last_ok`, debounce candidates,
//! backoff; W3). `seq` is not stored: it comes from the journal tail.
use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use serde_json::Value;

use super::event::Skipped;

/// `format` of `state.json`; a file without it (a Go state file, for instance) is not ours.
pub const STATE_FORMAT: &str = "stw-watch-state";
/// `schema_version` of `state.json`.
pub const STATE_SCHEMA_VERSION: u32 = 1;
/// Closed-unmerged PRs remembered per repository (oldest dropped first).
pub const MAX_CLOSED_PRS: usize = 5000;

/// The check class of a PR head. `Unobserved` (written as the empty string) before the first read.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub enum Class {
    #[default]
    #[serde(rename = "")]
    Unobserved,
    #[serde(rename = "none")]
    NoChecks,
    #[serde(rename = "pending")]
    Pending,
    #[serde(rename = "green")]
    Green,
    #[serde(rename = "red")]
    Red,
}

/// The per-PR counters of the transition families. They only grow.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Epochs {
    pub head: u64,
    pub checks: u64,
    pub mergeable: u64,
    pub ready: u64,
    pub base: u64,
}

impl Epochs {
    /// The larger of every counter.
    pub fn max(self, o: Epochs) -> Epochs {
        Epochs {
            head: self.head.max(o.head),
            checks: self.checks.max(o.checks),
            mergeable: self.mergeable.max(o.mergeable),
            ready: self.ready.max(o.ready),
            base: self.base.max(o.base),
        }
    }
}

/// A mergeability sighting that needs a second consecutive cycle before it is announced.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct MergeCandidate {
    pub mergeable: bool,
    pub behind: bool,
    pub state: String,
}

/// The last accepted observation of one open PR plus its debounce candidates.
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PrState {
    pub number: u64,
    pub title: String,
    pub author: String,
    pub labels: Vec<String>,
    pub draft: bool,
    pub head: String,
    pub head_ref: String,
    pub base: String,
    pub fork: bool,
    /// 0 for the first appearance, plus one for every reopening after a close without merge.
    pub generation: u64,
    pub epochs: Epochs,
    pub class: Class,
    /// A class seen once; it becomes `class` (and an event) when the next cycle sees it again. `None`: no candidate.
    pub class_candidate: Option<Class>,
    pub pending_since: String,
    pub pending_names: Vec<String>,
    pub timeout_fired: bool,
    pub mergeable: Option<bool>,
    pub mergeable_state: String,
    pub conflicting: bool,
    pub behind: bool,
    pub mergeable_candidate: Option<MergeCandidate>,
    pub detail_due: bool,
    /// Left the open list while `pulls/N` was unreadable; retried every cycle.
    pub gone: bool,
}

/// A closed-unmerged PR: a reopened one gets the next generation and keeps its counters.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ClosedPr {
    pub number: u64,
    pub generation: u64,
    pub epochs: Epochs,
}

/// Merge work for the restacker: it stays until a poll has emitted the restack events of the merge.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RestackDue {
    pub merged_pr: u64,
    pub merge_commit: Option<String>,
    pub old_parent: String,
    pub base: String,
    pub head_ref: String,
    /// True unless the merged PR was observed open with its head in the base repository: a fork-merged or an unobserved
    /// PR never yields children of the first class.
    pub head_fork: bool,
}

/// Everything accepted about one repository.
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RepoState {
    /// False: the next successful read is a silent baseline.
    pub baselined: bool,
    pub since: String,
    pub last_ok: String,
    pub default_branch: String,
    /// The conditional-request store of the reader (W2/W3 give it a type); the core only drops it.
    pub conditional: Option<Value>,
    pub prs: BTreeMap<u64, PrState>,
    /// Closed-unmerged generations, oldest first, at most `MAX_CLOSED_PRS`.
    pub closed: Vec<ClosedPr>,
    pub restack_due: Vec<RestackDue>,
    /// Ids of the `restack-needed` events already journalled for the merges in `restack_due`.
    pub restack_done: Vec<String>,
}

/// Failure and stall bookkeeping of one repository, or of the run (key `""`).
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Health {
    pub failures: u64,
    pub episode: u64,
    pub error_open: bool,
    pub last_code: String,
    pub stalled: bool,
    pub stall_episode: u64,
    /// Backoff: not polled before this time (run level: the hold). Empty: no wait.
    pub next_try: String,
}

/// `state.json`.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct State {
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub(super) issues: BTreeMap<String, IssueState>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub(super) workflows: BTreeMap<String, WorkflowState>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub(super) branches: BTreeMap<String, BranchState>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub(super) source_errors: BTreeMap<String, u64>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub(super) activity: BTreeMap<String, ActivityState>,
    pub format: String,
    pub schema_version: u32,
    pub applied_seq: u64,
    pub printed_seq: u64,
    pub interval_seconds: u64,
    pub cycle: u64,
    pub repos_epoch: u64,
    /// The watched set; `None` before the first cycle (and after a rebuild from the journal): the first cycle sets it
    /// silently.
    pub repos: Option<Vec<String>>,
    pub skipped: Vec<Skipped>,
    pub repo: BTreeMap<String, RepoState>,
    /// Key `""` is the run level.
    pub health: BTreeMap<String, Health>,
}

impl Default for State {
    fn default() -> State {
        State::new()
    }
}

impl State {
    /// The empty state of a new directory.
    pub fn new() -> State {
        State {
            issues: BTreeMap::new(),
            workflows: BTreeMap::new(),
            branches: BTreeMap::new(),
            source_errors: BTreeMap::new(),
            activity: BTreeMap::new(),
            format: STATE_FORMAT.to_string(),
            schema_version: STATE_SCHEMA_VERSION,
            applied_seq: 0,
            printed_seq: 0,
            interval_seconds: 0,
            cycle: 0,
            repos_epoch: 0,
            repos: None,
            skipped: Vec::new(),
            repo: BTreeMap::new(),
            health: BTreeMap::new(),
        }
    }

    /// The bookkeeping of `repo` (`""` for the run), created when missing.
    pub fn health_mut(&mut self, repo: &str) -> &mut Health {
        self.health.entry(repo.to_string()).or_default()
    }

    /// The file content: compact JSON, one line, newline at the end.
    pub fn encode(&self) -> Vec<u8> {
        let mut out = serde_json::to_vec(self)
            .expect("the state serializes: strings, numbers and ordered maps only");
        out.push(b'\n');
        out
    }

    /// Reads `state.json`. Anything that is not a state of this format and version is an error: another `format`, another
    /// `schema_version`, an unknown or missing key, a wrong type, trailing data.
    pub fn decode(bytes: &[u8]) -> Result<State, String> {
        #[derive(Deserialize)]
        struct Head {
            format: Option<String>,
            schema_version: Option<u64>,
        }
        let head: Head = serde_json::from_slice(bytes).map_err(|e| e.to_string())?;
        if head.format.as_deref() != Some(STATE_FORMAT) {
            return Err("not a stw watch state (format)".to_string());
        }
        if head.schema_version != Some(u64::from(STATE_SCHEMA_VERSION)) {
            return Err(format!(
                "unsupported schema_version {:?}",
                head.schema_version
            ));
        }
        serde_json::from_slice(bytes).map_err(|e| e.to_string())
    }
}

impl RepoState {
    /// A repository seen for the first time at `since`.
    pub fn new(since: &str) -> RepoState {
        RepoState {
            since: since.to_string(),
            ..RepoState::default()
        }
    }

    /// The record of a closed-unmerged PR and its index.
    pub fn closed_record(&self, number: u64) -> Option<(usize, &ClosedPr)> {
        self.closed
            .iter()
            .enumerate()
            .find(|(_, c)| c.number == number)
    }

    /// Remembers a closed-unmerged PR (replacing an older record of the same number), keeping the newest `MAX_CLOSED_PRS`.
    pub fn record_closed(&mut self, c: ClosedPr) {
        self.closed.retain(|x| x.number != c.number);
        self.closed.push(c);
        if self.closed.len() > MAX_CLOSED_PRS {
            let drop = self.closed.len() - MAX_CLOSED_PRS;
            self.closed.drain(..drop);
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub(super) struct IssueState {
    pub baselined: bool,
    pub epoch: u64,
    pub open: BTreeMap<i64, crate::issue_list::Issue>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub(super) struct WorkflowState {
    // Rebaseline the filtered query when opening state from the unfiltered watcher.
    #[serde(default)]
    pub failure_baselined: bool,
    pub seen: std::collections::BTreeSet<(u64, u64)>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub(super) struct ActivityState {
    pub baselined: bool,
    pub epoch: u64,
    pub versions: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub(super) struct BranchState {
    pub epoch: u64,
    pub heads: BTreeMap<String, String>,
}
