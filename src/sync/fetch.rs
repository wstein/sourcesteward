//! Fetch of `origin` (apply mode only).
//!
//! By remote kind: `ssh://`, `user@host:path`, local paths and `file://` are fetched by the installed `git`
//! through the runner (`git_runner`, reported as `"via": "git"`), because gix would start `ssh` or
//! `git-upload-pack` itself for them (`tests/remote_kinds.rs` proves it no longer does). `https://`,
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
        Scheme::Ssh | Scheme::File => {
            let dir = repo
                .workdir()
                .map(std::path::Path::to_path_buf)
                .unwrap_or_else(|| repo.git_dir().to_path_buf());
            match git_runner::shared().run(&dir, Op::FetchOrigin) {
                Ok(_) => done(Fetch::Done, Some(git_runner::VIA), None),
                Err(e) => {
                    let msg: String = scrub(&e.to_string(), url.as_ref())
                        .chars()
                        .take(400)
                        .collect();
                    let fetch = if e.is_unsupported() {
                        Fetch::GitUnsupported(msg)
                    } else {
                        Fetch::Failed(msg)
                    };
                    done(fetch, Some(git_runner::VIA), Some(e.kind()))
                }
            }
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
    if let Some(c) = gh {
        let (user, pass) = c.basic_auth();
        conn = conn.with_credentials(move |action| offer_credentials(action, &user, &pass));
    }
    let prep = conn.prepare_fetch(gix::progress::Discard, Default::default())?;
    prep.receive(gix::progress::Discard, &AtomicBool::new(false))?;
    Ok(true)
}
