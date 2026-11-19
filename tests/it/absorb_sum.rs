//! The input of the plan checksum of `stw absorb`: the canonical JSON text (the encoding of the Python spike) and the
//! document it is taken over. Expected texts are from `json.dumps(sort_keys=True, separators=(",", ":"))`.
use serde_json::json;
use stw::absorb::{canonical_json, plan_sum};
use stw::sha256::hex;

#[test]
fn keys_are_sorted_at_every_level_and_nothing_is_padded() {
    let v = json!({"z": [1, null, true, false], "a": {"b": 2, "a": -1}, "m": []});
    assert_eq!(
        canonical_json(&v),
        r#"{"a":{"a":-1,"b":2},"m":[],"z":[1,null,true,false]}"#
    );
}

#[test]
fn non_ascii_text_is_escaped_as_the_spike_does() {
    let v = json!(["ü中😀\u{1}\n\"\\\u{7f}/"]);
    // DEL is escaped too: everything outside space..~ is
    assert_eq!(
        canonical_json(&v),
        r#"["\u00fc\u4e2d\ud83d\ude00\u0001\n\"\\\u007f/"]"#
    );
}

#[test]
fn the_checksum_covers_options_plan_and_the_unmasked_paths() {
    let options = json!({"base": null, "max_stack": 10, "ambiguous_newest": false});
    let plan = json!({"k": "v"});
    let raw = vec![b"a\xffb\xc3\xbc".to_vec()];
    let want = r#"{"options":{"ambiguous_newest":false,"base":null,"max_stack":10},"plan":{"k":"v"},"raw_paths":["a\udcffb\u00fc"]}"#;
    assert_eq!(plan_sum(&options, &raw, &plan), hex(want.as_bytes()));
}

#[test]
fn the_checksum_follows_each_of_its_three_parts() {
    let options = json!({"max_stack": 10});
    let plan = json!({"k": "v"});
    let raw = vec![b"a".to_vec()];
    let base = plan_sum(&options, &raw, &plan);
    assert_ne!(base, plan_sum(&json!({"max_stack": 11}), &raw, &plan));
    assert_ne!(base, plan_sum(&options, &[b"b".to_vec()], &plan));
    assert_ne!(base, plan_sum(&options, &raw, &json!({"k": "w"})));
    assert_eq!(base.len(), 64);
}
