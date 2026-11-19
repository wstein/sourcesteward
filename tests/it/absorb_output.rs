// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! `stw absorb`: the JSON document, the text, the masking of untrusted text and the files that are no modified text.
use std::os::unix::fs::{symlink, PermissionsExt};

use serde_json::{json, Value};

use super::absorb_fx::*;

fn keys(v: &Value) -> Vec<&str> {
    v.as_object().unwrap().keys().map(String::as_str).collect()
}

#[test]
fn json_document_has_the_keys_of_the_model() {
    let fx = Fx::new();
    let (c1, c2, c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "fixed"));
    let p = fx.plan(&[]);
    assert_eq!(
        keys(&p),
        [
            "schema_version",
            "command",
            "mode",
            "head",
            "branch",
            "index_digest",
            "base",
            "max_stack",
            "stack",
            "stack_end",
            "hunks",
            "files_skipped",
            "fixups",
            "rewrite",
            "summary",
            "effects",
            "plan_sum"
        ]
    );
    assert_eq!(
        (&p["schema_version"], &p["command"], &p["mode"]),
        (&json!(1), &json!("absorb"), &json!("plan"))
    );
    assert_eq!((&p["max_stack"], &p["base"]), (&json!(10), &Value::Null));
    assert_eq!((&p["head"], &p["branch"]), (&json!(c3), &json!("main")));
    assert_eq!(
        p["index_digest"],
        stw::sha256::hex(&fx.g_raw(&["ls-files", "-s", "-z"]))
    );
    let stack: Vec<&str> = p["stack"]
        .as_array()
        .unwrap()
        .iter()
        .map(|s| s["sha"].as_str().unwrap())
        .collect();
    assert_eq!(stack, [c3.as_str(), c2.as_str(), c1.as_str()]);
    assert_eq!(p["stack"][0]["subject"], "feat: three");
    assert_eq!(p["stack_end"], "root");
    assert_eq!(
        p["effects"],
        json!({"writes": [], "index_refresh": false, "network": false, "hooks": false, "prompts": false})
    );
    assert_eq!(
        keys(&p["hunks"][0]),
        [
            "id",
            "path",
            "old",
            "new",
            "assignment",
            "target",
            "basis",
            "reason",
            "origins"
        ]
    );
    assert_eq!(
        p["hunks"][0]["origins"],
        json!([{"sha": c2, "subject": "feat: two", "lines": 1}])
    );
    assert_eq!(keys(&p["rewrite"]), ["count", "commits", "ref", "new_shas"]);
    assert_eq!(
        keys(&p["summary"]),
        ["hunks", "absorbed", "skipped", "fixups", "rewritten"]
    );
}

#[test]
fn json_is_one_compact_line() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "fixed"));
    let o = fx.run(&["--json"]);
    assert_eq!(o.rc, 0, "{}", o.err);
    assert!(
        o.out.ends_with("}\n") && o.out.matches('\n').count() == 1,
        "{}",
        o.out
    );
    assert_eq!(
        o.out.trim_end(),
        serde_json::to_string(&serde_json::from_str::<Value>(&o.out).unwrap()).unwrap()
    );
}

#[test]
fn text_output_agrees_with_json() {
    let fx = Fx::new();
    let (_c1, c2, c3, t3) = fx.base10(true);
    fx.stage1(
        "a.txt",
        &replace_line(&insert_after(&t3, 5, "ins"), 3, "fixed"),
    );
    let o = fx.run(&[]);
    assert_eq!(o.rc, 0, "{}", o.err);
    assert!(!o.out.trim().is_empty(), "no output");
    let lines: Vec<&str> = o.out.lines().collect();
    assert_eq!(
        lines[0],
        format!(
            "absorb plan (read-only): head {} on main, stack 3 commit(s), ends at published",
            &c3[..7]
        )
    );
    assert_eq!(
        lines[1],
        format!(
            "h1 a.txt -3,1 +3,1 -> {} feat: two (overlaps-lines)",
            &c2[..7]
        )
    );
    assert_eq!(lines[2], "h2 a.txt -5,0 +6,1 skipped: no-candidate");
    assert_eq!(lines[3], "fixup! feat: two <- h1");
    assert_eq!(
        lines[4],
        format!(
            "rewrite: 2 commit(s) get new SHAs (oldest {}); nothing was written",
            &c2[..7]
        )
    );
    assert_eq!(lines.len(), 5);
    assert!(!o.out.contains("git "), "no command for the caller to type");
    let h = fx.run(&["--human"]);
    assert_eq!(h.out, o.out, "--human is the default");
}

#[test]
fn text_output_without_absorbed_hunks() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(true);
    fx.stage1("a.txt", &insert_after(&t3, 5, "x"));
    let o = fx.run(&[]);
    assert_eq!(o.rc, 0);
    assert!(
        o.out
            .lines()
            .last()
            .unwrap()
            .starts_with("rewrite: 0 commit(s)"),
        "{}",
        o.out
    );
}

#[test]
fn human_and_json_cannot_be_combined() {
    let fx = Fx::new();
    fx.base10(false);
    let o = fx.run(&["--human", "--json"]);
    assert_eq!(o.rc, 2);
    assert!(!o.err.trim().is_empty());
}

#[test]
fn control_and_bidi_characters_are_masked() {
    let fx = Fx::new();
    let t1 = l(4);
    let name = "a\u{1b}\u{202e}b.txt";
    fx.commit1(name, &t1, "feat: evil \u{202e}\u{7} subject\u{200b} end");
    fx.commit1("z.txt", "z\n", "feat: other");
    fx.stage1(name, &replace_line(&t1, 2, "f"));
    let p = fx.plan(&[]);
    let h = hunk(&p, 0);
    assert_eq!(h["path"], "a??b.txt");
    assert_eq!(h["target"]["subject"], "feat: evil ?? subject? end");
    assert_eq!(
        p["fixups"][0]["subject"],
        "fixup! feat: evil ?? subject? end"
    );
    let o = fx.run(&[]);
    for bad in ["\u{1b}", "\u{202e}", "\u{7}", "\u{200b}"] {
        assert!(!o.out.contains(bad), "{bad:?} in {}", o.out);
    }
    assert!(o.out.contains("a??b.txt"));
}

#[test]
fn a_path_that_is_not_utf8_has_no_blame_and_is_refused_with_a_masked_message() {
    let fx = Fx::new();
    // `git add` takes the name as an argument: build the file name from bytes
    use std::os::unix::ffi::OsStrExt;
    let name = std::ffi::OsStr::from_bytes(b"k\xffl.txt");
    std::fs::write(fx.r.join(name), l(4)).unwrap();
    fx.g(&["add", "-A", "."]);
    fx.commit(&[], "feat: odd name");
    fx.commit1("z.txt", "z\n", "feat: other");
    std::fs::write(fx.r.join(name), replace_line(&l(4), 2, "f")).unwrap();
    fx.g(&["add", "-A", "."]);
    // the removed lines need `git blame` and its path argument is text: no evidence, no plan (a limit, named in the README)
    let d = fx.error("blame-failed", &[]);
    let msg = d["error"]["message"].as_str().unwrap();
    assert!(msg.contains("k?l.txt"), "{msg}");
    assert!(msg.is_ascii() || !msg.contains('\u{fffd}'), "{msg}");
}

#[test]
fn files_that_are_not_modified_text_are_reported() {
    let fx = Fx::new();
    fx.write("bin.dat", [0u8, 1, 2]);
    fx.commit(
        &[
            ("a.txt", Some(&l(4))),
            ("old.txt", Some("gone\n")),
            ("mv.txt", Some("move me\nsecond line\nthird line\n")),
            ("m.sh", Some("echo\n")),
        ],
        "feat: base",
    );
    symlink("a.txt", fx.r.join("ln")).unwrap();
    fx.g(&["add", "ln"]);
    fx.commit(&[], "feat: link");
    fx.stage(&[("new.txt", Some("new\n")), ("old.txt", None)]);
    fx.write("bin.dat", [0u8, 1, 3]);
    fx.g(&["add", "bin.dat"]);
    fx.g(&["mv", "mv.txt", "moved.txt"]);
    std::fs::set_permissions(fx.r.join("m.sh"), std::fs::Permissions::from_mode(0o755)).unwrap();
    fx.g(&["add", "m.sh"]);
    std::fs::remove_file(fx.r.join("ln")).unwrap();
    symlink("b.txt", fx.r.join("ln")).unwrap();
    fx.g(&["add", "ln"]);
    fx.g(&[
        "update-index",
        "--add",
        "--cacheinfo",
        &format!("160000,{},sub", "1".repeat(40)),
    ]);
    let p = fx.plan(&[]);
    let got: Vec<(&str, &str)> = p["files_skipped"]
        .as_array()
        .unwrap()
        .iter()
        .map(|f| (f["path"].as_str().unwrap(), f["reason"].as_str().unwrap()))
        .collect();
    assert_eq!(
        got,
        [
            ("bin.dat", "binary"),
            ("ln", "symlink"),
            ("m.sh", "mode-only"),
            ("moved.txt", "added"),
            ("mv.txt", "deleted"),
            ("new.txt", "added"),
            ("old.txt", "deleted"),
            ("sub", "submodule"),
        ],
        "sorted by path"
    );
    assert_eq!(p["hunks"], json!([]));
    let o = fx.run(&[]);
    assert!(o.out.contains("file bin.dat skipped: binary"), "{}", o.out);
}

#[test]
fn typechange_is_reported() {
    let fx = Fx::new();
    fx.commit(
        &[("a.txt", Some(&l(4))), ("t", Some("file\n"))],
        "feat: base",
    );
    std::fs::remove_file(fx.r.join("t")).unwrap();
    symlink("a.txt", fx.r.join("t")).unwrap();
    fx.g(&["add", "t"]);
    assert_eq!(
        fx.plan(&[])["files_skipped"],
        json!([{"path": "t", "reason": "typechange"}])
    );
}

#[test]
fn mode_change_together_with_a_content_change_still_has_the_hunk() {
    let fx = Fx::new();
    let t = l(5);
    let c1 = fx.commit1("a.sh", &t, "feat: script");
    fx.commit1("b.txt", "b\n", "feat: other");
    fx.stage1("a.sh", &replace_line(&t, 2, "fixed"));
    std::fs::set_permissions(fx.r.join("a.sh"), std::fs::Permissions::from_mode(0o755)).unwrap();
    fx.g(&["add", "a.sh"]);
    let p = fx.plan(&[]);
    assert_eq!(hunk(&p, 0)["target"]["sha"], c1);
    assert_eq!(p["files_skipped"], json!([]));
}

#[test]
fn renamed_file_is_delete_plus_add_and_not_followed() {
    let fx = Fx::new();
    fx.commit1("old.txt", &l(8), "feat: one");
    fx.commit1("x.txt", "x\n", "feat: two");
    fx.g(&["mv", "old.txt", "new.txt"]);
    let p = fx.plan(&[]);
    assert_eq!(
        p["files_skipped"],
        json!([{"path": "new.txt", "reason": "added"}, {"path": "old.txt", "reason": "deleted"}])
    );
}

#[test]
fn rename_with_an_edit_in_the_index_is_not_followed_either() {
    let fx = Fx::new();
    let t = l(8);
    fx.commit1("old.txt", &t, "feat: one");
    fx.commit1("x.txt", "x\n", "feat: two");
    fx.g(&["mv", "old.txt", "new.txt"]);
    fx.stage1("new.txt", &replace_line(&t, 3, "edited"));
    assert_eq!(fx.plan(&[])["hunks"], json!([]));
}

#[test]
fn a_file_renamed_in_the_stack_is_found_under_its_new_name_only_as_added() {
    let fx = Fx::new();
    let t = l(8);
    fx.commit1("old.txt", &t, "feat: one");
    fx.g(&["mv", "old.txt", "new.txt"]);
    let c2 = fx.commit(&[], "refactor: rename");
    fx.commit1("x.txt", "x\n", "feat: three");
    fx.stage1("new.txt", &replace_line(&t, 3, "edited"));
    let p = fx.plan(&[]);
    assert_eq!(hunk(&p, 0)["target"]["sha"], c2);
    assert_eq!(hunk(&p, 0)["basis"], "file-added");
}

#[test]
fn the_subject_is_the_first_paragraph_on_one_line() {
    let fx = Fx::new();
    let t = l(6);
    fx.commit1(
        "a.txt",
        &t,
        "\n\nfeat: first line  \n  continued on the second\n\nbody of the message\n",
    );
    fx.commit1("b.txt", "x\n", "feat: other");
    fx.stage1("a.txt", &replace_line(&t, 2, "f"));
    let p = fx.plan(&[]);
    assert_eq!(
        hunk(&p, 0)["target"]["subject"],
        "feat: first line   continued on the second"
    );
}
