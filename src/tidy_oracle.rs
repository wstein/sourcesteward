//! Pure comparison functions of the `tidy` oracle (package 4): the hunk keys of gix against those of `git diff -U0`, the
//! tally of differing multisets per `diff.algorithm`, and the GitHub truth of a merged pull request against the plan.
//! Nothing here starts a process or reads a repository; `tests/it/oracle_tidy.rs` and `tests/oracle/tidy_oracle.sh` do.
//! Integers only: shares are counted, the display is rounded.
use crate::tidy_similarity::{fmt_permille, Entry, Keys, Sim};
use std::collections::BTreeMap;

// ---- the diff of git ----

/// The path of a `---` or `+++` line seen so far: not seen, `/dev/null`, or a path.
type HeaderPath = Option<Option<Vec<u8>>>;

/// The text entries (`+` and `-` lines, path and trimmed line) of the output of `git diff -U0 --no-renames`.
/// A binary file, a mode change and a `\ No newline` marker give no entry. A path in quotes is an error (run git with
/// `core.quotepath=off`; a name with a tab, a quote or a backslash stays quoted and cannot be read here).
pub fn parse_unified_u0(diff: &[u8]) -> Result<Vec<Entry>, String> {
    let mut out = Vec::new();
    let (mut old, mut new): (HeaderPath, HeaderPath) = (None, None);
    let mut in_hunk = false;
    let mut path: Vec<u8> = Vec::new();
    for line in diff.split(|b| *b == b'\n') {
        if line.starts_with(b"diff --git ") {
            in_hunk = false;
            old = None;
            new = None;
            continue;
        }
        if !in_hunk {
            if let Some(r) = line.strip_prefix(b"--- ") {
                old = Some(header_path(r, b"a/")?);
            } else if let Some(r) = line.strip_prefix(b"+++ ") {
                new = Some(header_path(r, b"b/")?);
            } else if line.starts_with(b"@@") {
                in_hunk = true;
                path = match (&new, &old) {
                    (Some(Some(p)), _) | (_, Some(Some(p))) => p.clone(),
                    _ => return Err("a hunk without a path".into()),
                };
            }
            continue;
        }
        if line.starts_with(b"@@") {
            continue;
        }
        let sign = match line.first() {
            Some(b'+') => b'+',
            Some(b'-') => b'-',
            _ => continue,
        };
        out.push(Entry {
            path: path.clone(),
            sign,
            line: crate::tidy_similarity::trim_line(&line[1..]).to_vec(),
        });
    }
    Ok(out)
}

/// The path of a `---` or `+++` line: `None` for `/dev/null`.
fn header_path(rest: &[u8], prefix: &[u8]) -> Result<Option<Vec<u8>>, String> {
    let rest = rest.strip_suffix(b"\t").unwrap_or(rest);
    if rest == b"/dev/null" {
        return Ok(None);
    }
    if rest.first() == Some(&b'"') {
        return Err(format!("a quoted path: {}", String::from_utf8_lossy(rest)));
    }
    match rest.strip_prefix(prefix) {
        Some(p) => Ok(Some(p.to_vec())),
        None => Err(format!(
            "a path without {}",
            String::from_utf8_lossy(prefix)
        )),
    }
}

/// The keys of the text entries only: the entries of gix for a binary file, a mode or an empty file have no counterpart in
/// the diff of git and stay out of the comparison.
pub fn text_keys(entries: &[Entry]) -> Keys {
    let text: Vec<Entry> = entries
        .iter()
        .filter(|e| e.sign == b'+' || e.sign == b'-')
        .cloned()
        .collect();
    Keys::from_entries(&text)
}

/// The two multisets are not equal (a different size or a smaller intersection).
pub fn differs(sim: &Sim) -> bool {
    sim.inter != sim.a || sim.inter != sim.b
}

// ---- tally ----

/// How many worst commits a tally keeps.
pub const KEEP_WORST: usize = 5;

/// Commits compared, how many differ, and the worst ones (lowest value first, then the smaller commit ID).
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Tally {
    pub total: u64,
    pub differing: u64,
    pub worst: Vec<(String, Sim)>,
}

impl Tally {
    pub fn add(&mut self, id: &str, sim: Sim) {
        self.total += 1;
        if !differs(&sim) {
            return;
        }
        self.differing += 1;
        self.worst.push((id.to_string(), sim));
        self.worst
            .sort_by(|x, y| x.1.cmp_value(&y.1).then_with(|| x.0.cmp(&y.0)));
        self.worst.truncate(KEEP_WORST);
    }
    /// The share of differing commits in tenths of a percent, rounded half up; None without a commit.
    pub fn share_permille(&self) -> Option<u32> {
        if self.total == 0 {
            return None;
        }
        Some(((2000 * self.differing + self.total) / (2 * self.total)) as u32)
    }
    /// The worst value in tenths of a percent; None when nothing differs.
    pub fn worst_permille(&self) -> Option<u32> {
        self.worst.first().map(|w| w.1.permille())
    }
}

/// One tally per setting (for example `git-myers+indent`), in the order of the names.
#[derive(Clone, Debug, Default)]
pub struct Matrix {
    rows: BTreeMap<String, Tally>,
}

impl Matrix {
    pub fn add(&mut self, setting: &str, id: &str, sim: Sim) {
        self.rows
            .entry(setting.to_string())
            .or_default()
            .add(id, sim);
    }
    pub fn get(&self, setting: &str) -> Option<&Tally> {
        self.rows.get(setting)
    }
    /// A Markdown table: setting, commits, differing, share in percent, worst value in percent (with the commit).
    pub fn render_table(&self) -> String {
        let mut s = String::from(
            "| setting | commits | differing | share % | worst % |\n|---|---|---|---|---|\n",
        );
        for (name, t) in &self.rows {
            let share = t.share_permille().map_or("-".into(), fmt_permille);
            let worst = match t.worst.first() {
                Some((id, sim)) => {
                    format!("{} ({})", fmt_permille(sim.permille()), short(id))
                }
                None => "-".into(),
            };
            s.push_str(&format!(
                "| {name} | {} | {} | {share} | {worst} |\n",
                t.total, t.differing
            ));
        }
        s
    }
    /// The worst commits of every setting that has any, one line each: setting, commit, value, intersection and sizes.
    pub fn render_counterexamples(&self) -> String {
        let mut s = String::new();
        for (name, t) in &self.rows {
            for (id, sim) in &t.worst {
                s.push_str(&format!(
                    "{name} {} {} % ({} of {} and {})\n",
                    short(id),
                    fmt_permille(sim.permille()),
                    sim.inter,
                    sim.a,
                    sim.b
                ));
            }
        }
        s
    }
}

fn short(id: &str) -> &str {
    &id[..id.len().min(12)]
}
