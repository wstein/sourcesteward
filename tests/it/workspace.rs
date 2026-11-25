use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::os::unix::process::ExitStatusExt;
use std::path::Path;
use std::process::{Command, Output, Stdio};

use serde_json::Value;

use crate::common::Fx;

fn git(repo: &Path, args: &[&str]) -> String {
    let result = Command::new("git")
        .current_dir(repo)
        .env("GIT_OPTIONAL_LOCKS", "0")
        .args(args)
        .output()
        .unwrap();
    assert!(result.status.success(), "{:?}: {:?}", args, result);
    String::from_utf8(result.stdout).unwrap()
}

fn fixture() -> Fx {
    let fx = Fx::new();
    for (path, bytes) in [
        ("mixed", "head\n"),
        ("staged-delete", "staged\n"),
        ("unstaged-delete", "unstaged\n"),
        (".gitignore", "ignored\n"),
    ] {
        fs::write(fx.path.join(path), bytes).unwrap();
    }
    git(&fx.path, &["add", "-A"]);
    git(&fx.path, &["commit", "-m", "fixture"]);
    fs::write(fx.path.join("mixed"), "index\n").unwrap();
    fs::write(fx.path.join("staged-new"), "new\n").unwrap();
    git(&fx.path, &["add", "--", "mixed", "staged-new"]);
    git(&fx.path, &["rm", "--", "staged-delete"]);
    for (path, bytes) in [
        ("mixed", "worktree\n"),
        ("selected file", "selected\n"),
        ("ignored", "ignored\n"),
        ("omitted", "omitted\n"),
    ] {
        fs::write(fx.path.join(path), bytes).unwrap();
    }
    fs::remove_file(fx.path.join("unstaged-delete")).unwrap();
    fx
}

fn command(repo: &Path, args: &[&str]) -> Command {
    let mut cmd = Command::new(env!("CARGO_BIN_EXE_stw"));
    cmd.current_dir(repo).stdin(Stdio::null()).arg("workspace");
    cmd.args(args).arg("--json");
    cmd
}

fn document(result: Output, exit: i32) -> Value {
    assert_eq!(result.status.code(), Some(exit), "{result:?}");
    let doc: Value = serde_json::from_slice(&result.stdout).unwrap();
    assert_eq!(doc["schema_version"], 1);
    doc
}

fn run(repo: &Path, args: &[&str], exit: i32) -> Value {
    document(command(repo, args).output().unwrap(), exit)
}

fn save(repo: &Path) -> String {
    let doc = run(
        repo,
        &["checkpoint", "--task", "demo", "--include", "selected file"],
        0,
    );
    assert_eq!(doc["published"], true);
    doc["checkpoint"].as_str().unwrap().to_owned()
}

fn apply(repo: &Path, plan: &Path, include: &str, exit: i32) -> Value {
    run(
        repo,
        &[
            "checkpoint",
            "--task",
            "demo",
            "--include",
            include,
            "--apply-plan",
            plan.to_str().unwrap(),
        ],
        exit,
    )
}

#[test]
fn a_saved_plan_refuses_changed_selection_base_or_ref_before_writing_objects() {
    for changed in ["selection", "base", "ref"] {
        let fx = fixture();
        let previous = save(&fx.path);
        let report = run(
            &fx.path,
            &[
                "checkpoint",
                "--task",
                "demo",
                "--include",
                "selected file",
                "--plan",
            ],
            0,
        );
        let path = fx.path.with_file_name("plan.json");
        fs::write(&path, serde_json::to_vec(&report["plan"]).unwrap()).unwrap();

        let mut include = "selected file";
        let mut expected = previous;
        match changed {
            "selection" => include = "omitted",
            "base" => {
                git(&fx.path, &["commit", "-m", "change base"]);
            }
            "ref" => {
                fs::write(fx.path.join("mixed"), "winner\n").unwrap();
                let winner = save(&fx.path);
                assert_ne!(winner, expected);
                expected = winner;
            }
            _ => unreachable!(),
        }
        let objects_before = git(&fx.path, &["count-objects", "-v"]);
        let refused = apply(&fx.path, &path, include, 1);
        assert_eq!(refused["published"], false, "{changed}");
        assert_eq!(refused["checkpoint"], expected, "{changed}");
        assert_eq!(git(&fx.path, &["count-objects", "-v"]), objects_before);
        let inspect = run(&fx.path, &["inspect", "--task", "demo"], 0);
        assert_eq!(inspect["checkpoint"], expected, "{changed}");
    }
}

#[test]
fn plan_and_resume_preserve_the_selected_workspace_state() {
    let fx = fixture();
    let index_path = fx.path.join(".git/index");
    let before = fs::read(&index_path).unwrap();
    let objects_before = git(&fx.path, &["count-objects", "-v"]);
    let plan = run(
        &fx.path,
        &[
            "checkpoint",
            "--task",
            "demo",
            "--include",
            "selected file",
            "--plan",
        ],
        0,
    );
    assert_eq!(plan["published"], false);
    assert_eq!(plan["omitted"], serde_json::json!(["ignored", "omitted"]));
    assert_eq!(git(&fx.path, &["count-objects", "-v"]), objects_before);
    assert!(!fx.path.join(".git/refs/stw/tasks/demo").exists());

    let path = fx.path.with_file_name("plan.json");
    fs::write(&path, serde_json::to_vec(&plan["plan"]).unwrap()).unwrap();
    let applied = apply(&fx.path, &path, "selected file", 0);
    assert_eq!(applied["published"], true);
    let id = applied["checkpoint"].as_str().unwrap().to_owned();
    assert_eq!(fs::read(index_path).unwrap(), before);
    assert_eq!(
        fs::read_to_string(fx.path.join("mixed")).unwrap(),
        "worktree\n"
    );
    let inspect = run(&fx.path, &["inspect", "--task", "demo"], 0);
    assert_eq!(inspect["checkpoint"], id);

    let dest = fx.path.with_file_name("resumed");
    let dest_str = dest.to_str().unwrap();
    assert_eq!(
        run(&fx.path, &["resume", &id, dest_str], 0)["restored"],
        true
    );
    for args in [
        &["rev-parse", "HEAD"][..],
        &["ls-files", "--stage", "-z"],
        &["diff", "--cached", "--binary"],
        &["diff", "--binary"],
    ] {
        assert_eq!(git(&fx.path, args), git(&dest, args), "{args:?}");
    }
    assert_eq!(
        git(&dest, &["ls-files", "--others", "--exclude-standard", "-z"]),
        "selected file\0"
    );
    assert_eq!(
        fs::read_to_string(dest.join("selected file")).unwrap(),
        "selected\n"
    );
    for path in ["ignored", "omitted", "staged-delete", "unstaged-delete"] {
        assert!(!dest.join(path).exists());
    }
    let refusal = run(&fx.path, &["resume", &id, dest_str], 1);
    assert_eq!(refusal["blocker"], "destination is occupied");
}

#[test]
fn publication_is_the_boundary_after_process_death() {
    let fx = fixture();
    let original = save(&fx.path);
    fs::write(fx.path.join("mixed"), "later\n").unwrap();
    let shim = fx.path.with_file_name("bin");
    fs::create_dir(&shim).unwrap();
    let script = shim.join("git");
    // Stop the STW parent just before or after real Git publishes the task ref.
    fs::write(
        &script,
        r#"#!/bin/sh
for arg do
    if [ "$arg" = update-ref ]; then
        if [ "$STW_TEST_POINT" = after ]; then "$STW_REAL_GIT" "$@" || exit; fi
        kill -KILL "$PPID"
        exit 0
    fi
done
exec "$STW_REAL_GIT" "$@"
"#,
    )
    .unwrap();
    fs::set_permissions(&script, fs::Permissions::from_mode(0o700)).unwrap();
    let old_path = std::env::var_os("PATH").unwrap();
    let real = std::env::split_paths(&old_path)
        .filter(|dir| dir.is_absolute())
        .map(|dir| dir.join("git"))
        .find(|path| path.is_file())
        .unwrap();
    let path = std::env::join_paths(std::iter::once(shim).chain(std::env::split_paths(&old_path)))
        .unwrap();

    for (point, expected) in [("before", "worktree\n"), ("after", "later\n")] {
        // Pass test controls in the wrapper itself. The runner intentionally scrubs arbitrary variables.
        let template = fs::read_to_string(&script).unwrap();
        let configured = template
            .replace("$STW_TEST_POINT", point)
            .replace("$STW_REAL_GIT", real.to_str().unwrap());
        let wrapped = fx.path.with_file_name(point);
        fs::create_dir(&wrapped).unwrap();
        let executable = wrapped.join("git");
        fs::write(&executable, configured).unwrap();
        fs::set_permissions(&executable, fs::Permissions::from_mode(0o700)).unwrap();
        let path =
            std::env::join_paths(std::iter::once(wrapped).chain(std::env::split_paths(&path)))
                .unwrap();
        let killed = command(&fx.path, &["checkpoint", "--task", "demo"])
            .env("PATH", path)
            .output()
            .unwrap();
        assert_eq!(killed.status.signal(), Some(9), "{killed:?}");
        let inspect = run(&fx.path, &["inspect", "--task", "demo"], 0);
        let current = inspect["checkpoint"].as_str().unwrap();
        assert_eq!(current == original, point == "before");
        let dest = fx.path.with_file_name(format!("restored-{point}"));
        run(&fx.path, &["resume", current, dest.to_str().unwrap()], 0);
        assert_eq!(fs::read_to_string(dest.join("mixed")).unwrap(), expected);
    }
    let winner = run(&fx.path, &["inspect", "--task", "demo"], 0)["checkpoint"]
        .as_str()
        .unwrap()
        .to_owned();
    let task_ref = "refs/stw/tasks/demo";
    git(&fx.path, &["update-ref", task_ref, &original, &winner]);
    let plan = run(&fx.path, &["checkpoint", "--task", "demo", "--plan"], 0);
    let plan_file = fx.path.with_file_name("race-plan.json");
    fs::write(&plan_file, serde_json::to_vec(&plan["plan"]).unwrap()).unwrap();
    // Simulate another writer at the boundary after plan validation and capture.
    fs::write(
        &script,
        format!(
            r#"#!/bin/sh
for arg do
    if [ "$arg" = update-ref ]; then
        "{real}" update-ref "{task_ref}" "{winner}" "{original}" || exit
        break
    fi
done
exec "{real}" "$@"
"#,
            real = real.display()
        ),
    )
    .unwrap();
    let refused = document(
        command(
            &fx.path,
            &[
                "checkpoint",
                "--task",
                "demo",
                "--apply-plan",
                plan_file.to_str().unwrap(),
            ],
        )
        .env("PATH", path)
        .output()
        .unwrap(),
        1,
    );
    assert_eq!(refused["published"], false);
    assert_eq!(refused["checkpoint"], winner);
    assert_eq!(git(&fx.path, &["rev-parse", task_ref]).trim(), winner);
}

#[test]
fn refused_and_incomplete_operations_are_non_destructive() {
    let fx = fixture();
    let id = save(&fx.path);
    git(&fx.path, &["update-index", "--assume-unchanged", "mixed"]);
    let refused = run(&fx.path, &["checkpoint", "--task", "demo"], 1);
    assert_eq!(refused["published"], false);
    assert_eq!(refused["checkpoint"], id);
    let dest = fx.path.with_file_name("incomplete");
    let marker = dest.with_extension("stw-handoff-incomplete");
    fs::write(&marker, "marker left by an interrupted process").unwrap();
    let retry = run(&fx.path, &["resume", &id, dest.to_str().unwrap()], 1);
    assert_eq!(retry["incomplete"], true);
    assert_eq!(retry["restored"], false);
    assert!(!dest.exists());
    assert_eq!(
        fs::read_to_string(marker).unwrap(),
        "marker left by an interrupted process"
    );
    assert_eq!(
        run(&fx.path, &["inspect", "--task", "demo"], 0)["checkpoint"],
        id
    );
}

#[test]
fn a_linked_worktree_can_checkpoint_and_handoff_again() {
    let fx = fixture();
    let first = save(&fx.path);
    let source = fx.path.with_file_name("linked-source");
    run(&fx.path, &["resume", &first, source.to_str().unwrap()], 0);
    fs::write(source.join("mixed"), "linked edit\n").unwrap();
    let next = save(&source);
    assert_ne!(first, next);
    assert_eq!(
        run(&fx.path, &["inspect", "--task", "demo"], 0)["checkpoint"],
        next
    );
    let destination = fx.path.with_file_name("linked-destination");
    run(
        &source,
        &["resume", &next, destination.to_str().unwrap()],
        0,
    );
    assert_eq!(git(&destination, &["show", ":mixed"]), "index\n");
    assert_eq!(
        fs::read_to_string(destination.join("mixed")).unwrap(),
        "linked edit\n"
    );
    assert_eq!(
        fs::read_to_string(fx.path.join("mixed")).unwrap(),
        "worktree\n"
    );
}

#[test]
fn capture_rereads_content_when_the_index_stat_cache_matches() {
    let fx = fixture();
    // With ctime ignored, preserve the size and staged mtime exactly. A copied
    // index can then make Git believe the new content is already staged.
    git(&fx.path, &["config", "--local", "core.trustctime", "false"]);
    let mixed = fx.path.join("mixed");
    fs::write(&mixed, "index\n").unwrap();
    let old = std::time::SystemTime::UNIX_EPOCH + std::time::Duration::from_secs(1_700_000_000);
    let file = fs::File::options().write(true).open(&mixed).unwrap();
    let times = fs::FileTimes::new().set_modified(old);
    file.set_times(times).unwrap();
    git(&fx.path, &["add", "--", "mixed"]);
    fs::write(&mixed, "later\n").unwrap();
    file.set_times(times).unwrap();
    let id = save(&fx.path);
    let dest = fx.path.with_file_name("same-stat-resume");
    run(&fx.path, &["resume", &id, dest.to_str().unwrap()], 0);
    assert_eq!(git(&dest, &["show", ":mixed"]), "index\n");
    assert_eq!(fs::read_to_string(dest.join("mixed")).unwrap(), "later\n");
}
