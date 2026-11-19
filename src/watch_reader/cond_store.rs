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

use serde_json::{json, Map, Value};

use super::http::{
    encode_branch_segment, valid_etag, valid_sha, PullItem, PullListState, PullPage, MAX_PAGES_12,
    MAX_PAGES_25,
};

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

fn bad<T>(what: &str) -> Result<T, CondError> {
    Err(CondError(what.to_string()))
}

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

fn valid_key(k: &str) -> bool {
    if let Some(n) = k.strip_prefix("pulls/") {
        return !n.is_empty()
            && n.len() <= 12
            && n.bytes().all(|b| b.is_ascii_digit())
            && !n.starts_with('0');
    }
    if let Some(rest) = k.strip_prefix("checks/") {
        return rest.split_once('/').is_some_and(|(sha, which)| {
            valid_sha(sha) && (which == "runs" || which == "statuses")
        });
    }
    false
}

fn item_json(i: &PullItem) -> Value {
    json!({"number": i.number, "title": i.title, "user": i.user, "labels": i.labels, "draft": i.draft,
           "head_sha": i.head_sha, "head_ref": i.head_ref, "base_ref": i.base_ref, "base_sha": i.base_sha})
}

fn only_keys(o: &Map<String, Value>, keys: &[&str], what: &str) -> Result<(), CondError> {
    if o.len() != keys.len() || !keys.iter().all(|k| o.contains_key(*k)) {
        return bad(&format!("{what} has other fields than {keys:?}"));
    }
    Ok(())
}

fn text(v: &Value, what: &str, max: usize) -> Result<String, CondError> {
    match v.as_str() {
        Some(s) if s.len() <= max => Ok(s.to_string()),
        _ => bad(&format!("{what} is not a text of at most {max} bytes")),
    }
}

fn etag(v: &Value, what: &str) -> Result<String, CondError> {
    let s = text(v, what, 256)?;
    if valid_etag(&s) {
        Ok(s)
    } else {
        bad(&format!("{what} is not a valid ETag"))
    }
}

fn item_from(v: &Value) -> Result<PullItem, CondError> {
    let o = v
        .as_object()
        .map_or_else(|| bad("an item is not an object"), Ok)?;
    only_keys(
        o,
        &[
            "number", "title", "user", "labels", "draft", "head_sha", "head_ref", "base_ref",
            "base_sha",
        ],
        "an item",
    )?;
    let number = o["number"].as_u64().filter(|n| *n >= 1);
    let (Some(number), Some(draft)) = (number, o["draft"].as_bool()) else {
        return bad("an item has a bad number or draft flag");
    };
    let mut labels = Vec::new();
    for l in o["labels"]
        .as_array()
        .map_or_else(|| bad("labels are no array"), Ok)?
    {
        labels.push(text(l, "a label", MAX_ITEM_TEXT)?);
    }
    let head_sha = text(&o["head_sha"], "head_sha", 40)?;
    let base_sha = text(&o["base_sha"], "base_sha", 40)?;
    if !valid_sha(&head_sha) || (!base_sha.is_empty() && !valid_sha(&base_sha)) {
        return bad("an item has a bad SHA");
    }
    Ok(PullItem {
        number,
        title: text(&o["title"], "title", MAX_ITEM_TEXT)?,
        user: text(&o["user"], "user", MAX_ITEM_TEXT)?,
        labels,
        draft,
        head_sha,
        head_ref: text(&o["head_ref"], "head_ref", MAX_ITEM_TEXT)?,
        base_ref: text(&o["base_ref"], "base_ref", MAX_ITEM_TEXT)?,
        base_sha,
    })
}

impl CondStore {
    /// Drops the validators of PRs and heads that are no longer open and the tips of bases that no PR targets, so that
    /// the store stays bounded.
    pub fn prune(&mut self, items: &[PullItem]) {
        let mut keep = std::collections::BTreeSet::new();
        let mut bases = std::collections::BTreeSet::new();
        for it in items {
            keep.insert(detail_key(it.number));
            keep.insert(checks_key(&it.head_sha, "runs"));
            keep.insert(checks_key(&it.head_sha, "statuses"));
            bases.insert(it.base_ref.as_str());
        }
        self.validators.retain(|k, _| keep.contains(k));
        self.tips.retain(|b, _| bases.contains(b.as_str()));
    }

    /// The serialized form (see the module documentation).
    pub fn to_json(&self) -> Value {
        let pages: Vec<Value> = self
            .list
            .pages
            .iter()
            .map(|p| {
                json!({"etag": p.etag, "link": p.link,
                       "items": p.items.iter().map(item_json).collect::<Vec<_>>()})
            })
            .collect();
        let tips: Map<String, Value> = self
            .tips
            .iter()
            .map(|(b, t)| (b.clone(), json!({"etag": t.etag, "sha": t.sha})))
            .collect();
        let validators: Map<String, Value> = self
            .validators
            .iter()
            .map(|(k, v)| (k.clone(), Value::String(v.clone())))
            .collect();
        json!({"schema_version": SCHEMA_VERSION,
               "list": {"per_page": self.list.per_page, "pages": pages},
               "tips": tips, "validators": validators})
    }

    /// Reads the serialized form back; strict (see the module documentation).
    pub fn from_json(v: &Value) -> Result<CondStore, CondError> {
        let o = v.as_object().map_or_else(|| bad("not an object"), Ok)?;
        only_keys(
            o,
            &["schema_version", "list", "tips", "validators"],
            "the store",
        )?;
        if o["schema_version"].as_u64() != Some(SCHEMA_VERSION) {
            return bad("schema_version is not 1");
        }
        let l = o["list"]
            .as_object()
            .map_or_else(|| bad("list is not an object"), Ok)?;
        only_keys(l, &["per_page", "pages"], "list")?;
        let per_page = match l["per_page"].as_u64() {
            Some(0) => 0u32,
            Some(12) => 12,
            Some(25) => 25,
            _ => return bad("list.per_page is not 0, 12 or 25"),
        };
        let pages_v = l["pages"]
            .as_array()
            .map_or_else(|| bad("pages are no array"), Ok)?;
        let limit = match per_page {
            0 => 0,
            12 => MAX_PAGES_12 as usize,
            _ => MAX_PAGES_25 as usize,
        };
        if pages_v.len() > limit {
            return bad("more list pages than the page bound of the size");
        }
        let mut pages = Vec::with_capacity(pages_v.len());
        for p in pages_v {
            let po = p
                .as_object()
                .map_or_else(|| bad("a page is not an object"), Ok)?;
            only_keys(po, &["etag", "link", "items"], "a page")?;
            let items_v = po["items"]
                .as_array()
                .map_or_else(|| bad("items are no array"), Ok)?;
            if items_v.len() > per_page as usize || items_v.len() > MAX_ITEMS_PER_PAGE {
                return bad("a page holds more items than its size");
            }
            let link = text(&po["link"], "link", MAX_LINK)?;
            if link.bytes().any(|b| b < 0x20 || b == 0x7f) {
                return bad("a link has a control character");
            }
            let etag_s = text(&po["etag"], "etag", 256)?;
            if !valid_etag(&etag_s) {
                return bad("a page etag is not a valid ETag");
            }
            pages.push(PullPage {
                etag: etag_s,
                link,
                items: items_v.iter().map(item_from).collect::<Result<_, _>>()?,
            });
        }
        let tips_o = o["tips"]
            .as_object()
            .map_or_else(|| bad("tips are no object"), Ok)?;
        if tips_o.len() > MAX_TIPS {
            return bad("too many tips");
        }
        let mut tips = BTreeMap::new();
        for (b, t) in tips_o {
            let to = t
                .as_object()
                .map_or_else(|| bad("a tip is not an object"), Ok)?;
            only_keys(to, &["etag", "sha"], "a tip")?;
            let sha = text(&to["sha"], "tip sha", 40)?;
            if encode_branch_segment(b).is_none() || !valid_sha(&sha) {
                return bad("a tip has a bad branch name or SHA");
            }
            tips.insert(
                b.clone(),
                TipEntry {
                    etag: etag(&to["etag"], "tip etag")?,
                    sha,
                },
            );
        }
        let val_o = o["validators"]
            .as_object()
            .map_or_else(|| bad("validators are no object"), Ok)?;
        if val_o.len() > MAX_VALIDATORS {
            return bad("too many validators");
        }
        let mut validators = BTreeMap::new();
        for (k, e) in val_o {
            if !valid_key(k) {
                return bad("a validator has a key that is no request path of the reader");
            }
            validators.insert(k.clone(), etag(e, "a validator")?);
        }
        Ok(CondStore {
            list: PullListState { per_page, pages },
            tips,
            validators,
        })
    }
}
