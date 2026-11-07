//! `stw sync` and the agent-rules checkout: the state line, and the fast-forward update with its conditions.
mod common;
use common::*;
use stw::sync::rules::{agent_rules, render_human, to_json, RulesOpts};

struct R {
    fx: Fx,
    old: gix::ObjectId,
    c2: gix::ObjectId,
}

/// main at c0 (checked out); origin/main at c2 (two owner commits: rules/global.md; skills/ and install.sh).
fn setup() -> R {
    let fx = Fx::new();
    let c0 = fx.commit(
        None,
        &[
            ("README.md", Some("readme\n")),
            ("install.sh", Some("echo old\n")),
        ],
        "c0",
    );
    let c1 = fx.commit(
        Some(c0),
        &[("rules/global.md", Some("rules\n"))],
        "rules: add global",
    );
    let c2 = fx.commit(
        Some(c1),
        &[
            ("skills/agent-workflow/SKILL.md", Some("skill\n")),
            ("install.sh", Some("touch EXECUTED\n")),
        ],
        "skills: add workflow",
    );
    fx.branch("main", c0);
    fx.set_head("main");
    fx.remote("main", c2);
    fx.origin_head("main");
    fx.track("main");
    fx.checkout_head();
    R { fx, old: c0, c2 }
}

fn opts() -> RulesOpts {
    RulesOpts {
        path: None,
        update: true,
        apply: true,
        offline: true,
    }
}

fn at(r: &R) -> RulesOpts {
    RulesOpts {
        path: Some(r.fx.path.clone()),
        ..opts()
    }
}

fn show(r: &R) -> stw::sync::rules::RulesRec {
    agent_rules(
        r.fx.path.parent().unwrap(),
        None,
        &RulesOpts {
            update: false,
            ..at(r)
        },
    )
}

fn untouched(r: &R) {
    assert_eq!(r.fx.tip("main"), r.old, "ref must not move");
    assert_eq!(r.fx.read("install.sh").as_deref(), Some("echo old\n"));
    assert!(!r.fx.exists("rules"));
}

#[test]
fn no_checkout_is_not_found_and_an_update_is_refused() {
    let fx = Fx::new();
    let dir = tempfile::tempdir_in(fx.path.parent().unwrap()).unwrap();
    let rec = agent_rules(
        dir.path(),
        None,
        &RulesOpts {
            update: false,
            ..opts()
        },
    );
    assert_eq!(render_human(&rec), "agent-rules: not found\n");
    let rec = agent_rules(dir.path(), None, &opts());
    assert_eq!(rec.action, "refused");
    let want = dir.path().join(".agents");
    assert!(
        rec.reason.contains(&want.display().to_string()),
        "{}",
        rec.reason
    );
    let given = dir.path().join("elsewhere");
    let rec = agent_rules(
        dir.path(),
        None,
        &RulesOpts {
            path: Some(given.clone()),
            ..opts()
        },
    );
    assert!(
        rec.reason.contains(&given.display().to_string()),
        "{}",
        rec.reason
    );
    assert!(!rec.reason.contains(".agents"), "{}", rec.reason);
    assert!(
        std::fs::read_dir(dir.path()).unwrap().next().is_none(),
        "nothing created"
    );
}

#[test]
fn the_default_place_is_the_agents_submodule_dir_in_the_workspace() {
    let r = setup();
    let root = r.fx.path.parent().unwrap().to_path_buf();
    std::fs::rename(&r.fx.path, root.join(".agents")).unwrap();
    let rec = agent_rules(
        &root,
        None,
        &RulesOpts {
            update: false,
            ..opts()
        },
    );
    assert_eq!(rec.state, "behind");
    // a checkout elsewhere in the workspace is not guessed
    std::fs::rename(root.join(".agents"), root.join("other")).unwrap();
    assert_eq!(
        agent_rules(
            &root,
            None,
            &RulesOpts {
                update: false,
                ..opts()
            }
        )
        .state,
        "not-found"
    );
}

#[test]
fn display_says_behind_with_count_and_as_of_last_fetch_and_changes_nothing() {
    let r = setup();
    let rec = show(&r);
    assert_eq!(rec.state, "behind");
    assert_eq!(
        render_human(&rec),
        "agent-rules: behind 2 commits of origin/main (as of last fetch)\n"
    );
    untouched(&r);
    assert_eq!(to_json(&rec)["behind"], 2);
}

#[test]
fn display_states_current_diverged_dirty_not_default_and_ahead() {
    let r = setup();
    r.fx.branch("main", r.c2);
    r.fx.checkout_head();
    assert_eq!(
        render_human(&show(&r)),
        "agent-rules: current (as of last fetch)\n"
    );

    let local = r.fx.commit(Some(r.c2), &[("x", Some("x\n"))], "local");
    r.fx.branch("main", local);
    r.fx.checkout_head();
    assert!(render_human(&show(&r))
        .starts_with("agent-rules: ahead 1 commit of origin/main (local only)"));

    let other = r.fx.commit(Some(r.old), &[("y", Some("y\n"))], "other");
    r.fx.branch("main", other);
    r.fx.checkout_head();
    assert!(render_human(&show(&r)).starts_with("agent-rules: diverged"));

    r.fx.branch("main", r.old);
    r.fx.checkout_head();
    r.fx.write("README.md", "edited\n");
    assert!(render_human(&show(&r)).starts_with("agent-rules: dirty"));

    let r = setup();
    r.fx.branch("topic", r.old);
    r.fx.set_head("topic");
    assert!(render_human(&show(&r)).starts_with("agent-rules: not on default branch"));
}

#[test]
fn update_fast_forwards_and_lists_commits_files_and_the_new_sessions_note() {
    let r = setup();
    let rec = agent_rules(r.fx.path.parent().unwrap(), None, &at(&r));
    assert_eq!(rec.action, "updated", "{}", rec.reason);
    assert_eq!(r.fx.tip("main"), r.c2);
    assert_eq!(r.fx.read("rules/global.md").as_deref(), Some("rules\n"));
    assert_eq!(rec.commits.len(), 2);
    assert_eq!(rec.commits[0].subject, "skills: add workflow");
    assert_eq!(
        rec.flagged,
        [
            "install.sh",
            "rules/global.md",
            "skills/agent-workflow/SKILL.md"
        ]
    );
    let text = render_human(&rec);
    assert!(text.contains("! skills/agent-workflow/SKILL.md"), "{text}");
    assert!(
        text.contains("applies to NEW sessions; running agents keep their loaded rules"),
        "{text}"
    );
    assert!(
        !text.contains("next:") && !text.contains("\n    run "),
        "stw prints no commands: {text}"
    );
}

#[test]
fn update_never_runs_install_sh_and_writes_nothing_outside_the_checkout() {
    let r = setup();
    let parent = r.fx.path.parent().unwrap().to_path_buf();
    let before: Vec<_> = std::fs::read_dir(&parent)
        .unwrap()
        .map(|e| e.unwrap().file_name())
        .collect();
    let rec = agent_rules(&parent, None, &at(&r));
    assert_eq!(rec.action, "updated");
    assert_eq!(
        r.fx.read("install.sh").as_deref(),
        Some("touch EXECUTED\n"),
        "file updated, not run"
    );
    assert!(!r.fx.exists("EXECUTED") && !parent.join("EXECUTED").exists());
    let after: Vec<_> = std::fs::read_dir(&parent)
        .unwrap()
        .map(|e| e.unwrap().file_name())
        .collect();
    assert_eq!(before, after);
}

#[test]
fn dry_run_shows_the_result_and_changes_nothing() {
    let r = setup();
    let rec = agent_rules(
        r.fx.path.parent().unwrap(),
        None,
        &RulesOpts {
            apply: false,
            offline: false,
            ..at(&r)
        },
    );
    assert_eq!(rec.action, "would-update");
    assert_eq!(rec.fetch, "not-run", "a dry run never fetches");
    assert_eq!(rec.commits.len(), 2);
    assert_eq!(rec.head.as_deref(), Some(r.old.to_string().as_str()));
    let text = render_human(&rec);
    assert!(text.contains("behind 2 commits of origin/main"), "{text}");
    assert!(text.contains("rules commit "), "{text}");
    assert!(
        text.contains("dry run (without --apply): would fast-forward"),
        "{text}"
    );
    untouched(&r);
}

fn refused(r: &R, needle: &str) {
    let rec = agent_rules(r.fx.path.parent().unwrap(), None, &at(r));
    assert_eq!(rec.action, "refused");
    assert!(rec.reason.contains(needle), "{}", rec.reason);
    assert!(render_human(&rec).contains("refused, nothing changed"));
}

#[test]
fn update_is_refused_for_a_dirty_tree() {
    let r = setup();
    r.fx.write("README.md", "edited\n");
    refused(&r, "changed file");
    assert!(!r.fx.exists("rules"));
    assert_eq!(r.fx.tip("main"), r.old);
}

#[test]
fn update_is_refused_off_the_default_branch() {
    let r = setup();
    r.fx.branch("topic", r.old);
    r.fx.set_head("topic");
    refused(&r, "not the default branch");
    assert_eq!(r.fx.tip("topic"), r.old);
}

#[test]
fn update_is_refused_when_diverged() {
    let r = setup();
    let local = r.fx.commit(Some(r.old), &[("y", Some("y\n"))], "local");
    r.fx.branch("main", local);
    r.fx.checkout_head();
    refused(&r, "not a fast-forward");
    assert_eq!(r.fx.tip("main"), local);
}

fn with_foreign(author: (&str, &str), committer: (&str, &str)) -> R {
    let mut r = setup();
    let c3 = r.fx.commit_by(
        Some(r.c2),
        &[("guards/x", Some("x\n"))],
        "guards: x",
        author,
        committer,
    );
    r.fx.remote("main", c3);
    r.c2 = c3;
    r
}

#[test]
fn foreign_authors_are_listed_once_as_information_and_never_refused() {
    let r = with_foreign(
        ("Eve", "eve@example.org"),
        ("Web Flow", "noreply@github.com"),
    );
    let rec = agent_rules(r.fx.path.parent().unwrap(), None, &at(&r));
    assert_eq!(rec.action, "updated", "{}", rec.reason);
    assert_eq!(r.fx.tip("main"), r.c2);
    assert_eq!(
        rec.foreign,
        ["Eve <eve@example.org>", "Web Flow <noreply@github.com>"]
    );
    let text = render_human(&rec);
    assert_eq!(text.matches("info: new commits by").count(), 1, "{text}");
    assert!(
        text.contains("Eve <eve@example.org>, Web Flow <noreply@github.com>"),
        "{text}"
    );
    // own commits only: no note
    let r = setup();
    let rec = agent_rules(r.fx.path.parent().unwrap(), None, &at(&r));
    assert!(rec.foreign.is_empty() && !render_human(&rec).contains("info:"));
    // the configured identity decides what is foreign
    let r = with_foreign(("Eve", "eve@example.org"), ("Eve", "eve@example.org"));
    r.fx.set_identity("Eve", "eve@example.org");
    let rec = agent_rules(r.fx.path.parent().unwrap(), None, &at(&r));
    assert_eq!(rec.action, "updated", "{}", rec.reason);
    assert_eq!(rec.foreign, ["T <t@example.com>"]);
}

fn detach(r: &R, id: gix::ObjectId) {
    std::fs::write(r.fx.path.join(".git/HEAD"), format!("{id}\n")).unwrap();
    r.fx.checkout_head();
}

/// Make the fixture look like a submodule: the git directory moved away, `.git` a file pointing to it.
fn make_submodule(r: &R) {
    let modules = r.fx.path.parent().unwrap().join("modules/agents");
    std::fs::create_dir_all(modules.parent().unwrap()).unwrap();
    std::fs::rename(r.fx.path.join(".git"), &modules).unwrap();
    std::fs::write(r.fx.path.join(".git"), "gitdir: ../modules/agents\n").unwrap();
}

#[test]
fn a_detached_submodule_checkout_fast_forwards_without_committing_the_superproject() {
    let r = setup();
    detach(&r, r.old);
    make_submodule(&r);
    let root = r.fx.path.parent().unwrap().to_path_buf();
    let rec = agent_rules(&root, None, &at(&r));
    assert!(rec.detached && rec.submodule);
    assert_eq!(rec.action, "updated", "{}", rec.reason);
    let repo = gix::open(&r.fx.path).unwrap();
    assert_eq!(repo.head_id().unwrap().detach(), r.c2);
    assert!(repo.head_name().unwrap().is_none(), "still detached");
    assert_eq!(r.fx.read("rules/global.md").as_deref(), Some("rules\n"));
    let text = render_human(&rec);
    assert!(text.contains("(detached HEAD)"), "{text}");
    assert!(
        text.contains("repo has a new commit, recorded as modified in the workspace"),
        "{text}"
    );
    assert!(!text.contains("next:"), "{text}");
}

#[test]
fn a_detached_checkout_that_is_current_changes_nothing() {
    let r = setup();
    detach(&r, r.c2);
    let rec = agent_rules(r.fx.path.parent().unwrap(), None, &at(&r));
    assert_eq!(rec.state, "current");
    assert_eq!(rec.action, "none", "{}", rec.reason);
    assert!(!render_human(&rec).contains("refused"));
}

#[test]
fn a_detached_checkout_with_diverged_commits_or_changes_is_refused() {
    let r = setup();
    let local = r.fx.commit(Some(r.old), &[("y", Some("y\n"))], "local");
    detach(&r, local);
    refused(&r, "diverged");
    let r = setup();
    detach(&r, r.old);
    r.fx.write("README.md", "edited\n");
    refused(&r, "changed file");
    assert!(!r.fx.exists("rules"));
    assert_eq!(r.fx.read("README.md").as_deref(), Some("edited\n"));
}

fn stw(args: &[&str]) -> std::process::Output {
    std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .args(args)
        .output()
        .unwrap()
}

#[test]
fn cli_json_carries_agent_rules_and_a_refusal_exits_1() {
    let r = setup();
    let root = r.fx.path.parent().unwrap().to_path_buf();
    std::fs::rename(&r.fx.path, root.join(".agents")).unwrap();
    gix::init(&root).unwrap();
    let rs = root.to_str().unwrap();
    let o = stw(&["sync", "--root", rs, "--offline", "--json"]);
    let v: serde_json::Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(v["agent_rules"]["state"], "behind");
    assert_eq!(o.status.code(), Some(0), "{v}");
    // without --apply: reports, changes nothing
    let o = stw(&["sync", "--root", rs, "--rules"]);
    assert_eq!(o.status.code(), Some(0));
    let text = String::from_utf8_lossy(&o.stdout).into_owned();
    assert!(text.contains("dry run (without --apply)"), "{text}");
    assert!(!root.join(".agents/rules").exists());
    // a refusal exits 1
    std::fs::write(root.join(".agents/README.md"), "edited\n").unwrap();
    let o = stw(&["sync", "--root", rs, "--offline", "--rules", "--apply"]);
    assert_eq!(o.status.code(), Some(1));
    let text = String::from_utf8_lossy(&o.stdout).into_owned();
    let text = text[text.find("agent-rules:").unwrap()..].to_string();
    assert!(text.contains("refused, nothing changed"), "{text}");
    assert!(!text.contains("next:"), "{text}");
    std::fs::write(root.join(".agents/README.md"), "readme\n").unwrap();
    // updated
    let o = stw(&["sync", "--root", rs, "--offline", "--rules", "--apply"]);
    assert_eq!(
        o.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&o.stdout)
    );
    assert!(root.join(".agents/rules/global.md").exists());
    // status takes the same --rules-path
    let other = root.join(".agents");
    let o = stw(&[
        "status",
        "--root",
        rs,
        "--offline",
        "--json",
        "--rules-path",
        other.to_str().unwrap(),
    ]);
    let v: serde_json::Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(v["agent_rules"]["state"], "current", "{v}");
    // --dry-run no longer exists: dry run is the default without --apply
    assert_eq!(
        stw(&["sync", "--root", rs, "--dry-run"]).status.code(),
        Some(2)
    );
}

#[test]
fn ahead_only_is_a_no_op_for_a_branch_and_for_a_detached_head() {
    for detached in [false, true] {
        let r = setup();
        let local = r.fx.commit(Some(r.c2), &[("y", Some("y\n"))], "local");
        r.fx.branch("main", local);
        if detached {
            detach(&r, local);
        } else {
            r.fx.checkout_head();
        }
        let rec = agent_rules(r.fx.path.parent().unwrap(), None, &at(&r));
        assert_eq!(rec.action, "none", "detached={detached}: {}", rec.reason);
        let text = render_human(&rec);
        assert!(
            text.contains("ahead of origin/main by 1 commit, nothing to fast-forward"),
            "{text}"
        );
        assert!(!text.contains("refused"));
    }
}

#[test]
fn the_cause_of_an_unknown_default_branch_is_named() {
    // no remote origin at all
    let r = setup();
    let cfg = r.fx.path.join(".git/config");
    let text = std::fs::read_to_string(&cfg).unwrap();
    let cut = text.find("[remote \"origin\"]").unwrap();
    let end = text[cut..].find("[user]").unwrap() + cut;
    std::fs::write(&cfg, format!("{}{}", &text[..cut], &text[end..])).unwrap();
    std::fs::remove_dir_all(r.fx.path.join(".git/refs/remotes")).unwrap();
    let o = RulesOpts {
        offline: false,
        ..at(&r)
    };
    let rec = agent_rules(r.fx.path.parent().unwrap(), None, &o);
    assert_eq!(rec.action, "refused");
    assert!(rec.reason.contains("no remote origin"), "{}", rec.reason);
    assert!(!rec.reason.contains("fetch first"));
    // origin without any default branch
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a", Some("a\n"))], "c0");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    let rec = agent_rules(
        fx.path.parent().unwrap(),
        None,
        &RulesOpts {
            path: Some(fx.path.clone()),
            ..opts()
        },
    );
    assert_eq!(rec.action, "refused");
    assert!(
        rec.reason.contains("origin has no default branch"),
        "{}",
        rec.reason
    );
}

#[test]
fn origin_head_on_another_branch_decides_the_default() {
    let r = setup();
    r.fx.remote("trunk", r.c2);
    r.fx.origin_head("trunk");
    refused(&r, "not the default branch (origin/trunk)");
    r.fx.branch("trunk", r.old);
    r.fx.set_head("trunk");
    let rec = agent_rules(r.fx.path.parent().unwrap(), None, &at(&r));
    assert_eq!(rec.action, "updated", "{}", rec.reason);
    assert_eq!(r.fx.tip("trunk"), r.c2);
    assert_eq!(r.fx.tip("main"), r.old);
}

#[test]
fn a_linked_worktree_is_not_a_submodule() {
    let r = setup();
    r.fx.branch("wt", r.old);
    let wt = real_worktree(&r.fx, "x", "wt");
    assert!(wt.join(".git").is_file());
    let rec = agent_rules(
        r.fx.path.parent().unwrap(),
        None,
        &RulesOpts {
            path: Some(wt),
            ..opts()
        },
    );
    assert!(!rec.submodule);
    // a real submodule layout is one
    let r = setup();
    detach(&r, r.old);
    make_submodule(&r);
    let rec = agent_rules(r.fx.path.parent().unwrap(), None, &at(&r));
    assert!(rec.submodule);
}

/// Checkout at c0 on branch main (upstream origin/main), whose origin is a local repository with a newer commit.
fn with_local_origin() -> (Fx, Fx, gix::ObjectId) {
    let files = [
        ("README.md", Some("readme\n")),
        ("install.sh", Some("echo old\n")),
    ];
    let origin = Fx::new();
    let o0 = origin.commit(None, &files, "c0");
    let o1 = origin.commit(
        Some(o0),
        &[("rules/global.md", Some("rules\n"))],
        "rules: add global",
    );
    origin.branch("main", o1);
    origin.set_head("main");
    let fx = Fx::new();
    let c0 = fx.commit(None, &files, "c0");
    assert_eq!(c0, o0, "fixtures build the same first commit");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.track("main");
    fx.checkout_head();
    fx.set_origin_url(origin.path.to_str().unwrap());
    (fx, origin, o1)
}

#[test]
fn apply_fetches_a_local_origin_when_online() {
    let (fx, _origin, o1) = with_local_origin();
    let rec = agent_rules(
        fx.path.parent().unwrap(),
        None,
        &RulesOpts {
            path: Some(fx.path.clone()),
            update: true,
            apply: true,
            ..Default::default()
        },
    );
    assert_eq!(rec.fetch, "ok", "{}", rec.reason);
    assert_eq!(rec.action, "updated", "{}", rec.reason);
    assert_eq!(fx.tip("main"), o1);
    assert_eq!(fx.read("rules/global.md").as_deref(), Some("rules\n"));
}

#[test]
fn the_rules_submodule_is_only_touched_by_rules_apply() {
    let (fx, _origin, o1) = with_local_origin();
    let root = fx.path.parent().unwrap().to_path_buf();
    let sub = root.join(".agents");
    std::fs::rename(&fx.path, &sub).unwrap();
    gix::init(&root).unwrap();
    std::fs::write(
        root.join(".gitmodules"),
        "[submodule \".agents\"]\n\tpath = .agents\n\turl = https://github.com/wstein/dotagents\n",
    )
    .unwrap();
    let rs = root.to_str().unwrap();
    let run = |args: &[&str]| {
        std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
            .env("HOME", "/nonexistent")
            .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
            .env_remove("GH_CONFIG_DIR")
            .env_remove("XDG_CONFIG_HOME")
            .env_remove("GITHUB_TOKEN")
            .env_remove("GH_TOKEN")
            .args(args)
            .output()
            .unwrap()
    };
    let tip = || gix::open(&sub).unwrap().head_id().unwrap().detach();
    let old = tip();
    // (1) plain sync --apply leaves it alone: not listed, not fetched, not moved
    let o = run(&["sync", "--apply", "--root", rs]);
    let text = String::from_utf8_lossy(&o.stdout).into_owned();
    assert!(!text.contains(".agents"), "{text}");
    assert_eq!(tip(), old, "{text}");
    assert!(!sub.join("rules").exists());
    let o = run(&["status", "--root", rs, "--offline"]);
    assert!(!String::from_utf8_lossy(&o.stdout).contains("  .agents"));
    // (2) --rules --apply updates it once, with the preview
    let o = run(&["sync", "--rules", "--apply", "--root", rs]);
    let text = String::from_utf8_lossy(&o.stdout).into_owned();
    assert_eq!(o.status.code(), Some(0), "{text}");
    assert_eq!(tip(), o1);
    assert_eq!(text.matches("rules: add global").count(), 1, "{text}");
    assert!(text.contains("! rules/global.md"), "{text}");
    assert!(text.contains("applies to NEW sessions"), "{text}");
    // (3) the superproject was not written: no commit
    assert!(gix::open(&root).unwrap().head_id().is_err());
}
