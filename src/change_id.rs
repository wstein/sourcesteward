//! Change-Id trailers: Crockford Base32 form and normalisation (D-20261127-11, D-20261127-13).
//!
//! A Change-Id is navigation only. It is never evidence of identity or approval
//! (D-20261127-12). Minting and rewriting belong to later packages; this module only reads.

/// Crockford Base32 alphabet: `0-9A-Z` without `I`, `L`, `O`, or `U`.
pub const ALPHABET: &str = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

/// Canonical Change-Id length after normalisation (125 bits).
pub const ID_LENGTH: usize = 25;

/// Shortest prefix that may match a Change-Id (50 bits).
pub const PREFIX_MIN: usize = 10;

/// Normalise a query or trailer value: upper case, drop hyphens, map `I`/`L`→`1` and `O`→`0`.
pub fn normalize(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    for ch in text.chars() {
        if ch == '-' {
            continue;
        }
        let upper = ch.to_ascii_uppercase();
        let mapped = match upper {
            'I' | 'L' => '1',
            'O' => '0',
            other => other,
        };
        out.push(mapped);
    }
    out
}

/// Return the canonical 25-symbol form, or `None` when the text is not a Change-Id.
pub fn canonical(text: &str) -> Option<String> {
    let norm = normalize(text);
    if norm.len() == ID_LENGTH && norm.chars().all(|c| ALPHABET.contains(c)) {
        Some(norm)
    } else {
        None
    }
}

/// Classify a Change-Id query. Returns `(Some(norm), None)` for a valid id or prefix,
/// or `(None, Some(reason))` when the query is refused.
pub fn classify_query(query: &str) -> (Option<String>, Option<&'static str>) {
    let norm = normalize(query);
    if norm.is_empty() || norm.chars().any(|c| !ALPHABET.contains(c)) {
        return (None, Some("bad-query"));
    }
    if norm.len() < PREFIX_MIN {
        // A short pure-hex string is a bad hash query elsewhere; keep the reason distinct
        // for non-hex Change-Id material that is simply too short.
        if norm.chars().all(|c| c.is_ascii_hexdigit()) {
            return (None, Some("bad-query"));
        }
        return (None, Some("prefix-too-short"));
    }
    if norm.len() > ID_LENGTH {
        return (None, Some("bad-query"));
    }
    (Some(norm), None)
}

/// True when `query` looks like a git object name the resolver tries as a hash first
/// (7–39 hex, or a full 40/64-hex object id).
pub fn looks_like_hash_query(query: &str) -> bool {
    let b = query.as_bytes();
    let n = b.len();
    if !(n == 40 || n == 64 || (7..=39).contains(&n)) {
        return false;
    }
    b.iter().all(|c| c.is_ascii_hexdigit())
}

/// Recognised `Change-Id:` lines anywhere in the message (trailer block and body).
/// Returns unique canonical ids in encounter order.
pub fn parse_message(message: &str) -> Vec<String> {
    let mut found = Vec::new();
    for line in message.lines() {
        let trimmed = line.trim();
        let Some(rest) = trimmed.strip_prefix("Change-Id:") else {
            continue;
        };
        let value = rest.trim();
        let Some(canon) = canonical(value) else {
            continue;
        };
        if !found.contains(&canon) {
            found.push(canon);
        }
    }
    found
}

/// True when a stored id equals the wanted id, or (for a prefix query) starts with it.
pub fn matches(wanted: &str, stored: &str) -> bool {
    if wanted.len() == ID_LENGTH {
        stored == wanted
    } else {
        stored.starts_with(wanted)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalize_maps_il_o_hyphens_and_case() {
        assert_eq!(normalize("ab-il-o"), "AB110");
        assert_eq!(normalize("AbCd"), "ABCD");
    }

    #[test]
    fn canonical_rejects_wrong_length_and_alphabet() {
        assert!(canonical("0123456789ABCDEFGHJKMNPQR").is_some());
        assert!(canonical("0123456789ABCDEFGHJKMNPQ").is_none()); // 24
        assert!(canonical("0123456789ABCDEFGHJKMNPQRU").is_none()); // U
        assert_eq!(
            canonical("0123456789abcdefghjkmnpqr").as_deref(),
            Some("0123456789ABCDEFGHJKMNPQR")
        );
    }

    #[test]
    fn classify_prefix_bounds() {
        assert_eq!(
            classify_query("ABCDEFGHJ"),
            (None, Some("prefix-too-short"))
        );
        let (ok, reason) = classify_query("ABCDEFGHJK");
        assert!(reason.is_none());
        assert_eq!(ok.as_deref(), Some("ABCDEFGHJK"));
        assert_eq!(
            classify_query("0123456789ABCDEFGHJKMNPQRS"),
            (None, Some("bad-query"))
        ); // 26
    }

    #[test]
    fn parse_finds_body_and_trailer_lines() {
        // Second line: hyphens and I/L/O; after normalisation it is 25 symbols.
        let msg = "subject\n\nChange-Id: 0123456789abcdefghjkmnpqr\n\
                   body line\nChange-Id: AB-IL-O-1234567890ABCDEFGHJK\n";
        let ids = parse_message(msg);
        assert_eq!(ids.len(), 2);
        assert_eq!(ids[0], "0123456789ABCDEFGHJKMNPQR");
        assert_eq!(ids[1], "AB1101234567890ABCDEFGHJK");
    }

    #[test]
    fn looks_like_hash_covers_abbrev_and_full() {
        assert!(looks_like_hash_query("abcdef0"));
        assert!(looks_like_hash_query(&"a".repeat(40)));
        assert!(!looks_like_hash_query("abcdefg")); // g
        assert!(!looks_like_hash_query("abc")); // too short
    }
}
