//! W2 and W3 before the command is trusted: a transient read keeps the accepted list, and a
//! restart reprints an event that was journalled and not yet printed.
use stw::watch::cycle::{self, Poll};
use stw::watch::event::Kind;
use stw::watch::reader::{self, HttpGet, HttpResponse, OpenPr, ReadFailure};
use stw::watch::state::State;
use stw::watch::{Core, Output, Step, WatchError};

fn pr(number: u64, head: &str) -> OpenPr {
    OpenPr {
        number,
        title: format!("t{number}"),
        author: "ada".into(),
        draft: false,
        base: "main".into(),
        head_ref: "topic".into(),
        head: head.into(),
        fork: false,
    }
}

struct OneGet {
    status: u16,
    body: Vec<u8>,
}

impl HttpGet for OneGet {
    fn get(&self, _url: &str) -> Result<HttpResponse, ReadFailure> {
        Ok(HttpResponse {
            status: self.status,
            body: self.body.clone(),
            retry_after_seconds: None,
            remaining_zero: false,
            rate_reset: None,
        })
    }
}

#[test]
fn a_truncated_or_failed_status_does_not_replace_the_open_list() {
    assert!(matches!(
        reader::classify_status(503, None, false, None, b""),
        ReadFailure::Transient { code, .. } if code == "http-5xx"
    ));
    let body = br#"[{"number":7,"title":"T","draft":false,"user":{"login":"ada"},"base":{"ref":"main","repo":{"full_name":"o/r"}},"head":{"ref":"topic","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","repo":{"full_name":"o/r"}}}]"#;
    let http = OneGet {
        status: 200,
        body: body.to_vec(),
    };
    let prs = reader::read_open_pulls(&http, "https://api.github.com", "o/r").unwrap();
    assert_eq!(prs.len(), 1);

    let mut state = State::new();
    let poll = Poll {
        repo: "o/r".into(),
        result: Ok(vec![pr(7, &"a".repeat(40))]),
    };
    let events = cycle::advance(&mut state, &[poll], 1_700_000_000);
    assert!(events.is_empty(), "the first read is a silent baseline");
    assert!(state.repo["o/r"].baselined);

    let failed = Poll {
        repo: "o/r".into(),
        result: Err(ReadFailure::Transient {
            code: "http-5xx".into(),
            retry_in_seconds: 30,
            retry_until: None,
        }),
    };
    let events = cycle::advance(&mut state, &[failed], 1_700_000_060);
    assert_eq!(events[0].kind(), Kind::WatchError);
    assert_eq!(state.repo["o/r"].prs[&7].head, "a".repeat(40));

    let changed = Poll {
        repo: "o/r".into(),
        result: Ok(vec![pr(7, &"b".repeat(40))]),
    };
    let events = cycle::advance(&mut state, &[changed], 1_700_000_120);
    assert_eq!(events[0].kind(), Kind::WatchRecovered);
    assert_eq!(events[1].kind(), Kind::HeadChanged);
    assert_eq!(state.repo["o/r"].prs[&7].head, "b".repeat(40));
}

struct StopAfterAppend {
    lines: Vec<String>,
}

impl Output for StopAfterAppend {
    fn print(&mut self, ev: &stw::watch::event::Event) -> Result<(), WatchError> {
        self.lines.push(ev.to_line());
        Ok(())
    }

    fn after(&mut self, step: Step) -> Result<(), WatchError> {
        if step == Step::Append(1) {
            return Err(WatchError::Io("killed after the journal append".into()));
        }
        Ok(())
    }
}

#[test]
fn a_restart_reprints_the_event_journalled_before_the_crash() {
    let (_dir, path) = state_path();
    assert!(path.is_absolute());

    let mut state = State::new();
    cycle::advance(
        &mut state,
        &[Poll {
            repo: "o/r".into(),
            result: Ok(vec![pr(7, &"a".repeat(40))]),
        }],
        1_700_000_000,
    );
    let events = cycle::advance(
        &mut state,
        &[Poll {
            repo: "o/r".into(),
            result: Ok(vec![pr(7, &"b".repeat(40))]),
        }],
        1_700_000_060,
    );
    assert_eq!(events.len(), 1);

    let mut out = StopAfterAppend { lines: Vec::new() };
    let (mut core, _) = Core::open(&path, false, &mut out).unwrap();
    core.state = state;
    core.commit(&[], &mut out).unwrap();
    let err = core.commit(&events, &mut out).unwrap_err();
    assert!(err.to_string().contains("killed"));
    drop(core);

    let mut again = StopAfterAppend { lines: Vec::new() };
    let (_core, report) = Core::open(&path, false, &mut again).unwrap();
    assert!(report.reprinted >= 1, "{report:?}");
    assert!(
        again.lines.iter().any(|l| l.contains("head-changed")),
        "{:?}",
        again.lines
    );
    assert!(
        out.lines.is_empty(),
        "the crash is before print: {out:?}",
        out = out.lines
    );
}

fn body(head: &str) -> Vec<u8> {
    format!(
        r#"[{{"number":7,"title":"T","draft":false,"user":{{"login":"ada"}},"base":{{"ref":"main","repo":{{"full_name":"o/r"}}}},"head":{{"ref":"topic","sha":"{head}","repo":{{"full_name":"o/r"}}}}}}]"#
    )
    .into_bytes()
}

struct FixedGet(Vec<u8>);

impl HttpGet for FixedGet {
    fn get(&self, _url: &str) -> Result<HttpResponse, ReadFailure> {
        Ok(HttpResponse {
            status: 200,
            body: self.0.clone(),
            retry_after_seconds: None,
            remaining_zero: false,
            rate_reset: None,
        })
    }
}

struct CountingGet {
    hits: std::cell::Cell<usize>,
}

impl HttpGet for CountingGet {
    fn get(&self, _url: &str) -> Result<HttpResponse, ReadFailure> {
        let n = self.hits.get();
        self.hits.set(n + 1);
        let head = if n == 0 {
            "a".repeat(40)
        } else {
            "b".repeat(40)
        };
        Ok(HttpResponse {
            status: 200,
            body: body(&head),
            retry_after_seconds: None,
            remaining_zero: false,
            rate_reset: None,
        })
    }
}

fn state_path() -> (tempfile::TempDir, std::path::PathBuf) {
    let dir = crate::watch_core::tmp();
    let path = std::fs::canonicalize(dir.path()).unwrap().join("state");
    (dir, path)
}

#[test]
fn baseline_replaces_the_open_list_without_events() {
    let (_dir, path) = state_path();
    let api = "https://api.github.com";
    let repo = vec!["o/r".to_string()];
    let mut out = Vec::new();
    stw::watch::command::run_once(
        &path,
        &repo,
        false,
        1_700_000_000,
        &FixedGet(body(&"a".repeat(40))),
        api,
        &mut out,
    )
    .unwrap();
    assert!(out.is_empty(), "{}", String::from_utf8_lossy(&out));

    out.clear();
    stw::watch::command::run_once(
        &path,
        &repo,
        false,
        1_700_000_060,
        &FixedGet(body(&"b".repeat(40))),
        api,
        &mut out,
    )
    .unwrap();
    let changed = String::from_utf8(out.clone()).unwrap();
    assert!(changed.contains("head-changed"), "{changed}");

    out.clear();
    stw::watch::command::run_once(
        &path,
        &repo,
        true,
        1_700_000_120,
        &FixedGet(body(&"c".repeat(40))),
        api,
        &mut out,
    )
    .unwrap();
    assert!(
        out.is_empty(),
        "a forced baseline is silent: {}",
        String::from_utf8_lossy(&out)
    );

    out.clear();
    stw::watch::command::run_once(
        &path,
        &repo,
        false,
        1_700_000_180,
        &FixedGet(body(&"d".repeat(40))),
        api,
        &mut out,
    )
    .unwrap();
    let next = String::from_utf8(out).unwrap();
    assert!(next.contains(&"c".repeat(40)), "{next}");
    assert!(next.contains("head-changed"), "{next}");

    let mut status = Vec::new();
    stw::watch::command::run_status(&path, 1_700_000_180, &mut status).unwrap();
    let text = String::from_utf8(status).unwrap();
    assert!(text.contains("\"repo\":\"o/r\""), "{text}");
}

struct Clock {
    now: i64,
    sleeps: Vec<u64>,
    stop_at: Option<usize>,
}

impl stw::watch::command::Pace for Clock {
    fn now_secs(&mut self) -> i64 {
        self.now
    }

    fn wait(&mut self, seconds: u64) -> bool {
        self.sleeps.push(seconds);
        self.now += i64::try_from(seconds).unwrap_or(i64::MAX);
        !matches!(self.stop_at, Some(n) if self.sleeps.len() == n)
    }
}

#[test]
fn a_stream_stops_on_interrupt_and_on_max_idle() {
    let api = "https://api.github.com";
    let repo = vec!["o/r".to_string()];
    let limits = stw::watch::command::StreamLimits {
        baseline: false,
        api_base: api.to_string(),
        interval_secs: 60,
        max_idle_secs: 0,
    };

    let (_dir, path) = state_path();
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: Some(1),
    };
    let mut out = Vec::new();
    let code = stw::watch::command::run_stream(
        &path,
        &repo,
        &FixedGet(body(&"a".repeat(40))),
        &mut out,
        limits,
        &mut pace,
    )
    .unwrap();
    assert_eq!(code, 0);
    assert_eq!(pace.sleeps, vec![60]);
    assert!(out.is_empty(), "{}", String::from_utf8_lossy(&out));

    let (_dir, path) = state_path();
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: None,
    };
    let mut out = Vec::new();
    let code = stw::watch::command::run_stream(
        &path,
        &repo,
        &FixedGet(body(&"a".repeat(40))),
        &mut out,
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: api.to_string(),
            interval_secs: 60,
            max_idle_secs: 150,
        },
        &mut pace,
    )
    .unwrap();
    assert_eq!(code, 0);
    assert_eq!(pace.sleeps.iter().sum::<u64>(), 150, "{:?}", pace.sleeps);
    assert!(out.is_empty(), "{}", String::from_utf8_lossy(&out));

    let (_dir, path) = state_path();
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: None,
    };
    let mut out = Vec::new();
    let code = stw::watch::command::run_stream(
        &path,
        &repo,
        &CountingGet {
            hits: std::cell::Cell::new(0),
        },
        &mut out,
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: api.to_string(),
            interval_secs: 60,
            max_idle_secs: 100,
        },
        &mut pace,
    )
    .unwrap();
    assert_eq!(code, 0);
    let text = String::from_utf8(out).unwrap();
    assert!(text.contains("head-changed"), "{text}");
    assert_eq!(pace.sleeps.iter().sum::<u64>(), 160, "{:?}", pace.sleeps);

    let (_dir, path) = state_path();
    let http = CountingGet {
        hits: std::cell::Cell::new(0),
    };
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: None,
    };
    let mut out = Vec::new();
    let code = stw::watch::command::run_stream(
        &path,
        &repo,
        &http,
        &mut out,
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: api.to_string(),
            interval_secs: 60,
            max_idle_secs: 10,
        },
        &mut pace,
    )
    .unwrap();
    assert_eq!(code, 0);
    assert_eq!(
        http.hits.get(),
        1,
        "the idle deadline ends the stream before another GET"
    );
    assert!(out.is_empty(), "{}", String::from_utf8_lossy(&out));
    assert_eq!(pace.sleeps, vec![10]);
}

#[test]
fn duration_flags_reject_a_missing_unit_and_a_short_interval() {
    assert_eq!(
        stw::watch::command::parse_duration_secs("1m30s").unwrap(),
        90
    );
    assert_eq!(stw::watch::command::parse_duration_secs("0").unwrap(), 0);
    assert!(stw::watch::command::parse_duration_secs("60").is_err());
    assert!(stw::watch::command::parse_duration_secs("1ms").is_err());
}

struct RateGet {
    hits: std::cell::Cell<usize>,
}

impl HttpGet for RateGet {
    fn get(&self, _url: &str) -> Result<HttpResponse, ReadFailure> {
        let n = self.hits.get();
        self.hits.set(n + 1);
        if n == 0 {
            Ok(HttpResponse {
                status: 403,
                body: br#"{"message":"You have exceeded a secondary rate limit"}"#.to_vec(),
                retry_after_seconds: Some(120),
                remaining_zero: false,
                rate_reset: None,
            })
        } else {
            Ok(HttpResponse {
                status: 200,
                body: body(&"a".repeat(40)),
                retry_after_seconds: None,
                remaining_zero: false,
                rate_reset: None,
            })
        }
    }
}

#[test]
fn a_rate_limited_403_holds_the_repository_until_retry_after() {
    let limited = reader::classify_status(
        403,
        Some(120),
        false,
        None,
        br#"{"message":"You have exceeded a secondary rate limit"}"#,
    );
    assert!(matches!(
        limited,
        ReadFailure::Transient { code, retry_in_seconds: 120, .. } if code == "rate-limited"
    ));
    let plain = reader::classify_status(
        403,
        None,
        false,
        None,
        br#"{"message":"Resource not accessible"}"#,
    );
    assert!(matches!(plain, ReadFailure::Fatal { code } if code == "forbidden"));
    let exhausted = reader::classify_status(403, None, true, None, b"{}");
    assert!(matches!(
        exhausted,
        ReadFailure::Transient { code, retry_in_seconds: 60, retry_until: None, .. } if code == "rate-limited"
    ));

    let api = "https://api.github.com";
    let repo = vec!["o/r".to_string()];
    let (_dir, path) = state_path();
    let http = RateGet {
        hits: std::cell::Cell::new(0),
    };
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: Some(13),
    };
    let mut out = Vec::new();
    let code = stw::watch::command::run_stream(
        &path,
        &repo,
        &http,
        &mut out,
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: api.to_string(),
            interval_secs: 10,
            max_idle_secs: 0,
        },
        &mut pace,
    )
    .unwrap();
    assert_eq!(code, 0);
    assert_eq!(
        http.hits.get(),
        2,
        "a 120s retry skips the polls inside the window: {:?}",
        pace.sleeps
    );
}

struct Moving {
    now: std::rc::Rc<std::cell::Cell<i64>>,
    sleeps: Vec<u64>,
    stop_at: Option<usize>,
}

impl stw::watch::command::Pace for Moving {
    fn now_secs(&mut self) -> i64 {
        self.now.get()
    }

    fn wait(&mut self, seconds: u64) -> bool {
        self.sleeps.push(seconds);
        self.now.set(
            self.now
                .get()
                .saturating_add(i64::try_from(seconds).unwrap_or(0)),
        );
        !matches!(self.stop_at, Some(n) if self.sleeps.len() == n)
    }
}

struct SlowLimit {
    now: std::rc::Rc<std::cell::Cell<i64>>,
    starts: std::cell::RefCell<Vec<i64>>,
    delay: i64,
    retry_after: Option<u64>,
    reset: Option<i64>,
}

impl HttpGet for SlowLimit {
    fn get(&self, _url: &str) -> Result<HttpResponse, ReadFailure> {
        let started = self.now.get();
        self.starts.borrow_mut().push(started);
        self.now.set(started.saturating_add(self.delay));
        if self.starts.borrow().len() == 1 {
            Ok(HttpResponse {
                status: 429,
                body: b"{}".to_vec(),
                retry_after_seconds: self.retry_after,
                remaining_zero: self.reset.is_some(),
                rate_reset: self.reset,
            })
        } else {
            Ok(HttpResponse {
                status: 200,
                body: body(&"a".repeat(40)),
                retry_after_seconds: None,
                remaining_zero: false,
                rate_reset: None,
            })
        }
    }
}

#[test]
fn a_slow_429_holds_from_the_response_not_from_the_request_start() {
    let now = std::rc::Rc::new(std::cell::Cell::new(1_700_000_000));
    let http = SlowLimit {
        now: std::rc::Rc::clone(&now),
        starts: std::cell::RefCell::new(Vec::new()),
        delay: 20,
        retry_after: Some(30),
        reset: None,
    };
    let (_dir, path) = state_path();
    let mut pace = Moving {
        now: std::rc::Rc::clone(&now),
        sleeps: Vec::new(),
        stop_at: Some(4),
    };
    let code = stw::watch::command::run_stream(
        &path,
        &["o/r".to_string()],
        &http,
        &mut Vec::new(),
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: "https://api.github.com".to_string(),
            interval_secs: 10,
            max_idle_secs: 0,
        },
        &mut pace,
    )
    .unwrap();
    let starts = http.starts.borrow().clone();
    assert_eq!(code, 0);
    assert_eq!(starts.len(), 2, "{starts:?}");
    assert!(
        starts[1] >= starts[0] + 50,
        "Retry-After starts when the 429 arrives: {starts:?}"
    );
}

#[test]
fn a_primary_limit_waits_until_the_reset_header() {
    let start = 1_700_000_000_i64;
    let reset = start + 300;
    let limited = reader::classify_status(403, None, true, Some(reset), b"{}");
    assert!(matches!(
        limited,
        ReadFailure::Transient { retry_until: Some(at), .. } if at == reset
    ));
    let now = std::rc::Rc::new(std::cell::Cell::new(start));
    let http = SlowLimit {
        now: std::rc::Rc::clone(&now),
        starts: std::cell::RefCell::new(Vec::new()),
        delay: 0,
        retry_after: None,
        reset: Some(reset),
    };
    let (_dir, path) = state_path();
    let mut pace = Moving {
        now,
        sleeps: Vec::new(),
        stop_at: Some(7),
    };
    stw::watch::command::run_stream(
        &path,
        &["o/r".to_string()],
        &http,
        &mut Vec::new(),
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: "https://api.github.com".to_string(),
            interval_secs: 10,
            max_idle_secs: 0,
        },
        &mut pace,
    )
    .unwrap();
    assert_eq!(
        http.starts.borrow().len(),
        1,
        "a reset 300s out must outlast the 60s fallback: {:?}",
        http.starts.borrow()
    );
}
