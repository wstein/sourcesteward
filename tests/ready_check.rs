use crate::common::FixtureCommand as _;
use stw::ready_check::{evaluate, ready, Facts, Status};

fn good() -> Facts {
    Facts {
        state: "open".into(),
        head: "a".repeat(40),
        base_ref: "main".into(),
        default_branch: "main".into(),
        review: "clear".into(),
        checks: "green".into(),
        mergeable: Some(true),
        rebaseable: Some(true),
        ..Facts::default()
    }
}

#[test]
fn every_condition_passes_on_the_good_pull_request() {
    let c = evaluate(&good());
    assert_eq!(
        c.iter().map(|c| c.id).collect::<Vec<_>>(),
        [
            "open",
            "review",
            "checks",
            "base",
            "blocker",
            "conflict",
            "rebaseable"
        ]
    );
    assert!(ready(&c));
    assert!(!ready(&[]));
}

#[test]
fn each_condition_fails_for_its_own_reason_only() {
    for i in 0..7 {
        let mut f = good();
        match i {
            0 => f.state = "closed".into(),
            1 => f.review = "findings".into(),
            2 => f.checks = "red".into(),
            3 => f.base_ref = "other".into(),
            4 => f.body = "Blocker: review pending".into(),
            5 => f.mergeable = Some(false),
            _ => f.rebaseable = Some(false),
        }
        let c = evaluate(&f);
        assert_eq!(c[i].status, Status::Fail, "{}", c[i].id);
        assert_eq!(c.iter().filter(|c| c.status != Status::Pass).count(), 1);
    }
}

#[test]
fn unknowns_never_pass_and_draft_is_not_a_condition() {
    let mut f = good();
    f.draft = true;
    assert!(ready(&evaluate(&f)));
    f.mergeable = None;
    assert_eq!(evaluate(&f)[5].status, Status::Unknown);
    f.pull_error = "unavailable".into();
    assert!(!ready(&evaluate(&f)));
}

#[test]
fn blocker_line_forms_and_none_spellings() {
    for body in [
        "Blocker: none",
        "## Blocker\n- none\n## Next\ntext",
        "| Blocker | n/a |",
        "Blocker-free",
        "**Blocker:** keine",
    ] {
        let mut f = good();
        f.body = body.into();
        assert_eq!(evaluate(&f)[4].status, Status::Pass, "{body}");
    }
    for body in [
        "Blocker: waiting",
        "## Blocker\n- waiting\n## Next",
        "Blocker:",
        "Blocker — waiting",
        "Status: Draft. Blocker： waiting",
    ] {
        let mut f = good();
        f.body = body.into();
        assert_eq!(evaluate(&f)[4].status, Status::Fail, "{body}");
    }
}

#[test]
fn stack_parent_and_carried_review_pass() {
    let mut f = good();
    f.base_ref = "topic".into();
    f.parent_pr = 4;
    f.review = "clear-carried".into();
    assert!(ready(&evaluate(&f)));
    f.parent_error = "not read".into();
    assert_eq!(evaluate(&f)[3].status, Status::Unknown);
}

#[test]
fn pull_detail_requires_valid_head_and_base_and_marks_merged() {
    use stw::ready_check::decode_pull;
    let p = serde_json::json!({"state":"closed","merged":true,"draft":true,"body":"Blocker: none", "head":{"sha":"a".repeat(40)}, "base":{"ref":"main"}, "mergeable":true,"rebaseable":true,"mergeable_state":"clean"});
    let f = decode_pull(&p).unwrap();
    assert_eq!(f.state, "merged");
    assert!(f.draft);
    assert_eq!(f.base_ref, "main");
    let mut malformed = p;
    malformed["head"]["sha"] = "bad".into();
    assert!(decode_pull(&malformed).is_err());
}

#[test]
fn cli_exposes_ready_check_and_validates_number() {
    let bin = env!("CARGO_BIN_EXE_stw");
    let help = std::process::Command::new(bin)
        .args(["ready-check", "--help"])
        .output()
        .unwrap();
    assert!(
        help.status.success(),
        "{}",
        String::from_utf8_lossy(&help.stderr)
    );
    let bad = std::process::Command::new(bin)
        .args(["ready-check", "0", "--repo", "o/r"])
        .output()
        .unwrap();
    assert_eq!(bad.status.code(), Some(2));
}

mod collection {
    use serde_json::{json, Value};
    use std::cell::RefCell;
    use stw::github::reads::{CheckRun, Comment, CommitStatus, Observed, Read, ReadyParent};
    use stw::ready_check::{collect, document, evaluate, ready, Source, Status};
    fn observed<T>(items: Vec<T>) -> Read<T> {
        Read::Observed(Observed {
            items,
            pages: 1,
            incomplete: None,
        })
    }
    struct Fake {
        calls: RefCell<Vec<&'static str>>,
        pull: Value,
        fail: &'static str,
        parent: Vec<ReadyParent>,
        conclusion: Option<&'static str>,
        run_status: &'static str,
        partial: bool,
        author: &'static str,
    }
    impl Fake {
        fn new() -> Self {
            Self {
                calls: RefCell::new(vec![]),
                pull: json!({"state":"open","draft":true,"body":"Blocker: none", "head":{"sha":"a".repeat(40)}, "base":{"ref":"main"}, "mergeable":true,"rebaseable":true,"mergeable_state":"clean"}),
                fail: "",
                parent: vec![],
                conclusion: Some("success"),
                run_status: "completed",
                partial: false,
                author: "OWNER",
            }
        }
        fn call(&self, name: &'static str) -> Result<(), String> {
            self.calls.borrow_mut().push(name);
            if self.fail == name {
                Err("failed".into())
            } else {
                Ok(())
            }
        }
    }
    impl Source for Fake {
        fn pull(&self, _: &str, _: u64) -> Result<Value, String> {
            self.call("pull")?;
            Ok(self.pull.clone())
        }
        fn repository(&self, _: &str) -> Result<Value, String> {
            self.call("repo")?;
            Ok(json!({"default_branch":"main"}))
        }
        fn parents(&self, _: &str) -> Result<Read<ReadyParent>, String> {
            self.call("parents")?;
            Ok(observed(self.parent.clone()))
        }
        fn runs(&self, _: &str, _: &str) -> Result<Read<CheckRun>, String> {
            self.call("runs")?;
            Ok(if self.partial {
                Read::Observed(Observed {
                    items: vec![],
                    pages: 1,
                    incomplete: Some("cut".into()),
                })
            } else {
                observed(vec![CheckRun {
                    id: 1,
                    name: "ci".into(),
                    status: self.run_status.into(),
                    conclusion: self.conclusion.map(str::to_string),
                    head_sha: Some("a".repeat(40)),
                    app: None,
                    started_at: None,
                    completed_at: None,
                }])
            })
        }
        fn statuses(&self, _: &str, _: &str) -> Result<Read<CommitStatus>, String> {
            self.call("statuses")?;
            Ok(observed(vec![]))
        }
        fn comments(&self, _: &str, _: u64) -> Result<Read<Comment>, String> {
            self.call("comments")?;
            Ok(observed(vec![Comment {
                submitted_review: false,
                id: 1,
                author: "alice".into(),
                author_type: "User".into(),
                association: self.author.into(),
                created_at: "2026-01-01T00:00:00Z".into(),
                updated_at: "2026-01-01T00:00:00Z".into(),
                body: format!(
                    "Review: CLEAR head={} patch-id={} pr=7",
                    "a".repeat(40),
                    "b".repeat(40)
                ),
                body_truncated: false,
            }]))
        }
    }
    #[test]
    fn run_ready_and_only_reads() {
        let src = Fake::new();
        let (f, e) = collect(&src, "o/r", 7);
        assert!(e.is_empty(), "{e:?}");
        assert!(ready(&evaluate(&f)), "{:?}", evaluate(&f));
        assert_eq!(
            *src.calls.borrow(),
            ["pull", "repo", "runs", "statuses", "comments"]
        );
        assert_eq!(document("o/r", 7, &f, &e, 0)["ready"], true);
    }
    #[test]
    fn read_failures_are_unknown_and_skip_dependent_reads() {
        let mut src = Fake::new();
        src.fail = "pull";
        let (f, e) = collect(&src, "o/r", 7);
        assert_eq!(e.len(), 1);
        assert_eq!(*src.calls.borrow(), ["pull", "repo"]);
        assert_eq!(evaluate(&f)[0].status, Status::Unknown);
        assert!(!ready(&evaluate(&f)));
    }
    #[test]
    fn parent_is_lowest_other_pr_and_list_only_when_needed() {
        let mut src = Fake::new();
        src.pull["base"]["ref"] = "topic".into();
        src.parent = vec![
            ReadyParent {
                number: 7,
                head_ref: "topic".into(),
            },
            ReadyParent {
                number: 9,
                head_ref: "topic".into(),
            },
            ReadyParent {
                number: 3,
                head_ref: "topic".into(),
            },
        ];
        let (f, _) = collect(&src, "o/r", 7);
        assert_eq!(f.parent_pr, 3);
        assert!(ready(&evaluate(&f)));
        assert!(src.calls.borrow().contains(&"parents"));
    }
    #[test]
    fn each_condition_fails_from_forge_data() {
        for i in 0..7 {
            let mut src = Fake::new();
            match i {
                0 => src.pull["state"] = "closed".into(),
                1 => src.author = "CONTRIBUTOR",
                2 => src.conclusion = Some("failure"),
                3 => src.pull["base"]["ref"] = "topic".into(),
                4 => src.pull["body"] = "Blocker: pending".into(),
                5 => src.pull["mergeable"] = false.into(),
                _ => src.pull["rebaseable"] = false.into(),
            }
            let (f, _) = collect(&src, "o/r", 7);
            assert_ne!(evaluate(&f)[i].status, Status::Pass, "condition {i}");
            assert!(!ready(&evaluate(&f)));
        }
    }
    #[test]
    fn scans_the_whole_body() {
        let mut src = Fake::new();
        src.pull["body"] = format!("{}\nBlocker: pending", "ä".repeat(40000)).into();
        let (f, _) = collect(&src, "o/r", 7);
        assert_eq!(evaluate(&f)[4].status, Status::Fail);
    }
    #[test]
    fn check_classes_and_incomplete_are_never_green() {
        for (status, conclusion, expected) in [
            ("queued", None, Status::Fail),
            ("completed", Some("failure"), Status::Fail),
            ("completed", Some("cancelled"), Status::Fail),
        ] {
            let mut src = Fake::new();
            src.run_status = status;
            src.conclusion = conclusion;
            let (f, _) = collect(&src, "o/r", 7);
            assert_eq!(evaluate(&f)[2].status, expected);
        }
        let mut src = Fake::new();
        src.partial = true;
        let (f, e) = collect(&src, "o/r", 7);
        assert_eq!(evaluate(&f)[2].status, Status::Unknown);
        assert_eq!(e[0].0, "checks");
    }
    #[test]
    fn untrusted_review_cannot_pass() {
        let mut src = Fake::new();
        src.author = "CONTRIBUTOR";
        let (f, _) = collect(&src, "o/r", 7);
        assert_ne!(evaluate(&f)[1].status, Status::Pass);
    }
    #[test]
    fn failure_of_checks_or_review_never_passes() {
        for source in ["runs", "statuses", "comments"] {
            let mut src = Fake::new();
            src.fail = source;
            let (f, e) = collect(&src, "o/r", 7);
            assert_eq!(e.len(), 1, "{source}");
            assert!(!ready(&evaluate(&f)), "{source}");
        }
    }
}

#[test]
fn go_blocker_corpus_extra_forms() {
    let failures = [
        "Blocker (Draft): pending re-review",
        "The stuck behavior. Blocker (Draft): pending",
        "Status: Draft. Blocker: x",
        "Blocker - x",
        "Blocker — x",
        "Blocker – x",
        "Blocker—x",
        "| Blocker | x |",
        "| Status | Draft | Blocker: x |",
        "- **Blocker** - x",
        "**Blocker (Draft):** x",
        "1. Blocker: x",
        "> Blocker: x",
        "Blockers: x",
        "## Blocker: x",
        "Blocker\u{a0}: x",
        "Blocker\u{a0}- x",
        "Blocker \u{a0}— x",
        "Blocker\u{3000}: x",
        "Blocker： x",
        "Blocker：x",
        "Blocker\u{a0}(Draft): x",
        "Blocker (Draft)\u{a0}: x",
        "Blocker -\u{a0}x",
        "Blocker -- x",
        "Blocker− x",
        "Blocker − x",
        "Blocker ‐ x",
        "Blocker ‑ x",
        "Blocker ― x",
        "Blocker: none, Blocker: x",
        "## Blocker\nx",
        "## Blocker\nkeiner\n\nReview pending.",
        "## Blocker\n\nnone\n\n- also x\n\n## Tests\ny",
    ];
    for body in failures {
        let mut f = good();
        f.body = body.into();
        assert_eq!(evaluate(&f)[4].status, Status::Fail, "{body}");
    }
    let passing = [
        "a blocker for CI was removed",
        "Blocker-free text",
        "Unblocker: x",
        "The blocker list: none of them",
        "Known blocker\nreview pending",
        "| Blocker | none |",
        "Blocker — none",
        "Blocker - n/a",
        "Blocker (Draft): keiner",
        "## Blocker\nkeiner\n\n## Tests\nReview pending.",
        "## Blocker\n- none\n- keiner\n\n## Next\nx",
        "Blocker:\nnone\n\nPurpose text follows the section.",
        "## Blocker\n1. none\n2) keine",
    ];
    for body in passing {
        let mut f = good();
        f.body = body.into();
        assert_eq!(evaluate(&f)[4].status, Status::Pass, "{body}");
    }
}

#[path = "common/mod.rs"]
mod common;
#[test]
fn cli_ready_check_reads_only_expected_gets() {
    use common::FakeRoutes;
    let api = FakeRoutes::start(|path, _query| {
        let sha = "a".repeat(40);
        let body = if path == "/repos/o/r/pulls/7" {
            serde_json::json!({"state":"open","draft":true,"body":"Blocker: none","head":{"sha":sha},"base":{"ref":"main"},"mergeable":true,"rebaseable":true,"mergeable_state":"clean"})
        } else if path == "/repos/o/r" {
            serde_json::json!({"default_branch":"main"})
        } else if path.ends_with("/check-runs") {
            serde_json::json!({"check_runs":[{"id":1,"name":"ci","status":"completed","conclusion":"success"}]})
        } else if path.ends_with("/statuses") {
            serde_json::json!([])
        } else if path.ends_with("/pulls/7/reviews") {
            serde_json::json!([{"id":1,"user":{"login":"alice","type":"User"},"author_association":"OWNER","state":"COMMENTED","commit_id":sha,"submitted_at":"2026-01-01T00:00:00Z","body":format!("Review: CLEAR head={} patch-id={} pr=7",sha,"b".repeat(40))}])
        } else if path.ends_with("/issues/7/comments") {
            serde_json::json!([])
        } else {
            return (404, vec![], "{}".into());
        };
        (200, vec![], body.to_string())
    });
    let result = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .fixture_env_clear()
        .env("HOME", "/nonexistent")
        .env("GH_TOKEN", "TEST-TOKEN")
        .env("STW_GITHUB_API_URL", &api.url)
        .args(["ready-check", "7", "--repo", "o/r", "--json"])
        .output()
        .unwrap();
    assert_eq!(
        result.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&result.stderr)
    );
    let doc: serde_json::Value = serde_json::from_slice(&result.stdout).unwrap();
    assert_eq!(doc["ready"], true);
    assert_eq!(doc["conditions"].as_array().unwrap().len(), 7);
    assert_eq!(api.hits().len(), 6);
}

#[test]
fn evidence_is_escaped_and_bounded() {
    let mut f = good();
    f.body = format!("Blocker: {}", "x\x1b[31m".repeat(400));
    let c = evaluate(&f);
    assert!(!c[4].evidence.contains('\x1b'));
    assert!(c.iter().all(|c| c.evidence.len() <= 400));
}

#[test]
fn cli_failure_prints_document_and_skips_head_reads() {
    use common::FakeRoutes;
    let api = FakeRoutes::start(|path, _| {
        if path == "/repos/o/r" {
            (200, vec![], r#"{"default_branch":"main"}"#.into())
        } else {
            (404, vec![], "{}".into())
        }
    });
    let result = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .fixture_env_clear()
        .env("HOME", "/nonexistent")
        .env("GH_TOKEN", "TEST-TOKEN")
        .env("STW_GITHUB_API_URL", &api.url)
        .args(["ready-check", "7", "--repo", "o/r", "--json"])
        .output()
        .unwrap();
    assert_eq!(result.status.code(), Some(1));
    let doc: serde_json::Value = serde_json::from_slice(&result.stdout).unwrap();
    assert_eq!(doc["ready"], false);
    assert_eq!(doc["conditions"][0]["status"], "unknown");
    assert_eq!(api.hits().len(), 2);
}

#[test]
fn cli_human_lists_all_conditions_on_failed_read() {
    use common::FakeRoutes;
    let api = FakeRoutes::start(|_, _| (404, vec![], "{}".into()));
    let result = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .fixture_env_clear()
        .env("HOME", "/nonexistent")
        .env("GH_TOKEN", "TEST-TOKEN")
        .env("STW_GITHUB_API_URL", &api.url)
        .args(["ready-check", "7", "--repo", "o/r", "--human"])
        .output()
        .unwrap();
    assert_eq!(result.status.code(), Some(1));
    let text = String::from_utf8(result.stdout).unwrap();
    for id in [
        "open",
        "review",
        "checks",
        "base",
        "blocker",
        "conflict",
        "rebaseable",
        "Ready: no",
    ] {
        assert!(text.contains(id), "{id}: {text}");
    }
}

#[test]
fn dirty_without_mergeable_false_and_no_review_are_not_ready() {
    let mut f = good();
    f.mergeable_state = "dirty".into();
    assert_eq!(evaluate(&f)[5].status, Status::Fail);
    assert!(!ready(&evaluate(&f)));
    let mut f = good();
    f.review = "no-review".into();
    assert_eq!(evaluate(&f)[1].status, Status::Fail);
    assert!(!ready(&evaluate(&f)));
}

fn cli_with_checks(runs: serde_json::Value, statuses: Vec<serde_json::Value>) -> serde_json::Value {
    use common::FakeRoutes;
    let api = FakeRoutes::start(move |path, query| {
        let sha = "a".repeat(40);
        let body = if path == "/repos/o/r/pulls/7" {
            serde_json::json!({"state":"open","draft":true,"body":"Blocker: none","head":{"sha":sha},"base":{"ref":"main"},"mergeable":true,"rebaseable":true,"mergeable_state":"clean"})
        } else if path == "/repos/o/r" {
            serde_json::json!({"default_branch":"main"})
        } else if path.ends_with("/check-runs") {
            if runs.is_object() {
                runs.clone()
            } else {
                serde_json::json!({"check_runs":runs})
            }
        } else if path.ends_with("/statuses") {
            let page = if query.contains("page=2") { 1 } else { 0 };
            statuses
                .get(page)
                .cloned()
                .unwrap_or_else(|| serde_json::json!([]))
        } else if path.ends_with("/pulls/7/reviews") {
            serde_json::json!([])
        } else if path.ends_with("/issues/7/comments") {
            serde_json::json!([{"id":1,"user":{"login":"alice","type":"User"},"author_association":"OWNER","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","body":format!("Review: CLEAR head={} patch-id={} pr=7",sha,"b".repeat(40))}])
        } else {
            return (404, vec![], "{}".into());
        };
        (200, vec![], body.to_string())
    });
    let result = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .fixture_env_clear()
        .env("HOME", "/nonexistent")
        .env("GH_TOKEN", "TEST-TOKEN")
        .env("STW_GITHUB_API_URL", &api.url)
        .args(["ready-check", "7", "--repo", "o/r", "--json"])
        .output()
        .unwrap();
    let doc: serde_json::Value = serde_json::from_slice(&result.stdout).unwrap();
    assert_eq!(
        result.status.code(),
        Some(if doc["ready"] == true { 0 } else { 1 })
    );
    doc
}

#[test]
fn newest_status_per_context_across_pages() {
    let mut page = vec![serde_json::json!({"id":1,"context":"ci","state":"success"})];
    page.extend(
        (2..=100)
            .map(|id| serde_json::json!({"id":id,"context":format!("job{id}"),"state":"success"})),
    );
    let old = serde_json::json!([{"id":101,"context":"ci","state":"failure"}]);
    let doc = cli_with_checks(serde_json::json!([]), vec![page.into(), old]);
    assert_eq!(doc["ready"], true, "{doc}");
    let doc = cli_with_checks(
        serde_json::json!([]),
        vec![
            serde_json::json!([{"id":1,"context":"ci","state":"success"},{"id":2,"context":"other","state":"failure"}]),
        ],
    );
    assert_eq!(doc["conditions"][2]["status"], "fail");
}

#[test]
fn malformed_and_missing_ci_evidence_is_unknown() {
    use serde_json::json;
    for run in [
        json!({"id":1,"status":"completed","conclusion":99}),
        json!({"id":1,"status":99,"conclusion":"success"}),
        json!({"id":1,"name":99,"status":"completed","conclusion":"success"}),
        json!({"id":1,"status":"completed","conclusion":"success","output":{"summary":99}}),
        json!({"id":1,"status":"completed"}),
        json!({"id":1,"conclusion":"success"}),
    ] {
        let doc = cli_with_checks(json!([run]), vec![json!([])]);
        assert_eq!(doc["conditions"][2]["status"], "unknown", "{doc}");
        assert_eq!(doc["ready"], false);
    }
    for status in [
        json!({"id":1,"context":"ci","state":99}),
        json!({"id":1,"context":99,"state":"success"}),
        json!({"id":1,"context":"ci","state":"success","description":99}),
        json!({"id":1,"context":"ci"}),
    ] {
        let doc = cli_with_checks(json!([]), vec![json!([status])]);
        assert_eq!(doc["conditions"][2]["status"], "unknown", "{doc}");
        assert_eq!(doc["ready"], false);
    }
    let doc = cli_with_checks(
        json!([{"id":1,"status":"in_progress","conclusion":null}]),
        vec![json!([])],
    );
    assert_eq!(doc["conditions"][2]["status"], "fail");
}

#[test]
fn malformed_check_run_total_count_is_unknown() {
    let doc = cli_with_checks(
        serde_json::json!({"total_count":"wrong","check_runs":[{"id":1,"status":"completed","conclusion":"success"}]}),
        vec![serde_json::json!([])],
    );
    assert_eq!(doc["conditions"][2]["status"], "unknown");
    assert_eq!(doc["ready"], false);
}

#[test]
fn truncated_status_context_is_unknown() {
    let context = "x".repeat(201);
    let doc = cli_with_checks(
        serde_json::json!([]),
        vec![serde_json::json!([{"id":1,"context":context,"state":"success"}])],
    );
    assert_eq!(doc["conditions"][2]["status"], "unknown");
    assert_eq!(doc["ready"], false);
}

/// `stw status` falls back to the Actions runs when a token cannot read check runs; `ready-check` does not: it decides
/// Ready, so a check class it cannot read is unknown evidence, and it never requests the Actions runs.
#[test]
fn cli_ready_check_stays_strict_a_403_on_check_runs_is_unknown_and_no_actions_run_is_read() {
    use common::FakeRoutes;
    let sha = "a".repeat(40);
    let api = FakeRoutes::start(move |path, _| {
        let body = if path == "/repos/o/r/pulls/7" {
            serde_json::json!({"state":"open","draft":true,"body":"Blocker: none","head":{"sha":sha},"base":{"ref":"main"},"mergeable":true,"rebaseable":true,"mergeable_state":"clean"})
        } else if path == "/repos/o/r" {
            serde_json::json!({"default_branch":"main"})
        } else if path.ends_with("/check-runs") {
            return (403, vec![], "{}".into());
        } else if path.ends_with("/actions/runs") {
            serde_json::json!({"total_count":1,"workflow_runs":[{"id":1,"workflow_id":7,"name":"Test","event":"push","status":"completed","conclusion":"success","head_sha":sha,"created_at":"2026-11-17T17:32:44Z","run_attempt":1}]})
        } else if path.ends_with("/statuses") {
            serde_json::json!([])
        } else if path.ends_with("/pulls/7/reviews") {
            serde_json::json!([])
        } else if path.ends_with("/issues/7/comments") {
            serde_json::json!([{"id":1,"user":{"login":"alice","type":"User"},"author_association":"OWNER","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","body":format!("Review: CLEAR head={} patch-id={} pr=7",sha,"b".repeat(40))}])
        } else {
            return (404, vec![], "{}".into());
        };
        (200, vec![], body.to_string())
    });
    let result = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .fixture_env_clear()
        .env("HOME", "/nonexistent")
        .env("GH_TOKEN", "TEST-TOKEN")
        .env("STW_GITHUB_API_URL", &api.url)
        .args(["ready-check", "7", "--repo", "o/r", "--json"])
        .output()
        .unwrap();
    assert_eq!(result.status.code(), Some(1));
    let doc: serde_json::Value = serde_json::from_slice(&result.stdout).unwrap();
    assert_eq!(doc["ready"], false);
    assert_eq!(doc["conditions"][2]["status"], "unknown");
    assert!(
        doc["errors"]
            .as_array()
            .unwrap()
            .iter()
            .any(|e| e["source"] == "checks"),
        "{doc}"
    );
    assert!(
        api.hits().iter().all(|h| !h.contains("/actions/")),
        "{:?}",
        api.hits()
    );
}
