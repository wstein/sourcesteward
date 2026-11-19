//! `Op::BlameLines` of the `via: git` runner (`stw absorb`): fixed argument vector, pinned environment (no lazy fetch),
//! the version floor and the validated parts. Harness confinement as in `git_runner_ops.rs`.
use std::ffi::OsString;

use stw::git_runner::{BlamePath, GitError, Hex40, Op, Runner};

fn osv(pairs: &[(&str, &str)]) -> Vec<(OsString, OsString)> {
    pairs
        .iter()
        .map(|(k, v)| (OsString::from(k), OsString::from(v)))
        .collect()
}

fn tmp() -> tempfile::TempDir {
    tempfile::Builder::new()
        .prefix("stw-runner-blame-")
        .tempdir()
        .unwrap()
}

fn rev() -> Hex40 {
    Hex40::parse(&"a".repeat(40)).unwrap()
}

fn blame(path: &str, first: u32, last: u32) -> Op {
    Op::BlameLines {
        rev: rev(),
        path: BlamePath::parse(path.as_bytes()).unwrap(),
        first,
        last,
    }
}

fn runner() -> Runner {
    Runner::from_env(osv(&[("PATH", "/usr/bin")]))
}

fn argv(op: Op) -> Result<Vec<String>, GitError> {
    let t = tmp();
    runner()
        .argv(t.path(), op)
        .map(|v| v.iter().map(|a| a.to_string_lossy().into_owned()).collect())
}

#[test]
fn the_vector_is_fixed_and_the_path_comes_after_the_separator() {
    let v = argv(blame("dir/a b.txt", 3, 5)).unwrap();
    let at = v.iter().position(|a| a == "blame").expect("blame");
    assert_eq!(
        &v[at..],
        [
            "blame",
            "--porcelain",
            "--no-textconv",
            "--ignore-revs-file=",
            "-L",
            "3,5",
            &"a".repeat(40),
            "--",
            "dir/a b.txt"
        ]
    );
    assert!(
        v[..at].contains(&"--literal-pathspecs".to_string()),
        "{v:?}"
    );
    assert!(
        v[..at]
            .windows(2)
            .any(|w| w == ["-c", "core.fsmonitor=false"]),
        "{v:?}"
    );
    assert!(
        v[..at]
            .windows(2)
            .any(|w| w == ["-c", "core.hooksPath=/dev/null"]),
        "{v:?}"
    );
}

#[test]
fn a_dash_path_cannot_become_an_option() {
    let v = argv(blame("-n.txt", 1, 1)).unwrap();
    let sep = v.iter().position(|a| a == "--").unwrap();
    assert_eq!(v[sep + 1], "-n.txt");
}

#[test]
fn the_child_gets_no_lazy_fetch_and_the_other_ops_are_unchanged() {
    let t = tmp();
    let r = Runner::from_env(osv(&[("PATH", "/usr/bin"), ("GIT_NO_LAZY_FETCH", "0")]));
    let env = |op: &Op| r.child_env_for(t.path(), op).unwrap();
    let get = |e: &[(OsString, OsString)], k: &str| {
        e.iter()
            .filter(|(n, _)| n == k)
            .map(|(_, v)| v.to_string_lossy().into_owned())
            .collect::<Vec<_>>()
    };
    assert_eq!(get(&env(&blame("a.txt", 1, 1)), "GIT_NO_LAZY_FETCH"), ["1"]);
    assert_eq!(
        get(&env(&blame("a.txt", 1, 1)), "GIT_OPTIONAL_LOCKS"),
        ["0"]
    );
    assert!(get(&env(&Op::FetchOrigin), "GIT_NO_LAZY_FETCH").is_empty());
    assert!(get(&env(&Op::PatchIdStable), "GIT_NO_LAZY_FETCH").is_empty());
}

#[test]
fn an_empty_or_reversed_range_is_refused_before_anything_runs() {
    for (first, last) in [(0, 1), (3, 2)] {
        let e = argv(blame("a.txt", first, last)).unwrap_err();
        assert!(matches!(e, GitError::Argument(_)), "{first},{last}: {e:?}");
    }
}

#[test]
fn the_path_must_be_text_without_line_breaks() {
    assert!(BlamePath::parse(b"dir/a b.txt").is_some());
    assert!(BlamePath::parse("üä.txt".as_bytes()).is_some());
    assert!(BlamePath::parse(b"").is_none());
    assert!(BlamePath::parse(b"k\xffl.txt").is_none());
    assert!(BlamePath::parse(b"a\0b").is_none());
    assert!(BlamePath::parse(b"a\nb").is_none());
    assert!(BlamePath::parse(b"a\rb").is_none());
}
