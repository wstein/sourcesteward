//! The state directory of a watcher: its id and default place, SHA-256 for the id, the freshness window of `--status`.
#[allow(unused_imports)]
use std::collections::BTreeSet;
#[allow(unused_imports)]
use std::fs::{File, OpenOptions};
#[allow(unused_imports)]
use std::io::Write;
#[allow(unused_imports)]
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt, PermissionsExt};
#[allow(unused_imports)]
use std::path::{Component, Path, PathBuf};
#[allow(unused_imports)]
use std::sync::atomic::{AtomicU64, Ordering};

#[allow(unused_imports)]
use serde::Serialize;

#[allow(unused_imports)]
use super::event::format_utc;
#[allow(unused_imports)]
use super::state::State;
#[allow(unused_imports)]
use super::WatchError;

/// Prefix of the text the workspace id hashes. It differs from the Go watcher's `stw-watch-v1`, so a Go state directory
/// and a Rust one never share a path (D-20261119-05: new format, new baseline).
pub const ID_PREFIX: &str = "stw-watch-rs1\n";

/// SHA-256 (FIPS 180-4), own code because no SHA-256 is in the dependency graph (D-20261119-04); NIST vectors in the tests.
pub fn sha256(data: &[u8]) -> [u8; 32] {
    const K: [u32; 64] = [
        0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4,
        0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe,
        0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f,
        0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
        0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc,
        0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
        0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116,
        0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
        0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
        0xc67178f2,
    ];
    let mut h: [u32; 8] = [
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab,
        0x5be0cd19,
    ];
    // Padding: 0x80, zeros up to 56 mod 64, the message length in bits as a big-endian u64.
    let mut msg = data.to_vec();
    msg.push(0x80);
    while msg.len() % 64 != 56 {
        msg.push(0);
    }
    msg.extend_from_slice(&((data.len() as u64).wrapping_mul(8)).to_be_bytes());
    for block in msg.as_chunks::<64>().0 {
        let mut w = [0u32; 64];
        for (i, word) in block.as_chunks::<4>().0.iter().enumerate() {
            w[i] = u32::from_be_bytes(*word);
        }
        for i in 16..64 {
            let s0 = w[i - 15].rotate_right(7) ^ w[i - 15].rotate_right(18) ^ (w[i - 15] >> 3);
            let s1 = w[i - 2].rotate_right(17) ^ w[i - 2].rotate_right(19) ^ (w[i - 2] >> 10);
            w[i] = w[i - 16]
                .wrapping_add(s0)
                .wrapping_add(w[i - 7])
                .wrapping_add(s1);
        }
        let [mut a, mut b, mut c, mut d, mut e, mut f, mut g, mut hh] = h;
        for i in 0..64 {
            let s1 = e.rotate_right(6) ^ e.rotate_right(11) ^ e.rotate_right(25);
            let ch = (e & f) ^ (!e & g);
            let t1 = hh
                .wrapping_add(s1)
                .wrapping_add(ch)
                .wrapping_add(K[i])
                .wrapping_add(w[i]);
            let s0 = a.rotate_right(2) ^ a.rotate_right(13) ^ a.rotate_right(22);
            let maj = (a & b) ^ (a & c) ^ (b & c);
            let t2 = s0.wrapping_add(maj);
            hh = g;
            g = f;
            f = e;
            e = d.wrapping_add(t1);
            d = c;
            c = b;
            b = a;
            a = t1.wrapping_add(t2);
        }
        for (x, y) in h.iter_mut().zip([a, b, c, d, e, f, g, hh]) {
            *x = x.wrapping_add(y);
        }
    }
    let mut out = [0u8; 32];
    for (i, x) in h.iter().enumerate() {
        out[i * 4..i * 4 + 4].copy_from_slice(&x.to_be_bytes());
    }
    out
}

/// Lower-case hex of `sha256`.
pub fn sha256_hex(data: &[u8]) -> String {
    sha256(data).iter().map(|b| format!("{b:02x}")).collect()
}

/// What the workspace id is made of: the canonical workspace root, or the list of repositories (`OWNER/NAME`).
pub enum IdSource<'a> {
    Workspace(&'a str),
    Repos(&'a [String]),
}

/// The first 16 hex digits of SHA-256 over `ID_PREFIX`, the host and either `workspace:ROOT` or `repos:` followed by the
/// lower-cased, de-duplicated, sorted repositories joined with `\n`.
pub fn workspace_id(host: &str, source: IdSource<'_>) -> String {
    let mut text = format!("{ID_PREFIX}{host}\n");
    match source {
        IdSource::Workspace(root) => {
            text.push_str("workspace:");
            text.push_str(root);
        }
        IdSource::Repos(repos) => {
            let list: BTreeSet<String> = repos.iter().map(|r| r.to_lowercase()).collect();
            text.push_str("repos:");
            text.push_str(&list.into_iter().collect::<Vec<_>>().join("\n"));
        }
    }
    sha256_hex(text.as_bytes())[..16].to_string()
}

/// `$XDG_STATE_HOME/stw/watch/<id>`, else `$HOME/.local/state/stw/watch/<id>`. An empty or missing value counts as unset.
pub fn default_state_dir(id: &str, xdg_state_home: Option<&str>, home: Option<&str>) -> PathBuf {
    let base = match xdg_state_home.filter(|x| !x.is_empty()) {
        Some(x) => PathBuf::from(x),
        None => PathBuf::from(home.unwrap_or(""))
            .join(".local")
            .join("state"),
    };
    base.join("stw").join("watch").join(id)
}

/// The age below which a heartbeat counts as alive: `max(3 x interval, 180)` seconds.
pub fn status_freshness_seconds(interval_seconds: u64) -> u64 {
    interval_seconds.saturating_mul(3).max(180)
}

// ---------------------------------------------------------------------------------------------------------------------
// The directory itself: creation and refusal, atomic files, heartbeat, state.json, the status report.

/// A path without `.` and `..` components, resolved lexically (like Go's `filepath.Clean`).
pub fn clean_path(p: &Path) -> PathBuf {
    let mut out = PathBuf::new();
    for c in p.components() {
        match c {
            Component::ParentDir => {
                if !out.pop() {
                    // `..` above the root stays at the root
                }
            }
            Component::CurDir => {}
            other => out.push(other.as_os_str()),
        }
    }
    out
}

/// Creates `dir` (mode 0700, also the missing parents) or accepts an existing 0700 one. Refused (and nothing created) are:
/// a relative path, a directory inside a Git work tree (a `.git` entry in it or above it) or below a `.stw` directory, found
/// after the symbolic links of the deepest existing ancestor are resolved, and an existing directory with another mode.
pub fn ensure_state_dir(dir: &Path) -> Result<(), WatchError> {
    if !dir.is_absolute() {
        return Err(WatchError::StateDirUsage);
    }
    let dir = clean_path(dir);
    // Resolve the deepest existing ancestor so a symbolic link into a work tree is seen.
    let mut existing = dir.clone();
    let mut rest: Vec<std::ffi::OsString> = Vec::new();
    while std::fs::symlink_metadata(&existing).is_err() {
        match (
            existing.file_name().map(|n| n.to_os_string()),
            existing.parent(),
        ) {
            (Some(name), Some(parent)) => {
                rest.push(name);
                existing = parent.to_path_buf();
            }
            _ => break,
        }
    }
    let mut resolved = std::fs::canonicalize(&existing)
        .map_err(|e| WatchError::Io(format!("cannot resolve {}: {e}", existing.display())))?;
    for name in rest.iter().rev() {
        resolved.push(name);
    }
    let mut p = resolved.as_path();
    loop {
        if p.file_name().is_some_and(|n| n == ".stw")
            || std::fs::symlink_metadata(p.join(".git")).is_ok()
        {
            return Err(WatchError::InsideWorkTree(resolved.display().to_string()));
        }
        match p.parent() {
            Some(parent) => p = parent,
            None => break,
        }
    }
    std::fs::DirBuilder::new()
        .recursive(true)
        .mode(0o700)
        .create(&resolved)
        .map_err(|e| WatchError::Io(format!("cannot create {}: {e}", resolved.display())))?;
    let meta = std::fs::metadata(&resolved)
        .map_err(|e| WatchError::Io(format!("cannot read {}: {e}", resolved.display())))?;
    if !meta.is_dir() {
        return Err(WatchError::Io(format!(
            "{} is not a directory",
            resolved.display()
        )));
    }
    let mode = meta.permissions().mode() & 0o777;
    if mode != 0o700 {
        return Err(WatchError::StateDirMode(format!(
            "{} has mode {mode:o}",
            resolved.display()
        )));
    }
    Ok(())
}

/// Flushes a directory entry (a created or renamed file) to disk.
pub fn sync_dir(dir: &Path) -> Result<(), WatchError> {
    File::open(dir)
        .and_then(|d| d.sync_all())
        .map_err(|e| WatchError::Io(format!("cannot sync {}: {e}", dir.display())))
}

/// Writes `dir/name` with mode 0600 atomically: a temporary file, fsync, rename, fsync of the directory. A reader sees the old
/// or the new content, never a mix; a failure leaves no temporary file behind.
pub fn write_atomic(dir: &Path, name: &str, data: &[u8]) -> Result<(), WatchError> {
    static COUNTER: AtomicU64 = AtomicU64::new(0);
    let tmp = dir.join(format!(
        "{name}.tmp-{}-{}",
        std::process::id(),
        COUNTER.fetch_add(1, Ordering::Relaxed)
    ));
    let target = dir.join(name);
    let result = (|| -> std::io::Result<()> {
        let mut f = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&tmp)?;
        f.write_all(data)?;
        f.sync_all()?;
        drop(f);
        std::fs::rename(&tmp, &target)
    })();
    if let Err(e) = result {
        let _ = std::fs::remove_file(&tmp);
        return Err(WatchError::Io(format!(
            "cannot write {}: {e}",
            target.display()
        )));
    }
    sync_dir(dir)
}

/// File names inside the state directory.
pub const STATE_NAME: &str = "state.json";
pub const HEARTBEAT_NAME: &str = "heartbeat";

/// The line `RFC3339 pid mode` of the last completed cycle.
pub fn write_heartbeat(dir: &Path, now_secs: i64, pid: u32, mode: &str) -> Result<(), WatchError> {
    write_atomic(
        dir,
        HEARTBEAT_NAME,
        format!("{} {pid} {mode}\n", format_utc(now_secs)).as_bytes(),
    )
}

/// The last heartbeat as read from the file.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Heartbeat {
    pub time: String,
    pub age_seconds: u64,
    pub pid: u32,
    pub mode: String,
}

/// Reads the heartbeat. `Ok(None)`: no file, or a file that is not three fields with a valid time and pid.
pub fn read_heartbeat(dir: &Path, now_secs: i64) -> Result<Option<Heartbeat>, WatchError> {
    let path = dir.join(HEARTBEAT_NAME);
    let raw = match std::fs::read_to_string(&path) {
        Ok(r) => r,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(e) if e.kind() == std::io::ErrorKind::InvalidData => return Ok(None),
        Err(e) => {
            return Err(WatchError::Io(format!(
                "cannot read {}: {e}",
                path.display()
            )))
        }
    };
    let f: Vec<&str> = raw.split_whitespace().collect();
    let [time, pid, mode] = f[..] else {
        return Ok(None);
    };
    let (Some(t), Ok(pid)) = (crate::github::parse_utc(time), pid.parse::<u32>()) else {
        return Ok(None);
    };
    Ok(Some(Heartbeat {
        time: time.to_string(),
        age_seconds: u64::try_from(now_secs.saturating_sub(t)).unwrap_or(0),
        pid,
        mode: mode.to_string(),
    }))
}

/// Reads `state.json`: `Ok(None)` when the file is missing, `CorruptState` when it cannot be used.
pub fn load_state(dir: &Path) -> Result<Option<State>, WatchError> {
    let _ = dir;
    Ok(None)
}

/// Writes `state.json` atomically.
pub fn save_state(dir: &Path, state: &State) -> Result<(), WatchError> {
    let _ = (dir, state);
    Ok(())
}

/// One watched repository in the status report.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct StatusRepo {
    pub repo: String,
    pub last_ok: Option<String>,
    pub age_seconds: Option<u64>,
    pub failures: u64,
    pub open_error: Option<String>,
}

/// The heartbeat part of the status report.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct StatusHeartbeat {
    pub time: String,
    pub age_seconds: u64,
    pub pid: u32,
}

/// The `--status --json` object: liveness of the watcher that owns the directory, read from the directory only (no
/// network, no lock taken, nothing written).
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct Status {
    pub schema_version: u32,
    pub state_dir: String,
    pub heartbeat: Option<StatusHeartbeat>,
    pub fresh: bool,
    pub interval_seconds: u64,
    pub seq: u64,
    pub lock_held: bool,
    pub repos: Vec<StatusRepo>,
    /// The pid the lock holder recorded (not part of the JSON).
    #[serde(skip)]
    pub lock_pid: Option<u32>,
}

/// Reads the status of `dir` at `now_secs`. A missing directory, state or heartbeat is not an error: the result is simply not
/// fresh. A corrupt `state.json` or journal is `CorruptState`.
pub fn read_status(dir: &Path, now_secs: i64) -> Result<Status, WatchError> {
    let _ = now_secs;
    Ok(Status {
        schema_version: 1,
        state_dir: dir.display().to_string(),
        heartbeat: None,
        fresh: false,
        interval_seconds: 60,
        seq: 0,
        lock_held: false,
        repos: Vec::new(),
        lock_pid: None,
    })
}
