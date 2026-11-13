//! `stw oplog undo`: backup refs, counter ref, compare-and-swap, retention, reflog entries and `refs.log` lines
//! (docs/oplog-format.md, 8). Fixtures are built with gix only; the binary runs with a scrubbed environment and no git.
use std::process::Command;

use crate::common::*;
use gix::ObjectId;
use serde_json::Value;
use stw::oplog::undo::{self, COUNTER_REF};
use stw::oplog::Ctx;

const Z: &str = "0000000000000000000000000000000000000000";

struct Run {
    code: i32,
    text: String,
    err: String,
}

fn run(fx: &Fx, args: &[&str]) -> Run {
    assert!(fx.path.is_absolute(), "unconfined");
    let home = tempfile::tempdir().unwrap();
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", "/nonexistent")
        .env("HOME", home.path())
        .current_dir(&fx.path)
        .arg("oplog")
        .args(args)
        .output()
        .unwrap();
    Run {
        code: o.status.code().unwrap_or(-1),
        text: String::from_utf8_lossy(&o.stdout).into_owned(),
        err: String::from_utf8_lossy(&o.stderr).into_owned(),
    }
}

fn json(fx: &Fx, args: &[&str]) -> Value {
    let mut a: Vec<&str> = args.to_vec();
    a.push("--json");
    let r = run(fx, &a);
    serde_json::from_str(&r.text)
        .unwrap_or_else(|e| panic!("not JSON ({e}): {}\nstderr: {}", r.text, r.err))
}

/// Branch `work` at c3 (reflog: created at c1, then c2, c3); HEAD on `main` at c1.
fn undo_fx() -> (Fx, [ObjectId; 3]) {
    undo_fx_named("work")
}

fn undo_fx_named(name: &str) -> (Fx, [ObjectId; 3]) {
    let fx = Fx::new();
    let a = fx.commit(None, &[("f", Some("a\n"))], "first");
    let b = fx.commit(Some(a), &[("f", Some("b\n"))], "second");
    let c = fx.commit(Some(b), &[("f", Some("c\n"))], "third");
    fx.branch("main", a);
    fx.set_head("main");
    fx.branch(name, c);
    let rows = [
        (Z.to_string(), a, "branch: Created from HEAD"),
        (a.to_string(), b, "commit: second"),
        (b.to_string(), c, "commit: third"),
    ];
    let mut s = String::new();
    for (i, (o, n, m)) in rows.iter().enumerate() {
        s += &format!(
            "{o} {n} T <t@example.com> {} +0000\t{m}\n",
            1_700_000_000 + 100 * i as i64
        );
    }
    fx.write(&format!(".git/logs/refs/heads/{name}"), &s);
    (fx, [a, b, c])
}

fn undo_args<'a>(to: &'a str, expect: &'a str) -> Vec<&'a str> {
    vec!["undo", "work", "--to", to, "--expect", expect, "--yes"]
}

fn refname_value(fx: &Fx, name: &str) -> Option<ObjectId> {
    fx.repo()
        .try_find_reference(name)
        .unwrap()
        .map(|mut r| r.peel_to_id().unwrap().detach())
}

fn refs_log(fx: &Fx) -> Vec<Vec<String>> {
    std::fs::read_to_string(fx.path.join(".git/stw/oplog/refs.log"))
        .unwrap_or_default()
        .lines()
        .map(|l| l.split('\t').map(str::to_string).collect())
        .collect()
}

fn reflog_lines(fx: &Fx, name: &str) -> Vec<String> {
    std::fs::read_to_string(fx.path.join(".git/logs").join(name))
        .unwrap_or_default()
        .lines()
        .map(str::to_string)
        .collect()
}

fn counter_text(fx: &Fx) -> String {
    let id = refname_value(fx, COUNTER_REF).expect("counter ref");
    String::from_utf8(fx.repo().find_object(id).unwrap().detach().data).unwrap()
}

/// Everything under refs/stw: the names and values.
fn stw_refs(fx: &Fx) -> Vec<(String, String)> {
    let repo = fx.repo();
    let mut v: Vec<(String, String)> = repo
        .references()
        .unwrap()
        .prefixed("refs/stw/")
        .unwrap()
        .flatten()
        .map(|mut r| {
            (
                r.name().as_bstr().to_string(),
                r.peel_to_id().unwrap().to_string(),
            )
        })
        .collect();
    v.sort();
    v
}

#[test]
fn undo_moves_the_branch_and_writes_backup_counter_and_log_lines() {
    let (fx, [_, c2, c3]) = undo_fx();
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(fx.tip("work"), c2);
    assert_eq!(refname_value(&fx, "refs/stw/backup/1/1"), Some(c3));
    assert!(counter_text(&fx).starts_with("1 "), "{}", counter_text(&fx));
    assert!(r.text.contains("refs/stw/backup/1/1"), "{}", r.text);
    assert!(
        r.text.contains(&format!(
            "way back:   stw oplog undo refs/heads/work --to {c3} --expect {c2} --yes"
        )),
        "{}",
        r.text
    );
    // refs.log: counter, backup, undo update; origin stw
    let l = refs_log(&fx);
    let find = |name: &str| {
        l.iter()
            .find(|f| f[3] == name)
            .unwrap_or_else(|| panic!("no line for {name}: {l:?}"))
    };
    let counter = find(COUNTER_REF);
    assert_eq!((counter[1].as_str(), counter[4].as_str()), (Z, "stw"));
    let backup = find("refs/stw/backup/1/1");
    assert_eq!(
        (
            backup[1].as_str(),
            backup[2].to_string(),
            backup[4].as_str()
        ),
        (Z, c3.to_string(), "stw")
    );
    let upd = find("refs/heads/work");
    assert_eq!(
        (upd[1].to_string(), upd[2].to_string(), upd[4].as_str()),
        (c3.to_string(), c2.to_string(), "stw")
    );
    assert_eq!(l.len(), 3, "{l:?}");
    // the way back works and takes run 2
    let back = run(
        &fx,
        &[
            "undo",
            "refs/heads/work",
            "--to",
            &c3.to_string(),
            "--expect",
            &c2.to_string(),
            "--yes",
        ],
    );
    assert_eq!(back.code, 0, "{}{}", back.text, back.err);
    assert_eq!(fx.tip("work"), c3);
    assert_eq!(refname_value(&fx, "refs/stw/backup/2/1"), Some(c2));
}

#[test]
fn json_names_backup_run_and_way_back() {
    let (fx, [_, c2, c3]) = undo_fx();
    let v = json(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(v["schema_version"], 1);
    assert_eq!(v["command"], "oplog undo");
    assert_eq!(v["ref"], "refs/heads/work");
    assert_eq!(v["executed"], true);
    assert_eq!(v["previous"], c3.to_string());
    assert_eq!(v["current"], c2.to_string());
    assert_eq!(v["backup_ref"], "refs/stw/backup/1/1");
    assert_eq!(v["run"], 1);
    assert_eq!(v["pruned"].as_array().unwrap().len(), 0);
    assert!(v["way_back"]
        .as_str()
        .unwrap()
        .starts_with("stw oplog undo refs/heads/work --to "));
    // a refusal prints the plan document with the command `oplog undo`
    let v = json(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(v["executed"], false, "{v}");
    assert_eq!(v["command"], "oplog undo");
}

#[test]
fn the_reflog_entries_are_written_with_log_all_ref_updates_false() {
    let (fx, [_, c2, c3]) = undo_fx();
    use std::io::Write;
    let mut cfg = std::fs::OpenOptions::new()
        .append(true)
        .open(fx.path.join(".git/config"))
        .unwrap();
    write!(cfg, "[core]\n\tlogAllRefUpdates = false\n").unwrap();
    drop(cfg);
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    let b = reflog_lines(&fx, "refs/stw/backup/1/1");
    assert_eq!(b.len(), 1, "{b:?}");
    assert!(
        b[0].starts_with(&format!("{Z} {c3} ")) && b[0].ends_with("\tstw oplog undo"),
        "{b:?}"
    );
    let w = reflog_lines(&fx, "refs/heads/work");
    assert_eq!(w.len(), 4, "{w:?}");
    assert!(
        w[3].starts_with(&format!("{c3} {c2} ")) && w[3].ends_with("\tstw oplog undo"),
        "{w:?}"
    );
    // the repository's own config is unchanged by it
    assert!(std::fs::read_to_string(fx.path.join(".git/config"))
        .unwrap()
        .ends_with("logAllRefUpdates = false\n"));
}

#[test]
fn a_wrong_expect_changes_nothing() {
    let (fx, [_, c2, c3]) = undo_fx();
    let before = tree_snapshot(&fx.path);
    // a full value that is not the current one
    let r = run(&fx, &undo_args(&c2.to_string(), &c2.to_string()));
    assert_eq!(r.code, 1, "{}{}", r.text, r.err);
    assert!(r.text.contains("--expect"), "{}", r.text);
    assert!(before == tree_snapshot(&fx.path), "the repository changed");
    assert_eq!(fx.tip("work"), c3);
    // an abbreviated value is a usage error
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()[..10]));
    assert_eq!(r.code, 2, "{}{}", r.text, r.err);
    assert!(before == tree_snapshot(&fx.path));
}

#[test]
fn expect_and_yes_and_to_are_required_and_without_yes_nothing_is_written() {
    let (fx, [_, c2, c3]) = undo_fx();
    let before = tree_snapshot(&fx.path);
    let (c2, c3) = (c2.to_string(), c3.to_string());
    assert_eq!(
        run(&fx, &["undo", "work", "--to", &c2, "--yes"]).code,
        2,
        "no --expect"
    );
    assert_eq!(
        run(&fx, &["undo", "work", "--expect", &c3, "--yes"]).code,
        2,
        "no --to"
    );
    let r = run(&fx, &["undo", "work", "--to", &c2, "--expect", &c3]);
    assert_eq!(r.code, 2, "no --yes: {}{}", r.text, r.err);
    assert!(r.err.contains("--yes"), "{}", r.err);
    assert!(before == tree_snapshot(&fx.path), "a dry run wrote");
    // undo-plan writes nothing and names the command that applies it
    let p = run(&fx, &["undo-plan", "work"]);
    assert_eq!(p.code, 0, "{}", p.text);
    assert!(
        p.text.contains(&format!(
            "Apply with: stw oplog undo refs/heads/work --to {c2} --expect {c3} --yes"
        )),
        "{}",
        p.text
    );
    assert!(before == tree_snapshot(&fx.path));
}

#[test]
fn a_checked_out_branch_is_refused_with_the_reset_keep_hint() {
    let (fx, [_, c2, c3]) = undo_fx();
    fx.set_head("work");
    let before = tree_snapshot(&fx.path);
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 1, "{}{}", r.text, r.err);
    assert!(
        r.text.contains("checked out") && r.text.contains("git reset --keep"),
        "{}",
        r.text
    );
    assert!(before == tree_snapshot(&fx.path), "the repository changed");
    assert_eq!(fx.tip("work"), c3);
}

#[test]
fn a_ref_that_is_no_local_branch_is_a_usage_error() {
    let (fx, [_, c2, c3]) = undo_fx();
    fx.set_ref("refs/tags/t", c3);
    let before = tree_snapshot(&fx.path);
    let r = run(
        &fx,
        &[
            "undo",
            "refs/tags/t",
            "--to",
            &c2.to_string(),
            "--expect",
            &c3.to_string(),
            "--yes",
        ],
    );
    assert_eq!(r.code, 2, "{}{}", r.text, r.err);
    assert!(before == tree_snapshot(&fx.path));
}

#[test]
fn the_update_is_a_compare_and_swap_and_a_lost_race_keeps_the_backup() {
    let (fx, [c1, c2, c3]) = undo_fx();
    let ctx = Ctx::open(&fx.path).unwrap();
    let mut moved = false;
    let e = match undo::undo_with(
        &ctx,
        &undo::Request {
            reference: "work".into(),
            to: c2.to_string(),
            expect: c3.to_string(),
        },
        &mut || {
            // another process moves the branch after the checks
            fx.branch("work", c1);
            moved = true;
        },
    ) {
        Ok(_) => panic!("the update went through over a changed ref"),
        Err(e) => e,
    };
    assert!(moved);
    assert!(
        e.to_string().contains("was not changed") && e.to_string().contains("refs/stw/backup/1/1"),
        "{e}"
    );
    assert_eq!(fx.tip("work"), c1, "the other writer's value stands");
    assert_eq!(refname_value(&fx, "refs/stw/backup/1/1"), Some(c3));
    assert!(
        !refs_log(&fx).iter().any(|f| f[3] == "refs/heads/work"),
        "no refs.log line for an update that did not happen"
    );
}

#[test]
fn an_existing_backup_ref_is_never_replaced() {
    let (fx, [c1, _, c3]) = undo_fx();
    let repo = fx.repo();
    // the ref is taken already: with the same value (gix would let a create-only edit pass) and with another one
    for taken in [c3, c1] {
        fx.set_ref("refs/stw/backup/1/1", taken);
        let e = undo::create_backup_with(&repo, "refs/stw/backup/1/1", c3, &mut || {}).unwrap_err();
        assert!(e.contains("exists already"), "{e}");
        assert_eq!(refname_value(&fx, "refs/stw/backup/1/1"), Some(taken));
    }
}

#[test]
fn a_backup_ref_that_is_there_without_a_counter_is_passed_over() {
    let (fx, [c1, c2, c3]) = undo_fx();
    fx.set_ref("refs/stw/backup/1/1", c1);
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(refname_value(&fx, "refs/stw/backup/1/1"), Some(c1));
    assert_eq!(refname_value(&fx, "refs/stw/backup/2/1"), Some(c3));
}

fn plain_repo() -> (Fx, ObjectId) {
    let fx = Fx::new();
    let a = fx.commit(None, &[("f", Some("a\n"))], "first");
    (fx, a)
}

fn put_counter(fx: &Fx, text: &str) {
    let blob = fx.repo().write_blob(text.as_bytes()).unwrap().detach();
    fx.set_ref(COUNTER_REF, blob);
}

#[test]
fn a_lost_race_for_the_first_run_takes_the_next_number() {
    let (fx, _) = plain_repo();
    let repo = fx.repo();
    let mut seen = Vec::new();
    let (run, prev, _) = undo::take_run_with(&repo, 5, &mut |attempt| {
        seen.push(attempt);
        if attempt == 0 {
            // another run writes its own blob for number 1 first
            put_counter(&fx, "1 other-1");
        }
    })
    .unwrap();
    assert_eq!((run, seen), (2, vec![0, 1]));
    assert!(prev.is_some());
    assert!(counter_text(&fx).starts_with("2 "));
}

#[test]
fn a_lost_race_for_a_later_run_takes_the_next_number() {
    let (fx, _) = plain_repo();
    put_counter(&fx, "4 seed");
    let repo = fx.repo();
    let (run, _, _) = undo::take_run_with(&repo, 5, &mut |attempt| {
        if attempt == 0 {
            put_counter(&fx, "5 other-2");
        }
    })
    .unwrap();
    assert_eq!(run, 6);
    assert!(counter_text(&fx).starts_with("6 "));
}

#[test]
fn a_run_number_that_keeps_being_taken_ends_in_an_error() {
    let (fx, _) = plain_repo();
    let repo = fx.repo();
    let mut n = 0u32;
    let e = undo::take_run_with(&repo, 3, &mut |attempt| {
        n += 1;
        put_counter(&fx, &format!("{} rival-{attempt}", attempt + 1));
    })
    .unwrap_err();
    assert_eq!(n, 3, "{e}");
    assert!(e.contains("after 3 tries"), "{e}");
}

#[test]
fn the_counter_is_never_reused_after_the_backups_are_pruned() {
    let (fx, [_, c2, c3]) = undo_fx();
    put_counter(&fx, "41 seed");
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(refname_value(&fx, "refs/stw/backup/42/1"), Some(c3));
}

fn seed_runs(fx: &Fx, from: u64, to: u64, value: ObjectId) {
    for run in from..=to {
        fx.set_ref(&format!("refs/stw/backup/{run}/1"), value);
    }
}

fn runs_of(fx: &Fx) -> Vec<String> {
    stw_refs(fx)
        .into_iter()
        .map(|(n, _)| n)
        .filter(|n| n.starts_with("refs/stw/backup/"))
        .collect()
}

#[test]
fn ten_runs_are_all_kept() {
    let (fx, [c1, c2, c3]) = undo_fx();
    seed_runs(&fx, 1, 9, c1);
    put_counter(&fx, "9 seed");
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    let runs = runs_of(&fx);
    assert_eq!(runs.len(), 10, "{runs:?}");
    assert!(
        runs.contains(&"refs/stw/backup/1/1".to_string())
            && runs.contains(&"refs/stw/backup/10/1".to_string())
    );
}

#[test]
fn the_eleventh_run_prunes_the_oldest_runs_whole_and_never_the_counter_or_the_newest() {
    let (fx, [c1, c2, c3]) = undo_fx();
    seed_runs(&fx, 1, 11, c1);
    // two refs in run 2 and a name that is no run: both stay what they are
    fx.set_ref("refs/stw/backup/2/2", c1);
    fx.set_ref("refs/stw/backup/x/1", c1);
    fx.set_ref("refs/stw/backup/1/x", c1);
    fx.set_ref("refs/stw/backup/z", c1);
    put_counter(&fx, "11 seed");
    let v = json(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(v["run"], 12, "{v}");
    let mut gone: Vec<String> = v["pruned"]
        .as_array()
        .unwrap()
        .iter()
        .map(|x| x.as_str().unwrap().to_string())
        .collect();
    gone.sort();
    assert_eq!(
        gone,
        [
            "refs/stw/backup/1/1",
            "refs/stw/backup/2/1",
            "refs/stw/backup/2/2"
        ],
        "{v}"
    );
    let runs = runs_of(&fx);
    for keep in (3..=12).map(|n| format!("refs/stw/backup/{n}/1")) {
        assert!(runs.contains(&keep), "{keep} missing from {runs:?}");
    }
    assert!(
        runs.contains(&"refs/stw/backup/x/1".to_string()),
        "{runs:?}"
    );
    assert!(runs.contains(&"refs/stw/backup/z".to_string()), "{runs:?}");
    assert_eq!(refname_value(&fx, "refs/stw/backup/12/1"), Some(c3));
    assert!(counter_text(&fx).starts_with("12 "));
    // the prune is in refs.log: old value to zeros
    let l = refs_log(&fx);
    let del = l
        .iter()
        .find(|f| f[3] == "refs/stw/backup/1/1")
        .expect("prune line");
    assert_eq!(
        (del[1].to_string(), del[2].as_str(), del[4].as_str()),
        (c1.to_string(), Z, "stw")
    );
    assert!(
        l.iter().any(|f| f[3] == "refs/stw/backup/2/2" && f[2] == Z),
        "{l:?}"
    );
}

#[test]
fn a_refs_log_that_is_a_symbolic_link_does_not_undo_the_undo() {
    let (fx, [_, c2, c3]) = undo_fx();
    let outside = fx.path.join("outside.log");
    std::fs::write(&outside, "").unwrap();
    std::fs::create_dir_all(fx.path.join(".git/stw/oplog")).unwrap();
    std::os::unix::fs::symlink(&outside, fx.path.join(".git/stw/oplog/refs.log")).unwrap();
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(fx.tip("work"), c2, "the ref change stands");
    assert!(r.err.contains("was not written"), "{}", r.err);
    assert_eq!(
        std::fs::read_to_string(&outside).unwrap(),
        "",
        "a link is never followed"
    );
    let gaps = std::fs::read_to_string(fx.path.join(".git/stw/oplog/errors.log")).unwrap();
    assert!(gaps.contains("write-failed"), "{gaps}");
    // a log that is a directory is no regular file either
    let (fx, [_, c2, c3]) = undo_fx();
    std::fs::create_dir_all(fx.path.join(".git/stw/oplog/refs.log")).unwrap();
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(fx.tip("work"), c2);
    assert!(r.err.contains("was not written"), "{}", r.err);
}

#[test]
fn without_a_configured_identity_the_reflog_names_the_fallback_identity() {
    let (fx, [_, c2, c3]) = undo_fx();
    fx.strip_identity();
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    let w = reflog_lines(&fx, "refs/heads/work");
    assert!(w[3].contains(" stw <stw@localhost> "), "{w:?}");
    let b = reflog_lines(&fx, "refs/stw/backup/1/1");
    assert!(b[0].contains(" stw <stw@localhost> "), "{b:?}");
}

#[test]
fn the_help_names_undo() {
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .args(["oplog", "--help"])
        .output()
        .unwrap();
    let t = String::from_utf8_lossy(&o.stdout);
    assert!(t.contains("undo"), "{t}");
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .args(["oplog", "undo", "--help"])
        .output()
        .unwrap();
    let t = String::from_utf8_lossy(&o.stdout);
    for w in [
        "--to",
        "--expect",
        "--yes",
        "compare-and-swap",
        "refs/stw/backup",
    ] {
        assert!(t.contains(w), "{w}: {t}");
    }
}

fn request(to: ObjectId, expect: ObjectId) -> undo::Request {
    undo::Request {
        reference: "work".into(),
        to: to.to_string(),
        expect: expect.to_string(),
    }
}

fn delete_counter(fx: &Fx) {
    std::fs::remove_file(fx.path.join(".git/refs/stw/backup-counter")).unwrap();
}

// ---- the run number never falls back below an existing run ------------------------------------------------------

#[test]
fn a_deleted_counter_does_not_reuse_a_number_and_the_new_backup_stays() {
    let (fx, [c1, c2, c3]) = undo_fx();
    seed_runs(&fx, 1, 12, c1);
    put_counter(&fx, "12 seed");
    delete_counter(&fx);
    let v = json(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(v["run"], 13, "{v}");
    assert_eq!(v["backup_ref"], "refs/stw/backup/13/1", "{v}");
    assert_eq!(refname_value(&fx, "refs/stw/backup/13/1"), Some(c3), "{v}");
    assert!(counter_text(&fx).starts_with("13 "));
    let mut gone: Vec<&str> = v["pruned"]
        .as_array()
        .unwrap()
        .iter()
        .map(|x| x.as_str().unwrap())
        .collect();
    gone.sort();
    assert_eq!(
        gone,
        [
            "refs/stw/backup/1/1",
            "refs/stw/backup/2/1",
            "refs/stw/backup/3/1"
        ],
        "{v}"
    );
}

#[test]
fn a_counter_below_the_highest_run_does_not_reuse_a_number() {
    let (fx, [c1, c2, c3]) = undo_fx();
    seed_runs(&fx, 1, 12, c1);
    put_counter(&fx, "3 seed");
    let v = json(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(v["run"], 13, "{v}");
    assert_eq!(refname_value(&fx, "refs/stw/backup/13/1"), Some(c3), "{v}");
}

#[test]
fn the_prune_never_removes_the_run_it_was_made_for() {
    let (fx, [c1, _, _]) = undo_fx();
    // runs 2 to 12 exist and run 1 was just taken: by the ranking alone run 1 would go
    seed_runs(&fx, 1, 12, c1);
    let (gone, failed) = undo::prune(&fx.repo(), 1).unwrap();
    assert!(failed.is_empty(), "{failed:?}");
    let names: Vec<&str> = gone.iter().map(|(n, _)| n.as_str()).collect();
    assert_eq!(names, ["refs/stw/backup/2/1"]);
    assert_eq!(refname_value(&fx, "refs/stw/backup/1/1"), Some(c1));
}

// ---- only a canonical decimal number names a run ----------------------------------------------------------------

#[test]
fn names_that_are_no_canonical_decimal_are_no_run_and_stay() {
    let (fx, [c1, c2, c3]) = undo_fx();
    seed_runs(&fx, 5, 14, c1);
    let odd = [
        "refs/stw/backup/+1/1",
        "refs/stw/backup/01/1",
        "refs/stw/backup/001/1",
        "refs/stw/backup/0/1",
        "refs/stw/backup/1/01",
        "refs/stw/backup/1/+1",
        "refs/stw/backup/2/0",
        "refs/stw/backup/99999999999999999999999/1",
    ];
    for n in odd {
        fx.set_ref(n, c1);
    }
    put_counter(&fx, "14 seed");
    let v = json(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(v["run"], 15, "{v}");
    assert_eq!(
        v["pruned"],
        serde_json::json!(["refs/stw/backup/5/1"]),
        "{v}"
    );
    let runs = runs_of(&fx);
    for n in odd {
        assert!(runs.contains(&n.to_string()), "{n} was touched: {runs:?}");
    }
    // the highest odd name does not count as a run either: the next number is 16
    let (c2s, c3s) = (c2.to_string(), c3.to_string());
    let back = json(
        &fx,
        &["undo", "work", "--to", &c3s, "--expect", &c2s, "--yes"],
    );
    assert_eq!(back["run"], 16, "{back}");
}

// ---- gaps of the run-number and create-only code ----------------------------------------------------------------

#[test]
fn a_backup_ref_that_appears_between_the_lookup_and_the_edit_is_not_replaced() {
    let (fx, [c1, _, c3]) = undo_fx();
    let repo = fx.repo();
    let name = "refs/stw/backup/1/1";
    let e = undo::create_backup_with(&repo, name, c3, &mut || fx.set_ref(name, c1)).unwrap_err();
    assert!(!e.is_empty());
    assert_eq!(
        refname_value(&fx, name),
        Some(c1),
        "the other writer's ref stands"
    );
    // and without a rival it is created
    let other = "refs/stw/backup/2/1";
    undo::create_backup_with(&repo, other, c3, &mut || {}).unwrap();
    assert_eq!(refname_value(&fx, other), Some(c3));
}

#[test]
fn the_command_tries_five_times_for_a_run_number() {
    let (fx, [_, c2, c3]) = undo_fx();
    let ctx = Ctx::open(&fx.path).unwrap();
    // four lost races, the fifth try wins
    let mut tries = 0;
    let out = undo::undo_hooked(
        &ctx,
        &request(c2, c3),
        &mut |attempt| {
            tries += 1;
            if attempt < 4 {
                put_counter(&fx, &format!("{} rival-{attempt}", attempt + 1));
            }
        },
        &mut || {},
    );
    match out {
        Ok(undo::Outcome::Done(d)) => assert_eq!((d.run, tries), (5, 5)),
        Ok(_) => panic!("refused"),
        Err(e) => panic!("{e}"),
    }
}

#[test]
fn the_command_gives_up_after_five_lost_races_and_changes_nothing() {
    let (fx, [_, c2, c3]) = undo_fx();
    let ctx = Ctx::open(&fx.path).unwrap();
    let mut tries = 0;
    let e = match undo::undo_hooked(
        &ctx,
        &request(c2, c3),
        &mut |attempt| {
            tries += 1;
            put_counter(&fx, &format!("{} rival-{attempt}", attempt + 1));
        },
        &mut || {},
    ) {
        Ok(_) => panic!("took a number against a rival that always wins"),
        Err(e) => e,
    };
    assert_eq!(tries, 5, "{e}");
    assert!(e.to_string().contains("after 5 tries"), "{e}");
    assert_eq!(fx.tip("work"), c3, "the branch stays");
    assert!(runs_of(&fx).is_empty());
}

#[test]
fn two_first_runs_do_not_share_a_number_even_when_the_rival_wrote_the_same_text() {
    // Two first runs both read no counter and take number 1. Without a nonce both write the blob "1": the same object, and
    // gix lets a create-only update pass when the ref already holds the value it would write.
    let (fx, _) = plain_repo();
    let repo = fx.repo();
    let (run, _, _) = undo::take_run_with(&repo, 5, &mut |attempt| {
        if attempt == 0 {
            put_counter(&fx, "1");
        }
    })
    .unwrap();
    assert_eq!(run, 2);
    assert!(counter_text(&fx).starts_with("2 "));
}

// ---- a command the output names is safe to paste ----------------------------------------------------------------

const EVIL: &str = "evil;$HOME|`id`'q";
const EVIL_REF: &str = "refs/heads/evil;$HOME|`id`'q";
const EVIL_QUOTED: &str = r"'refs/heads/evil;$HOME|`id`'\''q'";

#[test]
fn undo_plan_quotes_a_ref_name_with_shell_characters_and_json_keeps_the_raw_name() {
    let (fx, [_, c2, c3]) = undo_fx_named(EVIL);
    let r = run(&fx, &["undo-plan", EVIL_REF]);
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert!(
        r.text.contains(&format!(
            "Apply with: stw oplog undo {EVIL_QUOTED} --to {c2} --expect {c3} --yes"
        )),
        "{}",
        r.text
    );
    let v = json(&fx, &["undo-plan", EVIL_REF]);
    assert_eq!(
        v["apply"],
        format!("stw oplog undo {EVIL_REF} --to {c2} --expect {c3} --yes"),
        "{v}"
    );
    assert_eq!(v["ref"], EVIL_REF);
}

#[test]
fn undo_quotes_the_way_back_and_json_keeps_the_raw_name() {
    let (fx, [_, c2, c3]) = undo_fx_named(EVIL);
    let a = |r: &'static str| ["undo", r, "--to", "", "--expect", "", "--yes"].map(String::from);
    let mut args = a(EVIL_REF);
    args[3] = c2.to_string();
    args[5] = c3.to_string();
    let args: Vec<&str> = args.iter().map(String::as_str).collect();
    let r = run(&fx, &args);
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert!(
        r.text.contains(&format!(
            "way back:   stw oplog undo {EVIL_QUOTED} --to {c3} --expect {c2} --yes"
        )),
        "{}",
        r.text
    );
    // the same undo again through --json, from the state before
    fx.branch(EVIL, c3);
    let v = json(&fx, &args);
    assert_eq!(
        v["way_back"],
        format!("stw oplog undo {EVIL_REF} --to {c3} --expect {c2} --yes"),
        "{v}"
    );
}

#[test]
fn a_plain_ref_name_is_not_quoted() {
    let (fx, [_, c2, c3]) = undo_fx();
    let r = run(&fx, &undo_args(&c2.to_string(), &c3.to_string()));
    assert!(r.text.contains("undo refs/heads/work --to"), "{}", r.text);
    assert!(!r.text.contains('\''), "{}", r.text);
}
