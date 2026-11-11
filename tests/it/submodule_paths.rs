//! `.gitmodules` paths are untrusted: a path that is not a plain directory path below the workspace is never a
//! repository of `status`, `sync` or `tidy plan`; it is reported as its own entry (escaped) and nothing there is
//! read or written. Unit part: `repo::path_rejection`, `repo::list_repos`.
//!
//! Harness confinement: everything lives in a tempdir of `World` (git and stw run with a scrubbed environment).
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::Value;

use crate::sync_flags::World;

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

/// `.gitmodules` text for the paths (each already in git-config quoted form).
fn write_gitmodules(ws: &Path, paths: &[&str]) {
    let mut s = String::new();
    for (i, p) in paths.iter().enumerate() {
        s += &format!("[submodule \"m{i}\"]\n\tpath = {p}\n\turl = ../o.git\n");
    }
    std::fs::write(ws.join(".gitmodules"), s).unwrap();
}

const ESC_NAME: &str = "x\u{1b}[31mRED";

/// Hostile entries: the display name (as `.gitmodules` yields it) and the `.gitmodules` spelling.
fn hostile(t: &Ws) -> Vec<(String, String)> {
    let abs = t.outside.to_str().unwrap().to_string();
    // directories that WOULD be repositories if the path were accepted
    for d in ["-opt", ESC_NAME, "nl\nINJECTED", "sp ace"] {
        t.w.git(&t.w.base, &["clone", "-q", "o.git", &format!("ws/{d}")]);
    }
    std::os::unix::fs::symlink(&t.outside, t.ws.join("link")).unwrap();
    vec![
        ("../outside".into(), "../outside".into()),
        (abs.clone(), abs),
        ("-opt".into(), "-opt".into()),
        ("link".into(), "link".into()),
        (ESC_NAME.into(), ESC_NAME.into()),
        ("nl\nINJECTED".into(), "\"nl\\nINJECTED\"".into()),
        ("sp ace".into(), "sp ace".into()),
    ]
}

fn setup_hostile() -> (Ws, Vec<(String, String)>) {
    let t = setup();
    let h = hostile(&t);
    let mut spell: Vec<&str> = h.iter().map(|(_, s)| s.as_str()).collect();
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
    assert_eq!(rej, h.into_iter().map(|(n, _)| n).collect::<Vec<_>>());
}

#[test]
fn sync_dry_run_reports_refused_paths_and_touches_nothing() {
    let (t, h) = setup_hostile();
    let (code, text, v) = stw(&t, &["sync"]);
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
    assert!(
        text.contains("submodule path not accepted: ../outside"),
        "{text}"
    );
    no_raw_controls(&text);
    outside_unchanged(&t);
}

#[test]
fn sync_apply_fails_on_refused_paths_and_leaves_their_repositories_alone() {
    let (t, h) = setup_hostile();
    let (code, text, v) = stw(&t, &["sync", "--apply"]);
    assert_eq!(code, 1, "{text}");
    for (name, _) in &h {
        let r = entry(&v, name);
        assert_eq!(r["result"], "error", "{r}");
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
    let (code, text, v) = stw(&t, &["sync", "--apply", "--submodules", "--offline"]);
    assert_eq!(code, 1, "{text}");
    for (name, _) in &h {
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

#[test]
fn status_reports_refused_paths_and_does_not_read_them() {
    let (t, h) = setup_hostile();
    let (code, text, v) = stw(&t, &["status", "--offline"]);
    assert_eq!(code, 1, "{text}");
    for (name, _) in &h {
        let r = entry(&v, name);
        assert_eq!(r["state"], "error", "{r}");
        assert_eq!(r["path"], "", "{r}");
        assert!(
            r["error"]
                .as_str()
                .unwrap()
                .contains("submodule path not accepted"),
            "{r}"
        );
        assert_eq!(r["branch"], Value::Null, "{r}");
    }
    assert_eq!(entry(&v, "a/b")["state"], "ok");
    assert!(
        text.contains("submodule path not accepted: ../outside"),
        "{text}"
    );
    no_raw_controls(&text);
    outside_unchanged(&t);
}

#[test]
fn tidy_plan_reports_refused_paths_and_does_not_read_them() {
    let (t, h) = setup_hostile();
    let (code, text, v) = stw(&t, &["tidy", "plan", "--offline"]);
    assert_eq!(code, 1, "{text}");
    // the name of such an entry is the escaped text of the path
    let refused: Vec<&Value> = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|r| {
            r["error"]
                .as_str()
                .is_some_and(|e| e.contains("submodule path not accepted"))
        })
        .collect();
    assert_eq!(refused.len(), h.len(), "{v}");
    for r in refused {
        assert_eq!(r["path"], "", "{r}");
        no_raw_controls(r["name"].as_str().unwrap());
    }
    assert!(entry(&v, "a/b")["error"].is_null());
    assert!(
        text.contains("submodule path not accepted: ../outside"),
        "{text}"
    );
    no_raw_controls(&text);
    outside_unchanged(&t);
}

#[test]
fn a_workspace_of_plain_paths_has_no_refused_entry() {
    let t = setup();
    write_gitmodules(&t.ws, &["a/b"]);
    let (code, text, v) = stw(&t, &["sync", "--offline"]);
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

#[test]
fn tidy_apply_reports_refused_paths_and_does_not_touch_them() {
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
    assert_eq!(code, 1, "{text}");
    let refused = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|r| {
            r["error"]
                .as_str()
                .is_some_and(|e| e.contains("submodule path not accepted"))
        })
        .count();
    assert_eq!(refused, h.len(), "{v}");
    assert!(
        text.contains("submodule path not accepted: ../outside"),
        "{text}"
    );
    no_raw_controls(&text);
    outside_unchanged(&t);
}
