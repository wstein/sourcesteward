//! `stw status --all`: every repository of the workspace (root and nested submodules) in one line each, with the stack of
//! the current branch; `--json` is the `stw status --json` document plus the additive fields `kind`, `depth`, `stack` per
//! repository and `all`, `depth`, `limit`, `timeout` in `options`. Port of the spike `spikes/stw/status-all`
//! (deviations: README, "`stw status --all`").
//!
//! Harness confinement: every repository lives in a tempdir of `World`; git and stw run with a scrubbed environment
//! (`HOME` inside the tempdir, `GIT_CEILING_DIRECTORIES` above the work area); GitHub is a fake API on 127.0.0.1.
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::{json, Value};

use crate::common::*;
use crate::sync_flags::World;

const TOKEN: &str = "SECRETTOKEN-0123456789";

struct S {
    w: World,
    ws: PathBuf,
    c: PathBuf,
    n: PathBuf,
}

/// `ws` (a clone of the bare `ws.git`, branch main, upstream origin/main) has the submodule `c`; `c` has the submodule `n`.
/// `ws` and `c` are one commit ahead of their origin; `n` is level with it. No origin is a GitHub origin.
fn setup() -> S {
    let w = World::new();
    for (name, file) in [("n", "n.txt"), ("c", "c.txt"), ("ws", "ws.txt")] {
        let up = w.p(&format!("{name}-up"));
        std::fs::create_dir(&up).unwrap();
        w.git(&up, &["init", "-q"]);
        w.commit(&up, file, "1\n");
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
    }
    w.git(&w.base, &["clone", "-q", "ws.git", "ws"]);
    let ws = w.p("ws");
    w.git(
        &ws,
        &[
            "submodule",
            "add",
            "-q",
            w.p("c.git").to_str().unwrap(),
            "c",
        ],
    );
    let c = ws.join("c");
    w.git(
        &c,
        &[
            "submodule",
            "add",
            "-q",
            w.p("n.git").to_str().unwrap(),
            "n",
        ],
    );
    w.git(&c, &["commit", "-q", "-m", "add n"]);
    w.git(&ws, &["add", "c"]);
    w.git(&ws, &["commit", "-q", "-m", "add c"]);
    let n = c.join("n");
    S { w, ws, c, n }
}

struct Run {
    code: i32,
    out: String,
    err: String,
}

impl Run {
    fn json(&self) -> Value {
        serde_json::from_str(&self.out).unwrap_or_else(|e| panic!("{e}: {}{}", self.out, self.err))
    }
}

fn run(w: &World, cwd: &Path, api: Option<&str>, args: &[&str]) -> Run {
    let mut c = Command::new(env!("CARGO_BIN_EXE_stw"));
    c.env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &w.home)
        .env("GIT_CEILING_DIRECTORIES", &w.base)
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .current_dir(cwd);
    if let Some(a) = api {
        c.env("GH_TOKEN", TOKEN).env("STW_GITHUB_API_URL", a);
    }
    let o = c.args(args).output().unwrap();
    Run {
        code: o.status.code().unwrap_or(-1),
        out: String::from_utf8_lossy(&o.stdout).into_owned(),
        err: String::from_utf8_lossy(&o.stderr).into_owned(),
    }
}

/// `stw status --all --offline --root <ws> <extra>`
fn all(s: &S, extra: &[&str]) -> Run {
    let mut a = vec![
        "status",
        "--all",
        "--offline",
        "--root",
        s.ws.to_str().unwrap(),
    ];
    a.extend_from_slice(extra);
    run(&s.w, &s.w.base, None, &a)
}

/// The lines between `Repositories (N)` and the blank line after them.
fn rows(t: &str) -> Vec<String> {
    let b = t
        .split("\n\n")
        .find(|b| b.starts_with("Repositories ("))
        .unwrap_or_else(|| panic!("no Repositories block: {t}"));
    b.lines().skip(1).map(str::to_string).collect()
}

/// Runs of blanks collapsed (the name column is padded).
fn flat(l: &str) -> String {
    l.split_whitespace().collect::<Vec<_>>().join(" ")
}

fn repo<'a>(v: &'a Value, name: &str) -> &'a Value {
    v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .find(|r| r["name"] == name)
        .unwrap_or_else(|| panic!("no repository {name} in {v}"))
}

fn notice_codes(v: &Value) -> Vec<String> {
    v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .map(|n| n["code"].as_str().unwrap().to_string())
        .collect()
}

#[test]
fn the_text_lists_every_repository_of_a_nested_workspace_in_one_line_each() {
    let s = setup();
    let r = all(&s, &[]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let root = s.ws.to_str().unwrap();
    let lines: Vec<String> = rows(&r.out).iter().map(|l| flat(l)).collect();
    assert_eq!(
        lines,
        [
            "ws main origin/main +1/-0 clean stack - PRs n/a | Issues n/a (no-github-origin)",
            "c main origin/main +1/-0 clean stack - PRs n/a | Issues n/a (no-github-origin)",
            "c/n main origin/main +0/-0 clean stack - PRs n/a | Issues n/a (no-github-origin)",
        ]
    );
    let first = r.out.lines().next().unwrap();
    assert_eq!(
        first,
        format!("status --all  root={root}  repositories=3  depth<=16")
    );
    // the name column is 28 wide, a mark column (one blank) in front
    assert!(
        r.out.contains(&format!(
            "\n  {:<28} main  origin/main +1/-0  clean stack -  PRs n/a",
            "ws"
        )),
        "{}",
        r.out
    );
    assert!(
        r.out
            .contains("\n\nTotals  PRs n/a | Issues n/a | ahead 2 | behind 0\n\n"),
        "{}",
        r.out
    );
    assert!(
        r.out
            .contains("\n\nNotices (1)\n  GitHub not read: --offline\n\n"),
        "{}",
        r.out
    );
    assert!(
        r.out.ends_with("\n\n0 of 3 repositories need attention\n"),
        "{}",
        r.out
    );
    // the default blocks of `status` are not printed
    assert!(
        !r.out.contains("Needs action") && !r.out.contains("Counts\n"),
        "{}",
        r.out
    );
}

#[test]
fn the_json_is_the_status_document_plus_the_additive_fields() {
    let s = setup();
    let plain = run(
        &s.w,
        &s.w.base,
        None,
        &[
            "status",
            "--offline",
            "--json",
            "--root",
            s.ws.to_str().unwrap(),
        ],
    )
    .json();
    let r = all(&s, &["--json"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    assert_eq!(v["schema_version"], 2);
    assert_eq!(v["command"], "status");
    assert_eq!(v["options"]["all"], true);
    assert_eq!(v["options"]["depth"], 16);
    assert_eq!(v["options"]["limit"], 256);
    assert_eq!(v["options"]["timeout"], 90);
    let names: Vec<&str> = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .map(|r| r["name"].as_str().unwrap())
        .collect();
    assert_eq!(names, ["ws", "c", "c/n"]);
    for (name, kind, depth) in [
        ("ws", "root", 0),
        ("c", "submodule", 1),
        ("c/n", "submodule", 2),
    ] {
        let r = repo(&v, name);
        assert_eq!(r["kind"], kind, "{name}");
        assert_eq!(r["depth"], depth, "{name}");
        assert_eq!(r["stack"], json!({"chain": null, "links": 0}), "{name}");
        // every field of the plain document is there with the same value
        let p = repo(&plain, name);
        for (k, pv) in p.as_object().unwrap() {
            assert_eq!(&r[k], pv, "{name}.{k}");
        }
    }
    // top level: nothing but the options differ
    for (k, pv) in plain.as_object().unwrap() {
        if k != "options" && k != "repos" {
            assert_eq!(&v[k], pv, "{k}");
        }
    }
    for (k, pv) in plain["options"].as_object().unwrap() {
        assert_eq!(&v["options"][k], pv, "options.{k}");
    }
    // without --all nothing is added
    for k in ["all", "depth", "limit", "timeout"] {
        assert!(plain["options"].get(k).is_none(), "options.{k}");
    }
    for r in plain["repos"].as_array().unwrap() {
        for k in ["kind", "depth", "stack"] {
            assert!(r.get(k).is_none(), "repos[].{k}");
        }
    }
    // the options carry the values that were given
    let v = run(
        &s.w,
        &s.w.base,
        None,
        &[
            "status",
            "--all",
            "--json",
            "--root",
            s.ws.to_str().unwrap(),
            "--depth",
            "3",
            "--limit",
            "9",
            "--timeout",
            "2m",
        ],
    )
    .json();
    assert_eq!(v["options"]["depth"], 3);
    assert_eq!(v["options"]["limit"], 9);
    assert_eq!(v["options"]["timeout"], 120);
}

#[test]
fn the_depth_bounds_the_walk_and_a_cut_is_a_notice() {
    let s = setup();
    let r = all(&s, &["--depth", "1"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    assert!(
        r.out.starts_with(&format!(
            "status --all  root={}  repositories=2  depth<=1\n",
            s.ws.display()
        )),
        "{}",
        r.out
    );
    assert_eq!(rows(&r.out).len(), 2, "{}", r.out);
    assert!(
        r.out
            .contains("\n  c: submodules below depth 1 not listed\n"),
        "{}",
        r.out
    );
    let v = all(&s, &["--depth", "1", "--json"]).json();
    assert_eq!(v["summary"]["total"], 2);
    assert!(notice_codes(&v).contains(&"depth-limit".to_string()), "{v}");
    let n = v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .find(|n| n["code"] == "depth-limit")
        .unwrap()
        .clone();
    assert_eq!(n["repos"], json!(["c"]));
    assert_eq!(n["source"], "submodules");
    // depth 0: the root only, and its gitlink is the cut
    let v = all(&s, &["--depth", "0", "--json"]).json();
    assert_eq!(v["summary"]["total"], 1);
    let n = v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .find(|n| n["code"] == "depth-limit")
        .unwrap()
        .clone();
    assert_eq!(n["repos"], json!(["ws"]));
    // deep enough: no notice
    let v = all(&s, &["--depth", "2", "--json"]).json();
    assert_eq!(v["summary"]["total"], 3);
    assert!(
        !notice_codes(&v).contains(&"depth-limit".to_string()),
        "{v}"
    );
}

#[test]
fn more_repositories_than_the_limit_is_a_usage_error() {
    let s = setup();
    let r = all(&s, &["--limit", "2"]);
    assert_eq!(r.code, 2, "{}{}", r.out, r.err);
    assert!(
        r.err.contains("the workspace has more than 2 repositories"),
        "{}",
        r.err
    );
    assert!(r.out.is_empty(), "{}", r.out);
    let r = all(&s, &["--limit", "3"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    // the limit counts the root; a limit above the default of the workspace is allowed
    let r = all(&s, &["--limit", "1024", "--json"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
}

fn set_parent(s: &S, dir: &Path, branch: &str, parent: &str) {
    s.w.git(dir, &["config", &format!("branch.{branch}.remote"), "."]);
    s.w.git(
        dir,
        &[
            "config",
            &format!("branch.{branch}.merge"),
            &format!("refs/heads/{parent}"),
        ],
    );
}

#[test]
fn the_stack_is_the_chain_of_local_upstreams_of_the_current_branch() {
    let s = setup();
    let c = &s.c;
    s.w.git(c, &["checkout", "-q", "-b", "a"]);
    s.w.commit(c, "a.txt", "a\n");
    set_parent(&s, c, "a", "main");
    s.w.git(c, &["checkout", "-q", "-b", "b"]);
    s.w.commit(c, "b.txt", "b\n");
    set_parent(&s, c, "b", "a");
    // a second branch with a local parent that is not in the chain of the current one
    s.w.git(c, &["branch", "other", "main"]);
    set_parent(&s, c, "other", "main");
    let r = all(&s, &[]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let row = rows(&r.out)
        .iter()
        .map(|l| flat(l))
        .find(|l| l.trim_start_matches("! ").starts_with("c "))
        .unwrap();
    assert!(row.contains(" stack main<-a<-b "), "{row}");
    let v = all(&s, &["--json"]).json();
    assert_eq!(
        repo(&v, "c")["stack"],
        json!({"chain": ["main", "a", "b"], "links": 3})
    );
    // a branch without a local parent is no stack: `-` and chain null, the links of the others are counted
    s.w.git(c, &["checkout", "-q", "main"]);
    let v = all(&s, &["--json"]).json();
    assert_eq!(repo(&v, "c")["stack"], json!({"chain": null, "links": 3}));
    let r = all(&s, &[]);
    let row = rows(&r.out)
        .iter()
        .map(|l| flat(l))
        .find(|l| l.trim_start_matches("! ").starts_with("c "))
        .unwrap();
    assert!(row.contains(" stack - "), "{row}");
    // a remote upstream is no local parent
    s.w.git(c, &["checkout", "-q", "b"]);
    s.w.git(c, &["config", "branch.b.remote", "origin"]);
    let v = all(&s, &["--json"]).json();
    assert_eq!(repo(&v, "c")["stack"], json!({"chain": null, "links": 2}));
    // a branch name that contains `<-` stays one element in the JSON chain
    s.w.git(c, &["checkout", "-q", "-b", "x<-y", "main"]);
    set_parent(&s, c, "x<-y", "main");
    let v = all(&s, &["--json"]).json();
    assert_eq!(repo(&v, "c")["stack"]["chain"], json!(["main", "x<-y"]));
}

#[test]
fn a_cycle_of_local_upstreams_ends_the_chain_with_a_notice() {
    let s = setup();
    let c = &s.c;
    s.w.git(c, &["checkout", "-q", "-b", "a"]);
    s.w.git(c, &["checkout", "-q", "-b", "b"]);
    set_parent(&s, c, "a", "b");
    set_parent(&s, c, "b", "a");
    let v = all(&s, &["--json"]).json();
    assert_eq!(
        repo(&v, "c")["stack"]["chain"],
        json!(["b", "a", "b"]),
        "{v}"
    );
    let n = v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .find(|n| n["code"] == "stack-cycle")
        .unwrap()
        .clone();
    assert_eq!(n["repos"], json!(["c"]));
    assert_eq!(n["source"], "stack");
    assert_eq!(n["message"], "c: stack cycle at b");
    let r = all(&s, &[]);
    assert!(r.out.contains("\n  c: stack cycle at b\n"), "{}", r.out);
}

#[test]
fn counts_sync_forms_and_attention_follow_the_status_rules() {
    let s = setup();
    // c: one staged, one unstaged, one untracked, behind its origin by one
    std::fs::write(s.c.join("s.txt"), "s\n").unwrap();
    s.w.git(&s.c, &["add", "s.txt"]);
    std::fs::write(s.c.join("c.txt"), "changed\n").unwrap();
    std::fs::write(s.c.join("u.txt"), "u\n").unwrap();
    let up = s.w.p("c-up");
    s.w.commit(&up, "more.txt", "m\n");
    s.w.git(
        &up,
        &["push", "-q", s.w.p("c.git").to_str().unwrap(), "main"],
    );
    s.w.git(&s.c, &["fetch", "-q", "origin"]);
    // n: detached
    s.w.git(&s.n, &["checkout", "-q", "--detach"]);
    let r = all(&s, &[]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let ls: Vec<String> = rows(&r.out).iter().map(|l| l.to_string()).collect();
    assert_eq!(ls.len(), 3, "{}", r.out);
    assert!(ls[0].starts_with("  ws "), "{}", ls[0]);
    assert!(ls[1].starts_with("! c "), "{}", ls[1]);
    assert!(ls[2].starts_with("! c/n "), "{}", ls[2]);
    assert!(
        flat(&ls[1]).contains(" main origin/main +1/-1 1 staged 1 unstaged 1 untracked stack - "),
        "{}",
        ls[1]
    );
    let head = s.w.git(&s.n, &["rev-parse", "--short=12", "HEAD"]);
    assert!(
        flat(&ls[2]).starts_with(&format!("! c/n ({head}) - clean stack - ")),
        "{}",
        ls[2]
    );
    assert!(r.out.contains("ahead 2 | behind 1\n"), "{}", r.out);
    assert!(
        r.out.ends_with("\n\n2 of 3 repositories need attention\n"),
        "{}",
        r.out
    );
    let v = all(&s, &["--json"]).json();
    assert_eq!(
        repo(&v, "c")["reasons"],
        json!(["staged", "unstaged", "untracked", "behind"])
    );
    assert_eq!(repo(&v, "c/n")["reasons"], json!(["detached"]));
    assert_eq!(repo(&v, "c/n")["stack"], Value::Null);
    assert_eq!(repo(&v, "ws")["attention"], false);
    assert_eq!(v["summary"], json!({"total": 3, "attention": 2}));
    // no upstream, upstream gone, unborn
    s.w.git(&s.ws, &["checkout", "-q", "-b", "local"]);
    let v = all(&s, &["--json"]).json();
    assert_eq!(repo(&v, "ws")["reasons"], json!(["no-upstream"]));
    let r = all(&s, &[]);
    assert!(
        flat(&rows(&r.out)[0]).contains(" local no upstream clean stack - "),
        "{}",
        r.out
    );
    s.w.git(&s.ws, &["checkout", "-q", "main"]);
    s.w.git(&s.ws, &["update-ref", "-d", "refs/remotes/origin/main"]);
    let r = all(&s, &[]);
    assert!(
        flat(&rows(&r.out)[0]).contains(" main upstream gone clean stack - "),
        "{}",
        r.out
    );
    let v = all(&s, &["--json"]).json();
    assert_eq!(repo(&v, "ws")["upstream_gone"], true);
    assert_eq!(repo(&v, "ws")["reasons"], json!([]));
    let u = s.w.p("unborn-ws");
    std::fs::create_dir(&u).unwrap();
    s.w.git(&u, &["init", "-q"]);
    let r = run(
        &s.w,
        &s.w.base,
        None,
        &[
            "status",
            "--all",
            "--offline",
            "--root",
            u.to_str().unwrap(),
        ],
    );
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    assert!(
        flat(&rows(&r.out)[0]).contains(" main (unborn) clean stack - "),
        "{}",
        r.out
    );
    let v = run(
        &s.w,
        &s.w.base,
        None,
        &[
            "status",
            "--all",
            "--offline",
            "--json",
            "--root",
            u.to_str().unwrap(),
        ],
    )
    .json();
    assert_eq!(v["repos"][0]["stack"], Value::Null);
}

fn pr(n: u64) -> Value {
    json!({"number": n, "title": format!("pr {n}"), "draft": false, "user": {"login": "me", "type": "User"},
           "head": {"ref": format!("b{n}"), "sha": "abc"}, "state": "open", "merged_at": null})
}

fn issue(n: u64) -> Value {
    json!({"number": n, "title": format!("issue {n}"), "labels": []})
}

#[test]
fn github_counts_failed_reads_and_totals() {
    let s = setup();
    s.w.git(
        &s.ws,
        &["config", "remote.origin.url", "https://github.com/test/ws"],
    );
    s.w.git(
        &s.c,
        &["config", "remote.origin.url", "https://github.com/test/c"],
    );
    let api = FakeRoutes::start(|path, _| {
        let body = match path {
            "/repos/test/ws/pulls" => json!([pr(1), pr(2)]),
            "/repos/test/ws/issues" => json!([issue(3), issue(4), issue(5)]),
            "/repos/test/c/pulls" => return (404, vec![], "{}".into()),
            "/repos/test/c/issues" => return (404, vec![], "{}".into()),
            p if p.contains("/pulls/") => json!({"mergeable_state": "clean"}),
            _ => json!([]),
        };
        (200, vec![], body.to_string())
    });
    let root = s.ws.to_str().unwrap();
    let r = run(
        &s.w,
        &s.w.base,
        Some(&api.url),
        &["status", "--all", "--root", root],
    );
    assert_eq!(
        r.code, 0,
        "a failed GitHub read is a notice: {}{}",
        r.out, r.err
    );
    let ls: Vec<String> = rows(&r.out).iter().map(|l| flat(l)).collect();
    assert!(ls[0].ends_with("stack - PRs 2 | Issues 3"), "{}", ls[0]);
    assert!(
        ls[1].ends_with("stack - PRs ? | Issues ? (not-found)"),
        "{}",
        ls[1]
    );
    assert!(
        ls[2].ends_with("stack - PRs n/a | Issues n/a (no-github-origin)"),
        "{}",
        ls[2]
    );
    // a failed read makes the total `n/a`; the rows with a number keep theirs
    assert!(
        r.out
            .contains("\nTotals  PRs n/a | Issues n/a | ahead 2 | behind 0\n"),
        "{}",
        r.out
    );
    assert!(r.out.contains("c: pull requests not read ("), "{}", r.out);
    let v = run(
        &s.w,
        &s.w.base,
        Some(&api.url),
        &["status", "--all", "--json", "--root", root],
    )
    .json();
    assert_eq!(repo(&v, "c")["counts"]["pull_requests"]["state"], "failed");
    assert_eq!(repo(&v, "ws")["counts"]["pull_requests"]["open"], 2);
    // without the failing repository the totals are numbers
    s.w.git(
        &s.c,
        &["config", "remote.origin.url", "https://example.org/x/c"],
    );
    let r = run(
        &s.w,
        &s.w.base,
        Some(&api.url),
        &["status", "--all", "--root", root],
    );
    assert!(
        r.out
            .contains("\nTotals  PRs 2 | Issues 3 | ahead 2 | behind 0\n"),
        "{}",
        r.out
    );
    assert!(
        !r.out.contains("Notices ("),
        "no notice, no block: {}",
        r.out
    );
    // a failure class in the row: 403 is `forbidden`, a deadline is `timeout`
    let api = FakeRoutes::start(|_, _| (403, vec![], "{}".into()));
    let r = run(
        &s.w,
        &s.w.base,
        Some(&api.url),
        &["status", "--all", "--root", root],
    );
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    assert!(
        flat(&rows(&r.out)[0]).ends_with("PRs ? | Issues ? (forbidden)"),
        "{}",
        r.out
    );
}

#[test]
fn a_full_page_is_a_lower_bound() {
    let s = setup();
    s.w.git(
        &s.ws,
        &["config", "remote.origin.url", "https://github.com/test/ws"],
    );
    let api = FakeRoutes::start(|path, _| {
        let body = match path {
            "/repos/test/ws/pulls" => Value::Array((1..=100).map(pr).collect()),
            p if p.contains("/pulls/") => json!({"mergeable_state": "clean"}),
            _ => json!([]),
        };
        (200, vec![], body.to_string())
    });
    let r = run(
        &s.w,
        &s.w.base,
        Some(&api.url),
        &["status", "--all", "--root", s.ws.to_str().unwrap()],
    );
    assert!(
        flat(&rows(&r.out)[0]).ends_with("PRs 100+ | Issues 0"),
        "{}",
        r.out
    );
    assert!(
        r.out.contains("\nTotals  PRs 100+ | Issues 0 |"),
        "{}",
        r.out
    );
}

#[test]
fn a_repository_that_cannot_be_read_is_an_error_row_and_exits_1() {
    let s = setup();
    std::fs::write(s.n.join(".git"), "gitdir: /nonexistent/stw/none\n").unwrap();
    let r = all(&s, &[]);
    assert_eq!(r.code, 1, "{}{}", r.out, r.err);
    let ls = rows(&r.out);
    let row = ls
        .iter()
        .find(|l| l.starts_with("! c/n "))
        .unwrap_or_else(|| panic!("{}", r.out));
    assert!(row.trim_end().ends_with(')') && row.contains(" ("), "{row}");
    assert!(!row.contains("stack"), "{row}");
    let v = all(&s, &["--json"]);
    assert_eq!(v.code, 1);
    let v = v.json();
    assert_eq!(repo(&v, "c/n")["state"], "error");
    assert_eq!(repo(&v, "c/n")["reasons"], json!(["error"]));
    assert_eq!(repo(&v, "c/n")["stack"], Value::Null);
    assert_eq!(repo(&v, "c/n")["kind"], "submodule");
    assert!(
        r.out.ends_with("\n\n1 of 3 repositories need attention\n"),
        "{}",
        r.out
    );
    // the parents of the unreadable submodule are readable: its state is not looked into
    assert!(
        ls[0].starts_with("  ws ") && ls[1].starts_with("  c "),
        "{}",
        r.out
    );
}

#[test]
fn flags_that_do_not_fit_are_usage_errors() {
    let s = setup();
    let root = s.ws.to_str().unwrap();
    let st = |extra: &[&str]| {
        let mut a = vec!["status", "--offline", "--root", root];
        a.extend_from_slice(extra);
        run(&s.w, &s.w.base, None, &a)
    };
    for extra in [
        &["--depth", "1"][..],
        &["--limit", "5"],
        &["--all", "--depth", "65"],
        &["--all", "--depth", "x"],
        &["--all", "--limit", "0"],
        &["--all", "--limit", "1025"],
        &["--all", "--long"],
        &["--all", "--icons", "ascii"],
        &["--all", "--worktrees"],
    ] {
        let r = st(extra);
        assert_eq!(r.code, 2, "{extra:?}: {}{}", r.out, r.err);
        assert!(r.out.is_empty(), "{extra:?}: {}", r.out);
    }
    // the bounds themselves are fine
    for extra in [
        &["--all", "--depth", "0"][..],
        &["--all", "--depth", "64"],
        &["--all", "--limit", "3"],
    ] {
        let r = st(extra);
        assert_eq!(r.code, 0, "{extra:?}: {}{}", r.out, r.err);
    }
}

#[test]
fn text_from_outside_is_shown_escaped_and_the_run_writes_nothing() {
    let s = setup();
    let bidi = "x\u{202e}evil";
    s.w.git(&s.c, &["checkout", "-q", "-b", bidi]);
    let before = tree_snapshot(&s.ws);
    let r = all(&s, &[]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    assert!(!r.out.contains('\u{202e}'), "raw bidi control: {:?}", r.out);
    assert!(
        !r.out.chars().any(|c| c.is_control() && c != '\n'),
        "raw control character in {:?}",
        r.out
    );
    assert!(r.out.contains("evil"), "{}", r.out);
    assert_eq!(
        tree_snapshot(&s.ws),
        before,
        "status --all changed the workspace"
    );
}

#[test]
fn a_checkout_inside_a_nested_submodule_has_the_top_level_superproject_as_root() {
    let s = setup();
    let r = run(
        &s.w,
        &s.n,
        None,
        &["status", "--all", "--offline", "--json"],
    );
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    assert_eq!(v["root"], s.ws.to_str().unwrap());
    assert_eq!(v["summary"]["total"], 3);
}

#[test]
fn a_submodule_named_like_the_root_directory_keeps_its_own_depth_and_kind() {
    let s = setup();
    s.w.git(
        &s.ws,
        &[
            "submodule",
            "add",
            "-q",
            s.w.p("n.git").to_str().unwrap(),
            "ws",
        ],
    );
    let v = all(&s, &["--json"]).json();
    let repos = v["repos"].as_array().unwrap();
    let key = |r: &Value| {
        (
            r["name"].as_str().unwrap().to_string(),
            r["kind"].clone(),
            r["depth"].clone(),
        )
    };
    assert_eq!(
        repos.iter().map(key).collect::<Vec<_>>(),
        [
            ("ws".to_string(), json!("root"), json!(0)),
            ("c".to_string(), json!("submodule"), json!(1)),
            ("c/n".to_string(), json!("submodule"), json!(2)),
            ("ws".to_string(), json!("submodule"), json!(1)),
        ],
        "{v}"
    );
}

#[test]
fn the_class_of_a_failed_read_comes_from_the_start_of_the_message() {
    use stw::status_all::classify;
    assert_eq!(
        classify("GitHub API HTTP 404 for /repos/a/unreachable/pulls: x"),
        "not-found"
    );
    assert_eq!(
        classify("GitHub API HTTP 500 for /repos/a/unexpected/pulls"),
        "error"
    );
    assert_eq!(
        classify("GitHub API HTTP 403 for /repos/a/b/pulls: forbidden or rate limited"),
        "forbidden"
    );
    assert_eq!(
        classify("GitHub API unreachable for /repos/a/b/pulls: x"),
        "unreachable"
    );
    assert_eq!(
        classify("GitHub API sent an unexpected pulls response"),
        "unreadable"
    );
    assert_eq!(classify("deadline: the --timeout of 1s passed"), "timeout");
    assert_eq!(classify("something else HTTP 404"), "error");
}

#[test]
fn a_cycle_that_does_not_pass_the_current_branch_ends_the_chain_at_the_repeated_branch() {
    let s = setup();
    let c = &s.c;
    for b in ["a", "b", "cur"] {
        s.w.git(c, &["branch", b, "main"]);
    }
    set_parent(&s, c, "a", "b");
    set_parent(&s, c, "b", "a");
    set_parent(&s, c, "cur", "a");
    s.w.git(c, &["checkout", "-q", "cur"]);
    let v = all(&s, &["--json"]).json();
    assert_eq!(
        repo(&v, "c")["stack"]["chain"],
        json!(["a", "b", "a", "cur"]),
        "{v}"
    );
    let n = v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .find(|n| n["code"] == "stack-cycle")
        .unwrap()
        .clone();
    assert_eq!(n["message"], "c: stack cycle at a");
}

#[test]
fn the_row_names_why_github_was_not_read_and_which_read_failed() {
    let s = setup();
    s.w.git(
        &s.ws,
        &["config", "remote.origin.url", "https://github.com/test/ws"],
    );
    let root = s.ws.to_str().unwrap();
    let r = run(
        &s.w,
        &s.w.base,
        None,
        &["status", "--all", "--offline", "--root", root],
    );
    assert!(
        flat(&rows(&r.out)[0]).ends_with("PRs n/a | Issues n/a (offline)"),
        "{}",
        r.out
    );
    let r = run(&s.w, &s.w.base, None, &["status", "--all", "--root", root]);
    assert!(
        flat(&rows(&r.out)[0]).ends_with("PRs n/a | Issues n/a (no-token)"),
        "{}",
        r.out
    );
    // only the issues fail: the pull requests keep their number, the reason is the one of the issues
    let api = FakeRoutes::start(|path, _| match path {
        "/repos/test/ws/pulls" => (200, vec![], json!([pr(1), pr(2)]).to_string()),
        "/repos/test/ws/issues" => (404, vec![], "{}".into()),
        _ => (200, vec![], json!({"mergeable_state": "clean"}).to_string()),
    });
    let r = run(
        &s.w,
        &s.w.base,
        Some(&api.url),
        &["status", "--all", "--root", root],
    );
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    assert!(
        flat(&rows(&r.out)[0]).ends_with("PRs 2 | Issues ? (not-found)"),
        "{}",
        r.out
    );
}

#[test]
fn stats_verbose_and_review_are_usage_errors_with_all() {
    let s = setup();
    for flag in ["--stats", "--verbose", "--review"] {
        let r = run(
            &s.w,
            &s.w.base,
            None,
            &["status", "--all", flag, "--root", s.ws.to_str().unwrap()],
        );
        assert_eq!(r.code, 2, "{flag}: {}{}", r.out, r.err);
        assert!(r.out.is_empty(), "{flag}: {}", r.out);
    }
}

#[test]
fn a_gitlink_without_a_checkout_is_no_depth_limit_notice() {
    let s = setup();
    // a clone without --recurse-submodules: the gitlink `c` is in the index, nothing is checked out
    s.w.git(&s.w.base, &["clone", "-q", "ws", "wsc"]);
    let u = s.w.p("wsc");
    let r = run(
        &s.w,
        &s.w.base,
        None,
        &[
            "status",
            "--all",
            "--offline",
            "--json",
            "--depth",
            "0",
            "--root",
            u.to_str().unwrap(),
        ],
    );
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    assert_eq!(v["summary"]["total"], 1);
    assert!(
        !notice_codes(&v).contains(&"depth-limit".to_string()),
        "{v}"
    );
}
