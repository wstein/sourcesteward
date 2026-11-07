//! Fetch of `origin` (apply mode only) through gix's own transports.
//!
//! stw tries the fetch for every remote kind and reports what gix says. For `https://` remotes gix needs
//! no other program. For ssh remotes gix runs the system `ssh`, and for local paths and `file://` it runs
//! `git-upload-pack` (observed with fake programs on PATH, see `tests/remote_kinds.rs`); when such a fetch
//! fails the result is typed (`unsupported-remote`) and the message says to use git directly.
//!
//! HTTPS credentials: when the server asks, the GitHub token found by `auth` is offered for `github.com`
//! only (user `x-access-token`). Other hosts get no credentials from us.
use std::sync::atomic::AtomicBool;

use gix::credentials::helper::{Action, NextAction};
use gix::credentials::protocol::Outcome;
use gix::remote::Direction;
use gix::url::Scheme;

use crate::error::Result;
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
}

impl Fetch {
    /// Value of the `fetch` field in the JSON output.
    pub fn label(&self) -> &'static str {
        match self {
            Fetch::Done => "ok",
            Fetch::NoRemote => "no-remote",
            Fetch::Unsupported(_) => "unsupported-remote",
            Fetch::Failed(_) => "failed",
        }
    }

    pub fn message(&self) -> Option<&str> {
        match self {
            Fetch::Unsupported(m) | Fetch::Failed(m) => Some(m),
            Fetch::Done | Fetch::NoRemote => None,
        }
    }
}

pub fn fetch_origin(repo: &gix::Repository, gh: Option<&Client>) -> Fetch {
    let url = repo
        .find_remote("origin")
        .ok()
        .and_then(|r| r.url(Direction::Fetch).cloned());
    match run(repo, gh) {
        Ok(true) => Fetch::Done,
        Ok(false) => Fetch::NoRemote,
        Err(e) => failure(&e.to_string(), url.as_ref()),
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

fn run(repo: &gix::Repository, gh: Option<&Client>) -> Result<bool> {
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
