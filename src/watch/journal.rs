//! `journal.jsonl`: the append-only record of every emitted event, one `Event` JSON line each, fsynced before the event is
//! printed. Events only; never rotated. The next `seq` is the `seq` of the last complete line plus one, so it is gapless
//! and survives a lost `state.json`; it is read from the lines, never computed from a file offset.
//!
//! Damage is told apart by where it is:
//! - bytes after the last newline are a truncated write (a crash in the middle of an append). They are not part of the
//!   journal; the next append cuts them off (`Journal::junk_bytes` reports their length after `open`);
//! - a complete line that is not a valid event, a `seq` that is not the previous one plus one, or a line longer than
//!   `MAX_LINE_BYTES` is corruption: `WatchError::CorruptState`, nothing is repaired.
use std::fs::{File, OpenOptions};
use std::io::{BufRead, BufReader, Read, Seek, SeekFrom, Write};
use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};

use super::event::Event;
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

fn corrupt(msg: String) -> WatchError {
    WatchError::CorruptState(format!("{JOURNAL_NAME}: {msg}"))
}

/// Reads the whole file line by line (memory is bounded by one line), validates every complete line and calls `visit` for
/// each event in order.
pub fn scan(path: &Path, visit: &mut dyn FnMut(&Event)) -> Result<ScanEnd, WatchError> {
    let file = File::open(path)
        .map_err(|e| WatchError::Io(format!("cannot read {}: {e}", path.display())))?;
    let size = file
        .metadata()
        .map_err(|e| WatchError::Io(format!("cannot read {}: {e}", path.display())))?
        .len();
    let mut reader = BufReader::new(file);
    let (mut tail, mut good) = (0u64, 0u64);
    let mut buf = Vec::new();
    loop {
        buf.clear();
        let n = (&mut reader)
            .take(MAX_LINE_BYTES as u64 + 1)
            .read_until(b'\n', &mut buf)
            .map_err(|e| WatchError::Io(format!("cannot read {}: {e}", path.display())))?;
        if n == 0 {
            break;
        }
        if buf.last() != Some(&b'\n') {
            if n > MAX_LINE_BYTES {
                return Err(corrupt(format!(
                    "line {} is longer than {MAX_LINE_BYTES} bytes",
                    tail + 1
                )));
            }
            break; // an unterminated tail: a truncated write
        }
        if n > MAX_LINE_BYTES {
            return Err(corrupt(format!(
                "line {} is longer than {MAX_LINE_BYTES} bytes",
                tail + 1
            )));
        }
        let line = std::str::from_utf8(&buf[..n - 1])
            .map_err(|_| corrupt(format!("line {} is not UTF-8", tail + 1)))?;
        let ev = Event::from_line(line)
            .map_err(|e| corrupt(format!("line {} does not validate: {e}", tail + 1)))?;
        let want = tail + 1;
        if ev.seq != want {
            return Err(corrupt(format!("seq {} where {want} was expected", ev.seq)));
        }
        visit(&ev);
        tail = want;
        good += n as u64;
    }
    Ok(ScanEnd { tail, good, size })
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
        let path = dir.join(JOURNAL_NAME);
        let existed = path.exists();
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .mode(0o600)
            .open(&path)
            .map_err(|e| WatchError::Io(format!("cannot open {}: {e}", path.display())))?;
        file.set_permissions(std::fs::Permissions::from_mode(0o600))
            .map_err(|e| {
                WatchError::Io(format!("cannot set the mode of {}: {e}", path.display()))
            })?;
        if !existed {
            super::statedir::sync_dir(dir)?;
        }
        let end = scan(&path, &mut |_| {})?;
        Ok(Journal {
            path,
            file,
            tail: end.tail,
            good: end.good,
            junk: end.size - end.good,
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
        let io = |what: &str, e: std::io::Error| {
            WatchError::Io(format!("{what} {}: {e}", self.path.display()))
        };
        let size = self
            .file
            .metadata()
            .map_err(|e| io("cannot read", e))?
            .len();
        if size != self.good {
            self.file
                .set_len(self.good)
                .map_err(|e| io("cannot cut the truncated tail of", e))?;
        }
        let mut out = ev.clone();
        out.seq = self
            .tail
            .checked_add(1)
            .ok_or_else(|| corrupt("seq overflow".to_string()))?;
        let mut line = out.to_line();
        line.push('\n');
        self.file
            .seek(SeekFrom::Start(self.good))
            .map_err(|e| io("cannot write", e))?;
        self.file
            .write_all(line.as_bytes())
            .map_err(|e| io("cannot write", e))?;
        self.file.sync_all().map_err(|e| io("cannot sync", e))?;
        self.good += line.len() as u64;
        self.tail = out.seq;
        self.junk = 0;
        Ok(out)
    }

    /// The complete events with `seq` greater than `seq`, in order.
    pub fn after(&self, seq: u64) -> Result<Vec<Event>, WatchError> {
        let mut out = Vec::new();
        scan(&self.path, &mut |e| {
            if e.seq > seq {
                out.push(e.clone());
            }
        })?;
        Ok(out)
    }
}
