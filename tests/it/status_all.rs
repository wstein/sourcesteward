//! `stw status PATH..`: the repositories at the given paths (a repository with its nested submodules, or the repositories
//! found below a directory) in one line each, with the stack of the current branch; `--json` is the `stw status --json`
//! document plus the additive fields `kind`, `depth`, `stack` per repository, `paths` in `options` and, when a directory
//! was scanned, `depth`, `limit`, `timeout` in `options`. Port of the spike `spikes/stw/status-all` (deviations: README,
//! "`stw status PATH`"). The key of a repository is its path relative to the current directory.
//!
//! Harness confinement: every repository lives in a tempdir of `World`; git and stw run with a scrubbed environment
//! (`HOME` inside the tempdir, `GIT_CEILING_DIRECTORIES` above the work area); GitHub is a fake API on 127.0.0.1.
use crate::common::FixtureCommand as _;
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

/// `setup()` with the clone moved into the directory `grp`, which is no repository: `grp/ws`, `grp/ws/c`, `grp/ws/c/n`.
/// The submodule links are relative, so the move keeps them intact.
fn grouped() -> (S, PathBuf) {
    let mut s = setup();
    let grp = s.w.p("grp");
    std::fs::create_dir(&grp).unwrap();
    std::fs::rename(&s.ws, grp.join("ws")).unwrap();
    s.ws = grp.join("ws");
    s.c = s.ws.join("c");
    s.n = s.c.join("n");
    (s, grp)
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
    c.fixture_env_clear()
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

/// `stw status . --offline <extra>` in the workspace `ws`: the keys are `.`, `c`, `c/n`.
fn all(s: &S, extra: &[&str]) -> Run {
    let mut a = vec!["status", ".", "--offline"];
    a.extend_from_slice(extra);
    run(&s.w, &s.ws, None, &a)
}

/// `stw status <paths> --offline <extra>` in the directory `cwd`.
fn at(s: &S, cwd: &Path, paths: &[&str], extra: &[&str]) -> Run {
    let mut a = vec!["status"];
    a.extend_from_slice(paths);
    a.push("--offline");
    a.extend_from_slice(extra);
    run(&s.w, cwd, None, &a)
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

/// The repository with the key `key` (the `path` member).
fn repo<'a>(v: &'a Value, key: &str) -> &'a Value {
    v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .find(|r| r["path"] == key)
        .unwrap_or_else(|| panic!("no repository {key} in {v}"))
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
    let lines: Vec<String> = rows(&r.out).iter().map(|l| flat(l)).collect();
    assert_eq!(
        lines,
        [
            ". main origin/main +1/-0 clean stack - PRs n/a | Issues n/a (no-github-origin)",
            "c main origin/main +1/-0 clean stack - PRs n/a | Issues n/a (no-github-origin)",
            "c/n main origin/main +0/-0 clean stack - PRs n/a | Issues n/a (no-github-origin)",
        ]
    );
    let first = r.out.lines().next().unwrap();
    assert_eq!(first, "status  paths=.  repositories=3");
    // the name column is 28 wide, a mark column (one blank) in front
    assert!(
        r.out.contains(&format!(
            "\n  {:<28} main  origin/main +1/-0  clean stack -  PRs n/a",
            "."
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
    assert_eq!(v["options"]["paths"], json!(["."]));
    assert!(v["options"].get("all").is_none(), "{v}");
    // no directory was scanned: no scan options
    for k in ["depth", "limit", "timeout"] {
        assert!(v["options"].get(k).is_none(), "options.{k}");
    }
    let names: Vec<&str> = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .map(|r| r["name"].as_str().unwrap())
        .collect();
    assert_eq!(names, ["ws", "c", "c/n"]);
    for (name, key, kind, depth) in [
        ("ws", ".", "root", 0),
        ("c", "c", "submodule", 1),
        ("c/n", "c/n", "submodule", 2),
    ] {
        let r = repo(&v, key);
        assert_eq!(r["name"], name, "{key}");
        assert_eq!(r["kind"], kind, "{key}");
        assert_eq!(r["depth"], depth, "{key}");
        assert_eq!(r["stack"], json!({"chain": null, "links": 0}), "{key}");
        // every field of the plain document is there with the same value, the path (the key) excepted
        let p = plain["repos"]
            .as_array()
            .unwrap()
            .iter()
            .find(|p| p["name"] == name)
            .unwrap();
        for (k, pv) in p.as_object().unwrap() {
            if k != "path" {
                assert_eq!(&r[k], pv, "{key}.{k}");
            }
        }
    }
    // top level: nothing but the options differ
    for (k, pv) in plain.as_object().unwrap() {
        if k != "options" && k != "repos" {
            assert_eq!(&v[k], pv, "{k}");
        }
    }
    for (k, pv) in plain["options"].as_object().unwrap() {
        if k != "paths" {
            assert_eq!(&v["options"][k], pv, "options.{k}");
        }
    }
    // without a PATH nothing is added but the empty list of paths
    assert_eq!(plain["options"]["paths"], json!([]));
    for k in ["all", "depth", "limit", "timeout"] {
        assert!(plain["options"].get(k).is_none(), "options.{k}");
    }
    for r in plain["repos"].as_array().unwrap() {
        for k in ["kind", "depth", "stack"] {
            assert!(r.get(k).is_none(), "repos[].{k}");
        }
    }
    // a scanned directory: the options carry the values that were given, the paths as they were given
    let (s, _) = grouped();
    let v = run(
        &s.w,
        &s.w.base,
        None,
        &[
            "status",
            "--json",
            "./grp",
            "--depth",
            "3",
            "--limit",
            "9",
            "--timeout",
            "2m",
        ],
    )
    .json();
    assert_eq!(v["options"]["paths"], json!(["./grp"]));
    assert_eq!(v["options"]["depth"], 3);
    assert_eq!(v["options"]["limit"], 9);
    assert_eq!(v["options"]["timeout"], 120);
    // the defaults
    let v = at(&s, &s.w.base, &["grp"], &["--json"]).json();
    assert_eq!(v["options"]["depth"], 16);
    assert_eq!(v["options"]["limit"], 256);
    assert_eq!(v["options"]["timeout"], 90);
}

#[test]
fn the_depth_bounds_the_scan_and_a_cut_is_a_notice() {
    let (s, _) = grouped();
    let b = &s.w.base;
    // levels below `grp`: ws 1, ws/c 2, ws/c/n 3
    let r = at(&s, b, &["grp"], &["--depth", "2"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    assert!(
        r.out
            .starts_with("status  paths=grp  repositories=2  depth<=2\n"),
        "{}",
        r.out
    );
    assert_eq!(rows(&r.out).len(), 2, "{}", r.out);
    assert!(
        r.out
            .contains("\n  grp/ws/c: submodules below depth 2 not listed\n"),
        "{}",
        r.out
    );
    let v = at(&s, b, &["grp"], &["--depth", "2", "--json"]).json();
    assert_eq!(v["summary"]["total"], 2);
    let n = v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .find(|n| n["code"] == "depth-limit")
        .unwrap()
        .clone();
    assert_eq!(n["repos"], json!(["grp/ws/c"]));
    assert_eq!(n["source"], "submodules");
    // depth 1: the repository found at level 1, and its gitlink is the cut (named by the key of the repository)
    let v = at(&s, b, &["grp"], &["--depth", "1", "--json"]).json();
    assert_eq!(v["summary"]["total"], 1);
    let n = v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .find(|n| n["code"] == "depth-limit")
        .unwrap()
        .clone();
    assert_eq!(n["repos"], json!(["grp/ws"]));
    // depth 0: no level is looked at, the repository below is a notice of the scan
    let v = at(&s, b, &["grp"], &["--depth", "0", "--json"]).json();
    assert_eq!(v["summary"]["total"], 0);
    let n = v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .find(|n| n["code"] == "depth-limit")
        .unwrap()
        .clone();
    assert_eq!(n["repos"], json!(["grp"]));
    assert_eq!(n["source"], "scan");
    assert_eq!(n["message"], "grp: repositories below depth 0 not listed");
    // deep enough: no notice
    let v = at(&s, b, &["grp"], &["--depth", "3", "--json"]).json();
    assert_eq!(v["summary"]["total"], 3);
    assert!(
        !notice_codes(&v).contains(&"depth-limit".to_string()),
        "{v}"
    );
    // a directory that holds no repository at the bound is no notice either
    std::fs::create_dir_all(s.w.p("grp/plain/deeper")).unwrap();
    let v = at(&s, b, &["grp"], &["--depth", "1", "--json"]).json();
    assert!(
        !notice_codes(&v).iter().any(|c| c == "depth-limit")
            || v["notices"]
                .as_array()
                .unwrap()
                .iter()
                .all(|n| n["source"] != "scan"),
        "{v}"
    );
}

#[test]
fn more_repositories_than_the_limit_is_a_usage_error() {
    let (s, _) = grouped();
    let b = &s.w.base;
    let r = at(&s, b, &["grp"], &["--limit", "2"]);
    assert_eq!(r.code, 2, "{}{}", r.out, r.err);
    assert!(r.err.contains("more than 2 repositories"), "{}", r.err);
    assert!(r.out.is_empty(), "{}", r.out);
    let r = at(&s, b, &["grp"], &["--limit", "3"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    // the limit counts every repository of the run, over all paths
    let r = at(&s, b, &["grp", "grp/ws/c"], &["--limit", "3"]);
    assert_eq!(r.code, 0, "a duplicate counts once: {}{}", r.out, r.err);
    // a limit above the default is allowed
    let r = at(&s, b, &["grp"], &["--limit", "1024", "--json"]);
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
    // the full check: the changed submodule `c` is an unstaged path of its parent
    assert!(ls[0].starts_with("! . "), "{}", ls[0]);
    assert!(flat(&ls[0]).contains(" 1 unstaged stack - "), "{}", ls[0]);
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
        r.out.ends_with("\n\n3 of 3 repositories need attention\n"),
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
    assert_eq!(repo(&v, ".")["reasons"], json!(["unstaged"]));
    assert_eq!(repo(&v, ".")["changes"]["unstaged"], 1);
    assert_eq!(v["summary"], json!({"total": 3, "attention": 3}));
    // no upstream, upstream gone, unborn
    s.w.git(&s.ws, &["checkout", "-q", "-b", "local"]);
    let v = all(&s, &["--json"]).json();
    assert_eq!(repo(&v, ".")["reasons"], json!(["unstaged", "no-upstream"]));
    let r = all(&s, &[]);
    assert!(
        flat(&rows(&r.out)[0]).contains(" local no upstream 1 unstaged stack - "),
        "{}",
        r.out
    );
    s.w.git(&s.ws, &["checkout", "-q", "main"]);
    s.w.git(&s.ws, &["update-ref", "-d", "refs/remotes/origin/main"]);
    let r = all(&s, &[]);
    assert!(
        flat(&rows(&r.out)[0]).contains(" main upstream gone 1 unstaged stack - "),
        "{}",
        r.out
    );
    let v = all(&s, &["--json"]).json();
    assert_eq!(repo(&v, ".")["upstream_gone"], true);
    assert_eq!(repo(&v, ".")["reasons"], json!(["unstaged"]));
    let u = s.w.p("unborn-ws");
    std::fs::create_dir(&u).unwrap();
    s.w.git(&u, &["init", "-q"]);
    let r = run(&s.w, &u, None, &["status", ".", "--offline"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    assert!(
        flat(&rows(&r.out)[0]).contains(" main (unborn) clean stack - "),
        "{}",
        r.out
    );
    let v = run(&s.w, &u, None, &["status", ".", "--offline", "--json"]).json();
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
        if let Some(b) = ci_docs(path) {
            return (200, vec![], b);
        }
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
    let r = run(&s.w, &s.ws, Some(&api.url), &["status", "."]);
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
    let v = run(&s.w, &s.ws, Some(&api.url), &["status", ".", "--json"]).json();
    assert_eq!(repo(&v, "c")["counts"]["pull_requests"]["state"], "failed");
    assert_eq!(repo(&v, ".")["counts"]["pull_requests"]["open"], 2);
    // without the failing repository the totals are numbers
    s.w.git(
        &s.c,
        &["config", "remote.origin.url", "https://example.org/x/c"],
    );
    let r = run(&s.w, &s.ws, Some(&api.url), &["status", "."]);
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
    let r = run(&s.w, &s.ws, Some(&api.url), &["status", "."]);
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
    let r = run(&s.w, &s.ws, Some(&api.url), &["status", "."]);
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
    // the full check looks into the submodules: the parents of the unreadable submodule cannot be read either
    for (key, sub) in [(".", "c"), ("c", "n")] {
        assert_eq!(repo(&v, key)["state"], "error", "{key}");
        assert_eq!(
            repo(&v, key)["error"],
            format!("Could not determine status for submodule at \"{sub}\""),
            "{key}"
        );
    }
    assert!(
        r.out.ends_with("\n\n3 of 3 repositories need attention\n"),
        "{}",
        r.out
    );
}

#[test]
fn flags_that_do_not_fit_are_usage_errors() {
    let (s, _) = grouped();
    let b = &s.w.base;
    let st = |extra: &[&str]| {
        let mut a = vec!["status", "--offline"];
        a.extend_from_slice(extra);
        run(&s.w, b, None, &a)
    };
    for extra in [
        &["grp", "--depth", "65"][..],
        &["grp", "--depth", "x"],
        &["grp", "--limit", "0"],
        &["grp", "--limit", "1025"],
        &["grp", "--long"],
        &["grp", "--icons", "ascii"],
        &["grp", "--worktrees"],
        &["grp", "--repo", "ws"],
        &["--all", "grp"],
        &["--all"],
    ] {
        let r = st(extra);
        assert_eq!(r.code, 2, "{extra:?}: {}{}", r.out, r.err);
        assert!(r.out.is_empty(), "{extra:?}: {}", r.out);
    }
    // the bounds themselves are fine
    for extra in [
        &["grp", "--depth", "0"][..],
        &["grp", "--depth", "64"],
        &["grp", "--limit", "3"],
        &["grp", "--limit", "1024"],
    ] {
        let r = st(extra);
        assert_eq!(r.code, 0, "{extra:?}: {}{}", r.out, r.err);
    }
}

#[test]
fn depth_and_limit_need_a_directory_that_is_scanned() {
    let (s, grp) = grouped();
    let b = &s.w.base;
    let ws = "grp/ws";
    for (paths, flag, value) in [
        (&[][..], "--depth", "1"),
        (&[][..], "--limit", "5"),
        (&[ws][..], "--depth", "1"),
        (&[ws][..], "--limit", "5"),
        (&[ws, "grp/ws/c"][..], "--depth", "0"),
    ] {
        let r = at(&s, b, paths, &[flag, value]);
        assert_eq!(r.code, 2, "{paths:?} {flag}: {}{}", r.out, r.err);
        assert!(r.out.is_empty(), "{paths:?} {flag}: {}", r.out);
        assert!(
            r.err.contains(flag) && r.err.contains("directory"),
            "the error names the flag and the cause: {}",
            r.err
        );
    }
    // one directory among the paths is enough
    let r = at(&s, b, &[ws, "grp"], &["--depth", "3", "--limit", "9"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let _ = grp;
}

#[test]
fn the_run_of_a_repository_path_has_no_scan_options() {
    let (s, _) = grouped();
    let r = at(&s, &s.w.base, &["grp/ws"], &["--json"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    for k in ["depth", "limit", "timeout"] {
        assert!(v["options"].get(k).is_none(), "options.{k}");
    }
    let r = at(&s, &s.w.base, &["grp/ws"], &[]);
    assert!(
        r.out.starts_with("status  paths=grp/ws  repositories=3\n"),
        "{}",
        r.out
    );
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
    // the PATH is the nested submodule itself: its status alone, the root of the workspace is still the top-level superproject
    let r = run(&s.w, &s.n, None, &["status", ".", "--offline", "--json"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    assert_eq!(v["root"], s.ws.to_str().unwrap());
    assert_eq!(v["summary"]["total"], 1);
    assert_eq!(v["repos"][0]["path"], ".");
    assert_eq!(v["repos"][0]["name"], "n");
    assert_eq!(v["repos"][0]["kind"], "root");
}

#[test]
fn a_submodule_named_like_the_root_directory_has_its_own_key() {
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
            r["path"].as_str().unwrap().to_string(),
            r["name"].as_str().unwrap().to_string(),
            r["kind"].clone(),
            r["depth"].clone(),
        )
    };
    assert_eq!(
        repos.iter().map(key).collect::<Vec<_>>(),
        [
            (".".to_string(), "ws".to_string(), json!("root"), json!(0)),
            (
                "c".to_string(),
                "c".to_string(),
                json!("submodule"),
                json!(1)
            ),
            (
                "c/n".to_string(),
                "c/n".to_string(),
                json!("submodule"),
                json!(2)
            ),
            (
                "ws".to_string(),
                "ws".to_string(),
                json!("submodule"),
                json!(1)
            ),
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
    let r = run(&s.w, &s.ws, None, &["status", ".", "--offline"]);
    assert!(
        flat(&rows(&r.out)[0]).ends_with("PRs n/a | Issues n/a (offline)"),
        "{}",
        r.out
    );
    let r = run(&s.w, &s.ws, None, &["status", "."]);
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
    let r = run(&s.w, &s.ws, Some(&api.url), &["status", "."]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    assert!(
        flat(&rows(&r.out)[0]).ends_with("PRs 2 | Issues ? (not-found)"),
        "{}",
        r.out
    );
}

#[test]
fn stats_verbose_and_review_are_usage_errors_with_a_path() {
    let s = setup();
    for flag in ["--stats", "--verbose", "--review"] {
        // without --offline: --stats, --verbose and --review conflict with it by themselves
        let r = run(&s.w, &s.ws, None, &["status", ".", flag]);
        assert_eq!(r.code, 2, "{flag}: {}{}", r.out, r.err);
        assert!(r.out.is_empty(), "{flag}: {}", r.out);
    }
}

#[test]
fn a_gitlink_without_a_checkout_is_no_depth_limit_notice() {
    let s = setup();
    // a clone without a checkout of its gitlink `c`: the gitlink is in the index, nothing is checked out
    let g = s.w.p("grpc");
    std::fs::create_dir(&g).unwrap();
    s.w.git(&s.w.base, &["clone", "-q", "ws", "grpc/wsc"]);
    let r = at(&s, &s.w.base, &["grpc"], &["--json", "--depth", "1"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    assert_eq!(v["summary"]["total"], 1);
    assert!(
        !notice_codes(&v).contains(&"depth-limit".to_string()),
        "{v}"
    );
}

/// `grp2`: the directory `grp` of `grouped()` plus a second repository `grp/a/x` and a bare repository, a symlink and a
/// plain directory that must not be listed.
fn scan_tree(s: &S) -> PathBuf {
    let grp = s.w.p("grp");
    let x = grp.join("a/x");
    std::fs::create_dir_all(&x).unwrap();
    s.w.git(&x, &["init", "-q"]);
    s.w.commit(&x, "x.txt", "x\n");
    // a repository inside a repository that is no submodule is not listed
    let inner = s.ws.join("inner");
    std::fs::create_dir(&inner).unwrap();
    s.w.git(&inner, &["init", "-q"]);
    // a bare repository has no work tree, a symlink is not followed, a directory without repositories is empty
    s.w.git(
        &s.w.base,
        &["clone", "-q", "--bare", "ws.git", "grp/bare.git"],
    );
    std::os::unix::fs::symlink(s.w.p("c-up"), grp.join("link")).unwrap();
    std::fs::create_dir_all(grp.join("empty/deeper")).unwrap();
    grp
}

#[test]
fn a_directory_without_a_repository_is_scanned_below_in_sorted_order() {
    let (s, _) = grouped();
    let grp = scan_tree(&s);
    let r = at(&s, &s.w.base, &["grp"], &["--json"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    let keys: Vec<&str> = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .map(|r| r["path"].as_str().unwrap())
        .collect();
    // `a/x` sorts before `ws`; the submodules follow their repository; bare, link and inner are not listed
    assert_eq!(keys, ["grp/a/x", "grp/ws", "grp/ws/c", "grp/ws/c/n"], "{v}");
    let kinds: Vec<&str> = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .map(|r| r["kind"].as_str().unwrap())
        .collect();
    assert_eq!(kinds, ["root", "root", "submodule", "submodule"]);
    // the scan also works from inside: the keys are relative to the current directory
    let r = at(&s, &grp, &["."], &["--json"]);
    let keys: Vec<String> = r.json()["repos"]
        .as_array()
        .unwrap()
        .iter()
        .map(|r| r["path"].as_str().unwrap().to_string())
        .collect();
    assert_eq!(keys, ["a/x", "ws", "ws/c", "ws/c/n"]);
}

#[test]
fn a_repository_path_lists_that_repository_and_its_submodules_only() {
    let (s, _) = grouped();
    scan_tree(&s);
    let r = at(&s, &s.w.base, &["grp/ws/c"], &["--json"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    let keys: Vec<&str> = v["repos"]
        .as_array()
        .unwrap()
        .iter()
        .map(|r| r["path"].as_str().unwrap())
        .collect();
    assert_eq!(keys, ["grp/ws/c", "grp/ws/c/n"]);
    assert_eq!(v["repos"][0]["kind"], "root");
    assert_eq!(v["repos"][0]["depth"], 0);
    assert_eq!(v["repos"][1]["kind"], "submodule");
    assert_eq!(v["repos"][1]["depth"], 1);
    // a linked worktree is a repository path too
    s.w.git(
        &s.ws,
        &[
            "worktree",
            "add",
            "-q",
            "-b",
            "wt",
            s.w.p("wt").to_str().unwrap(),
        ],
    );
    let r = at(&s, &s.w.base, &["wt"], &["--json"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    assert_eq!(v["repos"][0]["path"], "wt");
    assert_eq!(v["repos"][0]["branch"], "wt");
}

#[test]
fn several_paths_keep_their_order_and_a_repository_is_listed_once() {
    let (s, _) = grouped();
    scan_tree(&s);
    let b = &s.w.base;
    let keys = |r: &Run| -> Vec<String> {
        r.json()["repos"]
            .as_array()
            .unwrap()
            .iter()
            .map(|r| r["path"].as_str().unwrap().to_string())
            .collect()
    };
    // argument order, sorted inside one path
    let r = at(&s, b, &["grp/a/x", "grp/ws/c"], &["--json"]);
    assert_eq!(keys(&r), ["grp/a/x", "grp/ws/c", "grp/ws/c/n"]);
    let r = at(&s, b, &["grp/ws/c", "grp/a/x"], &["--json"]);
    assert_eq!(keys(&r), ["grp/ws/c", "grp/ws/c/n", "grp/a/x"]);
    // `.` and `..`, the same repository by a symlink and the submodule named again: one row each, first mention wins
    std::os::unix::fs::symlink(&s.ws, s.w.p("grp/ws-link")).unwrap();
    let r = run(
        &s.w,
        &s.ws,
        None,
        &[
            "status",
            ".",
            "..",
            "../ws",
            "../ws-link",
            "c",
            "c/n",
            "--offline",
            "--json",
        ],
    );
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    // `..` is the directory `grp`: `ws` is known already, `a/x` is new
    assert_eq!(keys(&r), [".", "c", "c/n", "../a/x"], "{v}");
    assert_eq!(
        v["options"]["paths"],
        json!([".", "..", "../ws", "../ws-link", "c", "c/n"])
    );
    // the kind and the depth are those of the first mention: `c` first came as a submodule of `.`
    assert_eq!(repo(&v, "c")["kind"], "submodule");
    assert_eq!(repo(&v, "c")["depth"], 1);
    // the other way round: `c` first is a root
    let r = run(
        &s.w,
        &s.ws,
        None,
        &["status", "c", ".", "--offline", "--json"],
    );
    let v = r.json();
    assert_eq!(keys(&r), ["c", "c/n", "."]);
    assert_eq!(repo(&v, "c")["kind"], "root");
    assert_eq!(v["summary"]["total"], 3);
}

#[test]
fn the_key_of_a_repository_is_its_path_and_the_name_is_only_display() {
    let (s, _) = grouped();
    let grp = scan_tree(&s);
    // two repositories with the same name
    let x2 = grp.join("b/x");
    std::fs::create_dir_all(&x2).unwrap();
    s.w.git(&x2, &["init", "-q"]);
    s.w.commit(&x2, "x.txt", "x\n");
    std::fs::write(x2.join("dirty.txt"), "d\n").unwrap();
    let b = &s.w.base;
    let r = at(&s, b, &["grp/a/x", "grp/b/x"], &["--json"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    let v = r.json();
    assert_eq!(v["repos"][0]["name"], "x");
    assert_eq!(v["repos"][1]["name"], "x");
    assert_eq!(v["repos"][0]["path"], "grp/a/x");
    assert_eq!(v["repos"][1]["path"], "grp/b/x");
    // the text and the notices are told apart by the key
    let r = at(&s, b, &["grp/a/x", "grp/b/x"], &[]);
    let ls: Vec<String> = rows(&r.out).iter().map(|l| flat(l)).collect();
    assert!(ls[0].starts_with("! grp/a/x "), "{ls:?}");
    assert!(ls[1].starts_with("! grp/b/x "), "{ls:?}");
    s.w.git(
        &x2,
        &["config", "remote.origin.url", "https://github.com/test/x"],
    );
    let api = FakeRoutes::start(|_, _| (404, vec![], "{}".into()));
    let r = run(&s.w, b, Some(&api.url), &["status", "grp/a/x", "grp/b/x"]);
    assert!(
        r.out.contains("\n  grp/b/x: pull requests not read ("),
        "{}",
        r.out
    );
    let v = run(
        &s.w,
        b,
        Some(&api.url),
        &["status", "grp/a/x", "grp/b/x", "--json"],
    )
    .json();
    let n = v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .find(|n| n["code"] == "prs-not-read")
        .unwrap()
        .clone();
    assert_eq!(n["repos"], json!(["grp/b/x"]));
    // a path outside the current directory is the absolute path
    let abs = grp.join("a/x");
    let r = run(
        &s.w,
        &x2,
        None,
        &["status", abs.to_str().unwrap(), ".", "--offline", "--json"],
    );
    let v = r.json();
    assert_eq!(v["repos"][0]["path"], abs.to_str().unwrap());
    assert_eq!(v["repos"][1]["path"], ".");
    // a relative path outside it keeps its `..` components
    let r = run(
        &s.w,
        &x2,
        None,
        &["status", "../../a/x", "--offline", "--json"],
    );
    assert_eq!(r.json()["repos"][0]["path"], "../../a/x");
    // inside the current directory it is relative, whatever way it was written
    let r = run(
        &s.w,
        &grp,
        None,
        &["status", "./a/../a/x", "--offline", "--json"],
    );
    assert_eq!(r.json()["repos"][0]["path"], "a/x");
}

#[test]
fn a_path_that_does_not_exist_or_cannot_be_read_is_a_usage_error_and_prints_nothing() {
    let (s, _) = grouped();
    let b = &s.w.base;
    std::fs::write(s.w.p("a-file"), "f\n").unwrap();
    for (paths, why) in [
        (&["no-such-dir"][..], "PATH no-such-dir: "),
        (&["grp", "no-such-dir"], "PATH no-such-dir: "),
        (&["no-such-dir", "grp"], "PATH no-such-dir: "),
        (&["a-file"], "PATH a-file is not a directory"),
        (&["grp", "a-file"], "PATH a-file is not a directory"),
    ] {
        let r = at(&s, b, paths, &[]);
        assert_eq!(r.code, 2, "{paths:?}: {}{}", r.out, r.err);
        assert!(r.out.is_empty(), "{paths:?}: {}", r.out);
        assert!(
            r.err.starts_with(&format!("stw: {why}")),
            "{paths:?}: {}",
            r.err
        );
        let r = at(&s, b, paths, &["--json"]);
        assert_eq!(r.code, 2, "{paths:?}: {}{}", r.out, r.err);
        assert!(r.out.is_empty(), "{paths:?}: {}", r.out);
    }
    // a directory that cannot be read (not as root: the harness may run as root)
    use std::os::unix::fs::PermissionsExt;
    let locked = s.w.p("locked");
    std::fs::create_dir(&locked).unwrap();
    std::fs::set_permissions(&locked, std::fs::Permissions::from_mode(0o000)).unwrap();
    let unreadable = std::fs::read_dir(&locked).is_err();
    let r = at(&s, b, &["grp", "locked"], &[]);
    std::fs::set_permissions(&locked, std::fs::Permissions::from_mode(0o755)).unwrap();
    if unreadable {
        assert_eq!(r.code, 2, "{}{}", r.out, r.err);
        assert!(r.out.is_empty(), "{}", r.out);
        assert!(r.err.contains("PATH locked cannot be read"), "{}", r.err);
    }
}

#[test]
fn without_a_path_status_is_the_workspace_status_as_before() {
    let s = setup();
    let r = run(&s.w, &s.ws, None, &["status", "--offline"]);
    assert_eq!(r.code, 0, "{}{}", r.out, r.err);
    // the default text: its blocks, not the one-line rows
    assert!(r.out.contains("Counts\n"), "{}", r.out);
    assert!(!r.out.starts_with("status  "), "{}", r.out);
    let v = run(&s.w, &s.ws, None, &["status", "--offline", "--json"]).json();
    assert_eq!(v["summary"]["total"], 3);
    assert_eq!(v["options"]["paths"], json!([]));
    // --all is gone: no alias
    let r = run(&s.w, &s.ws, None, &["status", "--all", "--offline"]);
    assert_eq!(r.code, 2, "{}{}", r.out, r.err);
    assert!(r.out.is_empty(), "{}", r.out);
}

#[test]
fn the_unreadable_repository_of_a_scan_is_an_error_row_and_exits_1() {
    let (s, _) = grouped();
    std::fs::write(s.n.join(".git"), "gitdir: /nonexistent/stw/none\n").unwrap();
    let r = at(&s, &s.w.base, &["grp"], &[]);
    assert_eq!(r.code, 1, "{}{}", r.out, r.err);
    assert!(
        rows(&r.out).iter().any(|l| l.starts_with("! grp/ws/c/n ")),
        "{}",
        r.out
    );
}

#[test]
fn a_refused_gitlink_path_is_reported_below_the_key_of_its_repository() {
    let (s, _) = grouped();
    let n1 = s.w.git(&s.n, &["rev-parse", "HEAD"]);
    s.w.git(
        &s.c,
        &[
            "update-index",
            "--add",
            "--cacheinfo",
            &format!("160000,{n1},bad name"),
        ],
    );
    let v = at(&s, &s.w.base, &["grp/ws"], &["--json"]).json();
    assert_eq!(v["rejected_paths"][0]["path"], "grp/ws/c/bad name", "{v}");
    assert_eq!(v["summary"]["total"], 3, "no row for the refused path");
    let n = v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .find(|n| n["code"] == "rejected-path")
        .unwrap()
        .clone();
    assert!(
        n["message"]
            .as_str()
            .unwrap()
            .starts_with("submodule path not accepted: grp/ws/c/bad name: "),
        "{n}"
    );
    // the same repository from inside: the key is `.`, the path stays below it
    let r = run(&s.w, &s.ws, None, &["status", ".", "--offline", "--json"]);
    assert_eq!(r.json()["rejected_paths"][0]["path"], "c/bad name");
    // named twice: reported once
    let v = at(&s, &s.w.base, &["grp/ws", "grp/ws/c"], &["--json"]).json();
    assert_eq!(v["rejected_paths"].as_array().unwrap().len(), 1, "{v}");
}

#[test]
fn a_scan_leaves_out_the_agent_rules_checkout_unless_it_is_named() {
    let (s, grp) = grouped();
    scan_tree(&s);
    let keys = |paths: &[&str], rules: &Path| -> Vec<String> {
        let r = at(
            &s,
            &s.w.base,
            paths,
            &["--json", "--rules-path", rules.to_str().unwrap()],
        );
        assert_eq!(r.code, 0, "{}{}", r.out, r.err);
        r.json()["repos"]
            .as_array()
            .unwrap()
            .iter()
            .map(|r| r["path"].as_str().unwrap().to_string())
            .collect()
    };
    // a repository found by the scan
    let x = grp.join("a/x");
    assert_eq!(keys(&["grp"], &x), ["grp/ws", "grp/ws/c", "grp/ws/c/n"]);
    // a submodule of a repository found by the scan; its own submodule is still listed
    assert_eq!(keys(&["grp"], &s.c), ["grp/a/x", "grp/ws", "grp/ws/c/n"]);
    // named, it is listed: as a repository PATH, and as a submodule of a repository when it is named as well
    assert_eq!(keys(&["grp/a/x"], &x), ["grp/a/x"]);
    assert_eq!(
        keys(&["grp/ws", "grp/ws/c"], &s.c),
        ["grp/ws", "grp/ws/c", "grp/ws/c/n"]
    );
}

#[test]
fn a_scanned_directory_without_a_repository_is_a_notice_and_exit_0() {
    let (s, _) = grouped();
    scan_tree(&s);
    let b = &s.w.base;
    let one = |path: &str| {
        let r = at(&s, b, &[path], &["--json"]);
        assert_eq!(r.code, 0, "{path}: {}{}", r.out, r.err);
        r.json()
    };
    let note = |v: &Value| {
        v["notices"]
            .as_array()
            .unwrap()
            .iter()
            .find(|n| n["code"] == "no-repositories")
            .cloned()
    };
    // a bare repository has no work tree: it is named as such
    let v = one("grp/bare.git");
    assert_eq!(v["summary"]["total"], 0);
    let n = note(&v).expect("a notice");
    assert_eq!(n["source"], "scan");
    assert_eq!(n["repos"], json!(["grp/bare.git"]));
    assert_eq!(
        n["message"],
        "grp/bare.git: no repository found (a bare repository has no work tree and is not listed)"
    );
    // a directory with nothing in it
    let v = one("grp/empty");
    assert_eq!(
        note(&v).unwrap()["message"],
        "grp/empty: no repository found"
    );
    let r = at(&s, b, &["grp/empty"], &[]);
    assert!(
        r.out.contains("\n  grp/empty: no repository found\n"),
        "{}",
        r.out
    );
    // a directory with repositories, or whose repositories lie below the bound, has no such notice
    assert!(note(&one("grp")).is_none());
    let v = at(&s, b, &["grp"], &["--json", "--depth", "0"]).json();
    assert!(note(&v).is_none(), "{v}");
}
