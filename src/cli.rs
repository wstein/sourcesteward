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
    absorb, add, carryover, dev, git_runner, lint_files, lint_pr, lint_pr_cmd, oplog, prove,
    restack_plan, status, status_all, status_review, status_stats, sync, tidy, tidy_apply,
    tidy_history, tidy_similarity,
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
    /// Check the read-only preconditions for marking a pull request ready
    ReadyCheck(ReadyCheckArgs),
    /// Review helpers (read-only)
    #[command(subcommand_required = true, arg_required_else_help = true, after_help = EXIT_CODES)]
    Review {
        #[command(subcommand)]
        command: ReviewCommand,
    },
    /// Prove locally that a rewritten branch is the old one rebased and nothing else (read-only, no network)
    #[command(after_help = PROVE_HELP)]
    Prove(ProveArgs),
    /// Plan where the staged changes would go: the unpushed commit that owns each hunk (read-only, writes nothing)
    #[command(after_help = ABSORB_HELP)]
    Absorb(AbsorbArgs),
    /// Predict a rebase of a stack of branches before it happens (read-only, writes nothing)
    #[command(subcommand_required = true, arg_required_else_help = true, after_help = EXIT_CODES)]
    Restack {
        #[command(subcommand)]
        command: RestackCommand,
    },
    /// The op log: ref changes (timeline), rewrites of a commit (evolog), undo of one branch and the opt-in git hooks that write it
    #[command(subcommand_required = true, arg_required_else_help = true, after_help = OPLOG_HELP)]
    Oplog {
        #[command(subcommand)]
        command: OplogCommand,
    },
    /// Save and resume a local task checkpoint
    Workspace {
        #[command(subcommand)]
        command: crate::workspace::WorkspaceCommand,
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

#[derive(Args)]
struct ReadyCheckArgs {
    /// Pull request number
    number: String,
    /// Repository OWNER/NAME (default: the single GitHub repository in the workspace)
    #[arg(long)]
    repo: Option<String>,
    /// Print the human list (the default)
    #[arg(long, conflicts_with = "json")]
    human: bool,
    /// Print one JSON object
    #[arg(long)]
    json: bool,
}

fn run_ready_check(a: ReadyCheckArgs) -> Result<u8> {
    let n = a
        .number
        .parse::<u64>()
        .ok()
        .filter(|n| *n > 0 && n.to_string() == a.number)
        .ok_or_else(|| Error::usage("NUMBER must be a positive decimal integer"))?;
    let repo = match a.repo {
        Some(r) if lint_pr_cmd::valid_repo(&r) => r,
        Some(_) => return Err(Error::usage("--repo must be OWNER/NAME")),
        None => lint_pr_cmd::workspace_repo(None)?.0,
    };
    let gh = GithubState::discover(false);
    let client = gh.client().ok_or_else(|| {
        Error::github(format!(
            "hosting-api-unavailable: {}",
            gh.note.as_deref().unwrap_or_default()
        ))
    })?;
    let (f, errors) = crate::ready_check::collect(client, &repo, n);
    let doc = crate::ready_check::document(&repo, n, &f, &errors, crate::tidy::now_secs());
    if a.json {
        println!("{doc}");
    } else {
        let head: String = f.head.chars().take(7).collect();
        println!(
            "{}#{} head {}, draft {}",
            repo,
            n,
            if head.is_empty() { "-" } else { &head },
            if f.pull_error.is_empty() {
                f.draft.to_string()
            } else {
                "-".into()
            }
        );
        for c in crate::ready_check::evaluate(&f) {
            println!("  {:<10} {:<7} {}", c.id, c.status.label(), c.evidence);
        }
        for (source, message) in &errors {
            println!(
                "  read error ({}): {}",
                source,
                crate::github::reads::escape_untrusted(message, 300).0
            );
        }
        println!("Ready: {}", if doc["ready"] == true { "yes" } else { "no" });
    }
    Ok(if doc["ready"] == true {
        exit::OK
    } else {
        exit::FAILED
    })
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
enum RestackCommand {
    /// Which level of the stack rebases cleanly onto the new base, where a conflict arises (files) and which level only inherits it
    #[command(after_help = RESTACK_PLAN_HELP)]
    Plan(RestackPlanArgs),
}

#[derive(Subcommand)]
enum OplogCommand {
    /// Ref changes from the reflogs and the hook log `refs.log`, newest last, with what the logs do not cover
    #[command(after_help = OPLOG_HELP)]
    Timeline(OplogTimelineArgs),
    /// The recorded rewrites (amend, rebase) of a commit, from the hook log `rewrites.log`
    #[command(after_help = OPLOG_HELP)]
    Evolog(OplogEvologArgs),
    /// What undoing one local branch to an older reflog value would check; changes nothing
    #[command(after_help = OPLOG_HELP)]
    UndoPlan(OplogUndoPlanArgs),
    /// Move one local branch back to an older reflog value: backup ref, compare-and-swap update; writes
    #[command(after_help = OPLOG_HELP)]
    Undo(OplogUndoArgs),
    /// Install, check or remove the two git hooks that write the op log (post-rewrite, reference-transaction); opt-in per repository
    #[command(subcommand_required = true, arg_required_else_help = true, after_help = OPLOG_HOOKS_HELP)]
    Hooks {
        #[command(subcommand)]
        command: OplogHooksCommand,
    },
}

#[derive(Subcommand)]
enum OplogHooksCommand {
    /// Where the hooks are, whether they are ours, `core.hooksPath`, worktrees, submodules, logs (read-only)
    #[command(after_help = OPLOG_HOOKS_HELP)]
    Status(OplogHooksArgs),
    /// Write both hooks into this repository's hooks directory; without --yes only the plan
    #[command(after_help = OPLOG_HOOKS_HELP)]
    Install(OplogHooksWriteArgs),
    /// Remove our two hook files (only files that carry our header); the logs stay; without --yes only the plan
    #[command(after_help = OPLOG_HOOKS_HELP)]
    Uninstall(OplogHooksWriteArgs),
    /// Print the text of one hook (post-rewrite or reference-transaction); writes nothing
    #[command(after_help = OPLOG_HOOKS_HELP)]
    Print {
        /// post-rewrite or reference-transaction
        #[arg(value_name = "NAME")]
        name: String,
    },
}

#[derive(Subcommand)]
enum LintCommand {
    /// Check the commits, text and comments of a pull request against the machine-checkable working rules (read-only)
    #[command(after_help = LINT_PR_HELP)]
    Pr(LintPrArgs),
    /// Check the text of a review comment from a file against the review line rules (read-only, no network)
    #[command(after_help = LINT_REVIEW_HELP)]
    Review(LintReviewArgs),
    /// Check the text of a pull request from a file: length, Blocker line, attribution line, session link (read-only, no network)
    #[command(name = "pr-text", after_help = LINT_PR_TEXT_HELP)]
    PrText(LintPrTextArgs),
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

const LINT_REVIEW_HELP: &str = "\
New in stw (no Go counterpart). Reads the text of one review comment from FILE (UTF-8) and reports one finding per
violated rule with the rule ID, the severity and the line. Rules: the first line is exactly
`Review: <CLEAR|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n|none>` or
`Carried-over: from=<40 hex> to=<40 hex> patch-id=<40 hex> pr=<n|none>` (fixed keys and order, lower-case hex, read by the
strict parser of `review lines`); at most 15 lines; no carriage return anywhere; no session link. With --pr the `pr=` field must equal it. With --base and --head (both) the patch-id of
`BASE...HEAD` is computed by git (`git diff BASE...HEAD | git patch-id --stable`, first field; a tag is peeled to its
commit, no merge base is a usage error) and compared with `patch-id=`, and `head=` (`to=` of the Carried-over form) is compared with the id HEAD
resolves to. The repository is --checkout or the one around the current directory. Reads FILE and the repository only;
no network, nothing is written; `git` runs only for --base and --head. `--json` lists the rule table.

Exit codes:
  0  no finding of severity error
  3  at least one error finding (the output is printed)
  2  usage error (also: FILE not readable or not UTF-8, a revision that does not resolve, no merge base)";

#[derive(Args)]
struct LintReviewArgs {
    /// File with the text of the review comment
    #[arg(value_name = "FILE")]
    file: PathBuf,
    /// Base revision of the patch-id (with --head)
    #[arg(long, value_name = "REV")]
    base: Option<String>,
    /// Head revision: patch-id of BASE...HEAD and the head= field are compared (with --base)
    #[arg(long, value_name = "REV")]
    head: Option<String>,
    /// The pr= field must be this pull request number, or none
    #[arg(long, value_name = "NUMBER|none")]
    pr: Option<String>,
    /// Repository or worktree for --base and --head [default: the repository of the current directory]
    #[arg(long, value_name = "PATH")]
    checkout: Option<PathBuf>,
    /// Print the human list (the default)
    #[arg(long)]
    human: bool,
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
}

const LINT_PR_TEXT_HELP: &str = "\
New in stw (no Go counterpart). Reads the text of one pull request from FILE (UTF-8) and reports one finding per violated
rule with the rule ID, the severity and the line. Rules: the text is about 25 lines (warning above 25, error above 30,
the limits of `lint pr`); exactly one Blocker line of the form `Blocker: <text>` (a Draft names its blocker) or, with
--ready, exactly `Blocker: None.`; the last non-blank line is `🤖 Generated with [Claude Code](https://claude.com/claude-code)`;
no session link; no carriage return. A reference `#n` to the wrong repository is not checked. Reads FILE only; no network,
nothing is written. `--json` lists the rule table.

Exit codes:
  0  no finding of severity error (warnings do not fail)
  3  at least one error finding (the output is printed)
  2  usage error (also: FILE not readable or not UTF-8)";

#[derive(Args)]
struct LintPrTextArgs {
    /// File with the text of the pull request
    #[arg(value_name = "FILE")]
    file: PathBuf,
    /// The pull request is to be set Ready: `Blocker: None.` is required (without it, a Draft: a named blocker is required)
    #[arg(long)]
    ready: bool,
    /// Print the human list (the default)
    #[arg(long, conflicts_with = "json")]
    human: bool,
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
}

fn run_lint_pr_text(a: LintPrTextArgs) -> Result<u8> {
    let text = lint_files::read_text(&a.file)?;
    let report = lint_files::Report {
        command: "lint pr-text",
        file: a.file.display().to_string(),
        lines: lint_pr::line_count(&text),
        rule_ids: &lint_files::PR_TEXT_RULE_IDS,
        options: json!({"ready": a.ready}),
        findings: lint_files::lint_pr_text(&text, a.ready),
    };
    Ok(print_file_report(&report, a.json))
}

/// Prints a file report and returns the exit status: 0 without an error finding, 3 with one (as `lint pr`).
fn print_file_report(r: &lint_files::Report, json: bool) -> u8 {
    if json {
        print_json(&r.to_json(tidy::now_secs()));
    } else {
        print!("{}", r.render_human());
    }
    if r.passed() {
        exit::OK
    } else {
        lint_pr_cmd::EXIT_FINDINGS
    }
}

fn run_lint_review(a: LintReviewArgs) -> Result<u8> {
    if a.human && a.json {
        return Err(Error::usage("--human and --json cannot be combined"));
    }
    let pr =
        a.pr.as_deref()
            .map(|p| carryover::parse_pr(p).map_err(|e| Error::usage(e.to_string())))
            .transpose()?;
    let expect = match (&a.base, &a.head) {
        (Some(b), Some(h)) => Some(lint_files::expectation(a.checkout.as_deref(), b, h)?),
        (None, None) if a.checkout.is_none() => None,
        _ => {
            return Err(Error::usage(
                "--base and --head go together, and --checkout needs them",
            ))
        }
    };
    let text = lint_files::read_text(&a.file)?;
    let findings = lint_files::lint_review(
        &text,
        &lint_files::ReviewOpts {
            pr,
            expect: expect.clone(),
        },
    );
    let report = lint_files::Report {
        command: "lint review",
        file: a.file.display().to_string(),
        lines: lint_pr::line_count(&text),
        rule_ids: &lint_files::REVIEW_RULE_IDS,
        options: json!({
            "pr": a.pr,
            "base": a.base,
            "head": a.head,
            "resolved_head": expect.as_ref().map(|e| e.head.clone()),
            "computed_patch_id": expect.as_ref().map(|e| e.patch_id.clone()),
        }),
        findings,
    };
    Ok(print_file_report(&report, a.json))
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

const PROVE_HELP: &str = "\
New in stw (no Go counterpart). ALT is the old branch or head, NEU the new one (one revision each, not a range).
Eight checks, all must hold: (1) `git range-diff` shows `=` for every commit at the same position; (2) the patch-id of
every commit is equal, old against new; (3) the patch-id of `BASE...HEAD` is equal; (4) no merge commit in the new range;
(5) the same set of changed paths; (6) author and committer of every new commit are user.name <user.email> of the Git
configuration (read only); (7) every Co-Authored-By line of an old commit is in the new commit at the same position;
(8) no session link in the new commit texts. The old base is the
merge base of ALT and NEU, or, with --old-parent REV, REV (the old parent of ALT, which must be an ancestor of it: a child
branch whose parent was rebased). The new base is the same commit (a rewrite on the same base) or, with --base BASIS, BASIS
(a rebase onto it; BASIS must be an ancestor of NEU). A step that could not run is `unknown` and the verdict is `not
proved`. Only for `proved` the last line is `Carried-over: from=<ALT> to=<NEU> patch-id=<hex> pr=<n|none>`. Nothing is
posted and nothing is written; `git` runs for `range-diff` only, everything else is read in process.
The repository is the one of the current directory (it must belong to the workspace), or --repo NAME.

Exit codes:
  0  proved
  1  not proved, or a step could not run (the result is still printed)
  2  usage error";

#[derive(Args)]
struct ProveArgs {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// The base NEU was rebased onto (an ancestor of NEU) [default: the merge base of ALT and NEU, a rewrite on the same base]
    #[arg(long, value_name = "BASIS")]
    base: Option<String>,
    /// The old parent of ALT, an ancestor of it: the old base when ALT was a child branch whose parent was rebased (`git rebase --onto NEWPARENT OLDPARENT ALT`) [default: the merge base of ALT and NEU]
    #[arg(long, value_name = "REV")]
    old_parent: Option<String>,
    /// Pull request number written into the line, or `none` [default: none]
    #[arg(long, value_name = "NUMBER|none")]
    pr: Option<String>,
    /// Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
    /// Repository of the workspace (name as printed by stw) [default: the repository of the current directory]
    #[arg(long = "repo", value_name = "NAME")]
    repo: Option<String>,
    /// ALT NEU
    #[arg(value_name = "REV", num_args = 0..)]
    revs: Vec<String>,
}

/// The one repository a command works in: `--repo NAME` of the workspace, else the repository of the current
/// directory, which must be one of the workspace (`list_repos`: the root and its accepted submodules; a path that
/// `rejected_paths` names never is). A repository outside the workspace is never used. Without a repository
/// around the current directory the workspace must have exactly one repository.
fn selected_repo(root: Option<&Path>, name: Option<&str>) -> Result<RepoRef> {
    let cwd = std::env::current_dir().unwrap_or_default();
    let cwd = std::fs::canonicalize(&cwd).unwrap_or(cwd);
    let top = repo::top_level(&cwd);
    let root = root
        .map(Path::to_path_buf)
        .or_else(|| repo::default_root(&cwd))
        .or_else(|| top.clone())
        .ok_or_else(|| {
            Error::usage(
                "no workspace root: pass --root PATH, run inside a repository, or set STW_ROOT",
            )
        })?;
    if !root.is_dir() {
        return Err(Error::usage(format!(
            "workspace root is not a directory: {}",
            escape_name(&root.to_string_lossy())
        )));
    }
    let root = std::fs::canonicalize(&root).unwrap_or(root);
    let repos = repo::list_repos_checked(&root)?;
    let known = || {
        repos
            .iter()
            .map(|r| escape_name(&r.name))
            .collect::<Vec<_>>()
            .join(", ")
    };
    if let Some(n) = name {
        if let Some(r) = repos.iter().find(|r| r.name == n) {
            return Ok(r.clone());
        }
        let rejected = repo::rejected_paths(&root)
            .into_iter()
            .find(|(p, _)| p.trim_end_matches('/') == n)
            .map(|(p, why)| format!(" ({})", rejected_message(&p, &why)))
            .unwrap_or_default();
        return Err(Error::usage(format!(
            "unknown repository {}{rejected}; the workspace has: {}",
            escape_name(n),
            known()
        )));
    }
    let real = |p: &Path| std::fs::canonicalize(p).unwrap_or_else(|_| p.to_path_buf());
    match top {
        Some(top) => {
            let top = real(&top);
            repos
                .iter()
                .find(|r| real(&r.path) == top)
                .cloned()
                .ok_or_else(|| {
                    Error::usage(format!(
                        "the repository of the current directory is not part of the workspace {}: pass --repo NAME (the workspace has: {}) or --root PATH",
                        escape_name(&root.to_string_lossy()),
                        known()
                    ))
                })
        }
        None if repos.len() == 1 => Ok(repos[0].clone()),
        None => Err(Error::usage(format!(
            "not inside a repository: pass --repo NAME (the workspace has: {})",
            known()
        ))),
    }
}

fn run_prove(a: ProveArgs) -> Result<u8> {
    let req = prove::parse_request(
        &a.revs,
        a.base.as_deref(),
        a.old_parent.as_deref(),
        a.pr.as_deref(),
    )
    .map_err(|e| Error::usage(e.to_string()))?;
    let r = selected_repo(a.root.as_deref(), a.repo.as_deref())?;
    let dir = std::fs::canonicalize(&r.path).unwrap_or(r.path);
    let proof = prove::prove(&dir, git_runner::shared(), &req);
    if a.json {
        print_json(&proof.to_json());
    } else {
        print!("{}", proof.render_human());
    }
    Ok(if proof.proved { exit::OK } else { exit::FAILED })
}

const ABSORB_HELP: &str = "\
New in stw (no Go counterpart); specification and limits: README.md, section `stw absorb plan`. Computes the PLAN of
folding the staged changes into the unpushed commits that own the changed lines (the method of git-absorb: the newest
commit of the stack whose changes do not commute with the hunk). It writes nothing: no add, commit, rebase, ref, file or
index refresh, no network, no hook; a partial clone is refused (no lazy fetch). Git only: a jj or GitButler checkout is
refused. The stack: the first-parent commits of HEAD that no remote-tracking branch (and, without --base, no other
local branch) contains, by the author of `user.name` and `user.email`, up to a merge commit. A hunk that cannot be
placed is skipped with its reason (no-candidate, ambiguous, conflict, outside-range, published, foreign). The only git
program that runs is `git blame` for the removed lines (read-only, GIT_NO_LAZY_FETCH=1).

Exit codes:
  0  a plan was produced (also when every hunk is skipped)
  1  refused with a typed error code (nothing-staged, not-owned, published, unmerged-index, environment-override,
     partial-clone-unsupported, blame-failed, unsupported-backend, ...); with --json the error is the document
  2  usage error";

#[derive(Args)]
struct AbsorbArgs {
    /// Print one JSON object instead of text
    #[arg(long, conflicts_with = "human")]
    json: bool,
    /// Print the human list (the default)
    #[arg(long)]
    human: bool,
    /// The plan is the only mode of this version; accepted so that a later `--apply --plan-sum SUM` reads the same way
    #[arg(long)]
    plan: bool,
    /// The stack is REV..HEAD (REV an ancestor of HEAD); the stack limit does not apply [default: the commits no remote-tracking branch contains]
    #[arg(long, value_name = "REV")]
    base: Option<String>,
    /// At most N commits in the stack (without --base)
    #[arg(long, value_name = "N", default_value_t = absorb::DEFAULT_MAX_STACK,
          value_parser = clap::value_parser!(u32).range(1..=100_000))]
    max_stack: u32,
    /// A hunk whose lines come from several commits goes to the newest of them instead of being skipped as ambiguous
    #[arg(long)]
    ambiguous_newest: bool,
    /// Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
    /// Repository of the workspace (name as printed by stw) [default: the repository of the current directory]
    #[arg(long = "repo", value_name = "NAME")]
    repo: Option<String>,
}

fn run_absorb(a: AbsorbArgs) -> Result<u8> {
    let opts = absorb::Options {
        base: a.base.clone(),
        max_stack: a.max_stack as usize,
        ambiguous_newest: a.ambiguous_newest,
    };
    let refused = |f: absorb::Fail| {
        eprintln!("stw absorb: {}: {}", f.code, f.message);
        if a.json {
            print_json(&f.to_json());
        }
        Ok(exit::FAILED)
    };
    if let Some(f) = absorb::environment_override() {
        return refused(f);
    }
    let r = selected_repo(a.root.as_deref(), a.repo.as_deref())?;
    let dir = std::fs::canonicalize(&r.path).unwrap_or(r.path);
    let cwd = std::env::current_dir().unwrap_or_default();
    let cwd = std::fs::canonicalize(&cwd).unwrap_or(cwd);
    match absorb::plan(&dir, &cwd, git_runner::shared(), &opts) {
        Ok(plan) => {
            if a.json {
                print_json(plan.to_json());
            } else {
                print!("{}", plan.render_human());
            }
            Ok(exit::OK)
        }
        Err(f) => refused(f),
    }
}

const RESTACK_PLAN_HELP: &str = "\
Port of the single-branch prediction of the Go `restack plan` (BRANCH --old-parent REV --onto BASIS) plus a whole-stack
preview that is new in stw. BRANCH... are local branches, bottom to top; each sits on the one before. Without any branch
the chain is derived from the local branches: it starts at the checked-out branch and goes down to the base, each branch
the nearest one below the previous; anything ambiguous (two candidates, two branches at one commit, a detached head) is
refused and the explicit spelling is suggested. --onto is the new base [default: the default branch of the repository
(origin/HEAD), else a usage error]. Each level is replayed in memory onto the new tip of the level below (gix merge); a
level above a conflict only inherits it and is not computed. The first line is the verdict: `plan: clean` or `plan:
conflicts at A (files ...)` (also `refused`, `unknown`). The replay approximates `git rebase`: directory renames, rename
detection, binary files, submodules and merge drivers are covered in README.md; a case the replay cannot judge is
`unknown` with the reason, never `clean`; rerere is not considered. Nothing is written (no ref, file or object), no
network, no `git` process.

Exit codes:
  0  plan: clean
  1  conflicts, refused or unknown (the plan is printed)
  2  usage error (also: no --onto and no default branch, a branch that does not exist)";

#[derive(Args)]
struct RestackPlanArgs {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// The new base: a revision [default: the default branch of the repository (origin/HEAD)]
    #[arg(long, value_name = "BASIS")]
    onto: Option<String>,
    /// Old parent of the bottom branch, an ancestor of it (one branch only) [default: the merge base of the bottom branch and the base]
    #[arg(long, value_name = "REV")]
    old_parent: Option<String>,
    /// Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
    /// Repository of the workspace (name as printed by stw) [default: the repository of the current directory]
    #[arg(long = "repo", value_name = "NAME")]
    repo: Option<String>,
    /// Local branches, bottom to top [default: derived from the local branches]
    #[arg(value_name = "BRANCH")]
    branches: Vec<String>,
}

fn run_restack_plan(a: RestackPlanArgs) -> Result<u8> {
    let req = restack_plan::parse_request(&a.branches, a.onto.as_deref(), a.old_parent.as_deref())
        .map_err(|e| Error::usage(e.to_string()))?;
    let r = selected_repo(a.root.as_deref(), a.repo.as_deref())?;
    let dir = std::fs::canonicalize(&r.path).unwrap_or(r.path);
    let plan = restack_plan::plan(&dir, &req)?;
    if a.json {
        print_json(&plan.to_json());
    } else {
        print!("{}", plan.render_human());
    }
    Ok(if plan.is_clean() {
        exit::OK
    } else {
        exit::FAILED
    })
}

const OPLOG_HELP: &str = "\
New in stw (no Go counterpart); format and rules: docs/oplog-format.md. timeline, evolog and undo-plan are read-only:
nothing is written. `undo` and `hooks install|uninstall` write (see below; `hooks` has its own help). Nothing here installs a hook except `hooks install --yes`. No git program runs, except `git --version` of `undo-plan --keep` and `undo --keep` and the `git reset --keep` of `undo --keep`. The logs are `<git-common-dir>/stw/oplog/refs.log`, `rewrites.log` (written by
the hooks of the spikes `stw-oplog-refhook` and `stw-oplog-hook`) and the reflogs of the repository. A log that is a
symbolic link or not a regular file is not read. Reflogs are read for the files ref backend only.
timeline  A reflog row and the log line of the same change are one entry (flag `covered`); a log line without a reflog
          row (a deleted branch, a tag) is its own entry (`hook-only`). Groups are suspected only. The gaps list says what
          the logs do not show (never a proof of completeness). --ref GLOB filters by ref name (`*` matches any run);
          refs/remotes only with --remotes, other refs than refs/heads, refs/stash, refs/stw only with --all.
evolog    REV is a branch, a revision or a 40-digit commit id; the chain follows the recorded pairs back (--limit N commits).
undo-plan REF is a branch name, a full ref name or HEAD. --to OLD (lower-case hex prefix of an old reflog value) or
          --steps N (default 1) picks the target; --keep plans the undo of the checked-out branch (see undo). Exit 1 when a
          check is refused. It prints the `undo` command to apply.
undo      REF --to OLD --expect NEW [--keep] --yes: the same checks as undo-plan, then a backup ref refs/stw/backup/<run>/1
          on the current value, then the branch is set to OLD only if it still holds NEW (compare-and-swap). Local branches
          only; a branch that is checked out in a worktree is refused unless --keep is given. The newest 10 runs of backup
          refs are kept. Reflog entries (message `stw oplog undo`; with --keep git writes its own) and refs.log lines are written. Without --keep only the
          ref moves: index, working tree, other worktrees and pushes are not covered. The result names the backup ref and
          the way back.
          --keep: the branch must be the one this worktree has checked out (not another worktree's, not a detached HEAD);
          the index and the work tree must be clean (untracked files do not count, but one that the target would overwrite
          refuses, ignored ones too), no merge or other operation may run, no intent-to-add, assume-unchanged or
          skip-worktree entry may exist and no gitlink may differ between the two values. stw then runs `git reset --keep OLD` (the installed git, no hook,
          no --hard); branch, index and work tree move together. A refusal writes nothing.

Exit codes:
  0  done (undo-plan: no check refused)
  1  undo-plan or undo: a check is refused; the update failed; the repository could not be read
  2  usage error";

const OPLOG_HOOKS_HELP: &str = "\
New in stw (no Go counterpart); rules: docs/oplog-format.md, 11. The hooks `post-rewrite` (-> rewrites.log) and
`reference-transaction` (-> refs.log) are the scripts of the spikes; they never make git fail. Opt-in: only this command
installs them, `stw add`, `tidy` and `status` never do. No git program runs, no git config is written.
install    Both hooks into <git-common-dir>/hooks (shared by all linked worktrees), mode 755, plus one `install` line
           in events.log / events-refhook.log. Refused, with nothing written, when core.hooksPath is set (any source,
           also empty), when the hooks directory is a symbolic link, or when a hook of that name exists that is not ours
           (never overwritten or chained; the output gives a chain proposal). Without --yes: the plan, exit 1.
uninstall  Removes a hook file only if it is a regular file whose line 2 is our header; other files are not touched.
           Without --yes: the plan, exit 1. Logs are not deleted.
status     Read-only. A submodule has its own hooks directory: use `--repo NAME`.

Exit codes:
  0  done, already installed, nothing to remove, or status
  1  refused, or the plan without --yes, or a write failed
  2  usage error";

#[derive(Args)]
struct OplogHooksArgs {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
    /// Repository of the workspace (name as printed by stw) [default: the repository of the current directory]
    #[arg(long = "repo", value_name = "NAME")]
    repo: Option<String>,
}

#[derive(Args)]
struct OplogHooksWriteArgs {
    #[command(flatten)]
    common: OplogHooksArgs,
    /// Do it (without it only the plan is printed and nothing is written)
    #[arg(long)]
    yes: bool,
}

#[derive(Args)]
struct OplogTimelineArgs {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// Only refs whose name matches GLOB (`*` matches any run of characters)
    #[arg(long = "ref", value_name = "GLOB")]
    ref_glob: Option<String>,
    /// Also refs/remotes
    #[arg(long)]
    remotes: bool,
    /// Also refs/remotes and every other ref with a reflog
    #[arg(long)]
    all: bool,
    /// Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
    /// Repository of the workspace (name as printed by stw) [default: the repository of the current directory]
    #[arg(long = "repo", value_name = "NAME")]
    repo: Option<String>,
}

#[derive(Args)]
struct OplogEvologArgs {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// Commits of the chain shown at most [default: 200]
    #[arg(long, value_name = "N")]
    limit: Option<usize>,
    /// Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
    /// Repository of the workspace (name as printed by stw) [default: the repository of the current directory]
    #[arg(long = "repo", value_name = "NAME")]
    repo: Option<String>,
    /// A branch, a revision or a commit id
    #[arg(value_name = "REV")]
    rev: String,
}

#[derive(Args)]
struct OplogUndoPlanArgs {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// Plan the undo of the branch this worktree has checked out (`git reset --keep`, clean index and work tree only)
    #[arg(long)]
    keep: bool,
    /// The old value to go back to: a lower-case hex prefix of an old value of the reflog
    #[arg(long, value_name = "OLD", conflicts_with = "steps")]
    to: Option<String>,
    /// Go back this many reflog entries [default: 1]
    #[arg(long, value_name = "N")]
    steps: Option<u64>,
    /// Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
    /// Repository of the workspace (name as printed by stw) [default: the repository of the current directory]
    #[arg(long = "repo", value_name = "NAME")]
    repo: Option<String>,
    /// A branch name, a full ref name or HEAD
    #[arg(value_name = "REF")]
    reference: String,
}

#[derive(Args)]
struct OplogUndoArgs {
    /// Print one JSON object instead of text
    #[arg(long)]
    json: bool,
    /// The old value to go back to: a lower-case hex prefix of an old value of the reflog
    #[arg(long, value_name = "OLD")]
    to: String,
    /// The full current value of the ref; the update happens only if the ref still holds it
    #[arg(long, value_name = "NEW")]
    expect: String,
    /// Undo the branch this worktree has checked out: stw runs `git reset --keep OLD` (clean index and clean work tree
    /// only, untracked files stay); without it a checked-out branch is refused
    #[arg(long)]
    keep: bool,
    /// Do it (without it nothing is written; `undo-plan` shows the checks)
    #[arg(long)]
    yes: bool,
    /// Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]
    #[arg(long, value_name = "PATH")]
    root: Option<PathBuf>,
    /// Repository of the workspace (name as printed by stw) [default: the repository of the current directory]
    #[arg(long = "repo", value_name = "NAME")]
    repo: Option<String>,
    /// A branch name or a full ref name
    #[arg(value_name = "REF")]
    reference: String,
}

fn oplog_ctx(root: Option<&Path>, name: Option<&str>) -> Result<oplog::Ctx> {
    let r = selected_repo(root, name)?;
    let dir = std::fs::canonicalize(&r.path).unwrap_or(r.path);
    oplog::Ctx::open(&dir)
}

fn run_oplog_timeline(a: OplogTimelineArgs) -> Result<u8> {
    let ctx = oplog_ctx(a.root.as_deref(), a.repo.as_deref())?;
    let t = oplog::timeline::build(
        &ctx,
        &oplog::timeline::Options {
            remotes: a.remotes || a.all,
            all: a.all,
            pattern: a.ref_glob,
        },
    );
    if a.json {
        print_json(&t.to_json());
    } else {
        print!("{}", t.render_human());
    }
    Ok(exit::OK)
}

fn run_oplog_evolog(a: OplogEvologArgs) -> Result<u8> {
    let limit = a.limit.unwrap_or(oplog::evolog::DEFAULT_LIMIT);
    if limit == 0 {
        return Err(Error::usage("--limit must be at least 1"));
    }
    let ctx = oplog_ctx(a.root.as_deref(), a.repo.as_deref())?;
    let e = oplog::evolog::evolog(&ctx, &a.rev, limit)?;
    if a.json {
        print_json(&e.to_json());
    } else {
        print!("{}", e.render_human());
    }
    Ok(exit::OK)
}

fn run_oplog_undo_plan(a: OplogUndoPlanArgs) -> Result<u8> {
    if a.steps == Some(0) {
        return Err(Error::usage("--steps must be at least 1"));
    }
    if let Some(to) = &a.to {
        if to.is_empty() || !to.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f')) {
            return Err(Error::usage("--to must be lower-case hex"));
        }
    }
    let ctx = oplog_ctx(a.root.as_deref(), a.repo.as_deref())?;
    let plan = oplog::undo_plan::plan(
        &ctx,
        &oplog::undo_plan::Request {
            reference: a.reference,
            to: a.to,
            steps: a.steps,
            keep: a.keep,
        },
    )?;
    if a.json {
        print_json(&plan.to_json());
    } else {
        print!("{}", plan.render_human());
    }
    Ok(if plan.refused == 0 {
        exit::OK
    } else {
        exit::FAILED
    })
}

fn print_hooks_outcome(o: oplog::hooks::Outcome, json: bool) -> u8 {
    if json {
        print_json(&o.json);
    } else {
        print!("{}", o.text);
    }
    o.exit
}

fn run_oplog_hooks(command: OplogHooksCommand) -> Result<u8> {
    match command {
        OplogHooksCommand::Status(a) => {
            let ctx = oplog_ctx(a.root.as_deref(), a.repo.as_deref())?;
            Ok(print_hooks_outcome(oplog::hooks::status(&ctx)?, a.json))
        }
        OplogHooksCommand::Install(a) => {
            let ctx = oplog_ctx(a.common.root.as_deref(), a.common.repo.as_deref())?;
            Ok(print_hooks_outcome(
                oplog::hooks::install(&ctx, a.yes)?,
                a.common.json,
            ))
        }
        OplogHooksCommand::Uninstall(a) => {
            let ctx = oplog_ctx(a.common.root.as_deref(), a.common.repo.as_deref())?;
            Ok(print_hooks_outcome(
                oplog::hooks::uninstall(&ctx, a.yes)?,
                a.common.json,
            ))
        }
        OplogHooksCommand::Print { name } => {
            print!("{}", oplog::hooks::print(&name)?);
            Ok(exit::OK)
        }
    }
}

fn run_oplog_undo(a: OplogUndoArgs) -> Result<u8> {
    if !a.yes {
        return Err(Error::usage(
            "undo writes: add --yes (`stw oplog undo-plan` shows the checks without writing)",
        ));
    }
    if a.to.is_empty() || !a.to.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f')) {
        return Err(Error::usage("--to must be lower-case hex"));
    }
    let ctx = oplog_ctx(a.root.as_deref(), a.repo.as_deref())?;
    let out = oplog::undo::undo(
        &ctx,
        &oplog::undo::Request {
            reference: a.reference,
            to: a.to,
            expect: a.expect,
            keep: a.keep,
        },
    )?;
    match out {
        oplog::undo::Outcome::Done(d) => {
            if a.json {
                print_json(&d.to_json());
            } else {
                print!("{}", d.render_human());
            }
            Ok(exit::OK)
        }
        oplog::undo::Outcome::Refused(plan) => {
            if a.json {
                let mut v = plan.to_json_as("oplog undo");
                v["executed"] = false.into();
                print_json(&v);
            } else {
                print!(
                    "{}",
                    plan.render_human_as("undo", "refused, nothing was written")
                );
            }
            Ok(exit::FAILED)
        }
    }
}

#[derive(Subcommand)]
enum DevCommand {
    PatchId { repo: PathBuf, revs: Vec<String> },
}

/// Options every command shares.
#[derive(Args, Clone)]
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
    /// A branch whose commits without an equal patch on the base all have a patch at least this similar (percent, 1 to 100)
    /// is `review` with the proof `similar-patches` (never `delete`)
    #[arg(long, value_name = "PCT", default_value_t = tidy_similarity::HIGH_PCT as u8,
          value_parser = clap::value_parser!(u8).range(1..=100), conflicts_with = "no_similarity")]
    similarity: u8,
    /// Lower threshold in percent (1 to 100, not above --similarity): between the two the class is `modified`, below `partial`
    #[arg(long, value_name = "PCT", default_value_t = tidy_similarity::LOW_PCT as u8,
          value_parser = clap::value_parser!(u8).range(1..=100), conflicts_with = "no_similarity")]
    similarity_low: u8,
    /// Do not compare commits by similarity (the plan is as without that stage; saves the time of the comparison)
    #[arg(long)]
    no_similarity: bool,
    /// Do not map the commits of a branch to the base by author, time and subject (no `rebase-identity`, no `landed via`
    /// from the commits; no pull request commit reads)
    #[arg(long)]
    no_rebase_evidence: bool,
    /// Under each branch of the text plan: the evidence that led to the decision, or the evidence that is missing
    /// (read-only; the decision and the JSON are the same, which already holds the data)
    #[arg(long)]
    explain: bool,
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
    /// Fetch the remote-tracking refs of origin (not with --offline) and print what a run would do; change nothing else
    /// (alias: --dry-run)
    #[arg(long, conflicts_with = "apply", overrides_with = "plan")]
    plan: bool,
    /// Alias of --plan
    #[arg(long, conflicts_with = "apply", overrides_with = "dry_run")]
    dry_run: bool,
    /// Accepted and ignored: sync runs by default (kept so that earlier calls still work); not in the help
    #[arg(long, hide = true)]
    apply: bool,
    /// Fetch with --prune: delete remote-tracking refs of origin (refs/remotes/origin/* only) that are gone there.
    /// Overrides fetch.prune. With --plan it prunes as a run does
    #[arg(long, conflicts_with = "no_prune")]
    prune: bool,
    /// Fetch without pruning (overrides fetch.prune for a fetch by the installed git)
    #[arg(long)]
    no_prune: bool,
    /// Rebase a diverged branch (ahead and behind its upstream) onto the fetched upstream, with the installed git: a
    /// conflict stops the rebase as git leaves it. Without --rebase and --merge a diverged branch is only reported.
    /// The Git configuration (pull.rebase, branch.<name>.rebase, rebase.autoStash, ...) is not followed
    #[arg(long, conflicts_with = "merge")]
    rebase: bool,
    /// Merge the fetched upstream into a diverged branch (the only way to get a merge commit; a branch that can be
    /// fast-forwarded is still fast-forwarded), with the installed git
    #[arg(long)]
    merge: bool,
    /// Stash local changes of tracked files around a --rebase or --merge and reapply them (git's autostash); without
    /// it a rebase or merge is refused while tracked files have changes
    #[arg(long, conflicts_with = "no_autostash")]
    autostash: bool,
    /// Never stash (the default): --rebase and --merge refuse a repository with local changes of tracked files
    #[arg(long)]
    no_autostash: bool,
    /// Also move each pinned (detached) submodule to the commit the superproject records when that is a
    /// fast-forward: git submodule sync and update --no-fetch --checkout; never a clone, init or extra fetch. An
    /// uninitialized submodule or a missing commit is reported as unreachable. --plan says what would move
    #[arg(long)]
    submodules: bool,
    /// Also handle the agent-rules checkout (the submodule .agents of a whr workspace): fetch it and
    /// fast-forward HEAD to origin's default branch; with --plan it fetches origin and only reports. Refuses only to protect local
    /// changes or local commits. Never runs install.sh
    #[arg(long)]
    rules: bool,
    /// agent-rules checkout [default: <workspace>/.agents]
    #[arg(long, value_name = "PATH")]
    rules_path: Option<PathBuf>,
    /// With --rules: owner identity `Name <email>` that the author and the committer of every new commit must be
    /// (repeatable) [default: user.name and user.email of the Git configuration of the workspace]. A commit by anyone
    /// else refuses the update
    #[arg(long = "owner-identity", value_name = "NAME <EMAIL>")]
    owner_identity: Vec<String>,
    /// With --rules: a further identity `Name <email>` allowed as committer (not as author), such as the GitHub
    /// rebase-merge committer `<login>@users.noreply.github.com` (repeatable)
    #[arg(long = "allowed-committer", value_name = "NAME <EMAIL>")]
    allowed_committer: Vec<String>,
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
    /// Only these repositories, in one line each with the stack of the current branch and the GitHub counts; with
    /// --json the same document plus `kind`, `depth` and `stack` per repository. A PATH that is a repository (a checkout
    /// or a linked worktree) shows it with its nested submodules; any other directory is scanned below for repositories.
    /// The key of a repository is its path relative to the current directory. Not with --long, --icons, --worktrees,
    /// --review, --stats, --verbose, --repo. Without a PATH: the repositories of the workspace
    #[arg(value_name = "PATH", conflicts_with_all = ["long", "icons", "worktrees", "review", "stats", "verbose"])]
    paths: Vec<PathBuf>,
    /// Only when a PATH is a directory that is scanned: how many levels below it repositories and their submodules are
    /// listed, 0 to 64 [default: 16]; a cut is a notice
    #[arg(long, value_name = "N", value_parser = clap::value_parser!(u32).range(0..=status_all::MAX_DEPTH as i64))]
    depth: Option<u32>,
    /// Only when a PATH is a directory that is scanned: the most repositories of a run, 1 to 1024 [default: 256]; more is
    /// a usage error (exit 2)
    #[arg(long, value_name = "N", value_parser = clap::value_parser!(u32).range(1..=status_all::MAX_LIMIT as i64))]
    limit: Option<u32>,
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
    let mut repos = repo::list_repos_checked(&root)?;
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
    sync_flags: [(&str, bool); 14],
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
    if let Some(id) = &r.run {
        if git_runner::RunId::parse(id).is_none() {
            return Err(Error::usage(
                "--run needs a run ID as printed by stw sync (YYYYMMDDTHHMMSS[uuuuuu]Z-xxxxxxxx)",
            ));
        }
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
        similarity: None,
        rebase: false,
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

/// The workspace of `status PATH..`: the repositories of the paths. The root (the rules checkout, the JSON `root`) is
/// `--root`, else the one `workspace` finds, else the current directory; no workspace is needed.
fn path_workspace(
    c: &Common,
    rules_path: Option<&Path>,
    a: &StatusArgs,
) -> Result<(Workspace, status_all::Resolved)> {
    if !c.repos.is_empty() {
        return Err(Error::usage("--repo does not apply with a PATH"));
    }
    let cwd = std::env::current_dir().unwrap_or_default();
    let root = c
        .root
        .clone()
        .or_else(|| repo::default_root(&cwd))
        .unwrap_or_else(|| cwd.clone());
    if !root.is_dir() {
        return Err(Error::usage(format!(
            "workspace root is not a directory: {}",
            root.display()
        )));
    }
    let root = std::fs::canonicalize(&root).unwrap_or(root);
    let rules = sync::rules::checkout_path(&root, rules_path);
    let resolved = status_all::resolve(
        &a.paths,
        &cwd,
        a.depth.map(|d| d as usize),
        a.limit.map(|l| l as usize),
        &rules,
    )?;
    let ws = Workspace {
        root: root.to_string_lossy().into_owned(),
        repos: resolved.targets.iter().map(|t| t.repo.clone()).collect(),
        rejected: resolved.rejected.clone(),
        github: GithubState::discover(c.offline),
    };
    Ok((ws, resolved))
}

fn run_status(a: StatusArgs) -> Result<u8> {
    let started = std::time::Instant::now();
    let c = a.common.clone();
    if a.paths.is_empty() && (a.depth.is_some() || a.limit.is_some()) {
        let flag = if a.depth.is_some() {
            "--depth"
        } else {
            "--limit"
        };
        return Err(Error::usage(format!(
            "{flag} applies only when a PATH is a directory that is scanned for repositories; no PATH is given"
        )));
    }
    let (ws, res) = if a.paths.is_empty() {
        (workspace(&c, a.rules_path.as_deref())?, None)
    } else {
        let (ws, res) = path_workspace(&c, a.rules_path.as_deref(), &a)?;
        (ws, Some(res))
    };
    let scan = res
        .as_ref()
        .and_then(|r| r.scan)
        .map(|(depth, limit)| status_all::ScanOpts {
            depth,
            limit,
            timeout_secs: a.timeout.as_secs(),
        });
    let root = PathBuf::from(&ws.root);
    let opts = status::StatusOpts {
        worktrees: a.worktrees,
        fetch: a.fetch,
        review: a.review,
        stack: res.is_some(),
    };
    let mut list = match &res {
        Some(r) => repo::par_map(&r.targets, repo::MAX_PARALLEL, |t| {
            status::collect_local(&t.repo, &t.top, &opts, ws.github.client())
        }),
        None => repo::for_each(&ws.repos, |r| {
            status::collect_local(r, &root, &opts, ws.github.client())
        }),
    };
    if let Some(r) = &res {
        for (s, t) in list.iter_mut().zip(&r.targets) {
            s.depth = t.depth;
            s.key = t.key.clone();
            s.path = t.key.clone();
        }
    }
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
    let mut notices =
        status::collect_notices(&ws.github, &rejected_status_notices(&ws.rejected), &list);
    if let Some(r) = &res {
        let depth = scan.map_or(status_all::DEFAULT_DEPTH, |o| o.depth);
        notices.extend(status_all::extra_notices(
            &list, &r.cut, &r.beyond, &r.empty, depth,
        ));
    }
    if c.json {
        let mut v = status::to_json(
            &ws.root,
            &ws.github,
            &list,
            &notices,
            &opts,
            diag.then(|| status_stats::to_json(&stats, &timings)),
        );
        if res.is_some() {
            status_all::add_json(&mut v, &list, scan.as_ref());
        }
        if let Some(Value::Object(o)) = v.get_mut("options") {
            let paths: Vec<String> = a.paths.iter().map(|p| output::p2s(p)).collect();
            o.insert("paths".into(), json!(paths));
        }
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
    } else if res.is_some() {
        let paths: Vec<String> = a.paths.iter().map(|p| output::p2s(p)).collect();
        print!(
            "{}",
            status_all::render(
                &paths,
                &list,
                &notices,
                scan.as_ref(),
                ws.github.note_code()
            )
        );
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

/// The `stw` command as a user types it, for the version error of the git runner (`stw needs Git 2.41 or newer for
/// review carry-over (found 2.40.1)`). A fixed string per command; no argument text.
fn git_command_label(command: &Command) -> &'static str {
    match command {
        Command::Status(a) if a.fetch => "status --fetch",
        Command::Status(_) => "status",
        Command::Add(_) => "add",
        Command::Workspace { .. } => "workspace",
        Command::Sync(SyncArgs {
            sub: Some(SyncCommand::Restore(_)),
            ..
        }) => "sync restore",
        Command::Sync(a) if a.submodules => "sync --submodules",
        Command::Sync(_) => "sync",
        Command::Review {
            command: ReviewCommand::CarryOver(_),
        } => "review carry-over",
        Command::Prove(_) => "prove",
        Command::Absorb(_) => "absorb",
        Command::ReadyCheck(_) => "ready-check",
        Command::Lint {
            command: LintCommand::Review(_),
        } => "lint review",
        // these start no git (gix only); a new command must be named here on purpose
        Command::Tidy { .. }
        | Command::Restack { .. }
        | Command::Oplog { .. }
        | Command::Lint { .. }
        | Command::Dev { .. } => "this step",
    }
}

fn run(command: Command) -> Result<u8> {
    git_runner::shared().set_command(git_command_label(&command));
    let failed = |any: bool| if any { exit::FAILED } else { exit::OK };
    match command {
        Command::Status(a) => run_status(a),
        Command::ReadyCheck(a) => run_ready_check(a),
        Command::Tidy {
            command: TidyCommand::Plan(a),
        } => {
            if a.similarity_low > a.similarity {
                return Err(Error::usage(format!(
                    "--similarity-low ({}) must not be above --similarity ({})",
                    a.similarity_low, a.similarity
                )));
            }
            let ws = workspace(&a.common, None)?;
            let opts = tidy::PlanOpts {
                backup_days: a.backup_days,
                now: tidy::now_secs(),
                similarity: (!a.no_similarity).then(|| tidy::Similarity {
                    high: u32::from(a.similarity),
                    low: u32::from(a.similarity_low),
                }),
                rebase: !a.no_rebase_evidence,
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
                if a.explain {
                    print!("{}", tidy::render_explained(&plans, a.all));
                } else {
                    print!("{}", tidy::render_human(&plans, a.all));
                }
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
        Command::Workspace { command } => crate::workspace::run(command),
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
            rebase,
            merge,
            autostash,
            no_autostash,
            owner_identity,
            allowed_committer,
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
                ("--rebase", rebase),
                ("--merge", merge),
                ("--autostash", autostash),
                ("--no-autostash", no_autostash),
                ("--owner-identity", !owner_identity.is_empty()),
                ("--allowed-committer", !allowed_committer.is_empty()),
            ],
        ),
        Command::Sync(a) => {
            if !a.rules && (!a.owner_identity.is_empty() || !a.allowed_committer.is_empty()) {
                return Err(Error::usage(
                    "--owner-identity and --allowed-committer apply to --rules only",
                ));
            }
            let parse_all = |flag: &str, raw: &[String]| -> Result<Vec<crate::lint_pr::Identity>> {
                raw.iter()
                    .map(|r| {
                        lint_pr_cmd::parse_identity(r).ok_or_else(|| {
                            Error::usage(format!("{flag} {r:?} is not \"Name <email>\""))
                        })
                    })
                    .collect()
            };
            let owners = parse_all("--owner-identity", &a.owner_identity)?;
            let committers = parse_all("--allowed-committer", &a.allowed_committer)?;
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
                diverged: if a.rebase {
                    sync::Diverged::Rebase
                } else if a.merge {
                    sync::Diverged::Merge
                } else {
                    sync::Diverged::Skip
                },
                autostash: if a.autostash {
                    Some(true)
                } else if a.no_autostash {
                    Some(false)
                } else {
                    None
                },
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
                owners,
                committers,
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
                list.iter().any(|s| {
                    matches!(
                        s.result.as_str(),
                        "error" | "conflict" | "autostash-conflict"
                    )
                }) || rules.action == "refused",
            ))
        }
        Command::Review {
            command: ReviewCommand::CarryOver(a),
        } => run_carry_over(a),
        Command::Prove(a) => run_prove(a),
        Command::Absorb(a) => run_absorb(a),
        Command::Restack {
            command: RestackCommand::Plan(a),
        } => run_restack_plan(a),
        Command::Oplog { command } => match command {
            OplogCommand::Timeline(a) => run_oplog_timeline(a),
            OplogCommand::Evolog(a) => run_oplog_evolog(a),
            OplogCommand::UndoPlan(a) => run_oplog_undo_plan(a),
            OplogCommand::Undo(a) => run_oplog_undo(a),
            OplogCommand::Hooks { command } => run_oplog_hooks(command),
        },
        Command::Lint {
            command: LintCommand::Pr(a),
        } => run_lint_pr(a),
        Command::Lint {
            command: LintCommand::Review(a),
        } => run_lint_review(a),
        Command::Lint {
            command: LintCommand::PrText(a),
        } => run_lint_pr_text(a),
        Command::Dev {
            command: DevCommand::PatchId { repo, revs },
        } => {
            dev::patch_ids(&repo, &revs)?;
            Ok(exit::OK)
        }
    }
}
