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
    assert_eq!(
        id_counter("h7:watch-error:x", 'h'),
        None,
        "the first part of an id without a repository"
    );
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
use stw::watch::event::{
    BaseChanged, ChecksGreen, ChecksRed, Closed, HeadChanged, MergeableChanged, Merged,
    PendingTimeout, RestackNeeded,
};
use stw::watch::state::{Class, ClosedPr, Epochs, PrState};

fn state_with(events: &[Event]) -> State {
    let mut s = State::new();
    for e in events {
        s.apply(e);
    }
    s
}

fn pr(s: &State, n: u64) -> &PrState {
    &s.repo[REPO].prs[&n]
}

#[test]
fn a_new_pr_is_recorded_with_its_generation_and_a_due_detail_read() {
    let mut e = new_pr(4, H1);
    e.id = event_id(Some(REPO), Some(4), Kind::NewPr, &key_generation(2));
    let s = state_with(&[e]);
    let p = pr(&s, 4);
    assert_eq!(
        (
            p.title.as_str(),
            p.author.as_str(),
            p.base.as_str(),
            p.head_ref.as_str(),
            p.head.as_str()
        ),
        ("PR 4", "alice", "main", "feat/4", H1)
    );
    assert_eq!(
        (p.generation, p.class, p.detail_due, p.gone, p.fork, p.draft),
        (2, Class::NoChecks, true, false, false, false)
    );
}

#[test]
fn a_reopened_pr_keeps_the_counters_of_its_closed_record() {
    let mut s = State::new();
    s.apply(&new_pr(4, H1));
    s.apply(&ev(
        Some(4),
        &key_head(H2, 3),
        Details::HeadChanged(HeadChanged {
            old: H1.into(),
            new: H2.into(),
        }),
    ));
    s.apply(&ev(
        Some(4),
        &key_generation(0),
        Details::Closed(Closed {
            head: H2.into(),
            base: "main".into(),
        }),
    ));
    assert!(s.repo[REPO].prs.is_empty());
    assert_eq!(
        s.repo[REPO].closed,
        vec![ClosedPr {
            number: 4,
            generation: 0,
            epochs: Epochs {
                head: 3,
                ..Epochs::default()
            }
        }]
    );
    s.apply(&ev(
        Some(4),
        &key_generation(1),
        Details::NewPr(NewPr {
            head: H2.into(),
            ..NewPr::default()
        }),
    ));
    let p = pr(&s, 4);
    assert_eq!((p.generation, p.epochs.head), (1, 3));
}

#[test]
fn a_close_remembers_the_larger_counters_and_keeps_at_most_five_thousand_records() {
    let mut s = State::new();
    s.apply(&ev(
        Some(1),
        &key_generation(0),
        Details::Closed(Closed::default()),
    ));
    s.repo.get_mut(REPO).unwrap().closed[0].epochs.checks = 9;
    s.apply(&new_pr(1, H1));
    s.apply(&ev(
        Some(1),
        &key_generation(1),
        Details::Closed(Closed::default()),
    ));
    let rec = &s.repo[REPO].closed;
    assert_eq!(
        (rec.len(), rec[0].generation, rec[0].epochs.checks),
        (1, 1, 9),
        "the record is replaced, not doubled"
    );
    s.apply(&ev(
        Some(1),
        &key_generation(2),
        Details::Closed(Closed::default()),
    ));
    assert_eq!(
        s.repo[REPO].closed[0].epochs.checks, 9,
        "the counters of the older record survive a close of a PR without a record"
    );
    let rs = s.repo.get_mut(REPO).unwrap();
    for n in 2..5010u64 {
        rs.record_closed(ClosedPr {
            number: n,
            generation: 0,
            epochs: Epochs::default(),
        });
    }
    assert_eq!(
        rs.closed.len(),
        5000,
        "the cap is written out here, not read from the constant"
    );
    assert_eq!(rs.closed.last().unwrap().number, 5009);
    assert!(rs.closed_record(1).is_none(), "the oldest is dropped");
}

#[test]
fn head_changed_sets_the_head_raises_the_epoch_and_forgets_the_checks() {
    let mut s = State::new();
    s.apply(&new_pr(4, H1));
    {
        let p = s.repo.get_mut(REPO).unwrap().prs.get_mut(&4).unwrap();
        p.class = Class::Red;
        p.class_candidate = Some(Class::Green);
        p.timeout_fired = true;
        p.pending_since = T0.into();
        p.pending_names = vec!["ci".into()];
        p.detail_due = false;
        p.epochs.checks = 5;
    }
    s.apply(&ev(
        Some(4),
        &key_head(H2, 2),
        Details::HeadChanged(HeadChanged {
            old: H1.into(),
            new: H2.into(),
        }),
    ));
    let p = pr(&s, 4);
    assert_eq!(
        (p.head.as_str(), p.epochs.head, p.epochs.checks),
        (H2, 2, 5),
        "checks epoch survives"
    );
    assert_eq!(
        (
            p.class,
            p.class_candidate,
            p.timeout_fired,
            p.pending_since.as_str()
        ),
        (Class::Unobserved, None, false, "")
    );
    assert!(p.pending_names.is_empty() && p.detail_due);
    // a counter is never lowered
    s.apply(&ev(
        Some(4),
        &key_head(H1, 1),
        Details::HeadChanged(HeadChanged {
            old: H2.into(),
            new: H1.into(),
        }),
    ));
    assert_eq!(pr(&s, 4).epochs.head, 2);
}

#[test]
fn a_checks_event_of_the_current_head_sets_the_class_and_one_of_another_head_only_the_counter() {
    let mut s = State::new();
    s.apply(&new_pr(4, H1));
    let green = |head: &str, e: u64| {
        ev(
            Some(4),
            &key_checks(head, e),
            Details::ChecksGreen(ChecksGreen {
                head: head.into(),
                ..ChecksGreen::default()
            }),
        )
    };
    s.apply(&green(H1, 3));
    assert_eq!(
        (pr(&s, 4).class, pr(&s, 4).epochs.checks),
        (Class::Green, 3)
    );
    let red_of_another_head = ev(
        Some(4),
        &key_checks(H2, 7),
        Details::ChecksRed(ChecksRed {
            head: H2.into(),
            ..ChecksRed::default()
        }),
    );
    s.apply(&red_of_another_head);
    assert_eq!(
        (pr(&s, 4).class, pr(&s, 4).epochs.checks),
        (Class::Green, 7),
        "counter only"
    );
    let red = ev(
        Some(4),
        &key_checks(H1, 8),
        Details::ChecksRed(ChecksRed {
            head: H1.into(),
            ..ChecksRed::default()
        }),
    );
    s.apply(&red);
    let p = pr(&s, 4);
    assert_eq!(
        (p.class, p.epochs.checks, p.class_candidate, p.timeout_fired),
        (Class::Red, 8, None, false)
    );
    assert!(p.pending_since.is_empty() && p.pending_names.is_empty());
}

#[test]
fn a_pending_timeout_fires_once_for_the_head_and_keeps_the_pending_names() {
    let mut s = State::new();
    s.apply(&new_pr(4, H1));
    let t = |head: &str, e: u64| {
        ev(
            Some(4),
            &key_timeout(head, e),
            Details::ChecksPendingTimeout(PendingTimeout {
                head: head.into(),
                pending: vec!["a".into(), "b".into()],
                since: "2026-10-22T08:00:00Z".into(),
            }),
        )
    };
    s.apply(&t(H1, 2));
    let p = pr(&s, 4);
    assert_eq!(
        (
            p.class,
            p.timeout_fired,
            p.pending_since.as_str(),
            p.epochs.checks
        ),
        (Class::Pending, true, "2026-10-22T08:00:00Z", 2)
    );
    assert_eq!(p.pending_names, ["a", "b"]);
    // another head: the counter is raised, the state stays
    let mut other = s.clone();
    other.apply(&ev(
        Some(4),
        &key_head(H2, 1),
        Details::HeadChanged(HeadChanged {
            old: H1.into(),
            new: H2.into(),
        }),
    ));
    other.apply(&t(H1, 9));
    assert_eq!(
        (
            pr(&other, 4).class,
            pr(&other, 4).timeout_fired,
            pr(&other, 4).epochs.checks
        ),
        (Class::Unobserved, false, 9)
    );
}

#[test]
fn mergeable_ready_and_base_events_set_their_fields_and_counters() {
    let mut s = State::new();
    s.apply(&new_pr(4, H1));
    s.repo
        .get_mut(REPO)
        .unwrap()
        .prs
        .get_mut(&4)
        .unwrap()
        .detail_due = false;
    s.repo
        .get_mut(REPO)
        .unwrap()
        .prs
        .get_mut(&4)
        .unwrap()
        .mergeable_candidate = Some(stw::watch::state::MergeCandidate {
        mergeable: false,
        behind: false,
        state: "dirty".into(),
    });
    s.apply(&ev(
        Some(4),
        &key_mergeable(H1, 2),
        Details::MergeableChanged(MergeableChanged {
            mergeable: Some(false),
            state: "dirty".into(),
            conflicting: true,
            behind: false,
            base: "main".into(),
        }),
    ));
    let p = pr(&s, 4);
    assert_eq!(
        (
            p.mergeable,
            p.mergeable_state.as_str(),
            p.conflicting,
            p.behind,
            p.epochs.mergeable
        ),
        (Some(false), "dirty", true, false, 2)
    );
    assert!(p.mergeable_candidate.is_none());
    // `null` (not computed) keeps the old value
    s.apply(&ev(
        Some(4),
        &key_mergeable(H1, 3),
        Details::MergeableChanged(MergeableChanged {
            mergeable: None,
            state: "unknown".into(),
            ..MergeableChanged::default()
        }),
    ));
    assert_eq!(
        (pr(&s, 4).mergeable, pr(&s, 4).epochs.mergeable),
        (Some(false), 3)
    );
    s.apply(&ev(
        Some(4),
        &key_ready(4),
        Details::ReadyStateChanged(ReadyStateChanged {
            from: "ready".into(),
            to: "draft".into(),
        }),
    ));
    assert_eq!((pr(&s, 4).draft, pr(&s, 4).epochs.ready), (true, 4));
    s.apply(&ev(
        Some(4),
        &key_ready(5),
        Details::ReadyStateChanged(ReadyStateChanged {
            from: "draft".into(),
            to: "ready".into(),
        }),
    ));
    assert!(!pr(&s, 4).draft);
    s.apply(&ev(
        Some(4),
        &key_base(6),
        Details::BaseChanged(BaseChanged {
            old: "main".into(),
            new: "dev".into(),
        }),
    ));
    let p = pr(&s, 4);
    assert_eq!(
        (p.base.as_str(), p.epochs.base, p.detail_due),
        ("dev", 6, true)
    );
}

#[test]
fn a_merge_removes_the_pr_records_the_restack_work_once_and_makes_every_detail_read_due() {
    let mut s = State::new();
    s.apply(&new_pr(1, H1));
    s.apply(&new_pr(2, H2));
    for n in [1, 2] {
        s.repo
            .get_mut(REPO)
            .unwrap()
            .prs
            .get_mut(&n)
            .unwrap()
            .detail_due = false;
    }
    let merged = ev(
        Some(1),
        &key_merged(Some("m1")),
        Details::Merged(Merged {
            merge_commit: Some("m1".into()),
            base: "main".into(),
            head: H1.into(),
            head_ref: "feat/1".into(),
            merged_at: T0.into(),
            method_hint: "merge".into(),
        }),
    );
    s.apply(&merged);
    s.apply(&merged);
    let rs = &s.repo[REPO];
    assert!(!rs.prs.contains_key(&1) && rs.prs[&2].detail_due);
    assert_eq!(rs.restack_due.len(), 1, "the same merge is one record");
    let d = &rs.restack_due[0];
    assert_eq!(
        (
            d.merged_pr,
            d.merge_commit.as_deref(),
            d.old_parent.as_str(),
            d.base.as_str(),
            d.head_ref.as_str()
        ),
        (1, Some("m1"), H1, "main", "feat/1")
    );
    assert!(
        !d.head_fork,
        "PR 1 was observed open with its head in the base repository"
    );
}

#[test]
fn a_merge_of_an_unobserved_or_forked_pr_has_a_fork_head() {
    let merged = |n: u64| {
        ev(
            Some(n),
            &key_merged(None),
            Details::Merged(Merged {
                merge_commit: None,
                ..Merged::default()
            }),
        )
    };
    let mut s = State::new();
    s.apply(&merged(9));
    assert!(
        s.repo[REPO].restack_due[0].head_fork,
        "never observed: unknown head repository"
    );
    let mut forked = new_pr(5, H1);
    if let Details::NewPr(d) = &mut forked.details {
        d.fork = true;
    }
    s.apply(&forked);
    s.apply(&merged(5));
    assert!(
        s.repo[REPO]
            .restack_due
            .iter()
            .find(|d| d.merged_pr == 5)
            .unwrap()
            .head_fork
    );
    // a second merge commit of the same PR number is another record
    s.apply(&ev(
        Some(9),
        &key_merged(Some("x")),
        Details::Merged(Merged {
            merge_commit: Some("x".into()),
            ..Merged::default()
        }),
    ));
    assert_eq!(
        s.repo[REPO]
            .restack_due
            .iter()
            .filter(|d| d.merged_pr == 9)
            .count(),
        2
    );
}

#[test]
fn a_restack_event_marks_the_pair_done_once() {
    let mut s = State::new();
    let e = ev(
        Some(2),
        &key_restack(1, Some("m1")),
        Details::RestackNeeded(RestackNeeded {
            merged_pr: 1,
            ..RestackNeeded::default()
        }),
    );
    s.apply(&e);
    s.apply(&e);
    assert_eq!(s.repo[REPO].restack_done, std::slice::from_ref(&e.id));
    assert!(
        s.repo[REPO].prs.is_empty(),
        "a restack event does not create a PR"
    );
}

#[test]
fn an_event_of_a_pr_without_a_record_creates_it_to_keep_the_counters() {
    let s = state_with(&[ev(
        Some(4),
        &key_ready(3),
        Details::ReadyStateChanged(ReadyStateChanged {
            from: "ready".into(),
            to: "draft".into(),
        }),
    )]);
    assert_eq!(
        (pr(&s, 4).epochs.ready, pr(&s, 4).draft, pr(&s, 4).class),
        (3, true, Class::Unobserved)
    );
    assert_eq!(s.repo[REPO].since, T0);
    // a checks event teaches the rebuilt PR its head
    let s = state_with(&[ev(
        Some(4),
        &key_checks(H1, 2),
        Details::ChecksGreen(ChecksGreen {
            head: H1.into(),
            ..ChecksGreen::default()
        }),
    )]);
    assert_eq!(
        (pr(&s, 4).head.as_str(), pr(&s, 4).class),
        (H1, Class::Green)
    );
}
use stw::watch::event::{WatchError as WatchErrorDetails, WatchRecovered, WatchStalled};
use stw::watch::state::RepoState;

#[test]
fn repos_changed_updates_the_set_the_skipped_list_and_the_baseline_marks() {
    let mut s = State::new();
    s.repos = Some(vec!["a/a".into(), "b/b".into()]);
    for r in ["a/a", "b/b"] {
        let mut rs = RepoState::new(T0);
        rs.baselined = true;
        rs.restack_due.push(stw::watch::state::RestackDue {
            merged_pr: 1,
            merge_commit: None,
            old_parent: "x".into(),
            base: "main".into(),
            head_ref: "h".into(),
            head_fork: false,
        });
        rs.restack_done.push("id".into());
        rs.conditional = Some(serde_json::json!({"pages": {}}));
        s.repo.insert(r.to_string(), rs);
    }
    s.health_mut("b/b").next_try = "2026-10-22T09:10:00Z".into();
    s.apply(&repos_event(
        3,
        &["c/c"],
        &["b/b"],
        &[("/w/x", "no github remote")],
    ));
    assert_eq!(
        s.repos.as_deref(),
        Some(&["a/a".to_string(), "c/c".to_string()][..])
    );
    assert_eq!(s.repos_epoch, 3);
    assert_eq!(
        s.skipped,
        vec![Skipped {
            path: "/w/x".into(),
            reason: "no github remote".into()
        }]
    );
    let b = &s.repo["b/b"];
    assert!(
        !b.baselined
            && b.restack_due.is_empty()
            && b.restack_done.is_empty()
            && b.conditional.is_none(),
        "the removed record stays, reset"
    );
    assert_eq!(s.health["b/b"].next_try, "");
    let c = &s.repo["c/c"];
    assert!(!c.baselined);
    assert_eq!(c.since, T0);
    assert!(s.repo["a/a"].baselined, "untouched");
    // a repository that comes back needs its own baseline again; a repeated add is not doubled
    s.apply(&repos_event(4, &["b/b", "c/c"], &[], &[]));
    assert_eq!(
        s.repos.as_deref(),
        Some(&["a/a".to_string(), "c/c".to_string(), "b/b".to_string()][..])
    );
    assert!(s.skipped.is_empty());
    s.apply(&repos_event(2, &[], &[], &[]));
    assert_eq!(s.repos_epoch, 4, "never lowered");
}

fn run_ev(repo: Option<&str>, key: &str, details: Details) -> Event {
    let id = event_id(repo, None, details.kind(), key);
    Event::new(id, T0.to_string(), repo.map(str::to_string), None, details)
}

fn error_event(repo: Option<&str>, code: &str, episode: u64, failures: u64, retry: u64) -> Event {
    run_ev(
        repo,
        &key_error(code, episode, failures),
        Details::WatchError(WatchErrorDetails {
            code: code.into(),
            message: "m".into(),
            failures,
            retry_in_seconds: retry,
            repo: repo.map(str::to_string),
        }),
    )
}

#[test]
fn health_events_set_the_episode_the_open_error_and_the_retry_time() {
    let mut s = State::new();
    s.apply(&error_event(Some(REPO), "network", 2, 1, 120));
    let h = &s.health[REPO];
    assert_eq!(
        (
            h.episode,
            h.failures,
            h.last_code.as_str(),
            h.error_open,
            h.next_try.as_str()
        ),
        (2, 1, "network", true, "2026-10-22T09:02:00Z")
    );
    s.apply(&error_event(Some(REPO), "forbidden", 2, 3, 60));
    assert_eq!(
        (s.health[REPO].failures, s.health[REPO].last_code.as_str()),
        (3, "forbidden")
    );
    s.apply(&error_event(Some(REPO), "network", 1, 2, 60));
    assert_eq!(
        (s.health[REPO].episode, s.health[REPO].failures),
        (2, 3),
        "never lowered"
    );
    // an error without a repository is the run level
    s.apply(&error_event(None, "rate-limited", 1, 1, 30));
    assert!(s.health[""].error_open && s.health[""].episode == 1);
    assert_eq!(s.health.len(), 2);
    // recovery after the error episode
    let rec = run_ev(
        Some(REPO),
        &key_recovered_error(2),
        Details::WatchRecovered(WatchRecovered {
            after_failures: 3,
            repo: Some(REPO.into()),
        }),
    );
    s.apply(&rec);
    let h = &s.health[REPO];
    assert_eq!(
        (h.error_open, h.failures, h.last_code.as_str(), h.episode),
        (false, 0, "", 2)
    );
}

#[test]
fn a_stall_and_its_recovery_use_the_stall_episode_and_leave_the_error_state_alone() {
    let mut s = State::new();
    s.health_mut(REPO).error_open = true;
    s.health_mut(REPO).last_code = "network".into();
    let stalled = run_ev(
        Some(REPO),
        &key_stall(3),
        Details::WatchStalled(WatchStalled {
            since: T0.into(),
            threshold_seconds: 600,
            repo: REPO.into(),
        }),
    );
    s.apply(&stalled);
    assert_eq!(
        (s.health[REPO].stalled, s.health[REPO].stall_episode),
        (true, 3)
    );
    let rec = run_ev(
        Some(REPO),
        &key_stall(3),
        Details::WatchRecovered(WatchRecovered {
            after_failures: 5,
            repo: Some(REPO.into()),
        }),
    );
    s.apply(&rec);
    let h = &s.health[REPO];
    assert_eq!(
        (
            h.stalled,
            h.stall_episode,
            h.error_open,
            h.last_code.as_str()
        ),
        (false, 3, true, "network")
    );
}

#[test]
fn applying_the_same_event_again_changes_nothing() {
    let mut events = vec![
        new_pr(1, H1),
        new_pr(2, H2),
        ev(
            Some(1),
            &key_head(H2, 1),
            Details::HeadChanged(HeadChanged {
                old: H1.into(),
                new: H2.into(),
            }),
        ),
        ev(
            Some(1),
            &key_checks(H2, 1),
            Details::ChecksRed(ChecksRed {
                head: H2.into(),
                failing: vec!["ci".into()],
                ..ChecksRed::default()
            }),
        ),
        ev(
            Some(1),
            &key_ready(1),
            Details::ReadyStateChanged(ReadyStateChanged {
                from: "ready".into(),
                to: "draft".into(),
            }),
        ),
        ev(
            Some(1),
            &key_base(1),
            Details::BaseChanged(BaseChanged {
                old: "main".into(),
                new: "dev".into(),
            }),
        ),
        ev(
            Some(1),
            &key_mergeable(H2, 1),
            Details::MergeableChanged(MergeableChanged {
                mergeable: Some(true),
                ..MergeableChanged::default()
            }),
        ),
        ev(
            Some(1),
            &key_timeout(H2, 1),
            Details::ChecksPendingTimeout(PendingTimeout {
                head: H2.into(),
                pending: vec!["x".into()],
                since: T0.into(),
            }),
        ),
        repos_event(1, &["n/n"], &[], &[("/p", "r")]),
        ev(
            Some(2),
            &key_restack(1, Some("m")),
            Details::RestackNeeded(RestackNeeded::default()),
        ),
        ev(
            Some(1),
            &key_merged(Some("m")),
            Details::Merged(Merged {
                merge_commit: Some("m".into()),
                head: H2.into(),
                ..Merged::default()
            }),
        ),
        ev(
            Some(2),
            &key_generation(0),
            Details::Closed(Closed::default()),
        ),
    ];
    events.push(run_ev(
        Some(REPO),
        &key_stall(1),
        Details::WatchStalled(WatchStalled {
            since: T0.into(),
            threshold_seconds: 600,
            repo: REPO.into(),
        }),
    ));
    events.push(error_event(None, "network", 1, 1, 5));
    let mut s = State::new();
    for e in &events {
        s.apply(e);
        let once = s.clone();
        s.apply(e);
        assert_eq!(s, once, "{}", e.id);
    }
}

// ---------------------------------------------------------------------------------------------------------------------
// The state directory: creation and refusal, atomic files, heartbeat, state.json.
use std::os::unix::fs::PermissionsExt;
use std::path::Path;

use stw::watch::statedir::{
    clean_path, ensure_state_dir, load_state, read_heartbeat, save_state, write_atomic,
    write_heartbeat, STATE_NAME,
};
use stw::watch::WatchError;

fn tmp() -> tempfile::TempDir {
    tempfile::Builder::new()
        .prefix("stw-watch-core-")
        .tempdir()
        .unwrap()
}

fn mode(p: &Path) -> u32 {
    std::fs::metadata(p).unwrap().permissions().mode() & 0o777
}

#[test]
fn a_new_state_directory_is_created_private_with_its_missing_parents() {
    let t = tmp();
    let dir = t.path().join("a").join("b").join("watch");
    ensure_state_dir(&dir).unwrap();
    assert!(dir.is_dir());
    assert_eq!(
        (
            mode(&dir),
            mode(&t.path().join("a")),
            mode(&t.path().join("a/b"))
        ),
        (0o700, 0o700, 0o700)
    );
    ensure_state_dir(&dir).unwrap(); // an existing 0700 directory is accepted
}

#[test]
fn a_relative_state_directory_is_a_usage_error_and_nothing_is_created() {
    assert_eq!(
        ensure_state_dir(Path::new("relative/dir")),
        Err(WatchError::StateDirUsage)
    );
    assert!(!Path::new("relative").exists());
    assert_eq!(
        ensure_state_dir(Path::new("")),
        Err(WatchError::StateDirUsage)
    );
}

#[test]
fn a_directory_with_another_mode_is_refused() {
    let t = tmp();
    for m in [0o755, 0o770, 0o750, 0o500] {
        let dir = t.path().join(format!("m{m:o}"));
        std::fs::create_dir(&dir).unwrap();
        std::fs::set_permissions(&dir, std::fs::Permissions::from_mode(m)).unwrap();
        assert!(
            matches!(ensure_state_dir(&dir), Err(WatchError::StateDirMode(_))),
            "{m:o}"
        );
    }
}

#[test]
fn a_directory_inside_a_git_work_tree_or_below_dot_stw_is_refused_before_anything_is_created() {
    let t = tmp();
    let tree = t.path().join("tree");
    std::fs::create_dir_all(tree.join(".git")).unwrap();
    let inside = tree.join("deep").join("state");
    assert!(matches!(
        ensure_state_dir(&inside),
        Err(WatchError::InsideWorkTree(_))
    ));
    assert!(!tree.join("deep").exists(), "nothing created");
    // a `.git` file (a linked work tree or a submodule) counts too
    let linked = t.path().join("linked");
    std::fs::create_dir(&linked).unwrap();
    std::fs::write(linked.join(".git"), "gitdir: /elsewhere\n").unwrap();
    assert!(matches!(
        ensure_state_dir(&linked.join("s")),
        Err(WatchError::InsideWorkTree(_))
    ));
    // the work tree itself
    assert!(matches!(
        ensure_state_dir(&tree),
        Err(WatchError::InsideWorkTree(_))
    ));
    // below a `.stw` directory
    let stw = t.path().join("ws").join(".stw").join("state");
    assert!(matches!(
        ensure_state_dir(&stw),
        Err(WatchError::InsideWorkTree(_))
    ));
    assert!(!t.path().join("ws").exists());
}

#[test]
fn a_symbolic_link_into_a_work_tree_is_seen_and_dot_dot_is_resolved_first() {
    let t = tmp();
    let tree = t.path().join("tree");
    std::fs::create_dir_all(tree.join(".git")).unwrap();
    let link = t.path().join("link");
    std::os::unix::fs::symlink(&tree, &link).unwrap();
    assert!(matches!(
        ensure_state_dir(&link.join("state")),
        Err(WatchError::InsideWorkTree(_))
    ));
    // `tree/../ok` is `ok`: fine although the path names the work tree
    let ok = tree.join("..").join("ok");
    ensure_state_dir(&ok).unwrap();
    assert!(t.path().join("ok").is_dir());
    assert_eq!(clean_path(Path::new("/a/b/../c/./d")), Path::new("/a/c/d"));
    assert_eq!(clean_path(Path::new("/../x")), Path::new("/x"));
    assert_eq!(clean_path(Path::new("/")), Path::new("/"));
}

#[test]
fn a_path_that_is_a_file_is_not_a_state_directory() {
    let t = tmp();
    let f = t.path().join("file");
    std::fs::write(&f, "x").unwrap();
    assert!(
        matches!(ensure_state_dir(&f), Err(WatchError::Io(_))),
        "a file"
    );
}

#[test]
fn an_atomic_write_leaves_the_content_with_mode_0600_and_no_temporary_file() {
    let t = tmp();
    write_atomic(t.path(), "x.json", b"one").unwrap();
    write_atomic(t.path(), "x.json", b"two").unwrap();
    assert_eq!(std::fs::read(t.path().join("x.json")).unwrap(), b"two");
    assert_eq!(mode(&t.path().join("x.json")), 0o600);
    let names: Vec<String> = std::fs::read_dir(t.path())
        .unwrap()
        .map(|e| e.unwrap().file_name().into_string().unwrap())
        .collect();
    assert_eq!(names, ["x.json"]);
    // a failing write (no such directory) leaves nothing and says where
    let err = write_atomic(&t.path().join("nope"), "x", b"1").unwrap_err();
    assert!(matches!(err, WatchError::Io(_)), "{err}");
}

#[test]
fn the_heartbeat_is_one_line_with_time_pid_and_mode() {
    let t = tmp();
    write_heartbeat(t.path(), 1_792_003_723, 4242, "stream").unwrap();
    assert_eq!(
        std::fs::read_to_string(t.path().join("heartbeat")).unwrap(),
        "2026-10-14T18:48:43Z 4242 stream\n"
    );
    assert_eq!(mode(&t.path().join("heartbeat")), 0o600);
    let hb = read_heartbeat(t.path(), 1_792_003_723 + 90)
        .unwrap()
        .unwrap();
    assert_eq!(
        (hb.time.as_str(), hb.age_seconds, hb.pid, hb.mode.as_str()),
        ("2026-10-14T18:48:43Z", 90, 4242, "stream")
    );
    // a clock that went back counts as age 0
    assert_eq!(
        read_heartbeat(t.path(), 1_792_003_000)
            .unwrap()
            .unwrap()
            .age_seconds,
        0
    );
}

#[test]
fn a_missing_or_unreadable_heartbeat_is_none() {
    let t = tmp();
    assert_eq!(read_heartbeat(t.path(), 0).unwrap(), None);
    for bad in [
        "",
        "garbage\n",
        "2026-10-14T18:48:43Z 1\n",
        "2026-10-14T18:48:43Z x stream\n",
        "yesterday 1 stream\n",
        "2026-10-14T18:48:43Z 1 stream extra\n",
    ] {
        std::fs::write(t.path().join("heartbeat"), bad).unwrap();
        assert_eq!(read_heartbeat(t.path(), 0).unwrap(), None, "{bad:?}");
    }
    std::fs::write(t.path().join("heartbeat"), [0xff, 0xfe]).unwrap();
    assert_eq!(read_heartbeat(t.path(), 0).unwrap(), None, "not UTF-8");
}

#[test]
fn state_json_is_written_atomically_and_read_back() {
    let t = tmp();
    assert_eq!(load_state(t.path()).unwrap(), None, "missing");
    let mut s = State::new();
    s.cycle = 7;
    s.applied_seq = 3;
    save_state(t.path(), &s).unwrap();
    assert_eq!(mode(&t.path().join(STATE_NAME)), 0o600);
    assert_eq!(load_state(t.path()).unwrap(), Some(s));
    std::fs::write(t.path().join(STATE_NAME), "{not json").unwrap();
    assert!(matches!(
        load_state(t.path()),
        Err(WatchError::CorruptState(_))
    ));
    std::fs::write(t.path().join(STATE_NAME), r#"{"schema_version":1}"#).unwrap();
    assert!(
        matches!(load_state(t.path()), Err(WatchError::CorruptState(_))),
        "a Go state file is not ours"
    );
}

// ---------------------------------------------------------------------------------------------------------------------
// The journal: gapless seq from the lines, a truncated tail, corruption.
use std::io::Write as _;

use stw::watch::journal::{scan, Journal, JOURNAL_NAME, MAX_LINE_BYTES};

fn ready_event(n: u64) -> Event {
    let id =
        stw::watch::event::event_id(Some("o/r"), Some(1), Kind::ReadyStateChanged, &key_ready(n));
    Event::new(
        id,
        "2026-10-22T09:00:00Z".into(),
        Some("o/r".into()),
        Some(1),
        Details::ReadyStateChanged(ReadyStateChanged {
            from: "ready".into(),
            to: "draft".into(),
        }),
    )
}

fn append_n(j: &mut Journal, n: u64) {
    for i in 1..=n {
        j.append(&ready_event(i)).unwrap();
    }
}

fn raw(dir: &Path) -> Vec<u8> {
    std::fs::read(dir.join(JOURNAL_NAME)).unwrap()
}

#[test]
fn a_new_journal_is_empty_private_and_numbers_the_events_from_one() {
    let t = tmp();
    let mut j = Journal::open(t.path()).unwrap();
    assert_eq!((j.tail(), j.junk_bytes()), (0, 0));
    assert_eq!(mode(&t.path().join(JOURNAL_NAME)), 0o600);
    let a = j.append(&ready_event(1)).unwrap();
    let b = j.append(&ready_event(2)).unwrap();
    assert_eq!((a.seq, b.seq, j.tail()), (1, 2, 2));
    let lines = String::from_utf8(raw(t.path())).unwrap();
    assert_eq!(lines.lines().count(), 2);
    assert!(lines.ends_with('\n'));
    assert_eq!(Event::from_line(lines.lines().next().unwrap()).unwrap(), a);
    // the seq of the event handed in is ignored
    let mut e = ready_event(3);
    e.seq = 99;
    assert_eq!(j.append(&e).unwrap().seq, 3);
}

#[test]
fn the_next_seq_comes_from_the_last_line_after_a_reopen() {
    let t = tmp();
    append_n(&mut Journal::open(t.path()).unwrap(), 5);
    let mut j = Journal::open(t.path()).unwrap();
    assert_eq!(j.tail(), 5);
    assert_eq!(j.append(&ready_event(6)).unwrap().seq, 6);
    let after: Vec<u64> = j.after(3).unwrap().iter().map(|e| e.seq).collect();
    assert_eq!(after, [4, 5, 6]);
    assert!(j.after(6).unwrap().is_empty());
    assert!(j.after(100).unwrap().is_empty());
    assert_eq!(j.after(0).unwrap().len(), 6);
}

#[test]
fn a_journal_larger_than_a_megabyte_keeps_its_tail() {
    // the spike took the seq from an offset one megabyte before the end; the lines are the source here
    let t = tmp();
    let mut j = Journal::open(t.path()).unwrap();
    let mut e = ready_event(1);
    if let Details::ReadyStateChanged(d) = &mut e.details {
        d.from = "x".repeat(2000);
    }
    for _ in 0..700 {
        j.append(&e).unwrap();
    }
    assert!(raw(t.path()).len() > 1 << 20);
    drop(j);
    let mut j = Journal::open(t.path()).unwrap();
    assert_eq!(j.tail(), 700);
    assert_eq!(j.append(&e).unwrap().seq, 701);
}

#[test]
fn a_truncated_last_line_is_reported_ignored_and_cut_off_by_the_next_append() {
    let t = tmp();
    append_n(&mut Journal::open(t.path()).unwrap(), 2);
    let path = t.path().join(JOURNAL_NAME);
    let good = std::fs::metadata(&path).unwrap().len();
    for tail in [
        &b"{\"schema_version\":1,\"seq\":3,\"id\":\"x"[..],
        b"garbage without a newline",
        b"\x00\x00\x00",
        b"{",
        // longer than the line written over it: only a cut removes all of it
        &[b'x'; 1000],
    ] {
        let mut f = std::fs::OpenOptions::new()
            .append(true)
            .open(&path)
            .unwrap();
        f.write_all(tail).unwrap();
        drop(f);
        let mut j = Journal::open(t.path()).unwrap();
        assert_eq!(
            (j.tail(), j.junk_bytes()),
            (2, tail.len() as u64),
            "{tail:?}"
        );
        assert_eq!(j.after(0).unwrap().len(), 2);
        assert_eq!(j.append(&ready_event(3)).unwrap().seq, 3);
        assert_eq!(j.junk_bytes(), 0);
        // every line of the file validates again and nothing of the junk is left
        let mut n = 0;
        let end = scan(&path, &mut |_| n += 1).unwrap();
        assert_eq!(n, 3);
        assert_eq!(end.size, end.good, "no byte of the junk is left: {tail:?}");
        // restore two events for the next round
        std::fs::OpenOptions::new()
            .write(true)
            .open(&path)
            .unwrap()
            .set_len(good)
            .unwrap();
    }
}

#[test]
fn a_complete_line_that_is_not_a_valid_event_is_corruption() {
    let t = tmp();
    append_n(&mut Journal::open(t.path()).unwrap(), 2);
    let path = t.path().join(JOURNAL_NAME);
    let original = std::fs::read_to_string(&path).unwrap();
    let lines: Vec<&str> = original.lines().collect();
    for (what, content) in [
        (
            "an invalid line in the middle",
            format!("{}\nnot json\n{}\n", lines[0], lines[1]),
        ),
        (
            "an invalid last line with a newline",
            format!("{}\n{}\nnot json\n", lines[0], lines[1]),
        ),
        (
            "a gap in seq",
            format!(
                "{}\n{}\n",
                lines[0],
                lines[1].replace("\"seq\":2", "\"seq\":3")
            ),
        ),
        ("a repeated seq", format!("{}\n{}\n", lines[0], lines[0])),
        (
            "a journal that does not start at 1",
            format!("{}\n", lines[1]),
        ),
        (
            "an unknown kind",
            format!("{}\n", lines[0].replace("ready-state-changed", "nope")),
        ),
        ("a line that is not UTF-8", String::from("x")),
    ] {
        if what == "a line that is not UTF-8" {
            std::fs::write(&path, [b'{', 0xff, b'\n']).unwrap();
        } else {
            std::fs::write(&path, content).unwrap();
        }
        let err = Journal::open(t.path())
            .err()
            .unwrap_or_else(|| panic!("{what} was accepted"));
        assert!(matches!(err, WatchError::CorruptState(_)), "{what}: {err}");
        assert!(scan(&path, &mut |_| {}).is_err(), "{what}");
    }
}

#[test]
fn a_line_longer_than_the_limit_is_corruption_not_a_huge_allocation() {
    let t = tmp();
    let path = t.path().join(JOURNAL_NAME);
    // a valid event is refused for its length alone
    let mut long = ready_event(1);
    long.seq = 1;
    if let Details::ReadyStateChanged(d) = &mut long.details {
        d.from = "x".repeat(MAX_LINE_BYTES);
    }
    std::fs::write(&path, format!("{}\n", long.to_line())).unwrap();
    assert!(matches!(
        Journal::open(t.path()).err(),
        Some(WatchError::CorruptState(_))
    ));
    let mut big = vec![b'a'; MAX_LINE_BYTES + 10];
    // terminated or not: too long is too long
    std::fs::write(&path, &big).unwrap();
    assert!(matches!(
        Journal::open(t.path()).err(),
        Some(WatchError::CorruptState(_))
    ));
    big.push(b'\n');
    std::fs::write(&path, &big).unwrap();
    assert!(matches!(
        Journal::open(t.path()).err(),
        Some(WatchError::CorruptState(_))
    ));
}

#[test]
fn opening_tightens_the_mode_of_an_existing_journal() {
    let t = tmp();
    let path = t.path().join(JOURNAL_NAME);
    std::fs::write(&path, "").unwrap();
    std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o644)).unwrap();
    Journal::open(t.path()).unwrap();
    assert_eq!(mode(&path), 0o600);
}

// ---------------------------------------------------------------------------------------------------------------------
// The lock of the state directory.
use std::io::{BufRead, BufReader};
use std::process::{Command, Stdio};
use std::time::Duration;

use stw::watch::lock::{acquire_with, holder, LOCK_NAME};

const NO_WAIT: Duration = Duration::from_millis(0);

#[test]
fn a_second_handle_in_the_same_process_is_locked_out_and_sees_the_pid() {
    let t = tmp();
    let first = acquire_with(t.path(), 1, NO_WAIT).unwrap();
    assert_eq!(mode(&t.path().join(LOCK_NAME)), 0o600);
    let pid = std::process::id();
    assert_eq!(
        std::fs::read_to_string(t.path().join(LOCK_NAME)).unwrap(),
        format!("{pid}\n")
    );
    assert_eq!(
        acquire_with(t.path(), 2, NO_WAIT).unwrap_err(),
        WatchError::Locked(Some(pid))
    );
    assert_eq!(holder(t.path()), Some(Some(pid)));
    first.release();
    assert_eq!(holder(t.path()), None);
    let again = acquire_with(t.path(), 1, NO_WAIT).unwrap();
    drop(again);
    acquire_with(t.path(), 1, NO_WAIT).unwrap(); // dropping releases too
}

#[test]
fn the_holder_probe_never_creates_the_lock_file_and_leaves_a_free_lock_free() {
    let t = tmp();
    assert_eq!(holder(t.path()), None);
    assert!(!t.path().join(LOCK_NAME).exists());
    drop(acquire_with(t.path(), 1, NO_WAIT).unwrap());
    assert_eq!(holder(t.path()), None);
    acquire_with(t.path(), 1, NO_WAIT).unwrap();
}

#[test]
fn an_unreadable_pid_is_reported_as_unknown() {
    let t = tmp();
    let l = acquire_with(t.path(), 1, NO_WAIT).unwrap();
    std::fs::write(t.path().join(LOCK_NAME), "not a pid").unwrap();
    assert_eq!(
        acquire_with(t.path(), 1, NO_WAIT).unwrap_err(),
        WatchError::Locked(None)
    );
    assert_eq!(holder(t.path()), Some(None));
    drop(l);
}

#[test]
fn a_busy_lock_is_retried_for_the_pause_before_it_is_reported() {
    let t = tmp();
    let l = acquire_with(t.path(), 1, NO_WAIT).unwrap();
    let start = std::time::Instant::now();
    assert!(acquire_with(t.path(), 3, Duration::from_millis(60)).is_err());
    assert!(
        start.elapsed() >= Duration::from_millis(120),
        "two pauses between three attempts"
    );
    let dir = t.path().to_path_buf();
    let h = std::thread::spawn(move || acquire_with(&dir, 20, Duration::from_millis(50)).map(drop));
    std::thread::sleep(Duration::from_millis(100));
    drop(l);
    h.join()
        .unwrap()
        .expect("the retry got the lock once it was free");
}

/// Not a test of its own: the child of `a_second_process_is_locked_out`. It takes the lock, says so, and holds it until its
/// standard input closes.
#[test]
#[ignore]
fn lock_holder_child() {
    let Ok(dir) = std::env::var("STW_WATCH_LOCK_CHILD_DIR") else {
        return;
    };
    let _l = acquire_with(Path::new(&dir), 1, NO_WAIT).unwrap();
    println!("LOCKED {}", std::process::id());
    let mut line = String::new();
    let _ = std::io::stdin().read_line(&mut line);
}

#[test]
fn a_second_process_is_locked_out_and_the_lock_is_free_when_it_dies() {
    let t = tmp();
    let mut child = Command::new(std::env::current_exe().unwrap())
        .args([
            "--exact",
            "watch_core::lock_holder_child",
            "--ignored",
            "--nocapture",
            "--test-threads=1",
        ])
        .env("STW_WATCH_LOCK_CHILD_DIR", t.path())
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    // The reader runs in a thread so that a child that never says anything fails the test after a time limit instead of
    // hanging it (and the cargo queue). libtest prints `test NAME ... ` in front of the first output line of the child.
    let out = BufReader::new(child.stdout.take().unwrap());
    let (tx, rx) = std::sync::mpsc::channel::<u32>();
    std::thread::spawn(move || {
        for line in out.lines().map_while(Result::ok) {
            if let Some(p) = line
                .split_once("LOCKED ")
                .and_then(|(_, p)| p.trim().parse().ok())
            {
                let _ = tx.send(p);
            }
        }
    });
    let pid = rx.recv_timeout(Duration::from_secs(30));
    if pid.is_err() {
        let _ = child.kill();
        let _ = child.wait();
    }
    let pid = pid.expect("the child took the lock within 30 s");
    assert_ne!(pid, std::process::id());
    assert_eq!(
        acquire_with(t.path(), 1, NO_WAIT).unwrap_err(),
        WatchError::Locked(Some(pid))
    );
    assert_eq!(holder(t.path()), Some(Some(pid)));
    drop(child.stdin.take()); // the child reads EOF and exits
    child.wait().unwrap();
    assert_eq!(holder(t.path()), None);
    acquire_with(t.path(), 1, NO_WAIT).unwrap();
}

// ---------------------------------------------------------------------------------------------------------------------
// The status report.
use stw::watch::lock::acquire;
use stw::watch::statedir::read_status;

#[test]
fn the_status_is_read_from_the_directory_alone() {
    let t = tmp();
    let dir = t.path().join("s");
    ensure_state_dir(&dir).unwrap();
    let mut st = State::new();
    st.interval_seconds = 600;
    st.repos = Some(vec!["a/a".into(), "b/b".into()]);
    let mut ra = RepoState::new("2026-10-14T18:00:00Z");
    ra.last_ok = "2026-10-14T18:30:00Z".into();
    st.repo.insert("a/a".into(), ra);
    let h = st.health_mut("a/a");
    (h.failures, h.error_open, h.last_code) = (2, true, "network".into());
    let h = st.health_mut("b/b");
    (h.failures, h.error_open, h.last_code) = (1, false, "forbidden".into());
    save_state(&dir, &st).unwrap();
    let mut j = Journal::open(&dir).unwrap();
    append_n(&mut j, 3);
    let beat = stw::github::parse_utc("2026-10-14T18:40:00Z").unwrap();
    write_heartbeat(&dir, beat, 77, "stream").unwrap();
    let lock = acquire(&dir).unwrap();
    let s = read_status(&dir, beat + 1799).unwrap();
    assert!(s.fresh, "3 x 600 s = 1800 s");
    assert!(!read_status(&dir, beat + 1800).unwrap().fresh);
    assert_eq!(
        (s.seq, s.interval_seconds, s.lock_held, s.lock_pid),
        (3, 600, true, Some(std::process::id()))
    );
    let hb = s.heartbeat.clone().unwrap();
    assert_eq!(
        (hb.time.as_str(), hb.age_seconds, hb.pid),
        ("2026-10-14T18:40:00Z", 1799, 77)
    );
    assert_eq!(s.repos.len(), 2);
    assert_eq!(
        (
            s.repos[0].repo.as_str(),
            s.repos[0].last_ok.as_deref(),
            s.repos[0].age_seconds,
            s.repos[0].failures,
            s.repos[0].open_error.as_deref()
        ),
        (
            "a/a",
            Some("2026-10-14T18:30:00Z"),
            Some(600 + 1799),
            2,
            Some("network")
        )
    );
    assert_eq!(
        (
            s.repos[1].last_ok.as_deref(),
            s.repos[1].age_seconds,
            s.repos[1].failures,
            s.repos[1].open_error.as_deref()
        ),
        (None, None, 1, None),
        "no last read; the error is closed"
    );
    let json: serde_json::Value = serde_json::to_value(&s).unwrap();
    assert_eq!(
        json.as_object()
            .unwrap()
            .keys()
            .cloned()
            .collect::<Vec<_>>(),
        [
            "schema_version",
            "state_dir",
            "heartbeat",
            "fresh",
            "interval_seconds",
            "seq",
            "lock_held",
            "repos"
        ]
    );
    drop(lock);
    assert!(!read_status(&dir, beat).unwrap().lock_held);
}

#[test]
fn a_missing_directory_is_not_fresh_and_not_an_error_while_a_corrupt_file_or_a_relative_path_is() {
    let t = tmp();
    let s = read_status(&t.path().join("nothing"), 0).unwrap();
    assert!(!s.fresh && s.heartbeat.is_none() && s.repos.is_empty() && !s.lock_held);
    assert_eq!((s.seq, s.interval_seconds), (0, 60));
    assert_eq!(
        read_status(Path::new("rel"), 0).unwrap_err(),
        WatchError::StateDirUsage
    );
    std::fs::write(t.path().join("state.json"), "{").unwrap();
    assert!(matches!(
        read_status(t.path(), 0),
        Err(WatchError::CorruptState(_))
    ));
    std::fs::remove_file(t.path().join("state.json")).unwrap();
    std::fs::write(t.path().join("journal.jsonl"), "garbage\n").unwrap();
    assert!(matches!(
        read_status(t.path(), 0),
        Err(WatchError::CorruptState(_))
    ));
}

// ---------------------------------------------------------------------------------------------------------------------
// The session: start with replay, commit order, crash points.
use stw::watch::{Core, Output, Step};

/// Records what is printed (by seq) and the durable steps, and "crashes" (returns an error) after the chosen step.
#[derive(Default)]
struct Rec {
    printed: Vec<u64>,
    steps: Vec<Step>,
    crash_after: Option<Step>,
}

impl Output for Rec {
    fn print(&mut self, ev: &Event) -> Result<(), WatchError> {
        self.printed.push(ev.seq);
        Ok(())
    }
    fn after(&mut self, step: Step) -> Result<(), WatchError> {
        self.steps.push(step);
        if self.crash_after == Some(step) {
            return Err(WatchError::Io("crash".into()));
        }
        Ok(())
    }
}

fn open(dir: &Path, baseline: bool) -> (Core, stw::watch::OpenReport, Rec) {
    let mut rec = Rec::default();
    let (c, r) = Core::open(dir, baseline, &mut rec).unwrap();
    (c, r, rec)
}

fn new_pr_event(n: u64) -> Event {
    let id = stw::watch::event::event_id(Some("o/r"), Some(n), Kind::NewPr, "g0");
    Event::new(
        id,
        "2026-10-22T09:00:00Z".into(),
        Some("o/r".into()),
        Some(n),
        Details::NewPr(stw::watch::event::NewPr {
            head: format!("{n:040}"),
            base: "main".into(),
            ..Default::default()
        }),
    )
}

#[test]
fn a_cycle_commits_journal_then_apply_then_print_then_state() {
    let t = tmp();
    let (mut core, report, _) = open(&t.path().join("d"), false);
    assert_eq!(report, stw::watch::OpenReport::default());
    let mut rec = Rec::default();
    let written = core
        .commit(&[new_pr_event(1), new_pr_event(2)], &mut rec)
        .unwrap();
    assert_eq!(written.iter().map(|e| e.seq).collect::<Vec<_>>(), [1, 2]);
    assert_eq!(
        rec.steps,
        [
            Step::Append(1),
            Step::Append(2),
            Step::Print(1),
            Step::Print(2),
            Step::State
        ],
        "all appends before the first print, the state last"
    );
    assert_eq!(rec.printed, [1, 2]);
    assert_eq!(
        (core.state.applied_seq, core.state.printed_seq, core.tail()),
        (2, 2, 2)
    );
    assert!(core.state.repo["o/r"].prs.contains_key(&2), "applied");
    let on_disk = load_state(&t.path().join("d")).unwrap().unwrap();
    assert_eq!(on_disk, core.state);
    // an empty cycle still writes the state and prints nothing
    let mut rec = Rec::default();
    core.commit(&[], &mut rec).unwrap();
    assert_eq!(
        (rec.steps.clone(), rec.printed.clone()),
        (vec![Step::State], vec![])
    );
}

#[test]
fn an_event_that_the_cycle_applied_already_is_not_applied_twice() {
    let t = tmp();
    let (mut core, _, _) = open(&t.path().join("d"), false);
    let e = new_pr_event(1);
    core.state.apply(&e);
    let before = core.state.clone();
    core.commit(&[e], &mut Rec::default()).unwrap();
    let mut after = core.state.clone();
    after.applied_seq = 0;
    after.printed_seq = 0;
    assert_eq!(after, before);
}

#[test]
fn a_second_open_of_the_same_directory_is_locked_out_until_the_first_is_closed() {
    let t = tmp();
    let dir = t.path().join("d");
    let (core, _, _) = open(&dir, false);
    let err = Core::open(&dir, false, &mut Rec::default()).err().unwrap();
    assert!(matches!(err, WatchError::Locked(Some(_))), "{err}");
    core.close();
    open(&dir, false);
}

/// Dies after `step` of one commit of `n` events and returns the directory, as a restart finds it.
fn crashed(t: &tempfile::TempDir, n: u64, step: Step) -> std::path::PathBuf {
    let dir = t.path().join("d");
    let (mut core, _, _) = open(&dir, false);
    core.commit(&[new_pr_event(100)], &mut Rec::default())
        .unwrap(); // seq 1 is complete
    let events: Vec<Event> = (1..=n).map(new_pr_event).collect();
    let mut rec = Rec {
        crash_after: Some(step),
        ..Rec::default()
    };
    assert!(core.commit(&events, &mut rec).is_err());
    assert!(
        core.commit(&events, &mut Rec::default()).is_err(),
        "a failed session stays unusable"
    );
    drop(core);
    dir
}

#[test]
fn a_crash_after_the_append_replays_and_prints_the_event_once_more_and_never_journals_it_twice() {
    // `crashed` journals seq 1 (PR 100) and then dies after `step` of a commit of PR 1 and PR 2 (seq 2 and 3).
    for (step, tail, printed) in [
        (Step::Append(1), 2u64, vec![2u64]),
        (Step::Append(2), 3, vec![2, 3]),
        (Step::Print(1), 3, vec![2, 3]),
        (Step::Print(2), 3, vec![2, 3]),
        (Step::State, 3, vec![]),
    ] {
        let t = tmp();
        let dir = crashed(&t, 2, step);
        let (core, report, rec) = open(&dir, false);
        assert_eq!(
            core.tail(),
            tail,
            "{step:?}: every event is in the journal exactly once"
        );
        assert_eq!(
            rec.printed, printed,
            "{step:?}: the lines past printed_seq are printed again"
        );
        assert_eq!(
            (report.replayed, report.reprinted as usize),
            (
                if step == Step::State { 0 } else { tail - 1 },
                printed.len()
            ),
            "{step:?}"
        );
        assert_eq!(
            (core.state.applied_seq, core.state.printed_seq),
            (tail, tail),
            "{step:?}"
        );
        let known: Vec<u64> = core.state.repo["o/r"].prs.keys().copied().collect();
        assert_eq!(
            known,
            if tail == 3 {
                vec![1, 2, 100]
            } else {
                vec![1, 100]
            },
            "{step:?}: the events are applied"
        );
    }
}

#[test]
fn the_state_of_a_restart_equals_the_state_of_a_run_without_a_crash() {
    let events: Vec<Event> = (1..=3).map(new_pr_event).collect();
    let clean = {
        let t = tmp();
        let (mut core, _, _) = open(&t.path().join("d"), false);
        core.commit(&events, &mut Rec::default()).unwrap();
        core.state.clone()
    };
    for step in [Step::Append(3), Step::Print(1), Step::Print(3), Step::State] {
        let t = tmp();
        let dir = t.path().join("d");
        let (mut core, _, _) = open(&dir, false);
        let mut rec = Rec {
            crash_after: Some(step),
            ..Rec::default()
        };
        assert!(core.commit(&events, &mut rec).is_err());
        drop(core);
        let (core, _, _) = open(&dir, false);
        assert_eq!(core.state, clean, "{step:?}");
    }
}

#[test]
fn a_crash_during_the_replay_is_repeated_safely() {
    let t = tmp();
    let dir = crashed(&t, 2, Step::Print(1));
    let mut rec = Rec {
        crash_after: Some(Step::ReplayPrint(1)),
        ..Rec::default()
    };
    assert!(Core::open(&dir, false, &mut rec).is_err());
    assert_eq!(rec.steps, [Step::ReplayState, Step::ReplayPrint(1)]);
    let (core, _, rec) = open(&dir, false);
    assert_eq!(
        rec.printed,
        [2, 3],
        "the lines are printed again until a start finishes the replay"
    );
    assert_eq!(core.state.printed_seq, 3);
    let (_, report, rec) = {
        core.close();
        open(&dir, false)
    };
    assert_eq!(
        (report.replayed, report.reprinted, rec.printed.len()),
        (0, 0, 0),
        "a finished replay is not repeated"
    );
}

#[test]
fn a_lost_state_is_rebuilt_from_the_journal_silently_and_forces_a_baseline() {
    let t = tmp();
    let dir = t.path().join("d");
    let (mut core, _, _) = open(&dir, false);
    core.state.repos = Some(vec!["o/r".into()]);
    core.commit(&[new_pr_event(1), new_pr_event(2)], &mut Rec::default())
        .unwrap();
    core.close();
    std::fs::remove_file(dir.join(STATE_NAME)).unwrap();
    let (core, report, rec) = open(&dir, false);
    assert_eq!(
        (report.lost, report.replayed, report.reprinted),
        (true, 2, 0)
    );
    assert!(rec.printed.is_empty(), "no reprint");
    assert!(core.baseline);
    assert_eq!((core.state.applied_seq, core.state.printed_seq), (2, 2));
    assert_eq!(
        core.state.repos, None,
        "the set is learned again by the first cycle"
    );
    assert_eq!(core.state.repo["o/r"].prs.len(), 2, "the counters are back");
    assert!(core.state.skipped.is_empty());
}

#[test]
fn a_corrupt_state_is_an_error_unless_the_baseline_is_forced() {
    let t = tmp();
    let dir = t.path().join("d");
    let (mut core, _, _) = open(&dir, false);
    core.commit(&[new_pr_event(1)], &mut Rec::default())
        .unwrap();
    core.close();
    std::fs::write(dir.join(STATE_NAME), "{broken").unwrap();
    let err = Core::open(&dir, false, &mut Rec::default()).err().unwrap();
    assert!(matches!(err, WatchError::CorruptState(_)), "{err}");
    assert!(
        std::fs::read_to_string(dir.join(STATE_NAME))
            .unwrap()
            .starts_with("{broken"),
        "not replaced silently"
    );
    let (core, report, _) = open(&dir, true);
    assert!(report.lost && core.baseline);
    assert!(load_state(&dir).unwrap().is_some(), "repaired");
}

#[test]
fn a_state_ahead_of_the_journal_is_an_error_unless_the_baseline_is_forced() {
    let t = tmp();
    let dir = t.path().join("d");
    let (mut core, _, _) = open(&dir, false);
    core.commit(&[new_pr_event(1), new_pr_event(2)], &mut Rec::default())
        .unwrap();
    core.close();
    // the journal loses its last line (a restored backup of one file)
    let j = std::fs::read_to_string(dir.join(JOURNAL_NAME)).unwrap();
    std::fs::write(
        dir.join(JOURNAL_NAME),
        format!("{}\n", j.lines().next().unwrap()),
    )
    .unwrap();
    let err = Core::open(&dir, false, &mut Rec::default()).err().unwrap();
    assert!(matches!(err, WatchError::CorruptState(_)), "{err}");
    let (core, report, _) = open(&dir, true);
    assert!(report.lost);
    assert_eq!(
        (core.tail(), core.state.applied_seq, core.state.printed_seq),
        (1, 1, 1)
    );
}

#[test]
fn a_truncated_journal_tail_is_reported_by_the_start_and_cut_by_the_next_commit() {
    let t = tmp();
    let dir = t.path().join("d");
    let (mut core, _, _) = open(&dir, false);
    core.commit(&[new_pr_event(1)], &mut Rec::default())
        .unwrap();
    core.close();
    let mut f = std::fs::OpenOptions::new()
        .append(true)
        .open(dir.join(JOURNAL_NAME))
        .unwrap();
    f.write_all(b"{\"schema_version\":1,\"se").unwrap();
    drop(f);
    let (mut core, report, _) = open(&dir, false);
    assert_eq!(report.truncated_tail_bytes, 23);
    core.commit(&[new_pr_event(2)], &mut Rec::default())
        .unwrap();
    assert_eq!(core.tail(), 2);
    scan(&dir.join(JOURNAL_NAME), &mut |_| {}).unwrap();
}

#[test]
fn the_heartbeat_names_the_process_and_the_mode() {
    let t = tmp();
    let dir = t.path().join("d");
    let (core, _, _) = open(&dir, false);
    core.heartbeat(1_792_003_723, "once").unwrap();
    let hb = read_heartbeat(&dir, 1_792_003_723).unwrap().unwrap();
    assert_eq!((hb.pid, hb.mode.as_str()), (std::process::id(), "once"));
}

// ---------------------------------------------------------------------------------------------------------------------
// The scenario corpus (`tests/fixtures/watch/`, format in FORMAT.md): what the core can play of it.
//
// vectors:       event_id, id_counter, event_json (above), workspace_id, default_state_dir, status_freshness_seconds,
//                merged_due_head_fork.
// watch-cycles:  (1) the events of every scenario as an `apply` sequence, compared with `expected_state_view` on the properties
//                that follow from events alone (see `compare_view`); (2) the same events through the session (journal, apply,
//                print, state) with the crash points, restarts and file operations of the scenario, compared with the
//                printed ordinals and the journal ordinals; (3) the liveness report of the scenarios that probe it.
// Not played here (they need the cycle, W3): the debounce, the backoff times, the check classification, the shape, the
// restacker choice and the observer questions; `state_after` and `hints` of the steps are cycle outputs.
use std::collections::BTreeMap;

fn watch_cycle_scenarios() -> Vec<(String, Value)> {
    let mut out = Vec::new();
    for e in std::fs::read_dir(fixtures()).unwrap() {
        let p = e.unwrap().path();
        if p.extension().is_none_or(|x| x != "json") || p.file_stem().unwrap() == "index" {
            continue;
        }
        let v: Value = serde_json::from_str(&std::fs::read_to_string(&p).unwrap()).unwrap();
        if v["kind"] == "watch-cycles" {
            out.push((v["name"].as_str().unwrap().to_string(), v));
        }
    }
    out.sort_by(|a, b| a.0.cmp(&b.0));
    out
}

fn str_of(v: &Value) -> String {
    v.as_str().unwrap_or_default().to_string()
}

/// The events of a scenario in order, each with its step (0-based index of `cycles`). The id is the Go id: the Rust ids have the
/// same form, which `every_event_id_of_the_corpus_is_rebuilt_by_the_key_functions` proves.
fn log_of(sc: &Value) -> Vec<(usize, Event)> {
    let mut out = Vec::new();
    for (i, step) in sc["expected_events"].as_array().unwrap().iter().enumerate() {
        for e in step["events"].as_array().unwrap() {
            let kind = Kind::parse(e["kind"].as_str().unwrap()).unwrap();
            let details = Details::decode(kind, &e["details"].to_string()).unwrap();
            let mut ev = Event::new(
                e["go"]["id"].as_str().unwrap().to_string(),
                str_of(&e["time"]),
                e["repo"].as_str().map(str::to_string),
                e["pr"].as_u64(),
                details,
            );
            ev.seq = e["ordinal"].as_u64().unwrap();
            out.push((i, ev));
        }
    }
    out
}

/// The key an event of the corpus should have, built from its details and the counter in its id.
fn key_of(ev: &Event) -> String {
    let id = &ev.id;
    let c = |l: char| id_counter(id, l).unwrap_or_else(|| panic!("no counter {l} in {id}"));
    let parts: Vec<&str> = id.split(':').collect();
    match &ev.details {
        Details::NewPr(_) | Details::Closed(_) => key_generation(c('g')),
        Details::HeadChanged(d) => key_head(&d.new, c('h')),
        Details::ChecksGreen(d) => key_checks(&d.head, c('c')),
        Details::ChecksRed(d) => key_checks(&d.head, c('c')),
        Details::ChecksPendingTimeout(d) => key_timeout(&d.head, c('c')),
        Details::MergeableChanged(_) => key_mergeable(parts[parts.len() - 2], c('m')),
        Details::ReadyStateChanged(_) => key_ready(c('r')),
        Details::BaseChanged(_) => key_base(c('b')),
        Details::ReposChanged(_) => key_repos(c('p')),
        Details::Merged(d) => key_merged(d.merge_commit.as_deref()),
        Details::RestackNeeded(d) => key_restack(d.merged_pr, d.new_parent.as_deref()),
        Details::WatchError(d) => key_error(&d.code, c('e'), d.failures),
        Details::WatchRecovered(_) => {
            if parts.last().is_some_and(|p| p.starts_with('e')) {
                key_recovered_error(c('e'))
            } else {
                key_stall(c('s'))
            }
        }
        Details::WatchStalled(_) => key_stall(c('s')),
    }
}

#[test]
fn every_event_id_of_the_corpus_is_rebuilt_by_the_key_functions_and_unique_per_scenario() {
    let (mut events, mut per_kind) = (0, BTreeMap::new());
    for (name, sc) in watch_cycle_scenarios() {
        let mut seen = std::collections::BTreeSet::new();
        for (_, ev) in log_of(&sc) {
            assert_eq!(
                event_id(ev.repo.as_deref(), ev.pr, ev.kind(), &key_of(&ev)),
                ev.id,
                "{name}"
            );
            if sc["expect_unique_event_ids"] == true {
                assert!(seen.insert(ev.id.clone()), "{name}: {} twice", ev.id);
            }
            events += 1;
            *per_kind.entry(ev.kind()).or_insert(0) += 1;
        }
    }
    assert_eq!(per_kind.len(), 15, "every kind occurs in the corpus");
    assert!(events > 700, "{events}");
}

#[test]
fn the_workspace_id_groups_of_the_vector_stay_together_and_apart() {
    // The Go ids are format-dependent; which inputs share an id is not.
    let cs = cases("workspace-id");
    let mut by_go: BTreeMap<String, Vec<String>> = BTreeMap::new();
    for c in &cs {
        let i = &c["input"];
        let repos: Vec<String> = i["repos"].as_array().unwrap().iter().map(str_of).collect();
        let root = str_of(&i["workspace_root"]);
        let src = if root.is_empty() {
            IdSource::Repos(&repos)
        } else {
            IdSource::Workspace(&root)
        };
        by_go
            .entry(str_of(&c["expected"]["id"]))
            .or_default()
            .push(workspace_id(i["host"].as_str().unwrap(), src));
    }
    assert_eq!(by_go.len(), 4);
    let mut rust_ids = std::collections::BTreeSet::new();
    for ids in by_go.values() {
        assert!(ids.iter().all(|i| i == &ids[0]), "{ids:?}");
        rust_ids.insert(ids[0].clone());
    }
    assert_eq!(rust_ids.len(), 4, "different inputs, different ids");
    assert_eq!(by_go.values().map(Vec::len).max(), Some(3));
}

#[test]
fn the_default_state_dir_and_the_freshness_window_follow_their_vectors() {
    for c in cases("default-state-dir") {
        let i = &c["input"];
        let xdg = i["xdg_state_home"].as_str();
        let home = i["home"].as_str();
        let got = default_state_dir(i["id"].as_str().unwrap(), xdg, home);
        assert_eq!(
            got.to_str().unwrap(),
            c["expected"]["path"].as_str().unwrap(),
            "{}",
            c["name"]
        );
    }
    for c in cases("status-freshness-window") {
        let got = status_freshness_seconds(c["input"]["interval_seconds"].as_u64().unwrap());
        assert_eq!(
            got,
            c["expected"]["seconds"].as_u64().unwrap(),
            "{}",
            c["name"]
        );
    }
}

#[test]
fn a_merge_records_a_fork_head_unless_the_pr_was_seen_open_in_the_base_repository() {
    let cs = cases("merged-records-head-fork");
    assert!(cs.len() >= 3);
    for c in cs {
        let (fork, open) = (
            c["input"]["observed_fork"] == true,
            c["input"]["observed_open_pr"] == true,
        );
        let mut s = State::new();
        if open {
            let mut e = new_pr(5, H1);
            if let Details::NewPr(d) = &mut e.details {
                d.fork = fork;
            }
            s.apply(&e);
        }
        s.apply(&ev(
            Some(5),
            &key_merged(None),
            Details::Merged(Merged {
                merge_commit: None,
                ..Merged::default()
            }),
        ));
        assert_eq!(
            s.repo[REPO].restack_due[0].head_fork,
            c["expected"]["head_fork"] == true,
            "{}",
            c["name"]
        );
    }
}

// --- the cycle scenarios as apply sequences

fn ignored(o: &Value, sc: &Value) -> bool {
    let cfg = &sc["config"];
    let has = |key: &str, v: &Value| cfg[key].as_array().unwrap().contains(v);
    has("ignore_authors", &o["author"])
        || o["labels"]
            .as_array()
            .unwrap()
            .iter()
            .any(|l| has("ignore_labels", l))
}

/// What the baseline of the cycle does with a successful first read of a repository, minus the check class (W3): the PRs of
/// the observation replace the accepted ones, counters and generations carry over, vanished PRs are remembered.
fn seed_baseline(rs: &mut RepoState, prs: &[Value], sc: &Value) {
    let mut fresh = BTreeMap::new();
    let mut sorted: Vec<&Value> = prs.iter().collect();
    sorted.sort_by_key(|o| o["number"].as_u64().unwrap());
    for o in sorted {
        if ignored(o, sc) {
            continue;
        }
        let n = o["number"].as_u64().unwrap();
        let mut p = PrState {
            number: n,
            title: str_of(&o["title"]),
            author: str_of(&o["author"]),
            labels: o["labels"].as_array().unwrap().iter().map(str_of).collect(),
            draft: o["draft"] == true,
            head: str_of(&o["head"]),
            head_ref: str_of(&o["head_ref"]),
            base: str_of(&o["base"]),
            fork: o["fork"] == true,
            detail_due: o["detail"].is_null(),
            ..PrState::default()
        };
        if let Some(old) = rs.prs.get(&n) {
            p.epochs = old.epochs;
            p.generation = old.generation;
            if o["fork_known"] != true {
                p.fork = old.fork;
            }
        } else if let Some((_, rec)) = rs.closed_record(n) {
            p.epochs = rec.epochs;
            p.generation = rec.generation + 1;
        }
        fresh.insert(n, p);
    }
    let vanished: Vec<ClosedPr> = rs
        .prs
        .iter()
        .filter(|(n, _)| !fresh.contains_key(n))
        .map(|(n, old)| ClosedPr {
            number: *n,
            generation: old.generation,
            epochs: old.epochs,
        })
        .collect();
    for c in vanished {
        rs.record_closed(c);
    }
    rs.prs = fresh;
    rs.baselined = true;
}

fn config_repos(v: &Value) -> Vec<String> {
    v.as_array().unwrap().iter().map(str_of).collect()
}

/// The state after the events of the scenario, applied step by step. The steps add what the cycle does around the events that
/// is not an event and that the comparison needs: the repository set of the config, the silent baseline of a repository
/// polled for the first time (or again after a removal, a forced baseline or a lost state).
fn replay(sc: &Value) -> State {
    let log = log_of(sc);
    let mut state = State::new();
    let mut set = config_repos(&sc["config"]["repos"]);
    let mut need_sync = false;
    state.repos = Some(set.clone());
    for r in &set {
        state.repo.insert(
            r.clone(),
            RepoState::new(&str_of(&sc["config"]["start_time"])),
        );
    }
    for (i, step) in sc["cycles"].as_array().unwrap().iter().enumerate() {
        // `hints` name the repositories polled in the step; a step without hints (the crash scenarios) polled every repository
        // it has an observation for.
        let hints = &sc["expected_events"][i]["hints"];
        let polled: Vec<String> = match hints.as_object() {
            Some(h) => h.keys().cloned().collect(),
            None => step["observation"]
                .as_object()
                .map(|o| o.keys().cloned().collect())
                .unwrap_or_default(),
        };
        let change = &step["config_change"];
        if let Some(r) = change.get("repos").filter(|r| !r.is_null()) {
            set = config_repos(r);
        }
        let forced = change["baseline"] == true;
        let lost = matches!(
            step["file_op"].as_str(),
            Some("delete-state" | "corrupt-state")
        );
        if forced || lost {
            for rs in state.repo.values_mut() {
                rs.baselined = false;
                rs.restack_due.clear();
                rs.restack_done.clear();
            }
        }
        if lost {
            // the state is rebuilt from the journal alone; the set of repositories is learned again by the next cycle
            let mut rebuilt = State::new();
            for (_, e) in log.iter().filter(|(s, _)| *s < i) {
                rebuilt.apply(e);
            }
            state = rebuilt;
            need_sync = true;
        }
        if need_sync && step["action"] == "cycle" {
            state.repos = Some(set.clone());
            for r in &set {
                state
                    .repo
                    .entry(r.clone())
                    .or_insert_with(|| RepoState::new(&str_of(&step["time"])));
            }
            need_sync = false;
        }
        // what the cycle does to the state besides the events is lost when the process dies before the state is written
        let persists = step["crash"].is_null() || step["crash"]["point"] == "state";
        let mut events: Vec<&Event> = log
            .iter()
            .filter(|(s, _)| *s == i)
            .map(|(_, e)| e)
            .collect();
        let lead = events
            .iter()
            .take_while(|e| e.kind() == Kind::ReposChanged)
            .count();
        for e in events.drain(..lead) {
            check_apply(&mut state, e);
        }
        for repo in polled.iter().filter(|_| persists) {
            let obs = &step["observation"][repo];
            if obs["error"].is_null() && state.repos.iter().flatten().any(|r| r == repo) {
                let rs = state
                    .repo
                    .entry(repo.clone())
                    .or_insert_with(|| RepoState::new(""));
                if !rs.baselined {
                    seed_baseline(rs, obs["prs"].as_array().unwrap(), sc);
                } else {
                    // a PR that became ignored vanishes silently
                    for o in obs["prs"]
                        .as_array()
                        .unwrap()
                        .iter()
                        .filter(|o| ignored(o, sc))
                    {
                        let n = o["number"].as_u64().unwrap();
                        if let Some(old) = rs.prs.remove(&n) {
                            rs.record_closed(ClosedPr {
                                number: n,
                                generation: old.generation,
                                epochs: old.epochs,
                            });
                        }
                    }
                }
            }
        }
        for e in events {
            check_apply(&mut state, e);
        }
    }
    state
}

/// Applies an event, and checks that applying it again changes nothing and the effect on the restack work the table of the spec
/// names for merges and restack events.
fn check_apply(state: &mut State, e: &Event) {
    state.apply(e);
    let once = state.clone();
    state.apply(e);
    assert_eq!(*state, once, "applying {} twice", e.id);
    if let (Details::Merged(d), Some(repo), Some(n)) = (&e.details, e.repo.as_deref(), e.pr) {
        let rs = &state.repo[repo];
        assert!(!rs.prs.contains_key(&n), "{}", e.id);
        let due = rs
            .restack_due
            .iter()
            .find(|x| {
                x.merged_pr == n
                    && key_merged(x.merge_commit.as_deref())
                        == key_merged(d.merge_commit.as_deref())
            })
            .unwrap_or_else(|| panic!("no restack work after {}", e.id));
        assert_eq!(
            (&due.old_parent, &due.base, &due.head_ref),
            (&d.head, &d.base, &d.head_ref),
            "{}",
            e.id
        );
    }
    if let (Details::RestackNeeded(_), Some(repo)) = (&e.details, e.repo.as_deref()) {
        assert!(state.repo[repo].restack_done.contains(&e.id), "{}", e.id);
    }
}

fn epochs_of(v: &Value) -> Epochs {
    let n = |k: &str| v[k].as_u64().unwrap();
    Epochs {
        head: n("head"),
        checks: n("checks"),
        mergeable: n("mergeable"),
        ready: n("ready"),
        base: n("base"),
    }
}

/// Compares the replayed state with `expected_state_view` on what follows from the events and the baseline alone:
///
/// - `repos`, `skipped`, `repos_epoch`;
/// - per PR in the view: it exists in the replay and the replay has no other; `head`, `base`, `draft`, `generation` and the
///   epochs `head`, `mergeable`, `ready`, `base`; the epoch `checks` is at most the view's (a silent change of the class to
///   pending or none also raises it, and that is a cycle effect);
/// - closed records of the replay: the same generation and epochs (but `checks`) in the view;
/// - health: `episode`, `stall_episode`, `error_open`, `stalled`, `last_code` (while an error is open), `failures` at most the
///   view's (failures without an event are counted by the cycle).
///
/// Not compared (cycle effects, W3): `cycle`, `interval_seconds`, `last_ok`, `since`, `default_branch`, `class`,
/// `class_candidate`, `pending_*`, `timeout_fired`, the mergeable fields and candidate, `detail_due`, `gone`, `restack_due` and
/// `restack_done` (the cycle clears them after the restack), `next_try`, title, author, labels, head_ref, fork.
fn compare_view(name: &str, state: &State, view: &Value) -> Vec<String> {
    let mut bad = Vec::new();
    let mut check = |what: String, ok: bool| {
        if !ok {
            bad.push(format!("{name}: {what}"));
        }
    };
    check(
        "repos".into(),
        state.repos.clone().unwrap_or_default() == config_repos(&view["repos"]),
    );
    let skipped: Vec<(String, String)> = state
        .skipped
        .iter()
        .map(|s| (s.path.clone(), s.reason.clone()))
        .collect();
    let want: Vec<(String, String)> = view["skipped"]
        .as_array()
        .unwrap()
        .iter()
        .map(|s| (str_of(&s["path"]), str_of(&s["reason"])))
        .collect();
    check("skipped".into(), skipped == want);
    check(
        "repos_epoch".into(),
        state.repos_epoch == view["repos_epoch"].as_u64().unwrap(),
    );
    let health = |h: &stw::watch::state::Health, v: &Value, what: String, bad: &mut Vec<String>| {
        let ok = h.episode == v["episode"].as_u64().unwrap()
            && h.stall_episode == v["stall_episode"].as_u64().unwrap()
            && h.error_open == (v["error_open"] == true)
            && h.stalled == (v["stalled"] == true)
            && (!h.error_open || h.last_code == str_of(&v["last_code"]))
            && h.failures <= v["failures"].as_u64().unwrap();
        if !ok {
            bad.push(format!("{name}: health of {what}: {h:?} against {v}"));
        }
    };
    let none = stw::watch::state::Health::default();
    health(
        state.health.get("").unwrap_or(&none),
        &view["run_health"],
        "the run".into(),
        &mut bad,
    );
    for (repo, rv) in view["repo"].as_object().unwrap() {
        health(
            state.health.get(repo).unwrap_or(&none),
            &rv["health"],
            repo.clone(),
            &mut bad,
        );
        let Some(rs) = state.repo.get(repo) else {
            if !rv["prs"].as_object().unwrap().is_empty() {
                bad.push(format!(
                    "{name}: {repo} has PRs in the view and no record in the replay"
                ));
            }
            continue;
        };
        let vp = rv["prs"].as_object().unwrap();
        let mine: Vec<String> = rs.prs.keys().map(u64::to_string).collect();
        let theirs: Vec<String> = vp.keys().cloned().collect();
        if mine != theirs {
            bad.push(format!("{name}: {repo} PRs {mine:?} against {theirs:?}"));
            continue;
        }
        for (n, p) in vp {
            let m = &rs.prs[&n.parse().unwrap()];
            let e = epochs_of(&p["epochs"]);
            let ok = m.head == str_of(&p["head"])
                && m.base == str_of(&p["base"])
                && m.draft == (p["draft"] == true)
                && m.generation == p["generation"].as_u64().unwrap()
                && (
                    m.epochs.head,
                    m.epochs.mergeable,
                    m.epochs.ready,
                    m.epochs.base,
                ) == (e.head, e.mergeable, e.ready, e.base)
                && m.epochs.checks <= e.checks;
            if !ok {
                bad.push(format!("{name}: {repo}#{n}: {m:?} against {p}"));
            }
        }
        for c in &rs.closed {
            let found = rv["closed"]
                .as_array()
                .unwrap()
                .iter()
                .find(|v| v["number"].as_u64() == Some(c.number));
            let ok = found.is_some_and(|v| {
                let e = epochs_of(&v["epochs"]);
                v["generation"].as_u64() == Some(c.generation)
                    && (
                        c.epochs.head,
                        c.epochs.mergeable,
                        c.epochs.ready,
                        c.epochs.base,
                    ) == (e.head, e.mergeable, e.ready, e.base)
                    && c.epochs.checks <= e.checks
            });
            if !ok {
                bad.push(format!("{name}: {repo} closed {c:?} against {:?}", found));
            }
        }
    }
    bad
}

#[test]
fn the_events_of_every_cycle_scenario_replayed_through_apply_reach_the_expected_state() {
    let all = watch_cycle_scenarios();
    assert_eq!(all.len(), 194);
    let mut bad = Vec::new();
    for (name, sc) in &all {
        let state = replay(sc);
        bad.extend(compare_view(name, &state, &sc["expected_state_view"]));
    }
    assert!(
        bad.is_empty(),
        "{} mismatches, first: {:#?}",
        bad.len(),
        &bad[..bad.len().min(8)]
    );
}

// --- the same events through the session: crash points, restarts, file operations

fn crash_step(c: &Value) -> Option<Step> {
    if c.is_null() {
        return None;
    }
    let n = c["n"].as_u64().unwrap() as usize;
    Some(match c["point"].as_str().unwrap() {
        "append" => Step::Append(n),
        "print" => Step::Print(n),
        "state" => Step::State,
        other => panic!("unknown crash point {other}"),
    })
}

/// Plays the steps of a scenario against a state directory with the commit order of the core. Returns the journal as it ends.
fn run_session(name: &str, sc: &Value, dir: &Path) -> Vec<Event> {
    let log = log_of(sc);
    let mut core: Option<Core> = None;
    let mut corrupted = false;
    for (i, step) in sc["cycles"].as_array().unwrap().iter().enumerate() {
        let exp = &sc["expected_events"][i];
        let file_op = str_of(&step["file_op"]);
        let reopen = core.is_none()
            || step["restart_before"] == true
            || step["action"] == "open"
            || !file_op.is_empty();
        let mut rec = Rec::default();
        if reopen {
            if let Some(c) = core.take() {
                c.close();
            }
            match file_op.as_str() {
                "delete-state" => std::fs::remove_file(dir.join(STATE_NAME)).unwrap(),
                "corrupt-state" => {
                    std::fs::write(dir.join(STATE_NAME), "{corrupt").unwrap();
                    corrupted = true;
                }
                _ => {}
            }
            let baseline = step["config_change"]["baseline"] == true;
            match Core::open(dir, baseline, &mut rec) {
                Ok((c, report)) => {
                    assert!(
                        matches!(exp["result"].as_str(), Some("ok" | "crashed")),
                        "{name} step {}",
                        i + 1
                    );
                    let want_lost = file_op == "delete-state" || (corrupted && baseline);
                    assert_eq!(report.lost, want_lost, "{name} step {}", i + 1);
                    corrupted = false;
                    core = Some(c);
                }
                Err(e) => {
                    assert!(matches!(e, WatchError::CorruptState(_)), "{name}: {e}");
                    assert_eq!(
                        (exp["result"].as_str(), exp["error_kind"].as_str()),
                        (Some("error"), Some("corrupt-state")),
                        "{name}"
                    );
                }
            }
        }
        if step["action"] == "cycle" {
            if let Some(c) = core.as_mut() {
                let events: Vec<Event> = log
                    .iter()
                    .filter(|(s, _)| *s == i)
                    .map(|(_, e)| e.clone())
                    .collect();
                rec.crash_after = crash_step(&step["crash"]);
                let out = c.commit(&events, &mut rec);
                if exp["result"] == "crashed" {
                    assert!(
                        out.is_err(),
                        "{name} step {}: the crash point was not reached",
                        i + 1
                    );
                    core = None;
                } else {
                    let written = out.unwrap_or_else(|e| panic!("{name} step {}: {e}", i + 1));
                    for (w, (_, e)) in written.iter().zip(log.iter().filter(|(s, _)| *s == i)) {
                        assert_eq!((w.seq, &w.id), (e.seq, &e.id), "{name}");
                    }
                    c.heartbeat(0, "once").unwrap();
                }
            }
        }
        let want: Vec<u64> = exp["printed"]
            .as_array()
            .unwrap()
            .iter()
            .map(|v| v.as_u64().unwrap())
            .collect();
        assert_eq!(rec.printed, want, "{name} step {}: printed ordinals", i + 1);
    }
    if let Some(c) = core.take() {
        c.close();
    }
    let mut journal = Vec::new();
    scan(&dir.join(JOURNAL_NAME), &mut |e| journal.push(e.clone())).unwrap();
    journal
}

#[test]
fn the_session_reproduces_the_printed_ordinals_and_the_journal_of_every_cycle_scenario() {
    let all = watch_cycle_scenarios();
    let (mut crashed, mut restarts, mut steps) = (0, 0, 0);
    for (name, sc) in &all {
        let t = tmp();
        let dir = t.path().join("state");
        let journal = run_session(name, sc, &dir);
        let want: Vec<u64> = sc["expected_journal_ordinals"]
            .as_array()
            .unwrap()
            .iter()
            .map(|v| v.as_u64().unwrap())
            .collect();
        assert_eq!(
            journal.iter().map(|e| e.seq).collect::<Vec<_>>(),
            want,
            "{name}: journal ordinals"
        );
        let log = log_of(sc);
        assert_eq!(journal.len(), log.len(), "{name}: every event exactly once");
        for (j, (_, e)) in journal.iter().zip(&log) {
            assert_eq!(
                (&j.id, j.kind(), &j.details),
                (&e.id, e.kind(), &e.details),
                "{name}"
            );
        }
        for s in sc["cycles"].as_array().unwrap() {
            steps += 1;
            crashed += usize::from(!s["crash"].is_null());
            restarts += usize::from(s["restart_before"] == true);
        }
    }
    assert!(
        crashed >= 100 && restarts > 50 && steps > 500,
        "{crashed} {restarts} {steps}"
    );
}

#[test]
fn a_crash_scenario_ends_with_the_state_of_its_crash_free_variant() {
    // `crash-<case>-none` is the run without a crash; the journal and the replayed state are the same for every variant.
    let all = watch_cycle_scenarios();
    let by_name: BTreeMap<&str, &Value> = all.iter().map(|(n, v)| (n.as_str(), v)).collect();
    let mut pairs = 0;
    for (name, sc) in &all {
        let Some(case) = name.strip_prefix("crash-") else {
            continue;
        };
        let Some((stem, _)) = case.rsplit_once('-') else {
            continue;
        };
        let Some(none) = by_name.get(format!("crash-{stem}-none").as_str()) else {
            continue;
        };
        if name.ends_with("-none") {
            continue;
        }
        pairs += 1;
        let (a, b) = (log_of(sc), log_of(none));
        // the same events, once each; the order inside a step may differ (a crash moves the re-derivation to the next cycle)
        let strip = |l: &[(usize, Event)]| {
            let mut v: Vec<String> = l
                .iter()
                .map(|(_, e)| format!("{:?}", (e.kind(), &e.repo, e.pr, &e.details)))
                .collect();
            v.sort();
            v
        };
        assert_eq!(
            strip(&a),
            strip(&b),
            "{name}: the journal holds the events of the crash-free run"
        );
        let (sa, sb) = (replay(sc), replay(none));
        // what a crashed cycle loses (its baseline, its read hints) is not compared; the counters, the set and the health are
        assert_eq!(
            (sa.repos_epoch, &sa.repos, &sa.health),
            (sb.repos_epoch, &sb.repos, &sb.health),
            "{name}: the replayed counters equal the crash-free run"
        );
    }
    assert!(pairs >= 50, "{pairs}");
}

// --- the liveness report

#[test]
fn the_status_probes_of_the_corpus_are_answered_from_the_directory_alone() {
    let mut probes = 0;
    for (name, sc) in watch_cycle_scenarios() {
        let list = sc["status_probes"].as_array().unwrap();
        if list.is_empty() {
            continue;
        }
        let t = tmp();
        let dir = t.path().join("state");
        ensure_state_dir(&dir).unwrap();
        let view = &sc["expected_state_view"];
        let mut s = State::new();
        s.interval_seconds = view["interval_seconds"].as_u64().unwrap();
        s.repos = Some(config_repos(&view["repos"]));
        for (repo, rv) in view["repo"].as_object().unwrap() {
            let mut rs = RepoState::new(&str_of(&rv["since"]));
            rs.last_ok = str_of(&rv["last_ok"]);
            s.repo.insert(repo.clone(), rs);
            let h = &rv["health"];
            let st = s.health_mut(repo);
            st.failures = h["failures"].as_u64().unwrap();
            st.error_open = h["error_open"] == true;
            st.last_code = str_of(&h["last_code"]);
        }
        save_state(&dir, &s).unwrap();
        let last = str_of(&sc["cycles"].as_array().unwrap().last().unwrap()["time"]);
        let beat = stw::github::parse_utc(&last).unwrap();
        write_heartbeat(&dir, beat, 4242, &str_of(&sc["config"]["mode"])).unwrap();
        for p in list {
            let now = beat + p["offset_seconds"].as_i64().unwrap();
            let got = read_status(&dir, now).unwrap();
            let e = &p["expected"];
            assert_eq!(
                got.fresh,
                e["fresh"] == true,
                "{name} +{}",
                p["offset_seconds"]
            );
            assert_eq!(
                got.heartbeat.as_ref().map(|h| h.age_seconds),
                e["heartbeat_age_seconds"].as_u64(),
                "{name}"
            );
            assert_eq!(
                got.interval_seconds,
                e["interval_seconds"].as_u64().unwrap(),
                "{name}"
            );
            assert_eq!(got.lock_held, e["lock_held"] == true, "{name}");
            let repos: Vec<(String, Option<u64>, u64, Option<String>)> = got
                .repos
                .iter()
                .map(|r| {
                    (
                        r.repo.clone(),
                        r.age_seconds,
                        r.failures,
                        r.open_error.clone(),
                    )
                })
                .collect();
            let want: Vec<(String, Option<u64>, u64, Option<String>)> = e["repos"]
                .as_array()
                .unwrap()
                .iter()
                .map(|r| {
                    (
                        str_of(&r["repo"]),
                        r["last_ok_age_seconds"].as_u64(),
                        r["failures"].as_u64().unwrap(),
                        r["open_error"].as_str().map(str::to_string),
                    )
                })
                .collect();
            assert_eq!(repos, want, "{name}");
            probes += 1;
        }
    }
    assert_eq!(probes, 3);
}
