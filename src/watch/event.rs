//! The events of `stw watch`: the kinds of schema v1, their details, the event ids, the strict JSON line and the text
//! sanitizing. An event is one line of `journal.jsonl` and of stdout; no key is ever omitted.
//!
//! Event ids are `REPO#PR:KIND:KEY` (`-` for an absent PR; `-:KIND:KEY` without a repository). The key makes the id
//! unique and stable per transition: a counter (epoch, generation, episode) that only grows, plus what identifies the
//! change (head sha, merge commit, error code). `id_counter` reads a counter back from an id, which is how `apply` raises
//! the counters of the state from the journal.
use std::fmt;

use serde::ser::SerializeStruct;
use serde::{Deserialize, Serialize, Serializer};
use serde_json::Value;

/// `schema_version` of every event line.
pub const SCHEMA_VERSION: u32 = 1;
/// Longest text of a `watch-error` message in bytes (after masking).
pub const MAX_ERROR_MESSAGE: usize = 512;
/// Longest text of any other free-text detail in bytes (after masking).
pub const MAX_DETAIL_TEXT: usize = 2048;

/// The closed v1 enum of event kinds. `review-state-changed` is reserved and never emitted, so it is not a kind.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub enum Kind {
    NewPr,
    HeadChanged,
    ChecksGreen,
    ChecksRed,
    ChecksPendingTimeout,
    MergeableChanged,
    ReadyStateChanged,
    BaseChanged,
    Merged,
    Closed,
    RestackNeeded,
    ReposChanged,
    WatchError,
    WatchRecovered,
    WatchStalled,
}

impl Kind {
    pub const ALL: [Kind; 15] = [
        Kind::NewPr,
        Kind::HeadChanged,
        Kind::ChecksGreen,
        Kind::ChecksRed,
        Kind::ChecksPendingTimeout,
        Kind::MergeableChanged,
        Kind::ReadyStateChanged,
        Kind::BaseChanged,
        Kind::Merged,
        Kind::Closed,
        Kind::RestackNeeded,
        Kind::ReposChanged,
        Kind::WatchError,
        Kind::WatchRecovered,
        Kind::WatchStalled,
    ];

    /// The wire name (`new-pr`, `checks-pending-timeout`, ...).
    pub fn as_str(self) -> &'static str {
        match self {
            Kind::NewPr => "new-pr",
            Kind::HeadChanged => "head-changed",
            Kind::ChecksGreen => "checks-green",
            Kind::ChecksRed => "checks-red",
            Kind::ChecksPendingTimeout => "checks-pending-timeout",
            Kind::MergeableChanged => "mergeable-changed",
            Kind::ReadyStateChanged => "ready-state-changed",
            Kind::BaseChanged => "base-changed",
            Kind::Merged => "merged",
            Kind::Closed => "closed",
            Kind::RestackNeeded => "restack-needed",
            Kind::ReposChanged => "repos-changed",
            Kind::WatchError => "watch-error",
            Kind::WatchRecovered => "watch-recovered",
            Kind::WatchStalled => "watch-stalled",
        }
    }

    /// The kind of a wire name; `None` for an unknown name (also for the reserved `review-state-changed`).
    pub fn parse(name: &str) -> Option<Kind> {
        Kind::ALL.into_iter().find(|k| k.as_str() == name)
    }
}

/// Reasons of `restack-needed`.
pub const REASON_CHILD_OF_MERGED: &str = "child-of-merged-branch";
pub const REASON_CONTAINS_MERGED: &str = "contains-merged-commits";

/// A repository that was found but is not watched, with the reason.
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct Skipped {
    pub path: String,
    pub reason: String,
}

// The details of every kind: exactly the keys of the contract, in this order. Missing keys decode to the zero value and
// unknown keys are refused; lists are never null.

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct NewPr {
    pub title: String,
    pub author: String,
    pub draft: bool,
    pub base: String,
    pub head_ref: String,
    pub head: String,
    /// The head repository differs from the base repository.
    pub fork: bool,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct HeadChanged {
    pub old: String,
    pub new: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct ChecksGreen {
    pub head: String,
    pub draft: bool,
    pub base: String,
    pub total: u64,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct ChecksRed {
    pub head: String,
    pub failing: Vec<String>,
    pub failing_total: u64,
    pub shape: String,
    pub base: String,
    pub draft: bool,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct PendingTimeout {
    pub head: String,
    pub pending: Vec<String>,
    pub since: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct MergeableChanged {
    /// `None`: GitHub has not computed it.
    pub mergeable: Option<bool>,
    pub state: String,
    pub conflicting: bool,
    pub behind: bool,
    pub base: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct ReadyStateChanged {
    pub from: String,
    pub to: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct BaseChanged {
    pub old: String,
    pub new: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct Merged {
    /// `None`: the merge commit could not be read.
    pub merge_commit: Option<String>,
    pub base: String,
    pub head: String,
    pub head_ref: String,
    pub merged_at: String,
    pub method_hint: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct Closed {
    pub head: String,
    pub base: String,
}

/// `argv` is the command of the Go watcher (owner decision D-20261119-03: kept, an exception to D-20261103-03).
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct RestackNeeded {
    pub merged_pr: u64,
    pub base: String,
    pub reason: String,
    pub old_parent: String,
    pub new_parent: Option<String>,
    pub new_parent_source: String,
    pub branch: String,
    pub argv: Option<Vec<String>>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct ReposChanged {
    pub added: Vec<String>,
    pub removed: Vec<String>,
    pub skipped: Vec<Skipped>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct WatchError {
    pub code: String,
    pub message: String,
    pub failures: u64,
    pub retry_in_seconds: u64,
    pub repo: Option<String>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct WatchRecovered {
    pub after_failures: u64,
    pub repo: Option<String>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct WatchStalled {
    pub since: String,
    pub threshold_seconds: u64,
    pub repo: String,
}

/// The details of an event; the variant is the kind.
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(untagged)]
pub enum Details {
    NewPr(NewPr),
    HeadChanged(HeadChanged),
    ChecksGreen(ChecksGreen),
    ChecksRed(ChecksRed),
    ChecksPendingTimeout(PendingTimeout),
    MergeableChanged(MergeableChanged),
    ReadyStateChanged(ReadyStateChanged),
    BaseChanged(BaseChanged),
    Merged(Merged),
    Closed(Closed),
    RestackNeeded(RestackNeeded),
    ReposChanged(ReposChanged),
    WatchError(WatchError),
    WatchRecovered(WatchRecovered),
    WatchStalled(WatchStalled),
}

impl Details {
    pub fn kind(&self) -> Kind {
        match self {
            Details::NewPr(_) => Kind::NewPr,
            Details::HeadChanged(_) => Kind::HeadChanged,
            Details::ChecksGreen(_) => Kind::ChecksGreen,
            Details::ChecksRed(_) => Kind::ChecksRed,
            Details::ChecksPendingTimeout(_) => Kind::ChecksPendingTimeout,
            Details::MergeableChanged(_) => Kind::MergeableChanged,
            Details::ReadyStateChanged(_) => Kind::ReadyStateChanged,
            Details::BaseChanged(_) => Kind::BaseChanged,
            Details::Merged(_) => Kind::Merged,
            Details::Closed(_) => Kind::Closed,
            Details::RestackNeeded(_) => Kind::RestackNeeded,
            Details::ReposChanged(_) => Kind::ReposChanged,
            Details::WatchError(_) => Kind::WatchError,
            Details::WatchRecovered(_) => Kind::WatchRecovered,
            Details::WatchStalled(_) => Kind::WatchStalled,
        }
    }

    /// Decodes the `details` object of `kind`: unknown keys are refused, missing keys are the zero value.
    pub fn decode(kind: Kind, value: Value) -> Result<Details, String> {
        let _ = (kind, value);
        Err("not implemented".to_string())
    }
}

/// One event: a line of the journal and of stdout. `seq` is 0 until the journal assigns it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Event {
    pub seq: u64,
    pub id: String,
    /// RFC 3339 UTC with seconds (`2026-10-22T09:00:00Z`).
    pub time: String,
    pub repo: Option<String>,
    pub pr: Option<u64>,
    pub details: Details,
}

impl Event {
    pub fn new(
        id: String,
        time: String,
        repo: Option<String>,
        pr: Option<u64>,
        details: Details,
    ) -> Event {
        Event {
            seq: 0,
            id,
            time,
            repo,
            pr,
            details,
        }
    }

    pub fn kind(&self) -> Kind {
        self.details.kind()
    }

    /// The JSON line without the newline: keys `schema_version, seq, id, time, repo, pr, kind, details`, never omitted.
    pub fn to_line(&self) -> String {
        String::new()
    }

    /// Decodes a line strictly: one JSON object, schema version 1, a known kind, exact detail keys, no trailing data.
    pub fn from_line(line: &str) -> Result<Event, String> {
        let _ = line;
        Err("not implemented".to_string())
    }
}

impl Serialize for Event {
    fn serialize<S: Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        let mut st = s.serialize_struct("Event", 8)?;
        st.serialize_field("schema_version", &SCHEMA_VERSION)?;
        st.serialize_field("seq", &self.seq)?;
        st.serialize_field("id", &self.id)?;
        st.serialize_field("time", &self.time)?;
        st.serialize_field("repo", &self.repo)?;
        st.serialize_field("pr", &self.pr)?;
        st.serialize_field("kind", self.kind().as_str())?;
        st.serialize_field("details", &self.details)?;
        st.end()
    }
}

impl fmt::Display for Event {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.to_line())
    }
}

/// `REPO#PR:KIND:KEY`; `-` for an absent PR; without a repository (or with an empty one) `-:KIND:KEY`.
pub fn event_id(repo: Option<&str>, pr: Option<u64>, kind: Kind, key: &str) -> String {
    let prefix = match repo.filter(|r| !r.is_empty()) {
        Some(r) => match pr {
            Some(n) => format!("{r}#{n}"),
            None => format!("{r}#-"),
        },
        None => "-".to_string(),
    };
    format!("{prefix}:{}:{key}", kind.as_str())
}
/// `new-pr` and `closed`: `g<generation>`.
pub fn key_generation(generation: u64) -> String {
    format!("g{generation}")
}
/// `head-changed`: `<new head>:h<head epoch>`.
pub fn key_head(sha: &str, epoch: u64) -> String {
    format!("{sha}:h{epoch}")
}
/// `checks-green` and `checks-red`: `<head>:c<checks epoch>`.
pub fn key_checks(head: &str, epoch: u64) -> String {
    format!("{head}:c{epoch}")
}
/// `checks-pending-timeout`: `<head>:c<checks epoch>:t`.
pub fn key_timeout(head: &str, epoch: u64) -> String {
    format!("{}:t", key_checks(head, epoch))
}
/// `mergeable-changed`: `<head>:m<mergeable epoch>`.
pub fn key_mergeable(head: &str, epoch: u64) -> String {
    format!("{head}:m{epoch}")
}
/// `ready-state-changed`: `r<ready epoch>`.
pub fn key_ready(epoch: u64) -> String {
    format!("r{epoch}")
}
/// `base-changed`: `b<base epoch>`.
pub fn key_base(epoch: u64) -> String {
    format!("b{epoch}")
}
/// `repos-changed`: `p<repos epoch>`.
pub fn key_repos(epoch: u64) -> String {
    format!("p{epoch}")
}
/// `watch-recovered` after a `watch-error` episode: `e<episode>`.
pub fn key_recovered_error(episode: u64) -> String {
    format!("e{episode}")
}
/// `watch-stalled` and `watch-recovered` after it: `s<stall episode>`.
pub fn key_stall(episode: u64) -> String {
    format!("s{episode}")
}
/// `watch-error`: `<code>:e<episode>:f<failures>`.
pub fn key_error(code: &str, episode: u64, failures: u64) -> String {
    format!("{code}:e{episode}:f{failures}")
}
/// `merged`: the merge commit, `unknown` when it could not be read.
pub fn key_merged(commit: Option<&str>) -> String {
    match commit {
        Some(c) if !c.is_empty() => c.to_string(),
        _ => "unknown".to_string(),
    }
}
/// `restack-needed`: `<merged pr>:<merge commit or unknown>`.
pub fn key_restack(merged_pr: u64, commit: Option<&str>) -> String {
    format!("{merged_pr}:{}", key_merged(commit))
}

/// The counter an id carries for `letter` (`h`, `c`, `m`, `r`, `b`, `p`, `g`, `e`, `s`): the colon-separated part made of
/// the letter and one to eight digits, searched from the end and never in the first part (the repository). The parts are
/// short, so a hex SHA never matches.
pub fn id_counter(id: &str, letter: char) -> Option<u64> {
    let parts: Vec<&str> = id.split(':').collect();
    for p in parts.iter().skip(1).rev() {
        let digits = match p.strip_prefix(letter) {
            Some(d) => d,
            None => continue,
        };
        if digits.is_empty() || digits.len() > 8 || !digits.bytes().all(|b| b.is_ascii_digit()) {
            continue;
        }
        if let Ok(n) = digits.parse::<u64>() {
            return Some(n);
        }
    }
    None
}

/// Text from outside (an error message, a title) made safe for a JSON line and for any terminal: bytes that are not UTF-8
/// as `\xNN`, then the project's escape rule (`\`, control, invisible and direction-changing characters as `\n`, `\u{..}`),
/// then cut to at most `max_bytes` bytes at a character boundary. Go replaces control characters by a space and invalid
/// bytes by `?`; the Rust rule keeps the information (README deviation).
pub fn sanitize(text: &[u8], max_bytes: usize) -> String {
    let _ = (text, max_bytes);
    String::new()
}

/// Seconds since the epoch as `YYYY-MM-DDTHH:MM:SSZ` (UTC, no fraction). Years before 0 and after 9999 are not formatted
/// meaningfully; the clock of the watcher never gets there.
pub fn format_utc(secs: i64) -> String {
    let _ = secs;
    String::new()
}
