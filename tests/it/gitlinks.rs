//! The repositories of a workspace are the gitlinks of its index (Go: `stwsync` `submodules`, `ls-files --stage`
//! mode 160000), not the paths of `.gitmodules`. A gitlink needs no `.gitmodules` entry; a `.gitmodules` entry
//! without a gitlink is no repository, and a clone that no gitlink names is never one.
//!
//! Harness confinement: everything lives in a tempdir of `World` (git and stw run with a scrubbed environment).
use crate::common::FixtureCommand as _;
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::Value;

use crate::sync_flags::World;

struct Ws {
    w: World,
    ws: PathBuf,
    oid: String,
}

/// `ws`, a repository with one commit and no `.gitmodules`, next to the bare origin `o.git` the clones come from.
fn setup() -> Ws {
    let w = World::new();
    let up = w.p("up");
    std::fs::create_dir(&up).unwrap();
    w.git(&up, &["init", "-q"]);
    let oid = w.commit(&up, "f", "1\n").trim().to_string();
    w.git(&w.base, &["clone", "-q", "--bare", "up", "o.git"]);
    let ws = w.p("ws");
    std::fs::create_dir(&ws).unwrap();
    w.git(&ws, &["init", "-q"]);
    w.commit(&ws, "r", "r\n");
    Ws { w, ws, oid }
}

impl Ws {
    /// A checked-out clone at `rel` below the workspace (not yet in the index).
    fn clone_at(&self, rel: &str) {
        if let Some(parent) = Path::new(rel).parent() {
            std::fs::create_dir_all(self.ws.join(parent)).unwrap();
        }
        self.w.git(
            &self.w.base,
            &["clone", "-q", "o.git", &format!("ws/{rel}")],
        );
    }

    /// A gitlink at `rel` in the index (any path git accepts), with no checkout and no `.gitmodules`.
    fn gitlink(&self, rel: &str) {
        self.w.git(
            &self.ws,
            &[
                "update-index",
                "--add",
                "--cacheinfo",
                &format!("160000,{},{rel}", self.oid),
            ],
        );
    }

    fn gitmodules(&self, paths: &[&str]) {
        let mut s = String::new();
        for (i, p) in paths.iter().enumerate() {
            s += &format!("[submodule \"m{i}\"]\n\tpath = {p}\n\turl = ../o.git\n");
        }
        std::fs::write(self.ws.join(".gitmodules"), s).unwrap();
    }

    fn names(&self) -> Vec<String> {
        stw::repo::list_repos(&self.ws)
            .into_iter()
            .map(|r| r.name)
            .collect()
    }
}

fn stw_in(t: &Ws, cwd: &Path, args: &[&str]) -> (i32, Value) {
    let out = Command::new(env!("CARGO_BIN_EXE_stw"))
        .fixture_env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &t.w.home)
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .current_dir(cwd)
        .args(args)
        .arg("--json")
        .output()
        .unwrap();
    (
        out.status.code().unwrap_or(-1),
        serde_json::from_slice(&out.stdout).unwrap_or(Value::Null),
    )
}

fn repo_names(v: &Value) -> Vec<String> {
    v["repos"]
        .as_array()
        .unwrap_or_else(|| panic!("no repos in {v}"))
        .iter()
        .map(|r| r["name"].as_str().unwrap().to_string())
        .collect()
}

#[test]
fn a_gitlink_without_a_gitmodules_entry_is_a_repository() {
    let t = setup();
    t.clone_at("p");
    t.w.git(&t.ws, &["add", "p"]);
    assert!(!t.ws.join(".gitmodules").exists());
    assert_eq!(stw::repo::submodule_paths(&t.ws), ["p"]);
    assert_eq!(t.names(), ["ws", "p"]);
    let (code, v) = stw_in(
        &t,
        &t.w.base,
        &["status", "--offline", "--root", t.ws.to_str().unwrap()],
    );
    assert_eq!(code, 0, "{v}");
    assert_eq!(repo_names(&v), ["ws", "p"], "{v}");
}

#[test]
fn a_gitmodules_entry_without_a_gitlink_is_no_repository() {
    let t = setup();
    t.clone_at("q");
    t.gitmodules(&["q", "missing"]);
    assert!(stw::repo::submodule_paths(&t.ws).is_empty());
    assert_eq!(t.names(), ["ws"]);
    assert!(stw::repo::rejected_paths(&t.ws).is_empty());
    let (code, v) = stw_in(
        &t,
        &t.w.base,
        &["status", "--offline", "--root", t.ws.to_str().unwrap()],
    );
    assert_eq!(code, 0, "{v}");
    assert_eq!(repo_names(&v), ["ws"], "{v}");
}

#[test]
fn a_clone_that_no_gitlink_names_is_never_a_repository() {
    let t = setup();
    t.clone_at("loose");
    t.clone_at("p");
    t.w.git(&t.ws, &["add", "p"]);
    assert_eq!(t.names(), ["ws", "p"]);
}

#[test]
fn a_file_of_the_index_is_no_gitlink() {
    let t = setup();
    t.gitmodules(&["r"]);
    assert!(stw::repo::submodule_paths(&t.ws).is_empty());
}

#[test]
fn the_paths_are_sorted_and_a_conflicted_gitlink_is_listed_once() {
    let t = setup();
    t.gitlink("b");
    t.gitlink("a/z");
    t.gitlink("c");
    // c in three conflict stages instead of stage 0
    t.w.git(&t.ws, &["update-index", "--force-remove", "c"]);
    let info = format!(
        "160000 {o} 1\tc\n160000 {o} 2\tc\n160000 {o} 3\tc\n",
        o = t.oid
    );
    let mut child = Command::new("git")
        .current_dir(&t.ws)
        .fixture_env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &t.w.home)
        .args(["update-index", "--index-info"])
        .stdin(std::process::Stdio::piped())
        .spawn()
        .unwrap();
    std::io::Write::write_all(child.stdin.as_mut().unwrap(), info.as_bytes()).unwrap();
    assert!(child.wait().unwrap().success());
    assert_eq!(stw::repo::submodule_paths(&t.ws), ["a/z", "b", "c"]);
}

#[test]
fn a_refused_gitlink_path_is_reported_and_never_listed() {
    let t = setup();
    t.clone_at("sp ace");
    t.gitlink("sp ace");
    t.clone_at("ok");
    t.gitlink("ok");
    assert_eq!(t.names(), ["ws", "ok"]);
    let rej: Vec<String> = stw::repo::rejected_paths(&t.ws)
        .into_iter()
        .map(|(p, _)| p)
        .collect();
    assert_eq!(rej, ["sp ace"]);
}

#[test]
fn a_gitlink_without_a_checkout_is_not_listed() {
    let t = setup();
    t.gitlink("gone");
    assert_eq!(stw::repo::submodule_paths(&t.ws), ["gone"]);
    assert_eq!(t.names(), ["ws"]);
}

#[test]
fn the_workspace_of_a_gitlink_checkout_is_its_superproject_without_a_gitmodules_file() {
    let t = setup();
    t.clone_at("p");
    t.w.git(&t.ws, &["add", "p"]);
    let (code, v) = stw_in(&t, &t.ws.join("p"), &["status", "--offline"]);
    assert_eq!(code, 0, "{v}");
    assert_eq!(repo_names(&v), ["ws", "p"], "{v}");
    assert_eq!(
        v["root"]
            .as_str()
            .map(|r| std::fs::canonicalize(r).unwrap()),
        Some(t.ws.canonicalize().unwrap()),
        "{v}"
    );
}

#[test]
fn a_repository_with_gitlinks_and_no_gitmodules_file_is_a_workspace_root() {
    let t = setup();
    t.clone_at("p");
    t.w.git(&t.ws, &["add", "p"]);
    let (code, v) = stw_in(&t, &t.ws, &["status", "--offline"]);
    assert_eq!(code, 0, "{v}");
    assert_eq!(repo_names(&v), ["ws", "p"], "{v}");
}

#[test]
fn an_index_that_is_out_of_order_gives_the_paths_sorted() {
    let t = setup();
    // not an index git writes: gix does not sort what it reads, the entries stay as pushed (c, a, b)
    crate::common::stage_gitlinks_in(&t.ws, &["c", "a", "b"], false);
    assert_eq!(stw::repo::submodule_paths(&t.ws), ["a", "b", "c"]);
}

#[test]
fn a_gitlink_without_a_checkout_is_unreachable_for_submodules_and_a_gitmodules_entry_is_no_record()
{
    let t = setup();
    t.gitlink("gone");
    t.gitmodules(&["ghost"]);
    let (code, v) = stw_in(
        &t,
        &t.w.base,
        &[
            "sync",
            "--plan",
            "--submodules",
            "--offline",
            "--root",
            t.ws.to_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{v}");
    let gone = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .find(|r| r["name"] == "gone");
    let gone = gone.unwrap_or_else(|| panic!("no record for the gitlink in {v}"));
    assert_eq!(gone["result"], "skip", "{gone}");
    assert!(
        gone["reason"]
            .as_str()
            .unwrap()
            .starts_with("would be unreachable: "),
        "{gone}"
    );
    assert!(repo_names(&v).iter().all(|n| n != "ghost"), "{v}");
}

/// The workspace root of a nested checkout is the top-level superproject, whose list holds the nested ones (Go:
/// `--show-superproject-working-tree` up to 16 levels, `walk`); a directory below a gitlink path that is no gitlink
/// itself is no checkout of that superproject.
#[test]
fn a_nested_checkout_belongs_to_the_top_level_superproject() {
    let t = setup();
    t.clone_at("sub");
    t.w.git(&t.ws, &["add", "sub"]);
    let sub = t.ws.join("sub");
    t.w.git(&t.w.base, &["clone", "-q", "o.git", "ws/sub/n2"]);
    t.w.git(&sub, &["add", "n2"]);
    let (code, v) = stw_in(&t, &sub.join("n2"), &["status", "--offline"]);
    assert_eq!(code, 0, "{v}");
    assert_eq!(
        v["root"]
            .as_str()
            .map(|r| std::fs::canonicalize(r).unwrap()),
        Some(t.ws.canonicalize().unwrap()),
        "{v}"
    );
    assert_eq!(repo_names(&v), ["ws", "sub", "sub/n2"], "{v}");
    // a directory below a gitlink path that is no gitlink itself is no checkout of that superproject
    let other = t.ws.join("sub/plain");
    std::fs::create_dir_all(&other).unwrap();
    t.w.git(&other, &["init", "-q"]);
    let (code, v) = stw_in(&t, &other, &["status", "--offline"]);
    assert_eq!(code, 2, "no workspace: {v}");
}
