//! Port of the hunk-select spike checks. Disposable repositories only.
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::Value;

fn scratch() -> PathBuf {
    let root =
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../scratch/cursor-port-39/repos");
    // The product checkout is projects/sourcesteward/.worktrees/hunk-select, so three parents
    // is the anchorage workspace when the manifest lives there. Fall back to the env temp dir.
    if root.starts_with("/agent") || root.exists() {
        let _ = fs::create_dir_all(&root);
        if !root.to_string_lossy().to_ascii_lowercase().contains("hint") {
            return root;
        }
    }
    std::env::temp_dir().join("cursor-port-39-repos")
}

struct Repo {
    dir: tempfile::TempDir,
}

impl Repo {
    fn new() -> Repo {
        let parent = scratch();
        fs::create_dir_all(&parent).unwrap();
        let dir = tempfile::tempdir_in(&parent).unwrap();
        git(dir.path(), &["init", "-b", "main"]);
        git(dir.path(), &["commit", "--allow-empty", "-m", "base"]);
        Repo { dir }
    }

    fn path(&self) -> &Path {
        self.dir.path()
    }

    fn write(&self, name: &str, bytes: &[u8]) {
        fs::write(self.path().join(name), bytes).unwrap();
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

fn stw(dir: &Path, args: &[&str]) -> (u8, Value, String) {
    let out = Command::new(env!("CARGO_BIN_EXE_stw"))
        .args(args)
        .arg("--repo")
        .arg(dir)
        .arg("--json")
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_AUTHOR_NAME", "Spike Tester")
        .env("GIT_AUTHOR_EMAIL", "spike@example.invalid")
        .env("GIT_COMMITTER_NAME", "Spike Tester")
        .env("GIT_COMMITTER_EMAIL", "spike@example.invalid")
        .env("GIT_TERMINAL_PROMPT", "0")
        .output()
        .unwrap();
    let stdout = String::from_utf8_lossy(&out.stdout).to_string();
    let stderr = String::from_utf8_lossy(&out.stderr).to_string();
    let doc = serde_json::from_str(&stdout).unwrap_or(Value::Null);
    (
        out.status.code().unwrap_or(255) as u8,
        doc,
        format!("{stderr}{stdout}"),
    )
}

fn plan_file(repo: &Repo) -> PathBuf {
    let name = repo.path().file_name().unwrap().to_string_lossy();
    repo.path().with_file_name(format!("{name}.plan.json"))
}

fn file_sha(path: &Path) -> String {
    let out = Command::new("sha256sum").arg(path).output().unwrap();
    assert!(out.status.success());
    String::from_utf8_lossy(&out.stdout)
        .split_whitespace()
        .next()
        .unwrap()
        .to_string()
}

fn seed(repo: &Repo) {
    let body: String = (1..10).map(|i| format!("L{i:02}\n")).collect();
    repo.write("f.txt", body.as_bytes());
    git(repo.path(), &["add", "--", "f.txt"]);
    git(repo.path(), &["commit", "-m", "seed"]);
}

#[test]
fn list_modified_hunks() {
    let repo = Repo::new();
    seed(&repo);
    let body: String = (1..10)
        .map(|i| format!("L{i:02}\n"))
        .collect::<String>()
        .replace("L03\n", "X03\n")
        .replace("L07\n", "X07\n");
    repo.write("f.txt", body.as_bytes());
    let (code, doc, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0, "{raw}");
    assert_eq!(doc["command"], "hunk-select list");
    assert_eq!(doc["schema_version"], 1);
    let hits: Vec<_> = doc["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|h| h["path"] == "f.txt")
        .collect();
    assert!(hits.len() >= 2, "{raw}");
}

#[test]
fn list_untracked_deleted_binary_and_newline() {
    let repo = Repo::new();
    seed(&repo);
    repo.write("new.txt", b"hello\n");
    let (code, doc, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0);
    let untracked: Vec<_> = doc["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|h| h["path"] == "new.txt")
        .collect();
    assert_eq!(untracked.len(), 1);
    assert_eq!(untracked[0]["kind"], "untracked");
    assert!(untracked[0]["note"]
        .as_str()
        .unwrap_or("")
        .contains("whole-file"));

    fs::remove_file(repo.path().join("f.txt")).unwrap();
    let (code, doc, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0);
    assert!(doc["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .any(|h| h["kind"] == "deleted" || h["path"] == "f.txt"));

    repo.write("f.txt", b"one\ntwo\nthree");
    git(repo.path(), &["add", "--", "f.txt"]);
    git(repo.path(), &["commit", "-m", "nl"]);
    repo.write("f.txt", b"one\ntwo\nTHREE");
    let (code, doc, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let joined = doc["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .map(|h| h["text"].as_str().unwrap_or(""))
        .collect::<String>();
    assert!(joined.contains("No newline"), "{joined}");
}

#[test]
fn list_binary_refused_and_mode_and_rename() {
    let repo = Repo::new();
    seed(&repo);
    repo.write("bin.dat", b"a\0b\0c");
    git(repo.path(), &["add", "--", "bin.dat"]);
    git(repo.path(), &["commit", "-m", "bin"]);
    repo.write("bin.dat", b"a\0b\0c\0d");
    let (code, doc, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0, "{raw}");
    assert!(doc["refused"]
        .as_array()
        .unwrap()
        .iter()
        .any(|r| r["reason"] == "binary"));

    repo.write("u.bin", b"\0\0\0");
    let (code, doc, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0);
    assert!(doc["refused"]
        .as_array()
        .unwrap()
        .iter()
        .any(|r| r["path"] == "u.bin" && r["reason"] == "binary"));

    fs::remove_file(repo.path().join("u.bin")).unwrap();
    git(repo.path(), &["mv", "f.txt", "g.txt"]);
    let (code, doc, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0, "{raw}");
    assert!(doc["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .any(|h| h["kind"] == "renamed" || h["path"] == "g.txt"));

    git(repo.path(), &["commit", "-m", "renamed"]);
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(repo.path().join("g.txt"), fs::Permissions::from_mode(0o755)).unwrap();
        let (code, doc, raw) = stw(
            repo.path(),
            &[
                "workspace",
                "commit-plan",
                "--list-hunks",
                "--source",
                "worktree",
            ],
        );
        assert_eq!(code, 0, "{raw}");
        assert!(
            doc["hunks"]
                .as_array()
                .unwrap()
                .iter()
                .any(|h| h["kind"] == "mode"),
            "{raw}"
        );
    }
}

#[test]
fn commit_two_of_three_and_skip_middle() {
    let repo = Repo::new();
    seed(&repo);
    let body: String = (1..10).map(|i| format!("L{i:02}\n")).collect();
    let edited = body
        .replace("L02\n", "A02\n")
        .replace("L05\n", "A05\n")
        .replace("L08\n", "A08\n");
    repo.write("f.txt", edited.as_bytes());
    let (code, listed, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let mut hunks: Vec<Value> = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|h| h["path"] == "f.txt" && h["kind"] != "mode")
        .cloned()
        .collect();
    hunks.sort_by_key(|h| h["old_start"].as_u64().unwrap_or(0));
    assert!(hunks.len() >= 3, "{raw}");
    let plan_path = plan_file(&repo);
    let (code, plan, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            hunks[0]["id"].as_str().unwrap(),
            "--hunk",
            hunks[2]["id"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    fs::write(&plan_path, serde_json::to_string(&plan).unwrap()).unwrap();
    let (code, applied, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "two hunks",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let expected = body.replace("L02\n", "A02\n").replace("L08\n", "A08\n");
    let blob = Command::new("git")
        .args(["hash-object", "--stdin"])
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .spawn()
        .unwrap();
    use std::io::Write;
    let mut child = blob;
    child
        .stdin
        .as_mut()
        .unwrap()
        .write_all(expected.as_bytes())
        .unwrap();
    let out = child.wait_with_output().unwrap();
    assert!(out.status.success());
    let want = String::from_utf8_lossy(&out.stdout).trim().to_string();
    assert_eq!(git(repo.path(), &["rev-parse", "HEAD:f.txt"]), want);
    assert_eq!(
        applied["commit"].as_str().unwrap(),
        git(repo.path(), &["rev-parse", "HEAD"])
    );
    assert_eq!(
        git(repo.path(), &["rev-parse", ":f.txt"]),
        git(repo.path(), &["rev-parse", "HEAD:f.txt"])
    );
}

#[test]
fn pure_insertions_skip_unselected() {
    let repo = Repo::new();
    let base: String = (1..31).map(|i| format!("l{i:02}\n")).collect();
    repo.write("f.txt", base.as_bytes());
    git(repo.path(), &["add", "--", "f.txt"]);
    git(repo.path(), &["commit", "-m", "thirty"]);
    let mut out = String::new();
    for i in 1..31 {
        out.push_str(&format!("l{i:02}\n"));
        if i == 2 {
            out.push_str("X1\n");
        }
        if i == 12 {
            out.push_str("Y1\nY2\n");
        }
        if i == 22 {
            out.push_str("Z1\n");
        }
    }
    repo.write("f.txt", out.as_bytes());
    let (code, listed, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let hunks: Vec<_> = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|h| h["path"] == "f.txt" && h["kind"] != "mode")
        .collect();
    let x = hunks.iter().find(|h| h["old_start"] == 2).unwrap();
    let z = hunks.iter().find(|h| h["old_start"] == 22).unwrap();
    assert_eq!(z["new_start"], 26);
    let plan_path = plan_file(&repo);
    let (code, plan, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            x["id"].as_str().unwrap(),
            "--hunk",
            z["id"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    fs::write(&plan_path, serde_json::to_string(&plan).unwrap()).unwrap();
    let (code, _, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "xz",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let shown = Command::new("git")
        .args(["-C", repo.path().to_str().unwrap(), "show", "HEAD:f.txt"])
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .output()
        .unwrap();
    let text = String::from_utf8_lossy(&shown.stdout);
    let lines: Vec<_> = text.lines().collect();
    assert_eq!(lines[2], "X1");
    let i = lines.iter().position(|l| *l == "l22").unwrap();
    assert_eq!(lines[i + 1], "Z1");
    assert!(!text.contains("Y1"));
}

#[test]
fn staged_new_file_is_added_and_unrelated_stays() {
    let repo = Repo::new();
    seed(&repo);
    repo.write("brand.txt", b"new\n");
    git(repo.path(), &["add", "--", "brand.txt"]);
    let (code, listed, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let hit = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|h| h["path"] == "brand.txt")
        .unwrap();
    assert_eq!(hit["kind"], "added");
    let plan_path = plan_file(&repo);
    let (code, plan, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            hit["id"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    fs::write(&plan_path, serde_json::to_string(&plan).unwrap()).unwrap();
    let (code, _, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "add brand",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    assert_eq!(git(repo.path(), &["show", "HEAD:brand.txt"]), "new");

    repo.write("keep.txt", b"keep\n");
    git(repo.path(), &["add", "--", "keep.txt"]);
    git(repo.path(), &["commit", "-m", "keep", "--", "keep.txt"]);
    repo.write("staged.txt", b"staged\n");
    git(repo.path(), &["add", "--", "staged.txt"]);
    repo.write("keep.txt", b"keep\nedited\n");
    let body: String = (1..10).map(|i| format!("L{i:02}\n")).collect();
    repo.write(
        "f.txt",
        body.replace("L02\n", "X02\n")
            .replace("L08\n", "X08\n")
            .as_bytes(),
    );
    let (code, listed, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let target = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|h| h["path"] == "f.txt" && h["kind"] != "mode")
        .unwrap();
    let before_keep = fs::read(repo.path().join("keep.txt")).unwrap();
    let before_f = fs::read(repo.path().join("f.txt")).unwrap();
    let staged_blob = git(repo.path(), &["rev-parse", ":staged.txt"]);
    let plan_path = plan_file(&repo);
    let (code, plan, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            target["id"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    fs::write(&plan_path, serde_json::to_string(&plan).unwrap()).unwrap();
    repo.write("parallel.txt", b"p\n");
    git(repo.path(), &["add", "--", "parallel.txt"]);
    let (code, _, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "one",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    assert_eq!(fs::read(repo.path().join("keep.txt")).unwrap(), before_keep);
    assert_eq!(fs::read(repo.path().join("f.txt")).unwrap(), before_f);
    assert_eq!(git(repo.path(), &["rev-parse", ":staged.txt"]), staged_blob);
    assert_eq!(
        git(repo.path(), &["rev-parse", ":parallel.txt"]),
        git(repo.path(), &["hash-object", "parallel.txt"])
    );
    assert_eq!(
        git(repo.path(), &["rev-parse", ":f.txt"]),
        git(repo.path(), &["rev-parse", "HEAD:f.txt"])
    );
    let committed = git(repo.path(), &["show", "--name-only", "--format=", "HEAD"]);
    assert!(!committed.contains("staged.txt"));
    assert!(!committed.contains("keep.txt"));
}

#[test]
fn stale_and_unknown_and_usage() {
    let repo = Repo::new();
    seed(&repo);
    let body: String = (1..10)
        .map(|i| format!("L{i:02}\n"))
        .collect::<String>()
        .replace("L03\n", "Z03\n");
    repo.write("f.txt", body.as_bytes());
    let (_, listed, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    let id = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|h| h["path"] == "f.txt")
        .unwrap()["id"]
        .as_str()
        .unwrap()
        .to_string();
    let (code, plan, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            &id,
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let plan_path = plan_file(&repo);
    fs::write(&plan_path, serde_json::to_string(&plan).unwrap()).unwrap();
    git(repo.path(), &["commit", "--allow-empty", "-m", "move"]);
    let (code, doc, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "x",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 1, "{raw}");
    assert_eq!(doc["error"]["code"], "stale-branch");

    let (code, doc, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            "nope:1:1:1:1:deadbeef",
        ],
    );
    assert_eq!(code, 1);
    assert_eq!(doc["error"]["code"], "unknown-hunk");
    assert_eq!(doc["outcome"], "refused");

    let (code, doc, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "   ",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 2);
    assert_eq!(doc["error"]["code"], "empty-message");

    let (code, doc, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            &id,
            "f.txt",
        ],
    );
    assert_eq!(code, 1);
    assert_eq!(doc["error"]["code"], "hunk-and-paths");
}

#[test]
fn stale_worktree_and_index_and_unrelated_and_lock() {
    let repo = Repo::new();
    seed(&repo);
    let edited: String = (1..10)
        .map(|i| format!("L{i:02}\n"))
        .collect::<String>()
        .replace("L03\n", "Z03\n");
    repo.write("f.txt", edited.as_bytes());
    let (_, listed, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    let id = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|h| h["path"] == "f.txt")
        .unwrap()["id"]
        .clone();
    let (code, plan, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            id.as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0);
    let plan_path = plan_file(&repo);
    fs::write(&plan_path, serde_json::to_string(&plan).unwrap()).unwrap();
    let changed: String = (1..10)
        .map(|i| format!("L{i:02}\n"))
        .collect::<String>()
        .replace("L03\n", "CHANGED\n");
    repo.write("f.txt", changed.as_bytes());
    let head_before = git(repo.path(), &["rev-parse", "HEAD"]);
    let index_before = file_sha(&repo.path().join(".git/index"));
    let work_before = file_sha(&repo.path().join("f.txt"));
    let (code, doc, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "x",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 1, "{raw}");
    assert_eq!(doc["error"]["code"], "stale-worktree");
    assert_eq!(git(repo.path(), &["rev-parse", "HEAD"]), head_before);
    assert_eq!(file_sha(&repo.path().join(".git/index")), index_before);
    assert_eq!(file_sha(&repo.path().join("f.txt")), work_before);

    repo.write("f.txt", edited.as_bytes());
    let (_, listed, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    let id = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|h| h["path"] == "f.txt")
        .unwrap()["id"]
        .clone();
    let (_, plan, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            id.as_str().unwrap(),
        ],
    );
    fs::write(&plan_path, serde_json::to_string(&plan).unwrap()).unwrap();
    let saved = fs::read(repo.path().join("f.txt")).unwrap();
    repo.write("f.txt", b"temp staged content\n");
    git(repo.path(), &["add", "--", "f.txt"]);
    fs::write(repo.path().join("f.txt"), &saved).unwrap();
    let (code, doc, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "x",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 1, "{raw}");
    assert!(
        matches!(
            doc["error"]["code"].as_str(),
            Some("stale-index" | "unrelated-staged" | "stale-worktree")
        ),
        "{raw}"
    );
    assert_eq!(git(repo.path(), &["rev-parse", "HEAD"]), head_before);

    let mid: String = (1..10)
        .map(|i| format!("L{i:02}\n"))
        .collect::<String>()
        .replace("L03\n", "Z03\n");
    let final_body = mid.replace("L07\n", "Z07\n");
    repo.write("f.txt", mid.as_bytes());
    git(repo.path(), &["add", "--", "f.txt"]);
    repo.write("f.txt", final_body.as_bytes());
    let (_, listed, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    let id = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|h| h["path"] == "f.txt")
        .unwrap()["id"]
        .as_str()
        .unwrap();
    let (code, doc, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            id,
        ],
    );
    assert_eq!(code, 1, "{raw}");
    assert_eq!(doc["error"]["code"], "unrelated-staged");

    repo.write("f.txt", edited.as_bytes());
    let (_, listed, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    let h = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|h| h["path"] == "f.txt")
        .expect(&raw);
    let (code, plan, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            h["id"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    fs::write(&plan_path, serde_json::to_string(&plan).unwrap()).unwrap();
    let head = git(repo.path(), &["rev-parse", "HEAD"]);
    let index_hash = file_sha(&repo.path().join(".git/index"));
    let work_hash = file_sha(&repo.path().join("f.txt"));
    let lock = repo.path().join(".git/index.lock");
    fs::write(&lock, b"held").unwrap();
    let (code, doc, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "x",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 1, "{raw}");
    assert_eq!(doc["error"]["code"], "index-locked", "{raw}");
    assert_eq!(git(repo.path(), &["rev-parse", "HEAD"]), head);
    assert_eq!(file_sha(&repo.path().join(".git/index")), index_hash);
    assert_eq!(file_sha(&repo.path().join("f.txt")), work_hash);
    fs::remove_file(&lock).unwrap();
}

#[test]
fn crlf_and_untracked_apply_and_plan_sum_mismatch() {
    let repo = Repo::new();
    repo.write("f.txt", b"a\r\nb\r\nc\r\n");
    git(repo.path(), &["add", "--", "f.txt"]);
    git(repo.path(), &["commit", "-m", "crlf"]);
    repo.write("f.txt", b"a\r\nB\r\nc\r\n");
    let (code, doc, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let joined = doc["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .map(|h| h["text"].as_str().unwrap_or(""))
        .collect::<String>();
    assert!(joined.contains('B'), "{joined}");

    repo.write("new.txt", b"brand\n");
    let (_, listed, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    let id = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|h| h["path"] == "new.txt")
        .unwrap()["id"]
        .as_str()
        .unwrap()
        .to_string();
    let (_, plan, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            &id,
        ],
    );
    let plan_path = plan_file(&repo);
    fs::write(&plan_path, serde_json::to_string(&plan).unwrap()).unwrap();
    let (code, _, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "add new",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    assert_eq!(
        git(repo.path(), &["cat-file", "-p", "HEAD:new.txt"]),
        "brand"
    );

    let (code, doc, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "again",
            "--plan-sum",
            &"0".repeat(64),
        ],
    );
    assert_eq!(code, 1);
    assert_eq!(doc["error"]["code"], "plan-sum-mismatch");
}

#[test]
fn executable_mode_survives_the_index_update() {
    let repo = Repo::new();
    let body: String = (1..10).map(|i| format!("L{i:02}\n")).collect();
    repo.write("run.sh", body.as_bytes());
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(
            repo.path().join("run.sh"),
            fs::Permissions::from_mode(0o755),
        )
        .unwrap();
    }
    git(repo.path(), &["add", "--", "run.sh"]);
    git(repo.path(), &["commit", "-m", "add executable"]);
    assert!(git(repo.path(), &["ls-files", "-s", "--", "run.sh"]).starts_with("100755"));
    repo.write("run.sh", body.replace("L03\n", "Z03\n").as_bytes());
    let (code, listed, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let hunk = listed["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|h| h["path"] == "run.sh" && h["kind"] != "mode")
        .unwrap();
    let (code, plan, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            hunk["id"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let plan_path = plan_file(&repo);
    fs::write(&plan_path, serde_json::to_string(&plan).unwrap()).unwrap();
    let (code, _, raw) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-apply",
            plan_path.to_str().unwrap(),
            "--message",
            "edit exec",
            "--plan-sum",
            plan["plan_sum"].as_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{raw}");
    let staged = git(repo.path(), &["ls-files", "-s", "--", "run.sh"]);
    assert!(staged.starts_with("100755 "), "{staged}");
    assert!(!staged.starts_with("100644"));
    assert_eq!(git(repo.path(), &["diff", "--cached", "--", "run.sh"]), "");
}

#[test]
fn renumber_ignores_unselected_insertions() {
    let selected = vec![
        serde_json::json!({
            "id": "x",
            "path": "f.txt",
            "old_start": 2,
            "old_count": 0,
            "new_start": 3,
            "new_count": 1,
            "text": "@@ -2,0 +3 @@\n+X1\n",
            "kind": "modified",
        }),
        serde_json::json!({
            "id": "z",
            "path": "f.txt",
            "old_start": 22,
            "old_count": 0,
            "new_start": 26,
            "new_count": 1,
            "text": "@@ -22,0 +26 @@\n+Z1\n",
            "kind": "modified",
        }),
    ];
    let adj = stw::hunk_select::renumber_selected_for_apply(&selected);
    assert_eq!(adj[0]["new_start"], 3);
    assert_eq!(adj[1]["new_start"], 24);
    assert!(adj[1]["text"]
        .as_str()
        .unwrap()
        .lines()
        .next()
        .unwrap()
        .contains("+24,1"));
}

#[test]
fn plan_sum_binds_head_and_hunk_body() {
    let repo = Repo::new();
    seed(&repo);
    let body: String = (1..10)
        .map(|i| format!("L{i:02}\n"))
        .collect::<String>()
        .replace("L03\n", "Z\n");
    repo.write("f.txt", body.as_bytes());
    let (_, listed, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    let first = listed["hunks"][0]["id"].as_str().unwrap().to_string();
    repo.write("f.txt", body.replace('Z', "Q").as_bytes());
    let (_, again, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--list-hunks",
            "--source",
            "worktree",
        ],
    );
    let second = again["hunks"][0]["id"].as_str().unwrap();
    assert_ne!(first, second);
    repo.write("f.txt", body.as_bytes());
    let (_, plan, _) = stw(
        repo.path(),
        &[
            "workspace",
            "commit-plan",
            "--source",
            "worktree",
            "--hunk",
            &first,
        ],
    );
    let good = plan["plan_sum"].as_str().unwrap().to_string();
    let mut broken = plan.clone();
    broken["head"] = serde_json::json!("0".repeat(40));
    broken.as_object_mut().unwrap().remove("plan_sum");
    assert_ne!(good, stw::hunk_select::plan_sum_of(&broken));
}

#[test]
fn path_plan_still_rejects_a_missing_path_argument() {
    let repo = Repo::new();
    seed(&repo);
    let out = Command::new(env!("CARGO_BIN_EXE_stw"))
        .args(["workspace", "commit-plan", "--source", "worktree", "--repo"])
        .arg(repo.path())
        .output()
        .unwrap();
    assert_eq!(
        out.status.code(),
        Some(2),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
}
