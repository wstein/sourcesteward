//! Needs network (public GitHub repository, no token required). Run: cargo test --test it -- --ignored network
use stw::repo::RepoRef;
use stw::sync::{sync_repo, Opts};

#[test]
#[ignore]
fn fetch_and_fast_forward_a_clone_that_was_rewound_one_commit() {
    let base = std::env::var_os("CLAUDE_JOB_DIR")
        .map(|d| std::path::PathBuf::from(d).join("tmp"))
        .unwrap_or_else(std::env::temp_dir);
    let tmp = tempfile::Builder::new()
        .prefix("stw-net-")
        .tempdir_in(base)
        .unwrap();
    let dir = tmp.path().join("clone");
    // clone with gix
    let mut prep = gix::prepare_clone("https://github.com/octocat/Hello-World", &dir).unwrap();
    let (mut checkout, _) = prep
        .fetch_then_checkout(
            gix::progress::Discard,
            &std::sync::atomic::AtomicBool::new(false),
        )
        .unwrap();
    let (repo, _) = checkout
        .main_worktree(
            gix::progress::Discard,
            &std::sync::atomic::AtomicBool::new(false),
        )
        .unwrap();
    let branch = repo.head_name().unwrap().unwrap().shorten().to_string();
    let tip = repo.head_id().unwrap().detach();
    let parent = repo
        .find_commit(tip)
        .unwrap()
        .parent_ids()
        .next()
        .expect("repo has history")
        .detach();
    // rewind branch and tracking ref one commit, and the working tree
    use gix::refs::transaction::PreviousValue;
    repo.reference(
        format!("refs/heads/{branch}"),
        parent,
        PreviousValue::Any,
        "rewind",
    )
    .unwrap();
    repo.reference(
        format!("refs/remotes/origin/{branch}"),
        parent,
        PreviousValue::Any,
        "rewind",
    )
    .unwrap();
    // working tree of the parent commit
    let tree = repo
        .find_commit(parent)
        .unwrap()
        .tree_id()
        .unwrap()
        .detach();
    let mut index = repo.index_from_tree(&tree).unwrap();
    let mut opts = repo
        .checkout_options(gix::worktree::stack::state::attributes::Source::IdMapping)
        .unwrap();
    opts.overwrite_existing = true;
    opts.destination_is_initially_empty = false;
    gix::worktree::state::checkout(
        &mut index,
        repo.workdir().unwrap(),
        repo.objects.clone().into_arc().unwrap(),
        &gix::progress::Discard,
        &gix::progress::Discard,
        &std::sync::atomic::AtomicBool::new(false),
        opts,
    )
    .unwrap();
    index.write(Default::default()).unwrap();
    drop(repo);

    // dry run first: nothing may change
    let r = RepoRef {
        name: "clone".into(),
        path: dir.clone(),
    };
    let gh = stw::github::Client::from_env();
    let dry = sync_repo(
        &r,
        gh.as_ref(),
        &Opts {
            apply: false,
            offline: false,
            ..Default::default()
        },
    );
    assert_eq!(
        dry.result, "none",
        "tracking ref is rewound too, so locally there is nothing to pull: {dry:?}"
    );
    if gh.is_some() {
        assert_eq!(
            dry.remote_moved,
            Some(true),
            "REST check sees that origin has news: {dry:?}"
        );
    }
    let repo = gix::open(&dir).unwrap();
    assert_eq!(repo.head_id().unwrap().detach(), parent);
    drop(repo);

    let rec = sync_repo(
        &r,
        gh.as_ref(),
        &Opts {
            apply: true,
            offline: false,
            ..Default::default()
        },
    );
    assert_eq!(rec.fetch, "ok", "{rec:?}");
    assert_eq!(rec.result, "fast-forwarded", "{rec:?}");
    let repo = gix::open(&dir).unwrap();
    assert_eq!(repo.head_id().unwrap().detach(), tip);
    let c = stw::status::changes(&repo, true).unwrap();
    assert_eq!((c.staged, c.unstaged, c.untracked), (0, 0, 0));
}
