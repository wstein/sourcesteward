//! Parser of `refs.log` and `rewrites.log` and the encoding of ref names (`docs/oplog-format.md`, sections 2, 3.2, 4, 9).

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

pub fn parse_refs(data: &[u8]) -> Parsed<RefLine> {
    let mut out = Parsed::default();
    for line in lines(data) {
        match line.and_then(ref_line) {
            Some(l) => out.lines.push(l),
            None => out.bad += 1,
        }
    }
    out
}

pub fn parse_rewrites(data: &[u8]) -> Parsed<RewriteLine> {
    let mut out = Parsed::default();
    for line in lines(data) {
        match line.and_then(rewrite_line) {
            Some(l) => out.lines.push(l),
            None => out.bad += 1,
        }
    }
    out
}

/// The lines of a log: `None` for one that is skipped before any field is looked at (over 4 KB, NUL, not UTF-8).
/// The piece after the last LF is a line only when it is not empty (a hook killed before its LF).
fn lines(data: &[u8]) -> impl Iterator<Item = Option<Vec<&str>>> {
    let mut parts: Vec<&[u8]> = data.split(|b| *b == b'\n').collect();
    if parts.last().is_some_and(|l| l.is_empty()) {
        parts.pop();
    }
    parts.into_iter().map(|l| {
        if l.len() > MAX_LINE || l.contains(&0) {
            return None;
        }
        std::str::from_utf8(l).ok().map(|s| s.split('\t').collect())
    })
}

fn all_bytes(s: &str, ok: impl Fn(u8) -> bool) -> bool {
    s.bytes().all(ok)
}

fn is_hash(s: &str) -> bool {
    matches!(s.len(), 40 | 64) && all_bytes(s, |b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
}

/// Characters of an encoded name (spec 3.2): the plain set and `%`.
fn name_byte(b: u8) -> bool {
    b.is_ascii_alphanumeric() || b"._/+@:-%".contains(&b)
}

/// Every `%` is followed by two hex digits.
fn percent_ok(s: &str) -> bool {
    let b = s.as_bytes();
    let mut i = 0;
    while i < b.len() {
        if b[i] == b'%' {
            if i + 3 > b.len() {
                return false;
            }
            if !(b[i + 1].is_ascii_hexdigit() && b[i + 2].is_ascii_hexdigit()) {
                return false;
            }
            i += 3;
        } else {
            i += 1;
        }
    }
    true
}

fn is_name(s: &str) -> bool {
    (s == "HEAD" || s.starts_with("refs/")) && all_bytes(s, name_byte) && percent_ok(s)
}

/// A hash, or `ref:` and an encoded name.
fn is_value(s: &str) -> bool {
    match s.strip_prefix("ref:") {
        Some(t) => !t.is_empty() && all_bytes(t, name_byte) && percent_ok(t),
        None => is_hash(s),
    }
}

fn is_time(s: &str) -> bool {
    s == "unknown" || epoch_of(s).is_some()
}

fn is_word(s: &str) -> bool {
    !s.is_empty() && all_bytes(s, |b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

/// Fields from the 7th on must be `key=value` extensions (spec section 9): a glued line has a time, hash or name there.
fn extensions_ok(fields: &[&str]) -> bool {
    fields.iter().skip(6).all(|f| {
        let Some((k, v)) = f.split_once('=') else {
            return false;
        };
        let mut kb = k.bytes();
        kb.next().is_some_and(|b| b.is_ascii_lowercase())
            && kb.all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'_')
            && all_bytes(v, |b| {
                b.is_ascii_alphanumeric() || b"._/+@:=%-".contains(&b)
            })
    })
}

fn ref_line(f: Vec<&str>) -> Option<RefLine> {
    if f.len() < 5 || !extensions_ok(&f) {
        return None;
    }
    let worktree = f.get(5).copied().unwrap_or("");
    let ok = is_time(f[0])
        && is_value(f[1])
        && is_value(f[2])
        && is_name(f[3])
        && is_word(f[4])
        && all_bytes(worktree, |b| {
            b.is_ascii_alphanumeric() || b"._?-".contains(&b)
        });
    ok.then(|| RefLine {
        time: f[0].into(),
        old: f[1].into(),
        new: f[2].into(),
        refname: f[3].into(),
        context: f[4].into(),
        worktree: worktree.into(),
    })
}

fn rewrite_line(f: Vec<&str>) -> Option<RewriteLine> {
    if f.len() < 4 || !extensions_ok(&f) {
        return None;
    }
    let reduced = |s: &str| all_bytes(s, |b| b.is_ascii_alphanumeric() || b"._/+@?-".contains(&b));
    let extra = f.get(4).copied().unwrap_or("");
    let branch = f.get(5).copied().unwrap_or("");
    let ok = is_time(f[0])
        && is_word(f[1])
        && is_hash(f[2])
        && is_hash(f[3])
        && reduced(extra)
        && reduced(branch);
    ok.then(|| RewriteLine {
        time: f[0].into(),
        kind: f[1].into(),
        old: f[2].into(),
        new: f[3].into(),
        extra: extra.into(),
        branch: branch.into(),
    })
}

/// Percent-encoding of a ref name as the hook writes it (spec 3.2): bytes outside `[A-Za-z0-9._/+@:-]` become `%XX`
/// (upper case); a name over 200 bytes is cut at 200 bytes and ends in `%00` and the djb2 of the whole name.
pub fn encode_name(name: &[u8]) -> String {
    let cut = name.len() > 200;
    let shown = if cut { &name[..200] } else { name };
    let mut out = String::with_capacity(shown.len() + 11);
    for &b in shown {
        if b.is_ascii_alphanumeric() || b"._/+@:-".contains(&b) {
            out.push(b as char);
        } else {
            out.push_str(&format!("%{b:02X}"));
        }
    }
    if cut {
        out.push_str(&format!("%00{:08x}", djb2(name)));
    }
    out
}

/// Inverse of [`encode_name`] for names that were not cut; `None` for malformed input or a cut name (`%00`).
pub fn decode_name(s: &str) -> Option<Vec<u8>> {
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        if b[i] == b'%' {
            let hex = std::str::from_utf8(b.get(i + 1..i + 3)?).ok()?;
            let v = u8::from_str_radix(hex, 16).ok()?;
            if v == 0 {
                return None;
            }
            out.push(v);
            i += 3;
        } else {
            out.push(b[i]);
            i += 1;
        }
    }
    Some(out)
}

pub fn djb2(bytes: &[u8]) -> u32 {
    bytes.iter().fold(5381u32, |h, &b| {
        h.wrapping_mul(33).wrapping_add(u32::from(b))
    })
}

pub fn is_zero(id: &str) -> bool {
    id.len() >= 40 && all_bytes(id, |b| b == b'0')
}

/// Days since 1970-01-01 of a civil date (proleptic Gregorian; the algorithm of Howard Hinnant).
fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = y.div_euclid(400);
    let yoe = y - era * 400;
    let doy = (153 * (if m > 2 { m - 3 } else { m + 9 }) + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146_097 + doe - 719_468
}

fn civil_from_days(z: i64) -> (i64, i64, i64) {
    let z = z + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    (if m <= 2 { y + 1 } else { y }, m, d)
}

/// `2026-11-12T00:42:26Z` -> seconds since the epoch; `None` for any other shape or an impossible date.
pub fn epoch_of(time: &str) -> Option<i64> {
    let b = time.as_bytes();
    let shape = b.len() == 20
        && b[4] == b'-'
        && b[7] == b'-'
        && b[10] == b'T'
        && b[13] == b':'
        && b[16] == b':'
        && b[19] == b'Z'
        && b.iter()
            .enumerate()
            .all(|(i, c)| matches!(i, 4 | 7 | 10 | 13 | 16 | 19) || c.is_ascii_digit());
    if !shape {
        return None;
    }
    let n = |r: std::ops::Range<usize>| time[r].parse::<i64>().ok();
    let (y, mo, d, h, mi, s) = (
        n(0..4)?,
        n(5..7)?,
        n(8..10)?,
        n(11..13)?,
        n(14..16)?,
        n(17..19)?,
    );
    let leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;
    let dim = match mo {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 if leap => 29,
        2 => 28,
        _ => return None,
    };
    if d < 1 || d > dim || h > 23 || mi > 59 || s > 59 {
        return None;
    }
    Some(days_from_civil(y, mo, d) * 86_400 + h * 3600 + mi * 60 + s)
}

pub fn iso_of(epoch: i64) -> String {
    let (y, m, d) = civil_from_days(epoch.div_euclid(86_400));
    let s = epoch.rem_euclid(86_400);
    format!(
        "{y:04}-{m:02}-{d:02}T{:02}:{:02}:{:02}Z",
        s / 3600,
        (s % 3600) / 60,
        s % 60
    )
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
