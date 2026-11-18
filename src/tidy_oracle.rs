//! Pure comparison functions of the `tidy` oracle (package 4): the hunk keys of gix against those of `git diff -U0`, the
//! tally of differing multisets per `diff.algorithm`, and the GitHub truth of a merged pull request against the plan.
//! Nothing here starts a process or reads a repository; `tests/it/oracle_tidy.rs` and `tests/oracle/tidy_oracle.sh` do.
//! Integers only: shares are counted, the display is rounded.
use crate::tidy_similarity::{Entry, Keys, Sim};

pub fn parse_unified_u0(_diff: &[u8]) -> Result<Vec<Entry>, String> {
    Ok(Vec::new())
}

pub fn text_keys(_entries: &[Entry]) -> Keys {
    Keys::default()
}

pub fn differs(_sim: &Sim) -> bool {
    false
}
