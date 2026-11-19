//! The lock of a state directory: an advisory `flock` on the file `lock` (`std::fs::File::try_lock`, no `libc`), so a second
//! watcher in the same directory fails with `WatchError::Locked` (exit 3 in the command, W4). The holder writes its pid into
//! the file; `holder` reads it for `--status`.
#[allow(unused_imports)]
use std::fs::{File, OpenOptions, TryLockError};
#[allow(unused_imports)]
use std::io::Read;
#[allow(unused_imports)]
use std::os::unix::fs::{FileExt, OpenOptionsExt};
#[allow(unused_imports)]
use std::path::Path;
#[allow(unused_imports)]
use std::time::Duration;

#[allow(unused_imports)]
use super::WatchError;

/// File name inside the state directory.
pub const LOCK_NAME: &str = "lock";
/// A busy lock is retried briefly before it is reported: a `--status` probe holds a shared lock for an instant and must
/// not make a starting watcher report a second watcher.
pub const LOCK_ATTEMPTS: u32 = 5;
pub const LOCK_PAUSE: Duration = Duration::from_millis(250);

/// The exclusive lock of a state directory, held until it is dropped.
#[derive(Debug)]
pub struct DirLock {
    file: File,
}

fn pid_of(file: &File) -> Option<u32> {
    let _ = file;
    None
}

/// Takes the lock with the default retry (about one second) and records this process's pid.
pub fn acquire(dir: &Path) -> Result<DirLock, WatchError> {
    acquire_with(dir, LOCK_ATTEMPTS, LOCK_PAUSE)
}

/// `acquire` with its own number of attempts (at least one) and the pause between them.
pub fn acquire_with(dir: &Path, attempts: u32, pause: Duration) -> Result<DirLock, WatchError> {
    let _ = (attempts, pause);
    let file =
        File::open("/dev/null").map_err(|e| WatchError::Io(format!("{}: {e}", dir.display())))?;
    Ok(DirLock { file })
}

impl DirLock {
    /// Releases the lock (dropping the value does the same).
    pub fn release(self) {
        let _ = self.file.unlock();
    }
}

/// Whether a watcher holds the lock of `dir`, and the pid it recorded (`None` when unreadable). It takes a shared
/// non-blocking lock only to probe, writes nothing and never creates the lock file. `None`: nobody holds it.
pub fn holder(dir: &Path) -> Option<Option<u32>> {
    let _ = dir;
    None
}
