//! The store of conditional-request validators of the watcher (the Go `Conditional`): the ETag, Link header and
//! items of the stored list pages, the ETag and SHA of each base branch tip, and one ETag per detail or check path.
//! It lives in memory during a run and is part of the state of the core between runs.
//!
//! Layout (`schema_version` 1, new Rust format, not the Go `state.json`):
//!
//! ```json
//! {"schema_version": 1,
//!  "list": {"per_page": 25, "pages": [{"etag": "\"p1\"", "link": "", "items": [{"number": 1, "title": "t",
//!           "user": "u", "labels": [], "draft": false, "head_sha": "<40 hex>", "head_ref": "r", "base_ref": "main",
//!           "base_sha": ""}]}]},
//!  "tips": {"main": {"etag": "\"t\"", "sha": "<40 hex>"}},
//!  "validators": {"pulls/7": "\"d\"", "checks/<40 hex>/runs": "\"r\"", "checks/<40 hex>/statuses": "\"s\""}}
//! ```
//!
//! `list.per_page` is 0 (empty list), 25 or 12 and says what size the stored pages have. Keys are in a fixed order and
//! maps are sorted, so equal stores give equal bytes. `from_json` is strict: unknown fields, other versions, bad
//! validators, SHAs, keys or sizes are refused, and the caller starts with an empty store (one unconditional read
//! round). A store never holds a token or a response body other than the parsed list items.
use std::collections::BTreeMap;

use serde_json::Value;

use super::http::{PullItem, PullListState};

pub const SCHEMA_VERSION: u64 = 1;
/// Bounds that keep a damaged or hostile state file from growing without limit.
pub const MAX_VALIDATORS: usize = 20_000;
pub const MAX_TIPS: usize = 5_000;
pub const MAX_LINK: usize = 8_192;
pub const MAX_ITEM_TEXT: usize = 65_536;
pub const MAX_ITEMS_PER_PAGE: usize = 25;

/// One branch-tip read.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct TipEntry {
    pub etag: String,
    pub sha: String,
}

/// A stored value that does not fit the layout.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CondError(pub String);

impl std::fmt::Display for CondError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "validator store: {}", self.0)
    }
}

impl std::error::Error for CondError {}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CondStore {
    pub list: PullListState,
    /// Base branch name to its last tip.
    pub tips: BTreeMap<String, TipEntry>,
    /// Request path (`pulls/N`, `checks/SHA/runs`, `checks/SHA/statuses`) to its ETag.
    pub validators: BTreeMap<String, String>,
}

/// The key of the validator of `pulls/N`.
pub fn detail_key(number: u64) -> String {
    format!("pulls/{number}")
}

/// The key of the validator of the check runs (`which` = `runs`) or statuses of a head.
pub fn checks_key(sha: &str, which: &str) -> String {
    format!("checks/{sha}/{which}")
}

impl CondStore {
    /// Drops the validators of PRs and heads that are no longer open (stub).
    pub fn prune(&mut self, _items: &[PullItem]) {}

    /// The serialized form (stub).
    pub fn to_json(&self) -> Value {
        Value::Null
    }

    /// Reads the serialized form back (stub).
    pub fn from_json(_v: &Value) -> Result<CondStore, CondError> {
        Err(CondError("stub".into()))
    }
}
