//! CLI acceptance for `stw resolve` (SourceSteward #40, read-only first cut).
mod common;

use crate::common::FixtureCommand as _;
use std::process::{Command, Stdio};

use common::{path_str, tree_snapshot, Fx, Stamp};
use serde_json::Value;

const CID_A: &str = "0123456789ABCDEFGHJKMNPQR";
const CID_B: &str = "ABCDEFGHJKMNPQRSTVWXYZ012";
/// Pure-hex Change-Id (also a valid Crockford spelling) for the SHA collision case.
const CID_HEX: &str = "DEADBEEFCAFE0123456789ABC";

fn resolve(repo: &str, range: &[&str], query: &str, extra: &[&str]) -> (i32, Value) {
    let mut command = Command::new(env!("CARGO_BIN_EXE_stw"));
    command
        .fixture_env_clear()
        .env("HOME", "/nonexistent")
        .env("PATH", "/nonexistent")
        .arg("resolve")
        .arg("--repo")
        .arg(repo)
        .arg("--range")
        .args(range)
        .arg("--query")
        .arg(query)
        .args(extra)
        .stdin(Stdio::null());
    let output = command.output().unwrap();
    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);
    let doc = serde_json::from_str(&stdout).unwrap_or_else(|error| {
        panic!(
            "{error}: stdout={stdout:?} stderr={stderr:?} code={:?}",
            output.status.code()
        )
    });
    (output.status.code().unwrap(), doc)
}

fn msg(subject: &str, change_id: &str) -> String {
    format!("{subject}\n\nChange-Id: {change_id}\n")
}

#[test]
fn change_id_survives_rewrite_in_range() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    let old = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n"))],
        &msg("old tip", CID_A),
    );
    // Rewrite: same Change-Id on a new commit (simulates rebase that kept the message).
    let neu = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n")), ("b.txt", Some("x\n"))],
        &msg("old tip", CID_A),
    );
    fx.branch("main", base);
    fx.branch("topic", neu);
    let before = tree_snapshot(&fx.path);

    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], CID_A, &[]);
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["confidence"], "exact");
    assert_eq!(doc["method"], "trailer");
    assert_eq!(doc["commit"], neu.to_string());
    assert_eq!(doc["change_id"], CID_A);
    assert_ne!(doc["commit"], old.to_string());
    assert_eq!(tree_snapshot(&fx.path), before);
}

#[test]
fn merge_second_parent_keeps_change_id_in_range() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    let side = fx.commit(
        Some(base),
        &[("side.txt", Some("s\n"))],
        &msg("side change", CID_A),
    );
    let mainline = fx.commit(Some(base), &[("a.txt", Some("2\n"))], "mainline");
    // Merge: first parent mainline, second parent side (Change-Id only on side).
    let merge = fx.commit_with_parents(
        vec![mainline, side],
        Some(mainline),
        &[("merged.txt", Some("m\n"))],
        "Merge side\n",
    );
    fx.branch("main", base);
    fx.branch("merged", merge);
    let before = tree_snapshot(&fx.path);

    let (code, doc) = resolve(&path_str(&fx.path), &["main..merged"], CID_A, &[]);
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["confidence"], "exact");
    assert_eq!(doc["method"], "trailer");
    assert_eq!(doc["commit"], side.to_string());
    assert_eq!(tree_snapshot(&fx.path), before);
}

#[test]
fn squash_without_change_id_is_none() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    let _with_id = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n"))],
        &msg("feature", CID_A),
    );
    // Squash landed with PR title/body only — no Change-Id line.
    let landed = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n"))],
        "PR title\n\nPR description without an id.\n",
    );
    fx.branch("main", base);
    fx.branch("landed", landed);

    let (code, doc) = resolve(&path_str(&fx.path), &["main..landed"], CID_A, &[]);
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["confidence"], "none");
    assert_eq!(doc["reason"], "none");
    assert!(doc["commit"].is_null());
}

#[test]
fn normalize_query_and_trailer() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    // Trailer written with lower case, I/L/O and hyphens; canonical is CID_A with I→1 etc.
    // Use a form that normalises to CID_A: replace 1 with I in a spelling.
    let spelled = "0I23456789ABCDEFGHJKMNPQR"; // I → 1
    let tip = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n"))],
        &format!("subj\n\nChange-Id: {spelled}\n"),
    );
    fx.branch("main", base);
    fx.branch("topic", tip);

    let (code, doc) = resolve(
        &path_str(&fx.path),
        &["main..topic"],
        "0i-23456789abcdefghjkmnpqr",
        &[],
    );
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["change_id"], CID_A);
    assert_eq!(doc["commit"], tip.to_string());
}

#[test]
fn prefix_too_short_and_ambiguous_prefix() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    let a = fx.commit(Some(base), &[("a.txt", Some("2\n"))], &msg("a", CID_A));
    let b = fx.commit(Some(a), &[("a.txt", Some("3\n"))], &msg("b", CID_B));
    fx.branch("main", base);
    fx.branch("topic", b);

    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], "ABCDEFGHJ", &[]);
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["reason"], "prefix-too-short");

    // Shared 10-char prefix of CID_A and a sibling that starts the same — craft two ids.
    let shared = "ZZZZZZZZZZAAAAABBBBBCCCCC";
    let other = "ZZZZZZZZZZDDDDDEEEEEFFFFF";
    let c1 = fx.commit(Some(b), &[("a.txt", Some("4\n"))], &msg("c1", shared));
    let c2 = fx.commit(Some(c1), &[("a.txt", Some("5\n"))], &msg("c2", other));
    fx.branch("topic", c2);

    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], "ZZZZZZZZZZ", &[]);
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["confidence"], "ambiguous");
    assert_eq!(doc["reason"], "ambiguous");
    assert_eq!(doc["candidates"].as_array().unwrap().len(), 2);
}

#[test]
fn sha_in_range_is_exact_same() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    let tip = fx.commit(Some(base), &[("a.txt", Some("2\n"))], &msg("t", CID_A));
    fx.branch("main", base);
    fx.branch("topic", tip);
    let abbrev = &tip.to_string()[..7];

    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], abbrev, &[]);
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["confidence"], "exact");
    assert_eq!(doc["method"], "same");
    assert_eq!(doc["commit"], tip.to_string());
}

#[test]
fn change_id_flag_beats_hex_sha_collision() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    // Commit whose SHA abbreviation we will not use; Change-Id is pure hex.
    let with_id = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n"))],
        &msg("hex id", CID_HEX),
    );
    fx.branch("main", base);
    fx.branch("topic", with_id);

    // Without --change-id, a 12-hex prefix of CID_HEX that is not an object is tried as
    // hash first (miss), then as Change-Id prefix (hit).
    let prefix = &CID_HEX[..12];
    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], prefix, &[]);
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["method"], "trailer");
    assert_eq!(doc["commit"], with_id.to_string());

    // With --change-id, the same prefix is forced to Change-Id mode (no SHA attempt).
    let (code, doc) = resolve(
        &path_str(&fx.path),
        &["main..topic"],
        prefix,
        &["--change-id"],
    );
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["method"], "trailer");
    assert_eq!(doc["change_id"], CID_HEX);

    // Full SHA of with_id interpreted as Change-Id must not pretend to be a trailer match
    // when forced: a 40-hex string is a bad Change-Id (wrong length / not 25).
    let full = with_id.to_string();
    let (code, doc) = resolve(
        &path_str(&fx.path),
        &["main..topic"],
        &full,
        &["--change-id"],
    );
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["reason"], "bad-query");
}

#[test]
fn range_excludes_commits_outside() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    // Put CID_A on an ancestor of the excluded tip (`main`), so dropping the
    // hidden-set check in commits_in_range would still walk it from `topic`.
    let excluded = fx.commit(
        Some(base),
        &[("excl.txt", Some("e\n"))],
        &msg("excluded tip", CID_A),
    );
    let tip = fx.commit(
        Some(excluded),
        &[("in.txt", Some("i\n"))],
        &msg("inside", CID_B),
    );
    fx.branch("main", excluded);
    fx.branch("topic", tip);

    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], CID_A, &[]);
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["confidence"], "none");

    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], CID_B, &[]);
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["commit"], tip.to_string());
}

#[test]
fn old_sha_outside_range_finds_rewritten_commit_by_trailer() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    let old = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n"))],
        &msg("feature", CID_A),
    );
    // Keep `old` reachable from a side ref so the object stays in the odb, but search
    // only the rewritten tip (main..topic does not include `old`).
    let neu = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n")), ("extra.txt", Some("e\n"))],
        &msg("feature", CID_A),
    );
    fx.branch("main", base);
    fx.branch("old", old);
    fx.branch("topic", neu);

    let abbrev = &old.to_string()[..12];
    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], abbrev, &[]);
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["confidence"], "exact");
    assert_eq!(doc["method"], "trailer");
    assert_eq!(doc["commit"], neu.to_string());
    assert_eq!(doc["change_id"], CID_A);
}

#[test]
fn change_id_in_squash_body_is_found() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    // GitHub squash with COMMIT_MESSAGES puts Change-Id lines in the body list.
    let body = format!(
        "Squash subject\n\n* feature one\n\nChange-Id: {CID_A}\n\n* feature two\n\nChange-Id: {CID_B}\n"
    );
    let tip = fx.commit(Some(base), &[("a.txt", Some("2\n"))], &body);
    fx.branch("main", base);
    fx.branch("topic", tip);

    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], CID_A, &[]);
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["commit"], tip.to_string());
    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], CID_B, &[]);
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["commit"], tip.to_string());
}

#[test]
fn old_sha_with_two_ids_finds_surviving_second_id() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    let both = format!("feature\n\nChange-Id: {CID_A}\nChange-Id: {CID_B}\n");
    let old = fx.commit(Some(base), &[("a.txt", Some("2\n"))], &both);
    // Only B survives in the search range (A is absent from the landed message).
    let neu = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n")), ("extra.txt", Some("e\n"))],
        &msg("landed", CID_B),
    );
    fx.branch("main", base);
    fx.branch("old", old);
    fx.branch("topic", neu);

    let abbrev = &old.to_string()[..12];
    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], abbrev, &[]);
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["confidence"], "exact");
    assert_eq!(doc["method"], "trailer");
    assert_eq!(doc["commit"], neu.to_string());
    assert_eq!(doc["change_id"], CID_B);
}

#[test]
fn old_sha_with_two_ids_is_ambiguous_across_commits() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    let both = format!("feature\n\nChange-Id: {CID_A}\nChange-Id: {CID_B}\n");
    let old = fx.commit(Some(base), &[("a.txt", Some("2\n"))], &both);
    let tip_a = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n")), ("a-only.txt", Some("a\n"))],
        &msg("keep A", CID_A),
    );
    let tip_b = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n")), ("b-only.txt", Some("b\n"))],
        &msg("keep B", CID_B),
    );
    // Range includes both tips: `tip_a tip_b ^base`.
    fx.branch("main", base);
    fx.branch("old", old);
    fx.branch("a", tip_a);
    fx.branch("b", tip_b);

    let abbrev = &old.to_string()[..12];
    let (code, doc) = resolve(
        &path_str(&fx.path),
        &["a", "b", &format!("^{}", base)],
        abbrev,
        &[],
    );
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["confidence"], "ambiguous");
    assert_eq!(doc["reason"], "ambiguous");
    assert_eq!(doc["candidates"].as_array().unwrap().len(), 2);
}

#[test]
fn prefix_matching_two_ids_on_one_commit_is_ambiguous() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    // Shared 10-symbol prefix; distinct tails.
    let first = "ZZZZZZZZZZAAAAABBBBBCCCCC";
    let second = "ZZZZZZZZZZDDDDDEEEEEFFFFF";
    let body = format!("squash\n\nChange-Id: {first}\nChange-Id: {second}\n");
    let tip = fx.commit(Some(base), &[("a.txt", Some("2\n"))], &body);
    fx.branch("main", base);
    fx.branch("topic", tip);

    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], "ZZZZZZZZZZ", &[]);
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["confidence"], "ambiguous");
    assert_eq!(doc["reason"], "ambiguous");
    let candidates = doc["candidates"].as_array().unwrap();
    assert_eq!(candidates.len(), 2);
    assert_eq!(candidates[0]["commit"], tip.to_string());
    assert_eq!(candidates[1]["commit"], tip.to_string());
    let ids: Vec<_> = candidates
        .iter()
        .map(|c| c["change_id"].as_str().unwrap())
        .collect();
    assert!(ids.contains(&first));
    assert!(ids.contains(&second));
}

#[test]
fn empty_range_is_usage_error() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    fx.branch("main", base);
    let output = Command::new(env!("CARGO_BIN_EXE_stw"))
        .fixture_env_clear()
        .env("HOME", "/nonexistent")
        .env("PATH", "/nonexistent")
        .args(["resolve", "--repo", &path_str(&fx.path), "--query", CID_A])
        .stdin(Stdio::null())
        .output()
        .unwrap();
    // clap refuses a missing --range before we run.
    assert_ne!(output.status.code(), Some(0));
}

#[test]
fn ambiguous_abbreviation_is_reported() {
    // Known colliding empty-tree commits from the #40 spike (Spike Tester identity).
    let fx = Fx::new();
    let stamp_a = Stamp::new("Spike Tester", "spike@example.invalid", 1_000_020_582);
    let stamp_b = Stamp::new("Spike Tester", "spike@example.invalid", 1_000_039_360);
    let one = fx.commit_at(None, &[], "n20582\n", stamp_a, stamp_a);
    let two = fx.commit_at(None, &[], "n39360\n", stamp_b, stamp_b);
    assert_eq!(one.to_string(), "265315b154219ec467e401031a47081aea52f655");
    assert_eq!(two.to_string(), "265315ba2d5ce528b554e20c4ceb90f204c9944a");
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    fx.branch("main", base);

    let (code, doc) = resolve(&path_str(&fx.path), &["main"], "265315b", &[]);
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["confidence"], "ambiguous");
    assert_eq!(doc["reason"], "ambiguous-abbreviation");
    assert!(doc["commit"].is_null());
    let commits: std::collections::HashSet<_> = doc["candidates"]
        .as_array()
        .unwrap()
        .iter()
        .map(|c| c["commit"].as_str().unwrap())
        .collect();
    assert_eq!(
        commits,
        [one.to_string().as_str(), two.to_string().as_str()]
            .into_iter()
            .collect()
    );
}

#[test]
fn full_40_hex_in_range_is_exact_same() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    let tip = fx.commit(Some(base), &[("a.txt", Some("2\n"))], &msg("t", CID_A));
    fx.branch("main", base);
    fx.branch("topic", tip);

    let (code, doc) = resolve(&path_str(&fx.path), &["main..topic"], &tip.to_string(), &[]);
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["confidence"], "exact");
    assert_eq!(doc["method"], "same");
    assert_eq!(doc["commit"], tip.to_string());
    assert_eq!(doc["change_id"], CID_A);
}

#[test]
fn unknown_7_hex_and_64_hex_are_unknown_object() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    fx.branch("main", base);

    let (code, doc) = resolve(&path_str(&fx.path), &["main"], "0123456", &[]);
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["confidence"], "none");
    assert_eq!(doc["reason"], "unknown-object");

    let (code, doc) = resolve(&path_str(&fx.path), &["main"], &"ab".repeat(32), &[]);
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["confidence"], "none");
    assert_eq!(doc["reason"], "unknown-object");
}

#[test]
fn same_change_id_on_two_commits_is_ambiguous() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    // Two spellings of one id (O/I and hyphens) land on two commits after normalisation.
    let one = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n"))],
        "one\n\nChange-Id: 0G1G0G1G0GABCDEFGHJKMNPQR\n",
    );
    let two = fx.commit(
        Some(one),
        &[("b.txt", Some("b\n"))],
        "two\n\nChange-Id: OGIGOGIGOG-abcdefghjk-mnpqr\n",
    );
    fx.branch("main", base);
    fx.branch("topic", two);

    let (code, doc) = resolve(
        &path_str(&fx.path),
        &["main..topic"],
        "0G1G0G1G0GABCDEFGHJKMNPQR",
        &[],
    );
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["confidence"], "ambiguous");
    assert_eq!(doc["reason"], "ambiguous");
    assert!(doc["commit"].is_null());
    let commits: std::collections::HashSet<_> = doc["candidates"]
        .as_array()
        .unwrap()
        .iter()
        .map(|c| c["commit"].as_str().unwrap())
        .collect();
    assert_eq!(
        commits,
        [one.to_string().as_str(), two.to_string().as_str()]
            .into_iter()
            .collect()
    );
}
