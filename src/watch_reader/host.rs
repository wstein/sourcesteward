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
        let mut cond = cond.cloned().unwrap_or_default();
        let list = match self.r.list_open_pulls(repo, &cond.list, self.max_open) {
            Ok(l) => l,
            Err(e) => return Err(self.fail(e, cancel)),
        };
        cond.list = list.state.clone();
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
        })
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
