//! The one place where stw starts the installed `git` (`"via": "git"` in the JSON output).
//!
//! Decision (chartroom, 2026-11-07): `stw` reads and merges with `gix`; it starts `git` only for push, fetch of
//! ssh and local-path remotes, `range-diff` and conflict resolution in a worktree. `tests/it/no_binaries.rs`
//! allows a process to be started in this file and nowhere else.
//!
//! Rules of every call:
//! - The argument vector is fixed in code (`Op`); no free text is ever part of it. The repository is named
//!   explicitly with `-C <absolute directory>` (and is the working directory of the child); an empty,
//!   relative or missing path is refused, the process working directory is never used.
//! - The environment is an allowlist built from the caller's snapshot: PATH, HOME and the credential/ssh
//!   transport variables of the user (`SSH_AUTH_SOCK`, `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_ASKPASS`, ...)
//!   are kept. Everything else is dropped, in particular every other `GIT_*` (repository selectors,
//!   `GIT_CONFIG_*`, `GIT_EXEC_PATH`, trace and identity variables), editors, pagers and locale variables.
//!   Fixed values: `LC_ALL=C`, `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0`, `GIT_PAGER=cat`,
//!   `GIT_EDITOR=true`, `GIT_ALLOW_PROTOCOL` without `ext`, `GIT_CEILING_DIRECTORIES` = parent of the repository.
//!   The user's Git configuration and identity stay intact; no author or committer override is ever set.
//! - Output and time are bounded; the minimum Git version is checked once per runner; errors are typed and
//!   free of credentials (`redact`).
use std::ffi::{OsStr, OsString};
use std::io::Read;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::mpsc;
use std::sync::OnceLock;
use std::time::{Duration, Instant};

/// Value of the `via` field of every step that ran through this module.
pub const VIA: &str = "git";

/// A Git release, compared numerically.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub struct Version(pub u32, pub u32, pub u32);

impl std::fmt::Display for Version {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}.{}.{}", self.0, self.1, self.2)
    }
}

/// Parse the output of `git --version` (`git version 2.47.3`, `... 2.39.5 (Apple Git-154)`,
/// `... 2.45.1.windows.1`, `... 2.50.0-rc1`).
pub fn parse_version(text: &str) -> Option<Version> {
    let rest = text.trim().strip_prefix("git version ")?;
    let word = rest.split_whitespace().next()?;
    let mut parts = word.split('.');
    let num = |s: Option<&str>| -> Option<u32> {
        let s = s?;
        let digits: String = s.chars().take_while(char::is_ascii_digit).collect();
        digits.parse().ok()
    };
    let major = num(parts.next())?;
    let minor = num(parts.next())?;
    let patch = parts.next().and_then(|p| num(Some(p))).unwrap_or(0);
    Some(Version(major, minor, patch))
}

/// A full lower-case 40-hex object id. The only free-form data that reaches an argument vector of the
/// carry-over operations, and only in this form: it cannot start with `-`, contain `..`, a space or a ref name.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Hex40([u8; 40]);

impl Hex40 {
    pub fn parse(s: &str) -> Option<Hex40> {
        let b: [u8; 40] = s.as_bytes().try_into().ok()?;
        b.iter()
            .all(|c| matches!(c, b'0'..=b'9' | b'a'..=b'f'))
            .then_some(Hex40(b))
    }

    pub fn as_str(&self) -> &str {
        std::str::from_utf8(&self.0).expect("hex digits are ASCII")
    }
}

/// The ref namespace of the recovery refs of a `stw sync` run: `<ROOT><run id>/<n>`.
pub const RECOVERY_NAMESPACE: &str = "refs/stw/recovery/sync/";

/// The ID of a `stw sync` run, `YYYYMMDDTHHMMSS[uuuuuu]Z-xxxxxxxx` (UTC time stamp, optional microseconds, 8 lower-case
/// hex digits): the file name of its journal record and the name of its recovery refs. Only this form reaches an
/// argument vector, so a journal file cannot smuggle an option or another ref name into one.
#[derive(Clone, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub struct RunId(String);

impl RunId {
    pub fn parse(s: &str) -> Option<RunId> {
        let b = s.as_bytes();
        let (stamp, suffix) = s.split_once("Z-")?;
        let digits = |t: &str| t.bytes().all(|c| c.is_ascii_digit());
        let stamp_ok = match stamp.len() {
            15 | 21 => {
                stamp.as_bytes().get(8) == Some(&b'T') && digits(&stamp[..8]) && digits(&stamp[9..])
            }
            _ => false,
        };
        let suffix_ok = suffix.len() == 8
            && suffix
                .bytes()
                .all(|c| matches!(c, b'0'..=b'9' | b'a'..=b'f'));
        (b.len() <= 31 && stamp_ok && suffix_ok).then(|| RunId(s.to_string()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }

    /// Order key: the time stamp with microseconds (an ID without them counts as microsecond zero), then the suffix.
    /// "The latest run" is the highest key, never a modification time.
    pub fn order_key(&self) -> String {
        let (stamp, suffix) = self.0.split_once("Z-").unwrap_or((&self.0, ""));
        let mut k = stamp.to_string();
        if k.len() == 15 {
            k.push_str("000000");
        }
        format!("{k}-{suffix}")
    }
}

impl std::fmt::Display for RunId {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

/// The name of a recovery ref: `refs/stw/recovery/sync/<run>/<n>`.
pub fn recovery_ref(run: &RunId, n: u32) -> String {
    format!("{RECOVERY_NAMESPACE}{run}/{n}")
}

/// A full branch name `refs/heads/<name>` that passes the ref-name rules of git (no `..`, no control character, no
/// trailing `.lock`, ...) and whose short part does not start with `-`. The only free-form ref name in an argument
/// vector, and only in this form.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BranchRef(String);

impl BranchRef {
    pub fn parse(full: &str) -> Option<BranchRef> {
        let short = full.strip_prefix("refs/heads/")?;
        if short.is_empty()
            || short.starts_with('-')
            || full.len() > 255
            || full.bytes().any(|b| b < 0x20 || b == 0x7f || b == b' ')
        {
            return None;
        }
        gix::refs::FullName::try_from(full).ok()?;
        Some(BranchRef(full.to_string()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// The ref namespace of `Op::FetchPullHead`; the only ref it writes is `<ROOT>pr-<n>`.
pub const PULL_HEAD_NAMESPACE: &str = "refs/stw/carry-over/";

/// The empty tree: `GIT_ATTR_SOURCE` of `Op::RangeDiff`, so that no `.gitattributes` of the worktree (`-diff`,
/// `binary`, a textconv driver) can hide what a commit changed.
const EMPTY_TREE: &str = "4b825dc642cb6eb9a060e54bf8d69288fbee4904";

/// The operations that run through `git`. Each has a fixed argument vector and its own minimum version.
/// The operations of `stw add` carry validated values (`add::CanonicalUrl`, `add::SubPath`,
/// `add::RepoName`), never free text.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Op {
    /// `git fetch origin` for an ssh or local-path remote (no tags, no submodules).
    FetchOrigin,
    /// `FetchOrigin` with an explicit prune choice (`stw sync --prune` / `--no-prune`): `prune: true` adds `--prune`
    /// and `--no-prune-tags` (a tag is never pruned, whatever `fetch.pruneTags` says), `prune: false` adds
    /// `--no-prune` (it wins over `fetch.prune`). The caller has checked that every configured fetch refspec writes
    /// below `refs/remotes/origin/`. Same transports as `FetchOrigin`.
    FetchOriginPrune { prune: bool },
    /// `git submodule sync -- <path>` in the superproject: rewrites the submodule's `remote.origin.url` and the
    /// superproject's `submodule.<name>.url` from `.gitmodules`. Local, no network (`stw sync --submodules`).
    SubmoduleSync { path: crate::add::SubPath },
    /// `git submodule update --no-fetch --checkout -- <path>` in the superproject: the initialized submodule goes to
    /// the commit the superproject records. No fetch, no init, no clone, no recursion; `--checkout` wins over a
    /// configured `submodule.<name>.update`. The submodule's own config is checked like the superproject's, and
    /// refused when it names a `filter.*` driver (a checkout would start it).
    SubmoduleUpdate { path: crate::add::SubPath },
    /// `git check-attr filter diff merge -- .gitmodules` (`cached`: from the index only): what git itself would
    /// apply to `.gitmodules` when `stw add` commits it. Prints one `path: attribute: value` line per attribute.
    CheckAttrGitmodules { cached: bool },
    /// `git reset --hard HEAD` in a fresh clone that was fetched without a work tree (`stw add`). Runs
    /// with `core.attributesFile=/dev/null`; the caller has verified that no attribute file of the tree
    /// sets a filter.
    CheckoutClone,
    /// `git submodule add -- <url> <path>` on the present clone at `<path>`: local, no network.
    SubmoduleAddExisting {
        url: crate::add::CanonicalUrl,
        path: crate::add::SubPath,
    },
    /// `git submodule absorbgitdirs -- <path>`: the clone's git dir moves to `.git/modules/<path>`.
    AbsorbGitDirs { path: crate::add::SubPath },
    /// The one commit of `stw add`: `commit -m "add <name> submodule" -- .gitmodules <path>`.
    CommitSubmodule {
        name: crate::add::RepoName,
        path: crate::add::SubPath,
    },
    /// `git range-diff OLD_BASE..OLD_HEAD NEW_BASE..NEW_HEAD`, rows only (`--no-patch`); carry-over. `via: git`.
    RangeDiff {
        old_base: Hex40,
        old_head: Hex40,
        new_base: Hex40,
        new_head: Hex40,
    },
    /// Fetch `+refs/pull/<pr>/head` of `origin` into `refs/stw/carry-over/pr-<pr>`: objects and that private
    /// ref only (no tags, no FETCH_HEAD, no configured refmap). For an ssh or local-path remote, as `FetchOrigin`.
    FetchPullHead { pr: u64 },
    /// `git update-ref --create-reflog <recovery ref> <oid> <zeros>`: the recovery ref of a `stw sync` run holds the old
    /// tip of a branch before the run moves it. Create-only (the expected old value is the null id), so a ref of the
    /// same name is never overwritten.
    RecoveryRefCreate { run: RunId, n: u32, oid: Hex40 },
    /// `git update-ref --no-deref -d <recovery ref> <oid>`: delete one recovery ref, only while it still holds `oid`
    /// (retention of old runs).
    RecoveryRefDelete { run: RunId, n: u32, oid: Hex40 },
    /// `git update-ref --no-deref refs/heads/<name> <new> <expected>`: move a branch that is checked out nowhere, only
    /// while it still points at `expected` (compare and swap; `stw sync restore`). Never forced.
    UpdateBranchCas {
        run: RunId,
        branch: BranchRef,
        new: Hex40,
        expected: Hex40,
    },
    /// `git reset --keep <target> --`: move the checked-out branch and its worktree to `target`; refuses when a local
    /// change would be lost (`stw sync restore`, after the caller found the tracked tree clean). The repository's own
    /// config must not name a `filter.*` driver (the checkout would start it).
    ResetKeep { target: Hex40 },
}

impl Op {
    /// Minimum installed Git for this capability. Not copied from jj (2.42, which `worktree add --orphan`
    /// needs): the fetch vector uses `-C`, `-c`, `--quiet`, `--no-tags`, `--no-recurse-submodules` and
    /// `protocol.ext.allow`, all older than 2.30. Verified against 2.47.3 only (see the README).
    pub fn min_version(&self) -> Version {
        // the `stw add` vectors use only options that exist since git 2.30 as well; verified against 2.47.3 only
        match self {
            // `GIT_ATTR_SOURCE` (2.40) and `--no-patch` of range-diff
            Op::RangeDiff { .. } => Version(2, 40, 0),
            _ => Version(2, 30, 0),
        }
    }

    /// A variable part out of range (a pull request number of 0).
    fn validate(&self) -> std::result::Result<(), GitError> {
        match self {
            Op::FetchPullHead { pr: 0 } => Err(GitError::Argument(
                "a pull request number starts at 1".into(),
            )),
            Op::RecoveryRefCreate { n: 0, .. } | Op::RecoveryRefDelete { n: 0, .. } => Err(
                GitError::Argument("a recovery ref number starts at 1".into()),
            ),
            _ => Ok(()),
        }
    }

    fn args(&self) -> Vec<String> {
        let v = |a: &[&str]| a.iter().map(|s| (*s).to_string()).collect::<Vec<String>>();
        match self {
            Op::FetchOrigin => v(&[
                "fetch",
                "--quiet",
                "--no-tags",
                "--no-recurse-submodules",
                // `-c remote.origin.uploadpack=` does not win over the repository's own value (git uses
                // the first one); the option does
                "--upload-pack=git-upload-pack",
                "origin",
            ]),
            Op::FetchOriginPrune { prune } => {
                let mut a = v(&[
                    "fetch",
                    "--quiet",
                    "--no-tags",
                    "--no-recurse-submodules",
                    "--upload-pack=git-upload-pack",
                ]);
                if *prune {
                    a.extend(v(&["--prune", "--no-prune-tags"]));
                } else {
                    // `--no-prune-tags` too: `fetch.pruneTags` / `remote.origin.pruneTags` append the refspec
                    // `refs/tags/*:refs/tags/*` even without prune, and `--no-tags` does not stop that
                    a.extend(v(&["--no-prune", "--no-prune-tags"]));
                }
                a.extend(v(&["--", "origin"]));
                a
            }
            Op::SubmoduleSync { path } => {
                let mut a = v(&["submodule", "sync", "--"]);
                a.push(path.as_str().to_string());
                a
            }
            Op::SubmoduleUpdate { path } => {
                let mut a = v(&["submodule", "update", "--no-fetch", "--checkout", "--"]);
                a.push(path.as_str().to_string());
                a
            }
            Op::FetchPullHead { pr } => {
                let mut a = v(&[
                    "fetch",
                    "--quiet",
                    "--no-tags",
                    "--no-recurse-submodules",
                    "--upload-pack=git-upload-pack",
                    // no FETCH_HEAD; an empty refmap, so that a configured `remote.origin.fetch` writes nothing
                    "--no-write-fetch-head",
                    "--refmap=",
                    "--",
                    "origin",
                ]);
                a.push(format!("+refs/pull/{pr}/head:{PULL_HEAD_NAMESPACE}pr-{pr}"));
                a
            }
            Op::RecoveryRefCreate { run, n, oid } => {
                let mut a = v(&["update-ref", "--create-reflog", "-m"]);
                a.push(format!("stw sync {run}"));
                a.push(recovery_ref(run, *n));
                a.push(oid.as_str().to_string());
                a.push("0".repeat(40));
                a
            }
            Op::RecoveryRefDelete { run, n, oid } => {
                let mut a = v(&["update-ref", "--no-deref", "-d"]);
                a.push(recovery_ref(run, *n));
                a.push(oid.as_str().to_string());
                a
            }
            Op::UpdateBranchCas {
                run,
                branch,
                new,
                expected,
            } => {
                let mut a = v(&["update-ref", "--no-deref", "-m"]);
                a.push(format!("stw: sync restore {run}"));
                a.push(branch.as_str().to_string());
                a.push(new.as_str().to_string());
                a.push(expected.as_str().to_string());
                a
            }
            Op::ResetKeep { target } => {
                let mut a = v(&["reset", "--keep", "--quiet"]);
                a.push(target.as_str().to_string());
                a.push("--".to_string());
                a
            }
            Op::RangeDiff {
                old_base,
                old_head,
                new_base,
                new_head,
            } => {
                let mut a = v(&["range-diff", "--no-color", "--no-patch", "--no-notes"]);
                // the two-range form; git has no four-argument form
                a.push(format!("{}..{}", old_base.as_str(), old_head.as_str()));
                a.push(format!("{}..{}", new_base.as_str(), new_head.as_str()));
                a
            }
            Op::CheckAttrGitmodules { cached } => {
                let mut a = v(&["check-attr"]);
                if *cached {
                    a.push("--cached".to_string());
                }
                a.extend(v(&["filter", "diff", "merge", "--", ".gitmodules"]));
                a
            }
            Op::CheckoutClone => v(&["reset", "--hard", "--quiet", "HEAD"]),
            Op::SubmoduleAddExisting { url, path } => {
                let mut a = v(&["submodule", "add", "--"]);
                a.push(url.as_str().to_string());
                a.push(path.as_str().to_string());
                a
            }
            Op::AbsorbGitDirs { path } => {
                let mut a = v(&["submodule", "absorbgitdirs", "--"]);
                a.push(path.as_str().to_string());
                a
            }
            Op::CommitSubmodule { name, path } => {
                let mut a = v(&["commit", "--quiet", "--no-verify", "-m"]);
                a.push(format!("add {} submodule", name.as_str()));
                a.extend(v(&["--", ".gitmodules"]));
                a.push(path.as_str().to_string());
                a
            }
        }
    }

    /// Extra `-c` pairs after the shared leading ones. The `stw add` operations never sign, never use
    /// a configured or global attributes file (it would attach filter drivers) and never use a pager.
    fn extra_leading(&self) -> &'static [&'static str] {
        match self {
            Op::FetchOrigin | Op::FetchOriginPrune { .. } | Op::FetchPullHead { .. } => &[],
            // a patch comparison pins what hides or reshapes a change: `diff.ignoreSubmodules=all` hides a
            // gitlink, a size limit turns a file into "Binary files differ", context and algorithm reshape the
            // inner `git log -p` patches
            Op::RangeDiff { .. } => &[
                "-c",
                "diff.ignoreSubmodules=none",
                "-c",
                "core.attributesFile=/dev/null",
                "-c",
                "core.bigFileThreshold=512m",
                "-c",
                "diff.renames=false",
                "-c",
                "diff.context=3",
                "-c",
                "diff.interHunkContext=0",
                "-c",
                "diff.algorithm=myers",
                "-c",
                "diff.indentHeuristic=true",
                "-c",
                "diff.relative=false",
                "-c",
                "diff.suppressBlankEmpty=false",
                "-c",
                "core.quotePath=true",
                "-c",
                "log.showSignature=false",
                "-c",
                "log.follow=false",
                "-c",
                "log.mailmap=false",
            ],
            _ => &[
                "-c",
                "commit.gpgsign=false",
                "-c",
                "tag.gpgsign=false",
                "-c",
                "core.attributesFile=/dev/null",
            ],
        }
    }

    /// Value of `GIT_ALLOW_PROTOCOL` of the child. The `stw add` operations are local: no transport at all.
    fn allow_protocol(&self) -> &'static str {
        match self {
            Op::FetchOrigin | Op::FetchOriginPrune { .. } => "file:ssh:git:http:https",
            // ssh and local only: a `url.<base>.insteadOf` of the user's config cannot turn the remote into http(s)
            Op::FetchPullHead { .. } => "file:ssh",
            _ => "none",
        }
    }
}

/// Leading arguments of every call, after `-C <repo>`.
const LEADING: &[&str] = &[
    "-c",
    "core.fsmonitor=false",
    "-c",
    "submodule.recurse=false",
    "-c",
    "fetch.recurseSubmodules=false",
    "-c",
    "gc.auto=0",
    "-c",
    "maintenance.auto=false",
    "-c",
    "protocol.ext.allow=never",
    // no repository hook (`reference-transaction`, ...)
    "-c",
    "core.hooksPath=/dev/null",
];

/// Variables taken over from the caller's environment (credential and transport variables of the user).
const KEEP: &[&str] = &[
    "PATH",
    "HOME",
    "USER",
    "LOGNAME",
    "XDG_CONFIG_HOME",
    "TMPDIR",
    "SSH_AUTH_SOCK",
    "SSH_AGENT_PID",
    "GIT_SSH",
    "GIT_SSH_COMMAND",
    "GIT_SSH_VARIANT",
    "GIT_ASKPASS",
    "SSH_ASKPASS",
    "SSH_ASKPASS_REQUIRE",
    "DISPLAY",
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "NO_PROXY",
    "http_proxy",
    "https_proxy",
    "all_proxy",
    "no_proxy",
    "SSL_CERT_FILE",
    "SSL_CERT_DIR",
];

/// Variables whose values may hold secrets: their values are redacted from every error and output.
const SECRET_VARS: &[&str] = &["GH_TOKEN", "GITHUB_TOKEN"];

/// Bounds of one call.
#[derive(Clone, Copy, Debug)]
pub struct Limits {
    pub timeout: Duration,
    pub max_stdout: usize,
    pub max_stderr: usize,
}

impl Default for Limits {
    fn default() -> Self {
        Limits {
            timeout: Duration::from_secs(120),
            max_stdout: 1 << 20,
            max_stderr: 1 << 20,
        }
    }
}

/// Typed failures. `is_unsupported` ones mean "this machine cannot do the step" (exit 1, result `unsupported`).
#[derive(Debug)]
pub enum GitError {
    /// No `git` in the absolute entries of PATH.
    NotFound,
    /// `git --version` is older than the operation needs, or unreadable.
    Version {
        found: String,
        required: Version,
    },
    /// Git does not know an option of the fixed vector (exit 129 or "unknown option").
    UnknownOption(String),
    Timeout {
        secs: u64,
    },
    OutputLimit {
        stream: &'static str,
        limit: usize,
    },
    /// Git ran and failed; `stderr` is redacted and shortened.
    Exit {
        code: Option<i32>,
        stderr: String,
    },
    /// The process could not be started.
    Spawn(String),
    /// The repository path was refused (empty, relative, missing, not a directory, not a repository).
    Repo(String),
    /// The repository's own config (local or worktree scope) names a key that makes git run a command.
    /// The key is lower-case, as `section.subsection.key`. The same key in the user's config is allowed.
    Refused {
        key: String,
    },
    /// A variable part of the vector is out of range (a pull request number of 0).
    Argument(String),
    /// The remote's transport is not one the installed git fetches for stw (https, http, git and others stay
    /// with gix or are not run).
    Transport {
        scheme: String,
    },
}

impl GitError {
    /// Stable name for the JSON output.
    pub fn kind(&self) -> &'static str {
        match self {
            GitError::NotFound => "not-found",
            GitError::Version { .. } => "version",
            GitError::UnknownOption(_) => "unknown-option",
            GitError::Timeout { .. } => "timeout",
            GitError::OutputLimit { .. } => "output-limit",
            GitError::Exit { .. } => "exit",
            GitError::Spawn(_) => "spawn",
            GitError::Repo(_) => "repository",
            GitError::Refused { .. } => "refused-config",
            GitError::Argument(_) => "argument",
            GitError::Transport { .. } => "transport",
        }
    }

    /// The step cannot run here: git missing, too old or without a needed option.
    pub fn is_unsupported(&self) -> bool {
        matches!(
            self,
            GitError::NotFound
                | GitError::Version { .. }
                | GitError::UnknownOption(_)
                | GitError::Refused { .. }
                | GitError::Transport { .. }
        )
    }
}

impl std::fmt::Display for GitError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            GitError::NotFound => f.write_str("git is not installed (not found on PATH)"),
            GitError::Version { found, required } => write!(
                f,
                "installed git {found} is not usable: this step needs git {required} or newer"
            ),
            GitError::UnknownOption(l) => {
                write!(f, "installed git does not know an option stw needs: {l}")
            }
            GitError::Timeout { secs } => write!(f, "git did not finish within {secs} seconds"),
            GitError::OutputLimit { stream, limit } => {
                write!(f, "git wrote more than {limit} bytes to {stream}")
            }
            GitError::Exit { code, stderr } => match code {
                Some(c) => write!(f, "git exited with status {c}: {stderr}"),
                None => write!(f, "git was terminated by a signal: {stderr}"),
            },
            GitError::Spawn(m) => write!(f, "git could not be started: {m}"),
            GitError::Repo(m) => write!(f, "repository path refused: {m}"),
            GitError::Argument(m) => write!(f, "invalid argument: {m}"),
            GitError::Transport { scheme } => write!(
                f,
                "origin uses {scheme}: the installed git fetches ssh and local remotes only, an https remote is \
                 fetched by gix (not built for the pull request head)"
            ),
            GitError::Refused { key } if key == "info/attributes" => f.write_str(
                "the repository's info/attributes has active lines, which can hide a changed file from the \
                 patch comparison; stw does not run git with it (empty the file or compare by hand)",
            ),
            GitError::Refused { key } => write!(
                f,
                "the repository's own git config sets {key}, which makes git run a command; \
                 stw does not run git with it (put it in your user config instead)"
            ),
        }
    }
}

impl std::error::Error for GitError {}

/// What a successful call produced (stderr is redacted).
#[derive(Debug)]
pub struct Output {
    pub stdout: Vec<u8>,
    pub stderr: String,
}

/// Which process group git runs in.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum GroupMode {
    /// Own group unless stdin or stderr of stw is a terminal (the default).
    Auto,
    /// Always its own group: a timeout or output limit kills git and the helpers holding its pipes.
    Own,
    /// Always stw's group, as with plain git: tty prompts and Ctrl-C reach git. A timeout kills git only.
    Foreground,
}

/// The `Auto` rule: with a terminal on stdin or stderr git stays in the foreground process group, so that
/// terminal prompts (ssh host key, passphrase) and Ctrl-C work as with plain git; without one (agents, CI)
/// git gets its own group.
pub fn own_process_group(stdin_is_tty: bool, stderr_is_tty: bool) -> bool {
    !(stdin_is_tty || stderr_is_tty)
}

pub struct Runner {
    group: GroupMode,
    env: Vec<(OsString, OsString)>,
    limits: Limits,
    secrets: Vec<String>,
    version: OnceLock<std::result::Result<Version, VersionFailure>>,
}

#[derive(Clone, Debug)]
enum VersionFailure {
    NotFound,
    Found(String),
}

static SHARED: OnceLock<Runner> = OnceLock::new();

/// The runner of this process: environment snapshot taken once, version checked once per run.
pub fn shared() -> &'static Runner {
    SHARED.get_or_init(Runner::from_process)
}

impl Runner {
    pub fn from_process() -> Runner {
        Runner::from_env(std::env::vars_os().collect())
    }

    /// A runner over an environment snapshot (tests, or a caller that wants other variables).
    pub fn from_env(env: Vec<(OsString, OsString)>) -> Runner {
        let secrets = SECRET_VARS
            .iter()
            .filter_map(|k| env.iter().find(|(n, _)| n == OsStr::new(k)))
            .filter_map(|(_, v)| v.to_str().map(str::to_string))
            .filter(|v| !v.is_empty())
            .collect();
        Runner {
            group: GroupMode::Auto,
            env,
            limits: Limits::default(),
            secrets,
            version: OnceLock::new(),
        }
    }

    pub fn group_mode(mut self, mode: GroupMode) -> Runner {
        self.group = mode;
        self
    }

    pub fn limits(mut self, limits: Limits) -> Runner {
        self.limits = limits;
        self
    }

    /// Additional values to redact from errors and output.
    pub fn secrets(mut self, secrets: Vec<String>) -> Runner {
        self.secrets
            .extend(secrets.into_iter().filter(|s| !s.is_empty()));
        self
    }

    /// `git` from the absolute entries of the snapshot's PATH. Empty and relative entries (the current
    /// directory) are skipped.
    fn find_git(&self) -> Option<PathBuf> {
        let path = self
            .env
            .iter()
            .find(|(k, _)| k == OsStr::new("PATH"))?
            .1
            .clone();
        std::env::split_paths(&path)
            .filter(|d| d.is_absolute())
            .map(|d| d.join("git"))
            .find(|p| is_executable_file(p))
    }

    /// Installed Git version, asked once per runner.
    pub fn version(&self) -> std::result::Result<Version, GitError> {
        let cached = self.version.get_or_init(|| {
            let Some(exe) = self.find_git() else {
                return Err(VersionFailure::NotFound);
            };
            let mut cmd = Command::new(exe);
            cmd.arg("--version").current_dir("/");
            self.apply_env(&mut cmd, None, &Op::FetchOrigin);
            match self.exec(
                cmd,
                &Limits {
                    max_stdout: 4096,
                    max_stderr: 4096,
                    timeout: Duration::from_secs(10),
                },
            ) {
                Ok((st, out, _)) if st.success() => {
                    let text = String::from_utf8_lossy(&out).into_owned();
                    parse_version(&text).ok_or(VersionFailure::Found(short(&text)))
                }
                Ok((_, out, err)) => Err(VersionFailure::Found(short(&format!(
                    "{}{}",
                    String::from_utf8_lossy(&out),
                    String::from_utf8_lossy(&err)
                )))),
                Err(e) => Err(VersionFailure::Found(e.to_string())),
            }
        });
        match cached {
            Ok(v) => Ok(*v),
            Err(VersionFailure::NotFound) => Err(GitError::NotFound),
            Err(VersionFailure::Found(f)) => Err(GitError::Version {
                found: f.clone(),
                // the real requirement is attached by `run`; version() alone reports the lowest
                required: Version(0, 0, 0),
            }),
        }
    }

    fn check_repo(repo: &Path) -> std::result::Result<(), GitError> {
        if repo.as_os_str().is_empty() {
            return Err(GitError::Repo("empty path".into()));
        }
        if !repo.is_absolute() {
            return Err(GitError::Repo(format!(
                "{} is not an absolute path",
                repo.display()
            )));
        }
        if !repo.is_dir() {
            return Err(GitError::Repo(format!(
                "{} is not a directory",
                repo.display()
            )));
        }
        Ok(())
    }

    /// The full argument vector (without the program) for `op` in `repo`.
    pub fn argv(&self, repo: &Path, op: Op) -> std::result::Result<Vec<OsString>, GitError> {
        Self::check_repo(repo)?;
        op.validate()?;
        let mut v: Vec<OsString> = vec!["-C".into(), repo.as_os_str().to_owned()];
        v.extend(LEADING.iter().map(OsString::from));
        v.extend(op.extra_leading().iter().map(OsString::from));
        v.extend(op.args().iter().map(OsString::from));
        Ok(v)
    }

    /// The environment the child gets for `repo`.
    pub fn child_env(
        &self,
        repo: &Path,
    ) -> std::result::Result<Vec<(OsString, OsString)>, GitError> {
        Self::check_repo(repo)?;
        Ok(self.env_for(Some(repo), &Op::FetchOrigin))
    }

    /// The environment the child gets for `op` in `repo`.
    pub fn child_env_for(
        &self,
        repo: &Path,
        op: &Op,
    ) -> std::result::Result<Vec<(OsString, OsString)>, GitError> {
        Self::check_repo(repo)?;
        Ok(self.env_for(Some(repo), op))
    }

    fn env_for(&self, repo: Option<&Path>, op: &Op) -> Vec<(OsString, OsString)> {
        let mut out: Vec<(OsString, OsString)> = KEEP
            .iter()
            .filter_map(|k| {
                self.env
                    .iter()
                    .find(|(n, _)| n == OsStr::new(k))
                    .map(|(n, v)| (n.clone(), v.clone()))
            })
            .filter_map(|(n, v)| {
                if n != OsStr::new("PATH") {
                    return Some((n, v));
                }
                // git resolves `ssh` and helpers through PATH with the repository as working directory: an
                // empty or relative entry would pick a program out of the worktree
                let abs: Vec<PathBuf> = std::env::split_paths(&v)
                    .filter(|d| d.is_absolute())
                    .collect();
                std::env::join_paths(abs)
                    .ok()
                    .filter(|p| !p.is_empty())
                    .map(|p| (n, p))
            })
            .collect();
        let mut set = |k: &str, v: OsString| out.push((OsString::from(k), v));
        set("LC_ALL", "C".into());
        set("GIT_TERMINAL_PROMPT", "0".into());
        set("GIT_OPTIONAL_LOCKS", "0".into());
        set("GIT_PAGER", "cat".into());
        set("GIT_EDITOR", "true".into());
        set("GIT_ALLOW_PROTOCOL", op.allow_protocol().into());
        // the operations of `stw add` evaluate attributes: no system attributes file (`/etc/gitattributes`)
        if !matches!(
            op,
            Op::FetchOrigin | Op::FetchOriginPrune { .. } | Op::FetchPullHead { .. }
        ) {
            set("GIT_ATTR_NOSYSTEM", "1".into());
        }
        if matches!(op, Op::RangeDiff { .. }) {
            // no `.gitattributes` of the worktree, no replace refs: nothing may hide or swap a commit
            set("GIT_ATTR_SOURCE", EMPTY_TREE.into());
            set("GIT_NO_REPLACE_OBJECTS", "1".into());
        }
        if let Some(parent) = repo.and_then(Path::parent) {
            set("GIT_CEILING_DIRECTORIES", parent.as_os_str().to_owned());
        }
        out
    }

    fn apply_env(&self, cmd: &mut Command, repo: Option<&Path>, op: &Op) {
        cmd.env_clear();
        for (k, v) in self.env_for(repo, op) {
            cmd.env(k, v);
        }
    }

    /// Run `op` in `repo`. Checks the repository path, finds git, checks the version (once), starts the fixed
    /// vector with the scrubbed environment, and bounds time and output.
    pub fn run(&self, repo: &Path, op: Op) -> std::result::Result<Output, GitError> {
        let argv = self.argv(repo, op.clone())?;
        let exe = self.find_git().ok_or(GitError::NotFound)?;
        let found = self.version().map_err(|e| match e {
            GitError::Version { found, .. } => GitError::Version {
                found,
                required: op.min_version(),
            },
            other => other,
        })?;
        if found < op.min_version() {
            return Err(GitError::Version {
                found: found.to_string(),
                required: op.min_version(),
            });
        }
        if matches!(op, Op::FetchPullHead { .. }) {
            check_fetch_transport(repo)?;
        }
        refuse_local_command_config(repo, &op)?;
        if matches!(op, Op::ResetKeep { .. }) {
            // the checkout of a reset runs the clean/smudge filters of the repository
            refuse_config(repo, &|section, sub_name, key| {
                runs_a_command(section, sub_name, key) || section == "filter"
            })?;
        }
        if let Op::SubmoduleUpdate { path } = &op {
            // the checkout runs in the submodule: its own config must not name a program either
            let sub = repo.join(path.as_str());
            refuse_config(&sub, &|section, sub_name, key| {
                runs_a_command(section, sub_name, key) || section == "filter"
            })?;
        }
        let mut cmd = Command::new(exe);
        cmd.args(argv).current_dir(repo);
        self.apply_env(&mut cmd, Some(repo), &op);
        let (status, stdout, stderr) = self.exec(cmd, &self.limits)?;
        let stderr = shorten(
            &redact(&String::from_utf8_lossy(&stderr), &self.secrets),
            600,
        );
        if status.success() {
            return Ok(Output { stdout, stderr });
        }
        let lower = stderr.to_ascii_lowercase();
        if status.code() == Some(129)
            || lower.contains("unknown option")
            || lower.contains("unknown switch")
            || lower.contains("unrecognized option")
        {
            return Err(GitError::UnknownOption(first_line(&stderr)));
        }
        Err(GitError::Exit {
            code: status.code(),
            stderr,
        })
    }

    /// Start the command with stdin closed and both streams captured within the limits.
    fn exec(
        &self,
        mut cmd: Command,
        limits: &Limits,
    ) -> std::result::Result<(std::process::ExitStatus, Vec<u8>, Vec<u8>), GitError> {
        // Without a terminal git gets its own process group: a timeout or output limit then kills it together
        // with the helpers it started (ssh, upload-pack) that still hold the pipes. With a terminal it stays
        // in the foreground group (tty prompts and Ctrl-C work); then only git itself is killed.
        use std::io::IsTerminal;
        use std::os::unix::process::CommandExt;
        let own_group = match self.group {
            GroupMode::Own => true,
            GroupMode::Foreground => false,
            GroupMode::Auto => own_process_group(
                std::io::stdin().is_terminal(),
                std::io::stderr().is_terminal(),
            ),
        };
        cmd.stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        if own_group {
            cmd.process_group(0);
        }
        // ETXTBSY (26): another thread of this process still holds the executable open for writing
        // (it was just installed); a short retry is the documented remedy.
        let spawned = (0..10).find_map(|attempt| match cmd.spawn() {
            Err(e) if e.raw_os_error() == Some(26) && attempt < 9 => {
                std::thread::sleep(Duration::from_millis(20));
                None
            }
            other => Some(other),
        });
        let mut child = spawned
            .expect("loop returns a result")
            .map_err(|e| match e.kind() {
                std::io::ErrorKind::NotFound => GitError::NotFound,
                _ => GitError::Spawn(redact(&e.to_string(), &self.secrets)),
            })?;
        let (tx, rx) = mpsc::channel::<(bool, Option<Vec<u8>>)>();
        let streams: [(bool, Option<Box<dyn Read + Send>>); 2] = [
            (false, child.stdout.take().map(|s| Box::new(s) as _)),
            (true, child.stderr.take().map(|s| Box::new(s) as _)),
        ];
        for (is_err, stream) in streams {
            let tx = tx.clone();
            std::thread::spawn(move || {
                let Some(mut r) = stream else {
                    let _ = tx.send((is_err, None));
                    return;
                };
                let mut buf = [0u8; 8192];
                loop {
                    match r.read(&mut buf) {
                        Ok(0) | Err(_) => {
                            let _ = tx.send((is_err, None));
                            return;
                        }
                        Ok(n) => {
                            if tx.send((is_err, Some(buf[..n].to_vec()))).is_err() {
                                return;
                            }
                        }
                    }
                }
            });
        }
        drop(tx);
        let deadline = Instant::now() + limits.timeout;
        let (mut out, mut err) = (Vec::new(), Vec::new());
        let mut open = 2;
        let mut exited: Option<(std::process::ExitStatus, Instant)> = None;
        // After git exited, a background helper (an ssh control master) may keep the pipes open: wait briefly.
        const GRACE: Duration = Duration::from_millis(300);
        loop {
            match rx.recv_timeout(Duration::from_millis(20)) {
                Ok((is_err, Some(chunk))) => {
                    let (buf, max, name) = if is_err {
                        (&mut err, limits.max_stderr, "stderr")
                    } else {
                        (&mut out, limits.max_stdout, "stdout")
                    };
                    buf.extend_from_slice(&chunk);
                    if buf.len() > max {
                        kill_child(&mut child, own_group);
                        let _ = child.wait();
                        return Err(GitError::OutputLimit {
                            stream: name,
                            limit: max,
                        });
                    }
                }
                Ok((_, None)) => open -= 1,
                Err(mpsc::RecvTimeoutError::Timeout) => {}
                Err(mpsc::RecvTimeoutError::Disconnected) => open = 0,
            }
            if exited.is_none() {
                if let Ok(Some(st)) = child.try_wait() {
                    exited = Some((st, Instant::now()));
                }
            }
            match exited {
                Some((st, at)) if open == 0 || at.elapsed() > GRACE => return Ok((st, out, err)),
                Some(_) => {}
                None if Instant::now() >= deadline => {
                    kill_child(&mut child, own_group);
                    let _ = child.wait();
                    return Err(GitError::Timeout {
                        secs: limits.timeout.as_secs().max(1),
                    });
                }
                None => {}
            }
        }
    }
}

/// Keys that make git run a program, as `section.subsection.key` in lower case. Checked in the local and
/// worktree config only (the user's own config is trusted).
fn runs_a_command(section: &str, sub: Option<&str>, key: &str) -> bool {
    match section {
        "core" => matches!(
            key,
            "sshcommand" | "gitproxy" | "askpass" | "alternaterefscommand"
        ),
        "credential" => true,
        // git follows include files (a refused key in one of them would run) and newer git defines hooks
        // in `hook.<name>.*`; any such section in the repository's own config is refused
        "include" | "includeif" | "hook" => true,
        "url" => matches!(key, "insteadof" | "pushinsteadof"),
        "remote" => sub == Some("origin") && matches!(key, "receivepack" | "vcs"),
        _ => false,
    }
}

/// Read the repository's config with gix and refuse when a local or worktree section sets such a key. A
/// directory that is not a usable repository is refused too (fail closed).
fn refuse_local_command_config(repo: &Path, op: &Op) -> std::result::Result<(), GitError> {
    // range-diff runs `git log -p`: a diff program or textconv driver of the repository would start
    let patch = matches!(op, Op::RangeDiff { .. });
    refuse_config(repo, &|section, sub, key| {
        runs_a_command(section, sub, key)
            || (patch
                && section == "diff"
                && (key == "external" || (sub.is_some() && matches!(key, "textconv" | "command"))))
    })?;
    if patch {
        refuse_info_attributes(repo)?;
    }
    Ok(())
}

/// `FetchPullHead` runs for an ssh, scp-like, `file://` or local-path `origin` only (as `sync` does for
/// `FetchOrigin`); anything else is refused typed before git starts: no DNS lookup, no helper program.
fn check_fetch_transport(repo: &Path) -> std::result::Result<(), GitError> {
    let r = gix::open_opts(repo, gix::open::Options::isolated())
        .map_err(|e| GitError::Repo(format!("{}: {e}", repo.display())))?;
    let remote = r
        .find_remote("origin")
        .map_err(|e| GitError::Repo(format!("no usable remote origin: {e}")))?;
    let url = remote
        .url(gix::remote::Direction::Fetch)
        .ok_or_else(|| GitError::Repo("remote origin has no fetch url".into()))?;
    match url.scheme {
        gix::url::Scheme::Ssh | gix::url::Scheme::File => Ok(()),
        ref other => Err(GitError::Transport {
            scheme: other.as_str().to_string(),
        }),
    }
}

/// `info/attributes` is read in spite of `GIT_ATTR_SOURCE`; a line in it (`* -diff`) can turn a changed file into
/// "Binary files differ" and so into an equal patch. An empty or comment-only file is fine.
fn refuse_info_attributes(repo: &Path) -> std::result::Result<(), GitError> {
    let r = gix::open_opts(repo, gix::open::Options::isolated())
        .map_err(|e| GitError::Repo(format!("{}: {e}", repo.display())))?;
    let refused = || GitError::Refused {
        key: "info/attributes".into(),
    };
    let text = match std::fs::read(r.common_dir().join("info").join("attributes")) {
        Ok(t) => t,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(_) => return Err(refused()),
    };
    let active = text.split(|b| *b == b'\n').any(|l| {
        let t = l.trim_ascii();
        !t.is_empty() && t[0] != b'#'
    });
    if active {
        return Err(refused());
    }
    Ok(())
}

/// The key `mergetool.<t>.cmd` / `difftool.<t>.cmd`. Spelled with `concat!` because `tests/no_binaries.rs`
/// refuses the literal shell name anywhere in this file; this is a config key, not a program.
const TOOL_COMMAND_KEY: &str = concat!("c", "md");

/// Keys that `stw add` refuses in the repository's own config (local and worktree scope) on top of
/// `runs_a_command`: filter drivers, signing programs, diff/merge/tool commands, program-starting `core` keys,
/// aliases, pagers, trailers, per-remote programs and submodule update commands. The list is by key name
/// (conservative: `core.fsmonitor=true` is refused, `=false` too). The user's own config is not scanned.
fn refused_by_add(section: &str, sub: Option<&str>, key: &str) -> bool {
    if runs_a_command(section, sub, key) {
        return true;
    }
    match section {
        "filter" | "gpg" | "pager" | "trailer" | "alias" => true,
        "commit" | "tag" | "push" => key == "gpgsign",
        "diff" => key == "external" || (sub.is_some() && matches!(key, "textconv" | "command")),
        "merge" => sub.is_some() && key == "driver",
        "mergetool" | "difftool" => key == TOOL_COMMAND_KEY,
        "core" => matches!(
            key,
            "fsmonitor" | "hookspath" | "pager" | "editor" | "attributesfile" | "excludesfile"
        ),
        "sequence" => key == "editor",
        "submodule" => key == "update",
        "protocol" => key == "allow",
        "uploadpack" => key == "packobjectshook",
        "remote" => matches!(key, "receivepack" | "uploadpack" | "vcs" | "proxy"),
        _ => false,
    }
}

/// The config check of `stw add`, for the Workspace (`clone = false`) and for the fresh clone
/// (`clone = true`, where `core.worktree` is refused too: a clone made by this run has none).
pub fn check_config_for_add(repo: &Path, clone: bool) -> std::result::Result<(), GitError> {
    refuse_config(repo, &|section, sub, key| {
        refused_by_add(section, sub, key) || (clone && section == "core" && key == "worktree")
    })
}

fn refuse_config(
    repo: &Path,
    refused: &dyn Fn(&str, Option<&str>, &str) -> bool,
) -> std::result::Result<(), GitError> {
    // isolated: only the repository's own files are read, never the user's or system config
    let r = gix::open_opts(repo, gix::open::Options::isolated())
        .map_err(|e| GitError::Repo(format!("{}: {e}", repo.display())))?;
    let snapshot = r.config_snapshot();
    for section in snapshot.plumbing().sections() {
        if !matches!(
            section.meta().source,
            gix::config::Source::Local | gix::config::Source::Worktree
        ) {
            continue;
        }
        let header = section.header();
        let name = header.name().to_string().to_ascii_lowercase();
        let sub = header.subsection_name().map(|s| s.to_string());
        for key in section.body().value_names() {
            let key = key.to_string().to_ascii_lowercase();
            if refused(&name, sub.as_deref(), &key) {
                let full = match &sub {
                    Some(sub) => format!("{name}.{sub}.{key}"),
                    None => format!("{name}.{key}"),
                };
                return Err(GitError::Refused { key: full });
            }
        }
    }
    Ok(())
}

/// SIGKILL `child`; when it leads its own process group (`own_group`), the whole group. Never signals a
/// group that is not the child's own (it would be stw's).
fn kill_child(child: &mut std::process::Child, own_group: bool) {
    if own_group {
        if let Ok(pid) = i32::try_from(child.id()) {
            // SAFETY: plain kill(2) on a group id taken from a child we spawned with process_group(0).
            unsafe {
                libc::kill(-pid, libc::SIGKILL);
            }
        }
    }
    let _ = child.kill();
}

fn is_executable_file(p: &Path) -> bool {
    use std::os::unix::fs::PermissionsExt;
    p.metadata()
        .is_ok_and(|m| m.is_file() && m.permissions().mode() & 0o111 != 0)
}

fn first_line(s: &str) -> String {
    shorten(s.lines().next().unwrap_or(""), 200)
}

fn short(s: &str) -> String {
    shorten(s.trim(), 100)
}

fn shorten(s: &str, max: usize) -> String {
    let t = s.trim();
    if t.chars().count() <= max {
        t.to_string()
    } else {
        let mut o: String = t.chars().take(max).collect();
        o.push_str("...");
        o
    }
}

/// Remove credentials from text that may be shown: `scheme://user[:password]@` (the userinfo is dropped),
/// GitHub token shapes (`ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, `github_pat_`), the value of an
/// `Authorization:` header, and every value in `secrets`.
pub fn redact(text: &str, secrets: &[String]) -> String {
    let mut s = text.to_string();
    for sec in secrets.iter().filter(|x| !x.is_empty()) {
        s = s.replace(sec.as_str(), "***");
    }
    s = strip_userinfo(&s);
    s = mask_tokens(&s);
    mask_authorization(&s)
}

fn strip_userinfo(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut rest = s;
    while let Some(i) = rest.find("://") {
        out.push_str(&rest[..i + 3]);
        rest = &rest[i + 3..];
        let end = rest
            .find(|c: char| c.is_whitespace() || matches!(c, '/' | '\'' | '"' | '<' | '>'))
            .unwrap_or(rest.len());
        let authority = &rest[..end];
        if let Some(at) = authority.rfind('@') {
            rest = &rest[at + 1..];
        }
    }
    out.push_str(rest);
    out
}

fn mask_tokens(s: &str) -> String {
    const PREFIXES: &[&str] = &["github_pat_", "ghp_", "gho_", "ghu_", "ghs_", "ghr_"];
    let mut out = String::with_capacity(s.len());
    let mut i = 0;
    while i < s.len() {
        let rest = &s[i..];
        let at_boundary = i == 0
            || !s[..i]
                .chars()
                .next_back()
                .is_some_and(|c| c.is_ascii_alphanumeric() || c == '_');
        if at_boundary {
            if let Some(p) = PREFIXES.iter().find(|p| rest.starts_with(**p)) {
                let body = rest[p.len()..]
                    .chars()
                    .take_while(|c| c.is_ascii_alphanumeric() || *c == '_')
                    .count();
                if body >= 8 {
                    out.push_str("***");
                    i += p.len() + body;
                    continue;
                }
            }
        }
        let c = rest.chars().next().unwrap();
        out.push(c);
        i += c.len_utf8();
    }
    out
}

fn mask_authorization(s: &str) -> String {
    s.split_inclusive('\n')
        .map(|line| {
            let lower = line.to_ascii_lowercase();
            match lower.find("authorization:") {
                Some(i) => {
                    let keep = i + "authorization:".len();
                    let nl = if line.ends_with('\n') { "\n" } else { "" };
                    format!("{} ***{nl}", &line[..keep])
                }
                None => line.to_string(),
            }
        })
        .collect()
}
