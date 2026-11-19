//! The pull request reads of the rebase stage of `tidy plan`: the extra fields of `AnyPr`, the commit list of a
//! pull request and the time parser. A fake API on 127.0.0.1 answers; nothing leaves the machine.
use stw::auth::TokenSource;
use stw::github::{parse_utc, Client, PrCommitsRead};

use crate::common::FakeRoutes;

fn client(api: &FakeRoutes) -> Client {
    Client::with_base("tok".into(), TokenSource::Env, api.url.clone())
}

const SHA_A: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const SHA_B: &str = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

#[test]
fn utc_times_are_read_as_seconds_since_the_epoch() {
    assert_eq!(parse_utc("1970-01-01T00:00:00Z"), Some(0));
    assert_eq!(parse_utc("2023-11-14T22:13:20Z"), Some(1_700_000_000));
    assert_eq!(parse_utc("2024-02-29T00:00:00Z"), Some(1_709_164_800));
    assert_eq!(parse_utc("1969-12-31T23:59:59Z"), Some(-1));
    // 2000 is a leap year (divisible by 400), 1900 is not (divisible by 100)
    assert_eq!(parse_utc("2000-02-29T00:00:00Z"), Some(951_782_400));
    assert_eq!(parse_utc("1900-02-29T00:00:00Z"), None);
    assert_eq!(parse_utc("2023-02-29T00:00:00Z"), None);
    for bad in [
        "",
        "2023-11-14",
        "2023-11-14T22:13:20",
        "2023-13-14T22:13:20Z",
        "2023-11-32T22:13:20Z",
        "2023-11-14T25:13:20Z",
        "2023-11-14T22:13:20+01:00",
        "yesterday",
    ] {
        assert_eq!(parse_utc(bad), None, "{bad}");
    }
}

#[test]
fn the_pull_list_carries_the_merge_commit_the_merge_time_and_the_title() {
    let api = FakeRoutes::start(|_, _| {
        let body = format!(
            r#"[{{"number":7,"state":"closed","title":"Add x","merged_at":"2023-11-14T22:13:20Z","merge_commit_sha":"{SHA_A}","head":{{"ref":"feat","sha":"{SHA_B}"}}}},
               {{"number":8,"state":"open","title":"Open one","merged_at":null,"merge_commit_sha":"{SHA_A}","head":{{"ref":"o","sha":"{SHA_B}"}}}},
               {{"number":9,"state":"closed","title":"Closed","merged_at":null,"merge_commit_sha":null,"head":{{"ref":"c","sha":"{SHA_B}"}}}}]"#
        );
        (200, vec![], body)
    });
    let prs = client(&api).all_prs("o/r", 500).unwrap();
    assert_eq!(prs.len(), 3);
    assert_eq!(prs[0].state, "MERGED");
    assert_eq!(prs[0].merge_commit_sha, SHA_A);
    assert_eq!(prs[0].merged_at, Some(1_700_000_000));
    assert_eq!(prs[0].title, "Add x");
    assert_eq!(prs[1].state, "OPEN");
    assert_eq!(prs[1].merged_at, None);
    assert_eq!(prs[2].state, "CLOSED");
    assert_eq!(prs[2].merge_commit_sha, "");
    assert_eq!(prs[2].title, "Closed");
}

#[test]
fn the_pull_list_carries_the_repository_of_the_head() {
    let api = FakeRoutes::start(|_, _| {
        let body = format!(
            r#"[{{"number":7,"state":"closed","title":"a","merged_at":"2023-11-14T22:13:20Z","merge_commit_sha":"{SHA_A}","head":{{"ref":"feat","sha":"{SHA_B}","repo":{{"full_name":"o/r"}}}}}},
               {{"number":8,"state":"closed","title":"b","merged_at":"2023-11-14T22:13:20Z","merge_commit_sha":"{SHA_A}","head":{{"ref":"feat","sha":"{SHA_B}","repo":{{"full_name":"fork/r"}}}}}},
               {{"number":9,"state":"closed","title":"c","merged_at":"2023-11-14T22:13:20Z","merge_commit_sha":"{SHA_A}","head":{{"ref":"feat","sha":"{SHA_B}","repo":null}}}},
               {{"number":10,"state":"open","title":"d","merged_at":null,"merge_commit_sha":null,"head":{{"ref":"feat","sha":"{SHA_B}"}}}}]"#
        );
        (200, vec![], body)
    });
    let prs = client(&api).all_prs("o/r", 500).unwrap();
    let repos: Vec<&str> = prs.iter().map(|p| p.head_repo.as_str()).collect();
    // a deleted fork has `repo: null`, an answer without the field has none: both are empty
    assert_eq!(repos, ["o/r", "fork/r", "", ""]);
}

fn commit_json(sha: &str, name: &str, mail: &str, date: &str, msg: &str) -> String {
    format!(
        r#"{{"sha":"{sha}","commit":{{"author":{{"name":"{name}","email":"{mail}","date":"{date}"}},"message":"{msg}"}}}}"#
    )
}

#[test]
fn the_commit_list_of_a_pull_request_has_author_time_and_subject() {
    let api = FakeRoutes::start(|_, _| {
        let body = format!(
            "[{},{}]",
            commit_json(
                SHA_A,
                "Ada",
                "Ada@Example.com",
                "2023-11-14T22:13:20Z",
                "first\\n\\nbody"
            ),
            commit_json(
                SHA_B,
                "Bob",
                "b@example.com",
                "2023-11-14T22:14:00Z",
                "second"
            )
        );
        (200, vec![], body)
    });
    let c = client(&api);
    let PrCommitsRead::Commits(v) = c.pr_commits("o/r", 7).unwrap() else {
        panic!("expected commits")
    };
    assert_eq!(v.len(), 2);
    assert_eq!(v[0].id.to_string(), SHA_A);
    assert_eq!(v[0].name, b"Ada");
    assert_eq!(v[0].mail, b"Ada@Example.com");
    assert_eq!(v[0].secs, 1_700_000_000);
    assert_eq!(v[0].subject, b"first");
    assert_eq!(v[1].subject, b"second");
    assert_eq!(v[1].secs, 1_700_000_040);
    let hits = api.hits();
    assert_eq!(hits.len(), 1);
    assert!(
        hits[0].starts_with("GET /repos/o/r/pulls/7/commits?per_page=100 "),
        "{}",
        hits[0]
    );
}

#[test]
fn a_full_page_of_commits_is_too_many() {
    let api = FakeRoutes::start(|_, _| {
        let one = commit_json(SHA_A, "Ada", "a@example.com", "2023-11-14T22:13:20Z", "s");
        (200, vec![], format!("[{}]", vec![one; 100].join(",")))
    });
    assert!(matches!(
        client(&api).pr_commits("o/r", 7).unwrap(),
        PrCommitsRead::TooMany
    ));
}

#[test]
fn a_commit_list_that_cannot_be_read_is_an_error() {
    let api = FakeRoutes::start(|_, _| (404, vec![], "{}".into()));
    assert!(client(&api).pr_commits("o/r", 7).is_err());
    let api = FakeRoutes::start(|_, _| (200, vec![], r#"{"x":1}"#.into()));
    assert!(client(&api).pr_commits("o/r", 7).is_err());
    let api = FakeRoutes::start(|_, _| (200, vec![], r#"[{"sha":"zz"}]"#.into()));
    assert!(client(&api).pr_commits("o/r", 7).is_err());
}
