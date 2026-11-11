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
    assert_eq!(list.len(), 6, "{list:?}: no main checkout");
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
    assert_eq!(v["repos"][0]["counts"]["worktrees"], 4);
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
    assert_eq!(lines.len(), 6, "{t}");
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
