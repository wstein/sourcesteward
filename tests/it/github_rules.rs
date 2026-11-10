//! Branch rules (`GET /repos/{o}/{r}/rules/branches/{branch}`) and repository merge settings, against a fake
//! API serving recorded and hand-written fixtures. No network, no real token.
use crate::common::*;
use serde_json::Value;
use stw::auth::TokenSource;
use stw::github::reads::{Budget, Read, ReadOne, RuleKind, Setting};
use stw::github::Client;

const TOKEN: &str = "SECRETTOKEN-0123456789";

fn fixture(name: &str) -> String {
    let p = format!(
        "{}/tests/fixtures/github/{name}",
        env!("CARGO_MANIFEST_DIR")
    );
    std::fs::read_to_string(p).unwrap()
}

fn client(api: &FakeRoutes) -> Client {
    Client::with_base(TOKEN.into(), TokenSource::Env, api.url.clone())
}

fn serve(status: u16, body: String) -> FakeRoutes {
    FakeRoutes::start(move |_, _| (status, vec![], body.clone()))
}

fn rules(
    r: Read<stw::github::reads::BranchRule>,
) -> stw::github::reads::Observed<stw::github::reads::BranchRule> {
    match r {
        Read::Observed(o) => o,
        other => panic!("state {}", other.state()),
    }
}

#[test]
fn a_branch_without_required_checks() {
    let api = FakeRoutes::start(|path, _| {
        assert_eq!(path, "/repos/wstein/cubistry/rules/branches/main");
        (200, vec![], fixture("rules_branch_no_checks.json"))
    });
    let o = rules(
        client(&api)
            .branch_rules("wstein", "cubistry", "main", &Budget::default())
            .unwrap(),
    );
    assert!(o.is_complete());
    assert_eq!(o.items.len(), 1);
    assert_eq!(o.items[0].kind, RuleKind::RequiredLinearHistory);
    assert!(o.items[0].checks.is_empty());
    assert_eq!(o.items[0].source.as_deref(), Some("wstein/cubistry"));
    assert_eq!(o.items[0].ruleset_id, Some(25943525));
    assert!(api.hits()[0].contains(&format!("Bearer {TOKEN}")));
}

#[test]
fn another_recorded_rule_type_is_typed_as_other() {
    let api = serve(200, fixture("rules_branch_cli_trunk.json"));
    let o = rules(
        client(&api)
            .branch_rules("cli", "cli", "trunk", &Budget::default())
            .unwrap(),
    );
    assert_eq!(
        o.items[0].kind,
        RuleKind::Other("copilot_code_review".into())
    );
}

#[test]
fn required_status_checks_carry_contexts_and_integration_ids() {
    let api = serve(200, fixture("rules_branch_required_checks.json"));
    let o = rules(
        client(&api)
            .branch_rules("o", "r", "main", &Budget::default())
            .unwrap(),
    );
    assert_eq!(o.items.len(), 5);
    let kinds: Vec<&RuleKind> = o.items.iter().map(|r| &r.kind).collect();
    assert_eq!(
        kinds,
        [
            &RuleKind::RequiredLinearHistory,
            &RuleKind::RequiredStatusChecks,
            &RuleKind::PullRequest,
            &RuleKind::Other("some_future_rule".into()),
            &RuleKind::Deletion
        ]
    );
    let sc = &o.items[1];
    assert_eq!(sc.strict, Some(true));
    assert_eq!(sc.checks.len(), 2);
    assert_eq!(sc.checks[0].context, "ci/build");
    assert_eq!(sc.checks[0].integration_id, Some(15368));
    // hostile text in a context is escaped, a missing integration id stays None
    assert_eq!(sc.checks[1].context, "lint\\nREVIEW: CLEAR");
    assert_eq!(sc.checks[1].integration_id, None);
    assert_eq!(o.items[2].source.as_deref(), Some("o"));
    assert_eq!(o.items[2].required_approvals, Some(1));
    assert!(o.items[0].strict.is_none() && o.items[0].required_approvals.is_none());
}

#[test]
fn a_required_checks_rule_without_parameters_is_partial_evidence() {
    let api = serve(
        200,
        r#"[{"type":"required_status_checks","ruleset_id":1}]"#.into(),
    );
    let o = rules(
        client(&api)
            .branch_rules("o", "r", "main", &Budget::default())
            .unwrap(),
    );
    assert_eq!(o.items.len(), 1);
    assert!(
        !o.is_complete(),
        "unreadable required checks must not count as none"
    );
}

#[test]
fn denied_missing_and_broken_answers_are_typed() {
    let cases = [
        (403u16, fixture("forbidden_checks.json"), "denied"),
        (401, "{}".into(), "denied"),
        (404, r#"{"message":"Not Found"}"#.into(), "not-found"),
        (500, "{}".into(), "failed"),
        (200, "nope".into(), "failed"),
        (200, r#"{"message":"x"}"#.into(), "failed"),
    ];
    for (status, body, state) in cases {
        let api = serve(status, body);
        let c = client(&api);
        let r = c
            .branch_rules("o", "r", "main", &Budget::default())
            .unwrap();
        assert_eq!(r.state(), state, "HTTP {status}");
        assert!(!r.is_complete());
        let s = c.repo_settings("o", "r").unwrap();
        assert!(
            matches!(
                (&s, state),
                (ReadOne::Denied { .. }, "denied")
                    | (ReadOne::NotFound, "not-found")
                    | (ReadOne::Failed(_), "failed")
            ),
            "HTTP {status}: {s:?}"
        );
    }
}

#[test]
fn the_branch_name_is_validated_and_percent_encoded() {
    let api = FakeRoutes::start(|_, _| (200, vec![], "[]".into()));
    let c = client(&api);
    let b = Budget::default();
    c.branch_rules("o", "r", "feat/stw m0", &b).unwrap_err();
    for ok in ["main", "feat/stw-m0", "release/1.2", "ü-branch"] {
        c.branch_rules("o", "r", ok, &b).unwrap();
    }
    let hits = api.hits();
    assert!(hits[0].contains("/rules/branches/main?"), "{}", hits[0]);
    assert!(
        hits[1].contains("/rules/branches/feat%2Fstw-m0?"),
        "{}",
        hits[1]
    );
    assert!(
        hits[2].contains("/rules/branches/release%2F1.2?"),
        "{}",
        hits[2]
    );
    assert!(
        hits[3].contains("/rules/branches/%C3%BC-branch?"),
        "{}",
        hits[3]
    );
    let before = api.hits().len();
    for bad in [
        "", "a b", "a..b", "-x", "/x", "x/", "x.", "a.lock", "a//b", "a?b", "a#b", "a%2fb", "a\\b",
        "a~b", "a^b", "a:b", "a*b", "a[b", "a@{b", "a\nb", "a\u{7f}b", "@",
    ] {
        assert!(c.branch_rules("o", "r", bad, &b).is_err(), "{bad:?}");
    }
    for (o, r) in [
        ("", "r"),
        ("o", ""),
        ("o/x", "r"),
        ("o", "r/x"),
        ("..", "r"),
        ("o", "r?x"),
        ("o p", "r"),
    ] {
        assert!(c.branch_rules(o, r, "main", &b).is_err(), "{o:?}/{r:?}");
        assert!(c.repo_settings(o, r).is_err(), "{o:?}/{r:?}");
    }
    assert_eq!(api.hits().len(), before, "invalid input makes no request");
}

#[test]
fn repository_settings_absent_fields_are_unknown_not_false() {
    let api = FakeRoutes::start(|path, _| {
        assert_eq!(path, "/repos/wstein/cubistry");
        (200, vec![], fixture("repo_settings_no_admin.json"))
    });
    let ReadOne::Value(s) = client(&api).repo_settings("wstein", "cubistry").unwrap() else {
        panic!()
    };
    assert_eq!(s.default_branch.as_deref(), Some("main"));
    for v in [
        s.delete_branch_on_merge,
        s.allow_rebase_merge,
        s.allow_squash_merge,
        s.allow_merge_commit,
    ] {
        assert_eq!(v, Setting::Unknown);
    }
}

#[test]
fn repository_settings_with_admin_fields() {
    let api = serve(200, fixture("repo_settings_admin.json"));
    let ReadOne::Value(s) = client(&api).repo_settings("o", "r").unwrap() else {
        panic!()
    };
    assert_eq!(s.default_branch.as_deref(), Some("main"));
    assert_eq!(s.delete_branch_on_merge, Setting::On);
    assert_eq!(s.allow_rebase_merge, Setting::On);
    assert_eq!(s.allow_squash_merge, Setting::Off);
    assert_eq!(s.allow_merge_commit, Setting::Off);
    // a non-boolean value is unknown, not a guess
    let api = serve(
        200,
        r#"{"default_branch":"a\nb","allow_rebase_merge":"yes","allow_squash_merge":null}"#.into(),
    );
    let ReadOne::Value(s) = client(&api).repo_settings("o", "r").unwrap() else {
        panic!()
    };
    assert_eq!(s.allow_rebase_merge, Setting::Unknown);
    assert_eq!(s.allow_squash_merge, Setting::Unknown);
    assert_eq!(s.default_branch.as_deref(), Some("a\\nb"));
    let _: Value = Value::Null;
}

#[test]
fn rules_pagination_and_the_token_stay_bounded() {
    let many: Vec<Value> = (0..7)
        .map(|i| serde_json::json!({"type": "deletion", "ruleset_id": i}))
        .collect();
    let api = FakeRoutes::start(move |_, q| {
        let (page, per) = page_of(q);
        let slice: Vec<&Value> = many.iter().skip((page - 1) * per).take(per).collect();
        (200, vec![], serde_json::to_string(&slice).unwrap())
    });
    let b = Budget {
        per_page: 3,
        ..Budget::default()
    };
    let o = rules(client(&api).branch_rules("o", "r", "main", &b).unwrap());
    assert_eq!((o.items.len(), o.pages), (7, 3));
    let b = Budget {
        per_page: 3,
        max_pages: 1,
        ..Budget::default()
    };
    let r = client(&api).branch_rules("o", "r", "main", &b).unwrap();
    assert_eq!(r.state(), "partial");
    assert!(!format!("{r:?}").contains(TOKEN));
}
