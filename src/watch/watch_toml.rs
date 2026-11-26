//! The strict version 1 watch configuration and its standalone template table.
use std::collections::BTreeMap;

use toml_edit::{DocumentMut, Item};

use super::command::{parse_duration_secs, MIN_INTERVAL_SECS};

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct WatchFile {
    pub repos: Vec<String>,
    pub interval_secs: Option<u64>,
    pub max_open_prs: Option<u64>,
    pub detail_every: Option<u64>,
    pub ignore_authors: Vec<String>,
    pub ignore_labels: Vec<String>,
    pub templates: BTreeMap<String, String>,
}

pub fn parse_watch_toml(text: &str) -> Result<WatchFile, String> {
    let config = parse(text)?;
    if config.get("schema_version").and_then(Item::as_integer) != Some(1) {
        return Err("watch.toml: schema_version = 1 is required".into());
    }
    let mut file = WatchFile::default();
    for (key, value) in config.iter() {
        match key {
            "schema_version" => {}
            "repos" => file.repos = strings(value, key)?,
            "interval" => {
                let text = value
                    .as_str()
                    .ok_or("watch.toml: interval must be a string")?;
                let secs = parse_duration_secs(text).map_err(|e| format!("watch.toml: {e}"))?;
                if secs < MIN_INTERVAL_SECS {
                    return Err(format!(
                        "watch.toml: interval {text} is below the minimum of {MIN_INTERVAL_SECS}s"
                    ));
                }
                file.interval_secs = Some(secs);
            }
            "max_open_prs" => {
                let n = integer(value, key)?;
                if !(1..=2000).contains(&n) {
                    return Err("watch.toml: max_open_prs must be from 1 to 2000".into());
                }
                file.max_open_prs = Some(n);
            }
            "detail_every" => {
                let n = integer(value, key)?;
                if n == 0 {
                    return Err("watch.toml: detail_every must be at least 1".into());
                }
                file.detail_every = Some(n);
            }
            "ignore" => {
                let ignore = value
                    .as_table()
                    .ok_or("watch.toml: ignore must be a table")?;
                for (key, value) in ignore {
                    match key {
                        "authors" => file.ignore_authors = strings(value, "ignore.authors")?,
                        "labels" => file.ignore_labels = strings(value, "ignore.labels")?,
                        _ => return Err(format!("watch.toml: unknown ignore key {key:?}")),
                    }
                }
            }
            "templates" => file.templates = templates(value)?,
            _ => return Err(format!("watch.toml: unknown key {key:?}")),
        }
    }
    for repo in &file.repos {
        if !crate::watch_reader::http::valid_repo(repo) {
            return Err(format!(
                "watch.toml: repos entry {repo:?} is not OWNER/NAME"
            ));
        }
    }
    Ok(file)
}

pub fn parse_templates_toml(text: &str) -> Result<BTreeMap<String, String>, String> {
    let doc = parse(text)?;
    if doc.len() != 1 {
        return Err("expected only a [templates] table".into());
    }
    templates(doc.get("templates").ok_or("expected a [templates] table")?)
}

fn parse(text: &str) -> Result<DocumentMut, String> {
    text.parse::<DocumentMut>().map_err(|e| e.to_string())
}

fn templates(value: &Item) -> Result<BTreeMap<String, String>, String> {
    value
        .as_table()
        .ok_or("templates must be a table")?
        .iter()
        .map(|(kind, template)| {
            let text = template
                .as_str()
                .ok_or_else(|| format!("template {kind:?} must be a string"))?;
            Ok((kind.to_owned(), text.to_owned()))
        })
        .collect()
}

fn integer(value: &Item, key: &str) -> Result<u64, String> {
    value
        .as_integer()
        .and_then(|v| u64::try_from(v).ok())
        .ok_or_else(|| format!("watch.toml: {key} must be a nonnegative integer"))
}

fn strings(value: &Item, key: &str) -> Result<Vec<String>, String> {
    let error = || format!("watch.toml: {key} must be an array of strings");
    value
        .as_array()
        .ok_or_else(error)?
        .iter()
        .map(|value| value.as_str().map(str::to_owned).ok_or_else(error))
        .collect()
}
