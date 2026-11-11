//! `stw status --json`: the top-level `notices` array. It is made from the same list as the text block
//! `Notices (N)`: one element per line, same order, same message. Codes: README.md, "Notices".
use std::process::{Command, Output};

use crate::common::*;
use crate::sync_flags::World;
use serde_json::Value;

const TOKEN: &str = "SECRETTOKEN-0123456789";

fn stw(token: bool, api: Option<&str>, args: &[&str]) -> Output {
    let mut c = Command::new(env!("CARGO_BIN_EXE_stw"));
    c.env_clear()
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service");
    if token {
        c.env("GH_TOKEN", TOKEN);
    }
    if let Some(a) = api {
        c.env("STW_GITHUB_API_URL", a);
    }
    c.args(args).output().unwrap()
}

fn out(o: &Output) -> String {
    String::from_utf8_lossy(&o.stdout).into_owned()
}

fn json_of(o: &Output) -> Value {
    serde_json::from_slice(&o.stdout).unwrap_or_else(|e| panic!("{e}: {}", out(o)))
}

/// The lines of the text block `Notices (N)` and N.
fn text_notices(t: &str) -> (usize, Vec<String>) {
    let b = t
        .split("\n\n")
        .find(|b| b.starts_with("Notices ("))
        .unwrap_or_else(|| panic!("no Notices block: {t}"));
    let mut lines = b.lines();
    let head = lines.next().unwrap();
    let n: usize = head
        .trim_start_matches("Notices (")
        .trim_end_matches(')')
        .parse()
        .unwrap();
    let rest: Vec<String> = lines
        .map(|l| l.trim_start().to_string())
        .filter(|l| l != "none")
        .collect();
    (n, rest)
}

/// Text and JSON of the same run (`args` without `--json`), checked against each other: the count, the order and
/// the messages are the same. Returns the `notices` array.
fn both(token: bool, api: Option<&str>, args: &[&str]) -> Vec<Value> {
    let t = out(&stw(token, api, args));
    let mut j = args.to_vec();
    j.push("--json");
    let o = stw(token, api, &j);
    let v = json_of(&o);
    let arr = v["notices"]
        .as_array()
        .unwrap_or_else(|| panic!("notices is not an array: {v}"))
        .clone();
    let (n, lines) = text_notices(&t);
    assert_eq!(n, arr.len(), "count: {t}\n{v}");
    let msgs: Vec<&str> = arr.iter().map(|x| x["message"].as_str().unwrap()).collect();
    assert_eq!(lines, msgs, "text lines and JSON messages differ");
    let raw = out(&o);
    assert!(
        !raw.chars().any(|c| c.is_control() && c != '\n'),
        "raw control character in {raw:?}"
    );
    for x in &arr {
        let mut k: Vec<&str> = x.as_object().unwrap().keys().map(String::as_str).collect();
        k.sort();
        assert_eq!(k, ["code", "count", "message", "repos", "source"], "{x}");
        assert!(x["code"].is_string() && x["source"].is_string());
        assert!(x["repos"].is_array() && x["count"].is_u64() && x["count"].as_u64() >= Some(1));
    }
    arr
}

fn codes(a: &[Value]) -> Vec<&str> {
    a.iter().map(|x| x["code"].as_str().unwrap()).collect()
}

#[test]
fn a_run_without_notices_has_an_empty_array_and_none_in_the_text() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = FakeApi::start(200, "[]");
    let a = both(true, Some(&api.url), &["status", "--root", &r]);
    assert!(a.is_empty(), "{a:?}");
    let o = stw(true, Some(&api.url), &["status", "--root", &r, "--json"]);
    assert_eq!(json_of(&o)["notices"], serde_json::json!([]));
    assert_eq!(json_of(&o)["schema_version"], 2);
    assert!(out(&stw(true, Some(&api.url), &["status", "--root", &r]))
        .contains("Notices (0)\n  none\n"));
}

#[test]
fn offline_and_no_token_have_the_go_codes_and_the_github_note_as_message() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let a = both(false, None, &["status", "--root", &r, "--offline"]);
    assert_eq!(codes(&a), ["offline"]);
    assert_eq!(a[0]["source"], "github");
    assert_eq!(a[0]["repos"], serde_json::json!([]));
    assert_eq!(a[0]["count"], 1);
    let v = json_of(&stw(
        false,
        None,
        &["status", "--root", &r, "--offline", "--json"],
    ));
    assert_eq!(a[0]["message"], v["github_note"]);
    let a = both(false, None, &["status", "--root", &r]);
    assert_eq!(codes(&a), ["no-token"]);
    assert!(a[0]["message"]
        .as_str()
        .unwrap()
        .starts_with("GitHub not read: no GitHub token found"));
    let v = json_of(&stw(false, None, &["status", "--root", &r, "--json"]));
    assert_eq!(a[0]["message"], v["github_note"]);
}

#[test]
fn a_failed_pull_request_or_issue_read_is_one_notice_per_source_and_the_old_fields_stay() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    for status in [401u16, 403, 429, 500] {
        let api = match status {
            401 => FakeApi::start(401, "{}"),
            403 => FakeApi::start(403, "{}"),
            429 => FakeApi::start(429, "{}"),
            _ => FakeApi::start(500, "{}"),
        };
        let a = both(true, Some(&api.url), &["status", "--root", &r]);
        assert_eq!(codes(&a), ["prs-not-read", "issues-not-read"], "{status}");
        assert_eq!(a[0]["source"], "pulls");
        assert_eq!(a[1]["source"], "issues");
        let v = json_of(&stw(
            true,
            Some(&api.url),
            &["status", "--root", &r, "--json"],
        ));
        let name = v["repos"][0]["name"].as_str().unwrap();
        for x in &a {
            assert_eq!(x["repos"], serde_json::json!([name]));
            assert!(
                x["message"].as_str().unwrap().contains(&status.to_string()),
                "{x}"
            );
        }
        assert_eq!(
            a[0]["message"],
            format!(
                "{name}: pull requests not read ({})",
                v["repos"][0]["pull_requests"]["error"].as_str().unwrap()
            )
        );
        assert_eq!(v["repos"][0]["counts"]["issues"]["state"], "failed");
        assert!(v["github_note"].is_null());
    }
}

#[test]
fn the_deadline_is_a_notice_of_the_read_it_cut() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = FakeRoutes::start(|_, _| {
        std::thread::sleep(std::time::Duration::from_secs(3));
        (200, vec![], "[]".into())
    });
    let a = both(
        true,
        Some(&api.url),
        &["status", "--root", &r, "--timeout", "1s"],
    );
    assert_eq!(codes(&a), ["prs-not-read", "issues-not-read"]);
    assert!(
        a.iter()
            .all(|x| x["message"].as_str().unwrap().contains("deadline:")),
        "{a:?}"
    );
}

/// ESC, CR, LF and U+202E in the error text of a failed fetch (the URL of the remote carries them).
#[test]
fn a_failed_fetch_is_a_notice_and_hostile_characters_stay_escaped() {
    let w = World::new();
    let seed = w.p("seed");
    std::fs::create_dir(&seed).unwrap();
    w.git(&seed, &["init", "-q"]);
    w.commit(&seed, "a.txt", "a\n");
    w.git(&w.base, &["clone", "-q", "seed", "ws"]);
    let ws = w.p("ws");
    for url in [
        "/nonexistent/stw/gone",
        "ext::sh -c \u{1b}[31mx\rLINE\nTWO\u{202e}evil",
    ] {
        w.git(&ws, &["remote", "set-url", "origin", url]);
        let run = |json: bool| {
            let mut c = Command::new(env!("CARGO_BIN_EXE_stw"));
            c.env_clear()
                .env("PATH", std::env::var_os("PATH").unwrap())
                .env("HOME", &w.home)
                .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
                .current_dir(&w.base)
                .args(["status", "--offline", "--fetch", "--root"])
                .arg(&ws);
            if json {
                c.arg("--json");
            }
            c.output().unwrap()
        };
        let (t, j) = (run(false), run(true));
        assert_eq!(j.status.code(), Some(0));
        let v = json_of(&j);
        let a = v["notices"].as_array().unwrap();
        assert_eq!(codes(a), ["offline", "fetch-failed"], "{url}");
        let f = &a[1];
        assert_eq!(f["source"], "fetch");
        assert_eq!(f["repos"], serde_json::json!(["ws"]));
        let m = f["message"].as_str().unwrap();
        assert!(m.starts_with("ws: fetch failed ("), "{m}");
        assert!(v["repos"][0]["fetch_error"].is_string());
        let (n, lines) = text_notices(&out(&t));
        assert_eq!((n, lines[1].as_str()), (2, m));
        // the notices carry no bidi override (older fields such as `fetch_error` are unchanged)
        let raw = serde_json::to_string(a).unwrap();
        assert!(
            !raw.chars().any(|c| c.is_control() || c == '\u{202e}'),
            "{raw:?}"
        );
        let all = out(&j);
        assert!(!all.chars().any(|c| c.is_control() && c != '\n'), "{all:?}");
        if url.starts_with("ext::") {
            assert!(
                m.contains("\\u{1b}")
                    || m.contains("\\u{202e}")
                    || m.contains("\\r")
                    || m.contains("\\n"),
                "{m}"
            );
        }
    }
}

#[test]
fn an_unreadable_repository_has_no_notice_and_exit_1() {
    let fx = status_fixture();
    fx.write(
        ".gitmodules",
        "[submodule \"broken\"]\n\tpath = broken\n\turl = x\n",
    );
    fx.write("broken/.git", "gitdir: /nonexistent/stw/gitdir\n");
    let r = path_str(&fx.path);
    let o = stw(
        false,
        None,
        &["status", "--root", &r, "--offline", "--json"],
    );
    assert_eq!(o.status.code(), Some(1));
    let v = json_of(&o);
    assert_eq!(codes(v["notices"].as_array().unwrap()), ["offline"]);
}

#[test]
fn the_order_is_note_then_per_repository_then_rate_limit_then_paths() {
    // fetch (per repository) comes before pull requests, pull requests before issues
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = FakeApi::start(500, "{}");
    let a = both(true, Some(&api.url), &["status", "--root", &r]);
    assert_eq!(codes(&a), ["prs-not-read", "issues-not-read"]);
    // two runs give the same list
    let b = both(true, Some(&api.url), &["status", "--root", &r]);
    assert_eq!(a, b);
}

#[test]
fn reading_the_notices_changes_nothing_in_the_workspace() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let before = std::fs::read(fx.path.join(".git/HEAD")).unwrap();
    let idx = std::fs::metadata(fx.path.join(".git/index"))
        .ok()
        .map(|m| m.modified().unwrap());
    let api = FakeApi::start(500, "{}");
    both(true, Some(&api.url), &["status", "--root", &r]);
    assert_eq!(before, std::fs::read(fx.path.join(".git/HEAD")).unwrap());
    assert_eq!(
        idx,
        std::fs::metadata(fx.path.join(".git/index"))
            .ok()
            .map(|m| m.modified().unwrap())
    );
}
