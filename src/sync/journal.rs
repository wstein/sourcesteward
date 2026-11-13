//! The journal of a `stw sync` run: one record per repository and run, plus one recovery ref per changed branch.
//!
//! Ported from the Go tool (`internal/stwsync/journal.go`); a record written by either tool is read by the other.
//!
//! - Place: `<git common dir>/stw/sync/<run id>.json` (a local state of the repository, never part of the work tree,
//!   directories `0700`, files `0600`). A symbolic link as `stw`, `sync` or a record is refused.
//! - Content (schema 1): `type` `"sync"`, `schema_version`, `run_id`, `remote`, `started`, `entries[]` with `n`, `ref`,
//!   `worktree`, `old_oid`, `planned_new_oid`, `new_oid`, `orig_head`, `stash_oid`, `recovery_ref`, `state`, `reason`.
//!   States: `prepared` (written before the change), `completed`, `failed`, `restored`, `conflict` (Go only).
//! - Recovery ref: `refs/stw/recovery/sync/<run id>/<n>` holds the old tip, so the objects stay reachable.
//! - Order of a change: the recovery ref (create-only), then the record in state `prepared`, then the move of the
//!   branch, then the record in state `completed` (or `failed`). A crash between the steps leaves a recovery ref
//!   and never a moved branch without one. A record is replaced atomically (temporary file, fsync, rename, fsync of
//!   the directory); the first write of a record fails when the file exists.
//! - Run ID: UTC time stamp with microseconds and a random suffix; two runs, also parallel ones, get different IDs.
//! - Retention: after a run that completed a change in a repository, only the ten most recent completed runs of it
//!   keep their record and recovery refs (`retain`).
//!
//! Everything read from a record is data: the file name and the `run_id` inside must agree, the ref must be the entry's
//! own recovery ref, an OID must be 40 lower-case hex digits. The recovery refs are written and deleted through gix (create-only, delete only with the expected value), so a run needs no `git`.
use std::collections::BTreeSet;
use std::io::Write;
use std::path::{Path, PathBuf};

use serde_json::{json, Value};

use crate::git_runner::{recovery_ref, Hex40, RunId};

pub const STATE_PREPARED: &str = "prepared";
pub const STATE_COMPLETED: &str = "completed";
pub const STATE_FAILED: &str = "failed";
pub const STATE_RESTORED: &str = "restored";

/// How many completed runs of one repository keep their record and recovery refs.
pub const RETAINED_RUNS: usize = 10;

/// Most entries and bytes of a record that is read.
const MAX_ENTRIES: usize = 4096;
const MAX_RECORD_BYTES: u64 = 4 << 20;

/// A new run ID: UTC time stamp with microseconds and four random bytes as hex.
pub fn new_run_id() -> RunId {
    use std::hash::{BuildHasher, Hasher};
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default();
    // `2026-11-11T12:34:56Z` -> `20261111T123456` (microseconds and `Z-` follow)
    let stamp: String = crate::lint_pr_cmd::rfc3339_utc(now.as_secs() as i64)
        .trim_end_matches('Z')
        .chars()
        .filter(|c| *c != '-' && *c != ':')
        .collect();
    // RandomState is seeded from the operating system's randomness
    let mut h = std::collections::hash_map::RandomState::new().build_hasher();
    h.write_u128(now.as_nanos());
    h.write_u32(std::process::id());
    let id = format!(
        "{stamp}{:06}Z-{:08x}",
        now.subsec_micros(),
        h.finish() as u32
    );
    RunId::parse(&id).expect("a generated run id has the documented form")
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Entry {
    pub n: u32,
    pub reference: String,
    pub worktree: String,
    pub old_oid: String,
    pub planned_new_oid: String,
    pub new_oid: String,
    pub orig_head: String,
    pub stash_oid: String,
    pub recovery_ref: String,
    pub state: String,
    pub reason: String,
}

impl Entry {
    fn to_json(&self) -> Value {
        json!({"n": self.n, "ref": self.reference, "worktree": self.worktree, "old_oid": self.old_oid,
               "planned_new_oid": self.planned_new_oid, "new_oid": self.new_oid, "orig_head": self.orig_head,
               "stash_oid": self.stash_oid, "recovery_ref": self.recovery_ref, "state": self.state,
               "reason": self.reason})
    }

    fn from_json(v: &Value) -> Result<Entry, String> {
        let s = |k: &str| -> Result<String, String> {
            match v.get(k) {
                None | Some(Value::Null) => Ok(String::new()),
                Some(Value::String(t)) => Ok(t.clone()),
                Some(_) => Err(format!("entry field {k} is not a string")),
            }
        };
        let n = v
            .get("n")
            .and_then(Value::as_u64)
            .and_then(|n| u32::try_from(n).ok())
            .filter(|n| *n >= 1)
            .ok_or("entry field n is not a positive number")?;
        Ok(Entry {
            n,
            reference: s("ref")?,
            worktree: s("worktree")?,
            old_oid: s("old_oid")?,
            planned_new_oid: s("planned_new_oid")?,
            new_oid: s("new_oid")?,
            orig_head: s("orig_head")?,
            stash_oid: s("stash_oid")?,
            recovery_ref: s("recovery_ref")?,
            state: s("state")?,
            reason: s("reason")?,
        })
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Record {
    pub run_id: String,
    pub remote: String,
    pub started: String,
    pub entries: Vec<Entry>,
}

impl Record {
    pub fn to_json(&self) -> Value {
        json!({"type": "sync", "schema_version": 1, "run_id": self.run_id, "remote": self.remote,
               "started": self.started, "entries": self.entries.iter().map(Entry::to_json).collect::<Vec<_>>()})
    }

    /// Parse a record read from the file named `<id>.json`; the record must be a `sync` record of that run.
    pub fn parse(text: &str, id: &RunId) -> Result<Record, String> {
        let v: Value = serde_json::from_str(text).map_err(|e| format!("not JSON: {e}"))?;
        if v.get("type").and_then(Value::as_str) != Some("sync") {
            return Err("not a sync journal record".into());
        }
        if v.get("run_id").and_then(Value::as_str) != Some(id.as_str()) {
            return Err("the run id inside differs from the file name".into());
        }
        let list = v
            .get("entries")
            .and_then(Value::as_array)
            .ok_or("no entries list")?;
        if list.len() > MAX_ENTRIES {
            return Err(format!("more than {MAX_ENTRIES} entries"));
        }
        let text_field = |k: &str| v.get(k).and_then(Value::as_str).unwrap_or("").to_string();
        Ok(Record {
            run_id: id.as_str().to_string(),
            remote: text_field("remote"),
            started: text_field("started"),
            entries: list
                .iter()
                .map(Entry::from_json)
                .collect::<Result<_, _>>()?,
        })
    }

    /// No entry is still `prepared`: the record is finished (or was dealt with by a restore).
    pub fn is_done(&self) -> bool {
        self.entries.iter().all(|e| e.state != STATE_PREPARED)
    }

    pub fn changed_ok(&self) -> bool {
        self.entries.iter().any(|e| e.state == STATE_COMPLETED)
    }
}

/// `<git common dir>/stw/sync`.
pub fn dir_of(repo: &gix::Repository) -> PathBuf {
    repo.common_dir().join("stw").join("sync")
}

/// `dir` and its parent `stw` are real directories (no symbolic link); with `create` they are made (`0700`).
fn ensure_dir(dir: &Path, create: bool) -> std::io::Result<bool> {
    let parent = dir.parent().expect("the journal directory has a parent");
    for d in [parent, dir] {
        match std::fs::symlink_metadata(d) {
            Ok(m) if m.is_dir() => {}
            Ok(_) => {
                return Err(std::io::Error::other(format!(
                    "{} is not a plain directory",
                    d.display()
                )))
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound && create => {
                let mut b = std::fs::DirBuilder::new();
                #[cfg(unix)]
                std::os::unix::fs::DirBuilderExt::mode(&mut b, 0o700);
                b.create(d)?;
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(false),
            Err(e) => return Err(e),
        }
    }
    Ok(true)
}

/// The run IDs that have a record file in `dir` (names only, not read): entries named `<run id>.json` that are no directory. A missing directory is an empty list; a directory that cannot be read is an error.
pub fn list_ids(dir: &Path) -> Result<Vec<RunId>, String> {
    match ensure_dir(dir, false) {
        Ok(true) => {}
        Ok(false) => return Ok(Vec::new()),
        Err(e) => return Err(format!("{}: {e}", dir.display())),
    }
    let mut ids = Vec::new();
    for e in std::fs::read_dir(dir).map_err(|e| format!("{}: {e}", dir.display()))? {
        let e = e.map_err(|e| format!("{}: {e}", dir.display()))?;
        let name = e.file_name();
        let Some(id) = name
            .to_str()
            .and_then(|n| n.strip_suffix(".json"))
            .and_then(RunId::parse)
        else {
            continue;
        };
        // a symbolic link under a record's name is listed too (and refused by `load`), so that it is reported
        if e.file_type().is_ok_and(|t| !t.is_dir()) {
            ids.push(id);
        }
    }
    ids.sort_by_key(RunId::order_key);
    Ok(ids)
}

/// Read and check the record of run `id` in `dir`. `Ok(None)`: there is none.
pub fn load(dir: &Path, id: &RunId) -> Result<Option<Record>, String> {
    use std::io::Read;
    let path = dir.join(format!("{id}.json"));
    match std::fs::symlink_metadata(&path) {
        Ok(m) if m.is_file() => {}
        Ok(_) => return Err("the record is not a plain file".into()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(e) => return Err(e.to_string()),
    }
    let mut text = String::new();
    std::fs::File::open(&path)
        .and_then(|f| f.take(MAX_RECORD_BYTES + 1).read_to_string(&mut text))
        .map_err(|e| format!("cannot read the record: {e}"))?;
    if text.len() as u64 > MAX_RECORD_BYTES {
        return Err("the record is larger than 4 MiB".into());
    }
    Record::parse(&text, id).map(Some)
}

fn started_now() -> String {
    let secs = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0);
    crate::lint_pr_cmd::rfc3339_utc(secs)
}

/// The journal of one repository in one run. No file exists before the first `prepare`: a run without a change leaves
/// no record.
pub struct Journal {
    dir: PathBuf,
    id: RunId,
    rec: Record,
    /// The record file exists (it is replaced, not created, from now on).
    written: bool,
}

/// Where a change of the journal stands, for the report of the repository.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Note {
    pub run_id: String,
    pub n: u32,
    pub recovery_ref: String,
    pub state: String,
}

impl Journal {
    pub fn new(repo: &gix::Repository, id: RunId) -> Journal {
        Journal {
            dir: dir_of(repo),
            rec: Record {
                run_id: id.as_str().to_string(),
                remote: String::new(),
                started: started_now(),
                entries: Vec::new(),
            },
            id,
            written: false,
        }
    }

    /// The journal of an existing record (a restore rewrites the state of an entry it dealt with).
    pub fn from_record(repo: &gix::Repository, id: RunId, rec: Record) -> Journal {
        Journal {
            dir: dir_of(repo),
            id,
            rec,
            written: true,
        }
    }

    pub fn record(&self) -> &Record {
        &self.rec
    }

    pub fn note(&self, i: usize) -> Note {
        let e = &self.rec.entries[i];
        Note {
            run_id: self.id.as_str().to_string(),
            n: e.n,
            recovery_ref: e.recovery_ref.clone(),
            state: e.state.clone(),
        }
    }

    /// Create the recovery ref (create-only, holds `old`) and durably record the planned change of `reference`. Returns
    /// the index of the entry. When the record cannot be written, the recovery ref is deleted again and nothing is
    /// left. Nothing is changed besides these two.
    pub fn prepare(
        &mut self,
        repo: &gix::Repository,
        reference: &str,
        worktree: &str,
        old: Hex40,
        planned: Hex40,
    ) -> Result<usize, String> {
        self.prepare_planned(repo, reference, worktree, old, planned.as_str())
    }

    /// `prepare` for a rebase or a merge: the new tip is not known before git ran, so the entry names none
    /// (`planned_new_oid` empty, as in the Go record).
    pub fn prepare_unplanned(
        &mut self,
        repo: &gix::Repository,
        reference: &str,
        worktree: &str,
        old: Hex40,
    ) -> Result<usize, String> {
        self.prepare_planned(repo, reference, worktree, old, "")
    }

    fn prepare_planned(
        &mut self,
        repo: &gix::Repository,
        reference: &str,
        worktree: &str,
        old: Hex40,
        planned: &str,
    ) -> Result<usize, String> {
        let n = self.rec.entries.len() as u32 + 1;
        create_recovery_ref(repo, &recovery_ref(&self.id, n), old, &self.id)
            .map_err(|e| format!("recovery ref: {e}"))?;
        self.rec.entries.push(Entry {
            n,
            reference: reference.to_string(),
            worktree: worktree.to_string(),
            old_oid: old.as_str().to_string(),
            planned_new_oid: planned.to_string(),
            recovery_ref: recovery_ref(&self.id, n),
            state: STATE_PREPARED.into(),
            ..Default::default()
        });
        if let Err(e) = self.write() {
            self.rec.entries.pop();
            let _ = delete_recovery_ref(repo, &recovery_ref(&self.id, n), old);
            return Err(e);
        }
        Ok(self.rec.entries.len() - 1)
    }

    /// Record the outcome of entry `i`.
    pub fn finish(
        &mut self,
        i: usize,
        state: &str,
        new_oid: &str,
        reason: &str,
    ) -> Result<(), String> {
        let e = &mut self.rec.entries[i];
        e.state = state.into();
        e.new_oid = new_oid.into();
        e.reason = reason.into();
        self.write()
    }

    /// Replace the record file atomically; the first write refuses an existing file.
    fn write(&mut self) -> Result<(), String> {
        let fail =
            |what: &str, e: std::io::Error| format!("journal {what} {}: {e}", self.dir.display());
        ensure_dir(&self.dir, true).map_err(|e| fail("directory", e))?;
        let path = self.dir.join(format!("{}.json", self.id));
        let tmp = self.dir.join(format!("{}.json.tmp", self.id));
        let mut text = serde_json::to_vec(&self.rec.to_json()).expect("JSON value serializes");
        text.push(b'\n');
        match std::fs::remove_file(&tmp) {
            Ok(()) => {}
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
            Err(e) => return Err(fail("temporary file", e)),
        }
        let mut opts = std::fs::OpenOptions::new();
        opts.write(true).create_new(true);
        #[cfg(unix)]
        std::os::unix::fs::OpenOptionsExt::mode(&mut opts, 0o600);
        let mut f = opts.open(&tmp).map_err(|e| fail("temporary file", e))?;
        let wrote = f.write_all(&text).and_then(|()| f.sync_all());
        drop(f);
        if let Err(e) = wrote {
            let _ = std::fs::remove_file(&tmp);
            return Err(fail("write", e));
        }
        let placed = if self.written {
            std::fs::rename(&tmp, &path)
        } else {
            // create-only: the name is taken atomically or the call fails
            match std::fs::hard_link(&tmp, &path) {
                Ok(()) => std::fs::remove_file(&tmp),
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
                    let _ = std::fs::remove_file(&tmp);
                    return Err(format!("run id {} is already used", self.id));
                }
                Err(_) if !path.exists() => std::fs::rename(&tmp, &path),
                Err(e) => {
                    let _ = std::fs::remove_file(&tmp);
                    return Err(fail("record", e));
                }
            }
        };
        if let Err(e) = placed {
            let _ = std::fs::remove_file(&tmp);
            return Err(fail("record", e));
        }
        self.written = true;
        #[cfg(unix)]
        std::fs::File::open(&self.dir)
            .and_then(|d| d.sync_all())
            .map_err(|e| fail("directory fsync of", e))?;
        Ok(())
    }

    /// Last step of a run that completed a change here: drop the record and the recovery refs of the completed runs
    /// beyond the `RETAINED_RUNS` most recent ones (this run counts and is never dropped). A run with a `prepared`
    /// entry is kept until a restore has dealt with it. Best effort: a failure leaves the older runs in place.
    pub fn retain(&self, repo: &gix::Repository) {
        let Ok(ids) = list_ids(&self.dir) else {
            return;
        };
        let mut done: Vec<Record> = Vec::new();
        for id in ids.iter().filter(|i| **i != self.id) {
            if let Ok(Some(rec)) = load(&self.dir, id) {
                if rec.is_done() {
                    done.push(rec);
                }
            }
        }
        // `list_ids` is sorted by the run order already
        while done.len() > RETAINED_RUNS - 1 {
            let old = done.remove(0);
            if drop_recovery_refs(repo, &old) {
                let _ = std::fs::remove_file(self.dir.join(format!("{}.json", old.run_id)));
            }
        }
    }
}

/// Delete the recovery refs of one run; true when none is left. The record is data: only the entry's own name
/// `refs/stw/recovery/sync/<run>/<n>`, a valid old OID as the expected value and a ref that is not symbolic qualify. A
/// ref that cannot be read keeps the record.
fn drop_recovery_refs(repo: &gix::Repository, rec: &Record) -> bool {
    let Some(id) = RunId::parse(&rec.run_id) else {
        return false;
    };
    let mut clean = true;
    let mut seen = BTreeSet::new();
    for e in &rec.entries {
        let own = recovery_ref(&id, e.n);
        let old = Hex40::parse(&e.old_oid);
        let Some(old) = old.filter(|_| e.recovery_ref == own && seen.insert(e.n)) else {
            clean = false;
            continue;
        };
        match repo.try_find_reference(own.as_str()) {
            Ok(None) => continue, // verified missing
            Ok(Some(r)) => {
                // a symbolic ref is never followed, never deleted
                let direct = matches!(r.target(), gix::refs::TargetRef::Object(_));
                if !direct {
                    clean = false;
                    continue;
                }
            }
            Err(_) => {
                clean = false;
                continue;
            }
        }
        let gone = delete_recovery_ref(repo, &own, old);
        if gone.is_err() {
            clean = false;
        }
    }
    clean
}

/// Create the recovery ref `name` at `oid` with a reflog entry. Create-only: an existing ref of that name is never
/// replaced (the expected old value is "must not exist"). Through gix: no program is started, so the step does not depend
/// on `git` or on the repository's own config.
fn create_recovery_ref(
    repo: &gix::Repository,
    name: &str,
    oid: Hex40,
    run: &RunId,
) -> Result<(), String> {
    use gix::refs::transaction::{Change, LogChange, PreviousValue, RefEdit, RefLog};
    use gix::refs::Target;
    let id = gix::ObjectId::from_hex(oid.as_str().as_bytes()).map_err(|e| e.to_string())?;
    let name: gix::refs::FullName = name
        .try_into()
        .map_err(|e: gix::refs::name::Error| e.to_string())?;
    // gix lets "must not exist" pass when the ref already holds the same value: name the collision here
    if repo
        .try_find_reference(&name)
        .map_err(|e| e.to_string())?
        .is_some()
    {
        return Err("the ref exists already".into());
    }
    repo.edit_reference(RefEdit {
        change: Change::Update {
            log: LogChange {
                mode: RefLog::AndReference,
                force_create_reflog: true,
                message: format!("stw sync {run}").into(),
            },
            expected: PreviousValue::MustNotExist,
            new: Target::Object(id),
        },
        name: name.clone(),
        deref: false,
    })
    .map_err(|e| e.to_string())?;
    crate::oplog::write::log_ref_update(
        repo,
        &crate::oplog::write::RefUpdate {
            refname: name.as_bstr().to_string().as_str(),
            old: None,
            new: Some(id),
        },
    );
    Ok(())
}

/// Delete the recovery ref `name` only while it still holds `oid`.
fn delete_recovery_ref(repo: &gix::Repository, name: &str, oid: Hex40) -> Result<(), String> {
    use gix::refs::transaction::{Change, PreviousValue, RefEdit, RefLog};
    use gix::refs::Target;
    let id = gix::ObjectId::from_hex(oid.as_str().as_bytes()).map_err(|e| e.to_string())?;
    let name: gix::refs::FullName = name
        .try_into()
        .map_err(|e: gix::refs::name::Error| e.to_string())?;
    repo.edit_reference(RefEdit {
        change: Change::Delete {
            expected: PreviousValue::MustExistAndMatch(Target::Object(id)),
            log: RefLog::AndReference,
        },
        name: name.clone(),
        deref: false,
    })
    .map_err(|e| e.to_string())?;
    crate::oplog::write::log_ref_update(
        repo,
        &crate::oplog::write::RefUpdate {
            refname: name.as_bstr().to_string().as_str(),
            old: Some(id),
            new: None,
        },
    );
    Ok(())
}
