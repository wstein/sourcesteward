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
                    ("POST", Some(r)) if uri == "graphql" => respond(&r, &seen),
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
// ---- the store of validators (no Go counterpart: the layout is Rust's own, `schema_version` 1) -------------

use stw::watch_reader::cond_store::{checks_key, detail_key, CondStore, TipEntry};
use stw::watch_reader::http::PullItem;

const SHA_A: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const SHA_B: &str = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

fn item(number: u64, head: &str, base: &str) -> PullItem {
    PullItem {
        number,
        title: format!("PR {number}"),
        user: "alice".into(),
        labels: vec!["x".into()],
        draft: false,
        head_sha: head.into(),
        head_ref: format!("feat/{number}"),
        base_ref: base.into(),
        base_sha: String::new(),
    }
}

#[allow(clippy::field_reassign_with_default)]
fn sample_store() -> CondStore {
    let mut c = CondStore::default();
    c.list = PullListState {
        per_page: 25,
        pages: vec![PullPage {
            etag: "\"p1\"".into(),
            link: "<https://api.github.com/repositories/1/pulls?page=2>; rel=\"next\"".into(),
            items: vec![item(1, SHA_A, "main"), item(2, SHA_B, "feat/1")],
        }],
    };
    c.tips.insert(
        "feat/1".into(),
        TipEntry {
            etag: "W/\"t\"".into(),
            sha: SHA_B.into(),
        },
    );
    c.validators.insert(detail_key(7), "\"d7\"".into());
    c.validators
        .insert(checks_key(SHA_A, "runs"), "\"r\"".into());
    c.validators
        .insert(checks_key(SHA_A, "statuses"), "\"s\"".into());
    c
}

#[test]
fn cond_store_round_trips() {
    let c = sample_store();
    let back = CondStore::from_json(&c.to_json()).unwrap();
    assert_eq!(back, c);
    let empty = CondStore::default();
    assert_eq!(CondStore::from_json(&empty.to_json()).unwrap(), empty);
    // through bytes as well: what is written is read back
    let bytes = serde_json::to_vec(&c.to_json()).unwrap();
    assert_eq!(
        CondStore::from_json(&serde_json::from_slice(&bytes).unwrap()).unwrap(),
        c
    );
}

#[test]
#[allow(clippy::field_reassign_with_default)]
fn cond_store_layout_is_documented_and_deterministic() {
    let mut c = CondStore::default();
    c.list = PullListState {
        per_page: 12,
        pages: vec![PullPage {
            etag: "\"e\"".into(),
            link: String::new(),
            items: vec![item(3, SHA_A, "main")],
        }],
    };
    c.tips.insert(
        "main".into(),
        TipEntry {
            etag: "\"t\"".into(),
            sha: SHA_B.into(),
        },
    );
    c.validators.insert(detail_key(3), "\"d\"".into());
    let want = format!(
        "{{\"schema_version\":1,\"list\":{{\"per_page\":12,\"pages\":[{{\"etag\":\"\\\"e\\\"\",\"link\":\"\",\"items\":[{{\"number\":3,\"title\":\"PR 3\",\"user\":\"alice\",\"labels\":[\"x\"],\"draft\":false,\"head_sha\":\"{SHA_A}\",\"head_ref\":\"feat/3\",\"base_ref\":\"main\",\"base_sha\":\"\"}}]}}]}},\"tips\":{{\"main\":{{\"etag\":\"\\\"t\\\"\",\"sha\":\"{SHA_B}\"}}}},\"validators\":{{\"pulls/3\":\"\\\"d\\\"\"}}}}"
    );
    assert_eq!(serde_json::to_string(&c.to_json()).unwrap(), want);
    assert_eq!(serde_json::to_string(&c.clone().to_json()).unwrap(), want);
}

#[test]
#[allow(clippy::type_complexity)]
fn cond_store_refuses_what_does_not_fit_the_layout() {
    let good = sample_store().to_json();
    assert!(CondStore::from_json(&good).is_ok());
    let mut cases: Vec<(&str, Box<dyn Fn(&mut Value)>)> = vec![
        (
            "other version",
            Box::new(|v| v["schema_version"] = 2.into()),
        ),
        (
            "no version",
            Box::new(|v| {
                v.as_object_mut().unwrap().remove("schema_version");
            }),
        ),
        ("unknown top field", Box::new(|v| v["extra"] = 1.into())),
        ("per_page 7", Box::new(|v| v["list"]["per_page"] = 7.into())),
        (
            "per_page 0 with pages",
            Box::new(|v| v["list"]["per_page"] = 0.into()),
        ),
        (
            "per_page 12 with 13 items",
            Box::new(|v| {
                v["list"]["per_page"] = 12.into();
                let it = v["list"]["pages"][0]["items"][0].clone();
                v["list"]["pages"][0]["items"] = Value::Array(vec![it; 13]);
            }),
        ),
        (
            "too many pages",
            Box::new(|v| {
                let p = v["list"]["pages"][0].clone();
                v["list"]["pages"] = Value::Array(vec![p; 81]);
            }),
        ),
        (
            "page etag with a line break",
            Box::new(|v| v["list"]["pages"][0]["etag"] = "a\nb".into()),
        ),
        (
            "page etag over 256 bytes",
            Box::new(|v| v["list"]["pages"][0]["etag"] = "e".repeat(257).into()),
        ),
        (
            "link with a control character",
            Box::new(|v| v["list"]["pages"][0]["link"] = "<x>\u{1}".into()),
        ),
        (
            "link over 8192 bytes",
            Box::new(|v| v["list"]["pages"][0]["link"] = "l".repeat(8193).into()),
        ),
        (
            "item number 0",
            Box::new(|v| v["list"]["pages"][0]["items"][0]["number"] = 0.into()),
        ),
        (
            "item head sha short",
            Box::new(|v| v["list"]["pages"][0]["items"][0]["head_sha"] = "abc".into()),
        ),
        (
            "item base sha uppercase",
            Box::new(|v| {
                v["list"]["pages"][0]["items"][0]["base_sha"] = SHA_A.to_uppercase().into()
            }),
        ),
        (
            "item label no text",
            Box::new(|v| v["list"]["pages"][0]["items"][0]["labels"] = serde_json::json!([1])),
        ),
        (
            "item with an extra field",
            Box::new(|v| v["list"]["pages"][0]["items"][0]["x"] = 1.into()),
        ),
        (
            "item no object",
            Box::new(|v| v["list"]["pages"][0]["items"][0] = 5.into()),
        ),
        (
            "tip sha short",
            Box::new(|v| v["tips"]["feat/1"]["sha"] = "abc".into()),
        ),
        (
            "tip branch with a percent",
            Box::new(|v| {
                let t = v["tips"]["feat/1"].clone();
                v["tips"] = serde_json::json!({"a%2Fb": t});
            }),
        ),
        (
            "tip etag with a control character",
            Box::new(|v| v["tips"]["feat/1"]["etag"] = "\u{7f}".into()),
        ),
        (
            "validator key outside the reader paths",
            Box::new(|v| v["validators"]["repos/a/b"] = "\"x\"".into()),
        ),
        (
            "validator key pulls/0",
            Box::new(|v| v["validators"]["pulls/0"] = "\"x\"".into()),
        ),
        (
            "validator check key with a short sha",
            Box::new(|v| v["validators"]["checks/abc/runs"] = "\"x\"".into()),
        ),
        (
            "validator etag no text",
            Box::new(|v| v["validators"]["pulls/7"] = 5.into()),
        ),
        (
            "validator etag with a line break",
            Box::new(|v| v["validators"]["pulls/7"] = "a\r\nb".into()),
        ),
    ];
    cases.push(("not an object", Box::new(|v| *v = Value::Null)));
    for (name, mutate) in &cases {
        let mut v = good.clone();
        mutate(&mut v);
        assert!(CondStore::from_json(&v).is_err(), "{name}: accepted");
    }
}

#[test]
fn cond_store_bounds_the_number_of_validators() {
    let mut v = sample_store().to_json();
    let mut m = serde_json::Map::new();
    for n in 1..=20_001u64 {
        m.insert(detail_key(n), "\"e\"".into());
    }
    v["validators"] = Value::Object(m);
    assert!(CondStore::from_json(&v).is_err());
    let mut ok = serde_json::Map::new();
    for n in 1..=20_000u64 {
        ok.insert(detail_key(n), "\"e\"".into());
    }
    v["validators"] = Value::Object(ok);
    assert!(CondStore::from_json(&v).is_ok());
}

#[test]
fn cond_store_bounds_the_number_of_tips() {
    use stw::watch_reader::cond_store::MAX_TIPS;

    assert_eq!(MAX_TIPS, 5_000);
    let mut v = sample_store().to_json();
    let tips = |n: usize| {
        let mut m = serde_json::Map::new();
        for i in 1..=n {
            m.insert(
                format!("b{i}"),
                serde_json::json!({"etag": "\"t\"", "sha": SHA_A}),
            );
        }
        Value::Object(m)
    };
    v["tips"] = tips(MAX_TIPS);
    assert!(CondStore::from_json(&v).is_ok());
    v["tips"] = tips(MAX_TIPS + 1);
    assert!(CondStore::from_json(&v).is_err());
}

#[test]
fn cond_store_prune_keeps_only_open_prs_heads_and_bases() {
    let mut c = sample_store();
    c.validators.insert(detail_key(99), "\"old\"".into());
    c.validators
        .insert(checks_key(SHA_B, "runs"), "\"gone\"".into());
    c.tips.insert(
        "old-base".into(),
        TipEntry {
            etag: "\"t\"".into(),
            sha: SHA_A.into(),
        },
    );
    c.prune(&[item(7, SHA_A, "main"), item(2, SHA_A, "feat/1")]);
    let keys: Vec<&str> = c.validators.keys().map(String::as_str).collect();
    let a_runs = checks_key(SHA_A, "runs");
    let a_stat = checks_key(SHA_A, "statuses");
    assert_eq!(keys, vec![a_runs.as_str(), a_stat.as_str(), "pulls/7"]);
    assert_eq!(
        c.tips.keys().map(String::as_str).collect::<Vec<_>>(),
        vec!["feat/1"]
    );
}

// ---- the observer: vectors and observer-session scenarios -------------------------------------------------

use std::cell::RefCell;
use std::rc::Rc;
use std::sync::atomic::AtomicBool;

use stw::github::reads::WorkflowRun;
use stw::watch_reader::host::{
    decode_pull_detail, guess_default_branch, read_pull_commits, CommitsError, HostObserver,
    WatchRead,
};
use stw::watch_reader::http::PullListResult;
use stw::watch_reader::obs::{Hints, KnownPr, ObserveFailure, RepoObservation};

#[test]
fn vector_decode_pull_detail() {
    let n = each_case(
        "watch-decode-pull-detail",
        "decode_pull_detail",
        |name, i, e| {
            let got = decode_pull_detail(i["body"].as_str().unwrap().as_bytes());
            match got {
                Err(err) => {
                    assert_eq!(e["ok"], false, "{name}: {err}");
                    assert_eq!(err.code.as_str(), s(&e["error_code"]), "{name}");
                }
                Ok(d) => {
                    assert_eq!(e["ok"], true, "{name}");
                    assert_eq!(d.fork, e["fork"].as_bool().unwrap(), "{name}");
                    assert_eq!(d.head_sha, s(&e["head_sha"]), "{name}");
                    assert_eq!(d.mergeable, e["mergeable"].as_bool(), "{name}");
                    assert_eq!(d.mergeable_state, s(&e["mergeable_state"]), "{name}");
                    assert_eq!(d.rebaseable, e["rebaseable"].as_bool(), "{name}");
                }
            }
        },
    );
    assert_eq!(n, 5);
}

#[test]
fn vector_observe_default_branch() {
    let n = each_case(
        "observe-default-branch-guess",
        "observe_default_branch",
        |name, i, e| {
            let bases: Vec<&str> = i["pr_bases"]
                .as_array()
                .unwrap()
                .iter()
                .map(|b| b.as_str().unwrap())
                .collect();
            assert_eq!(
                guess_default_branch(&bases),
                s(&e["default_branch"]),
                "{name}"
            );
        },
    );
    assert_eq!(n, 7);
}

#[test]
fn vector_decode_pull_commits() {
    let n = each_case("commit-list-decode", "decode_pull_commits", |name, i, e| {
        let fake = CommitsFake {
            body: i["body"].as_str().unwrap().to_string(),
            transport: s(&i["transport_error"]).to_string(),
            seen: RefCell::new(String::new()),
        };
        let got = read_pull_commits(&fake, "o/r", 12, 2);
        assert_eq!(*fake.seen.borrow(), s(&e["request"]), "{name}: request");
        let want_kind = s(&e["error_kind"]);
        match got {
            Ok((shas, more)) => {
                assert_eq!(want_kind, "", "{name}");
                let want: Vec<&str> = e["shas"]
                    .as_array()
                    .unwrap()
                    .iter()
                    .map(|x| x.as_str().unwrap())
                    .collect();
                assert_eq!(
                    shas.iter().map(String::as_str).collect::<Vec<_>>(),
                    want,
                    "{name}"
                );
                assert_eq!(more, e["more"].as_bool().unwrap(), "{name}");
            }
            Err(CommitsError::Malformed) => assert_eq!(want_kind, "malformed", "{name}"),
            Err(CommitsError::Other(_)) => assert_eq!(want_kind, "other", "{name}"),
        }
    });
    assert_eq!(n, 9);
}

struct CommitsFake {
    body: String,
    transport: String,
    seen: RefCell<String>,
}

impl WatchRead for CommitsFake {
    fn pull_commits(
        &self,
        repo: &str,
        number: u64,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        *self.seen.borrow_mut() = format!("{repo}#{number} p{page} etag={etag:?}");
        match self.transport.as_str() {
            "malformed" => Err(WatchError::new(Code::Malformed, "x")),
            "other" => Err(WatchError::new(Code::Network, "x")),
            _ => Ok(WatchResponse {
                status: 200,
                body: self.body.clone().into_bytes(),
                ..WatchResponse::default()
            }),
        }
    }
    fn list_open_pulls(
        &self,
        _: &str,
        _: &PullListState,
        _: usize,
    ) -> Result<PullListResult, WatchError> {
        unreachable!()
    }
    fn pull_detail(&self, _: &str, _: u64, _: &str) -> Result<WatchResponse, WatchError> {
        unreachable!()
    }
    fn branch(&self, _: &str, _: &str, _: &str) -> Result<WatchResponse, WatchError> {
        unreachable!()
    }
    fn commit(&self, _: &str, _: &str, _: &str) -> Result<WatchResponse, WatchError> {
        unreachable!()
    }
    fn check_runs(&self, _: &str, _: &str, _: u64, _: &str) -> Result<WatchResponse, WatchError> {
        unreachable!()
    }
    fn statuses(&self, _: &str, _: &str, _: u64, _: &str) -> Result<WatchResponse, WatchError> {
        unreachable!()
    }
    fn rate_limit(&self) -> Result<stw::watch_reader::http::RateInfo, WatchError> {
        unreachable!()
    }
}

/// The fake reader of the Go tests (`observer_test.go: fakeReader`): canned documents, a validator for details, tips,
/// runs and statuses, and every call recorded with the validator it carried.
#[derive(Default)]
struct World {
    items: Vec<PullItem>,
    list_error: Option<WatchError>,
    details: HashMap<u64, String>,
    detail_etags: HashMap<u64, String>,
    tips: HashMap<String, String>,
    runs: HashMap<String, String>,
    stats: HashMap<String, String>,
    commits: HashMap<String, String>,
    pulls: HashMap<u64, String>,
    rate: stw::watch_reader::http::RateInfo,
    errs: HashMap<String, WatchError>,
    actions: Option<Vec<WorkflowRun>>,
}

#[derive(Default)]
struct FakeReader {
    w: World,
    calls: RefCell<Vec<String>>,
    etags: RefCell<HashMap<String, String>>,
}

impl FakeReader {
    fn note(&self, call: String, etag: &str) -> Result<(), WatchError> {
        self.etags
            .borrow_mut()
            .insert(call.clone(), etag.to_string());
        self.calls.borrow_mut().push(call.clone());
        match self.w.errs.get(&call) {
            Some(e) => Err(e.clone()),
            None => Ok(()),
        }
    }
    fn ok(etag: &str, body: &str) -> WatchResponse {
        WatchResponse {
            status: 200,
            etag: etag.into(),
            body: body.as_bytes().to_vec(),
            ..WatchResponse::default()
        }
    }
    fn not_modified(etag: &str) -> WatchResponse {
        WatchResponse {
            status: 304,
            not_modified: true,
            etag: etag.into(),
            ..WatchResponse::default()
        }
    }
}

impl WatchRead for FakeReader {
    fn repository_metadata(&self, _repo: &str) -> Result<WatchResponse, WatchError> {
        self.note("metadata".into(), "")?;
        Ok(Self::ok("", r#"{"default_branch":"trunk"}"#))
    }

    fn branches_page(&self, _repo: &str, page: u64) -> Result<WatchResponse, WatchError> {
        self.note(format!("branches/{page}"), "")?;
        let name = if page == 1 { "trunk" } else { "release" };
        let mut response = Self::ok(
            "",
            &serde_json::json!([{"name":name,"commit":{"sha":"a".repeat(40)}}]).to_string(),
        );
        if page == 1 {
            response.link =
                r#"<https://api.github.com/repos/o/r/branches?per_page=100&page=2>; rel="next""#
                    .into();
        }
        Ok(response)
    }

    fn recent_workflows(&self, _repo: &str) -> Result<Vec<WatchResponse>, WatchError> {
        self.note("workflows".into(), "")?;
        Ok(vec![
            Self::ok("", r#"{"workflow_runs":[{"status":"queued","id":1}]}"#),
            Self::ok("", r#"{"workflow_runs":[{"status":"in_progress","id":2}]}"#),
        ])
    }

    fn list_open_pulls(
        &self,
        _repo: &str,
        _prior: &PullListState,
        _max: usize,
    ) -> Result<PullListResult, WatchError> {
        self.calls.borrow_mut().push("list".into());
        if let Some(e) = &self.w.list_error {
            return Err(e.clone());
        }
        let state = PullListState {
            per_page: 25,
            pages: vec![PullPage {
                etag: "\"p1\"".into(),
                link: String::new(),
                items: self.w.items.clone(),
            }],
        };
        Ok(PullListResult {
            state,
            items: self.w.items.clone(),
            unchanged: false,
        })
    }
    fn pull_detail(&self, _repo: &str, n: u64, etag: &str) -> Result<WatchResponse, WatchError> {
        self.note(format!("detail/{n}"), etag)?;
        let tag = self.w.detail_etags.get(&n).cloned().unwrap_or_default();
        if !etag.is_empty() && etag == tag {
            return Ok(Self::not_modified(etag));
        }
        Ok(Self::ok(
            &tag,
            self.w.details.get(&n).map_or("", String::as_str),
        ))
    }
    fn branch(&self, _repo: &str, name: &str, etag: &str) -> Result<WatchResponse, WatchError> {
        self.note(format!("branch/{name}"), etag)?;
        let tip = self.w.tips.get(name).cloned().unwrap_or_default();
        let tag = format!("\"t-{tip}\"");
        if etag == tag {
            return Ok(Self::not_modified(etag));
        }
        Ok(Self::ok(
            &tag,
            &format!("{{\"commit\":{{\"sha\":\"{tip}\"}}}}"),
        ))
    }
    fn commit(&self, _repo: &str, sha: &str, etag: &str) -> Result<WatchResponse, WatchError> {
        self.note(format!("commit/{}", &sha[..4]), etag)?;
        Ok(Self::ok(
            "",
            self.w.commits.get(sha).map_or("", String::as_str),
        ))
    }
    fn pull_commits(
        &self,
        _repo: &str,
        n: u64,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        self.note(format!("commits/{n}/{page}"), etag)?;
        let body = self.w.pulls.get(&n).map_or("", String::as_str);
        Ok(Self::ok(
            "",
            if body.is_empty() || page > 1 {
                "[]"
            } else {
                body
            },
        ))
    }
    fn check_runs(
        &self,
        _repo: &str,
        sha: &str,
        _page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        self.note(format!("runs/{}", &sha[..4]), etag)?;
        let body = self.w.runs.get(sha).map_or("", String::as_str);
        let tag = format!("\"r-{}\"", body.len());
        if etag == tag {
            return Ok(Self::not_modified(etag));
        }
        Ok(Self::ok(
            &tag,
            if body.is_empty() {
                "{\"total_count\":0,\"check_runs\":[]}"
            } else {
                body
            },
        ))
    }
    fn statuses(
        &self,
        _repo: &str,
        sha: &str,
        _page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        self.note(format!("stats/{}", &sha[..4]), etag)?;
        let body = self.w.stats.get(sha).map_or("", String::as_str);
        let tag = format!("\"s-{}\"", body.len());
        if etag == tag {
            return Ok(Self::not_modified(etag));
        }
        Ok(Self::ok(&tag, if body.is_empty() { "[]" } else { body }))
    }
    fn rate_limit(&self) -> Result<stw::watch_reader::http::RateInfo, WatchError> {
        Ok(self.w.rate)
    }
    fn actions_runs(&self, repo: &str, sha: &str) -> Result<Vec<WorkflowRun>, WatchError> {
        self.calls
            .borrow_mut()
            .push(format!("actions/{}", &sha[..4]));
        match &self.w.actions {
            Some(r) => Ok(r.clone()),
            None => {
                let _ = repo;
                let mut e = WatchError::new(Code::Forbidden, "access was refused");
                e.status = 403;
                Err(e)
            }
        }
    }
}

/// The observer keeps one reader for its whole life; a scenario changes what it answers between steps.
#[derive(Clone)]
struct Swap(Rc<RefCell<Rc<FakeReader>>>);

impl Swap {
    fn cur(&self) -> Rc<FakeReader> {
        self.0.borrow().clone()
    }
}

impl WatchRead for Swap {
    fn repository_metadata(&self, repo: &str) -> Result<WatchResponse, WatchError> {
        self.cur().repository_metadata(repo)
    }
    fn branches_page(&self, repo: &str, page: u64) -> Result<WatchResponse, WatchError> {
        self.cur().branches_page(repo, page)
    }
    fn recent_workflows(&self, repo: &str) -> Result<Vec<WatchResponse>, WatchError> {
        self.cur().recent_workflows(repo)
    }
    fn list_open_pulls(
        &self,
        r: &str,
        p: &PullListState,
        m: usize,
    ) -> Result<PullListResult, WatchError> {
        self.cur().list_open_pulls(r, p, m)
    }
    fn pull_detail(&self, r: &str, n: u64, e: &str) -> Result<WatchResponse, WatchError> {
        self.cur().pull_detail(r, n, e)
    }
    fn branch(&self, r: &str, n: &str, e: &str) -> Result<WatchResponse, WatchError> {
        self.cur().branch(r, n, e)
    }
    fn commit(&self, r: &str, s: &str, e: &str) -> Result<WatchResponse, WatchError> {
        self.cur().commit(r, s, e)
    }
    fn pull_commits(&self, r: &str, n: u64, p: u64, e: &str) -> Result<WatchResponse, WatchError> {
        self.cur().pull_commits(r, n, p, e)
    }
    fn check_runs(&self, r: &str, s: &str, p: u64, e: &str) -> Result<WatchResponse, WatchError> {
        self.cur().check_runs(r, s, p, e)
    }
    fn statuses(&self, r: &str, s: &str, p: u64, e: &str) -> Result<WatchResponse, WatchError> {
        self.cur().statuses(r, s, p, e)
    }
    fn rate_limit(&self) -> Result<stw::watch_reader::http::RateInfo, WatchError> {
        self.cur().rate_limit()
    }
    fn actions_runs(&self, r: &str, s: &str) -> Result<Vec<WorkflowRun>, WatchError> {
        self.cur().actions_runs(r, s)
    }
}

#[test]
fn workflows_only_skips_pr_reads_and_all_continues_after_pr_denial() {
    for prs in [false, true] {
        let mut reader = FakeReader::default();
        let mut denied = WatchError::new(Code::Forbidden, "denied");
        denied.status = 403;
        reader.w.list_error = Some(denied);
        let reader = Rc::new(reader);
        let mut observer = HostObserver::new(
            Swap(Rc::new(RefCell::new(reader.clone()))),
            Box::new(|| 1_700_000_000),
            20,
            vec![],
            vec![],
        )
        .with_prs(prs)
        .with_workflows(true);

        let cancel = std::sync::atomic::AtomicBool::new(false);
        let first = observer
            .observe("o/r", &Hints::default(), None, &cancel)
            .unwrap();
        assert!(first.skip_pr_events);
        assert_eq!(first.pr_read_error.is_some(), prs);
        assert_eq!(first.workflows, Some(Ok(vec![])));
        assert_eq!(first.active_workflows, 2);
        let second = observer
            .observe("o/r", &Hints::default(), None, &cancel)
            .unwrap();
        assert!(second.pr_read_error.is_none());
        assert_eq!(second.workflows, Some(Ok(vec![])));
        let expected = if prs {
            vec!["list", "workflows", "workflows"]
        } else {
            vec!["workflows", "workflows"]
        };
        assert_eq!(*reader.calls.borrow(), expected);
    }
}

#[test]
fn watch_branch_selection_uses_repository_default_and_paginates_only_when_requested() {
    let mut reader = FakeReader::default();
    reader.w.tips.insert("trunk".into(), "a".repeat(40));
    reader.w.tips.insert("release".into(), "b".repeat(40));
    let reader = Rc::new(reader);
    let make = || {
        HostObserver::new(
            Swap(Rc::new(RefCell::new(reader.clone()))),
            Box::new(|| 1_700_000_000),
            20,
            vec![],
            vec![],
        )
        .with_prs(false)
    };
    let cancel = std::sync::atomic::AtomicBool::new(false);
    let result = make()
        .with_branches(true, false, vec!["release".into(), "trunk".into()])
        .observe("o/r", &Hints::default(), None, &cancel)
        .unwrap();
    assert_eq!(
        result.branches.unwrap().unwrap().keys().collect::<Vec<_>>(),
        ["release", "trunk"]
    );
    assert_eq!(
        reader.calls.borrow_mut().drain(..).collect::<Vec<_>>(),
        ["metadata", "branch/release", "branch/trunk"]
    );
    let result = make()
        .with_branches(true, true, vec![])
        .observe("o/r", &Hints::default(), None, &cancel)
        .unwrap();
    assert_eq!(result.branches.unwrap().unwrap().len(), 2);
    assert_eq!(*reader.calls.borrow(), ["branches/1", "branches/2"]);
}

fn code_of(name: &str) -> Code {
    [
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
    .into_iter()
    .find(|c| c.as_str() == name)
    .unwrap_or_else(|| panic!("unknown code {name}"))
}

fn err_of(v: &Value) -> WatchError {
    // kind "plain" is any other error: the observer reports it as `network`
    let code = if v["kind"] == "watch" {
        code_of(s(&v["code"]))
    } else {
        Code::Network
    };
    let mut e = WatchError::new(code, s(&v["message"]));
    e.retry_after = v["retry_after_seconds"].as_u64().unwrap_or(0);
    e
}

fn world_of(w: &Value, now: i64) -> World {
    let mut out = World::default();
    for it in w["list_items"].as_array().unwrap() {
        out.items.push(PullItem {
            number: it["number"].as_u64().unwrap(),
            title: s(&it["title"]).into(),
            user: s(&it["user"]).into(),
            labels: it["labels"]
                .as_array()
                .unwrap()
                .iter()
                .map(|l| s(l).to_string())
                .collect(),
            draft: it["draft"].as_bool().unwrap(),
            head_sha: s(&it["head_sha"]).into(),
            head_ref: s(&it["head_ref"]).into(),
            base_ref: s(&it["base_ref"]).into(),
            base_sha: String::new(),
        });
    }
    if w["list_error"].is_object() {
        out.list_error = Some(err_of(&w["list_error"]));
    }
    let nums = |m: &Value| -> HashMap<u64, String> {
        m.as_object()
            .unwrap()
            .iter()
            .map(|(k, v)| (k.parse().unwrap(), s(v).to_string()))
            .collect()
    };
    let strs = |m: &Value| -> HashMap<String, String> {
        m.as_object()
            .unwrap()
            .iter()
            .map(|(k, v)| (k.clone(), s(v).to_string()))
            .collect()
    };
    out.details = nums(&w["pull_details"]);
    out.detail_etags = nums(&w["pull_detail_etags"]);
    out.pulls = nums(&w["pull_commits"]);
    out.tips = strs(&w["branch_tips"]);
    out.runs = strs(&w["check_runs"]);
    out.stats = strs(&w["commit_statuses"]);
    out.commits = strs(&w["commits"]);
    if w["rate_limit"].is_object() {
        let r = &w["rate_limit"];
        out.rate = stw::watch_reader::http::RateInfo {
            known: r["known"].as_bool().unwrap(),
            limit: r["limit"].as_u64().unwrap(),
            remaining: r["remaining"].as_u64().unwrap(),
            reset: now + r["reset_in_seconds"].as_i64().unwrap(),
        };
    }
    for (k, e) in w["call_errors"].as_object().unwrap() {
        out.errs.insert(k.clone(), err_of(e));
    }
    out
}

fn hints_of(h: &Value) -> Hints {
    Hints {
        cycle: h["cycle"].as_u64().unwrap(),
        detail_every: h["detail_every"].as_u64().unwrap(),
        known: h["known"]
            .as_array()
            .map(|a| {
                a.iter()
                    .map(|k| KnownPr {
                        number: k["number"].as_u64().unwrap(),
                        head: s(&k["head"]).into(),
                        class: s(&k["class"]).into(),
                        class_candidate: s(&k["class_candidate"]).into(),
                        detail_due: k["detail_due"].as_bool().unwrap_or(false),
                    })
                    .collect()
            })
            .unwrap_or_default(),
    }
}

/// The observation as the corpus writes it (`oObs` of the exporter).
fn obs_json(o: &RepoObservation) -> Value {
    let prs: Vec<Value> = o
        .prs
        .iter()
        .map(|p| {
            let checks = p.checks.as_ref().map(|c| {
                serde_json::json!({
                    "runs": c.runs.iter().map(|r| serde_json::json!({"name": r.name, "status": r.status,
                        "conclusion": r.conclusion, "title": r.title, "summary": r.summary, "text": r.text})).collect::<Vec<_>>(),
                    "statuses": c.statuses.iter().map(|x| serde_json::json!({"context": x.context, "state": x.state,
                        "description": x.description})).collect::<Vec<_>>(),
                })
            });
            serde_json::json!({
                "number": p.number, "title": p.title, "author": p.author, "labels": p.labels, "draft": p.draft,
                "head": p.head, "head_ref": p.head_ref, "base": p.base, "fork": p.fork, "fork_known": p.fork_known,
                "detail": p.detail.as_ref().map(|d| serde_json::json!({"mergeable": d.mergeable, "state": d.state})),
                "checks": checks,
            })
        })
        .collect();
    let gone: serde_json::Map<String, Value> = o
        .gone
        .iter()
        .map(|(n, g)| {
            (
                n.to_string(),
                serde_json::json!({"merged": g.merged, "merge_commit": g.merge_commit, "merged_at": g.merged_at,
                    "base": g.base, "head": g.head, "head_ref": g.head_ref, "method_hint": g.method_hint.as_str()}),
            )
        })
        .collect();
    serde_json::json!({"default_branch": o.default_branch, "prs": prs, "gone": gone})
}

/// The calls of the fake in order; a read is conditional when the last validator sent under that call name was not
/// empty (that is how the exporter wrote the corpus).
fn calls_json(f: &FakeReader) -> Value {
    let etags = f.etags.borrow();
    Value::Array(
        f.calls
            .borrow()
            .iter()
            .map(|c| serde_json::json!({"call": c, "conditional": etags.get(c).is_some_and(|e| !e.is_empty())}))
            .collect(),
    )
}

fn clock_of(v: &Value) -> i64 {
    stw::github::parse_utc(s(&v["config"]["clock"])).unwrap()
}

fn run_observer_session(name: &str, v: &Value) {
    let now = clock_of(v);
    let cfg = &v["config"];
    let strs = |k: &str| -> Vec<String> {
        cfg[k]
            .as_array()
            .unwrap()
            .iter()
            .map(|x| s(x).to_string())
            .collect()
    };
    let swap = Swap(Rc::new(RefCell::new(Rc::new(FakeReader::default()))));
    let mk = |swap: &Swap| {
        HostObserver::new(
            swap.clone(),
            Box::new(move || now),
            cfg["max_open_prs"].as_u64().unwrap() as usize,
            strs("ignore_authors"),
            strs("ignore_labels"),
        )
    };
    let mut observer = mk(&swap);
    let mut cond: Option<stw::watch_reader::cond_store::CondStore> = None;
    for (i, step) in v["steps"].as_array().unwrap().iter().enumerate() {
        let ctx = format!("{name} step {}", i + 1);
        let want = &v["expected"][i];
        assert_eq!(want["step"], i + 1, "{ctx}");
        if step["new_process"] == true {
            observer = mk(&swap);
        }
        let fake = Rc::new(FakeReader {
            w: world_of(&step["world"], now),
            ..FakeReader::default()
        });
        *swap.0.borrow_mut() = fake.clone();
        let cancel = AtomicBool::new(step["context_canceled"] == true);
        let mut slow = Value::Null;
        if step["after_cycle_first"] == true {
            slow = observer.after_cycle().into();
        }
        let got = observer.observe("o/r", &hints_of(&step["hints"]), cond.as_ref(), &cancel);
        let (obs, err) = match got {
            Ok(o) => {
                if let Some(c) = &o.cond {
                    cond = Some(c.clone());
                }
                (obs_json(&o), Value::Null)
            }
            Err(ObserveFailure::Canceled) => (
                Value::Null,
                serde_json::json!({"code": "", "run_wide": false, "retry_in_seconds": 0, "canceled": true}),
            ),
            Err(ObserveFailure::Error(e)) => (
                Value::Null,
                serde_json::json!({"code": e.code.as_str(), "run_wide": e.run_wide, "retry_in_seconds": e.retry_in, "canceled": false}),
            ),
        };
        assert_eq!(obs, want["observation"], "{ctx}: observation");
        assert_eq!(err, want["error"], "{ctx}: error");
        assert_eq!(calls_json(&fake), want["calls"], "{ctx}: calls");
        let diags: Vec<String> = observer.take_diagnostics();
        let want_diags: Vec<String> = want["diagnostics"]
            .as_array()
            .unwrap()
            .iter()
            .map(|d| s(d).to_string())
            .collect();
        assert_eq!(diags, want_diags, "{ctx}: diagnostics");
        assert_eq!(slow, want["slow_after_cycle"], "{ctx}: slow_after_cycle");
    }
}

#[test]
fn observer_session_scenarios_are_replayed() {
    let all = scenarios_of("observer-session");
    assert_eq!(
        all.len(),
        14,
        "the corpus has 14 observer-session scenarios"
    );
    for (name, v) in &all {
        run_observer_session(name, v);
    }
}

#[test]
fn vector_checks_class() {
    use stw::watch_reader::obs::{checks_class, CheckRun, StatusContext};
    let mut total = 0;
    for (file, n) in [
        ("checks-classify-classes", 16),
        ("checks-classify-failing-names", 2),
        ("checks-shape-rules", 22),
        ("checks-shape-text-bound", 3),
    ] {
        total += each_case(file, "classify_checks", |name, i, e| {
            let runs: Vec<CheckRun> = i["runs"]
                .as_array()
                .unwrap()
                .iter()
                .map(|r| CheckRun {
                    name: s(&r["name"]).into(),
                    status: s(&r["status"]).into(),
                    conclusion: s(&r["conclusion"]).into(),
                    title: s(&r["title"]).into(),
                    summary: s(&r["summary"]).into(),
                    text: s(&r["text"]).into(),
                })
                .collect();
            let stats: Vec<StatusContext> = i["statuses"]
                .as_array()
                .unwrap()
                .iter()
                .map(|x| StatusContext {
                    context: s(&x["context"]).into(),
                    state: s(&x["state"]).into(),
                    description: s(&x["description"]).into(),
                })
                .collect();
            assert_eq!(
                checks_class(&runs, &stats),
                s(&e["class"]),
                "{file}: {name}"
            );
        });
        assert!(total >= n);
    }
    assert_eq!(total, 43);
}

#[test]
fn rfc3339_accepts_what_go_parses() {
    use stw::watch_reader::host::valid_rfc3339;
    for ok in [
        "2026-10-22T08:00:00Z",
        "2026-10-22T08:00:00.123456789Z",
        "2026-10-22T10:00:00+02:00",
        "2026-10-22T08:00:00-05:30",
        "2028-02-29T00:00:00Z",
    ] {
        assert!(valid_rfc3339(ok), "{ok}");
    }
    for bad in [
        "",
        "garbage",
        "2026-10-22 08:00:00Z",
        "2026-13-01T00:00:00Z",
        "2026-02-30T00:00:00Z",
        "2027-02-29T00:00:00Z",
        "2026-10-22T24:00:00Z",
        "2026-10-22T08:60:00Z",
        "2026-10-22T08:00:60Z",
        "2026-10-22T08:00:00",
        "2026-10-22T08:00:00+0200",
        "2026-10-22T08:00:00.Z",
        "2026-10-22t08:00:00Z",
        "2026-10-22T08:00:00+24:00",
        "2026-10-22T08:00:00Zjunk",
    ] {
        assert!(!valid_rfc3339(bad), "{bad}");
    }
}

// ---- tests without a Go counterpart: end to end over HTTP, the token, the Actions fallback, limits --------

fn sha(c: char) -> String {
    c.to_string().repeat(40)
}

fn list_route(base_ref: &str, head: &str) -> Value {
    let body = serde_json::json!([{"number": 1, "title": "PR 1", "user": {"login": "alice"}, "labels": [{"name": "x"}],
        "draft": false, "head": {"sha": head, "ref": "feat/1"}, "base": {"ref": base_ref, "sha": sha('a')}}]);
    serde_json::json!({"uri": "repos/o/r/pulls?state=open&per_page=25&page=1", "status": 0, "body": body.to_string(),
        "etag": "\"l1\"", "not_modified_on_matching_validator": true})
}

fn route(uri: &str, status: u64, body: &str, etag: &str) -> Value {
    serde_json::json!({"uri": uri, "status": status, "body": body, "etag": etag,
        "not_modified_on_matching_validator": !etag.is_empty()})
}

fn observer_over(forge: &Forge) -> HostObserver<WatchHttp> {
    let reader = WatchHttp::new(
        TOKEN.to_string(),
        forge.base.clone(),
        "api.github.com".into(),
    );
    HostObserver::new(reader, Box::new(|| 1_700_000_000), 2000, vec![], vec![])
}

fn hints(cycle: u64, known: Vec<KnownPr>) -> Hints {
    Hints {
        cycle,
        detail_every: 10,
        known,
    }
}

fn standard_routes(head: &str) -> Vec<Value> {
    let detail = r#"{"mergeable":true,"mergeable_state":"clean","head":{"repo":{"full_name":"o/r"}},"base":{"repo":{"full_name":"o/r"}}}"#;
    vec![
        list_route("main", head),
        route(
            "repos/o/r/branches/main",
            200,
            &format!("{{\"commit\":{{\"sha\":\"{}\"}}}}", sha('a')),
            "\"b1\"",
        ),
        route("repos/o/r/pulls/1", 200, detail, "\"d1\""),
        route(
            &format!("repos/o/r/commits/{head}/check-runs?per_page=100&page=1"),
            200,
            r#"{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{"title":"t","summary":"s","text":"x"}}]}"#,
            "\"r1\"",
        ),
        route(
            &format!("repos/o/r/commits/{head}/statuses?per_page=100&page=1"),
            200,
            r#"[{"context":"lint","state":"success","description":"ok"}]"#,
            "\"s1\"",
        ),
    ]
}

#[test]
fn observer_over_http_reads_then_uses_validators() {
    let head = sha('1');
    let forge = Forge::start(&standard_routes(&head));
    let mut o = observer_over(&forge);
    let cancel = AtomicBool::new(false);
    let obs = o.observe("o/r", &hints(1, vec![]), None, &cancel).unwrap();
    assert_eq!(obs.default_branch, "main");
    let p = &obs.prs[0];
    assert_eq!(
        (p.number, p.author.as_str(), p.fork_known, p.fork),
        (1, "alice", true, false)
    );
    assert_eq!(p.detail.as_ref().unwrap().state, "clean");
    let c = p.checks.as_ref().unwrap();
    assert_eq!(
        (
            c.runs[0].name.as_str(),
            c.runs[0].text.as_str(),
            c.statuses[0].context.as_str()
        ),
        ("ci", "x", "lint")
    );
    let seen: Vec<String> = forge.take_log().into_iter().map(|r| r.uri).collect();
    assert_eq!(seen.len(), 5, "{seen:?}");
    // the second cycle: green is accepted, the PR is settled; the list and the tip are conditional and answered 304
    let cond = obs.cond.clone().unwrap();
    let known = vec![KnownPr {
        number: 1,
        head: head.clone(),
        class: "green".into(),
        ..KnownPr::default()
    }];
    let obs2 = o
        .observe("o/r", &hints(2, known), Some(&cond), &cancel)
        .unwrap();
    assert!(obs2.prs[0].detail.is_none() && obs2.prs[0].checks.is_none());
    let log = forge.take_log();
    let got: Vec<(String, String)> = log
        .iter()
        .map(|r| (r.uri.clone(), r.if_none_match.clone()))
        .collect();
    assert_eq!(
        got,
        vec![
            (
                "repos/o/r/pulls?state=open&per_page=25&page=1".to_string(),
                "\"l1\"".to_string()
            ),
            ("repos/o/r/branches/main".to_string(), "\"b1\"".to_string()),
        ]
    );
    // a cond that went through bytes gives the same requests
    let again = CondStore::from_json(
        &serde_json::from_slice(&serde_json::to_vec(&cond.to_json()).unwrap()).unwrap(),
    )
    .unwrap();
    assert_eq!(again, cond);
}

#[test]
fn token_never_in_output() {
    let head = sha('1');
    let mut routes = standard_routes(&head);
    routes.insert(0, serde_json::json!({"uri": "repos/o/r/pulls/9", "status": 500, "echo_authorization_header": true}));
    let forge = Forge::start(&routes);
    let reader = WatchHttp::new(
        TOKEN.to_string(),
        forge.base.clone(),
        "api.github.com".into(),
    );
    let mut texts: Vec<String> = vec![format!("{reader:?}")];
    texts.push(reader.pull_detail("o/r", 9, "").unwrap_err().to_string());
    texts.push(format!(
        "{:?}",
        reader.pull_detail("o/r", 9, "").unwrap_err()
    ));
    let mut o = HostObserver::new(reader, Box::new(|| 1_700_000_000), 2000, vec![], vec![]);
    let cancel = AtomicBool::new(false);
    let obs = o.observe("o/r", &hints(1, vec![]), None, &cancel).unwrap();
    texts.push(format!("{obs:?}"));
    texts.push(obs.cond.as_ref().unwrap().to_json().to_string());
    // a failing observation: the error text of the core
    let broken = Forge::start(&[]);
    let mut o2 = observer_over(&broken);
    let err = o2
        .observe("o/r", &hints(1, vec![]), None, &cancel)
        .unwrap_err();
    texts.push(format!("{err:?}"));
    texts.extend(o.take_diagnostics());
    for t in &texts {
        assert!(!t.contains(TOKEN), "the token is in: {t}");
        assert!(!t.contains("Bearer"), "the header is in: {t}");
    }
    // the server did see the token (the test is not vacuous)
    assert!(forge
        .take_log()
        .iter()
        .all(|r| r.authorization == format!("Bearer {TOKEN}")));
}

#[test]
fn token_stays_out_of_refused_reads() {
    for status in [401, 403, 404, 429, 301] {
        let routes = vec![serde_json::json!({
            "uri": "repos/o/r/pulls/9",
            "status": status,
            "echo_authorization_header": true,
            "headers": {"location": "https://api.github.com/repos/other/name"},
        })];
        let forge = Forge::start(&routes);
        let reader = WatchHttp::new(
            TOKEN.to_string(),
            forge.base.clone(),
            "api.github.com".into(),
        );
        let err = reader.pull_detail("o/r", 9, "").unwrap_err();
        for t in [err.to_string(), format!("{err:?}")] {
            assert!(!t.contains(TOKEN), "status {status}: {t}");
            assert!(!t.contains("Bearer"), "status {status}: {t}");
        }
        assert!(
            forge
                .take_log()
                .iter()
                .any(|r| r.authorization == format!("Bearer {TOKEN}")),
            "status {status} never reached the server"
        );
    }
}

#[test]
fn the_reader_has_no_write_path() {
    let dir = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("src/watch_reader");
    for e in std::fs::read_dir(dir).unwrap() {
        let p = e.unwrap().path();
        let text = std::fs::read_to_string(&p).unwrap();
        for forbidden in [
            ".post(",
            ".put(",
            ".patch(",
            ".delete(",
            ".head(",
            ".send(",
            ".send_json(",
            ".send_form(",
        ] {
            assert!(!text.contains(forbidden), "{p:?} has {forbidden}");
        }
    }
}

fn runs_403(head: &str) -> Value {
    route(
        &format!("repos/o/r/commits/{head}/check-runs?per_page=100&page=1"),
        403,
        r#"{"message":"Resource not accessible by personal access token"}"#,
        "",
    )
}

#[test]
fn a_token_without_checks_falls_back_to_actions_runs() {
    let head = sha('1');
    let mut routes = standard_routes(&head);
    routes.insert(0, runs_403(&head));
    routes.insert(0, route(
        &format!("repos/o/r/actions/runs?head_sha={head}&per_page=100&page=1"),
        200,
        &serde_json::json!({"total_count": 3, "workflow_runs": [
            {"id": 11, "workflow_id": 5, "name": "CI", "event": "push", "status": "completed", "conclusion": "success",
             "head_sha": head, "created_at": "2026-10-22T08:00:00Z", "run_attempt": 1},
            {"id": 12, "workflow_id": 5, "name": "CI", "event": "push", "status": "completed", "conclusion": "failure",
             "head_sha": head, "created_at": "2026-10-22T09:00:00Z", "run_attempt": 1},
            {"id": 13, "workflow_id": 6, "name": "Lint", "event": "pull_request", "status": "in_progress", "conclusion": null,
             "head_sha": head, "created_at": "2026-10-22T09:00:00Z", "run_attempt": 1}]})
        .to_string(),
        "",
    ));
    let forge = Forge::start(&routes);
    let mut o = observer_over(&forge);
    let cancel = AtomicBool::new(false);
    let obs = o.observe("o/r", &hints(1, vec![]), None, &cancel).unwrap();
    let c = obs.prs[0].checks.as_ref().unwrap();
    assert_eq!(c.via, stw::watch_reader::obs::ChecksVia::ActionsRuns);
    let names: Vec<(&str, &str, &str)> = c
        .runs
        .iter()
        .map(|r| (r.name.as_str(), r.status.as_str(), r.conclusion.as_str()))
        .collect();
    assert_eq!(
        names,
        vec![
            ("CI (push)", "completed", "failure"),
            ("Lint (pull_request)", "in_progress", "")
        ]
    );
    assert!(
        c.runs
            .iter()
            .all(|r| r.text.is_empty() && r.title.is_empty()),
        "no output text from Actions runs"
    );
    assert_eq!(
        c.statuses.len(),
        1,
        "the commit statuses are read as before"
    );
    assert_eq!(
        o.take_diagnostics(),
        vec!["watch: o/r: checks via actions-runs (the token cannot read check runs)".to_string()]
    );
    // announced once per repository, not per cycle
    let _ = o
        .observe(
            "o/r",
            &hints(2, vec![]),
            Some(obs.cond.as_ref().unwrap()),
            &cancel,
        )
        .unwrap();
    assert!(o.take_diagnostics().is_empty());
}

#[test]
fn two_heads_announce_the_actions_fallback_once() {
    let a = sha('1');
    let b = sha('2');
    let detail = r#"{"mergeable":true,"mergeable_state":"clean","head":{"repo":{"full_name":"o/r"}},"base":{"repo":{"full_name":"o/r"}}}"#;
    let list = serde_json::json!([
        {"number": 1, "title": "PR 1", "user": {"login": "alice"}, "labels": [], "draft": false,
         "head": {"sha": &a, "ref": "feat/1"}, "base": {"ref": "main", "sha": sha('a')}},
        {"number": 2, "title": "PR 2", "user": {"login": "alice"}, "labels": [], "draft": false,
         "head": {"sha": &b, "ref": "feat/2"}, "base": {"ref": "main", "sha": sha('a')}}
    ]);
    let empty_runs = r#"{"total_count":0,"workflow_runs":[]}"#;
    let routes = vec![
        serde_json::json!({"uri": "repos/o/r/pulls?state=open&per_page=25&page=1", "status": 0,
            "body": list.to_string(), "etag": "\"l1\""}),
        route(
            "repos/o/r/branches/main",
            200,
            &format!("{{\"commit\":{{\"sha\":\"{}\"}}}}", sha('a')),
            "\"b1\"",
        ),
        route("repos/o/r/pulls/1", 200, detail, "\"d1\""),
        route("repos/o/r/pulls/2", 200, detail, "\"d2\""),
        route(
            &format!("repos/o/r/commits/{a}/statuses?per_page=100&page=1"),
            200,
            "[]",
            "",
        ),
        route(
            &format!("repos/o/r/commits/{b}/statuses?per_page=100&page=1"),
            200,
            "[]",
            "",
        ),
        runs_403(&a),
        runs_403(&b),
        route(
            &format!("repos/o/r/actions/runs?head_sha={a}&per_page=100&page=1"),
            200,
            empty_runs,
            "",
        ),
        route(
            &format!("repos/o/r/actions/runs?head_sha={b}&per_page=100&page=1"),
            200,
            empty_runs,
            "",
        ),
    ];
    let forge = Forge::start(&routes);
    let mut o = observer_over(&forge);
    let cancel = AtomicBool::new(false);
    let obs = o.observe("o/r", &hints(1, vec![]), None, &cancel).unwrap();
    assert_eq!(obs.prs.len(), 2);
    assert!(obs.prs.iter().all(|p| p
        .checks
        .as_ref()
        .is_some_and(|c| { c.via == stw::watch_reader::obs::ChecksVia::ActionsRuns })));
    assert_eq!(
        o.take_diagnostics(),
        vec!["watch: o/r: checks via actions-runs (the token cannot read check runs)".to_string()]
    );
}

#[test]
fn when_check_runs_and_actions_are_both_refused_the_read_is_forbidden() {
    let head = sha('1');
    let mut routes = standard_routes(&head);
    routes.insert(0, runs_403(&head));
    routes.insert(
        0,
        route(
            &format!("repos/o/r/actions/runs?head_sha={head}&per_page=100&page=1"),
            403,
            "{}",
            "",
        ),
    );
    let forge = Forge::start(&routes);
    let mut o = observer_over(&forge);
    let cancel = AtomicBool::new(false);
    match o.observe("o/r", &hints(1, vec![]), None, &cancel) {
        Err(ObserveFailure::Error(e)) => assert_eq!(e.code, Code::Forbidden),
        other => panic!("{other:?}"),
    }
}

#[test]
fn a_404_on_check_runs_is_not_the_fallback() {
    let head = sha('1');
    let mut routes = standard_routes(&head);
    routes.insert(
        0,
        route(
            &format!("repos/o/r/commits/{head}/check-runs?per_page=100&page=1"),
            404,
            "{}",
            "",
        ),
    );
    let forge = Forge::start(&routes);
    let mut o = observer_over(&forge);
    let cancel = AtomicBool::new(false);
    match o.observe("o/r", &hints(1, vec![]), None, &cancel) {
        Err(ObserveFailure::Error(e)) => assert_eq!(e.code, Code::Forbidden),
        other => panic!("{other:?}"),
    }
    assert!(forge.take_log().iter().all(|r| !r.uri.contains("actions/")));
}

#[test]
fn a_hung_server_ends_at_the_request_timeout() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let base = format!("http://{}", listener.local_addr().unwrap());
    std::thread::spawn(move || {
        let mut held = Vec::new();
        for s in listener.incoming().flatten() {
            held.push(s); // never answers
        }
    });
    let mut reader = WatchHttp::new(TOKEN.into(), base, "api.github.com".into());
    reader
        .set_request_timeout(std::time::Duration::from_millis(300))
        .unwrap();
    let started = std::time::Instant::now();
    let e = reader.pull_detail("o/r", 1, "").unwrap_err();
    assert!(started.elapsed() < std::time::Duration::from_secs(10));
    assert_eq!(e.code, Code::Network);
    assert_eq!(e.message, "request canceled or exceeded its deadline");
    assert!(reader
        .set_request_timeout(std::time::Duration::ZERO)
        .is_err());
    assert!(reader
        .set_request_timeout(std::time::Duration::from_secs(31))
        .is_err());
    assert!(reader
        .set_request_timeout(std::time::Duration::from_secs(30))
        .is_ok());
}

#[test]
fn a_refused_connection_is_a_network_error_without_details() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let base = format!("http://{}", listener.local_addr().unwrap());
    drop(listener);
    let reader = WatchHttp::new(TOKEN.into(), base.clone(), "api.github.com".into());
    let e = reader.rate_limit().unwrap_err();
    assert_eq!(
        (e.code, e.message.as_str()),
        (Code::Network, "HTTP read failed")
    );
    assert!(
        !e.to_string().contains(&base),
        "the address is not in the error"
    );
}

#[test]
fn hostile_link_headers_are_bounded() {
    let started = std::time::Instant::now();
    let big = format!("<{}>; rel=\"next\"", "a".repeat(1 << 20));
    assert_eq!(http::next_page_from_link(&big), None);
    let commas = ",".repeat(1 << 20);
    assert_eq!(http::next_page_from_link(&commas), None);
    let many = "<x?page=2>; rel=\"prev\", ".repeat(50_000) + "<https://h/x?page=3>; rel=next";
    assert_eq!(http::next_page_from_link(&many), Some(3));
    assert!(!http::has_next_rel(&commas));
    assert_eq!(
        http::next_page_from_link("<https://h/x?page=2&page=9>; rel=next"),
        Some(2)
    );
    assert_eq!(
        http::next_page_from_link("<https://h/x?page=%32>; rel=next"),
        Some(2)
    );
    assert_eq!(
        http::next_page_from_link("<https://h/x%zz?page=2>; rel=next"),
        None
    );
    assert_eq!(
        http::next_page_from_link("<https://h/x?page=2\n>; rel=next"),
        None
    );
    assert!(started.elapsed() < std::time::Duration::from_secs(10));
}

#[test]
fn a_list_that_never_ends_stops_at_the_page_bound() {
    // every page names a following page: the reader stops with too-many-open-prs at the bound of the size
    let mut routes = Vec::new();
    for page in 1..=81u64 {
        let item = serde_json::json!([{"number": page, "title": "t", "user": {"login": "u"}, "labels": [],
            "draft": false, "head": {"sha": sha('1'), "ref": "r"}, "base": {"ref": "main", "sha": sha('a')}}]);
        routes.insert(0, serde_json::json!({"uri": format!("repos/o/r/pulls?state=open&per_page=25&page={page}"), "status": 0,
            "headers": {"Link": format!("<https://api.github.com/repositories/1/pulls?per_page=25&page={}>; rel=\"next\"", page + 1)},
            "body": item.to_string()}));
    }
    let forge = Forge::start(&routes);
    let reader = WatchHttp::new(TOKEN.into(), forge.base.clone(), "api.github.com".into());
    let e = reader
        .list_open_pulls("o/r", &PullListState::default(), 2000)
        .unwrap_err();
    assert_eq!(e.code, Code::TooManyPrs);
    assert_eq!(forge.take_log().len(), 80, "no request beyond page 80");
}

#[test]
fn observer_masks_the_untrusted_base_name_in_its_diagnostic() {
    let mut fake = FakeReader::default();
    fake.w.items.push(PullItem {
        number: 1,
        head_sha: sha('1'),
        base_ref: "we%\u{202e}ird\u{7}\"x".into(),
        head_ref: "r".into(),
        ..PullItem::default()
    });
    fake.w.details.insert(1, "{}".into());
    let swap = Swap(Rc::new(RefCell::new(Rc::new(fake))));
    let mut o = HostObserver::new(swap, Box::new(|| 0), 2000, vec![], vec![]);
    let cancel = AtomicBool::new(false);
    o.observe("o/r", &hints(1, vec![]), None, &cancel).unwrap();
    let d = o.take_diagnostics();
    assert_eq!(d.len(), 1);
    assert!(
        d[0].chars().all(|c| !c.is_control() && c != '\u{202e}'),
        "{:?}",
        d[0]
    );
    assert!(
        d[0].contains("\\u{202e}") && d[0].contains("\\u{7}") && d[0].contains("\\\"x"),
        "{:?}",
        d[0]
    );
}

#[test]
fn rate_limit_evidence_each_source_alone() {
    let neutral = r#"{"message":"nope"}"#;
    let r = |n: u64, status: u64, headers: Value, body: &str| serde_json::json!({"uri": format!("repos/a/b/pulls/{n}"), "status": status, "headers": headers, "body": body});
    let routes = vec![
        r(
            1,
            403,
            serde_json::json!({"X-RateLimit-Remaining": "0"}),
            neutral,
        ),
        r(2, 403, serde_json::json!({"Retry-After": "30"}), neutral),
        r(
            3,
            403,
            Value::Null,
            r#"{"message":"You have exceeded a secondary rate limit"}"#,
        ),
        r(
            4,
            403,
            Value::Null,
            r#"{"message":"Abuse Detection mechanism"}"#,
        ),
        r(5, 403, Value::Null, neutral),
        r(6, 429, Value::Null, neutral),
        r(7, 403, serde_json::json!({"Retry-After": "abc"}), neutral),
        r(
            8,
            403,
            serde_json::json!({"X-RateLimit-Remaining": "7"}),
            neutral,
        ),
    ];
    let forge = Forge::start(&routes);
    let reader = WatchHttp::new(TOKEN.into(), forge.base.clone(), "api.github.com".into());
    let got: Vec<(Code, u64)> = (1..=8)
        .map(|n| {
            let e = reader.pull_detail("a/b", n, "").unwrap_err();
            (e.code, e.retry_after)
        })
        .collect();
    assert_eq!(
        got,
        vec![
            (Code::RateLimited, 0),
            (Code::RateLimited, 30),
            (Code::RateLimited, 0),
            (Code::RateLimited, 0),
            (Code::Forbidden, 0),
            (Code::RateLimited, 0),
            (Code::RateLimited, 0),
            (Code::Forbidden, 0),
        ]
    );
}

#[test]
fn check_runs_and_statuses_follow_pages_and_keep_the_newest_status_of_a_context() {
    let head = sha('1');
    let run = |i: usize| serde_json::json!({"name": format!("r{i}"), "status": "completed", "conclusion": "success", "output": {}});
    let page1: Vec<Value> = (0..100).map(run).collect();
    let page2: Vec<Value> = (100..150).map(run).collect();
    let st =
        |c: &str, state: &str| serde_json::json!({"context": c, "state": state, "description": ""});
    let mut s1: Vec<Value> = (0..100).map(|i| st(&format!("s{i}"), "success")).collect();
    s1[0] = st("s0", "failure");
    let s2 = vec![st("s0", "success"), st("extra", "pending")];
    let mut routes = standard_routes(&head);
    let base = format!("repos/o/r/commits/{head}");
    routes.insert(
        0,
        route(
            &format!("{base}/check-runs?per_page=100&page=1"),
            200,
            &serde_json::json!({"total_count": 150, "check_runs": page1}).to_string(),
            "\"r1\"",
        ),
    );
    routes.insert(
        0,
        route(
            &format!("{base}/check-runs?per_page=100&page=2"),
            200,
            &serde_json::json!({"total_count": 150, "check_runs": page2}).to_string(),
            "",
        ),
    );
    routes.insert(
        0,
        route(
            &format!("{base}/statuses?per_page=100&page=1"),
            200,
            &Value::Array(s1).to_string(),
            "\"s1\"",
        ),
    );
    routes.insert(
        0,
        route(
            &format!("{base}/statuses?per_page=100&page=2"),
            200,
            &Value::Array(s2).to_string(),
            "",
        ),
    );
    let forge = Forge::start(&routes);
    let mut o = observer_over(&forge);
    let cancel = AtomicBool::new(false);
    let obs = o.observe("o/r", &hints(1, vec![]), None, &cancel).unwrap();
    let c = obs.prs[0].checks.as_ref().unwrap();
    assert_eq!(c.runs.len(), 150);
    assert_eq!(c.runs[149].name, "r149");
    assert_eq!(
        c.statuses.len(),
        101,
        "the older s0 of page 2 does not count twice"
    );
    assert_eq!(
        (c.statuses[0].context.as_str(), c.statuses[0].state.as_str()),
        ("s0", "failure")
    );
    assert_eq!(c.statuses[100].context, "extra");
    let uris: Vec<String> = forge.take_log().into_iter().map(|r| r.uri).collect();
    assert_eq!(uris.iter().filter(|u| u.ends_with("&page=2")).count(), 2);
    assert_eq!(uris.iter().filter(|u| u.ends_with("&page=3")).count(), 0);
}

#[test]
fn a_missing_base_branch_is_no_failure() {
    let head = sha('1');
    let mut routes = standard_routes(&head);
    routes.insert(
        0,
        route(
            "repos/o/r/branches/main",
            404,
            r#"{"message":"Branch not found"}"#,
            "",
        ),
    );
    let forge = Forge::start(&routes);
    let mut o = observer_over(&forge);
    let cancel = AtomicBool::new(false);
    let obs = o.observe("o/r", &hints(1, vec![]), None, &cancel).unwrap();
    assert_eq!(obs.prs.len(), 1);
    assert!(obs.cond.unwrap().tips.is_empty());
}

#[test]
fn the_class_of_a_changed_head_is_read_again_and_unchanged_heads_stay_quiet() {
    // a head that did not change and whose class is green is not read at all
    let head = sha('1');
    let forge = Forge::start(&standard_routes(&head));
    let mut o = observer_over(&forge);
    let cancel = AtomicBool::new(false);
    let known = vec![KnownPr {
        number: 1,
        head: head.clone(),
        class: "green".into(),
        ..KnownPr::default()
    }];
    let obs = o
        .observe("o/r", &hints(1, known.clone()), None, &cancel)
        .unwrap();
    assert!(obs.prs[0].checks.is_none());
    // a new head is read
    let known2 = vec![KnownPr {
        number: 1,
        head: sha('2'),
        class: "green".into(),
        ..KnownPr::default()
    }];
    let obs2 = o
        .observe("o/r", &hints(2, known2), obs.cond.as_ref(), &cancel)
        .unwrap();
    assert!(obs2.prs[0].checks.is_some() && obs2.prs[0].detail.is_some());
}

/// A server that answers every request with a chunked 200 (no `Content-Length`) of `size` bytes of `x`.
fn chunked_server(size: usize) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let base = format!("http://{}", listener.local_addr().unwrap());
    std::thread::spawn(move || {
        for stream in listener.incoming() {
            let Ok(mut s) = stream else { continue };
            let mut buf = Vec::new();
            let mut chunk = [0u8; 1024];
            while !buf.windows(4).any(|w| w == b"\r\n\r\n") {
                match s.read(&mut chunk) {
                    Ok(0) | Err(_) => break,
                    Ok(n) => buf.extend_from_slice(&chunk[..n]),
                }
            }
            let head = "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n";
            let _ = s.write_all(head.as_bytes());
            let _ = s.write_all(format!("{size:x}\r\n").as_bytes());
            let _ = s.write_all(&vec![b'x'; size]);
            let _ = s.write_all(b"\r\n0\r\n\r\n");
        }
    });
    base
}

#[test]
fn a_chunked_body_is_cut_at_one_mib_without_a_declared_length() {
    let max = stw::watch_reader::http::MAX_BODY;
    let reader = WatchHttp::new(
        TOKEN.into(),
        chunked_server(max + 1),
        "api.github.com".into(),
    );
    let e = reader.pull_detail("a/b", 1, "").unwrap_err();
    assert_eq!(e.code, Code::Malformed, "{e}");
    let e = reader.pull_list_page("a/b", 25, 1, "").unwrap_err();
    assert_eq!(e.code, Code::PageTooLarge, "{e}");
    // exactly one MiB is still an answer
    let ok = WatchHttp::new(TOKEN.into(), chunked_server(max), "api.github.com".into());
    assert_eq!(ok.pull_detail("a/b", 1, "").unwrap().body.len(), max);
}

#[test]
fn cond_store_round_trips_the_halved_page_size() {
    let mut c = sample_store();
    c.list.per_page = 12;
    assert_eq!(CondStore::from_json(&c.to_json()).unwrap(), c);
}

#[test]
fn recent_workflows_keep_a_bounded_window_across_large_pages() {
    let mut routes: Vec<_> = (1..=4)
        .map(|page| {
            let body = serde_json::json!({
                "workflow_runs": vec![serde_json::json!({"status":"queued","metadata":"x".repeat(12_000)}); 25]
            });
            serde_json::json!({
                "uri": format!("repos/o/r/actions/runs?status=failure&per_page=25&page={page}"),
                "status": 200,
                "headers": {"Link": format!("<https://api.github.com/repos/o/r/actions/runs?status=failure&per_page=25&page={}>; rel=\"next\"", page+1)},
                "body": body.to_string(),
            })
        })
        .collect();
    for status in ["queued", "in_progress"] {
        routes.push(serde_json::json!({
            "uri": format!("repos/o/r/actions/runs?status={status}&per_page=25&page=1"),
            "status": 200,
            "body": r#"{"workflow_runs":[]}"#,
        }));
    }
    let forge = Forge::start(&routes);
    let reader = WatchHttp::new(TOKEN.into(), forge.base.clone(), "api.github.com".into());
    let pages = reader.recent_workflows("o/r").unwrap();
    let bytes: usize = pages.iter().map(|page| page.body.len()).sum();
    assert!(bytes > stw::watch_reader::http::MAX_BODY);
    let runs: usize = pages
        .iter()
        .map(|page| {
            let payload: Value = serde_json::from_slice(&page.body).unwrap();
            payload["workflow_runs"].as_array().unwrap().len()
        })
        .sum();
    assert_eq!(runs, 100);
    let seen = forge.log.lock().unwrap();
    assert_eq!(seen.len(), 6);
    for (request, page) in seen.iter().skip(2).zip(1..=4) {
        assert_eq!(
            request.uri,
            format!("repos/o/r/actions/runs?status=failure&per_page=25&page={page}")
        );
    }
}

#[test]
fn workflow_poll_detects_fast_and_late_failures_once() {
    use stw::watch::cycle::{Seen, advance_seen};
    use stw::watch::event::Details;
    use stw::watch::state::State;

    let workflow_page = |status: &str, rows: Value| {
        route(
            &format!("repos/o/r/actions/runs?status={status}&per_page=25&page=1"),
            200,
            &serde_json::json!({"workflow_runs": rows}).to_string(),
            "",
        )
    };
    let failed = |id| serde_json::json!({
        "id":id,"status":"completed","conclusion":"failure","name":"CI",
        "head_sha":sha('b'),"head_branch":"main","event":"push","run_attempt":1
    });
    let forge = Forge::start(&[
        workflow_page("queued", serde_json::json!([])),
        workflow_page("in_progress", serde_json::json!([{"id":1,"status":"in_progress"}])),
        workflow_page("failure", serde_json::json!([failed(2)])),
    ]);
    let mut observer = observer_over(&forge).with_prs(false).with_workflows(true);
    let cancel = AtomicBool::new(false);
    let mut legacy_state = serde_json::to_value(State::new()).unwrap();
    legacy_state["workflows"] = serde_json::json!({
        "o/r": {"baselined":true,"seen":[[9,1]]}
    });
    let mut state: State = serde_json::from_value(legacy_state).unwrap();
    let mut poll = || {
        let observation = observer.observe("o/r", &Hints::default(), None, &cancel).unwrap();
        let active = observation.active_workflows;
        let events = advance_seen(
            &mut state,
            &[Seen { repo:"o/r".into(), result:Ok(observation) }],
            1_700_000_000,
            10,
        );
        (active, events)
    };
    let (active, events) = poll();
    assert_eq!(active, 1);
    assert!(events.is_empty());
    forge.set_routes(&[
        workflow_page("in_progress", serde_json::json!([])),
        workflow_page("failure", serde_json::json!([failed(3), failed(2)])),
        route("repos/o/r/actions/runs/1", 200, &failed(1).to_string(), ""),
    ]);
    let (active, events) = poll();
    assert_eq!(active, 0);
    let ids: std::collections::BTreeSet<_> = events.iter().map(|event| match &event.details {
        Details::WorkflowCompleted(run) => run.run_id,
        other => panic!("unexpected event: {other:?}"),
    }).collect();
    assert_eq!(events.len(), 2);
    assert_eq!(ids, [1, 3].into());
    assert!(poll().1.is_empty());
    assert_eq!(forge.take_log().iter().filter(|r| r.uri == "repos/o/r/actions/runs/1").count(), 1);
}

#[test]
fn graph_check_rollup_reads_exact_head_and_falls_back_on_partial_data() {
    let head = sha('1');
    let graph_reply = serde_json::json!({
        "data":{"repository":{"object":{"oid":head,"statusCheckRollup":{"contexts":{
            "pageInfo":{"hasNextPage":false,"endCursor":null},
            "nodes":[
                {"__typename":"CheckRun","name":"CI","status":"COMPLETED","conclusion":"FAILURE"},
                {"__typename":"StatusContext","context":"deploy","state":"PENDING"}
            ]
        }}}}}
    });
    let mut routes = standard_routes(&head);
    routes.push(route("graphql", 200, &graph_reply.to_string(), ""));
    let forge = Forge::start(&routes);
    let observer = || observer_over(&forge).with_graph_sources(
        stw::github::Client::with_base(TOKEN.into(), stw::auth::TokenSource::Env, forge.base.clone()),
        false, false, false,
    );
    let cancel = AtomicBool::new(false);
    let result = observer().observe("o/r", &hints(1, vec![]), None, &cancel).unwrap();
    let checks = result.prs[0].checks.as_ref().unwrap();
    assert_eq!(checks.via, stw::watch_reader::obs::ChecksVia::Graphql);
    assert_eq!(checks.runs[0].conclusion, "failure");
    assert_eq!(checks.statuses[0].state, "pending");
    let log = forge.take_log();
    assert!(log.iter().any(|r| r.method == "POST" && r.uri == "graphql"));
    assert!(!log.iter().any(|r| r.uri.contains("/check-runs?")));
    let mut partial = graph_reply;
    partial["errors"] = serde_json::json!([{"message":"partial response"}]);
    forge.set_routes(&[route("graphql", 200, &partial.to_string(), "")]);
    let result = observer().observe("o/r", &hints(1, vec![]), None, &cancel).unwrap();
    assert_eq!(result.prs[0].checks.as_ref().unwrap().via, stw::watch_reader::obs::ChecksVia::CheckRuns);
    assert!(forge.take_log().iter().any(|r| r.uri.contains("/check-runs?")));
}
