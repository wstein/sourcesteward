//! The lock of a state directory: an advisory `flock` on the file `lock` (`std::fs::File::try_lock`, no `libc`), so a second
//! watcher in the same directory fails with `WatchError::Locked` (exit 3 in the command, W4). The holder writes its pid into
//! the file; `holder` reads it for `--status`.
use std::fs::{File, OpenOptions, TryLockError};
use std::io::Read;
use std::os::unix::fs::{FileExt, OpenOptionsExt, PermissionsExt};
use std::path::Path;
use std::time::Duration;

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
    let mut raw = Vec::new();
    file.take(32).read_to_end(&mut raw).ok()?;
    std::str::from_utf8(&raw).ok()?.trim().parse().ok()
}

/// Takes the lock with the default retry (about one second) and records this process's pid.
pub fn acquire(dir: &Path) -> Result<DirLock, WatchError> {
    acquire_with(dir, LOCK_ATTEMPTS, LOCK_PAUSE)
}

/// `acquire` with its own number of attempts (at least one) and the pause between them.
pub fn acquire_with(dir: &Path, attempts: u32, pause: Duration) -> Result<DirLock, WatchError> {
    let path = dir.join(LOCK_NAME);
    let file = OpenOptions::new()
        .read(true)
        .write(true)
        .create(true)
        .truncate(false)
        .mode(0o600)
        .open(&path)
        .map_err(|e| WatchError::Io(format!("cannot open {}: {e}", path.display())))?;
    file.set_permissions(std::fs::Permissions::from_mode(0o600))
        .map_err(|e| WatchError::Io(format!("cannot set the mode of {}: {e}", path.display())))?;
    for i in 0..attempts.max(1) {
        match file.try_lock() {
            Ok(()) => {
                file.set_len(0)
                    .and_then(|()| {
                        file.write_all_at(format!("{}\n", std::process::id()).as_bytes(), 0)
                    })
                    .map_err(|e| WatchError::Io(format!("cannot write {}: {e}", path.display())))?;
                return Ok(DirLock { file });
            }
            Err(TryLockError::WouldBlock) => {
                if i + 1 < attempts.max(1) {
                    std::thread::sleep(pause);
                }
            }
            Err(TryLockError::Error(e)) => {
                return Err(WatchError::Io(format!(
                    "cannot lock {}: {e}",
                    path.display()
                )));
            }
        }
    }
    Err(WatchError::Locked(pid_of(&file)))
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
    let file = File::open(dir.join(LOCK_NAME)).ok()?;
    match file.try_lock_shared() {
        Ok(()) => {
            let _ = file.unlock();
            None
        }
        Err(TryLockError::WouldBlock) => Some(pid_of(&file)),
        Err(TryLockError::Error(_)) => None,
    }
}
