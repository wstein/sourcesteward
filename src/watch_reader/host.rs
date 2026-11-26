//! `HostObserver`: one snapshot per repository and cycle over the conditional reads of the watch reader (port of
//! `internal/watchcmd/observer.go` and `pullsread.go`). It reads only when the accepted state makes it necessary: list
//! pages every cycle, `branches/BASE` once per base, `pulls/N` for a new PR, a new head, a moved base, after a merge
//! and on the slow refresh, check runs and statuses for pending, unobserved or changed PRs, and `pulls/N` once for a
//! PR that left the open list. Nothing here writes to a forge.
use std::collections::{BTreeMap, BTreeSet};
use std::sync::atomic::{AtomicBool, Ordering};

use serde_json::Value;

use super::cond_store::{checks_key, detail_key, CondStore, TipEntry};
use super::http::{
    assess_rate_limit, encode_branch_segment, valid_sha, Advice, Code, PullItem, PullListResult,
    PullListState, RateInfo, WatchError, WatchHttp, WatchResponse, MAX_CHECK_PAGES, MAX_OPEN_PRS,
};
use super::obs::{
    checks_class, CheckRun, ChecksObs, ChecksVia, DetailObs, GoneObs, Hints, KnownPr, MethodHint,
    ObserveError, ObserveFailure, PrObs, RepoObservation, StatusContext, CLASS_NONE, CLASS_PENDING,
};
use crate::github::reads::{escape_untrusted, parse_workflow_run, Budget, WorkflowRun};

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
    fn wiki_head(&self, _repo: &str) -> Result<String, WatchError> {
        Err(WatchError::new(Code::Forbidden, "wiki read unavailable"))
    }
    fn recent_workflows(&self, _repo: &str) -> Result<WatchResponse, WatchError> {
        Err(WatchError::new(Code::Forbidden, "Actions read unavailable"))
    }
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
    fn wiki_head(&self, repo: &str) -> Result<String, WatchError> {
        WatchHttp::wiki_head(self, repo)
    }
    fn recent_workflows(&self, repo: &str) -> Result<WatchResponse, WatchError> {
        WatchHttp::recent_workflows(self, repo)
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

fn not_usable() -> WatchError {
    WatchError::new(Code::Malformed, "pull request response is not usable")
}

/// A JSON string, null or absent as text; `None` for any other type.
fn text_of(v: &Value) -> Option<String> {
    match v {
        Value::Null => Some(String::new()),
        Value::String(s) => Some(s.clone()),
        _ => None,
    }
}

fn flag_of(v: &Value) -> Option<Option<bool>> {
    match v {
        Value::Null => Some(None),
        Value::Bool(b) => Some(Some(*b)),
        _ => None,
    }
}

/// The `full_name` of a repository object, `None` for a missing or null repository; the outer `Option` is the type
/// check.
fn repo_name(parent: &Value) -> Option<Option<String>> {
    match &parent["repo"] {
        Value::Null => Some(None),
        Value::Object(o) => Some(Some(text_of(o.get("full_name").unwrap_or(&Value::Null))?)),
        _ => None,
    }
}

/// Decodes the body of one `pulls/N` read.
pub fn decode_pull_detail(body: &[u8]) -> Result<PullDetail, WatchError> {
    let v: Value = serde_json::from_slice(body).map_err(|_| not_usable())?;
    if !v.is_object() && !v.is_null() {
        return Err(not_usable());
    }
    let objs_ok = |k: &str| matches!(v[k], Value::Null | Value::Object(_));
    if !objs_ok("head") || !objs_ok("base") {
        return Err(not_usable());
    }
    let head_repo = repo_name(&v["head"]).ok_or_else(not_usable)?;
    let base_repo = repo_name(&v["base"]).ok_or_else(not_usable)?;
    let fork = match (&head_repo, &base_repo) {
        (Some(h), Some(b)) => h.to_lowercase() != b.to_lowercase(),
        _ => true,
    };
    Ok(PullDetail {
        mergeable: flag_of(&v["mergeable"]).ok_or_else(not_usable)?,
        mergeable_state: text_of(&v["mergeable_state"]).ok_or_else(not_usable)?,
        rebaseable: flag_of(&v["rebaseable"]).ok_or_else(not_usable)?,
        head_sha: text_of(&v["head"]["sha"]).ok_or_else(not_usable)?,
        fork,
    })
}

/// The default branch is guessed from the bases of the open PRs (`main`, then `master`): the reader has no
/// repository read. Unknown makes the core classify a red head conservatively.
pub fn guess_default_branch(bases: &[&str]) -> &'static str {
    for name in ["main", "master"] {
        if bases.contains(&name) {
            return name;
        }
    }
    ""
}

/// An RFC 3339 time stamp as Go's `time.Parse` accepts it: `YYYY-MM-DDTHH:MM:SS`, optional fraction, then `Z` or
/// `+HH:MM` / `-HH:MM`.
pub fn valid_rfc3339(s: &str) -> bool {
    let b = s.as_bytes();
    if b.len() < 20 || !s.is_ascii() {
        return false;
    }
    let num = |from: usize, to: usize| -> Option<u32> {
        let t = &s[from..to];
        t.bytes()
            .all(|c| c.is_ascii_digit())
            .then(|| t.parse().ok())?
    };
    let (Some(y), Some(mo), Some(d), Some(h), Some(mi), Some(sec)) = (
        num(0, 4),
        num(5, 7),
        num(8, 10),
        num(11, 13),
        num(14, 16),
        num(17, 19),
    ) else {
        return false;
    };
    if b[4] != b'-' || b[7] != b'-' || b[10] != b'T' || b[13] != b':' || b[16] != b':' {
        return false;
    }
    let leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;
    let dim = match mo {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 if leap => 29,
        2 => 28,
        _ => return false,
    };
    if !(1..=dim).contains(&d) || h > 23 || mi > 59 || sec > 59 {
        return false;
    }
    let mut rest = &s[19..];
    if let Some(frac) = rest.strip_prefix('.') {
        let n = frac.bytes().take_while(u8::is_ascii_digit).count();
        if n == 0 {
            return false;
        }
        rest = &frac[n..];
    }
    if rest == "Z" {
        return true;
    }
    let r = rest.as_bytes();
    r.len() == 6
        && (r[0] == b'+' || r[0] == b'-')
        && r[3] == b':'
        && rest[1..3].bytes().all(|c| c.is_ascii_digit())
        && rest[4..6].bytes().all(|c| c.is_ascii_digit())
        && rest[1..3].parse::<u32>().is_ok_and(|x| x <= 23)
        && rest[4..6].parse::<u32>().is_ok_and(|x| x <= 59)
}

/// The failure of one page of `pulls/N/commits` as the restacker sees it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum CommitsError {
    /// The answer does not have the expected shape.
    Malformed,
    /// Any other read failure.
    Other(WatchError),
}

/// One page of the commits of a pull request, read without a validator (the page is only read in the cycle of a
/// merge): the 40-hex SHAs in order, and whether the page was full (100) so that more may follow.
pub fn read_pull_commits<R: WatchRead + ?Sized>(
    r: &R,
    repo: &str,
    number: u64,
    page: u64,
) -> Result<(Vec<String>, bool), CommitsError> {
    let res = r.pull_commits(repo, number, page, "").map_err(|e| {
        if e.code == Code::Malformed {
            CommitsError::Malformed
        } else {
            CommitsError::Other(e)
        }
    })?;
    let v: Value = serde_json::from_slice(&res.body).map_err(|_| CommitsError::Malformed)?;
    let arr = v.as_array().ok_or(CommitsError::Malformed)?;
    let mut shas = Vec::with_capacity(arr.len());
    for it in arr {
        let sha = it["sha"].as_str().filter(|s| valid_sha(s));
        shas.push(sha.ok_or(CommitsError::Malformed)?.to_string());
    }
    Ok((shas, arr.len() >= 100))
}

/// One half of the checks of a head: the items read, the ETag of the first page, and whether the first page was a 304.
struct Half<T> {
    items: Vec<T>,
    first_etag: String,
    same: bool,
}

fn str_of(v: &Value) -> Option<String> {
    text_of(v)
}

fn json_body(res: &WatchResponse, what: &str) -> Result<Value, WatchError> {
    serde_json::from_slice(&res.body)
        .map_err(|_| WatchError::new(Code::Malformed, format!("{what} response is not usable")))
}

fn run_of(v: &Value) -> Option<CheckRun> {
    let o = v.as_object()?;
    let get = |k: &str| str_of(o.get(k).unwrap_or(&Value::Null));
    let (title, summary, text) = match o.get("output").unwrap_or(&Value::Null) {
        Value::Null => (String::new(), String::new(), String::new()),
        Value::Object(out) => {
            let g = |k: &str| str_of(out.get(k).unwrap_or(&Value::Null));
            (g("title")?, g("summary")?, g("text")?)
        }
        _ => return None,
    };
    Some(CheckRun {
        name: get("name")?,
        status: get("status")?,
        conclusion: get("conclusion")?,
        title,
        summary,
        text,
    })
}

/// A watcher over one reader. Not shared between threads: it keeps the class of the last check read of each PR, a
/// rate-limit hold and the diagnostics of its cycles.
pub struct HostObserver<R: WatchRead> {
    graph: Option<crate::github::Client>,
    prs: bool,
    pulls_refused: BTreeSet<String>,
    issues: bool,
    reviews: bool,
    discussions: bool,
    activity_refused: BTreeSet<(String, &'static str)>,
    issue_retry_at: i64,
    workflows: bool,
    wiki: bool,
    wiki_retry_at: i64,
    workflows_refused: BTreeSet<String>,
    workflows_retry_at: i64,
    issues_refused: BTreeSet<String>,
    r: R,
    now: Box<dyn Fn() -> i64 + Send>,
    max_open: usize,
    ignore_authors: Vec<String>,
    ignore_labels: Vec<String>,
    /// `repo#n`: check class of the last read, to confirm a debounce candidate.
    last_class: BTreeMap<String, String>,
    /// Unix time until which a rate-limit wait holds; the next `observe` reports it once.
    hold_until: i64,
    warned: BTreeSet<String>,
    diags: Vec<String>,
}

impl<R: WatchRead> HostObserver<R> {
    /// `now` is the clock in Unix seconds; `max_open` the bound of open PRs per repository (1 to 2000; anything below 1
    /// means the default).
    pub fn new(
        r: R,
        now: Box<dyn Fn() -> i64 + Send>,
        max_open: usize,
        ignore_authors: Vec<String>,
        ignore_labels: Vec<String>,
    ) -> HostObserver<R> {
        HostObserver {
            graph: None,
            prs: true,
            pulls_refused: BTreeSet::new(),
            issues: false,
            reviews: false,
            discussions: false,
            activity_refused: BTreeSet::new(),
            issue_retry_at: 0,
            workflows: false,
            wiki: false,
            wiki_retry_at: 0,
            workflows_refused: BTreeSet::new(),
            workflows_retry_at: 0,
            issues_refused: BTreeSet::new(),
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

    pub fn with_prs(mut self, enabled: bool) -> Self {
        self.prs = enabled;
        self
    }

    pub fn with_wiki(mut self, enabled: bool) -> Self {
        self.wiki = enabled;
        self
    }

    pub fn with_workflows(mut self, enabled: bool) -> Self {
        self.workflows = enabled;
        self
    }

    pub fn with_graph_sources(
        mut self,
        client: crate::github::Client,
        issues: bool,
        reviews: bool,
        discussions: bool,
    ) -> Self {
        self.graph = Some(client);
        self.issues = issues;
        self.reviews = reviews;
        self.discussions = discussions;
        self
    }

    /// The one-line diagnostics since the last call (never an event, never a token).
    pub fn take_diagnostics(&mut self) -> Vec<String> {
        std::mem::take(&mut self.diags)
    }

    /// Reads `rate_limit` (free) once per cycle and applies the slowdown policy: it reports whether the next interval
    /// is to be doubled and arms a wait that the next `observe` turns into a run-wide `rate-limited` error.
    pub fn after_cycle(&mut self) -> bool {
        let Ok(info) = self.r.rate_limit() else {
            return false;
        };
        let d = assess_rate_limit(&info);
        if d.advice == Advice::Wait {
            self.hold_until = d.until;
        }
        d.advice == Advice::Slow
    }

    fn take_hold(&mut self) -> u64 {
        let until = std::mem::take(&mut self.hold_until);
        let d = until.saturating_sub((self.now)());
        if until > 0 && d > 0 {
            d as u64
        } else {
            0
        }
    }

    fn ignored(&self, it: &PullItem) -> bool {
        self.ignore_authors.contains(&it.user)
            || it.labels.iter().any(|l| self.ignore_labels.contains(l))
    }

    /// Maps a read failure to the classified error of the core. A rate limit holds the whole run.
    fn fail(&self, e: WatchError, cancel: &AtomicBool) -> ObserveFailure {
        if cancel.load(Ordering::SeqCst) {
            return ObserveFailure::Canceled;
        }
        let mut oe = ObserveError {
            code: e.code,
            message: e.message.clone(),
            run_wide: false,
            retry_in: 0,
        };
        if e.code == Code::RateLimited {
            let now = (self.now)();
            oe.run_wide = true;
            oe.retry_in = e.rate_limited_until(now).saturating_sub(now).max(0) as u64;
        }
        ObserveFailure::Error(oe)
    }

    /// One snapshot of `repo`. `cond` is the store of validators the core kept from the last cycle; the observation
    /// carries its replacement. `cancel` is checked when a read fails (a canceled run is not a repository failure).
    pub fn observe(
        &mut self,
        repo: &str,
        hints: &Hints,
        cond: Option<&CondStore>,
        cancel: &AtomicBool,
    ) -> Result<RepoObservation, ObserveFailure> {
        let wait = self.take_hold();
        if wait > 0 {
            return Err(ObserveFailure::Error(ObserveError {
                code: Code::RateLimited,
                message: "rate limit nearly exhausted; waiting for the reset".into(),
                run_wide: true,
                retry_in: wait,
            }));
        }
        let optional = self.workflows || self.issues || self.discussions || self.wiki;
        let mut observation = RepoObservation {
            skip_pr_events: true,
            ..RepoObservation::default()
        };
        if (self.prs || self.reviews) && !self.pulls_refused.contains(repo) {
            match self.observe_pulls(repo, hints, cond, cancel) {
                Ok(pulls) => observation = pulls,
                Err(ObserveFailure::Error(err)) if optional && !err.run_wide => {
                    observation.pr_read_error = Some(format!("prs-{}", err.code.as_str()));
                }
                Err(err) => return Err(err),
            }
        }
        let issues = self.read_issues(repo);
        let workflows = self.read_workflows(repo, &mut observation.active_workflows);
        let prs = &observation.prs;
        let mut activity = Vec::new();
        if self.reviews {
            let batches = prs.len().div_ceil(20).max(1);
            let batch = hints.cycle as usize % batches;
            for pr in prs.iter().skip(batch * 20).take(20) {
                let query = format!("query($o:String!, $n:String!) {{repository(owner:$o,name:$n) {{pullRequest(number:{}) {{reviews(last:20) {{nodes {{id state submittedAt url comments(last:20) {{nodes {{id url createdAt}}}}}}}}}}}}}}", pr.number);
                let scope = super::obs::ActivityScope::Reviews(pr.number);
                if let Some(result) = self.read_activity(repo, "reviews", &query, |root| {
                    parse_reviews(root, pr.number)
                }) {
                    activity.push((scope, result));
                }
            }
        }
        if self.discussions {
            let query = "query($o:String!, $n:String!) {repository(owner:$o,name:$n) {hasDiscussionsEnabled discussions(first:50,orderBy:{field:UPDATED_AT,direction:DESC}) {nodes {id number title url updatedAt closed comments {totalCount}}}}}";
            if let Some(result) = self.read_activity(repo, "discussions", query, parse_discussions)
            {
                activity.push((super::obs::ActivityScope::Discussions, result));
            }
        }
        if let Some(result) = self.read_wiki(repo) {
            activity.push((super::obs::ActivityScope::Wiki, result));
        }
        observation.issues = issues;
        observation.workflows = workflows;
        observation.activity = activity;
        Ok(observation)
    }

    fn observe_pulls(
        &mut self,
        repo: &str,
        hints: &Hints,
        cond: Option<&CondStore>,
        cancel: &AtomicBool,
    ) -> Result<RepoObservation, ObserveFailure> {
        let mut cond = cond.cloned().unwrap_or_default();
        let list = match self.r.list_open_pulls(repo, &cond.list, self.max_open) {
            Ok(l) => l,
            Err(e) => {
                if (self.workflows || self.issues || self.discussions || self.wiki)
                    && matches!(e.status, 401 | 403 | 404)
                    && e.code != Code::RateLimited
                {
                    self.pulls_refused.insert(repo.to_owned());
                }
                return Err(self.fail(e, cancel));
            }
        };
        cond.list = list.state.clone();
        if !self.prs {
            let mut prs: Vec<_> = list
                .items
                .iter()
                .filter(|it| !self.ignored(it))
                .map(|it| PrObs {
                    number: it.number,
                    ..PrObs::default()
                })
                .collect();
            prs.sort_by_key(|pr| pr.number);
            cond.prune(&list.items);
            return Ok(RepoObservation {
                prs,
                cond: Some(cond),
                skip_pr_events: true,
                ..RepoObservation::default()
            });
        }
        let known: BTreeMap<u64, &KnownPr> = hints.known.iter().map(|k| (k.number, k)).collect();
        let open: BTreeSet<u64> = list.items.iter().map(|i| i.number).collect();
        let mut gone = BTreeMap::new();
        let mut merged_any = false;
        for k in &hints.known {
            if open.contains(&k.number) {
                continue;
            }
            match self.read_gone(repo, k) {
                Ok(Some(g)) => {
                    merged_any |= g.merged;
                    gone.insert(k.number, g);
                }
                Ok(None) => {}
                Err(e) => return Err(self.fail(e, cancel)),
            }
        }
        let mut items = list.items.clone();
        items.sort_by_key(|i| i.number);
        let tips = match self.read_tips(repo, &items, &mut cond) {
            Ok(t) => t,
            Err(e) => return Err(self.fail(e, cancel)),
        };
        let mut prs = Vec::with_capacity(items.len());
        for it in &items {
            let mut p = PrObs {
                number: it.number,
                title: it.title.clone(),
                author: it.user.clone(),
                labels: it.labels.clone(),
                draft: it.draft,
                head: it.head_sha.clone(),
                head_ref: it.head_ref.clone(),
                base: it.base_ref.clone(),
                ..PrObs::default()
            };
            if !self.ignored(it) {
                let k = known.get(&it.number).copied();
                let refresh = hints.detail_every > 0
                    && hints.cycle.saturating_add(it.number) % hints.detail_every == 0;
                let head_same = k.is_some_and(|k| k.head == it.head_sha);
                let force = !head_same
                    || k.is_some_and(|k| k.detail_due)
                    || tips.contains(&it.base_ref)
                    || merged_any;
                if force || refresh {
                    let etag = if force {
                        String::new()
                    } else {
                        cond.validators
                            .get(&detail_key(it.number))
                            .cloned()
                            .unwrap_or_default()
                    };
                    match self.read_detail(repo, it.number, &etag, &mut cond) {
                        Ok(Some((d, fork))) => {
                            p.detail = Some(d);
                            p.fork = fork;
                            p.fork_known = true;
                        }
                        Ok(None) => {} // a 304 read nothing: the accepted fork flag stays
                        Err(e) => return Err(self.fail(e, cancel)),
                    }
                }
                let ck = format!("{repo}#{}", it.number);
                let last = self.last_class.get(&ck).cloned().unwrap_or_default();
                let (class, cand) =
                    k.map_or(("", ""), |k| (k.class.as_str(), k.class_candidate.as_str()));
                let inflight = (!last.is_empty() && last != class) || !cand.is_empty();
                if !head_same
                    || class.is_empty()
                    || class == CLASS_PENDING
                    || class == CLASS_NONE
                    || inflight
                    || refresh
                {
                    match self.read_checks(repo, &it.head_sha, inflight, &mut cond) {
                        Ok(Some((ch, class))) => {
                            p.checks = Some(ch);
                            self.last_class.insert(ck, class.to_string());
                        }
                        Ok(None) => {}
                        Err(e) => return Err(self.fail(e, cancel)),
                    }
                }
            }
            prs.push(p);
        }
        let bases: Vec<&str> = items.iter().map(|i| i.base_ref.as_str()).collect();
        cond.prune(&items);
        Ok(RepoObservation {
            default_branch: guess_default_branch(&bases).to_string(),
            prs,
            gone,
            cond: Some(cond),
            ..RepoObservation::default()
        })
    }

    fn read_workflows(
        &mut self,
        repo: &str,
        active: &mut usize,
    ) -> Option<Result<Vec<crate::watch::event::WorkflowCompleted>, String>> {
        if !self.workflows
            || self.workflows_refused.contains(repo)
            || (self.now)() < self.workflows_retry_at
        {
            return None;
        }
        let res = match self.r.recent_workflows(repo) {
            Ok(res) => res,
            Err(e) => {
                if matches!(e.status, 401 | 403 | 404) && e.code != Code::RateLimited {
                    self.workflows_refused.insert(repo.into());
                }
                if e.code == Code::RateLimited {
                    self.workflows_retry_at = e.rate_limited_until((self.now)());
                }
                return Some(Err(format!("workflows-{}", e.code.as_str())));
            }
        };
        let parse = || -> Option<(Vec<crate::watch::event::WorkflowCompleted>, usize)> {
            let payload: Value = serde_json::from_slice(&res.body).ok()?;
            let rows = payload["workflow_runs"].as_array()?;
            let mut runs = Vec::new();
            let mut pending = 0;
            for row in rows {
                if row["status"].as_str()? != "completed" {
                    pending += 1;
                    continue;
                }
                let text = |key: &str| row[key].as_str().map(str::to_owned);
                let id = row["id"].as_u64().filter(|id| *id > 0)?;
                runs.push(crate::watch::event::WorkflowCompleted {
                    run_id: id,
                    attempt: row["run_attempt"].as_u64().filter(|attempt| *attempt > 0)?,
                    conclusion: text("conclusion")?,
                    name: text("name")?,
                    branch: text("head_branch").unwrap_or_default(),
                    head: text("head_sha")?,
                    trigger: text("event")?,
                    url: format!("https://github.com/{repo}/actions/runs/{id}"),
                });
            }
            Some((runs, pending))
        };
        Some(match parse() {
            Some((runs, pending)) => {
                *active = pending;
                Ok(runs)
            }
            None => Err("workflows-malformed-response".into()),
        })
    }

    fn read_wiki(
        &mut self,
        repo: &str,
    ) -> Option<Result<Vec<crate::watch::event::Activity>, String>> {
        use crate::watch::event::{Activity, ActivityKind};
        let key = (repo.to_owned(), "wiki");
        if !self.wiki || self.activity_refused.contains(&key) || (self.now)() < self.wiki_retry_at {
            return None;
        }
        match self.r.wiki_head(repo) {
            Ok(head) => Some(Ok(vec![Activity {
                kind: ActivityKind::Wiki,
                id: "HEAD".into(),
                summary: head.clone(),
                version: head,
                number: 0,
                url: format!("https://github.com/{repo}/wiki"),
            }])),
            Err(e) => {
                if matches!(e.status, 401 | 403 | 404) && e.code != Code::RateLimited {
                    self.activity_refused.insert(key);
                }
                if e.code == Code::RateLimited {
                    self.wiki_retry_at = e.rate_limited_until((self.now)());
                }
                Some(Err(format!("wiki-{}", e.code.as_str())))
            }
        }
    }

    fn read_activity(
        &mut self,
        repo: &str,
        source: &'static str,
        query: &str,
        parse: impl FnOnce(&Value) -> Option<Vec<crate::watch::event::Activity>>,
    ) -> Option<Result<Vec<crate::watch::event::Activity>, String>> {
        let client = self.graph.as_ref()?;
        let key = (repo.to_owned(), source);
        if self.activity_refused.contains(&key) || (self.now)() < self.issue_retry_at {
            return None;
        }
        let (owner, name) = repo.split_once('/')?;
        let request = serde_json::json!({"query": query, "variables": {"o": owner, "n": name}});
        client.reset_poll_rate_limit();
        client.arm_deadline(std::time::Duration::from_secs(10));
        let response = client.graphql(&request);
        if client.rate_limited() {
            self.hold_graph_reads();
            return Some(Err(format!("{source}-rate-limited")));
        }
        let refusal = match &response {
            Err(e) => graph_refusal(&e.to_string()),
            Ok(v) => v["errors"].as_array().is_some_and(|errors| {
                errors.iter().any(|err| {
                    err["type"].as_str() == Some("FORBIDDEN")
                        || err["message"].as_str().is_some_and(graph_refusal)
                })
            }),
        };
        if refusal {
            self.activity_refused.insert(key);
            return Some(Err(format!("{source}-read-refused")));
        }
        if source == "discussions"
            && response.as_ref().ok().is_some_and(|v| {
                v["data"]["repository"]["hasDiscussionsEnabled"].as_bool() == Some(false)
            })
        {
            self.activity_refused.insert(key);
            return Some(Err("discussions-disabled".into()));
        }
        let result = response.ok().and_then(|v| {
            if v["errors"]
                .as_array()
                .is_some_and(|errors| !errors.is_empty())
            {
                return None;
            }
            parse(&v["data"]["repository"])
        });
        Some(result.ok_or_else(|| format!("{source}-read-incomplete")))
    }

    fn hold_graph_reads(&mut self) {
        let now = (self.now)();
        let reset = self
            .graph
            .as_ref()
            .and_then(|client| client.stats().rate)
            .filter(|rate| rate.remaining == 0)
            .and_then(|rate| i64::try_from(rate.reset).ok());
        self.issue_retry_at = reset.unwrap_or_default().max(now.saturating_add(60));
    }

    fn read_issues(&mut self, repo: &str) -> Option<crate::issue_list::Listed> {
        if !self.issues {
            return None;
        }
        let client = self.graph.as_ref()?;
        if self.issues_refused.contains(repo) || (self.now)() < self.issue_retry_at {
            return None;
        }
        client.reset_poll_rate_limit();
        client.arm_deadline(std::time::Duration::from_secs(10));
        let mut listed =
            crate::issue_list::read(&[repo.to_owned()], crate::issue_list::PAGE, 1, |body| {
                client.graphql(body)
            })
            .pop()?;
        if client.rate_limited() {
            self.hold_graph_reads();
            listed.coverage = "incomplete";
        } else if listed.coverage != "complete"
            && listed.error.as_deref().is_some_and(graph_refusal)
        {
            self.issues_refused.insert(repo.into());
        }
        Some(listed)
    }

    /// Reads `branches/BASE` once per distinct base branch of the watched PRs and returns the bases whose tip moved.
    /// A base branch that does not exist (404) is no failure: its PRs just see no move.
    fn read_tips(
        &mut self,
        repo: &str,
        items: &[PullItem],
        cond: &mut CondStore,
    ) -> Result<BTreeSet<String>, WatchError> {
        let mut moved = BTreeSet::new();
        let mut seen = BTreeSet::new();
        for it in items {
            if self.ignored(it) || !seen.insert(it.base_ref.clone()) {
                continue;
            }
            if encode_branch_segment(&it.base_ref).is_none() {
                // a base name the reader refuses (for example one with %) must not fail the repository
                if self.warned.insert(format!("{repo}\0{}", it.base_ref)) {
                    let (name, _) = escape_untrusted(&it.base_ref, 200);
                    self.diags.push(format!(
                        "watch: {repo}: the tip of base branch \"{}\" is not read (name not allowed); its PRs are still watched",
                        name.replace('"', "\\\"")
                    ));
                }
                continue;
            }
            let prior = cond.tips.get(&it.base_ref).cloned();
            let res = match self.r.branch(
                repo,
                &it.base_ref,
                prior.as_ref().map_or("", |p| p.etag.as_str()),
            ) {
                Ok(r) => r,
                Err(e) if e.code == Code::Forbidden && e.status == 404 => continue,
                Err(e) => return Err(e),
            };
            if res.not_modified {
                continue;
            }
            let sha = serde_json::from_slice::<Value>(&res.body)
                .ok()
                .and_then(|v| v["commit"]["sha"].as_str().map(str::to_string))
                .filter(|s| valid_sha(s))
                .ok_or_else(|| WatchError::new(Code::Malformed, "branch response is not usable"))?;
            let was_new_or_moved = prior.as_ref().is_none_or(|p| p.sha != sha);
            cond.tips.insert(
                it.base_ref.clone(),
                TipEntry {
                    etag: res.etag.clone(),
                    sha,
                },
            );
            if was_new_or_moved {
                moved.insert(it.base_ref.clone());
            }
        }
        Ok(moved)
    }

    /// Reads `pulls/N`. A 304 is "unchanged": no detail, the accepted values stay.
    fn read_detail(
        &mut self,
        repo: &str,
        number: u64,
        etag: &str,
        cond: &mut CondStore,
    ) -> Result<Option<(DetailObs, bool)>, WatchError> {
        let res = self.r.pull_detail(repo, number, etag)?;
        if res.not_modified {
            return Ok(None);
        }
        let doc = decode_pull_detail(&res.body)?;
        if !res.etag.is_empty() {
            cond.validators.insert(detail_key(number), res.etag.clone());
        }
        Ok(Some((
            DetailObs {
                mergeable: doc.mergeable,
                state: doc.mergeable_state,
            },
            doc.fork,
        )))
    }

    /// Reads `pulls/N` of a PR that left the open list. `None` when the PR is still open on GitHub (the list lagged):
    /// it stays unresolved and is retried.
    fn read_gone(&mut self, repo: &str, k: &KnownPr) -> Result<Option<GoneObs>, WatchError> {
        let res = self.r.pull_detail(repo, k.number, "")?;
        let v: Value = serde_json::from_slice(&res.body).map_err(|_| not_usable())?;
        if !v.is_object() && !v.is_null() {
            return Err(not_usable());
        }
        let state = text_of(&v["state"]).ok_or_else(not_usable)?;
        let merged = flag_of(&v["merged"])
            .ok_or_else(not_usable)?
            .unwrap_or(false);
        let merge_commit = text_of(&v["merge_commit_sha"]).ok_or_else(not_usable)?;
        let merged_at = text_of(&v["merged_at"]).ok_or_else(not_usable)?;
        let base = text_of(&v["base"]["ref"]).ok_or_else(not_usable)?;
        let head_sha = text_of(&v["head"]["sha"]).ok_or_else(not_usable)?;
        let head_ref = text_of(&v["head"]["ref"]).ok_or_else(not_usable)?;
        if state != "open" && state != "closed" {
            return Err(not_usable());
        }
        if state == "open" {
            return Ok(None);
        }
        // Values that become event details are validated here: anything else is a malformed response for this PR.
        let invalid = || {
            WatchError::new(
                Code::Malformed,
                "pull request response has an invalid value",
            )
        };
        if !valid_sha(&head_sha)
            || (merged && !merge_commit.is_empty() && !valid_sha(&merge_commit))
        {
            return Err(invalid());
        }
        if merged && !valid_rfc3339(&merged_at) {
            return Err(invalid());
        }
        let mut g = GoneObs {
            merged,
            merge_commit: None,
            merged_at,
            base,
            head: head_sha,
            head_ref,
            method_hint: MethodHint::Unknown,
        };
        if merged && !merge_commit.is_empty() {
            g.method_hint = self.method_hint(repo, &merge_commit);
            g.merge_commit = Some(merge_commit);
        }
        Ok(Some(g))
    }

    /// A merge commit has two parents, a rebase or squash merge one. Unreadable: unknown.
    fn method_hint(&self, repo: &str, sha: &str) -> MethodHint {
        let Ok(res) = self.r.commit(repo, sha, "") else {
            return MethodHint::Unknown;
        };
        let Ok(v) = serde_json::from_slice::<Value>(&res.body) else {
            return MethodHint::Unknown;
        };
        match v["parents"].as_array().map(Vec::len) {
            Some(2) => MethodHint::Merge,
            Some(1) => MethodHint::RebaseOrSquash,
            _ => MethodHint::Unknown,
        }
    }

    /// Reads the check runs and commit statuses of one head. `None` is "unchanged" (both conditional reads answered
    /// 304). `force` sends no validator, so a debounce candidate is confirmed with a fresh read. The class returned
    /// is only used to notice a candidate in flight.
    fn read_checks(
        &mut self,
        repo: &str,
        sha: &str,
        force: bool,
        cond: &mut CondStore,
    ) -> Result<Option<(ChecksObs, &'static str)>, WatchError> {
        let (runs_key, stat_key) = (checks_key(sha, "runs"), checks_key(sha, "statuses"));
        let tag = |c: &CondStore, k: &str| {
            if force {
                String::new()
            } else {
                c.validators.get(k).cloned().unwrap_or_default()
            }
        };
        let (runs_tag, stat_tag) = (tag(cond, &runs_key), tag(cond, &stat_key));
        let mut runs = self.read_runs_or_actions(repo, sha, &runs_tag)?;
        let mut stats = self.read_statuses(repo, sha, &stat_tag)?;
        if runs.0.same && stats.same {
            return Ok(None);
        }
        if runs.0.same {
            // the other half changed: the classification needs both
            runs = self.read_runs_or_actions(repo, sha, "")?;
        }
        if stats.same {
            stats = self.read_statuses(repo, sha, "")?;
        }
        for (key, etag) in [
            (&runs_key, &runs.0.first_etag),
            (&stat_key, &stats.first_etag),
        ] {
            if etag.is_empty() {
                cond.validators.remove(key);
            } else {
                cond.validators.insert(key.clone(), etag.clone());
            }
        }
        let class = checks_class(&runs.0.items, &stats.items);
        Ok(Some((
            ChecksObs {
                runs: runs.0.items,
                statuses: stats.items,
                via: runs.1,
            },
            class,
        )))
    }

    /// The check runs; a 403 (a fine-grained token without the Checks permission) falls back to the Actions runs of
    /// the head, once announced per repository.
    fn read_runs_or_actions(
        &mut self,
        repo: &str,
        sha: &str,
        etag: &str,
    ) -> Result<(Half<CheckRun>, ChecksVia), WatchError> {
        match self.read_runs(repo, sha, etag) {
            Ok(h) => Ok((h, ChecksVia::CheckRuns)),
            Err(e) if e.code == Code::Forbidden && e.status == 403 => {
                let runs = self.r.actions_runs(repo, sha)?;
                if self.warned.insert(format!("{repo}\0actions-runs")) {
                    self.diags.push(format!(
                        "watch: {repo}: checks via actions-runs (the token cannot read check runs)"
                    ));
                }
                let items = crate::status_ci::latest_runs(&runs, sha)
                    .into_iter()
                    .map(|r| CheckRun {
                        name: r.name,
                        status: r.status,
                        conclusion: r.conclusion.unwrap_or_default(),
                        ..CheckRun::default()
                    })
                    .collect();
                Ok((
                    Half {
                        items,
                        first_etag: String::new(),
                        same: false,
                    },
                    ChecksVia::ActionsRuns,
                ))
            }
            Err(e) => Err(e),
        }
    }

    fn read_runs(&self, repo: &str, sha: &str, etag: &str) -> Result<Half<CheckRun>, WatchError> {
        let mut runs: Vec<CheckRun> = Vec::new();
        let mut first = String::new();
        for page in 1..=MAX_CHECK_PAGES {
            let tag = if page == 1 { etag } else { "" };
            let res = self.r.check_runs(repo, sha, page, tag)?;
            if res.not_modified {
                return Ok(Half {
                    items: Vec::new(),
                    first_etag: tag.to_string(),
                    same: true,
                });
            }
            if page == 1 {
                first = res.etag.clone();
            }
            let bad = || WatchError::new(Code::Malformed, "check runs response is not usable");
            let v = json_body(&res, "check runs")?;
            if !v.is_object() && !v.is_null() {
                return Err(bad());
            }
            let total = match &v["total_count"] {
                Value::Null => 0,
                Value::Number(n) => n.as_u64().ok_or_else(bad)?,
                _ => return Err(bad()),
            };
            let arr = match &v["check_runs"] {
                Value::Null => Vec::new(),
                Value::Array(a) => a.clone(),
                _ => return Err(bad()),
            };
            for r in &arr {
                runs.push(run_of(r).ok_or_else(bad)?);
            }
            if arr.is_empty() || runs.len() as u64 >= total {
                break;
            }
        }
        Ok(Half {
            items: runs,
            first_etag: first,
            same: false,
        })
    }

    fn read_statuses(
        &self,
        repo: &str,
        sha: &str,
        etag: &str,
    ) -> Result<Half<StatusContext>, WatchError> {
        let mut stats: Vec<StatusContext> = Vec::new();
        let mut seen = BTreeSet::new();
        let mut first = String::new();
        for page in 1..=MAX_CHECK_PAGES {
            let tag = if page == 1 { etag } else { "" };
            let res = self.r.statuses(repo, sha, page, tag)?;
            if res.not_modified {
                return Ok(Half {
                    items: Vec::new(),
                    first_etag: tag.to_string(),
                    same: true,
                });
            }
            if page == 1 {
                first = res.etag.clone();
            }
            let bad = || WatchError::new(Code::Malformed, "statuses response is not usable");
            let v = json_body(&res, "statuses")?;
            let arr = match &v {
                Value::Null => Vec::new(),
                Value::Array(a) => a.clone(),
                _ => return Err(bad()),
            };
            for s in &arr {
                let o = s.as_object().ok_or_else(bad)?;
                let get = |k: &str| str_of(o.get(k).unwrap_or(&Value::Null)).ok_or_else(bad);
                let (context, state, description) =
                    (get("context")?, get("state")?, get("description")?);
                // newest first: the first entry of a context is its current state
                if seen.insert(context.clone()) {
                    stats.push(StatusContext {
                        context,
                        state,
                        description,
                    });
                }
            }
            if arr.len() < 100 {
                break;
            }
        }
        Ok(Half {
            items: stats,
            first_etag: first,
            same: false,
        })
    }
}

fn graph_refusal(error: &str) -> bool {
    let lower = error.to_ascii_lowercase();
    [
        "http 401",
        "http 403",
        "http 404",
        "forbidden",
        "not accessible",
        "permission",
    ]
    .iter()
    .any(|reason| lower.contains(reason))
}

fn parse_reviews(root: &Value, number: u64) -> Option<Vec<crate::watch::event::Activity>> {
    use crate::watch::event::{Activity, ActivityKind};
    let mut changes = Vec::new();
    for row in root["pullRequest"]["reviews"]["nodes"].as_array()? {
        let Some(submitted) = row["submittedAt"].as_str() else {
            continue;
        };
        changes.push(Activity {
            kind: ActivityKind::Review,
            id: row["id"].as_str()?.into(),
            version: row["state"].as_str()?.into(),
            number,
            url: row["url"].as_str()?.into(),
            summary: format!("{} at {submitted}", row["state"].as_str()?),
        });
        for comment in row["comments"]["nodes"].as_array()? {
            changes.push(Activity {
                kind: ActivityKind::ReviewComment,
                id: comment["id"].as_str()?.into(),
                version: comment["createdAt"].as_str()?.into(),
                number,
                url: comment["url"].as_str()?.into(),
                summary: String::new(),
            });
        }
    }
    Some(changes)
}

fn parse_discussions(root: &Value) -> Option<Vec<crate::watch::event::Activity>> {
    use crate::watch::event::{Activity, ActivityKind};
    root["discussions"]["nodes"]
        .as_array()?
        .iter()
        .map(|row| {
            Some(Activity {
                kind: ActivityKind::Discussion,
                id: row["id"].as_str()?.into(),
                version: format!(
                    "{}:{}:{}",
                    row["comments"]["totalCount"].as_u64()?,
                    row["closed"].as_bool()?,
                    row["updatedAt"].as_str()?
                ),
                number: row["number"].as_u64()?,
                url: row["url"].as_str()?.into(),
                summary: row["title"].as_str()?.into(),
            })
        })
        .collect()
}
