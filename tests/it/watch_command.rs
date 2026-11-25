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
        })
    }
}

#[test]
fn a_truncated_or_failed_status_does_not_replace_the_open_list() {
    assert!(matches!(
        reader::classify_status(503, None),
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
        if step == Step::Print(1) {
            return Err(WatchError::Io("killed after the journal append".into()));
        }
        Ok(())
    }
}

#[test]
fn a_restart_reprints_the_event_journalled_before_the_crash() {
    let dir = tempfile::tempdir().unwrap();
    let path = std::fs::canonicalize(dir.path()).unwrap().join("state");
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
}
