//! Nested submodules (gitlinks in gitlinks), as Go walks them (`stwsync` `walk`, `workspaceRoot`): the repositories of a
//! workspace are listed depth-first, a nested one named `outer/inner`; the root of a checkout is the top-level
//! superproject (up to 16 levels); the walk stops with an error above 256 repositories.
//!
//! Harness confinement: everything lives in a tempdir of `World` (git and stw run with a scrubbed environment).
use crate::common::FixtureCommand as _;
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

impl N {
    fn oid_of_c(&self) -> String {
        self.w.git(&self.c, &["rev-parse", "HEAD"])
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
        .fixture_env_clear()
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

/// `n` gets its own submodule `m`, which `n` records at m2 while it sits at m1 (3 levels: ws, c, c/n, c/n/m).
fn add_third_level(t: &N) -> (PathBuf, String) {
    let mup = t.w.p("m-up");
    std::fs::create_dir(&mup).unwrap();
    t.w.git(&mup, &["init", "-q"]);
    let m1 = t.w.commit(&mup, "m.txt", "1\n");
    t.w.git(&t.w.base, &["clone", "-q", "--bare", "m-up", "m.git"]);
    let m2 = t.w.commit(&mup, "m.txt", "2\n");
    t.w.git(
        &mup,
        &["push", "-q", t.w.p("m.git").to_str().unwrap(), "main"],
    );
    t.w.git(
        &t.n,
        &[
            "submodule",
            "add",
            "-q",
            t.w.p("m.git").to_str().unwrap(),
            "m",
        ],
    );
    t.w.git(&t.n, &["commit", "-q", "-m", "add m"]);
    let m = t.n.join("m");
    t.w.git(&m, &["fetch", "-q", "origin"]);
    t.w.git(&m, &["checkout", "-q", "--detach", &m1]);
    t.w.git(
        &t.n,
        &["update-index", "--cacheinfo", &format!("160000,{m2},m")],
    );
    (m, m2)
}

fn plan_submodules(t: &N, extra: &[&str]) -> (i32, Value, String) {
    let mut args = vec!["sync", "--plan", "--submodules", "--offline"];
    args.extend_from_slice(extra);
    args.extend_from_slice(&["--root", t.ws.to_str().unwrap()]);
    stw(t, &t.w.base, &args)
}

fn rec<'a>(v: &'a Value, name: &str) -> &'a Value {
    v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .find(|r| r["name"] == name)
        .unwrap_or_else(|| panic!("no {name} in {v}"))
}

#[test]
fn a_third_level_pinned_submodule_is_stepped_against_its_own_holder() {
    let t = setup();
    let (m, m2) = add_third_level(&t);
    let (code, v, err) = plan_submodules(&t, &[]);
    assert_eq!(code, 0, "{v} {err}");
    let r = rec(&v, "c/n/m");
    assert_eq!(r["submodule"]["state"], "would-move", "{r}");
    assert_eq!(r["submodule"]["new"], m2.as_str(), "{r}");
    assert_eq!(names(&t.ws), ["ws", "c", "c/n", "c/n/m"]);
    assert!(m.join(".git").exists());
}

#[test]
fn repo_names_a_nested_submodule_whose_holder_is_left_out_of_the_list() {
    let t = setup();
    pin_nested_ahead(&t);
    let (code, v, err) = plan_submodules(&t, &["--repo", "c/n"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(rec(&v, "c/n")["submodule"]["state"], "would-move", "{v}");
}

/// The agent-rules checkout (`<root>/.agents`) is left to the rules logic, but its own submodules are listed and
/// stepped like those of any repository (Go: `walk` descends after the rules record).
fn add_rules_checkout(t: &N) {
    t.w.git(
        &t.ws,
        &[
            "submodule",
            "add",
            "-q",
            t.w.p("c.git").to_str().unwrap(),
            ".agents",
        ],
    );
    let rules = t.ws.join(".agents");
    t.w.git(
        &rules,
        &[
            "submodule",
            "add",
            "-q",
            t.w.p("n.git").to_str().unwrap(),
            "x",
        ],
    );
    t.w.git(&rules, &["commit", "-q", "-m", "add x"]);
    t.w.git(&t.ws, &["add", ".agents"]);
    t.w.git(&t.ws, &["commit", "-q", "-m", "add rules"]);
}

#[test]
fn a_pinned_submodule_of_the_rules_checkout_is_stepped_against_the_rules_checkout() {
    let t = setup();
    add_rules_checkout(&t);
    let rules = t.ws.join(".agents");
    let x = rules.join("x");
    t.w.git(&x, &["fetch", "-q", "origin"]);
    t.w.git(&x, &["checkout", "-q", "--detach", &t.n1]);
    t.w.git(
        &rules,
        &["update-index", "--cacheinfo", &format!("160000,{},x", t.n2)],
    );
    let (code, v, err) = plan_submodules(&t, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(
        rec(&v, ".agents/x")["submodule"]["state"],
        "would-move",
        "{v}"
    );
}

#[test]
fn an_uninitialized_gitlink_of_the_rules_checkout_is_reported() {
    let t = setup();
    add_rules_checkout(&t);
    let rules = t.ws.join(".agents");
    t.w.git(
        &rules,
        &[
            "update-index",
            "--add",
            "--cacheinfo",
            &format!("160000,{},u", t.n1),
        ],
    );
    let (code, v, err) = plan_submodules(&t, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert!(
        rec(&v, ".agents/u")["reason"]
            .as_str()
            .unwrap()
            .contains("not initialized"),
        "{v}"
    );
}

fn stw_ceiling(t: &N, cwd: &Path, ceiling: &Path, args: &[&str]) -> (i32, Value, String) {
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .fixture_env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &t.w.home)
        .env("GIT_CEILING_DIRECTORIES", ceiling)
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

#[test]
fn a_foreign_outer_repository_behind_a_ceiling_is_no_superproject() {
    let t = setup();
    // `outer` is a repository the user does not work in; its index names the workspace path as a gitlink
    let outer = t.w.p("outer");
    std::fs::create_dir(&outer).unwrap();
    t.w.git(&outer, &["init", "-q"]);
    t.w.commit(&outer, "f", "f\n");
    let home = outer.join("home");
    std::fs::create_dir(&home).unwrap();
    let w = home.join("w");
    t.w.git(&t.w.base, &["clone", "-q", "c.git", "outer/home/w"]);
    t.w.git(
        &w,
        &[
            "submodule",
            "add",
            "-q",
            t.w.p("n.git").to_str().unwrap(),
            "s",
        ],
    );
    t.w.git(&w, &["commit", "-q", "-m", "add s"]);
    let head = t.w.git(&w, &["rev-parse", "HEAD"]);
    t.w.git(
        &outer,
        &[
            "update-index",
            "--add",
            "--cacheinfo",
            &format!("160000,{head},home/w"),
        ],
    );
    let before = t.w.git(&outer, &["rev-parse", "HEAD"]);
    let (code, v, err) = stw_ceiling(&t, &w.join("s"), &outer, &["status", "--offline"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(
        v["root"]
            .as_str()
            .map(|r| std::fs::canonicalize(r).unwrap()),
        Some(std::fs::canonicalize(&w).unwrap()),
        "a ceiling hides the outer repository: {v}"
    );
    assert_eq!(t.w.git(&outer, &["rev-parse", "HEAD"]), before);
}

#[test]
fn an_unrelated_repository_between_a_checkout_and_an_outer_one_ends_the_search() {
    let t = setup();
    let outer = t.w.p("outer2");
    std::fs::create_dir(&outer).unwrap();
    t.w.git(&outer, &["init", "-q"]);
    t.w.commit(&outer, "f", "f\n");
    std::fs::create_dir_all(outer.join("a")).unwrap();
    t.w.git(&t.w.base, &["clone", "-q", "c.git", "outer2/a/b"]);
    t.w.git(&t.w.base, &["clone", "-q", "c.git", "outer2/a/b/w2"]);
    let w2 = outer.join("a/b/w2");
    // the repository in between holds a gitlink too, but not for the checkout
    t.w.git(
        &outer.join("a/b"),
        &[
            "update-index",
            "--add",
            "--cacheinfo",
            &format!("160000,{},other", t.oid_of_c()),
        ],
    );
    t.w.git(
        &w2,
        &[
            "submodule",
            "add",
            "-q",
            t.w.p("n.git").to_str().unwrap(),
            "s",
        ],
    );
    t.w.git(&w2, &["commit", "-q", "-m", "add s"]);
    let head = t.w.git(&w2, &["rev-parse", "HEAD"]);
    t.w.git(
        &outer,
        &[
            "update-index",
            "--add",
            "--cacheinfo",
            &format!("160000,{head},a/b/w2"),
        ],
    );
    let (code, v, err) = stw(&t, &w2.join("s"), &["status", "--offline"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(
        v["root"]
            .as_str()
            .map(|r| std::fs::canonicalize(r).unwrap()),
        Some(std::fs::canonicalize(&w2).unwrap()),
        "git names no superproject here: {v}"
    );
}
