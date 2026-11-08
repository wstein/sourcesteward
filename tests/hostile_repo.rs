//! A repository that names programs everywhere (filter drivers, textconv, credential helper, hooks, ssh
//! command) must not make `status`, `tidy plan` or `sync` start any of them. Markers are files the programs
//! would create; `sh` is on PATH so that a program could really run.
mod common;
use std::path::Path;
use std::process::Command;

use common::*;

fn hostile() -> (Fx, tempfile::TempDir) {
    let fx = Fx::new();
    let c0 = fx.commit(
        None,
        &[
            ("a.txt", Some("a\n")),
            (".gitattributes", Some("*.txt filter=x diff=x\n")),
        ],
        "c0",
    );
    fx.branch("main", c0);
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.track("main");
    fx.set_head("main");
    fx.checkout_head();
    let markers = tempfile::tempdir().unwrap();
    let m = |n: &str| markers.path().join(n).display().to_string();
    let cfg = format!(
        "[filter \"x\"]\n\tclean = \"touch {c}; cat\"\n\tsmudge = \"touch {s}; cat\"\n\tprocess = \"touch {p}\"\n\trequired = false\n\
         [diff \"x\"]\n\ttextconv = \"touch {t}; cat\"\n\
         [credential]\n\thelper = \"!f() {{ touch {h}; }}; f\"\n\
         [core]\n\tsshCommand = \"touch {ssh}\"\n\tfsmonitor = \"touch {fsm}\"\n\tpager = \"touch {pg}\"\n\teditor = \"touch {ed}\"\n",
        c = m("clean"),
        s = m("smudge"),
        p = m("process"),
        t = m("textconv"),
        h = m("helper"),
        ssh = m("ssh"),
        fsm = m("fsmonitor"),
        pg = m("pager"),
        ed = m("editor"),
    );
    use std::io::Write;
    std::fs::OpenOptions::new()
        .append(true)
        .open(fx.path.join(".git/config"))
        .unwrap()
        .write_all(cfg.as_bytes())
        .unwrap();
    // a hook, and a modified tracked file so that status has to look at the content
    let hook = fx.path.join(".git/hooks/post-checkout");
    std::fs::create_dir_all(hook.parent().unwrap()).unwrap();
    std::fs::write(&hook, format!("#!/bin/sh\ntouch {}\n", m("hook"))).unwrap();
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(&hook, std::fs::Permissions::from_mode(0o755)).unwrap();
    fx.write("a.txt", "b\n");
    (fx, markers)
}

fn markers(dir: &Path) -> Vec<String> {
    let mut v: Vec<String> = std::fs::read_dir(dir)
        .unwrap()
        .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
        .collect();
    v.sort();
    v
}

fn run(fx: &Fx, args: &[&str]) {
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", "/usr/bin:/bin")
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .args(args)
        .args(["--json", "--root"])
        .arg(&fx.path)
        .output()
        .unwrap();
    assert!(
        o.status.code() == Some(0),
        "{args:?}: {}{}",
        String::from_utf8_lossy(&o.stdout),
        String::from_utf8_lossy(&o.stderr)
    );
}

#[test]
fn status_starts_no_program_named_by_the_repository() {
    let (fx, m) = hostile();
    run(&fx, &["status", "--offline"]);
    assert_eq!(
        markers(m.path()),
        Vec::<String>::new(),
        "status started a program"
    );
}

#[test]
fn tidy_plan_starts_no_program_named_by_the_repository() {
    let (fx, m) = hostile();
    run(&fx, &["tidy", "plan", "--offline"]);
    assert_eq!(
        markers(m.path()),
        Vec::<String>::new(),
        "tidy plan started a program"
    );
}

#[test]
fn sync_dry_run_and_offline_start_no_program_named_by_the_repository() {
    let (fx, m) = hostile();
    run(&fx, &["sync"]);
    run(&fx, &["sync", "--offline", "--apply"]);
    assert_eq!(
        markers(m.path()),
        Vec::<String>::new(),
        "sync started a program"
    );
}

/// Every repository the commands read is opened by `repo::open` (which drops filter drivers). A direct
/// `gix::open`, `gix::open_opts` or `gix::discover` elsewhere would bypass that.
#[test]
fn repositories_are_opened_only_through_repo_open() {
    fn walk(dir: &Path, out: &mut Vec<std::path::PathBuf>) {
        for e in std::fs::read_dir(dir).unwrap() {
            let p = e.unwrap().path();
            if p.is_dir() {
                walk(&p, out);
            } else if p.extension().is_some_and(|x| x == "rs") {
                out.push(p);
            }
        }
    }
    let mut files = vec![];
    walk(
        &Path::new(env!("CARGO_MANIFEST_DIR")).join("src"),
        &mut files,
    );
    for f in files {
        let rel = f
            .strip_prefix(env!("CARGO_MANIFEST_DIR"))
            .unwrap()
            .to_string_lossy()
            .into_owned();
        // the repo module itself, and the runner, which reads only the repository's own config
        if rel == "src/repo/mod.rs" || rel == "src/git_runner.rs" {
            continue;
        }
        let text = std::fs::read_to_string(&f).unwrap();
        for (n, line) in text.lines().enumerate() {
            let code = line.split("//").next().unwrap();
            for bad in [
                "gix::open(",
                "gix::open_opts(",
                "gix::discover(",
                "gix::ThreadSafeRepository::open",
            ] {
                assert!(
                    !code.contains(bad),
                    "{rel}:{}: {bad} bypasses repo::open",
                    n + 1
                );
            }
        }
    }
}
