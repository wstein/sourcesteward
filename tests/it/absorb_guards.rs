// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! `stw absorb`: the refusals before any plan (typed error codes, exit codes) and the backend detection.
use super::absorb_fx::*;

#[test]
fn nothing_staged() {
    let fx = Fx::new();
    fx.base10(false);
    let d = fx.error("nothing-staged", &[]);
    assert!(d["error"]["message"]
        .as_str()
        .unwrap()
        .to_lowercase()
        .contains("nothing"));
    fx.write("a.txt", "changed in the work tree only\n");
    fx.error("nothing-staged", &[]);
}

#[test]
fn no_commits() {
    let fx = Fx::new();
    fx.write("a.txt", "x\n");
    fx.g(&["add", "a.txt"]);
    fx.error("no-commits", &[]);
}

#[test]
fn detached_head() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.g(&["checkout", "-q", "--detach"]);
    fx.stage1("a.txt", &replace_line(&t3, 3, "f"));
    fx.error("detached-head", &[]);
}

#[test]
fn no_identity() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "f"));
    std::fs::write(&fx.cfg, "[init]\n\tdefaultBranch = main\n").unwrap();
    let d = fx.error("no-identity", &[]);
    assert!(d["error"]["message"]
        .as_str()
        .unwrap()
        .contains("user.name"));
    std::fs::write(&fx.cfg, "[user]\n\tname = Test\n").unwrap();
    let d = fx.error("no-identity", &[]);
    assert!(d["error"]["message"]
        .as_str()
        .unwrap()
        .contains("user.email"));
}

#[test]
fn not_a_repository_is_a_usage_error() {
    let fx = Fx::new();
    let plain = fx.s.join("plain");
    std::fs::create_dir(&plain).unwrap();
    let o = fx.stw_in(&plain, &[], &[]);
    assert_eq!(o.rc, 2, "{}", o.err);
    assert!(!o.err.trim().is_empty());
}

#[test]
fn usage_errors_exit_2() {
    let fx = Fx::new();
    fx.base10(false);
    for args in [
        vec!["--nope"],
        vec!["--apply"],
        vec!["--max-stack", "0"],
        vec!["--max-stack", "x"],
        vec!["--base"],
        vec!["extra"],
    ] {
        let o = fx.run(&args);
        assert_eq!(o.rc, 2, "{args:?}: {} {}", o.out, o.err);
        assert!(!o.err.trim().is_empty(), "{args:?}");
    }
}

#[test]
fn unmerged_index_is_refused() {
    let fx = Fx::new();
    let t = l(6);
    fx.commit1("a.txt", &t, "feat: one");
    fx.g(&["checkout", "-q", "-b", "side"]);
    fx.commit1("a.txt", &replace_line(&t, 2, "side"), "feat: side");
    fx.g(&["checkout", "-q", "main"]);
    fx.commit1("a.txt", &replace_line(&t, 2, "main"), "feat: main");
    fx.g_ok(&["merge", "side"]);
    assert!(
        fx.g(&["status", "--short"]).starts_with("UU"),
        "fixture has an unmerged path"
    );
    fx.error("unmerged-index", &[]);
}

#[test]
fn git_environment_overrides_are_refused() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "f"));
    let r = fx.r.to_str().unwrap();
    let cases = [
        ("GIT_DIR", format!("{r}/.git")),
        ("GIT_WORK_TREE", r.to_string()),
        ("GIT_INDEX_FILE", format!("{r}/.git/index")),
        ("GIT_COMMON_DIR", format!("{r}/.git")),
        ("GIT_OBJECT_DIRECTORY", format!("{r}/.git/objects")),
    ];
    for (var, val) in &cases {
        let d = fx.error_rc("environment-override", &[], &[(var, val)], 1);
        assert!(
            d["error"]["message"].as_str().unwrap().contains(var),
            "{var}"
        );
    }
}

#[test]
fn plan_flag_is_accepted_and_the_default() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "f"));
    let a = fx.run(&["--plan"]);
    assert_eq!(a.rc, 0, "{}", a.err);
    assert_eq!(a.out, fx.run(&[]).out);
    assert_eq!(fx.run(&["--plan", "--json"]).out, fx.run(&["--json"]).out);
}

#[test]
fn subdirectory_as_working_directory_works() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.commit1("sub/x.txt", "x\n", "feat: sub");
    fx.stage1("a.txt", &replace_line(&t3, 3, "f"));
    let p = fx.plan_in(&fx.r.join("sub"), &[]);
    assert_eq!(hunk(&p, 0)["path"], "a.txt");
}

#[test]
fn max_stack_default_ignores_git_config_absorb_max_stack() {
    let fx = Fx::new();
    let (_shas, t) = fx.many(12);
    let mut cfg = std::fs::read_to_string(&fx.cfg).unwrap();
    cfg.push_str("[absorb]\n\tmaxStack = 50\n");
    std::fs::write(&fx.cfg, cfg).unwrap();
    fx.stage1("a.txt", &replace_line(&t, 1, "fixed"));
    assert_eq!(fx.plan(&[])["stack_end"], "limit");
}

#[test]
fn the_error_document_is_written_for_json_only() {
    let fx = Fx::new();
    fx.base10(false);
    let o = fx.run(&[]);
    assert_eq!(o.rc, 1);
    assert!(o.out.is_empty(), "no document without --json: {}", o.out);
    assert!(o.err.contains("nothing-staged"), "{}", o.err);
    let o = fx.run(&["--json"]);
    assert_eq!(
        serde_json::from_str::<serde_json::Value>(&o.out).unwrap()["error"]["code"],
        "nothing-staged"
    );
}

// --- backends -------------------------------------------------------------------------------------------------------

fn subs(fx: &Fx, log: &std::path::Path) -> Vec<String> {
    fx.git_calls(log)
        .iter()
        .filter_map(|(a, _)| Fx::subcommand(a))
        .collect()
}

#[test]
fn jj_directory_is_unsupported_and_no_git_runs() {
    let fx = Fx::new();
    fx.base10(false);
    fx.stage1("a.txt", "changed\n");
    std::fs::create_dir(fx.r.join(".jj")).unwrap();
    let log = fx.logged_git();
    let d = fx.error("unsupported-backend", &[]);
    assert!(d["error"]["message"].as_str().unwrap().contains("jj"));
    assert_eq!(subs(&fx, &log), Vec::<String>::new());
}

#[test]
fn jj_in_an_ancestor_up_to_the_git_boundary() {
    let fx = Fx::new();
    fx.base10(false);
    fx.stage1("a.txt", "changed\n");
    std::fs::create_dir(fx.r.join(".jj")).unwrap();
    std::fs::create_dir(fx.r.join("sub")).unwrap();
    let o = fx.stw_in(&fx.r.join("sub"), &["--json"], &[]);
    assert_eq!(o.rc, 1, "{}", o.err);
    assert_eq!(
        serde_json::from_str::<serde_json::Value>(&o.out).unwrap()["error"]["code"],
        "unsupported-backend"
    );
}

#[test]
fn jj_above_the_git_boundary_does_not_count() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    std::fs::create_dir(fx.s.join(".jj")).unwrap();
    fx.stage1("a.txt", &replace_line(&t3, 3, "f"));
    fx.plan(&[]);
}

#[test]
fn jj_marker_of_the_wrong_type_is_invalid_metadata() {
    let fx = Fx::new();
    fx.base10(false);
    fx.write(".jj", "not a directory");
    fx.error("invalid-metadata", &[]);
}

#[test]
fn gitbutler_markers() {
    let fx = Fx::new();
    let (c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "f"));
    let log = fx.logged_git();
    std::fs::create_dir(fx.r.join(".git/gitbutler")).unwrap();
    fx.error("unsupported-backend", &[]);
    std::fs::remove_dir(fx.r.join(".git/gitbutler")).unwrap();
    fx.plan(&[]);
    fx.g(&["update-ref", "refs/gitbutler/x", &c1]);
    let d = fx.error("unsupported-backend", &[]);
    assert!(d["error"]["message"]
        .as_str()
        .unwrap()
        .contains("GitButler"));
    fx.g(&["update-ref", "-d", "refs/gitbutler/x"]);
    fx.g(&["update-ref", "refs/heads/gitbutler/workspace", &c1]);
    fx.error("unsupported-backend", &[]);
    fx.g(&["pack-refs", "--all"]);
    fx.error("unsupported-backend", &[]);
    // without any marker the plan runs again
    fx.g(&["update-ref", "-d", "refs/heads/gitbutler/workspace"]);
    fx.g(&["pack-refs", "--all"]);
    fx.plan(&[]);
    let s = subs(&fx, &log);
    assert!(s.iter().all(|c| c == "blame"), "{s:?}");
}

#[test]
fn jj_and_gitbutler_together_are_ambiguous() {
    let fx = Fx::new();
    fx.base10(false);
    fx.stage1("a.txt", "changed\n");
    std::fs::create_dir(fx.r.join(".jj")).unwrap();
    std::fs::create_dir(fx.r.join(".git/gitbutler")).unwrap();
    fx.error("ambiguous-backend", &[]);
}

#[test]
fn refusal_reads_no_index_and_runs_no_git() {
    let fx = Fx::new();
    fx.base10(false);
    fx.stage1("a.txt", "changed\n");
    std::fs::create_dir(fx.r.join(".git/gitbutler")).unwrap();
    let log = fx.logged_git();
    fx.error("unsupported-backend", &[]);
    assert!(fx.git_calls(&log).is_empty());
}
