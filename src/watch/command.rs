//! `stw watch` (W4): one poll (`--once`), a resident poll (`--stream`), or a read of the
//! heartbeat (`--status`). `--stream` holds the lock and repeats the open-list cycle until
//! SIGINT, SIGTERM, or `--max-idle`. Checks, mergeability, restack, and `--until-event` are
//! not this command. Exit 0 when a poll recorded its outcome or a stream ended on a signal or
//! idle, 1 when every repository read failed (`--once`) or the state could not be used, 2 for
//! usage, 3 when another watcher holds the lock, 5 when `--status` finds no fresh heartbeat.

use std::io::Write;
use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, Instant};

use super::cycle::{self, Poll};
use super::event::Event;
use super::reader::{self, HttpGet};
use super::statedir::{self, IdSource};
use super::{Core, Output, WatchError};

pub const EXIT_FAILED: u8 = 1;
pub const EXIT_USAGE: u8 = 2;
pub const EXIT_LOCKED: u8 = 3;
pub const EXIT_STALE: u8 = 5;

/// Smallest `--interval`. Matches the Go watcher.
pub const MIN_INTERVAL_SECS: u64 = 10;
/// `--stream` interval when the flag is omitted.
pub const DEFAULT_INTERVAL_SECS: u64 = 60;

static STREAM_STOP: AtomicBool = AtomicBool::new(false);

struct Lines<W> {
    out: W,
}

impl<W: Write> Output for Lines<W> {
    fn print(&mut self, ev: &Event) -> Result<(), WatchError> {
        writeln!(self.out, "{}", ev.to_line()).map_err(|e| WatchError::Io(e.to_string()))
    }
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
    /// Arms SIGINT and SIGTERM, then clears a stop left by an earlier stream in this process.
    pub fn install() -> Result<LivePace, WatchError> {
        arm_stop()?;
        STREAM_STOP.store(false, Ordering::SeqCst);
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
            if STREAM_STOP.load(Ordering::Relaxed) {
                return false;
            }
            let left = end.saturating_duration_since(Instant::now());
            std::thread::sleep(left.min(Duration::from_millis(200)));
        }
        !STREAM_STOP.load(Ordering::Relaxed)
    }
}

extern "C" fn handle_stop(_: libc::c_int) {
    STREAM_STOP.store(true, Ordering::Relaxed);
}

fn arm_stop() -> Result<(), WatchError> {
    unsafe {
        let mut action: libc::sigaction = std::mem::zeroed();
        action.sa_sigaction = handle_stop as *const () as libc::sighandler_t;
        action.sa_flags = libc::SA_RESTART;
        if libc::sigemptyset(&mut action.sa_mask) != 0 {
            return Err(WatchError::Io("cannot clear the signal mask".to_string()));
        }
        for sig in [libc::SIGINT, libc::SIGTERM] {
            if libc::sigaction(sig, &action, std::ptr::null_mut()) != 0 {
                return Err(WatchError::Io(
                    "cannot install the stop handler".to_string(),
                ));
            }
        }
    }
    Ok(())
}

/// `--interval` and `--max-idle`. `max_idle_secs` of 0 is off.
pub struct StreamLimits {
    pub baseline: bool,
    pub api_base: String,
    pub interval_secs: u64,
    pub max_idle_secs: u64,
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
    http: &dyn HttpGet,
    api_base: &str,
    out: &mut dyn Write,
) -> Result<u8, WatchError> {
    if repos.is_empty() {
        return Err(WatchError::StateDirUsage);
    }
    let mut lines = Lines { out };
    let (mut core, _) = Core::open(dir, baseline, &mut lines)?;
    let polls = poll_all(repos, http, api_base);
    let failed = polls.iter().filter(|p| p.result.is_err()).count();
    prepare_cycle(&mut core, repos, baseline, 0);
    let events = cycle::advance(&mut core.state, &polls, now_secs);
    let committed = core.commit(&events, &mut lines);
    if let Err(err) = committed {
        core.close();
        return Err(err);
    }
    let beat = core.heartbeat(now_secs, "once");
    core.close();
    beat?;
    if failed == repos.len() {
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
    http: &dyn HttpGet,
    out: &mut dyn Write,
    limits: StreamLimits,
    pace: &mut dyn Pace,
) -> Result<u8, WatchError> {
    if repos.is_empty() {
        return Err(WatchError::StateDirUsage);
    }
    let mut lines = Lines { out };
    let (mut core, _) = Core::open(dir, limits.baseline, &mut lines)?;
    let mut force = limits.baseline || core.baseline;
    let mut last_event = pace.now_secs();
    loop {
        let now = pace.now_secs();
        if idle_elapsed(limits.max_idle_secs, now, last_event) {
            core.close();
            return Ok(0);
        }
        let polls = poll_ready(repos, http, &limits.api_base, &core.state, now);
        // The hold starts when the response arrives. A slow read must not spend
        // `Retry-After` on time that already passed inside the request.
        let arrived = pace.now_secs();
        prepare_cycle(&mut core, repos, force, limits.interval_secs);
        force = false;
        let events = cycle::advance(&mut core.state, &polls, arrived);
        if let Err(err) = core.commit(&events, &mut lines) {
            core.close();
            return Err(err);
        }
        if let Err(err) = core.heartbeat(arrived, "stream") {
            core.close();
            return Err(err);
        }
        if !events.is_empty() {
            last_event = arrived;
        }
        let wait = stream_wait(
            limits.interval_secs,
            limits.max_idle_secs,
            arrived,
            last_event,
        );
        if wait == 0 || !pace.wait(wait) {
            core.close();
            return Ok(0);
        }
    }
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
    http: &dyn HttpGet,
    api_base: &str,
    state: &super::state::State,
    now: i64,
) -> Vec<Poll> {
    let due: Vec<String> = repos
        .iter()
        .filter(|repo| !retry_holds(state, repo, now))
        .cloned()
        .collect();
    poll_all(&due, http, api_base)
}

fn retry_holds(state: &super::state::State, repo: &str, now: i64) -> bool {
    state
        .health
        .get(repo)
        .and_then(|h| crate::github::parse_utc(&h.next_try))
        .is_some_and(|until| now < until)
}

fn poll_all(repos: &[String], http: &dyn HttpGet, api_base: &str) -> Vec<Poll> {
    repos
        .iter()
        .map(|repo| Poll {
            repo: repo.clone(),
            result: reader::read_open_pulls(http, api_base, repo),
        })
        .collect()
}

/// Records the watched set for `--status` and, when `force` is set, makes the next successful
/// read a silent replacement of the open list.
fn prepare_cycle(core: &mut Core, repos: &[String], force: bool, interval_secs: u64) {
    if force || core.baseline {
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
