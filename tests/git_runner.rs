//! The `via: git` runner (`stw::git_runner`): fixed argument vector, scrubbed environment, repository
//! selection, version check, bounded time and output, credential redaction, typed errors.
//!
//! Harness confinement: every test that starts a program works in a tempdir it created. The runner is given
//! that directory (an empty path is refused), real git runs only `init`, `add`, `commit`, `branch` and the
//! runner's own `fetch` inside it, with `GIT_CEILING_DIRECTORIES` set to the tempdir's parent. Nothing here
//! runs gc, reflog expire, reset, update-ref or branch deletion.
use std::ffi::OsString;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::time::Duration;

use stw::git_runner::{parse_version, redact, GitError, Limits, Op, Runner, Version, VIA};

fn osv(pairs: &[(&str, &str)]) -> Vec<(OsString, OsString)> {
    pairs
        .iter()
        .map(|(k, v)| (OsString::from(k), OsString::from(v)))
        .collect()
}

fn tmp() -> tempfile::TempDir {
    tempfile::Builder::new()
        .prefix("stw-runner-")
        .tempdir()
        .unwrap()
}

fn script(dir: &Path, name: &str, body: &str) -> PathBuf {
    let p = dir.join(name);
    std::fs::write(&p, format!("#!/bin/sh\n{body}\n")).unwrap();
    std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
    p
}

/// A fake `git`: answers `--version` with `ver`, otherwise logs argv and environment to `log` and runs `rest`.
fn fake_git(dir: &Path, ver: &str, rest: &str) -> PathBuf {
    let log = dir.join("calls.log");
    script(
        dir,
        "git",
        &format!(
            "if [ \"$1\" = \"--version\" ]; then echo 'git version {ver}'; echo VERSION >> '{log}'; exit 0; fi\n\
             echo \"ARGV $*\" >> '{log}'\nenv | sort >> '{log}'\n{rest}",
            log = log.display()
        ),
    );
    log
}

fn runner_with(bin: &Path, extra: &[(&str, &str)]) -> Runner {
    // the fake git is first; the system tools (env, sort, sleep, yes) follow
    let path = format!("{}:/usr/bin:/bin", bin.display());
    let mut env = osv(&[("PATH", path.as_str())]);
    env.extend(osv(extra));
    Runner::from_env(env)
}

#[test]
fn version_strings() {
    assert_eq!(parse_version("git version 2.47.3"), Some(Version(2, 47, 3)));
    assert_eq!(
        parse_version("git version 2.39.5 (Apple Git-154)\n"),
        Some(Version(2, 39, 5))
    );
    assert_eq!(
        parse_version("git version 2.45.1.windows.1"),
        Some(Version(2, 45, 1))
    );
    assert_eq!(parse_version("git version 2.45"), Some(Version(2, 45, 0)));
    assert_eq!(
        parse_version("git version 2.50.0-rc1"),
        Some(Version(2, 50, 0))
    );
    assert_eq!(parse_version("hello"), None);
    assert_eq!(parse_version("git version x.y.z"), None);
    assert_eq!(parse_version(""), None);
}

#[test]
fn the_argument_vector_is_fixed_and_names_the_repository_explicitly() {
    let t = tmp();
    let r = Runner::from_env(osv(&[("PATH", "/usr/bin")]));
    let argv: Vec<String> = r
        .argv(t.path(), Op::FetchOrigin)
        .unwrap()
        .iter()
        .map(|a| a.to_string_lossy().into_owned())
        .collect();
    let repo = t.path().to_str().unwrap();
    assert_eq!(
        argv,
        [
            "-C",
            repo,
            "-c",
            "core.fsmonitor=false",
            "-c",
            "submodule.recurse=false",
            "-c",
            "fetch.recurseSubmodules=false",
            "-c",
            "gc.auto=0",
            "-c",
            "maintenance.auto=false",
            "-c",
            "protocol.ext.allow=never",
            "-c",
            "core.hooksPath=/dev/null",
            "fetch",
            "--quiet",
            "--no-tags",
            "--no-recurse-submodules",
            "--upload-pack=git-upload-pack",
            "origin"
        ]
    );
    assert_eq!(VIA, "git");
}

#[test]
fn the_runner_never_falls_back_to_the_current_directory() {
    let r = Runner::from_env(osv(&[("PATH", "/usr/bin")]));
    for bad in ["", "relative/dir", ".", "/nonexistent-stw-dir-0000"] {
        let e = r.argv(Path::new(bad), Op::FetchOrigin).unwrap_err();
        assert!(matches!(e, GitError::Repo(_)), "{bad:?}: {e:?}");
        assert_eq!(e.kind(), "repository");
    }
    // a file is not a repository directory either
    let t = tmp();
    let f = t.path().join("f");
    std::fs::write(&f, "x").unwrap();
    assert!(matches!(
        r.argv(&f, Op::FetchOrigin),
        Err(GitError::Repo(_))
    ));
    // and run() refuses before it looks for or starts git
    let r = Runner::from_env(osv(&[("PATH", "")]));
    assert!(matches!(
        r.run(Path::new(""), Op::FetchOrigin),
        Err(GitError::Repo(_))
    ));
}

#[test]
fn child_environment_is_an_allowlist() {
    let t = tmp();
    let hostile = [
        ("GIT_DIR", "/evil/gitdir"),
        ("GIT_WORK_TREE", "/evil/wt"),
        ("GIT_INDEX_FILE", "/evil/index"),
        ("GIT_OBJECT_DIRECTORY", "/evil/objects"),
        ("GIT_COMMON_DIR", "/evil/common"),
        ("GIT_NAMESPACE", "ns"),
        ("GIT_CONFIG", "/evil/cfg"),
        ("GIT_CONFIG_GLOBAL", "/evil/global"),
        ("GIT_CONFIG_SYSTEM", "/evil/system"),
        ("GIT_CONFIG_COUNT", "1"),
        ("GIT_CONFIG_KEY_0", "core.sshCommand"),
        ("GIT_CONFIG_VALUE_0", "evil"),
        ("GIT_CONFIG_PARAMETERS", "'core.pager=evil'"),
        ("GIT_EXEC_PATH", "/evil/exec"),
        ("GIT_TEMPLATE_DIR", "/evil/tpl"),
        ("GIT_EDITOR", "evil"),
        ("GIT_SEQUENCE_EDITOR", "evil"),
        ("GIT_PAGER", "evil"),
        ("EDITOR", "evil"),
        ("VISUAL", "evil"),
        ("PAGER", "evil"),
        ("GIT_TRACE", "1"),
        ("GIT_TRACE_CURL", "1"),
        ("GIT_AUTHOR_NAME", "evil"),
        ("GIT_COMMITTER_EMAIL", "evil@example.com"),
        ("GIT_PROXY_COMMAND", "evil"),
        ("GIT_ALLOW_PROTOCOL", "ext"),
        ("GIT_CEILING_DIRECTORIES", "/evil"),
        ("GIT_ASKPASS_EXTRA", "x"),
        ("LANG", "de_DE.UTF-8"),
        ("LANGUAGE", "de"),
        ("LC_MESSAGES", "de_DE.UTF-8"),
        ("LC_ALL", "de_DE.UTF-8"),
        ("GH_TOKEN", "ghp_SECRETSECRETSECRETSECRET0123456789"),
        ("GITHUB_TOKEN", "ghp_SECRETSECRETSECRETSECRET0123456789"),
        ("RANDOM_OTHER", "x"),
    ];
    let keep = [
        ("PATH", "/usr/bin"),
        ("HOME", "/home/u"),
        ("USER", "u"),
        ("XDG_CONFIG_HOME", "/home/u/.config"),
        ("TMPDIR", "/tmp"),
        ("SSH_AUTH_SOCK", "/run/agent.sock"),
        ("GIT_SSH", "/usr/bin/ssh"),
        ("GIT_SSH_COMMAND", "ssh -i /home/u/key"),
        ("GIT_SSH_VARIANT", "ssh"),
        ("GIT_ASKPASS", "/usr/bin/askpass"),
        ("SSH_ASKPASS", "/usr/bin/askpass"),
        ("HTTPS_PROXY", "http://proxy:3128"),
        ("SSL_CERT_FILE", "/etc/ca.pem"),
    ];
    let mut env = osv(&hostile);
    env.extend(osv(&keep));
    let r = Runner::from_env(env);
    let child: std::collections::BTreeMap<String, String> = r
        .child_env(t.path())
        .unwrap()
        .into_iter()
        .map(|(k, v)| {
            (
                k.to_string_lossy().into_owned(),
                v.to_string_lossy().into_owned(),
            )
        })
        .collect();
    for (k, _) in hostile {
        if ![
            "GIT_ALLOW_PROTOCOL",
            "GIT_CEILING_DIRECTORIES",
            "LC_ALL",
            "GIT_PAGER",
            "GIT_EDITOR",
        ]
        .contains(&k)
        {
            assert!(
                !child.contains_key(k),
                "{k} leaked into the child environment"
            );
        }
    }
    for (k, v) in keep {
        assert_eq!(
            child.get(k).map(String::as_str),
            Some(v),
            "{k} must be kept"
        );
    }
    // forced values replace the caller's
    assert_eq!(child["LC_ALL"], "C");
    assert_eq!(child["GIT_TERMINAL_PROMPT"], "0");
    assert_eq!(child["GIT_OPTIONAL_LOCKS"], "0");
    assert_eq!(child["GIT_PAGER"], "cat");
    assert_eq!(child["GIT_EDITOR"], "true");
    assert_eq!(child["GIT_ALLOW_PROTOCOL"], "file:ssh:git:http:https");
    assert_eq!(
        child["GIT_CEILING_DIRECTORIES"],
        t.path().parent().unwrap().to_str().unwrap()
    );
}

#[test]
fn the_started_process_sees_the_scrubbed_environment_and_the_fixed_argv() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    let log = fake_git(&bin, "2.47.3", "exit 0");
    let r = runner_with(
        &bin,
        &[
            ("GIT_DIR", "/evil"),
            ("GIT_CONFIG_COUNT", "1"),
            ("GIT_EDITOR", "evil"),
            ("LANG", "de_DE.UTF-8"),
            ("GIT_SSH_COMMAND", "ssh -i key"),
            ("SSH_AUTH_SOCK", "/s"),
            ("GH_TOKEN", "ghp_SECRETSECRETSECRETSECRET0123456789"),
        ],
    );
    r.run(&repo, Op::FetchOrigin).unwrap();
    let seen = std::fs::read_to_string(&log).unwrap();
    let argv = seen.lines().find(|l| l.starts_with("ARGV ")).unwrap();
    assert!(
        argv.starts_with(&format!(
            "ARGV -C {} -c core.fsmonitor=false",
            repo.display()
        )),
        "{argv}"
    );
    assert!(
        argv.ends_with(
            "fetch --quiet --no-tags --no-recurse-submodules --upload-pack=git-upload-pack origin"
        ),
        "{argv}"
    );
    for bad in [
        "GIT_DIR=",
        "GIT_CONFIG_COUNT",
        "GH_TOKEN",
        "LANG=",
        "GIT_EDITOR=evil",
    ] {
        assert!(!seen.contains(bad), "{bad} reached the child:\n{seen}");
    }
    assert!(seen.contains("GIT_SSH_COMMAND=ssh -i key"));
    assert!(seen.contains("SSH_AUTH_SOCK=/s"));
    assert!(seen.contains("LC_ALL=C"));
    assert!(seen.contains("GIT_TERMINAL_PROMPT=0"));
}

#[test]
fn the_working_directory_of_the_child_is_the_repository() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    script(
        &bin,
        "git",
        "if [ \"$1\" = \"--version\" ]; then echo 'git version 2.47.3'; exit 0; fi\npwd",
    );
    let out = runner_with(&bin, &[]).run(&repo, Op::FetchOrigin).unwrap();
    let pwd = String::from_utf8(out.stdout).unwrap();
    assert_eq!(
        Path::new(pwd.trim()).canonicalize().unwrap(),
        repo.canonicalize().unwrap()
    );
}

#[test]
fn missing_git_is_a_typed_unsupported_result() {
    let t = tmp();
    for path in [
        "",
        "relative/bin:.",
        &format!("{}/nope", t.path().display()),
    ] {
        let r = Runner::from_env(osv(&[("PATH", path)]));
        let e = r.run(t.path(), Op::FetchOrigin).unwrap_err();
        assert!(matches!(e, GitError::NotFound), "{path:?}: {e:?}");
        assert!(e.is_unsupported());
        assert_eq!(e.kind(), "not-found");
        assert!(matches!(r.version(), Err(GitError::NotFound)));
    }
    // no PATH variable at all
    let r = Runner::from_env(vec![]);
    assert!(matches!(
        r.run(t.path(), Op::FetchOrigin),
        Err(GitError::NotFound)
    ));
    // a `git` in the current directory or in a relative or empty PATH entry is never used
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    script(&bin, "git", "echo 'git version 2.47.3'");
    let r = Runner::from_env(osv(&[("PATH", ":bin:./bin")]));
    assert!(matches!(
        r.run(t.path(), Op::FetchOrigin),
        Err(GitError::NotFound)
    ));
    // tests run in the package directory: this relative entry would find an executable `git` there
    let rel = Path::new("tests/fixtures/relative-bin");
    assert!(
        rel.join("git").is_file(),
        "fixture missing (tests run in the package directory)"
    );
    let r = Runner::from_env(osv(&[("PATH", rel.to_str().unwrap())]));
    assert!(matches!(
        r.run(t.path(), Op::FetchOrigin),
        Err(GitError::NotFound)
    ));
    assert!(matches!(r.version(), Err(GitError::NotFound)));
}

#[test]
fn old_or_unreadable_versions_are_typed_and_nothing_else_is_started() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    let log = fake_git(&bin, "2.20.1", "exit 0");
    let r = runner_with(&bin, &[]);
    let e = r.run(&repo, Op::FetchOrigin).unwrap_err();
    match &e {
        GitError::Version { found, required } => {
            assert_eq!(found, "2.20.1");
            assert_eq!(*required, Op::FetchOrigin.min_version());
        }
        other => panic!("{other:?}"),
    }
    assert!(e.is_unsupported());
    assert_eq!(e.kind(), "version");
    assert!(e.to_string().contains("2.20.1"));
    let calls = std::fs::read_to_string(&log).unwrap();
    assert_eq!(calls.trim(), "VERSION", "only --version may run: {calls}");

    // unreadable version output
    let t2 = tmp();
    let bin2 = t2.path().join("bin");
    std::fs::create_dir(&bin2).unwrap();
    script(&bin2, "git", "echo 'not a version'");
    let e = runner_with(&bin2, &[])
        .run(&repo, Op::FetchOrigin)
        .unwrap_err();
    assert!(matches!(e, GitError::Version { .. }), "{e:?}");
    assert!(e.is_unsupported());

    // the minimum itself is accepted
    let t3 = tmp();
    let bin3 = t3.path().join("bin");
    std::fs::create_dir(&bin3).unwrap();
    let min = Op::FetchOrigin.min_version();
    fake_git(&bin3, &format!("{}.{}.{}", min.0, min.1, min.2), "exit 0");
    runner_with(&bin3, &[]).run(&repo, Op::FetchOrigin).unwrap();
}

#[test]
fn the_version_is_checked_once_per_runner() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    let log = fake_git(&bin, "2.47.3", "exit 0");
    let r = runner_with(&bin, &[]);
    r.run(&repo, Op::FetchOrigin).unwrap();
    r.run(&repo, Op::FetchOrigin).unwrap();
    let calls = std::fs::read_to_string(&log).unwrap();
    assert_eq!(calls.matches("VERSION").count(), 1, "{calls}");
    assert_eq!(calls.matches("ARGV ").count(), 2);
}

#[test]
fn an_unknown_option_is_a_typed_error() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    fake_git(
        &bin,
        "2.47.3",
        "echo 'error: unknown option `no-tags'\"'\" >&2; exit 129",
    );
    let e = runner_with(&bin, &[])
        .run(&repo, Op::FetchOrigin)
        .unwrap_err();
    assert!(matches!(e, GitError::UnknownOption(_)), "{e:?}");
    assert!(e.is_unsupported());
    assert_eq!(e.kind(), "unknown-option");
}

#[test]
fn unknown_options_are_recognised_by_exit_code_and_by_text() {
    let cases = [
        ("echo 'usage: git fetch [<options>]' >&2; exit 129", true),
        (
            "echo \"error: unrecognized option '--no-tags'\" >&2; exit 1",
            true,
        ),
        ("echo 'error: unknown switch `q' >&2; exit 1", true),
        ("echo 'fatal: boom' >&2; exit 1", false),
    ];
    for (body, unknown) in cases {
        let t = tmp();
        let bin = t.path().join("bin");
        std::fs::create_dir(&bin).unwrap();
        let repo = t.path().join("repo");
        gix::init(&repo).unwrap();
        fake_git(&bin, "2.47.3", body);
        let e = runner_with(&bin, &[])
            .run(&repo, Op::FetchOrigin)
            .unwrap_err();
        assert_eq!(
            matches!(e, GitError::UnknownOption(_)),
            unknown,
            "{body}: {e:?}"
        );
    }
}

#[test]
fn a_failing_git_reports_exit_code_and_redacted_stderr() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    fake_git(
        &bin,
        "2.47.3",
        "echo 'fatal: unable to access https://user:SECRETPW@example.com/x.git: gone ghp_SECRETSECRETSECRETSECRET0123456789 mytok123' >&2; exit 128",
    );
    let r = runner_with(&bin, &[("GH_TOKEN", "mytok123")]);
    let e = r.run(&repo, Op::FetchOrigin).unwrap_err();
    match &e {
        GitError::Exit { code, stderr } => {
            assert_eq!(*code, Some(128));
            for s in ["SECRETPW", "ghp_SECRET", "mytok123", "user:"] {
                assert!(!stderr.contains(s), "{s} in {stderr}");
            }
            assert!(stderr.contains("example.com/x.git"), "{stderr}");
        }
        other => panic!("{other:?}"),
    }
    assert!(!e.is_unsupported());
    assert_eq!(e.kind(), "exit");
    let shown = e.to_string();
    for s in ["SECRETPW", "ghp_SECRET", "mytok123"] {
        assert!(!shown.contains(s), "{s} in {shown}");
    }
}

#[test]
fn time_is_bounded() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    fake_git(&bin, "2.47.3", "sleep 30");
    let r = runner_with(&bin, &[]).limits(Limits {
        timeout: Duration::from_millis(400),
        ..Limits::default()
    });
    let start = std::time::Instant::now();
    let e = r.run(&repo, Op::FetchOrigin).unwrap_err();
    assert!(matches!(e, GitError::Timeout { .. }), "{e:?}");
    assert_eq!(e.kind(), "timeout");
    assert!(
        start.elapsed() < Duration::from_secs(10),
        "{:?}",
        start.elapsed()
    );
}

#[test]
fn output_is_bounded() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    fake_git(&bin, "2.47.3", "yes xxxxxxxxxxxxxxxxxxxx | head -c 5000000");
    let r = runner_with(&bin, &[]).limits(Limits {
        max_stdout: 10_000,
        ..Limits::default()
    });
    let e = r.run(&repo, Op::FetchOrigin).unwrap_err();
    assert!(matches!(e, GitError::OutputLimit { .. }), "{e:?}");
    assert_eq!(e.kind(), "output-limit");

    // stderr too
    let t2 = tmp();
    let bin2 = t2.path().join("bin");
    std::fs::create_dir(&bin2).unwrap();
    fake_git(
        &bin2,
        "2.47.3",
        "yes eeeeeeeeeeeeeeeeeeee | head -c 5000000 >&2",
    );
    let r = runner_with(&bin2, &[]).limits(Limits {
        max_stderr: 10_000,
        ..Limits::default()
    });
    assert!(matches!(
        r.run(&repo, Op::FetchOrigin),
        Err(GitError::OutputLimit { .. })
    ));
}

#[test]
fn redaction_removes_url_credentials_and_tokens() {
    let secrets = vec!["opaque-secret-value".to_string()];
    let cases = [
        (
            "fatal: https://user:pw@github.com/a/b.git",
            "fatal: https://github.com/a/b.git",
        ),
        ("ssh://git:pw@host:22/x", "ssh://host:22/x"),
        ("https://user:p@ss@host/x.git", "https://host/x.git"),
        ("https://us@er:p@@ss@host/x", "https://host/x"),
        ("http://tok@host/x", "http://host/x"),
        (
            "clone 'https://x-access-token:ghs_abc@github.com/a/b'",
            "clone 'https://github.com/a/b'",
        ),
        ("opaque-secret-value here", "*** here"),
        ("a ghp_abcdefghijklmnopqrstuvwxyz0123456789 b", "a *** b"),
        (
            "github_pat_11ABCDEFG0abcdef_xyzXYZ0123456789 end",
            "*** end",
        ),
        (
            "gho_abcdefghijklmnop ghu_abcdefghijklmnop ghs_abcdefghijklmnop ghr_abcdefghijklmnop",
            "*** *** *** ***",
        ),
        (
            "Authorization: Bearer abc.def\nnext",
            "Authorization: ***\nnext",
        ),
        ("authorization: basic QWxhZGRpbg==", "authorization: ***"),
        ("user@host:path is scp", "user@host:path is scp"),
        ("mail me@example.com", "mail me@example.com"),
    ];
    for (input, want) in cases {
        assert_eq!(redact(input, &secrets), want, "input {input:?}");
    }
    // an empty secret must not redact everything
    assert_eq!(redact("abc", &["".to_string()]), "abc");
}

// ---- real git, confined to tempdirs --------------------------------------------------------------

fn real_git_available() -> bool {
    std::process::Command::new("git")
        .arg("--version")
        .output()
        .is_ok_and(|o| o.status.success())
}

/// A runner for the real git with an isolated home: the user's global config does not apply.
fn real_runner(home: &Path) -> Runner {
    Runner::from_env(osv(&[
        ("PATH", std::env::var("PATH").unwrap().as_str()),
        ("HOME", home.to_str().unwrap()),
        ("XDG_CONFIG_HOME", home.join(".config").to_str().unwrap()),
    ]))
}

/// Run git in `dir` (a tempdir this test created) with an isolated environment.
fn git(dir: &Path, args: &[&str]) {
    assert!(
        !dir.as_os_str().is_empty() && dir.is_absolute(),
        "unconfined git: {dir:?}"
    );
    let out = std::process::Command::new("git")
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", dir)
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_CEILING_DIRECTORIES", dir.parent().unwrap())
        .current_dir(dir)
        .args(["-C"])
        .arg(dir)
        .args([
            "-c",
            "user.name=T",
            "-c",
            "user.email=t@example.com",
            "-c",
            "commit.gpgsign=false",
        ])
        .args(args)
        .output()
        .unwrap();
    assert!(
        out.status.success(),
        "git {args:?}: {}",
        String::from_utf8_lossy(&out.stderr)
    );
}

fn rev(dir: &Path, r: &str) -> String {
    let out = std::process::Command::new("git")
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", dir)
        .env("GIT_CEILING_DIRECTORIES", dir.parent().unwrap())
        .arg("-C")
        .arg(dir)
        .args(["rev-parse", r])
        .output()
        .unwrap();
    String::from_utf8(out.stdout).unwrap().trim().to_string()
}

/// `upstream` with one commit on `main`, and `clone` that has `origin` = `url` and no commits fetched yet.
fn fetch_fixture(t: &Path, url: impl Fn(&Path) -> String) -> (PathBuf, PathBuf) {
    let up = t.join("up");
    let clone = t.join("clone");
    std::fs::create_dir(&up).unwrap();
    std::fs::create_dir(&clone).unwrap();
    git(&up, &["init", "-q", "-b", "main"]);
    std::fs::write(up.join("a.txt"), "a\n").unwrap();
    git(&up, &["add", "a.txt"]);
    git(&up, &["commit", "-q", "-m", "c0"]);
    git(&clone, &["init", "-q", "-b", "main"]);
    git(&clone, &["remote", "add", "origin", &url(&up)]);
    (up, clone)
}

#[test]
fn real_git_fetches_a_local_path_remote() {
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let t = tmp();
    let (up, clone) = fetch_fixture(t.path(), |u| u.display().to_string());
    let out = real_runner(t.path()).run(&clone, Op::FetchOrigin).unwrap();
    assert!(out.stdout.len() < 1000);
    assert_eq!(rev(&clone, "refs/remotes/origin/main"), rev(&up, "HEAD"));
}

#[test]
fn real_git_fetches_over_ssh_with_the_users_ssh_command() {
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let t = tmp();
    let (up, clone) = fetch_fixture(t.path(), |u| format!("ssh://fake-host{}", u.display()));
    // a fake ssh: runs the remote command locally, in `up`, and records that it was used
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let ssh_log = t.path().join("ssh.log");
    let fake_ssh = script(
        &bin,
        "fake-ssh",
        &format!(
            "echo \"$*\" >> '{log}'\nfor last; do :; done\ncd '{up}' && exec sh -c \"$last\"",
            log = ssh_log.display(),
            up = up.display()
        ),
    );
    let mut env: Vec<(OsString, OsString)> = vec![
        ("PATH".into(), std::env::var_os("PATH").unwrap()),
        ("GIT_SSH_COMMAND".into(), fake_ssh.clone().into_os_string()),
        ("GIT_SSH_VARIANT".into(), "ssh".into()),
        // must not redirect the fetch away from `clone`
        ("GIT_DIR".into(), up.join(".git").into_os_string()),
    ];
    env.push(("GIT_WORK_TREE".into(), up.clone().into_os_string()));
    Runner::from_env(env).run(&clone, Op::FetchOrigin).unwrap();
    assert!(std::fs::read_to_string(&ssh_log)
        .unwrap()
        .contains("fake-host"));
    assert_eq!(rev(&clone, "refs/remotes/origin/main"), rev(&up, "HEAD"));
}

#[test]
fn real_git_refuses_the_ext_transport() {
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let t = tmp();
    let marker = t.path().join("ext-ran");
    let (_up, clone) = fetch_fixture(t.path(), |_| format!("ext::touch {}", marker.display()));
    let e = real_runner(t.path())
        .run(&clone, Op::FetchOrigin)
        .unwrap_err();
    assert!(matches!(e, GitError::Exit { .. }), "{e:?}");
    assert!(!marker.exists(), "the ext transport ran a command");
}

// ---- repository-local commands (hooks, uploadpack) -----------------------------------------------

fn append_local_config(repo: &Path, text: &str) {
    use std::io::Write;
    let mut f = std::fs::OpenOptions::new()
        .append(true)
        .open(repo.join(".git/config"))
        .unwrap();
    write!(f, "{text}").unwrap();
}

#[test]
fn real_git_runs_no_repository_hook_on_fetch() {
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let t = tmp();
    let (_up, clone) = fetch_fixture(t.path(), |u| u.display().to_string());
    let marker = t.path().join("hook-ran");
    script(
        &clone.join(".git/hooks"),
        "reference-transaction",
        &format!("touch '{}'", marker.display()),
    );
    real_runner(t.path()).run(&clone, Op::FetchOrigin).unwrap();
    assert!(!marker.exists(), "a repository hook ran during the fetch");
}

#[test]
fn real_git_ignores_a_repository_local_uploadpack_command() {
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let t = tmp();
    let (up, clone) = fetch_fixture(t.path(), |u| u.display().to_string());
    let marker = t.path().join("uploadpack-ran");
    let helper = script(
        t.path(),
        "my-upload-pack",
        &format!("touch '{}'\nexec git-upload-pack \"$@\"", marker.display()),
    );
    append_local_config(
        &clone,
        &format!("[remote \"origin\"]\n\tuploadpack = {}\n", helper.display()),
    );
    real_runner(t.path()).run(&clone, Op::FetchOrigin).unwrap();
    assert!(!marker.exists(), "remote.origin.uploadpack ran");
    assert_eq!(rev(&clone, "refs/remotes/origin/main"), rev(&up, "HEAD"));
}

#[test]
fn real_git_does_not_run_a_repository_local_ssh_command() {
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let t = tmp();
    let (_up, clone) = fetch_fixture(t.path(), |_| "ssh://fake-host/up".to_string());
    let marker = t.path().join("ssh-ran");
    let helper = script(
        t.path(),
        "my-ssh",
        &format!("touch '{}'\nexit 1", marker.display()),
    );
    append_local_config(
        &clone,
        &format!("[core]\n\tsshCommand = {}\n", helper.display()),
    );
    let _ = real_runner(t.path()).run(&clone, Op::FetchOrigin);
    assert!(!marker.exists(), "core.sshCommand of the repository ran");
}

/// Local (repository or worktree) config keys that make git run a command are refused with the key name;
/// the same keys in the user's global config stay allowed.
#[test]
fn local_config_keys_that_run_commands_are_refused_by_name() {
    let cases: &[(&str, &str)] = &[
        ("[core]\n\tsshCommand = evil\n", "core.sshcommand"),
        ("[core]\n\tgitProxy = evil\n", "core.gitproxy"),
        ("[core]\n\taskPass = evil\n", "core.askpass"),
        (
            "[core]\n\talternateRefsCommand = evil\n",
            "core.alternaterefscommand",
        ),
        ("[credential]\n\thelper = evil\n", "credential.helper"),
        (
            "[credential \"https://example.com\"]\n\thelper = evil\n",
            "credential.https://example.com.helper",
        ),
        ("[credential]\n\tusername = x\n", "credential.username"),
        (
            "[url \"ext::x \"]\n\tinsteadOf = https://a/\n",
            "url.ext::x .insteadof",
        ),
        (
            "[url \"ssh://b/\"]\n\tpushInsteadOf = https://a/\n",
            "url.ssh://b/.pushinsteadof",
        ),
        (
            "[remote \"origin\"]\n\treceivepack = evil\n",
            "remote.origin.receivepack",
        ),
        ("[remote \"origin\"]\n\tvcs = evil\n", "remote.origin.vcs"),
    ];
    for (snippet, key) in cases {
        let t = tmp();
        let bin = t.path().join("bin");
        std::fs::create_dir(&bin).unwrap();
        let repo = t.path().join("repo");
        gix::init(&repo).unwrap();
        let log = fake_git(&bin, "2.47.3", "exit 0");
        append_local_config(&repo, snippet);
        let e = runner_with(&bin, &[])
            .run(&repo, Op::FetchOrigin)
            .unwrap_err();
        match &e {
            GitError::Refused { key: k } => assert_eq!(k, key),
            other => panic!("{key}: {other:?}"),
        }
        assert!(e.is_unsupported());
        assert_eq!(e.kind(), "refused-config");
        assert!(e.to_string().contains(key), "{e}");
        let calls = std::fs::read_to_string(&log).unwrap();
        assert!(!calls.contains("ARGV"), "{key}: git was started: {calls}");
    }
}

#[test]
fn harmless_local_config_and_the_same_keys_in_the_users_config_are_allowed() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    fake_git(&bin, "2.47.3", "exit 0");
    append_local_config(
        &repo,
        "[remote \"origin\"]\n\turl = ssh://h/x\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n[remote \"other\"]\n\tvcs = x\n[user]\n\tname = T\n[core]\n\tbare = false\n",
    );
    // the user's global config holds all the dangerous keys
    let home = t.path().join("home");
    std::fs::create_dir(&home).unwrap();
    std::fs::write(
        home.join(".gitconfig"),
        "[core]\n\tsshCommand = ssh -i k\n\taskPass = a\n[credential]\n\thelper = store\n",
    )
    .unwrap();
    runner_with(&bin, &[("HOME", home.to_str().unwrap())])
        .run(&repo, Op::FetchOrigin)
        .unwrap();
}

#[test]
fn a_worktree_config_is_checked_like_the_local_one() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let repo = t.path().join("repo");
    gix::init(&repo).unwrap();
    fake_git(&bin, "2.47.3", "exit 0");
    append_local_config(&repo, "[extensions]\n\tworktreeConfig = true\n");
    std::fs::write(
        repo.join(".git/config.worktree"),
        "[core]\n\tsshCommand = evil\n",
    )
    .unwrap();
    let e = runner_with(&bin, &[])
        .run(&repo, Op::FetchOrigin)
        .unwrap_err();
    assert!(
        matches!(&e, GitError::Refused { key } if key == "core.sshcommand"),
        "{e:?}"
    );
}

#[test]
fn a_directory_that_is_not_a_repository_is_refused_before_git_starts() {
    let t = tmp();
    let bin = t.path().join("bin");
    std::fs::create_dir(&bin).unwrap();
    let plain = t.path().join("plain");
    std::fs::create_dir(&plain).unwrap();
    let log = fake_git(&bin, "2.47.3", "exit 0");
    let e = runner_with(&bin, &[])
        .run(&plain, Op::FetchOrigin)
        .unwrap_err();
    assert!(matches!(e, GitError::Repo(_)), "{e:?}");
    assert!(!std::fs::read_to_string(&log).unwrap().contains("ARGV"));
}

// ---- PATH of the child --------------------------------------------------------------------------

#[test]
fn the_child_gets_only_absolute_path_entries() {
    let t = tmp();
    let r = Runner::from_env(osv(&[("PATH", ":.:bin:/usr/bin::./x:/bin:")]));
    let env = r.child_env(t.path()).unwrap();
    let path = env
        .iter()
        .find(|(k, _)| k == "PATH")
        .unwrap()
        .1
        .to_str()
        .unwrap()
        .to_string();
    assert_eq!(path, "/usr/bin:/bin");
    // nothing absolute left: no PATH variable rather than an empty one (an empty one means "current directory")
    let r = Runner::from_env(osv(&[("PATH", ":.:bin")]));
    assert!(r
        .child_env(t.path())
        .unwrap()
        .iter()
        .all(|(k, _)| k != "PATH"));
}

#[test]
fn git_does_not_find_ssh_in_the_worktree_through_empty_or_dot_path_entries() {
    if !real_git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let t = tmp();
    let (_up, clone) = fetch_fixture(t.path(), |_| "ssh://fake-host/up".to_string());
    let marker = t.path().join("worktree-ssh-ran");
    script(
        &clone,
        "ssh",
        &format!("touch '{}'\nexit 1", marker.display()),
    );
    let git_dir = {
        let out = std::process::Command::new("sh")
            .args(["-c", "command -v git"])
            .output()
            .unwrap();
        PathBuf::from(String::from_utf8(out.stdout).unwrap().trim())
            .parent()
            .unwrap()
            .to_path_buf()
    };
    for path in [
        format!(":.:{}", git_dir.display()),
        format!(".:{}:", git_dir.display()),
    ] {
        let r = Runner::from_env(osv(&[
            ("PATH", path.as_str()),
            ("HOME", t.path().to_str().unwrap()),
        ]));
        let e = r.run(&clone, Op::FetchOrigin);
        assert!(
            e.is_err(),
            "no ssh exists on the child's PATH, the fetch must fail"
        );
        assert!(
            !marker.exists(),
            "ssh from the worktree ran (PATH {path:?})"
        );
    }
}

// ---- process group ------------------------------------------------------------------------------

/// True while a process with this id exists and is not a zombie.
fn alive(pid: &str) -> bool {
    let out = std::process::Command::new("ps")
        .args(["-o", "stat=", "-p", pid])
        .output()
        .unwrap();
    let stat = String::from_utf8_lossy(&out.stdout);
    let stat = stat.trim();
    !stat.is_empty() && !stat.starts_with('Z')
}

fn wait_dead(pid: &str) -> bool {
    for _ in 0..150 {
        if !alive(pid) {
            return true;
        }
        std::thread::sleep(Duration::from_millis(20));
    }
    false
}

#[test]
fn a_grandchild_holding_the_pipe_is_killed_with_the_group_on_timeout_and_on_output_limit() {
    for (rest, limits) in [
        (
            "sleep 60 & echo $! > PIDFILE\nsleep 60",
            Limits {
                timeout: Duration::from_millis(400),
                ..Limits::default()
            },
        ),
        (
            "sleep 60 & echo $! > PIDFILE\nyes xxxxxxxxxxxxxxxxxxxx | head -c 5000000",
            Limits {
                max_stdout: 10_000,
                ..Limits::default()
            },
        ),
    ] {
        let t = tmp();
        let bin = t.path().join("bin");
        std::fs::create_dir(&bin).unwrap();
        let repo = t.path().join("repo");
        gix::init(&repo).unwrap();
        let pidfile = t.path().join("grandchild.pid");
        fake_git(
            &bin,
            "2.47.3",
            &rest.replace("PIDFILE", &format!("'{}'", pidfile.display())),
        );
        let e = runner_with(&bin, &[])
            .limits(limits)
            .run(&repo, Op::FetchOrigin)
            .unwrap_err();
        assert!(
            matches!(e, GitError::Timeout { .. } | GitError::OutputLimit { .. }),
            "{e:?}"
        );
        let pid = std::fs::read_to_string(&pidfile)
            .unwrap()
            .trim()
            .to_string();
        assert!(wait_dead(&pid), "grandchild {pid} survived ({e:?})");
    }
}
