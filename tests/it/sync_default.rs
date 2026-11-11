//! `stw sync` acts by default, like Go; `--plan` and `--dry-run` (the same flag twice) only report; `--apply` is a hidden
//! alias without effect (owner decision 2026-11-11). Every repository lives in a tempdir of `sync_flags::World`.
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use serde_json::Value;

use crate::sync_flags::World;

struct D {
    w: World,
    ws: PathBuf,
    origin: PathBuf,
    /// the commit origin has beyond the clone
    new: String,
    old: String,
}

/// `ws` is a clone of `origin` that is behind by one commit (`new`); origin is a local path (fetched by the runner).
/// The clone has not fetched it: only a run (which fetches) can know.
fn setup() -> D {
    let w = World::new();
    let seed = w.p("seed");
    std::fs::create_dir(&seed).unwrap();
    w.git(&seed, &["init", "-q"]);
    let old = w.commit(&seed, "a.txt", "a\n");
    let origin = w.p("origin.git");
    w.git(&w.base, &["clone", "-q", "--bare", "seed", "origin.git"]);
    let ws = w.p("ws");
    w.git(&w.base, &["clone", "-q", "origin.git", "ws"]);
    let new = w.commit(&seed, "b.txt", "b\n");
    w.git(
        &seed,
        &["push", "-q", origin.to_str().unwrap(), "HEAD:main"],
    );
    D {
        w,
        ws,
        origin,
        new,
        old,
    }
}

/// Like `setup`, and the clone has fetched: a plan (which never fetches) sees that it is behind.
fn setup_fetched() -> D {
    let d = setup();
    d.w.git(&d.ws, &["fetch", "-q", "origin"]);
    d
}

impl D {
    fn tip(&self) -> String {
        self.w.git(&self.ws, &["rev-parse", "refs/heads/main"])
    }

    fn run(&self, args: &[&str]) -> (i32, Value, String) {
        self.w.stw(&self.ws, args)
    }
}

/// Every file below `dir` (the `.git` directory included) with its bytes.
fn snapshot(dir: &Path) -> BTreeMap<String, Vec<u8>> {
    fn walk(base: &Path, dir: &Path, out: &mut BTreeMap<String, Vec<u8>>) {
        for e in std::fs::read_dir(dir).unwrap() {
            let e = e.unwrap();
            let p = e.path();
            let rel = p.strip_prefix(base).unwrap().display().to_string();
            if e.file_type().unwrap().is_dir() {
                out.insert(format!("{rel}/"), Vec::new());
                walk(base, &p, out);
            } else {
                out.insert(rel, std::fs::read(&p).unwrap());
            }
        }
    }
    let mut out = BTreeMap::new();
    walk(dir, dir, &mut out);
    out
}

fn entry<'a>(v: &'a Value, name: &str) -> &'a Value {
    v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .find(|r| r["name"] == name)
        .unwrap_or_else(|| panic!("no {name} in {v}"))
}

const FLAG_SETS: &[&[&str]] = &[
    &[],
    &["--prune"],
    &["--no-prune"],
    &["--submodules"],
    &["--prune", "--submodules"],
    &["--no-prune", "--submodules"],
];

#[test]
fn without_a_flag_sync_acts_and_the_refs_move() {
    let d = setup();
    assert_eq!(d.tip(), d.old);
    let (code, v, err) = d.run(&[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(v["mode"], "apply");
    assert_eq!(v["options"]["dry_run"], false);
    let r = entry(&v, "ws");
    assert_eq!(r["result"], "fast-forwarded", "{r}");
    assert_eq!(r["fetch"], "ok", "{r}");
    assert_eq!(d.tip(), d.new);
    assert_eq!(
        std::fs::read_to_string(d.ws.join("b.txt")).unwrap(),
        "b\n",
        "the working tree moves with the branch"
    );
}

#[test]
fn every_flag_set_acts_by_default_and_changes_nothing_with_plan_or_dry_run() {
    for flags in FLAG_SETS {
        // default: acts
        let d = setup();
        let (code, v, err) = d.run(flags);
        assert_eq!(code, 0, "{flags:?}: {v} {err}");
        assert_eq!(v["mode"], "apply", "{flags:?}");
        assert_eq!(v["options"]["dry_run"], false, "{flags:?}");
        assert_eq!(v["summary"]["fast_forwarded"], 1, "{flags:?}: {v}");
        assert_eq!(d.tip(), d.new, "{flags:?}");
        // the preview in all three spellings: byte-identical repository, working tree and refs
        for plan in [
            &["--plan"][..],
            &["--dry-run"][..],
            &["--plan", "--dry-run"][..],
            &["--dry-run", "--plan"][..],
        ] {
            let d = setup_fetched();
            let before = snapshot(&d.ws);
            let origin_before = snapshot(&d.origin);
            let args: Vec<&str> = plan.iter().chain(flags.iter()).copied().collect();
            let (code, v, err) = d.run(&args);
            assert_eq!(code, 0, "{args:?}: {v} {err}");
            assert_eq!(v["mode"], "dry-run", "{args:?}");
            assert_eq!(v["options"]["dry_run"], true, "{args:?}");
            let r = entry(&v, "ws");
            assert_eq!(r["result"], "fast-forward", "{args:?}: {r}");
            assert_eq!(r["fetch"], "not-run", "{args:?}: {r}");
            assert_eq!(v["summary"]["would_fast_forward"], 1, "{args:?}");
            assert_eq!(v["summary"]["fast_forwarded"], 0, "{args:?}");
            assert_eq!(d.tip(), d.old, "{args:?}");
            assert!(
                snapshot(&d.ws) == before,
                "{args:?} wrote into the repository (FETCH_HEAD, index, refs or files)"
            );
            assert!(snapshot(&d.origin) == origin_before, "{args:?}");
        }
        // a plan never creates FETCH_HEAD (it fetches nothing); a run does
        let d = setup();
        let args: Vec<&str> = ["--plan"].iter().chain(flags.iter()).copied().collect();
        d.run(&args);
        assert!(!d.ws.join(".git/FETCH_HEAD").exists(), "{args:?}");
    }
}

#[test]
fn the_plan_text_keeps_the_dry_run_marker_and_a_run_does_not_have_it() {
    let d = setup_fetched();
    let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &d.w.home)
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .args(["sync", "--plan", "--offline", "--root"])
        .arg(&d.ws)
        .output()
        .unwrap();
    let text = String::from_utf8_lossy(&o.stdout).into_owned();
    assert!(
        text.contains(
            "dry run: 1 would fast-forward, 0 skipped. Nothing was written. Nothing was fetched either: based on the last fetch.\n"
        ),
        "{text}"
    );
    assert_eq!(d.tip(), d.old);
    let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &d.w.home)
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .args(["sync", "--root"])
        .arg(&d.ws)
        .output()
        .unwrap();
    let text = String::from_utf8_lossy(&o.stdout).into_owned();
    assert!(
        text.contains("1 fast-forwarded, 0 skipped, 0 failed"),
        "{text}"
    );
    assert!(!text.contains("dry run"), "{text}");
    assert_eq!(d.tip(), d.new);
}

#[test]
fn apply_is_an_alias_without_effect() {
    // the same result as without the flag
    let d = setup();
    let (code, v, err) = d.run(&["--apply"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(v["mode"], "apply");
    assert_eq!(v["options"]["dry_run"], false);
    assert_eq!(d.tip(), d.new);
    for flags in FLAG_SETS {
        let d = setup();
        let args: Vec<&str> = std::iter::once(&"--apply")
            .chain(flags.iter())
            .copied()
            .collect();
        let (code, v, err) = d.run(&args);
        assert_eq!(code, 0, "{args:?}: {v} {err}");
        assert_eq!(d.tip(), d.new, "{args:?}");
    }
}

#[test]
fn apply_together_with_plan_or_dry_run_is_a_usage_error_and_changes_nothing() {
    for bad in [
        &["--apply", "--plan"][..],
        &["--plan", "--apply"][..],
        &["--apply", "--dry-run"][..],
        &["--dry-run", "--apply", "--plan"][..],
    ] {
        let d = setup();
        let before = snapshot(&d.ws);
        let (code, v, err) = d.run(bad);
        assert_eq!(code, 2, "{bad:?}: {v} {err}");
        assert!(v.is_null(), "{bad:?} printed a result: {v}");
        assert!(err.contains("cannot be used with"), "{bad:?}: {err}");
        assert!(snapshot(&d.ws) == before, "{bad:?}");
        assert_eq!(d.tip(), d.old);
    }
}

#[test]
fn the_help_names_plan_and_dry_run_and_hides_apply() {
    let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .args(["sync", "--help"])
        .output()
        .unwrap();
    let help = String::from_utf8_lossy(&o.stdout).into_owned();
    assert_eq!(o.status.code(), Some(0));
    assert!(
        help.contains("--plan") && help.contains("--dry-run"),
        "{help}"
    );
    assert!(!help.contains("--apply"), "{help}");
    assert!(!help.contains("dry run unless"), "{help}");
    let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .arg("--help")
        .output()
        .unwrap();
    let top = String::from_utf8_lossy(&o.stdout).into_owned();
    assert!(
        !top.contains("sync --apply") && !top.contains("unless --apply"),
        "{top}"
    );
}

#[test]
fn a_run_still_leaves_diverged_and_in_progress_repositories_alone() {
    // diverged: a local commit as well
    let d = setup();
    d.w.commit(&d.ws, "local.txt", "l\n");
    let local = d.tip();
    let (code, v, err) = d.run(&[]);
    assert_eq!(code, 0, "{v} {err}");
    let r = entry(&v, "ws");
    assert_eq!(r["result"], "skip", "{r}");
    assert!(r["reason"].as_str().unwrap().starts_with("diverged"), "{r}");
    assert_eq!(d.tip(), local);
    // an operation in progress
    let d = setup();
    std::fs::write(d.ws.join(".git/MERGE_HEAD"), format!("{}\n", d.old)).unwrap();
    let (code, v, err) = d.run(&[]);
    assert_eq!(code, 0, "{v} {err}");
    let r = entry(&v, "ws");
    assert_eq!(r["result"], "skip", "{r}");
    assert!(r["reason"].as_str().unwrap().contains("in progress"), "{r}");
    assert_eq!(d.tip(), d.old);
    // a local change that the fast-forward would overwrite: refused, nothing changed
    let d = setup();
    let seed = d.w.p("seed");
    d.w.git(&seed, &["checkout", "-q", "-b", "x"]);
    std::fs::write(d.ws.join("b.txt"), "mine\n").unwrap();
    let before = std::fs::read(d.ws.join("b.txt")).unwrap();
    let (code, v, _) = d.run(&[]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(entry(&v, "ws")["result"], "error");
    assert_eq!(d.tip(), d.old);
    assert_eq!(std::fs::read(d.ws.join("b.txt")).unwrap(), before);
}

#[test]
fn a_run_without_network_is_exit_1_and_a_plan_is_exit_0() {
    let d = setup();
    d.w.git(
        &d.ws,
        &["remote", "set-url", "origin", "https://127.0.0.1:1/x/y.git"],
    );
    let (code, v, _) = d.run(&["--plan"]);
    assert_eq!(code, 0, "{v}");
    let (code, v, _) = d.run(&[]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(entry(&v, "ws")["result"], "error");
    assert_eq!(d.tip(), d.old);
    // --offline run: no fetch, nothing new to move, exit 0
    let (code, v, _) = d.run(&["--offline"]);
    assert_eq!(code, 0, "{v}");
    assert_eq!(entry(&v, "ws")["fetch"], "skipped-offline");
}

#[test]
fn a_repeated_plan_or_dry_run_flag_is_accepted() {
    for rep in [
        &["--plan", "--plan"][..],
        &["--dry-run", "--dry-run"][..],
        &["--plan", "--dry-run", "--plan"][..],
    ] {
        let d = setup_fetched();
        let before = snapshot(&d.ws);
        let (code, v, err) = d.run(rep);
        assert_eq!(code, 0, "{rep:?}: {v} {err}");
        assert_eq!(v["mode"], "dry-run", "{rep:?}");
        assert!(snapshot(&d.ws) == before, "{rep:?}");
    }
}

#[test]
fn rebase_merge_and_autostash_settings_are_not_followed_and_a_diverged_skip_says_so() {
    for cfg in [
        &[("pull.rebase", "true")][..],
        &[("branch.main.rebase", "merges")][..],
        &[
            ("pull.ff", "false"),
            ("rebase.autoStash", "true"),
            ("merge.autoStash", "true"),
        ][..],
    ] {
        // behind and dirty elsewhere: a plain fast-forward, no stash, no merge commit
        let d = setup();
        for (k, v) in cfg {
            d.w.git(&d.ws, &["config", k, v]);
        }
        std::fs::write(d.ws.join("untouched.txt"), "local\n").unwrap();
        d.w.git(&d.ws, &["add", "untouched.txt"]);
        let (code, v, err) = d.run(&[]);
        assert_eq!(code, 0, "{cfg:?}: {v} {err}");
        assert_eq!(entry(&v, "ws")["result"], "fast-forwarded", "{cfg:?}");
        assert_eq!(d.tip(), d.new, "{cfg:?}");
        assert_eq!(d.w.git(&d.ws, &["stash", "list"]), "", "{cfg:?}");
        assert_eq!(
            d.w.git(&d.ws, &["rev-list", "--merges", "--count", "main"]),
            "0",
            "{cfg:?}"
        );
        assert!(d.ws.join("untouched.txt").exists());
        // diverged: skipped; the reason names the ignored rebase setting only when one is set
        let d = setup();
        for (k, v) in cfg {
            d.w.git(&d.ws, &["config", k, v]);
        }
        d.w.commit(&d.ws, "local.txt", "l\n");
        let local = d.tip();
        let (code, v, _) = d.run(&[]);
        assert_eq!(code, 0, "{cfg:?}: {v}");
        let r = entry(&v, "ws");
        assert_eq!(r["result"], "skip", "{cfg:?}: {r}");
        let reason = r["reason"].as_str().unwrap();
        let named = reason.contains("stw sync never rebases (not ported)");
        let rebase_set = cfg.iter().any(|(k, _)| k.ends_with(".rebase"));
        assert_eq!(named, rebase_set, "{cfg:?}: {reason}");
        assert_eq!(d.tip(), local, "{cfg:?}");
    }
    // a false setting is not a request to rebase
    let d = setup();
    d.w.git(&d.ws, &["config", "pull.rebase", "false"]);
    d.w.commit(&d.ws, "local.txt", "l\n");
    let (_, v, _) = d.run(&[]);
    assert!(!entry(&v, "ws")["reason"]
        .as_str()
        .unwrap()
        .contains("never rebases"));
}
