//! Nested submodules (gitlinks in gitlinks), as Go walks them (`stwsync` `walk`, `workspaceRoot`): the repositories of a
//! workspace are listed depth-first, a nested one named `outer/inner`; the root of a checkout is the top-level
//! superproject (up to 16 levels); the walk stops with an error above 256 repositories.
//!
//! Harness confinement: everything lives in a tempdir of `World` (git and stw run with a scrubbed environment).
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::Value;

use crate::sync_flags::World;

struct N {
    w: World,
    ws: PathBuf,
    c: PathBuf,
    n: PathBuf,
    n1: String,
    n2: String,
}

/// `ws` has the submodule `c`; `c` has the submodule `n`; `n2` is a later commit on origin's main of `n`.
fn setup() -> N {
    let w = World::new();
    let up = w.p("n-up");
    std::fs::create_dir(&up).unwrap();
    w.git(&up, &["init", "-q"]);
    let n1 = w.commit(&up, "n.txt", "1\n");
    w.git(&w.base, &["clone", "-q", "--bare", "n-up", "n.git"]);
    let n2 = w.commit(&up, "n.txt", "2\n");
    w.git(&up, &["push", "-q", w.p("n.git").to_str().unwrap(), "main"]);
    let cup = w.p("c-up");
    std::fs::create_dir(&cup).unwrap();
    w.git(&cup, &["init", "-q"]);
    w.commit(&cup, "c.txt", "c\n");
    w.git(&w.base, &["clone", "-q", "--bare", "c-up", "c.git"]);
    let ws = w.p("ws");
    std::fs::create_dir(&ws).unwrap();
    w.git(&ws, &["init", "-q"]);
    w.commit(&ws, "ws.txt", "w\n");
    w.git(
        &ws,
        &[
            "submodule",
            "add",
            "-q",
            w.p("c.git").to_str().unwrap(),
            "c",
        ],
    );
    let c = ws.join("c");
    w.git(
        &c,
        &[
            "submodule",
            "add",
            "-q",
            w.p("n.git").to_str().unwrap(),
            "n",
        ],
    );
    w.git(&c, &["commit", "-q", "-m", "add n"]);
    w.git(&ws, &["add", "c"]);
    w.git(&ws, &["commit", "-q", "-m", "add c"]);
    let n = c.join("n");
    N {
        w,
        ws,
        c,
        n,
        n1,
        n2,
    }
}

fn names(root: &Path) -> Vec<String> {
    stw::repo::list_repos(root)
        .into_iter()
        .map(|r| r.name)
        .collect()
}

fn stw(t: &N, cwd: &Path, args: &[&str]) -> (i32, Value, String) {
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &t.w.home)
        .env("GIT_CEILING_DIRECTORIES", &t.w.base)
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .current_dir(cwd)
        .args(args)
        .arg("--json")
        .output()
        .unwrap();
    (
        o.status.code().unwrap_or(-1),
        serde_json::from_slice(&o.stdout).unwrap_or(Value::Null),
        String::from_utf8_lossy(&o.stderr).into_owned(),
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
fn nested_gitlinks_are_listed_depth_first_with_the_path_below_the_root() {
    let t = setup();
    assert_eq!(names(&t.ws), ["ws", "c", "c/n"]);
    let (code, v, err) = stw(
        &t,
        &t.w.base,
        &["status", "--offline", "--root", t.ws.to_str().unwrap()],
    );
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo_names(&v), ["ws", "c", "c/n"], "{v}");
    let (code, v, err) = stw(
        &t,
        &t.w.base,
        &[
            "sync",
            "--plan",
            "--offline",
            "--root",
            t.ws.to_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo_names(&v), ["ws", "c", "c/n"], "{v}");
}

#[test]
fn a_checkout_inside_a_nested_submodule_has_the_top_level_superproject_as_root() {
    let t = setup();
    let (code, v, err) = stw(&t, &t.n, &["status", "--offline"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo_names(&v), ["ws", "c", "c/n"], "{v}");
    let (code, v, err) = stw(&t, &t.c, &["status", "--offline"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo_names(&v), ["ws", "c", "c/n"], "{v}");
}

#[test]
fn the_root_climbs_at_most_sixteen_levels() {
    let t = setup();
    // 17 nested checkouts: only the 16 nearest superprojects are climbed, as Go's `workspaceRoot`
    let mut dir = t.ws.clone();
    let mut chain = vec![t.ws.clone()];
    for i in 0..17 {
        let sub = format!("d{i}");
        let up = t.w.p(&format!("u{i}"));
        std::fs::create_dir(&up).unwrap();
        t.w.git(&up, &["init", "-q"]);
        t.w.commit(&up, "f", "f\n");
        t.w.git(
            &dir,
            &["submodule", "add", "-q", up.to_str().unwrap(), &sub],
        );
        dir = dir.join(&sub);
        chain.push(dir.clone());
    }
    let deepest = chain.last().unwrap();
    let root = stw::repo::default_root(deepest).unwrap();
    assert_eq!(
        root, chain[1],
        "{root:?} is not the 16th level from the bottom"
    );
}

#[test]
fn the_walk_stops_above_the_repository_limit() {
    let t = setup();
    let (all, exceeded) = stw::repo::list_repos_limited(&t.ws, 3);
    assert_eq!(all.len(), 3);
    assert!(!exceeded, "3 repositories are not above the limit of 3");
    let (some, exceeded) = stw::repo::list_repos_limited(&t.ws, 2);
    assert!(exceeded);
    assert!(some.len() <= 3);
    assert_eq!(stw::repo::REPO_LIMIT, 256);
    let err = stw::repo::list_repos_checked_with(&t.ws, 2).unwrap_err();
    assert!(
        err.to_string()
            .contains("the workspace has more than 2 repositories"),
        "{err}"
    );
    assert_eq!(
        stw::repo::list_repos_checked_with(&t.ws, 3).unwrap().len(),
        3
    );
}

#[test]
fn a_workspace_of_more_than_256_repositories_is_refused() {
    let t = setup();
    // 254 more top-level clones: the root, c, c/n and 254 of them are 257 repositories
    for i in 0..254 {
        let rel = format!("r{i:03}");
        t.w.git(&t.w.base, &["clone", "-q", "c.git", &format!("ws/{rel}")]);
        t.w.git(&t.ws, &["add", &rel]);
    }
    let (code, v, err) = stw(
        &t,
        &t.w.base,
        &["status", "--offline", "--root", t.ws.to_str().unwrap()],
    );
    assert_eq!(code, 2, "{v} {err}");
    assert!(
        err.contains("the workspace has more than 256 repositories"),
        "{err}"
    );
    // `restack plan` and `prove` pick their repository from the same list
    let (code, v, err) = stw(
        &t,
        &t.w.base,
        &[
            "restack",
            "plan",
            "main",
            "--onto",
            "main",
            "--root",
            t.ws.to_str().unwrap(),
        ],
    );
    assert_eq!(code, 2, "{v} {err}");
    assert!(
        err.contains("the workspace has more than 256 repositories"),
        "{err}"
    );
}

#[test]
fn a_nested_repository_can_be_named_with_repo() {
    let t = setup();
    let (code, v, err) = stw(
        &t,
        &t.w.base,
        &[
            "restack",
            "plan",
            "main",
            "--onto",
            "main",
            "--repo",
            "c/n",
            "--root",
            t.ws.to_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{v} {err}");
    // `main` resolves in the nested repository, not in the workspace root
    assert_eq!(
        v["onto"]["commit"].as_str(),
        Some(t.w.git(&t.n, &["rev-parse", "main"]).as_str()),
        "{v}"
    );
}

#[test]
fn a_refused_path_of_a_nested_repository_is_reported_with_its_full_path() {
    let t = setup();
    t.w.git(
        &t.c,
        &[
            "update-index",
            "--add",
            "--cacheinfo",
            &format!("160000,{},bad name", t.n1),
        ],
    );
    let rej = stw::repo::rejected_paths(&t.ws);
    assert_eq!(rej.len(), 1, "{rej:?}");
    assert_eq!(rej[0].0, "c/bad name");
    let (code, v, err) = stw(
        &t,
        &t.w.base,
        &["status", "--offline", "--root", t.ws.to_str().unwrap()],
    );
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(v["rejected_paths"][0]["path"], "c/bad name", "{v}");
}

/// `c` records `n` at n2 while `n` still sits at n1 (detached; n2 is local).
fn pin_nested_ahead(t: &N) {
    t.w.git(&t.n, &["fetch", "-q", "origin"]);
    t.w.git(&t.n, &["checkout", "-q", "--detach", &t.n1]);
    t.w.git(
        &t.c,
        &["update-index", "--cacheinfo", &format!("160000,{},n", t.n2)],
    );
}

#[test]
fn the_dry_run_names_the_move_of_a_nested_pinned_submodule() {
    let t = setup();
    pin_nested_ahead(&t);
    let (code, v, err) = stw(
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
    assert_eq!(code, 0, "{v} {err}");
    let r = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .find(|r| r["name"] == "c/n")
        .unwrap_or_else(|| panic!("no c/n in {v}"));
    assert_eq!(r["submodule"]["state"], "would-move", "{r}");
    assert_eq!(r["submodule"]["new"], t.n2.as_str(), "{r}");
}

#[test]
fn a_run_moves_a_nested_pinned_submodule_to_the_commit_its_parent_records() {
    let t = setup();
    pin_nested_ahead(&t);
    let (code, v, err) = stw(
        &t,
        &t.w.base,
        &[
            "sync",
            "--submodules",
            "--offline",
            "--root",
            t.ws.to_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(t.w.git(&t.n, &["rev-parse", "HEAD"]), t.n2);
}

#[test]
fn an_uninitialized_nested_submodule_is_reported_as_unreachable_and_never_cloned() {
    let t = setup();
    t.w.git(
        &t.c,
        &[
            "update-index",
            "--add",
            "--cacheinfo",
            &format!("160000,{},m", t.n1),
        ],
    );
    let (code, v, err) = stw(
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
    assert_eq!(code, 0, "{v} {err}");
    let r = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .find(|r| r["name"] == "c/m")
        .unwrap_or_else(|| panic!("no c/m in {v}"));
    assert!(
        r["reason"].as_str().unwrap().contains("not initialized"),
        "{r}"
    );
    assert!(!t.c.join("m").exists());
}
