//! M1: the in-memory replay engine against the installed Git as oracle. Fixtures are built with gix in a scratch
//! repository (`tests/common`); Git only reads them, or works in a clone of them inside the same scratch directory.
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::process::Command;

use crate::common::*;
use gix::bstr::ByteSlice;
use gix::ObjectId;
use stw::replay::{
    configured_committer, conflict_paths, replay_commit, replay_range, ReplayError, Status,
};

fn committer() -> gix::actor::Signature {
    gix::actor::Signature {
        name: "Config Person".into(),
        email: "config@example.com".into(),
        time: gix::date::Time::new(1_800_000_000, 0),
    }
}

/// Git in `dir`, with no inherited configuration or GIT_* variable. `dir` must be inside the fixture scratch.
fn git(dir: &Path, args: &[&str]) -> (i32, String) {
    assert!(!dir.as_os_str().is_empty(), "empty repository path");
    assert!(
        dir.starts_with(std::env::temp_dir()) || std::env::var_os("CLAUDE_JOB_DIR").is_some(),
        "oracle runs only in a scratch directory: {}",
        dir.display()
    );
    let home = dir.parent().unwrap_or(dir);
    let o = Command::new("git")
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap_or_default())
        .env("HOME", home)
        .env("LC_ALL", "C")
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_EDITOR", "true")
        .env("GIT_COMMITTER_NAME", "Config Person")
        .env("GIT_COMMITTER_EMAIL", "config@example.com")
        .env("GIT_COMMITTER_DATE", "1800000000 +0000")
        .current_dir(dir)
        .args(args)
        .output()
        .unwrap();
    (
        o.status.code().unwrap_or(-1),
        String::from_utf8_lossy(&o.stdout).into_owned(),
    )
}

fn must(dir: &Path, args: &[&str]) -> String {
    let (code, out) = git(dir, args);
    assert_eq!(code, 0, "git {args:?} failed in {}", dir.display());
    out.trim_end().to_string()
}

/// What the oracle produces for `git rebase --onto onto old_base tip`: the tree of the result and one line per
/// replayed commit (author, author date, committer, subject), oldest first.
struct Oracle {
    tree: String,
    log: Vec<String>,
}

fn oracle_rebase(fx: &Fx, onto: ObjectId, old_base: ObjectId, tip: ObjectId) -> Oracle {
    let scratch = scratch();
    let clone = scratch.path().join("clone");
    let out = Command::new("git")
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap_or_default())
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .args(["clone", "-q", "--no-hardlinks", "--no-checkout"])
        .arg(&fx.path)
        .arg(&clone)
        .output()
        .unwrap();
    assert!(out.status.success(), "clone failed");
    must(&clone, &["checkout", "-q", "--detach", &tip.to_string()]);
    must(
        &clone,
        &[
            "rebase",
            "-q",
            "--onto",
            &onto.to_string(),
            &old_base.to_string(),
        ],
    );
    let tree = must(&clone, &["rev-parse", "HEAD^{tree}"]);
    let log = must(
        &clone,
        &[
            "log",
            "--reverse",
            "--format=%an <%ae> %ad|%cn <%ce> %cd|%s",
            "--date=raw",
            &format!("{onto}..HEAD"),
        ],
    )
    .lines()
    .map(str::to_string)
    .collect();
    Oracle { tree, log }
}

/// Conflicted paths and tree as `git merge-tree --write-tree --name-only` reports them (a copy in the scratch).
fn oracle_merge_tree(
    fx: &Fx,
    base: ObjectId,
    ours: ObjectId,
    theirs: ObjectId,
) -> (i32, Vec<String>) {
    let scratch = scratch();
    let clone = scratch.path().join("clone");
    let out = Command::new("git")
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap_or_default())
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .args(["clone", "-q", "--no-hardlinks", "--no-checkout"])
        .arg(&fx.path)
        .arg(&clone)
        .output()
        .unwrap();
    assert!(out.status.success(), "clone failed");
    let (code, text) = git(
        &clone,
        &[
            "merge-tree",
            "--write-tree",
            "--name-only",
            "--no-messages",
            &format!("--merge-base={base}"),
            &ours.to_string(),
            &theirs.to_string(),
        ],
    );
    let mut paths: Vec<String> = text.lines().skip(1).map(str::to_string).collect();
    paths.sort();
    paths.dedup();
    (code, paths)
}

/// The engine's view in the same shape as the oracle's.
fn engine_log(r: &stw::replay::Replay, onto: ObjectId) -> (String, Vec<String>) {
    let mut lines = Vec::new();
    let mut at = r.tip;
    while at != onto {
        let c = r.repo.find_commit(at).unwrap();
        let d = c.decode().unwrap();
        let a = d.author().unwrap();
        let k = d.committer().unwrap();
        lines.push(format!(
            "{} <{}> {} {}|{} <{}> {} {}|{}",
            a.name,
            a.email,
            a.time().unwrap().seconds,
            tz(a.time().unwrap().offset),
            k.name,
            k.email,
            k.time().unwrap().seconds,
            tz(k.time().unwrap().offset),
            d.message.to_str_lossy().lines().next().unwrap_or("")
        ));
        at = c.parent_ids().next().unwrap().detach();
    }
    lines.reverse();
    let tree = r
        .repo
        .find_commit(r.tip)
        .unwrap()
        .tree_id()
        .unwrap()
        .to_string();
    (tree, lines)
}

fn tz(offset: i32) -> String {
    format!(
        "{}{:02}{:02}",
        if offset < 0 { '-' } else { '+' },
        offset.abs() / 3600,
        offset.abs() % 3600 / 60
    )
}

/// Compare the engine with `git rebase --onto` on the same fixture.
fn assert_matches_git(
    fx: &Fx,
    onto: ObjectId,
    old_base: ObjectId,
    tip: ObjectId,
) -> stw::replay::Replay {
    let r = replay_range(&fx.repo(), onto, old_base, tip, &committer()).unwrap();
    assert!(!r.conflicted(), "{:?}", r.steps);
    let want = oracle_rebase(fx, onto, old_base, tip);
    let (tree, log) = engine_log(&r, onto);
    assert_eq!(tree, want.tree, "tree differs from git");
    assert_eq!(log, want.log, "commits differ from git");
    r
}

fn statuses(r: &stw::replay::Replay) -> Vec<Status> {
    r.steps.iter().map(|s| s.status).collect()
}

fn base_repo(fx: &Fx) -> ObjectId {
    fx.commit(
        None,
        &[
            ("a.txt", Some(&lines(30, "x"))),
            ("b.txt", Some(&lines(30, "y"))),
            ("keep.txt", Some("keep\n")),
        ],
        "base",
    )
}

fn edit(text: &str, line: usize, with: &str) -> String {
    text.replace(
        &format!("{}\n", text.lines().nth(line - 1).unwrap()),
        &format!("{with}\n"),
    )
}

#[test]
fn clean_replay_equals_git() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(
        Some(base),
        &[("b.txt", Some(&edit(&lines(30, "y"), 3, "ONTO")))],
        "onto",
    );
    let s1 = fx.commit(
        Some(base),
        &[("a.txt", Some(&edit(&lines(30, "x"), 20, "S1")))],
        "side 1",
    );
    let s2 = fx.commit(Some(s1), &[("c.txt", Some("new\n"))], "side 2");
    let r = assert_matches_git(&fx, onto, base, s2);
    assert_eq!(statuses(&r), [Status::Clean, Status::Clean]);
    assert!(r.steps.iter().all(|s| s.new.is_some()));
}

#[test]
fn rename_on_the_new_side_follows_the_edit() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(
        Some(base),
        &[("a.txt", None), ("moved.txt", Some(&lines(30, "x")))],
        "rename a to moved",
    );
    let side = fx.commit(
        Some(base),
        &[("a.txt", Some(&edit(&lines(30, "x"), 15, "EDIT")))],
        "edit a",
    );
    let r = assert_matches_git(&fx, onto, base, side);
    let tree = r.repo.find_commit(r.tip).unwrap().tree().unwrap();
    let moved = tree.find_entry("moved.txt").expect("renamed file exists");
    let blob = moved.object().unwrap().data.clone();
    assert!(
        blob.to_str_lossy().contains("EDIT"),
        "the edit followed the rename"
    );
    assert!(tree.find_entry("a.txt").is_none());
}

#[test]
fn binary_change_equals_git() {
    let fx = Fx::new();
    let base = fx.commit(
        None,
        &[("p.bin", Some("a\0b\0c")), ("t.txt", Some("t\n"))],
        "base",
    );
    let onto = fx.commit(Some(base), &[("t.txt", Some("t2\n"))], "onto");
    let side = fx.commit(Some(base), &[("p.bin", Some("a\0B\0c"))], "side binary");
    let r = assert_matches_git(&fx, onto, base, side);
    assert_eq!(statuses(&r), [Status::Clean]);
}

#[test]
fn empty_start_commit_is_kept_like_git() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(
        Some(base),
        &[("b.txt", Some(&edit(&lines(30, "y"), 3, "ONTO")))],
        "onto",
    );
    let e = fx.commit(Some(base), &[], "empty from the start");
    let s = fx.commit(
        Some(e),
        &[("a.txt", Some(&edit(&lines(30, "x"), 2, "S")))],
        "side",
    );
    let r = assert_matches_git(&fx, onto, base, s);
    assert_eq!(statuses(&r), [Status::EmptyStart, Status::Clean]);
}

#[test]
fn commit_that_became_empty_is_dropped_like_git() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    // onto makes the same change on line 5 and one more further down: the patch differs, the result is the same
    let a = edit(&lines(30, "x"), 5, "SAME");
    let onto = fx.commit(
        Some(base),
        &[("a.txt", Some(&edit(&a, 12, "MORE")))],
        "onto",
    );
    let side = fx.commit(Some(base), &[("a.txt", Some(&a))], "side");
    let tail = fx.commit(Some(side), &[("c.txt", Some("c\n"))], "tail");
    let r = assert_matches_git(&fx, onto, base, tail);
    assert_eq!(statuses(&r), [Status::Empty, Status::Clean]);
    assert_eq!(r.steps[0].new, None);
}

#[test]
fn patch_already_in_onto_is_dropped_as_empty_like_git() {
    // The same patch is in onto: git skips the commit by patch ID, the engine drops it because the merge
    // changes nothing. Both end with the same tree and commits.
    let fx = Fx::new();
    let base = base_repo(&fx);
    let change = edit(&lines(30, "x"), 20, "SAME");
    let o1 = fx.commit(Some(base), &[("c.txt", Some("c\n"))], "onto 1");
    let onto = fx.commit(Some(o1), &[("a.txt", Some(&change))], "same patch on onto");
    let side = fx.commit(Some(base), &[("a.txt", Some(&change))], "same patch");
    let s2 = fx.commit(
        Some(side),
        &[("b.txt", Some(&edit(&lines(30, "y"), 25, "S2")))],
        "side 2",
    );
    let r = assert_matches_git(&fx, onto, base, s2);
    assert_eq!(statuses(&r), [Status::Empty, Status::Clean]);
}

#[test]
fn a_patch_that_onto_made_and_later_reverted_is_applied_like_git() {
    // onto holds the patch and, later, its revert. The change is not in onto any more, so it has to come
    // through (a skip by patch ID would lose it).
    let fx = Fx::new();
    let base = base_repo(&fx);
    let change = edit(&lines(30, "x"), 20, "SAME");
    let o1 = fx.commit(Some(base), &[("a.txt", Some(&change))], "onto: the patch");
    let onto = fx.commit(
        Some(o1),
        &[("a.txt", Some(&lines(30, "x")))],
        "onto: revert",
    );
    let side = fx.commit(Some(base), &[("a.txt", Some(&change))], "side: the patch");
    let r = assert_matches_git(&fx, onto, base, side);
    assert_eq!(statuses(&r), [Status::Clean]);
    let tree = r.repo.find_commit(r.tip).unwrap().tree().unwrap();
    let blob = tree
        .find_entry("a.txt")
        .unwrap()
        .object()
        .unwrap()
        .data
        .clone();
    assert!(blob.to_str_lossy().contains("SAME"), "the change was lost");
}

#[test]
fn root_commit_in_the_range_is_refused() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(Some(base), &[("c.txt", Some("c\n"))], "onto");
    let other_root = fx.commit(None, &[("z.txt", Some("z\n"))], "unrelated root");
    let tip = fx.commit(Some(other_root), &[("z.txt", Some("zz\n"))], "tip");
    let e = replay_range(&fx.repo(), onto, base, tip, &committer())
        .err()
        .unwrap();
    assert!(
        matches!(e, ReplayError::RootCommit(c) if c == other_root),
        "{e}"
    );
    let e = replay_commit(&fx.repo(), other_root, onto, &committer())
        .err()
        .unwrap();
    assert!(matches!(e, ReplayError::RootCommit(_)), "{e}");
}

#[test]
fn merge_commit_in_the_range_is_refused_before_anything_is_replayed() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(Some(base), &[("c.txt", Some("c\n"))], "onto");
    let s1 = fx.commit(
        Some(base),
        &[("a.txt", Some(&edit(&lines(30, "x"), 2, "S1")))],
        "s1",
    );
    let s2 = fx.commit(
        Some(base),
        &[("b.txt", Some(&edit(&lines(30, "y"), 2, "S2")))],
        "s2",
    );
    let m = fx.commit_with_parents(
        vec![s1, s2],
        Some(s1),
        &[("b.txt", Some(&edit(&lines(30, "y"), 2, "S2")))],
        "merge",
    );
    let e = replay_range(&fx.repo(), onto, base, m, &committer())
        .err()
        .unwrap();
    assert!(matches!(e, ReplayError::MergeCommit(c) if c == m), "{e}");
    let e = replay_commit(&fx.repo(), m, onto, &committer())
        .err()
        .unwrap();
    assert!(matches!(e, ReplayError::MergeCommit(c) if c == m), "{e}");
}

/// Conflicted paths equal those of `git merge-tree`, and the replay stops at the conflict.
fn conflict_case(fx: &Fx, base: ObjectId, onto: ObjectId, side: ObjectId, kind: &str) {
    let tail = fx.commit(Some(side), &[("tail.txt", Some("t\n"))], "tail");
    let (code, want) = oracle_merge_tree(fx, base, onto, side);
    assert_eq!(code, 1, "git sees a conflict");
    let r = replay_range(&fx.repo(), onto, base, tail, &committer()).unwrap();
    assert!(r.conflicted());
    assert_eq!(r.steps.len(), 1, "the replay stops at the first conflict");
    let step = &r.steps[0];
    assert_eq!(step.new, None);
    let mut got = conflict_paths(step);
    got.sort();
    assert_eq!(got, want, "conflicted paths differ from git");
    assert!(
        step.conflicts.iter().all(|c| c.kind == kind),
        "{:?}",
        step.conflicts
    );
}

#[test]
fn content_conflict_matches_git() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(
        Some(base),
        &[("a.txt", Some(&edit(&lines(30, "x"), 10, "ONTO")))],
        "onto",
    );
    let side = fx.commit(
        Some(base),
        &[("a.txt", Some(&edit(&lines(30, "x"), 10, "SIDE")))],
        "side",
    );
    conflict_case(&fx, base, onto, side, "content");
}

#[test]
fn add_add_conflict_matches_git() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(Some(base), &[("n.txt", Some("one\n"))], "onto");
    let side = fx.commit(Some(base), &[("n.txt", Some("two\n"))], "side");
    conflict_case(&fx, base, onto, side, "add/add");
}

#[test]
fn modify_delete_conflict_matches_git() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(Some(base), &[("keep.txt", None)], "onto deletes");
    let side = fx.commit(
        Some(base),
        &[("keep.txt", Some("changed\n"))],
        "side modifies",
    );
    conflict_case(&fx, base, onto, side, "modify/delete");
}

#[test]
fn binary_conflict_matches_git() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("p.bin", Some("a\0b\0c"))], "base");
    let onto = fx.commit(Some(base), &[("p.bin", Some("a\0ONTO\0c"))], "onto");
    let side = fx.commit(Some(base), &[("p.bin", Some("a\0SIDE\0c"))], "side");
    conflict_case(&fx, base, onto, side, "content");
}

#[test]
fn a_file_added_to_a_directory_that_onto_renamed_is_a_conflict_like_git() {
    // git moves the new file into the renamed directory or reports a conflict (merge.directoryRenames=conflict
    // as the Go restack pins it); the tree merge of gix does neither and would recreate the old directory.
    let fx = Fx::new();
    let base = fx.commit(
        None,
        &[
            ("dir/x.txt", Some(&lines(10, "x"))),
            ("dir/y.txt", Some(&lines(10, "y"))),
            ("keep.txt", Some("keep\n")),
        ],
        "base",
    );
    let onto = fx.commit(
        Some(base),
        &[
            ("dir/x.txt", None),
            ("dir/y.txt", None),
            ("newdir/x.txt", Some(&lines(10, "x"))),
            ("newdir/y.txt", Some(&lines(10, "y"))),
        ],
        "rename dir to newdir",
    );
    let side = fx.commit(
        Some(base),
        &[("dir/new.txt", Some("new\n"))],
        "add dir/new.txt",
    );
    let (code, theirs) = oracle_merge_tree(&fx, base, onto, side);
    assert_eq!(code, 1, "git sees a conflict: {theirs:?}");
    let r = replay_range(&fx.repo(), onto, base, side, &committer()).unwrap();
    assert!(r.conflicted(), "{:?}", r.steps);
    let c = &r.steps[0].conflicts;
    assert_eq!(c.len(), 1);
    assert_eq!(c[0].kind, "directory-rename");
    assert!(c[0].path.ends_with(b"new.txt"), "{c:?}");
}

#[test]
fn a_file_added_to_a_directory_that_onto_deleted_is_not_a_conflict_like_git() {
    let fx = Fx::new();
    let base = fx.commit(
        None,
        &[("dir/x.txt", Some("x\n")), ("keep.txt", Some("keep\n"))],
        "base",
    );
    let onto = fx.commit(Some(base), &[("dir/x.txt", None)], "delete dir");
    let side = fx.commit(
        Some(base),
        &[("dir/new.txt", Some("new\n"))],
        "add dir/new.txt",
    );
    let r = assert_matches_git(&fx, onto, base, side);
    assert_eq!(statuses(&r), [Status::Clean]);
}

/// Like `conflict_case`, for kinds whose paths differ from `git merge-tree` (named in the module doc): git sees a
/// conflict, the kind is the expected one and the engine's paths are exactly `want`.
fn conflict_kind_case(
    fx: &Fx,
    base: ObjectId,
    onto: ObjectId,
    side: ObjectId,
    kind: &str,
    want: &[&str],
) {
    let (code, theirs) = oracle_merge_tree(fx, base, onto, side);
    assert_eq!(code, 1, "git sees a conflict: {theirs:?}");
    let r = replay_range(&fx.repo(), onto, base, side, &committer()).unwrap();
    assert!(r.conflicted(), "{:?}", r.steps);
    let step = &r.steps[0];
    assert!(
        step.conflicts.iter().all(|c| c.kind == kind),
        "{:?}",
        step.conflicts
    );
    let mut got = conflict_paths(step);
    got.sort();
    assert_eq!(got, want, "git says {theirs:?}");
}

#[test]
fn rename_rename_conflict_is_reported_like_git() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let a = lines(30, "x");
    let onto = fx.commit(
        Some(base),
        &[("a.txt", None), ("x.txt", Some(&a))],
        "onto renames to x",
    );
    let side = fx.commit(
        Some(base),
        &[("a.txt", None), ("y.txt", Some(&a))],
        "side renames to y",
    );
    conflict_kind_case(&fx, base, onto, side, "rename/rename", &["x.txt", "y.txt"]);
}

#[test]
fn rename_delete_conflict_is_reported_like_git() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(Some(base), &[("a.txt", None)], "onto deletes a");
    let side = fx.commit(
        Some(base),
        &[("a.txt", None), ("y.txt", Some(&lines(30, "x")))],
        "side renames a to y",
    );
    conflict_kind_case(&fx, base, onto, side, "rename/delete", &["a.txt", "y.txt"]);
}

#[test]
fn file_directory_conflict_is_reported_like_git() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(
        Some(base),
        &[("d", Some("a file\n"))],
        "onto adds the file d",
    );
    let side = fx.commit(
        Some(base),
        &[("d/f", Some("in a directory\n"))],
        "side adds d/f",
    );
    conflict_kind_case(&fx, base, onto, side, "file/directory", &["d", "d/f"]);
}

/// Every file under `.git` with its bytes, to compare before and after.
fn snapshot(dir: &Path) -> BTreeMap<PathBuf, Vec<u8>> {
    fn walk(root: &Path, dir: &Path, out: &mut BTreeMap<PathBuf, Vec<u8>>) {
        for e in std::fs::read_dir(dir).unwrap() {
            let p = e.unwrap().path();
            if p.is_dir() {
                walk(root, &p, out);
            } else {
                out.insert(
                    p.strip_prefix(root).unwrap().to_path_buf(),
                    std::fs::read(&p).unwrap(),
                );
            }
        }
    }
    let mut out = BTreeMap::new();
    walk(dir, dir, &mut out);
    out
}

#[test]
fn nothing_is_written_to_the_repository_and_the_new_objects_live_in_memory() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(
        Some(base),
        &[("b.txt", Some(&edit(&lines(30, "y"), 3, "ONTO")))],
        "onto",
    );
    let s1 = fx.commit(
        Some(base),
        &[("a.txt", Some(&edit(&lines(30, "x"), 20, "S1")))],
        "s1",
    );
    let s2 = fx.commit(Some(s1), &[("c.txt", Some("new\n"))], "s2");
    fx.branch("main", onto);
    fx.branch("side", s2);
    fx.set_head("main");
    fx.checkout_head();
    let before = snapshot(&fx.path);
    let r = replay_range(&fx.repo(), onto, base, s2, &committer()).unwrap();
    assert_eq!(statuses(&r), [Status::Clean, Status::Clean]);
    let after = snapshot(&fx.path);
    assert_eq!(
        before.keys().collect::<Vec<_>>(),
        after.keys().collect::<Vec<_>>(),
        "the set of files under .git (objects, refs, index, logs) is unchanged"
    );
    assert!(before == after, "a file under .git changed");
    // readable through the engine's handle, absent for everyone else
    assert!(r.repo.find_commit(r.tip).is_ok());
    assert!(
        fx.repo().find_commit(r.tip).is_err(),
        "the replayed commit was written to the object database"
    );
    // the on-disk repository is still consistent for git
    must(&fx.path, &["fsck", "--no-dangling"]);
    assert!(fx.exists("a.txt"));
}

#[test]
fn replayed_commit_keeps_the_author_and_takes_the_configured_committer() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let onto = fx.commit(
        Some(base),
        &[("b.txt", Some(&edit(&lines(30, "y"), 3, "ONTO")))],
        "onto",
    );
    // a commit with its own author, an encoding header, an odd message and a signature header
    let repo = fx.repo();
    let tree = repo.find_commit(base).unwrap().tree_id().unwrap().detach();
    let mut ed = repo.edit_tree(tree).unwrap();
    let blob = repo
        .write_blob(edit(&lines(30, "x"), 20, "S").as_bytes())
        .unwrap()
        .detach();
    ed.upsert("a.txt", gix::objs::tree::EntryKind::Blob, blob)
        .unwrap();
    let tree = ed.write().unwrap().detach();
    let commit = gix::objs::Commit {
        tree,
        parents: vec![base].into(),
        author: gix::actor::Signature {
            name: "Original Author".into(),
            email: "orig@example.org".into(),
            time: gix::date::Time::new(1_650_000_000, 3600),
        },
        committer: gix::actor::Signature {
            name: "Old Committer".into(),
            email: "old@example.org".into(),
            time: gix::date::Time::new(1_650_000_100, 0),
        },
        encoding: Some("ISO-8859-1".into()),
        message: "subject\n\nbody without final newline".into(),
        extra_headers: vec![("gpgsig".into(), "sig".into())],
    };
    let side = repo.write_object(&commit).unwrap().detach();
    let r = replay_range(&fx.repo(), onto, base, side, &committer()).unwrap();
    let new = r.steps[0].new.expect("replayed");
    assert_ne!(new, side);
    let c = r.repo.find_commit(new).unwrap();
    let d = c.decode().unwrap();
    let a = d.author().unwrap();
    assert_eq!(
        (a.name.to_str().unwrap(), a.email.to_str().unwrap()),
        ("Original Author", "orig@example.org")
    );
    assert_eq!(
        (a.time().unwrap().seconds, a.time().unwrap().offset),
        (1_650_000_000, 3600)
    );
    let k = d.committer().unwrap();
    assert_eq!(
        (k.name.to_str().unwrap(), k.email.to_str().unwrap()),
        ("Config Person", "config@example.com")
    );
    assert_eq!(k.time().unwrap().seconds, 1_800_000_000);
    assert_eq!(d.message, b"subject\n\nbody without final newline" as &[u8]);
    assert_eq!(
        d.encoding.map(|e| e.to_str().unwrap().to_string()),
        Some("ISO-8859-1".into())
    );
    assert!(
        d.extra_headers.is_empty(),
        "a signature does not survive a rewrite"
    );
}

#[test]
fn rerun_over_a_replayed_range_returns_the_original_commits() {
    let fx = Fx::new();
    let base = base_repo(&fx);
    let s1 = fx.commit(
        Some(base),
        &[("a.txt", Some(&edit(&lines(30, "x"), 20, "S1")))],
        "s1",
    );
    let s2 = fx.commit(Some(s1), &[("c.txt", Some("new\n"))], "s2");
    // onto = base: nothing moves, so tree and parent stay and the originals come back
    let r = replay_range(&fx.repo(), base, base, s2, &committer()).unwrap();
    assert_eq!(statuses(&r), [Status::Unchanged, Status::Unchanged]);
    assert_eq!(r.tip, s2);
    assert_eq!(r.steps[0].new, Some(s1));
}

#[test]
fn a_merge_driver_of_the_configuration_is_never_started() {
    let fx = Fx::new();
    let marker = fx.path.parent().unwrap().join("driver-ran");
    let base = fx.commit(
        None,
        &[
            (".gitattributes", Some("* merge=evil\n")),
            ("a.txt", Some(&lines(30, "x"))),
        ],
        "base",
    );
    use std::io::Write;
    let mut cfg = std::fs::OpenOptions::new()
        .append(true)
        .open(fx.path.join(".git/config"))
        .unwrap();
    write!(
        cfg,
        "[merge \"evil\"]\n\tname = evil\n\tdriver = touch {} %A\n",
        marker.display()
    )
    .unwrap();
    drop(cfg);
    fx.branch("main", base);
    fx.set_head("main");
    fx.checkout_head();
    let onto = fx.commit(
        Some(base),
        &[("a.txt", Some(&edit(&lines(30, "x"), 10, "ONTO")))],
        "onto",
    );
    let side = fx.commit(
        Some(base),
        &[("a.txt", Some(&edit(&lines(30, "x"), 10, "SIDE")))],
        "side",
    );
    let r = replay_range(&fx.repo(), onto, base, side, &committer()).unwrap();
    assert!(!marker.exists(), "the merge driver ran");
    assert!(r.conflicted(), "the built-in merge decided: {:?}", r.steps);
}

#[test]
fn committer_comes_from_the_config_only() {
    let now = gix::date::Time::new(1_700_000_000, 0);
    let fx = Fx::new();
    let c = configured_committer(&fx.repo(), now).unwrap();
    assert_eq!(
        (c.name.to_str().unwrap(), c.email.to_str().unwrap()),
        ("T", "t@example.com")
    );
    assert_eq!(c.time.seconds, 1_700_000_000);
    // committer.* beats user.*
    {
        use std::io::Write;
        let mut cfg = std::fs::OpenOptions::new()
            .append(true)
            .open(fx.path.join(".git/config"))
            .unwrap();
        write!(cfg, "[committer]\n\tname = C\n\temail = c@example.com\n").unwrap();
    }
    let c = configured_committer(&fx.repo(), now).unwrap();
    assert_eq!(c.name.to_str().unwrap(), "C");
    // none configured: the fallback of `repo::open` does not count
    let fx = Fx::new();
    fx.strip_identity();
    // isolated: neither the global Git config of whoever runs the test nor the environment is read
    let mut repo = gix::open_opts(&fx.path, gix::open::Options::isolated()).unwrap();
    repo.committer_or_set_fallback("stw", "stw@localhost")
        .unwrap();
    let e = configured_committer(&repo, now).err().unwrap();
    assert!(matches!(e, ReplayError::NoIdentity), "{e}");
}

/// Runs in the child process of `committer_ignores_the_environment`; does nothing in a normal test run.
#[test]
fn env_child() {
    let Some(cases) = std::env::var_os("STW_REPLAY_ENV_CASES") else {
        return;
    };
    let cases = cases.to_string_lossy().into_owned();
    let (with_config, without) = cases.split_once('|').unwrap();
    let now = gix::date::Time::new(1_700_000_000, 0);
    assert_eq!(std::env::var("GIT_COMMITTER_NAME").unwrap(), "EnvName");
    let c = configured_committer(&gix::open(with_config).unwrap(), now).unwrap();
    assert_eq!(
        c.name.to_str().unwrap(),
        "T",
        "the environment beat the config"
    );
    let e = configured_committer(&gix::open(without).unwrap(), now)
        .expect_err("the environment is not a configured identity");
    assert!(matches!(e, ReplayError::NoIdentity), "{e}");
}

#[test]
fn committer_ignores_the_environment() {
    let with_config = Fx::new();
    let without = Fx::new();
    without.strip_identity();
    let home = scratch();
    let out = Command::new(std::env::current_exe().unwrap())
        .env_clear()
        .env("HOME", home.path())
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_COMMITTER_NAME", "EnvName")
        .env("GIT_COMMITTER_EMAIL", "env@example.com")
        .env(
            "STW_REPLAY_ENV_CASES",
            format!("{}|{}", with_config.path.display(), without.path.display()),
        )
        .args(["replay::env_child", "--exact", "--nocapture"])
        .output()
        .unwrap();
    let text =
        String::from_utf8_lossy(&out.stdout).into_owned() + &String::from_utf8_lossy(&out.stderr);
    assert!(out.status.success(), "{text}");
    assert!(
        text.contains("1 passed"),
        "the child test did not run: {text}"
    );
}
