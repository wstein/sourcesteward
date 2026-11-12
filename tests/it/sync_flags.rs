//! `stw sync --prune`, `--no-prune` and `--submodules` (ports of the Go flags; behavior in `src/sync/fetch.rs` and
//! `src/sync/submodules.rs`; Go cases: `internal/stwsync/pull_test.go` prune, `submodules_test.go`).
//!
//! Harness confinement: every repository lives in a tempdir created here; git and `stw` run with a scrubbed
//! environment (no inherited `GIT_*`), `HOME` inside the tempdir, `GIT_CEILING_DIRECTORIES` above the work area.
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::Value;

use crate::common::scratch;

pub struct World {
    _t: tempfile::TempDir,
    pub base: PathBuf,
    pub home: PathBuf,
}

impl World {
    pub fn new() -> World {
        let t = scratch();
        let base = t.path().canonicalize().unwrap();
        let home = base.join("home");
        std::fs::create_dir(&home).unwrap();
        World { _t: t, base, home }
    }

    pub fn p(&self, name: &str) -> PathBuf {
        self.base.join(name)
    }

    /// Git in `dir` (inside the tempdir); fails the test when git fails.
    pub fn git(&self, dir: &Path, args: &[&str]) -> String {
        let (ok, out, err) = self.git_try(dir, args);
        assert!(ok, "git {args:?} in {}: {err}", dir.display());
        out
    }

    pub fn git_try(&self, dir: &Path, args: &[&str]) -> (bool, String, String) {
        assert!(
            dir.is_absolute() && dir.starts_with(&self.base),
            "unconfined git: {dir:?}"
        );
        let o = Command::new("git")
            .env_clear()
            .env("PATH", std::env::var_os("PATH").unwrap())
            .env("HOME", &self.home)
            .env("GIT_CONFIG_NOSYSTEM", "1")
            .env("GIT_CEILING_DIRECTORIES", &self.base)
            .current_dir(dir)
            .args([
                "-c",
                "user.name=T",
                "-c",
                "user.email=t@example.com",
                "-c",
                "commit.gpgsign=false",
                "-c",
                "protocol.file.allow=always",
                "-c",
                "init.defaultBranch=main",
            ])
            .args(args)
            .output()
            .unwrap();
        (
            o.status.success(),
            String::from_utf8_lossy(&o.stdout).trim().to_string(),
            String::from_utf8_lossy(&o.stderr).trim().to_string(),
        )
    }

    /// A commit with one file; returns its id.
    pub fn commit(&self, dir: &Path, file: &str, text: &str) -> String {
        std::fs::write(dir.join(file), text).unwrap();
        self.git(dir, &["add", "--", file]);
        self.git(dir, &["commit", "-q", "-m", &format!("{file}: {text}")]);
        self.git(dir, &["rev-parse", "HEAD"])
    }

    /// Run `stw sync` with `args` on the workspace at `root`; the JSON document, the exit code and stderr.
    pub fn stw(&self, root: &Path, args: &[&str]) -> (i32, Value, String) {
        let o = Command::new(env!("CARGO_BIN_EXE_stw"))
            .env_clear()
            .env("PATH", std::env::var_os("PATH").unwrap())
            .env("HOME", &self.home)
            .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
            .current_dir(&self.base)
            .arg("sync")
            .args(args)
            .args(["--json", "--root"])
            .arg(root)
            .output()
            .unwrap();
        let err = String::from_utf8_lossy(&o.stderr).into_owned();
        let v = serde_json::from_slice(&o.stdout).unwrap_or(Value::Null);
        (o.status.code().unwrap_or(-1), v, err)
    }
}

fn repo<'a>(v: &'a Value, name: &str) -> &'a Value {
    v["repos"]
        .as_array()
        .unwrap_or_else(|| panic!("no repos in {v}"))
        .iter()
        .find(|r| r["name"] == name)
        .unwrap_or_else(|| panic!("no repository {name} in {v}"))
}

// ---------------------------------------------------------------------------------------------------------
// --prune / --no-prune
// ---------------------------------------------------------------------------------------------------------

struct P {
    w: World,
    origin: PathBuf,
    ws: PathBuf,
}

/// `origin` (bare) with `main` and `gone`; `ws` is a clone that has fetched both, a local branch `keep`, a local
/// tag `t1` and a ref `refs/remotes/other/x` outside origin's namespace. `gone` is then deleted in `origin`.
fn prune_setup() -> P {
    let w = World::new();
    let seed = w.p("seed");
    std::fs::create_dir(&seed).unwrap();
    w.git(&seed, &["init", "-q"]);
    w.commit(&seed, "a.txt", "a\n");
    w.git(&seed, &["branch", "gone"]);
    let origin = w.p("origin.git");
    w.git(&w.base, &["clone", "-q", "--bare", "seed", "origin.git"]);
    let ws = w.p("ws");
    w.git(&w.base, &["clone", "-q", "origin.git", "ws"]);
    w.git(&ws, &["fetch", "-q", "origin"]);
    w.git(&ws, &["branch", "keep"]);
    w.git(&ws, &["tag", "t1"]);
    w.git(&ws, &["update-ref", "refs/remotes/other/x", "HEAD"]);
    w.git(&origin, &["branch", "-q", "-D", "gone"]);
    assert!(!w
        .git(&ws, &["branch", "-r", "--list", "origin/gone"])
        .is_empty());
    P { w, origin, ws }
}

fn has_tracking(p: &P, name: &str) -> bool {
    !p.w.git(
        &p.ws,
        &["branch", "-r", "--list", &format!("origin/{name}")],
    )
    .is_empty()
}

fn untouched_outside_scope(p: &P) {
    for r in [
        "refs/heads/main",
        "refs/heads/keep",
        "refs/tags/t1",
        "refs/remotes/other/x",
    ] {
        p.w.git(&p.ws, &["rev-parse", "--verify", "-q", r]);
    }
}

#[test]
fn prune_deletes_a_gone_remote_tracking_ref_and_nothing_else() {
    let p = prune_setup();
    let (code, v, err) = p.w.stw(&p.ws, &["--prune"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["fetch"], "ok", "{r}");
    assert_eq!(r["via"], "git");
    assert_eq!(r["prune"], "on");
    assert_eq!(r["pruned"], serde_json::json!(["refs/remotes/origin/gone"]));
    assert_eq!(v["summary"]["pruned"], 1);
    assert_eq!(v["options"]["prune"], true);
    assert!(r["reason"]
        .as_str()
        .unwrap()
        .contains("pruned 1 remote-tracking ref(s): refs/remotes/origin/gone"));
    assert!(!has_tracking(&p, "gone"));
    assert!(has_tracking(&p, "main"));
    untouched_outside_scope(&p);
}

#[test]
fn without_a_flag_and_without_configuration_nothing_is_pruned() {
    let p = prune_setup();
    let (code, v, err) = p.w.stw(&p.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["fetch"], "ok");
    assert!(r.get("prune").is_none(), "no flag, no field: {r}");
    assert!(has_tracking(&p, "gone"));
    assert!(v["options"]["prune"].is_null());
}

#[test]
fn fetch_prune_from_the_configuration_applies_without_a_flag() {
    let p = prune_setup();
    p.w.git(&p.ws, &["config", "fetch.prune", "true"]);
    let (code, v, err) = p.w.stw(&p.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert!(
        !has_tracking(&p, "gone"),
        "fetch.prune applies as for git fetch"
    );
    assert_eq!(
        repo(&v, "ws")["pruned"],
        serde_json::json!(["refs/remotes/origin/gone"])
    );
}

#[test]
fn no_prune_overrides_fetch_prune() {
    let p = prune_setup();
    p.w.git(&p.ws, &["config", "fetch.prune", "true"]);
    let (code, v, err) = p.w.stw(&p.ws, &["--no-prune"]);
    assert_eq!(code, 0, "{v} {err}");
    assert!(has_tracking(&p, "gone"), "--no-prune keeps the ref");
    let r = repo(&v, "ws");
    assert_eq!(
        (r["prune"].as_str(), r["via"].as_str()),
        (Some("off"), Some("git"))
    );
    assert_eq!(r["pruned"], serde_json::json!([]));
    assert_eq!(v["options"]["prune"], false);
}

#[test]
fn prune_overrides_fetch_prune_false() {
    let p = prune_setup();
    p.w.git(&p.ws, &["config", "fetch.prune", "false"]);
    p.w.stw(&p.ws, &["--prune"]);
    assert!(!has_tracking(&p, "gone"));
}

#[test]
fn prune_and_no_prune_exclude_each_other() {
    let p = prune_setup();
    let (code, v, err) = p.w.stw(&p.ws, &["--prune", "--no-prune"]);
    assert_eq!(code, 2, "{err}");
    assert!(v.is_null(), "usage error prints no result");
    assert!(
        err.contains("--prune") && err.contains("--no-prune"),
        "{err}"
    );
    assert!(has_tracking(&p, "gone"), "nothing ran");
}

#[test]
fn the_dry_run_prunes_nothing_fetches_nothing_and_says_what_prune_would_do() {
    let p = prune_setup();
    let (code, v, err) = p.w.stw(&p.ws, &["--plan", "--prune"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(v["mode"], "dry-run");
    assert_eq!(r["fetch"], "not-run");
    assert_eq!(r["prune"], "on");
    assert_eq!(
        r["tracking_refs"], 2,
        "origin/gone and origin/main, not origin/HEAD: {r}"
    );
    assert_eq!(r["pruned"], serde_json::json!([]));
    let reason = r["reason"].as_str().unwrap();
    assert!(
        reason.contains("--prune would delete") && reason.contains("refs/remotes/origin/"),
        "{reason}"
    );
    assert!(has_tracking(&p, "gone"));
    let (_, v, _) = p.w.stw(&p.ws, &["--plan", "--no-prune"]);
    assert!(repo(&v, "ws")["reason"]
        .as_str()
        .unwrap()
        .contains("--no-prune"));
}

#[test]
fn a_refspec_that_writes_outside_the_namespace_refuses_prune_before_any_fetch() {
    let p = prune_setup();
    p.w.git(
        &p.ws,
        &[
            "config",
            "--add",
            "remote.origin.fetch",
            "+refs/tags/*:refs/tags/*",
        ],
    );
    let (code, v, _) = p.w.stw(&p.ws, &["--prune"]);
    assert_eq!(code, 1);
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "error", "{r}");
    assert_eq!(r["fetch"], "failed");
    assert_eq!(r["git_error"], "refused-refspec");
    assert!(r["error"].as_str().unwrap().contains("refs/tags/*"), "{r}");
    assert!(r.get("via").is_none(), "nothing was started: {r}");
    assert!(
        !r["error"].as_str().unwrap().contains("GH_TOKEN"),
        "no token hint for a refusal"
    );
    assert!(has_tracking(&p, "gone"), "nothing pruned");
    // without --prune the same configuration is fetched as before
    let (code, v, _) = p.w.stw(&p.ws, &[]);
    assert_eq!((code, repo(&v, "ws")["fetch"].as_str()), (0, Some("ok")));
}

#[test]
fn a_tag_is_never_pruned_even_with_prune_tags_configured() {
    let p = prune_setup();
    p.w.git(&p.ws, &["config", "remote.origin.pruneTags", "true"]);
    let (code, v, err) = p.w.stw(&p.ws, &["--prune"]);
    assert_eq!(code, 0, "{v} {err}");
    p.w.git(&p.ws, &["rev-parse", "--verify", "-q", "refs/tags/t1"]);
    assert!(!has_tracking(&p, "gone"));
}

#[test]
fn a_branch_whose_upstream_was_pruned_is_reported_and_kept() {
    let p = prune_setup();
    p.w.git(&p.ws, &["branch", "--set-upstream-to=origin/gone", "keep"]);
    p.w.git(&p.ws, &["checkout", "-q", "keep"]);
    let (code, v, err) = p.w.stw(&p.ws, &["--prune"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "skip", "{r}");
    assert!(
        r["reason"]
            .as_str()
            .unwrap()
            .starts_with("upstream branch is gone"),
        "{r}"
    );
    p.w.git(&p.ws, &["rev-parse", "--verify", "-q", "refs/heads/keep"]);
}

#[test]
fn prune_runs_before_the_fast_forward_and_both_are_reported() {
    let p = prune_setup();
    let seed = p.w.p("seed");
    let tip = p.w.commit(&seed, "b.txt", "b\n");
    p.w.git(&seed, &["push", "-q", p.origin.to_str().unwrap(), "main"]);
    let (code, v, err) = p.w.stw(&p.ws, &["--prune"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "fast-forwarded", "{r}");
    assert_eq!(r["pruned"], serde_json::json!(["refs/remotes/origin/gone"]));
    assert_eq!(p.w.git(&p.ws, &["rev-parse", "HEAD"]), tip);
}

#[test]
fn prune_for_an_https_remote_goes_through_the_git_runner_and_no_prune_stays_with_gix() {
    let p = prune_setup();
    p.w.git(
        &p.ws,
        &["remote", "set-url", "origin", "https://127.0.0.1:1/x/y.git"],
    );
    let (code, v, _) = p.w.stw(&p.ws, &["--prune"]);
    assert_eq!(code, 1);
    let r = repo(&v, "ws");
    assert_eq!(
        (r["via"].as_str(), r["fetch"].as_str()),
        (Some("git"), Some("failed")),
        "{r}"
    );
    assert!(
        r["error"].as_str().unwrap().contains("credential helper"),
        "{r}"
    );
    let (_, v, _) = p.w.stw(&p.ws, &["--no-prune"]);
    assert_eq!(repo(&v, "ws")["via"], "gix", "gix path as before: {v}");
}

// ---------------------------------------------------------------------------------------------------------
// --submodules
// ---------------------------------------------------------------------------------------------------------

struct S {
    w: World,
    ws: PathBuf,
    c: PathBuf,
    /// commits of the submodule's origin: c1 (the gitlink), c2 (newer)
    c1: String,
    c2: String,
}

/// `ws` records the submodule `c` at c1 (detached there); `c2` is a later commit on origin's main.
fn sub_setup() -> S {
    let w = World::new();
    let up = w.p("c-up");
    std::fs::create_dir(&up).unwrap();
    w.git(&up, &["init", "-q"]);
    let c1 = w.commit(&up, "c.txt", "1\n");
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
    w.git(&ws, &["commit", "-q", "-m", "add c"]);
    let c2 = w.commit(&up, "c.txt", "2\n");
    w.git(&up, &["push", "-q", w.p("c.git").to_str().unwrap(), "main"]);
    let c = ws.join("c");
    w.git(&c, &["checkout", "-q", "--detach", &c1]);
    S { w, ws, c, c1, c2 }
}

impl S {
    /// The superproject records c2 while the submodule still sits at c1 (and has c2 locally).
    fn pin_ahead(&self) {
        self.w.git(&self.c, &["fetch", "-q", "origin"]);
        self.w.git(
            &self.ws,
            &[
                "update-index",
                "--cacheinfo",
                &format!("160000,{},c", self.c2),
            ],
        );
    }

    fn head(&self) -> String {
        self.w.git(&self.c, &["rev-parse", "HEAD"])
    }

    /// Everything the superproject records, which `--submodules` must not change.
    fn super_state(&self) -> String {
        format!(
            "{}|{}|{}",
            self.w.git(&self.ws, &["ls-files", "--stage"]),
            self.w.git(&self.ws, &["rev-parse", "HEAD", "HEAD^{tree}"]),
            std::fs::read_to_string(self.ws.join(".gitmodules")).unwrap()
        )
    }
}

#[test]
fn a_pinned_submodule_moves_to_the_recorded_commit_and_nothing_else_changes() {
    let s = sub_setup();
    s.pin_ahead();
    let before = s.super_state();
    let (code, v, err) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "c");
    assert_eq!(r["result"], "submodule-moved", "{r}");
    assert_eq!(r["submodule"]["state"], "moved");
    assert_eq!(r["submodule"]["old"], s.c1.as_str());
    assert_eq!(r["submodule"]["new"], s.c2.as_str());
    assert_eq!(r["via"], "git");
    assert_eq!(s.head(), s.c2);
    assert_eq!(
        s.super_state(),
        before,
        "a gitlink, the index or the superproject changed"
    );
    assert_eq!(v["summary"]["submodules_moved"], 1);
    assert_eq!(v["options"]["submodules"], true);
    assert!(
        r["reason"]
            .as_str()
            .unwrap()
            .contains("moved the pinned submodule"),
        "{r}"
    );
}

#[test]
fn the_dry_run_names_the_move_and_writes_nothing() {
    let s = sub_setup();
    s.pin_ahead();
    let url = s.w.git(&s.c, &["config", "remote.origin.url"]);
    s.w.git(
        &s.ws,
        &[
            "config",
            "-f",
            ".gitmodules",
            "submodule.c.url",
            s.w.p("other.git").to_str().unwrap(),
        ],
    );
    let before = s.super_state();
    let (code, v, err) = s.w.stw(&s.ws, &["--plan", "--submodules"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "c");
    assert_eq!(r["result"], "submodule-move", "{r}");
    assert_eq!(r["submodule"]["state"], "would-move");
    assert_eq!(r["submodule"]["old"], s.c1.as_str());
    assert_eq!(r["submodule"]["new"], s.c2.as_str());
    assert_eq!(v["summary"]["would_move_submodules"], 1);
    assert_eq!(s.head(), s.c1);
    assert_eq!(
        s.w.git(&s.c, &["config", "remote.origin.url"]),
        url,
        "no submodule sync in a dry run"
    );
    assert_eq!(s.super_state(), before);
}

#[test]
fn the_dry_run_with_prune_still_names_the_submodule_move() {
    let s = sub_setup();
    s.pin_ahead();
    let (code, v, err) =
        s.w.stw(&s.ws, &["--plan", "--prune", "--submodules", "--offline"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo(&v, "c")["submodule"]["state"], "would-move", "{v}");
    assert_eq!(s.head(), s.c1);
}

#[test]
fn without_the_flag_a_pinned_submodule_is_left_alone() {
    let s = sub_setup();
    s.pin_ahead();
    let (code, v, err) = s.w.stw(&s.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "c");
    assert_eq!(
        (r["result"].as_str(), r["reason"].as_str()),
        (Some("skip"), Some("detached HEAD"))
    );
    assert!(r.get("submodule").is_none());
    assert_eq!(s.head(), s.c1);
}

#[test]
fn a_submodule_at_the_recorded_commit_is_up_to_date() {
    let s = sub_setup();
    let (code, v, err) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "c");
    assert_eq!(
        (r["result"].as_str(), r["submodule"]["state"].as_str()),
        (Some("none"), Some("up-to-date")),
        "{r}"
    );
    assert_eq!(s.head(), s.c1);
}

#[test]
fn local_changes_in_the_submodule_keep_it_where_it_is() {
    let s = sub_setup();
    s.pin_ahead();
    std::fs::write(s.c.join("c.txt"), "dirty\n").unwrap();
    let (code, v, err) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "c");
    assert_eq!(
        (r["result"].as_str(), r["submodule"]["state"].as_str()),
        (Some("skip"), Some("skipped-dirty")),
        "{r}"
    );
    assert_eq!(s.head(), s.c1);
    assert_eq!(
        std::fs::read_to_string(s.c.join("c.txt")).unwrap(),
        "dirty\n"
    );
}

#[test]
fn a_recorded_commit_that_is_not_a_fast_forward_is_not_applied() {
    let s = sub_setup();
    s.pin_ahead();
    let local = s.w.commit(&s.c, "local.txt", "l\n");
    let (code, v, err) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "c");
    assert_eq!(
        (r["result"].as_str(), r["submodule"]["state"].as_str()),
        (Some("skip"), Some("not-fast-forward")),
        "{r}"
    );
    assert_eq!(s.head(), local);
    // HEAD ahead of the recorded commit is the same
    let s = sub_setup();
    s.w.git(&s.c, &["fetch", "-q", "origin"]);
    s.w.git(&s.c, &["checkout", "-q", "--detach", &s.c2]);
    let (_, v, _) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(
        repo(&v, "c")["submodule"]["state"],
        "not-fast-forward",
        "{v}"
    );
    assert_eq!(s.head(), s.c2);
}

#[test]
fn a_recorded_commit_the_submodule_lacks_is_unreachable_and_nothing_is_fetched_for_it() {
    let s = sub_setup();
    // a commit that exists in no repository the submodule can reach
    let other = s.w.p("other");
    std::fs::create_dir(&other).unwrap();
    s.w.git(&other, &["init", "-q"]);
    let foreign = s.w.commit(&other, "o.txt", "o\n");
    s.w.git(
        &s.ws,
        &[
            "update-index",
            "--cacheinfo",
            &format!("160000,{foreign},c"),
        ],
    );
    let (code, v, _) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(code, 1, "unreachable exits 1: {v}");
    let r = repo(&v, "c");
    assert_eq!(
        (r["result"].as_str(), r["submodule"]["state"].as_str()),
        (Some("error"), Some("unreachable")),
        "{r}"
    );
    assert!(
        r["reason"]
            .as_str()
            .unwrap()
            .starts_with("unreachable: the recorded commit"),
        "{r}"
    );
    assert_eq!(s.head(), s.c1);
    // the dry run only predicts
    let (code, v, _) = s.w.stw(&s.ws, &["--plan", "--submodules"]);
    assert_eq!(code, 0);
    assert!(
        repo(&v, "c")["reason"]
            .as_str()
            .unwrap()
            .starts_with("would be unreachable"),
        "{v}"
    );
}

#[test]
fn an_uninitialized_submodule_is_unreachable_and_never_cloned() {
    let s = sub_setup();
    let fresh = s.w.p("fresh");
    s.w.git(&s.w.base, &["clone", "-q", s.ws.to_str().unwrap(), "fresh"]);
    assert!(!fresh.join("c/.git").exists());
    let (code, v, _) = s.w.stw(&fresh, &["--submodules"]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "c");
    assert_eq!(
        (r["result"].as_str(), r["submodule"]["state"].as_str()),
        (Some("error"), Some("unreachable")),
        "{r}"
    );
    assert!(
        r["reason"].as_str().unwrap().contains("not initialized"),
        "{r}"
    );
    assert!(!fresh.join("c/.git").exists(), "stw never clones");
    // without the flag the submodule is not listed at all, as before
    let (code, v, _) = s.w.stw(&fresh, &[]);
    assert_eq!(code, 0);
    assert!(
        v["repos"]
            .as_array()
            .unwrap()
            .iter()
            .all(|r| r["name"] != "c"),
        "{v}"
    );
    // the dry run predicts
    let (code, v, _) = s.w.stw(&fresh, &["--plan", "--submodules"]);
    assert_eq!(code, 0);
    assert!(
        repo(&v, "c")["reason"]
            .as_str()
            .unwrap()
            .starts_with("would be unreachable"),
        "{v}"
    );
}

#[test]
fn hooks_of_the_submodule_do_not_run_during_the_move() {
    let s = sub_setup();
    s.pin_ahead();
    let marker = s.w.p("hook-ran");
    let hooks = s.ws.join(".git/modules/c/hooks");
    std::fs::create_dir_all(&hooks).unwrap();
    let hook = hooks.join("post-checkout");
    std::fs::write(&hook, format!("#!/bin/sh\ntouch '{}'\n", marker.display())).unwrap();
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(&hook, std::fs::Permissions::from_mode(0o755)).unwrap();
    let (code, v, err) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(s.head(), s.c2, "the move happened");
    assert!(!marker.exists(), "a hook of the submodule ran");
}

#[test]
fn a_filter_driver_in_the_submodule_config_refuses_the_move() {
    let s = sub_setup();
    s.pin_ahead();
    s.w.git(&s.c, &["config", "filter.x.smudge", "touch smudged"]);
    let (code, v, _) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "c");
    assert_eq!(
        (r["result"].as_str(), r["git_error"].as_str()),
        (Some("error"), Some("refused-config")),
        "{r}"
    );
    assert_eq!(s.head(), s.c1);
}

#[test]
fn a_changed_url_in_gitmodules_is_reported_without_credentials() {
    let s = sub_setup();
    s.pin_ahead();
    let old = s.w.git(&s.c, &["config", "remote.origin.url"]);
    let new = format!("{}", s.w.p("c.git").display());
    s.w.git(
        &s.ws,
        &[
            "config",
            "-f",
            ".gitmodules",
            "submodule.c.url",
            &format!("file://user:SECRETPW@/{}", new.trim_start_matches('/')),
        ],
    );
    let (code, v, err) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "c");
    let note = r["submodule"]["note"].as_str().unwrap_or("");
    assert!(note.contains("URL rewritten by git submodule sync"), "{r}");
    assert!(note.contains(&old), "{note}");
    assert!(!note.contains("SECRETPW"), "{note}");
    assert_eq!(s.head(), s.c2);
}

#[test]
fn a_submodule_path_that_is_not_a_plain_path_is_unreachable() {
    let s = sub_setup();
    s.w.git(&s.ws, &["mv", "c", "dir with space"]);
    let (code, v, _) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "dir with space");
    assert_eq!(r["submodule"]["state"], "unreachable", "{r}");
    assert!(
        r["reason"]
            .as_str()
            .unwrap()
            .contains("submodule path not accepted"),
        "{r}"
    );
}

#[test]
fn a_submodule_path_through_a_symbolic_link_is_left_alone() {
    let s = sub_setup();
    std::os::unix::fs::symlink(".", s.ws.join("dir")).unwrap();
    s.w.git(
        &s.ws,
        &[
            "config",
            "-f",
            ".gitmodules",
            "submodule.dirc.path",
            "dir/c",
        ],
    );
    crate::common::stage_gitlink_in(&s.ws, "dir/c");
    // offline: `dir/c` and `c` are one repository, which two fetches would lock against each other
    let (code, v, _) = s.w.stw(&s.ws, &["--submodules", "--offline"]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "dir/c");
    assert_eq!(r["submodule"]["state"], "unreachable", "{r}");
    assert!(
        r["reason"].as_str().unwrap().contains("symbolic link"),
        "{r}"
    );
}

#[test]
fn a_move_that_git_did_not_do_is_a_failure_not_a_success() {
    let s = sub_setup();
    s.pin_ahead();
    // an inactive submodule: `git submodule update` skips it without an error
    s.w.git(&s.ws, &["config", "submodule.c.active", "false"]);
    let (code, v, _) = s.w.stw(&s.ws, &["--submodules"]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "c");
    assert_eq!(
        (r["result"].as_str(), r["submodule"]["state"].as_str()),
        (Some("error"), Some("failed")),
        "{r}"
    );
    assert!(
        r["reason"].as_str().unwrap().contains("did not land"),
        "{r}"
    );
    assert_eq!(s.head(), s.c1);
}

#[test]
fn hostile_submodule_paths_are_reported_escaped_and_never_touched() {
    let s = sub_setup();
    let fresh = s.w.p("fresh");
    s.w.git(&s.w.base, &["clone", "-q", s.ws.to_str().unwrap(), "fresh"]);
    let mut gm = std::fs::read_to_string(fresh.join(".gitmodules")).unwrap();
    gm.push_str("[submodule \"esc\"]\n\tpath = x\u{1b}[31mRED\u{1b}[0m\n\turl = ../c.git\n");
    gm.push_str("[submodule \"up\"]\n\tpath = ../outside\n\turl = ../c.git\n");
    gm.push_str("[submodule \"abs\"]\n\tpath = /abs/x\n\turl = ../c.git\n");
    std::fs::write(fresh.join(".gitmodules"), gm).unwrap();
    // git cannot hold `../outside` or `/abs/x` in an index: `.gitmodules` alone makes no repository of them
    crate::common::stage_gitlink_in(&fresh, "x\u{1b}[31mRED\u{1b}[0m");
    // JSON: the control character is escaped by the encoder, the reason is the plain-path refusal
    let (code, v, _) = s.w.stw(&fresh, &["--submodules", "--offline"]);
    assert_eq!(code, 1, "{v}");
    for name in ["../outside", "/abs/x"] {
        assert!(
            v["repos"]
                .as_array()
                .unwrap()
                .iter()
                .all(|r| r["name"] != name),
            "{name}: {v}"
        );
    }
    let r = repo(&v, "x\u{1b}[31mRED\u{1b}[0m");
    assert_eq!(r["submodule"]["state"], "unreachable", "{r}");
    assert!(
        r["reason"]
            .as_str()
            .unwrap()
            .contains("submodule path not accepted"),
        "{r}"
    );
    // text: no raw control character reaches the terminal
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &s.w.home)
        .args(["sync", "--plan", "--submodules", "--offline", "--root"])
        .arg(&fresh)
        .output()
        .unwrap();
    let text = String::from_utf8_lossy(&o.stdout).into_owned();
    assert!(!text.contains('\u{1b}'), "{text:?}");
    assert!(text.contains("x\\u{1b}[31mRED"), "{text}");
}

#[test]
fn the_pass_acts_on_the_typed_pinned_flag_not_on_the_reason_text() {
    let s = sub_setup();
    s.pin_ahead();
    let o = stw::sync::Opts {
        offline: true,
        submodules: true,
        ..Default::default()
    };
    let rec = |reason: &str, pinned: bool| {
        let mut r = stw::sync::sync_repo(
            &stw::repo::RepoRef {
                name: "c".into(),
                path: s.c.clone(),
            },
            None,
            &o,
        );
        assert!(r.pinned, "a detached submodule is pinned: {r:?}");
        r.reason = reason.into();
        r.pinned = pinned;
        r
    };
    let run = |mut r: Vec<stw::sync::SyncRec>| {
        stw::sync::submodule_pass(&s.ws, &mut r, &o, false, std::path::Path::new("/none"));
        r.remove(0)
    };
    let moved = run(vec![rec("any other text", true)]);
    assert_eq!(moved.submodule.unwrap().state, "would-move");
    let left = run(vec![rec("detached HEAD", false)]);
    assert!(left.submodule.is_none(), "{left:?}");
}

#[test]
fn a_refspec_into_a_sibling_namespace_of_origin_refuses_prune() {
    let p = prune_setup();
    p.w.git(
        &p.ws,
        &[
            "config",
            "remote.origin.fetch",
            "+refs/heads/*:refs/remotes/origin-mirror/*",
        ],
    );
    let (code, v, _) = p.w.stw(&p.ws, &["--prune"]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "ws");
    assert_eq!(r["git_error"], "refused-refspec", "{r}");
    assert!(has_tracking(&p, "gone"));
}

#[test]
fn text_output_names_the_move_and_the_counts() {
    let s = sub_setup();
    s.pin_ahead();
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &s.w.home)
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .args(["sync", "--plan", "--submodules", "--root"])
        .arg(&s.ws)
        .output()
        .unwrap();
    let text = String::from_utf8_lossy(&o.stdout).into_owned();
    assert!(text.contains("submodule-move"), "{text}");
    assert!(
        text.contains("1 submodule(s) would move. Nothing was written."),
        "{text}"
    );
}

#[test]
fn the_rules_checkout_is_not_reported_as_an_uninitialized_submodule() {
    let s = sub_setup();
    let fresh = s.w.p("fresh");
    s.w.git(&s.w.base, &["clone", "-q", s.ws.to_str().unwrap(), "fresh"]);
    let (code, v, err) = s.w.stw(
        &fresh,
        &[
            "--submodules",
            "--rules-path",
            fresh.join("c").to_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{v} {err}");
    assert!(
        v["repos"]
            .as_array()
            .unwrap()
            .iter()
            .all(|r| r["name"] != "c"),
        "{v}"
    );
}

#[test]
fn a_repo_filter_does_not_add_uninitialized_submodules() {
    let s = sub_setup();
    let fresh = s.w.p("fresh");
    s.w.git(&s.w.base, &["clone", "-q", s.ws.to_str().unwrap(), "fresh"]);
    let (code, v, err) = s.w.stw(&fresh, &["--submodules", "--repo", "fresh"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(v["repos"].as_array().unwrap().len(), 1, "{v}");
}
