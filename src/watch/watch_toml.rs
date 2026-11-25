//! The small `.stw/watch.toml` this command reads. Schema version 1, a fixed set of keys.
//! Strings are double-quoted. Lists are `["a", "b"]`. One `[ignore]` table.

use super::command::{parse_duration_secs, MIN_INTERVAL_SECS};

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct WatchFile {
    pub repos: Vec<String>,
    pub interval_secs: Option<u64>,
    pub max_open_prs: Option<u64>,
    pub detail_every: Option<u64>,
    pub ignore_authors: Vec<String>,
    pub ignore_labels: Vec<String>,
}

pub fn parse_watch_toml(text: &str) -> Result<WatchFile, String> {
    let mut file = WatchFile::default();
    let mut section = "";
    let mut saw_version = false;
    for (n, raw) in text.lines().enumerate() {
        let line = strip_comment(raw).trim();
        if line.is_empty() {
            continue;
        }
        if let Some(name) = line.strip_prefix('[').and_then(|s| s.strip_suffix(']')) {
            let name = name.trim();
            if name != "ignore" {
                return Err(format!("watch.toml:{}: unknown table {name:?}", n + 1));
            }
            section = "ignore";
            continue;
        }
        let (key, value) = line
            .split_once('=')
            .ok_or_else(|| format!("watch.toml:{}: expected key = value", n + 1))?;
        let key = key.trim();
        let value = value.trim();
        match (section, key) {
            ("", "schema_version") => {
                if value != "1" {
                    return Err("watch.toml: schema_version = 1 is required".into());
                }
                saw_version = true;
            }
            ("", "repos") => file.repos = string_list(value, "repos")?,
            ("", "interval") => {
                let text = quoted(value, "interval")?;
                let secs = parse_duration_secs(&text).map_err(|e| format!("watch.toml: {e}"))?;
                if secs < MIN_INTERVAL_SECS {
                    return Err(format!(
                        "watch.toml: interval {text} is below the minimum of {MIN_INTERVAL_SECS}s"
                    ));
                }
                file.interval_secs = Some(secs);
            }
            ("", "max_open_prs") => {
                let n = value
                    .parse::<u64>()
                    .map_err(|_| "watch.toml: max_open_prs must be an integer".to_string())?;
                if !(1..=2000).contains(&n) {
                    return Err("watch.toml: max_open_prs must be from 1 to 2000".into());
                }
                file.max_open_prs = Some(n);
            }
            ("", "detail_every") => {
                let n = value
                    .parse::<u64>()
                    .map_err(|_| "watch.toml: detail_every must be an integer".to_string())?;
                if n < 1 {
                    return Err("watch.toml: detail_every must be at least 1".into());
                }
                file.detail_every = Some(n);
            }
            ("ignore", "authors") => file.ignore_authors = string_list(value, "ignore.authors")?,
            ("ignore", "labels") => file.ignore_labels = string_list(value, "ignore.labels")?,
            _ => return Err(format!("watch.toml:{}: unknown key {key:?}", n + 1)),
        }
    }
    if !saw_version {
        return Err("watch.toml: schema_version = 1 is required".into());
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

fn strip_comment(line: &str) -> &str {
    let mut in_string = false;
    let bytes = line.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'"' => in_string = !in_string,
            b'#' if !in_string => return &line[..i],
            _ => {}
        }
        i += 1;
    }
    line
}

fn quoted(value: &str, key: &str) -> Result<String, String> {
    let v = value
        .strip_prefix('"')
        .and_then(|s| s.strip_suffix('"'))
        .ok_or_else(|| format!("watch.toml: {key} must be a quoted string"))?;
    if v.contains('"') {
        return Err(format!("watch.toml: {key} must be a quoted string"));
    }
    Ok(v.to_string())
}

fn string_list(value: &str, key: &str) -> Result<Vec<String>, String> {
    let inner = value
        .strip_prefix('[')
        .and_then(|s| s.strip_suffix(']'))
        .ok_or_else(|| format!("watch.toml: {key} must be an array of strings"))?;
    if inner.trim().is_empty() {
        return Ok(Vec::new());
    }
    inner
        .split(',')
        .map(|part| quoted(part.trim(), key))
        .collect()
}
