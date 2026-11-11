//! Command line (clap): `stw status | tidy plan | tidy apply | tidy restore | sync`, the shared options, workspace selection and
//! the exit code of a run.
use std::path::{Path, PathBuf};
use std::process::ExitCode;

use clap::{Args, Parser, Subcommand};
use serde_json::Value;

use crate::error::{exit, Error, Kind, Result};
use crate::github::GithubState;
use crate::output::{self, print_json};
use crate::repo::{self, RepoRef};
use crate::{add, carryover, dev, git_runner, status, sync, tidy, tidy_apply};

const EXIT_CODES: &str = "\
Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync --apply` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), or `tidy restore` left an
     entry not restored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with \"schema_version\" (see README.md).";

#[derive(Parser)]
#[command(
    name = "stw",
    version,
    about = "SourceSteward: where am I, what can go, what is behind, across every repository of a workspace",
    after_help = EXIT_CODES,
    subcommand_required = true,
    arg_required_else_help = true
)]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// Where am I in every repository (read-only)
    #[command(after_help = EXIT_CODES)]
    Status(StatusArgs),
    /// Branch cleanup: `plan` shows it, `apply` carries it out, `restore` undoes it
    #[command(subcommand_required = true, arg_required_else_help = true, after_help = EXIT_CODES)]
    Tidy {
        #[command(subcommand)]
        command: TidyCommand,
    },
    /// Fast-forward every repository to its upstream (dry run unless --apply)
    #[command(after_help = EXIT_CODES)]
    Sync(SyncArgs),
    /// Bind an existing GitHub repository as a project (git submodule) of the workspace; one local commit
    #[command(after_help = EXIT_CODES)]
    Add(AddArgs),
    /// Review helpers (read-only)
    #[command(subcommand_required = true, arg_required_else_help = true, after_help = EXIT_CODES)]
    Review {
        #[command(subcommand)]
        command: ReviewCommand,
    },
    #[command(hide = true)]
    Dev {
        #[command(subcommand)]
        command: DevCommand,
    },
}

#[derive(Subcommand)]
enum TidyCommand {
    /// Which local branches and worktree registrations can be cleaned up (read-only, deletes nothing)
    #[command(after_help = EXIT_CODES)]
    Plan(TidyArgs),
    /// Carry out the plan: delete the local branches it marks `delete`, manifest first; undo with `restore`
    #[command(after_help = EXIT_CODES)]
    Apply(ApplyArgs),
    /// Re-create branches deleted by `apply` from the manifest (never overwrites)
    #[command(after_help = EXIT_CODES)]
    Restore(RestoreArgs),
}

#[derive(Subcommand)]
enum ReviewCommand {
    /// May the review of a pull request head be carried over to a rewritten head? Prints the `Carried-over:` line for a yes; posts nothing
    #[command(after_help = CARRY_OVER_HELP)]
    CarryOver(CarryOverArgs),
}

const CARRY_OVER_HELP: &str = "\
All three conditions must hold: (1) the patch-id of `BASE...HEAD` is equal for the old and the new head, each
against its own base; (2) there is no merge commit in NEW_BASE..NEW_HEAD; (3) `git range-diff OLD_BASE OLD_HEAD
NEW_BASE NEW_HEAD` shows `=` for every commit, at the same position. The four arguments are full 40-hex commit ids.
A step that could not run is `unknown` and the answer is no. Only for a yes the output ends with the line
`Carried-over: from=<40-hex> to=<40-hex> patch-id=<40-hex> pr=<n|none>`. A carry-over also needs `from` to be
CLEAR; stw does not read reviews here. --fetch fetches refs/pull/N/head of origin into refs/stw/carry-over/pr-N
(objects and that private ref only) and requires it to equal NEW_HEAD.

Exit codes:
  0  carry-over allowed
  1  not allowed, or a step could not run (the result is still printed)
  2  usage error";

#[derive(Args)]
struct CarryOverArgs {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// Pull request number written into the line, or `none` for a local review
    #[arg(long, value_name = "NUMBER|none")]
    pr: Option<String>,
    /// Fetch the head of the pull request from origin and require it to equal NEW_HEAD (needs a number)
    #[arg(long)]
    fetch: bool,
    /// Repository or worktree that holds the objects [default: the repository of the current directory]
    #[arg(long, value_name = "PATH")]
    checkout: Option<PathBuf>,
    /// OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD
    #[arg(value_name = "ID", num_args = 0..)]
    ids: Vec<String>,
}

fn run_carry_over(a: CarryOverArgs) -> Result<u8> {
    let req = carryover::parse_request(&a.ids, a.pr.as_deref(), a.fetch)
        .map_err(|e| Error::usage(e.to_string()))?;
    let start = match &a.checkout {
        Some(p) => p.clone(),
        None => std::env::current_dir()
            .map_err(|e| Error::usage(format!("no current directory: {e}")))?,
    };
    let start = std::fs::canonicalize(&start).unwrap_or(start);
    // the root of the repository or worktree around `start`; a directory that is none is checked as it is and
    // answers with a read failure
    let dir = repo::top_level(&start).unwrap_or(start);
    let res = carryover::check(&carryover::RepoGit::new(&dir, git_runner::shared()), &req);
    if a.json {
        print_json(&res.to_json());
    } else {
        print!("{}", res.render_human());
    }
    Ok(if res.carry_over {
        exit::OK
    } else {
        exit::FAILED
    })
}

#[derive(Subcommand)]
enum DevCommand {
    PatchId { repo: PathBuf, revs: Vec<String> },
}

/// Options every command shares.
#[derive(Args)]
struct Common {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// Do not talk to GitHub (no pull request data, no fetch)
    #[arg(long)]
    offline: bool,
    /// Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
    /// Only this repository of the workspace (name as printed by stw); repeatable
    #[arg(long = "repo", value_name = "NAME")]
    repos: Vec<String>,
}

#[derive(Args)]
struct TidyArgs {
    #[command(flatten)]
    common: Common,
    /// Also list the branches that are kept
    #[arg(long)]
    all: bool,
    /// Backup branches (r9-*, r10-*, pre-restack*, archive/*, backup/*) whose tip commit is at least N days old are `delete`
    #[arg(long, value_name = "N", default_value_t = tidy::DEFAULT_BACKUP_DAYS)]
    backup_days: u64,
}

#[derive(Args)]
struct ApplyArgs {
    #[command(flatten)]
    common: Common,
    /// Act on this `stw tidy plan --json` output (each entry is verified again first) [default: a fresh plan]
    #[arg(long, value_name = "FILE")]
    plan: Option<PathBuf>,
    /// Where the manifest goes [default: the user's data directory, stw/manifests]
    #[arg(long, value_name = "DIR")]
    manifest_dir: Option<PathBuf>,
    /// Backup branches whose tip commit is at least N days old are `delete`
    #[arg(long, value_name = "N", default_value_t = tidy::DEFAULT_BACKUP_DAYS)]
    backup_days: u64,
}

#[derive(Args)]
struct RestoreArgs {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// Run ID printed by `apply` [default: the newest run]
    #[arg(long, value_name = "ID")]
    run: Option<String>,
    /// Only this branch
    #[arg(long, value_name = "NAME")]
    branch: Option<String>,
    /// Where the manifests are [default: the user's data directory, stw/manifests]
    #[arg(long, value_name = "DIR")]
    manifest_dir: Option<PathBuf>,
}

#[derive(Args)]
struct SyncArgs {
    #[command(flatten)]
    common: Common,
    /// Fetch origin and fast-forward; never merges, rebases, stashes, resets or pushes
    #[arg(long)]
    apply: bool,
    /// Also handle the agent-rules checkout (the submodule .agents of a whr workspace): with --apply fetch it and
    /// fast-forward HEAD to origin's default branch; without --apply only report. Refuses only to protect local
    /// changes or local commits. Never runs install.sh
    #[arg(long)]
    rules: bool,
    /// agent-rules checkout [default: <workspace>/.agents]
    #[arg(long, value_name = "PATH")]
    rules_path: Option<PathBuf>,
}

#[derive(Args)]
struct AddArgs {
    /// OWNER/REPO of an existing repository, or REPO (the owner of the workspace's GitHub origin is used)
    spec: String,
    /// Where the project goes, relative to the workspace [default: projects/REPO]
    path: Option<String>,
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// Run every check and change nothing
    #[arg(long)]
    dry_run: bool,
    /// Workspace root, the top level of its git repository [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
}

#[derive(Args)]
struct StatusArgs {
    #[command(flatten)]
    common: Common,
    /// agent-rules checkout [default: <workspace>/.agents]
    #[arg(long, value_name = "PATH")]
    rules_path: Option<PathBuf>,
}

struct Workspace {
    root: String,
    repos: Vec<RepoRef>,
    github: GithubState,
}

fn workspace(c: &Common, rules_path: Option<&Path>) -> Result<Workspace> {
    let cwd = std::env::current_dir().unwrap_or_default();
    let root = c
        .root
        .clone()
        .or_else(|| repo::default_root(&cwd))
        .ok_or_else(|| {
            Error::usage(
                "no workspace root: pass --root PATH, run inside the workspace, or set STW_ROOT",
            )
        })?;
    if !root.is_dir() {
        return Err(Error::usage(format!(
            "workspace root is not a directory: {}",
            root.display()
        )));
    }
    let root = std::fs::canonicalize(&root).unwrap_or(root);
    let mut repos = repo::list_repos(&root);
    // the rules checkout is handled only by the agent-rules logic, never by the generic loop
    let rules = sync::rules::checkout_path(&root, rules_path);
    repos.retain(|r| std::fs::canonicalize(&r.path).unwrap_or(r.path.clone()) != rules);
    if !c.repos.is_empty() {
        let known: Vec<String> = repos.iter().map(|r| r.name.clone()).collect();
        if let Some(bad) = c.repos.iter().find(|n| !known.contains(n)) {
            return Err(Error::usage(format!(
                "unknown repository {bad:?}; the workspace has: {}",
                known.join(", ")
            )));
        }
        repos.retain(|r| c.repos.contains(&r.name));
    }
    Ok(Workspace {
        root: root.to_string_lossy().into_owned(),
        repos,
        github: GithubState::discover(c.offline),
    })
}

/// Parse the arguments, run, and return the process exit code.
pub fn main() -> ExitCode {
    let cli = match Cli::try_parse() {
        Ok(c) => c,
        Err(e) => {
            let _ = e.print();
            return ExitCode::from(if e.use_stderr() {
                exit::USAGE
            } else {
                exit::OK
            });
        }
    };
    match run(cli.command) {
        Ok(code) => ExitCode::from(code),
        Err(e) => {
            eprintln!("stw: {e}");
            ExitCode::from(if e.kind() == Kind::Usage {
                exit::USAGE
            } else {
                exit::FAILED
            })
        }
    }
}

fn run_add(a: AddArgs) -> Result<u8> {
    let cwd = std::env::current_dir().unwrap_or_default();
    let root = a
        .root
        .clone()
        .or_else(|| repo::add_root(&cwd))
        .ok_or_else(|| {
            Error::usage(
                "no workspace root: pass --root PATH, run inside the workspace, or set STW_ROOT",
            )
        })?;
    let root = add::absolute_root(&root)?;
    let origin_owner = repo::open_isolated(&root)
        .ok()
        .and_then(|r| repo::slug(&r))
        .and_then(|slug| slug.split('/').next().map(str::to_string));
    let req = add::parse_request(&a.spec, a.path.as_deref(), origin_owner.as_deref())?;
    let github = GithubState::discover(false);
    let cloner = add::GixCloner {
        credentials: github.client().map(|c| c.basic_auth()),
    };
    let report = add::add(&root, &req, &cloner, git_runner::shared(), a.dry_run)?;
    if a.json {
        print_json(&report.to_json(&output::p2s(&root)));
    } else if report.dry_run {
        println!(
            "would add {}/{} as {} in {} (dry run, nothing changed)",
            report.owner,
            report.repo,
            report.path,
            output::p2s(&root)
        );
    } else {
        println!(
            "added {}/{} as {} in {}: commit {} records {}",
            report.owner,
            report.repo,
            report.path,
            output::p2s(&root),
            output::short(report.commit.as_deref().unwrap_or("")),
            output::short(report.gitlink.as_deref().unwrap_or(""))
        );
    }
    Ok(exit::OK)
}

fn manifest_dir(given: Option<PathBuf>) -> Result<PathBuf> {
    given
        .or_else(tidy_apply::default_manifest_dir)
        .ok_or_else(|| {
            Error::usage("no manifest directory: pass --manifest-dir DIR (HOME is not set)")
        })
}

fn run_apply(a: ApplyArgs) -> Result<u8> {
    let ws = workspace(&a.common, None)?;
    let dir = manifest_dir(a.manifest_dir.clone())?;
    let wanted = match &a.plan {
        Some(f) => Some(tidy_apply::read_plan_file(f)?),
        None => None,
    };
    let now = tidy::now_secs();
    let popts = tidy::PlanOpts {
        backup_days: a.backup_days,
        now,
    };
    let aopts = tidy_apply::ApplyOpts {
        manifest_dir: dir,
        run_id: tidy_apply::new_run_id(),
        now,
    };
    let plans = repo::for_each(&ws.repos, |r| {
        tidy::plan_repo(r, ws.github.client(), a.common.offline, &popts)
    });
    let mut list = Vec::new();
    for (r, plan) in ws.repos.iter().zip(&plans) {
        let w: Option<&[tidy_apply::Planned]> = match &wanted {
            None => None,
            Some(all) => Some(
                all.iter()
                    .find(|(p, _)| same_path(p, &r.path))
                    .map(|(_, l)| l.as_slice())
                    .unwrap_or(&[]),
            ),
        };
        list.push(tidy_apply::apply_repo(r, plan, w, &aopts));
    }
    if let Some(all) = &wanted {
        for (p, _) in all {
            if !ws.repos.iter().any(|r| same_path(p, &r.path)) {
                list.push(tidy_apply::ApplyRepo {
                    name: p.clone(),
                    path: p.clone(),
                    error: Some("the plan names a repository that is not in this workspace".into()),
                    ..Default::default()
                });
            }
        }
    }
    if a.common.json {
        print_json(&tidy_apply::apply_to_json(&ws.root, &aopts.run_id, &list));
    } else {
        print!("{}", tidy_apply::apply_render_human(&aopts.run_id, &list));
    }
    Ok(if list.iter().any(|r| r.failed()) {
        exit::FAILED
    } else {
        exit::OK
    })
}

fn same_path(a: &str, b: &std::path::Path) -> bool {
    let (a, b) = (std::path::Path::new(a), b);
    a == b
        || matches!((std::fs::canonicalize(a), std::fs::canonicalize(b)), (Ok(x), Ok(y)) if x == y)
}

fn run(command: Command) -> Result<u8> {
    let failed = |any: bool| if any { exit::FAILED } else { exit::OK };
    match command {
        Command::Status(a) => {
            let c = a.common;
            let ws = workspace(&c, a.rules_path.as_deref())?;
            let root = PathBuf::from(&ws.root);
            let list = repo::for_each(&ws.repos, |r| {
                status::collect_one(r, &root, ws.github.client())
            });
            let rules = sync::rules::agent_rules(
                &root,
                None,
                &sync::rules::RulesOpts {
                    path: a.rules_path.clone(),
                    ..Default::default()
                },
            );
            if c.json {
                let mut v = status::to_json(&ws.root, &ws.github, &list);
                if let Value::Object(m) = &mut v {
                    m.insert("agent_rules".into(), sync::rules::to_json(&rules));
                }
                print_json(&v);
            } else {
                print!("{}", status::render_human(ws.github.note.clone(), &list));
                print!("{}", sync::rules::render_human(&rules));
            }
            Ok(failed(list.iter().any(|s| s.error.is_some())))
        }
        Command::Tidy {
            command: TidyCommand::Plan(a),
        } => {
            let ws = workspace(&a.common, None)?;
            let opts = tidy::PlanOpts {
                backup_days: a.backup_days,
                now: tidy::now_secs(),
            };
            let plans = repo::for_each(&ws.repos, |r| {
                tidy::plan_repo(r, ws.github.client(), a.common.offline, &opts)
            });
            if a.common.json {
                print_json(&tidy::to_json(&ws.root, &ws.github, &plans));
            } else {
                print!("{}", tidy::render_human(&plans, a.all));
                if let Some(n) = &ws.github.note {
                    println!("({n})");
                }
            }
            Ok(failed(plans.iter().any(|p| p.error.is_some())))
        }
        Command::Tidy {
            command: TidyCommand::Apply(a),
        } => run_apply(a),
        Command::Tidy {
            command: TidyCommand::Restore(a),
        } => {
            let dir = manifest_dir(a.manifest_dir.clone())?;
            let r = tidy_apply::restore(&dir, a.run.as_deref(), a.branch.as_deref())?;
            if a.json {
                print_json(&tidy_apply::restore_to_json(&r));
            } else {
                print!("{}", tidy_apply::restore_render_human(&r));
            }
            Ok(failed(!r.complete()))
        }
        Command::Add(a) => run_add(a),
        Command::Sync(a) => {
            let ws = workspace(&a.common, a.rules_path.as_deref())?;
            let opts = sync::Opts {
                apply: a.apply,
                offline: a.common.offline,
            };
            let list = repo::for_each(&ws.repos, |r| sync::sync_repo(r, ws.github.client(), &opts));
            let ropts = sync::rules::RulesOpts {
                path: a.rules_path.clone(),
                update: a.rules,
                apply: a.apply,
                offline: a.common.offline,
            };
            let rules =
                sync::rules::agent_rules(&PathBuf::from(&ws.root), ws.github.client(), &ropts);
            if a.common.json {
                let mut v = sync::to_json(&ws.root, a.apply, &list);
                if let Value::Object(m) = &mut v {
                    m.insert("agent_rules".into(), sync::rules::to_json(&rules));
                }
                print_json(&v);
            } else {
                print!("{}", sync::render_human(a.apply, &list));
                print!("{}", sync::rules::render_human(&rules));
            }
            Ok(failed(
                list.iter().any(|s| s.result == "error") || rules.action == "refused",
            ))
        }
        Command::Review {
            command: ReviewCommand::CarryOver(a),
        } => run_carry_over(a),
        Command::Dev {
            command: DevCommand::PatchId { repo, revs },
        } => {
            dev::patch_ids(&repo, &revs)?;
            Ok(exit::OK)
        }
    }
}
