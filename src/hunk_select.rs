//! Selected work-tree hunks for `stw workspace commit-plan`.
//!
//! The list, plan and apply documents follow the spike `spikes/stw/hunk-select/`
//! (sourcesteward#39). Git runs only through `git_runner`.

use std::collections::BTreeMap;
use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};

use serde_json::{json, Value};

use crate::error::exit;
use crate::git_runner::{self, BlamePath, BranchRef, GitError, Hex40, Op, WorkspaceOp};

const SCHEMA: u8 = 1;
const COMMAND: &str = "hunk-select";

pub struct Fail {
    pub code: &'static str,
    pub message: String,
    pub status: u8,
}

impl Fail {
    fn new(code: &'static str, message: impl Into<String>, status: u8) -> Fail {
        Fail {
            code,
            message: message.into(),
            status,
        }
    }
}

fn git(root: &Path, op: WorkspaceOp) -> Result<Vec<u8>, Fail> {
    git_in(root, op, None)
}

fn git_in(root: &Path, op: WorkspaceOp, input: Option<&[u8]>) -> Result<Vec<u8>, Fail> {
    match git_runner::shared().run_with_stdin(root, Op::Workspace(op), input) {
        Ok(out) => Ok(out.stdout),
        Err(GitError::Exit { stderr, .. }) => Err(Fail::new("git", stderr, 1)),
        Err(error) => Err(Fail::new("git", error.to_string(), 1)),
    }
}

fn git_missing(root: &Path, op: WorkspaceOp) -> Result<Option<Vec<u8>>, Fail> {
    match git_runner::shared().run(root, Op::Workspace(op)) {
        Ok(out) => Ok(Some(out.stdout)),
        Err(GitError::Exit { .. }) => Ok(None),
        Err(error) => Err(Fail::new("git", error.to_string(), 1)),
    }
}

fn text(bytes: &[u8]) -> String {
    String::from_utf8_lossy(bytes).into_owned()
}

fn path_arg(path: &str) -> Result<BlamePath, Fail> {
    BlamePath::parse(path.as_bytes())
        .ok_or_else(|| Fail::new("bad-path", format!("refused path {path}"), 1))
}

fn unquote_token(token: &[u8]) -> Vec<u8> {
    if !(token.first() == Some(&b'"') && token.last() == Some(&b'"') && token.len() >= 2) {
        return token.to_vec();
    }
    let mut out = Vec::new();
    let mut i = 1;
    while i < token.len() - 1 {
        if token[i] == b'\\' && i + 1 < token.len() - 1 {
            let n = token[i + 1];
            let esc = match n {
                b'a' => Some(7),
                b'b' => Some(8),
                b't' => Some(9),
                b'n' => Some(10),
                b'v' => Some(11),
                b'f' => Some(12),
                b'r' => Some(13),
                b'"' => Some(34),
                b'\\' => Some(92),
                _ => None,
            };
            if let Some(value) = esc {
                out.push(value);
                i += 2;
                continue;
            }
            if i + 3 < token.len()
                && token[i + 1..i + 4]
                    .iter()
                    .all(|b| (b'0'..=b'7').contains(b))
            {
                let oct = std::str::from_utf8(&token[i + 1..i + 4]).unwrap_or("0");
                out.push(u8::from_str_radix(oct, 8).unwrap_or(0));
                i += 4;
                continue;
            }
        }
        out.push(token[i]);
        i += 1;
    }
    out
}

fn unquote_path(raw: &str) -> String {
    text(&unquote_token(raw.as_bytes()))
}

/// A path Git can name but this command will not list. The display form keeps two
/// non-UTF-8 names distinct: lossy UTF-8 would turn both into the same replacement.
fn path_fault(bytes: &[u8]) -> Option<&'static str> {
    if std::str::from_utf8(bytes).is_err() {
        return Some("non-utf8-path");
    }
    if bytes.contains(&b'\t') {
        return Some("tab-in-path");
    }
    None
}

fn display_path(bytes: &[u8]) -> String {
    match std::str::from_utf8(bytes) {
        Ok(path) if !path.contains('\t') => path.to_string(),
        _ => {
            let mut out = String::from("\"");
            for &byte in bytes {
                match byte {
                    b'\\' => out.push_str("\\\\"),
                    b'"' => out.push_str("\\\""),
                    b'\t' => out.push_str("\\t"),
                    0x20..=0x7e => out.push(byte as char),
                    _ => out.push_str(&format!("\\{byte:03o}")),
                }
            }
            out.push('"');
            out
        }
    }
}

fn classify_path(bytes: Vec<u8>) -> Result<String, (&'static str, String)> {
    if let Some(reason) = path_fault(&bytes) {
        let shown = display_path(&bytes);
        return Err((reason, shown));
    }
    Ok(String::from_utf8(bytes).unwrap_or_default())
}

fn path_message(reason: &str) -> &'static str {
    match reason {
        "non-utf8-path" => "path is not UTF-8",
        "tab-in-path" => "path contains a tab",
        "non-utf8-text" => "hunk text is not UTF-8",
        _ => "path refused",
    }
}

fn short_hash(body: &[u8]) -> String {
    crate::sha256::hex(body)[..8].to_string()
}

/// The hunk id ends with the 8 hex digits of `short_hash` over its `text`.
fn hunk_text_matches_id(hunk: &Value) -> bool {
    let Some(id) = hunk["id"].as_str() else {
        return false;
    };
    let Some((_, hash)) = id.rsplit_once(':') else {
        return false;
    };
    if hash.len() != 8 || !hash.bytes().all(|b| b.is_ascii_hexdigit()) {
        return false;
    }
    let text = hunk["text"].as_str().unwrap_or("");
    hash == short_hash(text.as_bytes())
}

fn hunk_id(
    path: &str,
    old_start: u32,
    old_count: u32,
    new_start: u32,
    new_count: u32,
    body: &[u8],
) -> String {
    format!(
        "{path}:{old_start}:{old_count}:{new_start}:{new_count}:{}",
        short_hash(body)
    )
}

fn canonical_json(value: &Value) -> String {
    let mut out = String::new();
    canon(value, &mut out);
    out
}

fn canon(value: &Value, out: &mut String) {
    match value {
        Value::Null => out.push_str("null"),
        Value::Bool(b) => out.push_str(if *b { "true" } else { "false" }),
        Value::Number(n) => out.push_str(&n.to_string()),
        Value::String(s) => {
            out.push('"');
            for c in s.chars() {
                match c {
                    '"' => out.push_str("\\\""),
                    '\\' => out.push_str("\\\\"),
                    '\n' => out.push_str("\\n"),
                    '\r' => out.push_str("\\r"),
                    '\t' => out.push_str("\\t"),
                    '\u{8}' => out.push_str("\\b"),
                    '\u{c}' => out.push_str("\\f"),
                    c if c.is_control() => out.push_str(&format!("\\u{:04x}", u32::from(c))),
                    c => out.push(c),
                }
            }
            out.push('"');
        }
        Value::Array(items) => {
            out.push('[');
            for (i, item) in items.iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                canon(item, out);
            }
            out.push(']');
        }
        Value::Object(map) => {
            let mut keys: Vec<&String> = map.keys().collect();
            keys.sort();
            out.push('{');
            for (i, key) in keys.into_iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                out.push('"');
                for c in key.chars() {
                    match c {
                        '"' => out.push_str("\\\""),
                        '\\' => out.push_str("\\\\"),
                        c => out.push(c),
                    }
                }
                out.push_str("\":");
                canon(&map[key], out);
            }
            out.push('}');
        }
    }
}

pub fn plan_sum_of(doc: &Value) -> String {
    let mut copy = doc.clone();
    if let Some(map) = copy.as_object_mut() {
        map.remove("plan_sum");
    }
    crate::sha256::hex(canonical_json(&copy).as_bytes())
}

fn emit(doc: &Value) {
    println!(
        "{}",
        serde_json::to_string_pretty(doc).unwrap_or_else(|_| "{}".into())
    );
}

fn effects() -> Value {
    json!({"writes": [], "network": false, "hooks": false, "prompts": false})
}

fn head_state(root: &Path) -> Result<(String, String), Fail> {
    let head = text(&git(root, WorkspaceOp::RevParseHead)?)
        .trim()
        .to_string();
    let symbolic = git_missing(root, WorkspaceOp::SymbolicRefHead)?;
    let Some(symbolic) = symbolic else {
        return Err(Fail::new("detached-head", "HEAD is detached", 1));
    };
    let reference = text(&symbolic).trim().to_string();
    if reference.is_empty() {
        return Err(Fail::new("detached-head", "HEAD is detached", 1));
    }
    Ok((head, reference))
}

fn index_entry_for(root: &Path, path: &str) -> Result<Option<String>, Fail> {
    let out = git(
        root,
        WorkspaceOp::LsFilesStage {
            path: path_arg(path)?,
        },
    )?;
    let rec = out.split(|b| *b == 0).next().unwrap_or_default();
    if rec.is_empty() {
        Ok(None)
    } else {
        Ok(Some(text(rec)))
    }
}

fn blob_hash_bytes(root: &Path, data: &[u8]) -> Result<String, Fail> {
    Ok(text(&git_in(root, WorkspaceOp::HashObject, Some(data))?)
        .trim()
        .to_string())
}

fn worktree_metadata(full: &Path) -> Result<Option<std::fs::Metadata>, Fail> {
    match fs::symlink_metadata(full) {
        Ok(meta) => Ok(Some(meta)),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(Fail::new("worktree", error.to_string(), 1)),
    }
}

fn worktree_blob(root: &Path, path: &str) -> Result<Option<String>, Fail> {
    let full = root.join(path);
    let Some(meta) = worktree_metadata(&full)? else {
        return Ok(None);
    };
    // `is_file` and `fs::read` follow a link and would hash a file outside the repository.
    if meta.file_type().is_symlink() {
        return Err(Fail::new("symlink", format!("refusing symlink {path}"), 1));
    }
    if !meta.is_file() {
        return Ok(None);
    }
    let data = fs::read(&full).map_err(|e| Fail::new("worktree", e.to_string(), 1))?;
    Ok(Some(blob_hash_bytes(root, &data)?))
}

fn head_blob(root: &Path, path: &str) -> Result<Option<String>, Fail> {
    let Some(out) = git_missing(
        root,
        WorkspaceOp::RevParseHeadPath {
            path: path_arg(path)?,
        },
    )?
    else {
        return Ok(None);
    };
    Ok(Some(text(&out).trim().to_string()))
}

struct RawHunk {
    path: String,
    old_path: String,
    new_path: String,
    old_start: u32,
    old_count: u32,
    new_start: u32,
    new_count: u32,
    body: Vec<u8>,
    file_header: Vec<u8>,
    binary: bool,
    /// The `diff --git` line could not be parsed. The path is empty so a later hunk is not
    /// attributed to the previous file.
    unparsed: bool,
    /// Named refusal (`non-utf8-path`, `tab-in-path`). `path` is the distinct display form.
    fault: Option<&'static str>,
}

fn parse_diff_hunks(diff: &[u8]) -> Vec<RawHunk> {
    parse_diff_hunks_keep(diff)
}

fn parse_diff_hunks_keep(diff: &[u8]) -> Vec<RawHunk> {
    let mut lines = Vec::new();
    let mut rest = diff;
    while !rest.is_empty() {
        if let Some(pos) = rest.iter().position(|b| *b == b'\n') {
            lines.push(&rest[..=pos]);
            rest = &rest[pos + 1..];
        } else {
            lines.push(rest);
            break;
        }
    }
    let mut hunks = Vec::new();
    let mut i = 0;
    let mut path = String::new();
    let mut old_path = String::new();
    let mut new_path = String::new();
    let mut file_header: Vec<u8> = Vec::new();
    while i < lines.len() {
        let line = lines[i];
        let trimmed = trim_nl(line);
        if trimmed.starts_with(b"diff --git ") {
            // Drop the previous file before parsing. A header git quoted, or one we cannot
            // read, must not keep that file's path for the next hunk.
            path.clear();
            old_path.clear();
            new_path.clear();
            file_header.clear();
            file_header.extend_from_slice(line);
            i += 1;
            let mut preamble: Vec<&[u8]> = Vec::new();
            let mut binary = false;
            while i < lines.len()
                && !lines[i].starts_with(b"@@")
                && !trim_nl(lines[i]).starts_with(b"diff --git ")
            {
                if lines[i].starts_with(b"Binary files ")
                    || lines[i].starts_with(b"GIT binary patch")
                {
                    binary = true;
                }
                preamble.push(lines[i]);
                i += 1;
            }
            let parsed = decide_paths(trimmed, &preamble);
            let (old, new) = match parsed {
                PathChoice::Paths(old, new) => (old, new),
                PathChoice::Bad(reason, shown) => {
                    hunks.push(RawHunk {
                        path: shown,
                        old_path: String::new(),
                        new_path: String::new(),
                        old_start: 0,
                        old_count: 0,
                        new_start: 0,
                        new_count: 0,
                        body: Vec::new(),
                        file_header: std::mem::take(&mut file_header),
                        binary: false,
                        unparsed: false,
                        fault: Some(reason),
                    });
                    path.clear();
                    old_path.clear();
                    new_path.clear();
                    file_header = Vec::new();
                    continue;
                }
                PathChoice::Missing => {
                    // The running path stays empty, so a following @@ is not attributed to the
                    // previous file. The refusal still names this file when the preamble has one.
                    hunks.push(RawHunk {
                        path: path_hint_from_preamble(&preamble),
                        old_path: String::new(),
                        new_path: String::new(),
                        old_start: 0,
                        old_count: 0,
                        new_start: 0,
                        new_count: 0,
                        body: Vec::new(),
                        file_header: std::mem::take(&mut file_header),
                        binary: false,
                        unparsed: true,
                        fault: None,
                    });
                    continue;
                }
            };
            old_path = old;
            new_path = new;
            path = if new_path == "/dev/null" {
                old_path.clone()
            } else {
                new_path.clone()
            };
            for pre in &preamble {
                file_header.extend_from_slice(pre);
            }
            if binary {
                hunks.push(RawHunk {
                    path,
                    old_path,
                    new_path,
                    old_start: 0,
                    old_count: 0,
                    new_start: 0,
                    new_count: 0,
                    body: Vec::new(),
                    file_header,
                    binary: true,
                    unparsed: false,
                    fault: None,
                });
                path = String::new();
                old_path = String::new();
                new_path = String::new();
                file_header = Vec::new();
            }
            continue;
        }
        if let Some((old_start, old_count, new_start, new_count)) = parse_hunk_header(trimmed) {
            let mut body = vec![line.to_vec()];
            i += 1;
            while i < lines.len() {
                let next = lines[i];
                if next.starts_with(b"@@") || trim_nl(next).starts_with(b"diff --git ") {
                    break;
                }
                if next.starts_with(b"+")
                    || next.starts_with(b"-")
                    || next.starts_with(b" ")
                    || next.starts_with(b"\\")
                {
                    body.push(next.to_vec());
                    i += 1;
                    continue;
                }
                break;
            }
            if path.is_empty() {
                continue;
            }
            hunks.push(RawHunk {
                path: path.clone(),
                old_path: old_path.clone(),
                new_path: new_path.clone(),
                old_start,
                old_count,
                new_start,
                new_count,
                body: body.concat(),
                file_header: file_header.clone(),
                binary: false,
                unparsed: false,
                fault: None,
            });
            continue;
        }
        i += 1;
    }
    hunks
}

fn trim_nl(line: &[u8]) -> &[u8] {
    let line = line.strip_suffix(b"\n").unwrap_or(line);
    line.strip_suffix(b"\r").unwrap_or(line)
}

enum PathChoice {
    Paths(String, String),
    Bad(&'static str, String),
    Missing,
}

/// Rename lines win over the equal-side split. `diff --git a/z b/z b/z b/z` has an
/// equal split at `z b/z`, which is not the rename `z` -> `z b/z b/z`.
fn decide_paths(header: &[u8], preamble: &[&[u8]]) -> PathChoice {
    if let Some(choice) = rename_choice(preamble) {
        return choice;
    }
    match parse_diff_header_paths(header) {
        PathChoice::Missing => paths_from_preamble(preamble)
            .map(|(old, new)| PathChoice::Paths(old, new))
            .unwrap_or(PathChoice::Missing),
        other => other,
    }
}

fn rename_choice(lines: &[&[u8]]) -> Option<PathChoice> {
    let mut from = None;
    let mut to = None;
    for line in lines {
        let trimmed = trim_nl(line);
        if let Some(rest) = trimmed.strip_prefix(b"rename from ") {
            from = Some(classify_path(unquote_token(rest)));
        } else if let Some(rest) = trimmed.strip_prefix(b"rename to ") {
            to = Some(classify_path(unquote_token(rest)));
        }
    }
    match (from, to) {
        (None, None) => None,
        (Some(Err((reason, shown))), _) | (_, Some(Err((reason, shown)))) => {
            Some(PathChoice::Bad(reason, shown))
        }
        (Some(Ok(old)), Some(Ok(new))) if !old.is_empty() && !new.is_empty() => {
            Some(PathChoice::Paths(old, new))
        }
        _ => None,
    }
}

/// The two paths of a `diff --git` line. Git quotes a non-ASCII path (`core.quotePath`),
/// so the line is `diff --git "a/caf\303\251" "b/caf\303\251"` rather than `diff --git a/...`.
/// A space is not quoted: `diff --git a/x y.txt b/x y.txt`. Without a rename both sides are
/// the same path, so the split is the one where `a/P` and `b/P` are equal.
fn parse_diff_header_paths(line: &[u8]) -> PathChoice {
    let Some(rest) = trim_nl(line).strip_prefix(b"diff --git ") else {
        return PathChoice::Missing;
    };
    if rest.contains(&b'"') {
        return parse_quoted_header_paths(rest);
    }
    split_where_sides_match(rest)
}

fn parse_quoted_header_paths(rest: &[u8]) -> PathChoice {
    let Some((old_token, rest)) = split_diff_token(rest) else {
        return PathChoice::Missing;
    };
    let Some(rest) = rest.strip_prefix(b" ") else {
        return PathChoice::Missing;
    };
    let Some((new_token, rest)) = split_diff_token(rest) else {
        return PathChoice::Missing;
    };
    if !rest.is_empty() {
        return PathChoice::Missing;
    }
    let old_path = match one_side(old_token, "a/") {
        Ok(Some(path)) => path,
        Ok(None) => return PathChoice::Missing,
        Err((reason, shown)) => return PathChoice::Bad(reason, shown),
    };
    let new_path = match one_side(new_token, "b/") {
        Ok(Some(path)) => path,
        Ok(None) => return PathChoice::Missing,
        Err((reason, shown)) => return PathChoice::Bad(reason, shown),
    };
    if old_path.is_empty() || new_path.is_empty() {
        return PathChoice::Missing;
    }
    PathChoice::Paths(old_path, new_path)
}

fn split_where_sides_match(rest: &[u8]) -> PathChoice {
    let marker = b" b/";
    let mut from = 0;
    while from + marker.len() <= rest.len() {
        let Some(rel) = rest[from..]
            .windows(marker.len())
            .position(|window| window == marker)
        else {
            break;
        };
        let at = from + rel;
        let left = &rest[..at];
        let right = &rest[at + 1..];
        let old = match one_side(left, "a/") {
            Ok(path) => path,
            Err((reason, shown)) => return PathChoice::Bad(reason, shown),
        };
        let new = match one_side(right, "b/") {
            Ok(path) => path,
            Err((reason, shown)) => return PathChoice::Bad(reason, shown),
        };
        if let (Some(old), Some(new)) = (old, new) {
            if !old.is_empty() && old == new {
                return PathChoice::Paths(old, new);
            }
        }
        from = at + 1;
    }
    PathChoice::Missing
}

/// A rename's `diff --git` line has two different paths, so the equal-side split does not
/// apply. `rename from` / `rename to` name them; `---` / `+++` do when those lines are absent.
fn paths_from_preamble(lines: &[&[u8]]) -> Option<(String, String)> {
    let (from, to, minus, plus) = preamble_names(lines);
    if let (Some(old), Some(new)) = (from, to) {
        if !old.is_empty() && !new.is_empty() {
            return Some((old, new));
        }
    }
    match (minus, plus) {
        (Some(old), Some(new)) if !old.is_empty() && !new.is_empty() => Some((old, new)),
        _ => None,
    }
}

fn path_hint_from_preamble(lines: &[&[u8]]) -> String {
    let (from, to, minus, plus) = preamble_names(lines);
    for candidate in [to, plus, from, minus].into_iter().flatten() {
        if !candidate.is_empty() && candidate != "/dev/null" {
            return candidate;
        }
    }
    String::new()
}

fn preamble_names(
    lines: &[&[u8]],
) -> (
    Option<String>,
    Option<String>,
    Option<String>,
    Option<String>,
) {
    let mut from = None;
    let mut to = None;
    let mut minus = None;
    let mut plus = None;
    for line in lines {
        let trimmed = trim_nl(line);
        if let Some(rest) = trimmed.strip_prefix(b"rename from ") {
            from = Some(unquote_path(&text(rest)));
        } else if let Some(rest) = trimmed.strip_prefix(b"rename to ") {
            to = Some(unquote_path(&text(rest)));
        } else if let Some(rest) = trimmed.strip_prefix(b"--- ") {
            minus = decode_ab_path(rest);
        } else if let Some(rest) = trimmed.strip_prefix(b"+++ ") {
            plus = decode_ab_path(rest);
        }
    }
    (from, to, minus, plus)
}

fn decode_ab_path(rest: &[u8]) -> Option<String> {
    let token = if rest.first() == Some(&b'"') {
        split_diff_token(rest)
            .map(|(token, _)| token)
            .unwrap_or(rest)
    } else {
        match rest.iter().position(|byte| *byte == b'\t') {
            Some(at) => &rest[..at],
            None => rest,
        }
    };
    let decoded = classify_path(unquote_token(token)).ok()?;
    if decoded == "/dev/null" {
        return Some(decoded);
    }
    decoded
        .strip_prefix("a/")
        .or_else(|| decoded.strip_prefix("b/"))
        .filter(|path| !path.is_empty())
        .map(str::to_string)
}

fn split_diff_token(input: &[u8]) -> Option<(&[u8], &[u8])> {
    if input.first() == Some(&b'"') {
        let mut i = 1;
        while i < input.len() {
            if input[i] == b'\\' {
                i += 2;
                continue;
            }
            if input[i] == b'"' {
                return Some((&input[..=i], &input[i + 1..]));
            }
            i += 1;
        }
        return None;
    }
    let end = input.iter().position(|b| *b == b' ').unwrap_or(input.len());
    if end == 0 {
        return None;
    }
    Some((&input[..end], &input[end..]))
}

fn one_side(token: &[u8], prefix: &str) -> Result<Option<String>, (&'static str, String)> {
    let decoded = unquote_token(token);
    if decoded == b"/dev/null" {
        return Ok(Some("/dev/null".to_string()));
    }
    let Some(rest) = decoded.strip_prefix(prefix.as_bytes()) else {
        return Ok(None);
    };
    if rest.is_empty() {
        return Ok(None);
    }
    classify_path(rest.to_vec()).map(Some)
}

fn parse_hunk_header(line: &[u8]) -> Option<(u32, u32, u32, u32)> {
    let line = std::str::from_utf8(line).ok()?;
    let rest = line.strip_prefix("@@ -")?;
    let (old, rest) = rest.split_once(" +")?;
    let (new, _) = rest.split_once(" @@")?;
    let (old_start, old_count) = split_count(old)?;
    let (new_start, new_count) = split_count(new)?;
    Some((old_start, old_count, new_start, new_count))
}

fn split_count(part: &str) -> Option<(u32, u32)> {
    if let Some((start, count)) = part.split_once(',') {
        Some((start.parse().ok()?, count.parse().ok()?))
    } else {
        Some((part.parse().ok()?, 1))
    }
}

fn summary_path(raw: &str) -> Result<String, (&'static str, String)> {
    let token = raw.trim();
    let bytes = if token.as_bytes().first() == Some(&b'"') {
        split_diff_token(token.as_bytes())
            .map(|(token, _)| token)
            .unwrap_or(token.as_bytes())
    } else {
        token.as_bytes()
    };
    classify_path(unquote_token(bytes))
}

fn summary_meta(root: &Path) -> Result<(Vec<Value>, Vec<Value>), Fail> {
    let out = text(&git(root, WorkspaceOp::DiffSummary)?);
    let mut found = Vec::new();
    let mut refused = Vec::new();
    for line in out.lines() {
        if let Some(rest) = line.trim_start().strip_prefix("mode change ") {
            let mut parts = rest.splitn(3, ' ');
            let old_mode = parts.next().unwrap_or("");
            let arrow = parts.next().unwrap_or("");
            let tail = parts.next().unwrap_or("");
            if arrow != "=>" {
                continue;
            }
            let Some((new_mode, path_raw)) = tail.split_once(' ') else {
                continue;
            };
            let path = match summary_path(path_raw) {
                Ok(path) => path,
                Err((reason, shown)) => {
                    refused.push(json!({
                        "path": shown,
                        "reason": reason,
                        "message": path_message(reason),
                    }));
                    continue;
                }
            };
            let body = format!("mode change {old_mode} => {new_mode} {path}\n");
            found.push(json!({
                "id": hunk_id(&path, 0, 0, 0, 0, body.as_bytes()),
                "path": path,
                "old_start": 0,
                "old_count": 0,
                "new_start": 0,
                "new_count": 0,
                "text": body,
                "kind": "mode",
                "old_mode": old_mode,
                "new_mode": new_mode,
            }));
            continue;
        }
        let trimmed = line.trim_start();
        if let Some(rest) = trimmed.strip_prefix("rename ") {
            if let Some((paths, pct)) = rest.rsplit_once(" (") {
                let Some(pct) = pct.strip_suffix("%)") else {
                    continue;
                };
                let (old_path, new_path) = if let Some((prefix, brace)) = paths.split_once(" {") {
                    let Some(brace) = brace.strip_suffix('}') else {
                        continue;
                    };
                    let Some((old_name, new_name)) = brace.split_once(" => ") else {
                        continue;
                    };
                    (
                        format!("{prefix}{old_name}").replace("//", "/"),
                        format!("{prefix}{new_name}").replace("//", "/"),
                    )
                } else if let Some((old_path, new_path)) = split_arrow(paths) {
                    (old_path.to_string(), new_path.to_string())
                } else {
                    continue;
                };
                let (old_path, new_path) = match (summary_path(&old_path), summary_path(&new_path))
                {
                    (Ok(old_path), Ok(new_path)) => (old_path, new_path),
                    (Err((reason, shown)), _) | (_, Err((reason, shown))) => {
                        refused.push(json!({
                            "path": shown,
                            "reason": reason,
                            "message": path_message(reason),
                        }));
                        continue;
                    }
                };
                let body = format!("rename {old_path} => {new_path} ({pct}%)\n");
                found.push(json!({
                    "id": hunk_id(&new_path, 0, 0, 0, 0, body.as_bytes()),
                    "path": new_path,
                    "old_path": old_path,
                    "old_start": 0,
                    "old_count": 0,
                    "new_start": 0,
                    "new_count": 0,
                    "text": body,
                    "kind": "renamed",
                    "similarity": pct.parse::<u32>().unwrap_or(100),
                }));
            }
        }
    }
    Ok((found, refused))
}

/// Split `old => new` outside quotes, so a quoted path may contain spaces.
fn split_arrow(paths: &str) -> Option<(&str, &str)> {
    let bytes = paths.as_bytes();
    let mut i = 0;
    let mut quoted = false;
    while i + 4 <= bytes.len() {
        if bytes[i] == b'"' && (i == 0 || bytes[i - 1] != b'\\') {
            quoted = !quoted;
            i += 1;
            continue;
        }
        if !quoted && bytes[i..i + 4] == *b" => " {
            return Some((&paths[..i], &paths[i + 4..]));
        }
        i += 1;
    }
    None
}

fn untracked_hunk(root: &Path, path: &str) -> Result<Option<Value>, Fail> {
    let full = root.join(path);
    let Some(meta) = worktree_metadata(&full)? else {
        return Ok(None);
    };
    if meta.file_type().is_symlink() {
        return Ok(Some(json!({
            "refused": true,
            "path": path,
            "reason": "symlink",
            "message": "untracked symlink refused",
        })));
    }
    if !meta.is_file() {
        return Ok(None);
    }
    let data = fs::read(&full).map_err(|e| Fail::new("worktree", e.to_string(), 1))?;
    if std::str::from_utf8(&data).is_err() {
        return Ok(Some(json!({
            "refused": true,
            "path": path,
            "reason": "non-utf8-text",
            "message": path_message("non-utf8-text"),
        })));
    }
    if data.iter().take(8000).any(|b| *b == 0) {
        return Ok(Some(json!({
            "refused": true,
            "path": path,
            "reason": "binary",
            "message": "untracked binary file refused",
        })));
    }
    let lines = split_keep(&data);
    let (new_count, body) = if data.is_empty() {
        (0u32, b"@@ -0,0 +0,0 @@\n".to_vec())
    } else {
        let new_count = lines.len() as u32;
        let mut body = format!("@@ -0,0 +1,{new_count} @@\n").into_bytes();
        for line in &lines {
            body.push(b'+');
            body.extend_from_slice(line);
        }
        if !data.ends_with(b"\n") && !data.ends_with(b"\r") {
            // The marker is its own patch line. The added line keeps a newline
            // here so `git apply` can strip it; without that newline the marker
            // is committed as file content.
            if body.last() != Some(&b'\n') {
                body.push(b'\n');
            }
            body.extend_from_slice(b"\\ No newline at end of file\n");
        }
        (new_count, body)
    };
    let header = format!(
        "diff --git a/{path} b/{path}\nnew file mode 100644\n--- /dev/null\n+++ b/{path}\n"
    );
    let new_start = if new_count == 0 { 0 } else { 1 };
    Ok(Some(json!({
        "id": hunk_id(path, 0, 0, new_start, new_count, &body),
        "path": path,
        "old_start": 0,
        "old_count": 0,
        "new_start": new_start,
        "new_count": new_count,
        "text": text(&body),
        "kind": "untracked",
        "file_header": header,
        "note": "untracked counts as one whole-file hunk",
    })))
}

fn split_keep(data: &[u8]) -> Vec<Vec<u8>> {
    if data.is_empty() {
        return Vec::new();
    }
    let mut lines = Vec::new();
    let mut rest = data;
    while !rest.is_empty() {
        if let Some(pos) = rest.iter().position(|b| *b == b'\n') {
            lines.push(rest[..=pos].to_vec());
            rest = &rest[pos + 1..];
        } else {
            lines.push(rest.to_vec());
            break;
        }
    }
    lines
}

pub fn list_hunks(root: &Path, only: &[String]) -> Result<Value, Fail> {
    let (head, reference) = head_state(root)?;
    let mut refused = Vec::new();
    let mut hunks = Vec::new();
    let diff = git(root, WorkspaceOp::DiffWorktreeU0)?;
    for raw in parse_diff_hunks(&diff) {
        if let Some(reason) = raw.fault {
            refused.push(json!({
                "path": raw.path,
                "reason": reason,
                "message": path_message(reason),
            }));
            continue;
        }
        if raw.unparsed || raw.path.is_empty() {
            refused.push(json!({
                "path": raw.path,
                "reason": "unparsed-diff",
                "message": "diff header could not be parsed",
            }));
            continue;
        }
        if std::str::from_utf8(&raw.body).is_err() || std::str::from_utf8(&raw.file_header).is_err()
        {
            refused.push(json!({
                "path": raw.path,
                "reason": "non-utf8-text",
                "message": path_message("non-utf8-text"),
            }));
            continue;
        }
        if raw.binary {
            refused.push(
                json!({"path": raw.path, "reason": "binary", "message": "binary change refused"}),
            );
            continue;
        }
        let id = hunk_id(
            &raw.path,
            raw.old_start,
            raw.old_count,
            raw.new_start,
            raw.new_count,
            &raw.body,
        );
        let header = &raw.file_header;
        let kind = if header
            .windows(b"new file mode".len())
            .any(|w| w == b"new file mode")
            || raw.old_path == "/dev/null"
        {
            "added"
        } else if header
            .windows(b"deleted file mode".len())
            .any(|w| w == b"deleted file mode")
            || raw.new_path == "/dev/null"
        {
            "deleted"
        } else if !raw.old_path.is_empty()
            && !raw.new_path.is_empty()
            && raw.old_path != raw.new_path
            && raw.old_path != "/dev/null"
            && raw.new_path != "/dev/null"
        {
            "renamed"
        } else {
            "modified"
        };
        let mut entry = json!({
            "id": id,
            "path": raw.path,
            "old_start": raw.old_start,
            "old_count": raw.old_count,
            "new_start": raw.new_start,
            "new_count": raw.new_count,
            "text": text(&raw.body),
            "kind": kind,
        });
        if !header.is_empty() {
            entry["file_header"] = json!(text(header));
        }
        if kind == "renamed" {
            entry["old_path"] = json!(raw.old_path);
        }
        hunks.push(entry);
    }
    let (metas, summary_refused) = summary_meta(root)?;
    refused.extend(summary_refused);
    for meta in metas {
        let skip = meta["kind"] == "renamed"
            && hunks
                .iter()
                .any(|h| h["kind"] == "renamed" && h["path"] == meta["path"]);
        if !skip {
            hunks.push(meta);
        }
    }
    let others = git(root, WorkspaceOp::LsFilesOthers)?;
    for raw_path in others.split(|b| *b == 0) {
        if raw_path.is_empty() {
            continue;
        }
        if let Err((reason, shown)) = classify_path(raw_path.to_vec()) {
            refused.push(json!({
                "path": shown,
                "reason": reason,
                "message": path_message(reason),
            }));
            continue;
        }
        let path = std::str::from_utf8(raw_path).unwrap_or("").to_string();
        let Some(item) = untracked_hunk(root, &path)? else {
            continue;
        };
        if item.get("refused").and_then(Value::as_bool) == Some(true) {
            refused.push(json!({
                "path": path,
                "reason": item["reason"],
                "message": item["message"],
            }));
            continue;
        }
        hunks.push(item);
    }
    if !only.is_empty() {
        hunks.retain(|h| only.iter().any(|p| h["path"] == p.as_str()));
        refused.retain(|h| only.iter().any(|p| h["path"] == p.as_str()));
    }
    Ok(json!({
        "schema_version": SCHEMA,
        "command": format!("{COMMAND} list"),
        "head": head,
        "ref": reference,
        "context": 0,
        "hunks": hunks,
        "refused": refused,
        "effects": effects(),
    }))
}

fn index_blob(entry: Option<&str>) -> Option<String> {
    let entry = entry?;
    entry.split_whitespace().nth(1).map(str::to_string)
}

fn classify_index(root: &Path, path: &str) -> Result<&'static str, Fail> {
    let entry = index_entry_for(root, path)?;
    let ib = index_blob(entry.as_deref());
    let hb = head_blob(root, path)?;
    let wb = worktree_blob(root, path)?;
    Ok(match (ib, hb, wb) {
        (None, _, _) => "absent",
        (Some(ib), Some(hb), _) if ib == hb => "clean",
        (Some(ib), _, Some(wb)) if ib == wb => "staged-full",
        _ => "staged-unrelated",
    })
}

pub fn make_plan(root: &Path, ids: &[String]) -> Result<Value, Fail> {
    if ids.is_empty() {
        return Err(Fail::new("usage", "plan requires at least one --hunk", 2));
    }
    let listed = list_hunks(root, &[])?;
    let mut by_id = BTreeMap::new();
    for hunk in listed["hunks"].as_array().cloned().unwrap_or_default() {
        if let Some(id) = hunk["id"].as_str() {
            by_id.insert(id.to_string(), hunk);
        }
    }
    let mut selected = Vec::new();
    for id in ids {
        let Some(mut hunk) = by_id.get(id).cloned() else {
            return Err(Fail::new(
                "unknown-hunk",
                format!("hunk id not in the current list: {id}"),
                1,
            ));
        };
        let path = hunk["path"].as_str().unwrap_or("").to_string();
        let state = classify_index(root, &path)?;
        if state == "staged-unrelated" {
            return Err(Fail::new(
                "unrelated-staged",
                format!("selected path {path} has unrelated staged changes"),
                1,
            ));
        }
        hunk["worktree_blob"] = json!(worktree_blob(root, &path)?);
        hunk["index_entry"] = json!(index_entry_for(root, &path)?);
        hunk["head_blob"] = json!(head_blob(root, &path)?);
        hunk["index_state"] = json!(state);
        selected.push(hunk);
    }
    let mut doc = json!({
        "schema_version": SCHEMA,
        "command": format!("{COMMAND} plan"),
        "head": listed["head"],
        "ref": listed["ref"],
        "selected": selected,
        "effects": effects(),
    });
    doc["plan_sum"] = json!(plan_sum_of(&doc));
    Ok(doc)
}

fn rewrite_header(
    body: &str,
    old_start: u32,
    old_count: u32,
    new_start: u32,
    new_count: u32,
) -> String {
    let mut lines: Vec<&str> = body.split_inclusive('\n').collect();
    if lines.is_empty() {
        return body.to_string();
    }
    let Some(rest) = lines[0].strip_prefix("@@ ") else {
        return body.to_string();
    };
    let Some((_, trail)) = rest.split_once(" @@") else {
        return body.to_string();
    };
    lines[0] = "";
    let owned = format!("@@ -{old_start},{old_count} +{new_start},{new_count} @@{trail}");
    let mut out = owned;
    for line in lines.into_iter().skip(1) {
        out.push_str(line);
    }
    out
}

pub fn renumber_selected_for_apply(selected: &[Value]) -> Vec<Value> {
    let mut order = Vec::new();
    let mut by_path: BTreeMap<String, Vec<Value>> = BTreeMap::new();
    for hunk in selected {
        let path = hunk["path"].as_str().unwrap_or("").to_string();
        if !by_path.contains_key(&path) {
            order.push(path.clone());
        }
        by_path.entry(path).or_default().push(hunk.clone());
    }
    let mut out = Vec::new();
    for path in order {
        let mut group = by_path.remove(&path).unwrap_or_default();
        group.sort_by(|a, b| {
            let key = |h: &Value| {
                (
                    h["old_start"].as_u64().unwrap_or(0),
                    h["new_start"].as_u64().unwrap_or(0),
                    h["id"].as_str().unwrap_or("").to_string(),
                )
            };
            key(a).cmp(&key(b))
        });
        let mut offset: i64 = 0;
        for hunk in group {
            let mut adj = hunk.clone();
            let old_start = hunk["old_start"].as_u64().unwrap_or(0) as i64;
            let old_count = hunk["old_count"].as_u64().unwrap_or(0) as i64;
            let new_count = hunk["new_count"].as_u64().unwrap_or(0) as i64;
            let kind = hunk["kind"].as_str().unwrap_or("modified");
            if matches!(kind, "mode" | "renamed") && old_count == 0 && new_count == 0 {
                out.push(adj);
                continue;
            }
            let new_start = if old_count == 0 {
                if old_start == 0 && new_count > 0 {
                    1 + offset
                } else if old_start > 0 || offset > 0 || new_count != 0 {
                    old_start + 1 + offset
                } else {
                    0
                }
            } else {
                old_start + offset
            };
            adj["new_start"] = json!(new_start);
            if let Some(text) = adj["text"].as_str() {
                if text.trim_start().starts_with("@@") {
                    adj["text"] = json!(rewrite_header(
                        text,
                        old_start as u32,
                        old_count as u32,
                        new_start as u32,
                        new_count as u32,
                    ));
                }
            }
            offset += new_count - old_count;
            out.push(adj);
        }
    }
    out
}

fn build_patch(hunk: &Value) -> Vec<u8> {
    let path = hunk["path"].as_str().unwrap_or("");
    let kind = hunk["kind"].as_str().unwrap_or("modified");
    let body = hunk["text"].as_str().unwrap_or("").as_bytes();
    if kind == "mode" {
        let old_mode = hunk["old_mode"].as_str().unwrap_or("");
        let new_mode = hunk["new_mode"].as_str().unwrap_or("");
        return format!("diff --git a/{path} b/{path}\nold mode {old_mode}\nnew mode {new_mode}\n")
            .into_bytes();
    }
    if kind == "untracked" || kind == "added" {
        if let Some(header) = hunk["file_header"].as_str() {
            let mut out = header.as_bytes().to_vec();
            out.extend_from_slice(body);
            return out;
        }
        let mut out = format!(
            "diff --git a/{path} b/{path}\nnew file mode 100644\n--- /dev/null\n+++ b/{path}\n"
        )
        .into_bytes();
        out.extend_from_slice(body);
        return out;
    }
    if kind == "deleted" {
        let mut out = format!(
            "diff --git a/{path} b/{path}\ndeleted file mode 100644\n--- a/{path}\n+++ /dev/null\n"
        )
        .into_bytes();
        out.extend_from_slice(body);
        return out;
    }
    if kind == "renamed" {
        let old_path = hunk["old_path"].as_str().unwrap_or(path);
        let pct = hunk["similarity"].as_u64().unwrap_or(100);
        let mut header = format!(
            "diff --git a/{old_path} b/{path}\nsimilarity index {pct}%\nrename from {old_path}\nrename to {path}\n"
        )
        .into_bytes();
        if hunk["old_count"].as_u64() == Some(0)
            && hunk["new_count"].as_u64() == Some(0)
            && !hunk["text"].as_str().unwrap_or("").starts_with("@@")
        {
            return header;
        }
        header.extend(format!("--- a/{old_path}\n+++ b/{path}\n").into_bytes());
        header.extend_from_slice(body);
        return header;
    }
    let mut out =
        format!("diff --git a/{path} b/{path}\n--- a/{path}\n+++ b/{path}\n").into_bytes();
    out.extend_from_slice(body);
    out
}

fn tree_entry(root: &Path, commit: &str, path: &str) -> Result<Option<(String, String)>, Fail> {
    let treeish =
        Hex40::parse(commit).ok_or_else(|| Fail::new("bad-plan", "commit id is not 40 hex", 1))?;
    let out = git(
        root,
        WorkspaceOp::LsTreePath {
            treeish,
            path: path_arg(path)?,
        },
    )?;
    let rec = out.split(|b| *b == 0).next().unwrap_or_default();
    if rec.is_empty() {
        return Ok(None);
    }
    let meta = rec.split(|b| *b == b'\t').next().unwrap_or_default();
    let text_meta = text(meta);
    let mut words = text_meta.split_whitespace();
    let mode = words.next().unwrap_or("").to_string();
    let _kind = words.next();
    let oid = words.next().unwrap_or("").to_string();
    if mode.is_empty() || oid.is_empty() {
        return Ok(None);
    }
    Ok(Some((mode, oid)))
}

fn plan_index_actions(root: &Path, plan: &Value, new_commit: &str) -> Result<Vec<Value>, Fail> {
    let mut by_path = BTreeMap::new();
    for hunk in plan["selected"].as_array().cloned().unwrap_or_default() {
        let path = hunk["path"].as_str().unwrap_or("").to_string();
        by_path.insert(path, hunk);
    }
    let mut actions = Vec::new();
    for (path, hunk) in by_path {
        let state = hunk["index_state"].as_str().unwrap_or("");
        let entry = tree_entry(root, new_commit, &path)?;
        match state {
            "clean" => {
                if let Some((mode, oid)) = entry {
                    actions.push(json!({"path": path, "action": "set-to-new-head", "reason": "was-clean", "mode": mode, "oid": oid}));
                } else {
                    actions.push(json!({"path": path, "action": "remove", "reason": "was-clean"}));
                }
            }
            "absent" => {
                if let Some((mode, oid)) = entry {
                    actions.push(json!({"path": path, "action": "add", "reason": "was-absent", "mode": mode, "oid": oid}));
                } else {
                    actions.push(
                        json!({"path": path, "action": "none", "reason": "absent-and-deleted"}),
                    );
                }
            }
            "staged-full" => {
                actions.push(json!({"path": path, "action": "leave", "reason": "staged-full"}))
            }
            _ => {
                return Err(Fail::new(
                    "unrelated-staged",
                    format!("selected path {path} has unrelated staged changes"),
                    1,
                ));
            }
        }
    }
    Ok(actions)
}

fn git_dir(root: &Path) -> Result<PathBuf, Fail> {
    let raw = text(&git(root, WorkspaceOp::GitDir)?).trim().to_string();
    let path = PathBuf::from(&raw);
    if path.is_absolute() {
        Ok(path)
    } else {
        Ok(root.join(path))
    }
}

fn recheck(root: &Path, plan: &Value) -> Result<(), Fail> {
    let (head, reference) = head_state(root)?;
    if head != plan["head"].as_str().unwrap_or("")
        || reference != plan["ref"].as_str().unwrap_or("")
    {
        return Err(Fail::new(
            "stale-branch",
            "branch tip moved since the plan",
            1,
        ));
    }
    for hunk in plan["selected"].as_array().cloned().unwrap_or_default() {
        let path = hunk["path"].as_str().unwrap_or("");
        let wb = worktree_blob(root, path)?;
        if wb.as_deref() != hunk["worktree_blob"].as_str()
            && !(wb.is_none() && hunk["worktree_blob"].is_null())
        {
            return Err(Fail::new(
                "stale-worktree",
                format!("work tree of {path} changed since the plan"),
                1,
            ));
        }
        let ie = index_entry_for(root, path)?;
        let planned = hunk["index_entry"].as_str().map(str::to_string);
        if ie != planned {
            return Err(Fail::new(
                "stale-index",
                format!("index entry of {path} changed since the plan"),
                1,
            ));
        }
        if classify_index(root, path)? == "staged-unrelated" {
            return Err(Fail::new(
                "unrelated-staged",
                format!("selected path {path} has unrelated staged changes"),
                1,
            ));
        }
    }
    Ok(())
}

fn build_prepared_index(
    root: &Path,
    real_index: &Path,
    actions: &[Value],
    scratch: &Path,
) -> Result<Vec<u8>, Fail> {
    let prepared = scratch.join("prepared-index");
    fs::copy(real_index, &prepared).map_err(|e| Fail::new("index", e.to_string(), 1))?;
    for action in actions {
        let path = action["path"].as_str().unwrap_or("");
        match action["action"].as_str().unwrap_or("") {
            "remove" => {
                git(
                    root,
                    WorkspaceOp::UpdateIndexRemove {
                        index: prepared.clone(),
                        path: path_arg(path)?,
                    },
                )?;
            }
            "set-to-new-head" | "add" => {
                let mode = action["mode"].as_str().unwrap_or("").to_string();
                let oid = Hex40::parse(action["oid"].as_str().unwrap_or(""))
                    .ok_or_else(|| Fail::new("bad-index-action", "oid is not 40 hex", 1))?;
                let op = if action["action"] == "add" {
                    WorkspaceOp::UpdateIndexAdd {
                        index: prepared.clone(),
                        mode,
                        oid,
                        path: path_arg(path)?,
                    }
                } else {
                    WorkspaceOp::UpdateIndexCacheinfo {
                        index: prepared.clone(),
                        mode,
                        oid,
                        path: path_arg(path)?,
                    }
                };
                git(root, op)?;
            }
            "leave" | "none" => {}
            other => {
                return Err(Fail::new(
                    "bad-index-action",
                    format!("unknown index action {other}"),
                    1,
                ))
            }
        }
    }
    fs::read(&prepared).map_err(|e| Fail::new("index", e.to_string(), 1))
}

fn publish(
    root: &Path,
    plan: &Value,
    reference: &str,
    old_head: &str,
    commit: &str,
    actions: &[Value],
    scratch: &Path,
) -> Result<(), Fail> {
    let git_dir = git_dir(root)?;
    let index_path = git_dir.join("index");
    if !index_path.is_file() {
        return Err(Fail::new(
            "missing-index",
            "repository has no index file",
            1,
        ));
    }
    let lock_path = git_dir.join("index.lock");
    let mut lock = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&lock_path)
        .map_err(|_| Fail::new("index-locked", "the real index is locked", 1))?;
    let result = (|| {
        recheck(root, plan)?;
        let tip = text(&git(root, WorkspaceOp::RevParseHead)?)
            .trim()
            .to_string();
        if tip != old_head {
            return Err(Fail::new(
                "stale-branch",
                "branch tip moved before publication",
                1,
            ));
        }
        let prepared = build_prepared_index(root, &index_path, actions, scratch)?;
        lock.write_all(&prepared)
            .map_err(|e| Fail::new("index", e.to_string(), 1))?;
        lock.sync_all()
            .map_err(|e| Fail::new("index", e.to_string(), 1))?;
        let branch = BranchRef::parse(reference)
            .ok_or_else(|| Fail::new("bad-ref", "ref is not a branch", 1))?;
        let new = Hex40::parse(commit)
            .ok_or_else(|| Fail::new("bad-plan", "commit id is not 40 hex", 1))?;
        let expected = Hex40::parse(old_head)
            .ok_or_else(|| Fail::new("bad-plan", "old head is not 40 hex", 1))?;
        git(
            root,
            WorkspaceOp::MoveBranch {
                reference: branch.clone(),
                new,
                expected,
            },
        )?;
        if let Err(error) = fs::rename(&lock_path, &index_path) {
            let rollback = git(
                root,
                WorkspaceOp::MoveBranch {
                    reference: branch,
                    new: expected,
                    expected: new,
                },
            );
            let detail = if rollback.is_ok() {
                format!("branch restored to {old_head}")
            } else {
                format!(
                    "branch rollback failed; previous head {old_head}, attempted commit {commit}"
                )
            };
            return Err(Fail::new(
                "index-publish-failed",
                format!("could not publish the index ({error}); {detail}"),
                1,
            ));
        }
        Ok(())
    })();
    if lock_path.exists() {
        let _ = fs::remove_file(&lock_path);
    }
    result
}

pub fn apply_plan(
    root: &Path,
    plan: &Value,
    message: &str,
    expected_sum: Option<&str>,
) -> Result<Value, Fail> {
    if plan["schema_version"].as_u64() != Some(SCHEMA as u64) {
        return Err(Fail::new("bad-plan", "unsupported plan schema_version", 1));
    }
    if plan["command"].as_str() != Some(&format!("{COMMAND} plan")) {
        return Err(Fail::new(
            "bad-plan",
            "plan command field is not hunk-select plan",
            1,
        ));
    }
    if message.trim().is_empty() {
        return Err(Fail::new("empty-message", "commit message is empty", 2));
    }
    let Some(got_sum) = plan["plan_sum"].as_str() else {
        return Err(Fail::new("plan-sum-required", "plan has no plan_sum", 1));
    };
    if expected_sum.is_some_and(|sum| sum != got_sum) || plan_sum_of(plan) != got_sum {
        return Err(Fail::new("plan-sum-mismatch", "plan_sum does not match", 1));
    }
    for hunk in plan["selected"].as_array().cloned().unwrap_or_default() {
        if !hunk_text_matches_id(&hunk) {
            return Err(Fail::new(
                "hunk-id-mismatch",
                "hunk text does not match the id hash",
                1,
            ));
        }
    }
    recheck(root, plan)?;
    let head = plan["head"].as_str().unwrap_or("").to_string();
    let reference = plan["ref"].as_str().unwrap_or("").to_string();
    let tmp = tempfile::tempdir().map_err(|e| Fail::new("temp", e.to_string(), 1))?;
    if tmp
        .path()
        .to_string_lossy()
        .to_ascii_lowercase()
        .contains("hint")
    {
        return Err(Fail::new(
            "path-contains-hint",
            "temp path must not contain 'hint'",
            1,
        ));
    }
    let index_file = tmp.path().join("index");
    git(
        root,
        WorkspaceOp::ReadTreeHead {
            index: index_file.clone(),
        },
    )?;
    let selected = plan["selected"].as_array().cloned().unwrap_or_default();
    for hunk in renumber_selected_for_apply(&selected) {
        let patch = build_patch(&hunk);
        let patch_path = tmp
            .path()
            .join(format!("hunk-{}.patch", short_hash(&patch)));
        fs::write(&patch_path, &patch).map_err(|e| Fail::new("patch", e.to_string(), 1))?;
        git(
            root,
            WorkspaceOp::ApplyCached {
                index: index_file.clone(),
                patch: patch_path,
            },
        )?;
    }
    let tree = text(&git(root, WorkspaceOp::WriteTree(index_file))?)
        .trim()
        .to_string();
    let head_tree = text(&git(root, WorkspaceOp::RevParseHeadTree)?)
        .trim()
        .to_string();
    if tree == head_tree {
        return Err(Fail::new(
            "empty-commit",
            "selected hunks would make an empty commit",
            1,
        ));
    }
    let tree_id = Hex40::parse(&tree)
        .ok_or_else(|| Fail::new("git", "write-tree did not return a sha", 1))?;
    let parent =
        Hex40::parse(&head).ok_or_else(|| Fail::new("bad-plan", "head is not 40 hex", 1))?;
    let mut message_bytes = message.as_bytes().to_vec();
    if !message_bytes.ends_with(b"\n") {
        message_bytes.push(b'\n');
    }
    let commit = text(&git_in(
        root,
        WorkspaceOp::CommitTree {
            tree: tree_id,
            parents: vec![parent],
        },
        Some(&message_bytes),
    )?)
    .trim()
    .to_string();
    let actions = plan_index_actions(root, plan, &commit)?;
    publish(root, plan, &reference, &head, &commit, &actions, tmp.path())?;
    Ok(json!({
        "schema_version": SCHEMA,
        "command": format!("{COMMAND} apply"),
        "old_head": head,
        "commit": commit,
        "ref": reference,
        "tree": tree,
        "selected": selected.iter().map(|h| h["id"].clone()).collect::<Vec<_>>(),
        "index_actions": actions,
        "plan_sum": got_sum,
        "outcome": "ok",
    }))
}

fn refused(command: &str, fail: &Fail) -> Value {
    json!({
        "schema_version": SCHEMA,
        "command": format!("{COMMAND} {command}"),
        "error": {"code": fail.code, "message": fail.message},
        "outcome": "refused",
    })
}

pub fn print_list(root: &Path, paths: &[String]) -> u8 {
    match list_hunks(root, paths) {
        Ok(doc) => {
            emit(&doc);
            exit::OK
        }
        Err(fail) => {
            emit(&refused("list", &fail));
            eprintln!("stw workspace commit-plan: {}: {}", fail.code, fail.message);
            fail.status
        }
    }
}

pub fn print_plan(root: &Path, ids: &[String]) -> u8 {
    match make_plan(root, ids) {
        Ok(doc) => {
            emit(&doc);
            exit::OK
        }
        Err(fail) => {
            emit(&refused("plan", &fail));
            eprintln!("stw workspace commit-plan: {}: {}", fail.code, fail.message);
            fail.status
        }
    }
}

pub fn print_apply(root: &Path, plan: &Value, message: &str, expected: Option<&str>) -> u8 {
    match apply_plan(root, plan, message, expected) {
        Ok(doc) => {
            emit(&doc);
            exit::OK
        }
        Err(fail) => {
            emit(&refused("apply", &fail));
            eprintln!(
                "stw workspace commit-apply: {}: {}",
                fail.code, fail.message
            );
            fail.status
        }
    }
}

pub fn is_hunk_plan(doc: &Value) -> bool {
    doc["command"].as_str() == Some("hunk-select plan")
        || doc.get("plan").and_then(|p| p["command"].as_str()) == Some("hunk-select plan")
}

pub fn plan_body(doc: &Value) -> Value {
    doc.get("plan")
        .filter(|p| p["command"].as_str() == Some("hunk-select plan"))
        .cloned()
        .unwrap_or_else(|| doc.clone())
}

#[cfg(test)]
mod tests {
    use super::parse_diff_hunks;

    #[test]
    fn a_quoted_header_names_its_file_and_a_broken_header_drops_the_previous_path() {
        let diff = concat!(
            "diff --git a/plain.txt b/plain.txt\n",
            "--- a/plain.txt\n",
            "+++ b/plain.txt\n",
            "@@ -1 +1 @@\n",
            "-old\n",
            "+new\n",
            "diff --git \"a/caf\\303\\251.txt\" \"b/caf\\303\\251.txt\"\n",
            "--- \"a/caf\\303\\251.txt\"\n",
            "+++ \"b/caf\\303\\251.txt\"\n",
            "@@ -1 +1 @@\n",
            "-alt\n",
            "+neu\n",
            "diff --git broken\n",
            "@@ -1 +1 @@\n",
            "-x\n",
            "+y\n",
        );
        let hunks = parse_diff_hunks(diff.as_bytes());
        let paths: Vec<(&str, bool)> = hunks
            .iter()
            .map(|hunk| (hunk.path.as_str(), hunk.unparsed))
            .collect();
        assert_eq!(
            paths,
            vec![("plain.txt", false), ("café.txt", false), ("", true)]
        );
        assert!(
            hunks
                .iter()
                .all(|hunk| hunk.path != "plain.txt" || !hunk.unparsed),
            "the broken header must not be committed into plain.txt"
        );
    }

    #[test]
    fn a_space_in_the_path_splits_where_both_sides_match() {
        let diff = concat!(
            "diff --git a/x y.txt b/x y.txt\n",
            "@@ -1 +1 @@\n",
            "-old\n",
            "+new\n",
            "diff --git a/tabbed y.txt b/tabbed y.txt\n",
            "--- a/tabbed y.txt\t\n",
            "+++ b/tabbed y.txt\t\n",
            "@@ -1 +1 @@\n",
            "-old\n",
            "+new\n",
            "diff --git a/x b/y.txt b/x b/y.txt\n",
            "--- a/x b/y.txt\t\n",
            "+++ b/x b/y.txt\t\n",
            "@@ -1 +1 @@\n",
            "-old\n",
            "+new\n",
            "diff --git a/old name.txt b/new name.txt\n",
            "similarity index 80%\n",
            "rename from old name.txt\n",
            "rename to new name.txt\n",
            "--- a/old name.txt\t\n",
            "+++ b/new name.txt\t\n",
            "@@ -1 +1 @@\n",
            "-old\n",
            "+new\n",
            "diff --git nonsense\n",
            "--- a/kept name.txt\t\n",
            "@@ -1 +1 @@\n",
            "-x\n",
            "+y\n",
        );
        let hunks = parse_diff_hunks(diff.as_bytes());
        let found: Vec<(&str, &str, bool)> = hunks
            .iter()
            .map(|hunk| (hunk.path.as_str(), hunk.old_path.as_str(), hunk.unparsed))
            .collect();
        assert_eq!(
            found,
            vec![
                ("x y.txt", "x y.txt", false),
                ("tabbed y.txt", "tabbed y.txt", false),
                ("x b/y.txt", "x b/y.txt", false),
                ("new name.txt", "old name.txt", false),
                ("kept name.txt", "", true),
            ]
        );
    }

    #[test]
    fn rename_lines_win_over_an_equal_split_and_a_mode_only_header_still_splits() {
        let diff = concat!(
            "diff --git a/z b/z b/z b/z\n",
            "similarity index 100%\n",
            "rename from z\n",
            "rename to z b/z b/z\n",
            "@@ -1 +1 @@\n",
            "-a\n",
            "+b\n",
            "diff --git a/old name.txt b/new name.txt\n",
            "similarity index 100%\n",
            "rename from old name.txt\n",
            "rename to new name.txt\n",
            "@@ -1 +1 @@\n",
            "-a\n",
            "+b\n",
            "diff --git a/x b/y.txt b/x b/y.txt\n",
            "old mode 100644\n",
            "new mode 100755\n",
            "@@ -1 +1 @@\n",
            "-a\n",
            "+b\n",
        );
        let hunks = parse_diff_hunks(diff.as_bytes());
        let found: Vec<(&str, &str)> = hunks
            .iter()
            .map(|hunk| (hunk.path.as_str(), hunk.old_path.as_str()))
            .collect();
        assert_eq!(
            found,
            vec![
                ("z b/z b/z", "z"),
                ("new name.txt", "old name.txt"),
                ("x b/y.txt", "x b/y.txt"),
            ]
        );
    }
}
