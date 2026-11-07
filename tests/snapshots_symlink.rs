//! Own test binary: the test changes `TMPDIR` and `CLAUDE_JOB_DIR`, which must not leak into other tests.
mod common;
use common::*;

/// stw canonicalizes `--root`. With a temp dir behind a symlink (macOS: /var -> /private/var) the fixture path is
/// not the path stw prints; the snapshot must still be the same.
#[cfg(unix)]
#[test]
fn status_json_with_the_temp_dir_behind_a_symlink() {
    let real = scratch();
    let link = real.path().join("link");
    std::fs::create_dir(real.path().join("target")).unwrap();
    std::os::unix::fs::symlink(real.path().join("target"), &link).unwrap();
    // an environment as the fixtures support it: CLAUDE_JOB_DIR with a tmp/ is preferred over TMPDIR
    let job = scratch();
    std::fs::create_dir(job.path().join("tmp")).unwrap();
    let old_job = std::env::var_os("CLAUDE_JOB_DIR");
    std::env::set_var("CLAUDE_JOB_DIR", job.path());
    let old = std::env::var_os("TMPDIR");
    std::env::set_var("TMPDIR", &link);
    let fx = status_fixture();
    match old_job {
        Some(v) => std::env::set_var("CLAUDE_JOB_DIR", v),
        None => std::env::remove_var("CLAUDE_JOB_DIR"),
    }
    match old {
        Some(v) => std::env::set_var("TMPDIR", v),
        None => std::env::remove_var("TMPDIR"),
    }
    assert!(fx.path.starts_with(&link), "fixture is not behind the link");
    let pretty = status_json(&fx, &path_str(&fx.path));
    insta::with_settings!({ filters => vec![
        (r#""engine": "[^"]*""#, r#""engine": "[ENGINE]""#),
    ] }, {
        insta::assert_snapshot!("status_json_of_a_fixture_workspace", pretty);
    });
}
