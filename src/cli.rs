//! Command line (clap): `stw status | tidy plan | tidy apply | tidy restore | tidy history | sync`, the shared options, workspace selection and
//! the exit code of a run.
use std::path::{Path, PathBuf};
use std::process::ExitCode;
use std::time::Duration;

use clap::{Args, Parser, Subcommand};
use serde_json::{json, Value};

use crate::error::{exit, Error, Kind, Result};
use crate::github::GithubState;
use crate::output::{self, print_json};
use crate::repo::{self, RepoRef};
use crate::{
    add, carryover, dev, git_runner, lint_pr_cmd, status, status_review, status_stats, sync, tidy,
    tidy_apply, tidy_history,
};

const EXIT_CODES: &str = "\
Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
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
    /// Fast-forward every repository to its upstream (--plan or --dry-run only shows what it would do)
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
    /// Check work products against the working rules (read-only)
    #[command(subcommand_required = true, arg_required_else_help = true, after_help = EXIT_CODES)]
    Lint {
        #[command(subcommand)]
        command: LintCommand,
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
    /// List the stored manifests (the journals of `apply`), newest first, with the number of branches each recorded and
    /// how many `restore` could create now (read-only)
    #[command(after_help = EXIT_CODES)]
    History(HistoryArgs),
}

#[derive(Subcommand)]
enum ReviewCommand {
    /// May the review of a pull request head be carried over to a rewritten head? Prints the `Carried-over:` line for a yes; posts nothing
    #[command(after_help = CARRY_OVER_HELP)]
    CarryOver(CarryOverArgs),
}

#[derive(Subcommand)]
enum LintCommand {
    /// Check the commits, text and comments of a pull request against the machine-checkable working rules (read-only)
    #[command(after_help = LINT_PR_HELP)]
    Pr(LintPrArgs),
}

const LINT_PR_HELP: &str = "\
Read-only (GET only). Reads one pull request, its commits (up to 300) and its conversation comments (up to 300) and
reports one finding per violated rule with the rule ID, the severity and the location (PR title or text, comment ID,
commit SHA, line). Rules: no session link (Claude session trailer or code-session URL) in commit messages, PR title, PR
text and comments; author and committer of every commit is an owner identity; Co-Authored-By trailer; Conventional
Commit subjects; PR text about 25 lines (warning above 25, error above 30), a Blocker line and an Author agent line; the
first line of a review comment follows `Review: <CLEAR|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n>` (or the
Carried-over form) for this pull request; a review comment has at most 15 lines. `--json` lists the rule table.
The repository is --repo or, without it, the single GitHub repository of the workspace of PATH (default: the current
directory). Owner identities are --owner-identity (repeatable), else user.name and user.email of the Git configuration
read from the workspace (or the current directory with --repo); without any, usage error. The token is the one of the
other commands. Nothing is written to disk or to the forge.

Exit codes:
  0  no finding of severity error (warnings do not fail)
  1  a read failed or there is no GitHub token (no output)
  2  usage error
  3  at least one error finding, or a list was cut at its bound (the output is printed; the result is not a pass)";

#[derive(Args)]
struct LintPrArgs {
    /// Pull request number
    #[arg(value_name = "PR")]
    pr: String,
    /// Workspace whose single GitHub repository is meant [default: the current directory]
    #[arg(value_name = "PATH")]
    path: Option<String>,
    /// Pull request repository [default: the single GitHub repository of the workspace]
    #[arg(long = "repo", value_name = "OWNER/NAME")]
    repo: Option<String>,
    /// Owner identity for the author and committer rules (repeatable) [default: user.name and user.email of the Git configuration]
    #[arg(long = "owner-identity", value_name = "NAME <EMAIL>")]
    owners: Vec<String>,
    /// Print the human list (the default)
    #[arg(long)]
    human: bool,
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
}

fn run_lint_pr(a: LintPrArgs) -> Result<u8> {
    let p = lint_pr_cmd::prepare(&lint_pr_cmd::Flags {
        pr: a.pr,
        repo: a.repo,
        path: a.path,
        owners: a.owners,
        human: a.human,
        json: a.json,
    })?;
    lint_pr_cmd::run(&p)
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

#[derive(Subcommand)]
enum SyncCommand {
    /// Move the branches of a sync run back to the tips they had before it: only where a branch still stands on the tip
    /// the run left (else `changed-since`, nothing touched). Pruned refs, stash reapplies and submodule moves stay
    #[command(after_help = EXIT_CODES)]
    Restore(SyncRestoreArgs),
}

#[derive(Args)]
struct SyncRestoreArgs {
    /// Workspace root (same as --root)
    #[arg(value_name = "PATH")]
    path: Option<PathBuf>,
    /// Run ID printed by `sync` [default: the highest run ID found in the workspace]
    #[arg(long, value_name = "ID")]
    run: Option<String>,
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
}

#[derive(Args)]
struct HistoryArgs {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// Most journals listed, 1 to 1024
    #[arg(long, value_name = "N", default_value_t = tidy_history::DEFAULT_LIMIT as u16,
          value_parser = clap::value_parser!(u16).range(1..=tidy_history::MAX_LIMIT as i64))]
    limit: u16,
    /// Where the manifests are [default: the user's data directory, stw/manifests]
    #[arg(long, value_name = "DIR")]
    manifest_dir: Option<PathBuf>,
}

#[derive(Args)]
struct SyncArgs {
    #[command(subcommand)]
    sub: Option<SyncCommand>,
    #[command(flatten)]
    common: Common,
    /// Print what a run would do and change nothing: no fetch, no write (alias: --dry-run)
    #[arg(long, conflicts_with = "apply", overrides_with = "plan")]
    plan: bool,
    /// Alias of --plan
    #[arg(long, conflicts_with = "apply", overrides_with = "dry_run")]
    dry_run: bool,
    /// Accepted and ignored: sync runs by default (kept so that earlier calls still work); not in the help
    #[arg(long, hide = true)]
    apply: bool,
    /// Fetch with --prune: delete remote-tracking refs of origin (refs/remotes/origin/* only) that are gone there.
    /// Overrides fetch.prune. With --plan it only says what it would do
    #[arg(long, conflicts_with = "no_prune")]
    prune: bool,
    /// Fetch without pruning (overrides fetch.prune for a fetch by the installed git)
    #[arg(long)]
    no_prune: bool,
    /// Also move each pinned (detached) submodule to the commit the superproject records when that is a
    /// fast-forward: git submodule sync and update --no-fetch --checkout; never a clone, init or extra fetch. An
    /// uninitialized submodule or a missing commit is reported as unreachable. --plan says what would move
    #[arg(long)]
    submodules: bool,
    /// Also handle the agent-rules checkout (the submodule .agents of a whr workspace): fetch it and
    /// fast-forward HEAD to origin's default branch; with --plan only report. Refuses only to protect local
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
    /// Detailed text: a card of facts per repository and the full explanation of every reason it needs action (the JSON
    /// is the same)
    #[arg(short = 'l', long)]
    long: bool,
    /// Overall deadline of the GitHub reads, 1s to 10m, like 90s or 2m (not with --offline)
    #[arg(long, value_name = "DURATION", default_value = "90s", value_parser = parse_timeout, conflicts_with = "offline")]
    timeout: Duration,
    /// Print diagnostics at the end: API requests, rate limit, elapsed time (not with --offline); with --json the
    /// document gets a `stats` member
    #[arg(long, conflicts_with = "offline")]
    stats: bool,
    /// Print the diagnostics per repository and source, then the stats line (not with --offline); with --json they go
    /// to stderr
    #[arg(long, conflicts_with = "offline")]
    verbose: bool,
    /// Fetch origin of every repository first: remote-tracking refs only, no prune, no tags, no submodules; the one
    /// thing that is not read-only
    #[arg(long)]
    fetch: bool,
    /// List the linked worktrees of each repository (the main checkout is not listed)
    #[arg(long)]
    worktrees: bool,
    /// Also read the review state of the open pull requests from their comments (one comment read per pull
    /// request, at most 200 per run; not with --offline)
    #[arg(long, conflicts_with = "offline")]
    review: bool,
    /// Glyph set of the text: unicode, nerd or ascii [default: unicode on a terminal with a UTF-8 locale, else ascii]
    #[arg(long, value_name = "SET", value_parser = ["unicode", "nerd", "ascii"])]
    icons: Option<String>,
}

/// The bounds of `--timeout`, the overall deadline of the remote half.
pub const MIN_TIMEOUT: Duration = Duration::from_secs(1);
pub const MAX_TIMEOUT: Duration = Duration::from_secs(600);

/// A duration as Go writes it (`90s`, `2m`, `1m30s`, `1.5s`, `500ms`; units ns, us, ms, s, m, h) within 1s to 10m.
pub fn parse_timeout(text: &str) -> std::result::Result<Duration, String> {
    let bad = |why: &str| format!("{text:?} is not a duration: {why} (like 90s, 2m or 1m30s)");
    let mut rest = text.strip_prefix('+').unwrap_or(text);
    if rest.is_empty() {
        return Err(bad("empty"));
    }
    let mut total_ns: f64 = 0.0;
    while !rest.is_empty() {
        let n_end = rest
            .find(|c: char| !(c.is_ascii_digit() || c == '.'))
            .unwrap_or(rest.len());
        let (num, tail) = rest.split_at(n_end);
        let n: f64 = num.parse().map_err(|_| bad("a number is missing"))?;
        let u_end = tail
            .find(|c: char| c.is_ascii_digit() || c == '.')
            .unwrap_or(tail.len());
        let (unit, next) = tail.split_at(u_end);
        let ns = match unit {
            "ns" => 1.0,
            "us" | "\u{b5}s" | "\u{3bc}s" => 1e3,
            "ms" => 1e6,
            "s" => 1e9,
            "m" => 60e9,
            "h" => 3600e9,
            "" => return Err(bad("the unit is missing")),
            _ => return Err(bad("unknown unit")),
        };
        total_ns += n * ns;
        rest = next;
    }
    let d = Duration::from_nanos(total_ns.round() as u64);
    if !(MIN_TIMEOUT..=MAX_TIMEOUT).contains(&d) {
        return Err(format!(
            "{text} is outside {}s to {}m",
            MIN_TIMEOUT.as_secs(),
            MAX_TIMEOUT.as_secs() / 60
        ));
    }
    Ok(d)
}

struct Workspace {
    root: String,
    repos: Vec<RepoRef>,
    /// `.gitmodules` paths that are not accepted (never in `repos`); empty when repositories were named.
    rejected: Vec<(String, String)>,
    github: GithubState,
}

/// Notices for the paths of `.gitmodules` that are not accepted: text lines and the JSON field
/// `rejected_paths` (`[{path (escaped), reason}]`).
fn rejected_message(p: &str, why: &str) -> String {
    format!("submodule path not accepted: {}: {why}", escape_name(p))
}

fn rejected_notices(rejected: &[(String, String)]) -> Vec<String> {
    rejected
        .iter()
        .map(|(p, why)| rejected_message(p, why))
        .collect()
}

fn rejected_status_notices(rejected: &[(String, String)]) -> Vec<status::Notice> {
    rejected
        .iter()
        .map(|(p, why)| status::Notice {
            code: "rejected-path",
            source: "submodules",
            repos: Vec::new(),
            message: rejected_message(p, why),
            count: 1,
        })
        .collect()
}

fn rejected_json(rejected: &[(String, String)]) -> Value {
    Value::Array(
        rejected
            .iter()
            .map(|(p, why)| json!({"path": escape_name(p), "reason": why}))
            .collect(),
    )
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
    let rejected = if c.repos.is_empty() {
        repo::rejected_paths(&root)
    } else {
        Vec::new()
    };
    Ok(Workspace {
        root: root.to_string_lossy().into_owned(),
        repos,
        rejected,
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

/// `stw sync restore`: any sync option given with it is a usage error (nothing is done).
fn run_sync_restore(
    r: SyncRestoreArgs,
    common: &Common,
    sync_flags: [(&str, bool); 8],
) -> Result<u8> {
    let given = sync_flags
        .iter()
        .find(|(_, on)| *on)
        .map(|(n, _)| *n)
        .or(common.offline.then_some("--offline"))
        .or((!common.repos.is_empty()).then_some("--repo"));
    if let Some(name) = given {
        return Err(Error::usage(format!(
            "{name} does not apply to stw sync restore"
        )));
    }
    let roots = [r.path.clone(), r.root.clone(), common.root.clone()];
    if roots.iter().flatten().count() > 1 {
        return Err(Error::usage(
            "name the workspace once: PATH or --root, not both",
        ));
    }
    let json = r.json || common.json;
    let c = Common {
        json,
        offline: true,
        root: roots.into_iter().flatten().next(),
        repos: Vec::new(),
    };
    let ws = workspace(&c, None)?;
    let result = sync::restore::restore(&ws.repos, r.run.as_deref());
    if json {
        print_json(&sync::restore::to_json(
            &ws.root,
            &result,
            rejected_json(&ws.rejected),
        ));
    } else if let Some((code, message)) = &result.error {
        return Err(Error::repository(format!("sync: {code}: {message}")));
    } else {
        print!("{}", sync::restore::render_human(&result));
        for n in rejected_notices(&ws.rejected) {
            println!("({n})");
        }
    }
    Ok(if result.failed() {
        exit::FAILED
    } else {
        exit::OK
    })
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
        let mut v = tidy_apply::apply_to_json(&ws.root, &aopts.run_id, &list);
        if let Value::Object(m) = &mut v {
            m.insert("rejected_paths".into(), rejected_json(&ws.rejected));
        }
        print_json(&v);
    } else {
        print!("{}", tidy_apply::apply_render_human(&aopts.run_id, &list));
        print_notices(&ws.rejected);
    }
    Ok(if list.iter().any(|r| r.failed()) {
        exit::FAILED
    } else {
        exit::OK
    })
}

fn print_notices(rejected: &[(String, String)]) {
    for n in rejected_notices(rejected) {
        println!("({n})");
    }
}

fn escape_name(p: &str) -> String {
    crate::github::reads::escape_untrusted(p, 200).0
}

fn same_path(a: &str, b: &std::path::Path) -> bool {
    let (a, b) = (std::path::Path::new(a), b);
    a == b
        || matches!((std::fs::canonicalize(a), std::fs::canonicalize(b)), (Ok(x), Ok(y)) if x == y)
}

fn run_status(a: StatusArgs) -> Result<u8> {
    let started = std::time::Instant::now();
    let c = a.common;
    let ws = workspace(&c, a.rules_path.as_deref())?;
    let root = PathBuf::from(&ws.root);
    let opts = status::StatusOpts {
        worktrees: a.worktrees,
        fetch: a.fetch,
        review: a.review,
    };
    let mut list = repo::for_each(&ws.repos, |r| {
        status::collect_local(r, &root, &opts, ws.github.client())
    });
    let local_done = started.elapsed();
    if let Some(cl) = ws.github.client() {
        cl.arm_deadline(a.timeout);
    }
    list = repo::for_each_mut(list, |s| status::read_remote(s, ws.github.client()));
    if let (true, Some(cl)) = (a.review, ws.github.client()) {
        list = status_review::read_reviews(list, cl);
    }
    let timings = status_stats::Timings {
        total: started.elapsed(),
        local: local_done,
        remote: started.elapsed().saturating_sub(local_done),
    };
    let stats = ws.github.client().map(|cl| cl.stats()).unwrap_or_default();
    let diag = a.stats || a.verbose;
    let rules = sync::rules::agent_rules(
        &root,
        None,
        &sync::rules::RulesOpts {
            path: a.rules_path.clone(),
            ..Default::default()
        },
    );
    let notices =
        status::collect_notices(&ws.github, &rejected_status_notices(&ws.rejected), &list);
    if c.json {
        let mut v = status::to_json(
            &ws.root,
            &ws.github,
            &list,
            &notices,
            &opts,
            diag.then(|| status_stats::to_json(&stats, &timings)),
        );
        if let Value::Object(m) = &mut v {
            m.insert("agent_rules".into(), sync::rules::to_json(&rules));
            m.insert("rejected_paths".into(), rejected_json(&ws.rejected));
        }
        print_json(&v);
        if a.verbose {
            // standard output stays one JSON document
            eprintln!();
            eprint!(
                "{}",
                status_stats::breakdown(&stats, status::escape_for_text)
            );
            eprintln!("{}", status_stats::stats_line(&stats, &timings));
        }
    } else {
        let mut tail = String::new();
        if a.verbose {
            tail += "\n";
            tail += &status_stats::breakdown(&stats, status::escape_for_text);
        }
        if diag {
            tail += &status_stats::stats_line(&stats, &timings);
            tail.push('\n');
        }
        print!(
            "{}",
            status::render_human(
                &notices,
                &list,
                &sync::rules::render_human(&rules),
                &status::View {
                    icons: status::resolve_icons(a.icons.as_deref(), &status::Terminal::detect()),
                    long: a.long,
                    diagnostics: &tail,
                }
            )
        );
    }
    Ok(if list.iter().any(|s| s.error.is_some()) {
        exit::FAILED
    } else {
        exit::OK
    })
}

fn run(command: Command) -> Result<u8> {
    let failed = |any: bool| if any { exit::FAILED } else { exit::OK };
    match command {
        Command::Status(a) => run_status(a),
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
                let mut v = tidy::to_json(&ws.root, &ws.github, &plans);
                if let Value::Object(m) = &mut v {
                    m.insert("rejected_paths".into(), rejected_json(&ws.rejected));
                }
                print_json(&v);
            } else {
                print!("{}", tidy::render_human(&plans, a.all));
                print_notices(&ws.rejected);
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
        Command::Tidy {
            command: TidyCommand::History(a),
        } => {
            let dir = manifest_dir(a.manifest_dir.clone())?;
            let h = tidy_history::history(&dir, usize::from(a.limit))?;
            if a.json {
                print_json(&tidy_history::to_json(&h));
            } else {
                print!("{}", tidy_history::render_human(&h));
            }
            Ok(exit::OK)
        }
        Command::Add(a) => run_add(a),
        Command::Sync(SyncArgs {
            sub: Some(SyncCommand::Restore(r)),
            common,
            plan,
            dry_run,
            apply,
            prune,
            no_prune,
            submodules,
            rules,
            rules_path,
        }) => run_sync_restore(
            r,
            &common,
            [
                ("--plan", plan),
                ("--dry-run", dry_run),
                ("--apply", apply),
                ("--prune", prune),
                ("--no-prune", no_prune),
                ("--submodules", submodules),
                ("--rules", rules),
                ("--rules-path", rules_path.is_some()),
            ],
        ),
        Command::Sync(a) => {
            let ws = workspace(&a.common, a.rules_path.as_deref())?;
            // sync acts unless --plan or --dry-run asks for the preview; --apply is an ignored alias
            let execute = !(a.plan || a.dry_run);
            let opts = sync::Opts {
                apply: execute,
                offline: a.common.offline,
                prune: if a.prune {
                    sync::fetch::Prune::On
                } else if a.no_prune {
                    sync::fetch::Prune::Off
                } else {
                    sync::fetch::Prune::Unset
                },
                submodules: a.submodules,
                // one ID for the whole run; a plan journals nothing
                run_id: execute.then(sync::journal::new_run_id),
            };
            let mut list =
                repo::for_each(&ws.repos, |r| sync::sync_repo(r, ws.github.client(), &opts));
            sync::rejected_pass(&ws.rejected, &mut list, &opts);
            if a.submodules {
                let root = PathBuf::from(&ws.root);
                let rules_dir = sync::rules::checkout_path(&root, a.rules_path.as_deref());
                sync::submodule_pass(
                    &root,
                    &mut list,
                    &opts,
                    a.common.repos.is_empty(),
                    &rules_dir,
                );
            }
            let ropts = sync::rules::RulesOpts {
                path: a.rules_path.clone(),
                update: a.rules,
                apply: execute,
                offline: a.common.offline,
            };
            let rules =
                sync::rules::agent_rules(&PathBuf::from(&ws.root), ws.github.client(), &ropts);
            if a.common.json {
                let mut v = sync::to_json(&ws.root, execute, &list, &opts);
                if let Value::Object(m) = &mut v {
                    m.insert("agent_rules".into(), sync::rules::to_json(&rules));
                }
                print_json(&v);
            } else {
                print!("{}", sync::render_human(execute, &list, &opts));
                print!("{}", sync::rules::render_human(&rules));
            }
            Ok(failed(
                list.iter().any(|s| s.result == "error") || rules.action == "refused",
            ))
        }
        Command::Review {
            command: ReviewCommand::CarryOver(a),
        } => run_carry_over(a),
        Command::Lint {
            command: LintCommand::Pr(a),
        } => run_lint_pr(a),
        Command::Dev {
            command: DevCommand::PatchId { repo, revs },
        } => {
            dev::patch_ids(&repo, &revs)?;
            Ok(exit::OK)
        }
    }
}
