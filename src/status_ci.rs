//! The `ci-main` source of `stw status`: the CI state of the head of the default branch of a repository with a
//! GitHub origin. Port of the Go source `ci-main` (`statusremote.ReadCIMain`): read the default branch and its head,
//! read the check runs and the commit statuses of that head and give the one class `none`, `pending`, `green` or
//! `red` (the classification of `stw ready-check`, `ready_check::check_class`, no second rule).
//!
//! New against Go: a token without the permission for check runs (fine-grained tokens have no `Checks`) gets 403 on
//! `check-runs`. Then the runs of Actions workflows of the head (`actions/runs?head_sha=`, `Actions: read`) stand in
//! for the check runs: per workflow and event the newest run counts. Which way was taken is `source`; the choice is
//! made by the answer of GitHub only, never by the kind of the token. When both are refused the read is `forbidden`.
//! Read-only (GET), bounded by the `Budget` of the list reads.
use std::collections::BTreeMap;

use crate::github::reads::{Budget, CheckRun, Denial, Read, WorkflowRun};
use crate::github::Client;
use crate::ready_check::check_class;

/// Where the runs of the class came from.
pub const VIA_CHECK_RUNS: &str = "check-runs";
pub const VIA_ACTIONS_RUNS: &str = "actions-runs";

/// What `stw status` says about both rights when no way to read the CI was left.
pub const NEEDS_RIGHTS: &str =
    "forbidden: the token can read neither check runs nor Actions runs; it needs Checks: Read (classic token: scope repo) or Actions: Read";

/// The CI of the head of the default branch.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CiMain {
    pub branch: String,
    /// The full commit id of the head.
    pub head: String,
    /// `none` (no check, no run, no status), `pending`, `green` or `red`.
    pub class: &'static str,
    /// `check-runs` or `actions-runs`.
    pub source: &'static str,
}

impl CiMain {
    /// `via check runs` or `via Actions runs`, for the text.
    pub fn via_text(&self) -> &'static str {
        if self.source == VIA_ACTIONS_RUNS {
            "via Actions runs"
        } else {
            "via check runs"
        }
    }
}

/// A list that was read completely, else why it is not usable. A denial is the second half of the `Err`.
fn whole<T>(what: &str, r: Read<T>) -> Result<Vec<T>, Gap> {
    match r {
        Read::Observed(o) if o.incomplete.is_none() => Ok(o.items),
        Read::Observed(o) => Err(Gap::Other(format!(
            "{what} read incompletely ({})",
            o.incomplete.unwrap_or_default()
        ))),
        Read::Denied {
            kind: Denial::Forbidden,
            ..
        } => Err(Gap::Forbidden),
        Read::Denied { message, .. } => Err(Gap::Other(message)),
        Read::NotFound => Err(Gap::Other(format!("{what}: not found"))),
        Read::Failed(m) => Err(Gap::Other(m)),
    }
}

/// Why a list is missing: the token may not read it (the one case that has a fallback) or something else.
enum Gap {
    Forbidden,
    Other(String),
}

/// The newest run of every workflow and event of `sha`, as the check runs the classification takes. Runs of another
/// commit are ignored. Newest: `created_at`, then `run_attempt`, then `id`.
pub fn latest_runs(runs: &[WorkflowRun], sha: &str) -> Vec<CheckRun> {
    let mut newest: BTreeMap<(u64, &str), &WorkflowRun> = BTreeMap::new();
    for r in runs.iter().filter(|r| r.head_sha.as_deref() == Some(sha)) {
        let key = (r.workflow_id, r.event.as_str());
        let rank = |r: &WorkflowRun| (r.created_at.clone(), r.run_attempt, r.id);
        if newest.get(&key).is_none_or(|old| rank(r) > rank(old)) {
            newest.insert(key, r);
        }
    }
    newest
        .into_values()
        .map(|r| CheckRun {
            id: r.id,
            name: format!("{} ({})", r.name, r.event),
            status: r.status.clone(),
            conclusion: r.conclusion.clone(),
            head_sha: r.head_sha.clone(),
            app: None,
            started_at: None,
            completed_at: None,
        })
        .collect()
}

/// Read the CI of the default branch of `slug`. The `Err` is the cause for the notice, already cut to one line.
pub fn read_ci_main(c: &Client, slug: &str) -> Result<CiMain, String> {
    let (branch, head) = c.default_branch_head(slug).map_err(|e| e.to_string())?;
    let budget = Budget::default();
    let direct = c
        .check_runs(slug, &head, &budget)
        .map_err(|e| e.to_string())?;
    let (runs, source) = match whole("check runs", direct) {
        Ok(runs) => (runs, VIA_CHECK_RUNS),
        Err(Gap::Other(m)) => return Err(m),
        Err(Gap::Forbidden) => {
            let actions = c
                .workflow_runs(slug, &head, &budget)
                .map_err(|e| e.to_string())?;
            match whole("Actions runs", actions) {
                Ok(runs) => (latest_runs(&runs, &head), VIA_ACTIONS_RUNS),
                Err(Gap::Forbidden) => return Err(NEEDS_RIGHTS.into()),
                Err(Gap::Other(m)) => return Err(m),
            }
        }
    };
    let statuses = c
        .commit_statuses(slug, &head, &budget)
        .map_err(|e| e.to_string())?;
    let statuses = match whole("commit statuses", statuses) {
        Ok(s) => s,
        Err(Gap::Forbidden) => {
            return Err("forbidden: the token cannot read commit statuses".into())
        }
        Err(Gap::Other(m)) => return Err(m),
    };
    let class = check_class(&runs, &statuses)?;
    Ok(CiMain {
        branch,
        head,
        class,
        source,
    })
}
