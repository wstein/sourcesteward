//! Patch IDs with `git patch-id --stable` semantics, computed in-process.
//!
//! The ID is a SHA-1 over the diff of a commit against its first parent as `git diff-tree -p` prints it,
//! read the way `git patch-id` reads it (a port of `get_one_patchid` in git's `builtin/patch-id.c`): hunk
//! headers (line numbers) and `index` lines are not hashed, every ASCII white space byte is removed from
//! every other line. `--stable`: the diff is cut into units, each unit is hashed on its own and the 20-byte
//! hashes are summed (little-endian with carry), so the order of the units does not matter. A unit ends where
//! the next `diff` line follows a complete hunk, so a file diff without hunks (mode change, empty file, exact
//! rename) belongs to the unit of the next file diff; a binary file adds the abbreviated blob ids of its
//! `index` line to the hash and ends its unit, and the `diff --git` line after it is not hashed; the last unit
//! is added even when it is empty. `\ No newline` markers are not hashed. Merge commits have no patch ID
//! (`git cherry` skips them as well). Renames are detected as `git diff` does by default (`patch_rename.rs`).
//!
//! Diff hunks come from `gix-imara-diff` (Myers, no slider heuristic) with 3 lines of context. Git uses
//! xdiff; both are Myers-based but differ in heuristics, so IDs agree only where the hunks agree. The
//! oracle scripts in `tests/oracle/` measure the agreement on real commits.

use gix::bstr::{BStr, BString};
use gix::diff::blob::{Algorithm, Diff, InternedInput};
use gix::objs::tree::EntryKind;
use gix::ObjectId;

use crate::error::Result;

const CONTEXT: u32 = 3;

/// One line of the diff text as far as `git patch-id` reads it.
enum Line {
    Text(Vec<u8>),
    /// The `index` line of a binary diff: the abbreviated blob ids git prints, and the full ones.
    Index {
        pre: String,
        post: String,
        pre_full: String,
        post_full: String,
    },
    /// The counts of a hunk header (`@@ -a,<before> +c,<after> @@`).
    Hunk {
        before: i64,
        after: i64,
    },
}

/// A port of `get_one_patchid` (builtin/patch-id.c, `--stable`). A second digest hashes the same lines with
/// their white space and with full blob ids, and also the lines git skips (`Binary files ... differ`, the
/// `diff` line after a binary file); it is cut into the same units.
struct Machine {
    cur: gix::hash::Hasher,
    sum: [u8; 20],
    exact_cur: gix::hash::Hasher,
    exact_sum: [u8; 20],
    before: i64,
    after: i64,
    binary: bool,
    ids: (String, String),
    full_ids: (String, String),
    patchlen: usize,
    stopped: bool,
    log: Option<Vec<u8>>,
}

/// Add `digest` to `sum` as a little-endian number, byte by byte (git's `flush_one_hunk` in stable mode).
fn add_digest(sum: &mut [u8; 20], digest: &[u8]) {
    let mut carry: u16 = 0;
    for (i, b) in digest.iter().enumerate() {
        carry += sum[i] as u16 + *b as u16;
        sum[i] = carry as u8;
        carry >>= 8;
    }
}

fn starts_with(line: &[u8], prefix: &str) -> bool {
    line.starts_with(prefix.as_bytes())
}

impl Machine {
    fn new() -> Self {
        Machine {
            cur: gix::hash::hasher(gix::hash::Kind::Sha1),
            sum: [0; 20],
            exact_cur: gix::hash::hasher(gix::hash::Kind::Sha1),
            exact_sum: [0; 20],
            before: -1,
            after: -1,
            binary: false,
            ids: Default::default(),
            full_ids: Default::default(),
            patchlen: 0,
            stopped: false,
            log: None,
        }
    }

    fn exact_line(&mut self, line: &[u8]) {
        // the length prefix keeps line borders unambiguous
        self.exact_cur.update(&(line.len() as u64).to_be_bytes());
        self.exact_cur.update(line);
    }

    /// git's `remove_space` on a line: a C string, so it ends at a NUL byte, and ASCII white space is dropped.
    fn hash_line(&mut self, line: &[u8]) {
        self.exact_line(line);
        let c_string = line.split(|b| *b == 0).next().unwrap_or(&[]);
        let buf: Vec<u8> = c_string
            .iter()
            .copied()
            .filter(|b| !matches!(b, b' ' | b'\t' | b'\n' | 0x0b | 0x0c | b'\r'))
            .collect();
        self.patchlen += buf.len();
        self.cur.update(&buf);
        if let Some(log) = &mut self.log {
            log.extend_from_slice(&buf);
            log.push(b'\n');
        }
    }

    /// git's `flush_one_hunk` in stable mode: add the digest of the current unit to the sum.
    fn flush(&mut self) -> Result<()> {
        let done = std::mem::replace(&mut self.cur, gix::hash::hasher(gix::hash::Kind::Sha1));
        add_digest(&mut self.sum, done.try_finalize()?.as_bytes());
        let exact = std::mem::replace(
            &mut self.exact_cur,
            gix::hash::hasher(gix::hash::Kind::Sha1),
        )
        .try_finalize()?;
        add_digest(&mut self.exact_sum, exact.as_bytes());
        if let Some(log) = &mut self.log {
            log.extend_from_slice(b"--unit-end--\n");
        }
        Ok(())
    }

    fn feed(&mut self, line: Line) -> Result<()> {
        if self.stopped {
            return Ok(());
        }
        match line {
            Line::Index {
                pre,
                post,
                pre_full,
                post_full,
            } => {
                self.ids = (pre, post);
                self.full_ids = (pre_full, post_full);
                Ok(())
            }
            Line::Hunk { before, after } => {
                if self.before == 0 && self.after == 0 {
                    self.before = before;
                    self.after = after;
                }
                Ok(())
            }
            Line::Text(l) => self.text(&l),
        }
    }

    fn text(&mut self, l: &[u8]) -> Result<()> {
        // parsing a diff header?
        if self.before == -1 {
            if starts_with(l, "Binary files") || starts_with(l, "GIT binary patch") {
                let ids = std::mem::take(&mut self.ids);
                self.cur.update(ids.0.as_bytes());
                self.cur.update(ids.1.as_bytes());
                let full = std::mem::take(&mut self.full_ids);
                self.exact_line(l);
                self.exact_line(full.0.as_bytes());
                self.exact_line(full.1.as_bytes());
                self.flush()?;
                self.binary = true;
                self.before = 0;
                return Ok(());
            } else if starts_with(l, "--- ") {
                self.before = 1;
                self.after = 1;
            } else if !l.first().is_some_and(|b| b.is_ascii_alphabetic()) {
                self.stopped = true;
                return Ok(());
            }
        }
        if self.binary {
            if starts_with(l, "diff ") {
                // git does not hash this line
                self.exact_line(l);
                self.binary = false;
                self.before = -1;
            }
            return Ok(());
        }
        // looking for a valid hunk header: a unit ends where the next `diff` line follows a complete hunk
        if self.before == 0 && self.after == 0 {
            if !starts_with(l, "diff ") {
                self.stopped = true;
                return Ok(());
            }
            self.flush()?;
            self.before = -1;
            self.after = -1;
        }
        // inside a hunk, or a header line
        if l.first() == Some(&b'-') || l.first() == Some(&b' ') {
            self.before -= 1;
        }
        if l.first() == Some(&b'+') || l.first() == Some(&b' ') {
            self.after -= 1;
        }
        self.hash_line(l);
        Ok(())
    }

    /// (patch ID, white-space-preserving digest of the same units). git adds the last unit even when it is
    /// empty; an empty diff has no units at all.
    fn finish(mut self) -> Result<(ObjectId, ObjectId)> {
        if self.patchlen > 0 {
            self.flush()?;
        }
        Ok((
            ObjectId::from_bytes_or_panic(&self.sum),
            ObjectId::from_bytes_or_panic(&self.exact_sum),
        ))
    }
}

/// NUL in the first 8000 bytes, as git decides.
pub(crate) fn is_binary(data: &[u8]) -> bool {
    data[..data.len().min(8000)].contains(&0)
}

pub(crate) fn mode_str(kind: EntryKind) -> &'static str {
    match kind {
        EntryKind::Blob => "100644",
        EntryKind::BlobExecutable => "100755",
        EntryKind::Link => "120000",
        EntryKind::Commit => "160000",
        EntryKind::Tree => "040000",
    }
}

/// Content that git diffs for an entry: the blob, or `Subproject commit <id>` for a gitlink.
pub(crate) fn content(repo: &gix::Repository, kind: EntryKind, id: ObjectId) -> Result<Vec<u8>> {
    if kind == EntryKind::Commit {
        return Ok(format!("Subproject commit {id}\n").into_bytes());
    }
    Ok(repo.find_object(id)?.data.clone())
}

/// The unified-diff lines of `before` -> `after` (`---`, `+++`, hunk headers, 3 lines of context) as `out`.
fn render_hunks(
    out: &mut Vec<Line>,
    before: &[u8],
    after: &[u8],
    old_name: &[u8],
    new_name: &[u8],
) {
    let input = InternedInput::new(before, after);
    let mut diff = Diff::compute(Algorithm::Myers, &input);
    diff.postprocess_no_heuristic(&input);
    let hunks: Vec<_> = diff.hunks().collect();
    if hunks.is_empty() {
        return;
    }
    out.push(Line::Text([b"--- ".as_slice(), old_name].concat()));
    out.push(Line::Text([b"+++ ".as_slice(), new_name].concat()));
    let b: Vec<&[u8]> = input.before.iter().map(|t| input.interner[*t]).collect();
    let a: Vec<&[u8]> = input.after.iter().map(|t| input.interner[*t]).collect();
    // a missing final newline adds a `\ No newline` marker line, which git does not hash
    let emit = |lines: &mut Vec<Line>, prefix: u8, line: &[u8]| {
        let mut l = Vec::with_capacity(line.len() + 1);
        l.push(prefix);
        l.extend_from_slice(line);
        lines.push(Line::Text(l));
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
        let mut lines: Vec<Line> = Vec::new();
        let start = hunks[i].before.start.saturating_sub(CONTEXT);
        for k in start..hunks[i].before.start {
            emit(&mut lines, b' ', b[k as usize]);
        }
        for n in i..=j {
            for k in hunks[n].before.clone() {
                emit(&mut lines, b'-', b[k as usize]);
            }
            for k in hunks[n].after.clone() {
                emit(&mut lines, b'+', a[k as usize]);
            }
            let gap_end = if n < j {
                hunks[n + 1].before.start
            } else {
                (hunks[n].before.end + CONTEXT).min(nb)
            };
            for k in hunks[n].before.end..gap_end {
                emit(&mut lines, b' ', b[k as usize]);
            }
        }
        let count = |signs: [u8; 2]| {
            lines
                .iter()
                .filter(|l| matches!(l, Line::Text(t) if signs.contains(&t[0])))
                .count() as i64
        };
        out.push(Line::Hunk {
            before: count(*b" -"),
            after: count(*b" +"),
        });
        out.append(&mut lines);
        i = j + 1;
    }
}

/// One changed file of a tree diff without rename detection (a rename is a deletion and an addition).
pub(crate) struct FileChange {
    pub(crate) path: BString,
    pub(crate) old: Option<(EntryKind, ObjectId)>,
    pub(crate) new: Option<(EntryKind, ObjectId)>,
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

/// The files that differ between two trees (no trees, no renames), sorted by path.
pub(crate) fn changed_files(
    repo: &gix::Repository,
    old: &gix::Tree<'_>,
    new: &gix::Tree<'_>,
) -> Result<Vec<FileChange>> {
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
    Ok(files)
}

/// One entry of the diff as git prints it: an addition, a deletion or a modification of one path. For an
/// addition or a deletion `old_path` and `new_path` are the same path.
pub(crate) struct Pair {
    pub(crate) old_path: BString,
    pub(crate) new_path: BString,
    pub(crate) old: Option<(EntryKind, ObjectId)>,
    pub(crate) new: Option<(EntryKind, ObjectId)>,
    /// `Some(percent)` for a rename (100 for an exact one): `similarity index <n>%`.
    pub(crate) similarity: Option<u64>,
}

/// The file type of an entry as `S_IFMT` tells them apart: regular file, symbolic link, gitlink.
fn type_class(kind: EntryKind) -> u8 {
    match kind {
        EntryKind::Blob | EntryKind::BlobExecutable => 0,
        EntryKind::Link => 1,
        _ => 2,
    }
}

impl Pair {
    /// The entries git prints for one changed path: a change of the file type (file, link, gitlink) is a
    /// deletion followed by an addition.
    pub(crate) fn of(f: &FileChange) -> Vec<Pair> {
        let pair = |old, new| Pair {
            old_path: f.path.clone(),
            new_path: f.path.clone(),
            old,
            new,
            similarity: None,
        };
        match (f.old, f.new) {
            (Some(o), Some(n)) if type_class(o.0) != type_class(n.0) => {
                vec![pair(Some(o), None), pair(None, Some(n))]
            }
            (old, new) => vec![pair(old, new)],
        }
    }
}

fn tree_diff_inner(
    repo: &gix::Repository,
    old: &gix::Tree<'_>,
    new: &gix::Tree<'_>,
    log: bool,
) -> Result<((ObjectId, ObjectId), Vec<u8>)> {
    let files = changed_files(repo, old, new)?;
    let mut m = Machine::new();
    if log {
        m.log = Some(Vec::new());
    }
    for pair in files.iter().flat_map(Pair::of) {
        for line in render_pair(repo, &pair)? {
            m.feed(line)?;
        }
    }
    let log = m.log.take().unwrap_or_default();
    Ok((m.finish()?, log))
}

/// The abbreviated blob ids of an `index` line, as git prints them (the abbreviation length is the one gix
/// computes for the repository: `core.abbrev`, else from the object count; an absent side is zeros).
fn abbreviated(
    repo: &gix::Repository,
    old: Option<ObjectId>,
    new: Option<ObjectId>,
) -> Result<(String, String)> {
    let short = |id: Option<ObjectId>| -> Result<Option<String>> {
        match id {
            Some(id) => Ok(Some(repo.find_object(id)?.id().shorten()?.to_string())),
            None => Ok(None),
        }
    };
    let (o, n) = (short(old)?, short(new)?);
    let len = o.as_ref().or(n.as_ref()).map_or(7, |s| s.len());
    Ok((
        o.unwrap_or_else(|| "0".repeat(len)),
        n.unwrap_or_else(|| "0".repeat(len)),
    ))
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

/// The diff text git prints for one entry (`git diff-tree -p`), as lines.
fn render_pair(repo: &gix::Repository, pair: &Pair) -> Result<Vec<Line>> {
    let (old, new) = (pair.old, pair.new);
    let mut out: Vec<Line> = Vec::new();
    let text = |out: &mut Vec<Line>, l: &[u8]| out.push(Line::Text(l.to_vec()));
    // git quotes names in diff headers (core.quotePath=true) and hashes the raw bytes
    let header = [
        quote_path(b"a/", pair.old_path.as_ref()),
        quote_path(b"b/", pair.new_path.as_ref()),
    ];
    let mut line = b"diff --git ".to_vec();
    line.extend_from_slice(&header[0]);
    line.push(b' ');
    line.extend_from_slice(&header[1]);
    text(&mut out, &line);
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
        (None, Some((k, _))) => text(
            &mut out,
            format!("new file mode {}", mode_str(k)).as_bytes(),
        ),
        (Some((k, _)), None) => text(
            &mut out,
            format!("deleted file mode {}", mode_str(k)).as_bytes(),
        ),
        (Some((ko, _)), Some((kn, _))) if ko != kn => {
            text(&mut out, format!("old mode {}", mode_str(ko)).as_bytes());
            text(&mut out, format!("new mode {}", mode_str(kn)).as_bytes());
        }
        _ => {}
    }
    if let Some(percent) = pair.similarity {
        text(&mut out, format!("similarity index {percent}%").as_bytes());
        // the words stand outside the quotes of a quoted name
        for (words, path) in [
            (&b"rename from "[..], &pair.old_path),
            (&b"rename to "[..], &pair.new_path),
        ] {
            let mut l = words.to_vec();
            l.extend_from_slice(&quote_path(b"", path.as_ref()));
            text(&mut out, &l);
        }
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
        return Ok(out);
    }
    if is_binary(&before) || is_binary(&after) {
        let (pre, post) = abbreviated(repo, old.map(|o| o.1), new.map(|n| n.1))?;
        let full = |x: Option<(EntryKind, ObjectId)>| {
            x.map(|(_, id)| id.to_string())
                .unwrap_or_else(|| "0".repeat(40))
        };
        out.push(Line::Index {
            pre,
            post,
            pre_full: full(old),
            post_full: full(new),
        });
        let mut l = b"Binary files ".to_vec();
        l.extend_from_slice(&old_name);
        l.extend_from_slice(b" and ");
        l.extend_from_slice(&new_name);
        l.extend_from_slice(b" differ");
        text(&mut out, &l);
        return Ok(out);
    }
    render_hunks(&mut out, &before, &after, &old_name, &new_name);
    Ok(out)
}
