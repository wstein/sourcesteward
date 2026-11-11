//! `stw status --review`: the review state of the open pull requests from their comments (Go: `--review` of
//! `internal/cli/status.go`, `internal/pulls/review.go`, `internal/cli/status_review_test.go`; the states come from the
//! M3 parser, `src/review.rs`). Cases follow `internal/reviewstate/state_test.go`: CLEAR at the head, FINDINGS, a
//! CLEAR of another head, a valid and an invalid carry-over, no review comment, a foreign author, later comments
//! that win. Deviations from Go are listed in README.md, "Review state in `stw status --review`".
use std::collections::BTreeMap;
use std::process::{Command, Output};
use std::sync::Arc;

use crate::common::*;
use serde_json::{json, Value};

const TOKEN: &str = "SECRETTOKEN-0123456789";
/// Head of every pull request, and another head for stale reviews.
const HEAD: &str = "1111111111111111111111111111111111111111";
const OLD: &str = "2222222222222222222222222222222222222222";
const PATCH: &str = "3333333333333333333333333333333333333333";
const OTHER_PATCH: &str = "4444444444444444444444444444444444444444";

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

fn json_of(o: &Output) -> Value {
    serde_json::from_slice(&o.stdout).unwrap_or_else(|e| panic!("{e}: {}", out(o)))
}

fn flat(t: &str) -> String {
    t.lines()
        .map(|l| l.split_whitespace().collect::<Vec<_>>().join(" "))
        .collect::<Vec<_>>()
        .join("\n")
}

fn review(verdict: &str, head: &str, patch: &str, pr: u64) -> String {
    format!("Review: {verdict} head={head} patch-id={patch} pr={pr}")
}

fn carried(from: &str, to: &str, patch: &str, pr: u64) -> String {
    format!("Carried-over: from={from} to={to} patch-id={patch} pr={pr}")
}

/// A comment of `login` with an `author_association`; `n` orders the comments in time.
fn comment(n: u64, login: &str, assoc: &str, body: &str) -> Value {
    let stamp = format!("2026-03-01T10:{:02}:00Z", n);
    json!({"id": 1000 + n, "user": {"login": login, "type": "User"}, "author_association": assoc,
           "created_at": stamp, "updated_at": stamp, "body": body})
}

fn owner(n: u64, body: &str) -> Value {
    comment(n, "boss", "OWNER", body)
}

/// What the comment read of one pull request answers.
#[derive(Clone)]
enum Answer {
    Comments(Vec<Value>),
    /// Every page is full: the list never ends.
    Endless,
    Status(u16, Vec<(String, String)>),
}

/// test/fixture with the given open pull requests `(number, head sha, title)` and their comment answers.
fn forge(prs: Vec<(u64, &'static str, &'static str)>, answers: Vec<(u64, Answer)>) -> FakeRoutes {
    forge_with(prs, answers, None, "clean")
}

/// `forge` with a fixed head ref for every pull request (None: `b{n}`) and the `mergeable_state` of the single-PR answer.
fn forge_with(
    prs: Vec<(u64, &'static str, &'static str)>,
    answers: Vec<(u64, Answer)>,
    head_ref: Option<&'static str>,
    merge_state: &'static str,
) -> FakeRoutes {
    let answers: Arc<BTreeMap<u64, Answer>> = Arc::new(answers.into_iter().collect());
    FakeRoutes::start(move |path, query| {
        let ok = |v: Value| (200, vec![], v.to_string());
        match path {
            "/repos/test/fixture/pulls" => ok(Value::Array(
                prs.iter()
                    .map(|(n, sha, title)| {
                        json!({"number": n, "title": title, "draft": false,
                               "user": {"login": "me", "type": "User"},
                               "head": {"ref": head_ref.map_or(format!("b{n}"), str::to_string), "sha": sha}, "state": "open", "merged_at": null})
                    })
                    .collect(),
            )),
            "/repos/test/fixture/issues" => ok(json!([])),
            p if p.starts_with("/repos/test/fixture/pulls/") => ok(json!({"mergeable_state": merge_state})),
            p if p.starts_with("/repos/test/fixture/issues/") && p.ends_with("/comments") => {
                let n: u64 = p
                    .trim_start_matches("/repos/test/fixture/issues/")
                    .trim_end_matches("/comments")
                    .parse()
                    .unwrap();
                let (page, per) = page_of(query);
                match answers.get(&n) {
                    None => ok(json!([])),
                    Some(Answer::Comments(c)) => {
                        ok(Value::Array(c.iter().skip((page - 1) * per).take(per).cloned().collect()))
                    }
                    Some(Answer::Endless) => ok(Value::Array(
                        (0..per as u64)
                            .map(|i| {
                                let mut c = owner(0, &format!("chatter {page}/{i}"));
                                c["id"] = json!(100_000 + page as u64 * 1000 + i);
                                c
                            })
                            .collect(),
                    )),
                    Some(Answer::Status(code, h)) => (*code, h.clone(), "{}".into()),
                }
            }
            _ => (404, vec![], "{}".into()),
        }
    })
}

fn comment_hits(api: &FakeRoutes, n: u64) -> usize {
    let needle = format!("GET /repos/test/fixture/issues/{n}/comments?");
    api.hits().iter().filter(|h| h.starts_with(&needle)).count()
}

fn prs_json(o: &Output) -> Vec<Value> {
    json_of(o)["repos"][0]["pull_requests"]["open"]
        .as_array()
        .unwrap()
        .clone()
}

fn by_number(prs: &[Value], n: u64) -> &Value {
    prs.iter().find(|p| p["number"] == n).expect("pull request")
}

fn run_review(api: &FakeRoutes, extra: &[&str]) -> Output {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let mut args = vec!["status", "--root", &r, "--review"];
    args.extend_from_slice(extra);
    stw(Some(&api.url), &args)
}

#[test]
fn a_clear_at_the_head_is_shown_in_the_text_and_in_the_json() {
    let api = forge(
        vec![(7, HEAD, "seven")],
        vec![(
            7,
            Answer::Comments(vec![owner(1, &review("CLEAR", HEAD, PATCH, 7))]),
        )],
    );
    let o = run_review(&api, &[]);
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let t = out(&o);
    assert!(t.contains("#7 seven [b7, review clear, CLEAN]"), "{t}");
    assert!(flat(&t).contains("\nreviews: 1 clear\n"), "{t}");
    let o2 = run_review(&api, &["--json"]);
    let prs = prs_json(&o2);
    assert_eq!(prs[0]["review"], "clear");
    let st = &prs[0]["review_state"];
    assert_eq!(st["status"], "clear");
    assert_eq!(st["head"], HEAD);
    assert_eq!(st["decisive"]["comment_id"], 1001);
    assert_eq!(st["decisive"]["author"], "boss");
    assert!(st["read_error"].is_null());
    let v = json_of(&o2);
    assert_eq!(v["options"]["review"], true);
    let rv = &v["repos"][0]["pull_requests"]["review"];
    assert_eq!(rv["states"]["clear"], 1);
    assert_eq!(rv["not_read"], 0);
    assert_eq!(rv["limit"], 200);
}

/// Go `state_test.go` (clear, findings, clear-stale, carried, unparsed, none) with the M3 states.
#[test]
fn the_states_of_a_mixed_set_of_pull_requests() {
    let a = |c: Vec<Value>| Answer::Comments(c);
    let api = forge(
        vec![
            (1, HEAD, "clear"),
            (2, HEAD, "findings"),
            (3, HEAD, "other head"),
            (4, HEAD, "carried"),
            (5, HEAD, "bad carry"),
            (6, HEAD, "no comment"),
            (7, HEAD, "foreign author"),
            (8, HEAD, "findings after clear"),
            (9, HEAD, "clear after findings"),
            (10, HEAD, "other pr"),
            (11, HEAD, "chat only"),
            (12, HEAD, "member"),
            (13, HEAD, "collaborator"),
            (14, HEAD, "contributor"),
        ],
        vec![
            (1, a(vec![owner(1, &review("CLEAR", HEAD, PATCH, 1))])),
            (2, a(vec![owner(1, &review("FINDINGS", HEAD, PATCH, 2))])),
            (3, a(vec![owner(1, &review("CLEAR", OLD, PATCH, 3))])),
            (
                4,
                a(vec![
                    owner(1, &review("CLEAR", OLD, PATCH, 4)),
                    owner(2, &carried(OLD, HEAD, PATCH, 4)),
                ]),
            ),
            (
                5,
                a(vec![
                    owner(1, &review("CLEAR", OLD, PATCH, 5)),
                    owner(2, &carried(OLD, HEAD, OTHER_PATCH, 5)),
                ]),
            ),
            (
                7,
                a(vec![
                    comment(1, "stranger", "NONE", &review("CLEAR", HEAD, PATCH, 7)),
                    owner(2, "looks fine"),
                ]),
            ),
            (
                8,
                a(vec![
                    owner(1, &review("CLEAR", HEAD, PATCH, 8)),
                    owner(2, &review("FINDINGS", HEAD, PATCH, 8)),
                ]),
            ),
            (
                9,
                a(vec![
                    owner(1, &review("FINDINGS", HEAD, PATCH, 9)),
                    owner(2, &review("CLEAR", HEAD, PATCH, 9)),
                ]),
            ),
            (10, a(vec![owner(1, &review("CLEAR", HEAD, PATCH, 99))])),
            (11, a(vec![owner(1, "thanks"), owner(2, "will do")])),
            (
                12,
                a(vec![comment(
                    1,
                    "mem",
                    "MEMBER",
                    &review("CLEAR", HEAD, PATCH, 12),
                )]),
            ),
            (
                13,
                a(vec![comment(
                    1,
                    "col",
                    "COLLABORATOR",
                    &review("CLEAR", HEAD, PATCH, 13),
                )]),
            ),
            (
                14,
                a(vec![comment(
                    1,
                    "con",
                    "CONTRIBUTOR",
                    &review("CLEAR", HEAD, PATCH, 14),
                )]),
            ),
        ],
    );
    let o = run_review(&api, &["--json"]);
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let prs = prs_json(&o);
    let label = |n: u64| by_number(&prs, n)["review"].as_str().unwrap().to_string();
    assert_eq!(label(1), "clear");
    assert_eq!(label(2), "findings");
    assert_eq!(
        label(3),
        "no-review",
        "a CLEAR of another head says nothing about this head"
    );
    assert_eq!(label(4), "clear-carried");
    assert_eq!(
        label(5),
        "no-review",
        "a carry-over with another patch-id is not valid"
    );
    assert_eq!(label(6), "no-review");
    assert_eq!(label(7), "unverified-author");
    assert_eq!(label(8), "findings");
    assert_eq!(label(9), "clear");
    assert_eq!(
        label(10),
        "unknown",
        "a trusted line of another pull request that names the head"
    );
    assert_eq!(label(11), "no-review");
    assert_eq!(label(12), "clear", "MEMBER is trusted");
    assert_eq!(label(13), "clear", "COLLABORATOR is trusted");
    assert_eq!(label(14), "unverified-author", "CONTRIBUTOR is not trusted");
    let kinds = |n: u64| -> Vec<String> {
        by_number(&prs, n)["review_state"]["findings"]
            .as_array()
            .unwrap()
            .iter()
            .map(|f| f["kind"].as_str().unwrap().to_string())
            .collect()
    };
    assert!(
        kinds(5).contains(&"invalid-carry-chain".to_string()),
        "{:?}",
        kinds(5)
    );
    assert!(kinds(3).contains(&"stale-head".to_string()));
    assert!(kinds(7).contains(&"untrusted-author".to_string()));
    assert!(kinds(10).contains(&"wrong-pr".to_string()));
    let chain = by_number(&prs, 4)["review_state"]["chain"]
        .as_array()
        .unwrap()
        .len();
    assert_eq!(chain, 2, "the CLEAR and the carry-over");
    let rv = &json_of(&o)["repos"][0]["pull_requests"]["review"];
    assert_eq!(
        rv["states"],
        json!({"clear": 4, "clear_carried": 1, "findings": 2, "no_review": 4, "unverified_author": 2, "unknown": 1})
    );
    let t = out(&run_review(&api, &[]));
    assert!(
        flat(&t).contains("\nreviews: 4 clear, 1 clear-carried, 2 findings, 4 no-review, 2 unverified-author, 1 ?\n"),
        "{t}"
    );
    assert!(t.contains("#10 other pr [b10, review ?, CLEAN]"), "{t}");
}

#[test]
fn each_pull_request_costs_one_comment_read_and_the_counts_do_not_change() {
    let api = forge(
        vec![(1, HEAD, "one"), (2, HEAD, "two"), (3, HEAD, "three")],
        vec![(
            2,
            Answer::Comments(vec![owner(1, &review("CLEAR", HEAD, PATCH, 2))]),
        )],
    );
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let plain = stw(Some(&api.url), &["status", "--root", &r, "--json"]);
    assert!(
        api.hits().iter().all(|h| !h.contains("/comments")),
        "a plain run reads no comments"
    );
    let with = stw(
        Some(&api.url),
        &["status", "--root", &r, "--review", "--json"],
    );
    for n in [1, 2, 3] {
        assert_eq!(comment_hits(&api, n), 1, "PR {n}: {:?}", api.hits());
    }
    let (p, w) = (json_of(&plain), json_of(&with));
    assert_eq!(p["repos"][0]["counts"], w["repos"][0]["counts"]);
    assert_eq!(p["totals"], w["totals"]);
    assert_eq!(p["summary"], w["summary"]);
    // a plain run has no review state anywhere
    assert_eq!(p["options"]["review"], false);
    assert!(p["repos"][0]["pull_requests"]["review"].is_null());
    for pr in p["repos"][0]["pull_requests"]["open"].as_array().unwrap() {
        assert!(
            pr["review"].is_null() && pr["review_state"].is_null(),
            "{pr}"
        );
    }
    // text: the Counts block is the same, no review words in a plain run
    let (tp, tw) = (
        out(&stw(Some(&api.url), &["status", "--root", &r])),
        out(&stw(Some(&api.url), &["status", "--root", &r, "--review"])),
    );
    let counts = |t: &str| t.split("\n\n").next().unwrap().to_string();
    assert_eq!(counts(&tp), counts(&tw));
    assert!(!tp.contains("review"), "{tp}");
}

#[test]
fn a_comment_read_that_is_denied_missing_or_broken_is_a_question_mark_and_a_notice_never_a_state() {
    let st = |c: u16| Answer::Status(c, vec![]);
    let api = forge(
        vec![
            (1, HEAD, "one"),
            (2, HEAD, "two"),
            (3, HEAD, "three"),
            (4, HEAD, "four"),
        ],
        vec![
            (1, st(403)),
            (2, st(404)),
            (3, st(500)),
            (
                4,
                Answer::Status(429, vec![("x-ratelimit-remaining".into(), "0".into())]),
            ),
        ],
    );
    let o = run_review(&api, &[]);
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let t = out(&o);
    for n in 1..=4 {
        assert!(
            t.contains(&format!("#{n} ")) && t.contains(&format!("b{n}, review ?, CLEAN]")),
            "{t}"
        );
    }
    assert!(flat(&t).contains("\nreviews: 4 ?\n"), "{t}");
    let notices = t
        .split("\n\n")
        .find(|b| b.starts_with("Notices ("))
        .unwrap();
    assert!(
        notices.contains("repo: review state not read for 4 pull requests (#1:"),
        "{notices}"
    );
    assert!(flat(&t).contains("PRs 4 (bot 0)"), "the counts stay: {t}");
    let o = run_review(&api, &["--json"]);
    let prs = prs_json(&o);
    for n in 1..=4 {
        let p = by_number(&prs, n);
        assert_eq!(p["review"], "unknown", "{p}");
        assert!(p["review_state"]["decisive"].is_null());
        assert!(p["review_state"]["read_error"].is_string(), "{p}");
    }
    let err_of = |n: u64| {
        by_number(&prs, n)["review_state"]["read_error"]
            .as_str()
            .unwrap()
            .to_string()
    };
    assert!(
        err_of(1).contains("403") && err_of(1).contains("permission"),
        "{}",
        err_of(1)
    );
    assert!(err_of(2).contains("404"), "{}", err_of(2));
    assert!(err_of(3).contains("500"), "{}", err_of(3));
    assert!(err_of(4).contains("rate limit"), "{}", err_of(4));
}

#[test]
fn a_comment_list_that_hits_the_read_budget_is_unknown() {
    let api = forge(
        vec![(1, HEAD, "short thread"), (2, HEAD, "endless")],
        vec![(2, Answer::Endless)],
    );
    let o = run_review(&api, &["--json"]);
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let p = prs_json(&o);
    assert_eq!(by_number(&p, 2)["review"], "unknown");
    let why = by_number(&p, 2)["review_state"]["read_error"]
        .as_str()
        .unwrap();
    assert!(why.contains("item budget of 500"), "{why}");
    assert_eq!(
        comment_hits(&api, 2),
        6,
        "5 full pages fill the item budget, the first item of page 6 trips it: no further page"
    );
    assert_eq!(by_number(&p, 1)["review"], "no-review");
}

#[test]
fn review_is_a_usage_error_with_offline_and_nothing_is_read() {
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let api = forge(vec![(1, HEAD, "one")], vec![]);
    for args in [vec!["--offline", "--review"], vec!["--review", "--offline"]] {
        let mut a = vec!["status", "--root", &r];
        a.extend(args.iter());
        let o = stw(Some(&api.url), &a);
        assert_eq!(o.status.code(), Some(2), "{}", err(&o));
        assert!(
            err(&o).contains("--review") && err(&o).contains("--offline"),
            "{}",
            err(&o)
        );
        assert!(out(&o).is_empty());
    }
    assert!(api.hits().is_empty());
    // --review=maybe is no flag value
    let o = stw(None, &["status", "--root", &r, "--review=maybe"]);
    assert_eq!(o.status.code(), Some(2));
}

#[test]
fn a_pull_request_with_a_head_that_is_not_a_full_commit_id_is_unknown() {
    let api = forge(vec![(1, "abc", "short head")], vec![]);
    let o = run_review(&api, &["--json"]);
    let prs = prs_json(&o);
    assert_eq!(prs[0]["review"], "unknown");
    assert!(prs[0]["review_state"]["read_error"]
        .as_str()
        .unwrap()
        .contains("commit id"));
    assert_eq!(
        comment_hits(&api, 1),
        0,
        "nothing to bind a verdict to: no read"
    );
}

#[test]
fn long_text_shows_the_evidence_of_each_state() {
    let api = forge(
        vec![(1, HEAD, "carried"), (2, HEAD, "findings and chatter")],
        vec![
            (
                1,
                Answer::Comments(vec![
                    owner(1, &review("CLEAR", OLD, PATCH, 1)),
                    owner(2, &carried(OLD, HEAD, PATCH, 1)),
                ]),
            ),
            (
                2,
                Answer::Comments(vec![
                    comment(1, "stranger", "NONE", &review("CLEAR", HEAD, PATCH, 2)),
                    owner(2, &review("FINDINGS", HEAD, PATCH, 2)),
                ]),
            ),
        ],
    );
    let o = run_review(&api, &["--long"]);
    let t = out(&o);
    assert!(
        t.contains(&format!(
            "        decided by comment 1002 of boss at 2026-03-01T10:02:00Z: Carried-over: from={OLD} to={HEAD} patch-id={PATCH} pr=1\n"
        )),
        "{t}"
    );
    assert!(
        t.contains("        carried through 1 link, from comment 1001 of boss at"),
        "{t}"
    );
    assert!(
        t.contains(&format!(
            "        decided by comment 1002 of boss at 2026-03-01T10:02:00Z: Review: FINDINGS head={HEAD} patch-id={PATCH} pr=2\n"
        )),
        "{t}"
    );
    assert!(
        t.contains("        finding untrusted-author: comment 1001 of stranger\n"),
        "{t}"
    );
    // without --long the evidence lines are not there
    let short = out(&run_review(&api, &[]));
    assert!(
        !short.contains("decided by") && !short.contains("finding "),
        "{short}"
    );
}

#[test]
fn text_of_the_forge_is_escaped_and_cut_in_text_and_json() {
    let hostile = format!(
        "Review: CLEAR\u{1b}[31m head={HEAD} patch-id={PATCH} pr=1 {}",
        "x".repeat(400)
    );
    let api = forge(
        vec![(1, HEAD, "title \u{1b}[2J\u{202e}reversed")],
        vec![(
            1,
            Answer::Comments(vec![
                owner(1, &hostile),
                owner(2, "second\r\nReview: CLEAR forged"),
            ]),
        )],
    );
    let o = run_review(&api, &["--long"]);
    let t = out(&o);
    for c in t.chars() {
        assert!(
            c == '\n' || !c.is_control(),
            "raw control character {c:?} in {t}"
        );
        assert!(
            !matches!(c as u32, 0x202A..=0x202E | 0x200B..=0x200F),
            "{t}"
        );
    }
    assert!(
        t.contains(
            "finding unparsed-review-variant: comment 1001 of boss: Review: CLEAR\\u{1b}[31m head="
        ),
        "{t}"
    );
    assert!(t.contains("title \\u{1b}[2J\\u{202e}reversed"), "{t}");
    assert!(!t.contains(&"x".repeat(150)), "the line is cut");
    // the second comment is not a line: its text never appears
    assert!(!t.contains("forged"), "{t}");
    let j = json_of(&run_review(&api, &["--json"]));
    let raw = j.to_string();
    assert!(
        !raw.contains(&"x".repeat(150)),
        "the line is cut in the JSON too"
    );
    let f = &j["repos"][0]["pull_requests"]["open"][0]["review_state"]["findings"][0];
    assert_eq!(f["kind"], "unparsed-review-variant");
    assert!(f["detail"].as_str().unwrap().contains("\\u{1b}"), "{f}");
    assert!(!out(&run_review(&api, &["--json"])).contains('\u{1b}'));
}

#[test]
fn the_comment_reads_are_counted_in_the_stats_as_pulls_requests() {
    let api = forge(vec![(1, HEAD, "one"), (2, HEAD, "two")], vec![]);
    let fx = status_fixture();
    let r = path_str(&fx.path);
    let n = |args: &[&str]| -> u64 {
        let mut a = vec!["status", "--root", &r, "--json", "--stats"];
        a.extend_from_slice(args);
        json_of(&stw(Some(&api.url), &a))["stats"]["requests"]
            .as_u64()
            .unwrap()
    };
    let base = n(&[]);
    assert_eq!(n(&["--review"]), base + 2, "one request per pull request");
    let v = stw(
        Some(&api.url),
        &["status", "--root", &r, "--review", "--verbose"],
    );
    let t = out(&v);
    assert!(
        t.contains("    pulls:      5 reqs"),
        "list + 2 merge states + 2 comment reads: {t}"
    );
}

#[test]
fn the_timeout_cuts_the_comment_reads_to_question_marks_and_a_notice() {
    let api = FakeRoutes::start(|path, _| {
        if path.ends_with("/comments") {
            std::thread::sleep(std::time::Duration::from_secs(4));
            return (200, vec![], "[]".into());
        }
        let body = match path {
            "/repos/test/fixture/pulls" => json!([
                {"number": 1, "title": "one", "draft": false, "user": {"login": "me", "type": "User"},
                 "head": {"ref": "b1", "sha": HEAD}},
                {"number": 2, "title": "two", "draft": false, "user": {"login": "me", "type": "User"},
                 "head": {"ref": "b2", "sha": HEAD}}]),
            "/repos/test/fixture/issues" => json!([]),
            _ => json!({"mergeable_state": "clean"}),
        };
        (200, vec![], body.to_string())
    });
    let t0 = std::time::Instant::now();
    let o = run_review(&api, &["--timeout", "1s"]);
    assert!(
        t0.elapsed() < std::time::Duration::from_secs(3),
        "{:?}",
        t0.elapsed()
    );
    assert_eq!(o.status.code(), Some(0), "{}{}", out(&o), err(&o));
    let t = out(&o);
    assert!(flat(&t).contains("\nreviews: 2 ?\n"), "{t}");
    let notices = t
        .split("\n\n")
        .find(|b| b.starts_with("Notices ("))
        .unwrap();
    assert!(
        notices.contains("review state not read for 2 pull requests"),
        "{notices}"
    );
    assert!(notices.contains("--timeout of 1s"), "{notices}");
    assert!(
        flat(&t).contains("PRs 2 (bot 0)"),
        "the PR count is intact: {t}"
    );
}

/// More pull requests than the limit (several repositories can add up to it): exactly `REVIEW_LIMIT` reads, the rest
/// is `not read`, in the text and the JSON counters.
#[test]
fn at_most_the_review_limit_of_pull_requests_is_read_and_the_rest_is_not_read() {
    use stw::auth::TokenSource;
    use stw::github::OpenPr;
    use stw::status::{collect_local, StatusOpts};
    use stw::status_review::{read_reviews, tally, REVIEW_LIMIT};
    let fx = status_fixture();
    let root = fx.path.clone();
    let rr = stw::repo::RepoRef {
        name: "fixture".into(),
        path: root.clone(),
    };
    let api = FakeRoutes::start(|_, _| (200, vec![], "[]".into()));
    let client = stw::github::Client::with_base(TOKEN.into(), TokenSource::Env, api.url.clone());
    let mut s = collect_local(&rr, &root, &StatusOpts::default(), None);
    s.slug = Some("test/fixture".into());
    s.pr_read = true;
    s.prs = (1..=REVIEW_LIMIT as u64 + 5)
        .map(|n| OpenPr {
            number: n,
            title: format!("pr {n}"),
            head: format!("b{n}"),
            head_sha: HEAD.into(),
            draft: false,
            author: "me".into(),
            bot: false,
            review: None,
            merge_state: None,
        })
        .collect();
    let list = read_reviews(vec![s], &client);
    assert_eq!(api.hits().len(), REVIEW_LIMIT);
    let t = tally(&list[0].prs);
    assert_eq!((t.no_review, t.not_read), (REVIEW_LIMIT, 5));
    assert!(list[0].prs[REVIEW_LIMIT - 1].review.is_some());
    assert!(list[0].prs[REVIEW_LIMIT].review.is_none());
    let notices = stw::status_review::notices(&list[0]);
    assert_eq!(
        notices,
        vec![
            "fixture: review state not read for 5 pull requests beyond the limit of 200 per run"
                .to_string()
        ]
    );
}

/// The head branch and the merge state of a pull request are text from the forge: escaped in the text.
#[test]
fn head_branch_and_merge_state_are_escaped_in_the_pull_request_line() {
    let api = forge_with(
        vec![(1, HEAD, "plain")],
        vec![],
        Some("br\u{1b}[2J\rx\u{202e}y"),
        "clean\u{1b}[31m",
    );
    let t = out(&run_review(&api, &[]));
    for c in t.chars() {
        assert!(
            c == '\n' || !c.is_control(),
            "raw control character {c:?} in {t}"
        );
        assert!(!matches!(c as u32, 0x202A..=0x202E), "{t}");
    }
    assert!(
        t.contains("[br\\u{1b}[2J\\rx\\u{202e}y, review no-review, CLEAN\\u{1b}[31M]"),
        "{t}"
    );
}
