//! `stw lint review FILE` and `stw lint pr-text FILE` (new, no Go counterpart): the text of a review comment or of a
//! PR text from a file, read-only, no network. Fixtures: real comments and PR texts, `tests/fixtures/lint/`.
//! Harness confinement: repositories and files live in tempdirs created here; the binary runs with a scrubbed
//! environment and starts no network.
use std::path::{Path, PathBuf};
use std::process::{Command, Output};

use crate::common::*;
use serde_json::Value;

const HEAD: &str = "82dc939a0115b4f116c8ac71fb73eb3b96a1d01b";
const PATCH: &str = "8a7ecc88da6cff3f867a0e932184ca39f5bd151a";

fn fixture(name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/lint")
        .join(name)
}

fn stw(args: &[&str]) -> Output {
    let home = scratch();
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", home.path())
        .current_dir(home.path())
        .arg("lint")
        .args(args)
        .output()
        .unwrap()
}

fn code(o: &Output) -> i32 {
    o.status.code().unwrap()
}

fn out_text(o: &Output) -> String {
    format!(
        "{}\n{}",
        String::from_utf8_lossy(&o.stdout),
        String::from_utf8_lossy(&o.stderr)
    )
}

/// The rule IDs of the findings of a `--json` run, in order.
fn rules(o: &Output) -> Vec<String> {
    let v: Value =
        serde_json::from_slice(&o.stdout).unwrap_or_else(|e| panic!("{e}: {}", out_text(o)));
    v["findings"]
        .as_array()
        .unwrap()
        .iter()
        .map(|f| f["rule"].as_str().unwrap().to_string())
        .collect()
}

/// A review text in a file of its own; returns the run of `lint review FILE --json EXTRA`.
fn review_of(text: &str, extra: &[&str]) -> Output {
    let dir = scratch();
    let f = dir.path().join("comment.txt");
    std::fs::write(&f, text).unwrap();
    let mut args = vec!["review", f.to_str().unwrap(), "--json"];
    args.extend(extra);
    stw(&args)
}

fn good_line() -> String {
    format!("Review: CLEAR head={HEAD} patch-id={PATCH} pr=49")
}

fn good_text() -> String {
    format!(
        "{}\nRound 1: nothing found.\nPR body: 7 lines, no session links.\n",
        good_line()
    )
}

#[test]
fn real_review_comments_pass() {
    for name in [
        "review-cubelab-49-clear.txt",
        "review-dotagents-99-findings.txt",
        "review-dotagents-99-clear.txt",
        "carried-over-cube-assembler-61.txt",
    ] {
        let f = fixture(name);
        let o = stw(&["review", f.to_str().unwrap(), "--json"]);
        assert_eq!(code(&o), 0, "{name}: {}", out_text(&o));
        let v: Value = serde_json::from_slice(&o.stdout).unwrap();
        assert_eq!(v["summary"]["passed"], true, "{name}");
        assert_eq!(v["findings"].as_array().unwrap().len(), 0, "{name}");
    }
}

#[test]
fn json_document_shape() {
    let f = fixture("review-cubelab-49-clear.txt");
    let o = stw(&["review", f.to_str().unwrap(), "--json"]);
    let v: Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(v["schema_version"], 1);
    assert_eq!(v["command"], "lint review");
    assert!(v["time"].as_str().unwrap().ends_with('Z'));
    assert!(v["file"]
        .as_str()
        .unwrap()
        .ends_with("review-cubelab-49-clear.txt"));
    let ids: Vec<&str> = v["rules"]
        .as_array()
        .unwrap()
        .iter()
        .map(|r| r["id"].as_str().unwrap())
        .collect();
    for id in [
        "review-line-format",
        "review-patch-id",
        "review-head",
        "review-comment-length",
        "session-link",
        "text-line-endings",
    ] {
        assert!(ids.contains(&id), "rule {id} listed: {ids:?}");
    }
    assert!(
        !ids.contains(&"review-session-word"),
        "no rule on the word session: {ids:?}"
    );
    assert_eq!(v["summary"]["errors"], 0);
    assert_eq!(v["summary"]["warnings"], 0);
}

#[test]
fn human_output_lists_each_finding() {
    let dir = scratch();
    let f = dir.path().join("c.txt");
    std::fs::write(&f, format!("{}\nbody\r\n", good_line())).unwrap();
    let o = stw(&["review", f.to_str().unwrap()]);
    assert_eq!(code(&o), 3, "{}", out_text(&o));
    let text = String::from_utf8_lossy(&o.stdout).into_owned();
    assert!(text.contains("FAIL, 1 errors, 0 warnings"), "{text}");
    assert!(text.contains("text-line-endings"), "{text}");
    assert!(text.contains("file:2"), "{text}");
}

#[test]
fn the_first_line_follows_the_fixed_grammar() {
    let h = HEAD;
    let p = PATCH;
    let bad: Vec<(&str, String)> = vec![
        (
            "uppercase hex",
            format!("Review: CLEAR head={} patch-id={p} pr=49", h.to_uppercase()),
        ),
        (
            "uppercase patch-id",
            format!("Review: CLEAR head={h} patch-id={} pr=49", p.to_uppercase()),
        ),
        (
            "short head",
            "Review: CLEAR head=82dc939 patch-id=8a7ecc88da6cff3f867a0e932184ca39f5bd151a pr=49"
                .into(),
        ),
        (
            "order",
            format!("Review: CLEAR patch-id={p} head={h} pr=49"),
        ),
        (
            "verdict",
            format!("Review: clear head={h} patch-id={p} pr=49"),
        ),
        (
            "zero pr",
            format!("Review: CLEAR head={h} patch-id={p} pr=0"),
        ),
        (
            "leading zero pr",
            format!("Review: CLEAR head={h} patch-id={p} pr=049"),
        ),
        (
            "markdown",
            format!("**Review: CLEAR** head={h} patch-id={p} pr=49"),
        ),
        (
            "double blank",
            format!("Review:  CLEAR head={h} patch-id={p} pr=49"),
        ),
        (
            "trailing blank",
            format!("Review: CLEAR head={h} patch-id={p} pr=49 "),
        ),
        (
            "extra field",
            format!("Review: CLEAR head={h} patch-id={p} pr=49 x=1"),
        ),
        ("empty", String::new()),
    ];
    for (name, line) in bad {
        let o = review_of(&format!("{line}\nbody\n"), &[]);
        assert_eq!(code(&o), 3, "{name}: {}", out_text(&o));
        assert!(
            rules(&o).contains(&"review-line-format".to_string()),
            "{name}"
        );
    }
    // the first line is the first line: a blank line before it is no review line
    let o = review_of(&format!("\n{}\n", good_line()), &[]);
    assert_eq!(code(&o), 3);
    assert!(rules(&o).contains(&"review-line-format".to_string()));
    // pr=none is the local form
    let o = review_of(
        &format!("Review: FINDINGS head={HEAD} patch-id={PATCH} pr=none\n"),
        &[],
    );
    assert_eq!(code(&o), 0, "{}", out_text(&o));
}

#[test]
fn pr_has_at_most_nine_digits_as_in_lint_pr() {
    let line = |pr: &str| format!("Review: CLEAR head={HEAD} patch-id={PATCH} pr={pr}\nbody\n");
    assert_eq!(code(&review_of(&line("999999999"), &[])), 0);
    for pr in ["1000000000", "99999999999", "18446744073709551616"] {
        let o = review_of(&line(pr), &[]);
        assert_eq!(code(&o), 3, "pr={pr}");
        assert_eq!(rules(&o), vec!["review-line-format"], "pr={pr}");
    }
    let carried = format!("Carried-over: from={HEAD} to={HEAD} patch-id={PATCH} pr=1000000000\n");
    assert_eq!(rules(&review_of(&carried, &[])), vec!["review-line-format"]);
}

#[test]
fn carriage_returns_are_errors_anywhere() {
    // `pr=49\r`: CRLF line ends
    let o = review_of(&format!("{}\r\nbody\r\n", good_line()), &[]);
    assert_eq!(code(&o), 3);
    let r = rules(&o);
    assert!(r.contains(&"text-line-endings".to_string()), "{r:?}");
    assert!(r.contains(&"review-line-format".to_string()), "{r:?}");
    // a CR in a later line only
    let o = review_of(&format!("{}\nbody\r\nmore\n", good_line()), &[]);
    assert_eq!(code(&o), 3);
    assert_eq!(rules(&o), vec!["text-line-endings"]);
    // a lone CR inside a line
    let o = review_of(&format!("{}\nbo\rdy\n", good_line()), &[]);
    assert_eq!(rules(&o), vec!["text-line-endings"]);
}

#[test]
fn the_carried_over_form_is_fixed_too() {
    let from = "5da6a04090ad10bb55a9a012cd17c4fa2755a1a6";
    let to = "bfbf79cc1d3372c874bd7c3db028a416e3208f94";
    let p = "c9d382d0ef112f55ea237acfc0d8bb3e380c46b5";
    let ok = format!("Carried-over: from={from} to={to} patch-id={p} pr=61\nline 2\n");
    assert_eq!(code(&review_of(&ok, &[])), 0);
    for (name, line) in [
        (
            "order",
            format!("Carried-over: to={to} from={from} patch-id={p} pr=61"),
        ),
        (
            "uppercase",
            format!(
                "Carried-over: from={from} to={} patch-id={p} pr=61",
                to.to_uppercase()
            ),
        ),
        (
            "short",
            format!("Carried-over: from=5da6a04 to={to} patch-id={p} pr=61"),
        ),
        (
            "key",
            format!("Carried-over: from={from} head={to} patch-id={p} pr=61"),
        ),
        (
            "carried-over",
            format!("Carried-Over: from={from} to={to} patch-id={p} pr=61"),
        ),
    ] {
        let o = review_of(&format!("{line}\nline 2\n"), &[]);
        assert_eq!(code(&o), 3, "{name}");
        assert_eq!(rules(&o), vec!["review-line-format"], "{name}");
    }
}

#[test]
fn a_review_comment_has_at_most_15_lines() {
    let body = |n: usize| {
        let mut t = good_line();
        for i in 2..=n {
            t.push_str(&format!("\nline {i}"));
        }
        t.push('\n');
        t
    };
    for n in [1, 14, 15] {
        let o = review_of(&body(n), &[]);
        assert_eq!(code(&o), 0, "{n} lines: {}", out_text(&o));
    }
    let o = review_of(&body(16), &[]);
    assert_eq!(code(&o), 3);
    assert_eq!(rules(&o), vec!["review-comment-length"]);
    // trailing blank lines do not count
    let o = review_of(&format!("{}\n\n\n", body(15)), &[]);
    assert_eq!(code(&o), 0, "{}", out_text(&o));
}

#[test]
fn the_word_session_is_no_finding_only_the_link_patterns_are() {
    // owner decision: only the session-link patterns count, as in `lint pr`
    for text in [
        "the session ended",
        "Session-Link-Prüfung: 0",
        "possession of the branch",
        "PR body: 7 lines, no session links.",
        "session links: none",
    ] {
        let o = review_of(&format!("{}\n{text}\n", good_line()), &[]);
        assert_eq!(code(&o), 0, "{text}: {}", out_text(&o));
    }
}

#[test]
fn a_session_link_is_an_error() {
    // assembled so that this file does not hold the patterns in plain text
    let trailer = concat!("Claude", "-Session: https://example.invalid/x");
    let url = concat!("https://claude", ".ai/code/session_", "01ABC");
    for text in [trailer, url] {
        let o = review_of(&format!("{}\n{text}\n", good_line()), &[]);
        assert_eq!(code(&o), 3, "{text}");
        assert!(rules(&o).contains(&"session-link".to_string()), "{text}");
    }
}

/// A repository with `base` and a `head` that adds a file; returns the checkout, base, head and the patch-id of
/// `base...head` as `stw dev patch-id` computes it for the head commit (one commit above the base).
fn repo_with_change() -> (Fx, String, String, String) {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("a\n"))], "base");
    let head = fx.commit(Some(base), &[("a.txt", Some("a\nb\n"))], "head");
    let patch = stw::patch_id::commit_patch_id(&fx.repo(), head)
        .unwrap()
        .expect("a patch")
        .to_string();
    (fx, base.to_string(), head.to_string(), patch)
}

#[test]
fn patch_id_and_head_are_compared_with_the_repository() {
    let (fx, base, head, patch) = repo_with_change();
    let co = fx.path.to_str().unwrap();
    let args = |extra: &[&'static str]| {
        let mut a = vec![
            "--base",
            base.as_str(),
            "--head",
            head.as_str(),
            "--checkout",
            co,
        ];
        a.extend(extra);
        a.into_iter().map(String::from).collect::<Vec<_>>()
    };
    let run = |text: &str, a: Vec<String>| {
        let r: Vec<&str> = a.iter().map(String::as_str).collect();
        review_of(text, &r)
    };
    let good = format!("Review: CLEAR head={head} patch-id={patch} pr=7\nok\n");
    let o = run(&good, args(&[]));
    assert_eq!(code(&o), 0, "{}", out_text(&o));
    // the revisions may be names: the head= field is compared with the resolved id
    fx.branch("topic", head.parse().unwrap());
    let o = run(&good, {
        let mut a = args(&[]);
        a[3] = "topic".into();
        a
    });
    assert_eq!(code(&o), 0, "{}", out_text(&o));
    // a wrong patch-id
    let wrong = "0".repeat(40);
    let o = run(
        &format!("Review: CLEAR head={head} patch-id={wrong} pr=7\n"),
        args(&[]),
    );
    assert_eq!(code(&o), 3);
    assert_eq!(rules(&o), vec!["review-patch-id"]);
    // a wrong head
    let other = "1".repeat(40);
    let o = run(
        &format!("Review: CLEAR head={other} patch-id={patch} pr=7\n"),
        args(&[]),
    );
    assert_eq!(rules(&o), vec!["review-head"]);
    // a wrong pr
    let o = run(&good, args(&["--pr", "8"]));
    assert_eq!(code(&o), 3);
    assert_eq!(rules(&o), vec!["review-line-format"]);
    let o = run(&good, args(&["--pr", "7"]));
    assert_eq!(code(&o), 0, "{}", out_text(&o));
    // the Carried-over form: `to` is the head
    let carried = format!("Carried-over: from={other} to={head} patch-id={patch} pr=7\n");
    assert_eq!(code(&run(&carried, args(&[]))), 0);
    let carried_bad = format!("Carried-over: from={other} to={other} patch-id={patch} pr=7\n");
    assert_eq!(rules(&run(&carried_bad, args(&[]))), vec!["review-head"]);
    let carried_wrong = format!("Carried-over: from={other} to={head} patch-id={wrong} pr=7\n");
    assert_eq!(
        rules(&run(&carried_wrong, args(&[]))),
        vec!["review-patch-id"]
    );
}

#[test]
fn usage_errors_exit_two() {
    let (fx, base, head, _) = repo_with_change();
    let co = fx.path.to_str().unwrap();
    let dir = scratch();
    let f = dir.path().join("c.txt");
    std::fs::write(&f, good_text()).unwrap();
    let fs = f.to_str().unwrap();
    for args in [
        vec!["review", fs, "--base", base.as_str(), "--checkout", co],
        vec!["review", fs, "--head", head.as_str(), "--checkout", co],
        vec![
            "review",
            fs,
            "--base",
            "nope",
            "--head",
            head.as_str(),
            "--checkout",
            co,
        ],
        vec!["review", fs, "--checkout", co],
        vec!["review", fs, "--json", "--human"],
        vec!["review", fs, "--pr", "0"],
        vec!["review", fs, "--pr", "x"],
        vec!["review", "/nonexistent/stw-lintfiles/comment.txt"],
        vec!["review"],
    ] {
        let o = stw(&args);
        assert_eq!(code(&o), 2, "{args:?}: {}", out_text(&o));
    }
    // not UTF-8
    std::fs::write(&f, [0xff, 0xfe, b'\n']).unwrap();
    assert_eq!(code(&stw(&["review", fs])), 2);
}

const GEN: &str = "🤖 Generated with [Claude Code](https://claude.com/claude-code)";

/// A PR text in a file of its own; returns the run of `lint pr-text FILE --json EXTRA`.
fn pr_text_of(text: &str, extra: &[&str]) -> Output {
    let dir = scratch();
    let f = dir.path().join("pr.txt");
    std::fs::write(&f, text).unwrap();
    let mut args = vec!["pr-text", f.to_str().unwrap(), "--json"];
    args.extend(extra);
    stw(&args)
}

fn good_pr(blocker: &str) -> String {
    format!("Purpose.\n\nChanges\n- one\n\nTests: 3 passed\nNot verified: nothing\n{blocker}\n\n{GEN}\n")
}

#[test]
fn real_pr_texts_pass_as_ready() {
    for name in [
        "pr-text-cubelab-49.txt",
        "pr-text-dotagents-99.txt",
        "pr-text-cube-assembler-61.txt",
    ] {
        let f = fixture(name);
        let o = stw(&["pr-text", f.to_str().unwrap(), "--ready", "--json"]);
        assert_eq!(code(&o), 0, "{name}: {}", out_text(&o));
        let v: Value = serde_json::from_slice(&o.stdout).unwrap();
        assert_eq!(v["command"], "lint pr-text");
        assert_eq!(v["summary"]["passed"], true, "{name}");
        // the same text is no Draft text: it has `Blocker: None.`
        let o = stw(&["pr-text", f.to_str().unwrap(), "--json"]);
        assert_eq!(code(&o), 3, "{name}");
        assert_eq!(rules(&o), vec!["pr-blocker-line"], "{name}");
    }
}

#[test]
fn a_draft_names_its_blocker_a_ready_pr_has_none() {
    let draft = good_pr("Blocker: Independent review pending");
    let o = pr_text_of(&draft, &[]);
    assert_eq!(code(&o), 0, "{}", out_text(&o));
    let o = pr_text_of(&draft, &["--ready"]);
    assert_eq!(code(&o), 3);
    assert_eq!(rules(&o), vec!["pr-blocker-line"]);
    let ready = good_pr("Blocker: None.");
    assert_eq!(code(&pr_text_of(&ready, &["--ready"])), 0);
    let o = pr_text_of(&ready, &[]);
    assert_eq!(code(&o), 3);
    assert_eq!(rules(&o), vec!["pr-blocker-line"]);
}

#[test]
fn none_in_any_spelling_is_no_named_blocker_and_ready_takes_only_the_exact_line() {
    // a Draft names a blocker (skill section 9: "Blocker oder keiner"; core: a Draft starts with a named blocker)
    for b in [
        "Blocker: none.",
        "Blocker: None",
        "Blocker: None. ",
        "Blocker: NONE",
        "Blocker: none",
        "Blocker: keiner",
        "Blocker: Keiner.",
    ] {
        let o = pr_text_of(&good_pr(b), &[]);
        assert_eq!(code(&o), 3, "draft {b:?}: {}", out_text(&o));
        assert_eq!(rules(&o), vec!["pr-blocker-line"], "draft {b:?}");
    }
    // a blocker that merely begins with None names one
    assert_eq!(
        code(&pr_text_of(
            &good_pr("Blocker: None of the CI runs is green"),
            &[]
        )),
        0
    );
    // a Ready PR has exactly `Blocker: None.`
    for b in [
        "Blocker: None.x",
        "Blocker: None. ",
        "Blocker: None. Review pending",
        "Blocker: Nonesuch",
        "Blocker: none.",
        "Blocker: None",
    ] {
        let o = pr_text_of(&good_pr(b), &["--ready"]);
        assert_eq!(code(&o), 3, "ready {b:?}: {}", out_text(&o));
        assert_eq!(rules(&o), vec!["pr-blocker-line"], "ready {b:?}");
    }
}

#[test]
fn the_blocker_line_is_exactly_one_line_of_the_fixed_form() {
    for (name, text) in [
        ("missing", good_pr("Tests again: 3 passed")),
        ("empty", good_pr("Blocker: ")),
        ("blank only", good_pr("Blocker:    ")),
        ("no blank after the colon", good_pr("Blocker:None.")),
        ("lower case", good_pr("blocker: None.")),
        ("markdown", good_pr("**Blocker:** None.")),
        ("plural", good_pr("Blockers: None.")),
        ("two lines", good_pr("Blocker: None.\nBlocker: None.")),
    ] {
        for extra in [&["--ready"][..], &[][..]] {
            let o = pr_text_of(&text, extra);
            assert_eq!(code(&o), 3, "{name} {extra:?}: {}", out_text(&o));
            assert_eq!(rules(&o), vec!["pr-blocker-line"], "{name} {extra:?}");
        }
    }
    // `None.` needs the full stop
    let o = pr_text_of(&good_pr("Blocker: None"), &["--ready"]);
    assert_eq!(rules(&o), vec!["pr-blocker-line"]);
}

#[test]
fn the_last_non_blank_line_is_the_attribution_line() {
    let body = "Purpose.\nBlocker: None.\n";
    assert_eq!(code(&pr_text_of(&format!("{body}{GEN}"), &["--ready"])), 0);
    assert_eq!(
        code(&pr_text_of(&format!("{body}{GEN}\n\n\n"), &["--ready"])),
        0
    );
    for (name, text) in [
        ("missing", body.to_string()),
        ("not last", format!("{body}{GEN}\nCloses #1\n")),
        ("other text", format!("{body}Generated with Claude Code\n")),
        (
            "other url",
            format!("{body}🤖 Generated with [Claude Code](https://claude.ai/code)\n"),
        ),
        (
            "no robot",
            format!("{body}Generated with [Claude Code](https://claude.com/claude-code)\n"),
        ),
        ("trailing blank", format!("{body}{GEN} \n")),
        ("empty file", String::new()),
    ] {
        let o = pr_text_of(&text, &["--ready"]);
        assert_eq!(code(&o), 3, "{name}: {}", out_text(&o));
        assert!(
            rules(&o).contains(&"pr-generated-line".to_string()),
            "{name}"
        );
    }
}

#[test]
fn a_session_link_and_a_carriage_return_are_errors() {
    let trailer = concat!("Claude", "-Session: https://example.invalid/x");
    let url = concat!("https://claude", ".ai/code/session_", "01ABC");
    for t in [trailer, url] {
        let text = format!("Purpose.\n{t}\nBlocker: None.\n\n{GEN}\n");
        let o = pr_text_of(&text, &["--ready"]);
        assert_eq!(code(&o), 3, "{t}");
        assert_eq!(rules(&o), vec!["session-link"], "{t}");
    }
    let o = pr_text_of(
        &good_pr("Blocker: None.").replace('\n', "\r\n"),
        &["--ready"],
    );
    assert_eq!(code(&o), 3);
    let r = rules(&o);
    assert!(r.contains(&"text-line-endings".to_string()), "{r:?}");
    // CR before the line feed does not make the Blocker or the attribution line wrong on their own
    assert!(!r.contains(&"pr-blocker-line".to_string()), "{r:?}");
    // the word session is no finding in a PR text
    let o = pr_text_of(
        &format!("Purpose: session handling.\nBlocker: None.\n{GEN}\n"),
        &["--ready"],
    );
    assert_eq!(code(&o), 0, "{}", out_text(&o));
}

#[test]
fn the_length_warns_above_25_and_fails_above_30() {
    let text = |n: usize| {
        let mut t = String::from("Purpose.\nBlocker: None.\n");
        for i in 3..n {
            t.push_str(&format!("line {i}\n"));
        }
        t.push_str(GEN);
        t.push('\n');
        t
    };
    for n in [10, 25] {
        let o = pr_text_of(&text(n), &["--ready"]);
        assert_eq!(code(&o), 0, "{n}: {}", out_text(&o));
        assert!(rules(&o).is_empty(), "{n}");
    }
    for n in [26, 30] {
        let o = pr_text_of(&text(n), &["--ready"]);
        assert_eq!(code(&o), 0, "{n}: a warning does not fail");
        assert_eq!(rules(&o), vec!["pr-body-length"], "{n}");
        let v: Value = serde_json::from_slice(&o.stdout).unwrap();
        assert_eq!(v["findings"][0]["severity"], "warning");
        assert_eq!(v["summary"]["warnings"], 1);
    }
    let o = pr_text_of(&text(31), &["--ready"]);
    assert_eq!(code(&o), 3);
    assert_eq!(rules(&o), vec!["pr-body-length"]);
    let v: Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(v["findings"][0]["severity"], "error");
}

#[test]
fn pr_text_json_and_usage() {
    let o = pr_text_of(&good_pr("Blocker: None."), &["--ready"]);
    let v: Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(v["schema_version"], 1);
    assert_eq!(v["options"]["ready"], true);
    let ids: Vec<&str> = v["rules"]
        .as_array()
        .unwrap()
        .iter()
        .map(|r| r["id"].as_str().unwrap())
        .collect();
    assert_eq!(
        ids,
        [
            "pr-body-length",
            "pr-blocker-line",
            "pr-generated-line",
            "session-link",
            "text-line-endings"
        ]
    );
    // human list
    let dir = scratch();
    let f = dir.path().join("pr.txt");
    std::fs::write(&f, "Purpose.\n").unwrap();
    let o = stw(&["pr-text", f.to_str().unwrap()]);
    assert_eq!(code(&o), 3);
    let t = String::from_utf8_lossy(&o.stdout).into_owned();
    assert!(t.contains("FAIL, 2 errors, 0 warnings"), "{t}");
    assert!(t.contains("pr-blocker-line"), "{t}");
    assert!(t.contains("pr-generated-line"), "{t}");
    for args in [
        vec!["pr-text", "/nonexistent/stw-lintfiles/pr.txt"],
        vec!["pr-text"],
        vec!["pr-text", f.to_str().unwrap(), "--json", "--human"],
    ] {
        assert_eq!(code(&stw(&args)), 2, "{args:?}");
    }
    std::fs::write(&f, [0xff, 0xfe]).unwrap();
    assert_eq!(code(&stw(&["pr-text", f.to_str().unwrap()])), 2);
}
