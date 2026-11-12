//! The repositories of `status`, `sync`, `tidy plan` and `tidy apply` are the gitlinks of the index. A gitlink path is
//! untrusted: one that is not a plain directory path below the workspace is never a repository; it is reported as its
//! own entry (escaped) and nothing there is read or written. Unit part: `repo::path_rejection`, `repo::list_repos`.
//!
//! Harness confinement: everything lives in a tempdir of `World` (git and stw run with a scrubbed environment).
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::Value;

use crate::sync_flags::World;

impl Ws {
    fn names(&self) -> Vec<String> {
        stw::repo::list_repos(&self.ws)
            .into_iter()
            .map(|r| r.name)
            .collect()
    }
}

struct Ws {
    w: World,
    ws: PathBuf,
    outside: PathBuf,
    o_head: String,
    o_origin_ref: String,
}

/// `ws` (the workspace, with one valid nested submodule checkout `a/b`) next to `outside`, a clone of a bare
/// origin that has a newer commit: a fetch plus fast-forward there would move HEAD and `origin/main`.
fn setup() -> Ws {
    let w = World::new();
    let up = w.p("up");
    std::fs::create_dir(&up).unwrap();
    w.git(&up, &["init", "-q"]);
    w.commit(&up, "f", "1\n");
    w.git(&w.base, &["clone", "-q", "--bare", "up", "o.git"]);
    w.git(&w.base, &["clone", "-q", "o.git", "outside"]);
    w.commit(&up, "f", "2\n");
    w.git(&up, &["push", "-q", w.p("o.git").to_str().unwrap(), "main"]);
    let outside = w.p("outside");
    let o_head = w.git(&outside, &["rev-parse", "HEAD"]);
    let o_origin_ref = w.git(&outside, &["rev-parse", "refs/remotes/origin/main"]);
    let ws = w.p("ws");
    std::fs::create_dir(&ws).unwrap();
    w.git(&ws, &["init", "-q"]);
    w.commit(&ws, "r", "r\n");
    std::fs::create_dir_all(ws.join("a")).unwrap();
    w.git(&w.base, &["clone", "-q", "o.git", "ws/a/b"]);
    Ws {
        w,
        ws,
        outside,
        o_head,
        o_origin_ref,
    }
}

/// `.gitmodules` text for the paths (each already in git-config quoted form), and a gitlink in the index for
/// each of them (the repositories of a workspace are the gitlinks; a trailing `/` is no part of an index path,
/// so `a/b` and `a/b/` are one gitlink).
fn write_gitmodules(ws: &Path, paths: &[&str]) {
    let mut s = String::new();
    for (i, p) in paths.iter().enumerate() {
        s += &format!("[submodule \"m{i}\"]\n\tpath = {p}\n\turl = ../o.git\n");
    }
    std::fs::write(ws.join(".gitmodules"), s).unwrap();
    for p in paths {
        let display = match p.strip_prefix('"').and_then(|q| q.strip_suffix('"')) {
            Some(q) => q.replace("\\n", "\n"),
            None => p.to_string(),
        };
        let display = display.strip_suffix('/').unwrap_or(&display);
        // a path that git cannot hold (absolute, with a `..` segment) stays in `.gitmodules` only: it is no
        // gitlink, so no repository, and is never read (see `paths_that_only_gitmodules_names_are_ignored`)
        if !display.starts_with('/') && !display.split('/').any(|c| c == "..") {
            crate::common::stage_gitlink_in(ws, display);
        }
    }
}

const ESC_NAME: &str = "x\u{1b}[31mRED";

/// Hostile entries: the display name (as `.gitmodules` yields it) and the `.gitmodules` spelling.
fn hostile(t: &Ws) -> Vec<(String, String)> {
    // directories that WOULD be repositories if the path were accepted
    for d in ["-opt", ESC_NAME, "nl\nINJECTED", "sp ace"] {
        t.w.git(&t.w.base, &["clone", "-q", "o.git", &format!("ws/{d}")]);
    }
    std::os::unix::fs::symlink(&t.outside, t.ws.join("link")).unwrap();
    vec![
        ("-opt".into(), "-opt".into()),
        ("link".into(), "link".into()),
        (ESC_NAME.into(), ESC_NAME.into()),
        ("nl\nINJECTED".into(), "\"nl\\nINJECTED\"".into()),
        ("sp ace".into(), "sp ace".into()),
        // no directory: only the report of the refused path may name it (not "not initialized")
        ("no checkout".into(), "no checkout".into()),
    ]
}

fn setup_hostile() -> (Ws, Vec<(String, String)>) {
    let t = setup();
    let h = hostile(&t);
    let abs = t.outside.to_str().unwrap().to_string();
    let mut spell: Vec<&str> = h.iter().map(|(_, s)| s.as_str()).collect();
    spell.push("../outside");
    spell.push(&abs);
    spell.push("a/b");
    write_gitmodules(&t.ws, &spell);
    (t, h)
}

fn stw(t: &Ws, args: &[&str]) -> (i32, String, Value) {
    let run = |json: bool| {
        let mut c = Command::new(env!("CARGO_BIN_EXE_stw"));
        c.env_clear()
            .env("PATH", std::env::var_os("PATH").unwrap())
            .env("HOME", &t.w.home)
            .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
            .current_dir(&t.w.base)
            .args(args)
            .arg("--root")
            .arg(&t.ws);
        if json {
            c.arg("--json");
        }
        c.output().unwrap()
    };
    let text = run(false);
    let j = run(true);
    let out = String::from_utf8_lossy(&text.stdout).into_owned();
    if !args.contains(&"--apply") {
        // an apply changes the workspace: its second run may differ
        assert_eq!(text.status.code(), j.status.code(), "text and json differ");
    }
    (
        j.status.code().unwrap_or(-1),
        out,
        serde_json::from_slice(&j.stdout).unwrap_or(Value::Null),
    )
}

fn entry<'a>(v: &'a Value, name: &str) -> &'a Value {
    v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .find(|r| r["name"] == name)
        .unwrap_or_else(|| panic!("no entry {name:?} in {v}"))
}

fn outside_unchanged(t: &Ws) {
    assert_eq!(t.w.git(&t.outside, &["rev-parse", "HEAD"]), t.o_head);
    assert_eq!(
        t.w.git(&t.outside, &["rev-parse", "refs/remotes/origin/main"]),
        t.o_origin_ref
    );
    assert!(!t.outside.join(".git/FETCH_HEAD").exists());
}

fn no_raw_controls(text: &str) {
    assert!(
        !text.chars().any(|c| c == '\u{1b}'),
        "raw ESC in output: {text:?}"
    );
    assert!(!text.contains("\nINJECTED"), "newline injection: {text:?}");
}

#[test]
fn path_rejection_rules() {
    let t = setup();
    std::os::unix::fs::symlink(&t.outside, t.ws.join("link")).unwrap();
    std::os::unix::fs::symlink(".", t.ws.join("dot")).unwrap();
    let rej = |p: &str| stw::repo::path_rejection(&t.ws, p);
    for p in [
        "../outside",
        "/abs",
        "-opt",
        "a/../../outside",
        "x\u{1b}y",
        "nl\nx",
        "sp ace",
        "link",
        "dot/a",
        ".git/x",
        "",
    ] {
        assert!(rej(p).is_some(), "{p:?} must be rejected");
    }
    assert!(rej("a/b").is_none());
    assert!(rej("plain-name_1.x").is_none());
    assert!(rej("not/yet/there").is_none());
}

#[test]
fn list_repos_leaves_out_refused_paths_and_keeps_valid_ones() {
    let (t, h) = setup_hostile();
    let names: Vec<String> = stw::repo::list_repos(&t.ws)
        .into_iter()
        .map(|r| r.name)
        .collect();
    assert_eq!(names, vec!["ws".to_string(), "a/b".to_string()]);
    let rej: Vec<String> = stw::repo::rejected_paths(&t.ws)
        .into_iter()
        .map(|(p, _)| p)
        .collect();
    // the repositories are the gitlinks of the index, in path order
    let mut want: Vec<String> = h.into_iter().map(|(n, _)| n).collect();
    want.sort();
    assert_eq!(rej, want);
}

/// Git cannot hold an absolute path or a `..` segment in an index, and a clone brings no index: a `.gitmodules`
/// that names one has no gitlink for it, so it is no repository and no entry of any command (Go ignores it too).
#[test]
fn paths_that_only_gitmodules_names_are_ignored() {
    let (t, _) = setup_hostile();
    let abs = t.outside.to_str().unwrap().to_string();
    let rej: Vec<String> = stw::repo::rejected_paths(&t.ws)
        .into_iter()
        .map(|(p, _)| p)
        .collect();
    assert!(
        !rej.contains(&"../outside".to_string()) && !rej.contains(&abs),
        "{rej:?}"
    );
    assert!(!t.names().iter().any(|n| n == "../outside" || *n == abs));
    let (code, text, v) = stw(&t, &["sync", "--submodules", "--offline"]);
    assert_eq!(code, 1, "{text}");
    for name in ["../outside", abs.as_str()] {
        assert!(
            v["repos"]
                .as_array()
                .unwrap()
                .iter()
                .all(|r| r["name"] != name),
            "{name}: {v}"
        );
    }
    outside_unchanged(&t);
}

#[test]
fn sync_dry_run_reports_refused_paths_and_touches_nothing() {
    let (t, h) = setup_hostile();
    let (code, text, v) = stw(&t, &["sync", "--plan"]);
    assert_eq!(code, 0, "{text}");
    for (name, _) in &h {
        let r = entry(&v, name);
        assert_eq!(r["result"], "skip", "{r}");
        let reason = r["reason"].as_str().unwrap();
        assert!(reason.contains("submodule path not accepted"), "{r}");
        assert_eq!(r["path"], "", "{r}");
        no_raw_controls(reason);
    }
    let ok = entry(&v, "a/b");
    assert!(!ok["reason"].as_str().unwrap().contains("not accepted"));
    assert!(!ok["path"].as_str().unwrap().is_empty());
    assert!(text.contains("submodule path not accepted: -opt"), "{text}");
    no_raw_controls(&text);
    outside_unchanged(&t);
}

#[test]
fn sync_apply_hints_at_refused_paths_and_leaves_their_repositories_alone() {
    let (t, h) = setup_hostile();
    let (code, text, v) = stw(&t, &["sync"]);
    // without --submodules a refused path is a hint, not a failure (Go's `unreachable` arises only there)
    assert_eq!(code, 0, "{text}");
    for (name, _) in &h {
        let r = entry(&v, name);
        assert_eq!(r["result"], "skip", "{r}");
        assert!(
            r["reason"]
                .as_str()
                .unwrap()
                .contains("submodule path not accepted"),
            "{r}"
        );
        assert_eq!(r["fetch"], "not-run", "{r}");
    }
    no_raw_controls(&text);
    outside_unchanged(&t);
    // the valid nested checkout is still synced (fetched: origin/main of a/b now has the new commit)
    let ab = t.ws.join("a/b");
    assert_ne!(
        t.w.git(&ab, &["rev-parse", "refs/remotes/origin/main"]),
        t.o_origin_ref
    );
}

#[test]
fn sync_with_submodules_names_each_refused_path_once() {
    let (t, h) = setup_hostile();
    let (code, text, v) = stw(&t, &["sync", "--submodules", "--offline"]);
    assert_eq!(code, 1, "{text}");
    for (name, _) in &h {
        assert_eq!(entry(&v, name)["result"], "error");
        let n = v["repos"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|r| r["name"] == name.as_str())
            .count();
        assert_eq!(n, 1, "{name:?}");
    }
    outside_unchanged(&t);
}

/// The refused paths of a JSON document: `rejected_paths` [{path, reason}], one per hostile path, escaped.
fn assert_rejected_field(v: &Value, n: usize) {
    let r = v["rejected_paths"]
        .as_array()
        .unwrap_or_else(|| panic!("no rejected_paths in {v}"));
    assert_eq!(r.len(), n, "{v}");
    for e in r {
        no_raw_controls(e["path"].as_str().unwrap());
        assert!(!e["reason"].as_str().unwrap().is_empty());
    }
    assert!(r.iter().any(|e| e["path"] == "-opt"), "{v}");
}

#[test]
fn sync_submodules_dry_run_predicts_unreachable_as_a_skip() {
    let (t, h) = setup_hostile();
    let (code, text, v) = stw(&t, &["sync", "--plan", "--submodules", "--offline"]);
    assert_eq!(code, 0, "{text}");
    for (name, _) in &h {
        let r = entry(&v, name);
        assert_eq!(r["result"], "skip", "{r}");
        assert!(
            r["reason"]
                .as_str()
                .unwrap()
                .starts_with("would be unreachable: "),
            "{r}"
        );
    }
    outside_unchanged(&t);
}

#[test]
fn status_notes_refused_paths_without_a_repository_entry() {
    let (t, h) = setup_hostile();
    let (code, text, v) = stw(&t, &["status", "--offline"]);
    assert_eq!(code, 0, "{text}");
    let names: Vec<&str> = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .map(|r| r["name"].as_str().unwrap())
        .collect();
    assert_eq!(names, ["ws", "a/b"], "{v}");
    assert_eq!(v["summary"]["total"], 2, "{v}");
    assert_rejected_field(&v, h.len());
    assert_eq!(
        text.matches("  submodule path not accepted: ").count(),
        h.len(),
        "{text}"
    );
    assert!(text.contains("submodule path not accepted: -opt"), "{text}");
    assert!(
        text.contains("rename with git mv; stw handles only [A-Za-z0-9._/-]"),
        "{text}"
    );
    assert!(text.contains("Repositories (2)"), "{text}");
    no_raw_controls(&text);
    outside_unchanged(&t);
}

#[test]
fn tidy_plan_notes_refused_paths_without_a_repository_entry() {
    let (t, h) = setup_hostile();
    let (code, text, v) = stw(&t, &["tidy", "plan", "--offline"]);
    assert_eq!(code, 0, "{text}");
    assert_eq!(v["repos"].as_array().unwrap().len(), 2, "{v}");
    assert_rejected_field(&v, h.len());
    assert!(
        text.contains("(submodule path not accepted: -opt"),
        "{text}"
    );
    no_raw_controls(&text);
    outside_unchanged(&t);
}

#[test]
fn tidy_apply_notes_refused_paths_and_does_not_touch_them() {
    let (t, h) = setup_hostile();
    let dir = t.w.p("manifests");
    let (code, text, v) = stw(
        &t,
        &[
            "tidy",
            "apply",
            "--offline",
            "--manifest-dir",
            dir.to_str().unwrap(),
        ],
    );
    assert_eq!(code, 0, "{text}");
    assert_eq!(v["repos"].as_array().unwrap().len(), 2, "{v}");
    assert_rejected_field(&v, h.len());
    assert!(
        text.contains("(submodule path not accepted: -opt"),
        "{text}"
    );
    no_raw_controls(&text);
    outside_unchanged(&t);
}

#[test]
fn repo_names_decide_whether_refused_paths_are_reported() {
    let (t, _) = setup_hostile();
    let (code, text, v) = stw(&t, &["status", "--offline", "--repo", "a/b"]);
    assert_eq!(code, 0, "{text}");
    assert_eq!(v["rejected_paths"].as_array().map(Vec::len), Some(0), "{v}");
    assert!(!text.contains("not accepted"), "{text}");
    let (code, text, _) = stw(&t, &["status", "--offline", "--repo", "link"]);
    assert_eq!(code, 2, "{text}");
}

#[test]
fn a_gitlink_is_listed_and_synced_once() {
    let t = setup();
    write_gitmodules(&t.ws, &["a/b"]);
    assert_eq!(t.names(), ["ws", "a/b"]);
    let (code, text, v) = stw(&t, &["sync", "--plan", "--offline"]);
    assert_eq!(code, 0, "{text}");
    assert_eq!(v["repos"].as_array().unwrap().len(), 2, "{v}");
    let (code, text, _) = stw(&t, &["sync", "--plan", "--offline", "--repo", "a/b"]);
    assert_eq!(code, 0, "{text}");
    // a refused path is reported once
    write_gitmodules(&t.ws, &["x y"]);
    assert_eq!(stw::repo::rejected_paths(&t.ws).len(), 1);
}

#[test]
fn a_submodule_named_like_the_root_is_listed_and_a_refused_one_is_reported() {
    let t = setup();
    t.w.git(&t.w.base, &["clone", "-q", "o.git", "ws/ws"]);
    write_gitmodules(&t.ws, &["ws"]);
    let names: Vec<(String, bool)> = stw::repo::list_repos(&t.ws)
        .into_iter()
        .map(|r| (r.name, r.path == t.ws))
        .collect();
    assert_eq!(names, [("ws".into(), true), ("ws".into(), false)]);
    // the same name, but through a symbolic link to the outside: refused, and shown the same way everywhere
    std::fs::remove_dir_all(t.ws.join("ws")).unwrap();
    std::os::unix::fs::symlink(&t.outside, t.ws.join("ws")).unwrap();
    assert_eq!(stw::repo::list_repos(&t.ws).len(), 1);
    let (code, text, v) = stw(&t, &["sync"]);
    assert_eq!(code, 0, "{text}");
    assert!(
        v["repos"]
            .as_array()
            .unwrap()
            .iter()
            .any(|r| r["result"] == "skip"
                && r["reason"]
                    .as_str()
                    .is_some_and(|x| x.contains("symbolic link"))),
        "{v}"
    );
    let (code, text, v) = stw(&t, &["status", "--offline"]);
    assert_eq!(code, 0, "{text}");
    let r = v["rejected_paths"].as_array().unwrap();
    assert_eq!(r.len(), 1, "{v}");
    assert!(
        r[0]["reason"].as_str().unwrap().contains("symbolic link"),
        "{v}"
    );
    outside_unchanged(&t);
}

#[test]
fn a_workspace_of_plain_paths_has_no_refused_entry() {
    let t = setup();
    write_gitmodules(&t.ws, &["a/b"]);
    let (code, text, v) = stw(&t, &["sync", "--plan", "--offline"]);
    assert_eq!(code, 0, "{text}");
    assert_eq!(v["repos"].as_array().unwrap().len(), 2, "{v}");
    let (code, text, v) = stw(&t, &["status", "--offline"]);
    assert_eq!(code, 0, "{text}");
    assert_eq!(v["repos"].as_array().unwrap().len(), 2, "{v}");
}

#[test]
fn below_means_strictly_below() {
    let below = |a: &str, b: &str| stw::repo::is_below(Path::new(a), Path::new(b));
    assert!(below("/w/root/a/b", "/w/root"));
    assert!(!below("/w/root", "/w/root"));
    assert!(!below("/w/outside", "/w/root"));
    assert!(!below("/w/root-x", "/w/root"));
    assert!(!below("/w", "/w/root"));
}

/// A refused path is a notice `rejected-path` (source `submodules`, no repository) in the text and the JSON: same
/// list, same order, no raw control character or bidi override in either.
#[test]
fn refused_paths_are_notices_in_the_json_too_with_hostile_characters_escaped() {
    let t = setup();
    write_gitmodules(&t.ws, &["-opt", "\"nl\\nx\"", "rtl\u{202e}x", ESC_NAME]);
    let (code, text, v) = stw(&t, &["status", "--offline"]);
    assert_eq!(code, 0, "{text}");
    let n = v["notices"].as_array().unwrap();
    assert_eq!(n.len(), 1 + 4, "{v}");
    assert_eq!(n[0]["code"], "offline");
    let lines: Vec<&str> = text
        .lines()
        .filter(|l| l.starts_with("  submodule path not accepted: "))
        .map(|l| l.trim_start())
        .collect();
    assert_eq!(lines.len(), 4, "{text}");
    for (x, l) in n[1..].iter().zip(&lines) {
        assert_eq!(x["code"], "rejected-path");
        assert_eq!(x["source"], "submodules");
        assert_eq!(x["repos"], serde_json::json!([]));
        assert_eq!(x["count"], 1);
        assert_eq!(x["message"].as_str().unwrap(), *l);
        let m = x["message"].as_str().unwrap();
        assert!(
            !m.chars().any(|c| c.is_control() || c == '\u{202e}'),
            "{m:?}"
        );
    }
    assert!(text.contains("Notices (5)\n"), "{text}");
    assert!(!text.contains('\u{202e}') && !text.contains('\u{1b}'));
}
