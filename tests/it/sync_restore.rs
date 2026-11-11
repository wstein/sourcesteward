//! `stw sync restore [PATH] [--run ID]` (port of `internal/stwsync/restore.go`; Go cases: `restore_test.go`, the help text
//! of `cli/sync.go`). Repositories of `sync_journal::setup`: the root `ws` and the submodule `sub`, both behind origin.
use std::path::Path;

use serde_json::Value;
use stw::git_runner::{Hex40, RunId};
use stw::sync::journal::{self, Journal};

use crate::sync_journal::{repo, setup, J};

fn restore(j: &J, args: &[&str]) -> (i32, Value, String) {
    let mut a = vec!["restore"];
    a.extend_from_slice(args);
    j.w.stw(&j.ws, &a)
}

/// One run that moves both repositories; its ID.
fn run(j: &J) -> String {
    let (code, v, err) = j.w.stw(&j.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    v["run_id"].as_str().expect("a run id").to_string()
}

fn write_record(j: &J, repo_dir: &Path, id: &str, body: &str) {
    let dir = j.git_dir(repo_dir).join("stw/sync");
    std::fs::create_dir_all(&dir).unwrap();
    std::fs::write(dir.join(format!("{id}.json")), body).unwrap();
}

fn record(id: &str, entries: &str) -> String {
    format!(
        r#"{{"type":"sync","schema_version":1,"run_id":"{id}","remote":"","started":"2026-11-11T12:00:00Z","entries":[{entries}]}}"#
    )
}

fn entry(reference: &str, old: &str, new: &str, state: &str) -> String {
    format!(
        r#"{{"n":1,"ref":"{reference}","worktree":"../outside","old_oid":"{old}","planned_new_oid":"{new}","new_oid":"{new}","orig_head":"","stash_oid":"","recovery_ref":"refs/stw/recovery/sync/x/1","state":"{state}","reason":""}}"#
    )
}

#[test]
fn a_run_that_moved_two_repositories_is_restored_in_both() {
    let j = setup();
    let id = run(&j);
    assert_eq!(j.tip(&j.ws), j.ws_new);
    assert!(j.ws.join("a2.txt").exists());
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(v["command"], "sync");
    assert_eq!(v["mode"], "restore");
    assert_eq!(v["run_id"], id.as_str());
    assert!(v["error"].is_null());
    for (name, dir, old, new) in [
        ("ws", &j.ws, &j.ws_old, &j.ws_new),
        ("sub", &j.sub, &j.sub_old, &j.sub_new),
    ] {
        let r = repo(&v, name);
        assert_eq!(r["result"], "restored", "{r}");
        assert_eq!(r["branch"], "main");
        assert_eq!(r["old_oid"], new.as_str());
        assert_eq!(r["new_oid"], old.as_str());
        assert_eq!(r["via"], "git");
        assert_eq!(&j.tip(dir), old, "{name}: the branch is back");
        // the recovery ref still holds the old tip
        assert_eq!(j.recovery(dir, &id, 1).as_deref(), Some(old.as_str()));
    }
    // index and files went with the branch of the checked-out worktree
    assert!(!j.ws.join("a2.txt").exists());
    assert_eq!(
        j.w.git(&j.ws, &["status", "--porcelain", "--untracked-files=no"]),
        ""
    );
    let mut keys: Vec<&String> = v["summary"].as_object().unwrap().keys().collect();
    keys.sort();
    assert_eq!(
        keys,
        [
            "already_restored",
            "changed_since",
            "failed",
            "journal_incomplete",
            "restored",
            "total"
        ]
    );
    assert_eq!(v["summary"]["restored"], 2);
}

#[test]
fn the_text_names_the_run_and_counts_the_results() {
    let j = setup();
    let id = run(&j);
    let (code, out, err) = j.text(&["restore"]);
    assert_eq!(code, 0, "{out} {err}");
    assert!(
        out.starts_with(&format!("sync restore of run {id}\n")),
        "{out}"
    );
    assert!(out.contains("restored"), "{out}");
    assert!(
        out.trim_end().ends_with(
            "2 restored, 0 already restored, 0 changed since, 0 journal incomplete, 0 failed"
        ),
        "{out}"
    );
}

#[test]
fn a_second_restore_says_already_restored() {
    let j = setup();
    run(&j);
    restore(&j, &[]);
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(code, 0, "{v} {err}");
    for name in ["ws", "sub"] {
        assert_eq!(repo(&v, name)["result"], "already-restored", "{v}");
    }
    assert_eq!(v["summary"]["already_restored"], 2);
    assert_eq!(j.tip(&j.ws), j.ws_old);
}

#[test]
fn a_branch_with_a_new_commit_is_changed_since_and_not_touched() {
    let j = setup();
    run(&j);
    j.w.commit(&j.ws, "mine.txt", "mine\n");
    let mine = j.tip(&j.ws);
    let (code, v, _) = restore(&j, &[]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "changed-since", "{r}");
    assert_eq!(r["reason"], "the branch moved since the run");
    assert_eq!(j.tip(&j.ws), mine, "nothing was touched");
    assert!(j.ws.join("mine.txt").exists());
    // the other repository is restored all the same
    assert_eq!(repo(&v, "sub")["result"], "restored");
    assert_eq!(j.tip(&j.sub), j.sub_old);
    assert_eq!(v["summary"]["changed_since"], 1);
}

#[test]
fn the_highest_run_is_the_default_and_an_older_one_is_named_with_run() {
    let j = setup();
    let first = run(&j);
    j.news("n1");
    let after_first = j.tip(&j.ws);
    let second = run(&j);
    assert!(first < second);
    assert_ne!(j.tip(&j.ws), after_first);
    // default: the second run only
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(v["run_id"], second.as_str());
    assert_eq!(j.tip(&j.ws), after_first);
    // the first run, named
    let (code, v, err) = restore(&j, &["--run", &first]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(v["run_id"], first.as_str());
    assert_eq!(j.tip(&j.ws), j.ws_old);
    assert_eq!(j.tip(&j.sub), j.sub_old);
}

#[test]
fn an_older_run_whose_tip_was_left_by_a_later_run_is_changed_since() {
    let j = setup();
    let first = run(&j);
    j.news("n1");
    run(&j);
    let tip = j.tip(&j.ws);
    let (code, v, _) = restore(&j, &["--run", &first]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(repo(&v, "ws")["result"], "changed-since");
    assert_eq!(j.tip(&j.ws), tip);
}

#[test]
fn the_default_is_the_highest_run_id_not_the_newest_file() {
    let j = setup();
    let real = run(&j);
    // a record with a higher ID is written first in time order, the real one stays the newest file by name order of
    // creation: the ID decides
    let high = "29990101T000000000000Z-00000000";
    write_record(&j, &j.ws, high, &record(high, ""));
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(v["run_id"], high, "{v} {err}");
    assert_eq!(code, 0, "{v}");
    assert_eq!(j.tip(&j.ws), j.ws_new, "the real run was not restored");
    let _ = real;
}

#[test]
fn an_unknown_run_and_a_workspace_without_a_journal_are_refused_and_nothing_changes() {
    let j = setup();
    // no journal at all
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(code, 1, "{v} {err}");
    assert_eq!(v["error"]["code"], "sync-restore-no-run");
    assert_eq!(
        v["error"]["message"],
        "no stw sync run is journaled in this workspace"
    );
    let (code, out, err) = j.text(&["restore"]);
    assert_eq!(code, 1, "{out}");
    assert!(err.contains("sync-restore-no-run"), "{err}");
    assert!(out.is_empty(), "{out}");
    run(&j);
    // a well-formed ID that no repository has
    let other = "20200101T000000000000Z-deadbeef";
    let (code, v, _) = restore(&j, &["--run", other]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(v["error"]["code"], "sync-restore-no-run");
    assert_eq!(
        v["error"]["message"],
        format!("run {other} is not journaled in this workspace")
    );
    assert!(v["run_id"].is_null());
    assert_eq!(j.tip(&j.ws), j.ws_new, "nothing was touched");
    // a malformed ID is refused before anything is read
    for bad in [
        "../x",
        "-x",
        "latest",
        "20200101T000000000000Z-DEADBEEF",
        "",
    ] {
        let (code, v, err) = restore(&j, &["--run", bad]);
        assert!(code == 1 || code == 2, "{bad}: {code} {v} {err}");
        assert_eq!(j.tip(&j.ws), j.ws_new, "{bad}");
    }
}

#[test]
fn a_sync_option_with_restore_is_a_usage_error() {
    let j = setup();
    run(&j);
    for flags in [
        &["--plan"][..],
        &["--dry-run"][..],
        &["--apply"][..],
        &["--prune"][..],
        &["--no-prune"][..],
        &["--submodules"][..],
        &["--rules"][..],
        &["--rules-path", "x"][..],
        &["--offline"][..],
        &["--repo", "sub"][..],
    ] {
        // the option before the word `restore`, as in Go
        let mut args: Vec<&str> = flags.to_vec();
        args.push("restore");
        let (code, _, err) = j.w.stw(&j.ws, &args);
        assert_eq!(code, 2, "{flags:?}: {err}");
        assert!(
            err.contains("does not apply to stw sync restore") || err.contains("error"),
            "{flags:?}: {err}"
        );
        // and after it (a flag restore does not know)
        let (code, _, _) = j.w.stw(&j.ws, &["restore", flags[0]]);
        assert_eq!(code, 2, "{flags:?}");
        assert_eq!(j.tip(&j.ws), j.ws_new, "{flags:?}: nothing was touched");
        assert_eq!(j.tip(&j.sub), j.sub_new, "{flags:?}");
    }
    // `--run` is a restore option only
    let (code, _, _) =
        j.w.stw(&j.ws, &["--run", "20200101T000000000000Z-deadbeef"]);
    assert_eq!(code, 2);
}

#[test]
fn restore_is_given_the_workspace_as_a_path_or_as_root_but_not_both() {
    let j = setup();
    run(&j);
    let o = |args: &[&str]| {
        std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
            .env_clear()
            .env("PATH", std::env::var_os("PATH").unwrap())
            .env("HOME", &j.w.home)
            .current_dir(&j.w.base)
            .args(args)
            .output()
            .unwrap()
    };
    let ws = j.ws.to_str().unwrap();
    let both = o(&["sync", "restore", ws, "--root", ws]);
    assert_eq!(both.status.code(), Some(2));
    assert_eq!(j.tip(&j.ws), j.ws_new);
    let by_path = o(&["sync", "restore", ws, "--json"]);
    assert_eq!(
        by_path.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&by_path.stderr)
    );
    assert_eq!(j.tip(&j.ws), j.ws_old);
}

#[test]
fn a_crash_before_the_branch_moved_leaves_a_record_that_restores_as_already_restored() {
    // recovery ref and record in `prepared`, the branch never moved
    let j = setup();
    let r = stw::repo::open(&j.ws).unwrap();
    let id = journal::new_run_id();
    let mut jr = Journal::new(&r, id.clone());
    jr.prepare(
        &j.ws,
        "refs/heads/main",
        "w",
        Hex40::parse(&j.ws_old).unwrap(),
        Hex40::parse(&j.ws_new).unwrap(),
    )
    .unwrap();
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(v["run_id"], id.as_str());
    assert_eq!(repo(&v, "ws")["result"], "already-restored");
    assert_eq!(j.tip(&j.ws), j.ws_old);
    // the restore dealt with the incomplete record
    let rec = journal::load(&journal::dir_of(&r), &id).unwrap().unwrap();
    assert_eq!(rec.entries[0].state, "restored");
}

#[test]
fn a_crash_after_the_branch_moved_and_before_the_record_was_completed_is_restored() {
    // recovery ref and `prepared` record, the branch moved (the process died before `completed`)
    let j = setup();
    let r = stw::repo::open(&j.ws).unwrap();
    let id = journal::new_run_id();
    let mut jr = Journal::new(&r, id.clone());
    jr.prepare(
        &j.ws,
        "refs/heads/main",
        "w",
        Hex40::parse(&j.ws_old).unwrap(),
        Hex40::parse(&j.ws_new).unwrap(),
    )
    .unwrap();
    j.w.git(&j.ws, &["fetch", "-q", "origin"]);
    j.w.git(&j.ws, &["merge", "-q", "--ff-only", &j.ws_new]);
    assert_eq!(j.tip(&j.ws), j.ws_new);
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo(&v, "ws")["result"], "restored", "{v}");
    assert_eq!(j.tip(&j.ws), j.ws_old);
    let rec = journal::load(&journal::dir_of(&r), &id).unwrap().unwrap();
    assert_eq!(rec.entries[0].state, "restored");
}

#[test]
fn a_prepared_entry_without_a_planned_tip_is_journal_incomplete() {
    let j = setup();
    let id = "20261111T000000000000Z-0000000a";
    let e = format!(
        r#"{{"n":1,"ref":"refs/heads/main","worktree":"","old_oid":"{}","planned_new_oid":"","new_oid":"","orig_head":"","stash_oid":"","recovery_ref":"refs/stw/recovery/sync/{id}/1","state":"prepared","reason":""}}"#,
        j.ws_old
    );
    write_record(&j, &j.ws, id, &record(id, &e));
    let (code, v, _) = restore(&j, &[]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "journal-incomplete", "{r}");
    assert!(r["reason"].as_str().unwrap().contains("manual reset"));
    assert_eq!(j.tip(&j.ws), j.ws_old);
}

#[test]
fn a_manipulated_record_fails_before_git_sees_it() {
    let j = setup();
    run(&j);
    let tip = j.tip(&j.ws);
    let good_old = j.ws_old.clone();
    let id = "29990101T000000000000Z-00000001";
    let cases: Vec<(String, &str)> = vec![
        (
            entry("refs/heads/../outside", &good_old, &tip, "completed"),
            "ref with ..",
        ),
        (
            entry("refs/heads/-x", &good_old, &tip, "completed"),
            "ref that looks like an option",
        ),
        (
            entry("--upload-pack=x", &good_old, &tip, "completed"),
            "an option as ref",
        ),
        (entry("refs/tags/v1", &good_old, &tip, "completed"), "a tag"),
        (
            entry("refs/stw/recovery/sync/x/1", &good_old, &tip, "completed"),
            "a recovery ref",
        ),
        (entry("HEAD", &good_old, &tip, "completed"), "HEAD"),
        (
            entry("refs/heads/a b", &good_old, &tip, "completed"),
            "a space",
        ),
        (
            entry("refs/heads/main", "zz", &tip, "completed"),
            "old not hex",
        ),
        (
            entry(
                "refs/heads/main",
                &good_old.to_uppercase(),
                &tip,
                "completed",
            ),
            "old upper case",
        ),
        (
            entry("refs/heads/main", &good_old[..39], &tip, "completed"),
            "old short",
        ),
        (
            entry("refs/heads/main", &"a".repeat(64), &tip, "completed"),
            "old 64 digits",
        ),
        (
            entry("refs/heads/main", &good_old, "--force", "completed"),
            "new is an option",
        ),
        (
            entry(
                "refs/heads/main",
                &good_old,
                &tip.to_uppercase(),
                "completed",
            ),
            "new upper case",
        ),
    ];
    for (e, why) in cases {
        write_record(&j, &j.ws, id, &record(id, &e));
        let before: Vec<String> = j.recovery_refs(&j.ws);
        let (code, v, _) = restore(&j, &[]);
        assert_eq!(code, 1, "{why}: {v}");
        assert_eq!(v["run_id"], id, "{why}");
        let r = repo(&v, "ws");
        assert_eq!(r["result"], "failed", "{why}: {r}");
        assert_eq!(r["reason"], "bad-record", "{why}: {r}");
        assert_eq!(j.tip(&j.ws), tip, "{why}: nothing was touched");
        assert_eq!(j.recovery_refs(&j.ws), before, "{why}");
        assert!(!j.ws.parent().unwrap().join("outside").exists());
    }
    // the file is not the record of that run, not JSON, or a link
    for (body, why) in [
        ("not json".to_string(), "not JSON"),
        (
            record("29990101T000000000000Z-00000002", ""),
            "run id inside differs",
        ),
        (
            record(id, "").replace("\"sync\"", "\"tidy\""),
            "foreign type",
        ),
        (record(id, r#"{"n":"1"}"#), "entry field of the wrong type"),
    ] {
        write_record(&j, &j.ws, id, &body);
        let (code, v, _) = restore(&j, &[]);
        assert_eq!(code, 1, "{why}: {v}");
        let r = repo(&v, "ws");
        assert_eq!(r["result"], "failed", "{why}: {r}");
        assert!(
            r["reason"].as_str().unwrap().contains("cannot be used"),
            "{why}: {r}"
        );
        assert_eq!(j.tip(&j.ws), tip, "{why}");
    }
    #[cfg(unix)]
    {
        let dir = j.git_dir(&j.ws).join("stw/sync");
        let target = j.w.p("target.json");
        std::fs::write(&target, record(id, "")).unwrap();
        std::fs::remove_file(dir.join(format!("{id}.json"))).unwrap();
        std::os::unix::fs::symlink(&target, dir.join(format!("{id}.json"))).unwrap();
        let (code, v, _) = restore(&j, &[]);
        assert_eq!(code, 1, "{v}");
        assert_eq!(repo(&v, "ws")["result"], "failed", "a link as record: {v}");
    }
}

#[test]
fn a_manipulated_worktree_field_is_never_used() {
    let j = setup();
    run(&j);
    let outside = j.w.p("outside");
    std::fs::create_dir(&outside).unwrap();
    let id = "29990101T000000000000Z-00000003";
    let tip = j.tip(&j.ws);
    let e = entry("refs/heads/main", &j.ws_old, &tip, "completed")
        .replace("../outside", outside.to_str().unwrap());
    write_record(&j, &j.ws, id, &record(id, &e));
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo(&v, "ws")["result"], "restored");
    assert_eq!(repo(&v, "ws")["path"], j.ws.to_str().unwrap());
    assert_eq!(std::fs::read_dir(&outside).unwrap().count(), 0);
}

#[test]
fn local_changes_in_the_checked_out_worktree_stop_the_restore() {
    for staged in [false, true] {
        let j = setup();
        run(&j);
        std::fs::write(j.ws.join("a.txt"), "my edit\n").unwrap();
        if staged {
            j.w.git(&j.ws, &["add", "a.txt"]);
        }
        let (code, v, _) = restore(&j, &[]);
        assert_eq!(code, 1, "{v}");
        let r = repo(&v, "ws");
        assert_eq!(r["result"], "changed-since", "{r}");
        assert_eq!(r["reason"], "the worktree has local changes");
        assert_eq!(j.tip(&j.ws), j.ws_new);
        assert_eq!(
            std::fs::read_to_string(j.ws.join("a.txt")).unwrap(),
            "my edit\n"
        );
        // an untracked file does not count as a local change
        let j = setup();
        run(&j);
        std::fs::write(j.ws.join("scratch.txt"), "x").unwrap();
        let (code, v, _) = restore(&j, &[]);
        assert_eq!(code, 0, "{v}");
        assert!(j.ws.join("scratch.txt").exists());
    }
}

#[test]
fn an_operation_in_progress_stops_the_restore() {
    let j = setup();
    run(&j);
    std::fs::write(
        j.git_dir(&j.ws).join("MERGE_HEAD"),
        format!("{}\n", j.ws_old),
    )
    .unwrap();
    let (code, v, _) = restore(&j, &[]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(repo(&v, "ws")["result"], "changed-since");
    assert_eq!(j.tip(&j.ws), j.ws_new);
}

#[test]
fn a_branch_that_is_checked_out_nowhere_moves_back_by_compare_and_swap() {
    let j = setup();
    run(&j);
    // the worktree sits on another branch: only the ref moves, the files stay
    j.w.git(&j.ws, &["checkout", "-q", "-b", "other"]);
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo(&v, "ws")["result"], "restored", "{v}");
    assert_eq!(repo(&v, "ws")["via"], "git");
    assert_eq!(j.tip(&j.ws), j.ws_old);
    assert!(
        j.ws.join("a2.txt").exists(),
        "the checked-out branch's files are not touched"
    );
    assert_eq!(
        j.w.git(&j.ws, &["symbolic-ref", "HEAD"]),
        "refs/heads/other"
    );
    let log = j.w.git(&j.ws, &["reflog", "show", "-1", "refs/heads/main"]);
    assert!(log.contains("stw: sync restore"), "{log}");
}

#[test]
fn a_branch_checked_out_in_a_linked_worktree_is_left_alone() {
    let j = setup();
    run(&j);
    j.w.git(&j.ws, &["checkout", "-q", "-b", "other"]);
    let wt = j.w.p("linked");
    j.w.git(
        &j.ws,
        &["worktree", "add", "-q", wt.to_str().unwrap(), "main"],
    );
    let (code, v, _) = restore(&j, &[]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "changed-since", "{r}");
    assert_eq!(r["reason"], "the branch is checked out in another worktree");
    assert_eq!(j.tip(&j.ws), j.ws_new);
}

#[test]
fn a_deleted_branch_and_a_symbolic_branch_are_changed_since() {
    let j = setup();
    run(&j);
    j.w.git(&j.ws, &["checkout", "-q", "-b", "other"]);
    j.w.git(&j.ws, &["branch", "-q", "-D", "main"]);
    let (code, v, _) = restore(&j, &[]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(repo(&v, "ws")["reason"], "the branch no longer exists");
    assert!(
        !j.w.git_try(&j.ws, &["rev-parse", "--verify", "-q", "refs/heads/main"])
            .0
    );
    // a symbolic ref in its place
    j.w.git(
        &j.ws,
        &["symbolic-ref", "refs/heads/main", "refs/heads/other"],
    );
    let (code, v, _) = restore(&j, &[]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(repo(&v, "ws")["reason"], "the branch ref is symbolic");
}

#[test]
fn an_old_tip_that_is_gone_fails_and_nothing_moves() {
    let j = setup();
    run(&j);
    let id = "29990101T000000000000Z-00000004";
    let tip = j.tip(&j.ws);
    let e = entry("refs/heads/main", &"1".repeat(40), &tip, "completed");
    write_record(&j, &j.ws, id, &record(id, &e));
    let (code, v, _) = restore(&j, &[]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(repo(&v, "ws")["reason"], "old-object-missing");
    assert_eq!(j.tip(&j.ws), tip);
}

#[test]
fn pruned_refs_and_the_stash_are_not_undone() {
    let j = setup();
    // a remote-tracking ref that is gone on origin, and a stash entry
    j.w.git(
        &j.ws_up,
        &[
            "push",
            "-q",
            j.ws.parent().unwrap().join("a.git").to_str().unwrap(),
            "HEAD:refs/heads/gone",
        ],
    );
    j.w.git(&j.ws, &["fetch", "-q", "origin"]);
    j.w.git(
        &j.ws_up,
        &[
            "push",
            "-q",
            j.ws.parent().unwrap().join("a.git").to_str().unwrap(),
            ":refs/heads/gone",
        ],
    );
    std::fs::write(j.ws.join("a.txt"), "stashed\n").unwrap();
    j.w.git(&j.ws, &["stash", "push", "-q"]);
    let stash = j.w.git(&j.ws, &["rev-parse", "refs/stash"]);
    let (code, v, err) = j.w.stw(&j.ws, &["--prune"]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(
        repo(&v, "ws")["pruned"],
        serde_json::json!(["refs/remotes/origin/gone"])
    );
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(j.tip(&j.ws), j.ws_old);
    assert!(
        !j.w.git_try(
            &j.ws,
            &["rev-parse", "--verify", "-q", "refs/remotes/origin/gone"]
        )
        .0,
        "a pruned ref stays pruned"
    );
    assert_eq!(
        j.w.git(&j.ws, &["rev-parse", "refs/stash"]),
        stash,
        "the stash is untouched"
    );
}

#[test]
fn a_hostile_branch_name_is_escaped_in_the_text() {
    let j = setup();
    let name = "ev\u{202e}il";
    j.w.git(&j.ws, &["branch", "-m", "main", name]);
    run(&j);
    let (code, out, err) = j.text(&["restore"]);
    assert_eq!(code, 0, "{out} {err}");
    assert!(
        !out.contains('\u{202e}') && !out.contains('\u{1b}'),
        "{out:?}"
    );
    assert!(out.contains("ev\\u{202e}il"), "{out}");
}

#[test]
fn restore_without_a_run_in_a_repository_that_has_none_skips_it() {
    // only the submodule moved: the root is not part of the run and is not listed
    let j = setup();
    j.w.git(&j.ws, &["checkout", "-q", "--detach"]);
    let id = run(&j);
    let (code, v, err) = restore(&j, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(v["run_id"], id.as_str());
    assert_eq!(v["repos"].as_array().unwrap().len(), 1, "{v}");
    assert_eq!(repo(&v, "sub")["result"], "restored");
    let _ = RunId::parse(&id).unwrap();
}

#[test]
fn a_journal_directory_that_is_a_symbolic_link_is_reported() {
    #[cfg(unix)]
    {
        let j = setup();
        run(&j);
        let dir = j.git_dir(&j.ws).join("stw");
        let moved = j.w.p("moved-stw");
        std::fs::rename(&dir, &moved).unwrap();
        std::os::unix::fs::symlink(&moved, &dir).unwrap();
        let (code, v, _) = restore(&j, &[]);
        assert_eq!(code, 1, "{v}");
        let r = repo(&v, "ws");
        assert_eq!(r["result"], "failed", "{r}");
        assert!(
            r["reason"].as_str().unwrap().contains("journal directory"),
            "{r}"
        );
        assert_eq!(j.tip(&j.ws), j.ws_new);
    }
}
