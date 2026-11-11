//! Carry-over check (`stw::carryover`, M4) against the Go tests of `legacy/sourcesteward/internal/carryover`.
//! Plan: docs/migration-plan.md, "Cutover and archive of legacy/sourcesteward", slice 1, step 3.
//!
//! Mapping of the 46 Go tests (45 cases and `TestMain`). R = a case here, G = `tests/it/git_runner_ops.rs`,
//! P = `tests/it/patchid.rs`, C = `tests/it/cli.rs`/`tests/it/carryover_cli.rs`, DROP = no Rust case.
//!
//! args_test.go: `TestParseRequestAcceptsOnlyFullLowercaseObjectIDs` R `request_takes_only_full_lowercase_object_ids`;
//!   `TestParseRequestRejectsBadCounts` R `request_needs_exactly_four_ids`;
//!   `TestParseRequestValidatesPullNumberAndRemote` R `request_validates_the_pull_request_number` (no `--remote`: D2).
//! git_test.go: `TestGuardAllowsOnlyTheReviewedVectors`, `TestGuardRefusesAnExtraArgumentOnEveryShape`,
//!   `TestDiffAndLogPinEveryOutputFlag`: G `range_diff_argv_is_fixed_and_pins_the_patch_comparison` (the argv is fixed per
//!   `Op` in code, there is no guard, D3); `TestRunnerRefusesInvalidIDsBeforeRunningGit`: G `hex40_accepts_only_lower_case_object_ids`;
//!   `TestFetchWritesOnlyThePrivateRef`, `TestFetchWritesNoConfiguredRefmapRef`, `TestNoHookRunsDuringTheFetch`,
//!   `TestUnreachableRemoteIsUnknown`: G `real_git_*pull_head*`; `TestMovedPullHeadRefuses`: R `a_moved_pull_head_refuses`;
//!   `TestPatchIDIgnoresRenameDetection`: R `patch_id_takes_no_rename_detection`; `TestPatchIDSeesBinaryContent`: R
//!   `different_binary_content_changes_the_patch_id`; `TestNoDiffDriverRunsAndAWhitespaceChangeStaysNo`: R
//!   `a_diff_driver_of_the_repository_config_is_refused_and_never_runs` (D4) and R `whitespace_only_change_keeps_the_patch_id_but_range_diff_refuses`;
//!   `TestInheritedGitDirDoesNotRedirectTheRunner`: R `inherited_git_variables_do_not_redirect_the_runner`;
//!   `TestCheckoutOutsideARepositoryIsAReadFailure`: R `a_checkout_outside_a_repository_is_a_read_failure`.
//! hardening_test.go: `TestAttributesThatHideTextDoNotHideACommit`: R `attributes_that_hide_text_do_not_hide_a_commit` (D4);
//!   `TestSubmoduleIgnoreSettingsDoNotHideAGitlinkChange`: R `submodule_ignore_settings_do_not_hide_a_gitlink_change`;
//!   `TestEnvironmentDropsTheRepositorySelectingVariables`, `TestACorruptIndexFileOfTheEnvironmentChangesNothing`:
//!   R `inherited_git_variables_do_not_redirect_the_runner` and G `range_diff_pins_the_attribute_source_and_the_other_ops_do_not`;
//!   `TestParseLogRejectsMalformedOutput`: R `malformed_range_diff_output_is_rejected` (the output is range-diff rows, not `git log`);
//!   `TestRunnerBoundsTheCommitsPerSide`: R `commits_per_side_are_bounded`.
//! hostile_test.go: `TestVerdictsOfTheCleanConfiguration`: R `verdicts_of_the_clean_configuration`;
//!   `TestVerdictsDoNotChangeUnderHostileConfiguration`, `TestOneHostileSettingAtATime`: R `verdicts_do_not_change_under_hostile_configuration`
//!   and R `one_hostile_setting_at_a_time`.
//! check_test.go: R cases named like the Go tests in snake case (`pure_rebase_is_carried_over_with_the_exact_line`, ...);
//!   `TestCommitComparisonIgnoresOnlyBlobIDsAndLineNumbers` and `TestCommitComparisonAddedRemovedAndReordered`: R
//!   `commit_comparison_ignores_only_blob_ids_and_line_numbers`, `commit_comparison_added_removed_and_reordered` (range-diff replaces the Go comparison, D1).
//! render_test.go: R `human_output_ends_with_the_line_only_for_a_yes`, `json_has_the_contract_keys`, `line_format` (schema 2, D5).
//! main_test.go `TestMain`: DROP (Go harness; this harness works in tempdirs it creates and refuses an empty path).
//!
//! Deviations from Go:
//! - D1 range-diff: Go compares the commits itself (message and patch without index lines and hunk headers). Rust runs `git range-diff`
//!   (plan M4, git boundary) and takes `=` at the same position; a reordered commit is `=` for git and a deviation here.
//!   Three more cases where Rust says no and Go says yes: only the author of a commit changed (the metadata block of range-diff names
//!   the author, Go compares message and patch: `only_the_author_changed_is_refused`); an identical patch twice in a range (range-diff pairs it
//!   across and the position rule refuses: `an_identical_patch_twice_in_a_range_is_refused`); a note on the new commit is ignored as in Go
//!   (`--no-notes`, `a_note_on_the_new_commit_does_not_matter`).
//! - D2 `--fetch` fetches `origin` only; Go takes `--remote NAME`. `--pr none` is a local review (line `pr=none`); Go needs a number.
//!   The fetch runs for an ssh, scp-like, `file://` or local-path `origin` only; http, https and git:// are refused typed before git starts
//!   (the https fetch stays with gix); a `core.sshCommand` of the user's config runs as with `FetchOrigin`.
//! - D3 argv: fixed per `Op` in `git_runner`; no guard over free vectors. Environment: allowlist (Go: all `GIT_*` but network ones dropped).
//! - D4 hostile repository config: `diff.external`, `diff.<driver>.textconv|command` and active lines of `info/attributes` make the
//!   range-diff step unsupported (typed, answer no); Go ignores them through flags and keeps the verdict. `.gitattributes`
//!   of the worktree is not read (empty attribute source).
//! - D5 output: schema 2 (`pr` is a number or `"none"`, a `via` per step, `read_errors`), patch-id is the 40-hex of the in-process
//!   implementation (`git patch-id --stable` semantics, see `tests/it/patchid.rs`).
//! - D6 replace refs (`refs/replace/*`) are not ignored in process (Go: `--no-replace-objects`); git ignores them (`GIT_NO_REPLACE_OBJECTS`).
//! - D7 the check runs in the root of the repository or worktree given (the CLI resolves the current directory to it).
//!
//! Harness confinement: every repository is built in a tempdir this file creates (`common::Fx`); git runs with an isolated
//! `HOME` and the runner's scrubbed environment; nothing touches the real repository, no gc, reset or update-ref.
use std::ffi::OsString;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};

use crate::common::*;
use gix::ObjectId;
use stw::carryover::{
    check, line, parse_range_diff, parse_request, CarryOver, Deviation, Git, ReadError, RepoGit,
    Request, Row, State, UsageError, MAX_COMMITS, SCHEMA_VERSION,
};
use stw::git_runner::Runner;
use stw::review::{parse_strict, Line, PrRef, Verdict};

fn git_available() -> bool {
    std::process::Command::new("git")
        .arg("--version")
        .output()
        .is_ok_and(|o| o.status.success())
}

macro_rules! need_git {
    () => {
        if !git_available() {
            eprintln!("git not installed: not run");
            return;
        }
    };
}

fn osv(pairs: &[(&str, &str)]) -> Vec<(OsString, OsString)> {
    pairs
        .iter()
        .map(|(k, v)| (OsString::from(k), OsString::from(v)))
        .collect()
}

fn home() -> tempfile::TempDir {
    tempfile::Builder::new()
        .prefix("stw-carryover-home-")
        .tempdir()
        .unwrap()
}

fn runner(home: &Path) -> Runner {
    Runner::from_env(osv(&[
        ("PATH", std::env::var("PATH").unwrap().as_str()),
        ("HOME", home.to_str().unwrap()),
        ("XDG_CONFIG_HOME", home.join(".config").to_str().unwrap()),
    ]))
}

fn sha(c: char) -> String {
    c.to_string().repeat(40)
}

fn req(pr: u64, ob: ObjectId, oh: ObjectId, nb: ObjectId, nh: ObjectId) -> Request {
    Request {
        pr: PrRef::Number(pr),
        old_base: ob.to_string(),
        old_head: oh.to_string(),
        new_base: nb.to_string(),
        new_head: nh.to_string(),
        fetch: false,
    }
}

fn run(fx: &Fx, r: &Request) -> CarryOver {
    let h = home();
    check(&RepoGit::new(&fx.path, &runner(h.path())), r)
}

const A_FILE: &str = "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n";
/// Four lines changed, so that range-diff pairs a whitespace-edited c1 with the original (`!`).
const C1_FILE: &str = "one\nTWO\nthree\nFOUR\nfive\nSIX\nseven\nEIGHT\n";

/// The usual fixture: main has m1; feature has c1 (a.txt) and c2 (b.txt); main then gets m2.
struct Stack {
    fx: Fx,
    m1: ObjectId,
    c2: ObjectId,
    m2: ObjectId,
}

fn stack() -> Stack {
    let fx = Fx::new();
    let m1 = fx.commit(
        None,
        &[
            ("a.txt", Some(A_FILE)),
            ("b.txt", Some("x = 1\ny = 2\n")),
            ("c.txt", Some("p\nq\n")),
        ],
        "m1",
    );
    let c1 = fx.commit(Some(m1), &[("a.txt", Some(C1_FILE))], "c1");
    let c2 = fx.commit(Some(c1), &[("b.txt", Some("x = 1\ny = 3\n"))], "c2");
    let m2 = fx.commit(Some(m1), &[("c.txt", Some("p\nq\nr\n"))], "m2");
    Stack { fx, m1, c2, m2 }
}

impl Stack {
    /// feature rebased onto main (a pure rebase): c1', c2'.
    fn rebased(&self) -> (ObjectId, ObjectId) {
        let c1 = self
            .fx
            .commit(Some(self.m2), &[("a.txt", Some(C1_FILE))], "c1");
        let c2 = self
            .fx
            .commit(Some(c1), &[("b.txt", Some("x = 1\ny = 3\n"))], "c2");
        (c1, c2)
    }
    fn check(&self, new_base: ObjectId, new_head: ObjectId) -> CarryOver {
        run(&self.fx, &req(12, self.m1, self.c2, new_base, new_head))
    }
}

fn reasons(r: &CarryOver) -> Vec<&'static str> {
    r.reasons.iter().map(|x| x.label()).collect()
}

fn want_reasons(r: &CarryOver, want: &[&str]) {
    assert_eq!(reasons(r), want, "{r:#?}");
    assert_eq!(r.carry_over, want.is_empty(), "{r:#?}");
    assert_eq!(r.line.is_some(), r.carry_over, "{r:#?}");
}

// ---- args_test.go ----

fn args(v: &[&str]) -> Vec<String> {
    v.iter().map(|s| s.to_string()).collect()
}

#[test]
fn request_takes_only_full_lowercase_object_ids() {
    let (a, b, c, d) = (sha('1'), sha('2'), sha('3'), sha('4'));
    let ok = parse_request(&args(&[&a, &b, &c, &d]), Some("12"), false).unwrap();
    assert_eq!(
        (&ok.old_base, &ok.old_head, &ok.new_base, &ok.new_head),
        (&a, &b, &c, &d)
    );
    assert_eq!(ok.pr, PrRef::Number(12));
    let hex39 = "a".repeat(39);
    let hex41 = "a".repeat(41);
    let upper = "A".repeat(40);
    let bad: Vec<(&str, String)> = vec![
        ("ref name", "main".into()),
        ("leading dash", "--output=/tmp/x".into()),
        ("range", format!("{}..{}", "a".repeat(19), "b".repeat(19))),
        ("short", hex39),
        ("long", hex41),
        ("upper case", upper),
        ("space", format!(" {}", "a".repeat(39))),
        ("newline", format!("{}\n", "a".repeat(39))),
        ("empty", String::new()),
        ("not hex", "g".repeat(40)),
    ];
    for pos in 0..4 {
        for (name, value) in &bad {
            let mut v = vec![a.clone(), b.clone(), c.clone(), d.clone()];
            v[pos] = value.clone();
            let e = parse_request(&v, Some("12"), false).unwrap_err();
            assert!(
                e.0.contains(["OLD_BASE", "OLD_HEAD", "NEW_BASE", "NEW_HEAD"][pos]),
                "{name} at {pos}: {e}"
            );
        }
    }
}

#[test]
fn request_needs_exactly_four_ids() {
    let ids = [sha('1'), sha('2'), sha('3'), sha('4'), sha('5')];
    for n in [0usize, 1, 2, 3, 5] {
        let v: Vec<String> = ids.iter().take(n).cloned().collect();
        assert!(parse_request(&v, Some("1"), false).is_err(), "{n} ids");
    }
    let mut five = ids.to_vec();
    five.truncate(5);
    assert!(parse_request(&five, Some("1"), false).is_err());
}

#[test]
fn request_validates_the_pull_request_number() {
    let v = args(&[&sha('1'), &sha('2'), &sha('3'), &sha('4')]);
    for ok in ["1", "12", "999999999", "1000000000", "18446744073709551615"] {
        let r = parse_request(&v, Some(ok), false).unwrap();
        assert_eq!(r.pr, PrRef::Number(ok.parse().unwrap()), "{ok}");
    }
    // DEVIATION D2: `none` is a local review
    assert_eq!(
        parse_request(&v, Some("none"), false).unwrap().pr,
        PrRef::None
    );
    for bad in [
        None,
        Some(""),
        Some("0"),
        Some("07"),
        Some("-1"),
        Some("+1"),
        Some("1 "),
        Some("1x"),
        Some("None"),
        Some("18446744073709551616"),
    ] {
        let e = parse_request(&v, bad, false).unwrap_err();
        assert!(e.0.contains("--pr"), "{bad:?}: {e}");
    }
    // `--fetch` is for a pull request only
    assert!(parse_request(&v, Some("5"), true).unwrap().fetch);
    let e: UsageError = parse_request(&v, Some("none"), true).unwrap_err();
    assert!(e.0.contains("--fetch"), "{e}");
}

// ---- check_test.go: scenarios on real repositories ----

#[test]
fn pure_rebase_is_carried_over_with_the_exact_line() {
    need_git!();
    let s = stack();
    let (_, head) = s.rebased();
    let res = s.check(s.m2, head);
    want_reasons(&res, &[]);
    assert_eq!(res.patch_id.state, State::Equal);
    assert_eq!(res.merge_commits.state, State::None);
    assert_eq!(res.range_diff.state, State::Equal);
    assert_eq!(res.range_diff.total, 2);
    let want = format!(
        "Carried-over: from={} to={} patch-id={} pr=12",
        s.c2, head, res.patch_id.new
    );
    let l = res.line.clone().unwrap();
    assert_eq!(l, want);
    assert_eq!(l.matches('\n').count(), 0);
    assert_eq!(res.patch_id.new.len(), 40);
    // the line is a valid `Carried-over:` line of the M3 grammar
    assert_eq!(
        parse_strict(&l),
        Some(Line::Carried {
            from: s.c2.to_string(),
            to: head.to_string(),
            patch_id: res.patch_id.new.clone(),
            pr: PrRef::Number(12)
        })
    );
}

#[test]
fn identical_heads_are_carried_over() {
    need_git!();
    let s = stack();
    want_reasons(&s.check(s.m1, s.c2), &[]);
}

#[test]
fn merge_commit_refuses_and_only_that_condition() {
    need_git!();
    let s = stack();
    // feature with main merged in: c2 and m2 are the parents, the tree has both changes
    let merge = s.fx.commit_with_parents(
        vec![s.c2, s.m2],
        Some(s.c2),
        &[("c.txt", Some("p\nq\nr\n"))],
        "merge main",
    );
    let res = s.check(s.m2, merge);
    want_reasons(&res, &["merge-commits"]);
    assert_eq!(res.merge_commits.state, State::Found);
    assert_eq!(res.merge_commits.commits, [merge.to_string()]);
    assert_eq!(
        res.patch_id.state,
        State::Equal,
        "the other two conditions hold"
    );
    assert_eq!(res.range_diff.state, State::Equal);
}

#[test]
fn patch_id_change_refuses() {
    need_git!();
    let s = stack();
    let c1 = s.fx.commit(Some(s.m2), &[("a.txt", Some(C1_FILE))], "c1");
    let c2 =
        s.fx.commit(Some(c1), &[("b.txt", Some("x = 1\ny = 4\n"))], "c2");
    let res = s.check(s.m2, c2);
    want_reasons(&res, &["patch-id-differs", "range-diff-not-equal"]);
    assert_eq!(res.patch_id.state, State::Differs);
    assert_ne!(res.patch_id.old, res.patch_id.new);
}

#[test]
fn whitespace_only_change_keeps_the_patch_id_but_range_diff_refuses() {
    need_git!();
    let s = stack();
    let ws = C1_FILE.replacen("TWO", "TWO ", 1);
    let c1 = s.fx.commit(Some(s.m2), &[("a.txt", Some(&ws))], "c1");
    let c2 =
        s.fx.commit(Some(c1), &[("b.txt", Some("x = 1\ny = 3\n"))], "c2");
    let res = s.check(s.m2, c2);
    assert_eq!(
        res.patch_id.state,
        State::Equal,
        "git patch-id --stable ignores whitespace: {:?}",
        res.patch_id
    );
    want_reasons(&res, &["range-diff-not-equal"]);
    assert_eq!(res.range_diff.not_equal, 1);
    assert_eq!(res.range_diff.deviations[0].mark, '!');
}

#[test]
fn added_empty_commit_refuses() {
    need_git!();
    let s = stack();
    let (_, c2) = s.rebased();
    let empty = s.fx.commit(Some(c2), &[], "empty");
    let res = s.check(s.m2, empty);
    want_reasons(&res, &["range-diff-not-equal"]);
    assert_eq!(res.patch_id.state, State::Equal);
    assert_eq!(res.range_diff.not_equal, 1);
    assert_eq!(
        res.range_diff.deviations[0],
        Deviation {
            old: None,
            new: Some(3),
            mark: '>'
        }
    );
}

#[test]
fn reordered_commits_refuse_by_position() {
    need_git!();
    let s = stack();
    let c2 =
        s.fx.commit(Some(s.m2), &[("b.txt", Some("x = 1\ny = 3\n"))], "c2");
    let c1 = s.fx.commit(Some(c2), &[("a.txt", Some(C1_FILE))], "c1");
    let res = s.check(s.m2, c1);
    assert_eq!(
        res.patch_id.state,
        State::Equal,
        "the combined diff is the same"
    );
    want_reasons(&res, &["range-diff-not-equal"]);
    assert_eq!(
        res.range_diff.not_equal, 2,
        "both positions differ: {:?}",
        res.range_diff
    );
    assert_eq!(res.range_diff.deviations[0].mark, '!');
}

#[test]
fn rename_with_content_change_refuses() {
    need_git!();
    let s = stack();
    let c1 = s.fx.commit(Some(s.m2), &[("a.txt", Some(C1_FILE))], "c1");
    let c2 = s.fx.commit(
        Some(c1),
        &[("b.txt", None), ("d.txt", Some("x = 1\ny = 3\nz = 9\n"))],
        "c2",
    );
    want_reasons(
        &s.check(s.m2, c2),
        &["patch-id-differs", "range-diff-not-equal"],
    );
}

#[test]
fn rewritten_parent_is_carried_over_with_the_four_argument_form() {
    need_git!();
    let fx = Fx::new();
    let m1 = fx.commit(
        None,
        &[("a.txt", Some(A_FILE)), ("m.txt", Some("m\n"))],
        "m1",
    );
    let p1 = fx.commit(Some(m1), &[("p.txt", Some("parent\n"))], "p1");
    let k1 = fx.commit(
        Some(p1),
        &[("a.txt", Some(&A_FILE.replacen("two", "TWO", 1)))],
        "k1",
    );
    let k2 = fx.commit(Some(k1), &[("b.txt", Some("child\n"))], "k2");
    // the parent is rebase-merged: main moves on and gets a rewritten p1
    let m2 = fx.commit(Some(m1), &[("m.txt", Some("m\nm2\n"))], "m2");
    let p1_new = fx.commit(Some(m2), &[("p.txt", Some("parent\n"))], "p1");
    assert_ne!(p1, p1_new, "the parent was rewritten");
    let k1n = fx.commit(
        Some(p1_new),
        &[("a.txt", Some(&A_FILE.replacen("two", "TWO", 1)))],
        "k1",
    );
    let k2n = fx.commit(Some(k1n), &[("b.txt", Some("child\n"))], "k2");
    let res = run(&fx, &req(5, p1, k2, p1_new, k2n));
    want_reasons(&res, &[]);
    assert!(res.line.unwrap().ends_with("pr=5"));
}

#[test]
fn missing_object_refuses() {
    need_git!();
    let s = stack();
    let (_, head) = s.rebased();
    let mut r = req(12, s.m1, s.c2, s.m2, head);
    r.new_head = sha('a');
    let res = run(&s.fx, &r);
    want_reasons(&res, &["object-missing"]);
    assert_eq!(res.patch_id.state, State::Unknown);
    // a blob or tree id is not a commit
    let repo = s.fx.repo();
    let tree = repo.find_commit(s.c2).unwrap().tree_id().unwrap();
    r.new_head = tree.to_string();
    want_reasons(&run(&s.fx, &r), &["object-missing"]);
}

/// Every file of `dir` with its size, recursively (the object store and refs included).
fn snapshot(dir: &Path) -> Vec<(PathBuf, u64)> {
    fn walk(d: &Path, out: &mut Vec<(PathBuf, u64)>) {
        for e in std::fs::read_dir(d).unwrap().flatten() {
            let p = e.path();
            if p.is_dir() {
                walk(&p, out);
            } else {
                out.push((p.clone(), e.metadata().unwrap().len()));
            }
        }
    }
    let mut v = Vec::new();
    walk(dir, &mut v);
    v.sort();
    v
}

#[test]
fn check_writes_nothing() {
    need_git!();
    let s = stack();
    let (_, head) = s.rebased();
    s.fx.branch("main", s.m2);
    let before = snapshot(&s.fx.path);
    want_reasons(&s.check(s.m2, head), &[]);
    assert_eq!(snapshot(&s.fx.path), before, "the repository changed");
}

// ---- check_test.go: scripted Git ----

struct Fake {
    has: bool,
    patch: std::collections::HashMap<String, String>,
    merges: Vec<String>,
    rows: Vec<Row>,
    err_patch: bool,
    err_merge: bool,
    err_rows: bool,
    pull_tip: String,
    err_fetch: bool,
}

fn boom(step: &'static str) -> ReadError {
    ReadError {
        step,
        via: "gix",
        kind: "gix".into(),
        unsupported: false,
        message: "boom".into(),
    }
}

impl Git for Fake {
    fn fetch_pull(&self, _pr: u64) -> Result<String, ReadError> {
        if self.err_fetch {
            return Err(boom("fetch-pr-head"));
        }
        Ok(self.pull_tip.clone())
    }
    fn has_commit(&self, _id: &str) -> Result<bool, ReadError> {
        Ok(self.has)
    }
    fn patch_id(&self, _base: &str, head: &str) -> Result<String, ReadError> {
        if self.err_patch {
            return Err(boom("patch-id"));
        }
        Ok(self.patch.get(head).cloned().unwrap_or_default())
    }
    fn merges(&self, _b: &str, _h: &str) -> Result<Vec<String>, ReadError> {
        if self.err_merge {
            return Err(boom("merge-commits"));
        }
        Ok(self.merges.clone())
    }
    fn commit_patch_ids(&self, _b: &str, _h: &str) -> Result<Vec<String>, ReadError> {
        Ok(vec![])
    }
    fn range_rows(&self, _r: &Request) -> Result<Vec<Row>, ReadError> {
        if self.err_rows {
            return Err(boom("range-diff"));
        }
        Ok(self.rows.clone())
    }
}

fn fake_req() -> Request {
    Request {
        pr: PrRef::Number(3),
        old_base: sha('1'),
        old_head: sha('2'),
        new_base: sha('3'),
        new_head: sha('4'),
        fetch: false,
    }
}

fn ok_fake() -> Fake {
    Fake {
        has: true,
        patch: [(sha('2'), sha('9')), (sha('4'), sha('9'))].into(),
        merges: vec![],
        rows: vec![Row {
            old: Some(1),
            mark: '=',
            new: Some(1),
        }],
        err_patch: false,
        err_merge: false,
        err_rows: false,
        pull_tip: String::new(),
        err_fetch: false,
    }
}

#[test]
fn read_failures_never_become_a_yes() {
    assert!(
        check(&ok_fake(), &fake_req()).carry_over,
        "the baseline is a yes"
    );
    type Mutate = fn(&mut Fake);
    let cases: [(&str, Mutate); 3] = [
        ("patch-id", |f| f.err_patch = true),
        ("merges", |f| f.err_merge = true),
        ("range-diff", |f| f.err_rows = true),
    ];
    for (name, mutate) in cases {
        let mut f = ok_fake();
        mutate(&mut f);
        let res = check(&f, &fake_req());
        assert!(!res.carry_over && res.line.is_none(), "{name}: {res:#?}");
        assert_eq!(reasons(&res)[0], "read-failed", "{name}");
        assert_eq!(
            res.read_errors.len(),
            if name == "patch-id" { 2 } else { 1 },
            "{name}"
        );
    }
}

#[test]
fn empty_patch_id_and_empty_range_refuse() {
    let mut f = ok_fake();
    f.patch.clear();
    want_reasons(&check(&f, &fake_req()), &["patch-id-empty"]);
    let mut f = ok_fake();
    f.rows.clear();
    want_reasons(&check(&f, &fake_req()), &["range-empty"]);
}

#[test]
fn pull_head_comparison() {
    let mut r = fake_req();
    r.fetch = true;
    let mut f = ok_fake();
    f.pull_tip = r.new_head.clone();
    let res = check(&f, &r);
    assert!(res.carry_over && res.pr_head == State::Match, "{res:#?}");
    f.pull_tip = sha('5');
    let res = check(&f, &r);
    want_reasons(&res, &["pr-head-mismatch"]);
    assert_eq!(res.pr_head, State::Mismatch);
    f.err_fetch = true;
    let res = check(&f, &r);
    assert!(!res.carry_over && res.pr_head == State::Unknown, "{res:#?}");
    assert_eq!(res.read_errors[0].step, "fetch-pr-head");
    assert_eq!(check(&ok_fake(), &fake_req()).pr_head, State::NotChecked);
}

#[test]
fn commit_comparison_added_removed_and_reordered() {
    let row = |o: Option<u32>, m: char, n: Option<u32>| Row {
        old: o,
        mark: m,
        new: n,
    };
    let mut f = ok_fake();
    // an added commit
    f.rows = vec![row(Some(1), '=', Some(1)), row(None, '>', Some(2))];
    let res = check(&f, &fake_req());
    assert!(!res.carry_over && res.range_diff.total == 2, "{res:#?}");
    assert_eq!(
        res.range_diff.deviations,
        [Deviation {
            old: None,
            new: Some(2),
            mark: '>'
        }]
    );
    // a removed commit
    f.rows = vec![row(Some(1), '=', Some(1)), row(Some(2), '<', None)];
    let d = check(&f, &fake_req()).range_diff.deviations;
    assert_eq!(
        d,
        [Deviation {
            old: Some(2),
            new: None,
            mark: '<'
        }]
    );
    // reordered: git pairs them (`=`) at other positions
    f.rows = vec![row(Some(1), '=', Some(2)), row(Some(2), '=', Some(1))];
    let res = check(&f, &fake_req());
    assert!(!res.carry_over && res.range_diff.not_equal == 2, "{res:#?}");
    assert!(res.range_diff.deviations.iter().all(|d| d.mark == '!'));
    // the listed rows are bounded
    f.rows = (1..=300).map(|n| row(Some(n), '!', Some(n))).collect();
    let res = check(&f, &fake_req());
    assert_eq!(res.range_diff.not_equal, 300);
    assert_eq!(
        res.range_diff.deviations.len(),
        stw::carryover::MAX_DEVIATIONS
    );
    assert!(res.range_diff.truncated);
}

// ---- check_test.go: what range-diff ignores and what it does not ----

#[test]
fn commit_comparison_ignores_only_blob_ids_and_line_numbers() {
    need_git!();
    let s = stack();
    // a hunk that moves (main gained lines far above it) and other blob ids stay equal
    let long = |extra: &str, change: bool| {
        let mut v: Vec<String> = (1..=30).map(|n| format!("line{n}")).collect();
        if change {
            v[19] = "CHANGED".into();
        }
        format!("{extra}{}\n", v.join("\n"))
    };
    let base =
        s.fx.commit(None, &[("long.txt", Some(&long("", false)))], "base");
    let old =
        s.fx.commit(Some(base), &[("long.txt", Some(&long("", true)))], "change");
    let top = s.fx.commit(
        Some(base),
        &[("long.txt", Some(&long("top1\ntop2\ntop3\ntop4\n", false)))],
        "main moves",
    );
    let new = s.fx.commit(
        Some(top),
        &[("long.txt", Some(&long("top1\ntop2\ntop3\ntop4\n", true)))],
        "change",
    );
    let res = run(&s.fx, &req(1, base, old, top, new));
    assert_eq!(res.range_diff.state, State::Equal, "{res:#?}");
    assert_eq!(res.patch_id.state, State::Equal, "{res:#?}");
    // whitespace, message and changed context are different commits
    let ws = C1_FILE.replacen("TWO", "TWO ", 1);
    let tail = |parent: ObjectId| {
        s.fx.commit(Some(parent), &[("b.txt", Some("x = 1\ny = 3\n"))], "c2")
    };
    let c1_ws = s.fx.commit(Some(s.m2), &[("a.txt", Some(&ws))], "c1");
    let c1_msg =
        s.fx.commit(Some(s.m2), &[("a.txt", Some(C1_FILE))], "c1 changed");
    // main changed a line that is context of the c1 hunk
    let m3 = s.fx.commit(
        Some(s.m1),
        &[("a.txt", Some(&A_FILE.replacen("three", "THREE", 1)))],
        "m3",
    );
    let c1_ctx = s.fx.commit(
        Some(m3),
        &[("a.txt", Some(&C1_FILE.replacen("three", "THREE", 1)))],
        "c1",
    );
    let cases: Vec<(&str, ObjectId, ObjectId)> = vec![
        ("whitespace", s.m2, tail(c1_ws)),
        ("message", s.m2, tail(c1_msg)),
        ("changed context", m3, tail(c1_ctx)),
    ];
    for (name, base, head) in cases {
        let res = s.check(base, head);
        assert_eq!(res.range_diff.state, State::Differs, "{name}: {res:#?}");
        assert_eq!(res.range_diff.not_equal, 1, "{name}: {res:#?}");
        let d = res.range_diff.deviations[0];
        assert_eq!((d.mark, d.old, d.new), ('!', Some(1), Some(1)), "{name}");
    }
}

// ---- git_test.go ----

#[test]
fn patch_id_takes_no_rename_detection() {
    need_git!();
    let s = stack();
    // a pure rename: the diff is a deletion and an addition, so the patch-id is not empty and differs from the base
    let renamed = s.fx.commit(
        Some(s.m2),
        &[("b.txt", None), ("d.txt", Some("x = 1\ny = 2\n"))],
        "rename",
    );
    let res = run(&s.fx, &req(1, s.m2, renamed, s.m2, renamed));
    assert_eq!(res.patch_id.state, State::Equal);
    assert_eq!(
        res.patch_id.old.len(),
        40,
        "a rename has a patch-id (no detection): {res:#?}"
    );
    // the same rename with a changed line is another patch-id (git would call it a rename with changes)
    let renamed2 = s.fx.commit(
        Some(s.m2),
        &[("b.txt", None), ("d.txt", Some("x = 1\ny = 2\nz\n"))],
        "rename",
    );
    let r2 = run(&s.fx, &req(1, s.m2, renamed2, s.m2, renamed2));
    assert_ne!(res.patch_id.old, r2.patch_id.old);
}

#[test]
fn different_binary_content_changes_the_patch_id() {
    need_git!();
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("a\n"))], "base");
    let bin = |b: &[u8]| {
        let repo = fx.repo();
        let blob = repo.write_blob(b).unwrap().detach();
        let tree_id = repo.find_commit(base).unwrap().tree_id().unwrap().detach();
        let mut ed = repo.edit_tree(tree_id).unwrap();
        ed.upsert("blob.bin", gix::objs::tree::EntryKind::Blob, blob)
            .unwrap();
        let tree = ed.write().unwrap().detach();
        let sig = gix::actor::Signature {
            name: "T".into(),
            email: "t@example.com".into(),
            time: gix::date::Time::new(1_800_000_000, 0),
        };
        repo.write_object(&gix::objs::Commit {
            tree,
            parents: vec![base].into(),
            author: sig.clone(),
            committer: sig,
            encoding: None,
            message: "bin".into(),
            extra_headers: vec![],
        })
        .unwrap()
        .detach()
    };
    let one = bin(&[0, 1, 2]);
    let two = bin(&[0, 1, 3]);
    let res = run(&fx, &req(1, base, one, base, two));
    assert_eq!(res.patch_id.state, State::Differs, "{res:#?}");
}

#[test]
fn a_moved_pull_head_refuses() {
    need_git!();
    let up = Fx::new();
    let c0 = up.commit(None, &[("a.txt", Some("a\n"))], "c0");
    let c1 = up.commit(Some(c0), &[("a.txt", Some("a\nb\n"))], "c1");
    let c2 = up.commit(Some(c0), &[("a.txt", Some("a\nc\n"))], "c1");
    up.branch("main", c0);
    up.set_ref("refs/pull/7/head", c2);
    let clone = Fx::new();
    clone.set_origin_url(up.path.to_str().unwrap());
    let h = home();
    let rn = runner(h.path());
    let g = RepoGit::new(&clone.path, &rn);
    // the pull request head moved on to c2 after the review of c1
    let mut r = req(7, c0, c1, c0, c1);
    r.fetch = true;
    let res = check(&g, &r);
    want_reasons(&res, &["pr-head-mismatch", "object-missing"]);
    assert_eq!(res.pr_head, State::Mismatch);
    // the head it was asked about: the fetch brings the objects and the answer is a yes
    let mut r = req(7, c0, c2, c0, c2);
    r.fetch = true;
    let res = check(&g, &r);
    assert_eq!(res.pr_head, State::Match, "{res:#?}");
    assert!(res.read_errors.is_empty(), "{res:#?}");
    // an unknown pull request: the fetch fails, the answer is no
    let mut r = req(8, c0, c2, c0, c2);
    r.fetch = true;
    let res = check(&g, &r);
    assert!(!res.carry_over && res.pr_head == State::Unknown, "{res:#?}");
    assert_eq!(res.read_errors[0].via, "git");
}

#[test]
fn inherited_git_variables_do_not_redirect_the_runner() {
    need_git!();
    let s = stack();
    let (_, head) = s.rebased();
    let other = Fx::new();
    other.commit(None, &[("x.txt", Some("x\n"))], "other");
    let h = home();
    // the caller's environment names another repository, an index file, and tracing: none of it reaches git
    let junk = other.path.join(".git");
    let env = osv(&[
        ("PATH", std::env::var("PATH").unwrap().as_str()),
        ("HOME", h.path().to_str().unwrap()),
        ("GIT_DIR", junk.to_str().unwrap()),
        ("GIT_WORK_TREE", "/nonexistent/GIT_WORK_TREE"),
        ("GIT_INDEX_FILE", "/nonexistent/index"),
        ("GIT_COMMON_DIR", "/nonexistent/common"),
        ("GIT_OBJECT_DIRECTORY", "/nonexistent/objects"),
        ("GIT_ALTERNATE_OBJECT_DIRECTORIES", "/nonexistent/alt"),
        ("GIT_NAMESPACE", "hostile"),
        ("GIT_PREFIX", "sub/"),
    ]);
    let r = Runner::from_env(env);
    let res = check(
        &RepoGit::new(&s.fx.path, &r),
        &req(12, s.m1, s.c2, s.m2, head),
    );
    want_reasons(&res, &[]);
}

#[test]
fn a_checkout_outside_a_repository_is_a_read_failure() {
    let t = home();
    let h = home();
    let res = check(&RepoGit::new(t.path(), &runner(h.path())), &fake_req());
    want_reasons(&res, &["read-failed"]);
    assert!(!res.carry_over);
    assert_eq!(res.read_errors[0].step, "objects");
    // an empty or relative path is refused by the runner before git starts
    let res = check(&RepoGit::new(Path::new(""), &runner(h.path())), &fake_req());
    assert!(!res.carry_over);
}

// ---- hardening_test.go, hostile_test.go ----

/// `lines12`: twelve lines with a blank context line 5.
fn lines12(change: &[(usize, &str)]) -> String {
    (1..=12)
        .map(|i| {
            if i == 5 {
                "\n".to_string()
            } else if let Some((_, v)) = change.iter().find(|(n, _)| *n == i) {
                format!("{v}\n")
            } else {
                format!("l{i}\n")
            }
        })
        .collect()
}

struct Hostile {
    fx: Fx,
    cases: Vec<(&'static str, [ObjectId; 4], bool)>,
}

/// One repository with every scenario as a commit pair, as the hostile fixture of Go.
fn hostile_repo() -> Hostile {
    let fx = Fx::new();
    let b0 = fx.commit(
        None,
        &[
            ("a2.txt", Some(&lines12(&[]))),
            ("f.txt", Some("start\n")),
            ("c.txt", Some("p\n")),
            ("sub/keep.txt", Some("k\n")),
        ],
        "base",
    );
    let story = |from: ObjectId, l6: &str, mid: &str| {
        let step = fx.commit(
            Some(from),
            &[
                ("a2.txt", Some(&lines12(&[(6, l6)]))),
                ("f.txt", Some(&format!("{mid}\n"))),
            ],
            "step",
        );
        fx.commit(Some(step), &[("f.txt", Some("final\n"))], "final")
    };
    let old_head = story(b0, "L6", "ok");
    let evil = story(b0, "L6", "EVIL");
    let ws = story(b0, "L6 ", "ok");
    let m1 = fx.commit(Some(b0), &[("c.txt", Some("p\nq\n"))], "m1");
    let pure = {
        let step = fx.commit(
            Some(m1),
            &[
                ("a2.txt", Some(&lines12(&[(6, "L6")]))),
                ("f.txt", Some("ok\n")),
            ],
            "step",
        );
        fx.commit(Some(step), &[("f.txt", Some("final\n"))], "final")
    };
    let m2 = fx.commit(
        Some(b0),
        &[("a2.txt", Some(&lines12(&[(4, "CHANGED4")])))],
        "m2",
    );
    let ctx = {
        let step = fx.commit(
            Some(m2),
            &[
                ("a2.txt", Some(&lines12(&[(4, "CHANGED4"), (6, "L6")]))),
                ("f.txt", Some("ok\n")),
            ],
            "step",
        );
        fx.commit(Some(step), &[("f.txt", Some("final\n"))], "final")
    };
    let id = |c: char| ObjectId::from_hex(sha(c).as_bytes()).unwrap();
    let g2 = fx.commit_gitlink(b0, "gl", id('2'), "bump gl");
    let g3 = fx.commit_gitlink(b0, "gl", id('3'), "bump gl");
    let cases = vec![
        ("pure rebase", [b0, old_head, m1, pure], true),
        (
            "other content in an intermediate commit",
            [b0, old_head, b0, evil],
            false,
        ),
        ("whitespace only", [b0, old_head, b0, ws], false),
        (
            "changed context after a restack",
            [b0, old_head, m2, ctx],
            false,
        ),
        ("gitlink", [b0, g2, b0, g3], false),
    ];
    Hostile { fx, cases }
}

fn verdicts(h: &Hostile, r: &Runner) -> Vec<CarryOver> {
    h.cases
        .iter()
        .map(|(_, [ob, oh, nb, nh], _)| {
            check(&RepoGit::new(&h.fx.path, r), &req(9, *ob, *oh, *nb, *nh))
        })
        .collect()
}

#[test]
fn verdicts_of_the_clean_configuration() {
    need_git!();
    let h = hostile_repo();
    let home = home();
    for ((name, _, yes), res) in h.cases.iter().zip(verdicts(&h, &runner(home.path()))) {
        assert_eq!(res.carry_over, *yes, "{name}: {res:#?}");
        assert!(res.read_errors.is_empty(), "{name}: {res:#?}");
    }
}

fn marker_script(dir: &Path) -> (PathBuf, PathBuf) {
    let log = dir.join("ran");
    let script = dir.join("driver.sh");
    std::fs::write(
        &script,
        format!(
            "#!/bin/sh\necho ran >> '{}'\ncat \"$1\" 2>/dev/null\n",
            log.display()
        ),
    )
    .unwrap();
    std::fs::set_permissions(&script, std::fs::Permissions::from_mode(0o755)).unwrap();
    (script, log)
}

/// Every key of Go's hostile set. The first group runs a program: it is the user's config only (a repository
/// with it is refused, D4). The second is harmful but passive: it may be in the repository too.
type Keys = Vec<(String, String)>;

fn hostile_keys(script: &Path, dir: &Path) -> (Keys, Keys) {
    let s = script.to_str().unwrap().to_string();
    let order = dir.join("order");
    std::fs::write(&order, "sub/\n").unwrap();
    let attrs = dir.join("attrs");
    std::fs::write(&attrs, "* -diff\n*.txt diff=strip\n").unwrap();
    let kv = |k: &str, v: &str| (k.to_string(), v.to_string());
    let running = vec![
        kv("diff.external", &s),
        kv("diff.strip.textconv", &s),
        kv("diff.strip.command", &s),
        kv("core.pager", &s),
        kv("core.fsmonitor", &s),
        kv("pager.log", &s),
        kv("pager.diff", &s),
        kv("gpg.program", &s),
    ];
    let passive = [
        ("diff.relative", "true"),
        ("diff.context", "0"),
        ("diff.algorithm", "patience"),
        ("diff.indentHeuristic", "false"),
        ("diff.interHunkContext", "50"),
        ("diff.mnemonicPrefix", "true"),
        ("diff.noprefix", "true"),
        ("diff.srcPrefix", "X/"),
        ("diff.dstPrefix", "Y/"),
        ("diff.suppressBlankEmpty", "true"),
        ("diff.orderFile", order.to_str().unwrap()),
        ("diff.renames", "copies"),
        ("diff.renameLimit", "1"),
        ("diff.colorMoved", "zebra"),
        ("diff.colorMovedWS", "allow-indentation-change"),
        ("diff.wsErrorHighlight", "all"),
        ("diff.ignoreSubmodules", "all"),
        ("diff.submodule", "diff"),
        ("core.quotePath", "false"),
        ("core.abbrev", "4"),
        ("core.bigFileThreshold", "1"),
        ("core.attributesFile", attrs.to_str().unwrap()),
        ("core.whitespace", "-trailing-space"),
        ("core.autocrlf", "true"),
        ("core.eol", "crlf"),
        ("core.notesRef", "refs/notes/hostile"),
        ("core.precomposeUnicode", "false"),
        ("log.showRoot", "false"),
        ("log.abbrevCommit", "true"),
        ("log.date", "relative"),
        ("log.decorate", "full"),
        ("log.follow", "true"),
        ("log.mailmap", "true"),
        ("log.showSignature", "true"),
        ("log.diffMerges", "separate"),
        ("i18n.logOutputEncoding", "ISO-8859-1"),
        ("i18n.commitEncoding", "ISO-8859-1"),
        ("format.pretty", "oneline"),
        ("format.notes", "true"),
        ("notes.displayRef", "refs/notes/*"),
        ("color.ui", "always"),
        ("color.diff", "always"),
        ("gc.auto", "1"),
        ("fetch.writeCommitGraph", "true"),
    ]
    .into_iter()
    .map(|(k, v)| kv(k, v))
    .collect();
    (running, passive)
}

fn config_text(keys: &[(String, String)]) -> String {
    use std::collections::BTreeMap;
    type Section = (String, Option<String>);
    let mut sections: BTreeMap<Section, Keys> = BTreeMap::new();
    for (k, v) in keys {
        let parts: Vec<&str> = k.split('.').collect();
        let (sec, sub, key) = match parts.as_slice() {
            [s, k] => (s.to_string(), None, k.to_string()),
            [s, sub, k] => (s.to_string(), Some(sub.to_string()), k.to_string()),
            _ => panic!("{k}"),
        };
        sections
            .entry((sec, sub))
            .or_default()
            .push((key, v.clone()));
    }
    let mut out = String::new();
    for ((sec, sub), kvs) in sections {
        match sub {
            Some(sub) => out.push_str(&format!("[{sec} \"{sub}\"]\n")),
            None => out.push_str(&format!("[{sec}]\n")),
        }
        for (k, v) in kvs {
            out.push_str(&format!("\t{k} = {v}\n"));
        }
    }
    out
}

fn append_local_config(repo: &Path, text: &str) {
    use std::io::Write;
    let mut f = std::fs::OpenOptions::new()
        .append(true)
        .open(repo.join(".git/config"))
        .unwrap();
    write!(f, "{text}").unwrap();
}

#[test]
fn verdicts_do_not_change_under_hostile_configuration() {
    need_git!();
    let h = hostile_repo();
    let clean_home = home();
    let clean = verdicts(&h, &runner(clean_home.path()));
    for ((name, _, yes), res) in h.cases.iter().zip(&clean) {
        assert_eq!(
            res.carry_over, *yes,
            "the clean verdict of {name}: {res:#?}"
        );
    }
    // the user's configuration (global, XDG) and the repository's passive keys are hostile; so is the environment
    let hh = home();
    let tools = home();
    let (script, log) = marker_script(tools.path());
    let (running, passive) = hostile_keys(&script, tools.path());
    let all: Vec<_> = running.iter().chain(&passive).cloned().collect();
    std::fs::write(hh.path().join(".gitconfig"), config_text(&all)).unwrap();
    std::fs::create_dir_all(hh.path().join(".config/git")).unwrap();
    std::fs::write(hh.path().join(".config/git/config"), config_text(&all)).unwrap();
    append_local_config(&h.fx.path, &config_text(&passive));
    let trace = tools.path().join("trace");
    let env = osv(&[
        ("PATH", std::env::var("PATH").unwrap().as_str()),
        ("HOME", hh.path().to_str().unwrap()),
        (
            "XDG_CONFIG_HOME",
            hh.path().join(".config").to_str().unwrap(),
        ),
        (
            "GIT_CONFIG_GLOBAL",
            hh.path().join(".gitconfig").to_str().unwrap(),
        ),
        ("GIT_CONFIG_COUNT", "2"),
        ("GIT_CONFIG_KEY_0", "diff.relative"),
        ("GIT_CONFIG_VALUE_0", "true"),
        ("GIT_CONFIG_KEY_1", "diff.context"),
        ("GIT_CONFIG_VALUE_1", "0"),
        (
            "GIT_CONFIG_PARAMETERS",
            "'diff.context=0' 'diff.relative=true'",
        ),
        ("GIT_DIFF_OPTS", "-u0"),
        ("GIT_EXTERNAL_DIFF", script.to_str().unwrap()),
        ("GIT_PAGER", script.to_str().unwrap()),
        ("GIT_NOTES_REF", "refs/notes/hostile"),
        ("GIT_EXEC_PATH", "/nonexistent/exec-path"),
        ("GIT_ATTR_SOURCE", "HEAD~1000"),
        ("GIT_TRACE", trace.to_str().unwrap()),
    ]);
    let hostile = verdicts(&h, &Runner::from_env(env));
    for (((name, _, _), c), x) in h.cases.iter().zip(&clean).zip(&hostile) {
        assert_eq!(
            c, x,
            "{name}: the verdict changed\nclean:   {c:#?}\nhostile: {x:#?}"
        );
    }
    assert!(!log.exists(), "a program of the hostile configuration ran");
    assert!(!trace.exists(), "GIT_TRACE was honored");
}

#[test]
fn one_hostile_setting_at_a_time() {
    need_git!();
    // each setting alone, so that a failure names the key
    let settings: Vec<(&str, &str)> = vec![
        ("diff.relative", "true"),
        ("diff.context", "0"),
        ("diff.interHunkContext", "50"),
        ("diff.algorithm", "patience"),
        ("diff.renames", "copies"),
        ("diff.ignoreSubmodules", "all"),
        ("core.bigFileThreshold", "1"),
        ("log.showSignature", "true"),
        ("log.follow", "true"),
        ("core.quotePath", "false"),
    ];
    for (k, v) in settings {
        let h = hostile_repo();
        append_local_config(&h.fx.path, &config_text(&[(k.to_string(), v.to_string())]));
        let home = home();
        for ((name, _, yes), res) in h.cases.iter().zip(verdicts(&h, &runner(home.path()))) {
            assert_eq!(res.carry_over, *yes, "{k}={v}: {name}: {res:#?}");
        }
    }
}

#[test]
fn attributes_that_hide_text_do_not_hide_a_commit() {
    need_git!();
    // DEVIATION D4: `.gitattributes` of the worktree is not read; active lines of info/attributes make the step unsupported
    let story = |fx: &Fx| {
        let base = fx.commit(None, &[("f.txt", Some("start\n"))], "base");
        let step = fx.commit(Some(base), &[("f.txt", Some("ok\n"))], "step");
        let old = fx.commit(Some(step), &[("f.txt", Some("final\n"))], "final");
        let evil = fx.commit(Some(base), &[("f.txt", Some("EVIL\n"))], "step");
        let new = fx.commit(Some(evil), &[("f.txt", Some("final\n"))], "final");
        (base, old, new)
    };
    // worktree .gitattributes
    let fx = Fx::new();
    let (base, old, new) = story(&fx);
    std::fs::write(fx.path.join(".gitattributes"), "*.txt -diff\n").unwrap();
    let res = run(&fx, &req(1, base, old, base, new));
    assert!(!res.carry_over, "{res:#?}");
    assert_eq!(res.range_diff.state, State::Differs, "{res:#?}");
    assert!(res.range_diff.not_equal > 0);
    // the same without the attribute: the same verdict
    std::fs::remove_file(fx.path.join(".gitattributes")).unwrap();
    assert_eq!(
        run(&fx, &req(1, base, old, base, new)).range_diff.state,
        State::Differs
    );
    // info/attributes: refused, typed, never a yes
    for text in ["*.txt -diff\n", "*.txt binary\n"] {
        let fx = Fx::new();
        let (base, old, new) = story(&fx);
        std::fs::create_dir_all(fx.path.join(".git/info")).unwrap();
        std::fs::write(fx.path.join(".git/info/attributes"), text).unwrap();
        let res = run(&fx, &req(1, base, old, base, new));
        assert!(!res.carry_over && res.line.is_none(), "{text:?}: {res:#?}");
        let e = res
            .read_errors
            .iter()
            .find(|e| e.step == "range-diff")
            .unwrap();
        assert!(e.unsupported && e.kind == "refused-config", "{e:?}");
        assert_eq!(res.range_diff.state, State::Unknown);
    }
}

#[test]
fn a_diff_driver_of_the_repository_config_is_refused_and_never_runs() {
    need_git!();
    // DEVIATION D4: Go keeps the verdict through flags; Rust refuses by name and answers no
    let s = stack();
    let tools = home();
    let (script, log) = marker_script(tools.path());
    append_local_config(
        &s.fx.path,
        &format!(
            "[diff \"strip\"]\n\ttextconv = {0}\n[diff]\n\texternal = {0}\n",
            script.display()
        ),
    );
    std::fs::create_dir_all(s.fx.path.join(".git/info")).unwrap();
    std::fs::write(s.fx.path.join(".git/info/attributes"), "*.txt diff=strip\n").unwrap();
    let ws = C1_FILE.replacen("TWO", "TWO ", 1);
    let c1 = s.fx.commit(Some(s.m2), &[("a.txt", Some(&ws))], "c1");
    let c2 =
        s.fx.commit(Some(c1), &[("b.txt", Some("x = 1\ny = 3\n"))], "c2");
    let res = s.check(s.m2, c2);
    assert!(!res.carry_over && res.line.is_none(), "{res:#?}");
    assert_eq!(
        res.patch_id.state,
        State::Equal,
        "the patch-id ignores whitespace"
    );
    let e = res
        .read_errors
        .iter()
        .find(|e| e.step == "range-diff")
        .unwrap();
    assert!(e.unsupported && e.kind == "refused-config", "{e:?}");
    assert!(!log.exists(), "a diff driver ran");
}

#[test]
fn submodule_ignore_settings_do_not_hide_a_gitlink_change() {
    need_git!();
    let setups: [(&str, Option<&str>); 2] = [
        (".gitmodules ignore=all", None),
        (
            "diff.ignoreSubmodules",
            Some("[diff]\n\tignoreSubmodules = all\n"),
        ),
    ];
    for (name, config) in setups {
        let fx = Fx::new();
        if let Some(c) = config {
            append_local_config(&fx.path, c);
        }
        let mut files: Vec<(&str, Option<&str>)> = vec![("a.txt", Some("a\n"))];
        if config.is_none() {
            files.push((
                ".gitmodules",
                Some("[submodule \"sub\"]\n\tpath = sub\n\turl = https://example.invalid/sub.git\n\tignore = all\n"),
            ));
        }
        let base = fx.commit(None, &files, "base");
        let id = |c: char| ObjectId::from_hex(sha(c).as_bytes()).unwrap();
        let old = fx.commit_gitlink(base, "sub", id('2'), "bump sub");
        let new = fx.commit_gitlink(base, "sub", id('3'), "bump sub");
        let res = run(&fx, &req(1, base, old, base, new));
        assert!(!res.carry_over, "{name}: {res:#?}");
        assert_eq!(
            res.patch_id.state,
            State::Differs,
            "{name}: a different gitlink must differ: {res:#?}"
        );
        assert_eq!(res.range_diff.state, State::Differs, "{name}: {res:#?}");
    }
}

// ---- hardening_test.go: output bounds ----

#[test]
fn malformed_range_diff_output_is_rejected() {
    let good = "1:  abc1234 = 1:  def5678 one\n-:  ------- > 2:  0123456 two\n2:  1111111 < -:  ------- three\n3:  aaaaaaa ! 3:  bbbbbbb four\n";
    let rows = parse_range_diff(good).unwrap();
    assert_eq!(rows.len(), 4);
    assert_eq!(
        rows[1],
        Row {
            old: None,
            mark: '>',
            new: Some(2)
        }
    );
    assert_eq!(parse_range_diff("").unwrap(), []);
    for (name, out) in [
        ("junk before a row", "junk\n1:  abc = 1:  def one\n"),
        ("a patch line", "1:  abc = 1:  def one\n    @@ -1 +1 @@\n"),
        ("no marker", "1:  abc 1:  def one\n"),
        ("unknown marker", "1:  abc ? 1:  def one\n"),
        ("equal without a new position", "1:  abc = -:  --- one\n"),
        ("added with an old position", "1:  abc > 1:  def one\n"),
        ("removed with a new position", "1:  abc < 1:  def one\n"),
        ("position zero", "0:  abc = 0:  def one\n"),
        ("position with a sign", "+1:  abc = 1:  def one\n"),
        ("no colon", "1  abc = 1  def one\n"),
    ] {
        assert!(parse_range_diff(out).is_err(), "{name}: accepted");
    }
}

#[test]
fn commits_per_side_are_bounded() {
    let row = |n: usize| format!("{n}:  abc1234 = {n}:  def5678 c{n}\n");
    let exactly: String = (1..=MAX_COMMITS).map(row).collect();
    assert_eq!(parse_range_diff(&exactly).unwrap().len(), MAX_COMMITS);
    let over: String = (1..=MAX_COMMITS + 1).map(row).collect();
    let e = parse_range_diff(&over).unwrap_err();
    assert!(e.contains("more than 500"), "{e}");
}

// ---- render_test.go ----

fn pure_result() -> (CarryOver, CarryOver) {
    let yes = check(&ok_fake(), &fake_req());
    let mut f = ok_fake();
    f.merges = vec![sha('7')];
    let no = check(&f, &fake_req());
    (yes, no)
}

#[test]
fn human_output_ends_with_the_line_only_for_a_yes() {
    let (yes, no) = pure_result();
    let out = yes.render_human();
    let last = out.trim_end_matches('\n').lines().last().unwrap();
    assert_eq!(
        last,
        format!(
            "Carried-over: from={} to={} patch-id={} pr=3",
            sha('2'),
            sha('4'),
            sha('9')
        )
    );
    assert!(out.contains("carry-over: yes"));
    assert!(out.contains("(via git)"), "the git steps are marked");
    let out = no.render_human();
    assert!(!out.contains("Carried-over:"), "{out}");
    assert!(out.contains("carry-over: no (merge-commits)"), "{out}");
    assert!(out.contains("Delta check instead of a carry-over."));
    // a failed read is shown with its step, its way and "unsupported here" for a missing git
    let mut f = ok_fake();
    f.err_rows = true;
    let out = check(&f, &fake_req()).render_human();
    assert!(
        out.contains("read error    range-diff via gix: boom"),
        "{out}"
    );
}

#[test]
fn json_has_the_contract_keys() {
    let (yes, no) = pure_result();
    let v = yes.to_json();
    assert_eq!(v["schema_version"], SCHEMA_VERSION);
    assert_eq!(SCHEMA_VERSION, 2, "Go: 1 (D5)");
    assert_eq!(v["command"], "review carry-over");
    assert_eq!(v["pr"], 3);
    for k in [
        "from",
        "to",
        "patch_id",
        "merge_commits",
        "range_diff",
        "pr_head",
        "carry_over",
        "reasons",
        "read_errors",
        "line",
    ] {
        assert!(v.get(k).is_some(), "{k}");
    }
    assert_eq!(v["carry_over"], true);
    assert_eq!(v["range_diff"]["via"], "git");
    assert_eq!(v["pr_head"]["via"], "git");
    assert_eq!(v["patch_id"]["via"], "gix");
    assert_eq!(v["pr_head"]["state"], "not-checked");
    assert!(v["line"]
        .as_str()
        .unwrap()
        .starts_with("Carried-over: from="));
    let v = no.to_json();
    assert_eq!(v["carry_over"], false);
    assert!(v["line"].is_null());
    assert_eq!(v["reasons"], serde_json::json!(["merge-commits"]));
    assert_eq!(v["merge_commits"]["count"], 1);
    // a local review
    let mut r = fake_req();
    r.pr = PrRef::None;
    let v = check(&ok_fake(), &r).to_json();
    assert_eq!(v["pr"], "none");
    assert!(v["line"].as_str().unwrap().ends_with("pr=none"));
}

#[test]
fn line_format() {
    let l = line(&sha('a'), &sha('b'), &sha('c'), PrRef::Number(12));
    assert_eq!(
        l,
        format!(
            "Carried-over: from={} to={} patch-id={} pr=12",
            sha('a'),
            sha('b'),
            sha('c')
        )
    );
    let l = line(&sha('a'), &sha('b'), &sha('c'), PrRef::None);
    assert!(l.ends_with("pr=none"));
    // both forms are lines of the M3 grammar
    for pr in [PrRef::Number(7), PrRef::None] {
        let l = line(&sha('a'), &sha('b'), &sha('c'), pr);
        assert!(
            matches!(parse_strict(&l), Some(Line::Carried { pr: p, .. }) if p == pr),
            "{l}"
        );
    }
    // and the same crate reads `Review:` lines with the verdict enum
    assert!(matches!(
        parse_strict(&format!(
            "Review: CLEAR head={} patch-id={} pr=1",
            sha('a'),
            sha('c')
        )),
        Some(Line::Review {
            verdict: Verdict::Clear,
            ..
        })
    ));
}

// ---- conservative differences to Go (answer no where Go says yes), see D1 ----

#[test]
fn a_note_on_the_new_commit_does_not_matter() {
    need_git!();
    // Go reads the log with `--no-notes`; range-diff shows a note as a change unless it is told not to
    let s = stack();
    let (_, head) = s.rebased();
    let note =
        s.fx.commit(None, &[(&head.to_string(), Some("a note\n"))], "notes");
    s.fx.set_ref("refs/notes/commits", note);
    let res = s.check(s.m2, head);
    want_reasons(&res, &[]);
    assert_eq!(res.range_diff.state, State::Equal, "{res:#?}");
}

#[test]
fn only_the_author_changed_is_refused() {
    need_git!();
    // DEVIATION D1: Go compares message and patch; the metadata block of range-diff names the author
    let s = stack();
    let c1 = s.fx.commit_by(
        Some(s.m2),
        &[("a.txt", Some(C1_FILE))],
        "c1",
        ("Other", "other@example.com"),
        ("T", "t@example.com"),
    );
    let c2 =
        s.fx.commit(Some(c1), &[("b.txt", Some("x = 1\ny = 3\n"))], "c2");
    let res = s.check(s.m2, c2);
    assert_eq!(res.patch_id.state, State::Equal, "{res:#?}");
    want_reasons(&res, &["range-diff-not-equal"]);
    assert_eq!(res.range_diff.deviations[0].mark, '!');
}

#[test]
fn an_identical_patch_twice_in_a_range_is_refused() {
    need_git!();
    // DEVIATION D1: Go pairs by position and says yes for a pure rebase; range-diff pairs identical patches
    // across (1 with 3, 3 with 1) and the position rule refuses
    let s = stack();
    let chain = |base: ObjectId| {
        let a = s.fx.commit(Some(base), &[("t.txt", Some("t\n"))], "add t");
        let b = s.fx.commit(Some(a), &[("t.txt", None)], "del t");
        s.fx.commit(Some(b), &[("t.txt", Some("t\n"))], "add t")
    };
    let old = chain(s.m1);
    let new = chain(s.m2);
    let res = run(&s.fx, &req(1, s.m1, old, s.m2, new));
    assert!(!res.carry_over && res.line.is_none(), "{res:#?}");
    assert_eq!(res.patch_id.state, State::Equal, "{res:#?}");
    assert_eq!(res.range_diff.state, State::Differs, "{res:#?}");
}

#[test]
fn a_gitattributes_that_hides_a_change_does_not_make_range_diff_equal() {
    need_git!();
    // Without the empty attribute source `* -diff` turns both patches into "Binary files differ", range-diff
    // pairs the two commits with `=` and the check would see equal commits for different content.
    let fx = Fx::new();
    let base = fx.commit(None, &[("f.txt", Some("a\n"))], "base");
    let old = fx.commit(Some(base), &[("f.txt", Some("b\n"))], "change");
    let new = fx.commit(Some(base), &[("f.txt", Some("c\n"))], "change");
    std::fs::write(fx.path.join(".gitattributes"), "* -diff\n").unwrap();
    let res = run(&fx, &req(1, base, old, base, new));
    assert_eq!(res.range_diff.state, State::Differs, "{res:#?}");
    assert!(!res.carry_over);
}
