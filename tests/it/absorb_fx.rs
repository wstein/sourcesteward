//! Fixtures of the `stw absorb` tests: throwaway repositories under the temp directory, built with the git binary and a
//! test-owned global config (identity, default branch; the user's own config is never read or written), and a runner for
//! the `stw` binary with a cleared environment. Every helper refuses a path outside the scratch directory.
#![allow(dead_code)]
use std::cell::Cell;
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::Value;

pub struct Out {
    pub rc: i32,
    pub out: String,
    pub err: String,
}

pub struct Fx {
    _tmp: tempfile::TempDir,
    /// The scratch directory of the test.
    pub s: PathBuf,
    /// The repository (`s/work/r`).
    pub r: PathBuf,
    pub cfg: PathBuf,
    /// Extra directory first on PATH (a git wrapper), when set.
    pub path_prefix: std::cell::RefCell<Option<PathBuf>>,
    tick: Cell<u32>,
}

pub fn lines(n: usize, prefix: &str) -> String {
    (1..=n).map(|i| format!("{prefix}{i:02}\n")).collect()
}

pub fn l(n: usize) -> String {
    lines(n, "l")
}

pub fn replace_line(text: &str, n: usize, new: &str) -> String {
    let mut v: Vec<&str> = text.split('\n').collect();
    v[n - 1] = new;
    v.join("\n")
}

pub fn insert_after(text: &str, n: usize, new: &str) -> String {
    let mut v: Vec<&str> = text.split('\n').collect();
    v.insert(n, new);
    v.join("\n")
}

fn under(path: &Path, root: &Path) -> bool {
    path.starts_with(root)
}

impl Fx {
    pub fn new() -> Fx {
        let tmp = tempfile::Builder::new()
            .prefix("stw-absorb-")
            .tempdir()
            .unwrap();
        let s = std::fs::canonicalize(tmp.path()).unwrap();
        assert!(
            s.is_absolute() && s.components().count() > 2,
            "scratch {s:?}"
        );
        for d in ["home", "bin", "work"] {
            std::fs::create_dir(s.join(d)).unwrap();
        }
        let cfg = s.join("gitconfig");
        std::fs::write(
            &cfg,
            "[user]\n\tname = Test\n\temail = t@example.invalid\n[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n[maintenance]\n\tauto = false\n",
        )
        .unwrap();
        let r = s.join("work/r");
        std::fs::create_dir(&r).unwrap();
        let fx = Fx {
            _tmp: tmp,
            s,
            r,
            cfg,
            path_prefix: Default::default(),
            tick: Cell::new(0),
        };
        fx.g(&["init", "-q"]);
        fx
    }

    fn path_env(&self) -> std::ffi::OsString {
        let base = std::env::var_os("PATH").unwrap();
        match &*self.path_prefix.borrow() {
            Some(p) => {
                let mut v = vec![p.clone()];
                v.extend(std::env::split_paths(&base));
                std::env::join_paths(v).unwrap()
            }
            None => base,
        }
    }

    fn base_command(&self, program: &Path, cwd: &Path) -> Command {
        assert!(
            cwd == self.s || under(cwd, &self.s),
            "path outside the scratch: {cwd:?}"
        );
        let mut c = Command::new(program);
        c.env_clear()
            .env("PATH", self.path_env())
            .env("HOME", self.s.join("home"))
            .env("GIT_CONFIG_GLOBAL", &self.cfg)
            .env("GIT_CONFIG_SYSTEM", "/dev/null")
            .env("GIT_TERMINAL_PROMPT", "0")
            .env("GIT_CEILING_DIRECTORIES", &self.s)
            .current_dir(cwd);
        c
    }

    /// `git` in the repository; fails the test on a non-zero exit. Returns trimmed stdout.
    pub fn g(&self, args: &[&str]) -> String {
        let o = self.g_in(&self.r, args, &[]);
        assert_eq!(o.rc, 0, "git {args:?} failed: {}", o.err);
        o.out.trim().to_string()
    }

    pub fn g_raw(&self, args: &[&str]) -> Vec<u8> {
        let o = self
            .base_command(&which_git(), &self.r)
            .arg("-C")
            .arg(&self.r)
            .args(args)
            .output()
            .unwrap();
        assert!(o.status.success(), "git {args:?}");
        o.stdout
    }

    pub fn g_in(&self, cwd: &Path, args: &[&str], env: &[(&str, &str)]) -> Out {
        let o = self
            .base_command(&which_git(), cwd)
            .arg("-C")
            .arg(cwd)
            .args(args)
            .envs(env.iter().copied())
            .output()
            .unwrap();
        Out {
            rc: o.status.code().unwrap_or(-1),
            out: String::from_utf8_lossy(&o.stdout).into_owned(),
            err: String::from_utf8_lossy(&o.stderr).into_owned(),
        }
    }

    pub fn g_ok(&self, args: &[&str]) -> Out {
        self.g_in(&self.r, args, &[])
    }

    pub fn write(&self, path: &str, content: impl AsRef<[u8]>) {
        let full = self.r.join(path);
        std::fs::create_dir_all(full.parent().unwrap()).unwrap();
        std::fs::write(full, content).unwrap();
    }

    /// Writes the files (`None` deletes), commits them and returns the SHA.
    pub fn commit(&self, files: &[(&str, Option<&str>)], msg: &str) -> String {
        self.commit_by(files, msg, None)
    }

    pub fn commit_by(
        &self,
        files: &[(&str, Option<&str>)],
        msg: &str,
        author: Option<(&str, &str)>,
    ) -> String {
        for (p, c) in files {
            match c {
                Some(c) => self.write(p, c),
                None => {
                    self.g(&["rm", "-q", "--", p]);
                }
            }
        }
        self.g(&["add", "-A", "--", "."]);
        self.tick.set(self.tick.get() + 1);
        let d = format!("2026-01-01T00:{:02}:00+00:00", self.tick.get());
        let msgfile = self.s.join("msg");
        std::fs::write(&msgfile, msg).unwrap();
        let mut env = vec![
            ("GIT_AUTHOR_DATE", d.as_str()),
            ("GIT_COMMITTER_DATE", d.as_str()),
        ];
        if let Some((n, e)) = author {
            env.push(("GIT_AUTHOR_NAME", n));
            env.push(("GIT_AUTHOR_EMAIL", e));
        }
        let o = self.g_in(
            &self.r,
            &[
                "commit",
                "-q",
                "--allow-empty",
                "-F",
                msgfile.to_str().unwrap(),
            ],
            &env,
        );
        assert_eq!(o.rc, 0, "commit failed: {}", o.err);
        self.g(&["rev-parse", "HEAD"])
    }

    /// Writes the files into the work tree and the index (`None` removes them).
    pub fn stage(&self, files: &[(&str, Option<&str>)]) {
        for (p, c) in files {
            match c {
                Some(c) => {
                    self.write(p, c);
                    self.g(&["add", "--", p]);
                }
                None => {
                    self.g(&["rm", "-q", "--", p]);
                }
            }
        }
    }

    /// `refs/remotes/origin/main` at `sha`: everything up to it counts as published.
    pub fn publish(&self, sha: &str) {
        self.g(&["update-ref", "refs/remotes/origin/main", sha]);
    }

    /// a.txt has l01..l10; one commit changes line 3 and one changes line 8. Returns (c1, c2, c3, text at HEAD).
    /// Without `publish` c1 creates the file; with it an initial commit creates it and is published, and c1 changes line 1.
    pub fn base10(&self, publish: bool) -> (String, String, String, String) {
        let mut t1 = l(10);
        if publish {
            self.commit(&[("a.txt", Some(&t1))], "chore: init");
            self.publish("HEAD");
            t1 = replace_line(&t1, 1, "l01 by c1");
        }
        let c1 = self.commit(&[("a.txt", Some(&t1))], "feat: one");
        let t2 = replace_line(&t1, 3, "l03 by c2");
        let c2 = self.commit(&[("a.txt", Some(&t2))], "feat: two");
        let t3 = replace_line(&t2, 8, "l08 by c3");
        let c3 = self.commit(&[("a.txt", Some(&t3))], "feat: three");
        (c1, c2, c3, t3)
    }

    /// c0 creates a.txt; commit k (k >= 1) changes line 2k+1 (odd lines: changes of neighbouring lines would touch).
    pub fn many(&self, n: usize) -> (Vec<String>, String) {
        let mut t = l(2 * n + 2);
        let mut shas = vec![self.commit(&[("a.txt", Some(&t))], "feat: c0")];
        for k in 1..n {
            t = replace_line(&t, 2 * k + 1, &format!("l{:02} by c{k}", 2 * k + 1));
            shas.push(self.commit(&[("a.txt", Some(&t))], &format!("feat: c{k}")));
        }
        (shas, t)
    }

    /// One file, one commit.
    pub fn commit1(&self, path: &str, content: &str, msg: &str) -> String {
        self.commit(&[(path, Some(content))], msg)
    }

    pub fn commit1_by(&self, path: &str, content: &str, msg: &str, who: (&str, &str)) -> String {
        self.commit_by(&[(path, Some(content))], msg, Some(who))
    }

    /// Stage one file.
    pub fn stage1(&self, path: &str, content: &str) {
        self.stage(&[(path, Some(content))]);
    }

    // --- the program under test ---------------------------------------------------------------------------------------

    pub fn stw_in(&self, cwd: &Path, args: &[&str], env: &[(&str, &str)]) -> Out {
        let o = self
            .base_command(Path::new(env!("CARGO_BIN_EXE_stw")), cwd)
            .arg("absorb")
            .args(args)
            .envs(env.iter().copied())
            .output()
            .unwrap();
        Out {
            rc: o.status.code().unwrap_or(-1),
            out: String::from_utf8_lossy(&o.stdout).into_owned(),
            err: String::from_utf8_lossy(&o.stderr).into_owned(),
        }
    }

    pub fn run(&self, args: &[&str]) -> Out {
        self.stw_in(&self.r, args, &[])
    }

    /// `stw` with the given arguments (not prefixed with `absorb`).
    pub fn stw(&self, args: &[&str]) -> Out {
        let o = self
            .base_command(Path::new(env!("CARGO_BIN_EXE_stw")), &self.r)
            .args(args)
            .output()
            .unwrap();
        Out {
            rc: o.status.code().unwrap_or(-1),
            out: String::from_utf8_lossy(&o.stdout).into_owned(),
            err: String::from_utf8_lossy(&o.stderr).into_owned(),
        }
    }

    pub fn run_env(&self, args: &[&str], env: &[(&str, &str)]) -> Out {
        self.stw_in(&self.r, args, env)
    }

    /// `stw absorb` with this PATH (for example one without git).
    pub fn run_with_path(&self, args: &[&str], path: &str) -> Out {
        let o = self
            .base_command(Path::new(env!("CARGO_BIN_EXE_stw")), &self.r)
            .env("PATH", path)
            .arg("absorb")
            .args(args)
            .output()
            .unwrap();
        Out {
            rc: o.status.code().unwrap_or(-1),
            out: String::from_utf8_lossy(&o.stdout).into_owned(),
            err: String::from_utf8_lossy(&o.stderr).into_owned(),
        }
    }

    /// The plan as JSON; the run must exit 0.
    pub fn plan(&self, args: &[&str]) -> Value {
        self.plan_in(&self.r, args)
    }

    pub fn plan_in(&self, cwd: &Path, args: &[&str]) -> Value {
        let mut a = vec!["--json"];
        a.extend_from_slice(args);
        let o = self.stw_in(cwd, &a, &[]);
        assert_eq!(o.rc, 0, "exit {}, stderr: {}", o.rc, o.err);
        assert!(!o.out.trim().is_empty(), "no output");
        serde_json::from_str(&o.out).unwrap_or_else(|e| panic!("{e}: {}", o.out))
    }

    /// A refusal: exit `rc`, the error document on stdout with `code`, the message also on stderr.
    pub fn error_rc(&self, code: &str, args: &[&str], env: &[(&str, &str)], rc: i32) -> Value {
        let mut a = vec!["--json"];
        a.extend_from_slice(args);
        let o = self.stw_in(&self.r, &a, env);
        assert_eq!(o.rc, rc, "exit {}, stderr {}", o.rc, o.err);
        assert!(!o.out.trim().is_empty(), "no output (stderr: {})", o.err);
        let d: Value = serde_json::from_str(&o.out).unwrap();
        assert_eq!(d["error"]["code"], code, "{}", o.out);
        assert_eq!(d["schema_version"], 1);
        assert_eq!(d["command"], "absorb");
        assert!(!o.err.trim().is_empty(), "message also on stderr");
        d
    }

    pub fn error(&self, code: &str, args: &[&str]) -> Value {
        self.error_rc(code, args, &[], 1)
    }

    /// A `git` first on PATH that logs its arguments and `GIT_OPTIONAL_LOCKS`/`GIT_NO_LAZY_FETCH`, then runs the real git.
    /// Returns the log file.
    pub fn logged_git(&self) -> PathBuf {
        let log = self.s.join("gitlog");
        let real = which_git();
        let script = format!(
            "#!/bin/sh\nprintf '%s\\037' \"$@\" >> {log}\nprintf '\\036%s\\n' \"$GIT_OPTIONAL_LOCKS$GIT_NO_LAZY_FETCH\" >> {log}\nexec {real} \"$@\"\n",
            log = log.display(),
            real = real.display()
        );
        self.install_git_wrapper(&script);
        log
    }

    pub fn install_git_wrapper(&self, script: &str) {
        use std::os::unix::fs::PermissionsExt;
        let p = self.s.join("bin/git");
        std::fs::write(&p, script).unwrap();
        std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
        *self.path_prefix.borrow_mut() = Some(self.s.join("bin"));
    }

    /// The calls the wrapper logged: (arguments, lock setting).
    pub fn calls(&self, log: &Path) -> Vec<(Vec<String>, String)> {
        let Ok(text) = std::fs::read_to_string(log) else {
            return vec![];
        };
        text.split('\n')
            .filter(|r| !r.is_empty())
            .map(|rec| {
                let (args, lock) = rec.split_once('\x1e').unwrap();
                let mut a: Vec<String> = args.split('\x1f').map(str::to_string).collect();
                a.pop();
                (a, lock.to_string())
            })
            .collect()
    }

    /// The calls of the stw program without the two that are no read of the repository: the one that gix makes itself to
    /// find the system configuration of the git installation (`git config -lz --show-origin ...`) and the runner's
    /// `git --version`. What stw starts.
    pub fn git_calls(&self, log: &Path) -> Vec<(Vec<String>, String)> {
        const GIX_CONFIG: [&str; 6] = [
            "config",
            "-lz",
            "--show-origin",
            "--no-includes",
            "--show-scope",
            "--name-only",
        ];
        self.calls(log)
            .into_iter()
            .filter(|(a, _)| a.iter().map(String::as_str).ne(GIX_CONFIG) && a != &["--version"])
            .collect()
    }

    /// The git subcommand of an argument vector (after the `-c`/`-C` options).
    pub fn subcommand(args: &[String]) -> Option<String> {
        let mut i = 0;
        while i < args.len() {
            let a = &args[i];
            if a == "-c" || a == "-C" {
                i += 2;
            } else if a.starts_with('-') {
                i += 1;
            } else {
                return Some(a.clone());
            }
        }
        None
    }

    /// Every entry below `root` (`.git` included): kind, mode, size and content; access times are not part of it.
    pub fn snapshot(root: &Path) -> BTreeMap<String, (String, u32, Vec<u8>)> {
        use std::os::unix::fs::MetadataExt;
        fn walk(dir: &Path, root: &Path, out: &mut BTreeMap<String, (String, u32, Vec<u8>)>) {
            let mut es: Vec<_> = std::fs::read_dir(dir)
                .unwrap()
                .map(|e| e.unwrap().path())
                .collect();
            es.sort();
            for p in es {
                let md = std::fs::symlink_metadata(&p).unwrap();
                let rel = p.strip_prefix(root).unwrap().to_string_lossy().into_owned();
                if md.file_type().is_symlink() {
                    out.insert(
                        rel,
                        (
                            "link".into(),
                            0,
                            std::fs::read_link(&p)
                                .unwrap()
                                .to_string_lossy()
                                .into_owned()
                                .into_bytes(),
                        ),
                    );
                } else if md.is_dir() {
                    out.insert(rel, ("dir".into(), md.mode(), vec![]));
                    walk(&p, root, out);
                } else {
                    out.insert(rel, ("file".into(), md.mode(), std::fs::read(&p).unwrap()));
                }
            }
        }
        let mut out = BTreeMap::new();
        walk(root, root, &mut out);
        out
    }
}

pub fn which_git() -> PathBuf {
    for d in std::env::split_paths(&std::env::var_os("PATH").unwrap()) {
        let p = d.join("git");
        if p.is_file() {
            return p;
        }
    }
    panic!("git is not installed");
}

pub fn hunk(p: &Value, i: usize) -> &Value {
    &p["hunks"][i]
}
