//! GitHub REST behavior against a fake API: the token is used but never printed, `--offline` makes no
//! request, and the JSON field names are the documented ones.
use std::process::{Command, Output};

use crate::common::*;
use serde_json::Value;

const TOKEN: &str = "SECRETTOKEN-0123456789";

fn stw(api: &str, args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .env("GH_TOKEN", TOKEN)
        .env("STW_GITHUB_API_URL", api)
        .args(args)
        .output()
        .unwrap()
}

fn root() -> Fx {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.branch("main", c0);
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.track("main");
    fx.set_head("main");
    fx.checkout_head();
    fx
}

fn all_output(o: &Output) -> String {
    format!(
        "{}{}",
        String::from_utf8_lossy(&o.stdout),
        String::from_utf8_lossy(&o.stderr)
    )
}

#[test]
fn the_token_is_sent_but_never_printed_on_any_status_path() {
    let fx = root();
    let r = path_str(&fx.path);
    let cases = [
        (200u16, "[]", ""),
        (401, "{}", "token rejected"),
        (403, "{}", "rate limited"),
        (404, "{}", "no access"),
        (500, "{}", "HTTP 500"),
        (200, "not json at all", "unreadable JSON"),
    ];
    for (status, body, expect) in cases {
        let api = FakeApi::start(status, body);
        for cmd in [
            vec!["status", "--root", &r],
            vec!["status", "--root", &r, "--json"],
            vec!["tidy", "plan", "--root", &r],
            vec!["tidy", "plan", "--root", &r, "--json"],
            vec!["sync", "--plan", "--root", &r, "--json"],
        ] {
            let o = stw(&api.url, &cmd);
            let out = all_output(&o);
            assert!(
                !out.contains(TOKEN),
                "HTTP {status} {cmd:?} printed the token: {out}"
            );
            // sync only reads the remote branch and ignores a failed lookup; the others report it
            if cmd[0] != "sync" {
                assert!(
                    out.contains(expect),
                    "HTTP {status} {cmd:?}: expected {expect:?} in {out}"
                );
            }
        }
        let hits = api.hits();
        assert!(
            !hits.is_empty(),
            "HTTP {status}: the fake API was never asked"
        );
        assert!(
            hits.iter().all(|h| h.contains(&format!("Bearer {TOKEN}"))),
            "the token is sent as a bearer token: {hits:?}"
        );
    }
}

#[test]
fn offline_makes_no_request_and_online_does() {
    let fx = root();
    let r = path_str(&fx.path);
    let api = FakeApi::start(200, "[]");
    for cmd in [
        vec!["status", "--root", &r, "--offline"],
        vec!["tidy", "plan", "--root", &r, "--offline"],
        vec!["sync", "--plan", "--root", &r, "--offline"],
        vec!["sync", "--root", &r, "--offline"],
    ] {
        let o = stw(&api.url, &cmd);
        assert_eq!(o.status.code(), Some(0), "{cmd:?}: {}", all_output(&o));
    }
    assert!(
        api.hits().is_empty(),
        "--offline asked GitHub: {:?}",
        api.hits()
    );
    let o = stw(&api.url, &["status", "--root", &r]);
    assert_eq!(o.status.code(), Some(0));
    assert!(
        !api.hits().is_empty(),
        "control: online status must ask the API"
    );
}

fn keys(v: &Value) -> Vec<String> {
    let mut k: Vec<String> = v.as_object().unwrap().keys().cloned().collect();
    k.sort();
    k
}

fn doc(strs: &[&str]) -> Vec<String> {
    let mut v: Vec<String> = strs.iter().map(|s| s.to_string()).collect();
    v.sort();
    v
}

/// Field names of every JSON object, as documented in README.md ("Output for agents"). Adding or
/// renaming a field fails here until the list and the README are updated together.
#[test]
fn json_field_names_are_the_documented_ones() {
    let fx = root();
    let c0 = fx.tip("main");
    let extra = fx.commit(Some(c0), &[("b.txt", Some("b\n"))], "unmerged");
    fx.branch("topic", extra);
    add_worktree(&fx, "gone", "topic", false, false);
    let r = path_str(&fx.path);
    let pr = r#"[{"number":7,"title":"t","draft":false,"user":{"login":"u"},"head":{"ref":"topic","sha":"abc"},"state":"open","merged_at":null}]"#;
    let api = FakeApi::start(200, pr);

    let st: Value =
        serde_json::from_slice(&stw(&api.url, &["status", "--root", &r, "--json"]).stdout).unwrap();
    assert_eq!(
        keys(&st),
        doc(&[
            "schema_version",
            "command",
            "root",
            "options",
            "engine",
            "github",
            "github_auth",
            "agent_rules",
            "github_note",
            "rejected_paths",
            "repos",
            "summary",
            "totals"
        ])
    );
    assert_eq!(keys(&st["summary"]), doc(&["total", "attention"]));
    let counts = doc(&["pull_requests", "issues", "branches", "worktrees", "stash"]);
    assert_eq!(keys(&st["totals"]), counts);
    assert_eq!(keys(&st["repos"][0]["counts"]), counts);
    let src = |sub: &str| doc(&["state", "open", sub, "truncated"]);
    assert_eq!(keys(&st["totals"]["pull_requests"]), src("bot"));
    assert_eq!(keys(&st["totals"]["issues"]), src("bug"));
    assert_eq!(keys(&st["totals"]["branches"]), doc(&["local", "remote"]));
    let repo = &st["repos"][0];
    assert_eq!(
        keys(repo),
        doc(&[
            "name",
            "path",
            "state",
            "branch",
            "detached",
            "head",
            "upstream",
            "upstream_gone",
            "ahead",
            "behind",
            "changes",
            "stash",
            "operation",
            "gitlink",
            "slug",
            "attention",
            "reasons",
            "counts",
            "fetch",
            "fetch_error",
            "worktree_list",
            "pull_requests",
            "error"
        ])
    );
    assert_eq!(
        keys(&st["options"]),
        doc(&["fetch", "worktrees", "icons", "review"])
    );
    assert_eq!(keys(&repo["fetch"]), doc(&["state", "via", "error"]));
    assert_eq!(
        keys(&repo["changes"]),
        doc(&["staged", "unstaged", "untracked", "conflicts"])
    );
    assert_eq!(keys(&repo["gitlink"]), doc(&["recorded", "state"]));
    assert_eq!(
        keys(&repo["pull_requests"]),
        doc(&["read", "error", "open", "review"])
    );
    assert_eq!(
        keys(&repo["pull_requests"]["open"][0]),
        doc(&[
            "number",
            "title",
            "head",
            "draft",
            "author",
            "review",
            "review_state",
            "hints",
            "base",
            "default_branch",
            "merge_state"
        ])
    );

    let td: Value =
        serde_json::from_slice(&stw(&api.url, &["tidy", "plan", "--root", &r, "--json"]).stdout)
            .unwrap();
    assert_eq!(
        keys(&td),
        doc(&[
            "schema_version",
            "command",
            "read_only",
            "root",
            "engine",
            "github",
            "github_auth",
            "github_note",
            "rejected_paths",
            "repos",
            "summary"
        ])
    );
    assert_eq!(
        keys(&td["summary"]),
        doc(&["delete", "delete_with_worktree", "review", "keep"])
    );
    let repo = &td["repos"][0];
    assert_eq!(
        keys(repo),
        doc(&[
            "name",
            "path",
            "base",
            "base_commit",
            "slug",
            "note",
            "branches",
            "stale_worktree_registrations",
            "error"
        ])
    );
    assert_eq!(
        keys(&repo["branches"][0]),
        doc(&[
            "branch",
            "commit",
            "decision",
            "proof",
            "detail",
            "unique_commits",
            "upstream",
            "upstream_track",
            "worktree"
        ])
    );
    assert_eq!(
        keys(&repo["stale_worktree_registrations"][0]),
        doc(&["path", "locked", "lock_reason"])
    );

    let sy: Value =
        serde_json::from_slice(&stw(&api.url, &["sync", "--plan", "--root", &r, "--json"]).stdout)
            .unwrap();
    assert_eq!(
        keys(&sy),
        doc(&[
            "schema_version",
            "command",
            "mode",
            "root",
            "engine",
            "agent_rules",
            "options",
            "repos",
            "summary"
        ])
    );
    assert_eq!(
        keys(&sy["options"]),
        doc(&["prune", "submodules", "dry_run"])
    );
    assert_eq!(
        keys(&sy["summary"]),
        doc(&[
            "total",
            "fast_forwarded",
            "would_fast_forward",
            "skipped",
            "errors",
            "submodules_moved",
            "would_move_submodules",
            "pruned"
        ])
    );
    assert_eq!(
        keys(&sy["repos"][0]),
        doc(&[
            "name",
            "path",
            "branch",
            "upstream",
            "ahead",
            "behind",
            "dirty_files",
            "fetch",
            "last_fetch_seconds_ago",
            "remote_moved",
            "result",
            "reason",
            "error"
        ])
    );
}

/// Every field name above is documented in the README.
#[test]
fn readme_mentions_every_documented_field() {
    let readme =
        std::fs::read_to_string(concat!(env!("CARGO_MANIFEST_DIR"), "/README.md")).unwrap();
    for f in [
        "schema_version",
        "command",
        "root",
        "engine",
        "github_auth",
        "github_note",
        "read_only",
        "mode",
        "state",
        "detached",
        "upstream_gone",
        "gitlink",
        "attention",
        "reasons",
        "pull_requests",
        "merge_state",
        "review_state",
        "base_commit",
        "unique_commits",
        "upstream_track",
        "stale_worktree_registrations",
        "lock_reason",
        "dirty_files",
        "fetch",
        "last_fetch_seconds_ago",
        "remote_moved",
        "would_fast_forward",
        "fast_forwarded",
    ] {
        assert!(readme.contains(f), "README does not mention `{f}`");
    }
    assert!(
        !readme.contains("next_steps") || readme.contains("no `next_steps`"),
        "README must say that `next_steps` is gone"
    );
}
