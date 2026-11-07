//! Token discovery for GitHub. Order: `GH_TOKEN`, `GITHUB_TOKEN`, `~/.config/gh/hosts.yml` (`oauth_token`),
//! macOS keychain (the item gh creates: service `gh:github.com`). A token is never printed, logged or put in errors.
use std::path::PathBuf;

/// Where a token came from (label only; the token itself is never exposed through this).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TokenSource {
    Env,
    HostsYml,
    Keychain,
}

impl TokenSource {
    pub fn label(self) -> &'static str {
        match self {
            TokenSource::Env => "environment",
            TokenSource::HostsYml => "gh hosts.yml",
            TokenSource::Keychain => "macOS keychain",
        }
    }
}

/// Inputs of token discovery, injectable for tests.
pub struct AuthEnv {
    pub env: Vec<(String, String)>,
    pub hosts_yml: Option<PathBuf>,
    pub keychain_service: String,
}

impl AuthEnv {
    pub fn from_process() -> Self {
        let get = |k: &str| std::env::var(k).ok().filter(|v| !v.is_empty());
        let mut env = Vec::new();
        for k in ["GH_TOKEN", "GITHUB_TOKEN"] {
            if let Some(v) = get(k) {
                env.push((k.to_string(), v));
            }
        }
        let cfg_dir = get("GH_CONFIG_DIR")
            .map(PathBuf::from)
            .or_else(|| get("XDG_CONFIG_HOME").map(|x| PathBuf::from(x).join("gh")))
            .or_else(|| get("HOME").map(|h| PathBuf::from(h).join(".config").join("gh")));
        AuthEnv {
            env,
            hosts_yml: cfg_dir.map(|d| d.join("hosts.yml")),
            keychain_service: get("STW_KEYCHAIN_SERVICE").unwrap_or_else(|| "gh:github.com".into()),
        }
    }
}

/// Find a token. Returns the token and its source label.
pub fn find_token(a: &AuthEnv) -> Option<(String, TokenSource)> {
    for k in ["GH_TOKEN", "GITHUB_TOKEN"] {
        if let Some((_, v)) = a.env.iter().find(|(n, _)| n == k) {
            return Some((v.clone(), TokenSource::Env));
        }
    }
    if let Some(p) = &a.hosts_yml {
        if let Ok(text) = std::fs::read_to_string(p) {
            if let Some(t) = token_from_hosts_yml(&text) {
                return Some((t, TokenSource::HostsYml));
            }
        }
    }
    keychain_token(&a.keychain_service).map(|t| (t, TokenSource::Keychain))
}

/// Minimal reader for gh's hosts.yml: the `oauth_token` of the `github.com` block, preferring the
/// host-level key (4 spaces) over the one nested below `users:`.
pub fn token_from_hosts_yml(text: &str) -> Option<String> {
    let mut in_host = false;
    let mut nested: Option<String> = None;
    for line in text.lines() {
        if !line.starts_with(' ') && !line.trim().is_empty() {
            in_host = line.trim_end() == "github.com:";
            continue;
        }
        if !in_host {
            continue;
        }
        let indent = line.len() - line.trim_start().len();
        if let Some(v) = line.trim().strip_prefix("oauth_token:") {
            let v = v.trim().trim_matches(|c| c == '"' || c == '\'');
            if v.is_empty() {
                continue;
            }
            if indent <= 4 {
                return Some(v.to_string());
            }
            nested.get_or_insert_with(|| v.to_string());
        }
    }
    nested
}

/// gh stores the token via go-keyring: plain, `go-keyring-base64:<b64>` or `go-keyring-encoded:<hex>`.
pub fn decode_go_keyring(raw: &str) -> Option<String> {
    use base64::Engine;
    if let Some(b) = raw.strip_prefix("go-keyring-base64:") {
        let bytes = base64::engine::general_purpose::STANDARD
            .decode(b.trim())
            .ok()?;
        return String::from_utf8(bytes).ok();
    }
    if let Some(h) = raw.strip_prefix("go-keyring-encoded:") {
        let h = h.trim();
        if h.len() % 2 != 0 {
            return None;
        }
        let bytes: Option<Vec<u8>> = (0..h.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&h[i..i + 2], 16).ok())
            .collect();
        return String::from_utf8(bytes?).ok();
    }
    Some(raw.to_string())
}

#[cfg(target_os = "macos")]
fn keychain_token(service: &str) -> Option<String> {
    use security_framework::item::{ItemClass, ItemSearchOptions, Limit, SearchResult};
    let results = ItemSearchOptions::new()
        .class(ItemClass::generic_password())
        .service(service)
        .load_data(true)
        .limit(Limit::All)
        .search()
        .ok()?;
    for r in results {
        if let SearchResult::Data(d) = r {
            if let Some(t) = String::from_utf8(d)
                .ok()
                .and_then(|s| decode_go_keyring(s.trim()))
            {
                if !t.is_empty() {
                    return Some(t);
                }
            }
        }
    }
    None
}

#[cfg(not(target_os = "macos"))]
fn keychain_token(_service: &str) -> Option<String> {
    None
}
