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

/// The scenarios of one kind, by file name, in name order.
fn scenarios_of(kind: &str) -> Vec<(String, Value)> {
    let mut out = Vec::new();
    let mut names: Vec<String> = std::fs::read_dir(corpus())
        .unwrap()
        .filter_map(|e| {
            let n = e.unwrap().file_name().into_string().unwrap();
            n.strip_suffix(".json").map(str::to_string)
        })
        .filter(|n| n != "index")
        .collect();
    names.sort();
    for n in names {
        let v = load(&n);
        if v["kind"] == kind {
            out.push((n, v));
        }
    }
    out
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

// ---- the fake forge (a raw HTTP server on 127.0.0.1, port 0) ----------------------------------------------

use std::collections::{BTreeMap, HashMap};
use std::io::{Read as _, Write as _};
use std::net::TcpListener;
use std::sync::{Arc, Mutex};

use stw::watch_reader::http::{
    Code, PullListState, PullPage, WatchError, WatchHttp, WatchResponse,
};

const TOKEN: &str = "tok-SECRET-0123456789";

#[derive(Clone, Default)]
struct Route {
    status: u16,
    headers: Vec<(String, String)>,
    body: Vec<u8>,
    declared: u64,
    etag: String,
    not_modified: bool,
    echo_auth: bool,
}

fn route_of(v: &Value) -> (String, Route) {
    let mut r = Route {
        status: if v["status"] == 0 {
            200
        } else {
            v["status"].as_u64().unwrap() as u16
        },
        declared: v["declared_content_length"].as_u64().unwrap_or(0),
        etag: v["etag"].as_str().unwrap_or("").to_string(),
        not_modified: v["not_modified_on_matching_validator"]
            .as_bool()
            .unwrap_or(false),
        echo_auth: v["echo_authorization_header"].as_bool().unwrap_or(false),
        ..Route::default()
    };
    if let Some(h) = v["headers"].as_object() {
        for (k, val) in h {
            r.headers
                .push((k.clone(), val.as_str().unwrap().to_string()));
        }
    }
    r.body = if v["body_repeat"].is_object() {
        let t = v["body_repeat"]["text"].as_str().unwrap();
        t.repeat(v["body_repeat"]["count"].as_u64().unwrap() as usize)
            .into_bytes()
    } else {
        v["body"].as_str().unwrap_or("").as_bytes().to_vec()
    };
    (v["uri"].as_str().unwrap().to_string(), r)
}

/// One request as the fake forge saw it.
#[derive(Clone, Debug, PartialEq, Eq)]
struct Seen {
    method: String,
    uri: String,
    if_none_match: String,
    authorization: String,
}

struct Forge {
    base: String,
    routes: Arc<Mutex<BTreeMap<String, Route>>>,
    log: Arc<Mutex<Vec<Seen>>>,
}

impl Forge {
    fn start(routes: &[Value]) -> Forge {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let f = Forge {
            base,
            routes: Arc::new(Mutex::new(BTreeMap::new())),
            log: Arc::new(Mutex::new(Vec::new())),
        };
        {
            // among the routes of the scenario head the first one of a URI is served; a later one with the same
            // URI is what `set_routes` of a call puts in place
            let mut g = f.routes.lock().unwrap();
            for r in routes {
                let (uri, route) = route_of(r);
                g.entry(uri).or_insert(route);
            }
        }
        let (routes, log) = (f.routes.clone(), f.log.clone());
        std::thread::spawn(move || {
            for stream in listener.incoming() {
                let Ok(mut s) = stream else { continue };
                let mut buf = Vec::new();
                let mut chunk = [0u8; 2048];
                while !buf.windows(4).any(|w| w == b"\r\n\r\n") {
                    match s.read(&mut chunk) {
                        Ok(0) | Err(_) => break,
                        Ok(n) => buf.extend_from_slice(&chunk[..n]),
                    }
                }
                let text = String::from_utf8_lossy(&buf).into_owned();
                let mut lines = text.lines();
                let first = lines.next().unwrap_or("");
                let mut parts = first.split(' ');
                let (method, target) = (parts.next().unwrap_or(""), parts.next().unwrap_or(""));
                let hdr = |name: &str| {
                    text.lines()
                        .find_map(|l| {
                            let (k, v) = l.split_once(':')?;
                            k.eq_ignore_ascii_case(name).then(|| v.trim().to_string())
                        })
                        .unwrap_or_default()
                };
                let uri = target.trim_start_matches('/').to_string();
                let seen = Seen {
                    method: method.to_string(),
                    uri: uri.clone(),
                    if_none_match: hdr("if-none-match"),
                    authorization: hdr("authorization"),
                };
                log.lock().unwrap().push(seen.clone());
                let route = routes.lock().unwrap().get(&uri).cloned();
                let resp = match (method, route) {
                    ("GET", Some(r)) => respond(&r, &seen),
                    ("GET", None) => b"HTTP/1.1 404 Not Found\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}".to_vec(),
                    _ => b"HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\nConnection: close\r\n\r\n".to_vec(),
                };
                let _ = s.write_all(&resp);
            }
        });
        f
    }

    fn set_routes(&self, routes: &[Value]) {
        let mut g = self.routes.lock().unwrap();
        for r in routes {
            let (uri, route) = route_of(r);
            g.insert(uri, route);
        }
    }

    fn take_log(&self) -> Vec<Seen> {
        std::mem::take(&mut *self.log.lock().unwrap())
    }
}

fn respond(r: &Route, seen: &Seen) -> Vec<u8> {
    if r.not_modified && !r.etag.is_empty() && seen.if_none_match == r.etag {
        let mut out = String::from("HTTP/1.1 304 Not Modified\r\nConnection: close\r\n");
        for (k, v) in &r.headers {
            if !k.eq_ignore_ascii_case("link") {
                out.push_str(&format!("{k}: {v}\r\n"));
            }
        }
        out.push_str("\r\n");
        return out.into_bytes();
    }
    let body = if r.echo_auth {
        seen.authorization.clone().into_bytes()
    } else {
        r.body.clone()
    };
    let len = if r.declared > 0 {
        r.declared
    } else {
        body.len() as u64
    };
    let mut out = format!("HTTP/1.1 {} X\r\nContent-Type: application/json\r\nContent-Length: {len}\r\nConnection: close\r\n", r.status);
    for (k, v) in &r.headers {
        out.push_str(&format!("{k}: {v}\r\n"));
    }
    if !r.etag.is_empty() {
        out.push_str(&format!("ETag: {}\r\n", r.etag));
    }
    out.push_str("\r\n");
    let mut b = out.into_bytes();
    b.extend_from_slice(&body);
    b
}

// ---- http-reader scenarios -------------------------------------------------------------------------------

fn rate_json(r: &stw::watch_reader::http::RateInfo) -> Value {
    serde_json::json!({"known": r.known, "limit": r.limit, "remaining": r.remaining, "reset_unix": r.reset})
}

fn s(v: &Value) -> &str {
    v.as_str().unwrap_or("")
}

enum Outcome {
    Response(WatchResponse),
    Rate(stw::watch_reader::http::RateInfo),
    List(stw::watch_reader::http::PullListResult),
}

fn run_call(
    reader: &WatchHttp,
    call: &Value,
    prior: &HashMap<String, PullListState>,
) -> Result<Outcome, WatchError> {
    let repo = s(&call["repo"]);
    let num = call["number"].as_u64().unwrap_or(0);
    let page = call["page"].as_u64().unwrap_or(0);
    let etag = s(&call["if_none_match"]);
    match s(&call["op"]) {
        "pull_detail" => reader.pull_detail(repo, num, etag).map(Outcome::Response),
        "pull_list_page" => reader
            .pull_list_page(repo, call["per_page"].as_u64().unwrap() as u32, page, etag)
            .map(Outcome::Response),
        "pull_commits" => reader
            .pull_commits(repo, num, page, etag)
            .map(Outcome::Response),
        "branch" => reader
            .branch(repo, s(&call["name"]), etag)
            .map(Outcome::Response),
        "commit" => reader
            .commit(repo, s(&call["sha"]), etag)
            .map(Outcome::Response),
        "check_runs" => reader
            .check_runs(repo, s(&call["sha"]), page, etag)
            .map(Outcome::Response),
        "statuses" => reader
            .statuses(repo, s(&call["sha"]), page, etag)
            .map(Outcome::Response),
        "rate_limit" => reader.rate_limit().map(Outcome::Rate),
        "list_open_pulls" => {
            let state = match s(&call["prior_list_state"]) {
                "previous" => prior.get(repo).cloned().unwrap_or_default(),
                "per_page_only" => PullListState {
                    per_page: call["prior_per_page"].as_u64().unwrap() as u32,
                    pages: Vec::new(),
                },
                _ => PullListState::default(),
            };
            // a count below 1 (the corpus has -1 and 0) cannot be a `usize`: it is the refused value 0
            let max = call["max_open_prs"].as_i64().unwrap_or(0).max(0) as usize;
            reader.list_open_pulls(repo, &state, max).map(Outcome::List)
        }
        other => panic!("unknown op {other}"),
    }
}

fn check_error(name: &str, n: usize, e: &WatchError, want: &Value, routes: &[Value]) {
    let ctx = format!("{name} call {n}");
    assert_eq!(e.code.as_str(), s(&want["code"]), "{ctx}: code");
    assert_eq!(
        u64::from(e.status),
        want["status"].as_u64().unwrap_or(0),
        "{ctx}: status"
    );
    assert_eq!(e.new_name, s(&want["new_name"]), "{ctx}: new_name");
    assert_eq!(
        e.retry_after,
        want["retry_after_seconds"].as_u64().unwrap_or(0),
        "{ctx}: retry_after"
    );
    match want["rate_limit"].as_object() {
        None => assert!(!e.rate.known, "{ctx}: no rate evidence expected"),
        Some(_) => assert_eq!(rate_json(&e.rate), want["rate_limit"], "{ctx}: rate"),
    }
    let text = e.to_string();
    assert!(
        !text.contains(TOKEN),
        "{ctx}: the token is in the error text"
    );
    for r in routes {
        let b = s(&r["body"]);
        if b.len() >= 4 {
            assert!(
                !text.contains(b),
                "{ctx}: the response body is in the error text"
            );
        }
    }
    if want["message_at_most_200_bytes"] == true {
        assert!(e.message.len() <= 200, "{ctx}: message length");
    }
    assert_eq!(
        want["canceled"], false,
        "{ctx}: a request cannot be canceled in this reader"
    );
}

fn check_list(name: &str, n: usize, got: &stw::watch_reader::http::PullListResult, want: &Value) {
    let ctx = format!("{name} call {n}");
    assert_eq!(
        got.items.len() as u64,
        want["items"].as_u64().unwrap(),
        "{ctx}: item count"
    );
    let numbers: Vec<u64> = got.items.iter().map(|i| i.number).collect();
    let want_numbers: Vec<u64> = want["numbers"]
        .as_array()
        .unwrap()
        .iter()
        .map(|x| x.as_u64().unwrap())
        .collect();
    assert_eq!(numbers, want_numbers, "{ctx}: numbers");
    assert_eq!(
        got.unchanged,
        want["unchanged"].as_bool().unwrap_or(false),
        "{ctx}: unchanged"
    );
    assert_eq!(
        u64::from(got.state.per_page),
        want["per_page"].as_u64().unwrap(),
        "{ctx}: per_page"
    );
    let pages: Vec<&PullPage> = got.state.pages.iter().collect();
    let want_pages = want["pages"].as_array().unwrap();
    assert_eq!(pages.len(), want_pages.len(), "{ctx}: page count");
    for (p, w) in pages.iter().zip(want_pages) {
        assert_eq!(p.etag, s(&w["etag"]), "{ctx}: page etag");
        assert_eq!(
            !p.link.is_empty(),
            w["has_link"].as_bool().unwrap(),
            "{ctx}: has_link"
        );
        assert_eq!(
            p.items.len() as u64,
            w["items"].as_u64().unwrap(),
            "{ctx}: page items"
        );
    }
    if let Some(first) = want["first_item"].as_object() {
        let it = &got.items[0];
        let labels: Vec<&str> = it.labels.iter().map(String::as_str).collect();
        let want_labels: Vec<&str> = first["labels"]
            .as_array()
            .unwrap()
            .iter()
            .map(|x| x.as_str().unwrap())
            .collect();
        assert_eq!(it.number, first["number"].as_u64().unwrap(), "{ctx}");
        assert_eq!(it.title, s(&first["title"]), "{ctx}");
        assert_eq!(it.user, s(&first["user"]), "{ctx}");
        assert_eq!(labels, want_labels, "{ctx}");
        assert_eq!(it.draft, first["draft"].as_bool().unwrap(), "{ctx}");
        assert_eq!(it.head_sha, s(&first["head_sha"]), "{ctx}");
        assert_eq!(it.head_ref, s(&first["head_ref"]), "{ctx}");
        assert_eq!(it.base_ref, s(&first["base_ref"]), "{ctx}");
        assert_eq!(it.base_sha, s(&first["base_sha"]), "{ctx}");
    }
}

fn run_http_reader(name: &str, v: &Value) {
    let routes = v["routes"].as_array().unwrap();
    let forge = Forge::start(routes);
    let api_host = http::api_destination(s(&v["host"])).0;
    let mk = || WatchHttp::new(TOKEN.to_string(), forge.base.clone(), api_host.clone());
    let mut reader = mk();
    let mut prior: HashMap<String, PullListState> = HashMap::new();
    let mut all_routes: Vec<Value> = routes.clone();
    for (i, call) in v["calls"].as_array().unwrap().iter().enumerate() {
        let n = i + 1;
        let want = &v["expected"][i];
        assert_eq!(want["call"], n, "{name}: expectation order");
        if call["new_reader_before"] == true {
            reader = mk();
        }
        if call["reader_already_halved"] == true {
            reader.mark_halved(s(&call["repo"]));
        }
        if let Some(sr) = call["set_routes"].as_array() {
            forge.set_routes(sr);
            all_routes.extend(sr.iter().cloned());
        }
        forge.take_log();
        let got = run_call(&reader, call, &prior);
        let log = forge.take_log();
        assert!(
            log.iter().all(|r| r.method == "GET"),
            "{name} call {n}: a request that is not a GET"
        );
        let want_reqs: Vec<(String, String)> = want["requests"]
            .as_array()
            .map(|a| {
                a.iter()
                    .map(|r| (s(&r["uri"]).to_string(), s(&r["if_none_match"]).to_string()))
                    .collect()
            })
            .unwrap_or_default();
        let got_reqs: Vec<(String, String)> = log
            .iter()
            .map(|r| (r.uri.clone(), r.if_none_match.clone()))
            .collect();
        assert_eq!(got_reqs, want_reqs, "{name} call {n}: requests");
        for r in &log {
            assert!(
                r.authorization == format!("Bearer {TOKEN}"),
                "{name} call {n}: bearer token"
            );
        }
        match (got, want["error"].is_object()) {
            (Err(e), true) => check_error(name, n, &e, &want["error"], &all_routes),
            (Err(e), false) => panic!("{name} call {n}: unexpected error {e}"),
            (Ok(_), true) => panic!("{name} call {n}: expected the error {}", want["error"]),
            (Ok(Outcome::Response(r)), false) => {
                let w = &want["response"];
                assert_eq!(
                    u64::from(r.status),
                    w["status"].as_u64().unwrap(),
                    "{name} call {n}: status"
                );
                assert_eq!(
                    r.not_modified,
                    w["not_modified"].as_bool().unwrap(),
                    "{name} call {n}: not_modified"
                );
                assert_eq!(r.etag, s(&w["etag"]), "{name} call {n}: etag");
                assert_eq!(r.link, s(&w["link"]), "{name} call {n}: link");
                assert_eq!(
                    String::from_utf8_lossy(&r.body),
                    s(&w["body"]),
                    "{name} call {n}: body"
                );
                assert_eq!(rate_json(&r.rate), w["rate_limit"], "{name} call {n}: rate");
            }
            (Ok(Outcome::Rate(r)), false) => {
                assert_eq!(
                    rate_json(&r),
                    want["rate_info"],
                    "{name} call {n}: rate_info"
                );
            }
            (Ok(Outcome::List(l)), false) => {
                check_list(name, n, &l, &want["list"]);
                prior.insert(s(&call["repo"]).to_string(), l.state.clone());
            }
        }
    }
}

#[test]
fn http_reader_scenarios_are_replayed() {
    let all = scenarios_of("http-reader");
    assert_eq!(all.len(), 25, "the corpus has 25 http-reader scenarios");
    for (name, v) in &all {
        run_http_reader(name, v);
    }
}

#[test]
fn reader_error_codes_have_stable_names() {
    let names: Vec<&str> = [
        Code::RateLimited,
        Code::Network,
        Code::Unauthorized,
        Code::Forbidden,
        Code::Http5xx,
        Code::Malformed,
        Code::TooManyPrs,
        Code::PageTooLarge,
        Code::RepoMoved,
    ]
    .iter()
    .map(|c| c.as_str())
    .collect();
    assert_eq!(
        names,
        [
            "rate-limited",
            "network",
            "unauthorized",
            "forbidden",
            "http-5xx",
            "malformed-response",
            "too-many-open-prs",
            "page-too-large",
            "repo-moved"
        ]
    );
}
