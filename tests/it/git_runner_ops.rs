//! `Op::RangeDiff` and `Op::FetchPullHead` of the `via: git` runner (carry-over, M4): fixed argument vector,
//! pinned environment and configuration, version, typed unsupported without git, refused repository config.
//! Go reference: `internal/carryover/git.go` (guard, fetch flags, pinned config).
//!
//! Harness confinement: every test works in a tempdir it created; the runner is given that directory (an empty
//! path is refused); real git runs with an isolated home and `GIT_CEILING_DIRECTORIES`; no `GIT_*` of the caller
//! reaches a child.
use std::ffi::OsString;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};

use crate::common::*;
use stw::git_runner::{GitError, Hex40, Op, Runner, Version, PULL_HEAD_NAMESPACE};

fn osv(pairs: &[(&str, &str)]) -> Vec<(OsString, OsString)> {
    pairs
        .iter()
        .map(|(k, v)| (OsString::from(k), OsString::from(v)))
        .collect()
}

fn tmp() -> tempfile::TempDir {
    tempfile::Builder::new()
        .prefix("stw-runner-ops-")
        .tempdir()
        .unwrap()
}

fn h(c: char) -> Hex40 {
    Hex40::parse(&c.to_string().repeat(40)).unwrap()
}

fn range_diff() -> Op {
    Op::RangeDiff {
        old_base: h('1'),
        old_head: h('2'),
        new_base: h('3'),
        new_head: h('4'),
    }
}

fn argv(op: Op) -> Vec<String> {
    let t = tmp();
    Runner::from_env(osv(&[("PATH", "/usr/bin")]))
        .argv(t.path(), op)
        .unwrap()
        .iter()
        .map(|a| a.to_string_lossy().into_owned())
        .collect()
}

fn after_common(v: &[String]) -> &[String] {
    // `-C <repo>` and the seven common `-c` pairs (tests/git_runner.rs pins them)
    assert_eq!((v[0].as_str(), v[2].as_str()), ("-C", "-c"));
    &v[16..]
}

fn script(dir: &Path, name: &str, body: &str) -> PathBuf {
    let p = dir.join(name);
    std::fs::write(&p, format!("#!/bin/sh\n{body}\n")).unwrap();
    std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
    p
}

fn real_git_available() -> bool {
    std::process::Command::new("git")
        .arg("--version")
        .output()
        .is_ok_and(|o| o.status.success())
}

fn real_runner(home: &Path) -> Runner {
    Runner::from_env(osv(&[
        ("PATH", std::env::var("PATH").unwrap().as_str()),
        ("HOME", home.to_str().unwrap()),
        ("XDG_CONFIG_HOME", home.join(".config").to_str().unwrap()),
    ]))
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
fn hex40_accepts_only_lower_case_object_ids() {
    // Go TestParseRequestAcceptsOnlyFullLowercaseObjectIDs, TestRunnerRefusesInvalidIDsBeforeRunningGit
    let ok = "0123456789abcdef0123456789abcdef01234567";
    assert_eq!(Hex40::parse(ok).unwrap().as_str(), ok);
    for bad in [
        "",
        "main",
        &ok[..39],
        &format!("{ok}0"),
        &ok.to_uppercase(),
        &format!("-{}", &ok[1..]),
        &format!("{}..{}", &ok[..19], &ok[..19]),
        &format!(" {}", &ok[1..]),
        &format!("{}\n", &ok[..39]),
        &"g".repeat(40),
        &"a".repeat(64),
    ] {
        assert!(Hex40::parse(bad).is_none(), "{bad:?}");
    }
}

#[test]
fn range_diff_argv_is_fixed_and_pins_the_patch_comparison() {
    // Go TestGuardAllowsOnlyTheReviewedVectors, TestDiffAndLogPinEveryOutputFlag (the rows that apply)
    let v = argv(range_diff());
    let tail = after_common(&v);
    assert_eq!(
        tail,
        [
            "-c",
            "diff.ignoreSubmodules=none",
            "-c",
            "core.attributesFile=/dev/null",
            "-c",
            "core.bigFileThreshold=512m",
            "-c",
            "diff.renames=false",
            "-c",
            "diff.context=3",
            "-c",
            "diff.interHunkContext=0",
            "-c",
            "diff.algorithm=myers",
            "-c",
            "diff.indentHeuristic=true",
            "-c",
            "diff.relative=false",
            "-c",
            "diff.suppressBlankEmpty=false",
            "-c",
            "core.quotePath=true",
            "-c",
            "log.showSignature=false",
            "-c",
            "log.follow=false",
            "-c",
            "log.mailmap=false",
            "range-diff",
            "--no-color",
            "--no-patch",
            "--no-notes",
            &format!("{}..{}", "1".repeat(40), "2".repeat(40)),
            &format!("{}..{}", "3".repeat(40), "4".repeat(40)),
        ]
    );
}

#[test]
fn fetch_pull_head_argv_writes_only_the_private_ref() {
    // Go TestFetchWritesOnlyThePrivateRef, TestFetchWritesNoConfiguredRefmapRef (argv part)
    let v = argv(Op::FetchPullHead { pr: 12 });
    assert_eq!(
        after_common(&v),
        [
            "fetch",
            "--quiet",
            "--no-tags",
            "--no-recurse-submodules",
            "--upload-pack=git-upload-pack",
            "--no-write-fetch-head",
            "--refmap=",
            "--",
            "origin",
            "+refs/pull/12/head:refs/stw/carry-over/pr-12",
        ]
    );
    assert_eq!(PULL_HEAD_NAMESPACE, "refs/stw/carry-over/");
    let big = argv(Op::FetchPullHead { pr: u64::MAX });
    assert!(big
        .last()
        .unwrap()
        .starts_with("+refs/pull/18446744073709551615/head:"));
}

#[test]
fn a_pull_request_number_of_zero_is_refused_before_git_starts() {
    let t = tmp();
    let r = Runner::from_env(osv(&[("PATH", "/usr/bin")]));
    let e = r.argv(t.path(), Op::FetchPullHead { pr: 0 }).unwrap_err();
    assert!(matches!(e, GitError::Argument(_)), "{e:?}");
    assert_eq!(e.kind(), "argument");
    assert!(!e.is_unsupported());
    // `run` fails the same way, also without any git on PATH
    let e = r.run(t.path(), Op::FetchPullHead { pr: 0 }).unwrap_err();
    assert!(matches!(e, GitError::Argument(_)), "{e:?}");
}

#[test]
fn range_diff_pins_the_attribute_source_and_the_other_ops_do_not() {
    let t = tmp();
    let r = Runner::from_env(osv(&[("PATH", "/usr/bin"), ("GIT_ATTR_SOURCE", "HEAD")]));
    let env = |op: &Op| r.child_env_for(t.path(), op).unwrap();
    let get = |e: &[(OsString, OsString)], k: &str| {
        e.iter()
            .rev()
            .find(|(n, _)| n == k)
            .map(|(_, v)| v.to_string_lossy().into_owned())
    };
    let e = env(&range_diff());
    assert_eq!(
        get(&e, "GIT_ATTR_SOURCE").as_deref(),
        Some("4b825dc642cb6eb9a060e54bf8d69288fbee4904"),
        "the empty tree: no .gitattributes of the worktree"
    );
    assert_eq!(get(&e, "GIT_ATTR_NOSYSTEM").as_deref(), Some("1"));
    assert_eq!(get(&e, "GIT_NO_REPLACE_OBJECTS").as_deref(), Some("1"));
    assert_eq!(get(&e, "LC_ALL").as_deref(), Some("C"));
    // a caller's value never passes, and the fetch ops do not set it
    for op in [Op::FetchOrigin, Op::FetchPullHead { pr: 1 }] {
        assert_eq!(get(&env(&op), "GIT_ATTR_SOURCE"), None);
    }
}

#[test]
fn range_diff_without_git_is_typed_unsupported() {
    // Plan M0: typed unsupported with an empty PATH
    let t = tmp();
    for path in ["", "relative/bin:."] {
        let r = Runner::from_env(osv(&[("PATH", path)]));
        for op in [range_diff(), Op::FetchPullHead { pr: 3 }] {
            let e = r.run(t.path(), op).unwrap_err();
            assert!(matches!(e, GitError::NotFound), "{path:?}: {e:?}");
            assert!(e.is_unsupported());
        }
    }
}

/// A runner whose `git` is a script that reports `version` and succeeds.
fn fake_git_runner(t: &Path, version: &str) -> Runner {
    let bin = t.join("bin");
    std::fs::create_dir_all(&bin).unwrap();
    script(&bin, "git", &format!("echo 'git version {version}'"));
    Runner::from_env(osv(&[(
        "PATH",
        &format!("{}:/usr/bin:/bin", bin.display()),
    )]))
}

#[test]
fn range_diff_needs_git_2_41() {
    // GIT_ATTR_SOURCE exists since Git 2.41; 2.40 ignores it silently, so a `-diff` attribute could hide a change
    assert_eq!(range_diff().min_version(), Version(2, 41, 0));
    assert_eq!(Op::FetchPullHead { pr: 1 }.min_version(), Version(2, 30, 0));
    let t = tmp();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    let r = fake_git_runner(t.path(), "2.40.1");
    r.set_command("review carry-over");
    let e = r.run(&repo, range_diff()).unwrap_err();
    assert!(
        matches!(&e, GitError::Version { required, .. } if *required == Version(2, 41, 0)),
        "{e:?}"
    );
    assert_eq!(
        e.to_string(),
        "stw needs Git 2.41 or newer for review carry-over (found 2.40.1)"
    );
    assert!(e.is_unsupported());
    let t2 = tmp();
    let repo2 = t2.path().join("repo");
    gix::init(&repo2).unwrap();
    let ok = fake_git_runner(t2.path(), "2.41.0");
    ok.run(&repo2, range_diff()).unwrap();
}

#[test]
fn the_floor_of_every_op_is_declared_and_enforced() {
    // 2.36: `-c core.fsmonitor=false` is a boolean only since 2.36 (before, the value names a hook command), so
    // every op that reads the index needs it. Static evidence (Documentation/config/core.txt of v2.35 vs v2.36);
    // with 2.35.3 no test of add, sync --submodules or sync restore failed (the 48 failures are range-diff/merge-tree
    // tests that need 2.40/2.41), so the bound is the documented one, not an observed failure.
    // 2.30: the fail-closed value for the fetch/update-ref vectors (their options are older, 2.29 for the fetch).
    let sub = || stw::add::SubPath::parse("projects/tool").unwrap();
    let name = || stw::add::RepoName::parse("tool").unwrap();
    let ops: Vec<(Op, Version)> = vec![
        (Op::FetchOrigin, Version(2, 30, 0)),
        (Op::FetchOriginPrune { prune: true }, Version(2, 30, 0)),
        (Op::FetchOriginNoPruneTags, Version(2, 30, 0)),
        (Op::FetchPullHead { pr: 1 }, Version(2, 30, 0)),
        (
            Op::UpdateBranchCas {
                run: stw::git_runner::RunId::parse("20260101T000000Z-0123abcd").unwrap(),
                branch: stw::git_runner::BranchRef::parse("refs/heads/x").unwrap(),
                new: h('a'),
                expected: h('b'),
            },
            Version(2, 30, 0),
        ),
        (range_diff(), Version(2, 41, 0)),
        (Op::SubmoduleSync { path: sub() }, Version(2, 36, 0)),
        (Op::SubmoduleUpdate { path: sub() }, Version(2, 36, 0)),
        (Op::CheckAttrGitmodules { cached: true }, Version(2, 36, 0)),
        (Op::CheckoutClone, Version(2, 36, 0)),
        (
            Op::SubmoduleAddExisting {
                url: stw::add::CanonicalUrl::new(&name(), &name()),
                path: sub(),
            },
            Version(2, 36, 0),
        ),
        (Op::AbsorbGitDirs { path: sub() }, Version(2, 36, 0)),
        (
            Op::CommitSubmodule {
                name: name(),
                path: sub(),
            },
            Version(2, 36, 0),
        ),
        (Op::ResetKeep { target: h('c') }, Version(2, 36, 0)),
        (
            Op::SyncMerge {
                target: h('d'),
                autostash: false,
            },
            Version(2, 36, 0),
        ),
        (
            Op::DiffRange {
                base: h('e'),
                head: h('f'),
            },
            Version(2, 30, 0),
        ),
        (Op::PatchIdStable, Version(2, 30, 0)),
        (
            Op::Workspace(stw::git_runner::WorkspaceOp::IndexEntries),
            Version(2, 46, 0),
        ),
        // `GIT_NO_LAZY_FETCH` is honored since 2.46; an older git would fetch missing objects of a partial clone
        (
            Op::BlameLines {
                rev: h('a'),
                path: stw::git_runner::BlamePath::parse(b"a.txt").unwrap(),
                first: 1,
                last: 1,
            },
            Version(2, 46, 0),
        ),
        // `--no-update-refs` and `rebase.updateRefs` exist since 2.38
        (
            Op::SyncRebase {
                upstream: stw::git_runner::RemoteRef::parse("refs/remotes/origin/main").unwrap(),
                autostash: false,
            },
            Version(2, 38, 0),
        ),
        (Op::AbsorbAutosquash { onto: None }, Version(2, 38, 0)),
    ];
    // no wildcard: a new `Op` does not compile until it is listed above
    let listed = |op: &Op| match op {
        Op::Workspace(_)
        | Op::FetchOrigin
        | Op::FetchOriginPrune { .. }
        | Op::FetchOriginNoPruneTags
        | Op::FetchPullHead { .. }
        | Op::UpdateBranchCas { .. }
        | Op::RangeDiff { .. }
        | Op::DiffRange { .. }
        | Op::PatchIdStable
        | Op::BlameLines { .. }
        | Op::SubmoduleSync { .. }
        | Op::SubmoduleUpdate { .. }
        | Op::CheckAttrGitmodules { .. }
        | Op::CheckoutClone
        | Op::SubmoduleAddExisting { .. }
        | Op::AbsorbGitDirs { .. }
        | Op::CommitSubmodule { .. }
        | Op::SyncMerge { .. }
        | Op::SyncRebase { .. }
        | Op::AbsorbAutosquash { .. }
        | Op::ResetKeep { .. } => (),
    };
    for (op, floor) in ops {
        listed(&op);
        assert_eq!(op.min_version(), floor, "{op:?}");
        // one patch level below the floor is refused before anything runs
        let below = format!("{}.{}.9", floor.0, floor.1 - 1);
        let t = tmp();
        let repo = t.path().join("repo");
        gix::init(&repo).unwrap();
        let r = fake_git_runner(t.path(), &below);
        let e = r.run(&repo, op.clone()).unwrap_err();
        assert!(
            matches!(&e, GitError::Version { required, .. } if *required == floor),
            "{op:?}: {e:?}"
        );
    }
}

#[test]
fn the_version_message_names_the_command_and_escapes_the_found_text() {
    let t = tmp();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    // an unreadable `git --version` is shown escaped, never raw
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    script(&bin, "git", "printf 'bogus \\033[31mred\\n'");
    let r = Runner::from_env(osv(&[(
        "PATH",
        &format!("{}:/usr/bin:/bin", bin.display()),
    )]));
    r.set_command("prove");
    let e = r.run(&repo, range_diff()).unwrap_err();
    let m = e.to_string();
    assert!(
        m.starts_with("stw needs Git 2.41 or newer for prove (found "),
        "{m}"
    );
    assert!(!m.contains('\x1b') && !m.contains('\n'), "{m:?}");
    // without a command the text still says what is needed
    let r2 = fake_git_runner(t.path(), "2.35.3");
    let e = r2.run(&repo, Op::ResetKeep { target: h('c') }).unwrap_err();
    assert_eq!(
        e.to_string(),
        "stw needs Git 2.36 or newer for this step (found 2.35.3)"
    );
}

#[test]
fn local_config_that_starts_a_diff_program_is_refused_for_range_diff_only() {
    // Go TestNoDiffDriverRunsAndAWhitespaceChangeStaysNo: Go ignores the driver, Rust refuses by name (typed).
    let cases: &[(&str, &str)] = &[
        ("[diff]\n\texternal = evil\n", "diff.external"),
        ("[diff \"x\"]\n\ttextconv = evil\n", "diff.x.textconv"),
        ("[diff \"x\"]\n\tcommand = evil\n", "diff.x.command"),
        ("[diff \"x\"]\n\tTextConv = evil\n", "diff.x.textconv"),
    ];
    for (text, key) in cases {
        let fx = Fx::new();
        append_local_config(&fx.path, text);
        let t = tmp();
        // an executable `git` is needed for the check to be reached; a fake one that must never run
        let bin = t.path().join("bin");
        std::fs::create_dir(&bin).unwrap();
        script(&bin, "git", "echo 'git version 2.47.3'");
        let r2 = Runner::from_env(osv(&[("PATH", &format!("{}:/usr/bin", bin.display()))]));
        let e = r2.run(&fx.path, range_diff()).unwrap_err();
        assert!(
            matches!(&e, GitError::Refused { key: k } if k == key),
            "{key}: {e:?}"
        );
        assert!(e.is_unsupported());
        // the same keys do not hinder a fetch (they do not run there)
        let e = r2.run(&fx.path, Op::FetchPullHead { pr: 1 });
        assert!(!matches!(e, Err(GitError::Refused { .. })), "{key}: {e:?}");
    }
}

#[test]
fn info_attributes_with_active_lines_is_refused_for_range_diff() {
    // Go TestAttributesThatHideTextDoNotHideACommit
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    script(&bin, "git", "echo 'git version 2.47.3'");
    let r = Runner::from_env(osv(&[("PATH", &format!("{}:/usr/bin", bin.display()))]));
    let fx = Fx::new();
    let info = fx.path.join(".git/info");
    std::fs::create_dir_all(&info).unwrap();
    // empty, comments and blank lines do not hide anything: the fake git is reached (and fails to run range-diff)
    std::fs::write(info.join("attributes"), "# nothing\n\n  \n").unwrap();
    let e = r.run(&fx.path, range_diff());
    assert!(!matches!(e, Err(GitError::Refused { .. })), "{e:?}");
    for line in ["* -diff\n", "# c\n*.rs binary\n", "  *  -diff"] {
        std::fs::write(info.join("attributes"), line).unwrap();
        let e = r.run(&fx.path, range_diff()).unwrap_err();
        assert!(
            matches!(&e, GitError::Refused { key } if key == "info/attributes"),
            "{line:?}: {e:?}"
        );
        assert!(e.is_unsupported());
        assert!(e.to_string().contains("info/attributes"));
        // the fetch never reads attributes
        let e = r.run(&fx.path, Op::FetchPullHead { pr: 1 });
        assert!(
            !matches!(e, Err(GitError::Refused { .. })),
            "{line:?}: {e:?}"
        );
    }
}

// ---- real git ------------------------------------------------------------------------------------

/// `up`: a repository with `main` (c0) and `refs/pull/7/head` (c1 on top of c0); `clone`: empty, `origin` = `up`.
fn pull_fixture(t: &Path) -> (Fx, Fx, String, String) {
    let up = Fx::new();
    let c0 = up.commit(None, &[("a.txt", Some("a\n"))], "c0");
    let c1 = up.commit(Some(c0), &[("a.txt", Some("a\nb\n"))], "c1");
    up.branch("main", c0);
    up.set_ref("refs/pull/7/head", c1);
    let clone = Fx::new();
    clone.set_origin_url(up.path.to_str().unwrap());
    let _ = t;
    (up, clone, c0.to_string(), c1.to_string())
}

#[test]
fn real_git_fetches_the_pull_head_into_the_private_ref_and_nothing_else() {
    // Go TestFetchWritesOnlyThePrivateRef, TestFetchWritesNoConfiguredRefmapRef
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let t = tmp();
    let (_up, clone, _c0, c1) = pull_fixture(t.path());
    // a configured refmap that would write `refs/remotes/origin/*`, and one more fetch refspec
    append_local_config(
        &clone.path,
        "[remote \"origin\"]\n\tfetch = +refs/pull/*/head:refs/remotes/origin/pr/*\n\ttagopt = --tags\n",
    );
    let before = ref_names(&clone.path);
    real_runner(t.path())
        .run(&clone.path, Op::FetchPullHead { pr: 7 })
        .unwrap();
    let after = ref_names(&clone.path);
    let added: Vec<_> = after.iter().filter(|r| !before.contains(r)).collect();
    assert_eq!(added, ["refs/stw/carry-over/pr-7"], "only the private ref");
    assert_eq!(
        std::fs::read_to_string(clone.path.join(".git/refs/stw/carry-over/pr-7"))
            .unwrap()
            .trim(),
        c1
    );
    assert!(
        !clone.path.join(".git/FETCH_HEAD").exists(),
        "no FETCH_HEAD"
    );
}

#[test]
fn real_git_runs_no_hook_during_the_pull_head_fetch() {
    // Go TestNoHookRunsDuringTheFetch
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let t = tmp();
    let (_up, clone, _c0, _c1) = pull_fixture(t.path());
    let marker = t.path().join("hook-ran");
    for hook in ["reference-transaction", "post-fetch", "post-checkout"] {
        script(
            &clone.path.join(".git/hooks"),
            hook,
            &format!("touch '{}'", marker.display()),
        );
    }
    real_runner(t.path())
        .run(&clone.path, Op::FetchPullHead { pr: 7 })
        .unwrap();
    assert!(!marker.exists(), "a repository hook ran during the fetch");
}

#[test]
fn real_git_pull_head_fetch_of_a_missing_or_unreachable_remote_fails_typed() {
    // Go TestUnreachableRemoteIsUnknown: a failing fetch is an error, never a value
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let t = tmp();
    let (_up, clone, _c0, _c1) = pull_fixture(t.path());
    let r = real_runner(t.path());
    let e = r.run(&clone.path, Op::FetchPullHead { pr: 8 }).unwrap_err();
    assert!(
        matches!(e, GitError::Exit { .. }),
        "no such pull request: {e:?}"
    );
    clone.set_origin_url(t.path().join("gone").to_str().unwrap());
    let e = r.run(&clone.path, Op::FetchPullHead { pr: 7 }).unwrap_err();
    assert!(matches!(e, GitError::Exit { .. }), "unreachable: {e:?}");
}

fn ref_names(repo: &Path) -> Vec<String> {
    fn walk(dir: &Path, prefix: &str, out: &mut Vec<String>) {
        let Ok(rd) = std::fs::read_dir(dir) else {
            return;
        };
        for e in rd.flatten() {
            let name = format!("{prefix}{}", e.file_name().to_string_lossy());
            if e.path().is_dir() {
                walk(&e.path(), &format!("{name}/"), out);
            } else {
                out.push(name);
            }
        }
    }
    let mut v = Vec::new();
    walk(&repo.join(".git/refs"), "refs/", &mut v);
    v.sort();
    v
}

#[test]
fn pull_head_fetch_refuses_http_https_git_and_unknown_transports_before_git_starts() {
    // Chartroom decision: the fetch of an https remote stays with gix; the installed git fetches ssh and local remotes
    // only. A stub git logs every call: for a refused URL no `fetch` runs (and no DNS lookup is possible).
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let log = bin.join("calls.log");
    script(
        &bin,
        "git",
        &format!(
            "if [ \"$1\" = \"--version\" ]; then echo 'git version 2.47.3'; exit 0; fi\necho \"CALL $*\" >> '{}'\nexit 0",
            log.display()
        ),
    );
    let r = Runner::from_env(osv(&[(
        "PATH",
        &format!("{}:/usr/bin:/bin", bin.display()),
    )]));
    let refused = [
        "https://github.com/o/r.git",
        "http://example.invalid/o/r.git",
        "git://example.invalid/o/r.git",
        "ext::sh -c touch% /tmp/x",
        "ftp://example.invalid/o/r",
    ];
    for url in refused {
        let fx = Fx::new();
        fx.set_origin_url(url);
        let e = r.run(&fx.path, Op::FetchPullHead { pr: 7 }).unwrap_err();
        assert!(matches!(e, GitError::Transport { .. }), "{url}: {e:?}");
        assert!(e.is_unsupported(), "{url}");
        assert_eq!(e.kind(), "transport");
        assert!(e.to_string().contains("gix"), "{url}: {e}");
    }
    assert!(!log.exists(), "git ran for a refused remote");
    // ssh (both spellings), file and a local path reach git
    for url in [
        "ssh://git@example.invalid/o/r.git",
        "git@example.invalid:o/r.git",
        "file:///nonexistent/up",
        "/nonexistent/up",
    ] {
        let fx = Fx::new();
        fx.set_origin_url(url);
        let before = std::fs::read_to_string(&log).unwrap_or_default();
        r.run(&fx.path, Op::FetchPullHead { pr: 7 }).unwrap();
        let after = std::fs::read_to_string(&log).unwrap();
        assert!(
            after[before.len()..].contains("fetch"),
            "{url}: git did not run: {after}"
        );
    }
    // no origin at all: typed repository error, git not started
    let fx = Fx::new();
    fx.set_origin_url("");
    let e = r.run(&fx.path, Op::FetchPullHead { pr: 7 });
    assert!(e.is_err());
    // the other fetch op is not changed by this rule
    let fx = Fx::new();
    r.run(&fx.path, Op::FetchOrigin).unwrap();
}

#[test]
fn pull_head_fetch_allows_only_the_file_and_ssh_transports_even_through_a_user_rewrite() {
    // The URL check reads the repository's own config; a `url.<base>.insteadOf` of the user's config can still
    // rewrite an ssh-looking origin to https. GIT_ALLOW_PROTOCOL stops that: no http, https or git transport.
    let t = tmp();
    let r = Runner::from_env(osv(&[("PATH", "/usr/bin")]));
    let env = r
        .child_env_for(t.path(), &Op::FetchPullHead { pr: 1 })
        .unwrap();
    let allow = env
        .iter()
        .rev()
        .find(|(k, _)| k == "GIT_ALLOW_PROTOCOL")
        .map(|(_, v)| v.to_string_lossy().into_owned());
    assert_eq!(allow.as_deref(), Some("file:ssh"));
    // real git: the rewrite to https is refused by git itself, no lookup happens
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let home = tmp();
    std::fs::write(
        home.path().join(".gitconfig"),
        "[url \"https://evil.invalid/\"]\n\tinsteadOf = git@h:\n",
    )
    .unwrap();
    let fx = Fx::new();
    fx.set_origin_url("git@h:o/r");
    let e = real_runner(home.path())
        .run(&fx.path, Op::FetchPullHead { pr: 7 })
        .unwrap_err();
    match &e {
        GitError::Exit { stderr, .. } => assert!(
            stderr.contains("not allowed"),
            "git must refuse the https transport: {stderr}"
        ),
        other => panic!("{other:?}"),
    }
}
