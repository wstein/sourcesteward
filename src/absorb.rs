// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! `stw absorb` (new behavior, no Go counterpart): the read-only PLAN of folding the staged changes into the unpushed
//! commits that own the changed lines. STUB: the real implementation follows.
use std::path::Path;

use serde_json::{json, Value};

use crate::git_runner::Runner;

/// Commits in the stack without `--base`.
pub const DEFAULT_MAX_STACK: u32 = 10;
/// `schema_version` of the JSON document.
pub const SCHEMA_VERSION: u32 = 1;

pub struct Options {
    pub base: Option<String>,
    pub max_stack: usize,
    pub ambiguous_newest: bool,
}

/// A refusal with a typed code.
#[derive(Debug)]
pub struct Fail {
    pub code: &'static str,
    pub message: String,
}

impl Fail {
    pub fn to_json(&self) -> Value {
        json!({"schema_version": SCHEMA_VERSION, "command": "absorb",
            "error": {"code": self.code, "message": self.message}})
    }
}

pub struct Plan {
    doc: Value,
}

impl Plan {
    pub fn to_json(&self) -> &Value {
        &self.doc
    }
    pub fn render_human(&self) -> String {
        String::new()
    }
}

/// The variable that makes the repository ambiguous, if one is set.
pub fn environment_override() -> Option<Fail> {
    None
}

pub fn plan(_dir: &Path, _start: &Path, _runner: &Runner, _opts: &Options) -> Result<Plan, Fail> {
    Err(Fail {
        code: "not-implemented",
        message: "stw absorb is not implemented yet".into(),
    })
}

/// Bidi controls, zero-width characters and line and paragraph separators (the C0 and C1 controls are tested by range).
const MASKED: [char; 17] = [
    '\u{202a}', '\u{202b}', '\u{202c}', '\u{202d}', '\u{202e}', '\u{2066}', '\u{2067}', '\u{2068}',
    '\u{2069}', '\u{200e}', '\u{200f}', '\u{061c}', '\u{200b}', '\u{2060}', '\u{feff}', '\u{2028}',
    '\u{2029}',
];

/// Control, bidi and zero-width characters and line and paragraph separators become `?`.
pub fn sanitize(text: &str) -> String {
    text.chars()
        .map(|c| {
            let n = c as u32;
            if n < 0x20 || (0x7f..0xa0).contains(&n) || MASKED.contains(&c) {
                '?'
            } else {
                c
            }
        })
        .collect()
}

/// `sanitize` for bytes: every byte of an invalid UTF-8 sequence becomes `?`.
pub fn sanitize_bytes(bytes: &[u8]) -> String {
    let mut out = String::new();
    for chunk in bytes.utf8_chunks() {
        out.push_str(&sanitize(chunk.valid()));
        out.extend(std::iter::repeat_n('?', chunk.invalid().len()));
    }
    out
}

/// Verdict of one placed hunk: `Ok` when it is absorbed, else the reason it is skipped. `consistent` is the check that
/// the removed lines are where the commuted hunk says in the target's blob; `origin_commits` the number of commits the
/// removed lines come from. A hunk that cannot be applied is a conflict before it is ambiguous.
pub fn decide(
    consistent: bool,
    origin_commits: usize,
    ambiguous_newest: bool,
) -> Result<(), &'static str> {
    if !consistent {
        return Err("conflict");
    }
    if origin_commits >= 2 && !ambiguous_newest {
        return Err("ambiguous");
    }
    Ok(())
}

/// The JSON text that the plan checksum covers: object keys sorted, no white space, every non-ASCII character as `\uXXXX`
/// (a character above the BMP as a surrogate pair). The encoding of the Python spike (`json.dumps(sort_keys=True,
/// separators=(",", ":"))`), so that the two programs agree on the checksum.
pub fn canonical_json(_value: &Value) -> String {
    String::new()
}

/// SHA-256 over `{"options":..,"plan":..,"raw_paths":[..]}` in the encoding of `canonical_json`. `raw_paths` are the
/// paths as bytes, unmasked; a byte of an invalid UTF-8 sequence is `\udcXX`.
pub fn plan_sum(_options: &Value, _raw_paths: &[Vec<u8>], _plan: &Value) -> String {
    String::new()
}
