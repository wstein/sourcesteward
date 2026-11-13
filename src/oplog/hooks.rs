//! `stw oplog hooks status | install | uninstall | print`: the two git hooks that write the op log logs
//! (`post-rewrite` -> `rewrites.log`, `reference-transaction` -> `refs.log`) in ONE repository, opt-in and only on the
//! explicit command with `--yes` (`docs/oplog-format.md`, 11). The hook scripts are the texts of the accepted spikes
//! `spikes/stw/oplog-hooks` and `spikes/stw/oplog-refhook` (byte for byte, `hooks/*.sh`); this module only places them.
//!
//! Rules (owner decisions 2026-11-11/12 and the install rules of the spikes):
//! - `core.hooksPath` is read (every config source, `-c`/`GIT_CONFIG_*` included; empty counts as set) and never written.
//!   When it is set, nothing is installed: the hooks belong in the directory of another tool, the owner decides.
//! - An existing hook of the same name is never overwritten, chained or rebuilt: refusal with a chain proposal as text.
//! - A refusal for one hook refuses the install of both (nothing is written): the two logs belong together.
//! - The hooks directory is `<git-common-dir>/hooks`, which all linked worktrees share; a symbolic link there is refused.
//! - Without `--yes` the plan is printed and nothing is written (exit 1, as the spikes).
//! - `uninstall` removes only a regular file whose line 2 is our header; the logs stay.
//!
//! No git program runs; no git config is written.
use std::fmt::Write as _;
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::os::unix::fs::{DirBuilderExt, MetadataExt, OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};

use serde_json::{json, Value};

use super::parse::iso_of;
use super::timeline::SCHEMA_VERSION;
use super::Ctx;
use crate::error::{exit, Error, Result};
use crate::github::reads::escape_untrusted;

/// One of the two hooks: its file name, the header line (line 2 of the file: how `uninstall` knows its own file), the
/// events file that records install and uninstall (the readers take the window from it) and the script text.
pub struct HookDef {
    pub name: &'static str,
    pub header: &'static str,
    pub events: &'static str,
    pub text: &'static str,
}

pub const HOOKS: [HookDef; 2] = [
    HookDef {
        name: "post-rewrite",
        header: "# stw-oplog-hook v1",
        events: "events.log",
        text: include_str!("hooks/post-rewrite.sh"),
    },
    HookDef {
        name: "reference-transaction",
        header: "# stw-oplog-refhook v1",
        events: "events-refhook.log",
        text: include_str!("hooks/reference-transaction.sh"),
    },
];

/// The script text of a hook by its file name (`stw oplog hooks print NAME`).
pub fn hook_text(name: &str) -> Option<&'static str> {
    HOOKS.iter().find(|h| h.name == name).map(|h| h.text)
}

/// The result of a command: text, JSON document and exit code.
pub struct Outcome {
    pub text: String,
    pub json: Value,
    pub exit: u8,
}

fn esc(s: &str) -> String {
    escape_untrusted(s, 300).0
}

fn show(p: &Path) -> String {
    esc(&p.to_string_lossy())
}

/// What is at `<hooks>/<name>`.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
enum State {
    Absent,
    Ours {
        executable: bool,
    },
    /// Something else: another hook, a look-alike header, a link, a directory.
    Foreign,
}

impl State {
    fn word(self) -> &'static str {
        match self {
            State::Absent => "absent",
            State::Ours { .. } => "ours",
            State::Foreign => "foreign",
        }
    }
}

/// Our own file: a regular file (no link) whose line 2 is exactly the header (not `v10`, not CRLF).
fn state_of(path: &Path, header: &str) -> State {
    let Ok(md) = fs::symlink_metadata(path) else {
        return State::Absent;
    };
    if !md.file_type().is_file() {
        return State::Foreign;
    }
    let head = {
        use std::io::Read;
        let mut buf = Vec::new();
        let _ = fs::File::open(path).and_then(|f| f.take(4096).read_to_end(&mut buf));
        buf
    };
    let second = head.split(|&b| b == b'\n').nth(1);
    if second == Some(header.as_bytes()) {
        State::Ours {
            executable: md.permissions().mode() & 0o111 != 0,
        }
    } else {
        State::Foreign
    }
}

struct HooksPath {
    value: String,
    origin: String,
}

/// `core.hooksPath` as git would see it (last value of all sources wins); `Some` also when it is empty or has no value.
fn hooks_path(ctx: &Ctx) -> Result<Option<HooksPath>> {
    let repo = ctx.repo()?;
    let snap = repo.config_snapshot();
    let file = snap.plumbing();
    let mut found = None;
    if let Some(sections) = file.sections_by_name("core") {
        for s in sections {
            let Some(v) = s.body().value_implicit("hooksPath") else {
                continue;
            };
            let value = v
                .map(|v| String::from_utf8_lossy(v.as_ref()).into_owned())
                .unwrap_or_default();
            let meta = s.meta();
            let origin = match (&meta.path, meta.source) {
                (Some(p), _) => format!("file {}", p.display()),
                (None, gix::config::Source::Env | gix::config::Source::EnvOverride) => {
                    "environment GIT_CONFIG_COUNT".to_string()
                }
                (None, gix::config::Source::Cli) => "command line".to_string(),
                (None, other) => format!("{other:?}").to_lowercase(),
            };
            found = Some(HooksPath { value, origin });
        }
    }
    // `git -c` hands its settings to child processes in GIT_CONFIG_PARAMETERS; they win over every file
    if let Some(v) = std::env::var_os("GIT_CONFIG_PARAMETERS") {
        if let Some(value) = parameters_value(&v.to_string_lossy(), "core.hookspath") {
            found = Some(HooksPath {
                value,
                origin: "environment GIT_CONFIG_PARAMETERS".into(),
            });
        }
    }
    Ok(found)
}

/// The last value of `key` (lower case) in `GIT_CONFIG_PARAMETERS`: words of single-quoted pieces (`'` inside a piece is
/// written `'\''`), each `key=value` (also `'key'='value'`) or `key` alone. The variable name of the key is compared
/// case-insensitively. A word without value is an empty value.
fn parameters_value(params: &str, key: &str) -> Option<String> {
    let mut words: Vec<String> = Vec::new();
    let mut cur = String::new();
    let mut in_word = false;
    let mut quoted = false;
    let mut chars = params.chars().peekable();
    while let Some(c) = chars.next() {
        if quoted {
            if c == '\'' {
                quoted = false;
            } else {
                cur.push(c);
            }
        } else if c == '\'' {
            quoted = true;
            in_word = true;
        } else if c == '\\' && in_word {
            if let Some(n) = chars.next() {
                cur.push(n);
            }
        } else if c.is_whitespace() {
            if in_word {
                words.push(std::mem::take(&mut cur));
                in_word = false;
            }
        } else {
            cur.push(c);
            in_word = true;
        }
    }
    if in_word {
        words.push(cur);
    }
    let mut out = None;
    for w in words {
        let (k, v) = w.split_once('=').unwrap_or((w.as_str(), ""));
        // section and variable names are case-insensitive, a subsection is not (core has none)
        if k.eq_ignore_ascii_case(key) {
            out = Some(v.to_string());
        }
    }
    out
}

struct Inspect {
    common: PathBuf,
    hooks_dir: PathBuf,
    dir_is_link: bool,
    hooks_path: Option<HooksPath>,
    states: [State; 2],
}

fn inspect(ctx: &Ctx) -> Result<Inspect> {
    let hooks_dir = ctx.common.join("hooks");
    let dir_is_link = fs::symlink_metadata(&hooks_dir).is_ok_and(|m| m.file_type().is_symlink());
    let states = [
        state_of(&hooks_dir.join(HOOKS[0].name), HOOKS[0].header),
        state_of(&hooks_dir.join(HOOKS[1].name), HOOKS[1].header),
    ];
    Ok(Inspect {
        common: ctx.common.clone(),
        hooks_path: hooks_path(ctx)?,
        hooks_dir,
        dir_is_link,
        states,
    })
}

fn oplog_dir(common: &Path) -> PathBuf {
    common.join("stw/oplog")
}

fn base_json(command: &str, common: &Path) -> Value {
    json!({
        "schema_version": SCHEMA_VERSION, "command": command, "engine": crate::output::engine(),
        "repo": common.to_string_lossy(),
    })
}

fn hooks_path_json(h: &Option<HooksPath>) -> Value {
    match h {
        Some(h) => json!({ "value": h.value, "origin": h.origin }),
        None => Value::Null,
    }
}

fn hook_json(i: &Inspect, n: usize, action: &str) -> Value {
    let def = &HOOKS[n];
    let executable = match i.states[n] {
        State::Ours { executable } => json!(executable),
        _ => Value::Null,
    };
    json!({
        "name": def.name, "file": i.hooks_dir.join(def.name).to_string_lossy(), "header": def.header,
        "state": i.states[n].word(), "executable": executable, "action": action,
    })
}

// ---- status ----------------------------------------------------------------------------------------------------------

/// Read-only: `core.hooksPath`, the hooks directory, our two hooks, worktrees, submodules, logs.
pub fn status(ctx: &Ctx) -> Result<Outcome> {
    let i = inspect(ctx)?;
    let mut t = String::new();
    let _ = writeln!(t, "status  repo={}", show(&i.common));
    let _ = writeln!(t, "common-dir   {}", show(&i.common));
    match &i.hooks_path {
        Some(h) => {
            let _ = writeln!(
                t,
                "hooksPath    set: {} (from {}); install would be refused",
                esc(&h.value),
                esc(&h.origin)
            );
        }
        None => {
            let _ = writeln!(t, "hooksPath    not set");
        }
    }
    let _ = writeln!(
        t,
        "hooks dir    {}{}",
        show(&i.hooks_dir),
        if i.dir_is_link {
            " (common hooks dir is a symbolic link)"
        } else {
            ""
        }
    );
    let _ = writeln!(t, "hooks        {}", executable_hooks(&i.hooks_dir));
    for (n, def) in HOOKS.iter().enumerate() {
        match i.states[n] {
            State::Ours { executable } => {
                let _ = writeln!(
                    t,
                    "{} ours ({}){}",
                    def.name,
                    def.header,
                    if executable {
                        ""
                    } else {
                        ", NOT executable: git ignores it"
                    }
                );
            }
            State::Foreign => {
                let _ = writeln!(t, "{} exists, not ours (never touched)", def.name);
            }
            State::Absent => {
                let _ = writeln!(t, "{} absent in {}", def.name, show(&i.hooks_dir));
            }
        }
    }
    let linked = linked_worktrees(&i.common);
    if linked > 0 {
        let _ = writeln!(
            t,
            "worktrees    {} (the main one and {linked} linked): all use {} and write {} (common dir)",
            linked + 1,
            show(&i.hooks_dir),
            show(&oplog_dir(&i.common))
        );
    } else {
        let _ = writeln!(t, "worktrees    1 (no linked worktrees)");
    }
    let subs = submodule_hooks(&i.common);
    for (name, hooks) in &subs {
        let _ = writeln!(
            t,
            "submodule    {}: own hooks dir, hooks: {}; a hook of this repository does not apply there (hint only; `--repo {}`)",
            esc(name),
            if hooks.is_empty() {
                "(none)".to_string()
            } else {
                hooks.join(" ")
            },
            esc(name)
        );
    }
    if subs.is_empty() {
        let _ = writeln!(
            t,
            "submodules   none under {}",
            show(&i.common.join("modules"))
        );
    }
    let dir = oplog_dir(&i.common);
    let sizes: Vec<(&str, Option<u64>)> = ["refs.log", "rewrites.log", "errors.log"]
        .iter()
        .map(|n| (*n, file_size(&dir.join(n))))
        .collect();
    let gaps = file_size(&i.common.join("stw-oplog-gaps"));
    let mut l = String::from("logs        ");
    for (n, s) in &sizes {
        let _ = write!(
            l,
            " {n}: {}",
            s.map_or("none".to_string(), |b| format!("{b} bytes"))
        );
    }
    if let Some(b) = gaps {
        let _ = write!(
            l,
            "; stw-oplog-gaps: {b} bytes (the log directory itself was not writable)"
        );
    }
    let _ = writeln!(t, "{l}");
    let mut events = Vec::new();
    for def in &HOOKS {
        if let Ok(s) = fs::read_to_string(dir.join(def.events)) {
            if let Some(last) = s.lines().last() {
                events.push(format!("{}: {}", def.events, esc(&last.replace('\t', " "))));
            }
        }
    }
    if !events.is_empty() {
        let _ = writeln!(t, "events       {}", events.join("; "));
    }
    let mut j = base_json("oplog hooks status", &i.common);
    j["hooks_path"] = hooks_path_json(&i.hooks_path);
    j["hooks_dir"] = json!(i.hooks_dir.to_string_lossy());
    j["hooks_dir_is_symlink"] = json!(i.dir_is_link);
    j["hooks"] = json!([hook_json(&i, 0, "none"), hook_json(&i, 1, "none")]);
    j["worktrees"] = json!(linked + 1);
    j["submodules"] = json!(subs
        .iter()
        .map(|(n, h)| json!({ "name": n, "hooks": h }))
        .collect::<Vec<_>>());
    j["logs"] = json!(sizes
        .iter()
        .map(|(n, s)| json!({ "file": n, "bytes": s }))
        .collect::<Vec<_>>());
    j["executed"] = json!(false);
    Ok(Outcome {
        text: t,
        json: j,
        exit: exit::OK,
    })
}

fn file_size(p: &Path) -> Option<u64> {
    fs::symlink_metadata(p)
        .ok()
        .filter(|m| m.file_type().is_file())
        .map(|m| m.size())
}

fn executable_hooks(dir: &Path) -> String {
    let mut names: Vec<String> = fs::read_dir(dir)
        .into_iter()
        .flatten()
        .flatten()
        .filter(|e| {
            e.metadata()
                .is_ok_and(|m| m.is_file() && m.permissions().mode() & 0o111 != 0)
        })
        .map(|e| e.file_name().to_string_lossy().into_owned())
        .filter(|n| !n.ends_with(".sample") && !n.starts_with('.'))
        .map(|n| esc(&n))
        .collect();
    names.sort();
    if names.is_empty() {
        "(none executable)".into()
    } else {
        names.join(" ")
    }
}

fn linked_worktrees(common: &Path) -> usize {
    fs::read_dir(common.join("worktrees"))
        .into_iter()
        .flatten()
        .flatten()
        .filter(|e| e.path().is_dir())
        .count()
}

/// Submodule git directories below `<common>/modules` (nested names such as `libs/sub` included, at most 4 levels) and
/// the executable hooks each has.
fn submodule_hooks(common: &Path) -> Vec<(String, Vec<String>)> {
    fn walk(base: &Path, dir: &Path, depth: usize, out: &mut Vec<(String, Vec<String>)>) {
        let Ok(rd) = fs::read_dir(dir) else {
            return;
        };
        let mut ents: Vec<_> = rd.flatten().collect();
        ents.sort_by_key(|e| e.file_name());
        for e in ents {
            let p = e.path();
            if !e.file_type().is_ok_and(|t| t.is_dir()) {
                continue;
            }
            if p.join("HEAD").is_file() {
                let rel = p
                    .strip_prefix(base)
                    .unwrap_or(&p)
                    .to_string_lossy()
                    .into_owned();
                let hooks = executable_hooks(&p.join("hooks"));
                let list = if hooks == "(none executable)" {
                    Vec::new()
                } else {
                    hooks.split(' ').map(str::to_string).collect()
                };
                out.push((rel, list));
            } else if depth < 4 {
                walk(base, &p, depth + 1, out);
            }
        }
    }
    let base = common.join("modules");
    let mut out = Vec::new();
    walk(&base, &base, 1, &mut out);
    out
}

// ---- install ---------------------------------------------------------------------------------------------------------

fn chain_proposal(def: &HookDef) -> String {
    let call = if def.name == "reference-transaction" {
        "in=$(cat); printf \"%s\\n\" \"$in\" | /path/to/stw-reference-transaction.sh \"$@\" || :; <existing logic reading \"$in\">"
    } else {
        "in=$(cat); printf \"%s\\n\" \"$in\" | /path/to/stw-post-rewrite.sh \"$@\"; <existing logic reading \"$in\">"
    };
    let script = format!("/path/to/stw-{}.sh", def.name);
    let mut s = String::new();
    let _ = writeln!(
        s,
        "         Chain proposal (not executed, nothing is written even with --yes): save the script with"
    );
    let _ = writeln!(
        s,
        "           stw oplog hooks print {} > {script}; chmod +x {script}",
        def.name
    );
    let _ = writeln!(
        s,
        "         and add to the existing hook (stdin can be read once: keep a copy{}):",
        if def.name == "reference-transaction" {
            "; the phase is the first argument"
        } else {
            ""
        }
    );
    let _ = writeln!(s, "           {call}");
    if def.name == "reference-transaction" {
        let _ = writeln!(
            s,
            "         The existing hook decides whether a ref transaction may go on (its exit code in the phase prepared aborts it); this script never makes git fail (exit 0)."
        );
    } else {
        let _ = writeln!(
            s,
            "         The owner decides; the hook never makes git fail (exit 0)."
        );
    }
    s
}

/// Install both hooks. Without `yes`: the plan, nothing written, exit 1.
pub fn install(ctx: &Ctx, yes: bool) -> Result<Outcome> {
    let i = inspect(ctx)?;
    let mut t = format!("install  repo={}\n", show(&i.common));
    let mut j = base_json("oplog hooks install", &i.common);
    j["hooks_path"] = hooks_path_json(&i.hooks_path);
    j["executed"] = json!(false);
    let mut notes: Vec<String> = Vec::new();
    // repository-wide refusals: nothing is written
    if let Some(h) = &i.hooks_path {
        let _ = writeln!(
            t,
            "refused  core.hooksPath is set ({}, from {}). Nothing was installed or written.",
            esc(&h.value),
            esc(&h.origin)
        );
        for def in &HOOKS {
            let _ = writeln!(
                t,
                "         The hook would belong in: {}/{} (a relative value is relative to the directory where git runs hooks).",
                esc(&h.value),
                def.name
            );
        }
        let _ = writeln!(
            t,
            "         The owner decides: install by hand (`stw oplog hooks print NAME > <dir>/NAME; chmod +x`), or unset core.hooksPath, or"
        );
        let _ = writeln!(
            t,
            "         call the hook from an existing hook there. This tool never changes core.hooksPath."
        );
        return Ok(refused(t, j, &i, "refused"));
    }
    if i.dir_is_link {
        let _ = writeln!(
            t,
            "refused  {} is a symbolic link (the directory may be shared with other repositories). Nothing was written; the owner decides.",
            show(&i.hooks_dir)
        );
        return Ok(refused(t, j, &i, "refused"));
    }
    let mut any_foreign = false;
    for (n, def) in HOOKS.iter().enumerate() {
        if i.states[n] == State::Foreign {
            any_foreign = true;
            let _ = writeln!(
                t,
                "refused  {} exists and is not from this tool. Not overwritten, nothing was written.",
                show(&i.hooks_dir.join(def.name))
            );
            t.push_str(&chain_proposal(def));
        }
    }
    if any_foreign {
        let mut hooks = Vec::new();
        for (n, _) in HOOKS.iter().enumerate() {
            let action = match i.states[n] {
                State::Foreign => "refused",
                _ => "none",
            };
            hooks.push(hook_json(&i, n, action));
        }
        j["hooks"] = json!(hooks);
        j["refused"] = json!(true);
        j["result"] = json!("refused, nothing was written");
        return Ok(Outcome {
            text: t,
            json: j,
            exit: exit::FAILED,
        });
    }
    let todo: Vec<usize> = (0..2).filter(|&n| i.states[n] == State::Absent).collect();
    let mut hooks = Vec::new();
    for n in 0..2 {
        let action = if todo.contains(&n) {
            "install"
        } else {
            "already-installed"
        };
        hooks.push(hook_json(&i, n, action));
    }
    j["hooks"] = json!(hooks);
    j["refused"] = json!(false);
    for (n, def) in HOOKS.iter().enumerate() {
        if let State::Ours { executable } = i.states[n] {
            let _ = writeln!(
                t,
                "ok       already installed: {} ({}){}",
                show(&i.hooks_dir.join(def.name)),
                def.header,
                if executable {
                    ""
                } else {
                    "; NOT executable, git ignores it (the owner sets the mode)"
                }
            );
        }
    }
    if todo.is_empty() {
        let _ = writeln!(t, "Nothing to do.");
        j["result"] = json!("already installed");
        return Ok(Outcome {
            text: t,
            json: j,
            exit: exit::OK,
        });
    }
    for &n in &todo {
        let _ = writeln!(
            t,
            "plan     write {} ({})",
            show(&i.hooks_dir.join(HOOKS[n].name)),
            HOOKS[n].header
        );
    }
    let _ = writeln!(t, "         logs go to {}", show(&oplog_dir(&i.common)));
    if todo.contains(&1) {
        let _ = writeln!(
            t,
            "         the reference-transaction hook runs on every ref change in this repository (commit, fetch, tag, branch, rebase, ...)"
        );
    }
    if !yes {
        let _ = writeln!(t, "not executed: --yes is missing");
        j["result"] = json!("not executed: --yes is missing");
        return Ok(Outcome {
            text: t,
            json: j,
            exit: exit::FAILED,
        });
    }
    let mut done: Vec<usize> = Vec::new();
    for &n in &todo {
        match write_hook(&i.hooks_dir, &HOOKS[n]) {
            Ok(()) => {
                done.push(n);
                let _ = writeln!(
                    t,
                    "Result: installed {}",
                    show(&i.hooks_dir.join(HOOKS[n].name))
                );
            }
            Err(why) => {
                let _ = writeln!(t, "refused  {why}");
                if !done.is_empty() {
                    let _ = writeln!(
                        t,
                        "         the hook above stays installed; `stw oplog hooks uninstall --yes` removes it"
                    );
                }
                j["executed"] = json!(!done.is_empty());
                j["result"] = json!(format!("failed: {why}"));
                return Ok(Outcome {
                    text: t,
                    json: j,
                    exit: exit::FAILED,
                });
            }
        }
    }
    match prepare_oplog_dir(&i.common) {
        Ok(dir) => {
            for &n in &done {
                if let Err(why) = append_event(&dir, HOOKS[n].events, "install") {
                    notes.push(format!("no install record for {}: {why}", HOOKS[n].name));
                }
            }
        }
        Err(why) => {
            for &n in &done {
                notes.push(format!("no install record for {}: {why}", HOOKS[n].name));
            }
        }
    }
    for n in &notes {
        let _ = writeln!(
            t,
            "note     {}: the timeline then cannot tell a pre-install rewrite from a gap",
            esc(n)
        );
    }
    let _ = writeln!(t, "Remove with: stw oplog hooks uninstall --yes");
    j["executed"] = json!(true);
    j["notes"] = json!(notes);
    j["result"] = json!("installed");
    Ok(Outcome {
        text: t,
        json: j,
        exit: exit::OK,
    })
}

fn refused(t: String, mut j: Value, i: &Inspect, action: &str) -> Outcome {
    j["hooks"] = json!([hook_json(i, 0, action), hook_json(i, 1, action)]);
    j["refused"] = json!(true);
    j["result"] = json!("refused, nothing was written");
    Outcome {
        text: t,
        json: j,
        exit: exit::FAILED,
    }
}

/// Write the script next to the target under a temporary name, then link it into place: `link` fails when the target
/// exists, so a hook that appeared meanwhile wins and is never replaced.
fn write_hook(dir: &Path, def: &HookDef) -> std::result::Result<(), String> {
    let target = dir.join(def.name);
    fs::create_dir_all(dir).map_err(|e| format!("cannot create {}: {e}", show(dir)))?;
    let nanos = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_or(0, |d| d.subsec_nanos());
    let tmp = dir.join(format!(
        ".{}.stw-new.{}-{nanos}",
        def.name,
        std::process::id()
    ));
    let write = || -> std::io::Result<()> {
        let mut f = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o755)
            .open(&tmp)?;
        f.write_all(def.text.as_bytes())?;
        f.sync_all()?;
        // the umask may have taken bits away from the creation mode
        fs::set_permissions(&tmp, fs::Permissions::from_mode(0o755))
    };
    if let Err(e) = write() {
        let _ = fs::remove_file(&tmp);
        return Err(format!("cannot write {}: {e}", show(&tmp)));
    }
    let linked = fs::hard_link(&tmp, &target);
    let _ = fs::remove_file(&tmp);
    linked.map_err(|e| {
        format!(
            "{} appeared or cannot be linked ({e}); nothing installed there",
            show(&target)
        )
    })
}

/// `<common>/stw/oplog`, created private (0700) unless `stw` or `stw/oplog` is a link or no directory.
fn prepare_oplog_dir(common: &Path) -> std::result::Result<PathBuf, String> {
    let mut p = common.to_path_buf();
    for part in ["stw", "oplog"] {
        p.push(part);
        match fs::symlink_metadata(&p) {
            Ok(m) if m.file_type().is_symlink() => {
                return Err(format!("{} is a symbolic link", show(&p)))
            }
            Ok(m) if !m.is_dir() => return Err(format!("{} is not a directory", show(&p))),
            Ok(_) => {}
            Err(_) => {
                fs::DirBuilder::new()
                    .mode(0o700)
                    .create(&p)
                    .map_err(|e| format!("cannot create {}: {e}", show(&p)))?;
            }
        }
    }
    Ok(p)
}

/// `<time>\t<word>\n` in one append; a link or a file that is no regular file is not written.
fn append_event(dir: &Path, file: &str, word: &str) -> std::result::Result<(), String> {
    let p = dir.join(file);
    if let Ok(m) = fs::symlink_metadata(&p) {
        if !m.file_type().is_file() {
            return Err(format!("{} is not a regular file", show(&p)));
        }
    }
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_or(0, |d| d.as_secs() as i64);
    let line = format!("{}\t{word}\n", iso_of(now));
    let mut f = OpenOptions::new()
        .append(true)
        .create(true)
        .mode(0o600)
        .open(&p)
        .map_err(|e| format!("cannot write {}: {e}", show(&p)))?;
    f.write_all(line.as_bytes())
        .map_err(|e| format!("cannot write {}: {e}", show(&p)))
}

// ---- uninstall -------------------------------------------------------------------------------------------------------

/// Remove our own hook files. A file that is not ours stays (reported, no refusal: there is nothing of ours to remove).
/// Without `yes`: the plan, nothing removed, exit 1 when something would be removed.
pub fn uninstall(ctx: &Ctx, yes: bool) -> Result<Outcome> {
    let i = inspect(ctx)?;
    let mut t = format!("uninstall  repo={}\n", show(&i.common));
    let mut j = base_json("oplog hooks uninstall", &i.common);
    j["hooks_path"] = hooks_path_json(&i.hooks_path);
    j["executed"] = json!(false);
    if i.dir_is_link {
        let _ = writeln!(
            t,
            "refused  {} is a symbolic link (the directory may be shared with other repositories). Nothing was removed; the owner decides.",
            show(&i.hooks_dir)
        );
        return Ok(refused(t, j, &i, "refused"));
    }
    let mut hooks = Vec::new();
    let mut ours: Vec<usize> = Vec::new();
    for (n, def) in HOOKS.iter().enumerate() {
        let p = show(&i.hooks_dir.join(def.name));
        let action = match i.states[n] {
            State::Absent => {
                let _ = writeln!(t, "nothing  no {p}");
                "none"
            }
            State::Foreign => {
                let _ = writeln!(
                    t,
                    "not ours {p} does not carry the line '{}' as line 2 (or is a link): not touched.",
                    def.header
                );
                "not-ours"
            }
            State::Ours { .. } => {
                ours.push(n);
                "remove"
            }
        };
        hooks.push(hook_json(&i, n, action));
    }
    j["hooks"] = json!(hooks);
    j["refused"] = json!(false);
    if ours.is_empty() {
        j["result"] = json!("nothing to remove");
        return Ok(Outcome {
            text: t,
            json: j,
            exit: exit::OK,
        });
    }
    for &n in &ours {
        let _ = writeln!(
            t,
            "plan     remove {}",
            show(&i.hooks_dir.join(HOOKS[n].name))
        );
    }
    if !yes {
        let _ = writeln!(t, "not executed: --yes is missing");
        j["result"] = json!("not executed: --yes is missing");
        return Ok(Outcome {
            text: t,
            json: j,
            exit: exit::FAILED,
        });
    }
    let dir = oplog_dir(&i.common);
    let mut failed = false;
    let mut removed = 0;
    for &n in &ours {
        let path = i.hooks_dir.join(HOOKS[n].name);
        // read again right before the removal: the file may have changed since the inspection
        if !matches!(state_of(&path, HOOKS[n].header), State::Ours { .. }) {
            let _ = writeln!(t, "refused  {} changed meanwhile: not removed", show(&path));
            failed = true;
            continue;
        }
        match fs::remove_file(&path) {
            Ok(()) => {
                removed += 1;
                let _ = writeln!(t, "Result: removed {}", show(&path));
                if let Err(why) = append_event(&dir, HOOKS[n].events, "uninstall") {
                    let _ = writeln!(
                        t,
                        "note     no uninstall record for {}: {}",
                        HOOKS[n].name,
                        esc(&why)
                    );
                }
            }
            Err(e) => {
                let _ = writeln!(t, "refused  cannot remove {}: {e}", show(&path));
                failed = true;
            }
        }
    }
    if removed > 0 {
        let _ = writeln!(t, "Logs stay in {} (not deleted).", show(&dir));
    }
    j["executed"] = json!(removed > 0);
    j["result"] = json!(if failed { "failed" } else { "removed" });
    Ok(Outcome {
        text: t,
        json: j,
        exit: if failed { exit::FAILED } else { exit::OK },
    })
}

/// `stw oplog hooks print NAME`: the script text, nothing else.
pub fn print(name: &str) -> Result<String> {
    hook_text(name).map(str::to_string).ok_or_else(|| {
        Error::usage(format!(
            "unknown hook '{}': one of {}",
            esc(name),
            HOOKS.map(|h| h.name).join(", ")
        ))
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parameters_in_the_old_and_the_new_form() {
        let k = "core.hookspath";
        assert_eq!(
            parameters_value("'core.hooksPath=/a'", k),
            Some("/a".into())
        );
        assert_eq!(
            parameters_value("'core.hooksPath'='/b'", k),
            Some("/b".into())
        );
        assert_eq!(parameters_value("'core.hooksPath'", k), Some(String::new()));
        assert_eq!(
            parameters_value("'core.hooksPath='", k),
            Some(String::new())
        );
    }

    #[test]
    fn the_last_value_wins_and_other_keys_are_ignored() {
        let k = "core.hookspath";
        assert_eq!(
            parameters_value("'core.hooksPath=/a' 'user.name=x' 'core.hookspath=/c'", k),
            Some("/c".into())
        );
        assert_eq!(
            parameters_value("'user.name=x' 'core.hooksPathX=/a'", k),
            None
        );
        assert_eq!(parameters_value("", k), None);
    }

    #[test]
    fn a_quote_and_a_space_inside_a_value_stay_in_the_value() {
        let k = "core.hookspath";
        assert_eq!(
            parameters_value(r"'core.hooksPath=/a b'\''c'", k),
            Some("/a b'c".into())
        );
        // the value is not split at a second `=`
        assert_eq!(
            parameters_value("'core.hooksPath=/a=b'", k),
            Some("/a=b".into())
        );
    }

    #[test]
    fn our_file_needs_the_exact_header_on_line_two() {
        let d = tempfile::tempdir().unwrap();
        let h = "# stw-oplog-hook v1";
        let f = |name: &str, body: &str| {
            let p = d.path().join(name);
            fs::write(&p, body).unwrap();
            fs::set_permissions(&p, fs::Permissions::from_mode(0o755)).unwrap();
            p
        };
        assert_eq!(
            state_of(&f("ok", &format!("#!/bin/sh\n{h}\nexit 0\n")), h),
            State::Ours { executable: true }
        );
        assert_eq!(
            state_of(&f("v10", &format!("#!/bin/sh\n{h}0\n")), h),
            State::Foreign
        );
        assert_eq!(
            state_of(&f("crlf", &format!("#!/bin/sh\n{h}\r\n")), h),
            State::Foreign
        );
        assert_eq!(
            state_of(&f("line1", &format!("{h}\nx\n")), h),
            State::Foreign
        );
        assert_eq!(
            state_of(&f("line3", &format!("#!/bin/sh\n\n{h}\n")), h),
            State::Foreign
        );
        assert_eq!(state_of(&d.path().join("none"), h), State::Absent);
        assert_eq!(state_of(d.path(), h), State::Foreign);
    }
}
