//! Patch IDs with `git patch-id --stable` semantics, computed in-process.
//!
//! The ID is a SHA-1 over the diff of a commit against its first parent, normalized the way git does:
//! hunk headers (line numbers) are dropped, `index` lines are dropped, and every ASCII whitespace byte is
//! removed from every remaining line. `--stable`: every file diff is hashed on its own and the 20-byte hashes
//! are summed (little-endian with carry), so the file order does not matter. `\ No newline` markers are not hashed.
//! Merge commits have no patch ID (`git cherry` skips them as well).
//!
//! Diff hunks come from `gix-imara-diff` (Myers, no slider heuristic) with 3 lines of context. Git uses
//! xdiff; both are Myers-based but differ in heuristics, so IDs agree only where the hunks agree. The
//! oracle script in `tests/oracle/` measures the agreement on real commits.

use gix::bstr::{BStr, BString};
use gix::diff::blob::{Algorithm, Diff, InternedInput};
use gix::objs::tree::EntryKind;
use gix::ObjectId;

use crate::error::Result;

const CONTEXT: u32 = 3;

/// Hashes the lines of one file diff; `finish_file` folds the digest into the running stable sum.
struct Hasher {
    cur: gix::hash::Hasher,
    sum: [u8; 20],
    /// The same lines with their white space kept, hashed per file and summed like `sum`.
    exact_cur: gix::hash::Hasher,
    exact_sum: [u8; 20],
    log: Option<Vec<u8>>,
}

/// Add `digest` to `sum` as a little-endian number, byte by byte (git's `hash_obj` sum in stable mode).
fn add_digest(sum: &mut [u8; 20], digest: &[u8]) {
    let mut carry: u16 = 0;
    for (i, b) in digest.iter().enumerate() {
        carry += sum[i] as u16 + *b as u16;
        sum[i] = carry as u8;
        carry >>= 8;
    }
}

impl Hasher {
    fn new() -> Self {
        Hasher {
            cur: gix::hash::hasher(gix::hash::Kind::Sha1),
            sum: [0; 20],
            exact_cur: gix::hash::hasher(gix::hash::Kind::Sha1),
            exact_sum: [0; 20],
            log: None,
        }
    }
    /// Feed one logical line with all ASCII whitespace removed (git's `remove_space`).
    fn line(&mut self, line: &[u8]) {
        // the exact variant keeps every byte; the length prefix keeps line borders unambiguous
        self.exact_cur.update(&(line.len() as u64).to_be_bytes());
        self.exact_cur.update(line);
        let mut buf = Vec::with_capacity(line.len());
        buf.extend(
            line.iter()
                .copied()
                .filter(|b| !matches!(b, b' ' | b'\t' | b'\n' | 0x0b | 0x0c | b'\r')),
        );
        self.cur.update(&buf);
        if let Some(log) = &mut self.log {
            log.extend_from_slice(&buf);
            log.push(b'\n');
        }
    }
    /// End of one file diff (git's `flush_one_hunk` in stable mode): add its digest to the sum.
    fn finish_file(&mut self) -> Result<()> {
        let done = std::mem::replace(&mut self.cur, gix::hash::hasher(gix::hash::Kind::Sha1));
        let digest = done.try_finalize()?;
        add_digest(&mut self.sum, digest.as_bytes());
        let exact = std::mem::replace(
            &mut self.exact_cur,
            gix::hash::hasher(gix::hash::Kind::Sha1),
        )
        .try_finalize()?;
        add_digest(&mut self.exact_sum, exact.as_bytes());
        if let Some(log) = &mut self.log {
            log.extend_from_slice(b"--file-end--\n");
        }
        Ok(())
    }
    /// (patch ID, white-space-preserving digest of the same lines)
    fn finish(self) -> (ObjectId, ObjectId) {
        (
            ObjectId::from_bytes_or_panic(&self.sum),
            ObjectId::from_bytes_or_panic(&self.exact_sum),
        )
    }
}

fn is_binary(data: &[u8]) -> bool {
    data[..data.len().min(8000)].contains(&0)
}

fn mode_str(kind: EntryKind) -> &'static str {
    match kind {
        EntryKind::Blob => "100644",
        EntryKind::BlobExecutable => "100755",
        EntryKind::Link => "120000",
        EntryKind::Commit => "160000",
        EntryKind::Tree => "040000",
    }
}

/// Content that git diffs for an entry: the blob, or `Subproject commit <id>` for a gitlink.
fn content(repo: &gix::Repository, kind: EntryKind, id: ObjectId) -> Result<Vec<u8>> {
    if kind == EntryKind::Commit {
        return Ok(format!("Subproject commit {id}\n").into_bytes());
    }
    Ok(repo.find_object(id)?.data.clone())
}

/// Hash the unified-diff lines (without hunk headers) of `before` -> `after` into `h`.
fn hash_hunks(h: &mut Hasher, before: &[u8], after: &[u8], old_name: &[u8], new_name: &[u8]) {
    let input = InternedInput::new(before, after);
    let mut diff = Diff::compute(Algorithm::Myers, &input);
    diff.postprocess_no_heuristic(&input);
    let hunks: Vec<_> = diff.hunks().collect();
    if hunks.is_empty() {
        return;
    }
    h.line(&[b"--- ".as_slice(), old_name].concat());
    h.line(&[b"+++ ".as_slice(), new_name].concat());
    let b: Vec<&[u8]> = input.before.iter().map(|t| input.interner[*t]).collect();
    let a: Vec<&[u8]> = input.after.iter().map(|t| input.interner[*t]).collect();
    let emit = |h: &mut Hasher, prefix: u8, line: &[u8]| {
        let mut l = Vec::with_capacity(line.len() + 1);
        l.push(prefix);
        l.extend_from_slice(line);
        h.line(&l); // a missing final newline adds a `\ No newline` marker line, which git does not hash
    };
    let nb = b.len() as u32;
    let mut i = 0;
    while i < hunks.len() {
        // group hunks whose gap is at most 2 * CONTEXT lines (xdiff: max_common)
        let mut j = i;
        while j + 1 < hunks.len() && hunks[j + 1].before.start - hunks[j].before.end <= 2 * CONTEXT
        {
            j += 1;
        }
        let start = hunks[i].before.start.saturating_sub(CONTEXT);
        for k in start..hunks[i].before.start {
            emit(h, b' ', b[k as usize]);
        }
        for n in i..=j {
            for k in hunks[n].before.clone() {
                emit(h, b'-', b[k as usize]);
            }
            for k in hunks[n].after.clone() {
                emit(h, b'+', a[k as usize]);
            }
            let gap_end = if n < j {
                hunks[n + 1].before.start
            } else {
                (hunks[n].before.end + CONTEXT).min(nb)
            };
            for k in hunks[n].before.end..gap_end {
                emit(h, b' ', b[k as usize]);
            }
        }
        i = j + 1;
    }
}

struct FileChange {
    path: BString,
    old: Option<(EntryKind, ObjectId)>,
    new: Option<(EntryKind, ObjectId)>,
}

fn kind_of(m: gix::objs::tree::EntryMode) -> Option<EntryKind> {
    Some(m.kind())
}

/// (patch ID, white-space-preserving digest of the same diff lines, which include the quoted raw paths): equal
/// patches need both equal.
pub type PatchKey = (ObjectId, ObjectId);

/// Patch ID of `commit` against its first parent. `None` for merge commits and for commits that
/// change nothing (git prints no ID for those either).
pub fn commit_patch_id(repo: &gix::Repository, commit: ObjectId) -> Result<Option<ObjectId>> {
    Ok(commit_patch_key(repo, commit)?.map(|(id, _)| id))
}

/// Patch ID of `commit` and a digest of the same diff lines with their white space kept (the lines include the
/// file names, as raw bytes). Git's patch ID ignores all white space, in file names and in file content, so
/// `a b.txt` and `ab.txt`, or `\tcc x` and `  cc   x`, get the same ID; whoever takes equal IDs as proof of
/// equal patches has to compare the second component as well.
pub fn commit_patch_key(repo: &gix::Repository, commit: ObjectId) -> Result<Option<PatchKey>> {
    let c = repo.find_commit(commit)?;
    let parents: Vec<ObjectId> = c.parent_ids().map(|p| p.detach()).collect();
    if parents.len() > 1 {
        return Ok(None);
    }
    let new_tree = c.tree()?;
    let old_tree = match parents.first() {
        Some(p) => repo.find_commit(*p)?.tree()?,
        None => repo.empty_tree(),
    };
    if old_tree.id == new_tree.id {
        return Ok(None); // empty commit: no patch, nothing to compare
    }
    tree_diff_inner(repo, &old_tree, &new_tree, false).map(|((id, exact), _)| Some((id, exact)))
}

/// Patch ID of the difference between two trees.
pub fn tree_diff_patch_id(
    repo: &gix::Repository,
    old: &gix::Tree<'_>,
    new: &gix::Tree<'_>,
) -> Result<ObjectId> {
    tree_diff_inner(repo, old, new, false).map(|((id, _), _)| id)
}

/// The normalized lines that go into the hash (debug aid for the oracle comparison).
pub fn commit_patch_text(repo: &gix::Repository, commit: ObjectId) -> Result<String> {
    let c = repo.find_commit(commit)?;
    let new_tree = c.tree()?;
    let old_tree = match c.parent_ids().next() {
        Some(p) => repo.find_commit(p.detach())?.tree()?,
        None => repo.empty_tree(),
    };
    let (_, log) = tree_diff_inner(repo, &old_tree, &new_tree, true)?;
    Ok(String::from_utf8_lossy(&log).into_owned())
}

fn tree_diff_inner(
    repo: &gix::Repository,
    old: &gix::Tree<'_>,
    new: &gix::Tree<'_>,
    log: bool,
) -> Result<((ObjectId, ObjectId), Vec<u8>)> {
    use gix::diff::tree::recorder::Change;
    let mut rec = gix::diff::tree::Recorder::default();
    let mut state = gix::diff::tree::State::default();
    gix::diff::tree(
        gix::objs::TreeRefIter::from_bytes(&old.data, old.id.kind()),
        gix::objs::TreeRefIter::from_bytes(&new.data, new.id.kind()),
        &mut state,
        &repo.objects,
        &mut rec,
    )?;
    let mut files: Vec<FileChange> = Vec::new();
    for ch in rec.records {
        match ch {
            Change::Addition {
                entry_mode,
                oid,
                path,
                ..
            } if !entry_mode.is_tree() => files.push(FileChange {
                path,
                old: None,
                new: kind_of(entry_mode).map(|k| (k, oid)),
            }),
            Change::Deletion {
                entry_mode,
                oid,
                path,
                ..
            } if !entry_mode.is_tree() => files.push(FileChange {
                path,
                old: kind_of(entry_mode).map(|k| (k, oid)),
                new: None,
            }),
            Change::Modification {
                previous_entry_mode,
                previous_oid,
                entry_mode,
                oid,
                path,
            } => {
                if previous_entry_mode.is_tree() || entry_mode.is_tree() {
                    continue; // tree <-> tree modification: its entries are reported separately
                }
                files.push(FileChange {
                    path,
                    old: kind_of(previous_entry_mode).map(|k| (k, previous_oid)),
                    new: kind_of(entry_mode).map(|k| (k, oid)),
                })
            }
            _ => {}
        }
    }
    files.sort_by(|a, b| a.path.cmp(&b.path));
    let mut h = Hasher::new();
    if log {
        h.log = Some(Vec::new());
    }
    for f in files {
        hash_file(repo, &mut h, f.path.as_ref(), f.old, f.new)?;
        h.finish_file()?;
    }
    let log = h.log.take().unwrap_or_default();
    Ok((h.finish(), log))
}

/// `prefix` + `path` as git writes it in a diff header with `core.quotePath=true`: when the name contains
/// a control character, `"`, `\`, DEL or a byte >= 0x80, it is wrapped in double quotes with C escapes
/// (`\a \b \t \n \v \f \r \" \\`) and three-digit octal escapes for the other bytes.
fn quote_path(prefix: &[u8], path: &BStr) -> Vec<u8> {
    let needs_quote = path
        .iter()
        .any(|&b| b < 0x20 || b == b'"' || b == b'\\' || b >= 0x7f);
    let mut out = Vec::with_capacity(path.len() + prefix.len() + 2);
    if !needs_quote {
        out.extend_from_slice(prefix);
        out.extend_from_slice(path);
        return out;
    }
    out.push(b'"');
    out.extend_from_slice(prefix);
    for &b in path.iter() {
        match b {
            0x07 => out.extend_from_slice(b"\\a"),
            0x08 => out.extend_from_slice(b"\\b"),
            b'\t' => out.extend_from_slice(b"\\t"),
            b'\n' => out.extend_from_slice(b"\\n"),
            0x0b => out.extend_from_slice(b"\\v"),
            0x0c => out.extend_from_slice(b"\\f"),
            b'\r' => out.extend_from_slice(b"\\r"),
            b'"' => out.extend_from_slice(b"\\\""),
            b'\\' => out.extend_from_slice(b"\\\\"),
            b if !(0x20..0x7f).contains(&b) => {
                out.extend_from_slice(format!("\\{b:03o}").as_bytes())
            }
            b => out.push(b),
        }
    }
    out.push(b'"');
    out
}

fn hash_file(
    repo: &gix::Repository,
    h: &mut Hasher,
    path: &BStr,
    old: Option<(EntryKind, ObjectId)>,
    new: Option<(EntryKind, ObjectId)>,
) -> Result<()> {
    // git quotes names in diff headers (core.quotePath=true) and hashes the raw bytes
    let header = [quote_path(b"a/", path), quote_path(b"b/", path)];
    let mut line = b"diff --git ".to_vec();
    line.extend_from_slice(&header[0]);
    line.push(b' ');
    line.extend_from_slice(&header[1]);
    h.line(&line);
    let dev_null = b"/dev/null".to_vec();
    let (old_name, new_name) = (
        if old.is_some() {
            header[0].clone()
        } else {
            dev_null.clone()
        },
        if new.is_some() {
            header[1].clone()
        } else {
            dev_null
        },
    );
    match (old, new) {
        (None, Some((k, _))) => h.line(format!("new file mode {}", mode_str(k)).as_bytes()),
        (Some((k, _)), None) => h.line(format!("deleted file mode {}", mode_str(k)).as_bytes()),
        (Some((ko, _)), Some((kn, _))) if ko != kn => {
            h.line(format!("old mode {}", mode_str(ko)).as_bytes());
            h.line(format!("new mode {}", mode_str(kn)).as_bytes());
        }
        _ => {}
    }
    let before = match old {
        Some((k, id)) => content(repo, k, id)?,
        None => Vec::new(),
    };
    let after = match new {
        Some((k, id)) => content(repo, k, id)?,
        None => Vec::new(),
    };
    if before == after {
        return Ok(());
    }
    if is_binary(&before) || is_binary(&after) {
        let _ = (&old_name, &new_name);
        // git hashes the two (abbreviated) object ids of the `index` line instead of any content; the
        // abbreviation length depends on the repository, so full ids are used here (not oracle-comparable)
        let hex = |x: Option<(EntryKind, ObjectId)>| {
            x.map(|(_, id)| id.to_string())
                .unwrap_or_else(|| "0".repeat(40))
        };
        h.line(hex(old).as_bytes());
        h.line(hex(new).as_bytes());
        return Ok(());
    }
    hash_hunks(h, &before, &after, &old_name, &new_name);
    Ok(())
}
