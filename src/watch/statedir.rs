//! The state directory of a watcher: its id and default place, SHA-256 for the id, the freshness window of `--status`.
use std::path::PathBuf;

/// Prefix of the text the workspace id hashes. It differs from the Go watcher's `stw-watch-v1`, so a Go state directory
/// and a Rust one never share a path (D-20261119-05: new format, new baseline).
pub const ID_PREFIX: &str = "stw-watch-rs1\n";

/// SHA-256 (FIPS 180-4), own code because no SHA-256 is in the dependency graph (D-20261119-04); NIST vectors in the tests.
pub fn sha256(_data: &[u8]) -> [u8; 32] {
    [0; 32]
}

/// Lower-case hex of `sha256`.
pub fn sha256_hex(data: &[u8]) -> String {
    sha256(data).iter().map(|b| format!("{b:02x}")).collect()
}

/// What the workspace id is made of: the canonical workspace root, or the list of repositories (`OWNER/NAME`).
pub enum IdSource<'a> {
    Workspace(&'a str),
    Repos(&'a [String]),
}

/// The first 16 hex digits of SHA-256 over `ID_PREFIX`, the host and either `workspace:ROOT` or `repos:` followed by the
/// lower-cased, de-duplicated, sorted repositories joined with `\n`.
pub fn workspace_id(_host: &str, _source: IdSource<'_>) -> String {
    String::new()
}

/// `$XDG_STATE_HOME/stw/watch/<id>`, else `$HOME/.local/state/stw/watch/<id>`. An empty or missing value counts as unset.
pub fn default_state_dir(_id: &str, _xdg_state_home: Option<&str>, _home: Option<&str>) -> PathBuf {
    PathBuf::new()
}

/// The age below which a heartbeat counts as alive: `max(3 x interval, 180)` seconds.
pub fn status_freshness_seconds(_interval_seconds: u64) -> u64 {
    0
}
