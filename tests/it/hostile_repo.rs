//! A repository that names programs everywhere (filter drivers, textconv, credential helper, hooks, ssh
//! command) must not make `status`, `tidy plan` or `sync` start any of them. Markers are files the programs
//! would create; `sh` is on PATH so that a program could really run.
use std::path::Path;
use std::process::Command;

use crate::common::*;

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
    run_expect(fx, args, 0);
}

fn run_expect(fx: &Fx, args: &[&str], code: i32) {
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
        o.status.code() == Some(code),
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
    run(&fx, &["sync", "--plan", "--offline"]);
    run(&fx, &["sync", "--offline"]);
    assert_eq!(
        markers(m.path()),
        Vec::<String>::new(),
        "sync started a program"
    );
}

#[test]
fn sync_plan_fetching_a_local_remote_starts_no_program_named_by_the_repository() {
    let (fx, m) = hostile();
    let up = Fx::new();
    let c0 = up.commit(None, &[("a.txt", Some("a\n"))], "c0");
    up.branch("main", c0);
    up.set_head("main");
    fx.set_origin_url(&up.path.display().to_string());
    // the runner refuses a repository whose own config names a credential helper: the plan is an error, no program ran
    run_expect(&fx, &["sync", "--plan"], 1);
    assert_eq!(
        markers(m.path()),
        Vec::<String>::new(),
        "the fetch of a plan started a program"
    );
}

/// Imports that would let `gix::discover`, `gix::open`, `gix::open_opts` or `gix::ThreadSafeRepository` be called
/// under a short or another name (`use gix::discover;`, `use gix::{open_opts as o}`, `use gix as g;`): the text search
/// for the full path does not see those calls. Returns what is wrong in `code` (comments already removed).
fn import_bypasses(code: &str) -> Vec<String> {
    let flat = code.split_whitespace().collect::<Vec<_>>().join(" ");
    let mut out = vec![];
    for stmt in flat.split(';') {
        let stmt = stmt.trim();
        let stmt = stmt.strip_prefix("pub ").unwrap_or(stmt);
        let Some(rest) = stmt
            .strip_prefix("use ")
            .or(stmt.strip_prefix("extern crate "))
        else {
            continue;
        };
        let rest = rest.trim_start_matches("::");
        if !(rest == "gix" || rest.starts_with("gix ") || rest.starts_with("gix::")) {
            continue;
        }
        if rest.starts_with("gix as ") || rest == "gix" {
            out.push(format!("`{stmt}` renames the crate"));
            continue;
        }
        // `open::Options` and `discover::upwards` name modules; the bare names import the functions too
        let rest = rest.replace("open::", "").replace("discover::", "");
        let hit = rest
            .split(|c: char| !(c.is_alphanumeric() || c == '_'))
            .find(|t| {
                matches!(
                    *t,
                    "discover" | "open" | "open_opts" | "ThreadSafeRepository"
                )
            });
        if let Some(t) = hit {
            out.push(format!("`{stmt}` imports {t}"));
        }
    }
    out
}

#[test]
fn the_import_check_sees_short_names_and_aliases_of_the_opening_functions() {
    for bad in [
        "use gix::discover;",
        "use gix::open;",
        "use gix::open_opts as o;",
        "use gix::{ObjectId, discover as d};",
        "use gix::{\n    ObjectId,\n    open_opts,\n};",
        "use gix::ThreadSafeRepository as T;",
        "use ::gix::discover;",
        "pub use gix::open;",
        "use gix as g;",
        "extern crate gix as g;",
    ] {
        assert!(!import_bypasses(bad).is_empty(), "not seen: {bad}");
    }
    for good in [
        "use gix::open::Options;",
        "use gix::{open::Permissions, ObjectId};",
        "use gix::bstr::{BStr, ByteSlice};",
        "use gix::ObjectId;",
        "use std::fs::open;",
        "let discover = 1;",
    ] {
        assert_eq!(import_bypasses(good), Vec::<String>::new(), "{good}");
    }
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
        // the repo module opens repositories itself, `repo/discover.rs` applies ceiling and owner check, and the
        // runner reads only the repository's own config
        if rel == "src/repo/discover.rs" || rel == "src/git_runner.rs" {
            continue;
        }
        let text = std::fs::read_to_string(&f).unwrap();
        let code_only = text
            .lines()
            .map(|l| l.split("//").next().unwrap())
            .collect::<Vec<_>>()
            .join("\n");
        assert_eq!(
            import_bypasses(&code_only),
            Vec::<String>::new(),
            "{rel}: an import hides the opening functions from this test"
        );
        for (n, line) in text.lines().enumerate() {
            let code = line.split("//").next().unwrap();
            // discovery goes through repo::discover everywhere, `src/repo/mod.rs` included
            for bad in [
                "gix::discover(",
                "ThreadSafeRepository::discover",
                "discover_opts(",
            ] {
                assert!(
                    !code.contains(bad),
                    "{rel}:{}: {bad} bypasses repo::discover",
                    n + 1
                );
            }
            if rel == "src/repo/mod.rs" {
                continue;
            }
            for bad in [
                "gix::open(",
                "gix::open_opts(",
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

// ---- fast-forward and filter attributes ---------------------------------------------------------

/// main at c0, origin/main at c1 (changes `changed`); `.gitattributes` sends `*.bin` through filter `lfs`,
/// whose smudge program is configured when `driver` is set.
fn ff_fixture(changed: &str, driver: bool) -> (Fx, tempfile::TempDir, gix::ObjectId) {
    let fx = Fx::new();
    let base = [
        (".gitattributes", Some("*.bin filter=lfs\n")),
        ("data.bin", Some("pointer-v1\n")),
        ("other.txt", Some("o1\n")),
    ];
    let c0 = fx.commit(None, &base, "c0");
    let c1 = fx.commit(Some(c0), &[(changed, Some("v2\n"))], "c1");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.track("main");
    fx.checkout_head();
    let markers = tempfile::tempdir().unwrap();
    if driver {
        use std::io::Write;
        std::fs::OpenOptions::new()
            .append(true)
            .open(fx.path.join(".git/config"))
            .unwrap()
            .write_all(
                format!(
                    "[filter \"lfs\"]\n\tsmudge = \"touch {}; echo SMUDGED\"\n\tclean = cat\n",
                    markers.path().join("smudge").display()
                )
                .as_bytes(),
            )
            .unwrap();
    }
    (fx, markers, c0)
}

fn sync_apply(fx: &Fx) -> serde_json::Value {
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", "/usr/bin:/bin")
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .args(["sync", "--offline", "--json", "--root"])
        .arg(&fx.path)
        .output()
        .unwrap();
    serde_json::from_slice(&o.stdout).unwrap()
}

#[test]
fn fast_forward_is_refused_before_any_write_when_a_changed_path_has_a_filter_attribute() {
    for driver in [true, false] {
        let (fx, mdir, c0) = ff_fixture("data.bin", driver);
        let v = sync_apply(&fx);
        let r = &v["repos"][0];
        assert_eq!(r["result"], "error", "driver={driver}: {r}");
        assert!(
            r["error"].as_str().unwrap().contains("data.bin")
                && r["error"].as_str().unwrap().contains("filter"),
            "{r}"
        );
        assert_eq!(fx.tip("main"), c0, "the ref must not move");
        assert_eq!(
            fx.read("data.bin").as_deref(),
            Some("pointer-v1\n"),
            "nothing was written"
        );
        assert_eq!(markers(mdir.path()), Vec::<String>::new());
    }
}

#[test]
fn fast_forward_of_paths_without_a_filter_attribute_still_works_next_to_a_filter_driver() {
    let (fx, mdir, _c0) = ff_fixture("other.txt", true);
    let v = sync_apply(&fx);
    let r = &v["repos"][0];
    assert_eq!(r["result"], "fast-forwarded", "{r}");
    assert_eq!(fx.read("other.txt").as_deref(), Some("v2\n"));
    assert_eq!(markers(mdir.path()), Vec::<String>::new());
}

#[test]
fn fast_forward_uses_the_attributes_of_the_target_tree_and_checks_added_paths() {
    // c1 adds the filter rule and a new filtered file: the old tree has neither
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("other.txt", Some("o1\n"))], "c0");
    let c1 = fx.commit(
        Some(c0),
        &[
            (".gitattributes", Some("*.bin filter=lfs\n")),
            ("new.bin", Some("pointer\n")),
        ],
        "c1",
    );
    fx.branch("main", c0);
    fx.set_head("main");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.track("main");
    fx.checkout_head();
    let v = sync_apply(&fx);
    let r = &v["repos"][0];
    assert_eq!(r["result"], "error", "{r}");
    assert!(r["error"].as_str().unwrap().contains("new.bin"), "{r}");
    assert!(!fx.exists("new.bin"));
    assert_eq!(fx.tip("main"), c0);

    // the old tree had the rule, the new one removes it and changes a .bin: nothing filters it any more
    let fx = Fx::new();
    let c0 = fx.commit(
        None,
        &[
            (".gitattributes", Some("*.bin filter=lfs\n")),
            ("data.bin", Some("a\n")),
        ],
        "c0",
    );
    let c1 = fx.commit(
        Some(c0),
        &[(".gitattributes", None), ("data.bin", Some("b\n"))],
        "c1",
    );
    fx.branch("main", c0);
    fx.set_head("main");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.track("main");
    fx.checkout_head();
    let v = sync_apply(&fx);
    assert_eq!(v["repos"][0]["result"], "fast-forwarded", "{v}");
}
