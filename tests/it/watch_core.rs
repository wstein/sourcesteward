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
