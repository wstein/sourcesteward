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

    // Hash queries may carry every Change-Id on the source commit (squash messages).
    // A direct Change-Id query is a single normalised id or prefix.
    let mut wanted_ids: Vec<String> = Vec::new();
    let mut source_sha: Option<ObjectId> = None;
    let mut hash_miss = false;
    let mut id_query = false;

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
                let from_range = ids_by_commit
                    .iter()
                    .find(|(id, _)| *id == full)
                    .map(|(_, ids)| ids.clone());
                wanted_ids = match from_range {
                    Some(ids) => ids,
                    None => change_id::parse_message(&message_of(repo, full).unwrap_or_default()),
                };
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
            id_query = true;
            wanted_ids = norm.into_iter().collect();
        }
    } else {
        let (norm, reject) = change_id::classify_query(query);
        if let Some(reason) = reject {
            doc.reason = Some(reason);
            return Ok(doc);
        }
        id_query = true;
        wanted_ids = norm.into_iter().collect();
    }

    // One candidate per matching (commit, Change-Id) pair. Hash-sourced ids match by
    // equality; a Change-Id query may match by prefix (D-20261127-11).
    let mut trailer_hits = Vec::new();
    for (sha, ids) in &ids_by_commit {
        for cid in ids {
            let ok = if id_query {
                wanted_ids
                    .iter()
                    .any(|wanted| change_id::matches(wanted, cid))
            } else {
                wanted_ids.iter().any(|wanted| cid == wanted)
            };
            if ok {
                trailer_hits.push(candidate(*sha, "trailer", Some(cid.clone())));
            }
        }
    }

    if trailer_hits.len() > 1 {
        let distinct_commits: HashSet<&str> =
            trailer_hits.iter().map(|c| c.commit.as_str()).collect();
        // Several Change-Ids on one commit from a prefix, or several commits, are ambiguous.
        if distinct_commits.len() > 1 || id_query {
            doc.confidence = "ambiguous";
            doc.method = Some("trailer");
            doc.change_id = wanted_ids.first().cloned();
            doc.candidates = trailer_hits;
            doc.reason = Some("ambiguous");
            return Ok(doc);
        }
        // Hash query: several source ids landed on the same commit in range — one exact tip.
        let hit = trailer_hits[0].clone();
        if source_sha
            .map(|s| !present_set.contains(&s))
            .unwrap_or(true)
        {
            doc.confidence = "exact";
            doc.method = Some("trailer");
            doc.commit = Some(hit.commit.clone());
            doc.change_id = hit.change_id.clone();
            doc.candidates = vec![hit];
            doc.reason = None;
            return Ok(doc);
        }
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
        doc.change_id = hit.change_id.clone().or_else(|| wanted_ids.first().cloned());
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
    crate::repo::open(path).map_err(|e| {
        Error::repository(format!("cannot open {}: {e}", path.display()))
    })
}
