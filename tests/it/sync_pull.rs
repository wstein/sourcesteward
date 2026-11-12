//! `stw sync --rebase`, `--merge`, `--autostash` and `--no-autostash` (ports of the Go flags; behavior in
//! `src/sync/pull.rs`; Go cases: `internal/stwsync/pull_test.go`, `stash_test.go`, `sync_test.go`).
//!
//! Harness confinement as in `sync_flags.rs`: every repository lives in a tempdir, git and `stw` run with a scrubbed
//! environment and `HOME` inside the tempdir.
use std::path::PathBuf;

use serde_json::Value;

use crate::sync_flags::World;

struct D {
    w: World,
    origin: PathBuf,
    ws: PathBuf,
    /// the commit the local branch stood on before `stw` ran
    local: String,
    /// the tip of `origin/main` after the fetch
    upstream: String,
}

fn repo<'a>(v: &'a Value, name: &str) -> &'a Value {
    v["repos"]
        .as_array()
        .unwrap_or_else(|| panic!("no repos in {v}"))
        .iter()
        .find(|r| r["name"] == name)
        .unwrap_or_else(|| panic!("no repository {name} in {v}"))
}

/// `origin` (bare) has `main` with `base.txt` and `shared.txt`; `ws` (a clone) has one local commit `local.txt` and
/// `origin` one new commit `remote.txt`: `main` is ahead 1 and behind 1 once `ws` has fetched.
fn diverged() -> D {
    let w = World::new();
    let seed = w.p("seed");
    std::fs::create_dir(&seed).unwrap();
    w.git(&seed, &["init", "-q"]);
    w.commit(&seed, "base.txt", "base\n");
    w.commit(&seed, "shared.txt", "one\ntwo\nthree\n");
    let origin = w.p("origin.git");
    w.git(&w.base, &["clone", "-q", "--bare", "seed", "origin.git"]);
    let ws = w.p("ws");
    w.git(&w.base, &["clone", "-q", "origin.git", "ws"]);
    // stw's own git calls read the configuration of the repository, not the `-c` pairs of the harness
    w.git(&ws, &["config", "user.name", "T"]);
    w.git(&ws, &["config", "user.email", "t@example.com"]);
    let local = w.commit(&ws, "local.txt", "local\n");
    let other = w.p("other");
    w.git(&w.base, &["clone", "-q", "origin.git", "other"]);
    w.commit(&other, "remote.txt", "remote\n");
    w.git(&other, &["push", "-q", "origin", "HEAD:main"]);
    let upstream = w.git(&origin, &["rev-parse", "main"]);
    D {
        w,
        origin,
        ws,
        local,
        upstream,
    }
}

fn head(d: &D) -> String {
    d.w.git(&d.ws, &["rev-parse", "HEAD"])
}

fn parents(d: &D, rev: &str) -> usize {
    d.w.git(&d.ws, &["rev-list", "--parents", "-n", "1", rev])
        .split_whitespace()
        .count()
        - 1
}

fn is_ancestor(d: &D, a: &str, b: &str) -> bool {
    d.w.git_try(&d.ws, &["merge-base", "--is-ancestor", a, b]).0
}

#[test]
fn rebase_puts_the_local_commit_on_top_of_the_fetched_upstream() {
    let d = diverged();
    let (code, v, err) = d.w.stw(&d.ws, &["--rebase"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "rebased", "{r}");
    assert_eq!(v["summary"]["rebased"], 1);
    assert_eq!(v["options"]["rebase"], true);
    assert_eq!(v["options"]["merge"], false);
    assert!(v["options"]["autostash"].is_null());
    assert!(is_ancestor(&d, &d.upstream, "HEAD"));
    assert_ne!(head(&d), d.local, "the local commit was rewritten");
    assert_eq!(parents(&d, "HEAD"), 1, "no merge commit");
    assert_eq!(
        d.w.git(&d.ws, &["log", "-1", "--format=%s"]),
        "local.txt: local"
    );
    assert!(d.ws.join("remote.txt").is_file() && d.ws.join("local.txt").is_file());
    // the journal names the run and the entry
    let j = &r["journal"];
    assert_eq!(j["state"], "completed", "{r}");
    assert!(j["recovery_ref"]
        .as_str()
        .unwrap()
        .starts_with("refs/stw/recovery/sync/"));
    assert_eq!(
        d.w.git(&d.ws, &["rev-parse", j["recovery_ref"].as_str().unwrap()]),
        d.local,
        "the recovery ref holds the old tip"
    );
}

#[test]
fn merge_makes_a_merge_commit_of_the_local_branch_and_the_upstream() {
    let d = diverged();
    let (code, v, err) = d.w.stw(&d.ws, &["--merge"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "merged", "{r}");
    assert_eq!(v["summary"]["merged"], 1);
    assert_eq!(v["options"]["merge"], true);
    assert_eq!(parents(&d, "HEAD"), 2, "a merge commit");
    assert!(is_ancestor(&d, &d.upstream, "HEAD"));
    assert!(
        is_ancestor(&d, &d.local, "HEAD"),
        "the local history is kept"
    );
}

#[test]
fn a_branch_that_can_be_fast_forwarded_is_fast_forwarded_even_with_merge_and_rebase() {
    for flag in ["--merge", "--rebase"] {
        let w = World::new();
        let seed = w.p("seed");
        std::fs::create_dir(&seed).unwrap();
        w.git(&seed, &["init", "-q"]);
        w.commit(&seed, "a.txt", "a\n");
        w.git(&w.base, &["clone", "-q", "--bare", "seed", "origin.git"]);
        w.git(&w.base, &["clone", "-q", "origin.git", "ws"]);
        w.commit(&seed, "b.txt", "b\n");
        w.git(
            &seed,
            &[
                "push",
                "-q",
                w.p("origin.git").to_str().unwrap(),
                "HEAD:main",
            ],
        );
        let ws = w.p("ws");
        let (code, v, err) = w.stw(&ws, &[flag]);
        assert_eq!(code, 0, "{v} {err}");
        assert_eq!(repo(&v, "ws")["result"], "fast-forwarded", "{flag}");
        assert_eq!(
            w.git(&ws, &["rev-list", "--merges", "--count", "HEAD"]),
            "0"
        );
    }
}

#[test]
fn without_a_flag_a_diverged_branch_stays_and_the_configuration_is_not_followed() {
    let d = diverged();
    d.w.git(&d.ws, &["config", "pull.rebase", "true"]);
    let (code, v, err) = d.w.stw(&d.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "skip", "{r}");
    let reason = r["reason"].as_str().unwrap();
    assert!(
        reason.starts_with("diverged: ahead 1, behind 1"),
        "{reason}"
    );
    assert!(reason.contains("use --rebase"), "{reason}");
    assert_eq!(head(&d), d.local);
}

#[test]
fn a_plan_with_rebase_or_merge_changes_nothing_and_says_what_it_would_do() {
    let d = diverged();
    d.w.git(&d.ws, &["fetch", "-q", "origin"]);
    let (code, v, err) = d.w.stw(&d.ws, &["--rebase", "--plan"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "rebase", "{r}");
    assert!(r["reason"]
        .as_str()
        .unwrap()
        .contains("would rebase 1 local commit(s) onto 1 new"));
    assert_eq!(v["summary"]["would_rebase"], 1);
    let (_, v, _) = d.w.stw(&d.ws, &["--merge", "--plan"]);
    assert_eq!(repo(&v, "ws")["result"], "merge");
    assert_eq!(v["summary"]["would_merge"], 1);
    assert_eq!(head(&d), d.local);
    assert!(!d
        .w
        .git(&d.ws, &["for-each-ref", "refs/stw"])
        .contains("refs/stw"));
}

#[test]
fn local_changes_of_tracked_files_refuse_a_rebase_or_merge_without_autostash() {
    for (flag, verb) in [("--rebase", "rebase"), ("--merge", "merge")] {
        let d = diverged();
        std::fs::write(d.ws.join("base.txt"), "edited\n").unwrap();
        let (code, v, err) = d.w.stw(&d.ws, &[flag]);
        assert_eq!(code, 0, "{v} {err}");
        let r = repo(&v, "ws");
        assert_eq!(r["result"], "skip", "{r}");
        assert_eq!(
            r["reason"],
            format!("cannot {verb} with local changes (use --autostash)")
        );
        assert_eq!(head(&d), d.local);
        assert_eq!(
            std::fs::read_to_string(d.ws.join("base.txt")).unwrap(),
            "edited\n"
        );
        assert!(r.get("journal").is_none(), "nothing was journaled: {r}");
    }
}

#[test]
fn an_untracked_file_is_no_local_change() {
    let d = diverged();
    std::fs::write(d.ws.join("scratch.txt"), "x\n").unwrap();
    let (_, v, _) = d.w.stw(&d.ws, &["--rebase"]);
    assert_eq!(repo(&v, "ws")["result"], "rebased");
    assert!(d.ws.join("scratch.txt").is_file());
}

#[test]
fn autostash_stashes_the_change_and_puts_it_back() {
    for flag in ["--rebase", "--merge"] {
        let d = diverged();
        std::fs::write(d.ws.join("base.txt"), "edited\n").unwrap();
        let (code, v, err) = d.w.stw(&d.ws, &[flag, "--autostash"]);
        assert_eq!(code, 0, "{v} {err}");
        let r = repo(&v, "ws");
        assert!(
            matches!(r["result"].as_str(), Some("rebased" | "merged")),
            "{r}"
        );
        assert_eq!(v["options"]["autostash"], true);
        assert_eq!(
            std::fs::read_to_string(d.ws.join("base.txt")).unwrap(),
            "edited\n"
        );
        assert!(
            d.w.git(&d.ws, &["stash", "list"]).is_empty(),
            "the stash is empty again"
        );
    }
}

#[test]
fn the_stash_flags_win_over_the_configuration_in_both_directions() {
    // --autostash wins over rebase.autoStash=false
    let d = diverged();
    d.w.git(&d.ws, &["config", "rebase.autoStash", "false"]);
    std::fs::write(d.ws.join("base.txt"), "edited\n").unwrap();
    let (_, v, _) = d.w.stw(&d.ws, &["--rebase", "--autostash"]);
    assert_eq!(repo(&v, "ws")["result"], "rebased");
    // --no-autostash wins over rebase.autoStash=true
    let d = diverged();
    d.w.git(&d.ws, &["config", "rebase.autoStash", "true"]);
    std::fs::write(d.ws.join("base.txt"), "edited\n").unwrap();
    let (_, v, _) = d.w.stw(&d.ws, &["--rebase", "--no-autostash"]);
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "skip", "{r}");
    assert_eq!(v["options"]["autostash"], false);
    // the configuration alone does not stash: the argument vector of git is explicit
    let d = diverged();
    d.w.git(&d.ws, &["config", "rebase.autoStash", "true"]);
    std::fs::write(d.ws.join("base.txt"), "edited\n").unwrap();
    let (_, v, _) = d.w.stw(&d.ws, &["--rebase"]);
    assert_eq!(repo(&v, "ws")["result"], "skip");
    assert!(v["options"]["autostash"].is_null());
}

/// `origin` and `ws` both change line 2 of `shared.txt`.
fn conflicting() -> D {
    let mut d = diverged();
    let other = d.w.p("other");
    d.w.git(&other, &["pull", "-q", "--ff-only", "origin", "main"]);
    std::fs::write(other.join("shared.txt"), "one\nREMOTE\nthree\n").unwrap();
    d.w.git(&other, &["commit", "-q", "-a", "-m", "remote edit"]);
    d.w.git(&other, &["push", "-q", "origin", "HEAD:main"]);
    std::fs::write(d.ws.join("shared.txt"), "one\nLOCAL\nthree\n").unwrap();
    d.w.git(&d.ws, &["commit", "-q", "-a", "-m", "local edit"]);
    d.local = head(&d);
    d.upstream = d.w.git(&d.origin, &["rev-parse", "main"]);
    d
}

#[test]
fn a_rebase_conflict_is_left_in_progress_and_exits_one() {
    let d = conflicting();
    let (code, v, err) = d.w.stw(&d.ws, &["--rebase"]);
    assert_eq!(code, 1, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "conflict", "{r}");
    assert!(r["reason"]
        .as_str()
        .unwrap()
        .contains("git rebase --continue"));
    assert!(r["detail"].as_str().is_some_and(|t| !t.is_empty()), "{r}");
    assert_eq!(v["summary"]["conflicts"], 1);
    assert!(d.ws.join(".git/rebase-merge").is_dir() || d.ws.join(".git/rebase-apply").is_dir());
    assert_eq!(r["journal"]["state"], "failed");
    // the owner finishes it the way git says
    d.w.git(&d.ws, &["rebase", "--abort"]);
    assert_eq!(head(&d), d.local);
}

#[test]
fn a_merge_conflict_is_left_in_progress_and_exits_one() {
    let d = conflicting();
    let (code, v, err) = d.w.stw(&d.ws, &["--merge"]);
    assert_eq!(code, 1, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "conflict", "{r}");
    assert!(r["reason"].as_str().unwrap().contains("git merge --abort"));
    assert!(d.ws.join(".git/MERGE_HEAD").is_file());
    d.w.git(&d.ws, &["merge", "--abort"]);
    assert_eq!(head(&d), d.local);
}

#[test]
fn a_conflict_while_the_stash_is_put_back_is_an_autostash_conflict() {
    let d = diverged();
    let other = d.w.p("other");
    d.w.git(&other, &["pull", "-q", "--ff-only", "origin", "main"]);
    std::fs::write(other.join("shared.txt"), "one\nREMOTE\nthree\n").unwrap();
    d.w.git(&other, &["commit", "-q", "-a", "-m", "remote edit"]);
    d.w.git(&other, &["push", "-q", "origin", "HEAD:main"]);
    std::fs::write(d.ws.join("shared.txt"), "one\nLOCAL\nthree\n").unwrap();
    let (code, v, err) = d.w.stw(&d.ws, &["--rebase", "--autostash"]);
    assert_eq!(code, 1, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "autostash-conflict", "{r}");
    assert!(r["reason"].as_str().unwrap().contains("git stash pop"));
    assert!(
        !d.w.git(&d.ws, &["stash", "list"]).is_empty(),
        "the stash is kept"
    );
    assert!(is_ancestor(&d, &d.upstream_after(), "HEAD"));
}

impl D {
    fn upstream_after(&self) -> String {
        self.w.git(&self.origin, &["rev-parse", "main"])
    }
}

#[test]
fn sync_restore_moves_a_rebased_branch_back() {
    let d = diverged();
    let (_, v, _) = d.w.stw(&d.ws, &["--rebase"]);
    assert_eq!(repo(&v, "ws")["result"], "rebased");
    assert_ne!(head(&d), d.local);
    let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &d.w.home)
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .current_dir(&d.w.base)
        .args(["sync", "restore", "--json", "--root"])
        .arg(&d.ws)
        .output()
        .unwrap();
    assert_eq!(
        o.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&o.stderr)
    );
    assert_eq!(head(&d), d.local, "restore moved the branch to the old tip");
}

#[test]
fn no_hook_of_the_repository_runs() {
    for flag in ["--rebase", "--merge"] {
        let d2 = diverged();
        let hooks = d2.ws.join(".git/hooks");
        std::fs::create_dir_all(&hooks).unwrap();
        let marker = d2.w.p("hook-ran");
        for h in [
            "pre-rebase",
            "post-rewrite",
            "post-merge",
            "pre-merge-commit",
            "commit-msg",
        ] {
            let p = hooks.join(h);
            std::fs::write(
                &p,
                format!("#!/bin/sh\necho {h} >> \"{}\"\n", marker.display()),
            )
            .unwrap();
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
        }
        let (_, v, _) = d2.w.stw(&d2.ws, &[flag]);
        assert!(
            matches!(
                repo(&v, "ws")["result"].as_str(),
                Some("rebased" | "merged")
            ),
            "{v}"
        );
        assert!(!marker.exists(), "{flag}: a hook ran");
    }
}

#[test]
fn a_merge_driver_in_the_repository_config_refuses_the_run() {
    let d = diverged();
    d.w.git(&d.ws, &["config", "merge.mine.driver", "true %O %A %B"]);
    let (code, v, _) = d.w.stw(&d.ws, &["--merge"]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "error", "{r}");
    assert_eq!(head(&d), d.local);
}

#[test]
fn a_filter_driver_in_the_repository_config_refuses_the_run() {
    let d = diverged();
    d.w.git(&d.ws, &["config", "filter.x.smudge", "cat"]);
    let (code, v, _) = d.w.stw(&d.ws, &["--rebase"]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(repo(&v, "ws")["result"], "error");
    assert_eq!(head(&d), d.local);
}

#[test]
fn the_flags_exclude_each_other_and_are_refused_with_restore() {
    let d = diverged();
    for args in [
        vec!["--rebase", "--merge"],
        vec!["--autostash", "--no-autostash"],
    ] {
        let (code, _, err) = d.w.stw(&d.ws, &args);
        assert_eq!(code, 2, "{args:?} {err}");
    }
    for flag in ["--rebase", "--merge", "--autostash", "--no-autostash"] {
        let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
            .env_clear()
            .env("PATH", std::env::var_os("PATH").unwrap())
            .env("HOME", &d.w.home)
            .current_dir(&d.w.base)
            .args(["sync", flag, "restore", "--root"])
            .arg(&d.ws)
            .output()
            .unwrap();
        assert_eq!(o.status.code(), Some(2), "{flag}");
        assert!(String::from_utf8_lossy(&o.stderr)
            .contains(&format!("{flag} does not apply to stw sync restore")));
    }
    assert_eq!(head(&d), d.local);
}

#[test]
fn the_identity_flags_need_rules_and_a_well_formed_identity() {
    let d = diverged();
    let (code, _, err) =
        d.w.stw(&d.ws, &["--owner-identity", "Eve <eve@example.org>"]);
    assert_eq!(code, 2, "{err}");
    assert!(err.contains("apply to --rules only"), "{err}");
    let (code, _, err) =
        d.w.stw(&d.ws, &["--allowed-committer", "Eve <eve@example.org>"]);
    assert_eq!(code, 2, "{err}");
    let (code, _, err) =
        d.w.stw(&d.ws, &["--rules", "--owner-identity", "no brackets"]);
    assert_eq!(code, 2, "{err}");
    assert!(err.contains("--owner-identity"), "{err}");
    let (code, _, err) = d.w.stw(&d.ws, &["--rules", "--allowed-committer", "<a@b>"]);
    assert_eq!(code, 2, "{err}");
    assert!(err.contains("--allowed-committer"), "{err}");
}

#[test]
fn a_stack_branch_that_a_rebase_does_not_own_stays_where_it_is() {
    // `--no-update-refs`: a local branch at the old tip is not moved by the rebase of `main`
    let d = diverged();
    // the configuration asks git to move the branches below the rebased one; stw pins it off
    d.w.git(&d.ws, &["config", "rebase.updateRefs", "true"]);
    d.w.git(&d.ws, &["branch", "stack", "HEAD"]);
    let (_, v, _) = d.w.stw(&d.ws, &["--rebase"]);
    assert_eq!(repo(&v, "ws")["result"], "rebased");
    assert_eq!(d.w.git(&d.ws, &["rev-parse", "stack"]), d.local);
}

// ---------------------------------------------------------------------------------------------------------
// what the configuration of the repository cannot change
// ---------------------------------------------------------------------------------------------------------

fn executable(path: &std::path::Path, text: &str) {
    use std::os::unix::fs::PermissionsExt;
    std::fs::write(path, text).unwrap();
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o755)).unwrap();
}

#[test]
fn a_configured_gpg_program_is_not_started_by_a_merge() {
    let d = diverged();
    // the new upstream commit carries a (forged) signature header: `merge.verifySignatures` would start gpg
    let other = d.w.p("other");
    let raw = d.w.git(&other, &["cat-file", "commit", "HEAD"]);
    let (head, msg) = raw.split_once("\n\n").unwrap();
    let forged = format!(
        "{head}\ngpgsig -----BEGIN PGP SIGNATURE-----\n \n forged\n -----END PGP SIGNATURE-----\n\n{msg}\n"
    );
    let sink = d.w.p("forged-commit");
    std::fs::write(&sink, forged).unwrap();
    let id = d.w.git(
        &other,
        &[
            "hash-object",
            "-t",
            "commit",
            "-w",
            "--",
            sink.to_str().unwrap(),
        ],
    );
    d.w.git(
        &other,
        &[
            "push",
            "-q",
            "-f",
            "origin",
            &format!("{id}:refs/heads/main"),
        ],
    );
    let marker = d.w.p("gpg-ran");
    let gpg = d.w.p("fake-gpg");
    executable(
        &gpg,
        &format!("#!/bin/sh\necho ran >> \"{}\"\nexit 1\n", marker.display()),
    );
    d.w.git(&d.ws, &["config", "merge.verifySignatures", "true"]);
    d.w.git(&d.ws, &["config", "gpg.program", gpg.to_str().unwrap()]);
    let (code, v, err) = d.w.stw(&d.ws, &["--merge"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo(&v, "ws")["result"], "merged", "{v}");
    assert!(!marker.exists(), "the configured gpg program ran");
}

#[test]
fn a_configured_merge_option_set_of_the_branch_refuses_a_merge() {
    let d = diverged();
    d.w.git(&d.ws, &["config", "branch.main.mergeoptions", "-s ours"]);
    let (code, v, _) = d.w.stw(&d.ws, &["--merge"]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "error", "{r}");
    assert!(
        r["error"]
            .as_str()
            .unwrap()
            .contains("branch.main.mergeoptions is set"),
        "{r}"
    );
    assert_eq!(head(&d), d.local, "nothing changed");
    assert!(r.get("journal").is_none(), "nothing was journaled: {r}");
    // a rebase does not read the key
    let (_, v, _) = d.w.stw(&d.ws, &["--rebase"]);
    assert_eq!(repo(&v, "ws")["result"], "rebased");
}

#[test]
fn a_rebase_flattens_a_local_merge_commit_whatever_rebase_rebase_merges_says() {
    let d = diverged();
    d.w.git(&d.ws, &["checkout", "-q", "-b", "side"]);
    d.w.commit(&d.ws, "side.txt", "side\n");
    d.w.git(&d.ws, &["checkout", "-q", "main"]);
    d.w.git(
        &d.ws,
        &["merge", "-q", "--no-ff", "-m", "local merge", "side"],
    );
    d.w.git(&d.ws, &["config", "rebase.rebaseMerges", "true"]);
    assert_eq!(
        d.w.git(&d.ws, &["rev-list", "--merges", "--count", "main"]),
        "1"
    );
    let (code, v, err) = d.w.stw(&d.ws, &["--rebase"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo(&v, "ws")["result"], "rebased", "{v}");
    assert_eq!(
        d.w.git(&d.ws, &["rev-list", "--merges", "--count", "main"]),
        "0",
        "the merge commit was kept although stw asked for a linear rebase"
    );
}

#[test]
fn a_changed_submodule_is_no_local_change_for_the_gate() {
    let d = diverged();
    let subsrc = d.w.p("subsrc");
    std::fs::create_dir(&subsrc).unwrap();
    d.w.git(&subsrc, &["init", "-q"]);
    d.w.commit(&subsrc, "s.txt", "s\n");
    d.w.git(
        &d.ws,
        &["submodule", "add", "-q", subsrc.to_str().unwrap(), "sub"],
    );
    d.w.git(&d.ws, &["commit", "-q", "-m", "add sub"]);
    // the submodule moves on: the superproject shows a modified gitlink
    d.w.git(
        &d.ws.join("sub"),
        &["commit", "-q", "--allow-empty", "-m", "moved"],
    );
    assert!(d.w.git(&d.ws, &["status", "--porcelain"]).contains("M sub"));
    // the plan (it does not fetch) says the same as the run
    d.w.git(&d.ws, &["fetch", "-q", "origin"]);
    let (_, v, _) = d.w.stw(&d.ws, &["--rebase", "--plan"]);
    assert_eq!(repo(&v, "ws")["result"], "rebase", "{v}");
    let (code, v, err) = d.w.stw(&d.ws, &["--rebase"]);
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "rebased", "{r}");
    assert!(is_ancestor(&d, &d.upstream, "HEAD"));
}

// ---------------------------------------------------------------------------------------------------------
// a git that answers in a way the real one rarely does (a wrapper first in PATH), and the retention
// ---------------------------------------------------------------------------------------------------------

/// `stw sync` with a `git` wrapper first in `PATH`: `rebase` and `merge` run `on_update` (shell text, `$REAL` is the real
/// git, `$2` the repository), every other call goes to the real git.
fn stw_with_git(d: &D, on_update: &str, args: &[&str]) -> (i32, Value, String) {
    let real = String::from_utf8(
        std::process::Command::new("sh")
            .args(["-c", "command -v git"])
            .output()
            .unwrap()
            .stdout,
    )
    .unwrap();
    let dir = d.w.p("fakebin");
    std::fs::create_dir_all(&dir).unwrap();
    executable(
        &dir.join("git"),
        &format!(
            "#!/bin/sh\nREAL={}\ncase \" $* \" in\n  *\" rebase \"*|*\" merge \"*)\n{on_update}\n    ;;\n  *) exec \"$REAL\" \"$@\" ;;\nesac\n",
            real.trim()
        ),
    );
    let path = format!("{}:{}", dir.display(), std::env::var("PATH").unwrap());
    let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", path)
        .env("HOME", &d.w.home)
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .current_dir(&d.w.base)
        .arg("sync")
        .args(args)
        .args(["--json", "--root"])
        .arg(&d.ws)
        .output()
        .unwrap();
    (
        o.status.code().unwrap_or(-1),
        serde_json::from_slice(&o.stdout).unwrap_or(Value::Null),
        String::from_utf8_lossy(&o.stderr).into_owned(),
    )
}

#[test]
fn a_result_that_does_not_contain_the_upstream_tip_is_raced() {
    let d = diverged();
    // git "succeeds" but the branch got a new commit that is not on top of the upstream
    let (code, v, err) = stw_with_git(
        &d,
        "    \"$REAL\" -C \"$2\" -c user.name=T -c user.email=t@example.com commit -q --allow-empty -m x\n    exit 0",
        &["--rebase"],
    );
    assert_eq!(code, 1, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "error", "{r}");
    assert_eq!(r["error"], "raced", "{r}");
    assert_eq!(r["journal"]["state"], "failed");
    // and git that changes nothing at all is raced too
    let d = diverged();
    let (code, v, _) = stw_with_git(&d, "    exit 0", &["--rebase"]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(repo(&v, "ws")["error"], "raced");
}

#[test]
fn a_refusal_for_local_changes_is_a_skip_and_any_other_failure_is_an_error() {
    let d = diverged();
    let (code, v, err) = stw_with_git(
        &d,
        "    echo 'error: cannot rebase: You have unstaged changes.' >&2\n    exit 1",
        &["--rebase"],
    );
    assert_eq!(code, 0, "{v} {err}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "skip", "{r}");
    assert!(
        r["reason"]
            .as_str()
            .unwrap()
            .starts_with("git refused: error: cannot rebase"),
        "{r}"
    );
    assert_eq!(r["journal"]["state"], "failed");
    let d = diverged();
    let (code, v, _) = stw_with_git(
        &d,
        "    echo 'fatal: boom' >&2\n    exit 128",
        &["--rebase"],
    );
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "error", "{r}");
    assert!(
        r["error"]
            .as_str()
            .unwrap()
            .starts_with("git-failed: fatal: boom"),
        "{r}"
    );
}

#[test]
fn the_journal_keeps_the_ten_latest_runs_after_rebases_too() {
    let d = diverged();
    let other = d.w.p("other");
    for k in 0..11 {
        let (code, v, err) = d.w.stw(&d.ws, &["--rebase"]);
        assert_eq!(code, 0, "run {k}: {v} {err}");
        assert_eq!(repo(&v, "ws")["result"], "rebased", "run {k}");
        // the next divergence
        d.w.commit(&d.ws, &format!("l{k}.txt"), "l\n");
        d.w.commit(&other, &format!("r{k}.txt"), "r\n");
        d.w.git(&other, &["push", "-q", "origin", "HEAD:main"]);
    }
    let dir = d.ws.join(".git/stw/sync");
    let n = std::fs::read_dir(&dir)
        .unwrap()
        .filter(|e| {
            e.as_ref()
                .unwrap()
                .file_name()
                .to_string_lossy()
                .ends_with(".json")
        })
        .count();
    assert_eq!(n, 10, "records in {}", dir.display());
}
