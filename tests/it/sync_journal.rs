//! The journal of a `stw sync` run: run ID, record, recovery refs, order of the writes, retention (port of the Go
//! journal, `internal/stwsync/journal.go`; Go cases: `sync_test.go`, `retention_test.go`, `restore_test.go`).
//! Every repository lives in a tempdir of `sync_flags::World`.
use std::path::{Path, PathBuf};

use serde_json::Value;
use stw::git_runner::{recovery_ref, BranchRef, GitError, Hex40, Op, RemoteRef, RunId, Runner};
use stw::sync::journal::{self, Journal, Record};

use crate::common::scratch;
use crate::sync_flags::World;

/// Two repositories of the workspace, both behind their origin: the root `ws` and the submodule `sub` (on a branch).
pub struct J {
    pub w: World,
    pub ws: PathBuf,
    pub sub: PathBuf,
    pub ws_old: String,
    pub ws_new: String,
    pub sub_old: String,
    pub sub_new: String,
    pub ws_up: PathBuf,
    pub ws_origin: PathBuf,
    sub_up: PathBuf,
    sub_origin: PathBuf,
}

pub fn setup() -> J {
    let w = World::new();
    let up = |name: &str, file: &str| -> (PathBuf, PathBuf, String) {
        let seed = w.p(&format!("{name}-up"));
        std::fs::create_dir(&seed).unwrap();
        w.git(&seed, &["init", "-q"]);
        let old = w.commit(&seed, file, "1\n");
        let origin = w.p(&format!("{name}.git"));
        w.git(
            &w.base,
            &[
                "clone",
                "-q",
                "--bare",
                &format!("{name}-up"),
                &format!("{name}.git"),
            ],
        );
        (seed, origin, old)
    };
    let (ws_up, ws_origin, _) = up("a", "a.txt");
    let (sub_up, sub_origin, _) = up("b", "b.txt");
    let ws = w.p("ws");
    w.git(&w.base, &["clone", "-q", "a.git", "ws"]);
    w.git(
        &ws,
        &[
            "submodule",
            "add",
            "-q",
            sub_origin.to_str().unwrap(),
            "sub",
        ],
    );
    w.git(&ws, &["commit", "-q", "-m", "add sub"]);
    w.git(&ws, &["push", "-q", "origin", "HEAD:main"]);
    let ws_old = w.git(&ws, &["rev-parse", "HEAD"]);
    // the news of both origins: one commit each, on top of what the clone has
    w.git(
        &ws_up,
        &["fetch", "-q", ws_origin.to_str().unwrap(), "main"],
    );
    w.git(&ws_up, &["reset", "-q", "--hard", "FETCH_HEAD"]);
    let ws_new = w.commit(&ws_up, "a2.txt", "2\n");
    w.git(
        &ws_up,
        &["push", "-q", ws_origin.to_str().unwrap(), "HEAD:main"],
    );
    let sub_new = w.commit(&sub_up, "b2.txt", "2\n");
    w.git(
        &sub_up,
        &["push", "-q", sub_origin.to_str().unwrap(), "HEAD:main"],
    );
    let sub = ws.join("sub");
    let sub_old = w.git(&sub, &["rev-parse", "HEAD"]);
    J {
        ws,
        sub,
        ws_old,
        ws_new,
        sub_old,
        sub_new,
        ws_up,
        ws_origin,
        sub_up,
        sub_origin,
        w,
    }
}

impl J {
    /// Another commit on both origins (a later run has news again).
    pub fn news(&self, tag: &str) {
        for (up, origin) in [
            (&self.ws_up, &self.ws_origin),
            (&self.sub_up, &self.sub_origin),
        ] {
            let name = format!("{tag}.txt");
            self.w
                .git(up, &["fetch", "-q", origin.to_str().unwrap(), "main"]);
            self.w.git(up, &["reset", "-q", "--hard", "FETCH_HEAD"]);
            self.w.commit(up, &name, tag);
            self.w
                .git(up, &["push", "-q", origin.to_str().unwrap(), "HEAD:main"]);
        }
    }

    pub fn git_dir(&self, repo: &Path) -> PathBuf {
        PathBuf::from(self.w.git(
            repo,
            &["rev-parse", "--path-format=absolute", "--git-common-dir"],
        ))
    }

    pub fn journal_file(&self, repo: &Path, id: &str) -> PathBuf {
        self.git_dir(repo)
            .join("stw/sync")
            .join(format!("{id}.json"))
    }

    pub fn recovery(&self, repo: &Path, id: &str, n: u32) -> Option<String> {
        let (ok, out, _) = self.w.git_try(
            repo,
            &[
                "rev-parse",
                "--verify",
                "-q",
                &format!("refs/stw/recovery/sync/{id}/{n}"),
            ],
        );
        ok.then_some(out)
    }

    pub fn recovery_refs(&self, repo: &Path) -> Vec<String> {
        self.w
            .git(repo, &["for-each-ref", "--format=%(refname)", "refs/stw"])
            .lines()
            .map(str::to_string)
            .collect()
    }

    pub fn tip(&self, repo: &Path) -> String {
        self.w.git(repo, &["rev-parse", "refs/heads/main"])
    }

    pub fn text(&self, args: &[&str]) -> (i32, String, String) {
        let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
            .env_clear()
            .env("PATH", std::env::var_os("PATH").unwrap())
            .env("HOME", &self.w.home)
            .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
            .current_dir(&self.w.base)
            .arg("sync")
            .args(args)
            .arg("--root")
            .arg(&self.ws)
            .output()
            .unwrap();
        (
            o.status.code().unwrap_or(-1),
            String::from_utf8_lossy(&o.stdout).into_owned(),
            String::from_utf8_lossy(&o.stderr).into_owned(),
        )
    }
}

pub fn repo<'a>(v: &'a Value, name: &str) -> &'a Value {
    v["repos"]
        .as_array()
        .unwrap_or_else(|| panic!("no repos in {v}"))
        .iter()
        .find(|r| r["name"] == name)
        .unwrap_or_else(|| panic!("no repository {name} in {v}"))
}

#[test]
fn a_run_that_moves_two_repositories_journals_both() {
    let j = setup();
    let (code, v, err) = j.w.stw(&j.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    let id = v["run_id"].as_str().expect("a run id").to_string();
    assert!(RunId::parse(&id).is_some(), "{id}");
    for (name, dir, old, new) in [
        ("ws", &j.ws, &j.ws_old, &j.ws_new),
        ("sub", &j.sub, &j.sub_old, &j.sub_new),
    ] {
        let r = repo(&v, name);
        assert_eq!(r["result"], "fast-forwarded", "{r}");
        assert_eq!(&j.tip(dir), new, "{name}");
        let mut keys: Vec<&String> = r["journal"].as_object().unwrap().keys().collect();
        keys.sort();
        assert_eq!(keys, ["n", "recovery_ref", "state"], "{r}");
        assert_eq!(r["journal"]["n"], 1, "{r}");
        assert_eq!(r["journal"]["state"], "completed", "{r}");
        assert_eq!(
            r["journal"]["recovery_ref"],
            format!("refs/stw/recovery/sync/{id}/1")
        );
        // the recovery ref holds the old tip
        assert_eq!(
            j.recovery(dir, &id, 1).as_deref(),
            Some(old.as_str()),
            "{name}"
        );
        // the record, in the format of the Go tool
        let rec: Value =
            serde_json::from_slice(&std::fs::read(j.journal_file(dir, &id)).unwrap()).unwrap();
        assert_eq!(rec["type"], "sync");
        assert_eq!(rec["schema_version"], 1);
        assert_eq!(rec["run_id"], id.as_str());
        let e = &rec["entries"][0];
        assert_eq!(e["n"], 1);
        assert_eq!(e["ref"], "refs/heads/main");
        assert_eq!(e["old_oid"], old.as_str());
        assert_eq!(e["planned_new_oid"], new.as_str());
        assert_eq!(e["new_oid"], new.as_str());
        assert_eq!(e["state"], "completed");
        assert_eq!(e["recovery_ref"], format!("refs/stw/recovery/sync/{id}/1"));
    }
}

#[cfg(unix)]
#[test]
fn the_record_is_private_to_the_user() {
    use std::os::unix::fs::PermissionsExt;
    let j = setup();
    let (_, v, _) = j.w.stw(&j.ws, &[]);
    let id = v["run_id"].as_str().unwrap();
    let file = j.journal_file(&j.ws, id);
    let mode = |p: &Path| std::fs::metadata(p).unwrap().permissions().mode() & 0o777;
    assert_eq!(mode(&file), 0o600);
    assert_eq!(mode(file.parent().unwrap()), 0o700);
    assert_eq!(mode(file.parent().unwrap().parent().unwrap()), 0o700);
    let leftovers: Vec<_> = std::fs::read_dir(file.parent().unwrap())
        .unwrap()
        .map(|e| e.unwrap().file_name())
        .collect();
    assert_eq!(leftovers.len(), 1, "no temporary file stays: {leftovers:?}");
}

#[test]
fn the_text_names_the_run_and_its_recovery_refs() {
    let j = setup();
    let (code, out, err) = j.text(&[]);
    assert_eq!(code, 0, "{out} {err}");
    let line = out
        .lines()
        .find(|l| l.starts_with("journal: run "))
        .unwrap_or_else(|| panic!("no journal line in {out}"));
    let id = line
        .strip_prefix("journal: run ")
        .unwrap()
        .split(',')
        .next()
        .unwrap();
    assert!(RunId::parse(id).is_some(), "{line}");
    assert!(
        line.ends_with(&format!("recovery refs under refs/stw/recovery/sync/{id}")),
        "{line}"
    );
}

#[test]
fn a_plan_writes_no_journal_and_no_refs() {
    let j = setup();
    for args in [
        &["--plan"][..],
        &["--dry-run"][..],
        &["--plan", "--prune", "--submodules"][..],
    ] {
        let (code, v, err) = j.w.stw(&j.ws, args);
        assert_eq!(code, 0, "{args:?}: {v} {err}");
        assert!(v["run_id"].is_null(), "{args:?}: {v}");
        for r in v["repos"].as_array().unwrap() {
            assert!(r.get("journal").is_none(), "{r}");
        }
        for dir in [&j.ws, &j.sub] {
            assert!(!j.git_dir(dir).join("stw").exists(), "{args:?}");
            assert!(j.recovery_refs(dir).is_empty(), "{args:?}");
        }
    }
}

#[test]
fn a_run_without_a_change_writes_no_journal() {
    let j = setup();
    j.w.stw(&j.ws, &[]);
    let before = j.recovery_refs(&j.ws);
    // everything is current now
    let (code, v, err) = j.w.stw(&j.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert!(v["run_id"].is_null(), "{v}");
    assert_eq!(j.recovery_refs(&j.ws), before);
    let files = std::fs::read_dir(j.git_dir(&j.ws).join("stw/sync"))
        .unwrap()
        .count();
    assert_eq!(files, 1, "only the first run has a record");
}

#[test]
fn a_refused_fast_forward_leaves_no_record_and_no_recovery_ref() {
    let j = setup();
    // an untracked file where the update adds one: the check refuses before the journal step
    std::fs::write(j.ws.join("a2.txt"), "mine\n").unwrap();
    let (code, v, _) = j.w.stw(&j.ws, &[]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "error", "{r}");
    assert_eq!(j.tip(&j.ws), j.ws_old);
    assert!(r.get("journal").is_none(), "{r}");
    assert!(j.recovery_refs(&j.ws).is_empty());
    assert!(!j.git_dir(&j.ws).join("stw").exists());
    // the other repository was not held back and is journaled
    assert_eq!(repo(&v, "sub")["journal"]["state"], "completed");
    assert!(v["run_id"].is_string());
}

#[test]
fn when_the_record_cannot_be_written_the_branch_does_not_move() {
    let j = setup();
    // `stw` in the git directory is a file: the record has no place
    std::fs::write(j.git_dir(&j.ws).join("stw"), "in the way").unwrap();
    let (code, v, _) = j.w.stw(&j.ws, &[]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "error", "{r}");
    assert!(
        r["error"].as_str().unwrap().contains("journal-failed"),
        "{r}"
    );
    assert_eq!(j.tip(&j.ws), j.ws_old, "no change without a record");
    assert!(
        j.recovery_refs(&j.ws).is_empty(),
        "the recovery ref made for it was taken back"
    );
    assert!(!j.ws.join("a2.txt").exists(), "no file was written");
    // the other repository is not affected
    assert_eq!(j.tip(&j.sub), j.sub_new);
}

#[cfg(unix)]
#[test]
fn a_symbolic_link_as_the_journal_directory_is_refused() {
    let j = setup();
    let elsewhere = j.w.p("elsewhere");
    std::fs::create_dir(&elsewhere).unwrap();
    std::os::unix::fs::symlink(&elsewhere, j.git_dir(&j.ws).join("stw")).unwrap();
    let (code, v, _) = j.w.stw(&j.ws, &[]);
    assert_eq!(code, 1, "{v}");
    assert_eq!(j.tip(&j.ws), j.ws_old);
    assert_eq!(std::fs::read_dir(&elsewhere).unwrap().count(), 0);
    assert!(j.recovery_refs(&j.ws).is_empty());
}

#[test]
fn the_recovery_ref_and_the_record_exist_before_the_branch_moves() {
    // The state a crash right after the two writes leaves: recovery ref and record in `prepared`, branch unmoved.
    let j = setup();
    let repo = stw::repo::open(&j.ws).unwrap();
    let id = journal::new_run_id();
    let old = Hex40::parse(&j.ws_old).unwrap();
    let new = Hex40::parse(&j.ws_new).unwrap();
    let mut jr = Journal::new(&repo, id.clone());
    let i = jr
        .prepare(
            &repo,
            "refs/heads/main",
            &j.ws.display().to_string(),
            old,
            new,
        )
        .unwrap();
    assert_eq!(i, 0);
    assert_eq!(j.tip(&j.ws), j.ws_old, "prepare moves nothing");
    assert_eq!(
        j.recovery(&j.ws, id.as_str(), 1).as_deref(),
        Some(j.ws_old.as_str())
    );
    let rec = journal::load(&journal::dir_of(&repo), &id)
        .unwrap()
        .unwrap();
    assert_eq!(rec.entries[0].state, "prepared");
    assert_eq!(rec.entries[0].old_oid, j.ws_old);
    assert_eq!(rec.entries[0].planned_new_oid, j.ws_new);
    assert!(rec.entries[0].new_oid.is_empty());
}

#[test]
fn a_run_id_is_never_used_twice() {
    let j = setup();
    let repo = stw::repo::open(&j.ws).unwrap();
    let id = journal::new_run_id();
    let (old, new) = (
        Hex40::parse(&j.ws_old).unwrap(),
        Hex40::parse(&j.ws_new).unwrap(),
    );
    let mut first = Journal::new(&repo, id.clone());
    first
        .prepare(&repo, "refs/heads/main", "w", old, new)
        .unwrap();
    // a second run with the same ID (a collision): its recovery ref already exists, so it stops, and the record of
    // the first stays as it was
    let mut second = Journal::new(&repo, id.clone());
    let e = second
        .prepare(&repo, "refs/heads/other", "w", old, new)
        .unwrap_err();
    assert!(e.contains("recovery ref"), "{e}");
    let rec = journal::load(&journal::dir_of(&repo), &id)
        .unwrap()
        .unwrap();
    assert_eq!(rec.entries.len(), 1);
    assert_eq!(rec.entries[0].reference, "refs/heads/main");
}

#[test]
fn run_ids_are_distinct_and_follow_the_clock() {
    let ids: Vec<RunId> = (0..2000).map(|_| journal::new_run_id()).collect();
    let mut seen = std::collections::BTreeSet::new();
    for id in &ids {
        assert!(RunId::parse(id.as_str()).is_some(), "{id}");
        assert!(seen.insert(id.as_str().to_string()), "duplicate {id}");
    }
    let stamps: Vec<String> = ids
        .iter()
        .map(|i| i.order_key()[..21].to_string())
        .collect();
    assert!(
        stamps.windows(2).all(|w| w[0] <= w[1]),
        "time stamps never go back"
    );
    // the order key counts an ID without microseconds as microsecond zero
    let a = RunId::parse("20261111T123456Z-00000000").unwrap();
    let b = RunId::parse("20261111T123456000001Z-00000000").unwrap();
    assert!(a.order_key() < b.order_key());
    // the same second, microsecond zero: the suffix decides, as if the ID had six zero digits
    let c = RunId::parse("20261111T123456Z-ffffffff").unwrap();
    let d = RunId::parse("20261111T123456000000Z-00000000").unwrap();
    assert!(c.order_key() > d.order_key());
}

#[test]
fn a_record_of_the_go_tool_is_read_and_ours_has_the_same_keys() {
    let id = RunId::parse("20261111T123456123456Z-0a1b2c3d").unwrap();
    let go = r#"{"type":"sync","schema_version":1,"run_id":"20261111T123456123456Z-0a1b2c3d","remote":"","started":"2026-11-11T12:34:56Z","entries":[{"n":1,"ref":"refs/heads/main","worktree":"/w","old_oid":"1111111111111111111111111111111111111111","planned_new_oid":"2222222222222222222222222222222222222222","new_oid":"2222222222222222222222222222222222222222","orig_head":"1111111111111111111111111111111111111111","stash_oid":"","recovery_ref":"refs/stw/recovery/sync/20261111T123456123456Z-0a1b2c3d/1","state":"completed","reason":""}]}"#;
    let rec = Record::parse(go, &id).unwrap();
    assert_eq!(rec.entries[0].reference, "refs/heads/main");
    assert_eq!(rec.entries[0].state, "completed");
    let ours = rec.to_json();
    let theirs: Value = serde_json::from_str(go).unwrap();
    let keys = |v: &Value| -> Vec<String> { v.as_object().unwrap().keys().cloned().collect() };
    assert_eq!(keys(&ours), keys(&theirs));
    assert_eq!(keys(&ours["entries"][0]), keys(&theirs["entries"][0]));
    assert_eq!(ours, theirs);
}

#[test]
fn a_record_that_is_not_of_this_run_or_not_a_sync_record_is_refused() {
    let id = RunId::parse("20261111T123456123456Z-0a1b2c3d").unwrap();
    let ok = |extra: &str| {
        format!(
            r#"{{"type":"sync","run_id":"20261111T123456123456Z-0a1b2c3d","entries":[{extra}]}}"#
        )
    };
    assert!(Record::parse(&ok(""), &id).is_ok());
    for (text, why) in [
        ("not json".to_string(), "not JSON"),
        (
            r#"{"type":"tidy","run_id":"20261111T123456123456Z-0a1b2c3d","entries":[]}"#.into(),
            "type",
        ),
        (
            r#"{"type":"sync","run_id":"20261111T123456123456Z-ffffffff","entries":[]}"#.into(),
            "run id",
        ),
        (
            r#"{"type":"sync","run_id":"20261111T123456123456Z-0a1b2c3d"}"#.into(),
            "entries",
        ),
        (ok(r#"{"n":0}"#), "n"),
        (ok(r#"{"n":1,"ref":5}"#), "ref"),
        (ok(r#"{"n":-1}"#), "n"),
    ] {
        let e = Record::parse(&text, &id).unwrap_err();
        assert!(!e.is_empty(), "{why}");
    }
}

/// Twelve runs in one repository: only the ten most recent keep record and recovery ref (an older run whose recovery ref
/// is not clean stays, as in Go).
#[test]
fn retention_keeps_ten_runs_and_spares_what_is_not_clean() {
    let j = setup();
    let mut ids = Vec::new();
    let (_, v, _) = j.w.stw(&j.ws, &[]);
    ids.push(v["run_id"].as_str().unwrap().to_string());
    // an incomplete record (killed run) and a foreign file in the directory
    let repo = stw::repo::open(&j.ws).unwrap();
    let stuck = RunId::parse("20200101T000000000000Z-00000001").unwrap();
    let mut jr = Journal::new(&repo, stuck.clone());
    jr.prepare(
        &repo,
        "refs/heads/main",
        "w",
        Hex40::parse(&j.ws_old).unwrap(),
        Hex40::parse(&j.ws_new).unwrap(),
    )
    .unwrap();
    let dir = journal::dir_of(&repo);
    std::fs::write(dir.join("README"), "not a record").unwrap();
    // an old completed record whose entry names a ref that is not its own recovery ref: retention never deletes
    // that ref, and so the record stays; the real recovery ref of the entry is not touched either
    let foreign = "20200101T000000000000Z-00000002";
    let body = format!(
        r#"{{"type":"sync","schema_version":1,"run_id":"{foreign}","remote":"","started":"2020-01-01T00:00:00Z","entries":[{{"n":1,"ref":"refs/heads/main","worktree":"","old_oid":"{old}","planned_new_oid":"{old}","new_oid":"{old}","orig_head":"","stash_oid":"","recovery_ref":"refs/heads/main","state":"completed","reason":""}}]}}"#,
        old = j.ws_old
    );
    std::fs::write(dir.join(format!("{foreign}.json")), body).unwrap();
    j.w.git(
        &j.ws,
        &[
            "update-ref",
            &format!("refs/stw/recovery/sync/{foreign}/1"),
            &j.ws_old,
        ],
    );
    for n in 0..11 {
        j.news(&format!("n{n}"));
        let (_, v, err) = j.w.stw(&j.ws, &[]);
        ids.push(
            v["run_id"]
                .as_str()
                .unwrap_or_else(|| panic!("run {n}: {v} {err}"))
                .to_string(),
        );
        if n == 0 {
            // the first run's recovery ref was moved by someone: it is not deleted, and its record stays
            let moved = j.w.git(&j.ws, &["rev-parse", &j.ws_new]);
            j.w.git(
                &j.ws,
                &[
                    "update-ref",
                    &format!("refs/stw/recovery/sync/{}/1", ids[0]),
                    &moved,
                ],
            );
        }
    }
    assert_eq!(ids.len(), 12);
    let kept: Vec<String> = journal::list_ids(&dir)
        .unwrap()
        .iter()
        .map(|i| i.to_string())
        .collect();
    // ids[1] is the oldest clean run beyond the ten: dropped, with its recovery ref
    assert!(!kept.contains(&ids[1]), "{kept:?}");
    assert!(j.recovery(&j.ws, &ids[1], 1).is_none());
    // the last ten completed runs: ids[2..] plus the moved-ref run ids[0], which is spared
    for id in &ids[2..] {
        assert!(kept.contains(id), "{id} in {kept:?}");
        assert!(j.recovery(&j.ws, id, 1).is_some());
    }
    assert!(kept.contains(&ids[0]), "an unclean run keeps its record");
    assert!(j.recovery(&j.ws, &ids[0], 1).is_some());
    // the incomplete one stays too, and the foreign file is ignored and untouched
    assert!(kept.contains(&stuck.to_string()));
    assert!(dir.join("README").exists());
    assert!(
        kept.contains(&foreign.to_string()),
        "a record with a foreign ref stays"
    );
    assert!(j.recovery(&j.ws, foreign, 1).is_some());
    assert!(!j.tip(&j.ws).is_empty(), "the branch it named is untouched");
}

#[test]
fn the_argument_vectors_of_the_journal_operations_are_fixed() {
    let t = scratch();
    let argv = |op: Op| -> Vec<String> {
        Runner::from_env(vec![("PATH".into(), "/usr/bin".into())])
            .argv(t.path(), op)
            .unwrap()
            .iter()
            .map(|a| a.to_string_lossy().into_owned())
            .collect()
    };
    let tail = |v: &[String], from: &str| v[v.iter().position(|a| a == from).unwrap()..].to_vec();
    let run = RunId::parse("20261111T123456123456Z-0a1b2c3d").unwrap();
    let a = Hex40::parse(&"a".repeat(40)).unwrap();
    let b = Hex40::parse(&"b".repeat(40)).unwrap();
    let r = "refs/stw/recovery/sync/20261111T123456123456Z-0a1b2c3d/2";
    assert_eq!(recovery_ref(&run, 2), r);
    let branch = BranchRef::parse("refs/heads/feature/x").unwrap();
    assert_eq!(
        tail(
            &argv(Op::UpdateBranchCas {
                run,
                branch,
                new: a,
                expected: b
            }),
            "update-ref"
        ),
        [
            "update-ref".to_string(),
            "--no-deref".into(),
            "-m".into(),
            "stw: sync restore 20261111T123456123456Z-0a1b2c3d".into(),
            "refs/heads/feature/x".into(),
            "a".repeat(40),
            "b".repeat(40)
        ]
    );
    assert_eq!(
        tail(&argv(Op::ResetKeep { target: a }), "reset"),
        [
            "reset".to_string(),
            "--keep".into(),
            "--quiet".into(),
            "a".repeat(40),
            "--".into()
        ]
    );
    let up = RemoteRef::parse("refs/remotes/origin/feature/x").unwrap();
    assert_eq!(
        tail(
            &argv(Op::SyncRebase {
                upstream: up.clone(),
                autostash: false
            }),
            "rebase"
        ),
        [
            "rebase".to_string(),
            "--no-update-refs".into(),
            "--no-rebase-merges".into(),
            "--no-autostash".into(),
            "--fork-point".into(),
            "refs/remotes/origin/feature/x".into()
        ]
    );
    assert_eq!(
        tail(
            &argv(Op::SyncRebase {
                upstream: up,
                autostash: true
            }),
            "rebase"
        )[3],
        "--autostash"
    );
    assert_eq!(
        tail(
            &argv(Op::SyncMerge {
                target: a,
                autostash: false
            }),
            "merge"
        ),
        [
            "merge".to_string(),
            "--no-edit".into(),
            "--ff".into(),
            "--no-verify-signatures".into(),
            "--no-autostash".into(),
            "a".repeat(40)
        ]
    );
    assert_eq!(
        tail(
            &argv(Op::SyncMerge {
                target: a,
                autostash: true
            }),
            "merge"
        )[4],
        "--autostash"
    );
}

#[test]
fn only_a_remote_tracking_ref_of_origin_reaches_the_rebase_argument_vector() {
    for bad in [
        "",
        "main",
        "refs/remotes/origin/",
        "refs/remotes/origin/-x",
        "refs/remotes/origin/a..b",
        "refs/remotes/other/x",
        "refs/heads/x",
        "refs/remotes/origin/a b",
        "refs/remotes/origin/a\nb",
        "refs/remotes/origin/x.lock",
        "refs/remotes/origin/a~1",
        "refs/remotes/origin/@{u}",
        "--upload-pack=x",
    ] {
        assert!(RemoteRef::parse(bad).is_none(), "{bad:?}");
    }
    assert!(RemoteRef::parse("refs/remotes/origin/feature/ok-1.x").is_some());
}

#[test]
fn only_a_well_formed_run_id_or_branch_reaches_an_argument_vector() {
    for bad in [
        "",
        "../x",
        "-x",
        "20261111T123456Z-0A1B2C3D",
        "20261111T123456Z-0a1b2c3",
        "20261111T1234567Z-0a1b2c3d",
        "20261111T123456123456Z-0a1b2c3d/1",
        "20261111 123456Z-0a1b2c3d",
        "20261111T123456Z-0a1b2c3d\n",
        "x20261111T123456Z-0a1b2c3d",
        "20261111T123456Z-0a1b2c3d.json",
        "20261111T12345A123456Z-0a1b2c3d",
    ] {
        assert!(RunId::parse(bad).is_none(), "{bad:?}");
    }
    for good in [
        "20261111T123456Z-0a1b2c3d",
        "20261111T123456123456Z-0a1b2c3d",
    ] {
        assert!(RunId::parse(good).is_some(), "{good}");
    }
    for bad in [
        "",
        "main",
        "refs/heads/",
        "refs/heads/-x",
        "refs/heads/a..b",
        "refs/heads/../x",
        "refs/tags/x",
        "refs/heads/a b",
        "refs/heads/x.lock",
        "refs/heads/a\nb",
        "refs/heads/a\x1b[31m",
        "refs/heads/a~1",
        "refs/heads/a:b",
        "refs/heads/a?",
        "refs/heads/@{u}",
        "refs/heads/x/",
        "refs/heads//x",
        "refs/stw/recovery/x",
        "--refs/heads/x",
        "refs/heads/--upload-pack=x",
    ] {
        assert!(BranchRef::parse(bad).is_none(), "{bad:?}");
    }
    assert!(BranchRef::parse("refs/heads/feature/ok-1.x").is_some());
}

#[test]
fn a_reset_refuses_a_repository_that_names_a_filter_driver() {
    let w = World::new();
    let repo = w.p("r");
    std::fs::create_dir(&repo).unwrap();
    w.git(&repo, &["init", "-q"]);
    let c = w.commit(&repo, "a.txt", "1\n");
    w.git(&repo, &["config", "filter.lfs.smudge", "x"]);
    let runner = Runner::from_env(vec![
        ("PATH".into(), std::env::var_os("PATH").unwrap()),
        ("HOME".into(), w.home.clone().into_os_string()),
    ]);
    let e = runner
        .run(
            &repo,
            Op::ResetKeep {
                target: Hex40::parse(&c).unwrap(),
            },
        )
        .unwrap_err();
    assert!(
        matches!(&e, GitError::Refused { key } if key == "filter.lfs.smudge"),
        "{e}"
    );
}

#[test]
fn a_repository_config_that_names_a_program_stops_nothing_and_no_git_is_needed() {
    // The journal step goes through gix: a local core.sshCommand, credential helper or url rewrite does not refuse it,
    // and a run (here: from the refs already fetched) needs no `git` on PATH.
    let j = setup();
    for dir in [&j.ws, &j.sub] {
        j.w.git(dir, &["fetch", "-q", "origin"]);
        j.w.git(dir, &["config", "core.sshCommand", "false"]);
        j.w.git(dir, &["config", "credential.helper", "false"]);
        j.w.git(
            dir,
            &[
                "config",
                "url.https://example.invalid/.insteadOf",
                "ssh://example.invalid/",
            ],
        );
    }
    let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("HOME", &j.w.home)
        .current_dir(&j.w.base)
        .args(["sync", "--offline", "--json", "--root"])
        .arg(&j.ws)
        .output()
        .unwrap();
    let v: Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(o.status.code(), Some(0), "{v}");
    let id = v["run_id"].as_str().expect("journaled").to_string();
    for (dir, old, new) in [
        (&j.ws, &j.ws_old, &j.ws_new),
        (&j.sub, &j.sub_old, &j.sub_new),
    ] {
        assert_eq!(&j.tip(dir), new);
        assert_eq!(j.recovery(dir, &id, 1).as_deref(), Some(old.as_str()));
        assert!(j.journal_file(dir, &id).is_file());
    }
    let log = j
        .git_dir(&j.ws)
        .join(format!("logs/refs/stw/recovery/sync/{id}/1"));
    assert!(log.is_file(), "the recovery ref has a reflog");
    // restore needs git (reset --keep): the refusal of the repository's own config is said clearly
    let (code, v, _) = j.w.stw(&j.ws, &["restore"]);
    assert_eq!(code, 1, "{v}");
    let r = repo(&v, "ws");
    assert_eq!(r["result"], "failed", "{r}");
    assert!(
        r["reason"]
            .as_str()
            .unwrap()
            .contains("own git config sets core.sshcommand"),
        "{r}"
    );
    assert_eq!(j.tip(&j.ws), j.ws_new);
}
