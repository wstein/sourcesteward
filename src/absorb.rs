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

/// Control, bidi and zero-width characters and invalid UTF-8 become `?`.
pub fn sanitize(text: &str) -> String {
    text.to_string()
}

/// `sanitize` for bytes: every byte of an invalid UTF-8 sequence becomes `?`.
pub fn sanitize_bytes(bytes: &[u8]) -> String {
    String::from_utf8_lossy(bytes).into_owned()
}

/// Verdict of one placed hunk: `Ok` when it is absorbed, else the reason it is skipped.
pub fn decide(
    _consistent: bool,
    _origin_commits: usize,
    _ambiguous_newest: bool,
) -> Result<(), &'static str> {
    Ok(())
}
