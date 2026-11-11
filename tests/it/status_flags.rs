//! `stw status` flags ported from Go (`internal/cli/status.go`): `--icons`, `--worktrees`, `--fetch`, `--timeout`,
//! `--stats`, `--verbose`, `--long`. Go cases: `internal/cli/status_command_test.go`, `internal/statuscmd/terminal_test.go`.
use std::process::{Command, Output};

use crate::common::*;
use serde_json::Value;
use stw::status::{resolve_icons, Icons, Terminal};

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

fn out(o: &Output) -> String {
    String::from_utf8_lossy(&o.stdout).into_owned()
}

fn json_of(o: &Output) -> Value {
    serde_json::from_slice(&o.stdout).unwrap_or_else(|e| panic!("{e}: {}", out(o)))
}

fn err(o: &Output) -> String {
    String::from_utf8_lossy(&o.stderr).into_owned()
}

/// A root with one untracked file (needs attention) and a submodule that cannot be read (failed).
fn attention_and_failed() -> Fx {
    let fx = status_fixture();
    fx.write("untracked.txt", "x\n");
    fx.write(
        ".gitmodules",
        "[submodule \"broken\"]\n\tpath = broken\n\turl = x\n",
    );
    fx.write("broken/.git", "gitdir: /nonexistent/stw/gitdir\n");
    fx
}

// ---------------------------------------------------------------------------------------------------------
// --icons (Go: terminal_test.go ResolveIcons; status_command_test.go TestStatusOfflineIconsChangeOnlyTheMarkers)
// ---------------------------------------------------------------------------------------------------------

#[test]
fn icons_default_is_unicode_only_on_a_utf8_terminal() {
    let t = |tty, all, ctype, lang, term| Terminal::from_env_values(tty, all, ctype, lang, term);
    let unicode = |t: Terminal| resolve_icons(None, &t) == Icons::Unicode;
    assert!(unicode(t(
        true,
        None,
        None,
        Some("en_US.UTF-8"),
        Some("xterm")
    )));
    assert!(unicode(t(true, Some("C.utf8"), None, None, None)));
    // not a terminal: a pipe or a file
    assert!(!unicode(t(false, None, None, Some("en_US.UTF-8"), None)));
    // the first locale variable that is set decides: LC_ALL=C beats LANG=UTF-8
    assert!(!unicode(t(
        true,
        Some("C"),
        None,
        Some("en_US.UTF-8"),
        None
    )));
    assert!(!unicode(t(
        true,
        None,
        Some("POSIX"),
        Some("en_US.UTF-8"),
        None
    )));
    // an empty variable is as good as unset
    assert!(unicode(t(true, Some(""), None, Some("de_DE.UTF-8"), None)));
    // no locale at all, or a locale that is not UTF-8
    assert!(!unicode(t(true, None, None, None, None)));
    assert!(!unicode(t(
        true,
        None,
        None,
        Some("de_DE.ISO-8859-1"),
        None
    )));
    // TERM=dumb
    assert!(!unicode(t(
        true,
        None,
        None,
        Some("en_US.UTF-8"),
        Some("dumb")
    )));
    // an explicit value always wins
    let off = t(false, None, None, None, None);
    assert_eq!(resolve_icons(Some("nerd"), &off), Icons::Nerd);
    assert_eq!(resolve_icons(Some("unicode"), &off), Icons::Unicode);
    let on = t(true, None, None, Some("en_US.UTF-8"), None);
    assert_eq!(resolve_icons(Some("ascii"), &on), Icons::Ascii);
}

#[test]
fn icons_change_only_the_markers_of_the_repository_lines() {
    let fx = attention_and_failed();
    let r = path_str(&fx.path);
    let plain = stw(None, &["status", "--root", &r, "--offline"]);
    let ascii = stw(
        None,
        &["status", "--root", &r, "--offline", "--icons", "ascii"],
    );
    let unicode = stw(
        None,
        &["status", "--root", &r, "--offline", "--icons", "unicode"],
    );
    let nerd = stw(
        None,
        &["status", "--root", &r, "--offline", "--icons", "nerd"],
    );
    for o in [&plain, &ascii, &unicode, &nerd] {
        assert_eq!(o.status.code(), Some(1), "{}", out(o)); // the broken submodule
    }
    let p = out(&plain);
    // off a terminal the default is ascii; an explicit ascii equals it
    assert_eq!(out(&ascii), p);
    assert!(p.contains("\n! "), "{p}");
    // unicode: attention is a triangle, a repository that cannot be read a cross; nothing else changes
    let u = out(&unicode);
    assert!(u.contains("\n\u{25b2} "), "{u}");
    assert!(u.contains("\n\u{2716} broken"), "{u}");
    assert_eq!(
        u.replace("\u{25b2} ", "! ").replace("\u{2716} ", "! "),
        p,
        "only the markers differ"
    );
    let n = out(&nerd);
    assert!(n.contains("\n\u{f071} "), "{n}");
    assert!(n.contains("\n\u{f057} broken"), "{n}");
    assert_eq!(
        n.replace("\u{f071} ", "! ").replace("\u{f057} ", "! "),
        p,
        "only the markers differ"
    );
    // aligned like the ascii text: every marker is two columns wide
    for text in [&p, &u, &n] {
        let l = text.lines().find(|l| l.contains(" untracked")).unwrap();
        assert_eq!(l.chars().nth(1), Some(' '), "{l:?}");
    }
}

#[test]
fn icons_never_reach_the_json_and_a_bad_value_is_a_usage_error() {
    let fx = attention_and_failed();
    let r = path_str(&fx.path);
    let base = out(&stw(None, &["status", "--root", &r, "--offline", "--json"]));
    assert!(
        serde_json::from_str::<Value>(&base).unwrap()["options"]["icons"].is_null(),
        "{base}"
    );
    for set in ["unicode", "nerd", "ascii"] {
        let o = stw(
            None,
            &[
                "status",
                "--root",
                &r,
                "--offline",
                "--json",
                "--icons",
                set,
            ],
        );
        assert_eq!(out(&o), base, "--icons {set}");
    }
    for bad in ["emoji", "Unicode", ""] {
        let o = stw(None, &["status", "--root", &r, "--offline", "--icons", bad]);
        assert_eq!(o.status.code(), Some(2), "{bad:?}");
        assert!(out(&o).is_empty(), "{bad:?}: nothing on stdout");
        assert!(err(&o).contains("--icons"), "{}", err(&o));
    }
}

// ---------------------------------------------------------------------------------------------------------
// --worktrees (Go: stwsync/status.go worktrees(), cli/status.go writeStatusRepos; golden status-offline-worktrees.txt)
// ---------------------------------------------------------------------------------------------------------

/// Root `repo` on main with branches topic, other, third, gone, locked; linked worktrees: `clean` (topic),
/// `dirty` (other, with an untracked file), `locked` (locked, reason "test lock"), `detached` (HEAD on a commit),
/// `gone` (registration whose directory is gone, prunable) and `lockedgone` (locked, directory gone).
fn with_worktrees() -> Fx {
    let fx = status_fixture();
    let c0 = fx.tip("main");
    for b in ["topic", "other", "third", "gone", "lockedbr", "gonelocked"] {
        fx.branch(b, c0);
    }
    real_worktree(&fx, "clean", "topic");
    let dirty = real_worktree(&fx, "dirty", "other");
    std::fs::write(dirty.join("new.txt"), "x\n").unwrap();
    real_worktree(&fx, "locked", "lockedbr");
    std::fs::write(fx.path.join(".git/worktrees/locked/locked"), "test lock\n").unwrap();
    let detached = real_worktree(&fx, "detached", "third");
    std::fs::write(
        fx.path.join(".git/worktrees/detached/HEAD"),
        format!("{c0}\n"),
    )
    .unwrap();
    assert!(detached.is_dir());
    add_worktree(&fx, "gone", "gone", false, false);
    add_worktree(&fx, "lockedgone", "gonelocked", false, true);
    // a relative link (a `..` in the checkout path): listed like the others
    let base = fx.path.parent().unwrap();
    std::fs::create_dir_all(base.join("x")).unwrap();
    fx.branch("relbr", c0);
    real_worktree_at(&fx, "rel", "relbr", &base.join("x/../wt-rel"));
    fx
}

fn wt<'a>(v: &'a Value, suffix: &str) -> &'a Value {
    v["repos"][0]["worktree_list"]
        .as_array()
        .unwrap_or_else(|| panic!("no list in {v}"))
        .iter()
        .find(|w| w["path"].as_str().unwrap().ends_with(suffix))
        .unwrap_or_else(|| panic!("no worktree {suffix} in {v}"))
}

#[test]
fn worktrees_lists_the_linked_ones_with_their_state_and_not_the_main_checkout() {
    let fx = with_worktrees();
    let r = path_str(&fx.path);
    let o = stw(
        None,
        &["status", "--root", &r, "--offline", "--json", "--worktrees"],
    );
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let v = json_of(&o);
    assert_eq!(v["options"]["worktrees"], true);
    let list = v["repos"][0]["worktree_list"].as_array().unwrap();
    assert_eq!(list.len(), 7, "{list:?}: no main checkout");
    let clean = wt(&v, "/wt-clean");
    assert_eq!(clean["branch"], "topic");
    assert_eq!(
        (
            clean["detached"].clone(),
            clean["dirty"].clone(),
            clean["missing"].clone()
        ),
        (false.into(), false.into(), false.into()),
        "{clean}"
    );
    assert!(
        clean["locked"].is_null() && clean["prunable"] == false,
        "{clean}"
    );
    assert_eq!(wt(&v, "/wt-dirty")["dirty"], true);
    let rel = wt(&v, "/wt-rel");
    assert_eq!(
        (rel["branch"].clone(), rel["dirty"].clone()),
        ("relbr".into(), false.into()),
        "{rel}"
    );
    assert!(
        !rel["path"].as_str().unwrap().contains(".."),
        "shown without the `..`: {rel}"
    );
    let locked = wt(&v, "/wt-locked");
    assert_eq!(locked["locked"], "test lock", "{locked}");
    assert_eq!(locked["dirty"], false);
    let det = wt(&v, "/wt-detached");
    assert!(det["branch"].is_null() && det["detached"] == true, "{det}");
    let gone = wt(&v, "/wt-gone");
    assert_eq!(
        (gone["missing"].clone(), gone["prunable"].clone()),
        (true.into(), true.into()),
        "{gone}"
    );
    assert_eq!(
        gone["dirty"], false,
        "a missing checkout makes no dirty claim"
    );
    let lg = wt(&v, "/wt-lockedgone");
    assert_eq!(
        (
            lg["missing"].clone(),
            lg["prunable"].clone(),
            lg["locked"].clone()
        ),
        (true.into(), false.into(), "test lock".into()),
        "a locked registration is never prunable: {lg}"
    );
    // the count keeps its convention: existing linked checkouts, no stale registrations, no main checkout
    assert_eq!(v["repos"][0]["counts"]["worktrees"], 5);
    // without the flag the field is null and everything else is the same
    let plain = json_of(&stw(None, &["status", "--root", &r, "--offline", "--json"]));
    assert!(plain["repos"][0]["worktree_list"].is_null());
    assert_eq!(plain["options"]["worktrees"], false);
    assert_eq!(plain["repos"][0]["counts"], v["repos"][0]["counts"]);
}

#[test]
fn worktrees_text_has_one_line_per_linked_worktree_under_its_repository() {
    let fx = with_worktrees();
    let r = path_str(&fx.path);
    let plain = out(&stw(None, &["status", "--root", &r, "--offline"]));
    assert!(!plain.contains("    worktree "), "{plain}");
    let t = out(&stw(
        None,
        &["status", "--root", &r, "--offline", "--worktrees"],
    ));
    let lines: Vec<&str> = t
        .lines()
        .filter(|l| l.starts_with("    worktree "))
        .collect();
    assert_eq!(lines.len(), 7, "{t}");
    let find = |s: &str| {
        lines
            .iter()
            .find(|l| l.contains(s))
            .unwrap_or_else(|| panic!("{s} in {t}"))
    };
    assert!(find("/wt-clean").ends_with("  topic"), "{t}");
    assert!(find("/wt-dirty").ends_with("  other  dirty"), "{t}");
    assert!(
        find("/wt-locked").ends_with("  lockedbr  locked: test lock"),
        "{t}"
    );
    assert!(find("/wt-detached").ends_with("  detached"), "{t}");
    assert!(
        find("/wt-gone").ends_with("  gone  prunable  missing"),
        "{t}"
    );
    assert!(
        find("/wt-lockedgone").ends_with("  gonelocked  locked: test lock  missing"),
        "{t}"
    );
    // directly below the repository line, before Notices
    let at = |s: &str| t.find(s).unwrap();
    assert!(
        at("Repositories (") < at("    worktree ") && at("    worktree ") < at("Notices ("),
        "{t}"
    );
    // without the flag the rest of the text is the same
    assert_eq!(
        t.lines()
            .filter(|l| !l.starts_with("    worktree "))
            .collect::<Vec<_>>(),
        plain.lines().collect::<Vec<_>>()
    );
}

#[test]
fn worktree_text_from_the_repository_is_escaped() {
    let fx = status_fixture();
    let c0 = fx.tip("main");
    fx.branch("topic", c0);
    let base = fx.path.parent().unwrap();
    let dir = base.join("wt-\u{1b}[31mPATH");
    add_worktree_at(&fx, "evil", "topic", &dir, true, false);
    std::fs::write(
        fx.path.join(".git/worktrees/evil/HEAD"),
        "ref: refs/heads/a\u{1b}]0;title\u{7}b\n",
    )
    .unwrap();
    std::fs::write(
        fx.path.join(".git/worktrees/evil/locked"),
        "why \u{1b}[1mnow\n",
    )
    .unwrap();
    let r = path_str(&fx.path);
    let o = stw(None, &["status", "--root", &r, "--offline", "--worktrees"]);
    let t = out(&o);
    assert!(
        !t.contains('\u{1b}') && !t.contains('\u{7}'),
        "raw control character: {t:?}"
    );
    assert!(t.contains("\\u{1b}[31mPATH"), "{t}");
    assert!(t.contains("locked: why \\u{1b}[1mnow"), "{t}");
    // the JSON keeps the raw value (JSON escapes it itself)
    let v = json_of(&stw(
        None,
        &["status", "--root", &r, "--offline", "--worktrees", "--json"],
    ));
    assert!(v["repos"][0]["worktree_list"][0]["path"]
        .as_str()
        .unwrap()
        .contains('\u{1b}'));
}

#[test]
fn worktrees_reads_only() {
    let fx = with_worktrees();
    let r = path_str(&fx.path);
    let before = tree_of(fx.path.parent().unwrap());
    for args in [
        vec!["status", "--root", &r, "--offline", "--worktrees"],
        vec!["status", "--root", &r, "--offline", "--worktrees", "--json"],
    ] {
        assert_eq!(stw(None, &args).status.code(), Some(0));
    }
    assert_eq!(
        tree_of(fx.path.parent().unwrap()),
        before,
        "--worktrees changed files"
    );
}

/// Every file below `root` (relative path to content), for the read-only checks.
fn tree_of(root: &std::path::Path) -> std::collections::BTreeMap<String, Vec<u8>> {
    fn walk(
        base: &std::path::Path,
        dir: &std::path::Path,
        m: &mut std::collections::BTreeMap<String, Vec<u8>>,
    ) {
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
    let mut m = std::collections::BTreeMap::new();
    walk(root, root, &mut m);
    m
}

// ---------------------------------------------------------------------------------------------------------
// --fetch (Go: stwsync/status.go `git fetch --no-tags --no-write-fetch-head --no-recurse-submodules --no-prune`,
// stwsync/status_test.go fetch cases). Harness: every repository lives in a tempdir of `World`.
// ---------------------------------------------------------------------------------------------------------

use crate::sync_flags::World;

struct F {
    w: World,
    origin: std::path::PathBuf,
    ws: std::path::PathBuf,
}

/// `origin` (bare) with `main` and `gone`; `ws` is a clone of it with a local branch `keep`; then origin gets one
/// more commit on main and loses `gone`, so that `ws` is behind by one only after a fetch.
fn fetch_setup() -> F {
    let w = World::new();
    let seed = w.p("seed");
    std::fs::create_dir(&seed).unwrap();
    w.git(&seed, &["init", "-q"]);
    w.commit(&seed, "a.txt", "a\n");
    w.git(&seed, &["branch", "gone"]);
    let origin = w.p("origin.git");
    w.git(&w.base, &["clone", "-q", "--bare", "seed", "origin.git"]);
    let ws = w.p("ws");
    w.git(&w.base, &["clone", "-q", "origin.git", "ws"]);
    w.git(&ws, &["branch", "keep"]);
    w.commit(&seed, "b.txt", "b\n");
    w.git(
        &seed,
        &["push", "-q", origin.to_str().unwrap(), "HEAD:main"],
    );
    w.git(&origin, &["branch", "-q", "-D", "gone"]);
    F { w, origin, ws }
}

/// `stw status` on the workspace of `f` with the git of the test environment (HOME inside the tempdir).
fn status_of(f: &F, args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &f.w.home)
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .current_dir(&f.w.base)
        .arg("status")
        .args(["--root"])
        .arg(&f.ws)
        .args(args)
        .output()
        .unwrap()
}

/// Everything below `ws` that a fetch may not touch: all files but the object store, the remote-tracking refs
/// and their reflogs, FETCH_HEAD.
fn untouchable(ws: &std::path::Path) -> std::collections::BTreeMap<String, Vec<u8>> {
    tree_of(ws)
        .into_iter()
        .filter(|(k, _)| {
            !(k.starts_with(".git/objects")
                || k.starts_with(".git/refs/remotes")
                || k.starts_with(".git/logs/refs/remotes")
                || k == ".git/FETCH_HEAD")
        })
        .collect()
}

fn refs(f: &F) -> String {
    f.w.git(
        &f.ws,
        &["for-each-ref", "--format=%(refname) %(objectname)"],
    )
}

#[test]
fn without_fetch_origin_is_not_asked_and_the_counts_stay_as_of_the_last_fetch() {
    let f = fetch_setup();
    let o = status_of(&f, &["--offline", "--json"]);
    let v = json_of(&o);
    assert_eq!(v["options"]["fetch"], false);
    assert_eq!(v["repos"][0]["fetch"]["state"], "not-run");
    assert_eq!(v["repos"][0]["behind"], 0);
    assert_eq!(v["repos"][0]["counts"]["branches"]["remote"], 2); // main, gone
    assert!(v["repos"][0]["fetch_error"].is_null());
}

#[test]
fn fetch_updates_the_remote_tracking_refs_and_nothing_else() {
    let f = fetch_setup();
    let refs_before = refs(&f);
    let tree_before = untouchable(&f.ws);
    let o = status_of(&f, &["--offline", "--json", "--fetch"]);
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let v = json_of(&o);
    let r = &v["repos"][0];
    assert_eq!(v["options"]["fetch"], true);
    assert_eq!(r["fetch"]["state"], "ok", "{r}");
    assert_eq!(
        r["fetch"]["via"], "git",
        "a local-path remote runs through the runner"
    );
    assert_eq!(r["behind"], 1, "the fetch moved origin/main: {r}");
    // not pruned: `gone` is gone at origin and stays here
    assert_eq!(r["counts"]["branches"]["remote"], 2);
    let refs_after = refs(&f);
    let changed: Vec<&str> = refs_after
        .lines()
        .filter(|l| !refs_before.lines().any(|b| b == *l))
        .collect();
    // origin/main moved; origin/HEAD is a symbolic ref to it and shows the same id
    assert!(
        changed
            .iter()
            .any(|l| l.starts_with("refs/remotes/origin/main "))
            && changed
                .iter()
                .all(|l| l.starts_with("refs/remotes/origin/")),
        "{changed:?}"
    );
    assert_eq!(
        refs_before.lines().count(),
        refs_after.lines().count(),
        "no ref was added or removed"
    );
    assert_eq!(
        untouchable(&f.ws),
        tree_before,
        "branches, index, working tree, config: unchanged"
    );
    // the objects of the new commit are there
    f.w.git(&f.ws, &["rev-parse", "--verify", "-q", "origin/main"]);
}

#[test]
fn fetch_never_prunes_not_even_with_fetch_prune_configured() {
    let f = fetch_setup();
    f.w.git(&f.ws, &["config", "fetch.prune", "true"]);
    f.w.git(&f.ws, &["config", "remote.origin.pruneTags", "true"]);
    let o = status_of(&f, &["--offline", "--fetch"]);
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    assert!(
        !f.w.git(&f.ws, &["branch", "-r", "--list", "origin/gone"])
            .is_empty(),
        "fetch.prune must not apply to status --fetch"
    );
}

#[test]
fn fetch_runs_no_hook_of_the_repository() {
    let f = fetch_setup();
    let marker = f.w.p("hook-ran");
    for hook in [
        "reference-transaction",
        "post-fetch",
        "post-merge",
        "pre-auto-gc",
    ] {
        let p = f.ws.join(".git/hooks").join(hook);
        std::fs::write(&p, format!("#!/bin/sh\ntouch {}\n", marker.display())).unwrap();
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
    }
    let o = status_of(&f, &["--offline", "--fetch"]);
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    assert!(!marker.exists(), "a hook of the repository ran");
    assert_eq!(
        f.w.git(&f.ws, &["rev-parse", "origin/main"]),
        f.w.git(&f.origin, &["rev-parse", "main"]),
        "the fetch itself ran"
    );
}

#[test]
fn a_fetch_refspec_that_writes_outside_the_remote_tracking_namespace_is_refused_before_any_fetch() {
    let f = fetch_setup();
    f.w.git(
        &f.ws,
        &[
            "config",
            "--replace-all",
            "remote.origin.fetch",
            "+refs/heads/*:refs/heads/*",
        ],
    );
    let before = refs(&f);
    let o = status_of(&f, &["--offline", "--json", "--fetch"]);
    assert_eq!(
        o.status.code(),
        Some(0),
        "a failed fetch is a notice: {}{}",
        out(&o),
        err(&o)
    );
    let v = json_of(&o);
    let r = &v["repos"][0];
    assert_eq!(r["fetch"]["state"], "failed", "{r}");
    assert!(r["fetch"]["via"].is_null(), "nothing was started: {r}");
    assert!(
        r["fetch_error"]
            .as_str()
            .unwrap()
            .contains("refs/remotes/origin/"),
        "{r}"
    );
    assert_eq!(refs(&f), before, "no local branch was written");
    // the rest of the status is still there
    assert_eq!(r["branch"], "main");
}

#[test]
fn a_failed_fetch_is_a_detail_line_and_a_notice_and_exit_0() {
    let f = fetch_setup();
    // an origin that is not there (git fails), and a transport stw does not run, whose URL carries a control
    // sequence into the error text (git itself replaces the escape character in its messages)
    for url in ["/nonexistent/stw/gone", "ext::sh -c \u{1b}[31mx"] {
        f.w.git(&f.ws, &["remote", "set-url", "origin", url]);
        let o = status_of(&f, &["--offline", "--fetch"]);
        assert_eq!(o.status.code(), Some(0), "{url}: {}{}", out(&o), err(&o));
        let t = out(&o);
        assert!(t.contains("\n    fetch failed: "), "{url}: {t}");
        let notices = t
            .split("\n\n")
            .find(|b| b.starts_with("Notices ("))
            .unwrap();
        assert!(notices.contains("ws: fetch failed ("), "{url}: {notices}");
        assert!(!t.contains('\u{1b}'), "raw ESC: {t:?}");
        assert!(t.contains("clean"), "the local half is still shown: {t}");
        if url.starts_with("ext::") {
            assert!(
                t.contains("    fetch failed: ") && t.contains("\\u{1b}[31mx"),
                "{t}"
            );
        }
    }
}

#[test]
fn fetch_without_origin_is_no_remote_not_an_error() {
    let f = fetch_setup();
    f.w.git(&f.ws, &["remote", "remove", "origin"]);
    let o = status_of(&f, &["--offline", "--json", "--fetch"]);
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let r = &json_of(&o)["repos"][0]["fetch"];
    assert_eq!(r["state"], "no-remote", "{r}");
    assert!(r["error"].is_null(), "{r}");
}

#[test]
fn fetch_sends_no_github_request_with_offline_and_does_not_leak_a_token() {
    let f = fetch_setup();
    let api = FakeRoutes::start(|_, _| (200, vec![], "[]".into()));
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", &f.w.home)
        .env("GH_TOKEN", TOKEN)
        .env("STW_GITHUB_API_URL", &api.url)
        .current_dir(&f.w.base)
        .args(["status", "--offline", "--fetch", "--json", "--root"])
        .arg(&f.ws)
        .output()
        .unwrap();
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    assert!(api.hits().is_empty(), "{:?}", api.hits());
    assert!(!out(&o).contains(TOKEN) && !err(&o).contains(TOKEN));
}

// ---------------------------------------------------------------------------------------------------------
// --timeout (Go: status_command_test.go TestStatusUsageErrors, TestStatusTimeoutIsTheDeadlineOfTheRemoteHalf)
// ---------------------------------------------------------------------------------------------------------

#[test]
fn timeout_values_are_go_durations_within_1s_to_10m() {
    use std::time::Duration as D;
    let ok = |s: &str| stw::cli::parse_timeout(s);
    assert_eq!(ok("1s"), Ok(D::from_secs(1)));
    assert_eq!(ok("90s"), Ok(D::from_secs(90)));
    assert_eq!(ok("2m"), Ok(D::from_secs(120)));
    assert_eq!(ok("1m30s"), Ok(D::from_secs(90)));
    assert_eq!(ok("1.5s"), Ok(D::from_millis(1500)));
    assert_eq!(ok("1000ms"), Ok(D::from_secs(1)));
    assert_eq!(ok("10m"), Ok(D::from_secs(600)));
    assert_eq!(ok("600s"), Ok(D::from_secs(600)));
    for bad in [
        "0s", "999ms", "10m1s", "11m", "1h", "", "90", "s", "1s2", "soon", "-1m", "1x", "1.2.3s",
    ] {
        assert!(ok(bad).is_err(), "{bad:?} must be refused");
    }
    assert!(ok("999ms").unwrap_err().contains("outside 1s to 10m"));
}

#[test]
fn timeout_out_of_range_or_with_offline_is_a_usage_error_and_nothing_runs() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = FakeRoutes::start(|_, _| (200, vec![], "[]".into()));
    for args in [
        vec!["--timeout", "0s"],
        vec!["--timeout", "999ms"],
        vec!["--timeout", "11m"],
        vec!["--timeout=-1m"],
        vec!["--timeout", "soon"],
        vec!["--offline", "--timeout", "30s"],
        vec!["--offline", "--timeout=90s"],
    ] {
        let mut a = vec!["status", "--root", &r];
        a.extend(args.iter());
        let o = stw(Some(&api.url), &a);
        assert_eq!(o.status.code(), Some(2), "{args:?}: {}", err(&o));
        assert!(out(&o).is_empty(), "{args:?}");
        assert!(err(&o).contains("--timeout"), "{args:?}: {}", err(&o));
    }
    assert!(api.hits().is_empty(), "a usage error sends no request");
    for args in [
        vec!["--timeout", "1s"],
        vec!["--timeout", "10m"],
        vec!["--timeout", "2m", "--icons", "ascii"],
    ] {
        let mut a = vec!["status", "--root", &r];
        a.extend(args.iter());
        assert_eq!(stw(Some(&api.url), &a).status.code(), Some(0), "{args:?}");
    }
}

/// The deadline of the remote half: a forge that answers after 4 s is cut at 1 s; the counters are `?`, the
/// notices say why, exit 0 (a failed GitHub read is a notice in Rust), the local half is intact.
#[test]
fn timeout_cuts_the_github_reads_with_notices_and_question_marks() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = FakeRoutes::start(|_, _| {
        std::thread::sleep(std::time::Duration::from_secs(4));
        (200, vec![], "[]".into())
    });
    let t0 = std::time::Instant::now();
    let o = stw(Some(&api.url), &["status", "--root", &r, "--timeout", "1s"]);
    let took = t0.elapsed();
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    assert!(
        took < std::time::Duration::from_secs(3),
        "the deadline was not enforced: {took:?}"
    );
    let t = out(&o);
    let flat: String = t.split_whitespace().collect::<Vec<_>>().join(" ");
    assert!(flat.contains("PRs ? | Issues ?"), "{t}");
    let notices = t
        .split("\n\n")
        .find(|b| b.starts_with("Notices ("))
        .unwrap();
    assert!(
        notices.contains("pull requests not read (deadline:"),
        "{notices}"
    );
    assert!(notices.contains("issues not read (deadline:"), "{notices}");
    assert!(notices.contains("--timeout of 1s"), "{notices}");
    assert!(
        t.contains("origin/main up to date") && t.contains("clean"),
        "local half intact: {t}"
    );
}

// ---------------------------------------------------------------------------------------------------------
// --stats and --verbose (Go: docs/status.md "Diagnostics", cli/status_online.go statusStatsLine/writeStatusVerbose)
// ---------------------------------------------------------------------------------------------------------

/// test/fixture: two open PRs, one issue; every answer carries the rate-limit headers (reset 2023-11-14 22:13:20 UTC,
/// 4900 minus the number of answers so far remaining).
fn rate_api() -> FakeRoutes {
    let served = std::sync::atomic::AtomicU64::new(0);
    FakeRoutes::start(move |path, _| {
        let h = |k: &str, v: &str| (k.to_string(), v.to_string());
        // the remaining count falls with every answer: the last one (the smallest) is the one reported
        let n = served.fetch_add(1, std::sync::atomic::Ordering::SeqCst) + 1;
        let headers = vec![
            h("X-RateLimit-Limit", "5000"),
            h("X-RateLimit-Remaining", &(4900 - n).to_string()),
            h("X-RateLimit-Reset", "1700000000"),
        ];
        let pr = |n: u64| {
            serde_json::json!({"number": n, "title": "t", "draft": false, "user": {"login": "me", "type": "User"},
                           "head": {"ref": "b", "sha": "abc"}, "state": "open", "merged_at": null})
        };
        let body = match path {
            "/repos/test/fixture/pulls" => serde_json::json!([pr(1), pr(2)]),
            "/repos/test/fixture/issues" => {
                serde_json::json!([{"number": 3, "title": "i", "labels": []}])
            }
            _ => serde_json::json!({"mergeable_state": "clean"}),
        };
        (200, headers, body.to_string())
    })
}

#[test]
fn without_the_flags_there_is_no_diagnostic_output() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = rate_api();
    let t = out(&stw(Some(&api.url), &["status", "--root", &r]));
    assert!(!t.contains("stats:") && !t.contains("Diagnostic"), "{t}");
    assert!(
        t.trim_end()
            .lines()
            .last()
            .unwrap()
            .contains("repositories"),
        "{t}"
    );
    let v = json_of(&stw(Some(&api.url), &["status", "--root", &r, "--json"]));
    assert!(v.get("stats").is_none(), "{v}");
}

#[test]
fn stats_text_is_one_line_at_the_very_end_with_the_requests_and_the_rate_limit() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = rate_api();
    let o = stw(Some(&api.url), &["status", "--root", &r, "--stats"]);
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let t = out(&o);
    let n = api.hits().len();
    assert_eq!(
        n,
        4,
        "list + 2 details of the PRs + issues: {:?}",
        api.hits()
    );
    let last = t.trim_end().lines().last().unwrap();
    let want = format!("stats: API: {n} reqs (4,896/5,000 left, reset 22:13 UTC) | ");
    assert!(last.starts_with(&want), "{last}");
    let rest = &last[want.len()..];
    assert!(
        rest.contains(" elapsed (local ") && rest.contains(", remote ") && rest.ends_with(')'),
        "{rest}"
    );
    assert!(!t.contains("Diagnostic breakdown"), "{t}");
    assert_eq!(t.matches("stats:").count(), 1);
    // the summary line is still the last line of the report itself
    let before = &t[..t.rfind("stats:").unwrap()];
    assert!(
        before
            .trim_end()
            .lines()
            .last()
            .unwrap()
            .contains("repositories"),
        "{t}"
    );
    assert!(!t.contains(TOKEN) && !err(&o).contains(TOKEN));
}

#[test]
fn stats_json_adds_the_stats_member_and_stays_one_document() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = rate_api();
    let o = stw(
        Some(&api.url),
        &["status", "--root", &r, "--json", "--stats"],
    );
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let v = json_of(&o);
    assert_eq!(v["schema_version"], 2);
    let s = &v["stats"];
    assert_eq!(s["requests"], 4, "{s}");
    assert_eq!(s["rate_limit_limit"], 5000);
    assert_eq!(s["rate_limit_remaining"], 4896);
    assert_eq!(s["rate_limit_reset"], "2023-11-14T22:13:20Z");
    assert!(s["duration_ms"].is_u64(), "{s}");
    assert!(
        err(&o).is_empty(),
        "--stats alone prints nothing on stderr: {}",
        err(&o)
    );
    // no token and no URL in the member
    assert!(
        !s.to_string().contains(TOKEN) && !s.to_string().contains("http"),
        "{s}"
    );
}

#[test]
fn stats_without_any_request_say_so_and_leave_the_rate_limit_null() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    // no token: no client, no request
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .args(["status", "--root", &r, "--stats", "--json"])
        .output()
        .unwrap();
    assert_eq!(o.status.code(), Some(0));
    let s = &json_of(&o)["stats"];
    assert_eq!(s["requests"], 0);
    assert!(
        s["rate_limit_limit"].is_null() && s["rate_limit_reset"].is_null(),
        "{s}"
    );
}

#[test]
fn verbose_text_lists_the_sources_per_repository_before_the_stats_line() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = rate_api();
    let o = stw(Some(&api.url), &["status", "--root", &r, "--verbose"]);
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let t = out(&o);
    let tail = &t[t.find("\nDiagnostic breakdown:\n").expect(&t)..];
    let lines: Vec<&str> = tail.lines().collect();
    assert_eq!(lines[1], "Diagnostic breakdown:");
    assert_eq!(lines[2], "  test/fixture:");
    assert!(
        lines[3].starts_with("    issues:     1 req (0 cached 304s, 1 consumed) in "),
        "{}",
        lines[3]
    );
    assert!(
        lines[4].starts_with("    pulls:      3 reqs (0 cached 304s, 3 consumed) in "),
        "{}",
        lines[4]
    );
    assert!(lines[5].starts_with("stats: API: 4 reqs"), "{}", lines[5]);
    assert_eq!(lines.len(), 6, "{tail}");
}

#[test]
fn verbose_with_json_keeps_stdout_a_document_and_writes_the_breakdown_to_stderr() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = rate_api();
    let o = stw(
        Some(&api.url),
        &["status", "--root", &r, "--json", "--verbose"],
    );
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let v = json_of(&o);
    assert_eq!(
        v["stats"]["requests"], 4,
        "--verbose includes --stats: {}",
        v["stats"]
    );
    assert!(!out(&o).contains("Diagnostic"));
    let e = err(&o);
    assert!(
        e.contains("Diagnostic breakdown:\n  test/fixture:\n    issues:"),
        "{e}"
    );
    assert!(
        e.trim_end()
            .lines()
            .last()
            .unwrap()
            .starts_with("stats: API: 4 reqs"),
        "{e}"
    );
    assert!(!e.contains(TOKEN));
}

#[test]
fn stats_and_verbose_are_usage_errors_with_offline() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    for flag in ["--stats", "--verbose"] {
        let o = stw(None, &["status", "--root", &r, "--offline", flag]);
        assert_eq!(o.status.code(), Some(2), "{flag}: {}", err(&o));
        assert!(
            out(&o).is_empty() && err(&o).contains(flag),
            "{flag}: {}",
            err(&o)
        );
    }
}

#[test]
fn verbose_shows_a_hostile_slug_escaped() {
    let fx = status_fixture();
    fx.set_origin_url("https://github.com/own/\u{1b}[31mRED\u{1b}[0m");
    let r = path_str(&fx.path);
    let api = FakeRoutes::start(|_, _| (403, vec![], "{}".into()));
    let o = stw(Some(&api.url), &["status", "--root", &r, "--verbose"]);
    let t = out(&o);
    assert!(!t.contains('\u{1b}'), "raw ESC: {t:?}");
    assert!(!err(&o).contains('\u{1b}'));
}

#[test]
fn stats_formatting_helpers_follow_the_go_ones() {
    use std::time::Duration as D;
    use stw::github::{RateInfo, Stats};
    use stw::status_stats::{group_digits, short_duration, stats_line, to_json, Timings};
    assert_eq!(short_duration(D::from_millis(850)), "850ms");
    assert_eq!(short_duration(D::from_millis(1400)), "1.4s");
    assert_eq!(short_duration(D::from_secs(38)), "38s");
    assert_eq!(short_duration(D::from_secs(125)), "2m05s");
    assert_eq!(group_digits(0), "0");
    assert_eq!(group_digits(999), "999");
    assert_eq!(group_digits(1000), "1,000");
    assert_eq!(group_digits(4892), "4,892");
    assert_eq!(group_digits(1_234_567), "1,234,567");
    let t = Timings {
        total: D::from_millis(1400),
        local: D::from_millis(110),
        remote: D::from_millis(1300),
    };
    let at = |reset| Stats {
        requests: 1,
        rate: Some(RateInfo {
            limit: 5000,
            remaining: 4999,
            reset,
        }),
        sources: vec![],
    };
    // epoch, a leap day, the end of a year, a date after 2038
    for (reset, iso) in [
        (0, "1970-01-01T00:00:00Z"),
        (951_782_400, "2000-02-29T00:00:00Z"),
        (1_704_067_199, "2023-12-31T23:59:59Z"),
        (2_208_988_800, "2040-01-01T00:00:00Z"),
    ] {
        assert_eq!(to_json(&at(reset), &t)["rate_limit_reset"], iso);
    }
    assert_eq!(
        stats_line(&at(1_700_000_000), &t),
        "stats: API: 1 req (4,999/5,000 left, reset 22:13 UTC) | 1.4s elapsed (local 110ms, remote 1.3s)"
    );
    let none = Stats::default();
    assert_eq!(
        stats_line(&none, &t),
        "stats: API: 0 reqs | 1.4s elapsed (local 110ms, remote 1.3s)"
    );
}

// ---------------------------------------------------------------------------------------------------------
// --long / -l (Go: status_command_test.go --long cases, status_online_test.go long goldens)
// ---------------------------------------------------------------------------------------------------------

/// Root on main, behind origin/main by one, with a staged, an unstaged and an untracked path and a merge in
/// progress; a detached submodule `det`; a submodule `broken` that cannot be read.
fn long_fixture() -> Fx {
    let fx = status_fixture();
    let c0 = fx.tip("main");
    let c1 = fx.commit(Some(c0), &[("a.txt", Some("a\n"))], "c1");
    fx.remote("main", c1);
    fx.write("staged.txt", "s\n");
    fx.stage("staged.txt");
    fx.write("a.txt", "changed\n");
    fx.write("untracked.txt", "u\n");
    fx.write(".git/MERGE_HEAD", &format!("{c0}\n"));
    fx.write(
        ".gitmodules",
        "[submodule \"det\"]\n\tpath = det\n\turl = x\n[submodule \"broken\"]\n\tpath = broken\n\turl = x\n",
    );
    fx.write("broken/.git", "gitdir: /nonexistent/stw/gitdir\n");
    let det = std::path::Path::new(&fx.path).join("det");
    std::fs::create_dir_all(&det).unwrap();
    let sub = gix::init(&det).unwrap();
    let sig = gix::actor::SignatureRef {
        name: "T".into(),
        email: "t@example.com".into(),
        time: "1700000000 +0000",
    };
    let id = sub
        .commit_as(
            sig,
            sig,
            "refs/heads/tmp",
            "m",
            sub.empty_tree().id,
            gix::commit::NO_PARENT_IDS,
        )
        .unwrap();
    // HEAD on the commit itself: detached
    fx.write("det/.git/HEAD", &format!("{id}\n"));
    fx
}

#[test]
fn long_adds_cards_and_explanations_and_nothing_is_taken_away() {
    let fx = long_fixture();
    let r = path_str(&fx.path);
    let plain = stw(None, &["status", "--root", &r, "--offline"]);
    let long = stw(None, &["status", "--root", &r, "--offline", "--long"]);
    let short = stw(None, &["status", "--root", &r, "--offline", "-l"]);
    assert_eq!(plain.status.code(), Some(1), "{}", out(&plain)); // the broken submodule
    assert_eq!(out(&long), out(&short), "-l is --long");
    assert_eq!(long.status.code(), Some(1));
    let (p, l) = (out(&plain), out(&long));
    assert!(l.len() > p.len());
    // taking the added lines away leaves the default text
    let added = |line: &&str| {
        [
            "path: ",
            "remote: ",
            "head: ",
            "upstream: ",
            "changes: ",
            "branches: ",
            "gitlink: ",
            "- ",
        ]
        .iter()
        .any(|k| line.strip_prefix("    ").is_some_and(|r| r.starts_with(k)))
    };
    assert_eq!(
        l.lines()
            .filter(|x| !added(x))
            .collect::<Vec<_>>()
            .join("\n"),
        p.lines().collect::<Vec<_>>().join("\n")
    );
    // the blocks stay in order and the summary stays last
    let at = |t: &str, h: &str| t.find(h).unwrap_or_else(|| panic!("{h} in {t}"));
    assert!(
        at(&l, "Counts\n") < at(&l, "Needs action (")
            && at(&l, "Needs action (") < at(&l, "Repositories (")
            && at(&l, "Repositories (") < at(&l, "Notices (")
    );
    assert!(
        l.trim_end()
            .lines()
            .last()
            .unwrap()
            .contains("need attention"),
        "{l}"
    );
    // the explanations, under "Needs action", for every reason of the root
    let needs = l
        .split("\n\n")
        .find(|b| b.starts_with("Needs action ("))
        .unwrap();
    for want in [
        "- operation:merge: a merge is in progress: finish or abort it",
        "- staged: 1 path staged but not committed",
        "- unstaged: 1 path with tracked changes in the working tree that are not staged",
        "- untracked: ",
        "- behind: origin/main is 1 commit ahead of this branch",
        "- detached: HEAD is detached at ",
        "- error: the repository could not be read: ",
    ] {
        assert!(needs.contains(want), "{want:?} in\n{needs}");
    }
    // the card of the root
    for want in [
        "    remote: test/fixture\n",
        "    upstream: origin/main (ahead 0, behind 1)\n",
        "    changes: 1 staged, 1 unstaged, 3 untracked, 0 conflicts\n",
        "    branches: 1 local, 1 remote; linked worktrees: 0; stash: 0\n",
    ] {
        assert!(l.contains(want), "{want:?} in\n{l}");
    }
    assert!(l.contains("    head: ") && l.contains("    path: "), "{l}");
    // stw prints no command for someone else to run
    assert!(!l.contains("run:") && !l.contains("next:"), "{l}");
}

#[test]
fn long_does_not_change_the_json() {
    let fx = long_fixture();
    let r = path_str(&fx.path);
    let a = stw(None, &["status", "--root", &r, "--offline", "--json"]);
    for flag in ["--long", "-l"] {
        let b = stw(None, &["status", "--root", &r, "--offline", "--json", flag]);
        assert_eq!(out(&a), out(&b), "{flag}");
    }
}

#[test]
fn long_explains_every_reason_from_the_facts() {
    use stw::status::{collect_local, explain, reasons, StatusOpts};
    let fx = long_fixture();
    let root = std::fs::canonicalize(&fx.path).unwrap();
    let rr = stw::repo::RepoRef {
        name: "repo".into(),
        path: root.clone(),
    };
    let mut s = collect_local(&rr, &root, &StatusOpts::default(), None);
    s.changes.conflicts = 2;
    s.changes.staged = 3;
    s.changes.unstaged = 1;
    s.changes.untracked = 4;
    s.facts.upstream.short = None;
    s.facts.upstream.gone = false;
    s.facts.behind = None;
    assert_eq!(
        explain(&s, "conflicts"),
        "2 paths with merge conflicts: resolve them and stage the result"
    );
    assert_eq!(
        explain(&s, "staged"),
        "3 paths staged but not committed: commit them or unstage them"
    );
    assert!(explain(&s, "unstaged").starts_with("1 path with tracked changes"));
    assert_eq!(
        explain(&s, "untracked"),
        "4 paths untracked: add them, ignore them or remove them"
    );
    assert!(explain(&s, "no-upstream").starts_with("the branch main has no upstream"));
    s.error = Some("boom \u{1b}[31m".into());
    assert_eq!(
        explain(&s, "error"),
        "the repository could not be read: boom \\u{1b}[31m"
    );
    // every reason that `reasons` can give has its own text, none falls through to the generic one
    let all = reasons(&s);
    assert!(all.contains(&"error".to_string()));
    s.error = None;
    s.op = Some("rebase");
    assert!(explain(&s, "operation:rebase").starts_with("a rebase is in progress"));
}

#[test]
fn long_shows_text_from_the_repository_escaped() {
    let fx = long_fixture();
    fx.set_origin_url("https://github.com/own/\u{1b}[31mRED\u{1b}[0m");
    let r = path_str(&fx.path);
    let t = out(&stw(None, &["status", "--root", &r, "--offline", "--long"]));
    assert!(!t.contains('\u{1b}'), "raw ESC: {t:?}");
    assert!(t.contains("    remote: own/\\u{1b}[31mRED"), "{t}");
}
