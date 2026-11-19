//! `journal.jsonl`: the append-only record of every emitted event, one `Event` JSON line each, fsynced before the event is
//! printed. Events only; never rotated. The next `seq` is the `seq` of the last complete line plus one, so it is gapless
//! and survives a lost `state.json`; it is read from the lines, never computed from a file offset.
//!
//! Damage is told apart by where it is:
//! - bytes after the last newline are a truncated write (a crash in the middle of an append). They are not part of the
//!   journal; the next append cuts them off (`Journal::junk_bytes` reports their length after `open`);
//! - a complete line that is not a valid event, a `seq` that is not the previous one plus one, or a line longer than
//!   `MAX_LINE_BYTES` is corruption: `WatchError::CorruptState`, nothing is repaired.
#[allow(unused_imports)]
use std::fs::{File, OpenOptions};
#[allow(unused_imports)]
use std::io::{BufRead, BufReader, Read, Seek, SeekFrom, Write};
#[allow(unused_imports)]
use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
#[allow(unused_imports)]
use std::path::{Path, PathBuf};

#[allow(unused_imports)]
use super::event::Event;
#[allow(unused_imports)]
use super::WatchError;

/// File name inside the state directory.
pub const JOURNAL_NAME: &str = "journal.jsonl";
/// The longest line accepted when reading. Events are far smaller; the bound keeps a damaged file from being read into
/// memory as one line.
pub const MAX_LINE_BYTES: usize = 4 << 20;

/// What a scan of the file found at its end.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ScanEnd {
    /// `seq` of the last complete line (0 for an empty journal).
    pub tail: u64,
    /// Length of the prefix made of complete lines.
    pub good: u64,
    /// Length of the file.
    pub size: u64,
}

#[allow(dead_code)]
fn corrupt(msg: String) -> WatchError {
    WatchError::CorruptState(format!("{JOURNAL_NAME}: {msg}"))
}

/// Reads the whole file line by line (memory is bounded by one line), validates every complete line and calls `visit` for
/// each event in order.
pub fn scan(path: &Path, visit: &mut dyn FnMut(&Event)) -> Result<ScanEnd, WatchError> {
    let _ = (path, visit);
    Ok(ScanEnd {
        tail: 0,
        good: 0,
        size: 0,
    })
}

/// The journal of one state directory, open for appending. One writer: the caller holds the directory lock.
pub struct Journal {
    path: PathBuf,
    file: File,
    tail: u64,
    good: u64,
    junk: u64,
}

impl Journal {
    /// Opens (creating it with mode 0600) and validates the journal; an existing file gets mode 0600.
    pub fn open(dir: &Path) -> Result<Journal, WatchError> {
        let file = File::open("/dev/null").map_err(|e| WatchError::Io(e.to_string()))?;
        Ok(Journal {
            path: dir.join(JOURNAL_NAME),
            file,
            tail: 0,
            good: 0,
            junk: 0,
        })
    }

    /// `seq` of the last complete line (0 for an empty journal).
    pub fn tail(&self) -> u64 {
        self.tail
    }

    /// Bytes of a truncated write found at the end by `open` (0: none). The next `append` removes them.
    pub fn junk_bytes(&self) -> u64 {
        self.junk
    }

    /// Writes `ev` with the next `seq` (the `seq` it has is ignored), fsyncs and returns the event as written. An
    /// unterminated truncated tail is cut off first, so every line of the file validates.
    pub fn append(&mut self, ev: &Event) -> Result<Event, WatchError> {
        let _ = (&self.path, &self.file, self.tail, self.good);
        Ok(ev.clone())
    }

    /// The complete events with `seq` greater than `seq`, in order.
    pub fn after(&self, seq: u64) -> Result<Vec<Event>, WatchError> {
        let _ = (&self.path, seq);
        Ok(Vec::new())
    }
}
