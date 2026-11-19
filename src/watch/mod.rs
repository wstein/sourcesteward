//! `stw watch`, the core (port package W1): events, accepted state, the transitions `apply`, the journal, the lock and
//! the state directory. Pure logic and files; no network, no clock (times are parameters), no cycle (W3), no command (W4).
//!
//! The formats are the Rust formats of the port (owner decision D-20261119-05): not compatible with the Go watcher, which
//! is why the state directory id uses its own hash prefix and a Go directory is never opened by mistake. README section
//! "Watch core (port, W1)" documents state.json, journal.jsonl, the lock, the heartbeat and every deviation from Go.
//!
//! Crash model (the "Replay effects" table of Spec 035): an event is journalled exactly once (fsync), then applied to the
//! state (idempotent, `apply`), then printed; `state.json` follows with `applied_seq` = `printed_seq` = the last seq.
//! After a crash the start replays the journal lines past `applied_seq` into the state and prints the lines past
//! `printed_seq` again (at-least-once output).
use std::fmt;

pub mod apply;
pub mod event;
pub mod journal;
pub mod lock;
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
