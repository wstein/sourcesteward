// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! `stw absorb` writes nothing and starts no helper: the repository tree before and after, the git calls, a partial clone
//! and a failing blame.
use std::time::{Duration, SystemTime};

use serde_json::Value;

use super::absorb_fx::*;

fn scenario(fx: &Fx) -> (String, String, String, String) {
    let (c1, c2, c3, t3) = fx.base10(false);
    fx.stage(&[
        (
            "a.txt",
            Some(&replace_line(&replace_line(&t3, 3, "fixed3"), 5, "x5")),
        ),
        ("new.txt", Some("n\n")),
    ]);
    (c1, c2, c3, t3)
}

fn set_mtime(path: &std::path::Path, secs: u64) {
    let f = std::fs::File::options().write(true).open(path).unwrap();
    f.set_modified(SystemTime::UNIX_EPOCH + Duration::from_secs(secs))
        .unwrap();
}

#[test]
fn repository_tree_is_identical_after_a_plan() {
    let fx = Fx::new();
    scenario(&fx);
    let before = Fx::snapshot(&fx.r);
    for args in [
        vec![],
        vec!["--json"],
        vec!["--json", "--max-stack", "2"],
        vec!["--ambiguous-newest"],
        vec!["--base", "HEAD~1"],
    ] {
        let o = fx.run(&args);
        assert_eq!(o.rc, 0, "{args:?}: {}", o.err);
        assert!(!o.out.is_empty());
    }
    assert_eq!(Fx::snapshot(&fx.r), before);
}

#[test]
fn repository_tree_is_identical_after_refusals() {
    let fx = Fx::new();
    scenario(&fx);
    std::fs::create_dir(fx.r.join(".git/gitbutler")).unwrap();
    let before = Fx::snapshot(&fx.r);
    assert_eq!(fx.run(&["--json"]).rc, 1);
    assert_eq!(Fx::snapshot(&fx.r), before);
    std::fs::remove_dir(fx.r.join(".git/gitbutler")).unwrap();
    fx.g(&["reset", "-q"]);
    let before = Fx::snapshot(&fx.r);
    assert_eq!(fx.run(&["--json"]).rc, 1);
    assert_eq!(Fx::snapshot(&fx.r), before);
}

#[test]
fn a_stale_stat_cache_is_not_refreshed() {
    let fx = Fx::new();
    scenario(&fx);
    set_mtime(&fx.r.join("a.txt"), 1_000_000_000);
    fx.write("b.txt", "untracked\n");
    fx.g_ok(&["update-index", "--really-refresh"]);
    set_mtime(&fx.r.join("a.txt"), 1_100_000_000);
    let stale = std::fs::read(fx.r.join(".git/index")).unwrap();
    let before = Fx::snapshot(&fx.r);
    assert_eq!(fx.run(&["--json"]).rc, 0);
    assert_eq!(std::fs::read(fx.r.join(".git/index")).unwrap(), stale);
    assert_eq!(Fx::snapshot(&fx.r), before);
}

#[test]
fn the_status_command_would_have_written_the_index() {
    // proof that the previous test can fail: git status does refresh a stale index in this fixture
    let fx = Fx::new();
    scenario(&fx);
    set_mtime(&fx.r.join("a.txt"), 1_000_000_000);
    fx.g_ok(&["update-index", "--refresh"]);
    set_mtime(&fx.r.join("a.txt"), 1_100_000_000);
    let before = std::fs::read(fx.r.join(".git/index")).unwrap();
    fx.g(&["status", "-s"]);
    assert_ne!(std::fs::read(fx.r.join(".git/index")).unwrap(), before);
}

#[test]
fn only_blame_runs_with_the_lock_setting_no_lazy_fetch_and_fsmonitor_off() {
    let fx = Fx::new();
    scenario(&fx);
    let log = fx.logged_git();
    assert_eq!(fx.run(&["--json"]).rc, 0);
    let calls = fx.git_calls(&log);
    assert!(!calls.is_empty(), "git was not run through the wrapper");
    for (args, lock) in &calls {
        assert_eq!(Fx::subcommand(args).as_deref(), Some("blame"), "{args:?}");
        assert_eq!(
            lock, "01",
            "{args:?}: GIT_OPTIONAL_LOCKS=0 and GIT_NO_LAZY_FETCH=1"
        );
        assert!(
            args.contains(&"core.fsmonitor=false".to_string()),
            "{args:?}"
        );
        assert!(
            args.contains(&"--literal-pathspecs".to_string()),
            "{args:?}"
        );
        assert!(args.contains(&"--no-textconv".to_string()), "{args:?}");
        assert!(args.contains(&"--porcelain".to_string()), "{args:?}");
        assert!(
            args.contains(&"core.hooksPath=/dev/null".to_string()),
            "{args:?}"
        );
    }
}

#[test]
fn blame_is_asked_for_removed_lines_only_and_at_the_head_commit() {
    let fx = Fx::new();
    let (_c1, _c2, c3, t3) = fx.base10(true);
    // one replaced line (blame) and one pure insertion (no blame)
    fx.stage1(
        "a.txt",
        &replace_line(&insert_after(&t3, 5, "ins"), 3, "fixed"),
    );
    let log = fx.logged_git();
    assert_eq!(fx.run(&["--json"]).rc, 0);
    let calls = fx.git_calls(&log);
    assert_eq!(calls.len(), 1, "{calls:?}");
    let args = &calls[0].0;
    let at = |k: &str| args.iter().position(|a| a == k).unwrap();
    assert_eq!(args[at("-L") + 1], "3,3");
    assert_eq!(
        args[at("-L") + 2],
        c3,
        "blame at the commit, not at the name HEAD"
    );
    assert_eq!(args.last().unwrap(), "a.txt");
    assert_eq!(args[args.len() - 2], "--");
}

#[test]
fn external_diff_textconv_and_hooks_never_run() {
    let fx = Fx::new();
    let (_c1, c2, _c3, t3) = fx.base10(false);
    let marker = fx.s.join("marker");
    for name in ["ext", "conv", "hook"] {
        use std::os::unix::fs::PermissionsExt;
        let p = fx.s.join(name);
        std::fs::write(
            &p,
            format!("#!/bin/sh\necho {name} >> {}\nexit 0\n", marker.display()),
        )
        .unwrap();
        std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
    }
    let s = fx.s.display();
    let mut cfg = std::fs::read_to_string(fx.r.join(".git/config")).unwrap();
    cfg.push_str(&format!(
        "[diff]\n\texternal = {s}/ext\n[diff \"x\"]\n\ttextconv = {s}/conv\n[core]\n\tfsmonitor = {s}/hook\n\thooksPath = {s}\n"
    ));
    std::fs::write(fx.r.join(".git/config"), cfg).unwrap();
    std::fs::write(fx.r.join(".gitattributes"), "*.txt diff=x\n").unwrap();
    fx.stage1("a.txt", &replace_line(&t3, 3, "fixed"));
    let _ = std::fs::remove_file(&marker);
    let hook = format!("{s}/hook");
    let o = fx.run_env(
        &["--json"],
        &[
            ("GIT_EXTERNAL_DIFF", &format!("{s}/ext")),
            ("GIT_PAGER", &hook),
            ("PAGER", &hook),
        ],
    );
    assert!(!o.out.trim().is_empty(), "no output: {}", o.err);
    let p: Value = serde_json::from_str(&o.out).unwrap();
    assert_eq!(hunk(&p, 0)["target"]["sha"], c2);
    assert!(
        !marker.exists(),
        "a helper ran: {:?}",
        std::fs::read_to_string(&marker)
    );
}

#[test]
fn repository_diff_settings_do_not_change_the_plan() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "fixed"));
    let reference = fx.run(&["--json"]);
    assert_eq!(reference.rc, 0, "{}", reference.err);
    let mut cfg = std::fs::read_to_string(fx.r.join(".git/config")).unwrap();
    cfg.push_str("[diff]\n\tnoprefix = true\n\tmnemonicPrefix = true\n\talgorithm = patience\n\trenames = copies\n\tcontext = 9\n\tindentHeuristic = true\n\tinterHunkContext = 5\n\tcolor = always\n");
    std::fs::write(fx.r.join(".git/config"), cfg).unwrap();
    assert_eq!(fx.run(&["--json"]).out, reference.out);
}

#[test]
fn a_dirty_working_tree_is_not_read() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "fixed"));
    let reference = fx.plan(&[]);
    fx.write("a.txt", "completely different\n");
    assert_eq!(fx.plan(&[]), reference);
    std::fs::remove_file(fx.r.join("a.txt")).unwrap();
    assert_eq!(fx.plan(&[]), reference);
}

// --- partial clone, failing blame ------------------------------------------------------------------------------------

#[test]
fn partial_clone_is_refused_and_nothing_is_fetched() {
    // a blob:none clone of a throwaway source repository (the source gets uploadpack.allowFilter: a config change in a
    // scratch repository only); the clone keeps the blobs of HEAD, older blobs are missing and a lazy fetch would load them
    let fx = Fx::new();
    let mut t = l(10);
    fx.commit1("a.txt", &t, "feat: one");
    t = replace_line(&t, 3, "l03 by c2");
    fx.commit1("a.txt", &t, "feat: two");
    t = replace_line(&t, 8, "l08 by c3");
    fx.commit1("a.txt", &t, "feat: three");
    fx.g(&["config", "uploadpack.allowFilter", "true"]);
    let clone = fx.s.join("work/clone");
    let src = format!("file://{}", fx.r.display());
    let o = fx.g_in(
        &fx.s,
        &[
            "clone",
            "-q",
            "--filter=blob:none",
            &src,
            clone.to_str().unwrap(),
        ],
        &[],
    );
    assert_eq!(o.rc, 0, "{}", o.err);
    for r in fx
        .g_in(
            &clone,
            &["for-each-ref", "--format=%(refname)", "refs/remotes/"],
            &[],
        )
        .out
        .split_whitespace()
    {
        if r != "refs/remotes/origin/HEAD" {
            fx.g_in(&clone, &["update-ref", "-d", r], &[]);
        }
    }
    fx.g_in(
        &clone,
        &["symbolic-ref", "--delete", "refs/remotes/origin/HEAD"],
        &[],
    );
    std::fs::write(clone.join("a.txt"), replace_line(&t, 3, "l03 fixed")).unwrap();
    assert_eq!(fx.g_in(&clone, &["add", "a.txt"], &[]).rc, 0);
    let promisor = fx.g_in(
        &clone,
        &["config", "--get-regexp", r"^remote\..*\.promisor$"],
        &[],
    );
    assert!(
        promisor.out.contains("promisor"),
        "fixture is a partial clone"
    );
    let before = Fx::snapshot(&clone);
    let o = fx.stw_in(&clone, &["--json"], &[]);
    assert_eq!(o.rc, 1, "{}", o.err);
    let d: Value =
        serde_json::from_str(&o.out).unwrap_or_else(|e| panic!("{e}: {} / {}", o.out, o.err));
    assert_eq!(d["error"]["code"], "partial-clone-unsupported");
    assert_eq!(
        Fx::snapshot(&clone),
        before,
        "no promisor pack, no object written"
    );
}

#[test]
fn a_failing_blame_is_an_error_and_never_a_plan() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "fixed"));
    fx.install_git_wrapper(&format!(
        "#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = blame ]; then echo \"fatal: simulated\" >&2; exit 128; fi; done\nexec {} \"$@\"\n",
        which_git().display()
    ));
    let o = fx.run(&["--json"]);
    assert_eq!(o.rc, 1, "{}", o.out);
    let d: Value = serde_json::from_str(&o.out).unwrap();
    assert!(
        d.get("error").is_some(),
        "a hunk was planned without the evidence of blame"
    );
    assert_eq!(d["error"]["code"], "blame-failed");
}

#[test]
fn a_missing_git_program_is_blame_failed_too() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "fixed"));
    let empty = fx.s.join("bin");
    let o = fx.run_with_path(&["--json"], empty.to_str().unwrap());
    assert_eq!(o.rc, 1, "{}", o.out);
    assert_eq!(
        serde_json::from_str::<Value>(&o.out).unwrap()["error"]["code"],
        "blame-failed"
    );
}
