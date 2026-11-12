//! Parser of `refs.log` and `rewrites.log` and the encoding of ref names (`docs/oplog-format.md`, sections 2, 3.2, 4, 9).
//! Not built yet: the functions return empty results; the tests below fix the contract.

/// A line longer than this many bytes is skipped as `bad` (spec section 9).
pub const MAX_LINE: usize = 4096;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RefLine {
    pub time: String,
    pub old: String,
    pub new: String,
    /// As written in the file (percent-encoded).
    pub refname: String,
    pub context: String,
    pub worktree: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RewriteLine {
    pub time: String,
    pub kind: String,
    pub old: String,
    pub new: String,
    pub extra: String,
    pub branch: String,
}

#[derive(Debug)]
pub struct Parsed<T> {
    pub lines: Vec<T>,
    /// Lines that were skipped (spec section 9).
    pub bad: usize,
}

impl<T> Default for Parsed<T> {
    fn default() -> Self {
        Parsed {
            lines: Vec::new(),
            bad: 0,
        }
    }
}

pub fn parse_refs(_data: &[u8]) -> Parsed<RefLine> {
    Parsed::default()
}

pub fn parse_rewrites(_data: &[u8]) -> Parsed<RewriteLine> {
    Parsed::default()
}

/// Percent-encoding of a ref name as the hook writes it (spec 3.2).
pub fn encode_name(_name: &[u8]) -> String {
    String::new()
}

/// Inverse of [`encode_name`] for names that were not cut; `None` for malformed input or a cut name (`%00`).
pub fn decode_name(_s: &str) -> Option<Vec<u8>> {
    None
}

pub fn djb2(_bytes: &[u8]) -> u32 {
    0
}

pub fn is_zero(_id: &str) -> bool {
    false
}

/// `2026-11-12T00:42:26Z` -> seconds since the epoch.
pub fn epoch_of(_time: &str) -> Option<i64> {
    None
}

pub fn iso_of(_epoch: i64) -> String {
    String::new()
}

#[cfg(test)]
mod tests {
    use super::*;

    const H1: &str = "1111111111111111111111111111111111111111";
    const H2: &str = "2222222222222222222222222222222222222222";
    const Z: &str = "0000000000000000000000000000000000000000";
    const T: &str = "2026-11-12T00:42:26Z";

    fn refs_line(old: &str, new: &str, name: &str, ctx: &str, wt: &str) -> String {
        format!("{T}\t{old}\t{new}\t{name}\t{ctx}\t{wt}\n")
    }

    // The vectors of docs/oplog-format.md 3.2; the shell hook gives the same output (spikes/stw/oplog-refhook).
    #[test]
    fn name_vectors_of_the_spec() {
        let long_x = format!("refs/heads/{}", "x".repeat(210));
        let long_a = format!("refs/heads/{}", "ä".repeat(110));
        let cases: Vec<(Vec<u8>, String, u32)> = vec![
            (
                b"refs/heads/main".to_vec(),
                "refs/heads/main".into(),
                0x0866f11d,
            ),
            (
                "refs/heads/ä".as_bytes().to_vec(),
                "refs/heads/%C3%A4".into(),
                0xaee7ecff,
            ),
            (
                b"refs/tags/v1%".to_vec(),
                "refs/tags/v1%25".into(),
                0x9cff28ce,
            ),
            (
                long_x.as_bytes().to_vec(),
                format!("{}%00ffb8bca8", &long_x[..200]),
                0xffb8bca8,
            ),
            (b"".to_vec(), String::new(), 0x00001505),
        ];
        for (name, enc, h) in cases {
            assert_eq!(
                djb2(&name),
                h,
                "djb2 of {:?}",
                String::from_utf8_lossy(&name)
            );
            if !name.is_empty() {
                assert_eq!(encode_name(&name), enc);
            }
        }
        // the cut falls inside a 2-byte sequence: the first 200 bytes are encoded byte by byte
        let a = long_a.as_bytes();
        let mut expect = String::new();
        for &b in &a[..200] {
            if b.is_ascii_alphanumeric() || b"._/+@:-".contains(&b) {
                expect.push(b as char);
            } else {
                expect.push_str(&format!("%{b:02X}"));
            }
        }
        expect.push_str("%00b252bbfa");
        assert_eq!(djb2(a), 0xb252bbfa);
        assert_eq!(encode_name(a), expect);
    }

    #[test]
    fn decode_is_the_inverse_for_uncut_names() {
        for n in [
            "refs/heads/main",
            "refs/heads/ä",
            "refs/tags/v1%",
            "refs/heads/a b",
        ] {
            assert_eq!(
                decode_name(&encode_name(n.as_bytes())),
                Some(n.as_bytes().to_vec())
            );
        }
        assert_eq!(decode_name("refs/heads/%zz"), None);
        assert_eq!(decode_name("refs/heads/%4"), None);
        assert_eq!(
            decode_name("refs/heads/x%00ffb8bca8"),
            None,
            "a cut name cannot be decoded"
        );
    }

    #[test]
    fn time_round_trips_and_rejects_garbage() {
        assert_eq!(epoch_of("2026-11-12T00:42:26Z"), Some(1_794_444_146));
        assert_eq!(iso_of(1_794_444_146), "2026-11-12T00:42:26Z");
        assert_eq!(epoch_of("1970-01-01T00:00:00Z"), Some(0));
        assert_eq!(epoch_of("2026-13-12T00:42:26Z"), None);
        assert_eq!(epoch_of("2026-11-12 00:42:26"), None);
        assert_eq!(epoch_of("unknown"), None);
    }

    #[test]
    fn zero_ids() {
        assert!(is_zero(Z));
        assert!(is_zero(&"0".repeat(64)));
        assert!(!is_zero(H1));
        assert!(!is_zero(""));
        assert!(!is_zero("0"));
    }

    #[test]
    fn a_good_refs_line_is_read() {
        let p =
            parse_refs(refs_line(H1, H2, "refs/heads/main", "ref-transaction", "main").as_bytes());
        assert_eq!(p.bad, 0);
        assert_eq!(p.lines.len(), 1);
        let l = &p.lines[0];
        assert_eq!(
            (l.time.as_str(), l.old.as_str(), l.new.as_str()),
            (T, H1, H2)
        );
        assert_eq!(
            (l.refname.as_str(), l.context.as_str(), l.worktree.as_str()),
            ("refs/heads/main", "ref-transaction", "main")
        );
    }

    #[test]
    fn symbolic_values_unknown_time_and_sha256_are_accepted() {
        let h64 = "a".repeat(64);
        let z64 = "0".repeat(64);
        let mut s = String::new();
        s += &refs_line("ref:refs/heads/main", H1, "HEAD", "ref-transaction", "main");
        s += &format!("unknown\t{h64}\t{z64}\trefs/heads/x\tstw\tmain\n");
        let p = parse_refs(s.as_bytes());
        assert_eq!((p.lines.len(), p.bad), (2, 0));
    }

    #[test]
    fn bad_lines_are_counted_and_skipped() {
        let cases: Vec<(&str, String)> = vec![
            ("too few fields", format!("{T}\t{H1}\t{H2}\trefs/heads/x\n")),
            (
                "hash with upper case",
                refs_line(&H1.replace('1', "A"), H2, "refs/heads/x", "stw", "main"),
            ),
            (
                "hash of 39 digits",
                refs_line(&H1[..39], H2, "refs/heads/x", "stw", "main"),
            ),
            (
                "bad time",
                refs_line(H1, H2, "refs/heads/x", "stw", "main")
                    .replace("2026-11-12T00:42:26Z", "yesterday"),
            ),
            (
                "refname is not HEAD or refs/",
                refs_line(H1, H2, "heads/x", "stw", "main"),
            ),
            (
                "refname with a raw space",
                refs_line(H1, H2, "refs/heads/a b", "stw", "main"),
            ),
            ("empty line", "\n".to_string()),
            (
                "NUL byte",
                refs_line(H1, H2, "refs/heads/x\0", "stw", "main"),
            ),
            (
                "a time in field 7",
                format!("{T}\t{H1}\t{H2}\trefs/heads/x\tstw\tmain\t{T}\n"),
            ),
            (
                "a hash in field 7",
                format!("{T}\t{H1}\t{H2}\trefs/heads/x\tstw\tmain\t{H1}\n"),
            ),
            (
                "a name in field 7",
                format!("{T}\t{H1}\t{H2}\trefs/heads/x\tstw\tmain\trefs/heads/y\n"),
            ),
            (
                "longer than 4 KB",
                format!(
                    "{T}\t{H1}\t{H2}\trefs/heads/{}\tstw\tmain\n",
                    "x".repeat(4100)
                ),
            ),
        ];
        for (why, line) in cases {
            let p = parse_refs(line.as_bytes());
            assert_eq!((p.lines.len(), p.bad), (0, 1), "{why}");
        }
    }

    #[test]
    fn extension_fields_in_key_value_form_are_ignored() {
        let s = format!("{T}\t{H1}\t{H2}\trefs/heads/x\tstw\tmain\tcmd=sync\tn=1\n");
        let p = parse_refs(s.as_bytes());
        assert_eq!((p.lines.len(), p.bad), (1, 0));
        assert_eq!(p.lines[0].worktree, "main");
    }

    #[test]
    fn a_line_with_five_fields_is_read_without_a_worktree() {
        let s = format!("{T}\t{H1}\t{H2}\trefs/heads/x\tstw\n");
        let p = parse_refs(s.as_bytes());
        assert_eq!((p.lines.len(), p.bad), (1, 0));
        assert_eq!(p.lines[0].worktree, "");
    }

    #[test]
    fn a_long_line_does_not_stop_the_stream() {
        let mut s = format!(
            "{T}\t{H1}\t{H2}\trefs/heads/{}\tstw\tmain\n",
            "x".repeat(4100)
        );
        s += &refs_line(H1, H2, "refs/heads/ok", "stw", "main");
        let p = parse_refs(s.as_bytes());
        assert_eq!((p.lines.len(), p.bad), (1, 1));
        assert_eq!(p.lines[0].refname, "refs/heads/ok");
    }

    #[test]
    fn a_final_fragment_without_lf() {
        let good = refs_line(H1, H2, "refs/heads/a", "stw", "main");
        let full = format!("{good}{}", good.trim_end());
        let p = parse_refs(full.as_bytes());
        assert_eq!(
            (p.lines.len(), p.bad),
            (2, 0),
            "a complete last line without LF is read"
        );
        let cut = format!("{good}{T}\t{H1}\t{H2}\trefs/he");
        let p = parse_refs(cut.as_bytes());
        assert_eq!((p.lines.len(), p.bad), (1, 1), "a short fragment is bad");
    }

    #[test]
    fn two_lines_glued_by_a_missing_lf_are_bad_not_accepted() {
        // the hook was killed before its LF; the next hook appended: more than 6 fields, field 7 is a time
        let a = refs_line(H1, H2, "refs/heads/a", "stw", "main");
        let glued = format!(
            "{}{}",
            a.trim_end(),
            refs_line(H2, H1, "refs/heads/b", "stw", "main")
        );
        let p = parse_refs(glued.as_bytes());
        assert_eq!((p.lines.len(), p.bad), (0, 1));
    }

    #[test]
    fn markers_and_unknown_contexts_are_read_as_lines() {
        let mut s = refs_line(H1, Z, "refs/heads/x", "pre-old", "-");
        s += &refs_line(H1, H2, "refs/heads/x", "future-word", "main");
        let p = parse_refs(s.as_bytes());
        assert_eq!((p.lines.len(), p.bad), (2, 0));
        assert_eq!(p.lines[1].context, "future-word");
    }

    #[test]
    fn rewrites_lines() {
        let ok = format!("{T}\tamend\t{H1}\t{H2}\t\tmain\n{T}\tfuture\t{H2}\t{H1}\tx\tdetached\n");
        let p = parse_rewrites(ok.as_bytes());
        assert_eq!((p.lines.len(), p.bad), (2, 0), "an unknown kind is kept");
        assert_eq!(p.lines[1].kind, "future");
        assert_eq!(p.lines[0].extra, "");
        let bad = [
            format!("{T}\tamend\t{H1}\n"),
            format!("{T}\tamend\t{H1}\tnothex\t\tmain\n"),
            format!("{T}\tamend\t{H1}\t{}\t\tmain\n", &H2[..39]),
            "\n".to_string(),
        ];
        for b in bad {
            let p = parse_rewrites(b.as_bytes());
            assert_eq!((p.lines.len(), p.bad), (0, 1), "{b:?}");
        }
    }

    #[test]
    fn a_rewrites_line_with_four_fields_is_read() {
        let s = format!("{T}\tamend\t{H1}\t{H2}\n");
        let p = parse_rewrites(s.as_bytes());
        assert_eq!((p.lines.len(), p.bad), (1, 0));
        assert_eq!(
            (p.lines[0].extra.as_str(), p.lines[0].branch.as_str()),
            ("", "")
        );
    }
}
