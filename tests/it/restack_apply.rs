//! Port of `spikes/stw/restack-apply/test-restack-apply.py`, plus the path and ref-drift cases.
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::Value;

fn scratch() -> PathBuf {
    PathBuf::from("/tmp/cursor-restack-repos")
}

struct Repo {
    dir: tempfile::TempDir,
    repo: PathBuf,
    old: String,
}

impl Repo {
    fn new() -> Repo {
        let parent = scratch();
        fs::create_dir_all(&parent).unwrap();
        let dir = tempfile::tempdir_in(&parent).unwrap();
        let repo = dir.path().join("repo");
        fs::create_dir(&repo).unwrap();
        git(&repo, &["init", "-q", "-b", "main"]);
        git(&repo, &["config", "user.name", "Spike Tester"]);
        git(&repo, &["config", "user.email", "spike@example.invalid"]);
        fs::write(repo.join("base"), "base\n").unwrap();
        git(&repo, &["add", "base"]);
        git(&repo, &["commit", "-qm", "base"]);
        git(&repo, &["branch", "topic"]);
        fs::write(repo.join("upstream"), "up\n").unwrap();
        git(&repo, &["add", "upstream"]);
        git(&repo, &["commit", "-qm", "up"]);
        git(&repo, &["checkout", "-q", "topic"]);
        fs::write(repo.join("topic"), "topic\n").unwrap();
        git(&repo, &["add", "topic"]);
        git(&repo, &["commit", "-qm", "topic"]);
        let old = git(&repo, &["rev-parse", "HEAD"]);
        git(&repo, &["checkout", "-q", "main"]);
        Repo { dir, repo, old }
    }

    fn path(&self) -> &Path {
        &self.repo
    }

    fn plan_file(&self) -> PathBuf {
        self.dir.path().join("plan")
    }

    fn git(&self, args: &[&str]) -> String {
        git(self.path(), args)
    }
}

fn git(dir: &Path, args: &[&str]) -> String {
    let out = Command::new("git")
        .arg("-C")
        .arg(dir)
        .args(args)
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_AUTHOR_NAME", "Spike Tester")
        .env("GIT_AUTHOR_EMAIL", "spike@example.invalid")
        .env("GIT_COMMITTER_NAME", "Spike Tester")
        .env("GIT_COMMITTER_EMAIL", "spike@example.invalid")
        .env("GIT_TERMINAL_PROMPT", "0")
        .output()
        .unwrap();
    assert!(
        out.status.success(),
        "git {args:?} failed: {}",
        String::from_utf8_lossy(&out.stderr)
    );
    String::from_utf8_lossy(&out.stdout).trim().to_string()
}

fn stw(dir: &Path, args: &[&str]) -> (i32, String, String) {
    stw_env(dir, args, &[])
}

fn stw_env(dir: &Path, args: &[&str], extra: &[(&str, &str)]) -> (i32, String, String) {
    let mut cmd = Command::new(env!("CARGO_BIN_EXE_stw"));
    cmd.current_dir(dir)
        .args(args)
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_TERMINAL_PROMPT", "0")
        .env_remove("GIT_AUTHOR_NAME")
        .env_remove("GIT_AUTHOR_EMAIL")
        .env_remove("GIT_COMMITTER_NAME")
        .env_remove("GIT_COMMITTER_EMAIL");
    for (k, v) in extra {
        cmd.env(k, v);
    }
    let out = cmd.output().unwrap();
    (
        out.status.code().unwrap_or(1),
        String::from_utf8_lossy(&out.stdout).to_string(),
        String::from_utf8_lossy(&out.stderr).to_string(),
    )
}

fn prepare(repo: &Repo) -> (i32, String, String) {
    let file = repo.plan_file();
    stw(
        repo.path(),
        &[
            "restack",
            "prepare",
            "topic",
            "main",
            &file.to_string_lossy(),
        ],
    )
}

fn apply(repo: &Repo) -> (i32, String, String) {
    let file = repo.plan_file();
    stw(
        repo.path(),
        &["restack", "apply", &file.to_string_lossy(), "--yes"],
    )
}

fn undo(repo: &Repo) -> (i32, String, String) {
    let file = repo.plan_file();
    stw(
        repo.path(),
        &["restack", "undo", &file.to_string_lossy(), "--yes"],
    )
}

#[test]
fn apply_and_undo_round_trip_keeps_the_tree_and_the_backup() {
    let repo = Repo::new();
    let (code, out, err) = prepare(&repo);
    assert_eq!(code, 0, "{out}{err}");
    assert!(out.contains("plan "), "{out}");
    assert!(out.contains("prepared refs/stw/restack-prepared/"), "{out}");
    let id = fs::read_to_string(repo.plan_file()).unwrap();
    assert_eq!(id.trim().len(), 32);
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
    let (code, out, err) = apply(&repo);
    assert_eq!(code, 0, "{out}{err}");
    assert!(out.starts_with("backup: refs/stw/backup/1/1 "), "{out}");
    let new = repo.git(&["rev-parse", "topic"]);
    assert_ne!(new, repo.old);
    assert_eq!(
        repo.git(&["rev-parse", "topic:topic"]),
        repo.git(&["rev-parse", &format!("{}:topic", repo.old)])
    );
    assert_eq!(repo.git(&["rev-parse", "refs/stw/backup/1/1"]), repo.old);
    let (code, out, err) = undo(&repo);
    assert_eq!(code, 0, "{out}{err}");
    assert!(out.contains("retained undo target:"), "{out}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
}

#[test]
fn stale_branch_and_stale_onto_change_nothing_and_write_no_backup() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    repo.git(&["update-ref", "refs/heads/topic", "main", &repo.old]);
    let (code, _, err) = apply(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("stale branch"), "{err}");
    assert_eq!(
        repo.git(&["rev-parse", "topic"]),
        repo.git(&["rev-parse", "main"])
    );
    assert_ne!(
        Command::new("git")
            .arg("-C")
            .arg(repo.path())
            .args(["show-ref", "--verify", "--quiet", "refs/stw/backup-counter"])
            .status()
            .unwrap()
            .code(),
        Some(0)
    );

    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    repo.git(&["update-ref", "refs/heads/main", &repo.old]);
    let (code, _, err) = apply(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("stale onto"), "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
}

#[test]
fn a_checked_out_branch_is_refused_for_prepare_and_apply() {
    let repo = Repo::new();
    repo.git(&["checkout", "-q", "topic"]);
    let (code, _, err) = prepare(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("checked out"), "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);

    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    let other = repo.dir.path().join("other");
    repo.git(&["worktree", "add", "-q", &other.to_string_lossy(), "topic"]);
    let (code, _, err) = apply(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("checked out"), "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
}

#[test]
fn undo_of_a_moved_branch_leaves_the_newer_value() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    assert_eq!(apply(&repo).0, 0);
    repo.git(&["update-ref", "refs/heads/topic", "main"]);
    let (code, _, err) = undo(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("moved since apply"), "{err}");
    assert_eq!(
        repo.git(&["rev-parse", "topic"]),
        repo.git(&["rev-parse", "main"])
    );
}

#[test]
fn candidate_and_backup_survive_gc() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    repo.git(&["reflog", "expire", "--expire=now", "--all"]);
    repo.git(&["gc", "--prune=now"]);
    assert_eq!(apply(&repo).0, 0);
    repo.git(&["reflog", "expire", "--expire=now", "--all"]);
    repo.git(&["gc", "--prune=now"]);
    assert_eq!(undo(&repo).0, 0);
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
}

#[test]
fn a_missing_backup_refuses_undo_and_a_backup_lock_keeps_the_branch() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    assert_eq!(apply(&repo).0, 0);
    let new = repo.git(&["rev-parse", "topic"]);
    repo.git(&["update-ref", "-d", "refs/stw/backup/1/1"]);
    let (code, _, err) = undo(&repo);
    assert_eq!(code, 1, "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), new);

    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    let gitdir = repo.git(&["rev-parse", "--absolute-git-dir"]);
    let lock = PathBuf::from(gitdir).join("refs/stw/backup/1/1.lock");
    fs::create_dir_all(lock.parent().unwrap()).unwrap();
    fs::write(&lock, b"").unwrap();
    let (code, _, err) = apply(&repo);
    assert_eq!(code, 1, "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
}

#[test]
fn a_conflict_or_a_merge_refuses_and_writes_nothing() {
    let repo = Repo::new();
    repo.git(&["checkout", "-q", "topic"]);
    fs::write(repo.path().join("base"), "topic\n").unwrap();
    repo.git(&["add", "base"]);
    repo.git(&["commit", "-qm", "topic conflict"]);
    let tip = repo.git(&["rev-parse", "topic"]);
    repo.git(&["checkout", "-q", "main"]);
    fs::write(repo.path().join("base"), "upstream\n").unwrap();
    repo.git(&["add", "base"]);
    repo.git(&["commit", "-qm", "up conflict"]);
    let (code, _, err) = prepare(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("conflict"), "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), tip);
    assert!(!repo.plan_file().exists());

    let repo = Repo::new();
    repo.git(&["checkout", "-q", "topic"]);
    repo.git(&["merge", "-q", "--no-ff", "main", "-m", "merge"]);
    let tip = repo.git(&["rev-parse", "topic"]);
    repo.git(&["checkout", "-q", "main"]);
    let (code, _, err) = prepare(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("merge"), "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), tip);
}

#[test]
fn replay_matches_git_rebase_and_keeps_the_author() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    repo.git(&["checkout", "-q", "--detach", &repo.old]);
    repo.git(&["rebase", "main"]);
    let oracle_tree = repo.git(&["rev-parse", "HEAD^{tree}"]);
    let oracle = repo.git(&["log", "-1", "--format=%s%n%an <%ae>%n%aI", "HEAD"]);
    repo.git(&["checkout", "-q", "main"]);
    assert_eq!(apply(&repo).0, 0);
    assert_eq!(repo.git(&["rev-parse", "topic^{tree}"]), oracle_tree);
    assert_eq!(
        repo.git(&["log", "-1", "--format=%s%n%an <%ae>%n%aI", "topic"]),
        oracle
    );
    assert_eq!(
        repo.git(&["rev-parse", "topic^"]),
        repo.git(&["rev-parse", "main"])
    );
    assert_eq!(
        repo.git(&["log", "-1", "--format=%cn <%ce>", "topic"]),
        "Spike Tester <spike@example.invalid>"
    );
}

#[test]
fn an_operation_in_progress_and_a_symbolic_branch_are_refused() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    let gitdir = repo.git(&["rev-parse", "--absolute-git-dir"]);
    fs::write(
        PathBuf::from(&gitdir).join("MERGE_HEAD"),
        format!("{}\n", repo.old),
    )
    .unwrap();
    let (code, _, err) = apply(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("operation in progress"), "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
    fs::remove_file(PathBuf::from(&gitdir).join("MERGE_HEAD")).unwrap();
    fs::create_dir(PathBuf::from(&gitdir).join("rebase-merge")).unwrap();
    let (code, _, err) = apply(&repo);
    assert_eq!(code, 1, "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);

    let repo = Repo::new();
    repo.git(&["symbolic-ref", "refs/heads/alias", "refs/heads/topic"]);
    let (code, _, err) = stw(
        repo.path(),
        &[
            "restack",
            "prepare",
            "alias",
            "main",
            &repo.plan_file().to_string_lossy(),
        ],
    );
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("symbolic"), "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
}

#[test]
fn git_dir_is_refused_and_neither_repository_moves() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    let other = repo.dir.path().join("other-repo");
    let status = Command::new("cp")
        .args([
            "-a",
            &repo.path().to_string_lossy(),
            &other.to_string_lossy(),
        ])
        .status()
        .unwrap();
    assert!(status.success());
    let (code, _, err) = stw_env(
        repo.path(),
        &[
            "restack",
            "apply",
            &repo.plan_file().to_string_lossy(),
            "--yes",
        ],
        &[
            ("GIT_DIR", &other.join(".git").to_string_lossy()),
            ("GIT_WORK_TREE", &other.to_string_lossy()),
        ],
    );
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("GIT_"), "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
    assert_eq!(git(&other, &["rev-parse", "topic"]), repo.old);
}

#[test]
fn undo_keeps_the_old_tip_across_the_retention_boundary() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    assert_eq!(apply(&repo).0, 0);
    let new = repo.git(&["rev-parse", "topic"]);
    for n in 2..=10 {
        repo.git(&["update-ref", &format!("refs/stw/backup/{n}/1"), &new]);
    }
    let (code, _, err) = undo(&repo);
    assert_eq!(code, 0, "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
    assert_eq!(repo.git(&["rev-parse", "refs/stw/backup/11/2"]), repo.old);
    let refs = repo.git(&["for-each-ref", "--format=%(refname)", "refs/stw/backup/"]);
    assert_eq!(refs.lines().count(), 11, "{refs}");
    let gone = Command::new("git")
        .arg("-C")
        .arg(repo.path())
        .args(["show-ref", "--verify", "--quiet", "refs/stw/backup/1/1"])
        .status()
        .unwrap();
    assert!(!gone.success());
}

#[test]
fn a_foreign_committer_is_replaced_by_the_configured_identity() {
    let repo = Repo::new();
    let raw = Command::new("git")
        .arg("-C")
        .arg(repo.path())
        .args(["cat-file", "commit", &repo.old])
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .output()
        .unwrap()
        .stdout;
    let mut out = Vec::new();
    for line in raw.split_inclusive(|b| *b == b'\n') {
        if line.starts_with(b"committer ") {
            out.extend_from_slice(
                b"committer Foreign Source <foreign@example.invalid> 1600000000 +0000\n",
            );
        } else {
            out.extend_from_slice(line);
        }
    }
    let mut written = Command::new("git")
        .arg("-C")
        .arg(repo.path())
        .args(["hash-object", "-t", "commit", "-w", "--stdin"])
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .spawn()
        .unwrap();
    {
        use std::io::Write;
        written.stdin.take().unwrap().write_all(&out).unwrap();
    }
    let done = written.wait_with_output().unwrap();
    assert!(done.status.success());
    let old = String::from_utf8(done.stdout).unwrap();
    let old = old.trim();
    repo.git(&["update-ref", "refs/heads/topic", old, &repo.old]);
    let author = repo.git(&["log", "-1", "--format=%an <%ae>%n%aI", old]);
    assert_eq!(prepare(&repo).0, 0);
    assert_eq!(apply(&repo).0, 0);
    assert_eq!(
        repo.git(&["log", "-1", "--format=%an <%ae>%n%aI", "topic"]),
        author
    );
    assert_eq!(
        repo.git(&["log", "-1", "--format=%cn <%ce>", "topic"]),
        "Spike Tester <spike@example.invalid>"
    );
}

#[test]
fn a_missing_blob_is_refused_before_the_backup_counter_exists() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    let blob = repo.git(&["rev-parse", &format!("{}:topic", repo.old)]);
    let gitdir = repo.git(&["rev-parse", "--absolute-git-dir"]);
    let path = PathBuf::from(gitdir)
        .join("objects")
        .join(&blob[..2])
        .join(&blob[2..]);
    fs::remove_file(&path).unwrap();
    let (code, _, err) = apply(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("missing object"), "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
    let counter = Command::new("git")
        .arg("-C")
        .arg(repo.path())
        .args(["show-ref", "--verify", "--quiet", "refs/stw/backup-counter"])
        .status()
        .unwrap();
    assert!(!counter.success());
}

#[test]
fn the_counter_has_no_forced_reflog_but_the_branch_and_backup_do() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    let gitdir = PathBuf::from(repo.git(&["rev-parse", "--absolute-git-dir"]));
    let branch_log = gitdir.join("logs/refs/heads/topic");
    fs::remove_file(&branch_log).unwrap();
    assert_eq!(apply(&repo).0, 0);
    assert!(!gitdir.join("logs/refs/stw/backup-counter").exists());
    assert!(gitdir.join("logs/refs/stw/backup/1/1").is_file());
    assert!(branch_log.is_file());
}

#[test]
fn a_failed_refs_log_is_reported_and_the_branch_still_moves() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    let gitdir =
        PathBuf::from(repo.git(&["rev-parse", "--path-format=absolute", "--git-common-dir"]));
    let log = gitdir.join("stw/oplog/refs.log");
    if log.is_file() {
        fs::remove_file(&log).unwrap();
    }
    fs::create_dir_all(&log).unwrap();
    let (code, _, err) = apply(&repo);
    assert_eq!(code, 0, "{err}");
    assert!(err.contains("was not written"), "{err}");
    let errors = fs::read_to_string(gitdir.join("stw/oplog/errors.log")).unwrap();
    assert!(errors.contains("write-failed"), "{errors}");
    assert_ne!(repo.git(&["rev-parse", "topic"]), repo.old);
}

#[test]
fn a_plan_file_is_not_overwritten_and_a_used_plan_is_not_applied_again() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    let body = fs::read_to_string(repo.plan_file()).unwrap();
    let (code, _, err) = prepare(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("plan file failed"), "{err}");
    assert_eq!(fs::read_to_string(repo.plan_file()).unwrap(), body);
    assert_eq!(repo.git(&["rev-parse", "topic"]), repo.old);
    assert_eq!(apply(&repo).0, 0);
    let new = repo.git(&["rev-parse", "topic"]);
    let (code, _, err) = apply(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("stale branch"), "{err}");
    assert_eq!(repo.git(&["rev-parse", "topic"]), new);
}

#[test]
fn a_deleted_branch_is_refused_and_the_same_value_recreated_still_applies() {
    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    repo.git(&["update-ref", "-d", "refs/heads/topic"]);
    let (code, _, err) = apply(&repo);
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("missing ref"), "{err}");

    let repo = Repo::new();
    assert_eq!(prepare(&repo).0, 0);
    repo.git(&["update-ref", "-d", "refs/heads/topic"]);
    repo.git(&["update-ref", "refs/heads/topic", &repo.old]);
    assert_eq!(apply(&repo).0, 0);
    assert_ne!(repo.git(&["rev-parse", "topic"]), repo.old);
}

#[test]
fn paths_with_spaces_non_ascii_a_leading_dash_and_quotes_are_replayed() {
    let repo = Repo::new();
    repo.git(&["checkout", "-q", "topic"]);
    for name in ["my file.txt", "café.txt", "-dash.txt", "say\"hi\".txt"] {
        fs::write(repo.path().join(name), b"x\n").unwrap();
        repo.git(&["add", "--", name]);
    }
    repo.git(&["commit", "-qm", "odd paths"]);
    repo.git(&["checkout", "-q", "main"]);
    assert_eq!(prepare(&repo).0, 0);
    assert_eq!(apply(&repo).0, 0);
    let listed = Command::new("git")
        .arg("-C")
        .arg(repo.path())
        .args(["ls-tree", "-r", "-z", "--name-only", "topic"])
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .output()
        .unwrap();
    assert!(listed.status.success());
    let names = String::from_utf8(listed.stdout).unwrap();
    for name in ["my file.txt", "café.txt", "-dash.txt", "say\"hi\".txt"] {
        assert!(names.split('\0').any(|l| l == name), "{names:?}");
    }
}

#[test]
fn a_symlink_and_the_executable_bit_survive_the_replay() {
    let repo = Repo::new();
    repo.git(&["checkout", "-q", "topic"]);
    fs::write(repo.path().join("run"), b"#!/bin/sh\n").unwrap();
    repo.git(&["add", "run"]);
    repo.git(&["update-index", "--chmod=+x", "run"]);
    std::os::unix::fs::symlink("run", repo.path().join("link")).unwrap();
    repo.git(&["add", "link"]);
    repo.git(&["commit", "-qm", "modes"]);
    repo.git(&["checkout", "-qf", "main"]);
    assert_eq!(prepare(&repo).0, 0);
    assert_eq!(apply(&repo).0, 0);
    let tree = repo.git(&["ls-tree", "topic"]);
    assert!(
        tree.contains("100755 blob") && tree.contains("run"),
        "{tree}"
    );
    assert!(
        tree.contains("120000 blob") && tree.contains("link"),
        "{tree}"
    );
}

#[test]
fn home_config_and_hooks_do_not_affect_the_replay() {
    let repo = Repo::new();
    let home = repo.dir.path().join("home");
    fs::create_dir_all(home.join("hooks")).unwrap();
    let marker = repo.dir.path().join("hook-ran");
    fs::write(
        home.join("hooks/post-rewrite"),
        format!("#!/bin/sh\necho ran >> {}\n", marker.display()),
    )
    .unwrap();
    fs::write(
        home.join("hooks/reference-transaction"),
        format!("#!/bin/sh\necho ran >> {}\n", marker.display()),
    )
    .unwrap();
    Command::new("chmod")
        .args(["+x", &home.join("hooks/post-rewrite").to_string_lossy()])
        .status()
        .unwrap();
    Command::new("chmod")
        .args([
            "+x",
            &home.join("hooks/reference-transaction").to_string_lossy(),
        ])
        .status()
        .unwrap();
    fs::write(
        home.join(".gitconfig"),
        "[diff]\n\texternal = /bin/false\n[rebase]\n\tinstructionFormat = BROKEN %s\n[core]\n\thooksPath = hooks\n[commit]\n\tgpgsign = true\n[rerere]\n\tenabled = true\n[apply]\n\twhitespace = error\n",
    )
    .unwrap();
    let file = repo.plan_file();
    let (code, out, err) = stw_env(
        repo.path(),
        &[
            "restack",
            "prepare",
            "topic",
            "main",
            &file.to_string_lossy(),
        ],
        &[("HOME", &home.to_string_lossy())],
    );
    assert_eq!(code, 0, "{out}{err}");
    let (code, out, err) = stw_env(
        repo.path(),
        &["restack", "apply", &file.to_string_lossy(), "--yes"],
        &[("HOME", &home.to_string_lossy())],
    );
    assert_eq!(code, 0, "{out}{err}");
    assert!(!marker.exists());
    assert_eq!(
        repo.git(&["rev-parse", "topic:topic"]),
        repo.git(&["rev-parse", &format!("{}:topic", repo.old)])
    );
}

#[test]
fn json_names_the_command_and_a_missing_yes_is_usage() {
    let repo = Repo::new();
    let file = repo.plan_file();
    let (code, out, err) = stw(
        repo.path(),
        &[
            "restack",
            "prepare",
            "--json",
            "topic",
            "main",
            &file.to_string_lossy(),
        ],
    );
    assert_eq!(code, 0, "{out}{err}");
    let v: Value = serde_json::from_str(&out).unwrap();
    assert_eq!(v["schema_version"], 1);
    assert_eq!(v["command"], "restack prepare");
    assert_eq!(v["outcome"], "ok");
    let (code, out, err) = stw(
        repo.path(),
        &[
            "restack",
            "apply",
            "--json",
            &file.to_string_lossy(),
            "--yes",
        ],
    );
    assert_eq!(code, 0, "{out}{err}");
    let v: Value = serde_json::from_str(&out).unwrap();
    assert_eq!(v["command"], "restack apply");
    assert_eq!(v["schema_version"], 1);
    let (code, _, _) = stw(repo.path(), &["restack", "undo", &file.to_string_lossy()]);
    assert_eq!(code, 2);
}

#[test]
fn a_branch_name_with_a_space_is_refused_and_a_non_ascii_name_is_accepted() {
    let repo = Repo::new();
    let (code, _, err) = stw(
        repo.path(),
        &[
            "restack",
            "prepare",
            "my branch",
            "main",
            &repo.plan_file().to_string_lossy(),
        ],
    );
    assert_eq!(code, 1, "{err}");
    assert!(err.contains("refused"), "{err}");
    repo.git(&["branch", "töpíc", "topic"]);
    let (code, out, err) = stw(
        repo.path(),
        &[
            "restack",
            "prepare",
            "töpíc",
            "main",
            &repo.dir.path().join("plan-unicode").to_string_lossy(),
        ],
    );
    assert_eq!(code, 0, "{out}{err}");
    assert!(out.contains("refs/heads/töpíc"), "{out}");
}

/// A repository whose work tree is the temp directory itself, so the plan file can sit beside it
/// without being a second checkout.
struct Flat {
    dir: tempfile::TempDir,
    old: String,
}

impl Flat {
    fn new() -> Flat {
        let parent = scratch();
        fs::create_dir_all(&parent).unwrap();
        let dir = tempfile::tempdir_in(&parent).unwrap();
        let repo = dir.path().join("repo");
        fs::create_dir(&repo).unwrap();
        git(&repo, &["init", "-q", "-b", "main"]);
        git(&repo, &["config", "user.name", "Spike Tester"]);
        git(&repo, &["config", "user.email", "spike@example.invalid"]);
        fs::write(repo.join("base"), "base\n").unwrap();
        git(&repo, &["add", "base"]);
        git(&repo, &["commit", "-qm", "base"]);
        git(&repo, &["branch", "topic"]);
        fs::write(repo.join("upstream"), "up\n").unwrap();
        git(&repo, &["add", "upstream"]);
        git(&repo, &["commit", "-qm", "up"]);
        git(&repo, &["checkout", "-q", "topic"]);
        fs::write(repo.join("topic"), "topic\n").unwrap();
        git(&repo, &["add", "topic"]);
        git(&repo, &["commit", "-qm", "topic"]);
        let old = git(&repo, &["rev-parse", "HEAD"]);
        git(&repo, &["checkout", "-q", "main"]);
        Flat { dir, old }
    }

    fn path(&self) -> PathBuf {
        self.dir.path().join("repo")
    }

    fn plan(&self) -> PathBuf {
        self.dir.path().join("plan")
    }
}

#[test]
fn a_branch_moved_during_the_transaction_keeps_the_winner_and_the_backup() {
    let fix = Flat::new();
    stw::restack_apply::prepare(&fix.path(), "topic", "main", &fix.plan()).unwrap();
    let winner = git(&fix.path(), &["rev-parse", "main"]);
    let err = stw::restack_apply::apply_hooked(
        &fix.path(),
        &fix.plan(),
        &mut || {
            git(&fix.path(), &["update-ref", "refs/heads/topic", &winner]);
        },
        &mut || Ok(()),
    )
    .unwrap_err();
    assert_eq!(err.code, "outcome-unknown");
    assert!(err.message.contains("outcome unknown"), "{}", err.message);
    assert!(err.message.contains("may remain"), "{}", err.message);
    assert_eq!(git(&fix.path(), &["rev-parse", "topic"]), winner);
    assert_eq!(
        git(&fix.path(), &["rev-parse", "refs/stw/backup/1/1"]),
        fix.old
    );
}

#[test]
fn an_onto_moved_during_the_transaction_does_not_move_the_branch() {
    let fix = Flat::new();
    stw::restack_apply::prepare(&fix.path(), "topic", "main", &fix.plan()).unwrap();
    let err = stw::restack_apply::apply_hooked(
        &fix.path(),
        &fix.plan(),
        &mut || {
            git(&fix.path(), &["update-ref", "refs/heads/main", &fix.old]);
        },
        &mut || Ok(()),
    )
    .unwrap_err();
    assert_eq!(err.code, "outcome-unknown");
    assert_eq!(git(&fix.path(), &["rev-parse", "topic"]), fix.old);
    assert_eq!(
        git(&fix.path(), &["rev-parse", "refs/stw/backup/1/1"]),
        fix.old
    );
    assert_eq!(git(&fix.path(), &["rev-parse", "main"]), fix.old);
}

#[test]
fn a_symbolic_replacement_is_not_followed() {
    let fix = Flat::new();
    stw::restack_apply::prepare(&fix.path(), "topic", "main", &fix.plan()).unwrap();
    git(&fix.path(), &["branch", "victim", &fix.old]);
    let err = stw::restack_apply::apply_hooked(
        &fix.path(),
        &fix.plan(),
        &mut || {
            git(
                &fix.path(),
                &["symbolic-ref", "refs/heads/topic", "refs/heads/victim"],
            );
        },
        &mut || Ok(()),
    )
    .unwrap_err();
    assert_eq!(err.code, "outcome-unknown");
    assert_eq!(
        git(&fix.path(), &["symbolic-ref", "refs/heads/topic"]),
        "refs/heads/victim"
    );
    assert_eq!(git(&fix.path(), &["rev-parse", "victim"]), fix.old);
}

#[test]
fn a_symbolic_onto_is_not_replaced_by_a_direct_update() {
    let fix = Flat::new();
    stw::restack_apply::prepare(&fix.path(), "topic", "main", &fix.plan()).unwrap();
    let main = git(&fix.path(), &["rev-parse", "main"]);
    git(&fix.path(), &["branch", "victim", &main]);
    let err = stw::restack_apply::apply_hooked(
        &fix.path(),
        &fix.plan(),
        &mut || {
            git(
                &fix.path(),
                &["symbolic-ref", "refs/heads/main", "refs/heads/victim"],
            );
        },
        &mut || Ok(()),
    )
    .unwrap_err();
    assert_eq!(err.code, "outcome-unknown");
    assert_eq!(
        git(&fix.path(), &["symbolic-ref", "refs/heads/main"]),
        "refs/heads/victim"
    );
    assert_eq!(git(&fix.path(), &["rev-parse", "topic"]), fix.old);
}

#[test]
fn an_error_after_the_commit_reports_unknown_and_undo_still_works() {
    let fix = Flat::new();
    stw::restack_apply::prepare(&fix.path(), "topic", "main", &fix.plan()).unwrap();
    let err = stw::restack_apply::apply_hooked(&fix.path(), &fix.plan(), &mut || {}, &mut || {
        Err("interrupted after commit".into())
    })
    .unwrap_err();
    assert!(err.message.contains("outcome unknown"), "{}", err.message);
    assert_ne!(git(&fix.path(), &["rev-parse", "topic"]), fix.old);
    stw::restack_apply::undo(&fix.path(), &fix.plan()).unwrap();
    assert_eq!(git(&fix.path(), &["rev-parse", "topic"]), fix.old);
}

#[test]
fn a_failure_after_the_backup_leaves_the_counter_and_the_backup() {
    let fix = Flat::new();
    stw::restack_apply::prepare(&fix.path(), "topic", "main", &fix.plan()).unwrap();
    let err = stw::restack_apply::apply_hooked(
        &fix.path(),
        &fix.plan(),
        &mut || {
            git(&fix.path(), &["checkout", "-q", "topic"]);
        },
        &mut || Ok(()),
    )
    .unwrap_err();
    assert!(err.message.contains("may remain"), "{}", err.message);
    assert_eq!(git(&fix.path(), &["rev-parse", "topic"]), fix.old);
    assert_eq!(
        git(&fix.path(), &["rev-parse", "refs/stw/backup/1/1"]),
        fix.old
    );
    assert!(
        git(
            &fix.path(),
            &["rev-parse", "--verify", "refs/stw/backup-counter"]
        )
        .len()
            >= 40
    );
}
