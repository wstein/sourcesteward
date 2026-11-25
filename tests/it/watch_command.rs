//! W2 and W3 before the command is trusted: a transient read keeps the accepted list, and a
//! restart reprints an event that was journalled and not yet printed.
use stw::watch::cycle::{self, Poll};
use stw::watch::event::Kind;
use stw::watch::reader::{self, HttpGet, HttpResponse, OpenPr, ReadFailure};
use stw::watch::state::State;
use stw::watch::{Core, Output, Step, WatchError};

struct Listed {
    head: String,
    next_head: Option<String>,
    hits: std::cell::Cell<usize>,
    error_first: Option<stw::watch_reader::obs::ObserveError>,
    clock: Option<std::rc::Rc<std::cell::Cell<i64>>>,
    delay: i64,
    starts: std::cell::RefCell<Vec<i64>>,
    cond: Option<stw::watch_reader::cond_store::CondStore>,
    saw_cond: std::cell::Cell<bool>,
    slow: bool,
}

impl Listed {
    fn head(head: &str) -> Self {
        Self {
            head: head.to_string(),
            next_head: None,
            hits: std::cell::Cell::new(0),
            error_first: None,
            clock: None,
            delay: 0,
            starts: std::cell::RefCell::new(Vec::new()),
            cond: None,
            saw_cond: std::cell::Cell::new(false),
            slow: false,
        }
    }

    fn observation(&self) -> stw::watch_reader::obs::RepoObservation {
        let n = self.hits.get();
        let head = if n > 1 {
            self.next_head.clone().unwrap_or_else(|| self.head.clone())
        } else {
            self.head.clone()
        };
        stw::watch_reader::obs::RepoObservation {
            prs: vec![stw::watch_reader::obs::PrObs {
                number: 7,
                title: "t7".into(),
                author: "ada".into(),
                base: "main".into(),
                head_ref: "topic".into(),
                head,
                ..stw::watch_reader::obs::PrObs::default()
            }],
            cond: self.cond.clone(),
            ..stw::watch_reader::obs::RepoObservation::default()
        }
    }
}

impl stw::watch::command::WatchSource for Listed {
    fn observe_repo(
        &mut self,
        _repo: &str,
        _hints: &stw::watch_reader::obs::Hints,
        cond: Option<&stw::watch_reader::cond_store::CondStore>,
    ) -> Result<stw::watch_reader::obs::RepoObservation, stw::watch_reader::obs::ObserveFailure>
    {
        if let Some(clock) = &self.clock {
            self.starts.borrow_mut().push(clock.get());
            clock.set(clock.get().saturating_add(self.delay));
        }
        self.hits.set(self.hits.get() + 1);
        if cond.is_some() {
            self.saw_cond.set(true);
        }
        if self.hits.get() == 1 {
            if let Some(err) = self.error_first.clone() {
                return Err(stw::watch_reader::obs::ObserveFailure::Error(err));
            }
        }
        Ok(self.observation())
    }

    fn after_cycle(&mut self) -> bool {
        self.slow
    }
}

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

fn state_path() -> (tempfile::TempDir, std::path::PathBuf) {
    let dir = crate::watch_core::tmp();
    let path = std::fs::canonicalize(dir.path()).unwrap().join("state");
    (dir, path)
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

#[test]
fn baseline_replaces_the_open_list_without_events() {
    let (_dir, path) = state_path();
    let repo = ["o/r".to_string()];
    let mut out = Vec::new();
    stw::watch::command::run_once(
        &path,
        &repo,
        false,
        1_700_000_000,
        10,
        &mut Listed::head(&"a".repeat(40)),
        &mut out,
    )
    .unwrap();
    assert!(out.is_empty(), "{}", String::from_utf8_lossy(&out));

    stw::watch::command::run_once(
        &path,
        &repo,
        false,
        1_700_000_060,
        10,
        &mut Listed::head(&"b".repeat(40)),
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
        10,
        &mut Listed::head(&"c".repeat(40)),
        &mut out,
    )
    .unwrap();
    assert!(out.is_empty(), "{}", String::from_utf8_lossy(&out));

    stw::watch::command::run_once(
        &path,
        &repo,
        false,
        1_700_000_180,
        10,
        &mut Listed::head(&"d".repeat(40)),
        &mut out,
    )
    .unwrap();
    let next = String::from_utf8(out).unwrap();
    assert!(
        next.contains(&"c".repeat(40)) && next.contains("head-changed"),
        "{next}"
    );

    let mut status = Vec::new();
    stw::watch::command::run_status(&path, 1_700_000_180, &mut status).unwrap();
    assert!(String::from_utf8(status)
        .unwrap()
        .contains("\"repo\":\"o/r\""));
}

#[test]
fn a_stream_stops_on_interrupt_and_on_max_idle() {
    let repo = ["o/r".to_string()];
    let limits = |idle| stw::watch::command::StreamLimits {
        baseline: false,
        api_base: "https://api.github.com".into(),
        interval_secs: 60,
        max_idle_secs: idle,
        detail_every: 10,
        until: Vec::new(),
        timeout_secs: 0,
    };
    let (_dir, path) = state_path();
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: Some(1),
    };
    let code = stw::watch::command::run_stream(
        &path,
        &repo,
        &mut Listed::head(&"a".repeat(40)),
        &mut Vec::new(),
        limits(0),
        &mut pace,
    )
    .unwrap();
    assert_eq!((code, pace.sleeps.as_slice()), (0, &[60][..]));

    let (_dir, path) = state_path();
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: None,
    };
    let code = stw::watch::command::run_stream(
        &path,
        &repo,
        &mut Listed::head(&"a".repeat(40)),
        &mut Vec::new(),
        limits(150),
        &mut pace,
    )
    .unwrap();
    assert_eq!(code, 0);
    assert_eq!(pace.sleeps.iter().sum::<u64>(), 150);

    let (_dir, path) = state_path();
    let mut changed = Listed::head(&"a".repeat(40));
    changed.next_head = Some("b".repeat(40));
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: None,
    };
    let mut out = Vec::new();
    stw::watch::command::run_stream(&path, &repo, &mut changed, &mut out, limits(100), &mut pace)
        .unwrap();
    assert!(String::from_utf8(out).unwrap().contains("head-changed"));
    assert_eq!(pace.sleeps.iter().sum::<u64>(), 160);

    let (_dir, path) = state_path();
    let mut once = Listed::head(&"a".repeat(40));
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: None,
    };
    stw::watch::command::run_stream(
        &path,
        &repo,
        &mut once,
        &mut Vec::new(),
        limits(10),
        &mut pace,
    )
    .unwrap();
    assert_eq!(once.hits.get(), 1);
    assert_eq!(pace.sleeps, vec![10]);
}

#[test]
fn a_low_rate_limit_doubles_the_stream_interval() {
    let mut src = Listed::head(&"a".repeat(40));
    src.slow = true;
    let (_dir, path) = state_path();
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: Some(1),
    };
    stw::watch::command::run_stream(
        &path,
        &["o/r".to_string()],
        &mut src,
        &mut Vec::new(),
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: String::new(),
            interval_secs: 60,
            max_idle_secs: 0,
            detail_every: 10,
            until: Vec::new(),
            timeout_secs: 0,
        },
        &mut pace,
    )
    .unwrap();
    assert_eq!(pace.sleeps, vec![120]);
}

#[test]
fn until_event_exits_when_the_kind_is_printed() {
    let mut src = Listed::head(&"a".repeat(40));
    src.next_head = Some("b".repeat(40));
    let (_dir, path) = state_path();
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: None,
    };
    let code = stw::watch::command::run_stream(
        &path,
        &["o/r".to_string()],
        &mut src,
        &mut Vec::new(),
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: String::new(),
            interval_secs: 60,
            max_idle_secs: 0,
            detail_every: 10,
            until: vec!["head-changed".into()],
            timeout_secs: 590,
        },
        &mut pace,
    )
    .unwrap();
    assert_eq!(code, 0);
    assert_eq!(src.hits.get(), 2);
    assert_eq!(pace.sleeps, vec![60]);
}

#[test]
fn watch_toml_names_repos_interval_and_ignores() {
    let file = stw::watch::watch_toml::parse_watch_toml(
        r#"
schema_version = 1
repos = ["o/r", "o/s"]
interval = "90s"
max_open_prs = 50
detail_every = 4
[ignore]
authors = ["bot"]
labels = ["wip"]
"#,
    )
    .unwrap();
    assert_eq!(file.repos, vec!["o/r", "o/s"]);
    assert_eq!(file.interval_secs, Some(90));
    assert_eq!(file.max_open_prs, Some(50));
    assert_eq!(file.detail_every, Some(4));
    assert_eq!(file.ignore_authors, vec!["bot"]);
    assert_eq!(file.ignore_labels, vec!["wip"]);
    assert!(stw::watch::watch_toml::parse_watch_toml("repos = [\"o/r\"]").is_err());
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

#[test]
fn a_rate_limited_read_holds_the_repository() {
    let mut http = Listed::head(&"a".repeat(40));
    http.error_first = Some(stw::watch_reader::obs::ObserveError {
        code: stw::watch_reader::http::Code::RateLimited,
        message: "rate limit reached".into(),
        run_wide: false,
        retry_in: 120,
    });
    let (_dir, path) = state_path();
    let mut pace = Clock {
        now: 1_700_000_000,
        sleeps: Vec::new(),
        stop_at: Some(13),
    };
    stw::watch::command::run_stream(
        &path,
        &["o/r".to_string()],
        &mut http,
        &mut Vec::new(),
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: "https://api.github.com".into(),
            interval_secs: 10,
            max_idle_secs: 0,
            detail_every: 10,
            until: Vec::new(),
            timeout_secs: 0,
        },
        &mut pace,
    )
    .unwrap();
    assert_eq!(http.hits.get(), 2, "{:?}", pace.sleeps);
}

#[test]
fn a_slow_429_holds_from_the_response_not_from_the_request_start() {
    let now = std::rc::Rc::new(std::cell::Cell::new(1_700_000_000));
    let mut http = Listed::head(&"a".repeat(40));
    http.clock = Some(std::rc::Rc::clone(&now));
    http.delay = 20;
    http.error_first = Some(stw::watch_reader::obs::ObserveError {
        code: stw::watch_reader::http::Code::RateLimited,
        message: "rate limit reached".into(),
        run_wide: false,
        retry_in: 30,
    });
    let (_dir, path) = state_path();
    let mut pace = Moving {
        now: std::rc::Rc::clone(&now),
        sleeps: Vec::new(),
        stop_at: Some(4),
    };
    let code = stw::watch::command::run_stream(
        &path,
        &["o/r".to_string()],
        &mut http,
        &mut Vec::new(),
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: "https://api.github.com".to_string(),
            interval_secs: 10,
            max_idle_secs: 0,
            detail_every: 10,
            until: Vec::new(),
            timeout_secs: 0,
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
    let mut http = Listed::head(&"a".repeat(40));
    http.error_first = Some(stw::watch_reader::obs::ObserveError {
        code: stw::watch_reader::http::Code::RateLimited,
        message: "rate limit reached".into(),
        run_wide: true,
        retry_in: 300,
    });
    let (_dir, path) = state_path();
    let mut pace = Clock {
        now: start,
        sleeps: Vec::new(),
        stop_at: Some(7),
    };
    stw::watch::command::run_stream(
        &path,
        &["o/r".to_string()],
        &mut http,
        &mut Vec::new(),
        stw::watch::command::StreamLimits {
            baseline: false,
            api_base: "https://api.github.com".to_string(),
            interval_secs: 10,
            max_idle_secs: 0,
            detail_every: 10,
            until: Vec::new(),
            timeout_secs: 0,
        },
        &mut pace,
    )
    .unwrap();
    assert_eq!(
        http.hits.get(),
        1,
        "a reset 300s out must outlast the 60s fallback"
    );
}

#[test]
fn observations_emit_checks_merge_and_restack_and_the_store_survives() {
    use stw::watch::cycle::{self, Seen};
    use stw::watch_reader::obs::{
        CheckRun, ChecksObs, DetailObs, GoneObs, MethodHint, PrObs, RepoObservation,
    };

    let head = "a".repeat(40);
    let mut state = State::new();
    let first = RepoObservation {
        prs: vec![PrObs {
            number: 7,
            title: "t".into(),
            author: "ada".into(),
            head_ref: "topic".into(),
            head: head.clone(),
            base: "main".into(),
            ..PrObs::default()
        }],
        cond: Some(stw::watch_reader::cond_store::CondStore::default()),
        ..RepoObservation::default()
    };
    let events = cycle::advance_seen(
        &mut state,
        &[Seen {
            repo: "o/r".into(),
            result: Ok(first),
        }],
        1_700_000_000,
    );
    assert!(
        events.is_empty(),
        "the first observation is a silent baseline"
    );
    assert!(state.repo["o/r"].conditional.is_some());

    let mut child = PrObs {
        number: 8,
        title: "child".into(),
        author: "ada".into(),
        head_ref: "child".into(),
        head: "b".repeat(40),
        base: "topic".into(),
        checks: Some(ChecksObs {
            runs: vec![CheckRun {
                name: "ci".into(),
                status: "completed".into(),
                conclusion: "success".into(),
                ..CheckRun::default()
            }],
            ..ChecksObs::default()
        }),
        detail: Some(DetailObs {
            mergeable: Some(false),
            state: "dirty".into(),
        }),
        ..PrObs::default()
    };
    let _ = &mut child;
    let second = RepoObservation {
        prs: vec![child],
        gone: [(
            7,
            GoneObs {
                merged: true,
                merge_commit: Some("c".repeat(40)),
                merged_at: "2026-11-25T00:00:00Z".into(),
                base: "main".into(),
                head: head.clone(),
                head_ref: "topic".into(),
                method_hint: MethodHint::Merge,
            },
        )]
        .into_iter()
        .collect(),
        cond: Some(stw::watch_reader::cond_store::CondStore::default()),
        ..RepoObservation::default()
    };
    let events = cycle::advance_seen(
        &mut state,
        &[Seen {
            repo: "o/r".into(),
            result: Ok(second),
        }],
        1_700_000_060,
    );
    let kinds: Vec<_> = events.iter().map(|e| e.kind()).collect();
    assert!(kinds.contains(&Kind::Merged), "{kinds:?}");
    assert!(kinds.contains(&Kind::RestackNeeded), "{kinds:?}");
    assert!(kinds.contains(&Kind::ChecksGreen), "{kinds:?}");
    assert!(kinds.contains(&Kind::MergeableChanged), "{kinds:?}");

    let (_dir, path) = state_path();
    let mut src = Listed::head(&head);
    src.cond = Some(stw::watch_reader::cond_store::CondStore::default());
    stw::watch::command::run_once(
        &path,
        &["o/r".to_string()],
        false,
        1_700_000_000,
        10,
        &mut src,
        &mut Vec::new(),
    )
    .unwrap();
    let mut again = Listed::head(&head);
    stw::watch::command::run_once(
        &path,
        &["o/r".to_string()],
        false,
        1_700_000_060,
        10,
        &mut again,
        &mut Vec::new(),
    )
    .unwrap();
    assert!(
        again.saw_cond.get(),
        "the validator store is handed back on the next start"
    );
}
