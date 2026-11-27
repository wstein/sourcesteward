//! Read-only resolution of a commit SHA or Change-Id against an explicit rev range.
//!
//! First cut (SourceSteward #40): trailer search and SHA expansion only. Op-log and
//! patch-id fallbacks are later packages. The command writes nothing: no refs, index,
//! or worktree updates.
use std::collections::{HashSet, VecDeque};
use std::path::Path;

use gix::bstr::ByteSlice;
use gix::ObjectId;
use serde_json::{json, Value};

use crate::change_id;
use crate::error::{Error, Result};
use crate::output::{self, jopt};

const SCHEMA_VERSION: u32 = 1;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Candidate {
    pub commit: String,
    pub method: &'static str,
    pub change_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Document {
    pub query: String,
    pub range: Vec<String>,
    pub confidence: &'static str,
    pub method: Option<&'static str>,
    pub commit: Option<String>,
    pub change_id: Option<String>,
    pub candidates: Vec<Candidate>,
    pub oplog: &'static str,
    pub reason: Option<&'static str>,
}

impl Document {
    fn base(query: &str, range: &[String]) -> Self {
        Self {
            query: query.to_string(),
            range: range.to_vec(),
            confidence: "none",
            method: None,
            commit: None,
            change_id: None,
            candidates: Vec::new(),
            oplog: "absent",
            reason: Some("none"),
        }
    }

    pub fn to_json(&self) -> Value {
        json!({
            "schema_version": SCHEMA_VERSION,
            "command": "resolve",
            "engine": output::engine(),
            "query": self.query,
            "range": self.range,
            "confidence": self.confidence,
            "method": jopt(self.method),
            "commit": jopt(self.commit.as_deref()),
            "change_id": jopt(self.change_id.as_deref()),
            "candidates": self.candidates.iter().map(|c| json!({
                "commit": c.commit,
                "method": c.method,
                "change_id": jopt(c.change_id.as_deref()),
            })).collect::<Vec<_>>(),
            "oplog": self.oplog,
            "reason": jopt(self.reason),
        })
    }

    pub fn is_exact(&self) -> bool {
        self.confidence == "exact"
    }
}

fn candidate(commit: ObjectId, method: &'static str, change_id: Option<String>) -> Candidate {
    Candidate {
        commit: commit.to_string(),
        method,
        change_id,
    }
}

/// Resolve `rev` to a peeled commit object id.
pub fn resolve_commit(repo: &gix::Repository, rev: &str) -> Result<ObjectId> {
    let id = repo
        .rev_parse_single(rev)
        .map_err(|e| Error::usage(format!("cannot resolve {rev}: {e}")))?;
    let commit = id
        .object()
        .map_err(|e| Error::repository(e.to_string()))?
        .peel_to_commit()
        .map_err(|_| Error::usage(format!("{rev} is not a commit")))?;
    Ok(commit.id)
}

/// Expand a hex object name the way `git rev-parse --verify <q>^{{commit}}` does.
///
/// Returns `Ok(("unique", Some(id), []))`, `Ok(("ambiguous", None, commits))`, or
/// `Ok(("unknown", None, []))`.
pub fn expand_commit_query(
    repo: &gix::Repository,
    query: &str,
) -> Result<(&'static str, Option<ObjectId>, Vec<ObjectId>)> {
    let lowered = query.to_ascii_lowercase();
    // Full ids and unambiguous abbrevs through the normal rev-parse path.
    if let Ok(id) = repo.rev_parse_single(format!("{lowered}^{{commit}}").as_str()) {
        return Ok(("unique", Some(id.detach()), Vec::new()));
    }
    let prefix = match gix::hash::Prefix::from_hex(&lowered) {
        Ok(p) => p,
        Err(_) => return Ok(("unknown", None, Vec::new())),
    };
    let mut candidates: HashSet<ObjectId> = HashSet::new();
    let outcome = repo
        .objects
        .lookup_prefix(prefix, Some(&mut candidates))
        .map_err(|e| Error::repository(e.to_string()))?;
    match outcome {
        None => Ok(("unknown", None, Vec::new())),
        Some(Ok(id)) => {
            // Single object: accept only when it peels to a commit.
            match repo.find_commit(id) {
                Ok(c) => Ok(("unique", Some(c.id), Vec::new())),
                Err(_) => Ok(("unknown", None, Vec::new())),
            }
        }
        Some(Err(())) => {
            let mut commits = Vec::new();
            for id in candidates {
                if repo.find_commit(id).is_ok() {
                    commits.push(id);
                }
            }
            commits.sort_by_key(|id| id.to_string());
            if commits.is_empty() {
                Ok(("unknown", None, Vec::new()))
            } else if commits.len() == 1 {
                Ok(("unique", Some(commits[0]), Vec::new()))
            } else {
                Ok(("ambiguous", None, commits))
            }
        }
    }
}

/// Commits of a git rev-list style range (`A..B`, `B`, `A B ^C`, …), newest-first like `git log`.
pub fn commits_in_range(repo: &gix::Repository, range_args: &[String]) -> Result<Vec<ObjectId>> {
    if range_args.is_empty() {
        return Err(Error::usage("empty-range"));
    }
    let mut includes = Vec::new();
    let mut excludes = Vec::new();
    for arg in range_args {
        if let Some(rest) = arg.strip_prefix('^') {
            if rest.is_empty() {
                return Err(Error::usage("empty-range"));
            }
            excludes.push(resolve_commit(repo, rest)?);
            continue;
        }
        if arg.contains("...") {
            return Err(Error::usage(
                "three-dot ranges are not supported; use A..B or named tips",
            ));
        }
        if let Some((left, right)) = arg.split_once("..") {
            if right.is_empty() {
                return Err(Error::usage("empty tip in range"));
            }
            if !left.is_empty() {
                excludes.push(resolve_commit(repo, left)?);
            }
            includes.push(resolve_commit(repo, right)?);
            continue;
        }
        includes.push(resolve_commit(repo, arg)?);
    }
    if includes.is_empty() {
        return Err(Error::usage("empty-range"));
    }

    // Walk every include tip, hide excludes. Follow all parents (merge second parents too).
    let hidden: HashSet<ObjectId> = {
        let mut seen = HashSet::new();
        let mut queue: VecDeque<ObjectId> = excludes.into();
        while let Some(id) = queue.pop_front() {
            if !seen.insert(id) {
                continue;
            }
            if let Ok(c) = repo.find_commit(id) {
                for p in c.parent_ids() {
                    queue.push_back(p.detach());
                }
            }
        }
        seen
    };

    let mut seen = HashSet::new();
    let mut out = Vec::new();
    let mut queue: VecDeque<ObjectId> = includes.into();
    while let Some(id) = queue.pop_front() {
        if hidden.contains(&id) || !seen.insert(id) {
            continue;
        }
        out.push(id);
        let c = repo.find_commit(id)?;
        for p in c.parent_ids() {
            let pid = p.detach();
            if repo.has_object(pid) {
                queue.push_back(pid);
            }
        }
    }
    Ok(out)
}

fn message_of(repo: &gix::Repository, id: ObjectId) -> Result<String> {
    let c = repo.find_commit(id)?;
    match c.message_raw() {
        Ok(raw) => Ok(raw.to_str_lossy().into_owned()),
        Err(_) => {
            // Fall back to the decoded message when the raw bytes are unavailable.
            let decoded = c.decode()?;
            Ok(decoded.message.to_str_lossy().into_owned())
        }
    }
}

/// Options for a resolve run.
pub struct Opts<'a> {
    pub query: &'a str,
    pub range: &'a [String],
    /// Force Change-Id interpretation even when the query looks like a hex SHA.
    pub force_change_id: bool,
}

/// Resolve `query` against the commits of `range` in `repo`. Read-only.
pub fn resolve(repo: &gix::Repository, opts: Opts<'_>) -> Result<Document> {
    let query = opts.query.trim();
    let mut doc = Document::base(query, opts.range);
    if query.is_empty() {
        doc.reason = Some("bad-query");
        return Ok(doc);
    }

    let present = commits_in_range(repo, opts.range)?;
    let present_set: HashSet<ObjectId> = present.iter().copied().collect();
    let mut ids_by_commit: Vec<(ObjectId, Vec<String>)> = Vec::with_capacity(present.len());
    for &sha in &present {
        let ids = change_id::parse_message(&message_of(repo, sha)?);
        ids_by_commit.push((sha, ids));
    }

    let mut wanted_id: Option<String> = None;
    let mut source_sha: Option<ObjectId> = None;
    let mut hash_miss = false;

    let try_as_hash = !opts.force_change_id && change_id::looks_like_hash_query(query);
    if try_as_hash {
        let (status, full, abbrev_hits) = expand_commit_query(repo, query)?;
        if status == "ambiguous" {
            doc.confidence = "ambiguous";
            doc.method = Some("abbreviation");
            doc.candidates = abbrev_hits
                .into_iter()
                .map(|id| candidate(id, "abbreviation", None))
                .collect();
            doc.reason = Some("ambiguous-abbreviation");
            return Ok(doc);
        }
        if status == "unique" {
            if let Some(full) = full {
                source_sha = Some(full);
                // Prefer the Change-Id of the commit when it is in range; otherwise read its message.
                let from_range = ids_by_commit
                    .iter()
                    .find(|(id, _)| *id == full)
                    .map(|(_, ids)| ids.clone());
                let ids = match from_range {
                    Some(ids) => ids,
                    None => change_id::parse_message(&message_of(repo, full).unwrap_or_default()),
                };
                wanted_id = ids.into_iter().next();
            }
        } else {
            hash_miss = true;
            if query.len() < change_id::PREFIX_MIN {
                doc.reason = Some("unknown-object");
                return Ok(doc);
            }
            let (norm, reject) = change_id::classify_query(query);
            if reject.is_some() || norm.is_none() {
                doc.reason = Some("unknown-object");
                return Ok(doc);
            }
            wanted_id = norm;
        }
    } else {
        let (norm, reject) = change_id::classify_query(query);
        if let Some(reason) = reject {
            doc.reason = Some(reason);
            return Ok(doc);
        }
        wanted_id = norm;
    }

    // A hash query carries a full Change-Id from the source commit (equality only).
    // A Change-Id query may match by prefix of at least PREFIX_MIN.
    let mut trailer_hits = Vec::new();
    if let Some(ref wanted) = wanted_id {
        for (sha, ids) in &ids_by_commit {
            for cid in ids {
                let ok = if source_sha.is_some() {
                    cid == wanted
                } else {
                    change_id::matches(wanted, cid)
                };
                if ok {
                    trailer_hits.push(candidate(*sha, "trailer", Some(cid.clone())));
                    break;
                }
            }
        }
    }

    if trailer_hits.len() > 1 {
        doc.confidence = "ambiguous";
        doc.method = Some("trailer");
        doc.change_id = wanted_id;
        doc.candidates = trailer_hits;
        doc.reason = Some("ambiguous");
        return Ok(doc);
    }
    if trailer_hits.len() == 1
        && source_sha
            .map(|s| !present_set.contains(&s))
            .unwrap_or(true)
    {
        let hit = trailer_hits[0].clone();
        doc.confidence = "exact";
        doc.method = Some("trailer");
        doc.commit = Some(hit.commit.clone());
        // Prefer the canonical id from the trailer over a shorter query prefix.
        doc.change_id = hit.change_id.clone().or(wanted_id);
        doc.candidates = trailer_hits;
        doc.reason = None;
        return Ok(doc);
    }

    if let Some(source) = source_sha {
        if present_set.contains(&source) {
            let cid = ids_by_commit
                .iter()
                .find(|(id, _)| *id == source)
                .and_then(|(_, ids)| ids.first().cloned());
            doc.confidence = "exact";
            doc.method = Some("same");
            doc.commit = Some(source.to_string());
            doc.change_id = cid.clone();
            doc.candidates = vec![candidate(source, "same", cid)];
            doc.reason = None;
            return Ok(doc);
        }
    }

    doc.reason = Some(if hash_miss { "unknown-object" } else { "none" });
    Ok(doc)
}

/// Open a repository at `path` for a resolve run (direct path, like `stw inspect git`).
pub fn open_repo(path: &Path) -> Result<gix::Repository> {
    gix::open(path).map_err(|e| Error::repository(format!("cannot open {}: {e}", path.display())))
}
