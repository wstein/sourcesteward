//! The similarity stage of `stw tidy plan` (evidence class `similar-patches`, decision `review`, never `delete`).
use crate::common::*;
use gix::ObjectId;
use serde_json::Value;
use std::process::{Command, Output};
use stw::github::AnyPr;
use stw::repo::RepoRef;
use stw::tidy::{
    plan_repo_with_opts, unmatched_commits, PatchCache, PlanOpts, PrProof, RepoPlan, Similarity,
};
use stw::tidy_similarity::Class;

const T0: i64 = 1_700_000_000;
const DAY: i64 = 86_400;

/// A file of 30 lines `x<i>`.
fn base_file() -> String {
    (0..30).map(|i| format!("x{i}\n")).collect()
}

/// The file with its first 20 lines replaced by `<tag><i>`; the lines in `odd` read `odd<i>` instead. A change
/// against `base_file` has 40 keys (20 removed, 20 added); with `k` odd lines the pair has `40 - k` keys in common.
fn text(tag: &str, odd: &[usize]) -> String {
    (0..30)
        .map(|i| {
            if odd.contains(&i) {
                format!("odd{i}\n")
            } else if i < 20 {
                format!("{tag}{i}\n")
            } else {
                format!("x{i}\n")
            }
        })
        .collect()
}

fn st(secs: i64) -> Stamp<'static> {
    Stamp::new("T", "t@example.com", secs)
}

/// A commit with author and committer time `secs`; no ref is touched.
fn mkc(
    fx: &Fx,
    parent: Option<ObjectId>,
    edits: &[(&str, Option<&str>)],
    msg: &str,
    secs: i64,
) -> ObjectId {
    fx.commit_at(parent, edits, msg, st(secs), st(secs))
}

struct W {
    fx: Fx,
    c0: ObjectId,
    main: ObjectId,
}

/// `main` is `c0` (thirty days before `T0`) with the files `g0.txt` to `g9.txt`; `origin/main` is `main`.
fn world() -> W {
    let fx = Fx::new();
    let b = base_file();
    let names: Vec<String> = (0..10).map(|i| format!("g{i}.txt")).collect();
    let edits: Vec<(&str, Option<&str>)> = names
        .iter()
        .map(|n| (n.as_str(), Some(b.as_str())))
        .collect();
    let c0 = mkc(&fx, None, &edits, "c0", T0 - 30 * DAY);
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    W { fx, c0, main: c0 }
}

impl W {
    /// A commit on main at `secs`.
    fn land(&mut self, edits: &[(&str, Option<&str>)], msg: &str, secs: i64) -> ObjectId {
        let m = mkc(&self.fx, Some(self.main), edits, msg, secs);
        self.main = m;
        self.fx.remote("main", m);
        self.fx.branch("main", m);
        m
    }
    /// A commit on top of `parent` (default `c0`) at `secs`.
    fn on(
        &self,
        parent: Option<ObjectId>,
        edits: &[(&str, Option<&str>)],
        msg: &str,
        secs: i64,
    ) -> ObjectId {
        mkc(&self.fx, Some(parent.unwrap_or(self.c0)), edits, msg, secs)
    }
}

fn rr(w: &W) -> RepoRef {
    RepoRef {
        name: "fx".into(),
        path: w.fx.path.clone(),
    }
}

fn opts(sim: Option<Similarity>) -> PlanOpts {
    PlanOpts {
        backup_days: 7,
        now: T0,
        similarity: sim,
    }
}

fn plan_with(w: &W, prs: Option<Vec<AnyPr>>, o: &PlanOpts) -> RepoPlan {
    plan_repo_with_opts(
        &rr(w),
        PrProof {
            prs,
            note: "off".into(),
        },
        o,
    )
}

fn plan(w: &W) -> RepoPlan {
    plan_with(w, None, &opts(Some(Similarity::default())))
}

fn get<'a>(p: &'a RepoPlan, branch: &str) -> &'a stw::tidy::BranchPlan {
    p.branches
        .iter()
        .find(|b| b.branch == branch)
        .unwrap_or_else(|| panic!("no branch {branch}"))
}

fn class_of(p: &RepoPlan, branch: &str) -> Option<Class> {
    get(p, branch).similarity.as_ref().map(|s| s.report.class)
}

fn unmatched(w: &W, branch: &str) -> (usize, Vec<ObjectId>) {
    let repo = w.fx.repo();
    let mut cache = PatchCache::default();
    let u = unmatched_commits(&repo, &mut cache, w.main, w.fx.tip(branch)).unwrap();
    (u.with_patch, u.commits)
}

#[test]
fn unmatched_commits_are_the_branch_commits_without_a_patch_partner_on_main() {
    let mut w = world();
    let txt = text("n", &[]);
    let same = [("g0.txt", Some(txt.as_str()))];
    let a = w.on(None, &same, "feat: one", T0 - 2 * DAY);
    let b = w.on(
        Some(a),
        &[("g1.txt", Some("other\n"))],
        "feat: two",
        T0 - 2 * DAY + 60,
    );
    w.fx.branch("half", b);
    w.fx.branch("equal", a);
    w.fx.branch("merged", w.c0);
    w.land(&same, "feat: one", T0 - DAY);
    // `equal` has one commit and it has a partner; `half` has two and the second has none
    assert_eq!(unmatched(&w, "equal"), (1, vec![]));
    assert_eq!(unmatched(&w, "half"), (2, vec![b]));
    // an ancestor of main has no commit at all
    assert_eq!(unmatched(&w, "merged"), (0, vec![]));
}

/// The scenarios of the plan tests, one branch each (the commits of a branch are two days old, those on main one day):
/// `sim` two cherry-picks with a conflict resolution of one line each (97.5 %), `eq` an equal patch, `mod` 90 %,
/// `part` the same subject with other content, `squash` two commits landed as one, `empty` an empty commit.
fn scenarios() -> W {
    let mut w = world();
    let (t_branch, t_main) = (T0 - 2 * DAY, T0 - DAY);
    // sim: g0, g1
    let n = text("n", &[]);
    let n5 = text("n", &[5]);
    let m = text("m", &[]);
    let m7 = text("m", &[7]);
    let s1 = w.on(None, &[("g0.txt", Some(&n))], "feat: one", t_branch);
    let s2 = w.on(
        Some(s1),
        &[("g1.txt", Some(&m))],
        "feat: two",
        t_branch + 60,
    );
    w.fx.branch("sim", s2);
    w.land(&[("g0.txt", Some(&n5))], "feat: one", t_main);
    w.land(&[("g1.txt", Some(&m7))], "feat: two", t_main + 60);
    // eq: g2, exactly the same patch
    let e = text("e", &[]);
    let eq = w.on(None, &[("g2.txt", Some(&e))], "feat: equal", t_branch);
    w.fx.branch("eq", eq);
    w.land(&[("g2.txt", Some(&e))], "feat: equal", t_main + 120);
    // mod: g3, 4 of 20 lines differ: 36 of 40 keys = 90.0 %
    let p = text("p", &[]);
    let p4 = text("p", &[1, 3, 5, 7]);
    let md = w.on(None, &[("g3.txt", Some(&p))], "feat: modified", t_branch);
    w.fx.branch("mod", md);
    w.land(&[("g3.txt", Some(&p4))], "feat: modified", t_main + 180);
    // part: g4, same subject, other content (40 %)
    let q = text("q", &[]);
    let pt = w.on(None, &[("g4.txt", Some(&q))], "feat: part", t_branch);
    w.fx.branch("part", pt);
    let y: String = (0..30).map(|i| format!("y{i}\n")).collect();
    w.land(&[("g4.txt", Some(&y))], "feat: part", t_main + 240);
    // squash: g5 and g6 as two commits on the branch, one commit on main
    let r = text("r", &[]);
    let t = text("t", &[]);
    let q1 = w.on(None, &[("g5.txt", Some(&r))], "feat: first", t_branch);
    let q2 = w.on(
        Some(q1),
        &[("g6.txt", Some(&t))],
        "feat: second",
        t_branch + 60,
    );
    w.fx.branch("squash", q2);
    w.land(
        &[("g5.txt", Some(&r)), ("g6.txt", Some(&t))],
        "feat: both",
        t_main + 300,
    );
    // empty: the tree of c0
    let em = w.on(None, &[], "chore: nothing", t_branch);
    w.fx.branch("empty", em);
    w
}

#[test]
fn a_cherry_pick_with_a_conflict_resolution_is_similar_patches_and_review() {
    let w = scenarios();
    let p = plan(&w);
    assert!(p.error.is_none(), "{:?}", p.error);
    let b = get(&p, "sim");
    assert_eq!(
        (b.decision, b.proof),
        ("review", "similar-patches"),
        "{}",
        b.detail
    );
    assert!(b.detail.contains("97.5"), "{}", b.detail);
    assert!(b.detail.contains("threshold 95"), "{}", b.detail);
    assert!(
        b.worktree.is_none(),
        "no worktree is ever removed on this proof"
    );
    let s = b.similarity.as_ref().expect("similarity");
    assert_eq!(s.report.class, Class::Landed);
    assert_eq!((s.report.plus_commits, s.report.at_or_above), (2, 2));
    assert_eq!((s.high, s.low), (95, 70));
}

#[test]
fn the_existing_proof_has_precedence_over_similarity() {
    let w = scenarios();
    let p = plan(&w);
    let b = get(&p, "eq");
    assert_eq!((b.decision, b.proof), ("delete", "equal-patches"));
    assert!(b.similarity.is_none(), "not computed where a proof exists");
    // an ancestor of main
    w.fx.branch("anc", w.c0);
    let p = plan(&w);
    assert_eq!(get(&p, "anc").proof, "merged-ancestor");
    assert!(get(&p, "anc").similarity.is_none());
}

#[test]
fn modified_partial_squash_and_empty_branches_stay_unmerged_with_their_class() {
    let w = scenarios();
    let p = plan(&w);
    for (br, class) in [
        ("mod", Class::Modified),
        ("part", Class::Partial),
        ("squash", Class::Partial),
        ("empty", Class::None),
    ] {
        let b = get(&p, br);
        assert_eq!((b.decision, b.proof), ("keep", "unmerged"), "{br}");
        assert_eq!(class_of(&p, br), Some(class), "{br}");
    }
    let min = get(&p, "mod")
        .similarity
        .as_ref()
        .and_then(|s| s.report.min.map(|m| m.permille()));
    assert_eq!(min, Some(900));
}

#[test]
fn without_the_stage_the_plan_is_the_old_plan() {
    let w = scenarios();
    let p = plan_with(&w, None, &opts(None));
    let b = get(&p, "sim");
    assert_eq!((b.decision, b.proof), ("keep", "unmerged"));
    assert!(p.branches.iter().all(|b| b.similarity.is_none()));
}

#[test]
fn the_thresholds_move_the_class_at_the_boundary() {
    let w = scenarios();
    let at = |high, low| {
        let p = plan_with(&w, None, &opts(Some(Similarity { high, low })));
        let b = get(&p, "sim");
        (b.proof, class_of(&p, "sim"))
    };
    // the value is 97.5
    assert_eq!(at(97, 70), ("similar-patches", Some(Class::Landed)));
    assert_eq!(at(98, 70), ("unmerged", Some(Class::Modified)));
    assert_eq!(at(98, 98), ("unmerged", Some(Class::Partial)));
    assert_eq!(at(98, 97), ("unmerged", Some(Class::Modified)));
    assert_eq!(at(100, 100), ("unmerged", Some(Class::Partial)));
}

#[test]
fn similar_patches_comes_before_upstream_gone_and_pull_request_moved_and_after_the_gates() {
    let w = scenarios();
    let tip = w.fx.tip("sim");
    for b in ["sim-gone", "sim-moved", "sim-open", "sim-wt", "backup/sim"] {
        w.fx.branch(b, tip);
    }
    w.fx.track("sim-gone");
    add_worktree(&w.fx, "wt-sim", "sim-wt", true, false);
    let pr = |head: &str, n, state| AnyPr {
        head_ref: head.into(),
        head_sha: "0".repeat(40),
        number: n,
        state,
    };
    let prs = vec![pr("sim-moved", 2, "MERGED"), pr("sim-open", 3, "OPEN")];
    let p = plan_with(&w, Some(prs), &opts(Some(Similarity::default())));
    let v = |br: &str| (get(&p, br).decision, get(&p, br).proof);
    assert_eq!(v("sim-gone"), ("review", "similar-patches"));
    assert_eq!(v("sim-moved"), ("review", "similar-patches"));
    // the gates stay where they were
    assert_eq!(v("sim-open"), ("keep", "open-pull-request"));
    assert!(get(&p, "sim-open").similarity.is_none());
    assert_eq!(v("sim-wt"), ("keep", "in-worktree"));
    assert_eq!(class_of(&p, "sim-wt"), Some(Class::Landed));
    assert!(
        get(&p, "sim-wt").worktree.is_none(),
        "a worktree is only a candidate for a delete"
    );
    // a backup branch is never `review`
    assert_eq!(v("backup/sim"), ("keep", "backup-recent"));
    assert!(get(&p, "backup/sim").similarity.is_none());
}

#[test]
fn the_plan_is_the_same_on_every_run() {
    let w = scenarios();
    let a = format!("{:?}", plan(&w).branches);
    let b = format!("{:?}", plan(&w).branches);
    assert_eq!(a, b);
}

#[test]
fn the_index_starts_a_window_before_the_oldest_plus_commit() {
    let mut w = world();
    // main: an old commit (20 days before T0), one 8 days and one 3 days before T0; the branch commit is at T0
    let a = text("a", &[]);
    let b = text("b", &[]);
    let c = text("c", &[]);
    w.land(&[("g0.txt", Some(&a))], "feat: old", T0 - 20 * DAY);
    w.land(&[("g1.txt", Some(&b))], "feat: older", T0 - 8 * DAY);
    w.land(&[("g2.txt", Some(&c))], "feat: newer", T0 - 3 * DAY);
    let x = text("z", &[]);
    let br = w.on(None, &[("g5.txt", Some(&x))], "feat: unrelated", T0);
    w.fx.branch("work", br);
    let p = plan(&w);
    let indexed = get(&p, "work").similarity.as_ref().map(|s| s.indexed);
    // c0 is 30 days old: left out; "old" and "older" are more than seven days before the branch commit: left out
    assert_eq!(
        indexed,
        Some(1),
        "only commits from T0 - 7 days on are indexed"
    );
}

#[test]
fn a_shallow_clone_gives_a_plan_with_the_boundary_skipped() {
    let fx = Fx::new();
    // `c0` is the boundary of a shallow clone of depth 2: its parent `old` is not in the object database
    let old = mkc(
        &fx,
        None,
        &[("g0.txt", Some("old\n"))],
        "old",
        T0 - 40 * DAY,
    );
    let b = base_file();
    let c0 = mkc(&fx, Some(old), &[("g0.txt", Some(&b))], "c0", T0 - 5 * DAY);
    let a = text("a", &[]);
    let a5 = text("a", &[5]);
    let main = mkc(
        &fx,
        Some(c0),
        &[("g0.txt", Some(&a))],
        "feat: landed",
        T0 - DAY,
    );
    let work = mkc(
        &fx,
        Some(c0),
        &[("g0.txt", Some(&a5))],
        "feat: landed",
        T0 - 2 * DAY,
    );
    fx.remote("main", main);
    fx.origin_head("main");
    fx.branch("main", main);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("work", work);
    let git = fx.path.join(".git");
    std::fs::write(git.join("shallow"), format!("{c0}\n")).unwrap();
    let hex = old.to_string();
    std::fs::remove_file(git.join("objects").join(&hex[..2]).join(&hex[2..])).unwrap();
    let w = W { fx, c0, main };
    let p = plan(&w);
    assert!(p.error.is_none(), "{:?}", p.error);
    let b = get(&p, "work");
    assert_eq!((b.decision, b.proof), ("review", "similar-patches"));
    assert_eq!(b.similarity.as_ref().map(|s| s.skipped_shallow), Some(1));
}

#[test]
fn apply_never_deletes_a_similar_patches_branch() {
    let w = scenarios();
    let p = plan(&w);
    assert_eq!(get(&p, "sim").proof, "similar-patches");
    let dir = scratch();
    let r = stw::tidy_apply::apply_repo(
        &rr(&w),
        &p,
        None,
        &stw::tidy_apply::ApplyOpts {
            manifest_dir: dir.path().to_path_buf(),
            run_id: "0000000000001-1-0".into(),
            now: T0,
        },
    );
    assert!(r.error.is_none(), "{:?}", r.error);
    assert!(
        stw::repo::resolve(&w.fx.repo(), "refs/heads/sim").is_some(),
        "the branch stays"
    );
    assert!(
        r.branches.iter().all(|b| b.branch != "sim"),
        "{:?}",
        r.branches
    );
    // `eq` has an equal patch and is deleted
    assert!(stw::repo::resolve(&w.fx.repo(), "refs/heads/eq").is_none());
}

fn stw(home: &std::path::Path, args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("HOME", home)
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .args(args)
        .output()
        .unwrap()
}

fn plan_json(w: &W, extra: &[&str]) -> Value {
    let home = scratch();
    let r = path_str(&w.fx.path);
    let mut args = vec!["tidy", "plan", "--offline", "--json", "--root", r.as_str()];
    args.extend_from_slice(extra);
    let o = stw(home.path(), &args);
    assert_eq!(
        o.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&o.stderr)
    );
    serde_json::from_slice(&o.stdout).unwrap()
}

fn branch_json<'a>(v: &'a Value, name: &str) -> &'a Value {
    v["repos"][0]["branches"]
        .as_array()
        .unwrap()
        .iter()
        .find(|b| b["branch"] == name)
        .unwrap_or_else(|| panic!("no branch {name}"))
}

fn key_list(v: &Value) -> Vec<String> {
    let mut k: Vec<String> = v
        .as_object()
        .map(|m| m.keys().cloned().collect())
        .unwrap_or_default();
    k.sort();
    k
}

fn sorted(names: &[&str]) -> Vec<String> {
    let mut v: Vec<String> = names.iter().map(|s| s.to_string()).collect();
    v.sort();
    v
}

#[test]
fn the_json_of_a_similar_branch_has_the_stable_keys_and_the_figures() {
    let w = scenarios();
    let v = plan_json(&w, &[]);
    assert_eq!(v["schema_version"], 2);
    let b = branch_json(&v, "sim");
    assert_eq!(
        (&b["decision"], &b["proof"]),
        (&"review".into(), &"similar-patches".into())
    );
    let s = &b["similarity"];
    assert_eq!(
        key_list(s),
        sorted(&[
            "class",
            "threshold",
            "threshold_low",
            "window_days",
            "plus_commits",
            "at_or_above",
            "min",
            "median",
            "without_candidate",
            "skipped",
            "bounds",
            "commits"
        ])
    );
    assert_eq!(s["class"], "landed");
    assert_eq!(
        (&s["threshold"], &s["threshold_low"], &s["window_days"]),
        (&95.into(), &70.into(), &7.into())
    );
    assert_eq!(
        (
            &s["plus_commits"],
            &s["at_or_above"],
            &s["without_candidate"]
        ),
        (&2.into(), &2.into(), &0.into())
    );
    assert_eq!((&s["min"], &s["median"]), (&97.5.into(), &97.5.into()));
    assert_eq!(
        key_list(&s["skipped"]),
        sorted(&["merges", "empty", "too_large", "shallow"])
    );
    assert_eq!(
        key_list(&s["bounds"]),
        sorted(&["candidates_cut", "main_commits_indexed"])
    );
    let c = &s["commits"][0];
    assert_eq!(
        key_list(c),
        sorted(&[
            "commit",
            "subject",
            "outcome",
            "skipped",
            "candidate",
            "candidate_subject",
            "reason",
            "similarity",
            "similarity_no_path",
            "evidence"
        ])
    );
    assert_eq!(c["outcome"], "compared");
    assert_eq!(
        (&c["similarity"], &c["similarity_no_path"]),
        (&97.5.into(), &97.5.into())
    );
    assert_eq!(c["reason"], "subject");
    assert_eq!(c["candidate_subject"], c["subject"]);
    assert_eq!(c["candidate"].as_str().map(str::len), Some(40));
    assert_eq!(
        c["evidence"],
        serde_json::json!([]),
        "at or above the high threshold"
    );
    // where no proof applies the object is null
    assert!(branch_json(&v, "eq")["similarity"].is_null());
}

#[test]
fn the_json_of_a_modified_branch_has_evidence_lines() {
    let w = scenarios();
    let v = plan_json(&w, &[]);
    let b = branch_json(&v, "mod");
    assert_eq!(b["proof"], "unmerged");
    let s = &b["similarity"];
    assert_eq!(s["class"], "modified");
    let ev = s["commits"][0]["evidence"].as_array().unwrap();
    assert_eq!(
        ev.len(),
        5,
        "at most five lines, four lines differ on each side"
    );
    assert_eq!(
        key_list(&ev[0]),
        sorted(&["path", "sign", "line", "count_a", "count_b"])
    );
    // the squash has a commit below the low threshold, the empty branch has no commit
    assert_eq!(branch_json(&v, "squash")["similarity"]["class"], "partial");
    assert_eq!(branch_json(&v, "empty")["similarity"]["class"], "none");
    assert_eq!(branch_json(&v, "empty")["similarity"]["min"], Value::Null);
    let pt = &branch_json(&v, "part")["similarity"]["commits"][0];
    // the 20 removed lines are shared: 2 * 20 / (40 + 60)
    assert_eq!(pt["similarity"], 40.0);
}

#[test]
fn the_summary_counts_similar_branches_as_review_and_names_them() {
    let w = scenarios();
    let v = plan_json(&w, &[]);
    assert_eq!(
        key_list(&v["summary"]),
        sorted(&[
            "delete",
            "delete_with_worktree",
            "review",
            "keep",
            "similar"
        ])
    );
    assert_eq!(v["summary"]["review"], 1);
    assert_eq!(v["summary"]["similar"], 1);
    assert_eq!(v["summary"]["delete"], 1);
}

#[test]
fn the_text_plan_has_one_line_for_a_similar_branch_and_no_command() {
    let w = scenarios();
    let home = scratch();
    let r = path_str(&w.fx.path);
    let o = stw(home.path(), &["tidy", "plan", "--offline", "--root", &r]);
    assert_eq!(o.status.code(), Some(0));
    let out = String::from_utf8_lossy(&o.stdout).to_string();
    let line = out
        .lines()
        .find(|l| l.contains("[similar-patches]"))
        .unwrap_or_else(|| panic!("no line: {out}"));
    assert!(line.trim_start().starts_with("review "), "{line}");
    assert!(line.contains(" sim "), "{line}");
    assert!(line.contains("min 97.5 %"), "{line}");
    assert!(line.contains("not a deletion proof"), "{line}");
    assert!(
        !out.contains("stw tidy apply") && !out.contains("git branch"),
        "{out}"
    );
    assert!(out.contains("1 to review"), "{out}");
    assert!(out.contains("(1 of them similar-patches)"), "{out}");
}
