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

/// Items of `std::process` that do not start a process.
const HARMLESS: &[&str] = &["ExitCode", "id", "exit"];

/// The code of `text` with comments removed and the content of string literals blanked, so that neither can
/// hide or fake a match. Line structure is not kept.
fn code_only(text: &str) -> String {
    let c: Vec<char> = text.chars().collect();
    let mut out = String::new();
    let mut i = 0;
    while i < c.len() {
        match c[i] {
            '/' if c.get(i + 1) == Some(&'/') => {
                while i < c.len() && c[i] != '\n' {
                    i += 1;
                }
            }
            '/' if c.get(i + 1) == Some(&'*') => {
                i += 2;
                while i + 1 < c.len() && !(c[i] == '*' && c[i + 1] == '/') {
                    i += 1;
                }
                i += 2;
                out.push(' ');
            }
            'r' if matches!(c.get(i + 1), Some('"') | Some('#')) && !prev_is_ident(&c, i) => {
                // raw string r"..." or r#"..."#
                let mut j = i + 1;
                let mut hashes = 0;
                while c.get(j) == Some(&'#') {
                    hashes += 1;
                    j += 1;
                }
                if c.get(j) != Some(&'"') {
                    out.push('r');
                    i += 1;
                    continue;
                }
                j += 1;
                loop {
                    if j >= c.len() {
                        break;
                    }
                    if c[j] == '"' && (0..hashes).all(|k| c.get(j + 1 + k) == Some(&'#')) {
                        j += 1 + hashes;
                        break;
                    }
                    j += 1;
                }
                out.push_str("\"\"");
                i = j;
            }
            '"' => {
                i += 1;
                while i < c.len() && c[i] != '"' {
                    if c[i] == '\\' {
                        i += 1;
                    }
                    i += 1;
                }
                i += 1;
                out.push_str("\"\"");
            }
            ch => {
                out.push(ch);
                i += 1;
            }
        }
    }
    out
}

fn prev_is_ident(c: &[char], i: usize) -> bool {
    i > 0 && (c[i - 1].is_alphanumeric() || c[i - 1] == '_')
}

fn is_ident(ch: char) -> bool {
    ch.is_alphanumeric() || ch == '_'
}

/// What follows a `process` path segment: only `ExitCode`, `id` and `exit` (alone or in a brace list) are fine.
fn process_tail_is_harmless(after: &str) -> bool {
    let after = after.trim_start();
    let Some(rest) = after.strip_prefix("::") else {
        return false; // `process` used as a module on its own: `use std::process;`, `process as p`, `{process, ..}`
    };
    let rest = rest.trim_start();
    if let Some(list) = rest.strip_prefix('{') {
        let inner = list.split('}').next().unwrap_or("");
        return inner
            .split(',')
            .map(str::trim)
            .filter(|i| !i.is_empty())
            .all(|i| HARMLESS.contains(&i));
    }
    let ident: String = rest.chars().take_while(|ch| is_ident(*ch)).collect();
    // `process::exit(..)` / `process::id()` / `process::ExitCode`, but not `ExitCode as X` nor a longer path
    HARMLESS.contains(&ident.as_str()) && !rest[ident.len()..].trim_start().starts_with("as ")
}

/// Why `text` (the content of a source file) may start a process, or None. Finds `Command::new` and
/// friends, every `process` path or import other than `ExitCode`/`id`/`exit` (also across lines, in brace
/// lists and behind `as` aliases), `CommandExt`, `gix::command`, `posix_spawn`, `fork`, `exec*` and `system`.
pub fn spawn_marker(text: &str) -> Option<String> {
    let code = code_only(text);
    for m in [
        "Command::new",
        "CommandExt",
        "gix::command",
        "gix_command",
        "posix_spawn",
        "libc::fork",
        "libc::vfork",
        "libc::exec",
        "libc::system",
        "libc::popen",
        "nix::unistd",
        ".spawn()",
    ] {
        if code.contains(m) {
            return Some(m.to_string());
        }
    }
    // statements starting with `use`, to find `process` inside brace lists
    let mut use_ranges: Vec<(usize, usize)> = vec![];
    let mut from = 0;
    while let Some(p) = code[from..].find("use ") {
        let start = from + p;
        let boundary = start == 0 || !is_ident(code[..start].chars().next_back().unwrap());
        let end = code[start..].find(';').map_or(code.len(), |e| start + e);
        if boundary {
            use_ranges.push((start, end));
        }
        from = end.max(start + 4);
    }
    let mut i = 0;
    while let Some(p) = code[i..].find("process") {
        let at = i + p;
        let end = at + "process".len();
        i = end;
        let before = code[..at].chars().next_back();
        let after_ch = code[end..].chars().next();
        if before.is_some_and(is_ident) || after_ch.is_some_and(is_ident) {
            continue; // part of a longer word
        }
        let prev_nonspace = code[..at].trim_end().chars().next_back();
        let in_use = use_ranges.iter().any(|(s, e)| at >= *s && at < *e);
        if (prev_nonspace == Some(':') || in_use) && !process_tail_is_harmless(&code[end..]) {
            return Some("process".to_string());
        }
    }
    None
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
        if let Some(marker) = spawn_marker(&text) {
            if is_runner {
                runner_spawns = true;
            } else {
                panic!(
                    "{}: may start a process ({marker}); only {RUNNER} may",
                    f.display()
                );
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
        "use std::process::{\n    Command,\n};",
        "use std::process::{\n    ExitCode,\n    Command,\n};",
        "use std::process as p;\nfn f() { p::Command::new(\"x\"); }",
        "use std::process::Command as C;",
        "use std::process;\nfn f() { process::Command::new(\"x\"); }",
        "use std::{fs, process};",
        "use std::{\n    fs,\n    process::{self, ExitCode},\n};",
        "std::process::Command::new(x)",
        "tokio::process::Command::new(x)",
        "use std::os::unix::process::CommandExt;",
        "gix::command::prepare(x)",
        "child.spawn().unwrap()",
        "unsafe { libc::posix_spawn(a) }",
        "unsafe { libc::fork() }",
        "unsafe { libc::execvp(a, b) }",
        "std :: process :: Command :: new(x)",
        "std::process\n    ::Command::new(x)",
        "let s = \"//\"; std::process::Command::new(x);",
    ] {
        assert!(spawn_marker(code).is_some(), "{code}");
    }
    for code in [
        "s.spawn(move || 1)",
        "let command = 1;",
        "std::thread::spawn(f)",
        "fn main() -> std::process::ExitCode { std::process::ExitCode::from(1) }",
        "use std::process::ExitCode;",
        "use std::process::{ExitCode, id};",
        "let n = std::process::id();",
        "std::process::exit(2);",
        "let process = 3; let _ = process + 1;",
        "// std::process::Command::new(\"x\")",
        "/* use std::process::Command; */ let a = 1;",
        "let t = \"std::process::Command::new\";",
        "let t = r#\"Command::new(\"x\")\"#;",
        "/// the process exits\nfn f() {}",
        "let processed = 1;",
    ] {
        assert!(
            spawn_marker(code).is_none(),
            "{code}: {:?}",
            spawn_marker(code)
        );
    }
}
