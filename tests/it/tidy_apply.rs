//! `tidy apply`: what is deleted, what never is, the manifest, the re-verification, worktrees, remotes.
//! Every test runs in a throw-away repository; no real repository is touched. Mutations that prove the
//! checks are listed in tests/mutation/mutate.sh. OPEN (no mutation proves them): the `.git` file check of a
//! worktree (another check refuses the same case first), the registration path check, the fsync of the manifest.
//! Mutation ids: B1..B5, T3..T13, W4..W12, Y1..Y6, RS1..RS6, M1, N2.
use std::path::Path;

use crate::common::*;
use stw::repo::RepoRef;
use stw::tidy::{plan_repo_with_opts, PlanOpts, PrProof, RepoPlan};
use stw::tidy_apply::{
    apply_repo, read_plan_file, restore, ApplyOpts, ApplyRepo, BranchResult, Kind, Planned,
};

const DAY: i64 = 86_400;

struct W {
    fx: Fx,
    now: i64,
    c0: gix::ObjectId,
    u: gix::ObjectId,
}

/// main = c1 (origin/main = c1, checked out). Branches: merged-a, merged-b (ancestors: delete), unmerged (keep),
/// gone-up (review), backup/old (older than 7 days at `now`: delete), backup/new (younger: keep).
fn build() -> W {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    let c1 = fx.commit(Some(c0), &[("a.txt", Some("b\n"))], "main work");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.branch("main", c1);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("merged-a", c0);
    fx.branch("merged-b", c0);
    let u = fx.commit(Some(c0), &[("u.txt", Some("u\n"))], "unmerged");
    fx.branch("unmerged", u);
    let g = fx.commit(Some(c0), &[("g.txt", Some("g\n"))], "gone upstream");
    fx.branch("gone-up", g);
    fx.track("gone-up");
    let b1 = fx.commit(Some(c0), &[("o.txt", Some("old backup\n"))], "old backup");
    fx.branch("backup/old", b1);
    let b2 = fx.commit(Some(c0), &[("n.txt", Some("new backup\n"))], "new backup");
    fx.branch("backup/new", b2);
    let now = fx
        .repo()
        .find_commit(b1)
        .unwrap()
        .committer()
        .unwrap()
        .seconds()
        + 7 * DAY;
    W { fx, now, c0, u }
}

fn rr(w: &W) -> RepoRef {
    RepoRef {
        name: "fx".into(),
        path: w.fx.path.clone(),
    }
}

fn plan_with(w: &W, days: u64) -> RepoPlan {
    plan_repo_with_opts(
        &rr(w),
        PrProof {
            prs: None,
            note: "off".into(),
        },
        &PlanOpts {
            backup_days: days,
            now: w.now,
        },
    )
}

fn plan(w: &W) -> RepoPlan {
    plan_with(w, 7)
}

fn opts(w: &W, dir: &Path, run: &str) -> ApplyOpts {
    ApplyOpts {
        manifest_dir: dir.to_path_buf(),
        run_id: run.into(),
        now: w.now,
    }
}

const RUN: &str = "0000000000001-1-0";

fn apply(w: &W, dir: &Path) -> ApplyRepo {
    let r = apply_repo(&rr(w), &plan(w), None, &opts(w, dir, RUN));
    assert!(r.error.is_none(), "{:?}", r.error);
    r
}

fn has(w: &W, b: &str) -> bool {
    stw::repo::resolve(&w.fx.repo(), &format!("refs/heads/{b}")).is_some()
}

fn res<'a>(r: &'a ApplyRepo, b: &str) -> &'a BranchResult {
    r.branches
        .iter()
        .find(|x| x.branch == b)
        .unwrap_or_else(|| panic!("no result for {b}: {:?}", r.branches))
}

fn manifest_lines(dir: &Path, run: &str) -> Vec<serde_json::Value> {
    let text = std::fs::read_to_string(dir.join(format!("{run}.jsonl"))).unwrap();
    text.lines()
        .map(|l| serde_json::from_str(l).unwrap())
        .collect()
}

/// Everything outside `refs/heads` (remote-tracking refs, tags, ...) plus the config file.
fn remote_state(w: &W) -> Vec<String> {
    let repo = w.fx.repo();
    let mut v: Vec<String> = repo
        .references()
        .unwrap()
        .all()
        .unwrap()
        .filter_map(|r| r.ok())
        .map(|r| format!("{} {:?}", r.name().as_bstr(), r.target()))
        .filter(|s| !s.starts_with("refs/heads/"))
        .collect();
    v.push(std::fs::read_to_string(w.fx.path.join(".git/config")).unwrap());
    v
}

// ------------------------------------------------------------------ classes, manifest

#[test]
fn apply_deletes_only_the_delete_class() {
    let w = build();
    let tmp = scratch();
    let dir = tmp.path().join("manifests");
    let old_sha = w.fx.tip("backup/old");
    let r = apply(&w, &dir);
    for b in ["merged-a", "merged-b", "backup/old"] {
        assert_eq!(res(&r, b).action, "deleted", "{b}");
        assert!(!has(&w, b), "{b} must be gone");
    }
    // review, keep and the base branch are never touched
    for b in ["gone-up", "unmerged", "backup/new", "main"] {
        assert!(has(&w, b), "{b} must stay");
        assert!(
            r.branches.iter().all(|x| x.branch != b),
            "{b} is not even listed"
        );
    }
    let lines = manifest_lines(&dir, RUN);
    assert_eq!(lines.len(), 3);
    let old = lines.iter().find(|l| l["branch"] == "backup/old").unwrap();
    assert_eq!(old["sha"], old_sha.to_string());
    assert_eq!(old["proof"], "backup-older-than-days");
    assert_eq!(old["run"], RUN);
    assert_eq!(old["repo"], path_str(&w.fx.path));
    assert_eq!(old["time"], w.now);
    assert_eq!(old["manifest_version"], 1);
    let a = lines.iter().find(|l| l["branch"] == "merged-a").unwrap();
    assert_eq!(a["sha"], w.c0.to_string());
    assert_eq!(a["proof"], "merged-ancestor");
    assert_eq!(
        r.manifest.as_deref(),
        Some(path_str(&dir.join(format!("{RUN}.jsonl"))).as_str())
    );
}

#[test]
fn a_manifest_that_cannot_be_written_deletes_nothing() {
    let w = build();
    let tmp = scratch();
    let blocker = tmp.path().join("blocker");
    std::fs::write(&blocker, "a file, not a directory").unwrap();
    let r = apply_repo(
        &rr(&w),
        &plan(&w),
        None,
        &opts(&w, &blocker.join("sub"), RUN),
    );
    let e = r.error.as_deref().expect("an error");
    assert!(
        e.contains("manifest") && e.contains("nothing was deleted"),
        "{e}"
    );
    for b in ["merged-a", "merged-b", "backup/old"] {
        assert!(has(&w, b), "{b} must still exist");
    }
    assert!(r.failed());
}

#[test]
fn the_manifest_lists_a_branch_whose_deletion_then_fails() {
    let w = build();
    let tmp = scratch();
    let dir = tmp.path().join("m");
    // a stale lock file makes the ref update of merged-a fail, after the manifest was written
    std::fs::write(w.fx.path.join(".git/refs/heads/merged-a.lock"), "").unwrap();
    let r = apply(&w, &dir);
    assert_eq!(res(&r, "merged-a").action, "failed");
    assert!(has(&w, "merged-a"));
    assert_eq!(res(&r, "merged-b").action, "deleted");
    assert_eq!(
        manifest_lines(&dir, RUN).len(),
        3,
        "the manifest has all three"
    );
    assert!(r.failed());
}

// ------------------------------------------------------------------ re-verification

fn plan_file_entries(w: &W) -> Vec<Planned> {
    let tmp = scratch();
    let file = tmp.path().join("plan.json");
    let json = stw::tidy::to_json(
        &path_str(&w.fx.path),
        &stw::github::GithubState {
            client: None,
            note: None,
        },
        &[plan(w)],
    );
    std::fs::write(&file, json.to_string()).unwrap();
    let mut all = read_plan_file(&file).unwrap();
    assert_eq!(all.len(), 1);
    all.remove(0).1
}

#[test]
fn the_plan_file_lists_only_what_may_be_deleted() {
    let w = build();
    let list = plan_file_entries(&w);
    let mut names: Vec<&str> = list.iter().map(|p| p.branch.as_str()).collect();
    names.sort();
    assert_eq!(names, ["backup/old", "merged-a", "merged-b"]);
    assert!(list.iter().all(|p| p.kind == Kind::Branch));
}

#[test]
fn a_branch_listed_twice_in_the_plan_file_is_handled_once() {
    let w = build();
    let mut wanted = plan_file_entries(&w);
    wanted.extend(plan_file_entries(&w));
    let tmp = scratch();
    let dir = tmp.path().join("m");
    let r = apply_repo(&rr(&w), &plan(&w), Some(&wanted), &opts(&w, &dir, RUN));
    assert!(!r.failed(), "{:?}", r.branches);
    assert_eq!(r.branches.len(), 3, "{:?}", r.branches);
    assert!(r.branches.iter().all(|b| b.action == "deleted"));
    assert_eq!(
        manifest_lines(&dir, RUN).len(),
        3,
        "one manifest line per branch"
    );
}

#[test]
fn a_branch_that_moved_since_the_plan_is_skipped() {
    let w = build();
    let wanted = plan_file_entries(&w);
    w.fx.branch("merged-b", w.u); // moved to unmerged work after the plan
    let tmp = scratch();
    let dir = tmp.path().join("m");
    let r = apply_repo(&rr(&w), &plan(&w), Some(&wanted), &opts(&w, &dir, RUN));
    let b = res(&r, "merged-b");
    assert_eq!(b.action, "skipped");
    assert!(b.reason.contains("moved since the plan"), "{}", b.reason);
    assert_eq!(w.fx.tip("merged-b"), w.u, "the moved branch is untouched");
    assert_eq!(res(&r, "merged-a").action, "deleted");
}

#[test]
fn a_proof_that_no_longer_holds_skips_the_branch() {
    let w = build();
    let wanted = plan_file_entries(&w);
    // the same commit, but now a backup of 30 days is required: backup/old is keep again
    let r = apply_repo(
        &rr(&w),
        &plan_with(&w, 30),
        Some(&wanted),
        &opts(&w, scratch().path(), RUN),
    );
    let b = res(&r, "backup/old");
    assert_eq!(b.action, "skipped");
    assert!(b.reason.contains("no longer holds"), "{}", b.reason);
    assert!(has(&w, "backup/old"));
    assert!(!has(&w, "merged-a"));
}

#[test]
fn a_vanished_branch_is_reported_and_new_branches_are_not_added_to_a_plan_file() {
    let w = build();
    let wanted = plan_file_entries(&w);
    w.fx.set_ref("refs/heads/late", w.c0); // merged, but not in the plan file
    std::fs::remove_file(w.fx.path.join(".git/refs/heads/merged-b")).unwrap();
    let tmp = scratch();
    let r = apply_repo(
        &rr(&w),
        &plan(&w),
        Some(&wanted),
        &opts(&w, tmp.path(), RUN),
    );
    assert_eq!(res(&r, "merged-b").action, "skipped");
    assert!(res(&r, "merged-b").reason.contains("no longer exists"));
    assert!(has(&w, "late"), "not in the plan file: not deleted");
    assert!(r.branches.iter().all(|b| b.branch != "late"));
}

/// The plan is an argument of `apply_repo` (the CLI plans every repository first, then applies), so it can be
/// older than the repository. These two tests hand it a plan that is stale on purpose.
#[test]
fn a_branch_moved_after_the_plan_is_not_deleted_by_the_ref_update() {
    let w = build();
    let stale = plan(&w); // merged-a is `delete` at c0
    w.fx.branch("merged-a", w.u); // new, unmerged work lands on it afterwards
    let tmp = scratch();
    let r = apply_repo(&rr(&w), &stale, None, &opts(&w, tmp.path(), RUN));
    assert_eq!(res(&r, "merged-a").action, "failed");
    assert_eq!(w.fx.tip("merged-a"), w.u, "the new work is still there");
    assert_eq!(res(&r, "merged-b").action, "deleted");
}

#[test]
fn a_branch_checked_out_after_the_plan_is_not_deleted() {
    let w = build();
    let stale = plan(&w);
    let wt = add_worktree(&w.fx, "late", "merged-a", true, false); // checked out after the plan
    let tmp = scratch();
    let r = apply_repo(&rr(&w), &stale, None, &opts(&w, tmp.path(), RUN));
    let b = res(&r, "merged-a");
    assert_eq!(b.action, "skipped", "{}", b.reason);
    assert!(b.reason.contains("checked out in"), "{}", b.reason);
    assert!(has(&w, "merged-a"));
    assert!(wt.is_dir());
}

// ------------------------------------------------------------------ worktrees

fn set_file(dir: &Path, rel: &str, text: &str) {
    std::fs::write(dir.join(rel), text).unwrap();
}

#[test]
fn the_branch_checked_out_in_the_main_worktree_stays() {
    let w = build();
    w.fx.set_head("merged-a"); // main checkout is now on a merged branch
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    let b = res(&r, "merged-a");
    assert_eq!(b.action, "skipped", "{}", b.reason);
    assert!(has(&w, "merged-a"));
    assert!(r.worktrees.iter().any(|x| x.reason == "main worktree"));
    assert_eq!(res(&r, "merged-b").action, "deleted");
}

#[test]
fn a_clean_worktree_is_removed_with_its_branch() {
    let w = build();
    let wt = real_worktree(&w.fx, "a", "merged-a");
    assert!(wt.join("a.txt").is_file());
    let p = plan(&w);
    let bp = p.branches.iter().find(|b| b.branch == "merged-a").unwrap();
    assert_eq!((bp.decision, bp.proof), ("keep", "in-worktree"));
    assert_eq!(bp.worktree.as_ref().unwrap().proof, "merged-ancestor");
    let tmp = scratch();
    let dir = tmp.path().join("m");
    let r = apply(&w, &dir);
    assert_eq!(res(&r, "merged-a").action, "deleted", "{:?}", r.worktrees);
    assert!(!wt.exists(), "the worktree directory is gone");
    assert!(
        !w.fx.path.join(".git/worktrees/a").exists(),
        "so is the registration"
    );
    assert!(!has(&w, "merged-a"));
    let lines = manifest_lines(&dir, RUN);
    let l = lines.iter().find(|l| l["branch"] == "merged-a").unwrap();
    assert_eq!(l["worktree"], path_str(&wt));
    assert_eq!(l["proof"], "merged-ancestor");
    assert!(w.fx.exists("a.txt"), "the main checkout is untouched");
}

/// The same proof removes a worktree below `<repo>/.worktrees/` and one elsewhere; both stay when they are dirty.
#[test]
fn worktrees_inside_and_outside_the_worktrees_directory_are_removed_on_the_same_proof() {
    let w = build();
    let inside = real_worktree_at(&w.fx, "a", "merged-a", &w.fx.path.join(".worktrees/a"));
    let outside = real_worktree(&w.fx, "b", "merged-b");
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    for (b, dir) in [("merged-a", &inside), ("merged-b", &outside)] {
        assert_eq!(res(&r, b).action, "deleted", "{b}: {:?}", r.worktrees);
        assert!(!dir.exists(), "{b}: the directory is gone");
        assert!(!has(&w, b));
    }
}

#[test]
fn a_dirty_worktree_below_the_worktrees_directory_stays() {
    let w = build();
    let wt = real_worktree_at(&w.fx, "a", "merged-a", &w.fx.path.join(".worktrees/a"));
    set_file(&wt, "new.txt", "x\n");
    assert_worktree_stays(&w, &wt, "uncommitted changes");
}

fn assert_worktree_stays(w: &W, wt: &Path, why: &str) {
    let tmp = scratch();
    let r = apply(w, tmp.path());
    let b = res(&r, "merged-a");
    assert_eq!(b.action, "skipped", "{why}: {}", b.reason);
    assert!(has(w, "merged-a"), "{why}: branch stays");
    assert!(wt.join("a.txt").is_file(), "{why}: directory stays");
    let item = r
        .worktrees
        .iter()
        .find(|x| x.branch.as_deref() == Some("merged-a"))
        .unwrap();
    assert_eq!(item.action, "skipped");
    assert!(item.reason.contains(why), "{why} not in {:?}", item.reason);
}

#[test]
fn a_worktree_with_a_modified_file_stays() {
    let w = build();
    let wt = real_worktree(&w.fx, "a", "merged-a");
    set_file(&wt, "a.txt", "changed\n");
    assert_worktree_stays(&w, &wt, "uncommitted changes");
}

#[test]
fn a_worktree_with_an_untracked_file_stays() {
    let w = build();
    let wt = real_worktree(&w.fx, "a", "merged-a");
    set_file(&wt, "new.txt", "x\n");
    assert_worktree_stays(&w, &wt, "uncommitted changes");
}

/// `git worktree add --relative-paths`: both link files hold relative paths. Such a registration is never
/// followed or removed; it has exactly one status, and the path shown has no `..`.
#[test]
fn a_worktree_with_relative_paths_stays_and_is_shown_without_dot_dot() {
    let w = build();
    let wt = real_worktree(&w.fx, "a", "merged-a");
    let reg = w.fx.path.join(".git/worktrees/a");
    let name = wt.file_name().unwrap().to_string_lossy().into_owned();
    std::fs::write(reg.join("gitdir"), format!("../../../../{name}/.git\n")).unwrap();
    std::fs::write(wt.join(".git"), "gitdir: ../repo/.git/worktrees/a\n").unwrap();
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    assert_eq!(res(&r, "merged-a").action, "skipped");
    assert!(has(&w, "merged-a") && wt.join("a.txt").is_file());
    assert_eq!(r.registrations.len(), 1, "{:?}", r.registrations);
    assert!(r.worktrees.is_empty(), "{:?}", r.worktrees);
    assert_eq!(r.registrations[0].action, "skipped");
    assert_eq!(r.registrations[0].path, path_str(&wt));
    for p in [&r.registrations[0].path, &res(&r, "merged-a").reason] {
        assert!(!p.contains("/../"), "{p}");
    }
}

/// A registration whose `gitdir` is `<dir>/link/../wt-a/.git` with `link` a symbolic link to a foreign
/// directory: the lexical target `<dir>/wt-a` is a real, clean worktree, but the file system resolves the path
/// elsewhere. The lexical path must decide nothing: the worktree stays, one status, no failure.
#[test]
fn a_dot_dot_behind_a_symlink_selects_nothing_by_its_lexical_path() {
    let w = build();
    let wt = real_worktree(&w.fx, "a", "merged-a"); // <parent>/wt-a, clean
    let parent = wt.parent().unwrap().to_path_buf();
    let foreign = scratch();
    std::fs::create_dir_all(foreign.path().join("inner")).unwrap();
    std::os::unix::fs::symlink(foreign.path().join("inner"), parent.join("link")).unwrap();
    let reg = w.fx.path.join(".git/worktrees/a");
    std::fs::write(
        reg.join("gitdir"),
        format!("{}/link/../wt-a/.git\n", parent.display()),
    )
    .unwrap();
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    assert!(wt.join("a.txt").is_file(), "the lexical target must stay");
    assert!(reg.exists(), "the registration must stay");
    assert!(has(&w, "merged-a"));
    assert_eq!(r.registrations.len(), 1, "{:?}", r.registrations);
    assert_eq!(r.registrations[0].action, "skipped");
    assert!(r.worktrees.is_empty());
    assert!(!r.failed(), "{:?}", r);
    assert_eq!(res(&r, "merged-a").action, "skipped");
    assert!(foreign.path().join("inner").is_dir());
}

#[test]
fn a_locked_worktree_stays() {
    let w = build();
    let wt = real_worktree(&w.fx, "a", "merged-a");
    std::fs::write(w.fx.path.join(".git/worktrees/a/locked"), "usb stick\n").unwrap();
    assert_worktree_stays(&w, &wt, "locked: usb stick");
}

#[test]
fn a_worktree_whose_commits_are_not_pushed_stays() {
    let w = build();
    // equal-patches proof (squash-merge shape) but the tip is neither in the base nor in an upstream
    let r1 =
        w.fx.commit(Some(w.c0), &[("a.txt", Some("b\n"))], "same change");
    w.fx.branch("merged-a", r1);
    let wt = real_worktree(&w.fx, "a", "merged-a");
    let p = plan(&w);
    let bp = p.branches.iter().find(|b| b.branch == "merged-a").unwrap();
    assert_eq!(bp.worktree.as_ref().unwrap().proof, "equal-patches");
    assert_worktree_stays(&w, &wt, "not pushed");
}

#[test]
fn a_pushed_squash_merged_worktree_is_removed() {
    let w = build();
    let r1 =
        w.fx.commit(Some(w.c0), &[("a.txt", Some("b\n"))], "same change");
    w.fx.branch("merged-a", r1);
    w.fx.remote("merged-a", r1);
    w.fx.track("merged-a");
    let wt = real_worktree(&w.fx, "a", "merged-a");
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    assert_eq!(res(&r, "merged-a").action, "deleted", "{:?}", r.worktrees);
    assert!(!wt.exists());
}

#[test]
fn a_worktree_with_a_foreign_gitfile_stays() {
    let w = build();
    let wt = real_worktree(&w.fx, "a", "merged-a");
    // the directory's .git file names another registration
    // (a readable, clean repository: only the registration check can tell it is the wrong one)
    std::fs::write(
        wt.join(".git"),
        format!("gitdir: {}\n", w.fx.path.join(".git").display()),
    )
    .unwrap();
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    assert_eq!(res(&r, "merged-a").action, "skipped");
    assert!(wt.is_dir());
    assert!(w.fx.path.join(".git/worktrees/a").exists());
}

#[test]
fn a_dead_registration_is_removed_and_frees_its_branch() {
    let w = build();
    add_worktree(&w.fx, "gone", "merged-a", false, false);
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    assert!(
        r.registrations.iter().any(|x| x.action == "removed"),
        "{:?}",
        r.registrations
    );
    assert!(!w.fx.path.join(".git/worktrees/gone").exists());
    assert_eq!(res(&r, "merged-a").action, "deleted");
    assert!(!has(&w, "merged-a"));
}

#[test]
fn a_locked_dead_registration_stays_with_its_reason() {
    let w = build();
    add_worktree(&w.fx, "gone", "merged-a", false, true);
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    let reg = &r.registrations[0];
    assert_eq!(reg.action, "skipped");
    assert_eq!(reg.reason, "locked");
    assert!(w.fx.path.join(".git/worktrees/gone").exists());
    assert_eq!(res(&r, "merged-a").action, "skipped");
    assert!(has(&w, "merged-a"));
}

/// The plan file has no field for registrations, so a run from a plan file leaves them alone.
#[test]
fn a_plan_file_run_does_not_touch_dead_registrations() {
    let w = build();
    let wanted = plan_file_entries(&w);
    add_worktree(&w.fx, "gone", "unmerged", false, false);
    let tmp = scratch();
    let r = apply_repo(
        &rr(&w),
        &plan(&w),
        Some(&wanted),
        &opts(&w, tmp.path(), RUN),
    );
    assert!(r.registrations.is_empty(), "{:?}", r.registrations);
    assert!(w.fx.path.join(".git/worktrees/gone").exists());
    assert_eq!(res(&r, "merged-a").action, "deleted");
}

/// Ignore `.worktrees/` the way `tidy apply` does, so a worktree inside another one is invisible to `status`.
fn ignore_worktrees_dir(w: &W) {
    let ex = exclude_path(w);
    std::fs::create_dir_all(ex.parent().unwrap()).unwrap();
    let old = std::fs::read_to_string(&ex).unwrap_or_default();
    std::fs::write(&ex, format!("{old}.worktrees/\n*.log\n")).unwrap();
}

/// A worktree an agent made inside another one (ignored, so `status` does not see it) would be deleted with
/// its uncommitted work: the outer one stays, in the plan and in apply.
#[test]
fn a_worktree_nested_below_a_removable_one_blocks_its_removal() {
    let w = build();
    ignore_worktrees_dir(&w);
    let a = real_worktree_at(&w.fx, "a", "merged-a", &w.fx.path.join(".worktrees/a"));
    let b = real_worktree_at(&w.fx, "b", "unmerged", &a.join(".worktrees/b"));
    set_file(&b, "work.txt", "precious\n");
    let p = plan(&w);
    let cand = p
        .branches
        .iter()
        .find(|x| x.branch == "merged-a")
        .and_then(|x| x.worktree.as_ref())
        .unwrap();
    let why = cand.blocker.as_deref().expect("the plan names a blocker");
    assert!(why.contains("contains the worktree"), "{why}");
    assert!(why.contains(".worktrees/b"), "{why}");
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    assert_eq!(res(&r, "merged-a").action, "skipped");
    assert!(has(&w, "merged-a"));
    assert!(a.join("a.txt").is_file());
    assert_eq!(
        std::fs::read_to_string(b.join("work.txt")).unwrap(),
        "precious\n"
    );
}

/// The same for a git repository that is not a registered worktree.
#[test]
fn a_git_repository_below_a_removable_worktree_blocks_its_removal() {
    let w = build();
    ignore_worktrees_dir(&w);
    let a = real_worktree_at(&w.fx, "a", "merged-a", &w.fx.path.join(".worktrees/a"));
    std::fs::create_dir_all(a.join(".worktrees/clone/.git")).unwrap();
    std::fs::write(a.join(".worktrees/clone/work.txt"), "precious\n").unwrap();
    assert_worktree_stays(&w, &a, "contains the git repository");
    assert!(a.join(".worktrees/clone/work.txt").is_file());
}

/// Documented: ignored files are not changes; they go with the directory.
#[test]
fn ignored_files_go_with_the_removed_worktree() {
    let w = build();
    ignore_worktrees_dir(&w);
    let a = real_worktree_at(&w.fx, "a", "merged-a", &w.fx.path.join(".worktrees/a"));
    set_file(&a, "build.log", "noise\n");
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    assert_eq!(res(&r, "merged-a").action, "deleted", "{:?}", r.worktrees);
    assert!(!a.exists());
}

/// The plan names the worktree; a branch that is checked out elsewhere now is not the planned removal.
#[test]
fn a_branch_moved_to_another_worktree_after_the_plan_is_not_applied() {
    let w = build();
    let a = real_worktree(&w.fx, "a", "merged-a");
    let wanted = plan_file_entries(&w);
    let e = wanted.iter().find(|p| p.branch == "merged-a").unwrap();
    assert_eq!(e.kind, Kind::WorktreeBranch);
    assert_eq!(e.worktree.as_deref(), Some(path_str(&a).as_str()));
    std::fs::remove_dir_all(&a).unwrap();
    std::fs::remove_dir_all(w.fx.path.join(".git/worktrees/a")).unwrap();
    let c = real_worktree(&w.fx, "c", "merged-a");
    let tmp = scratch();
    let r = apply_repo(
        &rr(&w),
        &plan(&w),
        Some(&wanted),
        &opts(&w, tmp.path(), RUN),
    );
    let b = res(&r, "merged-a");
    assert_eq!(b.action, "skipped");
    assert!(b.reason.contains("no longer holds"), "{}", b.reason);
    assert!(c.join("a.txt").is_file());
    assert!(has(&w, "merged-a"));
}

/// git refuses to delete a branch a dead registration still holds; a plan file run does not remove the
/// registration, so it does not delete the branch either.
#[test]
fn a_plan_file_run_keeps_a_branch_held_by_a_dead_registration() {
    let w = build();
    let wanted = plan_file_entries(&w);
    add_worktree(&w.fx, "gone", "merged-a", false, false);
    let tmp = scratch();
    let r = apply_repo(
        &rr(&w),
        &plan(&w),
        Some(&wanted),
        &opts(&w, tmp.path(), RUN),
    );
    let b = res(&r, "merged-a");
    assert_eq!(b.action, "skipped");
    assert!(
        b.reason.contains("dead worktree registration"),
        "{}",
        b.reason
    );
    assert!(has(&w, "merged-a"));
    assert!(w.fx.path.join(".git/worktrees/gone").exists());
    assert_eq!(res(&r, "merged-b").action, "deleted");
}

// ------------------------------------------------------------------ .worktrees/ exclude

fn exclude_path(w: &W) -> std::path::PathBuf {
    w.fx.path.join(".git/info/exclude")
}

fn exclude_lines(w: &W) -> Vec<String> {
    std::fs::read_to_string(exclude_path(w))
        .unwrap_or_default()
        .lines()
        .map(str::to_string)
        .collect()
}

#[test]
fn apply_excludes_the_worktrees_directory_in_info_exclude_once() {
    let w = build();
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    assert_eq!(
        exclude_lines(&w)
            .iter()
            .filter(|l| *l == ".worktrees/")
            .count(),
        1,
        "{:?}",
        exclude_lines(&w)
    );
    assert_eq!(r.exclude.as_ref().unwrap().action, "added");
    // a second run: nothing is added again
    let r = apply(&w, tmp.path());
    assert_eq!(
        exclude_lines(&w)
            .iter()
            .filter(|l| *l == ".worktrees/")
            .count(),
        1
    );
    assert_eq!(r.exclude.as_ref().unwrap().action, "present");
}

#[test]
fn the_exclude_line_keeps_existing_content_and_never_touches_gitignore() {
    let w = build();
    std::fs::create_dir_all(w.fx.path.join(".git/info")).unwrap();
    // an existing file without a final newline
    std::fs::write(exclude_path(&w), "# mine\n*.tmp").unwrap();
    std::fs::write(w.fx.path.join(".gitignore"), "target/\n").unwrap();
    let tmp = scratch();
    apply(&w, tmp.path());
    assert_eq!(
        std::fs::read_to_string(exclude_path(&w)).unwrap(),
        "# mine\n*.tmp\n.worktrees/\n"
    );
    assert_eq!(
        std::fs::read_to_string(w.fx.path.join(".gitignore")).unwrap(),
        "target/\n"
    );
}

#[test]
fn a_crlf_line_counts_as_present_and_gets_no_duplicate() {
    let w = build();
    std::fs::create_dir_all(w.fx.path.join(".git/info")).unwrap();
    std::fs::write(exclude_path(&w), b"*.tmp\r\n.worktrees/\r\n").unwrap();
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    assert_eq!(r.exclude.as_ref().unwrap().action, "present");
    assert_eq!(
        std::fs::read(exclude_path(&w)).unwrap(),
        b"*.tmp\r\n.worktrees/\r\n"
    );
}

#[test]
fn an_equivalent_existing_line_counts_as_present() {
    for line in [".worktrees/", "/.worktrees/", ".worktrees", "/.worktrees"] {
        let w = build();
        std::fs::create_dir_all(w.fx.path.join(".git/info")).unwrap();
        std::fs::write(exclude_path(&w), format!("{line}\n")).unwrap();
        let tmp = scratch();
        let r = apply(&w, tmp.path());
        assert_eq!(r.exclude.as_ref().unwrap().action, "present", "{line}");
        assert_eq!(exclude_lines(&w), vec![line.to_string()], "{line}");
    }
}

#[test]
fn the_exclude_file_is_only_appended_to_byte_for_byte() {
    for old in [
        &b"# crlf\r\n*.tmp\r\n"[..],
        &b"no newline at the end \xff\xfe"[..],
        &b""[..],
    ] {
        let w = build();
        std::fs::create_dir_all(w.fx.path.join(".git/info")).unwrap();
        std::fs::write(exclude_path(&w), old).unwrap();
        let tmp = scratch();
        let r = apply(&w, tmp.path());
        assert_eq!(r.exclude.as_ref().unwrap().action, "added", "{old:?}");
        let new = std::fs::read(exclude_path(&w)).unwrap();
        assert!(new.starts_with(old), "the old bytes stay: {new:?}");
        let tail = &new[old.len()..];
        let expect: &[u8] = if old.is_empty() || old.ends_with(b"\n") {
            b".worktrees/\n"
        } else {
            b"\n.worktrees/\n"
        };
        assert_eq!(tail, expect, "{old:?}");
    }
}

#[test]
fn an_exclude_file_that_cannot_be_written_fails_the_run_but_not_the_deletions() {
    let w = build();
    // a directory in place of the file: reading and writing it both fail
    let _ = std::fs::remove_file(exclude_path(&w));
    std::fs::create_dir_all(exclude_path(&w)).unwrap();
    let tmp = scratch();
    let r = apply_repo(&rr(&w), &plan(&w), None, &opts(&w, tmp.path(), RUN));
    assert!(r.error.is_none(), "{:?}", r.error);
    assert_eq!(r.exclude.as_ref().unwrap().action, "failed");
    assert!(r.failed(), "a failed exclude makes the run exit 1");
    assert_eq!(res(&r, "merged-a").action, "deleted");
}

#[test]
fn a_linked_worktree_as_root_writes_the_common_exclude_file() {
    let w = build();
    let wt = real_worktree_at(&w.fx, "l", "unmerged", &w.fx.path.join(".worktrees/l"));
    let r = RepoRef {
        name: "wt".into(),
        path: wt.clone(),
    };
    let fresh = plan_repo_with_opts(
        &r,
        PrProof {
            prs: None,
            note: "off".into(),
        },
        &PlanOpts {
            backup_days: 7,
            now: w.now,
        },
    );
    let tmp = scratch();
    let res = apply_repo(&r, &fresh, None, &opts(&w, tmp.path(), RUN));
    assert!(res.error.is_none(), "{:?}", res.error);
    assert_eq!(res.exclude.as_ref().unwrap().action, "added");
    assert!(exclude_lines(&w).contains(&".worktrees/".to_string()));
    assert!(
        !w.fx.path.join(".git/worktrees/l/info/exclude").exists(),
        "not in the git directory of the linked worktree"
    );
}

#[test]
fn the_manifest_is_written_before_the_exclude_file() {
    let w = build();
    let tmp = scratch();
    let blocker = tmp.path().join("blocker");
    std::fs::write(&blocker, "a file, not a directory").unwrap();
    let r = apply_repo(
        &rr(&w),
        &plan(&w),
        None,
        &opts(&w, &blocker.join("sub"), RUN),
    );
    assert!(r.error.is_some() && r.failed());
    assert!(r.exclude.is_none(), "{:?}", r.exclude);
    assert!(
        !exclude_lines(&w).contains(&".worktrees/".to_string()),
        "the exclude file is untouched"
    );
    assert!(has(&w, "merged-a"), "nothing was deleted");
}

// ------------------------------------------------------------------ remote

#[test]
fn remote_state_is_untouched() {
    let w = build();
    // origin has a branch of the same name as a deleted one, and a remote-only branch
    w.fx.remote("merged-a", w.c0);
    w.fx.remote("old-remote-only", w.c0);
    let before = remote_state(&w);
    let tmp = scratch();
    let r = apply(&w, tmp.path());
    assert_eq!(res(&r, "merged-a").action, "deleted");
    assert_eq!(remote_state(&w), before);
}

// ------------------------------------------------------------------ restore

fn restore_in(dir: &Path) -> stw::tidy_apply::RestoreRun {
    restore(dir, None, None).unwrap()
}

#[test]
fn restore_recreates_deleted_branches_and_is_idempotent() {
    let w = build();
    let tmp = scratch();
    let dir = tmp.path().join("m");
    let before: Vec<_> = ["merged-a", "merged-b", "backup/old"]
        .iter()
        .map(|b| w.fx.tip(b))
        .collect();
    apply(&w, &dir);
    let r = restore_in(&dir);
    assert_eq!(r.entries.len(), 3);
    assert!(
        r.entries.iter().all(|e| e.action == "restored"),
        "{:?}",
        r.entries
    );
    for (b, sha) in ["merged-a", "merged-b", "backup/old"].iter().zip(before) {
        assert_eq!(w.fx.tip(b), sha, "{b}");
    }
    let again = restore_in(&dir);
    assert!(
        again.entries.iter().all(|e| e.action == "already-present"),
        "{:?}",
        again.entries
    );
    assert!(again.complete());
}

#[test]
fn restore_never_overwrites_a_branch_at_another_commit() {
    let w = build();
    let tmp = scratch();
    let dir = tmp.path().join("m");
    apply(&w, &dir);
    w.fx.branch("merged-a", w.u);
    let r = restore_in(&dir);
    let a = r.entries.iter().find(|e| e.branch == "merged-a").unwrap();
    assert_eq!(a.action, "not-overwritten");
    assert!(a.reason.contains("manifest says"), "{}", a.reason);
    assert_eq!(w.fx.tip("merged-a"), w.u, "the branch was not moved");
    assert!(!r.complete());
    assert!(has(&w, "merged-b"), "the others are restored");
}

#[test]
fn restore_reports_a_missing_object() {
    let w = build();
    let tmp = scratch();
    let line = serde_json::json!({"manifest_version": 1, "run": "r1", "repo": path_str(&w.fx.path),
        "branch": "lost", "sha": "1234567890123456789012345678901234567890", "proof": "x", "time": 1, "worktree": null});
    std::fs::write(tmp.path().join("r1.jsonl"), format!("{line}\n")).unwrap();
    let r = restore_in(tmp.path());
    assert_eq!(r.entries[0].action, "missing-object");
    assert!(
        r.entries[0].reason.contains("no longer in"),
        "{}",
        r.entries[0].reason
    );
    assert!(!has(&w, "lost"));
    assert!(!r.complete());
}

#[test]
fn a_run_killed_after_the_manifest_can_be_restored() {
    let w = build();
    let tmp = scratch();
    let dir = tmp.path().join("m");
    // every ref update fails, which leaves the state of a process killed right after the manifest
    for b in ["merged-a", "merged-b"] {
        std::fs::write(w.fx.path.join(format!(".git/refs/heads/{b}.lock")), "").unwrap();
    }
    std::fs::create_dir_all(w.fx.path.join(".git/refs/heads/backup")).unwrap();
    std::fs::write(w.fx.path.join(".git/refs/heads/backup/old.lock"), "").unwrap();
    let r = apply(&w, &dir);
    assert!(
        r.branches.iter().all(|b| b.action == "failed"),
        "{:?}",
        r.branches
    );
    let first = restore_in(&dir);
    assert!(
        first.entries.iter().all(|e| e.action == "already-present"),
        "{:?}",
        first.entries
    );
    assert!(
        first.complete(),
        "nothing was lost, restore has nothing to do"
    );
    // now the process "got further": merged-b is deleted, the others are not
    std::fs::remove_file(w.fx.path.join(".git/refs/heads/merged-b")).unwrap();
    std::fs::remove_file(w.fx.path.join(".git/refs/heads/merged-b.lock")).unwrap();
    let second = restore_in(&dir);
    let by = |b: &str| {
        second
            .entries
            .iter()
            .find(|e| e.branch == b)
            .unwrap()
            .action
    };
    assert_eq!(by("merged-b"), "restored");
    assert_eq!(by("merged-a"), "already-present");
    assert!(has(&w, "merged-b"));
}

#[test]
fn restore_picks_the_newest_run_unless_told_and_can_filter_a_branch() {
    let w = build();
    let tmp = scratch();
    let dir = tmp.path().join("m");
    let r1 = apply_repo(
        &rr(&w),
        &plan(&w),
        None,
        &opts(&w, &dir, "0000000000001-1-0"),
    );
    assert!(r1.error.is_none());
    // second run: a branch created later, merged
    w.fx.branch("second", w.c0);
    let r2 = apply_repo(
        &rr(&w),
        &plan(&w),
        None,
        &opts(&w, &dir, "0000000000002-1-0"),
    );
    assert_eq!(res(&r2, "second").action, "deleted");
    let newest = restore(&dir, None, None).unwrap();
    assert_eq!(newest.run, "0000000000002-1-0");
    assert_eq!(newest.entries.len(), 1);
    let older = restore(&dir, Some("0000000000001-1-0"), Some("merged-b")).unwrap();
    assert_eq!(older.entries.len(), 1);
    assert_eq!(older.entries[0].branch, "merged-b");
    assert!(
        has(&w, "merged-b") && !has(&w, "merged-a"),
        "only the filtered branch came back"
    );
    let e = restore(&dir, Some("0000000000001-1-0"), Some("nope"))
        .err()
        .unwrap();
    assert_eq!(e.kind(), stw::error::Kind::Usage);
    let e = restore(&dir, Some("no-such-run"), None).err().unwrap();
    assert_eq!(e.kind(), stw::error::Kind::Usage);
}

/// G1: two different non-UTF-8 paths with the same content used to get the same patch ID, so tidy took the
/// unmerged branch for already merged ("equal-patches") and deleted it.
#[test]
fn a_branch_changing_a_different_non_utf8_path_is_kept() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("base", Some("x\n"))], "c0");
    let on_main = fx.commit_bytes_path(c0, b"\xff.txt", b"new\n");
    let feature = fx.commit_bytes_path(c0, b"\xfe.txt", b"new\n");
    fx.remote("main", on_main);
    fx.origin_head("main");
    fx.branch("main", on_main);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("feature", feature);
    let now = fx
        .repo()
        .find_commit(feature)
        .unwrap()
        .committer()
        .unwrap()
        .seconds()
        + DAY;
    let w = W {
        fx,
        now,
        c0,
        u: feature,
    };
    let p = plan(&w);
    let bp = p.branches.iter().find(|b| b.branch == "feature").unwrap();
    assert_eq!(bp.decision, "keep", "[{}] {}", bp.proof, bp.detail);
    let dir = scratch();
    apply(&w, dir.path());
    assert_eq!(
        w.fx.tip("feature"),
        feature,
        "the unmerged branch must survive apply"
    );
}

/// G2: run from a linked worktree, tidy must not delete the (merged) branch that the main checkout has
/// checked out.
#[test]
fn from_a_linked_root_the_main_checkouts_branch_is_kept() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a", Some("old\n"))], "c0");
    let c1 = fx.commit(Some(c0), &[("a", Some("new\n"))], "c1");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.branch("main", c1);
    fx.branch("owner", c0); // ancestor of origin/main: merged
    fx.set_head("owner");
    fx.checkout_head();
    let wt = real_worktree(&fx, "linked", "main");
    let now = fx
        .repo()
        .find_commit(c1)
        .unwrap()
        .committer()
        .unwrap()
        .seconds()
        + DAY;
    let w = W { fx, now, c0, u: c1 };
    let rr = RepoRef {
        name: "fx".into(),
        path: wt,
    };
    let p = plan_repo_with_opts(
        &rr,
        PrProof {
            prs: None,
            note: "off".into(),
        },
        &PlanOpts {
            backup_days: 7,
            now,
        },
    );
    assert!(p.error.is_none(), "{:?}", p.error);
    let bp = p.branches.iter().find(|b| b.branch == "owner").unwrap();
    assert_eq!(bp.decision, "keep", "[{}] {}", bp.proof, bp.detail);
    let dir = scratch();
    let r = apply_repo(&rr, &p, None, &opts(&w, dir.path(), RUN));
    assert!(r.error.is_none(), "{:?}", r.error);
    assert_eq!(
        w.fx.tip("owner"),
        c0,
        "the main checkout's branch must survive apply"
    );
}

/// Git's patch ID ignores whitespace, also in file names: `a b.txt` and `ab.txt` get equal IDs. The
/// equal-patches proof additionally needs identical changed paths, else the branch would be deleted.
#[test]
fn a_branch_changing_a_path_that_differs_only_by_spaces_is_kept() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("base", Some("x\n"))], "c0");
    let on_main = fx.commit(Some(c0), &[("ab.txt", Some("new\n"))], "main");
    let feature = fx.commit(Some(c0), &[("a b.txt", Some("new\n"))], "feature");
    fx.remote("main", on_main);
    fx.origin_head("main");
    fx.branch("main", on_main);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("feature", feature);
    let now = fx
        .repo()
        .find_commit(feature)
        .unwrap()
        .committer()
        .unwrap()
        .seconds()
        + DAY;
    let w = W {
        fx,
        now,
        c0,
        u: feature,
    };
    let p = plan(&w);
    let bp = p.branches.iter().find(|b| b.branch == "feature").unwrap();
    assert_eq!(bp.decision, "keep", "[{}] {}", bp.proof, bp.detail);
    let dir = scratch();
    apply(&w, dir.path());
    assert_eq!(w.fx.tip("feature"), feature);
}

/// The same change on the same paths on a different base is still recognised (the proof is not weakened).
#[test]
fn a_branch_with_the_same_change_on_the_same_path_is_still_deleted() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("base", Some("x\n"))], "c0");
    let on_main = fx.commit(Some(c0), &[("a b.txt", Some("new\n"))], "main");
    let feature = fx.commit(Some(c0), &[("a b.txt", Some("new\n"))], "feature");
    fx.remote("main", on_main);
    fx.origin_head("main");
    fx.branch("main", on_main);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("feature", feature);
    let now = fx
        .repo()
        .find_commit(feature)
        .unwrap()
        .committer()
        .unwrap()
        .seconds()
        + DAY;
    let w = W {
        fx,
        now,
        c0,
        u: feature,
    };
    let p = plan(&w);
    let bp = p.branches.iter().find(|b| b.branch == "feature").unwrap();
    assert_eq!((bp.decision, bp.proof), ("delete", "equal-patches"));
}

/// Plan (and apply) for a branch `feature` forked from `c0`, against `origin/main` = one commit on `c0` that
/// applies `main_edits`. Returns (decision, proof) of `feature` and whether the branch still exists after apply.
fn feature_vs_main(
    base: &[(&str, Option<&str>)],
    main_edits: &[(&str, Option<&str>)],
    feature_edits: &[(&str, Option<&str>)],
) -> (&'static str, &'static str, bool) {
    let fx = Fx::new();
    let c0 = fx.commit(None, base, "c0");
    let on_main = fx.commit(Some(c0), main_edits, "main");
    let feature = fx.commit(Some(c0), feature_edits, "feature");
    fx.remote("main", on_main);
    fx.origin_head("main");
    fx.branch("main", on_main);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("feature", feature);
    let now = fx
        .repo()
        .find_commit(feature)
        .unwrap()
        .committer()
        .unwrap()
        .seconds()
        + DAY;
    let w = W {
        fx,
        now,
        c0,
        u: feature,
    };
    let p = plan(&w);
    let bp = p.branches.iter().find(|b| b.branch == "feature").unwrap();
    let (d, pr) = (bp.decision, bp.proof);
    let dir = scratch();
    apply(&w, dir.path());
    (d, pr, has(&w, "feature"))
}

/// Git's patch ID ignores white space in file content too; a change that differs from main only in white space
/// (here: indentation of a Makefile recipe) is a different change and must keep the branch.
#[test]
fn a_change_that_differs_only_in_white_space_is_kept() {
    let base = [("Makefile", Some("all:\n\tcc x\n"))];
    let (d, proof, exists) = feature_vs_main(
        &base,
        &[("Makefile", Some("all:\n  cc x\n"))],
        &[("Makefile", Some("all:\n\t\tcc   x\n"))],
    );
    assert_eq!((d, exists), ("keep", true), "[{proof}]");
}

/// The same change with the same white space is still recognised, also when main has other commits before it
/// (a rebase of an unchanged commit).
#[test]
fn an_identical_change_with_white_space_is_still_recognised() {
    let fx = Fx::new();
    let c0 = fx.commit(
        None,
        &[("Makefile", Some("all:\n\tcc x\n")), ("o.txt", Some("o\n"))],
        "c0",
    );
    let other = fx.commit(Some(c0), &[("o.txt", Some("o2\n"))], "other");
    let on_main = fx.commit(
        Some(other),
        &[("Makefile", Some("all:\n\t\tcc   x\n"))],
        "main",
    );
    let feature = fx.commit(
        Some(c0),
        &[("Makefile", Some("all:\n\t\tcc   x\n"))],
        "feature",
    );
    fx.remote("main", on_main);
    fx.origin_head("main");
    fx.branch("main", on_main);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("feature", feature);
    let now = fx
        .repo()
        .find_commit(feature)
        .unwrap()
        .committer()
        .unwrap()
        .seconds()
        + DAY;
    let w = W {
        fx,
        now,
        c0,
        u: feature,
    };
    let p = plan(&w);
    let bp = p.branches.iter().find(|b| b.branch == "feature").unwrap();
    assert_eq!((bp.decision, bp.proof), ("delete", "equal-patches"));
}

/// Path digest: a deleted file `a b.txt` is not the deletion of `ab.txt`.
#[test]
fn deleting_a_path_that_differs_only_by_spaces_is_kept() {
    let base = [("a b.txt", Some("x\n")), ("ab.txt", Some("x\n"))];
    let (d, proof, exists) = feature_vs_main(&base, &[("ab.txt", None)], &[("a b.txt", None)]);
    assert_eq!((d, exists), ("keep", true), "[{proof}]");
}

/// Path digest: paths of equal length that differ in the place of the space (`ab ` vs `a b`).
#[test]
fn adding_paths_of_equal_length_that_differ_in_the_space_is_kept() {
    let base = [("base", Some("x\n"))];
    let (d, proof, exists) =
        feature_vs_main(&base, &[("ab ", Some("new\n"))], &[("a b", Some("new\n"))]);
    assert_eq!((d, exists), ("keep", true), "[{proof}]");
}

/// Git hashes the diff lines without separators: the added line `a+b` and the two added lines `a`, `b` give
/// the same patch ID. The exact digest keeps the line borders (newlines and length prefix).
#[test]
fn one_added_line_is_not_two_added_lines_with_the_same_text() {
    let base = [("f.txt", Some("x\n"))];
    let (d, proof, exists) = feature_vs_main(
        &base,
        &[("f.txt", Some("x\na\nb\n"))],
        &[("f.txt", Some("x\na+b\n"))],
    );
    assert_eq!((d, exists), ("keep", true), "[{proof}]");
}
