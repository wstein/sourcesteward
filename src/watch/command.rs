//! `stw watch` (W4): one poll (`--once`) or a read of the heartbeat (`--status`).
//! The continuous stream is not this command yet. Exit 0 when a poll recorded its outcome,
//! 1 when every repository read failed or the state could not be used, 2 for usage, 3 when
//! another watcher holds the lock, 5 when `--status` finds no fresh heartbeat.

use std::io::Write;
use std::path::Path;

use super::cycle::{self, Poll};
use super::event::Event;
use super::reader::{self, HttpGet};
use super::statedir::{self, IdSource};
use super::{Core, Output, WatchError};

pub const EXIT_FAILED: u8 = 1;
pub const EXIT_USAGE: u8 = 2;
pub const EXIT_LOCKED: u8 = 3;
pub const EXIT_STALE: u8 = 5;

struct Lines<W> {
    out: W,
}

impl<W: Write> Output for Lines<W> {
    fn print(&mut self, ev: &Event) -> Result<(), WatchError> {
        writeln!(self.out, "{}", ev.to_line()).map_err(|e| WatchError::Io(e.to_string()))
    }
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
    let polls: Vec<Poll> = repos
        .iter()
        .map(|repo| Poll {
            repo: repo.clone(),
            result: reader::read_open_pulls(http, api_base, repo),
        })
        .collect();
    let failed = polls.iter().filter(|p| p.result.is_err()).count();
    let events = cycle::advance(&mut core.state, &polls, now_secs);
    core.commit(&events, &mut lines)?;
    core.heartbeat(now_secs, "once")?;
    core.close();
    if failed == repos.len() {
        Ok(EXIT_FAILED)
    } else {
        Ok(0)
    }
}

/// `--status`: the heartbeat only. No network and no lock.
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
