//! Reading the files of `<git-common-dir>/stw/oplog/` and the reflogs of the repository (`docs/oplog-format.md`, 1, 6, 9).
//! Nothing here writes. A log that is a symbolic link, not a regular file, or in a symbolic directory is not read.
use std::path::{Path, PathBuf};

use super::parse::{self, Parsed, RefLine, RewriteLine};

/// More than this many bytes of one log file are not read (the product rotates at 10 MiB; a hook alone does not).
const MAX_FILE: u64 = 64 * 1024 * 1024;

/// What reading one named file gave.
pub enum File {
    Missing,
    Unreadable(String),
    Data(Vec<u8>),
}

/// A log read as one stream: the old generation `<name>.1` first, then `<name>` (spec 6).
pub struct LogRead<T> {
    pub lines: Vec<T>,
    pub bad: usize,
    /// At least one of the two files exists.
    pub exists: bool,
    /// Why a file was not read (file name, reason).
    pub unreadable: Vec<(String, String)>,
}

pub struct Oplog {
    pub common: PathBuf,
    dir: PathBuf,
}

impl Oplog {
    pub fn new(common: &Path) -> Oplog {
        Oplog {
            common: common.to_path_buf(),
            dir: common.join("stw").join("oplog"),
        }
    }

    /// `Err(reason)` when `stw` or `stw/oplog` is a symbolic link or not a directory; `Ok(false)` when it is absent.
    fn dir_state(&self) -> Result<bool, String> {
        for p in [self.common.join("stw"), self.dir.clone()] {
            match std::fs::symlink_metadata(&p) {
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(false),
                Err(e) => return Err(e.to_string()),
                Ok(m) if m.file_type().is_symlink() => return Err("a symbolic link".into()),
                Ok(m) if !m.is_dir() => return Err("not a directory".into()),
                Ok(_) => {}
            }
        }
        Ok(true)
    }

    fn read_path(&self, p: &Path) -> File {
        match std::fs::symlink_metadata(p) {
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => File::Missing,
            Err(e) => File::Unreadable(e.to_string()),
            Ok(m) if m.file_type().is_symlink() => File::Unreadable("a symbolic link".into()),
            Ok(m) if !m.is_file() => File::Unreadable("not a regular file".into()),
            Ok(m) if m.len() > MAX_FILE => {
                File::Unreadable(format!("larger than {MAX_FILE} bytes"))
            }
            Ok(_) => match std::fs::read(p) {
                Ok(d) => File::Data(d),
                Err(e) => File::Unreadable(e.to_string()),
            },
        }
    }

    /// A file of the oplog directory.
    pub fn file(&self, name: &str) -> File {
        match self.dir_state() {
            Ok(false) => File::Missing,
            Err(why) => File::Unreadable(format!("stw/oplog is {why}")),
            Ok(true) => self.read_path(&self.dir.join(name)),
        }
    }

    fn stream<T>(&self, name: &str, parse: impl Fn(&[u8]) -> Parsed<T>) -> LogRead<T> {
        let mut out = LogRead {
            lines: Vec::new(),
            bad: 0,
            exists: false,
            unreadable: Vec::new(),
        };
        for n in [format!("{name}.1"), name.to_string()] {
            match self.file(&n) {
                File::Missing => {}
                File::Unreadable(why) => {
                    out.exists = true;
                    out.unreadable.push((n, why));
                }
                File::Data(d) => {
                    out.exists = true;
                    let p = parse(&d);
                    out.bad += p.bad;
                    out.lines.extend(p.lines);
                }
            }
        }
        out
    }

    pub fn refs(&self) -> LogRead<RefLine> {
        self.stream("refs.log", parse::parse_refs)
    }

    pub fn rewrites(&self) -> LogRead<RewriteLine> {
        self.stream("rewrites.log", parse::parse_rewrites)
    }

    /// `remotes=on` of the `config` file (read up to 1 KB, never executed).
    pub fn remotes_on(&self) -> bool {
        let File::Data(d) = self.file("config") else {
            return false;
        };
        let d = &d[..d.len().min(1024)];
        String::from_utf8_lossy(d).lines().any(|l| {
            l.split_once('=')
                .is_some_and(|(k, v)| k == "remotes" && matches!(v, "1" | "on" | "yes" | "true"))
        })
    }

    /// Install and uninstall events of an events file (`events-refhook.log` for `refs.log`, `events.log` for
    /// `rewrites.log`): (epoch, installed). `None` when the file does not exist or holds no valid line.
    pub fn window(&self, name: &str) -> Option<Window> {
        let File::Data(d) = self.file(name) else {
            return None;
        };
        let mut ev: Vec<(i64, bool)> = Vec::new();
        for l in String::from_utf8_lossy(&d).lines() {
            let Some((t, w)) = l.split_once('\t') else {
                continue;
            };
            let (Some(e), true) = (parse::epoch_of(t), matches!(w, "install" | "uninstall")) else {
                continue;
            };
            ev.push((e, w == "install"));
        }
        (!ev.is_empty()).then_some(Window(ev))
    }

    /// Gap lines of `errors.log` and of the fallback file `<common>/stw-oplog-gaps`: (kind, from the fallback file).
    pub fn gap_lines(&self) -> (Vec<GapLine>, Vec<(String, String)>) {
        let mut out = Vec::new();
        let mut unreadable = Vec::new();
        let sources: [(&str, File); 2] = [
            ("errors.log", self.file("errors.log")),
            (
                "stw-oplog-gaps",
                self.read_path(&self.common.join("stw-oplog-gaps")),
            ),
        ];
        for (i, (name, f)) in sources.into_iter().enumerate() {
            match f {
                File::Data(d) => {
                    for l in String::from_utf8_lossy(&d).lines() {
                        let f: Vec<&str> = l.split('\t').collect();
                        if f.len() >= 3 && f[1] == "gap" {
                            out.push((f[2].to_string(), i == 1));
                        }
                    }
                }
                File::Unreadable(why) => unreadable.push((name.to_string(), why)),
                File::Missing => {}
            }
        }
        (out, unreadable)
    }
}

/// A gap line: its kind and whether it comes from the fallback file `stw-oplog-gaps`.
pub type GapLine = (String, bool);

/// The install windows of one hook, from its events file.
pub struct Window(Vec<(i64, bool)>);

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Coverage {
    /// Before the first install record: the hook was not there yet.
    PreInstall,
    /// Between an install and the next uninstall: a line was expected.
    Expected,
    /// After an uninstall.
    HookAbsent,
}

impl Window {
    pub fn at(&self, epoch: i64) -> Coverage {
        // the last record at or before `epoch`; none: before the first install
        let mut state = None;
        for &(t, installed) in &self.0 {
            if t <= epoch {
                state = Some(installed);
            }
        }
        match state {
            None => Coverage::PreInstall,
            Some(true) => Coverage::Expected,
            Some(false) => Coverage::HookAbsent,
        }
    }
}

/// One line of a reflog file.
#[derive(Clone, Debug)]
pub struct ReflogEntry {
    pub old: String,
    pub new: String,
    pub epoch: i64,
    pub message: String,
}

fn is_hash(s: &str) -> bool {
    matches!(s.len(), 40 | 64) && s.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
}

/// A reflog file: `<old> <new> <ident>\t<message>`; a line that does not have this shape is skipped.
pub fn parse_reflog(data: &[u8]) -> Vec<ReflogEntry> {
    let mut out = Vec::new();
    for l in String::from_utf8_lossy(data).lines() {
        let (head, message) = l.split_once('\t').unwrap_or((l, ""));
        let mut it = head.splitn(3, ' ');
        let (Some(old), Some(new), Some(ident)) = (it.next(), it.next(), it.next()) else {
            continue;
        };
        if !is_hash(old) || !is_hash(new) {
            continue;
        }
        let mut tail = ident.rsplit(' ');
        let (_tz, Some(ts)) = (tail.next(), tail.next()) else {
            continue;
        };
        let Ok(epoch) = ts.parse::<i64>() else {
            continue;
        };
        out.push(ReflogEntry {
            old: old.into(),
            new: new.into(),
            epoch,
            message: message.into(),
        });
    }
    out
}

/// A reflog file of the repository, read without following links; `None` when absent or not a regular file.
pub fn read_reflog_file(p: &Path) -> Option<Vec<ReflogEntry>> {
    let m = std::fs::symlink_metadata(p).ok()?;
    if !m.is_file() || m.len() > MAX_FILE {
        return None;
    }
    std::fs::read(p).ok().map(|d| parse_reflog(&d))
}

/// A reflog file to read: the ref name it belongs to, the label shown, the path.
pub struct ReflogFile {
    pub refname: String,
    pub label: String,
    pub path: PathBuf,
}

/// The reflog files of `HEAD` (main and linked worktrees) and of the refs below `logs/refs`, links not followed.
/// Which refs: `refs/heads`, `refs/stash`, `refs/stw` always; `refs/remotes` with `remotes`; any other with `all`.
pub fn reflog_files(common: &Path, remotes: bool, all: bool) -> Vec<ReflogFile> {
    let mut out = Vec::new();
    let head = common.join("logs").join("HEAD");
    if std::fs::symlink_metadata(&head).is_ok_and(|m| m.is_file()) {
        out.push(ReflogFile {
            refname: "HEAD".into(),
            label: "HEAD".into(),
            path: head,
        });
    }
    let mut ids: Vec<String> = std::fs::read_dir(common.join("worktrees"))
        .map(|d| {
            d.flatten()
                .filter_map(|e| e.file_name().into_string().ok())
                .collect()
        })
        .unwrap_or_default();
    ids.sort();
    for id in ids {
        let p = common.join("worktrees").join(&id).join("logs").join("HEAD");
        if std::fs::symlink_metadata(&p).is_ok_and(|m| m.is_file()) {
            out.push(ReflogFile {
                refname: "HEAD".into(),
                label: format!("HEAD@{id}"),
                path: p,
            });
        }
    }
    let mut files = Vec::new();
    walk(&common.join("logs").join("refs"), "refs", &mut files);
    files.sort();
    for (name, path) in files {
        let keep = if name.starts_with("refs/heads/")
            || name == "refs/stash"
            || name.starts_with("refs/stw/")
        {
            true
        } else if name.starts_with("refs/remotes/") {
            remotes || all
        } else {
            all
        };
        if keep {
            out.push(ReflogFile {
                refname: name.clone(),
                label: name,
                path,
            });
        }
    }
    out
}

fn walk(dir: &Path, prefix: &str, out: &mut Vec<(String, PathBuf)>) {
    let Ok(rd) = std::fs::read_dir(dir) else {
        return;
    };
    for e in rd.flatten() {
        let Ok(name) = e.file_name().into_string() else {
            continue;
        };
        let Ok(ft) = e.file_type() else {
            continue;
        };
        let rel = format!("{prefix}/{name}");
        if ft.is_dir() {
            walk(&e.path(), &rel, out);
        } else if ft.is_file() {
            out.push((rel, e.path()));
        }
    }
}

/// Shell-like glob: `*` matches any run of characters (`/` included), `?` one character.
pub fn glob(pattern: &str, text: &str) -> bool {
    fn go(p: &[char], t: &[char]) -> bool {
        match p.first() {
            None => t.is_empty(),
            Some('*') => (0..=t.len()).any(|i| go(&p[1..], &t[i..])),
            Some('?') => !t.is_empty() && go(&p[1..], &t[1..]),
            Some(c) => t.first() == Some(c) && go(&p[1..], &t[1..]),
        }
    }
    let p: Vec<char> = pattern.chars().collect();
    let t: Vec<char> = text.chars().collect();
    go(&p, &t)
}
