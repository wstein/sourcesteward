//! The runner operations of `stw sync --prune`, `--no-prune` and `--submodules` (`Op::FetchOriginPrune`,
//! `Op::SubmoduleSync`, `Op::SubmoduleUpdate`): fixed argument vectors, transports, refused submodule config.
//! Harness confinement as in `sync_flags.rs`: repositories in tempdirs, a scrubbed environment.
use crate::common::scratch;
use crate::sync_flags::World;
use stw::git_runner::{GitError, Op, Runner};

fn argv(op: Op) -> Vec<String> {
    let t = scratch();
    Runner::from_env(vec![("PATH".into(), "/usr/bin".into())])
        .argv(t.path(), op)
        .unwrap()
        .iter()
        .map(|a| a.to_string_lossy().into_owned())
        .collect()
}

fn tail(v: &[String], from: &str) -> Vec<String> {
    let i = v
        .iter()
        .position(|a| a == from)
        .unwrap_or_else(|| panic!("{from} not in {v:?}"));
    v[i..].to_vec()
}

#[test]
fn the_new_operations_have_fixed_argument_vectors() {
    let on = argv(Op::FetchOriginPrune { prune: true });
    assert_eq!(
        tail(&on, "fetch"),
        [
            "fetch",
            "--quiet",
            "--no-tags",
            "--no-recurse-submodules",
            "--upload-pack=git-upload-pack",
            "--prune",
            "--no-prune-tags",
            "--",
            "origin"
        ]
    );
    let plan = argv(Op::FetchOriginNoPruneTags);
    assert_eq!(
        tail(&plan, "fetch"),
        [
            "fetch",
            "--quiet",
            "--no-tags",
            "--no-recurse-submodules",
            "--no-prune-tags",
            "--upload-pack=git-upload-pack",
            "--",
            "origin"
        ]
    );
    let off = argv(Op::FetchOriginPrune { prune: false });
    assert_eq!(
        tail(&off, "fetch"),
        [
            "fetch",
            "--quiet",
            "--no-tags",
            "--no-recurse-submodules",
            "--upload-pack=git-upload-pack",
            "--no-prune",
            "--no-prune-tags",
            "--",
            "origin"
        ]
    );
    let path = stw::add::SubPath::parse("projects/c").unwrap();
    assert_eq!(
        tail(&argv(Op::SubmoduleSync { path: path.clone() }), "submodule"),
        ["submodule", "sync", "--", "projects/c"]
    );
    let up = argv(Op::SubmoduleUpdate { path });
    assert_eq!(
        tail(&up, "submodule"),
        [
            "submodule",
            "update",
            "--no-fetch",
            "--checkout",
            "--",
            "projects/c"
        ]
    );
    for forbidden in ["--init", "--remote", "--recursive", "--force"] {
        assert!(!up.iter().any(|a| a == forbidden), "{forbidden}");
    }
    // the shared leading options (no recursion, no hooks, no ext transport) are in front of every vector
    assert!(up
        .windows(2)
        .any(|w| w == ["-c", "core.hooksPath=/dev/null"]));
    assert!(up
        .windows(2)
        .any(|w| w == ["-c", "submodule.recurse=false"]));
}

#[test]
fn the_submodule_operations_allow_no_transport_and_prune_keeps_the_fetch_transports() {
    let t = scratch();
    let r = Runner::from_env(vec![("PATH".into(), "/usr/bin".into())]);
    let allow = |op: &Op| {
        r.child_env_for(t.path(), op)
            .unwrap()
            .into_iter()
            .find(|(k, _)| k == "GIT_ALLOW_PROTOCOL")
            .map(|(_, v)| v.to_string_lossy().into_owned())
    };
    let path = stw::add::SubPath::parse("c").unwrap();
    assert_eq!(
        allow(&Op::SubmoduleSync { path: path.clone() }).as_deref(),
        Some("none")
    );
    assert_eq!(
        allow(&Op::SubmoduleUpdate { path }).as_deref(),
        Some("none")
    );
    let fetch = allow(&Op::FetchOrigin);
    assert_eq!(allow(&Op::FetchOriginPrune { prune: true }), fetch);
    assert_eq!(allow(&Op::FetchOriginPrune { prune: false }), fetch);
    assert_eq!(allow(&Op::FetchOriginNoPruneTags), fetch);
    assert!(!fetch.unwrap().contains("ext"));
}

#[test]
fn a_submodule_update_refuses_a_filter_driver_in_the_submodule_config() {
    let w = World::new();
    let ws = w.p("ws");
    std::fs::create_dir(&ws).unwrap();
    w.git(&ws, &["init", "-q"]);
    let sub = ws.join("c");
    std::fs::create_dir(&sub).unwrap();
    w.git(&sub, &["init", "-q"]);
    w.git(&sub, &["config", "filter.lfs.smudge", "x"]);
    let runner = Runner::from_env(vec![
        ("PATH".into(), std::env::var_os("PATH").unwrap()),
        ("HOME".into(), w.home.clone().into_os_string()),
    ]);
    let e = runner
        .run(
            &ws,
            Op::SubmoduleUpdate {
                path: stw::add::SubPath::parse("c").unwrap(),
            },
        )
        .unwrap_err();
    assert!(
        matches!(&e, GitError::Refused { key } if key == "filter.lfs.smudge"),
        "{e}"
    );
}
