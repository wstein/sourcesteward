//! Fetch of `origin` (apply mode only).
//!
//! By remote kind: `ssh://`, `user@host:path`, local paths and `file://` are fetched by the installed `git`
//! through the runner (`git_runner`, reported as `"via": "git"`), because gix would start `ssh` or
//! `git-upload-pack` itself for them (`tests/it/remote_kinds.rs` proves it no longer does). `https://`,
//! `http://` and `git://` are fetched by gix without any program (`"via": "gix"`); a failure of those
//! non-https kinds is typed `unsupported-remote`. Any other transport (`ext::`, `fd::`) is refused typed
//! without starting anything. Without a usable `git` the git steps report `unsupported`.
//!
//! HTTPS credentials: when the server asks, the GitHub token found by `auth` is offered for `github.com`
//! only (user `x-access-token`). Other hosts get no credentials from us.
use std::sync::atomic::AtomicBool;

use gix::credentials::helper::{Action, NextAction};
use gix::credentials::protocol::Outcome;
use gix::remote::Direction;
use gix::url::Scheme;

use crate::error::Result;
use crate::git_runner::{self, Op};
use crate::github::Client;

/// `stw sync --prune` / `--no-prune`. `Unset` is the old behavior: no option is passed to git and gix never prunes.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Prune {
    #[default]
    Unset,
    On,
    Off,
}

/// The namespace `--prune` may delete in: the remote-tracking refs of `origin`.
pub const PRUNE_SCOPE: &str = "refs/remotes/origin/";

/// What happened to the fetch of `origin`.
#[derive(Debug)]
pub enum Fetch {
    Done,
    /// The repository has no remote named `origin`.
    NoRemote,
    /// The fetch of a non-https remote failed; carries gix's message and the hint to use git directly.
    Unsupported(String),
    Failed(String),
    /// The step needs the installed git and it is missing, too old or lacks an option; carries the reason.
    GitUnsupported(String),
}

/// A fetch result plus how it was done.
#[derive(Debug)]
pub struct Fetched {
    pub fetch: Fetch,
    /// `"git"` (runner), `"gix"`, or None when nothing was started.
    pub via: Option<&'static str>,
    /// Kind of the typed git error (`not-found`, `version`, `unknown-option`, `timeout`, ...).
    pub git_error: Option<&'static str>,
}

impl Fetched {
    pub fn label(&self) -> &'static str {
        self.fetch.label()
    }
    pub fn message(&self) -> Option<&str> {
        self.fetch.message()
    }
}

impl Fetch {
    /// Value of the `fetch` field in the JSON output.
    pub fn label(&self) -> &'static str {
        match self {
            Fetch::Done => "ok",
            Fetch::NoRemote => "no-remote",
            Fetch::Unsupported(_) => "unsupported-remote",
            Fetch::Failed(_) => "failed",
            Fetch::GitUnsupported(_) => "unsupported",
        }
    }

    pub fn message(&self) -> Option<&str> {
        match self {
            Fetch::Unsupported(m) | Fetch::Failed(m) | Fetch::GitUnsupported(m) => Some(m),
            Fetch::Done | Fetch::NoRemote => None,
        }
    }
}

pub fn fetch_origin(repo: &gix::Repository, gh: Option<&Client>) -> Fetched {
    fetch_origin_with(repo, gh, Prune::Unset)
}

/// Every configured fetch refspec of `origin` writes below `refs/remotes/origin/` (a negative refspec `^...` and an
/// empty value are skipped), so that `fetch --prune` can delete nothing else. Err names the refspec.
pub fn check_prune_scope(repo: &gix::Repository) -> std::result::Result<(), String> {
    let snap = repo.config_snapshot();
    let specs = snap
        .plumbing()
        .strings_by("remote", Some("origin".into()), "fetch")
        .unwrap_or_default();
    for raw in specs {
        let spec = raw.to_string();
        let spec = spec.strip_prefix('+').unwrap_or(&spec);
        if spec.is_empty() || spec.starts_with('^') {
            continue;
        }
        let Some((_, dst)) = spec.split_once(':') else {
            continue;
        };
        if !dst.is_empty() && !dst.starts_with(PRUNE_SCOPE) {
            let (shown, _) = crate::github::reads::escape_untrusted(spec, 200);
            return Err(format!(
                "the fetch refspec {shown:?} of origin writes outside {PRUNE_SCOPE}*: --prune would delete refs there; nothing was fetched"
            ));
        }
    }
    Ok(())
}

/// `fetch_origin` with the prune choice. `On` is always fetched by the installed git (gix has no prune, and refs
/// are deleted only by git's own fetch), also for an https remote; `Off` and `Unset` keep the old transports.
pub fn fetch_origin_with(repo: &gix::Repository, gh: Option<&Client>, prune: Prune) -> Fetched {
    let url = repo
        .find_remote("origin")
        .ok()
        .and_then(|r| r.url(Direction::Fetch).cloned());
    let done = |fetch, via, git_error| Fetched {
        fetch,
        via,
        git_error,
    };
    let Some(u) = url.as_ref() else {
        // no `origin`, or one without a fetch URL: gix reports which
        return match run_gix(repo, gh) {
            Ok(false) => done(Fetch::NoRemote, None, None),
            Ok(true) => done(Fetch::Done, Some("gix"), None),
            Err(e) => done(failure(&e.to_string(), None), Some("gix"), None),
        };
    };
    match u.scheme {
        Scheme::Ssh | Scheme::File => run_git(repo, prune, url.as_ref(), false),
        Scheme::Https | Scheme::Http | Scheme::Git if prune == Prune::On => {
            run_git(repo, prune, url.as_ref(), true)
        }
        Scheme::Https | Scheme::Http | Scheme::Git => match run_gix(repo, gh) {
            Ok(true) => done(Fetch::Done, Some("gix"), None),
            Ok(false) => done(Fetch::NoRemote, None, None),
            Err(e) => done(failure(&e.to_string(), url.as_ref()), Some("gix"), None),
        },
        _ => done(
            Fetch::Unsupported(format!(
                "this remote uses a transport stw does not run: origin is {}",
                display_url(u)
            )),
            None,
            None,
        ),
    }
}

/// The fetch of `origin` by the installed git through the runner. `net_hint`: an https, http or git:// remote,
/// where git gets no GitHub token from stw (only the user's own credential helpers).
fn run_git(
    repo: &gix::Repository,
    prune: Prune,
    url: Option<&gix::Url>,
    net_hint: bool,
) -> Fetched {
    let done = |fetch, via, git_error| Fetched {
        fetch,
        via,
        git_error,
    };
    if prune == Prune::On {
        if let Err(m) = check_prune_scope(repo) {
            return done(Fetch::Failed(m), None, Some("refused-refspec"));
        }
    }
    let dir = repo
        .workdir()
        .map(std::path::Path::to_path_buf)
        .unwrap_or_else(|| repo.git_dir().to_path_buf());
    let op = match prune {
        Prune::Unset => Op::FetchOrigin,
        Prune::On => Op::FetchOriginPrune { prune: true },
        Prune::Off => Op::FetchOriginPrune { prune: false },
    };
    match git_runner::shared().run(&dir, op) {
        Ok(_) => done(Fetch::Done, Some(git_runner::VIA), None),
        Err(e) => {
            let mut msg: String = scrub(&e.to_string(), url).chars().take(400).collect();
            if net_hint && !e.is_unsupported() {
                msg += "; --prune fetches this remote with the installed git, which gets no GitHub token from stw: for a private repository use a git credential helper";
            }
            let fetch = if e.is_unsupported() {
                Fetch::GitUnsupported(msg)
            } else {
                Fetch::Failed(msg)
            };
            done(fetch, Some(git_runner::VIA), Some(e.kind()))
        }
    }
}

/// The result of a failed fetch: gix's message without URL credentials, typed `Unsupported` (with the
/// hint to use git directly) when the remote is not https.
pub fn failure(gix_message: &str, url: Option<&gix::Url>) -> Fetch {
    let msg: String = scrub(gix_message, url).chars().take(300).collect();
    match url {
        Some(u) if u.scheme != Scheme::Https => Fetch::Unsupported(format!(
            "{msg}; stw fetches https remotes: origin is {} ({}), use git directly for it",
            scheme_name(&u.scheme),
            display_url(u)
        )),
        _ => Fetch::Failed(msg),
    }
}

fn scheme_name(scheme: &Scheme) -> &'static str {
    match scheme {
        Scheme::Https => "https",
        Scheme::Ssh => "ssh",
        Scheme::File => "a local path",
        Scheme::Git => "git://",
        Scheme::Http => "plain http",
        _ => "an unknown kind",
    }
}

/// The URL as shown to the user: user name and password are removed.
fn display_url(url: &gix::Url) -> String {
    let mut plain = url.clone();
    plain.user = None;
    plain.password = None;
    plain.to_bstring().to_string()
}

/// `msg` with the URL's credentials removed (the full URL becomes the plain one, a lone password becomes `***`).
fn scrub(msg: &str, url: Option<&gix::Url>) -> String {
    let Some(url) = url else {
        return msg.to_string();
    };
    let mut out = msg.replace(&url.to_bstring().to_string(), &display_url(url));
    if let Some(pw) = url.password.as_deref().filter(|p| !p.is_empty()) {
        out = out.replace(pw, "***");
    }
    out
}

/// Answer a credentials request of the transport: the GitHub token for `github.com` over https only,
/// nothing for any other host, scheme or action. gix hands over the request as a URL only (`host` and
/// `protocol` are not filled in), so both are read from the URL.
pub fn offer_credentials(
    action: Action,
    user: &str,
    pass: &str,
) -> std::result::Result<Option<Outcome>, gix::Error> {
    let Action::Get(ctx) = action else {
        return Ok(None);
    };
    let parsed = ctx
        .url
        .as_ref()
        .and_then(|u| gix::url::parse(gix::bstr::BStr::new(u.as_slice())).ok());
    let (https, host) = match &parsed {
        Some(u) => (
            u.scheme == Scheme::Https,
            u.host().map(str::to_ascii_lowercase),
        ),
        None => (
            ctx.protocol.as_deref() == Some("https"),
            ctx.host.as_ref().map(|h| h.to_ascii_lowercase()),
        ),
    };
    if !https || host.as_deref() != Some("github.com") {
        return Ok(None);
    }
    Ok(Some(Outcome {
        identity: gix::sec::identity::Account {
            username: user.to_string(),
            password: pass.to_string(),
            oauth_refresh_token: None,
        },
        next: NextAction::from(ctx),
    }))
}

fn run_gix(repo: &gix::Repository, gh: Option<&Client>) -> Result<bool> {
    let Ok(remote) = repo.find_remote("origin") else {
        return Ok(false);
    };
    let remote = remote.with_fetch_tags(gix::remote::fetch::Tags::None);
    let mut conn = remote.connect(Direction::Fetch)?;
    // Always replace gix's own credential cascade: without a token it would run credential helpers named
    // in the repository's config (`credential.helper = !cmd`) on an http(s) 401. Our closure answers only
    // with the GitHub token for github.com and with nothing otherwise.
    let creds = gh.map(Client::basic_auth);
    conn = conn.with_credentials(move |action| match &creds {
        Some((user, pass)) => offer_credentials(action, user, pass),
        None => Ok(None),
    });
    let prep = conn.prepare_fetch(gix::progress::Discard, Default::default())?;
    prep.receive(gix::progress::Discard, &AtomicBool::new(false))?;
    Ok(true)
}
