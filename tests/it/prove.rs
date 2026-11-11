//! `stw prove ALT NEU [--base BASIS]`: the proof that a rewritten branch is the old one rebased. Repositories are
//! built with gix in a scratch directory (`tests/common`); the binary runs with a scrubbed environment. The
//! range-diff step needs the installed git (cases skip without it, as `carryover_cli.rs`).
use std::path::Path;
use std::process::{Command, Output};

use crate::common::*;
use gix::ObjectId;

fn git_available() -> bool {
    Command::new("git")
        .arg("--version")
        .output()
        .is_ok_and(|o| o.status.success())
}

fn stw(cwd: &Path, home: &Path, args: &[&str]) -> Output {
    assert!(cwd.is_absolute() && home.is_absolute(), "unconfined");
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", home)
        .current_dir(cwd)
        .arg("prove")
        .args(args)
        .output()
        .unwrap()
}

struct Run {
    code: i32,
    text: String,
    err: String,
}

impl Run {
    fn first(&self) -> &str {
        self.text.lines().next().unwrap_or("")
    }
    fn last(&self) -> &str {
        self.text.trim_end().lines().last().unwrap_or("")
    }
    fn check_line(&self, id: &str) -> &str {
        self.text
            .lines()
            .find(|l| {
                let t = l.trim_start_matches(|c: char| c.is_ascii_digit() || c == ' ');
                t.starts_with(id)
            })
            .unwrap_or_else(|| panic!("no check {id}:\n{}", self.text))
    }
}

fn run(fx: &Fx, args: &[&str]) -> Run {
    let home = tempfile::tempdir().unwrap();
    let o = stw(&fx.path, home.path(), args);
    Run {
        code: o.status.code().unwrap_or(-1),
        text: String::from_utf8_lossy(&o.stdout).into_owned(),
        err: String::from_utf8_lossy(&o.stderr).into_owned(),
    }
}

fn s(i: ObjectId) -> String {
    i.to_string()
}

const TRAILER: &str = "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>";

fn msg(subject: &str) -> String {
    format!("{subject}\n\n{TRAILER}\n")
}

/// A stack of two commits on `m1`, and the same two commits rebased onto `m2` (main moved on another file).
struct Pair {
    fx: Fx,
    m1: ObjectId,
    m2: ObjectId,
    old: [ObjectId; 2],
    new: [ObjectId; 2],
}

fn pair() -> Pair {
    let fx = Fx::new();
    let m1 = fx.commit(
        None,
        &[
            ("a.txt", Some("a\n")),
            ("b.txt", Some("b\n")),
            ("m.txt", Some("p\n")),
        ],
        "m1",
    );
    let c1 = fx.commit(Some(m1), &[("a.txt", Some("a\n1\n"))], &msg("feat: one"));
    let c2 = fx.commit(Some(c1), &[("b.txt", Some("b\n2\n"))], &msg("feat: two"));
    let m2 = fx.commit(Some(m1), &[("m.txt", Some("p\nq\n"))], "m2");
    let n1 = fx.commit(Some(m2), &[("a.txt", Some("a\n1\n"))], &msg("feat: one"));
    let n2 = fx.commit(Some(n1), &[("b.txt", Some("b\n2\n"))], &msg("feat: two"));
    Pair {
        fx,
        m1,
        m2,
        old: [c1, c2],
        new: [n1, n2],
    }
}

fn skip() -> bool {
    if git_available() {
        return false;
    }
    eprintln!("git not installed: not run");
    true
}

#[test]
fn a_pure_rebase_is_proved_and_ends_with_the_line() {
    if skip() {
        return;
    }
    let p = pair();
    let (alt, neu, base) = (s(p.old[1]), s(p.new[1]), s(p.m2));
    let r = run(&p.fx, &[&alt, &neu, "--base", &base]);
    assert_eq!(r.code, 0, "{}\n{}", r.text, r.err);
    assert_eq!(r.first(), "proved", "{}", r.text);
    for id in [
        "range-diff",
        "commit-patches",
        "patch-id",
        "merge-commits",
        "files",
        "identity",
        "trailers",
        "session-links",
    ] {
        assert!(r.check_line(id).contains(" ok "), "{id}\n{}", r.text);
    }
    let last = r.last();
    assert!(
        last.starts_with(&format!("Carried-over: from={alt} to={neu} patch-id=")),
        "{}",
        r.text
    );
    assert!(last.ends_with(" pr=none"), "{last}");
    // the line is the carry-over grammar
    assert!(
        stw::review::parse_strict(last).is_some(),
        "does not parse: {last}"
    );
    // the same line as the carry-over command gives for the same four ids
    let home = tempfile::tempdir().unwrap();
    let c = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", home.path())
        .current_dir(&p.fx.path)
        .args([
            "review",
            "carry-over",
            &s(p.m1),
            &alt,
            &base,
            &neu,
            "--pr",
            "none",
        ])
        .output()
        .unwrap();
    let c = String::from_utf8_lossy(&c.stdout).into_owned();
    assert_eq!(c.trim_end().lines().last().unwrap(), last, "{c}");
    // --pr N
    let r = run(&p.fx, &[&alt, &neu, "--base", &base, "--pr", "7"]);
    assert!(r.last().ends_with(" pr=7"), "{}", r.text);
}

#[test]
fn json_has_the_keys_and_the_line() {
    if skip() {
        return;
    }
    let p = pair();
    let (alt, neu, base) = (s(p.old[1]), s(p.new[1]), s(p.m2));
    let r = run(&p.fx, &[&alt, &neu, "--base", &base, "--json"]);
    assert_eq!(r.code, 0, "{}", r.text);
    let v: serde_json::Value = serde_json::from_str(&r.text).unwrap();
    assert_eq!(v["schema_version"], 2);
    assert_eq!(v["command"], "prove");
    assert_eq!(v["verdict"], "proved");
    assert_eq!(v["proved"], true);
    assert_eq!(v["pr"], "none");
    assert_eq!(v["ends"]["old_head"], alt);
    assert_eq!(v["ends"]["new_head"], neu);
    assert_eq!(v["ends"]["old_base"], s(p.m1));
    assert_eq!(v["ends"]["new_base"], base);
    assert_eq!(v["ends"]["base_source"], "flag");
    assert_eq!(v["ends"]["old_base_source"], "merge-base");
    let ids: Vec<&str> = v["checks"]
        .as_array()
        .unwrap()
        .iter()
        .map(|c| c["id"].as_str().unwrap())
        .collect();
    assert_eq!(
        ids,
        [
            "range-diff",
            "commit-patches",
            "patch-id",
            "merge-commits",
            "files",
            "identity",
            "trailers",
            "session-links"
        ]
    );
    assert_eq!(v["checks"][0]["via"], "git");
    assert_eq!(v["checks"][1]["via"], "gix");
    assert_eq!(v["checks"][1]["id"], "commit-patches");
    assert!(v["line"]
        .as_str()
        .unwrap()
        .starts_with("Carried-over: from="));
    assert_eq!(v["reasons"].as_array().unwrap().len(), 0);
}

#[test]
fn a_rewrite_on_the_same_base_needs_no_base_flag() {
    if skip() {
        return;
    }
    // same trees and texts, other commit ids (other dates): range-diff shows "="
    let fx = Fx::new();
    let m1 = fx.commit(None, &[("a.txt", Some("a\n"))], "m1");
    let c1 = fx.commit(Some(m1), &[("a.txt", Some("a\n1\n"))], &msg("feat: one"));
    let n1 = fx.commit(Some(m1), &[("a.txt", Some("a\n1\n"))], &msg("feat: one"));
    assert_ne!(c1, n1);
    let r = run(&fx, &[&s(c1), &s(n1)]);
    assert_eq!(r.code, 0, "{}", r.text);
    assert_eq!(r.first(), "proved");
    assert!(r.text.contains("base from merge-base"), "{}", r.text);
}

#[test]
fn branch_names_work_as_revisions() {
    if skip() {
        return;
    }
    let p = pair();
    p.fx.branch("old", p.old[1]);
    p.fx.branch("new", p.new[1]);
    p.fx.branch("main", p.m2);
    let r = run(&p.fx, &["old", "new", "--base", "main"]);
    assert_eq!(r.code, 0, "{}", r.text);
    assert!(r
        .last()
        .starts_with(&format!("Carried-over: from={} to={}", p.old[1], p.new[1])));
}

#[test]
fn reordered_commits_are_not_proved_although_the_patch_id_is_equal() {
    if skip() {
        return;
    }
    let p = pair();
    // the two commits touch different files: the other order gives the same cumulative diff
    let r1 =
        p.fx.commit(Some(p.m2), &[("b.txt", Some("b\n2\n"))], &msg("feat: two"));
    let r2 =
        p.fx.commit(Some(r1), &[("a.txt", Some("a\n1\n"))], &msg("feat: one"));
    let r = run(&p.fx, &[&s(p.old[1]), &s(r2), "--base", &s(p.m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert_eq!(r.first(), "not proved");
    assert!(r.check_line("patch-id").contains(" ok "), "{}", r.text);
    assert!(r.check_line("range-diff").contains("failed"), "{}", r.text);
    assert!(r.text.contains("range-diff-not-equal"));
    assert!(!r.text.contains("Carried-over:"), "{}", r.text);
}

#[test]
fn a_whitespace_change_keeps_the_patch_id_and_fails_the_range_diff() {
    if skip() {
        return;
    }
    let p = pair();
    let n1 =
        p.fx.commit(Some(p.m2), &[("a.txt", Some("a\n1 \n"))], &msg("feat: one"));
    let n2 =
        p.fx.commit(Some(n1), &[("b.txt", Some("b\n2\n"))], &msg("feat: two"));
    let r = run(&p.fx, &[&s(p.old[1]), &s(n2), "--base", &s(p.m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.check_line("patch-id").contains(" ok "), "{}", r.text);
    assert!(r.check_line("range-diff").contains("failed"), "{}", r.text);
    assert!(!r.text.contains("Carried-over:"));
}

#[test]
fn a_changed_commit_text_is_a_range_diff_bang() {
    if skip() {
        return;
    }
    let p = pair();
    let n1 = p.fx.commit(
        Some(p.m2),
        &[("a.txt", Some("a\n1\n"))],
        &msg("feat: one, reworded"),
    );
    let n2 =
        p.fx.commit(Some(n1), &[("b.txt", Some("b\n2\n"))], &msg("feat: two"));
    let r = run(&p.fx, &[&s(p.old[1]), &s(n2), "--base", &s(p.m2), "--json"]);
    assert_eq!(r.code, 1, "{}", r.text);
    let v: serde_json::Value = serde_json::from_str(&r.text).unwrap();
    assert_eq!(v["verdict"], "not proved");
    let rd = &v["checks"][0];
    assert_eq!(rd["id"], "range-diff");
    assert_eq!(rd["state"], "failed");
    assert!(rd["items"][0].as_str().unwrap().contains(" ! "), "{rd}");
    assert_eq!(v["checks"][2]["state"], "ok", "patch-id still equal");
    assert!(v["line"].is_null());
}

#[test]
fn an_additional_commit_is_not_proved() {
    if skip() {
        return;
    }
    let p = pair();
    let extra = p.fx.commit(
        Some(p.new[1]),
        &[("c.txt", Some("c\n"))],
        &msg("feat: three"),
    );
    let r = run(&p.fx, &[&s(p.old[1]), &s(extra), "--base", &s(p.m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert_eq!(r.first(), "not proved");
    assert!(r.check_line("range-diff").contains("failed"));
    assert!(r.check_line("patch-id").contains("failed"));
    assert!(r.check_line("files").contains("failed"), "{}", r.text);
    assert!(r.text.contains("> c.txt"), "{}", r.text);
}

#[test]
fn a_merge_commit_in_the_new_range_is_not_proved() {
    if skip() {
        return;
    }
    let p = pair();
    let side =
        p.fx.commit(Some(p.m2), &[("s.txt", Some("s\n"))], &msg("feat: side"));
    let merge = p.fx.commit_with_parents(
        vec![p.new[1], side],
        Some(p.new[1]),
        &[("s.txt", Some("s\n"))],
        &msg("merge"),
    );
    let r = run(&p.fx, &[&s(p.old[1]), &s(merge), "--base", &s(p.m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(
        r.check_line("merge-commits").contains("failed"),
        "{}",
        r.text
    );
    assert!(r.text.contains("merge-commits"));
}

#[test]
fn a_changed_conflict_resolution_changes_the_patch_id() {
    if skip() {
        return;
    }
    let fx = Fx::new();
    let m1 = fx.commit(None, &[("a.txt", Some("1\n2\n3\n"))], "m1");
    let c1 = fx.commit(Some(m1), &[("a.txt", Some("1\nX\n3\n"))], &msg("feat: x"));
    let m2 = fx.commit(Some(m1), &[("a.txt", Some("1\nY\n3\n"))], "m2");
    // resolved the conflict in favor of Z, not X
    let n1 = fx.commit(Some(m2), &[("a.txt", Some("1\nZ\n3\n"))], &msg("feat: x"));
    let r = run(&fx, &[&s(c1), &s(n1), "--base", &s(m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.check_line("patch-id").contains("failed"), "{}", r.text);
    assert!(r.text.contains("patch-id-differs"));
    assert!(
        r.check_line("files").contains(" ok "),
        "same file set: {}",
        r.text
    );
}

#[test]
fn a_foreign_identity_is_not_proved() {
    if skip() {
        return;
    }
    let p = pair();
    let n1 = p.fx.commit_by(
        Some(p.m2),
        &[("a.txt", Some("a\n1\n"))],
        &msg("feat: one"),
        ("Other\u{1b}[31mPerson", "other@example.com"),
        ("T", "t@example.com"),
    );
    let n2 =
        p.fx.commit(Some(n1), &[("b.txt", Some("b\n2\n"))], &msg("feat: two"));
    let r = run(&p.fx, &[&s(p.old[1]), &s(n2), "--base", &s(p.m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.check_line("identity").contains("failed"), "{}", r.text);
    assert!(r.text.contains("identity-mismatch"));
    // the name is escaped, never a raw control byte
    assert!(r.text.contains("Other\\u{1b}[31mPerson"), "{}", r.text);
    assert!(!r.text.contains('\u{1b}'), "{:?}", r.text);
    // range-diff names a changed author itself
    assert!(r.check_line("range-diff").contains("failed"), "{}", r.text);
}

#[test]
fn a_foreign_committer_is_not_proved() {
    if skip() {
        return;
    }
    let p = pair();
    let n1 = p.fx.commit_by(
        Some(p.m2),
        &[("a.txt", Some("a\n1\n"))],
        &msg("feat: one"),
        ("T", "t@example.com"),
        ("Other", "other@example.com"),
    );
    let n2 =
        p.fx.commit(Some(n1), &[("b.txt", Some("b\n2\n"))], &msg("feat: two"));
    let r = run(&p.fx, &[&s(p.old[1]), &s(n2), "--base", &s(p.m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(
        r.text.contains("committer Other <other@example.com>"),
        "{}",
        r.text
    );
    // range-diff does not see the committer: only the identity check catches it
    assert!(r.check_line("range-diff").contains(" ok "), "{}", r.text);
}

#[test]
fn no_configured_identity_is_unknown_never_a_pass() {
    if skip() {
        return;
    }
    let p = pair();
    p.fx.strip_identity();
    let r = run(&p.fx, &[&s(p.old[1]), &s(p.new[1]), "--base", &s(p.m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.check_line("identity").contains("unknown"), "{}", r.text);
    assert!(r.text.contains("identity-unknown"));
    assert!(!r.text.contains("Carried-over:"));
}

#[test]
fn a_missing_co_authored_by_trailer_is_not_proved() {
    if skip() {
        return;
    }
    let p = pair();
    let n1 =
        p.fx.commit(Some(p.m2), &[("a.txt", Some("a\n1\n"))], "feat: one\n");
    let n2 =
        p.fx.commit(Some(n1), &[("b.txt", Some("b\n2\n"))], &msg("feat: two"));
    let r = run(&p.fx, &[&s(p.old[1]), &s(n2), "--base", &s(p.m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.check_line("trailers").contains("failed"), "{}", r.text);
    assert!(r.text.contains("trailer-missing"));
    assert!(
        r.text
            .contains(&format!("{n1} lost: Co-Authored-By: Claude")),
        "{}",
        r.text
    );
}

#[test]
fn a_session_link_in_a_new_commit_text_is_not_proved() {
    if skip() {
        return;
    }
    let p = pair();
    let evil = format!(
        "feat: one \u{1b}[31mred\n\n{TRAILER}\n{}abc\n",
        concat!("Claude", "-Session: ")
    );
    let n1 = p.fx.commit(Some(p.m2), &[("a.txt", Some("a\n1\n"))], &evil);
    let n2 =
        p.fx.commit(Some(n1), &[("b.txt", Some("b\n2\n"))], &msg("feat: two"));
    let r = run(&p.fx, &[&s(p.old[1]), &s(n2), "--base", &s(p.m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(
        r.check_line("session-links").contains("failed"),
        "{}",
        r.text
    );
    assert!(r.text.contains("session-link"));
    // the subject of the commit is escaped: no raw escape byte reaches the output
    assert!(!r.text.contains('\u{1b}'), "{:?}", r.text);
    assert!(r.text.contains("\\u{1b}"), "{}", r.text);
}

#[test]
fn an_unknown_alt_is_not_proved_with_the_reason() {
    let p = pair();
    let r = run(&p.fx, &["no-such-branch", &s(p.new[1])]);
    assert_eq!(r.code, 1, "{}\n{}", r.text, r.err);
    assert_eq!(r.first(), "not proved");
    assert!(r.text.contains("unknown-revision"), "{}", r.text);
    assert!(r.text.contains("ALT no-such-branch"), "{}", r.text);
    assert!(!r.text.contains("Carried-over:"));
}

#[test]
fn neu_equal_to_alt_proves_nothing() {
    let p = pair();
    let h = s(p.new[1]);
    let r = run(&p.fx, &[&h, &h]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.text.contains("same-commit"), "{}", r.text);
    assert!(!r.text.contains("Carried-over:"));
}

#[test]
fn a_rebase_without_base_says_so_and_a_base_that_is_no_ancestor_is_refused() {
    if skip() {
        return;
    }
    let p = pair();
    let (alt, neu) = (s(p.old[1]), s(p.new[1]));
    let r = run(&p.fx, &[&alt, &neu]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(
        r.text.contains("hint: if NEU was rebased onto a new base"),
        "{}",
        r.text
    );
    // BASIS is the old tip, which is not below NEU
    let r = run(&p.fx, &[&alt, &neu, "--base", &s(p.old[0])]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.text.contains("base-not-below-new"), "{}", r.text);
}

#[test]
fn unrelated_histories_have_no_merge_base() {
    let p = pair();
    let other = p.fx.commit(None, &[("z.txt", Some("z\n"))], "root");
    let r = run(&p.fx, &[&s(other), &s(p.new[1])]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.text.contains("no-merge-base"), "{}", r.text);
}

#[test]
fn usage_errors_exit_two_before_any_read() {
    let p = pair();
    let h = s(p.new[1]);
    for args in [
        vec![],
        vec![h.as_str()],
        vec![h.as_str(), h.as_str(), h.as_str()],
        vec!["-x", h.as_str()],
        vec![h.as_str(), "a..b"],
        vec![h.as_str(), "x\ny"],
        vec![h.as_str(), h.as_str(), "--pr", "0"],
        vec![h.as_str(), h.as_str(), "--pr", "x"],
        vec![h.as_str(), h.as_str(), "--base", "-x"],
        vec![h.as_str(), h.as_str(), "--old-parent", "-x"],
        vec![h.as_str(), h.as_str(), "--old-parent", "a..b"],
        vec![h.as_str(), h.as_str(), "--repo", "no-such-repo"],
    ] {
        let r = run(&p.fx, &args);
        assert_eq!(r.code, 2, "{args:?}: {}{}", r.text, r.err);
        assert!(r.text.is_empty(), "{args:?}");
    }
}

#[test]
fn the_repository_comes_from_the_workspace_only() {
    if skip() {
        return;
    }
    let p = pair();
    let (alt, neu, base) = (s(p.old[1]), s(p.new[1]), s(p.m2));
    let home = tempfile::tempdir().unwrap();
    // no repository around the current directory: the workspace of one repository is used
    let args = [
        alt.as_str(),
        neu.as_str(),
        "--base",
        base.as_str(),
        "--root",
        p.fx.path.to_str().unwrap(),
    ];
    let o = stw(home.path(), home.path(), &args);
    assert_eq!(
        o.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&o.stdout)
    );
    // the repository of the current directory is not part of the workspace named by --root
    let other = Fx::new();
    let o = stw(&other.path, home.path(), &args);
    assert_eq!(o.status.code(), Some(2));
    let err = String::from_utf8_lossy(&o.stderr).into_owned();
    assert!(err.contains("not part of the workspace"), "{err}");
    // --repo names the workspace root by its directory name
    let name = p.fx.path.file_name().unwrap().to_str().unwrap();
    let o = stw(
        &other.path,
        home.path(),
        &[
            &alt,
            &neu,
            "--base",
            &base,
            "--root",
            p.fx.path.to_str().unwrap(),
            "--repo",
            name,
        ],
    );
    assert_eq!(
        o.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&o.stderr)
    );
}

#[test]
fn a_proof_writes_nothing_to_the_repository() {
    if skip() {
        return;
    }
    let p = pair();
    p.fx.branch("old", p.old[1]);
    p.fx.branch("new", p.new[1]);
    p.fx.set_head("new");
    p.fx.checkout_head();
    let before = tree_snapshot(&p.fx.path);
    let (alt, neu, base) = (s(p.old[1]), s(p.new[1]), s(p.m2));
    let r = run(&p.fx, &[&alt, &neu, "--base", &base]);
    assert_eq!(r.code, 0, "{}", r.text);
    let r = run(&p.fx, &["old", "new", "--base", &base, "--json"]);
    assert_eq!(r.code, 0, "{}", r.text);
    let r = run(&p.fx, &[&alt, &s(p.m2)]);
    assert_eq!(r.code, 1);
    assert!(
        before == tree_snapshot(&p.fx.path),
        "the repository changed"
    );
}

/// range-diff shows only "Binary files differ" for a binary change: two commits that change a binary file to
/// other contents in between look equal there. Cumulative patch-id and path set are equal as well.
fn binary_intermediate() -> (Fx, [ObjectId; 4]) {
    let fx = Fx::new();
    let m1 = fx.commit(
        None,
        &[
            ("img.bin", Some("\0X")),
            ("a.txt", Some("a\n")),
            ("b.txt", Some("b\n")),
        ],
        "m1",
    );
    let c1 = fx.commit(
        Some(m1),
        &[("img.bin", Some("\0Y")), ("a.txt", Some("a2\n"))],
        &msg("feat: one"),
    );
    let c2 = fx.commit(
        Some(c1),
        &[("img.bin", Some("\0Z")), ("b.txt", Some("b2\n"))],
        &msg("feat: two"),
    );
    let m2 = fx.commit(Some(m1), &[("m.txt", Some("m\n"))], "m2");
    let n1 = fx.commit(
        Some(m2),
        &[("img.bin", Some("\0W")), ("a.txt", Some("a2\n"))],
        &msg("feat: one"),
    );
    let n2 = fx.commit(
        Some(n1),
        &[("img.bin", Some("\0Z")), ("b.txt", Some("b2\n"))],
        &msg("feat: two"),
    );
    (fx, [c2, m2, n2, m1])
}

#[test]
fn a_binary_file_with_other_contents_in_an_intermediate_commit_is_not_proved() {
    if skip() {
        return;
    }
    let (fx, [alt, m2, neu, _]) = binary_intermediate();
    let r = run(&fx, &[&s(alt), &s(neu), "--base", &s(m2)]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert_eq!(r.first(), "not proved");
    assert!(r.check_line("range-diff").contains(" ok "), "{}", r.text);
    assert!(r.check_line("patch-id").contains(" ok "), "{}", r.text);
    assert!(r.check_line("files").contains(" ok "), "{}", r.text);
    assert!(
        r.check_line("commit-patches").contains("failed"),
        "{}",
        r.text
    );
    assert!(r.text.contains("commit-patch-differs"), "{}", r.text);
    assert!(r.text.contains("position 1"), "{}", r.text);
    assert!(!r.text.contains("Carried-over:"));
}

#[test]
fn a_merge_in_the_old_range_is_left_out_of_the_pairing_and_named() {
    if skip() {
        return;
    }
    let fx = Fx::new();
    let m1 = fx.commit(
        None,
        &[("a.txt", Some("a\n")), ("s.txt", Some("s\n"))],
        "m1",
    );
    let c1 = fx.commit(Some(m1), &[("a.txt", Some("a1\n"))], &msg("feat: one"));
    let side = fx.commit(Some(m1), &[("s.txt", Some("s1\n"))], &msg("feat: side"));
    let merge = fx.commit_with_parents(
        vec![c1, side],
        Some(c1),
        &[("s.txt", Some("s1\n"))],
        &msg("merge side"),
    );
    let m2 = fx.commit(Some(m1), &[("m.txt", Some("m\n"))], "m2");
    let n1 = fx.commit(Some(m2), &[("a.txt", Some("a1\n"))], &msg("feat: one"));
    let n2 = fx.commit(Some(n1), &[("s.txt", Some("s1\n"))], &msg("feat: side"));
    let r = run(&fx, &[&s(merge), &s(n2), "--base", &s(m2)]);
    assert_eq!(r.code, 0, "{}", r.text);
    assert_eq!(r.first(), "proved");
    assert!(
        r.check_line("trailers")
            .contains("the old range had 1 merge commits"),
        "{}",
        r.text
    );
}

/// Child branch whose parent was rebased: `git rebase --onto P' P C`.
struct Child {
    fx: Fx,
    m1: ObjectId,
    p: ObjectId,
    c: ObjectId,
    p2: ObjectId,
    c2: ObjectId,
}

fn child() -> Child {
    let fx = Fx::new();
    let m1 = fx.commit(
        None,
        &[("a.txt", Some("a\n")), ("img.bin", Some("\0X"))],
        "m1",
    );
    let p = fx.commit(Some(m1), &[("p.txt", Some("p\n"))], &msg("feat: parent"));
    let c = fx.commit(Some(p), &[("c.txt", Some("c\n"))], &msg("feat: child"));
    let m2 = fx.commit(Some(m1), &[("a.txt", Some("a\nm\n"))], "m2");
    let p2 = fx.commit(Some(m2), &[("p.txt", Some("p\n"))], &msg("feat: parent"));
    let c2 = fx.commit(Some(p2), &[("c.txt", Some("c\n"))], &msg("feat: child"));
    Child {
        fx,
        m1,
        p,
        c,
        p2,
        c2,
    }
}

#[test]
fn a_child_branch_restack_is_proved_with_the_old_parent() {
    if skip() {
        return;
    }
    let k = child();
    let (alt, neu, par, base) = (s(k.c), s(k.c2), s(k.p), s(k.p2));
    let r = run(
        &k.fx,
        &[&alt, &neu, "--old-parent", &par, "--base", &base, "--json"],
    );
    assert_eq!(r.code, 0, "{}", r.text);
    let v = serde_json::from_str::<serde_json::Value>(&r.text).unwrap();
    assert_eq!(v["verdict"], "proved");
    assert_eq!(v["ends"]["old_base"], par);
    assert_eq!(v["ends"]["old_base_source"], "flag");
    assert_eq!(v["request"]["old_parent"], par);
    assert!(v["line"]
        .as_str()
        .unwrap()
        .starts_with(&format!("Carried-over: from={alt} to={neu} ")));
    // without the option the old range holds the parent's commit too: not proved, with a hint for the option
    let r = run(&k.fx, &[&alt, &neu, "--base", &base]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert_eq!(r.first(), "not proved");
    assert!(r.text.contains("--old-parent OLDPARENT"), "{}", r.text);
    assert!(r.text.contains("(base from merge-base)"), "{}", r.text);
}

#[test]
fn an_old_parent_that_is_no_ancestor_the_alt_itself_or_too_far_down_is_not_proved() {
    if skip() {
        return;
    }
    let k = child();
    let (alt, neu, base) = (s(k.c), s(k.c2), s(k.p2));
    // not an ancestor of ALT
    let r = run(&k.fx, &[&alt, &neu, "--old-parent", &base, "--base", &base]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.text.contains("old-parent-not-ancestor"), "{}", r.text);
    assert!(!r.text.contains("Carried-over:"));
    // ALT itself: empty old range
    let r = run(&k.fx, &[&alt, &neu, "--old-parent", &alt, "--base", &base]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.text.contains("old-range-empty"), "{}", r.text);
    // one parent too far down: the old range has an extra commit
    let r = run(
        &k.fx,
        &[&alt, &neu, "--old-parent", &s(k.m1), "--base", &base],
    );
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.check_line("range-diff").contains("failed"), "{}", r.text);
    assert!(!r.text.contains("Carried-over:"));
    // unknown revision
    let r = run(
        &k.fx,
        &[&alt, &neu, "--old-parent", "no-such-rev", "--base", &base],
    );
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.text.contains("unknown-revision"), "{}", r.text);
}

#[test]
fn a_binary_intermediate_difference_is_not_proved_with_the_old_parent_either() {
    if skip() {
        return;
    }
    let fx = Fx::new();
    let m1 = fx.commit(
        None,
        &[("img.bin", Some("\0X")), ("a.txt", Some("a\n"))],
        "m1",
    );
    let p = fx.commit(Some(m1), &[("p.txt", Some("p\n"))], &msg("feat: parent"));
    let c1 = fx.commit(
        Some(p),
        &[("img.bin", Some("\0Y")), ("a.txt", Some("a2\n"))],
        &msg("feat: one"),
    );
    let c2 = fx.commit(Some(c1), &[("img.bin", Some("\0Z"))], &msg("feat: two"));
    let m2 = fx.commit(Some(m1), &[("m.txt", Some("m\n"))], "m2");
    let p2 = fx.commit(Some(m2), &[("p.txt", Some("p\n"))], &msg("feat: parent"));
    let n1 = fx.commit(
        Some(p2),
        &[("img.bin", Some("\0W")), ("a.txt", Some("a2\n"))],
        &msg("feat: one"),
    );
    let n2 = fx.commit(Some(n1), &[("img.bin", Some("\0Z"))], &msg("feat: two"));
    let r = run(
        &fx,
        &[&s(c2), &s(n2), "--old-parent", &s(p), "--base", &s(p2)],
    );
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(
        r.check_line("commit-patches").contains("failed"),
        "{}",
        r.text
    );
    assert!(!r.text.contains("Carried-over:"));
}
