//! Commit similarity for `stw tidy plan` (specification: `spikes/stw/tidy-similarity/README.md`). Stub: the tests come first.
use std::cmp::Ordering;

/// Intersection size and the sizes of both multisets: the integers every decision is made from.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Sim {
    pub inter: u64,
    pub a: u64,
    pub b: u64,
}

impl Sim {
    pub fn ge_pct(&self, _pct: u32) -> bool {
        unimplemented!()
    }
    pub fn permille(&self) -> u32 {
        unimplemented!()
    }
    pub fn shown(&self, _threshold_pct: u32) -> u32 {
        unimplemented!()
    }
    pub fn cmp_value(&self, _other: &Sim) -> Ordering {
        unimplemented!()
    }
    pub fn containment_in_a(&self) -> (u64, u64) {
        unimplemented!()
    }
    pub fn containment_in_b(&self) -> (u64, u64) {
        unimplemented!()
    }
}

pub fn fmt_permille(_p: u32) -> String {
    unimplemented!()
}

/// One changed line of a commit: the path, the sign (`+`, `-`, or a marker for binary, mode, empty files) and the line.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Entry {
    pub path: Vec<u8>,
    pub sign: u8,
    pub line: Vec<u8>,
}

/// ASCII white space removed at both ends.
pub fn trim_line(_raw: &[u8]) -> &[u8] {
    unimplemented!()
}

/// Sorted multisets of 64-bit hashes of the keys, with and without the path.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Keys {
    with_path: Vec<u64>,
    no_path: Vec<u64>,
}

impl Keys {
    pub fn from_entries(_entries: &[Entry]) -> Keys {
        unimplemented!()
    }
    pub fn len(&self) -> usize {
        self.with_path.len()
    }
    pub fn is_empty(&self) -> bool {
        self.with_path.is_empty()
    }
    pub fn similarity(&self, _other: &Keys) -> Sim {
        unimplemented!()
    }
    pub fn similarity_no_path(&self, _other: &Keys) -> Sim {
        unimplemented!()
    }
    /// The multiset of several commits together (a squash is the sum of its parts).
    pub fn sum(&self, _other: &Keys) -> Keys {
        unimplemented!()
    }
}
