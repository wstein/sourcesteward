//! Remotes that need another program (ssh, local paths) are fetched through the `via: git` runner and nowhere
//! else: gix never starts `ssh` or `git-upload-pack` itself. Unknown transports (`ext::`) are refused typed
//! without starting anything. A failed fetch is typed, the URL's password is never printed. Fake programs
//! first on PATH record every call.
mod common;
use std::process::Command;

use common::*;
use serde_json::Value;

fn setup() -> (Fx, std::path::PathBuf, tempfile::TempDir) {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.branch("main", c0);
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.track("main");
    fx.set_head("main");
    fx.checkout_head();
    let shim = tempfile::tempdir().unwrap();
    let log = shim.path().join("calls.log");
    for prog in ["ssh", "git-upload-pack", "git", "gh"] {
        let p = shim.path().join(prog);
        // the fake git reports a usable version, fails every other call and echoes a credential URL
        let body = if prog == "git" {
            format!(
                "if [ \"$1\" = \"--version\" ]; then echo 'git version 2.47.3'; echo \"git --version\" >> '{log}'; exit 0; fi\n\
                 echo \"git $*\" >> '{log}'\necho 'fatal: unable to access ssh://user:SECRETPW@host/x.git ghp_SECRETSECRETSECRET012345' >&2\nexit 99\n",
                log = log.display()
            )
        } else {
            format!("echo \"{prog} $*\" >> '{}'\nexit 99\n", log.display())
        };
        std::fs::write(&p, format!("#!/bin/sh\n{body}")).unwrap();
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
    }
    (fx, log, shim)
}

fn run_stw(fx: &Fx, path: &std::path::Path, args: &[&str]) -> (std::process::Output, Value) {
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", path)
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .args(args)
        .args(["--json", "--root"])
        .arg(&fx.path)
        .output()
        .unwrap();
    let v: Value = serde_json::from_slice(&o.stdout)
        .unwrap_or_else(|e| panic!("not JSON ({e}): {}", String::from_utf8_lossy(&o.stderr)));
    (o, v)
}

fn all(o: &std::process::Output) -> String {
    format!(
        "{}{}",
        String::from_utf8_lossy(&o.stdout),
        String::from_utf8_lossy(&o.stderr)
    )
}

#[test]
fn ssh_and_local_remotes_go_through_the_git_runner_only() {
    let (fx, log, shim) = setup();
    let bare = tempfile::tempdir().unwrap();
    gix::init_bare(bare.path()).unwrap();
    let local = bare.path().display().to_string();
    let urls = [
        "ssh://git@127.0.0.1:1/x/y.git".to_string(),
        "git@127.0.0.1:x/y.git".to_string(),
        "ssh://user:SECRETPW@127.0.0.1:1/x/y.git".to_string(),
        local.clone(),
        format!("file://{local}"),
    ];
    for url in urls {
        fx.set_origin_url(&url);
        let _ = std::fs::remove_file(&log);
        let (o, v) = run_stw(&fx, shim.path(), &["sync", "--apply"]);
        let r = &v["repos"][0];
        assert_eq!(o.status.code(), Some(1), "{url}: {v}");
        assert_eq!(r["fetch"], "failed", "{url}: {r}");
        assert_eq!(r["via"], "git", "{url}: {r}");
        assert_eq!(r["result"], "error", "{url}");
        assert!(r.get("next_steps").is_none(), "{url}: {r}");
        let calls = std::fs::read_to_string(&log).unwrap();
        // gix did not start ssh or git-upload-pack; only the runner started git
        for line in calls.lines() {
            assert!(
                line == "git --version" || line.starts_with("git -C "),
                "{url}: unexpected program call: {line}"
            );
        }
        assert_eq!(calls.lines().count(), 2, "{url}: {calls}");
        assert!(
            calls.contains("fetch --quiet --no-tags --no-recurse-submodules origin"),
            "{calls}"
        );
        let everything = all(&o);
        for secret in ["SECRETPW", "ghp_SECRET"] {
            assert!(
                !everything.contains(secret),
                "{url}: {secret} printed: {everything}"
            );
        }
    }
}

#[test]
fn unknown_transports_are_refused_typed_and_start_nothing() {
    let (fx, log, shim) = setup();
    for url in [
        "ext::touch /nonexistent/stw-ext-marker",
        "fd::7",
        "myhelper::some-address",
        "ssh::host/path",
    ] {
        fx.set_origin_url(url);
        let (o, v) = run_stw(&fx, shim.path(), &["sync", "--apply"]);
        let r = &v["repos"][0];
        assert_eq!(o.status.code(), Some(1), "{url}: {v}");
        assert_eq!(r["fetch"], "unsupported-remote", "{url}: {r}");
        assert_eq!(r["result"], "error");
        assert!(r.get("via").is_none(), "{url}: nothing ran: {r}");
        assert!(!log.exists(), "{url}: a program was started");
    }
}

#[test]
fn network_remotes_of_gix_fail_typed_without_any_program() {
    let (fx, log, shim) = setup();
    for url in [
        "git://127.0.0.1:1/x/y.git",
        "http://127.0.0.1:1/x/y.git",
        "http://user:SECRETPW@127.0.0.1:1/x/y.git",
    ] {
        fx.set_origin_url(url);
        let (o, v) = run_stw(&fx, shim.path(), &["sync", "--apply"]);
        let r = &v["repos"][0];
        assert_eq!(o.status.code(), Some(1), "{url}: {v}");
        assert_eq!(r["fetch"], "unsupported-remote", "{url}: {r}");
        assert_eq!(r["via"], "gix", "{url}: {r}");
        assert!(r["error"].as_str().unwrap().contains("use git directly"));
        assert!(!all(&o).contains("SECRETPW"), "{url}");
        assert!(!log.exists(), "{url}: a program was started");
    }
    fx.set_origin_url("https://127.0.0.1:1/x/y.git");
    let (_, v) = run_stw(&fx, shim.path(), &["sync", "--apply"]);
    assert_eq!(v["repos"][0]["fetch"], "failed");
    assert_eq!(v["repos"][0]["via"], "gix");
    assert!(!log.exists());
}

#[test]
fn without_git_the_git_steps_are_unsupported_and_core_reads_still_work() {
    let (fx, _log, _shim) = setup();
    let empty = tempfile::tempdir().unwrap();
    // delegated step: typed `unsupported`, exit 1, via git
    for url in ["ssh://git@127.0.0.1:1/x/y.git", "/some/local/path"] {
        fx.set_origin_url(url);
        let (o, v) = run_stw(&fx, std::path::Path::new(""), &["sync", "--apply"]);
        let r = &v["repos"][0];
        assert_eq!(o.status.code(), Some(1), "{url}: {v}");
        assert_eq!(r["fetch"], "unsupported", "{url}: {r}");
        assert_eq!(r["via"], "git");
        assert_eq!(r["result"], "error");
        assert!(
            r["error"].as_str().unwrap().contains("not installed"),
            "{r}"
        );
        assert_eq!(r["git_error"], "not-found");
        // an empty directory on PATH is the same
        let (o2, v2) = run_stw(&fx, empty.path(), &["sync", "--apply"]);
        assert_eq!(o2.status.code(), Some(1));
        assert_eq!(v2["repos"][0]["fetch"], "unsupported");
    }
    // everything gix does keeps working with an empty PATH
    fx.set_origin_url("ssh://git@127.0.0.1:1/x/y.git");
    for args in [
        &["status", "--offline"][..],
        &["tidy", "plan", "--offline"][..],
        &["sync"][..],
        &["sync", "--offline"][..],
    ] {
        let (o, v) = run_stw(&fx, std::path::Path::new(""), args);
        assert_eq!(o.status.code(), Some(0), "{args:?}: {v}");
    }
}

#[test]
fn a_too_old_git_is_unsupported_with_the_version_named() {
    let (fx, log, shim) = setup();
    std::fs::write(
        shim.path().join("git"),
        format!(
            "#!/bin/sh\necho \"git $*\" >> '{}'\necho 'git version 2.20.1'\n",
            log.display()
        ),
    )
    .unwrap();
    fx.set_origin_url("ssh://git@127.0.0.1:1/x/y.git");
    let (o, v) = run_stw(&fx, shim.path(), &["sync", "--apply"]);
    let r = &v["repos"][0];
    assert_eq!(o.status.code(), Some(1));
    assert_eq!(r["fetch"], "unsupported");
    assert_eq!(r["git_error"], "version");
    assert!(r["error"].as_str().unwrap().contains("2.20.1"), "{r}");
    assert_eq!(
        std::fs::read_to_string(&log).unwrap().trim(),
        "git --version"
    );
}

#[test]
fn a_local_path_remote_is_fetched_and_fast_forwarded_with_the_installed_git() {
    if Command::new("git").arg("--version").output().is_err() {
        eprintln!("git not installed: not run");
        return;
    }
    let up = Fx::new();
    let c0 = up.commit(None, &[("a.txt", Some("a\n"))], "c0");
    let c1 = up.commit(Some(c0), &[("a.txt", Some("a\nb\n"))], "c1");
    up.branch("main", c1);
    up.set_head("main");
    let fx = Fx::new();
    let d0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    assert_eq!(c0, d0, "fixtures build identical commits");
    fx.branch("main", d0);
    fx.remote("main", d0);
    fx.origin_head("main");
    fx.track("main");
    fx.set_head("main");
    fx.checkout_head();
    fx.set_origin_url(&up.path.display().to_string());
    let path = std::env::var_os("PATH").unwrap();
    let (o, v) = run_stw(&fx, std::path::Path::new(&path), &["sync", "--apply"]);
    let r = &v["repos"][0];
    assert_eq!(o.status.code(), Some(0), "{v}");
    assert_eq!(r["fetch"], "ok", "{r}");
    assert_eq!(r["via"], "git");
    assert_eq!(r["result"], "fast-forwarded", "{r}");
    assert_eq!(fx.read("a.txt").as_deref(), Some("a\nb\n"));
}

#[test]
fn dry_run_and_offline_never_look_at_the_remote_kind() {
    let (fx, log, shim) = setup();
    fx.set_origin_url("ssh://git@example.invalid/x/y.git");
    for extra in [&["--offline", "--apply"][..], &[][..]] {
        let o = Command::new(env!("CARGO_BIN_EXE_stw"))
            .env_clear()
            .env("PATH", shim.path())
            .env("HOME", "/nonexistent")
            .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
            .args(["sync", "--json", "--root"])
            .arg(&fx.path)
            .args(extra)
            .output()
            .unwrap();
        assert_eq!(o.status.code(), Some(0), "{extra:?}");
    }
    assert!(!log.exists());
}
