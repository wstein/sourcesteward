mod common;
use common::*;
use stw::repo::RepoRef;
use stw::status;
use stw::sync::fast_forward::{fast_forward, put_ref_back, FfError};
use stw::sync::{sync_repo, Opts};

struct S {
    fx: Fx,
    old: gix::ObjectId,
    new: gix::ObjectId,
}

/// main at c0 (checked out), origin/main two commits ahead: modifies a.txt, adds dir/new.txt, deletes old.txt.
fn setup() -> S {
    let fx = Fx::new();
    let c0 = fx.commit(
        None,
        &[
            ("a.txt", Some(&lines(10, "a"))),
            ("old.txt", Some("old\n")),
            ("keep.txt", Some("keep\n")),
            ("staged.txt", Some("staged\n")),
            ("sub/gone.txt", Some("gone\n")),
        ],
        "c0",
    );
    let c1 = fx.commit(
        Some(c0),
        &[
            ("a.txt", Some(&lines(10, "a").replace("a5\n", "A5\n"))),
            ("old.txt", None),
        ],
        "c1",
    );
    let c2 = fx.commit(
        Some(c1),
        &[("dir/new.txt", Some("new\n")), ("sub/gone.txt", None)],
        "c2",
    );
    fx.branch("main", c0);
    fx.set_head("main");
    fx.remote("main", c2);
    fx.origin_head("main");
    fx.track("main");
    fx.checkout_head();
    S {
        fx,
        old: c0,
        new: c2,
    }
}

fn repo_ref(s: &S) -> RepoRef {
    RepoRef {
        name: "fx".into(),
        path: s.fx.path.clone(),
    }
}

fn unchanged(s: &S) {
    assert_eq!(s.fx.tip("main"), s.old, "ref must not move");
}

#[test]
fn clean_fast_forward_updates_files_index_and_ref() {
    let s = setup();
    let (a, b) = fast_forward(&s.fx.repo(), s.new, "origin/main").unwrap();
    assert_eq!((a, b), (s.old, s.new));
    assert_eq!(s.fx.tip("main"), s.new);
    assert_eq!(
        s.fx.read("a.txt").unwrap(),
        lines(10, "a").replace("a5\n", "A5\n")
    );
    assert_eq!(s.fx.read("dir/new.txt").as_deref(), Some("new\n"));
    assert!(!s.fx.exists("old.txt"));
    assert!(
        !s.fx.exists("sub"),
        "directory emptied by the deletion is removed"
    );
    // index and worktree agree with HEAD: a fresh status sees nothing
    let c = status::changes(&s.fx.repo(), true).unwrap();
    assert_eq!(
        (c.staged, c.unstaged, c.untracked, c.conflicts),
        (0, 0, 0, 0),
        "{c:?}"
    );
    // reflog of the branch records the fast-forward
    let log = std::fs::read_to_string(s.fx.path.join(".git/logs/refs/heads/main")).unwrap();
    assert!(log.contains("merge origin/main: Fast-forward"), "{log}");
}

#[test]
fn dirty_files_that_the_update_does_not_touch_are_kept() {
    let s = setup();
    s.fx.write("keep.txt", "my local edit\n");
    s.fx.write("staged.txt", "my staged edit\n");
    s.fx.stage("staged.txt");
    fast_forward(&s.fx.repo(), s.new, "origin/main").unwrap();
    assert_eq!(s.fx.tip("main"), s.new);
    assert_eq!(s.fx.read("keep.txt").unwrap(), "my local edit\n");
    assert_eq!(s.fx.read("staged.txt").unwrap(), "my staged edit\n");
    let c = status::changes(&s.fx.repo(), false).unwrap();
    assert_eq!(
        (c.unstaged, c.staged),
        (1, 1),
        "local work is still local work: {c:?}"
    );
}

#[test]
fn unstaged_change_on_a_touched_file_is_an_error_without_any_change() {
    let s = setup();
    s.fx.write("a.txt", "local edit\n");
    let err = fast_forward(&s.fx.repo(), s.new, "origin/main").unwrap_err();
    assert!(
        matches!(&err, FfError::Overlap(v) if v == &["a.txt".to_string()]),
        "{err}"
    );
    unchanged(&s);
    assert_eq!(s.fx.read("a.txt").unwrap(), "local edit\n");
    assert!(
        !s.fx.exists("dir/new.txt"),
        "nothing is written when the check fails"
    );
    assert!(s.fx.exists("old.txt"));
}

#[test]
fn staged_change_on_a_touched_file_is_an_error_without_any_change() {
    let s = setup();
    s.fx.write("old.txt", "staged rewrite\n");
    s.fx.stage("old.txt");
    // worktree equals index here, so only the index differs from HEAD
    let err = fast_forward(&s.fx.repo(), s.new, "origin/main").unwrap_err();
    assert!(
        matches!(&err, FfError::Overlap(v) if v == &["old.txt".to_string()]),
        "{err}"
    );
    unchanged(&s);
    assert_eq!(s.fx.read("old.txt").unwrap(), "staged rewrite\n");
}

#[test]
fn locally_deleted_file_that_upstream_touches_is_an_error() {
    let s = setup();
    std::fs::remove_file(s.fx.path.join("a.txt")).unwrap();
    let err = fast_forward(&s.fx.repo(), s.new, "origin/main").unwrap_err();
    assert!(matches!(err, FfError::Overlap(_)), "{err}");
    unchanged(&s);
}

#[test]
fn untracked_file_in_the_way_of_an_added_file_is_an_error() {
    let s = setup();
    s.fx.write("dir/new.txt", "mine\n");
    let err = fast_forward(&s.fx.repo(), s.new, "origin/main").unwrap_err();
    assert!(
        matches!(&err, FfError::UntrackedInTheWay(v) if v == &["dir/new.txt".to_string()]),
        "{err}"
    );
    unchanged(&s);
    assert_eq!(s.fx.read("dir/new.txt").unwrap(), "mine\n");
}

#[test]
fn sync_apply_reports_fast_forwarded_with_before_and_after() {
    let s = setup();
    let rec = sync_repo(
        &repo_ref(&s),
        None,
        &Opts {
            apply: true,
            offline: true,
        },
    );
    assert_eq!(rec.result, "fast-forwarded", "{rec:?}");
    assert_eq!(rec.reason, format!("{}..{}", s.old, s.new));
    assert_eq!(s.fx.tip("main"), s.new);
}

#[test]
fn sync_apply_overlap_is_result_error_and_changes_nothing() {
    let s = setup();
    s.fx.write("a.txt", "local edit\n");
    let rec = sync_repo(
        &repo_ref(&s),
        None,
        &Opts {
            apply: true,
            offline: true,
        },
    );
    assert_eq!(rec.result, "error", "{rec:?}");
    assert!(rec.err.as_deref().unwrap().contains("a.txt"), "{rec:?}");
    unchanged(&s);
}

#[test]
fn diverged_is_skipped_even_with_apply() {
    let s = setup();
    let local =
        s.fx.commit(Some(s.old), &[("local.txt", Some("l\n"))], "local work");
    s.fx.branch("main", local);
    s.fx.checkout_head();
    let rec = sync_repo(
        &repo_ref(&s),
        None,
        &Opts {
            apply: true,
            offline: true,
        },
    );
    assert_eq!(rec.result, "skip", "{rec:?}");
    assert!(
        rec.reason.starts_with("diverged: ahead 1, behind 2"),
        "{}",
        rec.reason
    );
    assert_eq!(s.fx.tip("main"), local);
}

#[test]
fn dry_run_writes_nothing() {
    let s = setup();
    let rec = sync_repo(
        &repo_ref(&s),
        None,
        &Opts {
            apply: false,
            offline: true,
        },
    );
    assert_eq!(rec.result, "fast-forward", "{rec:?}");
    assert_eq!(rec.behind, Some(2));
    unchanged(&s);
    assert!(s.fx.exists("old.txt"));
}

/// c0 (checked out) -> c1 built by `edits`; origin/main = c1, main tracks it.
fn one_step(c0_files: &[(&str, Option<&str>)], edits: &[(&str, Option<&str>)]) -> S {
    let fx = Fx::new();
    let c0 = fx.commit(None, c0_files, "c0");
    let c1 = fx.commit(Some(c0), edits, "c1");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.track("main");
    fx.checkout_head();
    S {
        fx,
        old: c0,
        new: c1,
    }
}

#[test]
fn a_file_in_the_way_of_a_new_directory_is_an_error() {
    let s = setup();
    s.fx.write("dir", "a plain file where upstream wants a directory\n");
    let err = fast_forward(&s.fx.repo(), s.new, "origin/main").unwrap_err();
    assert!(
        matches!(&err, FfError::UntrackedInTheWay(v) if v == &["dir/new.txt".to_string()]),
        "{err}"
    );
    unchanged(&s);
    assert_eq!(
        s.fx.read("dir").unwrap(),
        "a plain file where upstream wants a directory\n"
    );
}

#[test]
fn a_tracked_file_that_becomes_a_directory_is_reported_as_that() {
    let s = one_step(
        &[("a", Some("file\n"))],
        &[("a", None), ("a/z", Some("z\n"))],
    );
    let err = fast_forward(&s.fx.repo(), s.new, "origin/main").unwrap_err();
    let msg = err.to_string();
    assert!(msg.contains("file and directory"), "{msg}");
    assert!(!msg.contains("untracked"), "wrong diagnosis: {msg}");
    unchanged(&s);
    assert_eq!(s.fx.read("a").unwrap(), "file\n");
}

#[test]
fn a_new_submodule_leaves_its_empty_directory_like_git_does() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    let c1 = fx.commit_gitlink(c0, "sm", c0, "add submodule");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.track("main");
    fx.checkout_head();
    fast_forward(&fx.repo(), c1, "origin/main").unwrap();
    assert!(
        fx.path.join("sm").is_dir(),
        "the submodule directory must exist"
    );
    let ch = status::changes(&fx.repo(), true).unwrap();
    assert_eq!((ch.staged, ch.unstaged, ch.untracked), (0, 0, 0));
}

#[test]
fn gone_upstream_is_skipped() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.origin_head("main");
    fx.track("main"); // origin/main is configured but the remote-tracking ref does not exist
    fx.checkout_head();
    let rec = sync_repo(
        &RepoRef {
            name: "fx".into(),
            path: fx.path.clone(),
        },
        None,
        &Opts {
            apply: false,
            offline: true,
        },
    );
    assert_eq!(rec.result, "skip", "{rec:?}");
    assert_eq!(rec.reason, "upstream branch is gone");
}

#[test]
fn a_failed_fetch_without_a_token_says_how_to_provide_one() {
    let s = setup();
    s.fx.set_origin_url("https://127.0.0.1:1/test/fixture.git");
    let run = |gh: Option<&stw::github::Client>| {
        sync_repo(
            &repo_ref(&s),
            gh,
            &Opts {
                apply: true,
                offline: false,
            },
        )
    };
    let rec = run(None);
    assert_eq!(
        (rec.result.as_str(), rec.fetch.as_str()),
        ("error", "failed"),
        "{rec:?}"
    );
    let err = rec.err.unwrap();
    assert!(
        err.contains("GH_TOKEN") && err.contains("GITHUB_TOKEN"),
        "{err}"
    );
    unchanged(&s);
    let c = stw::github::Client::new("T".into(), stw::auth::TokenSource::Env);
    let err = run(Some(&c)).err.unwrap();
    assert!(!err.contains("GH_TOKEN"), "a token was available: {err}");
}

/// The ref moves first, so a ref that cannot be written leaves index, working tree and HEAD untouched.
#[test]
fn a_ref_that_cannot_be_written_leaves_index_and_working_tree_alone() {
    use std::os::unix::fs::PermissionsExt;
    let s = setup();
    let heads = s.fx.path.join(".git/refs/heads");
    let index_before = std::fs::read(s.fx.path.join(".git/index")).unwrap();
    std::fs::set_permissions(&heads, std::fs::Permissions::from_mode(0o555)).unwrap();
    let err = fast_forward(&s.fx.repo(), s.new, "origin/main");
    std::fs::set_permissions(&heads, std::fs::Permissions::from_mode(0o755)).unwrap();
    let err = err.unwrap_err();
    assert!(err.to_string().contains("ref update"), "{err}");
    unchanged(&s);
    assert!(s.fx.exists("old.txt"), "a deleted file must still be there");
    assert!(!s.fx.exists("dir/new.txt"), "an added file must not appear");
    assert_eq!(s.fx.read("a.txt").unwrap(), lines(10, "a"));
    assert_eq!(
        std::fs::read(s.fx.path.join(".git/index")).unwrap(),
        index_before
    );
    let ch = status::changes(&s.fx.repo(), true).unwrap();
    assert_eq!((ch.staged, ch.unstaged, ch.untracked), (0, 0, 0));
}

/// If writing files fails after the ref moved, the ref is put back.
#[test]
fn a_failed_file_write_puts_the_ref_back() {
    use std::os::unix::fs::PermissionsExt;
    let s = setup();
    let dir = s.fx.path.clone();
    let index_before = std::fs::read(s.fx.path.join(".git/index")).unwrap();
    // a.txt is modified by the update; a read-only working directory makes the checkout fail
    std::fs::set_permissions(&dir, std::fs::Permissions::from_mode(0o555)).unwrap();
    let err = fast_forward(&s.fx.repo(), s.new, "origin/main");
    std::fs::set_permissions(&dir, std::fs::Permissions::from_mode(0o755)).unwrap();
    assert!(err.is_err(), "the write must fail in a read-only directory");
    assert_eq!(
        s.fx.tip("main"),
        s.old,
        "the ref must be back at the old commit"
    );
    // OPEN (no mutation proves these four assertions yet; the ref assertion above is proved by A1).
    // The index is written last, so it is untouched. Deletions run after the checkout, so old.txt is still there.
    assert_eq!(
        std::fs::read(s.fx.path.join(".git/index")).unwrap(),
        index_before
    );
    assert!(s.fx.exists("old.txt"), "a deleted file must still be there");
    assert!(!s.fx.exists("dir/new.txt"), "an added file must not appear");
    assert_eq!(s.fx.read("keep.txt").as_deref(), Some("keep\n"));
    // Known limit (README): files the checkout already wrote stay. a.txt is rewritten in place before the
    // failure on dir/new.txt; git status shows it as an unstaged change against the restored HEAD.
    assert_eq!(
        s.fx.read("a.txt").unwrap(),
        lines(10, "a").replace("a5\n", "A5\n")
    );
    let ch = status::changes(&s.fx.repo(), true).unwrap();
    assert_eq!((ch.staged, ch.unstaged, ch.untracked), (0, 1, 0));
}

/// If the index cannot be written (here: a stale index.lock), the files are already in place, the index is
/// as before, and the ref is put back. git status then shows the new files as changes against the old HEAD.
#[test]
fn a_failed_index_write_puts_the_ref_back_and_keeps_the_written_files() {
    let s = setup();
    let index_before = std::fs::read(s.fx.path.join(".git/index")).unwrap();
    std::fs::write(s.fx.path.join(".git/index.lock"), "").unwrap();
    let err = fast_forward(&s.fx.repo(), s.new, "origin/main");
    std::fs::remove_file(s.fx.path.join(".git/index.lock")).unwrap();
    let err = err.unwrap_err().to_string();
    assert!(err.starts_with("index:"), "{err}");
    assert!(!err.contains("could not be put back"), "{err}");
    assert_eq!(s.fx.tip("main"), s.old, "the ref must be back");
    assert_eq!(
        std::fs::read(s.fx.path.join(".git/index")).unwrap(),
        index_before
    );
    assert_eq!(s.fx.read("dir/new.txt").as_deref(), Some("new\n"));
    assert!(!s.fx.exists("old.txt"));
}

/// If the ref cannot be put back, the error names both failures and the ref stays where it is.
/// Provoked by calling the undo step while the ref is not at the commit it expects.
#[test]
fn a_failed_rollback_reports_both_errors_and_leaves_the_ref() {
    let s = setup();
    let cause = FfError::Other("index: disk full".into());
    // the ref is at s.old, but the undo step expects s.new: the ref update is refused
    let err = put_ref_back(&s.fx.repo(), s.old, s.new, "origin/main", cause).to_string();
    assert!(
        err.starts_with("index: disk full; and the ref could not be put back: "),
        "{err}"
    );
    assert!(err.contains("ref update"), "{err}");
    assert_eq!(
        s.fx.tip("main"),
        s.old,
        "a refused undo must not move the ref"
    );
}
