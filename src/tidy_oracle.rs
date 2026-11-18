//! Pure comparison functions of the `tidy` oracle (package 4): the hunk keys of gix against those of `git diff -U0`, the
//! tally of differing multisets per `diff.algorithm`, and the GitHub truth of a merged pull request against the plan.
//! Nothing here starts a process or reads a repository; `tests/it/oracle_tidy.rs` and `tests/oracle/tidy_oracle.sh` do.
//! Integers only: shares are counted, the display is rounded.
use crate::tidy_similarity::{Entry, Keys, Sim};

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
