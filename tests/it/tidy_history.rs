//! `stw tidy history` (port of the Go `tidy history`, `internal/workspace/tidy_v3_history.go`; Go cases:
//! `tidy_v3_history_test.go`, `cli/tidy_prune_v4_history_test.go` usage cases): the stored manifests of `tidy apply`, newest
//! first, read-only, every bound visible.
use std::collections::BTreeMap;
use std::path::Path;
use std::process::{Command, Output};
use std::time::Duration;

use serde_json::Value;
use stw::tidy_history::history_with;

use crate::common::*;

fn build() -> Fx {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    let c1 = fx.commit(Some(c0), &[("a.txt", Some("b\n"))], "main work");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.branch("main", c1);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("merged-a", c0);
    fx.branch("merged-b", c0);
    fx
}

fn stw(home: &Path, args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("HOME", home)
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .args(args)
        .output()
        .unwrap()
}

fn json(o: &Output) -> Value {
    serde_json::from_slice(&o.stdout).unwrap_or_else(|e| {
        panic!(
            "not JSON ({e}): {} {}",
            String::from_utf8_lossy(&o.stdout),
            String::from_utf8_lossy(&o.stderr)
        )
    })
}

fn history(home: &Path, dir: &Path, extra: &[&str]) -> Output {
    let mut a = vec![
        "tidy",
        "history",
        "--json",
        "--manifest-dir",
        dir.to_str().unwrap(),
    ];
    a.extend_from_slice(extra);
    stw(home, &a)
}

fn line(repo: &Path, branch: &str, sha: &str) -> String {
    format!(
        r#"{{"manifest_version":1,"run":"x","repo":{},"branch":{},"sha":"{sha}","proof":"merged-ancestor","time":1,"worktree":null}}"#,
        serde_json::to_string(&repo.to_str().unwrap()).unwrap(),
        serde_json::to_string(branch).unwrap()
    )
}

fn manifest(dir: &Path, run: &str, lines: &[String]) {
    std::fs::create_dir_all(dir).unwrap();
    std::fs::write(dir.join(format!("{run}.jsonl")), lines.join("\n") + "\n").unwrap();
}

fn entry<'a>(v: &'a Value, run: &str) -> &'a Value {
    v["entries"]
        .as_array()
        .unwrap()
        .iter()
        .find(|e| e["run"] == run)
        .unwrap_or_else(|| panic!("no {run} in {v}"))
}

fn tree(dir: &Path) -> BTreeMap<String, Vec<u8>> {
    fn walk(base: &Path, dir: &Path, out: &mut BTreeMap<String, Vec<u8>>) {
        for e in std::fs::read_dir(dir).unwrap() {
            let p = e.unwrap().path();
            let rel = p.strip_prefix(base).unwrap().display().to_string();
            if p.is_dir() {
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

#[test]
fn an_empty_or_missing_manifest_directory_is_an_empty_history() {
    let home = scratch();
    let empty = home.path().join("m");
    std::fs::create_dir(&empty).unwrap();
    for dir in [empty, home.path().join("never-made")] {
        let o = history(home.path(), &dir, &[]);
        assert_eq!(
            o.status.code(),
            Some(0),
            "{}",
            String::from_utf8_lossy(&o.stderr)
        );
        let v = json(&o);
        assert_eq!(v["command"], "tidy-history");
        assert_eq!(v["schema_version"], 2);
        assert_eq!(v["read_only"], true);
        assert_eq!(v["status"], "ok");
        assert_eq!(v["total"], 0);
        assert_eq!(v["entries"], serde_json::json!([]));
        assert_eq!(v["truncated"], serde_json::json!([]));
        assert_eq!(v["scan_truncated"], false);
        assert_eq!(v["manifest_dir"], dir.to_str().unwrap());
    }
    let text = String::from_utf8(
        stw(
            home.path(),
            &[
                "tidy",
                "history",
                "--manifest-dir",
                home.path().join("m").to_str().unwrap(),
            ],
        )
        .stdout,
    )
    .unwrap();
    assert!(text.starts_with("0 journal(s) in "), "{text}");
}

#[test]
fn a_run_of_tidy_apply_is_listed_with_its_counts_and_restore_reads_the_same_file() {
    let fx = build();
    let home = scratch();
    let r = path_str(&fx.path);
    let md = home.path().join("m");
    let apply = json(&stw(
        home.path(),
        &[
            "tidy",
            "apply",
            "--offline",
            "--json",
            "--root",
            &r,
            "--manifest-dir",
            md.to_str().unwrap(),
        ],
    ));
    let run = apply["run"].as_str().unwrap().to_string();
    let files = tree(&md);
    let refs = tree(&fx.path.join(".git/refs"));
    let o = history(home.path(), &md, &[]);
    assert_eq!(o.status.code(), Some(0));
    let v = json(&o);
    assert_eq!(v["total"], 1);
    let e = entry(&v, &run);
    assert_eq!(e["deleted"], 2, "{e}");
    assert_eq!(e["restorable"], 2, "{e}");
    assert_eq!(e["status"], "complete");
    assert_eq!(e["mode"], "local-refs");
    assert!(e["time"].as_str().unwrap().ends_with('Z'));
    // read-only: manifest directory and repository are byte-identical
    assert_eq!(tree(&md), files);
    assert_eq!(tree(&fx.path.join(".git/refs")), refs);
    // the manifest format is the one `tidy restore` reads: restore, then nothing is restorable any more
    let restored = json(&stw(
        home.path(),
        &[
            "tidy",
            "restore",
            "--json",
            "--manifest-dir",
            md.to_str().unwrap(),
        ],
    ));
    assert_eq!(restored["summary"]["restored"], 2);
    let v = json(&history(home.path(), &md, &[]));
    assert_eq!(entry(&v, &run)["deleted"], 2);
    assert_eq!(entry(&v, &run)["restorable"], 0, "the branches exist again");
}

#[test]
fn the_text_has_one_row_per_journal() {
    let home = scratch();
    let md = home.path().join("m");
    let fx = build();
    manifest(
        &md,
        "0000000000002-1-0",
        &[line(&fx.path, "gone", &fx.tip("merged-a").to_string())],
    );
    let o = stw(
        home.path(),
        &["tidy", "history", "--manifest-dir", md.to_str().unwrap()],
    );
    let text = String::from_utf8(o.stdout).unwrap();
    assert!(
        text.starts_with(&format!("1 journal(s) in {}, newest first\n", md.display())),
        "{text}"
    );
    let row = text.lines().nth(1).unwrap();
    assert!(
        row.contains("0000000000002-1-0")
            && row.contains("deleted 1")
            && row.contains("restorable 1")
            && row.ends_with("complete"),
        "{row}"
    );
}

#[test]
fn journals_come_newest_first_and_the_limit_is_a_visible_truncation() {
    let home = scratch();
    let md = home.path().join("m");
    let fx = build();
    for (i, run) in [
        "0000000000001-1-0",
        "0000000000003-1-0",
        "0000000000002-1-0",
    ]
    .iter()
    .enumerate()
    {
        let lines: Vec<String> = (0..=i)
            .map(|n| line(&fx.path, &format!("b{n}"), &fx.tip("main").to_string()))
            .collect();
        manifest(&md, run, &lines);
    }
    let v = json(&history(home.path(), &md, &[]));
    let runs: Vec<&str> = v["entries"]
        .as_array()
        .unwrap()
        .iter()
        .map(|e| e["run"].as_str().unwrap())
        .collect();
    assert_eq!(
        runs,
        [
            "0000000000003-1-0",
            "0000000000002-1-0",
            "0000000000001-1-0"
        ]
    );
    assert_eq!(v["truncated"], serde_json::json!([]));
    let v = json(&history(home.path(), &md, &["--limit", "2"]));
    assert_eq!(v["entries"].as_array().unwrap().len(), 2);
    assert_eq!(v["total"], 3);
    assert_eq!(
        v["truncated"],
        serde_json::json!([{"unit": "journals", "shown": 2, "total": 3}])
    );
    let text = String::from_utf8(
        stw(
            home.path(),
            &[
                "tidy",
                "history",
                "--limit",
                "2",
                "--manifest-dir",
                md.to_str().unwrap(),
            ],
        )
        .stdout,
    )
    .unwrap();
    assert!(text.contains("truncated: journals 2 of 3"), "{text}");
}

#[test]
fn a_bad_limit_or_a_repeated_flag_is_a_usage_error() {
    let home = scratch();
    let md = home.path().join("m");
    std::fs::create_dir(&md).unwrap();
    for extra in [
        vec!["--limit", "0"],
        vec!["--limit", "-1"],
        vec!["--limit", "1025"],
        vec!["--limit", "x"],
        vec!["--limit", "1", "--limit", "1"],
        vec!["--json", "--json"],
        vec!["--contract", "3"],
        vec!["--plan", "x"],
        vec!["--apply"],
    ] {
        let o = history(home.path(), &md, &extra);
        assert_eq!(
            o.status.code(),
            Some(2),
            "{extra:?}: {}",
            String::from_utf8_lossy(&o.stderr)
        );
        assert!(o.stdout.is_empty(), "{extra:?}");
    }
    let ok = history(home.path(), &md, &["--limit", "1024"]);
    assert_eq!(ok.status.code(), Some(0));
}

#[test]
fn the_counts_follow_the_repository() {
    let home = scratch();
    let md = home.path().join("m");
    let fx = build();
    let c0 = fx.tip("merged-a").to_string();
    let missing = "1".repeat(40);
    manifest(
        &md,
        "0000000000001-1-0",
        &[
            line(&fx.path, "free", &c0),                           // restorable
            line(&fx.path, "main", &c0),                           // the name is taken
            line(&fx.path, "gone-object", &missing),               // the commit is gone
            line(&fx.path, "bad sha", "xyz"),                      // not a commit id
            line(&fx.path, "bad..name", &c0),                      // not a branch name
            line(&fx.path, "-x", &c0),                             // an option-like name
            line(Path::new("relative/repo"), "rel", &c0),          // not an absolute path
            line(Path::new("/nonexistent/stw-history"), "x", &c0), // not a repository
            line(&fx.path, "UPPER", &c0.to_uppercase()),           // not lower-case hex
        ],
    );
    let v = json(&history(home.path(), &md, &[]));
    let e = entry(&v, "0000000000001-1-0");
    assert_eq!(
        (e["deleted"].as_u64(), e["restorable"].as_u64()),
        (Some(9), Some(1)),
        "{e}"
    );
    assert_eq!(e["status"], "complete");
}

#[test]
fn an_unreadable_manifest_is_listed_and_foreign_files_are_not() {
    let home = scratch();
    let md = home.path().join("m");
    let fx = build();
    let c0 = fx.tip("merged-a").to_string();
    manifest(&md, "0000000000001-1-0", &[line(&fx.path, "free", &c0)]);
    manifest(&md, "0000000000002-1-0", &["not json".to_string()]);
    manifest(
        &md,
        "0000000000003-1-0",
        &[r#"{"run":"x","repo":"/r"}"#.to_string()],
    );
    // not manifests: other extension, a directory, a link, a hidden name, an escape sequence in the name
    std::fs::write(md.join("notes.txt"), "x").unwrap();
    std::fs::create_dir(md.join("0000000000004-1-0.jsonl")).unwrap();
    std::fs::write(md.join(".hidden.jsonl"), "x").unwrap();
    std::fs::write(md.join("bad\u{1b}[31mname.jsonl"), "x").unwrap();
    #[cfg(unix)]
    std::os::unix::fs::symlink(
        md.join("0000000000001-1-0.jsonl"),
        md.join("0000000000005-1-0.jsonl"),
    )
    .unwrap();
    let o = history(home.path(), &md, &[]);
    assert_eq!(o.status.code(), Some(0));
    let v = json(&o);
    assert_eq!(v["total"], 3, "{v}");
    for run in ["0000000000002-1-0", "0000000000003-1-0"] {
        let e = entry(&v, run);
        assert_eq!(
            (
                e["status"].as_str(),
                e["deleted"].as_u64(),
                e["restorable"].as_u64(),
                e["mode"].as_str()
            ),
            (Some("unreadable"), Some(0), Some(0), Some("unknown")),
            "{e}"
        );
    }
    assert_eq!(entry(&v, "0000000000001-1-0")["status"], "complete");
    let text = String::from_utf8(
        stw(
            home.path(),
            &["tidy", "history", "--manifest-dir", md.to_str().unwrap()],
        )
        .stdout,
    )
    .unwrap();
    assert!(!text.contains('\u{1b}'), "{text:?}");
}

#[test]
fn an_escape_sequence_in_the_directory_name_is_escaped_in_the_text() {
    let home = scratch();
    let md = home.path().join("m\u{1b}[31m\u{202e}x");
    std::fs::create_dir(&md).unwrap();
    let text = String::from_utf8(
        stw(
            home.path(),
            &["tidy", "history", "--manifest-dir", md.to_str().unwrap()],
        )
        .stdout,
    )
    .unwrap();
    assert!(
        !text.contains('\u{1b}') && !text.contains('\u{202e}'),
        "{text:?}"
    );
    assert!(text.contains("\\u{1b}"), "{text}");
}

#[test]
fn every_bound_is_a_visible_marker() {
    let home = scratch();
    let md = home.path().join("m");
    let fx = build();
    let c0 = fx.tip("merged-a").to_string();
    manifest(
        &md,
        "0000000000001-1-0",
        &[line(&fx.path, "a", &c0), line(&fx.path, "b", &c0)],
    );
    manifest(&md, "0000000000002-1-0", &[line(&fx.path, "c", &c0)]);
    manifest(&md, "0000000000003-1-0", &[line(&fx.path, "d", &c0)]);
    // scan bound: two of three names are looked at
    let h = history_with(&md, 10, 2, 100, Duration::from_secs(30)).unwrap();
    assert!(h.scan_truncated);
    assert_eq!(h.total, 2);
    assert!(
        h.truncated.iter().any(|t| t.unit == "journals"),
        "{:?}",
        h.truncated
    );
    // entry bound: the 2-entry manifest is over a bound of 2 once another one was counted
    let h = history_with(&md, 10, 100, 2, Duration::from_secs(30)).unwrap();
    let over: Vec<_> = h
        .entries
        .iter()
        .filter(|e| e.status == "incomplete")
        .collect();
    assert_eq!(over.len(), 1, "{:?}", h.entries);
    assert_eq!(over[0].deleted, 2, "listed with its count, not checked");
    assert_eq!(over[0].restorable, 0);
    assert!(
        h.truncated
            .iter()
            .any(|t| t.unit == "objects" && t.total > t.shown),
        "{:?}",
        h.truncated
    );
    // time bound: nothing is checked, every row says so
    let h = history_with(&md, 10, 100, 100, Duration::ZERO).unwrap();
    assert!(h.entries.iter().all(|e| e.status == "incomplete"));
    assert!(
        h.truncated
            .iter()
            .any(|t| t.unit == "checked-journals" && t.shown == 0 && t.total == 3),
        "{:?}",
        h.truncated
    );
}

#[test]
fn history_calls_no_git_gh_or_ssh_and_needs_no_workspace() {
    // a process with an empty PATH and no repository around: gix reads only
    let home = scratch();
    let md = home.path().join("m");
    let fx = build();
    manifest(
        &md,
        "0000000000001-1-0",
        &[line(&fx.path, "free", &fx.tip("merged-a").to_string())],
    );
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("HOME", home.path())
        .current_dir(home.path())
        .args([
            "tidy",
            "history",
            "--json",
            "--manifest-dir",
            md.to_str().unwrap(),
        ])
        .output()
        .unwrap();
    assert_eq!(
        o.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&o.stderr)
    );
    assert_eq!(entry(&json(&o), "0000000000001-1-0")["restorable"], 1);
}
