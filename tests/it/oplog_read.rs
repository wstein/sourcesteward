//! `stw oplog timeline | evolog | undo-plan` (read-only; formats in docs/oplog-format.md). Logs and reflogs are
//! written by hand into fixture repositories built with gix; the binary runs with a scrubbed environment and no git.
use std::process::Command;

use crate::common::*;
use gix::ObjectId;
use serde_json::Value;

const T: &str = "2026-11-12T00:42:26Z";
const Z: &str = "0000000000000000000000000000000000000000";

fn h(c: char) -> String {
    c.to_string().repeat(40)
}

struct Run {
    code: i32,
    text: String,
    err: String,
}

fn run(fx: &Fx, args: &[&str]) -> Run {
    assert!(fx.path.is_absolute(), "unconfined");
    let home = tempfile::tempdir().unwrap();
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", "/nonexistent")
        .env("HOME", home.path())
        .current_dir(&fx.path)
        .arg("oplog")
        .args(args)
        .output()
        .unwrap();
    Run {
        code: o.status.code().unwrap_or(-1),
        text: String::from_utf8_lossy(&o.stdout).into_owned(),
        err: String::from_utf8_lossy(&o.stderr).into_owned(),
    }
}

fn json(fx: &Fx, args: &[&str]) -> Value {
    let mut a: Vec<&str> = args.to_vec();
    a.push("--json");
    let r = run(fx, &a);
    serde_json::from_str(&r.text)
        .unwrap_or_else(|e| panic!("not JSON ({e}): {}\nstderr: {}", r.text, r.err))
}

/// A file of `.git/stw/oplog/`.
fn oplog(fx: &Fx, name: &str, content: &str) {
    fx.write(&format!(".git/stw/oplog/{name}"), content);
}

/// A reflog file `.git/logs/<refname>`: (old, new, epoch, message).
fn reflog(fx: &Fx, refname: &str, rows: &[(&str, &str, i64, &str)]) {
    let mut s = String::new();
    for (o, n, e, m) in rows {
        s += &format!("{o} {n} T <t@example.com> {e} +0000\t{m}\n");
    }
    fx.write(&format!(".git/logs/{refname}"), &s);
}

fn line(time: &str, old: &str, new: &str, name: &str, ctx: &str, wt: &str) -> String {
    format!("{time}\t{old}\t{new}\t{name}\t{ctx}\t{wt}\n")
}

fn entries(v: &Value) -> &Vec<Value> {
    v["entries"].as_array().unwrap()
}

fn entry<'a>(v: &'a Value, r: &str, new: &str) -> &'a Value {
    entries(v)
        .iter()
        .find(|e| e["ref"] == r && e["new"] == new)
        .unwrap_or_else(|| panic!("no entry {r} {new} in {v}"))
}

fn gap<'a>(v: &'a Value, kind: &str) -> Option<&'a Value> {
    v["gaps"]
        .as_array()
        .unwrap()
        .iter()
        .find(|g| g["kind"] == kind)
}

// ---- timeline -----------------------------------------------------------------------------------------------------

/// A reflog row and the hook line of the same change are one entry: origin reflog, covered. A hook line that no reflog
/// has (a deleted branch) is its own entry.
#[test]
fn a_hook_line_covers_its_reflog_row_and_a_hook_only_line_is_its_own_entry() {
    let fx = Fx::new();
    reflog(
        &fx,
        "refs/heads/x",
        &[
            (Z, &h('1'), 1_700_000_000, "branch: Created from HEAD"),
            (&h('1'), &h('2'), 1_700_000_100, "commit: two"),
        ],
    );
    oplog(&fx, "events-refhook.log", "2020-01-01T00:00:00Z\tinstall\n");
    oplog(
        &fx,
        "refs.log",
        &(line(
            "2023-11-14T22:13:20Z",
            Z,
            &h('1'),
            "refs/heads/x",
            "ref-transaction",
            "main",
        ) + &line(
            "2023-11-14T22:15:00Z",
            &h('1'),
            &h('2'),
            "refs/heads/x",
            "ref-transaction",
            "main",
        ) + &line(
            "2023-11-14T22:16:00Z",
            &h('3'),
            Z,
            "refs/heads/gone",
            "ref-transaction",
            "main",
        )),
    );
    let v = json(&fx, &["timeline"]);
    assert_eq!(v["schema_version"], 1);
    assert_eq!(v["command"], "oplog timeline");
    let e = entry(&v, "refs/heads/x", &h('2'));
    assert_eq!(
        (e["origin"].as_str(), e["flag"].as_str()),
        (Some("reflog"), Some("covered"))
    );
    assert_eq!(e["epoch"], 1_700_000_100);
    assert_eq!(e["time"], "2023-11-14T22:15:00Z");
    let g = entry(&v, "refs/heads/gone", Z);
    assert_eq!(
        (g["origin"].as_str(), g["flag"].as_str()),
        (Some("hook"), Some("hook-only"))
    );
    assert_eq!(g["old"], h('3'));
    assert_eq!(entries(&v).len(), 3, "{v}");
}

#[test]
fn entries_are_sorted_by_time_then_by_file_order() {
    let fx = Fx::new();
    reflog(&fx, "refs/heads/a", &[(Z, &h('1'), 1_700_000_300, "x")]);
    reflog(&fx, "refs/heads/b", &[(Z, &h('2'), 1_700_000_100, "y")]);
    let v = json(&fx, &["timeline"]);
    let order: Vec<&str> = entries(&v)
        .iter()
        .map(|e| e["ref"].as_str().unwrap())
        .collect();
    assert_eq!(order, ["refs/heads/b", "refs/heads/a"]);
}

/// pre-old marker: a delete whose old value git did not tell gets it from the marker. Without a marker the old value
/// is unknown; the second `0 0` line of an already deleted ref is dropped.
#[test]
fn a_pre_old_marker_gives_the_old_value_of_a_delete() {
    let fx = Fx::new();
    oplog(
        &fx,
        "refs.log",
        &(line(
            "2026-01-01T00:00:00Z",
            &h('a'),
            Z,
            "refs/tags/v1",
            "pre-old",
            "-",
        ) + &line(
            "2026-01-01T00:00:00Z",
            Z,
            Z,
            "refs/tags/v1",
            "ref-transaction",
            "main",
        ) + &line(
            "2026-01-01T00:00:01Z",
            Z,
            Z,
            "refs/tags/v1",
            "ref-transaction",
            "main",
        ) + &line(
            "2026-01-01T00:00:02Z",
            Z,
            Z,
            "refs/tags/v2",
            "ref-transaction",
            "main",
        )),
    );
    let v = json(&fx, &["timeline"]);
    let tags: Vec<&Value> = entries(&v)
        .iter()
        .filter(|e| e["ref"] == "refs/tags/v1")
        .collect();
    assert_eq!(tags.len(), 1, "the second 0 0 line is dropped: {v}");
    assert_eq!(tags[0]["old"], h('a'));
    assert_eq!(tags[0]["new"], Z);
    let unknown = entry(&v, "refs/tags/v2", Z);
    assert_eq!(unknown["old"], Z);
    assert_eq!(unknown["flag"], "old-unknown");
}

#[test]
fn a_marker_without_a_following_line_is_an_aborted_transaction() {
    let fx = Fx::new();
    oplog(
        &fx,
        "refs.log",
        &line(T, &h('a'), Z, "refs/tags/v1", "pre-old", "-"),
    );
    let v = json(&fx, &["timeline"]);
    assert!(entries(&v).is_empty(), "{v}");
    assert!(
        v["other_lines"].as_array().unwrap().is_empty(),
        "a marker is not an unknown context"
    );
}

/// A following line with its own old value keeps it and the marker is dropped.
#[test]
fn a_marker_is_consumed_by_exactly_one_line() {
    let fx = Fx::new();
    oplog(
        &fx,
        "refs.log",
        &(line(T, &h('a'), Z, "refs/heads/x", "pre-old", "-")
            + &line(
                T,
                &h('b'),
                &h('c'),
                "refs/heads/x",
                "ref-transaction",
                "main",
            )
            + &line(T, Z, Z, "refs/heads/x", "ref-transaction", "main")),
    );
    let v = json(&fx, &["timeline"]);
    let first = entry(&v, "refs/heads/x", &h('c'));
    assert_eq!(first["old"], h('b'), "its own old value wins");
    let second = entry(&v, "refs/heads/x", Z);
    assert_eq!(second["flag"], "old-unknown", "the marker is spent: {v}");
}

#[test]
fn a_pre_same_marker_hides_the_packing_line() {
    let fx = Fx::new();
    oplog(
        &fx,
        "refs.log",
        &(line(T, &h('a'), Z, "refs/heads/x", "pre-same", "-")
            + &line(T, Z, &h('a'), "refs/heads/x", "ref-transaction", "main")
            + &line(
                T,
                &h('a'),
                &h('b'),
                "refs/heads/x",
                "ref-transaction",
                "main",
            )),
    );
    let v = json(&fx, &["timeline"]);
    assert_eq!(entries(&v).len(), 1, "{v}");
    assert_eq!(entries(&v)[0]["new"], h('b'));
}

#[test]
fn stw_lines_have_origin_stw_and_unknown_contexts_stay_out_of_the_timeline() {
    let fx = Fx::new();
    oplog(
        &fx,
        "refs.log",
        &(line(T, &h('1'), &h('2'), "refs/heads/x", "stw", "main")
            + &line(T, &h('2'), &h('3'), "refs/heads/x", "future-word", "main")),
    );
    let v = json(&fx, &["timeline"]);
    assert_eq!(entries(&v).len(), 1, "{v}");
    assert_eq!(entries(&v)[0]["origin"], "stw");
    let other = v["other_lines"].as_array().unwrap();
    assert_eq!(other.len(), 1);
    assert_eq!(
        (other[0]["context"].as_str(), other[0]["ref"].as_str()),
        (Some("future-word"), Some("refs/heads/x"))
    );
}

/// A symbolic value (`HEAD` moving to another branch) is shown whole, decoded; the text table does not cut it to 7 characters.
#[test]
fn a_symbolic_value_is_decoded_and_shown_whole() {
    let fx = Fx::new();
    oplog(
        &fx,
        "refs.log",
        &line(
            T,
            Z,
            "ref:refs/heads/a%C3%A4",
            "HEAD",
            "ref-transaction",
            "main",
        ),
    );
    let v = json(&fx, &["timeline"]);
    assert_eq!(entries(&v)[0]["new"], "ref:refs/heads/aä");
    let r = run(&fx, &["timeline"]);
    assert!(r.text.contains("0000000->ref:refs/heads/aä"), "{}", r.text);
}

#[test]
fn ref_names_are_decoded_and_escaped() {
    let fx = Fx::new();
    oplog(
        &fx,
        "refs.log",
        &line(
            T,
            &h('1'),
            &h('2'),
            "refs/heads/a%C3%A4%0Ab",
            "ref-transaction",
            "main",
        ),
    );
    let v = json(&fx, &["timeline"]);
    assert_eq!(entries(&v)[0]["ref"], "refs/heads/aä\\nb");
    let r = run(&fx, &["timeline"]);
    assert!(r.text.contains("refs/heads/aä\\nb"), "{}", r.text);
    assert!(
        !r.text.lines().any(|l| l.trim() == "b"),
        "a newline in a name must not break the table"
    );
}

#[test]
fn bad_lines_are_counted_in_a_gap_and_never_shown() {
    let fx = Fx::new();
    oplog(
        &fx,
        "refs.log",
        &(line(T, &h('1'), &h('2'), "refs/heads/x", "stw", "main")
            + "garbage\n"
            + &format!("{T}\t{}\t{}\trefs/heads/y\tstw\n", &h('1')[..39], h('2'))),
    );
    let v = json(&fx, &["timeline"]);
    assert_eq!(entries(&v).len(), 1);
    let g = gap(&v, "bad").expect("a bad gap");
    assert_eq!(g["ref"], "refs.log");
    assert!(g["text"].as_str().unwrap().starts_with("2 lines"), "{g}");
}

#[test]
fn the_install_window_tells_uncovered_from_pre_install() {
    let fx = Fx::new();
    // rows at 1_700_000_000 (2023-11-14), 1_800_000_000 (2027-01-15), 1_900_000_000 (2030-03-17)
    reflog(
        &fx,
        "refs/heads/x",
        &[
            (Z, &h('1'), 1_700_000_000, "a"),
            (&h('1'), &h('2'), 1_800_000_000, "b"),
            (&h('2'), &h('3'), 1_900_000_000, "c"),
        ],
    );
    oplog(
        &fx,
        "events-refhook.log",
        "2026-01-01T00:00:00Z\tinstall\n2029-01-01T00:00:00Z\tuninstall\n",
    );
    oplog(&fx, "refs.log", "");
    let v = json(&fx, &["timeline"]);
    assert_eq!(entry(&v, "refs/heads/x", &h('1'))["flag"], "pre-install");
    assert_eq!(entry(&v, "refs/heads/x", &h('2'))["flag"], "uncovered");
    assert_eq!(entry(&v, "refs/heads/x", &h('3'))["flag"], "hook-absent");
    assert!(gap(&v, "uncovered").is_some() && gap(&v, "pre-install").is_some());
}

#[test]
fn without_an_install_record_nothing_is_claimed_to_be_uncovered() {
    let fx = Fx::new();
    reflog(&fx, "refs/heads/x", &[(Z, &h('1'), 1_700_000_000, "a")]);
    let v = json(&fx, &["timeline"]);
    assert_eq!(entry(&v, "refs/heads/x", &h('1'))["flag"], "unknown-window");
    assert!(gap(&v, "no-install-record").is_some(), "{v}");
    assert!(gap(&v, "uncovered").is_none());
}

#[test]
fn the_head_row_of_a_commit_is_covered_by_the_branch_line() {
    let fx = Fx::new();
    reflog(
        &fx,
        "HEAD",
        &[(&h('1'), &h('2'), 1_800_000_000, "commit: x")],
    );
    oplog(&fx, "events-refhook.log", "2026-01-01T00:00:00Z\tinstall\n");
    oplog(
        &fx,
        "refs.log",
        &line(
            "2027-01-15T08:00:00Z",
            &h('1'),
            &h('2'),
            "refs/heads/x",
            "ref-transaction",
            "main",
        ),
    );
    let v = json(&fx, &["timeline"]);
    assert_eq!(entry(&v, "HEAD", &h('2'))["flag"], "covered");
}

#[test]
fn error_lines_are_capture_gaps_and_a_rotation_is_its_own_row() {
    let fx = Fx::new();
    oplog(
        &fx,
        "errors.log",
        &format!("{T}\tgap\tprepared\tfailed=1\tbad=0\t-\n{T}\tgap\tcommitted\tfailed=0\tbad=2\tmain\n{T}\tgap\trotated\tfailed=0\tbad=0\t-\n"),
    );
    fx.write(
        ".git/stw-oplog-gaps",
        &format!("{T}\tgap\tamend\tfailed=1\tbad=0\tmain\n"),
    );
    let v = json(&fx, &["timeline"]);
    assert!(
        gap(&v, "capture-gap").unwrap()["text"]
            .as_str()
            .unwrap()
            .starts_with("3 gap lines"),
        "{v}"
    );
    assert!(
        gap(&v, "rotated").unwrap()["text"]
            .as_str()
            .unwrap()
            .starts_with("1 rotation"),
        "{v}"
    );
}

/// A second rotation of one file replaced its old generation: the timeline says `truncated` for that file only.
#[test]
fn two_rotations_of_one_log_are_reported_as_truncated() {
    let rot = |name: &str| format!("{T}\tgap\trotated\tfailed=0\tbad=0\t{name}\n");
    let truncated = |lines: String| {
        let fx = Fx::new();
        oplog(&fx, "errors.log", &lines);
        let v = json(&fx, &["timeline"]);
        v["gaps"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|g| {
                g["kind"] == "truncated" && g["ref"].as_str().is_some_and(|r| r.ends_with(".log"))
            })
            .map(|g| g["ref"].as_str().unwrap().to_string())
            .collect::<Vec<_>>()
    };
    assert_eq!(truncated(rot("refs.log") + &rot("refs.log")), ["refs.log"]);
    assert_eq!(truncated(rot("rewrites.log").repeat(2)), ["rewrites.log"]);
    assert!(
        truncated(rot("refs.log")).is_empty(),
        "one rotation keeps the old generation"
    );
    assert!(
        truncated(rot("refs.log") + &rot("rewrites.log")).is_empty(),
        "one rotation of each file"
    );
}

/// The old generation is read first, so a marker at its end joins its line at the start of the new file.
#[test]
fn the_old_generation_is_read_before_the_current_file() {
    let fx = Fx::new();
    oplog(
        &fx,
        "refs.log.1",
        &line(T, &h('a'), Z, "refs/tags/v1", "pre-old", "-"),
    );
    oplog(
        &fx,
        "refs.log",
        &line(T, Z, Z, "refs/tags/v1", "ref-transaction", "main"),
    );
    let v = json(&fx, &["timeline"]);
    assert_eq!(entry(&v, "refs/tags/v1", Z)["old"], h('a'), "{v}");
}

#[test]
fn a_symlinked_log_is_not_read() {
    let fx = Fx::new();
    let target = fx.path.join("elsewhere.log");
    std::fs::write(
        &target,
        line(T, &h('1'), &h('2'), "refs/heads/x", "stw", "main"),
    )
    .unwrap();
    std::fs::create_dir_all(fx.path.join(".git/stw/oplog")).unwrap();
    std::os::unix::fs::symlink(&target, fx.path.join(".git/stw/oplog/refs.log")).unwrap();
    let v = json(&fx, &["timeline"]);
    assert!(entries(&v).is_empty(), "{v}");
    let g = gap(&v, "unreadable").unwrap();
    assert_eq!(g["ref"], "refs.log");
    assert!(
        g["text"].as_str().unwrap().contains("a symbolic link"),
        "the reason is the link, not 'not a regular file': {g}"
    );
}

#[test]
fn a_symlinked_oplog_directory_is_not_read() {
    let fx = Fx::new();
    let other = fx.path.join("other-oplog");
    std::fs::create_dir_all(&other).unwrap();
    std::fs::write(
        other.join("refs.log"),
        line(T, &h('1'), &h('2'), "refs/heads/x", "stw", "main"),
    )
    .unwrap();
    std::fs::create_dir_all(fx.path.join(".git/stw")).unwrap();
    std::os::unix::fs::symlink(&other, fx.path.join(".git/stw/oplog")).unwrap();
    let v = json(&fx, &["timeline"]);
    assert!(entries(&v).is_empty(), "{v}");
    let g = gap(&v, "unreadable").expect("an unreadable gap");
    assert!(
        g["text"].as_str().unwrap().contains("a symbolic link"),
        "the reason is the link, not 'not a directory': {g}"
    );
}

#[test]
fn groups_are_always_suspected() {
    let fx = Fx::new();
    reflog(
        &fx,
        "refs/heads/x",
        &[
            (Z, &h('1'), 1_700_000_000, "rebase (pick): one"),
            (&h('1'), &h('2'), 1_700_000_001, "rebase (pick): two"),
            (&h('2'), &h('3'), 1_700_000_100, "commit: later"),
        ],
    );
    let v = json(&fx, &["timeline"]);
    let e = entries(&v);
    assert_eq!(e[0]["group"], e[1]["group"]);
    assert!(e[0]["group"].is_string());
    assert!(e[2]["group"].is_null());
    assert!(e.iter().all(|x| x["group_basis"] == "suspected"));
    assert!(run(&fx, &["timeline"]).text.contains("suspected"));
}

#[test]
fn reflog_text_is_escaped() {
    let fx = Fx::new();
    reflog(
        &fx,
        "refs/heads/x",
        &[(Z, &h('1'), 1_700_000_000, "commit: \u{1b}[31mred\u{202e}")],
    );
    let r = run(&fx, &["timeline"]);
    assert!(
        !r.text.contains('\u{1b}') && !r.text.contains('\u{202e}'),
        "{:?}",
        r.text
    );
    let v = json(&fx, &["timeline"]);
    assert_eq!(entries(&v)[0]["message"], "commit: \\u{1b}[31mred\\u{202e}");
}

#[test]
fn remote_tracking_refs_need_remotes_and_other_refs_need_all() {
    let fx = Fx::new();
    reflog(
        &fx,
        "refs/remotes/origin/x",
        &[(Z, &h('1'), 1_700_000_000, "fetch")],
    );
    reflog(&fx, "refs/notes/n", &[(Z, &h('2'), 1_700_000_000, "notes")]);
    reflog(
        &fx,
        "refs/heads/x",
        &[(Z, &h('3'), 1_700_000_000, "branch")],
    );
    assert_eq!(entries(&json(&fx, &["timeline"])).len(), 1);
    assert_eq!(entries(&json(&fx, &["timeline", "--remotes"])).len(), 2);
    assert_eq!(entries(&json(&fx, &["timeline", "--all"])).len(), 3);
    let v = json(&fx, &["timeline", "--ref", "refs/heads/*"]);
    assert_eq!(entries(&v).len(), 1);
}

#[test]
fn a_remote_ref_is_not_flagged_as_uncovered_while_remotes_are_off() {
    let fx = Fx::new();
    reflog(
        &fx,
        "refs/remotes/origin/x",
        &[(Z, &h('1'), 1_800_000_000, "fetch")],
    );
    oplog(&fx, "events-refhook.log", "2026-01-01T00:00:00Z\tinstall\n");
    let v = json(&fx, &["timeline", "--remotes"]);
    assert_eq!(
        entry(&v, "refs/remotes/origin/x", &h('1'))["flag"],
        "not-expected"
    );
}

#[test]
fn the_files_backend_is_required_for_reflog_rows() {
    let fx = Fx::new();
    reflog(&fx, "refs/heads/x", &[(Z, &h('1'), 1_700_000_000, "a")]);
    let cfg = std::fs::read_to_string(fx.path.join(".git/config")).unwrap();
    std::fs::write(
        fx.path.join(".git/config"),
        format!("{cfg}[extensions]\n\trefStorage = reftable\n"),
    )
    .unwrap();
    let v = json(&fx, &["timeline"]);
    assert!(gap(&v, "backend").is_some(), "{v}");
    assert!(
        entries(&v).is_empty(),
        "no reflog rows are claimed for a backend that is not read"
    );
}

#[test]
fn timeline_writes_nothing() {
    let fx = Fx::new();
    reflog(&fx, "refs/heads/x", &[(Z, &h('1'), 1_700_000_000, "a")]);
    oplog(
        &fx,
        "refs.log",
        &line(T, &h('1'), &h('2'), "refs/heads/x", "stw", "main"),
    );
    let before = tree_snapshot(&fx.path);
    let _ = run(&fx, &["timeline"]);
    let _ = run(&fx, &["timeline", "--json", "--all"]);
    assert!(before == tree_snapshot(&fx.path), "the repository changed");
}

#[test]
fn an_empty_repository_has_no_entries() {
    let fx = Fx::new();
    let r = run(&fx, &["timeline"]);
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert!(r.text.contains("(no entries)"), "{}", r.text);
}

/// Parser rules seen through the command: each line below is skipped and counted, and the good line after it is read.
fn bad_then_good(fx: &Fx, bad: &str) -> Value {
    oplog(
        fx,
        "refs.log",
        &(bad.to_string() + &line(T, &h('1'), &h('2'), "refs/heads/ok", "stw", "main")),
    );
    json(fx, &["timeline"])
}

fn one_bad(v: &Value) {
    assert_eq!(entries(v).len(), 1, "{v}");
    assert_eq!(entries(v)[0]["ref"], "refs/heads/ok");
    assert!(
        gap(v, "bad").unwrap()["text"]
            .as_str()
            .unwrap()
            .starts_with("1 lines"),
        "{v}"
    );
}

/// A hook killed before its LF and the next one appended. With a normal time the colons of the second time already break
/// the worktree field; with the time `unknown` only the rule for fields from the 7th on (`key=value`) catches it.
#[test]
fn a_glued_line_is_bad_and_not_accepted() {
    let fx = Fx::new();
    let a = line(T, &h('1'), &h('2'), "refs/heads/a", "stw", "main");
    let b = line("unknown", &h('2'), &h('3'), "refs/heads/b", "stw", "main");
    one_bad(&bad_then_good(&fx, &format!("{}{b}", a.trim_end())));
}

#[test]
fn a_line_over_4_kb_is_bad_and_the_stream_goes_on() {
    let fx = Fx::new();
    let long = line(
        T,
        &h('1'),
        &h('2'),
        &format!("refs/heads/{}", "x".repeat(4100)),
        "stw",
        "main",
    );
    one_bad(&bad_then_good(&fx, &long));
}

#[test]
fn a_line_with_a_nul_byte_is_bad() {
    let fx = Fx::new();
    one_bad(&bad_then_good(
        &fx,
        &line(T, &h('1'), &h('2'), "refs/heads/a%00", "stw\0", "main"),
    ));
}

#[test]
fn a_name_with_a_broken_percent_escape_is_bad() {
    let fx = Fx::new();
    one_bad(&bad_then_good(
        &fx,
        &line(T, &h('1'), &h('2'), "refs/heads/a%zz", "stw", "main"),
    ));
    one_bad(&bad_then_good(
        &fx,
        &line(T, &h('1'), &h('2'), "refs/heads/a%4", "stw", "main"),
    ));
    one_bad(&bad_then_good(
        &fx,
        &line(T, &h('1'), "ref:refs/heads/a%", "HEAD", "stw", "main"),
    ));
}

#[test]
fn a_worktree_id_outside_the_reduced_character_set_is_bad() {
    let fx = Fx::new();
    one_bad(&bad_then_good(
        &fx,
        &line(T, &h('1'), &h('2'), "refs/heads/a", "stw", "a b"),
    ));
    one_bad(&bad_then_good(
        &fx,
        &line(T, &h('1'), &h('2'), "refs/heads/a", "stw", "wt/x"),
    ));
}

#[test]
fn a_log_that_is_a_directory_is_not_read_as_a_regular_file() {
    let fx = Fx::new();
    std::fs::create_dir_all(fx.path.join(".git/stw/oplog/refs.log")).unwrap();
    let v = json(&fx, &["timeline"]);
    let g = gap(&v, "unreadable").expect("an unreadable gap");
    assert_eq!(g["text"], "not read: not a regular file", "{g}");
}

#[test]
fn a_log_over_the_size_limit_is_not_read() {
    let fx = Fx::new();
    std::fs::create_dir_all(fx.path.join(".git/stw/oplog")).unwrap();
    let f = std::fs::File::create(fx.path.join(".git/stw/oplog/refs.log")).unwrap();
    f.set_len(64 * 1024 * 1024 + 1).unwrap();
    let v = json(&fx, &["timeline"]);
    let g = gap(&v, "unreadable").expect("an unreadable gap");
    assert!(g["text"].as_str().unwrap().contains("larger than"), "{g}");
    assert!(gap(&v, "bad").is_none(), "the file must not be parsed: {v}");
}

#[test]
fn a_hash_in_upper_case_is_bad() {
    let fx = Fx::new();
    one_bad(&bad_then_good(
        &fx,
        &line(T, &"A".repeat(40), &h('2'), "refs/heads/a", "stw", "main"),
    ));
}

#[test]
fn a_glued_rewrites_line_is_bad() {
    let fx = Fx::new();
    let [a, b, c] = three(&fx);
    let l1 = rewrites(&[("amend", &a.to_string(), &b.to_string())]);
    let l2 = format!("unknown\tamend\t{b}\t{c}\t\tmain\n");
    oplog(&fx, "rewrites.log", &format!("{}{l2}", l1.trim_end()));
    let v = json(&fx, &["evolog", &c.to_string()]);
    assert_eq!(v["gaps"]["bad_lines"], 1, "{v}");
    assert_eq!(v["pairs"], 0);
}

// ---- evolog -------------------------------------------------------------------------------------------------------

fn rewrites(pairs: &[(&str, &str, &str)]) -> String {
    pairs
        .iter()
        .map(|(k, o, n)| format!("{T}\t{k}\t{o}\t{n}\t\tmain\n"))
        .collect()
}

fn three(fx: &Fx) -> [ObjectId; 3] {
    let a = fx.commit(None, &[("f", Some("a\n"))], "first");
    let b = fx.commit(Some(a), &[("f", Some("b\n"))], "second");
    let c = fx.commit(Some(b), &[("f", Some("c\n"))], "third");
    [a, b, c]
}

#[test]
fn the_evolog_follows_the_recorded_rewrites_back() {
    let fx = Fx::new();
    let [a, b, c] = three(&fx);
    fx.branch("x", c);
    oplog(
        &fx,
        "rewrites.log",
        &rewrites(&[
            ("amend", &a.to_string(), &b.to_string()),
            ("rebase", &b.to_string(), &c.to_string()),
        ]),
    );
    let v = json(&fx, &["evolog", "x"]);
    assert_eq!(v["schema_version"], 1);
    assert_eq!(v["command"], "oplog evolog");
    assert_eq!(v["source"], "hook");
    assert_eq!(v["tip"], c.to_string());
    assert_eq!(v["ref"], "refs/heads/x");
    assert_eq!(v["pairs"], 2);
    let n = v["nodes"].as_array().unwrap();
    assert_eq!(n.len(), 3);
    assert_eq!(n[0]["depth"], 0);
    assert_eq!(n[0]["commit"], c.to_string());
    assert_eq!(n[0]["subject"], "third");
    assert_eq!(
        (n[1]["depth"].as_i64(), n[1]["rewritten_by"].as_str()),
        (Some(1), Some("rebase"))
    );
    assert_eq!(n[2]["depth"], 2);
    assert_eq!(n[2]["commit"], a.to_string());
    assert_eq!(n[2]["rewritten_by"], "amend");
    let t = run(&fx, &["evolog", "x"]);
    assert_eq!(t.code, 0, "{}", t.err);
    assert!(
        t.text.contains("<- ") && t.text.contains("(rebase at "),
        "{}",
        t.text
    );
}

#[test]
fn merges_splits_cycles_and_repeated_commits_are_flagged() {
    let fx = Fx::new();
    let [a, b, c] = three(&fx);
    let (a, b, c) = (a.to_string(), b.to_string(), c.to_string());
    // merge: a and b both became c; split: a also became b; self pair ignored; cycle c -> a
    oplog(
        &fx,
        "rewrites.log",
        &rewrites(&[
            ("rebase", &a, &c),
            ("rebase", &b, &c),
            ("amend", &a, &b),
            ("amend", &c, &c),
            ("amend", &c, &a),
        ]),
    );
    let v = json(&fx, &["evolog", &c]);
    let n = v["nodes"].as_array().unwrap();
    let flags: Vec<String> = n
        .iter()
        .map(|x| x["flags"].as_str().unwrap().to_string())
        .collect();
    assert!(flags[0].contains("merge=2"), "{flags:?}");
    assert!(flags.iter().any(|f| f.contains("split=2")), "{flags:?}");
    assert!(flags.iter().any(|f| f == "cycle"), "{flags:?}");
    assert!(flags.iter().any(|f| f == "seen-above"), "{flags:?}");
    assert_eq!(v["pairs"], 4, "the self pair is not a pair: {v}");
}

#[test]
fn the_first_of_two_equal_pairs_wins_and_an_unknown_kind_is_shown() {
    let fx = Fx::new();
    let [a, _, c] = three(&fx);
    let (a, c) = (a.to_string(), c.to_string());
    oplog(
        &fx,
        "rewrites.log",
        &rewrites(&[("future-kind", &a, &c), ("amend", &a, &c)]),
    );
    let v = json(&fx, &["evolog", &c]);
    let n = v["nodes"].as_array().unwrap();
    assert_eq!(n.len(), 2);
    assert_eq!(n[1]["rewritten_by"], "future-kind");
}

#[test]
fn the_limit_stops_the_chain() {
    let fx = Fx::new();
    let [a, b, c] = three(&fx);
    oplog(
        &fx,
        "rewrites.log",
        &rewrites(&[
            ("amend", &a.to_string(), &b.to_string()),
            ("amend", &b.to_string(), &c.to_string()),
        ]),
    );
    let v = json(&fx, &["evolog", &c.to_string(), "--limit", "2"]);
    let n = v["nodes"].as_array().unwrap();
    assert_eq!(n.len(), 3);
    assert_eq!(n[2]["flags"], "limit");
}

#[test]
fn a_predecessor_that_is_gone_says_so() {
    let fx = Fx::new();
    let [_, _, c] = three(&fx);
    oplog(
        &fx,
        "rewrites.log",
        &rewrites(&[("amend", &h('9'), &c.to_string())]),
    );
    let v = json(&fx, &["evolog", &c.to_string()]);
    assert_eq!(v["nodes"][1]["subject"], "object gone");
}

#[test]
fn without_a_log_the_evolog_has_one_node_and_says_why() {
    let fx = Fx::new();
    let [_, _, c] = three(&fx);
    let t = run(&fx, &["evolog", &c.to_string()]);
    assert_eq!(t.code, 0, "{}", t.err);
    assert!(t.text.contains("file does not exist"), "{}", t.text);
    assert!(t.text.contains("no recorded predecessor"), "{}", t.text);
}

#[test]
fn a_reflog_rewrite_without_a_hook_pair_is_uncovered() {
    let fx = Fx::new();
    let [a, b, c] = three(&fx);
    fx.branch("x", c);
    reflog(
        &fx,
        "refs/heads/x",
        &[
            (Z, &a.to_string(), 1_800_000_000, "commit: first"),
            (
                &a.to_string(),
                &b.to_string(),
                1_800_000_100,
                "commit (amend): second",
            ),
            (
                &b.to_string(),
                &c.to_string(),
                1_800_000_200,
                "rebase (finish): refs/heads/x onto y",
            ),
        ],
    );
    oplog(&fx, "events.log", "2026-01-01T00:00:00Z\tinstall\n");
    oplog(
        &fx,
        "rewrites.log",
        &rewrites(&[("rebase", &b.to_string(), &c.to_string())]),
    );
    let v = json(&fx, &["evolog", "x"]);
    assert_eq!(v["gaps"]["uncovered_rewrites"], 1, "{v}");
    assert_eq!(v["gaps"]["compared_with_reflog"], "refs/heads/x");
}

#[test]
fn capture_gaps_and_bad_lines_are_counted_in_the_evolog() {
    let fx = Fx::new();
    let [_, _, c] = three(&fx);
    oplog(
        &fx,
        "errors.log",
        &format!("{T}\tgap\tamend\tfailed=1\tbad=0\tmain\n"),
    );
    oplog(&fx, "rewrites.log", "garbage\n");
    let v = json(&fx, &["evolog", &c.to_string()]);
    assert_eq!(v["gaps"]["capture_errors_log"], 1);
    assert_eq!(v["gaps"]["bad_lines"], 1);
}

/// The JSON says what the text says: an unreadable log and whether `rewrites.log` exists at all.
#[test]
fn the_evolog_json_names_an_unreadable_log_and_a_missing_file() {
    let fx = Fx::new();
    let [_, _, c] = three(&fx);
    let tip = c.to_string();
    let v = json(&fx, &["evolog", &tip]);
    assert_eq!(v["gaps"]["log_exists"], false, "{v}");
    assert!(
        v["gaps"]["unreadable"].as_array().unwrap().is_empty(),
        "{v}"
    );
    let target = fx.path.join("elsewhere.log");
    std::fs::write(&target, rewrites(&[("amend", &h('1'), &tip)])).unwrap();
    std::fs::create_dir_all(fx.path.join(".git/stw/oplog")).unwrap();
    std::os::unix::fs::symlink(&target, fx.path.join(".git/stw/oplog/rewrites.log")).unwrap();
    let v = json(&fx, &["evolog", &tip]);
    assert_eq!(v["pairs"], 0, "{v}");
    assert_eq!(v["gaps"]["log_exists"], true, "{v}");
    let u = v["gaps"]["unreadable"].as_array().unwrap();
    assert_eq!(u.len(), 1, "{v}");
    assert_eq!(u[0]["file"], "rewrites.log");
    assert_eq!(u[0]["text"], "not read: a symbolic link");
    assert!(run(&fx, &["evolog", &tip])
        .text
        .contains("unreadable      rewrites.log: not read: a symbolic link"));
}

#[test]
fn evolog_usage_errors() {
    let fx = Fx::new();
    assert_eq!(run(&fx, &["evolog"]).code, 2);
    let r = run(&fx, &["evolog", "no-such-rev"]);
    assert_eq!(r.code, 2, "{}", r.err);
    let [_, _, c] = three(&fx);
    assert_eq!(
        run(&fx, &["evolog", &c.to_string(), "--limit", "1"]).code,
        0
    );
    assert_eq!(
        run(&fx, &["evolog", &c.to_string(), "--limit", "0"]).code,
        2
    );
}

#[test]
fn evolog_writes_nothing() {
    let fx = Fx::new();
    let [a, _, c] = three(&fx);
    oplog(
        &fx,
        "rewrites.log",
        &rewrites(&[("amend", &a.to_string(), &c.to_string())]),
    );
    let before = tree_snapshot(&fx.path);
    let _ = run(&fx, &["evolog", &c.to_string()]);
    assert!(before == tree_snapshot(&fx.path));
}

// ---- undo-plan ----------------------------------------------------------------------------------------------------

/// Branch `work` at c3 (reflog: created at c1, then c2, c3); HEAD on `main` at c1.
fn undo_fx() -> (Fx, [ObjectId; 3]) {
    let fx = Fx::new();
    let [c1, c2, c3] = three(&fx);
    fx.branch("main", c1);
    fx.set_head("main");
    fx.branch("work", c3);
    reflog(
        &fx,
        "refs/heads/work",
        &[
            (
                Z,
                &c1.to_string(),
                1_700_000_000,
                "branch: Created from HEAD",
            ),
            (
                &c1.to_string(),
                &c2.to_string(),
                1_700_000_100,
                "commit: second",
            ),
            (
                &c2.to_string(),
                &c3.to_string(),
                1_700_000_200,
                "commit: third",
            ),
        ],
    );
    (fx, [c1, c2, c3])
}

fn checks(v: &Value, status: &str) -> Vec<String> {
    v["checks"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|c| c["status"] == status)
        .map(|c| c["text"].as_str().unwrap().to_string())
        .collect()
}

#[test]
fn a_plan_for_a_plain_branch_has_all_checks_ok() {
    let (fx, [_, c2, c3]) = undo_fx();
    let r = run(&fx, &["undo-plan", "work"]);
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert!(r.text.contains("Result: all checks ok"), "{}", r.text);
    let v = json(&fx, &["undo-plan", "work"]);
    assert_eq!(v["schema_version"], 1);
    assert_eq!(v["command"], "oplog undo-plan");
    assert_eq!(v["ref"], "refs/heads/work");
    assert_eq!(v["current"], c3.to_string());
    assert_eq!(v["target"], c2.to_string(), "--steps defaults to 1");
    assert_eq!(v["executable"], true);
    assert!(checks(&v, "refused").is_empty(), "{v}");
    assert!(
        checks(&v, "note")
            .iter()
            .any(|t| t.contains("ancestor") && t.contains("plain move back")),
        "{v}"
    );
    assert!(
        checks(&v, "note")
            .iter()
            .any(|t| t.starts_with("1 commits would be reachable only through the reflog")),
        "{v}"
    );
}

#[test]
fn to_and_steps_pick_the_old_value() {
    let (fx, [c1, c2, _]) = undo_fx();
    let v = json(&fx, &["undo-plan", "work", "--steps", "2"]);
    assert_eq!(v["target"], c1.to_string());
    let prefix = &c2.to_string()[..10];
    let v = json(&fx, &["undo-plan", "work", "--to", prefix]);
    assert_eq!(v["target"], c2.to_string());
    let v = json(&fx, &["undo-plan", "refs/heads/work", "--steps", "1"]);
    assert_eq!(v["ref"], "refs/heads/work");
}

#[test]
fn the_plan_refuses_what_undo_would_refuse() {
    let (fx, [c1, _, _]) = undo_fx();
    // checked out
    fx.set_head("work");
    let v = json(&fx, &["undo-plan", "work"]);
    assert!(
        checks(&v, "refused")
            .iter()
            .any(|t| t.contains("checked out") && t.contains("git reset --keep")),
        "{v}"
    );
    assert_eq!(v["executable"], false);
    assert_eq!(run(&fx, &["undo-plan", "work"]).code, 1);
    fx.set_head("main");
    // the creation entry: --steps 3 selects the entry whose old value is zero
    let v = json(&fx, &["undo-plan", "work", "--steps", "3"]);
    assert!(
        checks(&v, "refused")
            .iter()
            .any(|t| t.contains("created the ref")),
        "{v}"
    );
    // the reflog has fewer entries
    let v = json(&fx, &["undo-plan", "work", "--steps", "9"]);
    assert!(
        checks(&v, "refused")
            .iter()
            .any(|t| t.contains("fewer than 9")),
        "{v}"
    );
    // a value that is no old value of the log
    let v = json(&fx, &["undo-plan", "work", "--to", &h('e')]);
    assert!(
        checks(&v, "refused")
            .iter()
            .any(|t| t.contains("is not an old value")),
        "{v}"
    );
    // changed since the newest reflog entry
    fx.write(".git/refs/heads/work", &format!("{c1}\n"));
    let v = json(&fx, &["undo-plan", "work"]);
    assert!(
        checks(&v, "refused")
            .iter()
            .any(|t| t.contains("changed since")),
        "{v}"
    );
}

#[test]
fn a_running_operation_and_an_unknown_ref_and_a_symbolic_ref_are_refused() {
    let (fx, _) = undo_fx();
    fx.write(".git/MERGE_HEAD", &format!("{}\n", h('1')));
    let v = json(&fx, &["undo-plan", "work"]);
    assert!(
        checks(&v, "refused")
            .iter()
            .any(|t| t.contains("operation in progress") && t.contains("MERGE_HEAD")),
        "{v}"
    );
    std::fs::remove_file(fx.path.join(".git/MERGE_HEAD")).unwrap();

    let v = json(&fx, &["undo-plan", "nobranch"]);
    assert!(
        checks(&v, "refused")
            .iter()
            .any(|t| t.contains("does not exist")),
        "{v}"
    );
    assert_eq!(run(&fx, &["undo-plan", "nobranch"]).code, 1);

    fx.write(".git/refs/heads/alias", "ref: refs/heads/work\n");
    reflog(&fx, "refs/heads/alias", &[(Z, &h('1'), 1_700_000_000, "x")]);
    let v = json(&fx, &["undo-plan", "alias"]);
    assert!(
        checks(&v, "refused")
            .iter()
            .any(|t| t.contains("symbolic ref")),
        "{v}"
    );
}

#[test]
fn an_ambiguous_to_prefix_is_refused() {
    let fx = Fx::new();
    let [c1, _, c3] = three(&fx);
    fx.branch("main", c1);
    fx.set_head("main");
    fx.branch("work", c3);
    let (a, b) = (
        format!("ab{}", "1".repeat(38)),
        format!("ab{}", "2".repeat(38)),
    );
    reflog(
        &fx,
        "refs/heads/work",
        &[
            (Z, &a, 1_700_000_000, "x"),
            (&a, &b, 1_700_000_100, "y"),
            (&b, &c3.to_string(), 1_700_000_200, "z"),
        ],
    );
    let v = json(&fx, &["undo-plan", "work", "--to", "ab"]);
    assert!(
        checks(&v, "refused")
            .iter()
            .any(|t| t.contains("is ambiguous: 2 different old values")),
        "{v}"
    );
    assert!(v["target"].is_null(), "no target is chosen: {v}");
}

#[test]
fn a_stash_is_previewed_but_marked_not_undoable() {
    let (fx, [c1, c2, _]) = undo_fx();
    fx.set_ref("refs/stash", c2);
    reflog(
        &fx,
        "refs/stash",
        &[(&c1.to_string(), &c2.to_string(), 1_700_000_000, "WIP")],
    );
    let r = run(&fx, &["undo-plan", "refs/stash"]);
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert!(r.text.contains("handles local branches only"), "{}", r.text);
    let v = json(&fx, &["undo-plan", "refs/stash"]);
    assert_eq!(v["executable"], false);
    assert!(checks(&v, "refused").is_empty());
}

#[test]
fn the_plan_says_it_does_not_run_undo_and_writes_nothing() {
    let (fx, _) = undo_fx();
    let before = tree_snapshot(&fx.path);
    let r = run(&fx, &["undo-plan", "work"]);
    assert!(
        r.text.contains("`stw oplog undo` is not built yet"),
        "{}",
        r.text
    );
    assert!(before == tree_snapshot(&fx.path));
}

#[test]
fn undo_plan_usage_errors() {
    let (fx, _) = undo_fx();
    assert_eq!(run(&fx, &["undo-plan"]).code, 2);
    assert_eq!(
        run(&fx, &["undo-plan", "work", "--to", "abc", "--steps", "1"]).code,
        2
    );
    assert_eq!(run(&fx, &["undo-plan", "work", "--steps", "0"]).code, 2);
    assert_eq!(
        run(&fx, &["undo-plan", "work", "--to", "ABC"]).code,
        2,
        "upper case is not hex here"
    );
}

#[test]
fn the_help_names_the_three_commands() {
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .args(["oplog", "--help"])
        .output()
        .unwrap();
    let t = String::from_utf8_lossy(&o.stdout);
    for w in ["timeline", "evolog", "undo-plan"] {
        assert!(t.contains(w), "{t}");
    }
}
