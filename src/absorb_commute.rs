// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! The commutation test of `stw absorb`. STUB: the real implementation follows.

/// One zero-context hunk.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Hunk {
    pub add_start: i64,
    pub add: Vec<Vec<u8>>,
    pub rem_start: i64,
    pub rem: Vec<Vec<u8>>,
}

impl Hunk {
    pub fn anchors(&self) -> (i64, i64, i64, i64) {
        (0, 0, 0, 0)
    }
    pub fn offset(&self) -> i64 {
        0
    }
    pub fn shifted(&self, _add: i64, _rem: i64) -> Hunk {
        self.clone()
    }
}

pub fn uniform(_items: &[&[u8]]) -> bool {
    false
}

/// `second` as if it had been applied before `first`; `None` when they cannot be interleaved.
pub fn commute(_first: &Hunk, _second: &Hunk) -> Option<Hunk> {
    None
}

/// `after` moved before all hunks of one commit's patch (last hunk first).
pub fn commute_diff_before(_after: &Hunk, _before: &[Hunk]) -> Option<Hunk> {
    None
}

/// The removed lines of `hunk` are where it says in `blob`.
pub fn lines_match(_blob: &[u8], _hunk: &Hunk) -> bool {
    false
}
