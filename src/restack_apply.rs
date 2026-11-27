//! `stw restack prepare`, `apply` and `undo`: replay one local branch onto another and move the branch.
//!
//! The prepared record is a commit at `refs/stw/restack-prepared/<id>` whose tree holds `plan.json` and whose
//! parents are the old tip, the onto tip and the replayed tip, so those objects survive `git gc`. The plan file
//! is created exclusively and contains only that id. Apply checks both tips again, writes `refs/stw/backup/<run>/1`
//! through the oplog counter, then updates the branch in one ref transaction that also checks the onto tip and the
//! prepared record. A ref that moved since the plan is left at its newer value.
//!
//! Replay is the in-process engine (`replay`). It keeps the author and uses the committer from the repository
//! config. No hook is run: the path does not call `git commit`, `git rebase` or `git update-ref`.
use std::collections::HashSet;
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::time::Duration;

use gix::objs::Write as _;
use gix::refs::transaction::{Change, LogChange, PreviousValue, RefEdit, RefLog};
use gix::refs::{FullName, Target};
use gix::ObjectId;
use serde_json::{json, Map, Value};

use crate::git_runner::{self, Hex40, Op};
use crate::oplog::undo::{self, BACKUP_PREFIX, COUNTER_REF, COUNTER_TRIES};
use crate::oplog::write::{log_ref_update, RefUpdate};
use crate::replay::{self, ReplayError, Status};

const SCHEMA_VERSION: u32 = 1;
const RECORD_MESSAGE: &str = "stw restack record\n";
/// Selectors that would make a Git command operate on a different repository than `--root`.
const GIT_SELECTORS: &[&str] = &[
    "GIT_DIR",
    "GIT_WORK_TREE",
    "GIT_COMMON_DIR",
    "GIT_OBJECT_DIRECTORY",
    "GIT_INDEX_FILE",
    "GIT_NAMESPACE",
    "GIT_ALTERNATE_OBJECT_DIRECTORIES",
];
const ONGOING: &[&str] = &[
    "MERGE_HEAD",
    "CHERRY_PICK_HEAD",
    "REVERT_HEAD",
    "BISECT_LOG",
    "rebase-merge",
    "rebase-apply",
    "sequencer",
];

#[derive(Debug)]
pub struct Fail {
    pub code: &'static str,
    pub message: String,
}

impl Fail {
    pub fn to_json(&self, command: &str) -> Value {
        json!({
            "schema_version": SCHEMA_VERSION,
            "command": command,
            "outcome": "refused",
            "error": {"code": self.code, "message": self.message},
        })
    }
}

#[derive(Debug)]
pub struct Report {
    pub text: String,
    pub value: Value,
}

fn fail(code: &'static str, message: impl Into<String>) -> Fail {
    Fail {
        code,
        message: message.into(),
    }
}

fn refuse_git_selectors() -> Result<(), Fail> {
    let hit: Vec<&str> = GIT_SELECTORS
        .iter()
        .copied()
        .filter(|k| std::env::var_os(k).is_some_and(|v| !v.is_empty()))
        .collect();
    if hit.is_empty() {
        return Ok(());
    }
    Err(fail(
        "git-environment",
        format!(
            "inherited GIT_ environment is unsupported: {}",
            hit.join(", ")
        ),
    ))
}

fn open_repo(path: &Path) -> Result<gix::Repository, Fail> {
    refuse_git_selectors()?;
    let repo = crate::repo::open_for_commit(path).map_err(|e| fail("repository", e.to_string()))?;
    if repo.object_hash() != gix::hash::Kind::Sha1 {
        return Err(fail("sha1-only", "only SHA-1 repositories supported"));
    }
    Ok(repo)
}

fn full_name(name: &str) -> Result<FullName, Fail> {
    name.try_into()
        .map_err(|e: gix::refs::name::Error| fail("invalid-ref", e.to_string()))
}

/// `name` as `refs/heads/…`. A name that is already another namespace is refused.
fn branch_name(name: &str) -> Result<String, Fail> {
    let refname = if let Some(rest) = name.strip_prefix("refs/") {
        if rest.starts_with("heads/") {
            name.to_string()
        } else {
            return Err(fail(
                "not-a-branch",
                format!("{name} is not a local branch"),
            ));
        }
    } else {
        format!("refs/heads/{name}")
    };
    full_name(&refname)?;
    Ok(refname)
}

fn direct(repo: &gix::Repository, name: &str) -> Result<ObjectId, Fail> {
    let found = repo
        .try_find_reference(name)
        .map_err(|e| fail("ref", e.to_string()))?
        .ok_or_else(|| fail("missing-ref", format!("missing ref: {name}")))?;
    match found.target() {
        gix::refs::TargetRef::Symbolic(_) => {
            Err(fail("symbolic-ref", format!("symbolic ref: {name}")))
        }
        gix::refs::TargetRef::Object(id) => {
            let id = id.to_owned();
            repo.find_commit(id)
                .map_err(|_| fail("not-a-commit", format!("{name} does not point at a commit")))?;
            Ok(id)
        }
    }
}

fn git_dirs(repo: &gix::Repository) -> Vec<PathBuf> {
    let mut dirs = Vec::new();
    if let Ok(main) = repo.main_repo() {
        dirs.push(main.git_dir().to_path_buf());
    }
    dirs.push(repo.git_dir().to_path_buf());
    if let Ok(linked) = repo.worktrees() {
        for wt in linked {
            dirs.push(wt.git_dir().to_path_buf());
        }
    }
    dirs.sort();
    dirs.dedup();
    dirs
}

fn head_branch(git_dir: &Path) -> Option<String> {
    let text = fs::read_to_string(git_dir.join("HEAD")).ok()?;
    text.trim()
        .strip_prefix("ref: ")
        .map(|s| s.trim().to_string())
}

fn from_config_files(meta: &gix::config::file::Metadata) -> bool {
    use gix::config::Source;
    matches!(
        meta.source,
        Source::GitInstallation
            | Source::System
            | Source::Git
            | Source::User
            | Source::Local
            | Source::Worktree
    )
}

fn identity_override(repo: &gix::Repository) -> bool {
    let snap = repo.config_snapshot();
    let cfg = snap.plumbing();
    [
        "author.name",
        "author.email",
        "committer.name",
        "committer.email",
    ]
    .into_iter()
    .any(|k| cfg.string_filter(k, from_config_files).is_some())
}

/// The branch is checked out in no worktree of this repository, and no worktree has a merge, rebase,
/// cherry-pick, revert or bisect in progress. `user.name` and `user.email` come from the Git config
/// chain, and that chain has no `author.*` or `committer.*` override.
fn inspect(repo: &gix::Repository, branch: &str) -> Result<(), Fail> {
    if identity_override(repo) {
        return Err(fail(
            "identity-override",
            "author or committer identity overrides are unsupported",
        ));
    }
    let now = gix::date::Time::now_local_or_utc();
    replay::configured_committer(repo, now).map_err(|_| {
        fail(
            "missing-identity",
            "missing Git identity: set user.name and user.email",
        )
    })?;
    for dir in git_dirs(repo) {
        if head_branch(&dir).as_deref() == Some(branch) {
            return Err(fail(
                "checked-out",
                format!("branch checked out in a worktree: {branch}"),
            ));
        }
        for name in ONGOING {
            let marker = dir.join(name);
            if marker.exists() {
                return Err(fail(
                    "operation-in-progress",
                    format!("operation in progress: {}", marker.display()),
                ));
            }
        }
    }
    Ok(())
}

fn commit_edits(repo: &gix::Repository, edits: Vec<RefEdit>) -> Result<(), String> {
    let mut store = repo.refs.clone();
    store.write_reflog = gix::refs::store::WriteReflog::Always;
    let lock = gix::lock::acquire::Fail::AfterDurationWithBackoff(Duration::from_millis(500));
    let committer = repo.committer().transpose().map_err(|e| e.to_string())?;
    // `?` would keep the transaction, which borrows `store`, alive across the error path.
    store
        .transaction()
        .prepare(edits, lock, lock)
        .map_err(|e| e.to_string())
        .and_then(|t| t.commit(committer).map(|_| ()).map_err(|e| e.to_string()))
}

fn update_edit(name: FullName, expected: ObjectId, new: ObjectId, message: &str) -> RefEdit {
    RefEdit {
        change: Change::Update {
            log: LogChange {
                mode: RefLog::AndReference,
                force_create_reflog: true,
                message: message.into(),
            },
            expected: PreviousValue::MustExistAndMatch(Target::Object(expected)),
            new: Target::Object(new),
        },
        name,
        deref: false,
    }
}

fn create_edit(name: FullName, new: ObjectId, message: &str) -> RefEdit {
    RefEdit {
        change: Change::Update {
            log: LogChange {
                mode: RefLog::AndReference,
                force_create_reflog: true,
                message: message.into(),
            },
            expected: PreviousValue::MustNotExist,
            new: Target::Object(new),
        },
        name,
        deref: false,
    }
}

fn log_update(repo: &gix::Repository, name: &str, old: Option<ObjectId>, new: Option<ObjectId>) {
    log_ref_update(
        repo,
        &RefUpdate {
            refname: name,
            old,
            new,
        },
    );
}

struct Backup {
    reference: String,
    retained: Option<String>,
    lines: Vec<String>,
}

/// The next backup run, then `refs/stw/backup/<run>/1` at `value`. When `retain` is set, `<run>/2` is
/// written before retention, so the undo target is not pruned with the run it came from.
fn backup(
    repo: &gix::Repository,
    value: ObjectId,
    retain: Option<ObjectId>,
) -> Result<Backup, Fail> {
    let (run, prev, blob) = undo::take_run_with(repo, COUNTER_TRIES, &mut |_| {})
        .map_err(|e| fail("backup-counter", e))?;
    log_update(repo, COUNTER_REF, prev, Some(blob));
    let reference = format!("{BACKUP_PREFIX}{run}/1");
    commit_edits(
        repo,
        vec![create_edit(
            full_name(&reference)?,
            value,
            "stw restack backup",
        )],
    )
    .map_err(|e| {
        fail(
            "backup-failed",
            format!(
                "backup ref {reference} not created, the branch was not moved; the backup counter may remain: {e}"
            ),
        )
    })?;
    log_update(repo, &reference, None, Some(value));
    let mut lines = Vec::new();
    let retained = if let Some(old) = retain {
        let name = format!("{BACKUP_PREFIX}{run}/2");
        commit_edits(
            repo,
            vec![create_edit(
                full_name(&name)?,
                old,
                "stw restack retained undo target",
            )],
        )
        .map_err(|e| {
            fail(
                "backup-failed",
                format!(
                    "retained undo target {name} not created, the branch was not moved; backup {reference} may remain: {e}"
                ),
            )
        })?;
        log_update(repo, &name, None, Some(old));
        lines.push(format!("retained undo target: {name} {old}"));
        Some(name)
    } else {
        None
    };
    lines.push(format!("backup: {reference} {value}"));
    match undo::prune(repo, run) {
        Ok((gone, failed)) => {
            for (name, id) in &gone {
                log_update(repo, name, Some(*id), None);
            }
            for f in failed {
                eprintln!("notice: old backup not removed: {f}");
            }
        }
        Err(e) => {
            return Err(fail(
                "backup-retention",
                format!(
                    "backup {reference} exists; retention failed and the branch was not moved: {e}"
                ),
            ));
        }
    }
    Ok(Backup {
        reference,
        retained,
        lines,
    })
}

fn new_id() -> Result<String, Fail> {
    let mut bytes = [0u8; 16];
    let mut file = fs::File::open("/dev/urandom")
        .map_err(|e| fail("plan-id", format!("could not read /dev/urandom: {e}")))?;
    std::io::Read::read_exact(&mut file, &mut bytes)
        .map_err(|e| fail("plan-id", format!("could not read /dev/urandom: {e}")))?;
    Ok(bytes.iter().map(|b| format!("{b:02x}")).collect())
}

fn write_record(
    repo: &gix::Repository,
    data: &Value,
    parents: &[ObjectId],
) -> Result<ObjectId, Fail> {
    let bytes = serde_json::to_vec(data).map_err(|e| fail("record", e.to_string()))?;
    let blob = repo
        .write_blob(bytes)
        .map_err(|e| fail("record", e.to_string()))?
        .detach();
    let mode = gix::objs::tree::EntryMode::try_from(0o100644u32)
        .map_err(|_| fail("record", "blob mode"))?;
    let tree = gix::objs::Tree {
        entries: vec![gix::objs::tree::Entry {
            mode,
            filename: "plan.json".into(),
            oid: blob,
        }],
    };
    let tree_id = repo
        .write_object(&tree)
        .map_err(|e| fail("record", e.to_string()))?
        .detach();
    let now = gix::date::Time::now_local_or_utc();
    let who = replay::configured_committer(repo, now)
        .map_err(|e| fail("missing-identity", e.to_string()))?;
    let commit = gix::objs::Commit {
        tree: tree_id,
        parents: parents.to_vec().into(),
        author: who.clone(),
        committer: who,
        encoding: None,
        message: RECORD_MESSAGE.into(),
        extra_headers: vec![],
    };
    repo.write_object(&commit)
        .map(|id| id.detach())
        .map_err(|e| fail("record", e.to_string()))
}

fn persist(disk: &gix::Repository, mem: &mut gix::Repository) -> Result<(), Fail> {
    let Some(mut objects) = mem.objects.take_object_memory() else {
        return Ok(());
    };
    for (_, (kind, data)) in objects.drain() {
        disk.objects
            .write_buf(kind, &data)
            .map_err(|e| fail("persist", format!("could not write replayed objects: {e}")))?;
    }
    Ok(())
}

fn status_word(status: Status) -> &'static str {
    match status {
        Status::Clean => "clean",
        Status::Unchanged => "unchanged",
        Status::Conflicted => "conflict",
        Status::Empty => "empty",
        Status::EmptyStart => "empty-start",
    }
}

fn replay_error(err: ReplayError) -> Fail {
    match err {
        ReplayError::MergeCommit(id) => fail("merge-commit", format!("commit {id} is a merge")),
        ReplayError::RootCommit(id) => fail("root-commit", format!("commit {id} is a root commit")),
        ReplayError::NoIdentity => fail(
            "missing-identity",
            "missing Git identity: set user.name and user.email in the repository",
        ),
        ReplayError::Git(m) => fail("replay-failed", m),
    }
}

fn objects_present(repo: &gix::Repository, roots: &[ObjectId]) -> Result<(), Fail> {
    let mut seen = HashSet::new();
    let mut stack: Vec<ObjectId> = roots.to_vec();
    while let Some(id) = stack.pop() {
        if !seen.insert(id) {
            continue;
        }
        let obj = repo.find_object(id).map_err(|_| {
            fail(
                "missing-object",
                format!("missing object {id}; nothing was backed up"),
            )
        })?;
        match obj.kind {
            gix::object::Kind::Blob => {}
            gix::object::Kind::Commit => {
                let commit = obj
                    .try_into_commit()
                    .map_err(|e| fail("missing-object", e.to_string()))?;
                let tree = commit
                    .tree_id()
                    .map_err(|e| fail("missing-object", e.to_string()))?
                    .detach();
                stack.push(tree);
                for parent in commit.parent_ids() {
                    stack.push(parent.detach());
                }
            }
            gix::object::Kind::Tree => {
                let tree = obj
                    .try_into_tree()
                    .map_err(|e| fail("missing-object", e.to_string()))?;
                for entry in tree.iter() {
                    let entry = entry.map_err(|e| fail("missing-object", e.to_string()))?;
                    stack.push(entry.oid().to_owned());
                }
            }
            gix::object::Kind::Tag => {
                let tag = obj
                    .try_into_tag()
                    .map_err(|e| fail("missing-object", e.to_string()))?;
                stack.push(
                    tag.target_id()
                        .map_err(|e| fail("missing-object", e.to_string()))?
                        .detach(),
                );
            }
        }
    }
    Ok(())
}

fn range_diff(
    root: &Path,
    old_base: ObjectId,
    old: ObjectId,
    new_base: ObjectId,
    new: ObjectId,
) -> Result<String, Fail> {
    let hex = |id: ObjectId| {
        Hex40::parse(&id.to_string())
            .ok_or_else(|| fail("range-diff", format!("not a SHA-1 id: {id}")))
    };
    let out = git_runner::shared()
        .run(
            root,
            Op::RangeDiff {
                old_base: hex(old_base)?,
                old_head: hex(old)?,
                new_base: hex(new_base)?,
                new_head: hex(new)?,
            },
        )
        .map_err(|e| fail("range-diff", e.to_string()))?;
    Ok(String::from_utf8_lossy(&out.stdout).trim_end().to_string())
}

fn put(map: &mut Map<String, Value>, key: &str, value: Value) {
    map.insert(key.to_string(), value);
}

struct Prepared {
    branch: String,
    onto: String,
    old: ObjectId,
    onto_oid: ObjectId,
    new: ObjectId,
    oldbase: ObjectId,
    pairs: Vec<String>,
    backup: Option<String>,
}

fn oid_field(v: &Value, key: &str) -> Result<ObjectId, Fail> {
    let s = v
        .get(key)
        .and_then(Value::as_str)
        .ok_or_else(|| fail("record", format!("record has no {key}")))?;
    ObjectId::from_hex(s.as_bytes())
        .map_err(|_| fail("record", format!("record {key} is not an object id")))
}

fn load_record(repo: &gix::Repository, reference: &str) -> Result<Prepared, Fail> {
    let record = direct(repo, reference)?;
    let commit = repo
        .find_commit(record)
        .map_err(|e| fail("record", e.to_string()))?;
    let tree = commit.tree().map_err(|e| fail("record", e.to_string()))?;
    let entry = tree
        .lookup_entry_by_path("plan.json")
        .map_err(|e| fail("record", e.to_string()))?
        .ok_or_else(|| fail("record", "plan.json is missing from the record"))?;
    let blob = repo
        .find_object(entry.object_id())
        .map_err(|e| fail("record", e.to_string()))?;
    let data: Value =
        serde_json::from_slice(&blob.data).map_err(|e| fail("record", e.to_string()))?;
    if !data.is_object() {
        return Err(fail("record", "invalid record"));
    }
    let need = |k: &str| {
        data.get(k)
            .and_then(Value::as_str)
            .map(str::to_string)
            .ok_or_else(|| fail("record", format!("record has no {k}")))
    };
    let pairs = data
        .get("pairs")
        .and_then(Value::as_array)
        .ok_or_else(|| fail("record", "record has no pairs"))?
        .iter()
        .map(|v| {
            v.as_str()
                .map(str::to_string)
                .ok_or_else(|| fail("record", "a pair is not a string"))
        })
        .collect::<Result<Vec<_>, Fail>>()?;
    Ok(Prepared {
        branch: need("branch")?,
        onto: need("onto")?,
        old: oid_field(&data, "old")?,
        onto_oid: oid_field(&data, "onto_oid")?,
        new: oid_field(&data, "new")?,
        oldbase: oid_field(&data, "oldbase")?,
        pairs,
        backup: data
            .get("backup")
            .and_then(Value::as_str)
            .map(str::to_string),
    })
}

fn read_token(file: &Path) -> Result<String, Fail> {
    let text =
        fs::read_to_string(file).map_err(|e| fail("plan-file", format!("plan file: {e}")))?;
    let id = text.trim();
    if id.len() != 32 || !id.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f')) {
        return Err(fail("plan-token", "invalid plan token"));
    }
    Ok(id.to_string())
}

fn unknown(backup: &str, detail: impl Into<String>) -> Fail {
    fail(
        "outcome-unknown",
        format!(
            "after backup {backup}, branch update outcome unknown; counter, backups and pruning may remain: {}",
            detail.into()
        ),
    )
}

pub fn prepare(root: &Path, branch: &str, onto: &str, file: &Path) -> Result<Report, Fail> {
    let repo = open_repo(root)?;
    let branch = branch_name(branch)?;
    let onto = branch_name(onto)?;
    if branch == onto {
        return Err(fail("same-branch", "branch and onto must differ"));
    }
    inspect(&repo, &branch)?;
    let old = direct(&repo, &branch)?;
    let onto_oid = direct(&repo, &onto)?;
    let oldbase = repo
        .merge_base(old, onto_oid)
        .map_err(|e| fail("merge-base", e.to_string()))?
        .ok_or_else(|| fail("unrelated", "branch and onto have no merge base"))?
        .detach();
    let now = gix::date::Time::now_local_or_utc();
    let committer = replay::configured_committer(&repo, now).map_err(replay_error)?;
    let mut replayed =
        replay::replay_range(&repo, onto_oid, oldbase, old, &committer).map_err(replay_error)?;
    if replayed.conflicted() {
        return Err(fail("conflict", "conflict"));
    }
    let new = replayed.tip;
    if new == old {
        return Err(fail("nothing-to-apply", "nothing to apply"));
    }
    let mut pairs = Vec::new();
    for step in &replayed.steps {
        let word = status_word(step.status);
        let line = match step.new {
            Some(id) => format!("{old} {id} {word}", old = step.old),
            None => format!("{} - {word}", step.old),
        };
        pairs.push(line);
    }
    persist(&repo, &mut replayed.repo)?;
    // An empty replay (`new` is `onto`) makes `git range-diff` exit 129. A failure of a real
    // range-diff happens before the record, the prepared ref and the plan file exist.
    let diff = if new == onto_oid {
        String::new()
    } else {
        range_diff(root, oldbase, old, onto_oid, new)?
    };
    let mut data = Map::new();
    put(&mut data, "branch", json!(branch));
    put(&mut data, "new", json!(new.to_string()));
    put(&mut data, "old", json!(old.to_string()));
    put(&mut data, "oldbase", json!(oldbase.to_string()));
    put(&mut data, "onto", json!(onto));
    put(&mut data, "onto_oid", json!(onto_oid.to_string()));
    put(
        &mut data,
        "pairs",
        Value::Array(pairs.iter().cloned().map(Value::String).collect()),
    );
    let record = write_record(&repo, &Value::Object(data), &[old, onto_oid, new])?;
    let id = new_id()?;
    let prepared = format!("refs/stw/restack-prepared/{id}");
    commit_edits(
        &repo,
        vec![create_edit(
            full_name(&prepared)?,
            record,
            "stw restack prepare",
        )],
    )
    .map_err(|e| fail("prepared-ref", e))?;
    log_update(&repo, &prepared, None, Some(record));
    let mut exclusive = OpenOptions::new();
    exclusive.write(true).create_new(true);
    match exclusive.open(file) {
        Ok(mut f) => {
            if let Err(e) = writeln!(f, "{id}") {
                return Err(fail(
                    "plan-file",
                    format!("plan file failed; retained prepared ref {prepared}: {e}"),
                ));
            }
        }
        Err(e) => {
            return Err(fail(
                "plan-file",
                format!("plan file failed; retained prepared ref {prepared}: {e}"),
            ));
        }
    }
    let mut text = format!(
        "plan {id}\nbranch {branch}\nonto {onto} {onto_oid}\nold {old}\nnew {new}\nprepared {prepared}\n"
    );
    text.push_str(&pairs.join("\n"));
    text.push('\n');
    if !diff.is_empty() {
        text.push_str(&diff);
        text.push('\n');
    }
    text.push_str("Apply with: stw restack apply PLAN --yes\n");
    let value = json!({
        "schema_version": SCHEMA_VERSION,
        "command": "restack prepare",
        "outcome": "ok",
        "id": id,
        "branch": branch,
        "onto": onto,
        "onto_oid": onto_oid.to_string(),
        "old": old.to_string(),
        "new": new.to_string(),
        "oldbase": oldbase.to_string(),
        "prepared": prepared,
        "pairs": pairs,
        "range_diff": diff,
        "plan_file": file.display().to_string(),
    });
    Ok(Report { text, value })
}

fn finish_tx(
    repo: &gix::Repository,
    backup_ref: &str,
    edits: Vec<RefEdit>,
    before_tx: &mut dyn FnMut(),
    after_commit: &mut dyn FnMut() -> Result<(), String>,
    branch: &str,
) -> Result<(), Fail> {
    before_tx();
    if let Err(e) = inspect(repo, branch) {
        return Err(unknown(
            backup_ref,
            format!("inspect failed before the transaction: {}", e.message),
        ));
    }
    if let Err(e) = commit_edits(repo, edits) {
        return Err(unknown(backup_ref, e));
    }
    if let Err(e) = after_commit() {
        return Err(unknown(backup_ref, e));
    }
    Ok(())
}

pub fn apply(root: &Path, file: &Path) -> Result<Report, Fail> {
    let mut before = || {};
    let mut after = || Ok(());
    apply_hooked(root, file, &mut before, &mut after)
}

pub fn undo(root: &Path, file: &Path) -> Result<Report, Fail> {
    let mut before = || {};
    let mut after = || Ok(());
    undo_hooked(root, file, &mut before, &mut after)
}

/// `apply` with hooks around the branch transaction. `before_tx` runs after the backup exists.
/// `after_commit` runs only when the transaction committed; an error from it is reported as an unknown
/// outcome because the process can no longer say whether the caller observed the update.
pub fn apply_hooked(
    root: &Path,
    file: &Path,
    before_tx: &mut dyn FnMut(),
    after_commit: &mut dyn FnMut() -> Result<(), String>,
) -> Result<Report, Fail> {
    let repo = open_repo(root)?;
    let id = read_token(file)?;
    let prepared_ref = format!("refs/stw/restack-prepared/{id}");
    let plan = load_record(&repo, &prepared_ref)?;
    inspect(&repo, &plan.branch)?;
    let branch_now = direct(&repo, &plan.branch)?;
    if branch_now != plan.old {
        return Err(fail(
            "stale-branch",
            format!(
                "stale branch: {} moved since the plan and was left at {branch_now}",
                plan.branch
            ),
        ));
    }
    let onto_now = direct(&repo, &plan.onto)?;
    if onto_now != plan.onto_oid {
        return Err(fail(
            "stale-onto",
            format!(
                "stale onto: {} moved since the plan and was left at {onto_now}",
                plan.onto
            ),
        ));
    }
    objects_present(&repo, &[plan.old, plan.onto_oid, plan.new])?;
    let rec = direct(&repo, &prepared_ref)?;
    let saved = backup(&repo, plan.old, None)?;
    let mut data = Map::new();
    put(&mut data, "backup", json!(saved.reference));
    put(&mut data, "branch", json!(plan.branch));
    put(&mut data, "new", json!(plan.new.to_string()));
    put(&mut data, "old", json!(plan.old.to_string()));
    put(&mut data, "oldbase", json!(plan.oldbase.to_string()));
    put(&mut data, "onto", json!(plan.onto));
    put(&mut data, "onto_oid", json!(plan.onto_oid.to_string()));
    put(
        &mut data,
        "pairs",
        Value::Array(plan.pairs.iter().cloned().map(Value::String).collect()),
    );
    let receipt = write_record(&repo, &Value::Object(data), &[rec, plan.new, plan.old])?;
    let applied = format!("refs/stw/restack-applied/{id}");
    let edits = vec![
        update_edit(full_name(&prepared_ref)?, rec, rec, "stw restack apply"),
        update_edit(
            full_name(&plan.onto)?,
            plan.onto_oid,
            plan.onto_oid,
            "stw restack apply",
        ),
        create_edit(full_name(&applied)?, receipt, "stw restack apply"),
        update_edit(
            full_name(&plan.branch)?,
            plan.old,
            plan.new,
            "stw restack apply",
        ),
    ];
    finish_tx(
        &repo,
        &saved.reference,
        edits,
        before_tx,
        after_commit,
        &plan.branch,
    )?;
    log_update(&repo, &plan.branch, Some(plan.old), Some(plan.new));
    log_update(&repo, &applied, None, Some(receipt));
    let mut text = saved.lines.join("\n");
    text.push('\n');
    text.push_str(&format!(
        "applied {} {} -> {}; undo: stw restack undo PLAN --yes\n",
        plan.branch, plan.old, plan.new
    ));
    let value = json!({
        "schema_version": SCHEMA_VERSION,
        "command": "restack apply",
        "outcome": "ok",
        "id": id,
        "branch": plan.branch,
        "old": plan.old.to_string(),
        "new": plan.new.to_string(),
        "backup": saved.reference,
        "applied": applied,
    });
    Ok(Report { text, value })
}

pub fn undo_hooked(
    root: &Path,
    file: &Path,
    before_tx: &mut dyn FnMut(),
    after_commit: &mut dyn FnMut() -> Result<(), String>,
) -> Result<Report, Fail> {
    let repo = open_repo(root)?;
    let id = read_token(file)?;
    let applied = format!("refs/stw/restack-applied/{id}");
    let plan = load_record(&repo, &applied)?;
    let Some(backup_ref) = plan.backup.clone() else {
        return Err(fail("record", "applied record has no backup"));
    };
    inspect(&repo, &plan.branch)?;
    let branch_now = direct(&repo, &plan.branch)?;
    if branch_now != plan.new {
        return Err(fail(
            "stale-branch",
            format!(
                "branch moved since apply: {} is at {branch_now} and was left there",
                plan.branch
            ),
        ));
    }
    let backed = direct(&repo, &backup_ref)?;
    if backed != plan.old {
        return Err(fail(
            "backup-changed",
            format!("retained backup missing or changed: {backup_ref}"),
        ));
    }
    objects_present(&repo, &[plan.old, plan.new])?;
    let rec = direct(&repo, &applied)?;
    let saved = backup(&repo, plan.new, Some(plan.old))?;
    let Some(retained) = saved.retained.clone() else {
        return Err(fail(
            "backup-failed",
            "undo did not retain the old tip; the branch was not moved",
        ));
    };
    let edits = vec![
        update_edit(full_name(&applied)?, rec, rec, "stw restack undo"),
        update_edit(
            full_name(&retained)?,
            plan.old,
            plan.old,
            "stw restack undo",
        ),
        update_edit(
            full_name(&plan.branch)?,
            plan.new,
            plan.old,
            "stw restack undo",
        ),
    ];
    finish_tx(
        &repo,
        &saved.reference,
        edits,
        before_tx,
        after_commit,
        &plan.branch,
    )?;
    log_update(&repo, &plan.branch, Some(plan.new), Some(plan.old));
    let mut text = saved.lines.join("\n");
    text.push('\n');
    text.push_str(&format!(
        "undone {} {} -> {}; backup {}\n",
        plan.branch, plan.new, plan.old, saved.reference
    ));
    let value = json!({
        "schema_version": SCHEMA_VERSION,
        "command": "restack undo",
        "outcome": "ok",
        "id": id,
        "branch": plan.branch,
        "old": plan.old.to_string(),
        "new": plan.new.to_string(),
        "backup": saved.reference,
        "retained": retained,
        "previous_backup": backup_ref,
    });
    Ok(Report { text, value })
}
