//! `stw status` flags ported from Go (`internal/cli/status.go`): `--icons`, `--worktrees`, `--fetch`, `--timeout`,
//! `--stats`, `--verbose`, `--long`. Go cases: `internal/cli/status_command_test.go`, `internal/statuscmd/terminal_test.go`.
use std::process::{Command, Output};

use crate::common::*;
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
