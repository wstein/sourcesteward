//! `HostObserver`: one snapshot per repository and cycle over the conditional reads of the watch reader (port of
//! `internal/watchcmd/observer.go` and `pullsread.go`). It reads only when the accepted state makes it necessary: list
//! pages every cycle, `branches/BASE` once per base, `pulls/N` for a new PR, a new head, a moved base, after a merge
//! and on the slow refresh, check runs and statuses for pending, unobserved or changed PRs, and `pulls/N` once for a
//! PR that left the open list. Nothing here writes to a forge.
use std::collections::{BTreeMap, BTreeSet};
use std::sync::atomic::AtomicBool;

use serde_json::Value;

use super::cond_store::CondStore;
use super::http::{
    Code, PullListResult, PullListState, RateInfo, WatchError, WatchHttp, WatchResponse,
    MAX_CHECK_PAGES, MAX_OPEN_PRS,
};
use super::obs::{Hints, ObserveFailure, RepoObservation};
use crate::github::reads::{parse_workflow_run, Budget, WorkflowRun};

/// The bound of open pull requests per repository when nothing else is set.
pub const DEFAULT_MAX_OPEN_PRS: usize = MAX_OPEN_PRS;

/// The reads of the observer. Every method is a GET: the observer cannot write to a forge.
pub trait WatchRead {
    fn list_open_pulls(
        &self,
        repo: &str,
        prior: &PullListState,
        max_open: usize,
    ) -> Result<PullListResult, WatchError>;
    fn pull_detail(&self, repo: &str, number: u64, etag: &str)
        -> Result<WatchResponse, WatchError>;
    fn branch(&self, repo: &str, name: &str, etag: &str) -> Result<WatchResponse, WatchError>;
    fn commit(&self, repo: &str, sha: &str, etag: &str) -> Result<WatchResponse, WatchError>;
    fn pull_commits(
        &self,
        repo: &str,
        number: u64,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError>;
    fn check_runs(
        &self,
        repo: &str,
        sha: &str,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError>;
    fn statuses(
        &self,
        repo: &str,
        sha: &str,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError>;
    fn rate_limit(&self) -> Result<RateInfo, WatchError>;
    /// The Actions runs of a head, the stand-in when the token cannot read check runs. The default is "refused".
    fn actions_runs(&self, _repo: &str, _sha: &str) -> Result<Vec<WorkflowRun>, WatchError> {
        let mut e = WatchError::new(Code::Forbidden, "access was refused");
        e.status = 403;
        Err(e)
    }
}

impl WatchRead for WatchHttp {
    fn list_open_pulls(
        &self,
        repo: &str,
        prior: &PullListState,
        max_open: usize,
    ) -> Result<PullListResult, WatchError> {
        WatchHttp::list_open_pulls(self, repo, prior, max_open)
    }
    fn pull_detail(
        &self,
        repo: &str,
        number: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        WatchHttp::pull_detail(self, repo, number, etag)
    }
    fn branch(&self, repo: &str, name: &str, etag: &str) -> Result<WatchResponse, WatchError> {
        WatchHttp::branch(self, repo, name, etag)
    }
    fn commit(&self, repo: &str, sha: &str, etag: &str) -> Result<WatchResponse, WatchError> {
        WatchHttp::commit(self, repo, sha, etag)
    }
    fn pull_commits(
        &self,
        repo: &str,
        number: u64,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        WatchHttp::pull_commits(self, repo, number, page, etag)
    }
    fn check_runs(
        &self,
        repo: &str,
        sha: &str,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        WatchHttp::check_runs(self, repo, sha, page, etag)
    }
    fn statuses(
        &self,
        repo: &str,
        sha: &str,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        WatchHttp::statuses(self, repo, sha, page, etag)
    }
    fn rate_limit(&self) -> Result<RateInfo, WatchError> {
        WatchHttp::rate_limit(self)
    }
    fn actions_runs(&self, repo: &str, sha: &str) -> Result<Vec<WorkflowRun>, WatchError> {
        let bad = || WatchError::new(Code::Malformed, "actions runs response is not usable");
        let mut out: Vec<WorkflowRun> = Vec::new();
        for page in 1..=MAX_CHECK_PAGES {
            let res = WatchHttp::actions_runs_page(self, repo, sha, page)?;
            let v: Value = serde_json::from_slice(&res.body).map_err(|_| bad())?;
            let total = v["total_count"].as_u64().ok_or_else(bad)?;
            let arr = v["workflow_runs"].as_array().ok_or_else(bad)?;
            for r in arr {
                out.push(parse_workflow_run(r, &Budget::default()).ok_or_else(bad)?);
            }
            if arr.is_empty() || out.len() as u64 >= total {
                break;
            }
        }
        Ok(out)
    }
}

/// The decoded `pulls/N`: what watch reads (`mergeable`, `mergeable_state`, fork) plus the head SHA and `rebaseable`
/// that `stw pulls` needs. `None` means GitHub answered null (not computed yet).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct PullDetail {
    pub mergeable: Option<bool>,
    pub mergeable_state: String,
    pub rebaseable: Option<bool>,
    pub head_sha: String,
    /// The head repository is missing or differs from the base repository (case-insensitive).
    pub fork: bool,
}

/// Decodes the body of one `pulls/N` read (stub).
pub fn decode_pull_detail(_body: &[u8]) -> Result<PullDetail, WatchError> {
    Err(WatchError::new(Code::Malformed, "stub"))
}

/// The default branch guessed from the bases of the open PRs (stub).
pub fn guess_default_branch(_bases: &[&str]) -> &'static str {
    "?"
}

/// A time stamp as Go's `time.Parse(time.RFC3339, ...)` accepts it (stub).
pub fn valid_rfc3339(_s: &str) -> bool {
    false
}

/// The failure of one page of `pulls/N/commits` as the restacker sees it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum CommitsError {
    /// The answer does not have the expected shape.
    Malformed,
    /// Any other read failure.
    Other(WatchError),
}

/// One page of the commits of a pull request (stub).
pub fn read_pull_commits<R: WatchRead + ?Sized>(
    _r: &R,
    _repo: &str,
    _number: u64,
    _page: u64,
) -> Result<(Vec<String>, bool), CommitsError> {
    Err(CommitsError::Malformed)
}

/// A watcher over one reader.
pub struct HostObserver<R: WatchRead> {
    r: R,
    now: Box<dyn Fn() -> i64 + Send>,
    max_open: usize,
    ignore_authors: Vec<String>,
    ignore_labels: Vec<String>,
    last_class: BTreeMap<String, String>,
    hold_until: i64,
    warned: BTreeSet<String>,
    diags: Vec<String>,
}

impl<R: WatchRead> HostObserver<R> {
    pub fn new(
        r: R,
        now: Box<dyn Fn() -> i64 + Send>,
        max_open: usize,
        ignore_authors: Vec<String>,
        ignore_labels: Vec<String>,
    ) -> HostObserver<R> {
        HostObserver {
            r,
            now,
            max_open: if max_open < 1 {
                DEFAULT_MAX_OPEN_PRS
            } else {
                max_open
            },
            ignore_authors,
            ignore_labels,
            last_class: BTreeMap::new(),
            hold_until: 0,
            warned: BTreeSet::new(),
            diags: Vec::new(),
        }
    }

    /// The one-line diagnostics since the last call.
    pub fn take_diagnostics(&mut self) -> Vec<String> {
        std::mem::take(&mut self.diags)
    }

    /// Reads `rate_limit` once per cycle (stub).
    pub fn after_cycle(&mut self) -> bool {
        false
    }

    /// One snapshot of `repo` (stub).
    pub fn observe(
        &mut self,
        _repo: &str,
        _hints: &Hints,
        _cond: Option<&CondStore>,
        _cancel: &AtomicBool,
    ) -> Result<RepoObservation, ObserveFailure> {
        let _ = (
            &self.r,
            &self.now,
            self.max_open,
            &self.ignore_authors,
            &self.ignore_labels,
            &self.last_class,
            self.hold_until,
            &self.warned,
        );
        Err(ObserveFailure::Canceled)
    }
}
