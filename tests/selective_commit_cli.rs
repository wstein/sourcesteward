use std::process::Command;
use std::{
    fs,
    path::{Path, PathBuf},
};
use stw::selective_commit::{apply, plan, Plan, Source};

struct Fixture {
    temp: tempfile::TempDir,
    head: String,
}

impl Fixture {
    fn new() -> Self {
        let mut fixture = Self {
            temp: tempfile::tempdir().unwrap(),
            head: String::new(),
        };
        fixture.git(&["init", "-q", "-b", "main"]);
        fixture.git(&["config", "user.name", "Example"]);
        fixture.git(&["config", "user.email", "test@example.test"]);
        fixture.git(&["config", "commit.gpgSign", "false"]);
        fixture.git(&["config", "maintenance.auto", "false"]);
        fs::write(fixture.root().join("selected"), "original\n").unwrap();
        fs::write(fixture.root().join("other"), "other original\n").unwrap();
        fixture.git(&["add", "."]);
        fixture.git(&["commit", "-qm", "initial"]);
        fixture.head = fixture.git(&["rev-parse", "HEAD"]);
        fs::write(fixture.root().join("selected"), "staged\n").unwrap();
        fs::write(fixture.root().join("other"), "other staged\n").unwrap();
        fixture.git(&["add", "."]);
        fs::write(fixture.root().join("selected"), "worktree\n").unwrap();
        fs::write(fixture.root().join("other"), "other worktree\n").unwrap();
        fixture
    }

    fn root(&self) -> &Path {
        self.temp.path()
    }

    fn git(&self, args: &[&str]) -> String {
        let result = Command::new("git")
            .current_dir(self.root())
            .args(args)
            .env("GIT_OPTIONAL_LOCKS", "0")
            .output()
            .unwrap();
        assert!(
            result.status.success(),
            "{args:?}: {}",
            String::from_utf8_lossy(&result.stderr)
        );
        String::from_utf8(result.stdout).unwrap()
    }

    fn plan(&self, source: Source) -> Plan {
        plan(self.root(), source, &[PathBuf::from("selected")]).unwrap()
    }

    fn index(&self) -> Vec<u8> {
        fs::read(self.root().join(".git/index")).unwrap()
    }

    fn check_other(&self) {
        assert_eq!(self.git(&["show", "HEAD:other"]), "other original\n");
        assert_eq!(self.git(&["show", ":other"]), "other staged\n");
        assert_eq!(
            fs::read_to_string(self.root().join("other")).unwrap(),
            "other worktree\n"
        );
        assert_eq!(self.git(&["diff", "--cached", "--name-only"]), "other\n");
    }
}

#[test]
fn index_source_preserves_unstaged_edits_and_index_bytes() {
    let fixture = Fixture::new();
    let index = fixture.index();
    let plan = fixture.plan(Source::Index);
    assert_eq!(plan.keep_staged, ["other"]);
    assert_ne!(plan.selected[0].index_blob, plan.selected[0].worktree_blob);
    assert_eq!(plan.selected[0].selected_blob, plan.selected[0].index_blob);
    let saved: Plan = serde_json::from_slice(&serde_json::to_vec(&plan).unwrap()).unwrap();
    let result = apply(fixture.root(), &saved, "selected").unwrap();
    assert_eq!(result.source, Source::Index);
    assert_eq!(fixture.git(&["show", "HEAD:selected"]), "staged\n");
    assert_eq!(fixture.git(&["rev-parse", "HEAD^"]), fixture.head);
    assert_eq!(
        fs::read_to_string(fixture.root().join("selected")).unwrap(),
        "worktree\n"
    );
    assert_eq!(fixture.index(), index);
    fixture.check_other();
}

#[test]
fn worktree_source_updates_only_selected_index_entry() {
    let fixture = Fixture::new();
    // Install a tree cache so a subsequent write-tree would be wrong if the cache survived.
    fixture.git(&["write-tree"]);
    let plan = fixture.plan(Source::Worktree);
    assert_eq!(
        plan.selected[0].selected_blob,
        plan.selected[0].worktree_blob
    );
    apply(fixture.root(), &plan, "selected").unwrap();
    assert_eq!(fixture.git(&["show", "HEAD:selected"]), "worktree\n");
    assert_eq!(fixture.git(&["show", ":selected"]), "worktree\n");
    let index_tree = fixture.git(&["write-tree"]);
    assert_eq!(
        fixture.git(&["show", &format!("{}:selected", index_tree.trim())]),
        "worktree\n"
    );
    assert_eq!(
        fixture.git(&["show", &format!("{}:other", index_tree.trim())]),
        "other staged\n"
    );
    fixture.check_other();
}

#[test]
fn changed_inputs_refuse_without_mutation() {
    for change in ["worktree", "index", "branch"] {
        let fixture = Fixture::new();
        let saved = fixture.plan(Source::Worktree);
        match change {
            "worktree" => fs::write(fixture.root().join("selected"), "later\n").unwrap(),
            "index" => {
                fixture.git(&["add", "other"]);
            }
            "branch" => {
                fixture.git(&["switch", "-qc", "elsewhere"]);
            }
            _ => unreachable!(),
        }
        let index = fixture.index();
        let selected = fs::read(fixture.root().join("selected")).unwrap();
        let error = apply(fixture.root(), &saved, "selective").unwrap_err();
        assert!(error.to_string().contains("stale preview"), "{error}");
        assert_eq!(fixture.git(&["rev-parse", "HEAD"]), fixture.head);
        assert_eq!(fixture.index(), index);
        assert_eq!(fs::read(fixture.root().join("selected")).unwrap(), selected);
    }
}

#[test]
fn index_lock_prevents_apply() {
    let fixture = Fixture::new();
    let saved = fixture.plan(Source::Index);
    let lock = fixture.root().join(".git/index.lock");
    fs::write(&lock, "other writer").unwrap();
    let error = apply(fixture.root(), &saved, "selective").unwrap_err();
    assert!(
        error.to_string().contains("could not lock the index"),
        "{error}"
    );
    assert_eq!(fs::read_to_string(lock).unwrap(), "other writer");
    assert_eq!(fixture.git(&["rev-parse", "HEAD"]), fixture.head);
}

#[test]
fn blocked_ref_publication_keeps_original_index_and_worktree() {
    let fixture = Fixture::new();
    let saved = fixture.plan(Source::Worktree);
    let index = fixture.index();
    let ref_lock = fixture.root().join(".git/refs/heads/main.lock");
    fs::write(&ref_lock, "other ref writer").unwrap();
    assert!(apply(fixture.root(), &saved, "selective").is_err());
    assert_eq!(fixture.git(&["rev-parse", "HEAD"]), fixture.head);
    assert_eq!(fixture.index(), index);
    assert_eq!(
        fs::read(fixture.root().join("selected")).unwrap(),
        b"worktree\n"
    );
    assert_eq!(fs::read(ref_lock).unwrap(), b"other ref writer");
    assert!(!fixture.root().join(".git/index.lock").exists());
}

#[cfg(unix)]
#[test]
fn swapped_symlink_is_not_read_or_committed() {
    let fixture = Fixture::new();
    let saved = fixture.plan(Source::Worktree);
    let index = fixture.index();
    let selected = fixture.root().join("selected");
    fs::remove_file(&selected).unwrap();
    std::os::unix::fs::symlink(fixture.root().join("other"), &selected).unwrap();
    let error = apply(fixture.root(), &saved, "selective").unwrap_err();
    assert!(error.to_string().contains("stale preview"), "{error}");
    assert_eq!(fixture.git(&["rev-parse", "HEAD"]), fixture.head);
    assert_eq!(fixture.index(), index);
    assert!(selected.symlink_metadata().unwrap().is_symlink());
}

#[cfg(unix)]
#[test]
fn worktree_source_uses_the_index_mode() {
    use std::os::unix::fs::PermissionsExt;
    let fixture = Fixture::new();
    fixture.git(&["update-index", "--chmod=+x", "selected"]);
    let file = fixture.root().join("selected");
    fs::set_permissions(&file, fs::Permissions::from_mode(0o644)).unwrap();
    let saved = fixture.plan(Source::Worktree);
    assert_eq!(saved.selected[0].mode, "100755");
    apply(fixture.root(), &saved, "selective").unwrap();
    assert!(fixture
        .git(&["ls-tree", "HEAD", "selected"])
        .starts_with("100755 "));
    assert_eq!(file.metadata().unwrap().permissions().mode() & 0o777, 0o644);
}

#[test]
fn hidden_index_entry_is_refused() {
    let fixture = Fixture::new();
    fixture.git(&["update-index", "--assume-unchanged", "selected"]);
    let error = plan(
        fixture.root(),
        Source::Worktree,
        &[PathBuf::from("selected")],
    )
    .unwrap_err();
    assert!(
        error.to_string().contains("hidden or intent-to-add"),
        "{error}"
    );
}

#[test]
fn a_file_directory_replacement_must_not_capture_unselected_deletions() {
    let fixture = Fixture::new();
    let root = fixture.root();
    fs::create_dir(root.join("dir")).unwrap();
    fs::write(root.join("dir/tracked"), "tracked").unwrap();
    fixture.git(&["add", "dir"]);
    fixture.git(&["commit", "-qm", "directory", "--", "dir"]);
    fs::remove_dir_all(root.join("dir")).unwrap();
    fs::write(root.join("dir"), "file").unwrap();
    fixture.git(&["add", "dir"]);
    let error = plan(root, Source::Index, &[PathBuf::from("dir")]).unwrap_err();
    assert!(
        error
            .to_string()
            .contains("would replace other paths in HEAD"),
        "{error}"
    );
}
