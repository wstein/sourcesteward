//! The product code must not start `git`, `gh`, `ssh` or any other process.
use std::path::{Path, PathBuf};

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

#[test]
fn src_never_spawns_a_process() {
    let mut files = vec![];
    rust_files(
        Path::new(concat!(env!("CARGO_MANIFEST_DIR"), "/src")),
        &mut files,
    );
    assert!(files.len() >= 10, "scan found only {} files", files.len());
    // the apply/restore module is part of the scan (a rename or move must not hide it)
    for must in ["tidy_apply.rs", "tidy.rs", "worktrees.rs"] {
        assert!(
            files
                .iter()
                .any(|f| f.file_name().is_some_and(|n| n == must)),
            "{must} is not scanned"
        );
    }
    for f in files {
        let text = std::fs::read_to_string(&f).unwrap();
        for (n, line) in text.lines().enumerate() {
            let code = line.split("//").next().unwrap();
            if code.contains("Command::new")
                || code.contains("process::Command")
                || (code.contains("process::{") && code.contains("Command"))
            {
                panic!("{}:{}: starts a process: {line}", f.display(), n + 1);
            }
        }
    }
}
