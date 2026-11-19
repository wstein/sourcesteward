//! The watch core (port package W1): `src/watch/`. Pure functions and files of the state directory; the replay of the
//! scenario corpus (`tests/fixtures/watch/`) is at the end of this file.
use stw::watch::statedir::{
    default_state_dir, sha256, sha256_hex, status_freshness_seconds, workspace_id, IdSource,
};

#[test]
fn sha256_matches_the_nist_vectors() {
    // FIPS 180-4 examples and the NIST CAVS short and long message boundaries (block size 64, padding at 55/56/64).
    assert_eq!(
        sha256_hex(b""),
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );
    assert_eq!(
        sha256_hex(b"abc"),
        "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    );
    assert_eq!(
        sha256_hex(b"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"),
        "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1"
    );
    assert_eq!(
        sha256_hex(
            b"abcdefghbcdefghicdefghijdefghijkefghijklfghijklmghijklmnhijklmnoijklmnopjklmnopqklmnopqrlmnopqrsmnopqrstnopqrstu"
        ),
        "cf5b16a778af8380036ce59e7b0492370b249b11e8f07a51afac45037afee9d1"
    );
    assert_eq!(
        sha256_hex(&vec![b'a'; 1_000_000]),
        "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0"
    );
    assert_eq!(sha256(b"abc")[0], 0xba);
}

#[test]
fn sha256_padding_boundaries_agree_with_sha256sum() {
    // Lengths around the padding boundaries; the digests were made with GNU `sha256sum` over `head -c N /dev/zero | tr '\0' a`.
    for (n, want) in [
        (
            55,
            "9f4390f8d30c2dd92ec9f095b65e2b9ae9b0a925a5258e241c9f1e910f734318",
        ),
        (
            56,
            "b35439a4ac6f0948b6d6f9e3c6af0f5f590ce20f1bde7090ef7970686ec6738a",
        ),
        (
            63,
            "7d3e74a05d7db15bce4ad9ec0658ea98e3f06eeecf16b4c6fff2da457ddc2f34",
        ),
        (
            64,
            "ffe054fe7ae0cb6dc65c3af9b61d5209f439851db43d0ba5997337df154668eb",
        ),
        (
            65,
            "635361c48bb9eab14198e76ea8ab7f1a41685d6ad62aa9146d301d4f17eb0ae0",
        ),
    ] {
        assert_eq!(sha256_hex(&vec![b'a'; n]), want, "length {n}");
    }
}

#[test]
fn the_workspace_id_is_sixteen_hex_digits_of_the_prefixed_hash() {
    // printf 'stw-watch-rs1\ngithub.com\nrepos:a/b' | sha256sum
    let a_b = ["a/b".to_string()];
    assert_eq!(
        workspace_id("github.com", IdSource::Repos(&a_b)),
        "0465033cc48d07a5"
    );
    // ... 'repos:a/b\nc/d'
    let two = ["c/d".to_string(), "a/b".to_string()];
    assert_eq!(
        workspace_id("github.com", IdSource::Repos(&two)),
        "431f3da077950d4c"
    );
    // ... 'workspace:/w/root'
    assert_eq!(
        workspace_id("github.com", IdSource::Workspace("/w/root")),
        "8633f7f15d37c015"
    );
    // ... host ghe.example
    assert_eq!(
        workspace_id("ghe.example", IdSource::Repos(&a_b)),
        "c7ecea5836adf0b7"
    );
    // no repositories: the text ends after `repos:`
    assert_eq!(
        workspace_id("github.com", IdSource::Repos(&[])),
        "d6b4026bb7cb9651"
    );
}

#[test]
fn the_repository_list_is_lower_cased_deduplicated_and_sorted_before_hashing() {
    let base = ["a/b".to_string(), "c/d".to_string()];
    let id = workspace_id("github.com", IdSource::Repos(&base));
    let shuffled = [
        "C/D".to_string(),
        "a/b".to_string(),
        "A/B".to_string(),
        "c/d".to_string(),
    ];
    assert_eq!(workspace_id("github.com", IdSource::Repos(&shuffled)), id);
    assert_eq!(id.len(), 16);
    assert!(id
        .bytes()
        .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)));
    assert_ne!(workspace_id("github.com", IdSource::Workspace("/a")), id);
    assert_ne!(
        workspace_id("github.com", IdSource::Workspace("/a")),
        workspace_id("github.com", IdSource::Workspace("/b"))
    );
}

#[test]
fn the_default_state_dir_follows_xdg_then_home() {
    assert_eq!(
        default_state_dir("abc", Some("/x/state"), Some("/h")).to_str(),
        Some("/x/state/stw/watch/abc")
    );
    assert_eq!(
        default_state_dir("abc", Some(""), Some("/h")).to_str(),
        Some("/h/.local/state/stw/watch/abc")
    );
    assert_eq!(
        default_state_dir("abc", None, Some("/h")).to_str(),
        Some("/h/.local/state/stw/watch/abc")
    );
}

#[test]
fn the_freshness_window_is_three_intervals_but_at_least_three_minutes() {
    for (iv, want) in [(10, 180), (60, 180), (61, 183), (120, 360), (600, 1800)] {
        assert_eq!(status_freshness_seconds(iv), want, "interval {iv}");
    }
    assert_eq!(status_freshness_seconds(u64::MAX), u64::MAX, "no overflow");
}

// ---------------------------------------------------------------------------------------------------------------------
// Events: kinds, ids, the strict JSON line, sanitizing, UTC time.

// ---------------------------------------------------------------------------------------------------------------------
// Events: kinds, ids, the strict JSON line, sanitizing, UTC time.
use std::path::PathBuf;

use proptest::prelude::*;
use serde_json::Value;
use stw::watch::event::{
    event_id, id_counter, key_base, key_checks, key_error, key_generation, key_head, key_mergeable,
    key_merged, key_ready, key_recovered_error, key_repos, key_restack, key_stall, key_timeout,
    Kind,
};

fn fixtures() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/watch")
}

fn scenario(name: &str) -> Value {
    let text = std::fs::read_to_string(fixtures().join(format!("{name}.json"))).unwrap();
    serde_json::from_str(&text).unwrap()
}

fn cases(name: &str) -> Vec<Value> {
    scenario(name)["cases"].as_array().unwrap().clone()
}

#[test]
fn the_fifteen_kinds_have_their_wire_names_and_the_reserved_kind_is_none() {
    assert_eq!(Kind::ALL.len(), 15);
    let names: Vec<&str> = Kind::ALL.iter().map(|k| k.as_str()).collect();
    assert_eq!(
        names,
        [
            "new-pr",
            "head-changed",
            "checks-green",
            "checks-red",
            "checks-pending-timeout",
            "mergeable-changed",
            "ready-state-changed",
            "base-changed",
            "merged",
            "closed",
            "restack-needed",
            "repos-changed",
            "watch-error",
            "watch-recovered",
            "watch-stalled"
        ]
    );
    for k in Kind::ALL {
        assert_eq!(Kind::parse(k.as_str()), Some(k));
    }
    assert_eq!(Kind::parse("review-state-changed"), None);
    assert_eq!(Kind::parse(""), None);
}

#[test]
fn the_id_of_an_event_is_repo_pr_kind_key() {
    assert_eq!(
        event_id(Some("o/r"), Some(41), Kind::NewPr, "g0"),
        "o/r#41:new-pr:g0"
    );
    assert_eq!(
        event_id(Some("o/r"), None, Kind::WatchRecovered, "e1"),
        "o/r#-:watch-recovered:e1"
    );
    assert_eq!(
        event_id(None, None, Kind::WatchError, "rate-limited:e1:f1"),
        "-:watch-error:rate-limited:e1:f1"
    );
    assert_eq!(
        event_id(Some(""), Some(3), Kind::ReposChanged, "p3"),
        "-:repos-changed:p3"
    );
}

#[test]
fn every_key_function_gives_the_key_of_the_vector() {
    let cases = cases("event-id-keys");
    assert_eq!(cases.len(), 19);
    for c in cases {
        let (f, a) = (c["input"]["fn"].as_str().unwrap(), &c["input"]["args"]);
        let n = |k: &str| a[k].as_u64().unwrap();
        let s = |k: &str| a[k].as_str().unwrap_or_default().to_string();
        let commit = a.get("merge_commit").and_then(Value::as_str);
        let got = match f {
            "event_id" => {
                let kind = Kind::parse(&s("kind")).unwrap();
                event_id(Some(&s("repo")), a["pr"].as_u64(), kind, &s("key"))
            }
            "key_head" => key_head(&s("head"), n("epoch")),
            "key_checks" => key_checks(&s("head"), n("epoch")),
            "key_timeout" => key_timeout(&s("head"), n("epoch")),
            "key_mergeable" => key_mergeable(&s("head"), n("epoch")),
            "key_ready" => key_ready(n("epoch")),
            "key_base" => key_base(n("epoch")),
            "key_generation" => key_generation(n("generation")),
            "key_merged" => key_merged(commit),
            "key_restack" => key_restack(n("merged_pr"), commit),
            "key_repos" => key_repos(n("epoch")),
            "key_error" => key_error(&s("code"), n("episode"), n("failures")),
            "key_recovered_error" => key_recovered_error(n("episode")),
            "key_stall" => key_stall(n("episode")),
            other => panic!("unknown function {other}"),
        };
        assert_eq!(got, c["expected"]["id"].as_str().unwrap(), "{}", c["name"]);
    }
}

#[test]
fn a_merge_commit_that_is_empty_counts_as_unknown() {
    assert_eq!(key_merged(Some("")), "unknown");
    assert_eq!(key_restack(7, Some("")), "7:unknown");
}

#[test]
fn id_counter_reads_the_letter_with_one_to_eight_digits() {
    for c in cases("event-id-counter") {
        let i = &c["input"];
        let letter = i["letter"].as_str().unwrap().chars().next().unwrap();
        let got = id_counter(i["id"].as_str().unwrap(), letter);
        let want = c["expected"]["found"]
            .as_bool()
            .unwrap()
            .then(|| c["expected"]["counter"].as_u64().unwrap());
        assert_eq!(got, want, "{}", c["name"]);
    }
    // the repository part is never a counter, even when it looks like one
    assert_eq!(id_counter("h7#4:new-pr:g0", 'h'), None);
    // digits only: no sign, no space
    assert_eq!(id_counter("o/r#1:head-changed:x:h+5", 'h'), None);
    assert_eq!(id_counter("o/r#1:head-changed:x:h-5", 'h'), None);
    assert_eq!(id_counter("o/r#1:head-changed:x:h", 'h'), None);
    // the last matching part wins (the search is from the end)
    assert_eq!(id_counter("o/r#1:k:h1:h2", 'h'), Some(2));
}

proptest! {
    #[test]
    fn a_hex_sha_is_never_a_counter(sha in "[0-9a-f]{40}|[0-9a-f]{64}") {
        let id = format!("o/r#4:head-changed:{sha}:h3");
        for letter in "hcmrbpgse".chars() {
            let want = (letter == 'h').then_some(3);
            prop_assert_eq!(id_counter(&id, letter), want);
        }
    }
}

use serde_json::json;
use stw::watch::event::Event;

fn null_lists_to_empty(v: &mut Value) {
    match v {
        Value::Object(m) => {
            for (k, x) in m.iter_mut() {
                if x.is_null()
                    && ["added", "removed", "skipped", "failing", "pending"].contains(&k.as_str())
                {
                    *x = json!([]);
                } else {
                    null_lists_to_empty(x);
                }
            }
        }
        Value::Array(a) => a.iter_mut().for_each(null_lists_to_empty),
        _ => {}
    }
}

#[test]
fn every_kind_encodes_to_the_keys_of_the_contract_in_the_order_of_the_contract() {
    let mut seen = std::collections::BTreeSet::new();
    for c in cases("event-json-shapes") {
        if c["input"]["op"] != "encode" {
            continue;
        }
        let mut input = c["input"]["event"].clone();
        null_lists_to_empty(&mut input);
        let ev =
            Event::from_line(&input.to_string()).unwrap_or_else(|e| panic!("{}: {e}", c["name"]));
        seen.insert(ev.kind());
        let line = ev.to_line();
        assert!(!line.contains('\n'));
        let got: Value = serde_json::from_str(&line).unwrap();
        let mut want: Value =
            serde_json::from_str(c["expected"]["line"].as_str().unwrap()).unwrap();
        null_lists_to_empty(&mut want);
        assert_eq!(got, want, "{}", c["name"]);
        // the order of the keys is the order of the contract (Go writes the same)
        let keys = |v: &Value| v.as_object().unwrap().keys().cloned().collect::<Vec<_>>();
        assert_eq!(keys(&got), keys(&want), "{}", c["name"]);
        assert_eq!(
            keys(&got["details"]),
            keys(&want["details"]),
            "{}",
            c["name"]
        );
        let mut sorted = keys(&got["details"]);
        sorted.sort();
        let expected_keys: Vec<String> = c["expected"]["detail_keys"]
            .as_array()
            .unwrap()
            .iter()
            .map(|k| k.as_str().unwrap().to_string())
            .collect();
        assert_eq!(sorted, expected_keys, "{}", c["name"]);
        assert_eq!(
            Event::from_line(&line).unwrap(),
            ev,
            "round trip of {}",
            c["name"]
        );
    }
    assert_eq!(seen.len(), 15);
}

#[test]
fn a_line_is_decoded_strictly() {
    for c in cases("event-json-shapes") {
        if c["input"]["op"] != "decode" {
            continue;
        }
        let got = Event::from_line(c["input"]["line"].as_str().unwrap()).is_ok();
        assert_eq!(
            got,
            c["expected"]["accepted"].as_bool().unwrap(),
            "{}",
            c["name"]
        );
    }
    let ok = |details: &str| {
        format!(
            r#"{{"schema_version":1,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"closed","details":{details}}}"#
        )
    };
    assert!(Event::from_line(&ok(r#"{"head":"a","base":"b"}"#)).is_ok());
    // lists are never null, numbers are non-negative integers, a key occurs once
    let green = r#"{"schema_version":1,"seq":1,"id":"x","time":"t","repo":"o/r","pr":1,"kind":"checks-red","details":{"failing":null}}"#;
    assert!(Event::from_line(green).is_err());
    assert!(Event::from_line(&ok(r#"{}"#).replace(r#""pr":null"#, r#""pr":-1"#)).is_err());
    assert!(Event::from_line(&ok(r#"{}"#).replace(r#""pr":null"#, r#""pr":1.5"#)).is_err());
    assert!(Event::from_line(&ok(r#"{"head":"a","head":"b"}"#)).is_err());
    assert!(Event::from_line(&ok(r#"{}"#).replace(r#""seq":1,"#, r#""seq":1,"seq":2,"#)).is_err());
    assert!(Event::from_line(&ok(r#"{}"#).replace(r#""kind":"closed","#, "")).is_err());
    assert!(Event::from_line("").is_err());
    assert!(Event::from_line("[]").is_err());
    assert!(
        Event::from_line(&format!("{} ", ok("{}"))).is_ok(),
        "trailing white space is not data"
    );
}

use stw::watch::event::sanitize;

#[test]
fn text_from_outside_is_masked_and_cut() {
    // control characters, backslash and direction changes as escapes
    assert_eq!(sanitize(b"a\tb\nc\x00d", 100), r"a\tb\nc\u{0}d");
    assert_eq!(sanitize("x\u{202e}y".as_bytes(), 100), r"x\u{202e}y");
    assert_eq!(sanitize(br"a\b", 100), r"a\\b");
    // bytes that are not UTF-8
    assert_eq!(sanitize(b"ok\xff\xfe!", 100), r"ok\xff\xfe!");
    // plain text is unchanged
    assert_eq!(sanitize("Grüße".as_bytes(), 100), "Grüße");
    // the cut is in bytes, at a character boundary, never longer than the limit
    assert_eq!(sanitize(b"abcdef", 3), "abc");
    assert_eq!(sanitize("aé".as_bytes(), 2), "a");
    assert_eq!(sanitize("aé".as_bytes(), 3), "aé");
    assert_eq!(sanitize(b"abc", 0), "");
    assert_eq!(sanitize(&[b'x'; 600], 512).len(), 512);
}

proptest! {
    #[test]
    fn sanitized_text_has_no_control_characters_and_fits(bytes in proptest::collection::vec(any::<u8>(), 0..300), max in 0usize..400) {
        let s = sanitize(&bytes, max);
        prop_assert!(s.len() <= max);
        prop_assert!(!s.chars().any(|c| c.is_control()));
        let bidi = '\u{202e}';
        prop_assert!(!s.contains(bidi));
    }
}

use stw::watch::event::format_utc;

#[test]
fn utc_time_is_formatted_with_a_z_and_no_fraction() {
    assert_eq!(format_utc(0), "1970-01-01T00:00:00Z");
    assert_eq!(format_utc(-1), "1969-12-31T23:59:59Z");
    assert_eq!(format_utc(1_000_000_000), "2001-09-09T01:46:40Z");
    assert_eq!(format_utc(951_782_400), "2000-02-29T00:00:00Z");
    assert_eq!(format_utc(951_868_800), "2000-03-01T00:00:00Z");
    assert_eq!(format_utc(4_102_444_800), "2100-01-01T00:00:00Z");
    assert_eq!(format_utc(1_792_003_723), "2026-10-14T18:48:43Z");
}

proptest! {
    #[test]
    fn utc_time_round_trips_through_the_reader(secs in 0i64..253_402_300_799) {
        prop_assert_eq!(stw::github::parse_utc(&format_utc(secs)), Some(secs));
    }
}

// ---------------------------------------------------------------------------------------------------------------------
// State and `apply`: the state file and the transition of every kind (the "Replay effects" table of Spec 035).

use stw::watch::event::{Details, NewPr, ReadyStateChanged, ReposChanged, Skipped};
use stw::watch::state::State;

const REPO: &str = "o/r";
const H1: &str = "1111111111111111111111111111111111111111";
const H2: &str = "2222222222222222222222222222222222222222";
const T0: &str = "2026-10-22T09:00:00Z";

/// An event of `REPO` (`repos-changed` has no repository).
fn ev(pr: Option<u64>, key: &str, details: Details) -> Event {
    let repo = (details.kind() != Kind::ReposChanged).then(|| REPO.to_string());
    let id = event_id(repo.as_deref(), pr, details.kind(), key);
    Event::new(id, T0.to_string(), repo, pr, details)
}

fn new_pr(n: u64, head: &str) -> Event {
    ev(
        Some(n),
        &key_generation(0),
        Details::NewPr(NewPr {
            title: format!("PR {n}"),
            author: "alice".into(),
            draft: false,
            base: "main".into(),
            head_ref: format!("feat/{n}"),
            head: head.into(),
            fork: false,
        }),
    )
}

fn repos_event(epoch: u64, added: &[&str], removed: &[&str], skipped: &[(&str, &str)]) -> Event {
    ev(
        None,
        &key_repos(epoch),
        Details::ReposChanged(ReposChanged {
            added: added.iter().map(|s| s.to_string()).collect(),
            removed: removed.iter().map(|s| s.to_string()).collect(),
            skipped: skipped
                .iter()
                .map(|(p, r)| Skipped {
                    path: p.to_string(),
                    reason: r.to_string(),
                })
                .collect(),
        }),
    )
}

#[test]
fn the_state_file_is_one_ordered_json_line_and_reads_back_equal() {
    let mut s = State::new();
    s.apply(&new_pr(10, H1));
    s.apply(&new_pr(9, H2));
    s.apply(&repos_event(1, &["z/z", "a/a"], &[], &[]));
    s.cycle = 4;
    s.interval_seconds = 60;
    let bytes = s.encode();
    assert_eq!(bytes.last(), Some(&b'\n'));
    assert_eq!(bytes.iter().filter(|b| **b == b'\n').count(), 1);
    assert_eq!(State::decode(&bytes).unwrap(), s);
    assert_eq!(s.encode(), bytes, "deterministic");
    let text = String::from_utf8(bytes).unwrap();
    assert!(
        text.starts_with(r#"{"format":"stw-watch-state","schema_version":1,"#),
        "{text}"
    );
    // PR numbers are object keys in numeric order of the map, `class` of an unobserved PR is the empty string
    assert!(
        text.contains(r#""prs":{"9":{"#) && text.contains(r#""class":"none""#),
        "{text}"
    );
    let mut unobserved = State::new();
    unobserved.apply(&ev(
        Some(1),
        &key_ready(1),
        Details::ReadyStateChanged(ReadyStateChanged::default()),
    ));
    assert!(String::from_utf8(unobserved.encode())
        .unwrap()
        .contains(r#""class":"""#));
}

#[test]
fn a_state_file_of_another_format_version_or_shape_is_refused() {
    let good = State::new().encode();
    let text = String::from_utf8(good).unwrap();
    for (what, bad) in [
        ("empty", String::new()),
        ("not json", "{".to_string()),
        ("an array", "[]".to_string()),
        (
            "a Go state file",
            r#"{"schema_version":1,"applied_seq":0,"repo":{},"health":{}}"#.to_string(),
        ),
        ("another format", text.replace("stw-watch-state", "other")),
        (
            "schema 2",
            text.replace(r#""schema_version":1"#, r#""schema_version":2"#),
        ),
        (
            "an unknown key",
            text.replace(r#""cycle":0"#, r#""cycle":0,"extra":1"#),
        ),
        ("a missing key", text.replace(r#""cycle":0,"#, "")),
        (
            "a wrong type",
            text.replace(r#""cycle":0"#, r#""cycle":"0""#),
        ),
        ("trailing data", format!("{} 1", text.trim_end())),
        (
            "a negative counter",
            text.replace(r#""cycle":0"#, r#""cycle":-1"#),
        ),
    ] {
        assert!(State::decode(bad.as_bytes()).is_err(), "{what}");
    }
    assert!(State::decode(text.as_bytes()).is_ok());
}
