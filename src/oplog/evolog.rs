//! `stw oplog evolog REV`: the chain of recorded rewrites (amend, rebase) of a commit, from `rewrites.log`
//! (read-only; `docs/oplog-format.md`, 4). The chain semantics are those of the accepted spike `stw-oplog-hook evolog`:
//! `(old, new)` is one edge, the first of equal pairs wins, self pairs are ignored, several old commits with one new
//! commit are a merge, one old with several successors a split, a cycle or a commit seen above stops the chain.
use std::collections::{HashMap, HashSet};
use std::fmt::Write;

use gix::bstr::ByteSlice;
use gix::ObjectId;
use serde_json::{json, Value};

use super::parse::iso_of;
use super::store::{self, Coverage, Oplog};
use super::timeline::SCHEMA_VERSION;
use super::Ctx;
use crate::error::{Error, Result};
use crate::github::reads::escape_untrusted;

pub const DEFAULT_LIMIT: usize = 200;

pub struct Node {
    pub depth: usize,
    pub commit: String,
    pub subject: String,
    /// The kind of the rewrite that made the node above (`-` for the tip).
    pub kind: String,
    pub time: String,
    pub flags: String,
}

pub struct Uncovered {
    pub state: &'static str,
    pub time: String,
    pub commit: String,
    pub message: String,
}

pub struct Evolog {
    pub tip: String,
    pub label: Option<String>,
    pub arg: String,
    pub pairs: usize,
    pub log_exists: bool,
    pub bad: usize,
    pub nodes: Vec<Node>,
    pub capture_errors_log: usize,
    pub capture_gaps_file: usize,
    pub uncovered: Vec<Uncovered>,
    pub pre_install: usize,
    pub compared: usize,
    pub unreadable: Vec<(String, String)>,
}

struct Chain {
    pred: HashMap<String, Vec<String>>,
    edge: HashMap<(String, String), (String, String)>,
    succ: HashMap<String, usize>,
}

type Row = (usize, String, String, String, String);

/// The depth-first walk of the awk original: `done` commits are shown once, `onpath` finds cycles.
struct Walker<'a> {
    chain: &'a Chain,
    limit: usize,
    done: HashSet<String>,
    onpath: HashSet<String>,
    count: usize,
    out: Vec<Row>,
}

impl Walker<'_> {
    fn walk(&mut self, sha: &str, depth: usize, ckind: &str, ctime: &str) {
        let row = |flags: String| {
            (
                depth,
                sha.to_string(),
                ckind.to_string(),
                ctime.to_string(),
                flags,
            )
        };
        if self.onpath.contains(sha) {
            self.out.push(row("cycle".into()));
            return;
        }
        if self.done.contains(sha) {
            self.out.push(row("seen-above".into()));
            return;
        }
        if self.count >= self.limit {
            self.out.push(row("limit".into()));
            return;
        }
        self.count += 1;
        self.done.insert(sha.to_string());
        self.onpath.insert(sha.to_string());
        let preds = self.chain.pred.get(sha).cloned().unwrap_or_default();
        let mut fl = Vec::new();
        if preds.len() > 1 {
            fl.push(format!("merge={}", preds.len()));
        }
        let succ = self.chain.succ.get(sha).copied().unwrap_or(0);
        if succ > 1 {
            fl.push(format!("split={succ}"));
        }
        self.out.push(row(fl.join(",")));
        for q in preds {
            let (t, k) = self.chain.edge[&(q.clone(), sha.to_string())].clone();
            self.walk(&q, depth + 1, &k, &t);
        }
        self.onpath.remove(sha);
    }
}

/// A reflog entry the post-rewrite hook should have seen: an amend or the end of a rebase.
fn is_rewrite(msg: &str) -> bool {
    msg.starts_with("commit (amend)") || (msg.contains("rebase") && msg.contains("(finish)"))
}

pub fn evolog(ctx: &Ctx, arg: &str, limit: usize) -> Result<Evolog> {
    let repo = ctx.repo()?;
    let tip: ObjectId = match repo
        .rev_parse_single(arg)
        .ok()
        .and_then(|id| id.object().ok())
        .and_then(|o| o.peel_to_commit().ok())
    {
        Some(c) => c.id,
        None => {
            let hex =
                arg.len() == 40 && arg.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'));
            match (hex, ObjectId::from_hex(arg.as_bytes())) {
                (true, Ok(id)) => id,
                _ => {
                    return Err(Error::usage(format!(
                        "cannot resolve '{}' to a commit",
                        escape_untrusted(arg, 200).0
                    )))
                }
            }
        }
    };
    let tip_s = tip.to_string();
    // the ref whose reflog is compared: a local branch, or the branch HEAD points at
    let exists = |n: &str| matches!(repo.try_find_reference(n), Ok(Some(_)));
    let label = if exists(&format!("refs/heads/{arg}")) {
        Some(format!("refs/heads/{arg}"))
    } else if arg.starts_with("refs/heads/") && exists(arg) {
        Some(arg.to_string())
    } else if arg == "HEAD" {
        repo.head_name()
            .ok()
            .flatten()
            .map(|n| n.as_bstr().to_string())
    } else {
        None
    };

    let oplog = Oplog::new(&ctx.common);
    let log = oplog.rewrites();
    let mut chain = Chain {
        pred: HashMap::new(),
        edge: HashMap::new(),
        succ: HashMap::new(),
    };
    let mut seen: HashSet<(String, String)> = HashSet::new();
    let mut news: HashSet<String> = HashSet::new();
    for l in &log.lines {
        news.insert(l.new.clone());
        if l.old == l.new || !seen.insert((l.old.clone(), l.new.clone())) {
            continue;
        }
        chain
            .pred
            .entry(l.new.clone())
            .or_default()
            .push(l.old.clone());
        chain.edge.insert(
            (l.old.clone(), l.new.clone()),
            (l.time.clone(), l.kind.clone()),
        );
        *chain.succ.entry(l.old.clone()).or_default() += 1;
    }
    let mut w = Walker {
        chain: &chain,
        limit,
        done: HashSet::new(),
        onpath: HashSet::new(),
        count: 0,
        out: Vec::new(),
    };
    w.walk(&tip_s, 0, "-", "-");
    let nodes = w
        .out
        .into_iter()
        .map(|(depth, commit, kind, time, flags)| Node {
            depth,
            subject: subject(repo, &commit),
            commit,
            kind,
            time,
            flags,
        })
        .collect();

    // the reflog of the branch against the hook's pairs
    let window = oplog.window("events.log");
    let (mut uncovered, mut pre, mut compared) = (Vec::new(), 0usize, 0usize);
    if let Some(l) = &label {
        if let Some(rows) = store::read_reflog_file(&ctx.common.join("logs").join(l)) {
            for r in rows.iter().filter(|r| is_rewrite(&r.message)) {
                compared += 1;
                if news.contains(&r.new) {
                    continue;
                }
                let state = match window.as_ref().map(|w| w.at(r.epoch)) {
                    None => "unknown-window",
                    Some(Coverage::PreInstall) => {
                        pre += 1;
                        continue;
                    }
                    Some(Coverage::Expected) => "uncovered",
                    Some(Coverage::HookAbsent) => "hook-absent",
                };
                uncovered.push(Uncovered {
                    state,
                    time: iso_of(r.epoch),
                    commit: r.new.clone(),
                    message: escape_untrusted(&r.message, 200).0,
                });
            }
        }
    }
    let (gap_lines, gap_unreadable) = oplog.gap_lines();
    let post_rewrite = |k: &str| matches!(k, "amend" | "rebase" | "other");
    let mut unreadable = log.unreadable.clone();
    unreadable.extend(gap_unreadable);
    Ok(Evolog {
        tip: tip_s,
        label,
        arg: arg.to_string(),
        pairs: seen.len(),
        log_exists: log.exists,
        bad: log.bad,
        nodes,
        capture_errors_log: gap_lines
            .iter()
            .filter(|(k, f, _)| !f && post_rewrite(k))
            .count(),
        capture_gaps_file: gap_lines
            .iter()
            .filter(|(k, f, _)| *f && post_rewrite(k))
            .count(),
        uncovered,
        pre_install: pre,
        compared,
        unreadable,
    })
}

fn subject(repo: &gix::Repository, sha: &str) -> String {
    let gone = || "object gone".to_string();
    let Ok(id) = ObjectId::from_hex(sha.as_bytes()) else {
        return gone();
    };
    match repo.find_commit(id) {
        Ok(c) => match c.message() {
            Ok(m) => escape_untrusted(&m.title.to_str_lossy(), 200).0,
            Err(_) => String::new(),
        },
        Err(_) => gone(),
    }
}

fn flag_text(f: &str) -> String {
    f.split(',')
        .map(|p| match p {
            "seen-above" => "seen above".to_string(),
            "cycle" => "cycle: chain stops".to_string(),
            "limit" => "limit reached: chain stops".to_string(),
            p if p.starts_with("merge=") => {
                format!("merge: {} predecessors (fixup/squash)", &p[6..])
            }
            p if p.starts_with("split=") => {
                format!("split: rewritten into {} different commits", &p[6..])
            }
            p => p.to_string(),
        })
        .collect::<Vec<_>>()
        .join(", ")
}

impl Evolog {
    pub fn to_json(&self) -> Value {
        let nodes: Vec<Value> = self
            .nodes
            .iter()
            .map(|n| {
                json!({
                    "depth": n.depth, "commit": n.commit, "subject": n.subject,
                    "rewritten_by": escape_untrusted(&n.kind, 40).0, "time": n.time, "flags": n.flags,
                })
            })
            .collect();
        let count = |s: &str| self.uncovered.iter().filter(|u| u.state == s).count();
        let unreadable: Vec<Value> = self
            .unreadable
            .iter()
            .map(|(n, why)| json!({ "file": n, "text": format!("not read: {}", escape_untrusted(why, 200).0) }))
            .collect();
        json!({
            "schema_version": SCHEMA_VERSION, "command": "oplog evolog", "engine": crate::output::engine(),
            "source": "hook", "tip": self.tip, "ref": self.label, "pairs": self.pairs, "nodes": nodes,
            "gaps": {
                "capture_errors_log": self.capture_errors_log, "capture_gaps_file": self.capture_gaps_file,
                "uncovered_rewrites": count("uncovered"), "hook_absent": count("hook-absent"),
                "unknown_window": count("unknown-window"), "pre_install": self.pre_install,
                "compared_with_reflog": self.label, "bad_lines": self.bad,
                "log_exists": self.log_exists, "unreadable": unreadable,
            },
        })
    }

    pub fn render_human(&self) -> String {
        let mut b = String::new();
        let _ = writeln!(
            b,
            "evolog  {}  tip={}",
            escape_untrusted(self.label.as_deref().unwrap_or(&self.arg), 200).0,
            self.tip
        );
        let _ = writeln!(
            b,
            "source  hook  rewrites.log ({} pairs recorded{})",
            self.pairs,
            if self.log_exists {
                ""
            } else {
                "; file does not exist: hook never ran or is not installed"
            }
        );
        for n in &self.nodes {
            let ind = "  ".repeat(n.depth);
            let short = &n.commit[..7.min(n.commit.len())];
            if n.depth == 0 {
                let _ = write!(b, "{ind}{short}  {}", n.subject);
            } else {
                let _ = write!(
                    b,
                    "{ind}<- {short}  {}  ({} at {})",
                    n.subject,
                    escape_untrusted(&n.kind, 40).0,
                    n.time
                );
            }
            if !n.flags.is_empty() {
                let _ = write!(b, "  [{}]", flag_text(&n.flags));
            }
            b.push('\n');
        }
        if self.nodes.len() <= 1 {
            let _ = writeln!(b, "(no recorded predecessor: the commit was never rewritten by a recorded amend/rebase, or the rewrite was not seen)");
        }
        let _ = writeln!(b, "Gaps");
        for (n, why) in &self.unreadable {
            let _ = writeln!(b, "  unreadable      {n}: not read: {why}");
        }
        if self.bad > 0 {
            let _ = writeln!(
                b,
                "  bad             {} lines of rewrites.log skipped (never shown, never deleted)",
                self.bad
            );
        }
        if self.capture_errors_log > 0 {
            let _ = writeln!(b, "  capture-gap     {} gap line(s) in errors.log: the hook failed to write; rewrites around them may be missing", self.capture_errors_log);
        }
        if self.capture_gaps_file > 0 {
            let _ = writeln!(b, "  capture-gap     {} gap line(s) in stw-oplog-gaps (the log directory itself was not writable)", self.capture_gaps_file);
        }
        for u in &self.uncovered {
            let why = match u.state {
                "uncovered" => "reflog shows it, the hook recorded no pair",
                "hook-absent" => "while the hook was uninstalled",
                _ => "no install record, so no hook entry could be expected (window unknown)",
            };
            let _ = writeln!(
                b,
                "  uncovered-rewrite  {}  {}  \"{}\": {why}",
                u.time,
                &u.commit[..7.min(u.commit.len())],
                u.message
            );
        }
        if self.pre_install > 0 {
            let _ = writeln!(b, "  pre-install     {} rewrite(s) in the reflog before the hook was installed: not in the evolog, not a capture gap", self.pre_install);
        }
        match &self.label {
            None => {
                let _ = writeln!(b, "  coverage        not compared with a reflog ('{}' is not a branch): pass a branch name", escape_untrusted(&self.arg, 200).0);
            }
            Some(l) => {
                let _ = writeln!(b, "  coverage        compared {} rewrite entries of the reflog of {} with the hook log", self.compared, escape_untrusted(l, 200).0);
            }
        }
        let _ = writeln!(b, "  note            a rewrite that kept the tip can show as uncovered; the hook does not see tags, branch deletion, reset, cherry-pick, merge, stash, or rewrites by tools that run no hooks (gix)");
        b
    }
}
