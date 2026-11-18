//! The pure comparison functions of the tidy oracle (`src/tidy_oracle.rs`): no network, no clone, throw-away fixtures.
use serde_json::{json, Value};
use std::collections::BTreeSet;
use stw::tidy_oracle::*;
use stw::tidy_similarity::{Entry, Keys, Sim};

fn e(path: &str, sign: u8, line: &str) -> Entry {
    Entry {
        path: path.as_bytes().to_vec(),
        sign,
        line: line.as_bytes().to_vec(),
    }
}

fn sim(inter: u64, a: u64, b: u64) -> Sim {
    Sim { inter, a, b }
}

// ---- the diff of git ----

#[test]
fn a_modified_file_gives_its_changed_lines_trimmed_with_the_path() {
    let d = b"diff --git a/src/a.txt b/src/a.txt\nindex 111..222 100644\n--- a/src/a.txt\n+++ b/src/a.txt\n@@ -2 +2,2 @@ ctx\n-  old \t\n+new\r\n+ more\n@@ -9,0 +11 @@\n+tail\n";
    assert_eq!(
        parse_unified_u0(d).unwrap(),
        vec![
            e("src/a.txt", b'-', "old"),
            e("src/a.txt", b'+', "new"),
            e("src/a.txt", b'+', "more"),
            e("src/a.txt", b'+', "tail"),
        ]
    );
}

#[test]
fn an_added_file_takes_the_new_path_and_a_deleted_file_the_old_one() {
    let d = b"diff --git a/n.txt b/n.txt\nnew file mode 100644\nindex 000..111\n--- /dev/null\n+++ b/n.txt\n@@ -0,0 +1 @@\n+one\ndiff --git a/o.txt b/o.txt\ndeleted file mode 100644\nindex 111..000\n--- a/o.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-gone\n";
    assert_eq!(
        parse_unified_u0(d).unwrap(),
        vec![e("n.txt", b'+', "one"), e("o.txt", b'-', "gone")]
    );
}

#[test]
fn a_changed_line_that_looks_like_a_header_is_a_line() {
    // the removed line "-- x" is printed as "--- x", the added line "++ y" as "+++ y"
    let d = b"diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n--- x\n+++ y\n";
    assert_eq!(
        parse_unified_u0(d).unwrap(),
        vec![e("f", b'-', "-- x"), e("f", b'+', "++ y")]
    );
}

#[test]
fn a_binary_file_a_mode_change_and_the_no_newline_marker_give_no_entry() {
    let d = b"diff --git a/b.bin b/b.bin\nindex 1..2 100644\nBinary files a/b.bin and b/b.bin differ\ndiff --git a/x.sh b/x.sh\nold mode 100644\nnew mode 100755\ndiff --git a/t b/t\n--- a/t\n+++ b/t\n@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n\\ No newline at end of file\n";
    assert_eq!(
        parse_unified_u0(d).unwrap(),
        vec![e("t", b'-', "a"), e("t", b'+', "b")]
    );
    assert_eq!(parse_unified_u0(b"").unwrap(), vec![]);
}

#[test]
fn a_path_with_a_space_loses_the_tab_git_adds_and_a_quoted_path_is_an_error() {
    let d =
        b"diff --git a/a b.txt b/a b.txt\n--- a/a b.txt\t\n+++ b/a b.txt\t\n@@ -0,0 +1 @@\n+x\n";
    assert_eq!(parse_unified_u0(d).unwrap(), vec![e("a b.txt", b'+', "x")]);
    let q = b"diff --git \"a/t\\tab\" \"b/t\\tab\"\n--- \"a/t\\tab\"\n+++ \"b/t\\tab\"\n@@ -0,0 +1 @@\n+x\n";
    let err = parse_unified_u0(q).unwrap_err();
    assert!(err.starts_with("a quoted path"), "{err}");
}

#[test]
fn text_keys_leave_out_the_marker_entries_of_gix() {
    let all = vec![
        e("a", b'+', "x"),
        e("a", b'-', "y"),
        e("b", b'B', "bin 1"),
        e("c", b'M', "100644>100755"),
        e("d", b'N', ""),
        e("e", b'X', ""),
    ];
    let text = vec![e("a", b'+', "x"), e("a", b'-', "y")];
    assert_eq!(text_keys(&all), Keys::from_entries(&text));
}

#[test]
fn equal_multisets_do_not_differ_and_a_size_or_content_difference_does() {
    assert!(!differs(&sim(5, 5, 5)));
    assert!(!differs(&sim(0, 0, 0)));
    assert!(differs(&sim(4, 5, 5)));
    assert!(differs(&sim(5, 5, 6)));
    assert!(differs(&sim(5, 6, 5)));
}

// ---- tally and matrix ----

#[test]
fn a_tally_counts_commits_and_differing_ones_and_keeps_the_worst_first() {
    let mut t = Tally::default();
    assert_eq!(t.share_permille(), None);
    assert_eq!(t.worst_permille(), None);
    t.add("aa", sim(5, 5, 5));
    t.add("c2", sim(8, 10, 10));
    t.add("c1", sim(8, 10, 10));
    t.add("bb", sim(3, 10, 10));
    t.add("dd", sim(9, 10, 10));
    assert_eq!((t.total, t.differing), (5, 4));
    let ids: Vec<&str> = t.worst.iter().map(|w| w.0.as_str()).collect();
    // lowest value first; equal values: the smaller commit ID
    assert_eq!(ids, ["bb", "c1", "c2", "dd"]);
    assert_eq!(t.worst_permille(), Some(300));
    // 4 of 5 = 80.0 %
    assert_eq!(t.share_permille(), Some(800));
}

#[test]
fn the_share_is_rounded_half_up_and_the_worst_list_is_cut() {
    let mut t = Tally::default();
    for i in 0..8 {
        t.add(&format!("x{i}"), sim(1, 2, 2));
    }
    for i in 0..1992 {
        t.add(&format!("y{i}"), sim(2, 2, 2));
    }
    // 8 of 2000 = 0.4 %
    assert_eq!(t.share_permille(), Some(4));
    assert_eq!(t.worst.len(), KEEP_WORST);
    let mut u = Tally::default();
    u.add("a", sim(0, 1, 1));
    for _ in 0..199 {
        u.add("z", sim(1, 1, 1));
    }
    // 1 of 200 = 0.5 %
    assert_eq!(u.share_permille(), Some(5));
    let mut h = Tally::default();
    h.add("a", sim(0, 1, 1));
    for _ in 0..2 {
        h.add("z", sim(1, 1, 1));
    }
    // 1 of 3 = 33.33 -> 333
    assert_eq!(h.share_permille(), Some(333));
    let mut r = Tally::default();
    r.add("a", sim(0, 1, 1));
    for _ in 0..1999 {
        r.add("z", sim(1, 1, 1));
    }
    // 1 of 2000 = 0.05 % = 0.5 permille: half up gives 1 permille
    assert_eq!(r.share_permille(), Some(1));
}

#[test]
fn the_matrix_has_one_row_per_setting_in_name_order_and_renders_a_table() {
    let mut m = Matrix::default();
    m.add("git-patience", "1111111111111111", sim(10, 10, 10));
    m.add("git-myers", "aaaaaaaaaaaaaaaaaaaa", sim(9, 10, 10));
    m.add("git-myers", "bbbbbbbbbbbbbbbbbbbb", sim(5, 5, 5));
    assert_eq!(m.get("git-myers").map(|t| t.total), Some(2));
    assert!(m.get("git-minimal").is_none());
    assert_eq!(
        m.render_table(),
        "| setting | commits | differing | share % | worst % |\n|---|---|---|---|---|\n| git-myers | 2 | 1 | 50.0 | 90.0 (aaaaaaaaaaaa) |\n| git-patience | 1 | 0 | 0.0 | - |\n"
    );
}

#[test]
fn counterexamples_name_setting_commit_value_and_sizes() {
    let mut m = Matrix::default();
    m.add("git-histogram", "abcdef0123456789", sim(19, 40, 38));
    m.add("git-myers", "0123456789abcdef", sim(1, 1, 1));
    assert_eq!(
        m.render_counterexamples(),
        "git-histogram abcdef012345 48.7 % (19 of 40 and 38)\n"
    );
}

// ---- the truth of GitHub against the plan ----

fn raw() -> Value {
    json!([
        {"number": 49,
         "pr": {"merged": true, "merge_commit_sha": "m2", "head_sha": "h2", "base": "main"},
         "pr_commits": [{"sha": "p1"}, {"sha": "p2"}],
         "main_commits": [{"sha": "m2"}, {"sha": "m1"}, {"sha": "old"}]},
        {"number": 61},
        {"number": 62, "pr": {"merged": false, "merge_commit_sha": null, "head_sha": "h"},
         "pr_commits": [], "main_commits": []}
    ])
}

#[test]
fn the_truth_keeps_merged_pull_requests_with_the_run_on_the_base() {
    let t = parse_truth(&raw()).unwrap();
    assert_eq!(
        t,
        vec![PrTruth {
            number: 49,
            head_sha: "h2".into(),
            merge_commit_sha: "m2".into(),
            base: "main".into(),
            commits: vec!["p1".into(), "p2".into()],
            main_run: vec!["m2".into(), "m1".into()],
        }]
    );
    assert!(parse_truth(&json!({})).is_err());
    let bad = json!([{"number": 5, "pr": {"merged": true, "head_sha": "h", "base": "main"}, "pr_commits": [], "main_commits": []}]);
    let err = parse_truth(&bad).unwrap_err();
    assert_eq!(err, "#5: no merge_commit_sha");
}

fn plan_json(branch: Value) -> Value {
    json!({"repos": [{"name": "r", "branches": [{"branch": "other"}, branch]}]})
}

fn rebase_branch(strength: &str, targets: &[&str]) -> Value {
    let mapping: Vec<Value> = targets.iter().map(|t| json!({"main": t})).collect();
    json!({"branch": "pr-49", "decision": "review", "proof": "rebase-identity",
           "landed_via": {"kind": "rebase", "basis": "commit-map", "strength": strength},
           "rebase": {"strength": strength, "n": 2, "mapped": targets.len(), "mapping": mapping}})
}

fn truth() -> PrTruth {
    let t = parse_truth(&raw()).unwrap();
    assert_eq!(t.len(), 1, "one merged pull request in the raw data");
    t.into_iter().next().unwrap()
}

#[test]
fn the_view_reads_the_decision_the_landing_and_the_mapped_commits() {
    let p = plan_json(rebase_branch("strong", &["m1", "m2"]));
    assert_eq!(
        branch_view(&p, "pr-49"),
        Some(PlanView {
            decision: "review".into(),
            proof: "rebase-identity".into(),
            kind: "rebase".into(),
            basis: "commit-map".into(),
            strength: "strong".into(),
            rebase_strength: Some("strong".into()),
            n: 2,
            mapped: 2,
            targets: BTreeSet::from(["m1".to_string(), "m2".to_string()]),
        })
    );
    assert!(branch_view(&p, "pr-50").is_none());
    // a branch without the rebase object (null) and a mapping entry without a target
    let q = plan_json(
        json!({"branch": "b", "decision": "keep", "proof": "unmerged", "rebase": null,
               "landed_via": {"kind": "unknown", "basis": "none", "strength": "none"}}),
    );
    let w = branch_view(&q, "b");
    assert_eq!(
        w.map(|w| (w.rebase_strength, w.n, w.mapped)),
        Some((None, 0, 0))
    );
}

#[test]
fn a_strong_rebase_onto_the_commits_github_wrote_is_a_rebase() {
    let p = plan_json(rebase_branch("strong", &["m1", "m2"]));
    let v = branch_view(&p, "pr-49");
    assert_eq!(judge(&truth(), v.as_ref(), "main"), Verdict::Rebase);
}

#[test]
fn a_strong_rebase_onto_other_commits_is_a_finding() {
    let p = plan_json(rebase_branch("strong", &["m1", "zz"]));
    let v = branch_view(&p, "pr-49");
    assert_eq!(
        judge(&truth(), v.as_ref(), "main"),
        Verdict::Miss(
            "landed via rebase (strong), but the mapped commits are not the 2 that GitHub wrote (2 mapped)".into()
        )
    );
}

#[test]
fn a_strong_rebase_that_maps_more_commits_than_github_wrote_is_a_finding() {
    let p = plan_json(rebase_branch("strong", &["m1", "m2", "zz"]));
    let v = branch_view(&p, "pr-49");
    assert_eq!(
        judge(&truth(), v.as_ref(), "main"),
        Verdict::Miss(
            "landed via rebase (strong), but the mapped commits are not the 2 that GitHub wrote (3 mapped)"
                .into()
        )
    );
}

#[test]
fn the_proof_equal_patches_is_accepted_and_anything_else_is_a_finding_with_the_cause() {
    let eq = plan_json(
        json!({"branch": "pr-49", "decision": "delete", "proof": "equal-patches",
        "landed_via": {"kind": "unknown", "basis": "none", "strength": "none"}, "rebase": null}),
    );
    assert_eq!(
        judge(&truth(), branch_view(&eq, "pr-49").as_ref(), "main"),
        Verdict::EqualPatches
    );
    let hint = plan_json(
        json!({"branch": "pr-49", "decision": "keep", "proof": "unmerged",
        "landed_via": {"kind": "unknown", "basis": "none", "strength": "none"},
        "rebase": {"strength": "hint", "n": 3, "mapped": 1, "mapping": [{"main": "m1"}]}}),
    );
    assert_eq!(
        judge(&truth(), branch_view(&hint, "pr-49").as_ref(), "main"),
        Verdict::Miss(
            "proof unmerged, landed via unknown/none/none, rebase hint, mapped 1 of 3".into()
        )
    );
    assert_eq!(
        judge(&truth(), None, "main"),
        Verdict::Miss("the branch is not in the plan".into())
    );
}

#[test]
fn a_rebase_that_is_not_strong_is_not_a_rebase_verdict() {
    let p = plan_json(rebase_branch("hint", &["m1", "m2"]));
    let mut b = p.clone();
    b["repos"][0]["branches"][1]["proof"] = json!("unmerged");
    let v = branch_view(&b, "pr-49");
    assert_eq!(
        judge(&truth(), v.as_ref(), "main"),
        Verdict::Miss(
            "proof unmerged, landed via rebase/commit-map/hint, rebase hint, mapped 2 of 2".into()
        )
    );
}

#[test]
fn a_pull_request_into_another_branch_is_stacked_and_the_plan_reading_is_named() {
    // GitHub wrote the commits into the branch `low`, not into the default branch: they reach it with the pull request of
    // `low`, so the run of the merge is not on the default branch and cannot be compared
    let mut t = truth();
    t.base = "low".into();
    let strong = plan_json(rebase_branch("strong", &["m1", "m2", "m0"]));
    assert_eq!(
        judge(&t, branch_view(&strong, "pr-49").as_ref(), "main"),
        Verdict::Stacked(
            "the base is low, not main; the plan says rebase/commit-map/strong".into()
        )
    );
    let eq = plan_json(
        json!({"branch": "pr-49", "decision": "delete", "proof": "equal-patches",
        "landed_via": {"kind": "unknown", "basis": "none", "strength": "none"}, "rebase": null}),
    );
    assert_eq!(
        judge(&t, branch_view(&eq, "pr-49").as_ref(), "main"),
        Verdict::Stacked("the base is low, not main; the plan says equal-patches".into())
    );
}

#[test]
fn a_stacked_pull_request_the_plan_does_not_recognise_is_a_finding() {
    let mut t = truth();
    t.base = "low".into();
    let hint = plan_json(
        json!({"branch": "pr-49", "decision": "keep", "proof": "unmerged",
        "landed_via": {"kind": "unknown", "basis": "none", "strength": "none"},
        "rebase": {"strength": "hint", "n": 3, "mapped": 1, "mapping": [{"main": "m1"}]}}),
    );
    assert_eq!(
        judge(&t, branch_view(&hint, "pr-49").as_ref(), "main"),
        Verdict::Miss(
            "proof unmerged, landed via unknown/none/none, rebase hint, mapped 1 of 3".into()
        )
    );
}

#[test]
fn the_summary_counts_the_verdicts_and_lists_the_findings() {
    let rows = vec![
        (49, Verdict::Rebase),
        (50, Verdict::EqualPatches),
        (51, Verdict::Miss("why".into())),
        (52, Verdict::Rebase),
        (53, Verdict::Stacked("s".into())),
    ];
    assert_eq!(
        render_verdicts("wstein/x", &rows),
        "wstein/x: 5 merged pull requests, 2 rebase, 1 equal-patches, 1 stacked, 1 findings\n  #51: why\n  #53 stacked: s\n"
    );
    assert_eq!(
        render_verdicts("wstein/y", &[]),
        "wstein/y: 0 merged pull requests, 0 rebase, 0 equal-patches, 0 stacked, 0 findings\n"
    );
}

// ---- the driver script ----

fn driver_script() -> std::path::PathBuf {
    std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/oracle/tidy_oracle.sh")
}

fn in_path(program: &str) -> std::path::PathBuf {
    std::env::split_paths(&std::env::var_os("PATH").unwrap())
        .map(|d| d.join(program))
        .find(|p| p.is_file())
        .unwrap_or_else(|| panic!("{program} is not on the PATH"))
}

/// The script with a PATH that has only `git` and `dirname`: where a guard does not refuse, the script stops at the
/// check for `cargo` and `gh` before it creates, clones or fetches anything.
fn driver(arg: &str) -> (Option<i32>, String) {
    driver_env(arg, &[])
}

fn driver_env(arg: &str, env: &[(&str, &str)]) -> (Option<i32>, String) {
    let bin = tempfile::tempdir().unwrap();
    for p in ["git", "dirname"] {
        std::os::unix::fs::symlink(in_path(p), bin.path().join(p)).unwrap();
    }
    let o = std::process::Command::new(in_path("bash"))
        .arg(driver_script())
        .arg(arg)
        .env("PATH", bin.path())
        .envs(env.iter().copied())
        .output()
        .unwrap();
    (
        o.status.code(),
        String::from_utf8_lossy(&o.stderr).to_string(),
    )
}

#[test]
fn the_driver_script_parses_and_refuses_bad_output_directories() {
    let syntax = std::process::Command::new("bash")
        .arg("-n")
        .arg(driver_script())
        .status()
        .unwrap();
    assert!(syntax.success(), "bash -n");
    let fx = crate::common::Fx::new();
    let inside = fx.path.join("out");
    let inside = inside.to_str().unwrap();
    for (arg, why) in [
        ("", "usage"),
        ("rel/dir", "must be an absolute path"),
        ("/tmp/stw-oracle-x/../y", "must not contain '..'"),
        ("/", "must not be /"),
        (inside, "inside a Git tree that does not ignore it"),
    ] {
        let (code, err) = driver(arg);
        assert_eq!(code, Some(2), "{arg:?}: {err}");
        assert!(err.contains(why), "{arg:?}: {err}");
    }
    assert!(
        !fx.path.join("out").exists(),
        "a refused path is not created"
    );
}

#[test]
fn the_driver_guards_the_raw_data_directory_and_resolves_symlinks() {
    let fx = crate::common::Fx::new();
    let tmp = tempfile::tempdir().unwrap();
    let out = tmp.path().join("out");
    let out = out.to_str().unwrap();
    let raw_in_tree = fx.path.join("raw");
    for (raw, why) in [
        ("", "ORACLE_RAWDIR is empty"),
        ("rel/raw", "ORACLE_RAWDIR must be an absolute path"),
        ("/tmp/x/../raw", "ORACLE_RAWDIR must not contain '..'"),
        (
            raw_in_tree.to_str().unwrap(),
            "ORACLE_RAWDIR is inside a Git tree that does not ignore it",
        ),
    ] {
        let (code, err) = driver_env(out, &[("ORACLE_RAWDIR", raw)]);
        assert_eq!(code, Some(2), "{raw:?}: {err}");
        assert!(err.contains(why), "{raw:?}: {err}");
    }
    // a link to a directory in the work tree: the directory behind it counts
    let link = tmp.path().join("link");
    std::os::unix::fs::symlink(&fx.path, &link).unwrap();
    let (code, err) = driver(link.join("new").to_str().unwrap());
    assert_eq!(code, Some(2), "{err}");
    assert!(
        err.contains("inside a Git tree that does not ignore it"),
        "{err}"
    );
    assert!(!fx.path.join("new").exists());
}

#[test]
fn the_driver_accepts_a_directory_that_the_git_tree_ignores() {
    let fx = crate::common::Fx::new();
    std::fs::write(fx.path.join(".gitignore"), "out/\n").unwrap();
    let dir = fx.path.join("out");
    let (code, err) = driver(dir.to_str().unwrap());
    // past the guards: the script stops at the check for cargo and gh, nothing is created
    assert_eq!(code, Some(2), "{err}");
    assert!(err.contains("needs cargo"), "{err}");
    assert!(!dir.exists());
}

// ---- against the real plan of a fixture ----

mod with_the_plan {
    use super::*;
    use crate::tidy_rebase_plan::{cs, json_of, plan, resolved, world, MERGE_SECS};

    /// A rebase merge with a conflict resolution in a fixture: the real plan JSON is judged against a truth built from
    /// the same commits, so that the names of the JSON fields the view reads are the ones the plan writes.
    #[test]
    fn the_real_plan_of_a_rebase_merge_is_judged_a_rebase() {
        let mut w = world();
        let list = cs(0, 2);
        let ids = w.branch("pr-7", &list);
        let landed = w.land(&list.iter().map(resolved).collect::<Vec<_>>(), MERGE_SECS);
        let p = plan(&w);
        let json = json_of(&p);
        let truth = PrTruth {
            number: 7,
            head_sha: ids[1].to_string(),
            merge_commit_sha: landed[1].to_string(),
            base: "main".into(),
            commits: ids.iter().map(|i| i.to_string()).collect(),
            main_run: landed.iter().rev().map(|i| i.to_string()).collect(),
        };
        let view = branch_view(&json, "pr-7");
        assert_eq!(
            judge(&truth, view.as_ref(), "main"),
            Verdict::Rebase,
            "{view:?}"
        );
        // the same plan against a truth with another run of commits is a finding
        let other = PrTruth {
            main_run: vec![ids[0].to_string()],
            ..truth
        };
        assert!(matches!(
            judge(&other, view.as_ref(), "main"),
            Verdict::Miss(_)
        ));
    }
}
