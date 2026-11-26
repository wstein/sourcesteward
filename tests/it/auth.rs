use stw::auth::*;
use stw::github::Client;

const HOSTS: &str = "github.com:\n    users:\n        someone:\n            oauth_token: nested-token\n    git_protocol: https\n    user: someone\n    oauth_token: host-token\n";

fn env(pairs: &[(&str, &str)], hosts: Option<std::path::PathBuf>, svc: &str) -> AuthEnv {
    AuthEnv {
        env: pairs
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect(),
        hosts_yml: hosts,
        keychain_service: svc.to_string(),
    }
}

#[test]
fn hosts_yml_prefers_the_host_level_token_over_the_nested_one() {
    assert_eq!(token_from_hosts_yml(HOSTS).as_deref(), Some("host-token"));
    let only_nested =
        "github.com:\n    users:\n        a:\n            oauth_token: nested-token\n";
    assert_eq!(
        token_from_hosts_yml(only_nested).as_deref(),
        Some("nested-token")
    );
    assert_eq!(
        token_from_hosts_yml("example.com:\n    oauth_token: other\n"),
        None
    );
}

#[test]
fn go_keyring_encodings_are_decoded() {
    assert_eq!(decode_go_keyring("plain").as_deref(), Some("plain"));
    assert_eq!(
        decode_go_keyring("go-keyring-base64:dG9rZW4=").as_deref(),
        Some("token")
    );
    assert_eq!(
        decode_go_keyring("go-keyring-encoded:746f6b656e").as_deref(),
        Some("token")
    );
}

#[test]
fn order_is_gh_token_then_github_token_then_hosts_yml() {
    let dir = tempfile::tempdir().unwrap();
    let hosts = dir.path().join("hosts.yml");
    std::fs::write(&hosts, HOSTS).unwrap();
    let svc = "stw-test-no-such-service";
    let (t, s) = find_token(&env(
        &[("GH_TOKEN", "a"), ("GITHUB_TOKEN", "b")],
        Some(hosts.clone()),
        svc,
    ))
    .unwrap();
    assert_eq!((t.as_str(), s), ("a", TokenSource::Env));
    let (t, s) = find_token(&env(&[("GITHUB_TOKEN", "b")], Some(hosts.clone()), svc)).unwrap();
    assert_eq!((t.as_str(), s), ("b", TokenSource::Env));
    let (t, s) = find_token(&env(&[], Some(hosts), svc)).unwrap();
    assert_eq!((t.as_str(), s), ("host-token", TokenSource::HostsYml));
    assert!(find_token(&env(&[], Some(dir.path().join("missing.yml")), svc)).is_none());
}

#[test]
fn debug_output_never_contains_the_token() {
    let c = Client::new("super-secret-token".into(), TokenSource::Env);
    let shown = format!("{c:?}");
    assert!(!shown.contains("super-secret"), "{shown}");
}

/// Writes a throw-away item into the login keychain under a test-only service name, reads it back
/// through the same code path as gh's item (`gh:github.com`), then deletes it.
#[cfg(target_os = "macos")]
#[test]
fn macos_keychain_item_in_gh_format_is_found() {
    use security_framework::passwords::{
        delete_generic_password, get_generic_password, set_generic_password,
    };
    let svc = format!("stw-test-{}", std::process::id());
    let secret = "keychain-test-token";
    use base64::Engine;
    let stored = format!(
        "go-keyring-base64:{}",
        base64::engine::general_purpose::STANDARD.encode(secret)
    );
    if set_generic_password(&svc, "tester", stored.as_bytes()).is_err() {
        eprintln!("keychain not writable in this session: test skipped");
        return;
    }
    if get_generic_password(&svc, "tester").is_err() {
        let _ = delete_generic_password(&svc, "tester");
        eprintln!("keychain not readable in this session: test skipped");
        return;
    }
    let found = find_token(&env(&[], None, &svc));
    let _ = delete_generic_password(&svc, "tester");
    let (t, s) = found.expect("token from keychain");
    assert_eq!((t.as_str(), s), (secret, TokenSource::Keychain));
}
