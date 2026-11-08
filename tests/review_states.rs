//! Data-driven review states: every `tests/review_states/<name>.json` is one case (comments in, expected state
//! out). Placeholders in bodies and heads: `{A}`..`{D}` are 40 lower-case hex digits (a, b, c, d), `{P}` a
//! patch-id (f), `{AU}` upper-case A, `{A39}`/`{A41}`/`{A64}` the same digit 39, 41 or 64 times.
use std::path::Path;

use serde_json::Value;
use stw::review::{evaluate, ReviewComment, Scope};

fn expand(s: &str) -> String {
    let rep = |n: usize, c: char| c.to_string().repeat(n);
    s.replace("{A39}", &rep(39, 'a'))
        .replace("{A41}", &rep(41, 'a'))
        .replace("{A64}", &rep(64, 'a'))
        .replace("{AU}", &rep(40, 'A'))
        .replace("{A}", &rep(40, 'a'))
        .replace("{B}", &rep(40, 'b'))
        .replace("{C}", &rep(40, 'c'))
        .replace("{D}", &rep(40, 'd'))
        .replace("{P}", &rep(40, 'f'))
}

fn run(path: &Path) -> datatest_stable::Result<()> {
    let case: Value = serde_json::from_str(&std::fs::read_to_string(path)?)?;
    let scope = match &case["scope"] {
        Value::Number(n) => Scope::Pr(n.as_u64().unwrap()),
        Value::String(s) if s == "local" => Scope::Local,
        other => panic!("bad scope {other}"),
    };
    let head = expand(case["head"].as_str().unwrap());
    let trusted: Vec<String> = case["trusted"]
        .as_array()
        .unwrap()
        .iter()
        .map(|v| v.as_str().unwrap().to_string())
        .collect();
    let trusted: Vec<&str> = trusted.iter().map(String::as_str).collect();
    let comments: Vec<ReviewComment> = case["comments"]
        .as_array()
        .unwrap()
        .iter()
        .map(|c| {
            let created = c["created_at"].as_str().unwrap().to_string();
            ReviewComment {
                id: c["id"].as_u64().unwrap(),
                author: c["author"].as_str().unwrap().to_string(),
                updated_at: c["updated_at"].as_str().unwrap_or(&created).to_string(),
                created_at: created,
                body: expand(c["body"].as_str().unwrap()),
                body_truncated: false,
            }
        })
        .collect();
    let st = evaluate(scope, &head, &comments, &trusted);
    let want = &case["expect"];
    let name = path.display();
    assert_eq!(
        st.status.label(),
        want["status"].as_str().unwrap(),
        "{name}: status; {:?}",
        st.reasons
    );
    let decisive = st.decisive.as_ref().map(|e| e.comment_id);
    assert_eq!(decisive, want["decisive"].as_u64(), "{name}: decisive");
    if let Some(chain) = want.get("chain") {
        let got: Vec<u64> = st.chain.iter().map(|e| e.comment_id).collect();
        let want: Vec<u64> = chain
            .as_array()
            .unwrap()
            .iter()
            .map(|v| v.as_u64().unwrap())
            .collect();
        assert_eq!(got, want, "{name}: chain");
    }
    let mut got: Vec<String> = st
        .findings
        .iter()
        .map(|f| format!("{}:{}", f.kind.label(), f.comment_id))
        .collect();
    got.sort();
    let mut exp: Vec<String> = want["findings"]
        .as_array()
        .unwrap()
        .iter()
        .map(|v| v.as_str().unwrap().to_string())
        .collect();
    exp.sort();
    assert_eq!(got, exp, "{name}: findings");
    if let Some(r) = want.get("unknown_reason_contains") {
        assert!(
            st.reasons.iter().any(|x| x.contains(r.as_str().unwrap())),
            "{name}: reasons {:?}",
            st.reasons
        );
    }
    if let Some(e) = want.get("edited") {
        assert_eq!(
            st.decisive.unwrap().edited,
            e.as_bool().unwrap(),
            "{name}: edited"
        );
    }
    if st.status != stw::review::Status::Unknown {
        assert!(st.reasons.is_empty(), "{name}: reasons only for unknown");
    }
    Ok(())
}

datatest_stable::harness! {
    { test = run, root = "tests/review_states", pattern = r"\.json$" },
}
