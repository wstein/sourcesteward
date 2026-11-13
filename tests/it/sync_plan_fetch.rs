//! `stw sync --plan` / `--dry-run` fetch the remote-tracking refs, like the Go plan (`internal/stwsync/sync.go`,
//! `TestSyncPlanChangesOnlyRemoteTrackingRefs`), and write nothing else: no branch, no working tree, no recovery
//! ref, no journal. Remotes are local bare repositories (no network).
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use serde_json::Value;

use crate::sync_flags::World;

struct D {
    w: World,
    ws: PathBuf,
    old: String,
    new: String,
}

/// `origin.git` has `main` and `gone`; `ws` is a clone that has fetched both and has a local branch `keep`, a tag `t1`
/// and a ref `refs/remotes/other/x`. Then origin gets one more commit on `main` (`new`) and loses `gone`.
fn setup() -> D {
    let w = World::new();
    let seed = w.p("seed");
    std::fs::create_dir(&seed).unwrap();
    w.git(&seed, &["init", "-q"]);
    let old = w.commit(&seed, "a.txt", "a\n");
    w.git(&seed, &["branch", "gone"]);
    let origin = w.p("origin.git");
    w.git(&w.base, &["clone", "-q", "--bare", "seed", "origin.git"]);
    let ws = w.p("ws");
    w.git(&w.base, &["clone", "-q", "origin.git", "ws"]);
    w.git(&ws, &["fetch", "-q", "origin"]);
    w.git(&ws, &["branch", "keep"]);
    w.git(&ws, &["tag", "t1"]);
    w.git(&ws, &["update-ref", "refs/remotes/other/x", "HEAD"]);
    let new = w.commit(&seed, "b.txt", "b\n");
    w.git(
        &seed,
        &["push", "-q", origin.to_str().unwrap(), "HEAD:main"],
    );
    w.git(&origin, &["branch", "-q", "-D", "gone"]);
    D { w, ws, old, new }
}

impl D {
    fn plan(&self, args: &[&str]) -> (i32, Value, String) {
        let mut a = vec!["--plan"];
        a.extend_from_slice(args);
        self.w.stw(&self.ws, &a)
    }

    fn refs(&self) -> BTreeMap<String, String> {
        self.w
            .git(
                &self.ws,
                &["for-each-ref", "--format=%(refname) %(objectname)"],
            )
            .lines()
            .filter_map(|l| l.split_once(' '))
            .map(|(a, b)| (a.to_string(), b.to_string()))
            .collect()
    }

    fn tracking(&self, name: &str) -> Option<String> {
        self.refs()
            .get(&format!("refs/remotes/origin/{name}"))
            .cloned()
    }
}

fn entry<'a>(v: &'a Value, name: &str) -> &'a Value {
    v["repos"]
        .as_array()
        .unwrap_or_else(|| panic!("no repos in {v}"))
        .iter()
        .find(|r| r["name"] == name)
        .unwrap_or_else(|| panic!("no {name} in {v}"))
}

/// All refs except the remote-tracking refs of `origin`.
fn others(m: &BTreeMap<String, String>) -> BTreeMap<String, String> {
    m.iter()
        .filter(|(k, _)| !k.starts_with("refs/remotes/origin/"))
        .map(|(k, v)| (k.clone(), v.clone()))
        .collect()
}

fn files(dir: &Path) -> Vec<String> {
    let mut v: Vec<String> = std::fs::read_dir(dir)
        .unwrap()
        .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
        .filter(|n| n != ".git")
        .collect();
    v.sort();
    v
}

#[test]
fn the_plan_fetches_the_new_commits_and_writes_nothing_but_the_tracking_refs() {
    let d = setup();
    let before = d.refs();
    assert_eq!(d.tracking("main").as_deref(), Some(d.old.as_str()));
    let head = d.w.git(&d.ws, &["rev-parse", "HEAD"]);
    let (code, v, err) = d.plan(&[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(v["mode"], "dry-run");
    let r = entry(&v, "ws");
    assert_eq!(r["fetch"], "ok", "{r}");
    assert_eq!(r["via"], "git", "{r}");
    assert_eq!(r["result"], "fast-forward", "{r}");
    assert_eq!(r["behind"], 1, "{r}");
    assert_eq!(v["run_id"], Value::Null);
    assert_eq!(v["summary"]["would_fast_forward"], 1);
    // the fetch wrote the tracking ref, nothing else
    assert_eq!(d.tracking("main").as_deref(), Some(d.new.as_str()));
    assert_eq!(others(&d.refs()), others(&before), "no other ref changed");
    assert_eq!(d.w.git(&d.ws, &["rev-parse", "HEAD"]), head);
    assert_eq!(d.w.git(&d.ws, &["rev-parse", "refs/heads/main"]), d.old);
    assert_eq!(files(&d.ws), ["a.txt"], "the working tree did not move");
    assert!(
        d.refs().keys().all(|k| !k.starts_with("refs/stw/")),
        "no recovery ref"
    );
    assert!(!d.ws.join(".git/stw").exists(), "no journal");
    // a run after the plan does what the plan said
    let (code, v, err) = d.w.stw(&d.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(entry(&v, "ws")["result"], "fast-forwarded");
    assert_eq!(d.w.git(&d.ws, &["rev-parse", "refs/heads/main"]), d.new);
}

#[test]
fn dry_run_is_the_same_flag_and_fetches_too() {
    let d = setup();
    let (code, v, err) = d.w.stw(&d.ws, &["--dry-run"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(entry(&v, "ws")["fetch"], "ok");
    assert_eq!(d.tracking("main").as_deref(), Some(d.new.as_str()));
}

#[test]
fn the_offline_plan_fetches_nothing() {
    let d = setup();
    let before = d.refs();
    let (code, v, err) = d.plan(&["--offline"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = entry(&v, "ws");
    assert_eq!(r["fetch"], "not-run", "{r}");
    assert_eq!(r["result"], "none", "up to date as of the last fetch: {r}");
    assert_eq!(d.refs(), before);
}

#[test]
fn a_repository_without_origin_is_a_no_remote_skip_not_an_error() {
    let d = setup();
    d.w.git(&d.ws, &["remote", "remove", "origin"]);
    let (code, v, err) = d.plan(&[]);
    assert_eq!(code, 0, "{v} {err}");
    let r = entry(&v, "ws");
    assert_eq!(r["fetch"], "no-remote", "{r}");
    assert_eq!(r["result"], "skip", "{r}");
    assert!(r["error"].is_null(), "{r}");
}

#[test]
fn a_branch_without_upstream_still_gets_the_fetch_of_origin_and_stays_a_skip() {
    let d = setup();
    d.w.git(&d.ws, &["checkout", "-q", "-b", "topic"]);
    let (code, v, err) = d.plan(&[]);
    assert_eq!(code, 0, "{v} {err}");
    let r = entry(&v, "ws");
    assert_eq!(r["fetch"], "ok", "{r}");
    assert_eq!(r["result"], "skip", "{r}");
    assert!(r["reason"].as_str().unwrap().contains("no upstream"), "{r}");
    assert_eq!(d.tracking("main").as_deref(), Some(d.new.as_str()));
}

#[test]
fn an_unreachable_remote_is_an_error_with_exit_1_and_changes_nothing() {
    let d = setup();
    let before = d.refs();
    let gone = d.w.p("no-such-remote.git");
    d.w.git(
        &d.ws,
        &["remote", "set-url", "origin", gone.to_str().unwrap()],
    );
    let (code, v, err) = d.plan(&[]);
    assert_eq!(code, 1, "{v} {err}");
    let r = entry(&v, "ws");
    assert_eq!(r["fetch"], "failed", "{r}");
    assert_eq!(r["result"], "error", "{r}");
    assert_eq!(r["reason"], "fetch failed", "{r}");
    assert!(!r["error"].as_str().unwrap_or("").is_empty(), "{r}");
    assert_eq!(v["summary"]["errors"], 1);
    assert_eq!(d.refs(), before);
}

#[test]
fn only_origin_is_fetched_when_there_are_several_remotes() {
    let d = setup();
    let other = d.w.p("other.git");
    d.w.git(
        &d.w.base,
        &["clone", "-q", "--bare", "origin.git", "other.git"],
    );
    d.w.git(&d.ws, &["remote", "add", "other", other.to_str().unwrap()]);
    d.w.git(&d.ws, &["fetch", "-q", "other"]);
    let seed = d.w.p("seed");
    let c = d.w.commit(&seed, "c.txt", "c\n");
    d.w.git(&seed, &["push", "-q", other.to_str().unwrap(), "HEAD:main"]);
    let other_before = d.refs().get("refs/remotes/other/main").cloned();
    let (code, v, err) = d.plan(&[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(entry(&v, "ws")["fetch"], "ok");
    assert_eq!(d.tracking("main").as_deref(), Some(d.new.as_str()));
    assert_eq!(
        d.refs().get("refs/remotes/other/main").cloned(),
        other_before,
        "other is not fetched (Go fetches the remote of the upstream; Rust fetches origin)"
    );
    assert_ne!(other_before.as_deref(), Some(c.as_str()));
}

#[test]
fn the_plan_without_a_prune_flag_follows_fetch_prune_like_a_run() {
    let d = setup();
    assert!(d.tracking("gone").is_some());
    let (code, v, err) = d.plan(&[]);
    assert_eq!(code, 0, "{v} {err}");
    assert!(
        d.tracking("gone").is_some(),
        "no flag, no fetch.prune: kept"
    );
    assert!(entry(&v, "ws").get("pruned").is_none());
    d.w.git(&d.ws, &["config", "fetch.prune", "true"]);
    let (code, v, err) = d.plan(&[]);
    assert_eq!(code, 0, "{v} {err}");
    assert!(d.tracking("gone").is_none(), "fetch.prune applies");
    assert_eq!(
        entry(&v, "ws")["pruned"],
        serde_json::json!(["refs/remotes/origin/gone"])
    );
}

#[test]
fn plan_prune_deletes_only_tracking_refs_of_origin_and_names_them() {
    let d = setup();
    let before = d.refs();
    let (code, v, err) = d.plan(&["--prune"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = entry(&v, "ws");
    assert_eq!(r["prune"], "on");
    assert_eq!(
        r["pruned"],
        serde_json::json!(["refs/remotes/origin/gone"]),
        "{r}"
    );
    assert!(
        r.get("tracking_refs").is_none(),
        "the count is for the offline plan: {r}"
    );
    assert_eq!(v["summary"]["pruned"], 1);
    assert!(
        r["reason"]
            .as_str()
            .unwrap()
            .contains("pruned 1 remote-tracking ref(s)"),
        "{r}"
    );
    assert!(d.tracking("gone").is_none());
    assert_eq!(
        others(&d.refs()),
        others(&before),
        "branches, tags and other remotes stay"
    );
    assert!(d.refs().contains_key("refs/heads/keep"));
    assert!(d.refs().contains_key("refs/tags/t1"));
    assert!(d.refs().contains_key("refs/remotes/other/x"));
}

#[test]
fn plan_no_prune_keeps_the_ref_even_with_fetch_prune_true() {
    let d = setup();
    d.w.git(&d.ws, &["config", "fetch.prune", "true"]);
    let (code, v, err) = d.plan(&["--no-prune"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = entry(&v, "ws");
    assert_eq!(r["prune"], "off");
    assert_eq!(r["pruned"], serde_json::json!([]));
    assert!(d.tracking("gone").is_some());
    assert_eq!(d.tracking("main").as_deref(), Some(d.new.as_str()));
}

#[test]
fn the_offline_plan_with_prune_still_only_counts() {
    let d = setup();
    let before = d.refs();
    let (code, v, err) = d.plan(&["--offline", "--prune"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = entry(&v, "ws");
    assert_eq!(r["fetch"], "not-run");
    assert_eq!(r["tracking_refs"], 2, "{r}");
    assert!(
        r["reason"]
            .as_str()
            .unwrap()
            .contains("--prune would delete"),
        "{r}"
    );
    assert_eq!(d.refs(), before);
}

#[test]
fn a_refspec_that_writes_outside_the_namespace_refuses_the_plan_before_any_fetch() {
    for flags in [&[][..], &["--prune"][..], &["--no-prune"][..]] {
        let d = setup();
        d.w.git(
            &d.ws,
            &[
                "config",
                "--add",
                "remote.origin.fetch",
                "+refs/tags/*:refs/tags/*",
            ],
        );
        let before = d.refs();
        let (code, v, _) = d.plan(flags);
        assert_eq!(code, 1, "{flags:?} {v}");
        let r = entry(&v, "ws");
        assert_eq!(r["result"], "error", "{flags:?} {r}");
        assert_eq!(r["fetch"], "failed", "{flags:?} {r}");
        assert_eq!(r["git_error"], "refused-refspec", "{flags:?} {r}");
        assert!(
            r["error"].as_str().unwrap().contains("refs/tags/*"),
            "{flags:?} {r}"
        );
        assert_eq!(d.refs(), before, "{flags:?} nothing was fetched");
    }
}

#[test]
fn the_plan_writes_no_branch_no_recovery_ref_and_no_journal_for_rebase_and_merge_either() {
    let d = setup();
    d.w.commit(&d.ws, "local.txt", "l\n");
    let before = others(&d.refs());
    for flag in ["--rebase", "--merge"] {
        let (code, v, err) = d.plan(&[flag]);
        assert_eq!(code, 0, "{flag} {v} {err}");
        let r = entry(&v, "ws");
        assert_eq!(r["fetch"], "ok", "{r}");
        assert!(
            r["result"] == "rebase" || r["result"] == "merge",
            "{flag}: {r}"
        );
        assert_eq!(others(&d.refs()), before, "{flag}");
        assert!(!d.ws.join(".git/stw").exists(), "{flag}");
        assert_eq!(d.tracking("main").as_deref(), Some(d.new.as_str()));
    }
}

#[test]
fn a_merge_that_the_run_would_refuse_stays_refused_in_the_plan_after_the_fetch() {
    let d = setup();
    d.w.commit(&d.ws, "local.txt", "l\n");
    d.w.git(&d.ws, &["config", "merge.log", "5"]);
    let (code, v, err) = d.plan(&["--merge"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = entry(&v, "ws");
    assert_eq!(r["fetch"], "ok", "{r}");
    assert_eq!(r["result"], "skip", "{r}");
    assert!(
        r["reason"].as_str().unwrap().contains("would be refused"),
        "{r}"
    );
}

#[test]
fn a_hook_of_the_repository_does_not_run_during_the_plan() {
    let d = setup();
    let marker = d.w.p("hook-ran");
    let hook = d.ws.join(".git/hooks/reference-transaction");
    std::fs::write(&hook, format!("#!/bin/sh\ntouch {}\n", marker.display())).unwrap();
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&hook, std::fs::Permissions::from_mode(0o755)).unwrap();
    }
    let (code, v, err) = d.plan(&[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(entry(&v, "ws")["fetch"], "ok");
    assert_eq!(d.tracking("main").as_deref(), Some(d.new.as_str()));
    assert!(!marker.exists(), "no hook runs during the fetch");
}

#[test]
fn the_plan_text_says_that_the_tracking_refs_were_fetched() {
    let d = setup();
    let run = |args: &[&str]| {
        let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
            .env_clear()
            .env("PATH", std::env::var_os("PATH").unwrap())
            .env("HOME", &d.w.home)
            .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
            .arg("sync")
            .args(args)
            .arg("--root")
            .arg(&d.ws)
            .output()
            .unwrap();
        String::from_utf8_lossy(&o.stdout).into_owned()
    };
    let offline = run(&["--plan", "--offline"]);
    assert!(
        offline.contains(
            "dry run: 0 would fast-forward, 0 skipped. Nothing was written. Nothing was fetched either: based on the last fetch.\n"
        ),
        "{offline}"
    );
    let online = run(&["--plan"]);
    assert!(
        online.contains(
            "dry run: 1 would fast-forward, 0 skipped. Only the remote-tracking refs were fetched; nothing else was written.\n"
        ),
        "{online}"
    );
    assert!(!online.contains("Nothing was fetched"), "{online}");
}
