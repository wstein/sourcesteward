//! `stw oplog timeline`: the reflogs and `refs.log` as one list of ref changes (read-only; `docs/oplog-format.md`, 3, 7).
//!
//! A reflog row and the `refs.log` line of the same change (same ref, same new value) are one entry (origin `reflog`,
//! flag `covered`). A `refs.log` line that no reflog row has (a deleted branch, a tag) is its own entry (origin `hook`
//! or `stw`, flag `hook-only`). Groups are suspected only. Gaps are always printed.
use std::collections::{HashMap, HashSet};
use std::fmt::Write;
use std::path::PathBuf;

use serde_json::{json, Value};

use super::parse::{self, is_zero, RefLine};
use super::store::{self, Coverage, Oplog};
use crate::github::reads::escape_untrusted;

/// `schema_version` of the op log JSON documents.
pub const SCHEMA_VERSION: u32 = 1;
const NAME_CAP: usize = 300;

pub struct Options {
    pub remotes: bool,
    pub all: bool,
    pub pattern: Option<String>,
}

pub struct Entry {
    pub epoch: i64,
    pub time: String,
    pub refname: String,
    pub old: String,
    pub new: String,
    pub origin: &'static str,
    pub message: String,
    pub flag: &'static str,
    pub group: Option<usize>,
    order: usize,
}

pub struct Gap {
    pub kind: &'static str,
    pub refname: String,
    pub text: String,
}

pub struct Timeline {
    pub common: PathBuf,
    pub entries: Vec<Entry>,
    /// Lines of `refs.log` with a well-formed shape and a context this version does not know.
    pub other_lines: Vec<RefLine>,
    pub gaps: Vec<Gap>,
}

/// The lines of `refs.log` after the markers are joined to their lines (spec 3.3): `pre-old` gives the old value of the
/// next `ref-transaction` line of the ref when git reported zeros, `pre-same` hides the next `0 -> value` line, a second
/// `0 0` line of an already deleted ref is dropped. Lines of unknown contexts go to `other`.
pub struct Joined {
    pub rows: Vec<Row>,
    pub other: Vec<RefLine>,
}

pub struct Row {
    pub line: RefLine,
    pub refname: String,
    pub old: String,
    pub old_unknown: bool,
}

pub fn display_name(encoded: &str) -> String {
    match parse::decode_name(encoded) {
        Some(b) => String::from_utf8_lossy(&b).into_owned(),
        None => encoded.to_string(),
    }
}

pub fn join_markers(lines: Vec<RefLine>) -> Joined {
    #[derive(Clone, Copy, PartialEq)]
    enum Kind {
        Old,
        Same,
    }
    let mut markers: HashMap<String, (Kind, String)> = HashMap::new();
    let mut last_new: HashMap<String, String> = HashMap::new();
    let mut out = Joined {
        rows: Vec::new(),
        other: Vec::new(),
    };
    for line in lines {
        let name = display_name(&line.refname);
        match line.context.as_str() {
            "pre-old" => {
                markers.insert(name, (Kind::Old, line.old));
            }
            "pre-same" => {
                markers.insert(name, (Kind::Same, line.old));
            }
            "ref-transaction" | "stw" => {
                let mut old = line.old.clone();
                let mut old_unknown = false;
                let marker = if line.context == "ref-transaction" {
                    markers.remove(&name)
                } else {
                    None
                };
                match marker {
                    Some((Kind::Same, _)) => {
                        if is_zero(&old) && !is_zero(&line.new) {
                            last_new.insert(name, line.new.clone());
                            continue;
                        }
                    }
                    Some((Kind::Old, mo)) => {
                        if is_zero(&old) {
                            old = mo;
                        }
                    }
                    None => {
                        if line.context == "ref-transaction" && is_zero(&old) && is_zero(&line.new)
                        {
                            if last_new.get(&name).is_some_and(|n| is_zero(n)) {
                                continue;
                            }
                            old_unknown = true;
                        }
                    }
                }
                last_new.insert(name.clone(), line.new.clone());
                out.rows.push(Row {
                    line,
                    refname: name,
                    old,
                    old_unknown,
                });
            }
            _ => out.other.push(line),
        }
    }
    out
}

fn hook_ref(name: &str) -> bool {
    // refs the hook does not log at all (spec 3.4)
    !["refs/bisect/", "refs/worktree/", "refs/rewritten/"]
        .iter()
        .any(|p| name.starts_with(p))
}

pub fn build(ctx: &super::Ctx, opts: &Options) -> Timeline {
    let oplog = Oplog::new(&ctx.common);
    let mut gaps: Vec<Gap> = Vec::new();
    let gap = |gaps: &mut Vec<Gap>, kind: &'static str, r: &str, text: String| {
        gaps.push(Gap {
            kind,
            refname: r.to_string(),
            text,
        })
    };
    let remotes_on = oplog.remotes_on();

    // refs.log
    let log = oplog.refs();
    for (n, why) in &log.unreadable {
        gap(&mut gaps, "unreadable", n, format!("not read: {why}"));
    }
    if log.bad > 0 {
        gap(
            &mut gaps,
            "bad",
            "refs.log",
            format!(
                "{} lines skipped (wrong shape, hash, time, name or a glued line); never shown, never deleted",
                log.bad
            ),
        );
    }
    let joined = join_markers(log.lines);
    let hook_set: HashSet<(String, String)> = joined
        .rows
        .iter()
        .map(|r| (r.refname.clone(), r.line.new.clone()))
        .collect();
    let heads_new: HashSet<&str> = joined
        .rows
        .iter()
        .filter(|r| r.refname.starts_with("refs/heads/"))
        .map(|r| r.line.new.as_str())
        .collect();
    let window = oplog.window("events-refhook.log");

    // reflog rows
    let mut entries: Vec<Entry> = Vec::new();
    let mut order = 0usize;
    let mut reflog_keys: HashSet<(String, String)> = HashSet::new();
    let backend = super::config_value(&ctx.common, "extensions.refStorage");
    let files_backend = backend
        .as_deref()
        .is_none_or(|b| b.eq_ignore_ascii_case("files"));
    if !files_backend {
        gap(
            &mut gaps,
            "backend",
            "-",
            format!(
                "extensions.refStorage={}: reflogs are not stored as files; this version reads only the files backend, so no reflog row is shown and the coverage of the hook is UNKNOWN",
                escape_untrusted(backend.as_deref().unwrap_or(""), 40).0
            ),
        );
    } else {
        let mut counts = [0usize; 3];
        for f in store::reflog_files(&ctx.common, opts.remotes, opts.all) {
            if opts
                .pattern
                .as_deref()
                .is_some_and(|p| !store::glob(p, &f.label))
            {
                continue;
            }
            let Some(rows) = store::read_reflog_file(&f.path) else {
                gap(
                    &mut gaps,
                    "unreadable",
                    &display(&f.label),
                    "reflog not read (not a regular file)".into(),
                );
                continue;
            };
            if rows.is_empty() {
                gap(
                    &mut gaps,
                    "empty-reflog",
                    &display(&f.label),
                    "reflog is empty or has no readable line".into(),
                );
                continue;
            }
            if !is_zero(&rows[0].old) {
                gap(
                    &mut gaps,
                    "truncated",
                    &display(&f.label),
                    format!(
                        "reflog does not start at the creation of the ref (oldest entry {}): earlier entries are missing (expired, truncated, or logging was off)",
                        parse::iso_of(rows[0].epoch)
                    ),
                );
            }
            for r in rows {
                order += 1;
                let covered = hook_set.contains(&(f.refname.clone(), r.new.clone()))
                    || (f.refname == "HEAD" && heads_new.contains(r.new.as_str()));
                let expected =
                    hook_ref(&f.refname) && (!f.refname.starts_with("refs/remotes/") || remotes_on);
                let flag = if covered {
                    "covered"
                } else if !expected {
                    "not-expected"
                } else {
                    match window.as_ref().map(|w| w.at(r.epoch)) {
                        None => "unknown-window",
                        Some(Coverage::PreInstall) => {
                            counts[0] += 1;
                            "pre-install"
                        }
                        Some(Coverage::Expected) => {
                            counts[1] += 1;
                            "uncovered"
                        }
                        Some(Coverage::HookAbsent) => {
                            counts[2] += 1;
                            "hook-absent"
                        }
                    }
                };
                reflog_keys.insert((f.refname.clone(), r.new.clone()));
                entries.push(Entry {
                    epoch: r.epoch,
                    time: parse::iso_of(r.epoch),
                    refname: display(&f.label),
                    old: r.old,
                    new: r.new,
                    origin: "reflog",
                    message: escape_untrusted(&r.message, NAME_CAP).0,
                    flag,
                    group: None,
                    order,
                });
            }
        }
        if counts[1] > 0 {
            gap(&mut gaps, "uncovered", "-", format!("{} reflog rows have no refs.log line although the hook was installed then (a clone, `-c core.hooksPath`, another tool, or a write error)", counts[1]));
        }
        if counts[0] > 0 {
            gap(
                &mut gaps,
                "pre-install",
                "-",
                format!(
                    "{} reflog rows are older than the first install record: not a capture gap",
                    counts[0]
                ),
            );
        }
        if counts[2] > 0 {
            gap(
                &mut gaps,
                "hook-absent",
                "-",
                format!(
                    "{} reflog rows fall into a time when the hook was uninstalled",
                    counts[2]
                ),
            );
        }
        if let Some(v) = config_gap(&ctx.common) {
            gap(&mut gaps, "config", "-", v);
        }
    }

    // refs.log lines no reflog row has
    for r in &joined.rows {
        if opts
            .pattern
            .as_deref()
            .is_some_and(|p| !store::glob(p, &r.refname))
        {
            continue;
        }
        if reflog_keys.contains(&(r.refname.clone(), r.line.new.clone())) {
            continue;
        }
        order += 1;
        let epoch = parse::epoch_of(&r.line.time).unwrap_or(0);
        let origin = if r.line.context == "stw" {
            "stw"
        } else {
            "hook"
        };
        entries.push(Entry {
            epoch,
            time: r.line.time.clone(),
            refname: display(&r.refname),
            old: value_text(&r.old),
            new: value_text(&r.line.new),
            origin,
            message: format!(
                "{} line, worktree {}",
                escape_untrusted(&r.line.context, 40).0,
                escape_untrusted(&r.line.worktree, 40).0
            ),
            flag: if r.old_unknown {
                "old-unknown"
            } else {
                "hook-only"
            },
            group: None,
            order,
        });
    }
    entries.sort_by_key(|e| (e.epoch, e.order));
    assign_groups(&mut entries);

    // gaps from the log files
    let (gap_lines, gap_unreadable) = oplog.gap_lines();
    for (n, why) in gap_unreadable {
        gap(&mut gaps, "unreadable", &n, format!("not read: {why}"));
    }
    let capture = gap_lines.iter().filter(|(k, _, _)| k != "rotated").count();
    let rotated = gap_lines.iter().filter(|(k, _, _)| k == "rotated").count();
    // a second rotation of one file has replaced its old generation: the oldest lines are gone (spec 6)
    for name in ["refs.log", "rewrites.log"] {
        let n = gap_lines
            .iter()
            .filter(|(k, _, last)| k == "rotated" && last == name)
            .count();
        if n >= 2 {
            gap(
                &mut gaps,
                "truncated",
                name,
                format!("{n} rotations of {name}: older lines were rotated away and are gone"),
            );
        }
    }
    if capture > 0 {
        gap(&mut gaps, "capture-gap", "-", format!("{capture} gap lines in errors.log / stw-oplog-gaps: the hook or stw failed to write; changes around them may be missing"));
    }
    if rotated > 0 {
        gap(
            &mut gaps,
            "rotated",
            "-",
            format!(
                "{rotated} rotation(s) noted in errors.log: older lines are in refs.log.1 or gone"
            ),
        );
    }
    if window.is_none() {
        gap(&mut gaps, "no-install-record", "-", "no install record (events-refhook.log): a hook line cannot be expected for any reflog row, so none is called uncovered".into());
    }
    gap(&mut gaps, "still-open", "-", "not seen by the hook: clones, `-c core.hooksPath`, other tools, gix writes of tools other than stw, refs below refs/remotes unless remotes=on".into());
    gap(&mut gaps, "suspected", "-", "groups are suspected only (same text stem, within one second); time and text prove no common operation".into());

    Timeline {
        common: ctx.common.clone(),
        entries,
        other_lines: joined.other,
        gaps,
    }
}

/// An `old`/`new` value of a log line for output: a hash as it is, a symbolic value decoded and escaped.
fn value_text(v: &str) -> String {
    if v.starts_with("ref:") {
        display(&display_name(v))
    } else {
        v.to_string()
    }
}

/// A hash cut to 7 digits; a symbolic value (already decoded and escaped) cut at 40 characters.
fn short_value(v: &str) -> String {
    if v.starts_with("ref:") {
        v.chars().take(40).collect()
    } else {
        v[..7.min(v.len())].to_string()
    }
}

fn display(s: &str) -> String {
    escape_untrusted(s, NAME_CAP).0
}

/// `core.logAllRefUpdates=false`: reflogs that do not exist yet are not created.
fn config_gap(common: &std::path::Path) -> Option<String> {
    let v = super::config_value(common, "core.logAllRefUpdates")?;
    matches!(v.to_ascii_lowercase().as_str(), "false" | "no" | "off" | "0" | "never").then(|| {
        format!(
            "core.logAllRefUpdates={}: no new reflog is created; only reflogs that already exist are appended, so changes of refs without one are missing from the reflog rows",
            escape_untrusted(&v, 40).0
        )
    })
}

/// Same text stem (before the first `:` or `(`) within one second of the last entry with that stem: one suspected group.
fn assign_groups(entries: &mut [Entry]) {
    let stem = |m: &str| {
        m.split([':', '('])
            .next()
            .unwrap_or("")
            .trim_end()
            .to_string()
    };
    let mut last: HashMap<String, (i64, usize)> = HashMap::new();
    let mut next = 0usize;
    let mut sizes: HashMap<usize, usize> = HashMap::new();
    let mut ids: Vec<Option<usize>> = vec![None; entries.len()];
    for (i, e) in entries.iter().enumerate() {
        if e.origin != "reflog" {
            continue;
        }
        let s = stem(&e.message);
        if s.is_empty() {
            continue;
        }
        let g = match last.get(&s) {
            Some(&(t, g)) if e.epoch - t <= 1 => g,
            _ => {
                next += 1;
                next
            }
        };
        last.insert(s, (e.epoch, g));
        *sizes.entry(g).or_default() += 1;
        ids[i] = Some(g);
    }
    for (e, id) in entries.iter_mut().zip(ids) {
        e.group = id.filter(|g| sizes.get(g).copied().unwrap_or(0) > 1);
    }
}

impl Timeline {
    pub fn to_json(&self) -> Value {
        let entries: Vec<Value> = self
            .entries
            .iter()
            .map(|e| {
                json!({
                    "time": e.time, "epoch": e.epoch, "ref": e.refname, "old": e.old, "new": e.new,
                    "origin": e.origin, "message": e.message, "flag": e.flag,
                    "group": e.group.map(|g| format!("g{g}")), "group_basis": "suspected",
                })
            })
            .collect();
        let other: Vec<Value> = self
            .other_lines
            .iter()
            .map(|l| {
                json!({
                    "time": l.time, "ref": display(&display_name(&l.refname)), "old": l.old, "new": l.new,
                    "context": display(&l.context), "worktree": display(&l.worktree),
                })
            })
            .collect();
        let gaps: Vec<Value> = self
            .gaps
            .iter()
            .map(|g| json!({ "kind": g.kind, "ref": g.refname, "text": g.text }))
            .collect();
        json!({
            "schema_version": SCHEMA_VERSION, "command": "oplog timeline", "engine": crate::output::engine(),
            "repo": display(&self.common.to_string_lossy()),
            "entries": entries, "other_lines": other, "gaps": gaps,
        })
    }

    pub fn render_human(&self) -> String {
        let mut b = String::new();
        let _ = writeln!(
            b,
            "stw oplog timeline  git-dir={}  (read-only; sources: reflog, refs.log)",
            display(&self.common.to_string_lossy())
        );
        let _ = writeln!(b, "Groups are suspected only (same text stem, within one second), never proof of one operation.\n");
        let _ = writeln!(
            b,
            "{:<20}  {:<30}  {:<17}  {:<7}  {:<9}  {:<13}  text",
            "time", "ref", "old->new", "origin", "group", "flag"
        );
        for e in &self.entries {
            let g = e
                .group
                .map_or("-".to_string(), |g| format!("g{g} suspected"));
            let _ = writeln!(
                b,
                "{:<20}  {:<30}  {:<17}  {:<7}  {:<9}  {:<13}  {}",
                e.time,
                e.refname,
                format!("{}->{}", short_value(&e.old), short_value(&e.new)),
                e.origin,
                g,
                e.flag,
                if e.message.is_empty() {
                    "(no text)"
                } else {
                    &e.message
                }
            );
        }
        if self.entries.is_empty() {
            let _ = writeln!(b, "(no entries)");
        }
        let _ = writeln!(b, "\n{} entries", self.entries.len());
        if !self.other_lines.is_empty() {
            let _ = writeln!(b, "{} refs.log lines have a context this version does not know (in --json: other_lines)", self.other_lines.len());
        }
        let _ = writeln!(b, "\nGaps:");
        for g in &self.gaps {
            let _ = writeln!(b, "  {:<18} {:<14} {}", g.kind, g.refname, g.text);
        }
        b
    }
}
