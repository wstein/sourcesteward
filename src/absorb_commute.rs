// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! The commutation test of `stw absorb`: can a staged hunk be moved before the changes of a commit? The newest commit of the
//! stack with which it cannot is the one that owns the hunk.
//!
//! `Hunk::anchors`, `commute`, `commute_diff_before` and `uniform` are a translation of the algorithm of git-absorb 0.9.0
//! (`src/commute.rs` and `src/owned.rs`), BSD 3-Clause License, Copyright (c) 2018, Stephen Jung. The full license text is
//! in `LICENSE-git-absorb-BSD-3` in the directory of this crate; it must stay with every redistribution of these parts,
//! in source and in binary form (documentation or other materials). The translation comes from the Python spike
//! `spikes/stw/absorb/stw-absorb-plan`, which carries the same notice.

/// One zero-context hunk. `add_start` and `rem_start` are the numbers of the hunk header (`@@ -rem_start,len +add_start,len @@`):
/// for a side without lines the number is that of the line before. Lines keep their newline (the last line of a file may
/// have none).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Hunk {
    pub add_start: i64,
    pub add: Vec<Vec<u8>>,
    pub rem_start: i64,
    pub rem: Vec<Vec<u8>>,
}

impl Hunk {
    /// The last unchanged line before and the first one after the hunk, on the removed side and then on the added side.
    pub fn anchors(&self) -> (i64, i64, i64, i64) {
        let (r, a) = (self.rem.len() as i64, self.add.len() as i64);
        if r == 0 && a == 0 {
            return (0, 1, 0, 1);
        }
        if a == 0 {
            return (
                self.rem_start - 1,
                self.rem_start + r,
                self.rem_start - 1,
                self.rem_start,
            );
        }
        if r == 0 {
            return (
                self.add_start - 1,
                self.add_start,
                self.add_start - 1,
                self.add_start + a,
            );
        }
        (
            self.rem_start - 1,
            self.rem_start + r,
            self.add_start - 1,
            self.add_start + a,
        )
    }

    /// Lines added minus lines removed.
    pub fn offset(&self) -> i64 {
        self.add.len() as i64 - self.rem.len() as i64
    }

    pub fn shifted(&self, add: i64, rem: i64) -> Hunk {
        Hunk {
            add_start: self.add_start + add,
            add: self.add.clone(),
            rem_start: self.rem_start + rem,
            rem: self.rem.clone(),
        }
    }
}

pub fn uniform(items: &[&[u8]]) -> bool {
    items.iter().all(|x| *x == items[0])
}

/// `second` as it would be if it had been applied before `first`; `None` when the two overlap and cannot be interleaved.
pub fn commute(first: &Hunk, second: &Hunk) -> Option<Hunk> {
    let (_, _, f_up, f_low) = first.anchors();
    let (s_up, s_low, _, _) = second.anchors();
    if f_low <= s_up {
        let off = -first.offset();
        return Some(second.shifted(off, off));
    }
    if s_low <= f_up {
        return Some(second.clone());
    }
    let all = |a: &[Vec<u8>], b: &[Vec<u8>]| {
        let items: Vec<&[u8]> = a.iter().chain(b).map(Vec::as_slice).collect();
        uniform(&items)
    };
    if (first.add.is_empty() && second.add.is_empty() && all(&first.rem, &second.rem))
        || (first.rem.is_empty() && second.rem.is_empty() && all(&first.add, &second.add))
    {
        return Some(second.clone());
    }
    None
}

/// `after` moved before all the hunks of one commit's patch (last hunk first); `None` when one of them does not commute.
pub fn commute_diff_before(after: &Hunk, before: &[Hunk]) -> Option<Hunk> {
    let mut cur = after.clone();
    for h in before.iter().rev() {
        cur = commute(h, &cur)?;
    }
    Some(cur)
}

/// The lines of `blob`, each with its newline; a last line without newline has none.
pub fn split_lines(blob: &[u8]) -> Vec<&[u8]> {
    let mut out: Vec<&[u8]> = blob.split_inclusive(|b| *b == b'\n').collect();
    if blob.is_empty() {
        out.clear();
    }
    out
}

/// Consistency check of a commuted hunk: its removed lines are where it says in the blob (a pure insertion: the position exists).
pub fn lines_match(blob: &[u8], hunk: &Hunk) -> bool {
    let ls = split_lines(blob);
    if !hunk.rem.is_empty() {
        let start = hunk.rem_start - 1;
        let Ok(start) = usize::try_from(start) else {
            return false;
        };
        return start + hunk.rem.len() <= ls.len()
            && ls[start..start + hunk.rem.len()]
                .iter()
                .zip(&hunk.rem)
                .all(|(a, b)| *a == b.as_slice());
    }
    hunk.add_start >= 1 && hunk.add_start <= ls.len() as i64 + 1
}
