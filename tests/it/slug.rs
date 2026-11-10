use crate::common::*;

fn slug(url: &str) -> Option<String> {
    let fx = Fx::new();
    fx.set_origin_url(url);
    stw::repo::slug(&fx.repo())
}

#[test]
fn slug_is_read_from_github_hosts_only() {
    for (url, want) in [
        ("https://github.com/wstein/stw.git", Some("wstein/stw")),
        ("https://github.com/wstein/stw/", Some("wstein/stw")),
        ("git@github.com:wstein/stw.git", Some("wstein/stw")),
        ("ssh://git@github.com/wstein/stw", Some("wstein/stw")),
        ("https://GitHub.com/wstein/stw", Some("wstein/stw")),
        ("https://gitlab.com/github.com/a/b.git", None),
        ("https://github.com.evil.example/a/b.git", None),
        ("https://example.com/x/github.com/a/b", None),
        ("git@gitlab.com:github.com/a.git", None),
    ] {
        assert_eq!(slug(url).as_deref(), want, "{url}");
    }
}
