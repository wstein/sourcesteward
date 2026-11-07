//! The product code starts a process in exactly one place: the `via: git` runner (`src/git_runner.rs`).
//! Everything else must not start `git`, `gh`, `ssh` or any other process. The architecture check stays:
//! it scans every source file and fails when a process is started anywhere else.
use std::path::{Path, PathBuf};

/// The one module that may start a process.
const RUNNER: &str = "git_runner.rs";

fn rust_files(dir: &Path, out: &mut Vec<PathBuf>) {
    for e in std::fs::read_dir(dir).unwrap() {
        let p = e.unwrap().path();
        if p.is_dir() {
            rust_files(&p, out);
        } else if p.extension().is_some_and(|x| x == "rs") {
            out.push(p);
        }
    }
}

/// Source patterns that start a process, found in the code part of a line (before `//`).
pub fn spawn_marker(code: &str) -> Option<&'static str> {
    [
        "Command::new",
        "process::Command",
        "CommandExt",
        "gix::command",
        "gix_command",
        "libc::system",
        "libc::exec",
        ".spawn()",
    ]
    .into_iter()
    .find(|m| code.contains(m))
    .or_else(|| {
        (code.contains("process::{") && code.contains("Command")).then_some("process::{Command}")
    })
}

#[test]
fn only_the_runner_spawns_a_process() {
    let mut files = vec![];
    rust_files(
        Path::new(concat!(env!("CARGO_MANIFEST_DIR"), "/src")),
        &mut files,
    );
    assert!(files.len() >= 10, "scan found only {} files", files.len());
    // the apply/restore module is part of the scan (a rename or move must not hide it)
    for must in ["tidy_apply.rs", "tidy.rs", "worktrees.rs", RUNNER] {
        assert!(
            files
                .iter()
                .any(|f| f.file_name().is_some_and(|n| n == must)),
            "{must} is not scanned"
        );
    }
    let mut runner_spawns = false;
    for f in files {
        let is_runner = f.file_name().is_some_and(|n| n == RUNNER);
        let text = std::fs::read_to_string(&f).unwrap();
        for (n, line) in text.lines().enumerate() {
            let code = line.split("//").next().unwrap();
            if let Some(marker) = spawn_marker(code) {
                if is_runner {
                    runner_spawns = true;
                } else {
                    panic!(
                        "{}:{}: starts a process ({marker}); only {RUNNER} may: {line}",
                        f.display(),
                        n + 1
                    );
                }
            }
        }
    }
    // the check stays live: the runner is the place that really starts git
    assert!(
        runner_spawns,
        "{RUNNER} no longer starts a process; is the scan still meaningful?"
    );
}

#[test]
fn the_runner_never_goes_through_a_shell() {
    let text =
        std::fs::read_to_string(concat!(env!("CARGO_MANIFEST_DIR"), "/src/git_runner.rs")).unwrap();
    for shell in [
        "\"sh\"",
        "\"/bin/sh\"",
        "\"bash\"",
        "\"zsh\"",
        "\"cmd\"",
        "\"cmd.exe\"",
    ] {
        assert!(!text.contains(shell), "the runner names a shell: {shell}");
    }
}

#[test]
fn the_markers_catch_the_ways_to_start_a_process() {
    for code in [
        "let c = Command::new(\"git\");",
        "use std::process::Command;",
        "use std::process::{self, Command};",
        "std::process::Command::new(x)",
        "use std::os::unix::process::CommandExt;",
        "gix::command::prepare(x)",
        "child.spawn().unwrap()",
    ] {
        assert!(spawn_marker(code).is_some(), "{code}");
    }
    for code in [
        "s.spawn(move || 1)",
        "let command = 1;",
        "std::thread::spawn(f)",
    ] {
        assert!(spawn_marker(code).is_none(), "{code}");
    }
}
