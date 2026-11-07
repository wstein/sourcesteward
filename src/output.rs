//! Helpers shared by the commands: JSON building and printing.
use serde_json::Value;

/// `Some(x)` -> string, `None` -> JSON null.
pub fn jopt<S: AsRef<str>>(s: Option<S>) -> Value {
    match s {
        Some(s) => Value::String(s.as_ref().to_string()),
        None => Value::Null,
    }
}

/// Empty string -> null, like the prototype's `jsn`.
pub fn jne(s: &str) -> Value {
    if s.is_empty() {
        Value::Null
    } else {
        Value::String(s.to_string())
    }
}

/// Lossy path -> string (paths in the output are display strings).
pub fn p2s(p: &std::path::Path) -> String {
    p.to_string_lossy().into_owned()
}

pub fn short(id: &str) -> &str {
    &id[..id.len().min(12)]
}

/// Version of gix this build is pinned to (checked against `Cargo.toml` by a test).
pub const GIX_VERSION: &str = "0.90.0";

/// `engine` field of every JSON document.
pub fn engine() -> String {
    format!("stw {} (gitoxide {GIX_VERSION})", env!("CARGO_PKG_VERSION"))
}

/// One compact JSON document on stdout, newline-terminated.
pub fn print_json(v: &Value) {
    println!(
        "{}",
        serde_json::to_string(v).expect("JSON value serializes")
    );
}
