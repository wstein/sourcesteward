//! `stw oplog hooks status | install | uninstall | print`: the two op log hooks (`post-rewrite`, `reference-transaction`) in
//! one repository, opt-in (docs/oplog-format.md, 11). Repositories are built by the `git` binary in a temporary directory
//! (reference use, as in the other CLI tests); `stw` itself runs with `PATH=/nonexistent`: the install starts no program.
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::Value;

const POST: &str = "post-rewrite";
const REF: &str = "reference-transaction";
const HEADER_POST: &str = "# stw-oplog-hook v1";
const HEADER_REF: &str = "# stw-oplog-refhook v1";

struct Run {
    code: i32,
    out: String,
    err: String,
}

struct R {
    _tmp: tempfile::TempDir,
    home: PathBuf,
    repo: PathBuf,
}

fn tmp_base() -> PathBuf {
    std::env::var_os("CLAUDE_JOB_DIR")
        .map(|d| PathBuf::from(d).join("tmp"))
        .filter(|p| p.is_dir())
        .unwrap_or_else(std::env::temp_dir)
}

/// A scrubbed `git` for fixtures and for the hooks' own work (identity from the environment of the child only).
fn git_in(home: &Path, dir: &Path, args: &[&str]) -> std::process::Output {
    assert!(dir.is_absolute() && dir.exists(), "unconfined: {dir:?}");
    let o = Command::new("git")
        .env_clear()
        .env("PATH", "/usr/bin:/bin:/usr/local/bin")
        .env("HOME", home)
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_AUTHOR_NAME", "T")
        .env("GIT_AUTHOR_EMAIL", "t@example.com")
        .env("GIT_COMMITTER_NAME", "T")
        .env("GIT_COMMITTER_EMAIL", "t@example.com")
        .current_dir(dir)
        .args(args)
        .output()
        .unwrap();
    assert!(
        o.status.success(),
        "git {args:?} failed: {}",
        String::from_utf8_lossy(&o.stderr)
    );
    o
}

impl R {
    fn new() -> R {
        let tmp = tempfile::Builder::new()
            .prefix("stw-hooks-")
            .tempdir_in(tmp_base())
            .unwrap();
        let home = tmp.path().join("home");
        std::fs::create_dir(&home).unwrap();
        let repo = tmp.path().join("repo");
        std::fs::create_dir(&repo).unwrap();
        let r = R {
            _tmp: tmp,
            home,
            repo,
        };
        r.git(&["init", "-q", "-b", "main"]);
        std::fs::write(r.repo.join("f"), "1\n").unwrap();
        r.git(&["add", "f"]);
        r.git(&["commit", "-q", "-m", "c1"]);
        r
    }

    fn git(&self, args: &[&str]) -> String {
        self.git_at(&self.repo, args)
    }

    fn git_at(&self, dir: &Path, args: &[&str]) -> String {
        String::from_utf8_lossy(&git_in(&self.home, dir, args).stdout)
            .trim()
            .to_string()
    }

    fn stw_env(&self, dir: &Path, args: &[&str], env: &[(&str, &str)]) -> Run {
        assert!(dir.is_absolute(), "unconfined");
        let mut c = Command::new(env!("CARGO_BIN_EXE_stw"));
        c.env_clear()
            .env("PATH", "/nonexistent")
            .env("HOME", &self.home)
            .current_dir(dir)
            .args(["oplog", "hooks"])
            .args(args);
        for (k, v) in env {
            c.env(k, v);
        }
        let o = c.output().unwrap();
        Run {
            code: o.status.code().unwrap_or(-1),
            out: String::from_utf8_lossy(&o.stdout).into_owned(),
            err: String::from_utf8_lossy(&o.stderr).into_owned(),
        }
    }

    fn stw(&self, args: &[&str]) -> Run {
        self.stw_env(&self.repo, args, &[])
    }

    fn stw_json(&self, args: &[&str]) -> Value {
        let mut a = args.to_vec();
        a.push("--json");
        let r = self.stw(&a);
        serde_json::from_str(&r.out)
            .unwrap_or_else(|e| panic!("not JSON ({e}): {}\nstderr: {}", r.out, r.err))
    }

    /// The top-level `stw oplog <args>` (timeline, evolog).
    fn oplog(&self, args: &[&str]) -> Run {
        let o = Command::new(env!("CARGO_BIN_EXE_stw"))
            .env_clear()
            .env("PATH", "/nonexistent")
            .env("HOME", &self.home)
            .current_dir(&self.repo)
            .arg("oplog")
            .args(args)
            .output()
            .unwrap();
        Run {
            code: o.status.code().unwrap_or(-1),
            out: String::from_utf8_lossy(&o.stdout).into_owned(),
            err: String::from_utf8_lossy(&o.stderr).into_owned(),
        }
    }

    fn dot_git(&self) -> PathBuf {
        self.repo.join(".git")
    }

    fn hook(&self, name: &str) -> PathBuf {
        self.dot_git().join("hooks").join(name)
    }

    fn oplog_dir(&self) -> PathBuf {
        self.dot_git().join("stw/oplog")
    }

    fn read(&self, p: &Path) -> String {
        std::fs::read_to_string(p).unwrap_or_default()
    }

    fn install(&self) -> Run {
        let r = self.stw(&["install", "--yes"]);
        assert_eq!(r.code, 0, "{}{}", r.out, r.err);
        r
    }
}

/// Every file, directory and link below `dir` with its content (or link target), sorted: "nothing was written" is
/// equality of two of these.
fn snapshot(dir: &Path) -> Vec<(String, String)> {
    fn walk(base: &Path, dir: &Path, out: &mut Vec<(String, String)>) {
        let mut ents: Vec<_> = std::fs::read_dir(dir).unwrap().flatten().collect();
        ents.sort_by_key(|e| e.file_name());
        for e in ents {
            let p = e.path();
            let rel = p.strip_prefix(base).unwrap().to_string_lossy().into_owned();
            let md = std::fs::symlink_metadata(&p).unwrap();
            if md.file_type().is_symlink() {
                let t = std::fs::read_link(&p).unwrap();
                out.push((rel, format!("-> {}", t.display())));
            } else if md.is_dir() {
                out.push((rel.clone(), "dir".into()));
                walk(base, &p, out);
            } else {
                let mode = md.permissions().mode() & 0o777;
                out.push((
                    rel,
                    format!(
                        "{mode:o} {}",
                        String::from_utf8_lossy(&std::fs::read(&p).unwrap())
                    ),
                ));
            }
        }
    }
    let mut v = Vec::new();
    walk(dir, dir, &mut v);
    v
}

fn is_exec(p: &Path) -> bool {
    std::fs::metadata(p).unwrap().permissions().mode() & 0o111 != 0
}

fn line2(p: &Path) -> String {
    std::fs::read_to_string(p)
        .unwrap()
        .lines()
        .nth(1)
        .unwrap_or("")
        .to_string()
}

#[test]
fn install_without_yes_prints_the_plan_and_writes_nothing() {
    let r = R::new();
    let before = snapshot(&r.dot_git());
    let o = r.stw(&["install"]);
    assert_eq!(o.code, 1, "{}{}", o.out, o.err);
    assert!(
        o.out.contains("not executed: --yes is missing"),
        "{}",
        o.out
    );
    assert!(o.out.contains("plan     write"), "{}", o.out);
    assert!(o.out.contains(POST) && o.out.contains(REF), "{}", o.out);
    assert!(
        o.out.contains("runs on every ref change"),
        "the plan names what the reference-transaction hook does: {}",
        o.out
    );
    assert_eq!(snapshot(&r.dot_git()), before);
    let j = r.stw_json(&["install"]);
    assert_eq!(j["command"], "oplog hooks install");
    assert_eq!(j["executed"], false);
    assert_eq!(snapshot(&r.dot_git()), before);
}

#[test]
fn install_yes_writes_both_hooks_executable_with_their_header_and_an_install_event() {
    let r = R::new();
    let o = r.install();
    assert!(o.out.contains("Result: installed"), "{}", o.out);
    for (name, header) in [(POST, HEADER_POST), (REF, HEADER_REF)] {
        let p = r.hook(name);
        assert!(p.is_file(), "{name}");
        assert!(is_exec(&p), "{name} must be executable");
        assert_eq!(
            std::fs::read_to_string(&p).unwrap().lines().next(),
            Some("#!/bin/sh")
        );
        assert_eq!(line2(&p), header, "{name}");
    }
    for ev in ["events.log", "events-refhook.log"] {
        let s = r.read(&r.oplog_dir().join(ev));
        let lines: Vec<&str> = s.lines().collect();
        assert_eq!(lines.len(), 1, "{ev}: {s:?}");
        let f: Vec<&str> = lines[0].split('\t').collect();
        assert_eq!(f.len(), 2, "{ev}");
        assert_eq!(f[1], "install");
        assert!(f[0].len() == 20 && f[0].ends_with('Z'), "{ev}: {}", f[0]);
    }
    // no leftover temporary file next to the hooks
    let names: Vec<String> = std::fs::read_dir(r.dot_git().join("hooks"))
        .unwrap()
        .flatten()
        .map(|e| e.file_name().to_string_lossy().into_owned())
        .filter(|n| n.starts_with('.'))
        .collect();
    assert!(names.is_empty(), "{names:?}");
    // the oplog directory is private
    let m = std::fs::metadata(r.oplog_dir())
        .unwrap()
        .permissions()
        .mode()
        & 0o777;
    assert_eq!(m, 0o700);
    // no git config is written
    assert!(!r.read(&r.dot_git().join("config")).contains("hooksPath"));
}

#[test]
fn the_installed_text_is_the_text_of_the_spikes() {
    // the spikes are the tested scripts; the product embeds the same bytes (skipped where the spikes are not checked out)
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../spikes/stw");
    let spikes = [
        (POST, root.join("oplog-hooks/stw-oplog-hook")),
        (REF, root.join("oplog-refhook/stw-oplog-refhook")),
    ];
    if !spikes.iter().all(|(_, p)| p.is_file()) {
        eprintln!("spikes not checked out: skipped");
        return;
    }
    let r = R::new();
    r.install();
    for (name, spike) in spikes {
        let o = Command::new("bash")
            .env_clear()
            .env("PATH", "/usr/bin:/bin")
            .arg(&spike)
            .arg("print")
            .output()
            .unwrap();
        assert!(o.status.success());
        assert_eq!(std::fs::read(r.hook(name)).unwrap(), o.stdout, "{name}");
        // `stw oplog hooks print NAME` writes the same text
        let p = r.stw(&["print", name]);
        assert_eq!(p.code, 0);
        assert_eq!(p.out.as_bytes(), o.stdout.as_slice(), "{name}");
    }
}

#[test]
fn print_writes_the_hook_text_and_nothing_else() {
    let r = R::new();
    let before = snapshot(&r.dot_git());
    let p = r.stw(&["print", POST]);
    assert_eq!(p.code, 0, "{}{}", p.out, p.err);
    assert_eq!(p.out.lines().nth(1), Some(HEADER_POST));
    let q = r.stw(&["print", REF]);
    assert_eq!(q.out.lines().nth(1), Some(HEADER_REF));
    let bad = r.stw(&["print", "pre-commit"]);
    assert_eq!(bad.code, 2, "{}{}", bad.out, bad.err);
    assert_eq!(snapshot(&r.dot_git()), before);
}

#[test]
fn install_twice_changes_nothing_the_second_time() {
    let r = R::new();
    r.install();
    let before = snapshot(&r.dot_git());
    let o = r.stw(&["install", "--yes"]);
    assert_eq!(o.code, 0, "{}{}", o.out, o.err);
    assert!(o.out.contains("already installed"), "{}", o.out);
    assert_eq!(
        snapshot(&r.dot_git()),
        before,
        "no second event, no rewrite"
    );
    let j = r.stw_json(&["install", "--yes"]);
    assert_eq!(j["executed"], false);
    assert_eq!(j["hooks"][0]["action"], "already-installed");
}

#[test]
fn an_installed_hook_records_rewrites_and_ref_changes_in_the_logs_the_readers_read() {
    let r = R::new();
    r.install();
    let old = r.git(&["rev-parse", "HEAD"]);
    r.git(&["commit", "-q", "--amend", "-m", "c1b"]);
    let new = r.git(&["rev-parse", "HEAD"]);
    assert_ne!(old, new);
    r.git(&["tag", "v1"]);
    r.git(&["branch", "work"]);
    r.git(&["branch", "-D", "work"]);
    let rw = r.read(&r.oplog_dir().join("rewrites.log"));
    let rows: Vec<Vec<&str>> = rw.lines().map(|l| l.split('\t').collect()).collect();
    assert_eq!(rows.len(), 1, "{rw}");
    assert_eq!(rows[0].len(), 6);
    assert_eq!(
        (rows[0][1], rows[0][2], rows[0][3]),
        ("amend", old.as_str(), new.as_str())
    );
    assert_eq!(rows[0][5], "main");
    let refs = r.read(&r.oplog_dir().join("refs.log"));
    let tag: Vec<&str> = refs
        .lines()
        .filter(|l| l.split('\t').nth(3) == Some("refs/tags/v1"))
        .collect();
    assert_eq!(tag.len(), 1, "{refs}");
    assert_eq!(tag[0].split('\t').count(), 6);
    assert!(
        refs.lines().any(|l| {
            let f: Vec<&str> = l.split('\t').collect();
            f.get(3) == Some(&"refs/heads/work")
                && f.get(2).is_some_and(|n| n.chars().all(|c| c == '0'))
        }),
        "the delete of the branch has a line: {refs}"
    );
    // the product's own readers understand what the hooks wrote
    let e = r.oplog(&["evolog", "HEAD"]);
    assert_eq!(e.code, 0, "{}{}", e.out, e.err);
    assert!(e.out.contains(&old[..7]), "{}", e.out);
    let t = r.oplog(&["timeline", "--json"]);
    let v: Value = serde_json::from_str(&t.out).unwrap();
    let kinds: Vec<&str> = v["gaps"]
        .as_array()
        .unwrap()
        .iter()
        .filter_map(|g| g["kind"].as_str())
        .collect();
    assert!(!kinds.contains(&"no-install-record"), "{kinds:?}");
    assert!(!kinds.contains(&"capture-gap"), "{kinds:?}");
}

#[test]
fn a_foreign_hook_is_never_overwritten_and_then_nothing_at_all_is_installed() {
    let r = R::new();
    let hooks = r.dot_git().join("hooks");
    std::fs::create_dir_all(&hooks).unwrap();
    std::fs::write(hooks.join(POST), "#!/bin/sh\n# mine\nexit 0\n").unwrap();
    std::fs::set_permissions(hooks.join(POST), std::fs::Permissions::from_mode(0o755)).unwrap();
    let before = snapshot(&r.dot_git());
    for args in [&["install"][..], &["install", "--yes"][..]] {
        let o = r.stw(args);
        assert_eq!(o.code, 1, "{}{}", o.out, o.err);
        assert!(
            o.out.contains("exists and is not from this tool"),
            "{}",
            o.out
        );
        assert!(o.out.contains("Chain proposal"), "{}", o.out);
        assert!(o.out.contains("stw oplog hooks print"), "{}", o.out);
    }
    assert_eq!(
        snapshot(&r.dot_git()),
        before,
        "the other hook is not installed either"
    );
    assert!(!r.hook(REF).exists());
    let j = r.stw_json(&["install", "--yes"]);
    assert_eq!(j["executed"], false);
    assert_eq!(j["hooks"][0]["state"], "foreign");
    assert_eq!(j["hooks"][0]["action"], "refused");
}

#[test]
fn a_foreign_hook_that_is_a_link_is_foreign_too() {
    let r = R::new();
    let hooks = r.dot_git().join("hooks");
    std::fs::create_dir_all(&hooks).unwrap();
    let real = r.repo.join("real-hook");
    std::fs::write(&real, format!("#!/bin/sh\n{HEADER_REF}\nexit 0\n")).unwrap();
    std::os::unix::fs::symlink(&real, hooks.join(REF)).unwrap();
    let before = snapshot(&r.dot_git());
    let o = r.stw(&["install", "--yes"]);
    assert_eq!(o.code, 1, "{}{}", o.out, o.err);
    assert_eq!(snapshot(&r.dot_git()), before);
    // and uninstall does not follow it
    let u = r.stw(&["uninstall", "--yes"]);
    assert_eq!(u.code, 0, "{}{}", u.out, u.err);
    assert!(hooks.join(REF).symlink_metadata().is_ok());
    assert!(real.exists());
}

fn assert_refused_by_hookspath(r: &R, o: &Run, value: &str) {
    assert_eq!(o.code, 1, "{}{}", o.out, o.err);
    assert!(
        o.out.contains("refused  core.hooksPath is set"),
        "{}",
        o.out
    );
    assert!(o.out.contains(value), "{}", o.out);
    assert!(
        o.out.contains(&format!("{value}/{POST}")) || value.is_empty(),
        "names the place the hook would belong: {}",
        o.out
    );
    assert!(!r.hook(POST).exists() && !r.hook(REF).exists());
    assert!(!r.oplog_dir().exists());
}

#[test]
fn core_hookspath_from_the_repository_config_refuses() {
    let r = R::new();
    r.git(&["config", "core.hooksPath", "/nonexistent-hp/local"]);
    let before = snapshot(&r.dot_git());
    for args in [&["install"][..], &["install", "--yes"][..]] {
        let o = r.stw(args);
        assert_refused_by_hookspath(&r, &o, "/nonexistent-hp/local");
        assert!(
            o.out.contains("This tool never changes core.hooksPath")
                || o.out.contains("never changes"),
            "{}",
            o.out
        );
    }
    assert_eq!(snapshot(&r.dot_git()), before, "the config is not touched");
}

#[test]
fn core_hookspath_from_the_global_config_refuses() {
    let r = R::new();
    std::fs::write(
        r.home.join(".gitconfig"),
        "[core]\n\thooksPath = /nonexistent-hp/global\n",
    )
    .unwrap();
    let o = r.stw(&["install", "--yes"]);
    assert_refused_by_hookspath(&r, &o, "/nonexistent-hp/global");
    assert!(
        o.out.contains(".gitconfig"),
        "the origin is named: {}",
        o.out
    );
}

#[test]
fn core_hookspath_from_the_environment_refuses() {
    let r = R::new();
    let o = r.stw_env(
        &r.repo,
        &["install", "--yes"],
        &[
            ("GIT_CONFIG_COUNT", "1"),
            ("GIT_CONFIG_KEY_0", "core.hooksPath"),
            ("GIT_CONFIG_VALUE_0", "/nonexistent-hp/env"),
        ],
    );
    assert_refused_by_hookspath(&r, &o, "/nonexistent-hp/env");
    let p = r.stw_env(
        &r.repo,
        &["install", "--yes"],
        &[(
            "GIT_CONFIG_PARAMETERS",
            "'core.hooksPath=/nonexistent-hp/params'",
        )],
    );
    assert_refused_by_hookspath(&r, &p, "/nonexistent-hp/params");
}

#[test]
fn an_empty_core_hookspath_counts_as_set() {
    let r = R::new();
    r.git(&["config", "core.hooksPath", ""]);
    let o = r.stw(&["install", "--yes"]);
    assert_eq!(o.code, 1, "{}{}", o.out, o.err);
    assert!(o.out.contains("core.hooksPath is set"), "{}", o.out);
    assert!(!r.hook(POST).exists());
}

#[test]
fn a_hookspath_value_with_control_characters_cannot_forge_output_lines() {
    let r = R::new();
    let o = r.stw_env(
        &r.repo,
        &["install", "--yes"],
        &[
            ("GIT_CONFIG_COUNT", "1"),
            ("GIT_CONFIG_KEY_0", "core.hooksPath"),
            ("GIT_CONFIG_VALUE_0", "/x\nResult: installed fake"),
        ],
    );
    assert_eq!(o.code, 1, "{}", o.out);
    assert!(
        !o.out.lines().any(|l| l.starts_with("Result: installed")),
        "{}",
        o.out
    );
    assert!(o.out.contains("\\n"), "{}", o.out);
}

#[test]
fn a_hooks_directory_that_is_a_symbolic_link_is_not_written_through() {
    let r = R::new();
    let shared = r.repo.join("shared-hooks");
    std::fs::create_dir(&shared).unwrap();
    let hooks = r.dot_git().join("hooks");
    std::fs::remove_dir_all(&hooks).unwrap();
    std::os::unix::fs::symlink(&shared, &hooks).unwrap();
    let before = snapshot(&r.dot_git());
    let shared_before = snapshot(&shared);
    let o = r.stw(&["install", "--yes"]);
    assert_eq!(o.code, 1, "{}{}", o.out, o.err);
    assert!(o.out.contains("symbolic link"), "{}", o.out);
    assert_eq!(snapshot(&shared), shared_before);
    assert_eq!(snapshot(&r.dot_git()), before);
    // our own file in a shared directory is not removed through the link either
    std::fs::write(shared.join(POST), format!("#!/bin/sh\n{HEADER_POST}\n")).unwrap();
    let u = r.stw(&["uninstall", "--yes"]);
    assert_eq!(u.code, 1, "{}{}", u.out, u.err);
    assert!(u.out.contains("symbolic link"), "{}", u.out);
    assert!(shared.join(POST).exists());
}

#[test]
fn a_missing_hooks_directory_is_created() {
    let r = R::new();
    std::fs::remove_dir_all(r.dot_git().join("hooks")).unwrap();
    r.install();
    assert!(is_exec(&r.hook(POST)) && is_exec(&r.hook(REF)));
}

#[test]
fn a_log_directory_that_is_a_symbolic_link_is_not_written_through() {
    let r = R::new();
    let elsewhere = r.repo.join("elsewhere");
    std::fs::create_dir(&elsewhere).unwrap();
    std::os::unix::fs::symlink(&elsewhere, r.dot_git().join("stw")).unwrap();
    let o = r.stw(&["install", "--yes"]);
    assert_eq!(o.code, 0, "{}{}", o.out, o.err);
    assert!(
        o.out.contains("note"),
        "the missing install record is named: {}",
        o.out
    );
    assert!(
        std::fs::read_dir(&elsewhere).unwrap().next().is_none(),
        "nothing is written through the link"
    );
    assert!(r.hook(POST).exists());
}

#[test]
fn uninstall_removes_only_our_files_and_leaves_the_logs() {
    let r = R::new();
    r.install();
    r.git(&["commit", "-q", "--amend", "-m", "again"]);
    let log = r.read(&r.oplog_dir().join("rewrites.log"));
    assert!(!log.is_empty());
    // without --yes: plan only
    let before = snapshot(&r.dot_git());
    let p = r.stw(&["uninstall"]);
    assert_eq!(p.code, 1, "{}{}", p.out, p.err);
    assert!(p.out.contains("plan     remove"), "{}", p.out);
    assert!(
        p.out.contains("not executed: --yes is missing"),
        "{}",
        p.out
    );
    assert_eq!(snapshot(&r.dot_git()), before);
    let o = r.stw(&["uninstall", "--yes"]);
    assert_eq!(o.code, 0, "{}{}", o.out, o.err);
    assert!(o.out.contains("Result: removed"), "{}", o.out);
    assert!(!r.hook(POST).exists() && !r.hook(REF).exists());
    assert_eq!(
        r.read(&r.oplog_dir().join("rewrites.log")),
        log,
        "logs stay"
    );
    for ev in ["events.log", "events-refhook.log"] {
        let s = r.read(&r.oplog_dir().join(ev));
        let words: Vec<&str> = s.lines().filter_map(|l| l.split('\t').nth(1)).collect();
        assert_eq!(words, ["install", "uninstall"], "{ev}");
    }
    // nothing left: a second uninstall is a no-op with exit 0
    let again = r.stw(&["uninstall", "--yes"]);
    assert_eq!(again.code, 0, "{}{}", again.out, again.err);
    assert!(again.out.contains("nothing"), "{}", again.out);
}

#[test]
fn uninstall_leaves_a_foreign_hook_and_a_look_alike_header() {
    let r = R::new();
    let hooks = r.dot_git().join("hooks");
    std::fs::create_dir_all(&hooks).unwrap();
    // line 2 only starts like our header
    std::fs::write(
        hooks.join(POST),
        "#!/bin/sh\n# stw-oplog-hook v10\nexit 0\n",
    )
    .unwrap();
    std::fs::write(hooks.join(REF), "#!/bin/sh\n# something else\nexit 0\n").unwrap();
    let before = snapshot(&r.dot_git());
    let o = r.stw(&["uninstall", "--yes"]);
    assert_eq!(o.code, 0, "{}{}", o.out, o.err);
    assert!(o.out.contains("not ours"), "{}", o.out);
    assert_eq!(snapshot(&r.dot_git()), before);
}

#[test]
fn uninstall_removes_ours_and_keeps_the_foreign_one_next_to_it() {
    let r = R::new();
    r.install();
    std::fs::write(r.hook(POST), "#!/bin/sh\n# mine\nexit 0\n").unwrap();
    let foreign = std::fs::read(r.hook(POST)).unwrap();
    let o = r.stw(&["uninstall", "--yes"]);
    assert_eq!(o.code, 0, "{}{}", o.out, o.err);
    assert_eq!(std::fs::read(r.hook(POST)).unwrap(), foreign);
    assert!(!r.hook(REF).exists());
    // the install record of the removed hook says uninstall, the other file has no new line
    assert!(r
        .read(&r.oplog_dir().join("events-refhook.log"))
        .trim_end()
        .ends_with("uninstall"));
    assert!(r
        .read(&r.oplog_dir().join("events.log"))
        .trim_end()
        .ends_with("install"));
}

#[test]
fn a_linked_worktree_installs_into_the_common_hooks_and_logs_there() {
    let r = R::new();
    let wt = r.repo.parent().unwrap().join("wt");
    r.git(&["worktree", "add", "-q", "-b", "side", wt.to_str().unwrap()]);
    let o = r.stw_env(&wt, &["install", "--yes"], &[]);
    assert_eq!(o.code, 0, "{}{}", o.out, o.err);
    assert!(r.hook(POST).is_file() && r.hook(REF).is_file(), "{}", o.out);
    assert!(!r
        .dot_git()
        .join("worktrees")
        .join("wt")
        .join("hooks")
        .exists());
    std::fs::write(wt.join("g"), "2\n").unwrap();
    r.git_at(&wt, &["add", "g"]);
    r.git_at(&wt, &["commit", "-q", "--amend", "-m", "from the worktree"]);
    let rw = r.read(&r.oplog_dir().join("rewrites.log"));
    assert_eq!(rw.lines().count(), 1, "{rw}");
    assert!(rw.lines().next().unwrap().ends_with("\tside"), "{rw}");
    // status from the worktree sees the same
    let s = r.stw_env(&wt, &["status"], &[]);
    assert_eq!(s.code, 0, "{}{}", s.out, s.err);
    assert!(s.out.contains("worktrees    2"), "{}", s.out);
}

#[test]
fn a_submodule_has_its_own_hooks_directory() {
    let r = R::new();
    let sub = r.repo.parent().unwrap().join("subsrc");
    std::fs::create_dir(&sub).unwrap();
    r.git_at(&sub, &["init", "-q", "-b", "main"]);
    std::fs::write(sub.join("s"), "s\n").unwrap();
    r.git_at(&sub, &["add", "s"]);
    r.git_at(&sub, &["commit", "-q", "-m", "s1"]);
    r.git(&[
        "-c",
        "protocol.file.allow=always",
        "submodule",
        "add",
        "-q",
        sub.to_str().unwrap(),
        "libs/sub",
    ]);
    r.git(&["commit", "-q", "-m", "add the submodule"]);
    let st = r.stw(&["status", "--repo", "libs/sub"]);
    assert_eq!(st.code, 0, "{}{}", st.out, st.err);
    let o = r.stw(&["install", "--yes", "--repo", "libs/sub"]);
    assert_eq!(o.code, 0, "{}{}", o.out, o.err);
    let modules = r.dot_git().join("modules/libs/sub");
    assert!(modules.join("hooks").join(POST).is_file(), "{}", o.out);
    assert!(modules.join("stw/oplog/events.log").is_file());
    assert!(!r.hook(POST).exists(), "the superproject has none");
    // the superproject's status hints at the submodule
    let s = r.stw(&["status"]);
    assert!(s.out.contains("submodule"), "{}", s.out);
}

#[test]
fn status_reports_absent_ours_foreign_and_not_executable() {
    let r = R::new();
    let j = r.stw_json(&["status"]);
    assert_eq!(j["command"], "oplog hooks status");
    assert_eq!(j["hooks_path"], Value::Null);
    assert_eq!(j["hooks"][0]["name"], POST);
    assert_eq!(j["hooks"][0]["state"], "absent");
    assert_eq!(j["hooks"][1]["state"], "absent");
    let before = snapshot(&r.dot_git());
    let t = r.stw(&["status"]);
    assert_eq!(t.code, 0, "{}{}", t.out, t.err);
    assert!(t.out.contains("hooksPath    not set"), "{}", t.out);
    assert!(t.out.contains(&format!("{POST} absent")), "{}", t.out);
    assert_eq!(snapshot(&r.dot_git()), before, "status writes nothing");

    r.install();
    let j = r.stw_json(&["status"]);
    assert_eq!(j["hooks"][0]["state"], "ours");
    assert_eq!(j["hooks"][0]["executable"], true);
    std::fs::set_permissions(r.hook(REF), std::fs::Permissions::from_mode(0o644)).unwrap();
    let t = r.stw(&["status"]);
    assert!(t.out.contains(&format!("{REF} ours")), "{}", t.out);
    assert!(t.out.contains("NOT executable"), "{}", t.out);
    let j = r.stw_json(&["status"]);
    assert_eq!(j["hooks"][1]["executable"], false);

    std::fs::write(r.hook(POST), "#!/bin/sh\n# mine\n").unwrap();
    let j = r.stw_json(&["status"]);
    assert_eq!(j["hooks"][0]["state"], "foreign");

    r.git(&["config", "core.hooksPath", "/nonexistent-hp/status"]);
    let j = r.stw_json(&["status"]);
    assert_eq!(j["hooks_path"]["value"], "/nonexistent-hp/status");
    assert!(j["hooks_path"]["origin"]
        .as_str()
        .unwrap()
        .contains("config"));
    let t = r.stw(&["status"]);
    assert!(t.out.contains("install would be refused"), "{}", t.out);
}

#[test]
fn a_bare_repository_gets_the_hooks_in_its_own_directory() {
    let r = R::new();
    let bare = r.repo.parent().unwrap().join("bare.git");
    r.git(&[
        "clone",
        "-q",
        "--bare",
        r.repo.to_str().unwrap(),
        bare.to_str().unwrap(),
    ]);
    let o = r.stw_env(&bare, &["install", "--yes"], &[]);
    assert_eq!(o.code, 0, "{}{}", o.out, o.err);
    assert!(bare.join("hooks").join(POST).is_file(), "{}", o.out);
    assert!(bare.join("stw/oplog/events.log").is_file());
}
