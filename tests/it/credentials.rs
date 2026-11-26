//! Which credentials the https fetch offers, and to whom.
use gix::credentials::helper::Action;
use stw::sync::fetch::offer_credentials;

fn ask(url: &str) -> Option<(String, String)> {
    let answer = offer_credentials(Action::get_for_url(url), "x-access-token", "TOKEN-VALUE")
        .unwrap_or_else(|_| panic!("callback failed for {url}"));
    answer.map(|o| match o.credentials {
        gix::sec::identity::Credentials::UsernamePassword {
            username, password, ..
        } => (username, password),
        gix::sec::identity::Credentials::Encoded { .. } => {
            panic!("expected username/password credentials")
        }
    })
}

#[test]
fn token_is_offered_for_github_https_urls() {
    let (user, pass) =
        ask("https://github.com/wstein/private-repo.git").expect("credentials offered");
    assert_eq!(user, "x-access-token");
    assert_eq!(pass, "TOKEN-VALUE");
    assert!(
        ask("https://GitHub.com/wstein/x").is_some(),
        "host is case-insensitive"
    );
    assert!(ask("https://someone@github.com/wstein/x").is_some());
}

#[test]
fn token_is_not_offered_to_other_hosts() {
    for url in [
        "https://example.com/wstein/x.git",
        "https://github.com.evil.example/wstein/x.git",
        "https://evil.example/github.com/x.git",
        "https://notgithub.com/x.git",
    ] {
        assert!(ask(url).is_none(), "{url} must not get the token");
    }
}

#[test]
fn token_is_not_offered_over_plain_http_or_other_schemes() {
    for url in [
        "http://github.com/wstein/x.git",
        "ssh://git@github.com/wstein/x.git",
        "git://github.com/wstein/x.git",
        "https://github.com@evil.example/x.git",
        "https://evil.example#@github.com/x.git",
    ] {
        assert!(ask(url).is_none(), "{url} must not get the token");
    }
    assert!(
        ask("https://github.com:443/wstein/x.git").is_some(),
        "explicit port stays allowed"
    );
}

#[test]
fn store_and_erase_never_offer_anything() {
    for action in [
        Action::Store("protocol=https\nhost=github.com\n".into()),
        Action::Erase("protocol=https\nhost=github.com\n".into()),
    ] {
        assert!(offer_credentials(action, "u", "p").unwrap().is_none());
    }
}

#[test]
fn a_fetch_failure_text_never_carries_the_url_password() {
    use stw::sync::fetch::{failure, Fetch};
    for raw in [
        "http://user:SECRETPW@example.invalid/x/y.git",
        "ssh://user:SECRETPW@example.invalid/x/y.git",
        "https://user:SECRETPW@example.invalid/x/y.git",
    ] {
        let url = gix::url::parse(gix::bstr::BStr::new(raw.as_bytes())).unwrap();
        // gix texts that name the URL, and one that only contains the password
        for text in [
            format!("Could not connect to {raw}"),
            "authentication failed for password SECRETPW".to_string(),
        ] {
            let f = failure(&text, Some(&url));
            let msg = f.message().unwrap();
            assert!(!msg.contains("SECRETPW"), "{raw}: {msg}");
            let want = if raw.starts_with("https") {
                "failed"
            } else {
                "unsupported-remote"
            };
            assert_eq!(f.label(), want, "{raw}");
        }
    }
    assert!(matches!(failure("x", None), Fetch::Failed(_)));
}
