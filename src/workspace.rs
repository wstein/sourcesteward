//! Local task checkpoints for full Git worktrees. The task ref is the publication boundary.
use std::collections::BTreeSet;
use std::fs::{self, OpenOptions};
use std::io::ErrorKind;
use std::path::{Path, PathBuf};

use clap::{Args, Subcommand};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

use crate::error::{exit, Error, Result};
use crate::git_runner::{self, Hex40, Op, WorkspaceOp as Git};

const FORMAT: &str = "stw-workspace-v1";
const PLAN_FORMAT: &str = "stw-workspace-plan-v1";
const WORKTREE_PLAN_FORMAT: &str = "stw-worktree-plan-v1";
const TASK_REFS: &str = "refs/stw/tasks/";

#[derive(Subcommand)]
pub(crate) enum WorkspaceCommand {
    /// Save the index and working tree under a local task ref
    Checkpoint(CheckpointArgs),
    /// Show the published checkpoint of a task
    Inspect(TaskArgs),
    /// Preview whether a checkpoint covers the worktree before retirement
    RetirementPlan(TaskArgs),
    /// Plan a detached linked worktree at a local revision
    WorktreePlan(WorktreePlanArgs),
    /// Create a detached linked worktree from a saved plan
    WorktreeApply(WorktreeApplyArgs),
    /// Restore a checkpoint into a new linked worktree
    Resume(ResumeArgs),
}

#[derive(Args)]
pub(crate) struct CheckpointArgs {
    #[arg(long)]
    task: String,
    /// Include this exact non-ignored untracked file, relative to the worktree root
    #[arg(long)]
    include: Vec<String>,
    /// Report the selection without writing Git objects or refs.
    ///
    /// With --json, save the "plan" object to a file outside the source worktree.
    /// Apply it with --apply-plan FILE and the same task and --include paths.
    #[arg(long)]
    plan: bool,
    /// Read the saved JSON plan object and verify it before capturing current contents
    #[arg(long, conflicts_with = "plan", value_name = "FILE")]
    apply_plan: Option<PathBuf>,
    #[command(flatten)]
    output: OutputArgs,
}

#[derive(Args)]
pub(crate) struct TaskArgs {
    #[arg(long)]
    task: String,
    #[command(flatten)]
    output: OutputArgs,
}

#[derive(Args)]
pub(crate) struct WorktreePlanArgs {
    /// Local revision to resolve to a commit
    revision: String,
    /// Path of the new linked worktree
    destination: PathBuf,
    #[command(flatten)]
    output: OutputArgs,
}

#[derive(Args)]
pub(crate) struct WorktreeApplyArgs {
    /// File containing the JSON plan object
    plan: PathBuf,
    #[command(flatten)]
    output: OutputArgs,
}

#[derive(Args)]
pub(crate) struct ResumeArgs {
    /// Full checkpoint commit SHA
    checkpoint: String,
    /// Path of the new linked worktree
    destination: PathBuf,
    #[command(flatten)]
    output: OutputArgs,
}

#[derive(Args)]
struct OutputArgs {
    /// Repository or worktree path (default: discover from the working directory)
    #[arg(long)]
    repo: Option<PathBuf>,
    /// Emit one versioned JSON document
    #[arg(long)]
    json: bool,
}

#[derive(Deserialize, Serialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct CheckpointPlan {
    format: String,
    task: String,
    base: String,
    #[serde(deserialize_with = "Deserialize::deserialize")]
    observed_checkpoint: Option<String>,
    included: BTreeSet<String>,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct WorktreePlan {
    format: String,
    base: String,
    destination: String,
    common_dir: String,
}

#[derive(Deserialize, Serialize)]
struct Record {
    format: String,
    base: String,
    index: String,
    included: BTreeSet<String>,
    omitted: BTreeSet<String>,
}

pub(crate) fn run(command: WorkspaceCommand) -> Result<u8> {
    let (name, opts) = match &command {
        WorkspaceCommand::Checkpoint(a) => ("checkpoint", &a.output),
        WorkspaceCommand::Inspect(a) => ("inspect", &a.output),
        WorkspaceCommand::RetirementPlan(a) => ("retirement-plan", &a.output),
        WorkspaceCommand::WorktreePlan(a) => ("worktree-plan", &a.output),
        WorkspaceCommand::WorktreeApply(a) => ("worktree-apply", &a.output),
        WorkspaceCommand::Resume(a) => ("resume", &a.output),
    };
    let mut report =
        json!({"schema_version": 1, "command": format!("workspace {name}"), "via": "git"});
    let result = (|| {
        let cwd = opts.repo.clone().unwrap_or(std::env::current_dir()?);
        let repo = crate::repo::discover::discover(&cwd)
            .ok_or_else(|| Error::usage("no Git repository found"))?;
        let root = repo
            .workdir()
            .ok_or_else(|| Error::usage("a worktree is required"))?
            .canonicalize()?;
        match &command {
            WorkspaceCommand::Checkpoint(a) => checkpoint(&repo, &root, a, &mut report),
            WorkspaceCommand::Inspect(a) => inspect(&repo, &root, a, &mut report),
            WorkspaceCommand::RetirementPlan(a) => retirement_plan(&repo, &root, a, &mut report),
            WorkspaceCommand::WorktreePlan(a) => worktree_plan(&repo, &root, a, &mut report),
            WorkspaceCommand::WorktreeApply(a) => worktree_apply(&repo, &root, a, &mut report),
            WorkspaceCommand::Resume(a) => resume(&repo, &root, a, &mut report),
        }
    })();
    let code = match result {
        Ok(()) => {
            report["outcome"] = json!("ok");
            exit::OK
        }
        Err(error) => {
            report["outcome"] = json!("refused");
            report["blocker"] = json!(error.to_string());
            match error.kind() {
                crate::error::Kind::Usage => exit::USAGE,
                _ => exit::FAILED,
            }
        }
    };
    if opts.json {
        println!("{report}");
    } else if code != exit::OK {
        eprintln!(
            "stw workspace {name}: {}",
            report["blocker"].as_str().unwrap_or_default()
        );
    } else {
        println!("workspace {name}: {report}");
    }
    Ok(code)
}

fn git(root: &Path, op: Git) -> Result<Vec<u8>> {
    Ok(git_runner::shared().run(root, Op::Workspace(op))?.stdout)
}

fn parse_id(text: &str) -> Result<Hex40> {
    Hex40::parse(text).ok_or_else(|| Error::usage("an ID must be a full lowercase SHA-1"))
}

fn output_id(bytes: Vec<u8>) -> Result<Hex40> {
    parse_id(String::from_utf8(bytes)?.trim())
}

fn task_checkpoint(repo: &gix::Repository, task: &str) -> Result<Option<Hex40>> {
    if !git_runner::valid_task_name(task) {
        return Err(Error::usage(
            "invalid task name: use ASCII letters, digits, underscores, or hyphens",
        ));
    }
    let Some(reference) = repo.try_find_reference(&format!("{TASK_REFS}{task}"))? else {
        return Ok(None);
    };
    let id = reference
        .try_id()
        .ok_or_else(|| Error::repository("task ref must be direct"))?;
    Ok(Some(parse_id(&id.to_string())?))
}

fn paths(root: &Path, ignored: bool) -> Result<BTreeSet<String>> {
    git_paths(root, Git::Untracked { ignored })
}

fn git_paths(root: &Path, op: Git) -> Result<BTreeSet<String>> {
    git(root, op)?
        .split(|b| *b == 0)
        .filter(|p| !p.is_empty())
        .map(|p| Ok(std::str::from_utf8(p)?.to_owned()))
        .collect()
}

fn workspace_config(repo: &gix::Repository) -> Result<()> {
    if repo.state().is_some() {
        return Err(Error::repository("a Git operation is in progress"));
    }
    full_workspace_config(repo)
}

fn full_workspace_config(repo: &gix::Repository) -> Result<()> {
    let config = repo.config_snapshot();
    if config.boolean("core.sparseCheckout").unwrap_or(false) {
        return Err(Error::repository(
            "sparse checkout is outside workspace checkpoint support",
        ));
    }
    if config.boolean("core.splitIndex").unwrap_or(false) {
        return Err(Error::repository(
            "split indexes are outside workspace checkpoint support",
        ));
    }
    for dir in [repo.common_dir(), repo.git_dir()] {
        match fs::read(dir.join("info/attributes")) {
            Ok(bytes) if !bytes.is_empty() => {
                return Err(Error::repository(
                    "info/attributes is outside workspace checkpoint support",
                ))
            }
            Ok(_) => (),
            Err(e) if e.kind() == ErrorKind::NotFound => (),
            Err(e) => return Err(e.into()),
        }
    }
    Ok(())
}

fn check_index_flags(repo: &gix::Repository) -> Result<()> {
    use gix::index::entry::Flags;
    let index_state = repo.index_or_empty()?;
    if index_state.entries().iter().any(|entry| {
        entry
            .flags
            .intersects(Flags::INTENT_TO_ADD | Flags::ASSUME_VALID | Flags::SKIP_WORKTREE)
    }) {
        return Err(Error::repository(
            "intent-to-add and hidden index entries are outside workspace checkpoint support",
        ));
    }

    Ok(())
}

fn check_entries(root: &Path, entries: &[u8], index: bool) -> Result<()> {
    for entry in entries.split(|b| *b == 0).filter(|e| !e.is_empty()) {
        let tab = entry
            .iter()
            .position(|b| *b == b'\t')
            .ok_or_else(|| Error::repository("unexpected Git index or tree record"))?;
        let (meta, name) = (&entry[..tab], &entry[tab + 1..]);
        if meta.starts_with(b"160000 ") {
            return Err(Error::repository(
                "submodules are outside workspace checkpoint support",
            ));
        }
        if index && !meta.ends_with(b" 0") {
            return Err(Error::repository("unmerged index entries"));
        }
        check_path(root, std::str::from_utf8(name)?)?;
    }
    Ok(())
}

fn check_path(root: &Path, name: &str) -> Result<()> {
    let path = Path::new(name);
    if path
        .file_name()
        .is_some_and(|part| part == ".gitattributes")
        || path
            .ancestors()
            .skip(1)
            .any(|dir| root.join(dir).join(".gitattributes").exists())
    {
        return Err(Error::repository(
            "worktree attributes are outside workspace checkpoint support",
        ));
    }
    Ok(())
}

fn commit_tree(root: &Path, tree: Hex40, parents: Vec<Hex40>, message: &[u8]) -> Result<Hex40> {
    let out = git_runner::shared().run_with_stdin(
        root,
        Op::Workspace(Git::CommitTree { tree, parents }),
        Some(message),
    )?;
    output_id(out.stdout)
}

fn checkpoint(
    repo: &gix::Repository,
    root: &Path,
    args: &CheckpointArgs,
    report: &mut Value,
) -> Result<()> {
    let previous = task_checkpoint(repo, &args.task)?;
    let base_id = repo.head_id()?.detach();
    let base = parse_id(&base_id.to_string())?;
    report["base"] = json!(base.as_str());
    report["checkpoint"] = json!(previous.as_ref().map(Hex40::as_str));
    report["published"] = json!(false);
    workspace_config(repo)?;

    check_index_flags(repo)?;

    let index_path = repo.index_path();
    let original = fs::read(&index_path)?;
    check_entries(root, &git(root, Git::IndexEntries)?, true)?;
    check_entries(root, &git(root, Git::TreeEntries(base))?, false)?;
    let untracked = paths(root, false)?;
    let ignored = paths(root, true)?;
    let included = args.include.iter().cloned().collect::<BTreeSet<_>>();
    if !included.is_subset(&untracked) {
        return Err(Error::usage(
            "--include must name exact non-ignored untracked files",
        ));
    }
    for name in &included {
        let kind = root.join(name).symlink_metadata()?.file_type();
        if !kind.is_file() && !kind.is_symlink() {
            return Err(Error::usage("--include must name a file or symbolic link"));
        }
        check_path(root, name)?;
    }
    let omitted = untracked
        .difference(&included)
        .cloned()
        .chain(ignored)
        .collect::<BTreeSet<_>>();
    let mut record = Record {
        format: FORMAT.into(),
        base: base.as_str().into(),
        index: String::new(),
        included,
        omitted,
    };
    report["included"] = json!(record.included);
    report["omitted"] = json!(record.omitted);
    if args.plan || args.apply_plan.is_some() {
        let current = CheckpointPlan {
            format: PLAN_FORMAT.into(),
            task: args.task.clone(),
            base: record.base.clone(),
            observed_checkpoint: previous.as_ref().map(|id| id.as_str().to_owned()),
            included: record.included.clone(),
        };
        if args.plan {
            report["plan"] = json!(current);
            report["next"] = json!("checkpoint");
            return Ok(());
        }
        if let Some(path) = &args.apply_plan {
            let saved: CheckpointPlan = serde_json::from_slice(&fs::read(path)?)
                .map_err(|error| Error::usage(format!("invalid saved checkpoint plan: {error}")))?;
            if saved != current {
                return Err(Error::repository(
                    "saved checkpoint plan no longer matches task, base, ref, or selection",
                ));
            }
        }
    }

    let temp = tempfile::tempdir()?;
    let staged = temp.path().join("staged");
    fs::write(&staged, &original)?;
    let index_tree = output_id(git(root, Git::WriteTree(staged))?)?;
    // A copy would retain stat cache entries. Load a fresh index so Git rereads
    // working-tree content even after an equal-size edit with a matching mtime.
    let index = temp.path().join("capture");
    git(
        root,
        Git::LoadIndex {
            tree: index_tree,
            index: Some(index.clone()),
        },
    )?;
    git(
        root,
        Git::Capture {
            index: index.clone(),
            paths: vec![],
        },
    )?;
    if !record.included.is_empty() {
        git(
            root,
            Git::Capture {
                index: index.clone(),
                paths: record.included.iter().cloned().collect(),
            },
        )?;
    }
    let worktree_tree = output_id(git(root, Git::WriteTree(index))?)?;
    let saved_index = commit_tree(root, index_tree, vec![base], b"STW workspace index\n")?;
    record.index = saved_index.as_str().into();
    let parents = std::iter::once(saved_index).chain(previous).collect();
    let candidate = commit_tree(root, worktree_tree, parents, &serde_json::to_vec(&record)?)?;
    if fs::read(&index_path)? != original || repo.head_id()?.detach() != base_id {
        return Err(Error::repository(
            "source HEAD or index changed during capture; retry the checkpoint",
        ));
    }
    if let Err(error) = git(
        root,
        Git::Publish {
            task: args.task.clone(),
            new: candidate,
            expected: previous,
        },
    ) {
        report["checkpoint"] = json!(task_checkpoint(repo, &args.task)?
            .as_ref()
            .map(Hex40::as_str));
        return Err(error);
    }
    report["checkpoint"] = json!(candidate.as_str());
    report["published"] = json!(true);
    report["next"] = json!("resume");
    Ok(())
}

fn record(root: &Path, checkpoint: Hex40) -> Result<Record> {
    let raw = git(root, Git::ReadCommit(checkpoint))?;
    let text = std::str::from_utf8(&raw)?;
    let (headers, message) = text
        .split_once("\n\n")
        .ok_or_else(|| Error::repository("invalid checkpoint commit"))?;
    let record: Record = serde_json::from_str(message)?;
    if record.format != FORMAT
        || headers
            .lines()
            .find_map(|line| line.strip_prefix("parent "))
            != Some(&record.index)
    {
        return Err(Error::repository("invalid workspace checkpoint metadata"));
    }
    let index = parse_id(&record.index)?;
    let index_raw = git(root, Git::ReadCommit(index))?;
    let index_text = std::str::from_utf8(&index_raw)?;
    if index_text
        .lines()
        .take_while(|line| !line.is_empty())
        .find_map(|line| line.strip_prefix("parent "))
        != Some(&record.base)
    {
        return Err(Error::repository(
            "checkpoint index does not belong to its recorded base",
        ));
    }
    Ok(record)
}

fn describe(id: Hex40, record: &Record, report: &mut Value) {
    report["checkpoint"] = json!(id.as_str());
    report["base"] = json!(record.base);
    report["included"] = json!(record.included);
    report["omitted"] = json!(record.omitted);
}

fn inspect(repo: &gix::Repository, root: &Path, args: &TaskArgs, report: &mut Value) -> Result<()> {
    let id = task_checkpoint(repo, &args.task)?
        .ok_or_else(|| Error::repository("task has no published checkpoint"))?;
    describe(id, &record(root, id)?, report);
    report["published"] = json!(true);
    report["next"] = json!("resume");
    Ok(())
}

fn retirement_plan(
    repo: &gix::Repository,
    root: &Path,
    args: &TaskArgs,
    report: &mut Value,
) -> Result<()> {
    report["ready"] = json!(false);
    let checkpoint = task_checkpoint(repo, &args.task)?
        .ok_or_else(|| Error::repository("task has no published checkpoint"))?;
    let record = record(root, checkpoint)?;
    describe(checkpoint, &record, report);
    // Unlike capture, preview must observe active operations and unmerged entries.
    full_workspace_config(repo)?;
    check_index_flags(repo)?;
    let head = parse_id(&repo.head_id()?.detach().to_string())?;
    let index_commit = parse_id(&record.index)?;
    check_entries(root, &git(root, Git::IndexEntries)?, false)?;
    check_entries(root, &git(root, Git::TreeEntries(checkpoint))?, false)?;
    let index_changes = git_paths(root, Git::IndexChanges(index_commit))?;

    let temp = tempfile::tempdir()?;
    let index = temp.path().join("checkpoint-index");
    git(
        root,
        Git::LoadIndex {
            tree: checkpoint,
            index: Some(index.clone()),
        },
    )?;
    let mut worktree_changes = git_paths(root, Git::WorktreeChanges(index))?;
    // Read untracked paths relative to the real index. The temporary index already
    // compares everything the checkpoint includes, even if untracked in the source.
    worktree_changes.extend(paths(root, false)?.difference(&record.included).cloned());
    let ignored = paths(root, true)?;
    let local_commits = String::from_utf8(git(root, Git::LocalCommits(head))?)?;
    let local_commits = local_commits.lines().collect::<Vec<_>>();
    let operations = operations_in_progress(repo.git_dir())?;

    report["head"] = json!(head.as_str());
    report["index_changes"] = json!(index_changes);
    report["worktree_changes"] = json!(worktree_changes);
    report["ignored"] = json!(ignored);
    report["local_commits"] = json!(local_commits);
    report["operations"] = json!(operations);
    report["ready"] = json!(
        head.as_str() == record.base
            && index_changes.is_empty()
            && worktree_changes.is_empty()
            && ignored.is_empty()
            && local_commits.is_empty()
            && operations.is_empty()
    );
    Ok(())
}

fn operations_in_progress(git_dir: &Path) -> Result<Vec<&'static str>> {
    let mut operations = Vec::new();
    for (marker, label) in [
        ("rebase-merge", "rebase"),
        ("rebase-apply", "rebase-or-am"),
        ("MERGE_HEAD", "merge"),
        ("CHERRY_PICK_HEAD", "cherry-pick"),
        ("REVERT_HEAD", "revert"),
        ("BISECT_START", "bisect"),
        ("sequencer", "sequencer"),
    ] {
        match fs::symlink_metadata(git_dir.join(marker)) {
            Ok(_) => operations.push(label),
            Err(error) if error.kind() == ErrorKind::NotFound => (),
            Err(error) => return Err(error.into()),
        }
    }
    Ok(operations)
}

fn canonical_destination(repo: &gix::Repository, path: &Path) -> Result<PathBuf> {
    let absolute = std::path::absolute(path)?;
    let name = absolute
        .file_name()
        .and_then(|name| name.to_str())
        .ok_or_else(|| Error::usage("destination must have a UTF-8 file name"))?;
    let parent = absolute
        .parent()
        .ok_or_else(|| Error::usage("destination must have a parent"))?
        .canonicalize()?;
    let destination = parent.join(name);
    for directory in [repo.common_dir(), repo.git_dir()] {
        if destination.starts_with(directory.canonicalize()?) {
            return Err(Error::usage(
                "destination must be outside Git administrative directories",
            ));
        }
    }
    if !parent.is_dir() {
        return Err(Error::usage("destination parent must be a directory"));
    }
    Ok(destination)
}

fn require_vacant(destination: &Path) -> Result<()> {
    match destination.symlink_metadata() {
        Ok(_) => Err(Error::repository("destination is occupied")),
        Err(error) if error.kind() == ErrorKind::NotFound => Ok(()),
        Err(error) => Err(error.into()),
    }
}

fn worktree_plan(
    repo: &gix::Repository,
    root: &Path,
    args: &WorktreePlanArgs,
    report: &mut Value,
) -> Result<()> {
    let base = output_id(git(root, Git::ResolveCommit(args.revision.clone()))?)?;
    full_workspace_config(repo)?;
    check_entries(root, &git(root, Git::TreeEntries(base))?, false)?;
    let destination = canonical_destination(repo, &args.destination)?;
    require_vacant(&destination)?;
    let common_dir = repo.common_dir().canonicalize()?;
    let plan = WorktreePlan {
        format: WORKTREE_PLAN_FORMAT.into(),
        base: base.as_str().into(),
        destination: destination
            .to_str()
            .ok_or_else(|| Error::usage("destination must be UTF-8"))?
            .into(),
        common_dir: common_dir
            .to_str()
            .ok_or_else(|| Error::usage("Git common directory must be UTF-8"))?
            .into(),
    };
    report["plan"] = json!(plan);
    report["next"] = json!("worktree-apply");
    Ok(())
}

fn worktree_apply(
    repo: &gix::Repository,
    root: &Path,
    args: &WorktreeApplyArgs,
    report: &mut Value,
) -> Result<()> {
    report["created"] = json!(false);
    let plan: WorktreePlan = serde_json::from_slice(&fs::read(&args.plan)?)
        .map_err(|error| Error::usage(format!("invalid saved worktree plan: {error}")))?;
    if plan.format != WORKTREE_PLAN_FORMAT {
        return Err(Error::usage("invalid saved worktree plan format"));
    }
    if repo.common_dir().canonicalize()?.to_str() != Some(plan.common_dir.as_str()) {
        return Err(Error::repository(
            "saved worktree plan belongs to a different Git repository",
        ));
    }
    let base = parse_id(&plan.base)?;
    let resolved = git(root, Git::ResolveCommit(plan.base)).map_err(|error| {
        Error::repository(format!("pinned base is not a local commit: {error}"))
    })?;
    if output_id(resolved)? != base {
        return Err(Error::repository("pinned base is not a local commit"));
    }
    full_workspace_config(repo)?;
    check_entries(root, &git(root, Git::TreeEntries(base))?, false)?;
    let destination = canonical_destination(repo, Path::new(&plan.destination))?;
    if destination.to_str() != Some(plan.destination.as_str()) {
        return Err(Error::usage("saved worktree destination is not canonical"));
    }
    require_vacant(&destination)?;
    report["base"] = json!(base.as_str());
    report["destination"] = json!(plan.destination);
    // Git accepts an empty directory. Claim it before invoking Git so a competing
    // creator cannot claim the path between validation and worktree add.
    if let Err(error) = fs::create_dir(&destination) {
        if error.kind() == ErrorKind::AlreadyExists {
            return Err(Error::repository("destination is occupied"));
        }
        return Err(error.into());
    }
    git(
        root,
        Git::AddWorktree {
            path: plan.destination,
            base,
            checkout: true,
        },
    )?;
    report["created"] = json!(true);
    report["next"] = json!("continue-work");
    Ok(())
}

fn resume(
    repo: &gix::Repository,
    root: &Path,
    args: &ResumeArgs,
    report: &mut Value,
) -> Result<()> {
    let id = parse_id(&args.checkpoint)?;
    let record = record(root, id)?;
    describe(id, &record, report);
    report["restored"] = json!(false);
    workspace_config(repo)?;
    let base = parse_id(&record.base)?;
    let index = parse_id(&record.index)?;
    for tree in [base, index, id] {
        check_entries(root, &git(root, Git::TreeEntries(tree))?, false)?;
    }
    let destination = canonical_destination(repo, &args.destination)?;
    let name = destination
        .file_name()
        .and_then(|name| name.to_str())
        .ok_or_else(|| Error::usage("destination must have a UTF-8 file name"))?;
    let path = destination
        .to_str()
        .ok_or_else(|| Error::usage("destination must be UTF-8"))?;
    let marker = destination.with_file_name(format!("{name}.stw-handoff-incomplete"));
    report["destination"] = json!(path);
    report["marker"] = json!(marker);
    if marker.symlink_metadata().is_ok() {
        report["incomplete"] = json!(true);
        return Err(Error::repository("a previous resume is incomplete"));
    }
    require_vacant(&destination)?;
    let pending = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&marker)?;
    report["incomplete"] = json!(true);
    serde_json::to_writer(
        pending,
        &json!({"checkpoint": id.as_str(), "destination": path}),
    )?;
    if let Err(error) = fs::create_dir(&destination) {
        fs::remove_file(&marker)?;
        report["incomplete"] = json!(false);
        return Err(error.into());
    }
    git(
        root,
        Git::AddWorktree {
            path: path.into(),
            base,
            checkout: false,
        },
    )?;
    let temp = tempfile::tempdir()?;
    git(
        &destination,
        Git::Materialize {
            index: temp.path().join("index"),
            checkpoint: id,
        },
    )?;
    git(
        &destination,
        Git::LoadIndex {
            tree: index,
            index: None,
        },
    )?;
    fs::remove_file(&marker)?;
    report["restored"] = json!(true);
    report["incomplete"] = json!(false);
    report["next"] = json!("continue-work");
    Ok(())
}
