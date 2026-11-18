//! Oracle of `stw tidy plan` against real repositories (package 4). All tests are `#[ignore]`: they run only with
//! `--ignored`, read paths from the environment, are read-only, and skip with a message when a variable is missing.
//! The driver is `tests/oracle/tidy_oracle.sh` (clones, fetches, report). Variables:
//!   STW_ORACLE_HUNK_REPO        a clone of a repository with many commits (hunk oracle and algorithm matrix)
//!   STW_ORACLE_HUNK_COMMITS     commits for the hunk oracle (default 300)
//!   STW_ORACLE_MATRIX_COMMITS   commits for the algorithm matrix (default 200)
//!   STW_ORACLE_HUNK_WINDOW      newest commits the sample is taken from (default 3000)
//!   STW_ORACLE_REBASE_REPOS     `clone:raw.json` entries separated by commas; the clone has the head of every merged pull
//!                               request as the branch `oracle-pr-<number>` (the driver fetches `refs/pull/<n>/head`)
//!   STW_ORACLE_STRICT           `1`: the rebase oracle fails on a finding (default: a finding is a result)
//!   STW_ORACLE_OUT              directory for the result files (absolute; outside a Git tree or ignored by it); without it only stdout
//! Findings are results, not failures: the tests fail only when the harness cannot compare (nothing compared, an
//! unreadable file).
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::Instant;

use gix::ObjectId;
use stw::github::GithubState;
use stw::repo::RepoRef;
use stw::tidy::{plan_repo_with_opts, to_json, PlanOpts, PrProof, Similarity};
use stw::tidy_oracle::{
    branch_view, judge, parse_truth, parse_unified_u0, render_verdicts, text_keys, Matrix, Verdict,
};
use stw::tidy_similarity::{commit_entries, Entries, Keys, Limits};

const PR_BRANCH: &str = "oracle-pr-";

fn var(name: &str) -> Option<String> {
    std::env::var(name).ok().filter(|v| !v.is_empty())
}

fn number(name: &str, default: usize) -> usize {
    var(name)
        .map(|v| {
            v.parse()
                .unwrap_or_else(|_| panic!("{name} is not a number"))
        })
        .unwrap_or(default)
}

/// The directory for results: absolute, and either outside a Git tree or ignored by it; None without the variable.
fn out_dir() -> Option<PathBuf> {
    let v = var("STW_ORACLE_OUT")?;
    let p = PathBuf::from(&v);
    assert!(p.is_absolute(), "STW_ORACLE_OUT must be absolute: {v}");
    std::fs::create_dir_all(&p).unwrap();
    let real = std::fs::canonicalize(&p).unwrap();
    // inside a Git work tree only where the tree ignores the directory (the scratch folder of a workspace)
    let q = |args: &[&str]| {
        Command::new("git")
            .arg("-C")
            .arg(&real)
            .args(args)
            .output()
            .expect("git can be started")
    };
    let inside = q(&["rev-parse", "--is-inside-work-tree"]);
    if String::from_utf8_lossy(&inside.stdout).trim() == "true" {
        assert!(
            q(&["check-ignore", "-q", "."]).status.success(),
            "STW_ORACLE_OUT is inside a Git tree that does not ignore it: {}",
            real.display()
        );
    }
    Some(real)
}

fn write_out(name: &str, text: &str) {
    println!("{text}");
    if let Some(d) = out_dir() {
        std::fs::write(d.join(name), text).unwrap();
    }
}

fn git(repo: &Path, args: &[&str]) -> Vec<u8> {
    let o = Command::new("git")
        .arg("-C")
        .arg(repo)
        .args(args)
        .output()
        .expect("git can be started");
    assert!(
        o.status.success(),
        "git {args:?} failed: {}",
        String::from_utf8_lossy(&o.stderr)
    );
    o.stdout
}

fn git_text(repo: &Path, args: &[&str]) -> String {
    String::from_utf8_lossy(&git(repo, args)).trim().to_string()
}

/// Non-merge commits with their parent, newest first, `want` of them spread evenly over the newest `window`.
fn sample(repo: &Path, window: usize, want: usize) -> Vec<(String, String)> {
    let n = format!("-n{window}");
    let log = git_text(
        repo,
        &[
            "log",
            "--no-merges",
            "--min-parents=1",
            "--format=%H %P",
            &n,
            "HEAD",
        ],
    );
    let all: Vec<(String, String)> = log
        .lines()
        .filter_map(|l| {
            let mut f = l.split(' ');
            Some((f.next()?.to_string(), f.next()?.to_string()))
        })
        .collect();
    if all.len() <= want {
        return all;
    }
    (0..want)
        .map(|i| all[i * all.len() / want].clone())
        .collect()
}

/// The keys of git for one commit and one setting.
///
/// Entries of the paths in `binary` (the files gix takes for binary) are left out: with `--text` git prints their lines,
/// gix has one marker entry that the comparison does not use.
fn git_keys(
    repo: &Path,
    parent: &str,
    id: &str,
    flags: &[&str],
    binary: &std::collections::HashSet<Vec<u8>>,
) -> Result<Keys, String> {
    let mut args = vec![
        "-c",
        "core.quotepath=off",
        "diff",
        "-U0",
        "--no-renames",
        "--no-color",
        "--no-ext-diff",
        "--no-textconv",
        "--text",
    ];
    args.extend_from_slice(flags);
    args.extend_from_slice(&[parent, id]);
    let out = git(repo, &args);
    let mut entries = parse_unified_u0(&out)?;
    entries.retain(|e| !binary.contains(&e.path));
    Ok(text_keys(&entries))
}

/// The settings of `git diff`: name and flags. The first has none: what the configuration of the clone makes of it.
const SETTINGS: &[(&str, &[&str])] = &[
    ("git-default", &[]),
    (
        "git-myers+indent",
        &["--diff-algorithm=myers", "--indent-heuristic"],
    ),
    (
        "git-myers-noindent",
        &["--diff-algorithm=myers", "--no-indent-heuristic"],
    ),
    (
        "git-histogram+indent",
        &["--diff-algorithm=histogram", "--indent-heuristic"],
    ),
    (
        "git-histogram-noindent",
        &["--diff-algorithm=histogram", "--no-indent-heuristic"],
    ),
    (
        "git-patience+indent",
        &["--diff-algorithm=patience", "--indent-heuristic"],
    ),
    (
        "git-patience-noindent",
        &["--diff-algorithm=patience", "--no-indent-heuristic"],
    ),
    (
        "git-minimal+indent",
        &["--diff-algorithm=minimal", "--indent-heuristic"],
    ),
    (
        "git-minimal-noindent",
        &["--diff-algorithm=minimal", "--no-indent-heuristic"],
    ),
];

struct Run {
    matrix: Matrix,
    commits: usize,
    skipped_gix: usize,
    unreadable: usize,
}

fn compare(repo: &Path, commits: &[(String, String)], settings: &[(&str, &[&str])]) -> Run {
    let g = gix::open(repo).expect("the clone opens");
    let lim = Limits::default();
    let mut run = Run {
        matrix: Matrix::default(),
        commits: commits.len(),
        skipped_gix: 0,
        unreadable: 0,
    };
    for (id, parent) in commits {
        let oid = ObjectId::from_hex(id.as_bytes()).unwrap();
        let mut binary = std::collections::HashSet::new();
        let mine = match commit_entries(&g, oid, &lim).expect("gix reads the commit") {
            Entries::Lines(l) => {
                binary.extend(l.iter().filter(|e| e.sign == b'B').map(|e| e.path.clone()));
                text_keys(&l)
            }
            Entries::Skipped(_) => {
                run.skipped_gix += 1;
                continue;
            }
        };
        let mut unreadable = false;
        for (name, flags) in settings {
            match git_keys(repo, parent, id, flags, &binary) {
                Ok(theirs) => run.matrix.add(name, id, mine.similarity(&theirs)),
                Err(_) => unreadable = true,
            }
        }
        run.unreadable += unreadable as usize;
    }
    run
}

fn header(repo: &Path, run: &Run) -> String {
    let cfg = |k: &str| {
        let o = Command::new("git")
            .arg("-C")
            .arg(repo)
            .args(["config", "--get", k])
            .output()
            .unwrap();
        let v = String::from_utf8_lossy(&o.stdout).trim().to_string();
        if v.is_empty() {
            "unset".into()
        } else {
            v
        }
    };
    format!(
        "repository: {}\ngit: {}\ndiff.algorithm: {}, diff.indentHeuristic: {}\ncommits sampled: {}, skipped by gix (merge, empty, too large): {}, unreadable for the parser: {}\n",
        repo.file_name().unwrap().to_string_lossy(),
        git_text(repo, &["--version"]),
        cfg("diff.algorithm"),
        cfg("diff.indentHeuristic"),
        run.commits,
        run.skipped_gix,
        run.unreadable
    )
}

fn hunk_repo() -> Option<PathBuf> {
    match var("STW_ORACLE_HUNK_REPO") {
        Some(p) => Some(PathBuf::from(p)),
        None => {
            println!("skipped: set STW_ORACLE_HUNK_REPO to a clone");
            None
        }
    }
}

/// The keys of gix (Myers, no slider heuristic) against `git diff -U0 --no-renames` as the prototype ran it.
#[test]
#[ignore]
fn hunk_oracle_against_default_git() {
    let Some(repo) = hunk_repo() else { return };
    let want = number("STW_ORACLE_HUNK_COMMITS", 300);
    let commits = sample(&repo, number("STW_ORACLE_HUNK_WINDOW", 3000), want);
    let run = compare(&repo, &commits, &SETTINGS[..1]);
    let t = run.matrix.get("git-default").expect("a row");
    assert!(t.total > 0, "no commit was compared");
    let text = format!(
        "{}\n{}\n{}",
        header(&repo, &run),
        run.matrix.render_table(),
        run.matrix.render_counterexamples()
    );
    write_out("hunk.md", &text);
}

/// The same commits against every `diff.algorithm` with and without the indent heuristic.
#[test]
#[ignore]
fn algorithm_matrix_against_gix() {
    let Some(repo) = hunk_repo() else { return };
    let want = number("STW_ORACLE_MATRIX_COMMITS", 200);
    let commits = sample(&repo, number("STW_ORACLE_HUNK_WINDOW", 3000), want);
    let run = compare(&repo, &commits, SETTINGS);
    assert!(
        run.matrix.get("git-default").is_some_and(|t| t.total > 0),
        "no commit was compared"
    );
    let text = format!(
        "{}\n{}\n{}",
        header(&repo, &run),
        run.matrix.render_table(),
        run.matrix.render_counterexamples()
    );
    write_out("matrix.md", &text);
}

/// Every merged pull request of the raw data, its head as a branch, against the plan of the clone.
#[test]
#[ignore]
fn rebase_oracle_against_github() {
    let Some(list) = var("STW_ORACLE_REBASE_REPOS") else {
        println!("skipped: set STW_ORACLE_REBASE_REPOS to clone:raw.json[,clone:raw.json]");
        return;
    };
    let mut report = String::new();
    let mut findings = 0;
    for entry in list.split(',') {
        let (clone, raw) = entry
            .rsplit_once(':')
            .unwrap_or_else(|| panic!("no clone:raw.json in {entry}"));
        let clone = PathBuf::from(clone);
        let raw: serde_json::Value =
            serde_json::from_slice(&std::fs::read(raw).expect("the raw data reads")).unwrap();
        let truths = parse_truth(&raw).expect("the raw data parses");
        assert!(!truths.is_empty(), "no merged pull request in {entry}");
        let name = clone.file_name().unwrap().to_string_lossy().to_string();
        let opts = PlanOpts {
            backup_days: 7,
            now: stw::tidy::now_secs(),
            similarity: Some(Similarity::default()),
            rebase: true,
        };
        let r = RepoRef {
            name: name.clone(),
            path: clone.clone(),
        };
        let proof = PrProof {
            prs: None,
            note: "oracle: pull requests not read".into(),
            api: Default::default(),
        };
        let started = Instant::now();
        let plan = plan_repo_with_opts(&r, proof, &opts);
        let secs = started.elapsed().as_secs_f64();
        assert!(plan.error.is_none(), "{name}: {:?}", plan.error);
        let json = to_json(
            "/oracle",
            &GithubState {
                client: None,
                note: None,
            },
            std::slice::from_ref(&plan),
        );
        let g = gix::open(&clone).expect("the clone opens");
        let default = plan
            .base
            .as_deref()
            .map(|b| {
                b.strip_prefix("refs/remotes/origin/")
                    .unwrap_or(b)
                    .to_string()
            })
            .unwrap_or_default();
        let mut rows: Vec<(u64, Verdict)> = Vec::new();
        for t in &truths {
            let branch = format!("{PR_BRANCH}{}", t.number);
            let tip = g
                .find_reference(format!("refs/heads/{branch}").as_str())
                .ok()
                .map(|mut r| r.peel_to_id().map(|i| i.to_string()).unwrap_or_default());
            let verdict = match tip {
                None => Verdict::Miss("the head of the pull request was not fetched".into()),
                Some(tip) if tip != t.head_sha => Verdict::Miss(format!(
                    "the head of the pull request is now {tip}, GitHub said {} when the raw data was read",
                    t.head_sha
                )),
                Some(_) => judge(t, branch_view(&json, &branch).as_ref(), &default),
            };
            rows.push((t.number, verdict));
        }
        // what carried the mapping: the proof of each branch, the methods and the notes of the mapping entries
        let mut proofs: std::collections::BTreeMap<String, u64> = Default::default();
        let (mut patch, mut ident, mut simil, mut tz, mut amb, mut later) =
            (0u64, 0u64, 0u64, 0, 0, 0);
        for t in &truths {
            let name = format!("{PR_BRANCH}{}", t.number);
            let Some(b) = json["repos"][0]["branches"]
                .as_array()
                .and_then(|l| l.iter().find(|b| b["branch"] == name.as_str()))
            else {
                continue;
            };
            *proofs
                .entry(b["proof"].as_str().unwrap_or("").to_string())
                .or_default() += 1;
            let r = &b["rebase"];
            patch += r["by_patch_id"].as_u64().unwrap_or(0);
            ident += r["by_identity"].as_u64().unwrap_or(0);
            simil += r["by_similarity"].as_u64().unwrap_or(0);
            for m in r["mapping"].as_array().into_iter().flatten() {
                tz += (m["tz_differs"] == true) as u64;
                amb += (m["ambiguous"] == true) as u64;
                later += (m["later"] == true) as u64;
            }
        }
        findings += rows
            .iter()
            .filter(|r| matches!(r.1, Verdict::Miss(_)))
            .count();
        report.push_str(&render_verdicts(&name, &rows));
        report.push_str(&format!(
            "  plan (rebase stage on, offline): {secs:.2} s, {} branches\n  proofs of the pull request branches: {proofs:?}\n  commits mapped by patch-id {patch}, identity {ident}, similarity {simil}; time zone differs {tz}, ambiguous {amb}, later version {later}\n",
            plan.branches.len()
        ));
        for (n, v) in &rows {
            if let Verdict::Miss(_) = v {
                let b = format!("{PR_BRANCH}{n}");
                if let Some(bp) = plan.branches.iter().find(|x| x.branch == b) {
                    report.push_str(&format!("    #{n} detail: {}\n", bp.detail));
                }
            }
        }
    }
    write_out("rebase.md", &report);
    if var("STW_ORACLE_STRICT").as_deref() == Some("1") {
        assert_eq!(findings, 0, "findings:\n{report}");
    }
}

// ---- the comparison itself, on a throw-away repository (no network, no clone) ----

/// A file that `.gitattributes` marks `-diff` is a text diff for gix and "Binary files differ" for git unless git is
/// told `--text`; a commit with a path that git quotes is unreadable for the parser once, not once per setting.
#[test]
fn the_comparison_reads_text_in_files_marked_binary_and_counts_unreadable_per_commit() {
    use crate::common::{Fx, Stamp};
    let fx = Fx::new();
    std::fs::write(fx.path.join(".gitattributes"), "bin.txt -diff\n").unwrap();
    let s = Stamp::new("A", "a@example.com", 1_700_000_000);
    let c0 = fx.commit_at(
        None,
        &[("bin.txt", Some("one\n")), ("a\tb.txt", Some("x\n"))],
        "c0",
        s,
        s,
    );
    let c1 = fx.commit_at(Some(c0), &[("bin.txt", Some("two\n"))], "c1", s, s);
    let c2 = fx.commit_at(Some(c1), &[("a\tb.txt", Some("y\n"))], "c2", s, s);
    let commits = vec![
        (c1.to_string(), c0.to_string()),
        (c2.to_string(), c1.to_string()),
    ];
    let run = compare(&fx.path, &commits, &SETTINGS[1..3]);
    assert_eq!(run.unreadable, 1, "one commit, two settings");
    for name in ["git-myers+indent", "git-myers-noindent"] {
        let t = run.matrix.get(name).expect("a row");
        assert_eq!((t.total, t.differing), (1, 0), "{name}");
    }
}

/// With `--text` git prints the lines of a file with a NUL byte; gix has one marker entry for it. The path of such a file
/// is left out on the side of git, so that a commit that changes a binary file still compares equal.
#[test]
fn a_file_with_a_nul_byte_is_left_out_on_both_sides() {
    use crate::common::{Fx, Stamp};
    let fx = Fx::new();
    let s = Stamp::new("A", "a@example.com", 1_700_000_000);
    let c0 = fx.commit_at(
        None,
        &[("nul.dat", Some("a\0b\n")), ("t.txt", Some("x\n"))],
        "c0",
        s,
        s,
    );
    let c1 = fx.commit_at(
        Some(c0),
        &[("nul.dat", Some("c\0d\n")), ("t.txt", Some("y\n"))],
        "c1",
        s,
        s,
    );
    let commits = vec![(c1.to_string(), c0.to_string())];
    let run = compare(&fx.path, &commits, &SETTINGS[1..3]);
    for name in ["git-myers+indent", "git-myers-noindent"] {
        let t = run.matrix.get(name).expect("a row");
        assert_eq!((t.total, t.differing), (1, 0), "{name}");
    }
}
