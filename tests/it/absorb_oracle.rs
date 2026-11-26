// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! Oracle of `stw absorb`: the Python spike `spikes/stw/absorb/stw-absorb-plan` (accepted by the owner) on the same
//! throwaway repositories. The plan document must be equal key for key, in order and byte for byte after parsing (the
//! spike prints `", "` and `": "` separators, stw prints none: the only difference of the bytes). Named scenarios (the
//! cases of the comparison list of the spike README) and pseudo-random ones (seeds 1 to 60), each in the default mode
//! and with `--ambiguous-newest`. git-absorb 0.9.0 is not installed on this host: the comparison with it is the one the
//! spike README reports. The test is skipped (and says so) when python3 or the spike is not there.
use crate::common::FixtureCommand as _;
use std::path::PathBuf;
use std::process::Command;

use serde_json::Value;

use super::absorb_fx::*;

fn spike() -> Option<PathBuf> {
    let p = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/spikes/absorb/stw-absorb-plan");
    p.is_file().then_some(p)
}

fn python_available() -> bool {
    Command::new("python3")
        .arg("--version")
        .output()
        .is_ok_and(|o| o.status.success())
}

fn run_spike(fx: &Fx, spike: &std::path::Path, args: &[&str]) -> (i32, String) {
    let o = Command::new("python3")
        .fixture_env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", fx.s.join("home"))
        .env("GIT_CONFIG_GLOBAL", &fx.cfg)
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_TERMINAL_PROMPT", "0")
        .env("GIT_CEILING_DIRECTORIES", &fx.s)
        .current_dir(&fx.s)
        .arg("-I")
        .arg(spike)
        .arg("--repo")
        .arg(&fx.r)
        .arg("--json")
        .args(args)
        .output()
        .unwrap();
    (
        o.status.code().unwrap_or(-1),
        String::from_utf8_lossy(&o.stdout).into_owned(),
    )
}

/// Equal documents, or equal error codes. Returns 1 when a document was compared.
fn compare(fx: &Fx, spike: &std::path::Path, name: &str, args: &[&str]) {
    let ours = fx.stw_in(&fx.r, &[&["--json"], args].concat(), &[]);
    let (rc, theirs) = run_spike(fx, spike, args);
    assert_eq!(
        ours.rc, rc,
        "{name} {args:?}: exit codes; ours {} / {}; spike {theirs}",
        ours.out, ours.err
    );
    let (a, b): (Value, Value) = (
        serde_json::from_str(&ours.out).unwrap(),
        serde_json::from_str(&theirs).unwrap(),
    );
    if rc != 0 {
        assert_eq!(a["error"]["code"], b["error"]["code"], "{name} {args:?}");
        return;
    }
    assert_eq!(
        serde_json::to_string(&a).unwrap(),
        serde_json::to_string(&b).unwrap(),
        "{name} {args:?}: the plan differs from the spike"
    );
}

type Scenario = fn(&Fx) -> Vec<Vec<String>>;

fn named() -> Vec<(&'static str, Scenario)> {
    vec![
        ("single", |fx| {
            let (_c1, _c2, _c3, t3) = fx.base10(false);
            fx.stage1("a.txt", &replace_line(&t3, 3, "l03 fixed"));
            vec![]
        }),
        ("two-hunks", |fx| {
            let (_c1, _c2, _c3, t3) = fx.base10(false);
            fx.stage1("a.txt", &replace_line(&replace_line(&t3, 8, "f8"), 3, "f3"));
            vec![]
        }),
        ("published-insert-and-fix", |fx| {
            let (_c1, _c2, _c3, t3) = fx.base10(true);
            fx.stage1(
                "a.txt",
                &replace_line(&insert_after(&t3, 5, "ins"), 3, "fixed"),
            );
            vec![]
        }),
        ("hunk-over-two-commits", |fx| {
            let t1 = l(6);
            fx.commit1("a.txt", &t1, "feat: one");
            let t2 = replace_line(&t1, 2, "l02 by c2");
            fx.commit1("a.txt", &t2, "feat: two");
            let t3 = replace_line(&t2, 3, "l03 by c3");
            fx.commit1("a.txt", &t3, "feat: three");
            fx.stage1("a.txt", &replace_line(&replace_line(&t3, 2, "x2"), 3, "x3"));
            vec![]
        }),
        ("deleted-lines", |fx| {
            let (_c1, _c2, _c3, t3) = fx.base10(false);
            let mut ls: Vec<&str> = t3.split('\n').collect();
            ls.remove(7);
            fx.stage1("a.txt", &ls.join("\n"));
            vec![]
        }),
        ("insertion-before-and-after-a-changed-line", |fx| {
            let (_c1, _c2, _c3, t3) = fx.base10(false);
            fx.stage1("a.txt", &insert_after(&t3, 3, "after three"));
            vec![]
        }),
        ("file-added-far-away", |fx| {
            fx.commit1("a.txt", &l(3), "feat: base");
            let t = lines(20, "n");
            fx.commit1("n.txt", &t, "feat: new file");
            fx.commit1("a.txt", &format!("{}more\n", l(3)), "feat: more");
            fx.stage1("n.txt", &replace_line(&t, 15, "far"));
            vec![]
        }),
        ("whitespace", |fx| {
            let (_c1, _c2, _c3, t3) = fx.base10(false);
            fx.stage1("a.txt", &replace_line(&t3, 3, "l03 by c2 "));
            vec![]
        }),
        ("crlf", |fx| {
            let t = l(6).replace('\n', "\r\n");
            fx.commit1("a.txt", &t, "feat: crlf");
            fx.commit1("b.txt", "x\n", "feat: other");
            fx.stage1("a.txt", &replace_line(&t, 3, "l03 fixed\r"));
            vec![]
        }),
        ("no-newline-at-end", |fx| {
            fx.commit1("a.txt", "one\ntwo\nthree", "feat: no eol");
            fx.commit1("b.txt", "x\n", "feat: other");
            fx.stage1("a.txt", "one\ntwo\nthree fixed");
            vec![]
        }),
        ("emptied-file", |fx| {
            let t = l(5);
            fx.commit1("a.txt", &t, "feat: one");
            fx.commit1("b.txt", "x\n", "feat: other");
            fx.stage1("a.txt", "");
            vec![]
        }),
        ("insertion-at-start-and-end", |fx| {
            let t = l(8);
            fx.commit1("a.txt", &t, "chore: init");
            fx.publish("HEAD");
            let t2 = replace_line(&t, 1, "l01 mine");
            fx.commit1("a.txt", &t2, "feat: two");
            fx.stage1("a.txt", &format!("top\n{t2}bottom\n"));
            vec![]
        }),
        ("repeated-lines", |fx| {
            let t = "same\nsame\nsame\nx\nsame\nsame\n";
            fx.commit1("a.txt", t, "feat: one");
            let t2 = "same\nsame\nsame\nsame\nx\nsame\nsame\n";
            fx.commit1("a.txt", t2, "feat: two");
            fx.stage1("a.txt", "same\nsame\nx\nsame\nsame\n");
            vec![]
        }),
        ("two-files", |fx| {
            fx.commit(
                &[("a.txt", Some(&l(5))), ("b.txt", Some(&lines(5, "b")))],
                "feat: one",
            );
            let b2 = replace_line(&lines(5, "b"), 2, "b02 by c2");
            fx.commit1("b.txt", &b2, "feat: two");
            fx.stage(&[
                ("a.txt", Some(&replace_line(&l(5), 2, "fixed a"))),
                ("b.txt", Some(&replace_line(&b2, 2, "fixed b"))),
            ]);
            vec![]
        }),
        ("stack-of-13", |fx| {
            let (_shas, t) = fx.many(13);
            fx.stage1("a.txt", &replace_line(&t, 3, "fixed"));
            vec![vec!["--max-stack".into(), "20".into()]]
        }),
        ("foreign-in-the-middle", |fx| {
            let t = l(8);
            fx.commit1("a.txt", &t, "feat: mine");
            let t2 = replace_line(&t, 2, "l02 foreign");
            fx.commit1_by("a.txt", &t2, "feat: theirs", ("Other", "o@example.invalid"));
            let t3 = replace_line(&t2, 6, "l06 mine");
            fx.commit1("a.txt", &t3, "feat: mine again");
            fx.stage1(
                "a.txt",
                &replace_line(&replace_line(&t3, 6, "fixed6"), 2, "fixed2"),
            );
            vec![]
        }),
        ("other-branch", |fx| {
            let t = l(8);
            fx.commit1("a.txt", &t, "feat: one");
            fx.g(&["branch", "other"]);
            let t2 = replace_line(&t, 6, "l06 mine");
            fx.commit1("a.txt", &t2, "feat: two");
            fx.stage1("a.txt", &replace_line(&replace_line(&t2, 6, "f6"), 2, "f2"));
            vec![]
        }),
        ("merge-in-the-span", |fx| {
            fx.commit1("a.txt", &l(6), "feat: one");
            fx.g(&["checkout", "-q", "-b", "side"]);
            fx.commit1("s.txt", "s\n", "feat: side");
            fx.g(&["checkout", "-q", "main"]);
            fx.commit1("m.txt", "m\n", "feat: main");
            fx.g(&["merge", "-q", "--no-ff", "-m", "merge side", "side"]);
            fx.g(&["branch", "-q", "-D", "side"]);
            let t3 = replace_line(&l(6), 5, "by c3");
            fx.commit1("a.txt", &t3, "feat: after merge");
            fx.stage1("a.txt", &replace_line(&t3, 2, "fixed"));
            vec![]
        }),
        ("explicit-base", |fx| {
            let (shas, t) = fx.many(6);
            fx.stage1("a.txt", &replace_line(&t, 5, "fixed"));
            vec![
                vec!["--base".into(), shas[1].clone()],
                vec!["--base".into(), "HEAD~2".into()],
            ]
        }),
        ("files-skipped", |fx| {
            fx.write("bin.dat", [0u8, 1, 2]);
            fx.commit(
                &[
                    ("a.txt", Some(&l(4))),
                    ("old.txt", Some("gone\n")),
                    ("m.sh", Some("echo\n")),
                ],
                "feat: base",
            );
            fx.stage(&[("new.txt", Some("new\n")), ("old.txt", None)]);
            fx.write("bin.dat", [0u8, 1, 3]);
            fx.g(&["add", "bin.dat"]);
            fx.stage1("a.txt", &replace_line(&l(4), 2, "fixed"));
            vec![]
        }),
        ("odd-paths", |fx| {
            let names = ["dir/a b.txt", "q\"uote.txt", "üä.txt"];
            let files: Vec<(&str, Option<&str>)> = names
                .iter()
                .map(|n| (*n, Some("l01\nl02\nl03\n")))
                .collect();
            fx.commit(&files, "feat: odd names");
            fx.commit1("z.txt", "z\n", "feat: other");
            let fixed = replace_line("l01\nl02\nl03\n", 2, "fixed");
            let staged: Vec<(&str, Option<&str>)> =
                names.iter().map(|n| (*n, Some(fixed.as_str()))).collect();
            fx.stage(&staged);
            vec![]
        }),
        ("masked-subject-and-branch", |fx| {
            let t1 = l(4);
            let name = "a\u{1b}\u{202e}b.txt";
            fx.commit1(name, &t1, "feat: evil \u{202e}\u{7} subject\u{200b} end");
            fx.commit1("z.txt", "z\n", "feat: other");
            fx.g(&["checkout", "-q", "-b", "feat/\u{202e}x"]);
            fx.stage1(name, &replace_line(&t1, 2, "f"));
            vec![]
        }),
        ("duplicate-subjects", |fx| {
            let t1 = l(6);
            fx.commit1("a.txt", &t1, "fix: same");
            fx.commit1("b.txt", "x\n", "fix: same");
            fx.stage1("a.txt", &replace_line(&t1, 2, "f2"));
            vec![]
        }),
        ("multi-line-subject", |fx| {
            let t1 = l(6);
            fx.commit1(
                "a.txt",
                &t1,
                "feat: first line\ncontinued on the second\n\nbody\n",
            );
            fx.commit1("b.txt", "x\n", "feat: other");
            fx.stage1("a.txt", &replace_line(&t1, 2, "f2"));
            vec![]
        }),
        ("nothing-staged", |fx| {
            fx.base10(false);
            vec![]
        }),
        ("not-owned-head", |fx| {
            fx.commit1_by(
                "a.txt",
                &l(4),
                "feat: theirs",
                ("Other", "o@example.invalid"),
            );
            fx.stage1("a.txt", &format!("{}x\n", l(4)));
            vec![]
        }),
        ("published-head", |fx| {
            fx.commit1("a.txt", &l(4), "feat: one");
            fx.publish("HEAD");
            fx.stage1("a.txt", &format!("{}x\n", l(4)));
            vec![]
        }),
    ]
}

struct Rng(u64);
impl Rng {
    fn next(&mut self, n: usize) -> usize {
        self.0 = self
            .0
            .wrapping_mul(6364136223846793005)
            .wrapping_add(1442695040888963407);
        ((self.0 >> 33) as usize) % n
    }
}

fn mutate(rng: &mut Rng, text: &str, tag: &str) -> String {
    let mut ls: Vec<String> = text.lines().map(str::to_string).collect();
    match rng.next(3) {
        0 if !ls.is_empty() => {
            let i = rng.next(ls.len());
            ls[i] = format!("{tag}-{}", rng.next(1000));
        }
        1 => {
            let i = rng.next(ls.len() + 1);
            ls.insert(i, format!("{tag}-ins-{}", rng.next(1000)));
        }
        _ if ls.len() > 3 => {
            let i = rng.next(ls.len());
            ls.remove(i);
        }
        _ => {}
    }
    ls.iter().map(|l| format!("{l}\n")).collect()
}

fn random_scenario(seed: u64, fx: &Fx) {
    let mut rng = Rng(seed.wrapping_mul(0x9E37_79B9_7F4A_7C15));
    let nfiles = 1 + rng.next(3);
    let repeated = seed.is_multiple_of(7);
    let mut texts: Vec<String> = (0..nfiles)
        .map(|f| {
            if repeated {
                (0..12)
                    .map(|i| format!("{}\n", ["same", "same", "x", "y"][(i + f) % 4]))
                    .collect()
            } else {
                lines(12, &format!("f{f}-"))
            }
        })
        .collect();
    let names: Vec<String> = (0..nfiles).map(|f| format!("f{f}.txt")).collect();
    let all: Vec<(&str, Option<&str>)> = names
        .iter()
        .zip(&texts)
        .map(|(n, t)| (n.as_str(), Some(t.as_str())))
        .collect();
    fx.commit(&all, "chore: init");
    if seed % 2 == 1 {
        fx.publish("HEAD");
    }
    for c in 0..(3 + rng.next(7)) {
        let f = rng.next(nfiles);
        texts[f] = mutate(&mut rng, &texts[f], &format!("c{c}"));
        fx.commit1(&names[f], &texts[f], &format!("feat: c{c}"));
    }
    for _ in 0..(1 + rng.next(2)) {
        let f = rng.next(nfiles);
        let new = mutate(&mut rng, &texts[f], "s");
        fx.stage1(&names[f], &new);
        texts[f] = new;
    }
}

#[test]
fn plans_equal_the_spike_on_named_scenarios() {
    let Some(spike) = spike().filter(|_| python_available()) else {
        eprintln!("python3 or the spike is not available: not run");
        return;
    };
    let mut n = 0;
    for (name, build) in named() {
        let fx = Fx::new();
        let extra = build(&fx);
        let mut variants: Vec<Vec<String>> = vec![vec![], vec!["--ambiguous-newest".into()]];
        variants.extend(extra);
        for v in variants {
            let refs: Vec<&str> = v.iter().map(String::as_str).collect();
            compare(&fx, &spike, name, &refs);
            n += 1;
        }
    }
    eprintln!("{n} comparisons equal");
}

#[test]
fn plans_equal_the_spike_on_random_repositories() {
    let Some(spike) = spike().filter(|_| python_available()) else {
        eprintln!("python3 or the spike is not available: not run");
        return;
    };
    let mut n = 0;
    for seed in 1..=60u64 {
        let fx = Fx::new();
        random_scenario(seed, &fx);
        for args in [vec![], vec!["--ambiguous-newest"], vec!["--max-stack", "3"]] {
            compare(&fx, &spike, &format!("seed {seed}"), &args);
            n += 1;
        }
    }
    eprintln!("{n} comparisons equal");
}
