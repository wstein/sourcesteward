//! Writing the op log: stw's own ref updates as `refs.log` lines (context `stw`) and the size rotation
//! (`docs/oplog-format.md`, 2, 3, 6). Hooks never see gix writes, so stw writes these lines itself.
//! Nothing here fails a command: a problem becomes a gap line and a notice on stderr, the ref update stands.
use std::fs::{File, OpenOptions};
use std::io::Write;
use std::os::unix::fs::{DirBuilderExt, FileExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use super::parse;
use super::store::Oplog;

/// Rotate a log file that is larger than this (10 MiB; spec 6).
pub const MAX_LOG_BYTES: u64 = 10 * 1024 * 1024;

/// The longest name written unhashed: longer names are cut here and end in `%00` and the djb2 hash (spec 3.2).
const NAME_CUT: usize = 200;

/// djb2 over the bytes (`h = 5381`, `h = h * 33 + byte` modulo 2^32).
pub fn djb2(name: &[u8]) -> u32 {
    name.iter().fold(5381u32, |h, &b| {
        h.wrapping_mul(33).wrapping_add(u32::from(b))
    })
}

/// A ref name as written to `refs.log` (spec 3.2): every byte outside `[A-Za-z0-9._/+@:-]` as `%XX`; a name over 200
/// bytes is cut at 200 bytes and ends in `%00` and the 8 hex digits of the djb2 of the whole name.
pub fn encode_name(name: &[u8]) -> String {
    let cut = &name[..name.len().min(NAME_CUT)];
    let mut out = String::with_capacity(cut.len());
    for &b in cut {
        if b.is_ascii_alphanumeric() || b"._/+@:-".contains(&b) {
            out.push(b as char);
        } else {
            out.push_str(&format!("%{b:02X}"));
        }
    }
    if name.len() > NAME_CUT {
        out.push_str(&format!("%00{:08x}", djb2(name)));
    }
    out
}

/// The worktree field: `main`, or the reduced id (`other` if it holds `/`, bytes outside `[A-Za-z0-9._-]` as `?`, at
/// most 40 characters).
pub fn worktree_field(id: Option<&std::ffi::OsStr>) -> String {
    use std::os::unix::ffi::OsStrExt;
    let Some(id) = id else {
        return "main".into();
    };
    // bytes, not a lossy UTF-8 conversion: one `?` per byte, as in the hook
    let bytes: Vec<u8> = id.as_bytes().to_vec();
    if bytes.contains(&b'/') {
        return "other".into();
    }
    bytes
        .into_iter()
        .map(|b| {
            if b.is_ascii_alphanumeric() || b"._-".contains(&b) {
                b as char
            } else {
                '?'
            }
        })
        .take(40)
        .collect()
}

/// One `refs.log` line without the final LF: `<time> <old> <new> <refname> <context> <worktree>`.
pub fn format_line(
    time: &str,
    old: &str,
    new: &str,
    refname: &str,
    context: &str,
    worktree: &str,
) -> String {
    format!(
        "{time}\t{old}\t{new}\t{}\t{context}\t{worktree}",
        encode_name(refname.as_bytes())
    )
}

fn now() -> String {
    let secs = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0);
    parse::iso_of(secs)
}

/// The directory `<common>/stw/oplog`, created with mode 0700 when absent. A symbolic link or a non-directory at `stw`
/// or `stw/oplog` is refused (links are never followed).
fn ensure_dir(common: &Path) -> Result<PathBuf, String> {
    let mut p = common.to_path_buf();
    for part in ["stw", "oplog"] {
        p.push(part);
        match std::fs::symlink_metadata(&p) {
            Ok(m) if m.file_type().is_symlink() => {
                return Err(format!("{} is a symbolic link", p.display()))
            }
            Ok(m) if !m.is_dir() => return Err(format!("{} is not a directory", p.display())),
            Ok(_) => {}
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => create_or_accept(&p)?,
            Err(e) => return Err(format!("{}: {e}", p.display())),
        }
    }
    Ok(p)
}

/// A symbolic link at `path` is refused (checked before the open, as the readers do; no `libc` here).
fn refuse_link(path: &Path) -> Result<(), String> {
    match std::fs::symlink_metadata(path) {
        Ok(m) if m.file_type().is_symlink() => {
            Err(format!("{} is a symbolic link", path.display()))
        }
        _ => Ok(()),
    }
}

/// Create the directory `p` with mode 0700. When another stw created it in the meantime (`AlreadyExists`), the
/// directory is accepted; a link or a non-directory there is refused as everywhere else.
fn create_or_accept(p: &Path) -> Result<(), String> {
    match std::fs::DirBuilder::new().mode(0o700).create(p) {
        Ok(()) => Ok(()),
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
            match std::fs::symlink_metadata(p) {
                Ok(m) if m.is_dir() => Ok(()),
                Ok(_) => Err(format!("{} is not a directory", p.display())),
                Err(e) => Err(format!("{}: {e}", p.display())),
            }
        }
        Err(e) => Err(format!("{}: {e}", p.display())),
    }
}

/// Whether the file ends in the middle of a line, judged from the size `len` that was read earlier. Other writers append
/// at the same time, and a file system such as FUSE may show a size that does not match the data: the byte at `len - 1`
/// can then be missing (nothing is read: no verdict) or lie inside a line that others finished since. So it reads from
/// `len - 1` on to the end of what is visible and judges the last byte that came back.
fn ends_without_lf(
    len: u64,
    read_at: &dyn Fn(&mut [u8], u64) -> std::io::Result<usize>,
) -> std::io::Result<bool> {
    let mut chunk = [0u8; 4096];
    let mut at = len - 1;
    let mut last = None;
    // 16 chunks at most: the file is not read to its end while others keep appending
    for _ in 0..16 {
        let n = read_at(&mut chunk, at)?;
        if n == 0 {
            break;
        }
        last = Some(chunk[n - 1]);
        at += n as u64;
    }
    Ok(last.is_some_and(|b| b != b'\n'))
}

/// How long `append_line` waits for the lock of a log at most (spec 2, "Lock wait").
const LOCK_BUDGET: Duration = Duration::from_millis(300);

/// Take the exclusive lock of `f`, retrying with a short backoff until `budget` is used up. `false` when the lock stayed
/// held or the file system has none: the caller goes on without it, stw never waits for a log (spec 2, Q16).
fn lock_within(f: &File, budget: Duration) -> bool {
    let end = Instant::now() + budget;
    let mut pause = Duration::from_millis(2);
    loop {
        match f.try_lock() {
            Ok(()) => return true,
            Err(std::fs::TryLockError::WouldBlock) => {}
            Err(std::fs::TryLockError::Error(_)) => return false,
        }
        let left = end.saturating_duration_since(Instant::now());
        if left.is_zero() {
            return false;
        }
        std::thread::sleep(pause.min(left));
        pause = (pause * 2).min(Duration::from_millis(50));
    }
}

/// Append one line to a file, once, with `O_APPEND`, a link is refused, mode 0600. When the file is non-empty and
/// does not end in LF (a writer killed mid-line), one LF goes first in the same write (spec 2). The check and the write
/// run under an exclusive lock of the file, which is waited for at most `LOCK_BUDGET`; a lock that stays held does not
/// stop the write.
fn append_line(path: &Path, line: &str) -> Result<(), String> {
    refuse_link(path)?;
    let mut f = OpenOptions::new()
        .read(true)
        .append(true)
        .create(true)
        .mode(0o600)
        .open(path)
        .map_err(|e| format!("{}: {e}", path.display()))?;
    let m = f.metadata().map_err(|e| e.to_string())?;
    if !m.is_file() {
        return Err(format!("{} is not a regular file", path.display()));
    }
    // One stw at a time from the tail check to the write: another writer's line that is in flight must not look like a
    // broken one (on FUSE it can be seen half written). Released when `f` is dropped; a file system without locks
    // goes on without one (hooks write the same files with `>>` and never take it). A lock that stays held after
    // `LOCK_BUDGET` is no reason to block either: the line is written without it.
    let _ = lock_within(&f, LOCK_BUDGET);
    let m = f.metadata().map_err(|e| e.to_string())?;
    let mut buf = Vec::with_capacity(line.len() + 2);
    if m.len() > 0
        && ends_without_lf(m.len(), &|b, at| f.read_at(b, at)).map_err(|e| e.to_string())?
    {
        buf.push(b'\n');
    }
    buf.extend_from_slice(line.as_bytes());
    buf.push(b'\n');
    f.write_all(&buf)
        .map_err(|e| format!("{}: {e}", path.display()))
}

/// A gap line `<time> gap <kind> failed=<n> bad=<n> <last>` in `errors.log`, else in `<common>/stw-oplog-gaps`, else
/// nowhere (spec 2.1).
fn gap(common: &Path, kind: &str, failed: u32, last: &str) {
    let line = format!("{}\tgap\t{kind}\tfailed={failed}\tbad=0\t{last}", now());
    if let Ok(d) = ensure_dir(common) {
        if append_line(&d.join("errors.log"), &line).is_ok() {
            return;
        }
    }
    let _ = append_line(&common.join("stw-oplog-gaps"), &line);
}

/// Rotate `refs.log` and `rewrites.log` above `max` bytes (spec 6): `try_lock` on `rotate.lock`, never wait; size again
/// under the lock; rename to `<name>.1`, replacing an older one; a `rotated` gap line per file. Never fails.
pub fn rotate_if_needed(common: &Path, max: u64) {
    let Ok(dir) = ensure_dir(common) else {
        return;
    };
    let big = |name: &str| {
        std::fs::symlink_metadata(dir.join(name)).is_ok_and(|m| m.is_file() && m.len() > max)
    };
    let names = ["refs.log", "rewrites.log"];
    if !names.iter().any(|n| big(n)) {
        return;
    }
    if refuse_link(&dir.join("rotate.lock")).is_err() {
        gap(common, "rotate-failed", 1, "-");
        return;
    }
    let lock: File = match OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(false)
        .mode(0o600)
        .open(dir.join("rotate.lock"))
    {
        Ok(f) => f,
        Err(_) => {
            gap(common, "rotate-failed", 1, "-");
            return;
        }
    };
    match lock.try_lock() {
        Ok(()) => {}
        Err(std::fs::TryLockError::WouldBlock) => return,
        Err(std::fs::TryLockError::Error(_)) => {
            gap(common, "rotate-failed", 1, "-");
            return;
        }
    }
    for name in names {
        if !big(name) {
            continue;
        }
        match std::fs::rename(dir.join(name), dir.join(format!("{name}.1"))) {
            Ok(()) => gap(common, "rotated", 0, name),
            Err(_) => gap(common, "rotate-failed", 1, name),
        }
    }
    // the lock is released when `lock` is dropped (and by the OS when the process ends)
}

/// Everything one update of a ref needs for its line.
pub struct RefUpdate<'a> {
    pub refname: &'a str,
    /// `None`: the ref did not exist (zeros).
    pub old: Option<gix::ObjectId>,
    /// `None`: the ref is deleted (zeros).
    pub new: Option<gix::ObjectId>,
}

/// Append the `refs.log` line of one ref update stw has made through gix, then rotate when the log is too large.
/// Skipped: a name that is not `HEAD` or below `refs/`, `old` equal to `new`, `refs/remotes/**` unless `remotes=on`.
/// Never fails the caller: a problem is a gap line `write-failed` and a notice on stderr.
pub fn log_ref_update(repo: &gix::Repository, u: &RefUpdate<'_>) {
    log_ref_update_in(repo.common_dir(), repo, u, MAX_LOG_BYTES);
}

pub fn log_ref_update_in(common: &Path, repo: &gix::Repository, u: &RefUpdate<'_>, max: u64) {
    if !(u.refname == "HEAD" || u.refname.starts_with("refs/")) || u.old == u.new {
        return;
    }
    if u.refname.starts_with("refs/remotes/") && !Oplog::new(common).remotes_on() {
        return;
    }
    let zeros = "0".repeat(repo.object_hash().len_in_hex());
    let hex = |o: Option<gix::ObjectId>| o.map_or_else(|| zeros.clone(), |i| i.to_string());
    let wt = (repo.git_dir() != common)
        .then(|| repo.git_dir().file_name())
        .flatten();
    let line = format_line(
        &now(),
        &hex(u.old),
        &hex(u.new),
        u.refname,
        "stw",
        &worktree_field(wt),
    );
    let result = ensure_dir(common).and_then(|d| append_line(&d.join("refs.log"), &line));
    match result {
        Ok(()) => rotate_if_needed(common, max),
        Err(e) => {
            gap(common, "write-failed", 1, "-");
            eprintln!(
                "stw: op log: the line for {} was not written: {e}",
                u.refname
            );
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn long(prefix: &str, ch: &str, n: usize) -> String {
        format!("{prefix}{}", ch.repeat(n))
    }

    #[test]
    fn djb2_and_encoding_match_the_spec_vectors() {
        assert_eq!(djb2(b""), 0x1505);
        assert_eq!(format!("{:08x}", djb2(b"refs/heads/main")), "0866f11d");
        assert_eq!(encode_name(b"refs/heads/main"), "refs/heads/main");
        assert_eq!(encode_name("refs/heads/ä".as_bytes()), "refs/heads/%C3%A4");
        assert_eq!(
            format!("{:08x}", djb2("refs/heads/ä".as_bytes())),
            "aee7ecff"
        );
        assert_eq!(encode_name(b"refs/tags/v1%"), "refs/tags/v1%25");
        assert_eq!(format!("{:08x}", djb2(b"refs/tags/v1%")), "9cff28ce");
        let x = long("refs/heads/", "x", 210);
        assert_eq!(
            encode_name(x.as_bytes()),
            format!("{}%00ffb8bca8", &x[..200])
        );
        let a = long("refs/heads/", "ä", 110);
        let enc = encode_name(a.as_bytes());
        assert!(enc.ends_with("%00b252bbfa"), "{enc}");
        // 200 bytes: 11 plain, 94 two-byte letters, the first byte of the 95th
        assert_eq!(enc.len(), 11 + 94 * 6 + 3 + 11);
        assert!(enc.contains("%C3%A4%C3") && enc.trim_end_matches("%00b252bbfa").ends_with("%C3"));
    }

    #[test]
    fn the_tail_check_follows_what_is_visible_not_only_the_size() {
        let at = |data: &'static [u8]| {
            move |b: &mut [u8], off: u64| -> std::io::Result<usize> {
                let off = off as usize;
                if off >= data.len() {
                    return Ok(0);
                }
                let n = b.len().min(data.len() - off);
                b[..n].copy_from_slice(&data[off..off + n]);
                Ok(n)
            }
        };
        // a complete last line
        assert!(!ends_without_lf(4, &at(b"a\nb\n")).unwrap());
        // a writer killed mid-line
        assert!(ends_without_lf(3, &at(b"a\nb")).unwrap());
        // the size is larger than the data (FUSE): nothing to read at len - 1, no verdict, no line feed
        assert!(!ends_without_lf(10, &at(b"a\nb\n")).unwrap());
        // the size is smaller than the data: len - 1 lies inside a line that others have finished
        assert!(!ends_without_lf(3, &at(b"aaaa\nbbbb\n")).unwrap());
        // the data beyond the first chunk counts
        static LONG_LF: [u8; 10000] = {
            let mut a = [b'x'; 10000];
            a[9999] = b'\n';
            a
        };
        static LONG_X: [u8; 10000] = [b'x'; 10000];
        assert!(!ends_without_lf(1, &at(&LONG_LF)).unwrap());
        assert!(ends_without_lf(1, &at(&LONG_X)).unwrap());
        // a read error is an error, not a verdict
        let err = |_: &mut [u8], _: u64| Err(std::io::Error::other("boom"));
        assert!(ends_without_lf(1, &err).is_err());
    }

    #[test]
    fn parallel_writers_leave_no_empty_line_and_lose_no_line() {
        // on a FUSE file system the size and the data of a file under concurrent appends disagree for moments;
        // there an empty line appeared (6 writers). On a local file system this cannot fail.
        let t = std::env::temp_dir().join(format!("stw-ow-par-{}", std::process::id()));
        std::fs::create_dir_all(&t).unwrap();
        let log = t.join("refs.log");
        let hs: Vec<_> = (0..6)
            .map(|w| {
                let log = log.clone();
                std::thread::spawn(move || {
                    for i in 0..2000 {
                        append_line(&log, &format!("w{w}\t{i}")).unwrap();
                    }
                })
            })
            .collect();
        for h in hs {
            h.join().unwrap();
        }
        let s = std::fs::read_to_string(&log).unwrap();
        let empty = s.lines().filter(|l| l.is_empty()).count();
        assert_eq!(
            (s.lines().count() - empty, empty),
            (12000, 0),
            "empty lines"
        );
        std::fs::remove_dir_all(&t).unwrap();
    }

    // The lock of a log is held by another open file description: the append must not hang and must write its line.
    fn append_with_timeout(log: &Path, line: &str) -> Duration {
        let (tx, rx) = std::sync::mpsc::channel();
        let (log, line) = (log.to_path_buf(), line.to_string());
        let start = Instant::now();
        std::thread::spawn(move || {
            let _ = tx.send(append_line(&log, &line));
        });
        rx.recv_timeout(Duration::from_secs(5))
            .expect("append_line hangs on a held lock")
            .unwrap();
        start.elapsed()
    }

    #[test]
    fn a_lock_that_stays_held_does_not_stop_the_append() {
        let t = std::env::temp_dir().join(format!("stw-ow-held-{}", std::process::id()));
        std::fs::create_dir_all(&t).unwrap();
        let log = t.join("refs.log");
        std::fs::write(&log, "first\npart").unwrap();
        let holder = File::open(&log).unwrap();
        holder.lock().unwrap();
        let took = append_with_timeout(&log, "second");
        assert!(took >= LOCK_BUDGET.mul_f32(0.8), "no wait: {took:?}");
        // the wait ends with the budget: not at the 5 s of the timeout, not at a larger budget
        assert!(took < LOCK_BUDGET + Duration::from_secs(1), "{took:?}");
        // the result is the one of an append under the lock: a broken tail gets its LF, the line is whole
        assert_eq!(
            std::fs::read_to_string(&log).unwrap(),
            "first\npart\nsecond\n"
        );
        drop(holder);
        std::fs::remove_dir_all(&t).unwrap();
    }

    #[test]
    fn a_lock_that_is_released_within_the_budget_is_taken() {
        let t = std::env::temp_dir().join(format!("stw-ow-rel-{}", std::process::id()));
        std::fs::create_dir_all(&t).unwrap();
        let log = t.join("refs.log");
        std::fs::write(&log, "").unwrap();
        let holder = File::open(&log).unwrap();
        holder.lock().unwrap();
        let rel = std::thread::spawn(move || {
            std::thread::sleep(Duration::from_millis(100));
            drop(holder);
        });
        let took = append_with_timeout(&log, "x");
        rel.join().unwrap();
        assert!(took >= Duration::from_millis(80), "did not wait: {took:?}");
        assert!(took < LOCK_BUDGET + Duration::from_secs(1), "{took:?}");
        assert_eq!(std::fs::read_to_string(&log).unwrap(), "x\n");
        std::fs::remove_dir_all(&t).unwrap();
    }

    #[test]
    fn lock_within_takes_a_lock_that_is_released_soon_and_holds_it() {
        let t = std::env::temp_dir().join(format!("stw-ow-within-{}", std::process::id()));
        std::fs::create_dir_all(&t).unwrap();
        let log = t.join("refs.log");
        std::fs::write(&log, "").unwrap();
        let holder = File::open(&log).unwrap();
        holder.lock().unwrap();
        let rel = std::thread::spawn(move || {
            std::thread::sleep(Duration::from_millis(20));
            drop(holder);
        });
        let mine = File::open(&log).unwrap();
        let start = Instant::now();
        // a long budget: the call returns when the lock is free, not at the end of the budget
        assert!(lock_within(&mine, Duration::from_secs(5)));
        let took = start.elapsed();
        rel.join().unwrap();
        assert!(took >= Duration::from_millis(15), "did not wait: {took:?}");
        // the first pauses are 2, 4, 8, 16 ms: the lock is seen within about 30 ms, far below 150 ms
        assert!(
            took < Duration::from_millis(150),
            "waited too long: {took:?}"
        );
        // the lock is held now: a second open file description does not get it
        let other = File::open(&log).unwrap();
        assert!(matches!(
            other.try_lock(),
            Err(std::fs::TryLockError::WouldBlock)
        ));
        drop(mine);
        assert!(other.try_lock().is_ok(), "released with the file");
        std::fs::remove_dir_all(&t).unwrap();
    }

    #[test]
    fn lock_within_gives_up_when_the_budget_is_used_up() {
        let t = std::env::temp_dir().join(format!("stw-ow-giveup-{}", std::process::id()));
        std::fs::create_dir_all(&t).unwrap();
        let log = t.join("refs.log");
        std::fs::write(&log, "").unwrap();
        let holder = File::open(&log).unwrap();
        holder.lock().unwrap();
        let mine = File::open(&log).unwrap();
        let start = Instant::now();
        assert!(!lock_within(&mine, Duration::from_millis(100)));
        let took = start.elapsed();
        assert!(took >= Duration::from_millis(90), "{took:?}");
        assert!(took < Duration::from_secs(1), "{took:?}");
        drop(holder);
        std::fs::remove_dir_all(&t).unwrap();
    }

    #[test]
    fn a_directory_that_exists_already_is_accepted_and_nothing_else_is() {
        let t = std::env::temp_dir().join(format!("stw-ow-dir-{}", std::process::id()));
        std::fs::create_dir_all(&t).unwrap();
        let d = t.join("d");
        std::fs::create_dir(&d).unwrap();
        assert_eq!(
            create_or_accept(&d),
            Ok(()),
            "a lost mkdir race is no failure"
        );
        let f = t.join("f");
        std::fs::write(&f, "").unwrap();
        assert!(create_or_accept(&f).is_err(), "a file is no directory");
        let l = t.join("l");
        std::os::unix::fs::symlink(&d, &l).unwrap();
        assert!(create_or_accept(&l).is_err(), "a link is refused");
        std::fs::remove_dir_all(&t).unwrap();
    }

    #[test]
    fn many_writers_on_a_fresh_repository_lose_no_line() {
        // threads race for the creation of `stw` and `stw/oplog`
        for round in 0..40 {
            let t =
                std::env::temp_dir().join(format!("stw-ow-race-{}-{round}", std::process::id()));
            std::fs::create_dir_all(&t).unwrap();
            let barrier = std::sync::Arc::new(std::sync::Barrier::new(8));
            let hs: Vec<_> = (0..8)
                .map(|_| {
                    let (t, b) = (t.clone(), barrier.clone());
                    std::thread::spawn(move || {
                        b.wait();
                        ensure_dir(&t).map(|_| ())
                    })
                })
                .collect();
            for h in hs {
                assert_eq!(h.join().unwrap(), Ok(()), "round {round}");
            }
            std::fs::remove_dir_all(&t).unwrap();
        }
    }

    #[test]
    fn a_name_of_exactly_200_bytes_is_not_cut() {
        let n = long("refs/heads/", "x", 189);
        assert_eq!(n.len(), 200);
        assert_eq!(encode_name(n.as_bytes()), n);
    }

    #[test]
    fn worktree_field_reduces_the_id() {
        assert_eq!(worktree_field(None), "main");
        use std::ffi::OsStr;
        use std::os::unix::ffi::OsStrExt;
        let f = |s: &str| worktree_field(Some(OsStr::new(s)));
        assert_eq!(f("wt-1.a_b"), "wt-1.a_b");
        assert_eq!(f("a/b"), "other");
        assert_eq!(f("ä b"), "???b");
        assert_eq!(f(&"w".repeat(50)).len(), 40);
        // an invalid UTF-8 byte is one `?`, as in the hook
        assert_eq!(worktree_field(Some(OsStr::from_bytes(b"a\xffb"))), "a?b");
    }

    #[test]
    fn a_line_has_six_fields_and_parses_back() {
        let z = "0".repeat(40);
        let new = "a".repeat(40);
        let l = format_line(
            "2026-11-12T00:42:26Z",
            &z,
            &new,
            "refs/heads/ä b",
            "stw",
            "main",
        );
        assert_eq!(l.split('\t').count(), 6);
        let p = parse::parse_refs(format!("{l}\n").as_bytes());
        assert_eq!(p.bad, 0);
        assert_eq!(p.lines.len(), 1);
        assert_eq!(p.lines[0].context, "stw");
    }
}
