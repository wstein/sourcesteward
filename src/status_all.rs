//! `stw status --all`: every repository of the workspace (the root and the nested submodules, depth-first) in one line
//! each, with the stack of the current branch. The rows are those of `stw status` (same `RepoStatus`, same attention
//! reasons, same notices); this module adds the stack, the one-line text and the additive JSON fields. Read-only.
use serde_json::{json, Value};

use crate::repo;
use crate::status::{self, Notice, RepoStatus, Source};

/// Default and bounds of `--depth` (how deep below the root submodules are listed; 0 is the root only).
pub const DEFAULT_DEPTH: usize = 16;
pub const MAX_DEPTH: usize = 64;
/// Bounds of `--limit` (the repositories of a run); the default is `repo::REPO_LIMIT`.
pub const MAX_LIMIT: usize = 1024;

/// What `--all` was asked for.
#[derive(Clone, Copy, Debug)]
pub struct AllOpts {
    pub depth: usize,
    pub limit: usize,
    /// The overall deadline of the GitHub reads (`--timeout`), whole seconds.
    pub timeout_secs: u64,
}

/// The stack of the current branch: the chain of local upstreams (`branch.<b>.remote` is `.` and `branch.<b>.merge` is
/// `refs/heads/<parent>`), the top-most local parent first, the current branch last. Upstream links only (README).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct StackFacts {
    /// None when the current branch has no local parent.
    pub chain: Option<Vec<String>>,
    /// Branches of the repository that have a local parent.
    pub links: usize,
    /// The branch at which the chain met itself.
    pub cycle_at: Option<String>,
}

/// The branches in a chain are followed at most this far (as the spike).
const CHAIN_LIMIT: usize = 64;

/// The local parent of `branch`, if it has one.
fn local_parent(repo: &gix::Repository, branch: &str) -> Option<String> {
    let cfg = repo.config_snapshot();
    let remote = cfg.string(format!("branch.{branch}.remote").as_str())?;
    if remote.as_slice() != b"." {
        return None;
    }
    let merge = cfg.string(format!("branch.{branch}.merge").as_str())?;
    let merge = String::from_utf8_lossy(merge.as_slice()).into_owned();
    merge
        .strip_prefix("refs/heads/")
        .filter(|p| !p.is_empty())
        .map(str::to_string)
}

pub fn stack_of(repo: &gix::Repository, branch: &str) -> StackFacts {
    let links = repo::local_branches(repo)
        .map(|b| {
            b.iter()
                .filter(|(n, _)| local_parent(repo, n).is_some())
                .count()
        })
        .unwrap_or(0);
    let mut chain = vec![branch.to_string()];
    let mut seen = vec![branch.to_string()];
    let mut cur = branch.to_string();
    let mut cycle_at = None;
    for _ in 0..CHAIN_LIMIT {
        let Some(parent) = local_parent(repo, &cur) else {
            break;
        };
        chain.insert(0, parent.clone());
        if seen.contains(&parent) {
            cycle_at = Some(parent);
            break;
        }
        seen.push(parent.clone());
        cur = parent;
    }
    StackFacts {
        chain: (chain.len() > 1).then_some(chain),
        links,
        cycle_at,
    }
}

/// The notices only `--all` has: a stack that meets itself, and the repositories whose submodules the depth bound left
/// out (`cut`: their names, as `repo::list_repos_depth` gives them).
pub fn extra_notices(list: &[RepoStatus], cut: &[String], depth: usize) -> Vec<Notice> {
    let mut v = Vec::new();
    for s in list {
        if let Some(at) = s.stack.as_ref().and_then(|k| k.cycle_at.as_deref()) {
            let name = status::escape_for_text(&s.name);
            v.push(Notice {
                code: "stack-cycle",
                source: "stack",
                repos: vec![name.clone()],
                message: format!("{name}: stack cycle at {}", status::escape_for_text(at)),
                count: 1,
            });
        }
    }
    for c in cut {
        let name = status::escape_for_text(c);
        v.push(Notice {
            code: "depth-limit",
            source: "submodules",
            repos: vec![name.clone()],
            message: format!("{name}: submodules below depth {depth} not listed"),
            count: 1,
        });
    }
    v
}

/// The additive fields of `--all` on the document of `status::to_json`: `options.all/depth/limit/timeout` and per
/// repository `kind`, `depth`, `stack`. Nothing else changes.
pub fn add_json(doc: &mut Value, list: &[RepoStatus], all: &AllOpts) {
    if let Some(Value::Object(o)) = doc.get_mut("options") {
        o.insert("all".into(), json!(true));
        o.insert("depth".into(), json!(all.depth));
        o.insert("limit".into(), json!(all.limit));
        o.insert("timeout".into(), json!(all.timeout_secs));
    }
    let Some(Value::Array(repos)) = doc.get_mut("repos") else {
        return;
    };
    for (r, s) in repos.iter_mut().zip(list) {
        let Value::Object(m) = r else { continue };
        m.insert(
            "kind".into(),
            json!(if s.depth == 0 { "root" } else { "submodule" }),
        );
        m.insert("depth".into(), json!(s.depth));
        m.insert(
            "stack".into(),
            s.stack
                .as_ref()
                .map_or(Value::Null, |k| json!({"chain": k.chain, "links": k.links})),
        );
    }
}

/// Why a GitHub read failed, as one word (the Rust client has no typed error: the class is read from its message).
pub fn classify(message: &str) -> &'static str {
    if message.starts_with("deadline:") {
        return "timeout";
    }
    // only the start of the message is read: the rest names the request path (a slug can contain any word)
    let Some(m) = message.strip_prefix("GitHub API ") else {
        return "error";
    };
    if m.starts_with("HTTP 401") {
        "unauthorized"
    } else if m.starts_with("HTTP 403") {
        "forbidden"
    } else if m.starts_with("HTTP 404") {
        "not-found"
    } else if m.starts_with("HTTP 429") {
        "rate-limited"
    } else if m.starts_with("sent unreadable JSON") || m.starts_with("sent an unexpected") {
        "unreadable"
    } else if m.starts_with("unreachable") {
        "unreachable"
    } else {
        "error"
    }
}

fn number(s: Source) -> String {
    match s {
        Source::Read {
            open, truncated, ..
        } => format!("{open}{}", if truncated { "+" } else { "" }),
        Source::Failed => "?".into(),
        Source::NotRead | Source::NoRemote => "n/a".into(),
    }
}

/// `PRs <n> | Issues <n>`, with the reason in brackets when one of them is not a number.
fn github_cell(s: &RepoStatus, note_code: Option<&str>) -> String {
    let c = status::counts_of(s);
    let mut cell = format!("PRs {} | Issues {}", number(c.prs), number(c.issues));
    let reason = match (c.prs, c.issues) {
        (Source::Failed, _) => s.pr_error.as_deref().map(classify),
        (_, Source::Failed) => s.issue_error.as_deref().map(classify),
        (Source::NoRemote, _) => Some("no-github-origin"),
        (Source::NotRead, _) | (_, Source::NotRead) => Some(note_code.unwrap_or("not-read")),
        _ => None,
    };
    if let Some(r) = reason {
        cell += &format!(" ({r})");
    }
    cell
}

fn sync_cell(s: &RepoStatus) -> String {
    let f = &s.facts;
    if f.upstream.gone {
        "upstream gone".into()
    } else if f.head.is_none() {
        "(unborn)".into()
    } else if f.branch.is_none() {
        "-".into()
    } else if let Some(u) = &f.upstream.short {
        format!(
            "{} +{}/-{}",
            status::escape_for_text(u),
            f.ahead.unwrap_or(0),
            f.behind.unwrap_or(0)
        )
    } else {
        "no upstream".into()
    }
}

fn dirty_cell(s: &RepoStatus) -> String {
    let c = &s.changes;
    let parts: Vec<String> = [
        (c.conflicts, "conflicts"),
        (c.staged, "staged"),
        (c.unstaged, "unstaged"),
        (c.untracked, "untracked"),
    ]
    .iter()
    .filter(|(n, _)| *n > 0)
    .map(|(n, w)| format!("{n} {w}"))
    .collect();
    if parts.is_empty() {
        "clean".into()
    } else {
        parts.join(" ")
    }
}

/// A total is a number only when every repository with a GitHub origin gave one (the JSON `totals` has the state).
fn total_cell(s: Source) -> String {
    match s {
        Source::Read { .. } => number(s),
        _ => "n/a".into(),
    }
}

/// The text of `status --all`; `note_code` is the code of the GitHub note of the run (`offline`, `no-token`).
pub fn render(
    root: &str,
    list: &[RepoStatus],
    notices: &[Notice],
    all: &AllOpts,
    note_code: Option<&str>,
) -> String {
    let mut out = format!(
        "status --all  root={}  repositories={}  depth<={}\n\n",
        status::escape_for_text(root),
        list.len(),
        all.depth
    );
    out += &format!("Repositories ({})\n", list.len());
    let mut attention = 0;
    let (mut ahead, mut behind) = (0, 0);
    for s in list {
        let mark = if status::reasons(s).is_empty() {
            ' '
        } else {
            attention += 1;
            '!'
        };
        let name = status::escape_for_text(&s.name);
        if let Some(e) = &s.error {
            out += &format!("{mark} {name:<28} ({})\n", status::escape_for_text(e));
            continue;
        }
        ahead += s.facts.ahead.unwrap_or(0);
        behind += s.facts.behind.unwrap_or(0);
        let branch = match &s.facts.branch {
            Some(b) => status::escape_for_text(b),
            None => format!(
                "({})",
                s.facts
                    .head
                    .map(|h| crate::output::short(&h.to_string()).to_string())
                    .unwrap_or_default()
            ),
        };
        let chain = s
            .stack
            .as_ref()
            .and_then(|k| k.chain.as_ref())
            .map(|c| {
                c.iter()
                    .map(|b| status::escape_for_text(b))
                    .collect::<Vec<_>>()
                    .join("<-")
            })
            .unwrap_or_else(|| "-".into());
        out += &format!(
            "{mark} {name:<28} {branch}  {}  {} stack {chain}  {}\n",
            sync_cell(s),
            dirty_cell(s),
            github_cell(s, note_code)
        );
    }
    let t = status::totals(list);
    out += &format!(
        "\nTotals  PRs {} | Issues {} | ahead {ahead} | behind {behind}\n",
        total_cell(t.prs),
        total_cell(t.issues)
    );
    if !notices.is_empty() {
        out += &format!("\nNotices ({})\n", notices.len());
        for n in notices {
            out += &format!("  {}\n", n.message);
        }
    }
    out += &format!(
        "\n{attention} of {} repositories need attention\n",
        list.len()
    );
    out
}
