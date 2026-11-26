//! `stw watch` (W4): one poll (`--once`), a resident poll (`--stream`), or a read of the
//! heartbeat (`--status`). `--stream` holds the lock and repeats the open-list cycle until
//! SIGINT, SIGTERM, or `--max-idle`. `--until-event` stops on a selected event or timeout. Exit 0 when a poll recorded its outcome or a stream ended on a signal or
//! idle, 1 when every repository read failed (`--once`) or the state could not be used, 2 for
//! usage, 3 when another watcher holds the lock, 4 on event timeout, and 5 when `--status` finds no fresh heartbeat.

use std::io::Write;
use std::path::Path;
use std::sync::atomic::AtomicBool;
use std::time::{Duration, Instant};

use super::cycle::{self, Seen};
use super::event::Event;
use super::statedir::{self, IdSource};
use super::{Core, Output, WatchError};
use crate::watch_reader::cond_store::CondStore;
use crate::watch_reader::host::{HostObserver, WatchRead};
use crate::watch_reader::obs::{Hints, KnownPr, ObserveFailure, RepoObservation};

pub const EXIT_FAILED: u8 = 1;
pub const EXIT_USAGE: u8 = 2;
pub const EXIT_LOCKED: u8 = 3;
pub const EXIT_STALE: u8 = 5;
/// `--until-event` reached its timeout.
pub const EXIT_TIMEOUT: u8 = 4;
/// Go's cap when a low rate limit slows the stream.
const SLOW_CAP_SECS: u64 = 600;
/// Go `DefaultPendingTimeout`. Zero disables `checks-pending-timeout`.
const PENDING_TIMEOUT_SECS: u64 = 45 * 60;

/// Smallest `--interval`. Matches the Go watcher.
pub const MIN_INTERVAL_SECS: u64 = 10;
/// `--stream` interval when the flag is omitted.
pub const DEFAULT_INTERVAL_SECS: u64 = 60;

pub struct Lines<W> {
    out: W,
    human: bool,
    diagnostics: Option<(u8, Box<dyn Write + Send + Sync>)>,
}

impl<W: Write> Lines<W> {
    pub fn new(out: W, human: bool) -> Self {
        Self {
            out,
            human,
            diagnostics: None,
        }
    }

    pub fn with_diagnostics(
        mut self,
        level: u8,
        writer: impl Write + Send + Sync + 'static,
    ) -> Self {
        self.diagnostics = Some((level, Box::new(writer)));
        self
    }
}

impl<W: Write> Output for Lines<W> {
    fn diagnostics(&mut self, level: u8) -> Option<&mut dyn Write> {
        let (verbosity, writer) = self.diagnostics.as_mut()?;
        (*verbosity >= level).then_some(writer.as_mut())
    }

    fn print(&mut self, ev: &Event) -> Result<(), WatchError> {
        let line = if self.human {
            super::human::render(ev)
        } else {
            ev.to_line()
        };
        writeln!(self.out, "{line}").map_err(|e| WatchError::Io(e.to_string()))
    }
}

fn diagnostic(out: &mut dyn Output, level: u8, message: std::fmt::Arguments<'_>) {
    if let Some(writer) = out.diagnostics(level) {
        let _ = writeln!(writer, "stw watch: {message}");
    }
}

fn poll_report(
    out: &mut dyn Output,
    polls: &[Seen],
    state: &super::state::State,
    events: &[Event],
    duration: Duration,
) {
    for poll in polls {
        let Some(issues) = poll
            .result
            .as_ref()
            .ok()
            .and_then(|obs| obs.issues.as_ref())
        else {
            continue;
        };
        if issues.coverage != "complete" {
            diagnostic(
                out,
                0,
                format_args!(
                    "{} issue list incomplete; retaining previous snapshot",
                    poll.repo
                ),
            );
        }
    }
    if out.diagnostics(1).is_none() {
        return;
    }
    let mut observed = 0;
    let mut totals = std::collections::BTreeMap::new();
    let mut count = |label, amount| *totals.entry(label).or_insert(0) += amount;
    for poll in polls {
        match &poll.result {
            Ok(obs) => {
                observed += 1;
                if let Some(issues) = obs
                    .issues
                    .as_ref()
                    .filter(|issues| issues.coverage == "complete")
                {
                    count("open issue(s)", issues.issues.len());
                }
                if obs.workflows.as_ref().is_some_and(Result::is_ok) {
                    count("active workflow run(s)", obs.active_workflows);
                }
                for (scope, result) in &obs.activity {
                    use super::event::ActivityKind;
                    use crate::watch_reader::obs::ActivityScope;
                    let Ok(activity) = result else {
                        continue;
                    };
                    match scope {
                        ActivityScope::Reviews(_) => {
                            let reviews = activity
                                .iter()
                                .filter(|item| item.kind == ActivityKind::Review)
                                .count();
                            count("recent review(s)", reviews);
                            count("recent inline review comment(s)", activity.len() - reviews);
                        }
                        ActivityScope::Discussions => count("recent discussion(s)", activity.len()),
                        ActivityScope::Wiki => count("wiki head(s)", activity.len()),
                    }
                }
                if !obs.skip_pr_events {
                    count("open PR(s)", obs.prs.len());
                    diagnostic(
                        out,
                        2,
                        format_args!("{} observed {} open PR(s)", poll.repo, obs.prs.len()),
                    );
                    if out.diagnostics(2).is_some() {
                        for pr in &obs.prs {
                            if let Some(candidate) = state
                                .repo
                                .get(&poll.repo)
                                .and_then(|repo| repo.prs.get(&pr.number))
                                .and_then(|stored| stored.class_candidate.as_ref())
                            {
                                diagnostic(
                                    out,
                                    2,
                                    format_args!(
                                        "{}#{} checks {} awaiting confirmation",
                                        poll.repo,
                                        pr.number,
                                        class_name(candidate)
                                    ),
                                );
                            }
                        }
                    }
                }
            }
            Err(ObserveFailure::Error(err)) => {
                diagnostic(
                    out,
                    1,
                    format_args!(
                        "{} read failed ({}) retry hint {}s",
                        poll.repo,
                        err.code.as_str(),
                        err.retry_in
                    ),
                );
                if let Some(health) = state
                    .health
                    .get(&poll.repo)
                    .filter(|health| !health.next_try.is_empty())
                {
                    diagnostic(
                        out,
                        1,
                        format_args!("{} backoff until {}", poll.repo, health.next_try),
                    );
                }
            }
            Err(ObserveFailure::Canceled) => {
                diagnostic(out, 2, format_args!("{} read canceled", poll.repo))
            }
        }
    }
    for event in events {
        diagnostic(
            out,
            2,
            format_args!("emitted {} ({:?})", event.kind().as_str(), event.id),
        );
    }
    let mut summary = String::new();
    for (label, count) in totals {
        use std::fmt::Write as _;
        let _ = write!(summary, ", {count} {label}");
    }
    diagnostic(
        out,
        1,
        format_args!(
            "poll completed in {}ms, {} of {} repository reads succeeded{summary}, {} event(s)",
            duration.as_millis(),
            observed,
            polls.len(),
            events.len()
        ),
    );
}

/// Clock and wait of a stream. A test supplies one that jumps the clock. `wait` returns false
/// when the stream should end (signal).
pub trait Pace {
    fn now_secs(&mut self) -> i64;
    fn wait(&mut self, seconds: u64) -> bool;
}

/// Wall clock. SIGINT and SIGTERM end the next wait.
pub struct LivePace;

impl LivePace {
    /// Arms SIGINT and SIGTERM through the git runner, the only file that may name libc.
    pub fn install() -> Result<LivePace, WatchError> {
        crate::signals::arm_watch_stop().map_err(WatchError::Io)?;
        Ok(LivePace)
    }
}

impl Pace for LivePace {
    fn now_secs(&mut self) -> i64 {
        crate::tidy::now_secs()
    }

    fn wait(&mut self, seconds: u64) -> bool {
        let end = Instant::now() + Duration::from_secs(seconds);
        while Instant::now() < end {
            if crate::signals::watch_stopped() {
                return false;
            }
            let left = end.saturating_duration_since(Instant::now());
            std::thread::sleep(left.min(Duration::from_millis(200)));
        }
        !crate::signals::watch_stopped()
    }
}

/// One repository observation. `HostObserver` is the production source.
pub trait WatchSource {
    fn observe_repo(
        &mut self,
        repo: &str,
        hints: &Hints,
        cond: Option<&CondStore>,
    ) -> Result<RepoObservation, ObserveFailure>;

    /// The reader's rate-limit policy after a cycle. `true` means the next wait uses a doubled interval.
    fn after_cycle(&mut self) -> bool {
        false
    }
}

impl<R: WatchRead> WatchSource for HostObserver<R> {
    fn observe_repo(
        &mut self,
        repo: &str,
        hints: &Hints,
        cond: Option<&CondStore>,
    ) -> Result<RepoObservation, ObserveFailure> {
        self.observe(repo, hints, cond, &AtomicBool::new(false))
    }

    fn after_cycle(&mut self) -> bool {
        HostObserver::after_cycle(self)
    }
}

/// `--interval` and `--max-idle`. `max_idle_secs` of 0 is off.
pub struct StreamLimits {
    pub baseline: bool,
    /// Kept for callers that still name the API host. The reader carries its own base.
    #[allow(dead_code)]
    pub api_base: String,
    pub interval_secs: u64,
    pub max_idle_secs: u64,
    /// Cycles between detail refreshes. The observer default is 10.
    pub detail_every: u64,
    /// Event kinds that end `--until-event` with exit 0.
    pub until: Vec<String>,
    /// Exit 4 when this many seconds pass with no matching event. 0 is off.
    pub timeout_secs: u64,
}

/// A Go duration (`60s`, `1m30s`, `0`) as whole seconds. A missing unit is an error.
pub fn parse_duration_secs(text: &str) -> Result<u64, String> {
    if text == "0" {
        return Ok(0);
    }
    if text.is_empty() {
        return Err(format!("{text:?} is not a duration"));
    }
    let mut rest = text;
    let mut total: u64 = 0;
    while !rest.is_empty() {
        let n_end = rest
            .find(|c: char| !c.is_ascii_digit())
            .unwrap_or(rest.len());
        if n_end == 0 {
            return Err(format!("{text:?} is not a duration"));
        }
        let (num, tail) = rest.split_at(n_end);
        let n: u64 = num
            .parse()
            .map_err(|_| format!("{text:?} is not a duration"))?;
        let (factor, unit_len) = match tail.chars().next() {
            Some('s') => (1_u64, 1),
            Some('m') => (60, 1),
            Some('h') => (3_600, 1),
            _ => return Err(format!("{text:?} is not a duration: the unit is missing")),
        };
        let step = n
            .checked_mul(factor)
            .ok_or_else(|| format!("{text:?} is not a duration"))?;
        total = total
            .checked_add(step)
            .ok_or_else(|| format!("{text:?} is not a duration"))?;
        rest = &tail[unit_len..];
    }
    Ok(total)
}

/// One `--once` run. `http` reads each repository. The state directory must be absolute.
pub fn run_once(
    dir: &Path,
    repos: &[String],
    baseline: bool,
    now_secs: i64,
    detail_every: u64,
    source: &mut dyn WatchSource,
    out: &mut dyn Write,
) -> Result<u8, WatchError> {
    run_once_output(
        dir,
        repos,
        baseline,
        now_secs,
        detail_every,
        source,
        &mut Lines::new(out, false),
    )
}

pub fn run_once_output(
    dir: &Path,
    repos: &[String],
    baseline: bool,
    now_secs: i64,
    detail_every: u64,
    source: &mut dyn WatchSource,
    out: &mut dyn Output,
) -> Result<u8, WatchError> {
    if repos.is_empty() {
        return Err(WatchError::StateDirUsage);
    }
    let (mut core, _) = Core::open(dir, baseline, out)?;
    diagnostic(
        out,
        1,
        format_args!("once repositories={repos:?} state={dir:?}"),
    );
    let poll_started = Instant::now();
    let polls = poll_ready(
        repos,
        source,
        &core.state,
        now_secs,
        detail_every,
        baseline || core.baseline,
        out,
    );
    let failed = polls.iter().filter(|p| p.result.is_err()).count();
    let succeeded = polls.iter().filter(|p| p.result.is_ok()).count();
    prepare_cycle(&mut core, repos, baseline, 0);
    let events = cycle::advance_seen(&mut core.state, &polls, now_secs, PENDING_TIMEOUT_SECS);
    let committed = core.commit(&events, out);
    if let Err(err) = committed {
        core.close();
        return Err(err);
    }
    poll_report(out, &polls, &core.state, &events, poll_started.elapsed());
    let beat = core.heartbeat(now_secs, "once");
    core.close();
    beat?;
    let stopped_early = polls.len() < repos.len() && failed > 0 && succeeded == 0;
    if failed == repos.len() || stopped_early {
        Ok(EXIT_FAILED)
    } else {
        Ok(0)
    }
}

/// `--stream`: poll until `pace.wait` reports a stop, or until `--max-idle` elapses with no
/// event. A silent baseline does not count as an event. The lock is held for the whole run.
/// A signal is exit 0, as is a reached idle limit.
pub fn run_stream(
    dir: &Path,
    repos: &[String],
    source: &mut dyn WatchSource,
    out: &mut dyn Write,
    limits: StreamLimits,
    pace: &mut dyn Pace,
) -> Result<u8, WatchError> {
    run_stream_output(
        dir,
        repos,
        source,
        &mut Lines::new(out, false),
        limits,
        pace,
    )
}

pub fn run_stream_output(
    dir: &Path,
    repos: &[String],
    source: &mut dyn WatchSource,
    out: &mut dyn Output,
    limits: StreamLimits,
    pace: &mut dyn Pace,
) -> Result<u8, WatchError> {
    if repos.is_empty() {
        return Err(WatchError::StateDirUsage);
    }
    let (mut core, _) = Core::open(dir, limits.baseline, out)?;
    diagnostic(
        out,
        1,
        format_args!(
            "stream repositories={repos:?} state={dir:?} interval={}s",
            limits.interval_secs
        ),
    );
    let mut force = limits.baseline || core.baseline;
    let mut last_event = pace.now_secs();
    let started = last_event;
    loop {
        let now = pace.now_secs();
        if limits.timeout_secs > 0 {
            let elapsed = u64::try_from(now.saturating_sub(started)).unwrap_or(u64::MAX);
            if elapsed >= limits.timeout_secs {
                core.close();
                return Ok(EXIT_TIMEOUT);
            }
        }
        if idle_elapsed(limits.max_idle_secs, now, last_event) {
            core.close();
            return Ok(0);
        }
        diagnostic(
            out,
            1,
            format_args!("poll started at {}", super::event::format_utc(now)),
        );
        let poll_started = Instant::now();
        let polls = poll_ready(
            repos,
            source,
            &core.state,
            now,
            limits.detail_every,
            force,
            out,
        );
        // The hold starts when the response arrives. A slow read must not spend
        // `Retry-After` on time that already passed inside the request.
        let arrived = pace.now_secs();
        prepare_cycle(&mut core, repos, force, limits.interval_secs);
        force = false;
        let events = cycle::advance_seen(&mut core.state, &polls, arrived, PENDING_TIMEOUT_SECS);
        if let Err(err) = core.commit(&events, out) {
            core.close();
            return Err(err);
        }
        if let Err(err) = core.heartbeat(arrived, "stream") {
            core.close();
            return Err(err);
        }
        poll_report(out, &polls, &core.state, &events, poll_started.elapsed());
        if !events.is_empty() {
            last_event = arrived;
        }
        if !limits.until.is_empty()
            && events
                .iter()
                .any(|ev| limits.until.iter().any(|kind| kind == ev.kind().as_str()))
        {
            core.close();
            return Ok(0);
        }
        let slow = source.after_cycle();
        let mut wait = stream_wait(
            slowed_interval(limits.interval_secs, slow),
            limits.max_idle_secs,
            arrived,
            last_event,
        );
        if limits.timeout_secs > 0 {
            let elapsed = u64::try_from(arrived.saturating_sub(started)).unwrap_or(u64::MAX);
            let left = limits.timeout_secs.saturating_sub(elapsed);
            if left == 0 {
                core.close();
                return Ok(EXIT_TIMEOUT);
            }
            wait = wait.min(left);
        }
        if wait > 0 {
            let reason = if slow { " (low rate limit)" } else { "" };
            diagnostic(out, 1, format_args!("next poll in {wait}s{reason}"));
        }
        if wait == 0 || !pace.wait(wait) {
            core.close();
            return Ok(0);
        }
    }
}

fn slowed_interval(interval_secs: u64, slow: bool) -> u64 {
    if !slow {
        return interval_secs;
    }
    interval_secs
        .saturating_mul(2)
        .min(interval_secs.max(SLOW_CAP_SECS))
}

fn idle_elapsed(max_idle_secs: u64, now: i64, last_event: i64) -> bool {
    if max_idle_secs == 0 {
        return false;
    }
    let idle_for = u64::try_from(now.saturating_sub(last_event)).unwrap_or(u64::MAX);
    idle_for >= max_idle_secs
}

/// The sleep until the next poll. Zero means `--max-idle` has already won, so the caller
/// exits without another request.
fn stream_wait(interval_secs: u64, max_idle_secs: u64, now: i64, last_event: i64) -> u64 {
    if max_idle_secs == 0 {
        return interval_secs;
    }
    let idle_for = u64::try_from(now.saturating_sub(last_event)).unwrap_or(u64::MAX);
    max_idle_secs.saturating_sub(idle_for).min(interval_secs)
}

/// Repositories whose `next_try` is still ahead of `now` are left out. A rate-limit answer
/// sets that deadline; polling them early would ignore `Retry-After`.
fn poll_ready(
    repos: &[String],
    source: &mut dyn WatchSource,
    state: &super::state::State,
    now: i64,
    detail_every: u64,
    baseline: bool,
    diagnostics: &mut dyn Output,
) -> Vec<Seen> {
    let mut out = Vec::new();
    for repo in repos {
        if retry_holds(state, repo, now) {
            diagnostic(
                diagnostics,
                1,
                format_args!(
                    "{repo} skipped, backoff until {}",
                    state.health[repo].next_try
                ),
            );
            continue;
        }
        let hints = hints_of(state, repo, detail_every);
        let saved = saved_cond(state, repo);
        diagnostic(diagnostics, 2, format_args!("{repo} reading GitHub"));
        let result = source.observe_repo(repo, &hints, saved.as_ref());
        if let Some(obs) = result.as_ref().ok().filter(|obs| !obs.skip_pr_events) {
            let previous = state.repo.get(repo);
            if baseline || !previous.is_some_and(|repo| repo.baselined) {
                diagnostic(diagnostics, 1, format_args!("{repo} silent baseline"));
            } else if diagnostics.diagnostics(2).is_some() {
                for pr in &obs.prs {
                    if let Some(old) = previous.and_then(|repo| repo.prs.get(&pr.number)) {
                        let head = if old.head == pr.head {
                            "unchanged"
                        } else {
                            "changed"
                        };
                        let readiness = if old.draft == pr.draft {
                            "unchanged"
                        } else if pr.draft {
                            "ready -> draft"
                        } else {
                            "draft -> ready"
                        };
                        diagnostic(
                            diagnostics,
                            2,
                            format_args!("{repo}#{} head {head}, readiness {readiness}", pr.number),
                        );
                    }
                }
            }
        }
        let run_wide = matches!(
            &result,
            Err(ObserveFailure::Error(err)) if err.run_wide
        );
        out.push(Seen {
            repo: repo.clone(),
            result,
        });
        if run_wide {
            diagnostic(
                diagnostics,
                1,
                format_args!("remaining reads stopped after a run-wide error"),
            );
            break;
        }
    }
    out
}

fn class_name(class: &super::state::Class) -> String {
    match class {
        super::state::Class::Unobserved => String::new(),
        super::state::Class::NoChecks => "none".into(),
        super::state::Class::Pending => "pending".into(),
        super::state::Class::Green => "green".into(),
        super::state::Class::Red => "red".into(),
    }
}

fn hints_of(state: &super::state::State, repo: &str, detail_every: u64) -> Hints {
    let Some(rs) = state.repo.get(repo) else {
        return Hints {
            cycle: state.cycle,
            detail_every,
            known: Vec::new(),
        };
    };
    Hints {
        cycle: state.cycle,
        detail_every,
        known: rs
            .prs
            .values()
            .map(|p| KnownPr {
                number: p.number,
                head: p.head.clone(),
                class: class_name(&p.class),
                class_candidate: p
                    .class_candidate
                    .as_ref()
                    .map(class_name)
                    .unwrap_or_default(),
                detail_due: p.detail_due,
            })
            .collect(),
    }
}

fn saved_cond(state: &super::state::State, repo: &str) -> Option<CondStore> {
    let value = state.repo.get(repo)?.conditional.as_ref()?;
    CondStore::from_json(value).ok()
}

fn retry_holds(state: &super::state::State, repo: &str, now: i64) -> bool {
    state
        .health
        .get(repo)
        .and_then(|h| crate::github::parse_utc(&h.next_try))
        .is_some_and(|until| now < until)
}

/// Records the watched set for `--status` and, when `force` is set, makes the next successful
/// read a silent replacement of the open list.
fn prepare_cycle(core: &mut Core, repos: &[String], force: bool, interval_secs: u64) {
    if force || core.baseline {
        for activity in core.state.activity.values_mut() {
            activity.baselined = false;
        }
        for workflows in core.state.workflows.values_mut() {
            workflows.baselined = false;
        }
        for issues in core.state.issues.values_mut() {
            issues.baselined = false;
        }
        for rs in core.state.repo.values_mut() {
            rs.baselined = false;
            rs.restack_due.clear();
            rs.restack_done.clear();
        }
        core.baseline = false;
    }
    core.state.repos = Some(repos.to_vec());
    if interval_secs > 0 {
        core.state.interval_seconds = interval_secs;
    }
}

/// `--status`: the heartbeat and the accepted repository list. No network and no lock.
pub fn run_status(dir: &Path, now_secs: i64, out: &mut dyn Write) -> Result<u8, WatchError> {
    let status = statedir::read_status(dir, now_secs)?;
    let fresh = status.fresh;
    serde_json::to_writer(&mut *out, &status).map_err(|e| WatchError::Io(e.to_string()))?;
    writeln!(out).map_err(|e| WatchError::Io(e.to_string()))?;
    if fresh {
        Ok(0)
    } else {
        Ok(EXIT_STALE)
    }
}

pub fn state_dir_for_repos(repos: &[String]) -> std::path::PathBuf {
    let host = std::env::var("HOSTNAME").unwrap_or_else(|_| "localhost".into());
    let id = statedir::workspace_id(&host, IdSource::Repos(repos));
    let xdg = std::env::var("XDG_STATE_HOME").ok();
    let home = std::env::var("HOME").ok();
    statedir::default_state_dir(&id, xdg.as_deref(), home.as_deref())
}

pub fn watch_error_exit(err: WatchError) -> u8 {
    match err {
        WatchError::Locked(_) => EXIT_LOCKED,
        WatchError::StateDirUsage | WatchError::InsideWorkTree(_) => EXIT_USAGE,
        _ => EXIT_FAILED,
    }
}

#[test]
fn a_stored_class_candidate_reaches_the_hints() {
    use super::state::{Class, PrState, RepoState, State};

    let mut state = State::new();
    let pr = PrState {
        number: 1,
        class: Class::Green,
        class_candidate: Some(Class::Red),
        detail_due: true,
        ..PrState::default()
    };
    let mut rs = RepoState::new("2026-11-25T00:00:00Z");
    rs.prs.insert(1, pr);
    state.repo.insert("o/r".into(), rs);
    let hints = hints_of(&state, "o/r", 10);
    assert_eq!(hints.known.len(), 1);
    assert_eq!(hints.known[0].class, "green");
    assert_eq!(hints.known[0].class_candidate, "red");
    assert!(hints.known[0].detail_due);
}
