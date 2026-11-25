//! `stw watch`, the core (port package W1): events, accepted state, the transitions `apply`, the journal, the lock and
//! the state directory. Pure logic and files; no network and no clock (times are parameters). The reader is `reader`,
//! the open-list cycle is `cycle`, and the command (`--once`, `--stream`, `--status`) is `command`.
//!
//! The formats are the Rust formats of the port (owner decision D-20261119-05): not compatible with the Go watcher, which
//! is why the state directory id uses its own hash prefix and a Go directory is never opened by mistake. README section
//! "Watch core (port, W1)" documents state.json, journal.jsonl, the lock, the heartbeat and every deviation from Go.
//!
//! Order of a cycle (W3 follows it, as Go `Watcher.Cycle`): the cycle emits an event and applies it to `Core::state` at once
//! (`State::apply`), does its own bookkeeping that is no event, and `Core::commit` then journals each event (fsync), prints
//! it and writes `state.json` with `applied_seq` = `printed_seq` = the last seq. `commit` does not apply.
//!
//! Crash model (the "Replay effects" table of Spec 035): an event is journalled exactly once. After a crash the start replays
//! the journal lines past `applied_seq` into the state (`apply` is idempotent: counters are raised, never lowered, every
//! other effect sets or removes) and prints the lines past `printed_seq` again (at-least-once output).
use std::fmt;
use std::path::{Path, PathBuf};

use event::Event;
use journal::Journal;
use lock::DirLock;
use state::State;
use statedir::{ensure_state_dir, load_state, save_state, write_heartbeat};

pub mod apply;
pub mod command;
pub mod cycle;
pub mod event;
pub mod journal;
pub mod lock;
pub mod reader;
pub mod state;
pub mod statedir;

/// What can go wrong in the core. The command (W4) maps the variants to exit codes (`Locked` is exit 3, the usage
/// variants 2, the rest 1); `into_error` gives the generic error with the matching kind for the rest of `stw`.
#[derive(Debug, PartialEq, Eq)]
pub enum WatchError {
    /// The state directory is not an absolute path.
    StateDirUsage,
    /// The state directory is inside a Git work tree or below a `.stw` directory.
    InsideWorkTree(String),
    /// The state directory does not have mode 0700.
    StateDirMode(String),
    /// `state.json` or `journal.jsonl` cannot be used: unreadable, not valid, behind the journal.
    CorruptState(String),
    /// Another watcher holds the lock of the state directory; the pid is what the holder wrote, if readable.
    Locked(Option<u32>),
    /// A file operation failed.
    Io(String),
}

impl fmt::Display for WatchError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            WatchError::StateDirUsage => {
                f.write_str("the state directory must be an absolute path")
            }
            WatchError::InsideWorkTree(p) => write!(
                f,
                "the state directory must not be inside a Git work tree or a .stw directory: {p}"
            ),
            WatchError::StateDirMode(m) => {
                write!(f, "the state directory must have mode 0700: {m}")
            }
            WatchError::CorruptState(m) => {
                write!(
                    f,
                    "the watch state is corrupt ({m}); use --baseline to replace it"
                )
            }
            WatchError::Locked(Some(pid)) => write!(
                f,
                "another stw watch (pid {pid}) holds the lock of this state directory"
            ),
            WatchError::Locked(None) => {
                f.write_str("another stw watch holds the lock of this state directory")
            }
            WatchError::Io(m) => f.write_str(m),
        }
    }
}

impl std::error::Error for WatchError {}

impl WatchError {
    /// The generic `stw` error: usage for the two state-directory refusals the user fixes with another path, else a
    /// repository (file) error. The message is the same.
    pub fn into_error(self) -> crate::error::Error {
        match self {
            WatchError::StateDirUsage | WatchError::InsideWorkTree(_) => {
                crate::error::Error::usage(self.to_string())
            }
            _ => crate::error::Error::repository(self.to_string()),
        }
    }
}

// ---------------------------------------------------------------------------------------------------------------------
// The session of a state directory: lock, journal and state, the start with replay and the commit of a cycle.

/// The durable steps of the commit order, reported to `Output::after` so a test can stop the process there (a crash).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Step {
    /// The n-th event of the cycle is in the journal (fsynced).
    Append(usize),
    /// The n-th event of the cycle was printed.
    Print(usize),
    /// `state.json` of the cycle was written.
    State,
    /// `state.json` was written after the replay at start.
    ReplayState,
    /// The n-th line was printed again at start.
    ReplayPrint(usize),
}

/// Where events are printed (stdout) and the hook after every durable step. An error from either stops the session as a
/// crash would: it is unusable until the directory is opened again.
pub trait Output {
    fn print(&mut self, ev: &Event) -> Result<(), WatchError>;
    fn after(&mut self, _step: Step) -> Result<(), WatchError> {
        Ok(())
    }
}

/// What the start found and did.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct OpenReport {
    /// The state was missing or unusable and was rebuilt from the journal; the first cycle is a silent baseline.
    pub lost: bool,
    /// Journal lines applied to the state (those past `applied_seq`).
    pub replayed: u64,
    /// Journal lines printed again (those past `printed_seq`).
    pub reprinted: u64,
    /// Bytes of a truncated write at the end of the journal (cut off by the next append).
    pub truncated_tail_bytes: u64,
}

/// An open state directory: the lock, the journal and the accepted state.
pub struct Core {
    pub dir: PathBuf,
    pub state: State,
    /// The next cycle is a silent baseline (forced by the caller, or the state was rebuilt from the journal).
    pub baseline: bool,
    journal: Journal,
    lock: Option<DirLock>,
    broken: bool,
}

impl Core {
    /// Opens `dir` (created with mode 0700 when new, refused inside a Git work tree), takes the lock, replays the journal
    /// into the state and prints again the events that may not have reached the output (at-least-once). Nothing is polled.
    ///
    /// `baseline` forces the first cycle to be a silent baseline and repairs a state that is corrupt or behind the journal;
    /// without it those are `WatchError::CorruptState`.
    pub fn open(
        dir: &Path,
        baseline: bool,
        out: &mut dyn Output,
    ) -> Result<(Core, OpenReport), WatchError> {
        ensure_state_dir(dir)?;
        let lock = lock::acquire(dir)?;
        Core::start(dir, baseline, lock, out)
    }

    fn start(
        dir: &Path,
        baseline: bool,
        lock: DirLock,
        out: &mut dyn Output,
    ) -> Result<(Core, OpenReport), WatchError> {
        let journal = Journal::open(dir)?;
        let tail = journal.tail();
        let mut report = OpenReport {
            truncated_tail_bytes: journal.junk_bytes(),
            ..OpenReport::default()
        };
        let mut lost = false;
        let loaded = match load_state(dir) {
            Ok(s) => s,
            Err(WatchError::CorruptState(_)) if baseline => {
                lost = true;
                None
            }
            Err(e) => return Err(e),
        };
        let mut state = match loaded {
            Some(s) => s,
            None => {
                lost |= tail > 0;
                State::new()
            }
        };
        if state.applied_seq > tail || state.printed_seq > tail {
            if !baseline {
                return Err(WatchError::CorruptState(format!(
                    "the journal ends at {tail}, behind the state"
                )));
            }
            state = State::new();
            lost = true;
        }
        let events = journal.after(state.applied_seq)?;
        for ev in &events {
            state.apply(ev); // the replay: counters and observed effects, idempotent
        }
        report.replayed = events.len() as u64;
        state.applied_seq = tail;
        let mut core = Core {
            dir: dir.to_path_buf(),
            state,
            baseline,
            journal,
            lock: Some(lock),
            broken: false,
        };
        if lost {
            // The journal rebuilt the counters; the observed truth is unknown: baseline silently.
            core.state.printed_seq = tail;
            core.state.repos = None;
            core.state.skipped.clear();
            core.baseline = true;
            report.lost = true;
        }
        if !events.is_empty() || lost {
            save_state(dir, &core.state)?;
            out.after(Step::ReplayState)?;
        }
        let pending = core.journal.after(core.state.printed_seq)?;
        for (i, ev) in pending.iter().enumerate() {
            out.print(ev)?;
            out.after(Step::ReplayPrint(i + 1))?;
        }
        report.reprinted = pending.len() as u64;
        if !pending.is_empty() {
            core.state.printed_seq = tail;
            save_state(dir, &core.state)?;
        }
        Ok((core, report))
    }

    /// `seq` of the last journalled event.
    pub fn tail(&self) -> u64 {
        self.journal.tail()
    }

    /// Commits the events of one cycle: each is appended to the journal and fsynced, then printed, then `state.json` is
    /// written with `applied_seq` = `printed_seq` = the last seq. It does not apply the events (Go: `Watcher.Cycle`): the
    /// cycle applies each event to `state` when it emits it (`State::apply`), then does its own bookkeeping that is no event,
    /// and `state` is what is saved. Only the replay at start applies, for the lines past `applied_seq`. Returns the events as
    /// journalled (with their `seq`). After an error the session is unusable: open the directory again, as after a crash.
    pub fn commit(
        &mut self,
        events: &[Event],
        out: &mut dyn Output,
    ) -> Result<Vec<Event>, WatchError> {
        if self.broken {
            return Err(WatchError::Io(
                "the watch session failed earlier; open the state directory again".to_string(),
            ));
        }
        self.broken = true;
        let mut written = Vec::with_capacity(events.len());
        for (i, ev) in events.iter().enumerate() {
            written.push(self.journal.append(ev)?);
            out.after(Step::Append(i + 1))?;
        }
        for (i, ev) in written.iter().enumerate() {
            out.print(ev)?;
            out.after(Step::Print(i + 1))?;
        }
        let tail = self.journal.tail();
        self.state.applied_seq = tail;
        self.state.printed_seq = tail;
        save_state(&self.dir, &self.state)?;
        out.after(Step::State)?;
        self.broken = false;
        Ok(written)
    }

    /// Writes the heartbeat (`RFC3339 pid mode`); the caller does it after every completed cycle, also after partial failures.
    pub fn heartbeat(&self, now_secs: i64, mode: &str) -> Result<(), WatchError> {
        write_heartbeat(&self.dir, now_secs, std::process::id(), mode)
    }

    /// Releases the lock and the journal.
    pub fn close(mut self) {
        if let Some(l) = self.lock.take() {
            l.release();
        }
    }
}
