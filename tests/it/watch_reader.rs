//! The reader of `stw watch` (port package W2): replay of the scenarios `vectors`, `http-reader` and `observer-session`
//! of the corpus `tests/fixtures/watch/` (FORMAT.md) against `src/watch_reader/`, plus the tests that have no Go
//! counterpart (conditional store layout, Actions fallback, token handling). Everything runs against fake servers or
//! fake readers on 127.0.0.1; there is no network access.
use std::path::PathBuf;

use serde_json::Value;
use stw::watch_reader::http::{self, Advice, RateInfo};

fn corpus() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/watch")
}

fn load(name: &str) -> Value {
    let p = corpus().join(format!("{name}.json"));
    serde_json::from_str(&std::fs::read_to_string(&p).unwrap_or_else(|e| panic!("{p:?}: {e}")))
        .unwrap()
}

/// Replays every case of a `vectors` file; returns the number of cases.
fn each_case(file: &str, function: &str, mut f: impl FnMut(&str, &Value, &Value)) -> usize {
    let v = load(file);
    assert_eq!(v["kind"], "vectors", "{file}");
    assert_eq!(v["function"], function, "{file}");
    let cases = v["cases"].as_array().unwrap();
    for c in cases {
        f(c["name"].as_str().unwrap(), &c["input"], &c["expected"]);
    }
    cases.len()
}

fn u(v: &Value) -> u64 {
    v.as_u64()
        .unwrap_or_else(|| panic!("not an unsigned number: {v}"))
}

fn rate_of(v: &Value) -> RateInfo {
    RateInfo {
        known: v["known"].as_bool().unwrap(),
        limit: u(&v["limit"]),
        remaining: u(&v["remaining"]),
        reset: v["reset_unix"].as_i64().unwrap(),
    }
}

#[test]
fn vector_assess_rate_limit() {
    let n = each_case("rate-limit-assess", "assess_rate_limit", |name, i, e| {
        let d = http::assess_rate_limit(&rate_of(i));
        let advice = match d.advice {
            Advice::None => "none",
            Advice::Slow => "slow",
            Advice::Wait => "wait",
        };
        assert_eq!(advice, e["advice"], "{name}");
        assert_eq!(d.until, e["until_unix"].as_i64().unwrap(), "{name}");
    });
    assert_eq!(n, 8);
}

#[test]
fn vector_parse_rate_headers() {
    let n = each_case("rate-limit-headers", "parse_rate_headers", |name, i, e| {
        let got = http::parse_rate_headers(
            i["limit"].as_str().unwrap(),
            i["remaining"].as_str().unwrap(),
            i["reset"].as_str().unwrap(),
        );
        assert_eq!(got, rate_of(e), "{name}");
    });
    assert_eq!(n, 3);
}

#[test]
fn vector_rate_limited_until() {
    let n = each_case(
        "rate-limited-wait-until",
        "rate_limited_until",
        |name, i, e| {
            let got = http::rate_limited_until(
                u(&i["retry_after_seconds"]),
                &rate_of(&i["rate_limit"]),
                i["now_unix"].as_i64().unwrap(),
            );
            assert_eq!(got, e["until_unix"].as_i64().unwrap(), "{name}");
        },
    );
    assert_eq!(n, 4);
}

#[test]
fn vector_parse_retry_after() {
    let n = each_case("retry-after-header", "parse_retry_after", |name, i, e| {
        let got = http::parse_retry_after(i["header"].as_str().unwrap()).unwrap_or(0);
        assert_eq!(got, u(&e["seconds"]), "{name}");
    });
    assert_eq!(n, 7);
}

#[test]
fn vector_next_page_from_link() {
    let n = each_case(
        "link-header-next-page",
        "next_page_from_link",
        |name, i, e| {
            let got = http::next_page_from_link(i["link"].as_str().unwrap());
            assert_eq!(got.is_some(), e["found"].as_bool().unwrap(), "{name}");
            assert_eq!(got.unwrap_or(0), u(&e["page"]), "{name}");
        },
    );
    assert_eq!(n, 10);
}

#[test]
fn vector_endpoint_allowlist() {
    let n = each_case(
        "watch-endpoint-allowlist",
        "watch_read_endpoint_allowed",
        |name, i, e| {
            assert_eq!(i["method"], "GET", "{name}: the corpus only has GET");
            let got = http::read_endpoint_allowed(i["endpoint"].as_str().unwrap());
            assert_eq!(got, e["allowed"].as_bool().unwrap(), "{name}");
        },
    );
    assert_eq!(n, 42);
}

#[test]
fn vector_encode_branch_segment() {
    let n = each_case(
        "branch-segment-encode",
        "encode_branch_segment",
        |name, i, e| {
            let got = http::encode_branch_segment(i["name"].as_str().unwrap());
            assert_eq!(got.is_some(), e["ok"].as_bool().unwrap(), "{name}");
            assert_eq!(
                got.unwrap_or_default(),
                e["segment"].as_str().unwrap(),
                "{name}"
            );
        },
    );
    assert_eq!(n, 35);
}

#[test]
fn vector_branch_segment_valid() {
    let n = each_case(
        "branch-segment-guard",
        "branch_segment_valid",
        |name, i, e| {
            let got = http::branch_segment_valid(i["segment"].as_str().unwrap());
            assert_eq!(got, e["valid"].as_bool().unwrap(), "{name}");
        },
    );
    assert_eq!(n, 25);
}

#[test]
fn vector_split_remote() {
    let n = each_case("watch-split-remote", "split_remote", |name, i, e| {
        let got = http::split_remote(i["remote"].as_str().unwrap());
        assert_eq!(got.is_some(), e["ok"].as_bool().unwrap(), "{name}");
        let (h, p) = got.unwrap_or_default();
        assert_eq!(h, e["host"].as_str().unwrap(), "{name}");
        assert_eq!(p, e["path"].as_str().unwrap(), "{name}");
    });
    assert_eq!(n, 11);
}

#[test]
fn vector_parse_remote() {
    let n = each_case("watch-parse-remote", "parse_remote", |name, i, e| {
        let got = http::parse_remote(i["remote"].as_str().unwrap(), i["host"].as_str().unwrap());
        assert_eq!(got.is_some(), e["ok"].as_bool().unwrap(), "{name}");
        assert_eq!(
            got.unwrap_or_default(),
            e["repo"].as_str().unwrap(),
            "{name}"
        );
    });
    assert_eq!(n, 21);
}

#[test]
fn vector_valid_host() {
    let n = each_case("watch-valid-host", "valid_host", |name, i, e| {
        let got = http::valid_host(i["host"].as_str().unwrap());
        assert_eq!(got, e["valid"].as_bool().unwrap(), "{name}");
    });
    assert_eq!(n, 11);
}
