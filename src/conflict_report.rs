//! Restack handoff reports. Only the explicitly supplied scratch store is writable.
use std::collections::BTreeMap;
use std::fs;
use std::io::{ErrorKind, Write};
use std::path::Path;

use gix::ObjectId;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

use crate::error::{Error, Result};
use crate::github::reads::escape_untrusted;
use crate::{replay, repo, restack_plan, sha256};

const SIDES: [&str; 3] = ["base", "current", "incoming"];

#[derive(Serialize, Deserialize)]
struct Pin {
    revision: String,
    branch: bool,
    expected: String,
}

impl Pin {
    fn resolve(&self, repo: &gix::Repository) -> Option<ObjectId> {
        if self.branch {
            repo::resolve(repo, &format!("refs/heads/{}", self.revision))
        } else {
            restack_plan::resolve(repo, &self.revision).ok()
        }
    }

    fn new(repo: &gix::Repository, revision: &str, branch: bool) -> Result<Self> {
        let mut pin = Self {
            revision: revision.to_owned(),
            branch,
            expected: String::new(),
        };
        pin.expected = pin
            .resolve(repo)
            .ok_or_else(|| Error::usage(format!("cannot resolve {}", visible(revision))))?
            .to_string();
        Ok(pin)
    }
}

fn visible(value: &str) -> String {
    escape_untrusted(value, 512).0
}

fn stale_pins(repo: &gix::Repository, pins: &[Pin]) -> Vec<Value> {
    pins.iter()
        .filter_map(|pin| {
            let observed = pin.resolve(repo).map(|id| id.to_string());
            (observed.as_deref() != Some(pin.expected.as_str())).then(|| {
                json!({"revision": visible(&pin.revision), "expected": pin.expected, "observed": observed})
            })
        })
        .collect()
}

fn unchanged_file(path: &Path) -> Result<Vec<u8>> {
    if !fs::symlink_metadata(path)?.is_file() {
        return Err(Error::repository(
            "a report or blob path is not a regular file",
        ));
    }
    Ok(fs::read(path)?)
}

fn save_exact(path: &Path, data: &[u8]) -> Result<()> {
    let mut pending = tempfile::NamedTempFile::new_in(path.parent().expect("a store file"))?;
    pending.write_all(data)?;
    match pending.persist_noclobber(path) {
        Ok(_) => Ok(()),
        Err(error) if error.error.kind() == ErrorKind::AlreadyExists => {
            if unchanged_file(path)? == data {
                Ok(())
            } else {
                Err(Error::repository(
                    "existing report or blob has different content",
                ))
            }
        }
        Err(error) => Err(error.error.into()),
    }
}

fn stage(
    mem: &gix::Repository,
    stage: replay::ConflictStage,
    blobs: &mut BTreeMap<ObjectId, Vec<u8>>,
) -> Result<Value> {
    let mode = format!("{:06o}", stage.mode);
    let oid = stage.id.to_string();
    if stage.mode == 0o160000 {
        return Ok(json!({"mode": mode, "oid": oid, "kind": "gitlink", "file": null}));
    }
    if let std::collections::btree_map::Entry::Vacant(entry) = blobs.entry(stage.id) {
        let blob = mem.find_blob(stage.id)?;
        entry.insert(blob.data.to_vec());
    }
    Ok(json!({"mode": mode, "oid": oid, "kind": "blob", "file": format!("{oid}.blob")}))
}

pub(crate) fn create(dir: &Path, store: &Path, onto: &str, branches: &[String]) -> Result<Value> {
    if branches.is_empty() {
        return Err(Error::usage("name at least one local stack branch"));
    }
    let request = restack_plan::parse_request(branches, Some(onto), None)
        .map_err(|error| Error::usage(error.to_string()))?;
    let repo = repo::open(dir)?;
    let store = store.canonicalize()?;
    if !store.is_dir() {
        return Err(Error::usage("report store must be a directory"));
    }
    for source in repo
        .workdir()
        .into_iter()
        .chain([repo.common_dir(), repo.git_dir()])
    {
        if store.starts_with(source.canonicalize()?) {
            return Err(Error::usage(
                "report store must be outside the source checkout and Git directory",
            ));
        }
    }
    let pins = std::iter::once(Pin::new(&repo, onto, false))
        .chain(branches.iter().map(|branch| Pin::new(&repo, branch, true)))
        .collect::<Result<Vec<_>>>()?;
    let mut blobs = BTreeMap::new();
    let mut versions = Vec::new();
    let plan = restack_plan::plan_recording_conflict(dir, &request, &mut |mem, branch, step| {
        // Include display paths with no unmerged stages (e.g. directory-rename evidence).
        let mut paths = step
            .conflicts
            .iter()
            .map(|c| (c.path.clone(), [None; 3]))
            .collect::<BTreeMap<_, _>>();
        paths.extend(step.conflict_stages.iter().cloned());
        for (path, stages) in paths {
            use gix::bstr::ByteSlice;
            let kind = step
                .conflicts
                .iter()
                .find(|c| c.path == path)
                .map(|c| c.kind);
            let mut value = json!({
                "branch": visible(branch), "commit": step.old.to_string(),
                "path": visible(&path.to_str_lossy()), "kind": kind,
            });
            for (side, entry) in SIDES.into_iter().zip(stages) {
                value[side] = match entry {
                    Some(entry) => stage(mem, entry, &mut blobs)?,
                    None => Value::Null,
                };
            }
            versions.push(value);
        }
        Ok(())
    })?;
    if versions.is_empty() {
        return Err(Error::repository("expected an explicit restack conflict"));
    }
    if plan.onto.to_string() != pins[0].expected
        || plan.levels.len() != branches.len()
        || plan
            .levels
            .iter()
            .zip(&pins[1..])
            .any(|(level, pin)| level.tip.to_string() != pin.expected)
        || !stale_pins(&repo::open(dir)?, &pins).is_empty()
    {
        return Err(Error::repository("an input revision moved while planning"));
    }
    let blocked: Vec<_> = plan
        .levels
        .iter()
        .filter(|level| level.state == restack_plan::LevelState::InheritedConflict)
        .map(|level| json!({"branch": visible(&level.branch), "tip": level.tip.to_string()}))
        .collect();
    let document = json!({
        "schema_version": 1,
        "inputs": pins,
        "plan": plan.to_json(),
        "conflict_versions": versions,
        "blocked_descendants": blocked,
    });
    let data = serde_json::to_vec(&document)?;
    let id = sha256::hex(&data);
    for (oid, bytes) in blobs {
        save_exact(&store.join(format!("{oid}.blob")), &bytes)?;
    }
    save_exact(&store.join(format!("{id}.json")), &data)?;
    Ok(json!({"schema_version": 1, "id": id}))
}

pub(crate) fn reopen(dir: &Path, store: &Path, id: &str) -> Result<Value> {
    if id.len() != 64 || !id.bytes().all(|c| matches!(c, b'0'..=b'9' | b'a'..=b'f')) {
        return Err(Error::usage(
            "expected a 64-digit lowercase conflict report ID",
        ));
    }
    let repo = repo::open(dir)?;
    let data = unchanged_file(&store.join(format!("{id}.json")))?;
    if sha256::hex(&data) != id {
        return Err(Error::repository(
            "report ID does not match the stored document",
        ));
    }
    let mut document: Value = serde_json::from_slice(&data)?;
    if document["schema_version"] != 1 {
        return Err(Error::repository("unsupported conflict report version"));
    }
    let versions = document["conflict_versions"]
        .as_array()
        .ok_or_else(|| Error::repository("invalid conflict versions in the report"))?;
    for version in versions {
        for side in SIDES {
            let entry = &version[side];
            if entry.is_null() || entry["kind"] == "gitlink" {
                continue;
            }
            let oid = entry["oid"]
                .as_str()
                .ok_or_else(|| Error::repository("invalid conflict blob reference"))?;
            let object = ObjectId::from_hex(oid.as_bytes())?;
            let name = format!("{object}.blob");
            if entry["kind"] != "blob" || entry["file"] != name {
                return Err(Error::repository("invalid conflict blob reference"));
            }
            let content = unchanged_file(&store.join(name))?;
            if gix::objs::compute_hash(object.kind(), gix::object::Kind::Blob, &content)? != object
            {
                return Err(Error::repository(format!(
                    "saved blob content does not match {object}"
                )));
            }
        }
    }
    let pins: Vec<Pin> = serde_json::from_value(document["inputs"].clone())?;
    document["stale"] = json!(stale_pins(&repo, &pins));
    document["id"] = json!(id);
    Ok(document)
}
