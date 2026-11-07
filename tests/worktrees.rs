mod common;
use common::*;
use stw::repo::worktrees;

fn fixture() -> Fx {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    for b in ["live", "gone", "locked-gone", "plain"] {
        fx.branch(b, c0);
    }
    fx
}

#[test]
fn live_worktree_protects_its_branch_and_main_checkout_counts() {
    let fx = fixture();
    let dir = add_worktree(&fx, "live", "live", true, false);
    let w = worktrees::scan(&fx.repo()).unwrap();
    assert_eq!(w.branch_dir("live"), Some(&dir));
    assert!(w.branch_dir("main").is_some(), "main checkout is in use");
    assert!(w.branch_dir("plain").is_none());
    assert!(w.stale.is_empty());
}

#[test]
fn missing_directory_protects_nothing_and_is_a_stale_registration() {
    let fx = fixture();
    let dir = add_worktree(&fx, "gone", "gone", false, false);
    let w = worktrees::scan(&fx.repo()).unwrap();
    assert!(
        w.branch_dir("gone").is_none(),
        "a missing directory must not protect the branch"
    );
    assert_eq!(w.stale.len(), 1);
    assert_eq!(w.stale[0].id, "gone");
    assert_eq!(w.stale[0].path, dir);
    assert!(!w.stale[0].locked);
}

#[test]
fn locked_registration_with_missing_directory_is_reported_as_locked() {
    let fx = fixture();
    add_worktree(&fx, "locked-gone", "locked-gone", false, true);
    let w = worktrees::scan(&fx.repo()).unwrap();
    assert!(w.branch_dir("locked-gone").is_none());
    assert_eq!(w.stale.len(), 1);
    assert!(w.stale[0].locked);
    assert_eq!(w.stale[0].lock_reason.as_deref(), Some("test lock"));
}

#[test]
fn registration_without_gitdir_file_is_ignored_like_git_does() {
    let fx = fixture();
    let reg = fx.path.join(".git/worktrees/broken");
    std::fs::create_dir_all(&reg).unwrap();
    std::fs::write(reg.join("HEAD"), "ref: refs/heads/plain\n").unwrap();
    let w = worktrees::scan(&fx.repo()).unwrap();
    assert!(w.stale.is_empty());
    assert!(w.branch_dir("plain").is_none());
}

/// G2: scanning from a linked worktree must still report the branch checked out in the main checkout.
#[test]
fn scan_from_a_linked_root_sees_the_main_checkout() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.branch("main", c0);
    fx.branch("owner", c0);
    fx.set_head("owner");
    fx.checkout_head();
    let wt = real_worktree(&fx, "linked", "main");
    let w = worktrees::scan(&stw::repo::open(&wt).unwrap()).unwrap();
    assert_eq!(w.branch_dir("owner"), Some(&fx.path), "{w:?}");
    assert_eq!(w.branch_dir("main"), Some(&wt));
    assert_eq!(w.main_dir.as_ref(), Some(&fx.path));
    assert_eq!(w.linked.len(), 1, "the linked root is listed once: {w:?}");
}
