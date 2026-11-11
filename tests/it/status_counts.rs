//! `stw status`: counts per repository and in total, separated blocks, exit 0 for notices only, the JSON
//! `counts` and `totals` objects, and that the command writes nothing.
use std::collections::BTreeMap;
use std::path::Path;
use std::process::{Command, Output};

use crate::common::*;
use serde_json::{json, Value};

const TOKEN: &str = "SECRETTOKEN-0123456789";

fn stw(api: Option<&str>, args: &[&str]) -> Output {
    let mut c = Command::new(env!("CARGO_BIN_EXE_stw"));
    c.env_clear()
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .env("GH_TOKEN", TOKEN);
    if let Some(a) = api {
        c.env("STW_GITHUB_API_URL", a);
    }
    c.args(args).output().unwrap()
}

/// Runs of blanks collapsed, so that column padding does not matter.
fn flat(t: &str) -> String {
    t.lines()
        .map(|l| l.split_whitespace().collect::<Vec<_>>().join(" "))
        .collect::<Vec<_>>()
        .join("\n")
}

fn out(o: &Output) -> String {
    String::from_utf8_lossy(&o.stdout).into_owned()
}

/// Root `repo` (test/fixture): local branches main, topic, other; remote branches main, topic; one
/// linked worktree; two stash entries. Plus a submodule `sub` (test/sub) without any branch.
fn workspace() -> Fx {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.branch("main", c0);
    fx.branch("topic", c0);
    fx.branch("other", c0);
    fx.remote("main", c0);
    fx.remote("topic", c0);
    fx.origin_head("main");
    fx.track("main");
    fx.set_head("main");
    fx.checkout_head();
    add_worktree(&fx, "w1", "topic", true, false);
    let id = c0.to_string();
    std::fs::write(fx.path.join(".git/refs/stash"), format!("{id}\n")).unwrap();
    std::fs::create_dir_all(fx.path.join(".git/logs/refs")).unwrap();
    let z = "0".repeat(40);
    std::fs::write(
        fx.path.join(".git/logs/refs/stash"),
        format!(
            "{z} {id} T <t@example.com> 1700000000 +0000\tWIP on main: one\n{id} {id} T <t@example.com> 1700000001 +0000\tWIP on main: two\n"
        ),
    )
    .unwrap();
    fx.write(
        ".gitmodules",
        "[submodule \"sub\"]\n\tpath = sub\n\turl = https://github.com/test/sub.git\n",
    );
    std::fs::create_dir_all(fx.path.join("sub")).unwrap();
    gix::init(fx.path.join("sub")).unwrap();
    use std::io::Write;
    let mut cfg = std::fs::OpenOptions::new()
        .append(true)
        .open(fx.path.join("sub/.git/config"))
        .unwrap();
    write!(
        cfg,
        "[remote \"origin\"]\n\turl = https://github.com/test/sub\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
    )
    .unwrap();
    fx
}

fn pr(n: u64, login: &str, kind: &str) -> Value {
    json!({"number": n, "title": format!("pr {n}"), "draft": false, "user": {"login": login, "type": kind},
           "head": {"ref": format!("b{n}"), "sha": "abc"}, "state": "open", "merged_at": null})
}

fn issue(n: u64, labels: &[&str]) -> Value {
    json!({"number": n, "title": format!("issue {n}"), "labels": labels.iter().map(|l| json!({"name": l})).collect::<Vec<_>>()})
}

/// A pull request as the issues list shows it: an issue with a `pull_request` key.
fn pull_in_issues(n: u64) -> Value {
    let mut p = issue(n, &["bug"]);
    p["pull_request"] = json!({});
    p
}

/// test/fixture: 3 open PRs (2 bots), 3 issues (2 bugs) next to a pull request that the issues list also
/// shows; test/sub: 1 PR, issues answer 403.
fn api() -> FakeRoutes {
    FakeRoutes::start(|path, _| {
        let body = match path {
            "/repos/test/fixture/pulls" => json!([
                pr(1, "dependabot[bot]", "Bot"),
                pr(2, "renovate[bot]", "User"),
                pr(3, "me", "User")
            ]),
            "/repos/test/fixture/issues" => json!([
                issue(4, &["bug"]),
                issue(5, &["Bug", "ui"]),
                issue(6, &["docs"]),
                pull_in_issues(7)
            ]),
            "/repos/test/sub/pulls" => json!([pr(9, "me", "User")]),
            "/repos/test/sub/issues" => return (403, vec![], "{}".into()),
            p if p.starts_with("/repos/test/") && p.contains("/pulls/") => {
                json!({"mergeable_state": "clean"})
            }
            _ => json!({}),
        };
        (200, vec![], body.to_string())
    })
}

fn tree(root: &Path) -> BTreeMap<String, Vec<u8>> {
    fn walk(base: &Path, dir: &Path, m: &mut BTreeMap<String, Vec<u8>>) {
        for e in std::fs::read_dir(dir).unwrap() {
            let p = e.unwrap().path();
            let rel = p.strip_prefix(base).unwrap().to_string_lossy().into_owned();
            if p.is_dir() {
                m.insert(format!("{rel}/"), vec![]);
                walk(base, &p, m);
            } else {
                m.insert(rel, std::fs::read(&p).unwrap_or_default());
            }
        }
    }
    let mut m = BTreeMap::new();
    walk(root, root, &mut m);
    m
}

#[test]
fn the_blocks_are_in_order_separated_and_the_summary_is_last() {
    let fx = workspace();
    let api = api();
    let o = stw(Some(&api.url), &["status", "--root", &path_str(&fx.path)]);
    let t = out(&o);
    assert_eq!(o.status.code(), Some(0), "{t}");
    let pos = |h: &str| {
        t.find(&format!("\n{h}"))
            .map(|i| i + 1)
            .or_else(|| t.starts_with(h).then_some(0))
            .unwrap_or_else(|| panic!("no block {h:?} in:\n{t}"))
    };
    let (c, n, r, no) = (
        pos("Counts\n"),
        pos("Needs action ("),
        pos("Repositories ("),
        pos("Notices ("),
    );
    assert!(c < n && n < r && r < no, "{t}");
    for at in [n, r, no] {
        assert_eq!(
            &t[at - 2..at],
            "\n\n",
            "blank line before the block at {at}:\n{t}"
        );
    }
    let last = t.trim_end().lines().last().unwrap();
    assert!(
        last.contains("repositories"),
        "summary is the last line: {t}"
    );
    assert!(
        t.contains("Repositories (2)\n") && t.contains("Notices (1)\n"),
        "{t}"
    );
}

#[test]
fn counts_per_repository_and_in_total_with_question_marks_for_unreadable_values() {
    let fx = workspace();
    let api = api();
    let o = stw(Some(&api.url), &["status", "--root", &path_str(&fx.path)]);
    let t = out(&o);
    let counts: Vec<&str> = t
        .split("\n\n")
        .find(|b| b.starts_with("Counts\n"))
        .unwrap()
        .lines()
        .skip(1)
        .collect();
    assert_eq!(counts.len(), 3, "{t}");
    let norm = |l: &str| l.split_whitespace().collect::<Vec<_>>().join(" ");
    assert_eq!(
        norm(counts[0]),
        "repo PRs 3 (bot 2) | Issues 3 (bug 2) | Branches 3/2 | Worktrees 1 | Stash 2"
    );
    assert_eq!(
        norm(counts[1]),
        "sub PRs 1 (bot 0) | Issues ? | Branches 0/0 | Worktrees 0 | Stash 0"
    );
    assert_eq!(
        norm(counts[2]),
        "All PRs 4 (bot 2) | Issues ? | Branches 3/2 | Worktrees 1 | Stash 2"
    );
    // aligned: the separators of all lines stand in the same columns
    let cols = |l: &str| l.match_indices(" | ").map(|(i, _)| i).collect::<Vec<_>>();
    assert_eq!(cols(counts[0]), cols(counts[1]), "{t}");
    assert_eq!(cols(counts[0]), cols(counts[2]), "{t}");
}

#[test]
fn offline_counts_of_the_hosting_are_not_available_and_never_zero() {
    let fx = workspace();
    let api = api();
    let o = stw(
        Some(&api.url),
        &["status", "--root", &path_str(&fx.path), "--offline"],
    );
    let t = out(&o);
    assert_eq!(o.status.code(), Some(0), "{t}");
    assert!(api.hits().is_empty());
    assert!(
        flat(&t).contains("PRs n/a | Issues n/a | Branches 3/2 | Worktrees 1 | Stash 2"),
        "{t}"
    );
    assert!(!t.contains("PRs 0") && !t.contains("Issues 0"), "{t}");
}

#[test]
fn only_notices_exit_0_and_stay_in_the_notices_block() {
    let fx = workspace();
    for (status, expect) in [
        (403u16, "forbidden"),
        (401, "token rejected"),
        (500, "HTTP 500"),
    ] {
        let api = FakeRoutes::start(move |_, _| (status, vec![], "{}".into()));
        let o = stw(Some(&api.url), &["status", "--root", &path_str(&fx.path)]);
        let t = out(&o);
        assert_eq!(
            o.status.code(),
            Some(0),
            "HTTP {status}: {t}{}",
            String::from_utf8_lossy(&o.stderr)
        );
        let notices = t
            .split("\n\n")
            .find(|b| b.starts_with("Notices ("))
            .unwrap_or_else(|| panic!("no Notices block: {t}"));
        assert!(notices.contains(expect), "{notices}");
        assert!(
            notices.contains("repo") && notices.contains("sub"),
            "{notices}"
        );
        assert!(flat(&t).contains("PRs ? | Issues ?"), "{t}");
    }
}

#[test]
fn a_repository_that_cannot_be_read_still_exits_1() {
    let fx = workspace();
    fx.write(
        ".gitmodules",
        "[submodule \"broken\"]\n\tpath = broken\n\turl = x\n",
    );
    fx.write("broken/.git", "gitdir: /nonexistent/stw/gitdir\n");
    let o = stw(
        None,
        &["status", "--root", &path_str(&fx.path), "--offline"],
    );
    assert_eq!(o.status.code(), Some(1), "{}", out(&o));
    assert!(
        flat(&out(&o)).contains("PRs ? | Issues ? | Branches ?/? | Worktrees ? | Stash ?"),
        "{}",
        out(&o)
    );
}

#[test]
fn a_full_page_marks_the_count_as_a_lower_bound() {
    let fx = workspace();
    let api = FakeRoutes::start(|path, _| {
        let body = match path {
            // a full raw page: 90 issues and 10 pull requests, which are not issues
            "/repos/test/fixture/issues" => Value::Array(
                (1..=100)
                    .map(|n| {
                        if n <= 90 {
                            issue(n, &["bug"])
                        } else {
                            pull_in_issues(n)
                        }
                    })
                    .collect(),
            ),
            "/repos/test/fixture/pulls" => {
                Value::Array((1..=100).map(|n| pr(n, "me", "User")).collect())
            }
            p if p.contains("/pulls/") => json!({"mergeable_state": "clean"}),
            _ => json!([]),
        };
        (200, vec![], body.to_string())
    });
    let t = out(&stw(
        Some(&api.url),
        &["status", "--root", &path_str(&fx.path)],
    ));
    let f = flat(&t);
    assert!(
        f.contains("repo PRs 100+ (bot 0+) | Issues 90+ (bug 90+)"),
        "{t}"
    );
    assert!(
        f.contains("All PRs 100+ (bot 0+) | Issues 90+ (bug 90+)"),
        "{t}"
    );
    let v: Value = serde_json::from_slice(
        &stw(
            Some(&api.url),
            &["status", "--root", &path_str(&fx.path), "--json"],
        )
        .stdout,
    )
    .unwrap();
    assert_eq!(v["repos"][0]["counts"]["issues"]["truncated"], true);
    assert_eq!(v["repos"][0]["counts"]["pull_requests"]["truncated"], true);
    assert_eq!(v["repos"][1]["counts"]["issues"]["truncated"], false);
}

#[test]
fn json_carries_counts_per_repository_and_totals_and_keeps_schema_2() {
    let fx = workspace();
    let api = api();
    let o = stw(
        Some(&api.url),
        &["status", "--root", &path_str(&fx.path), "--json"],
    );
    assert_eq!(o.status.code(), Some(0));
    let v: Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(v["schema_version"], 2);
    assert_eq!(
        v["repos"][0]["counts"],
        json!({
            "pull_requests": {"state": "ok", "open": 3, "bot": 2, "truncated": false},
            "issues": {"state": "ok", "open": 3, "bug": 2, "truncated": false},
            "branches": {"local": 3, "remote": 2},
            "worktrees": 1,
            "stash": 2
        })
    );
    assert_eq!(
        v["repos"][1]["counts"]["issues"],
        json!({"state": "failed", "open": null, "bug": null, "truncated": false})
    );
    assert_eq!(
        v["totals"],
        json!({
            "pull_requests": {"state": "ok", "open": 4, "bot": 2, "truncated": false},
            "issues": {"state": "failed", "open": null, "bug": null, "truncated": false},
            "branches": {"local": 3, "remote": 2},
            "worktrees": 1,
            "stash": 2
        })
    );
    // offline: not-read, never 0
    let o = stw(
        None,
        &[
            "status",
            "--root",
            &path_str(&fx.path),
            "--json",
            "--offline",
        ],
    );
    let v: Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(
        v["totals"]["pull_requests"],
        json!({"state": "not-read", "open": null, "bot": null, "truncated": false})
    );
}

#[test]
fn status_writes_nothing_with_or_without_the_network() {
    let fx = workspace();
    let api = api();
    let r = path_str(&fx.path);
    let before = tree(&fx.path);
    for args in [
        vec!["status", "--root", &r],
        vec!["status", "--root", &r, "--json"],
        vec!["status", "--root", &r, "--offline"],
    ] {
        let o = stw(Some(&api.url), &args);
        assert_eq!(o.status.code(), Some(0), "{args:?}");
    }
    assert_eq!(
        tree(&fx.path),
        before,
        "status changed files of the workspace"
    );
}

#[test]
fn a_repository_without_a_github_origin_is_left_out_of_the_totals() {
    let fx = workspace();
    fx.write(
        ".gitmodules",
        "[submodule \"sub\"]\n\tpath = sub\n\turl = https://github.com/test/sub.git\n[submodule \"plain\"]\n\tpath = plain\n\turl = x\n",
    );
    gix::init(fx.path.join("plain")).unwrap();
    let api = FakeRoutes::start(|path, _| {
        let body = match path {
            "/repos/test/fixture/pulls" => json!([pr(1, "a", "Bot"), pr(2, "b", "User")]),
            "/repos/test/fixture/issues" => json!([issue(4, &["bug"])]),
            p if p.contains("/pulls/") => json!({"mergeable_state": "clean"}),
            _ => json!([]),
        };
        (200, vec![], body.to_string())
    });
    let t = flat(&out(&stw(
        Some(&api.url),
        &["status", "--root", &path_str(&fx.path)],
    )));
    assert!(
        t.contains("plain PRs n/a | Issues n/a | Branches 0/0"),
        "{t}"
    );
    assert!(t.contains("All PRs 2 (bot 1) | Issues 1 (bug 1)"), "{t}");
}

#[test]
fn without_any_github_origin_the_totals_say_no_remote_not_not_read() {
    let tmp = tempfile::tempdir().unwrap();
    let root = tmp.path().join("plain");
    gix::init(&root).unwrap();
    let api = api();
    let o = stw(
        Some(&api.url),
        &["status", "--root", &path_str(&root), "--json"],
    );
    let v: Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(v["totals"]["pull_requests"]["state"], "no-remote", "{v}");
    assert_eq!(v["totals"]["issues"]["state"], "no-remote", "{v}");
    assert!(api.hits().is_empty());
}

#[test]
fn worktrees_count_existing_linked_checkouts_including_relative_links_but_not_missing_ones() {
    let fx = workspace();
    let base = fx.path.parent().unwrap();
    std::fs::create_dir_all(base.join("x")).unwrap();
    add_worktree_at(
        &fx,
        "rel-ok",
        "topic",
        &base.join("x/../wt-rel-ok"),
        true,
        false,
    );
    add_worktree_at(
        &fx,
        "rel-gone",
        "other",
        &base.join("x/../wt-rel-gone"),
        false,
        false,
    );
    let t = flat(&out(&stw(
        None,
        &["status", "--root", &path_str(&fx.path), "--offline"],
    )));
    assert!(t.contains("Worktrees 2 |"), "{t}");
}

#[test]
fn text_output_never_contains_raw_escape_sequences_from_slugs_or_names() {
    let fx = workspace();
    fx.set_origin_url("https://github.com/own/\u{1b}[31mRED\u{1b}[0m");
    fx.write(
        ".gitmodules",
        "[submodule \"s\"]\n\tpath = s\u{1b}[1mB\n\turl = x\n",
    );
    gix::init(fx.path.join("s\u{1b}[1mB")).unwrap();
    let r = path_str(&fx.path);
    let api = FakeRoutes::start(|_, _| (403, vec![], "{}".into()));
    for (a, args) in [
        (None, vec!["status", "--root", &r, "--offline"]),
        (Some(api.url.as_str()), vec!["status", "--root", &r]),
    ] {
        let o = stw(a, &args);
        let t = out(&o);
        assert!(!t.contains('\u{1b}'), "raw ESC in {args:?}: {t:?}");
        assert!(t.contains("Counts"), "{t}");
    }
    // the escaped slug is visible as text, in the notice of the online run
    let t = out(&stw(Some(&api.url), &["status", "--root", &r]));
    assert!(t.contains("\\u{1b}[31mRED"), "{t}");
}
