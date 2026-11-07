//! Remotes that gix serves through an external program (`ssh`, `git-upload-pack`) or that are not https:
//! a failed fetch is a typed error ("unsupported-remote", exit 1), never a crash, and the URL's password
//! is never printed. Fake programs that fail are first on PATH.
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
        std::fs::write(
            &p,
            format!(
                "#!/bin/sh\necho \"{prog} $*\" >> '{}'\nexit 99\n",
                log.display()
            ),
        )
        .unwrap();
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
    }
    (fx, log, shim)
}

#[test]
fn remotes_that_need_other_programs_fail_with_a_typed_error() {
    let (fx, _log, shim) = setup();
    let bare = tempfile::tempdir().unwrap();
    gix::init_bare(bare.path()).unwrap();
    let local = bare.path().display().to_string();
    let urls = [
        "ssh://git@127.0.0.1:1/x/y.git".to_string(),
        "git@127.0.0.1:x/y.git".to_string(),
        local.clone(),
        format!("file://{local}"),
        "git://127.0.0.1:1/x/y.git".to_string(),
        "http://127.0.0.1:1/x/y.git".to_string(),
        "http://user:SECRETPW@127.0.0.1:1/x/y.git".to_string(),
        "ssh://user:SECRETPW@127.0.0.1:1/x/y.git".to_string(),
    ];
    for url in urls {
        fx.set_origin_url(&url);
        let o = Command::new(env!("CARGO_BIN_EXE_stw"))
            .env_clear()
            .env("PATH", shim.path())
            .env("HOME", "/nonexistent")
            .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
            .args(["sync", "--apply", "--json", "--root"])
            .arg(&fx.path)
            .output()
            .unwrap();
        let v: Value = serde_json::from_slice(&o.stdout).unwrap_or_else(|e| {
            panic!(
                "{url}: not JSON ({e}): {}",
                String::from_utf8_lossy(&o.stderr)
            )
        });
        let r = &v["repos"][0];
        assert_eq!(o.status.code(), Some(1), "{url}: {v}");
        assert_eq!(r["fetch"], "unsupported-remote", "{url}: {r}");
        assert_eq!(r["result"], "error", "{url}");
        let msg = r["error"].as_str().unwrap();
        assert!(msg.contains("use git directly"), "{url}: {msg}");
        assert!(r.get("next_steps").is_none(), "{url}: {r}");
        let everything = format!(
            "{}{}",
            String::from_utf8_lossy(&o.stdout),
            String::from_utf8_lossy(&o.stderr)
        );
        assert!(
            !everything.contains("SECRETPW"),
            "{url}: password printed: {everything}"
        );
    }
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
