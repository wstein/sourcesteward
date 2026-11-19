//! Structure check of the watch scenarios (port corpus of `stw watch`, `tests/fixtures/watch/`). Only the shape is checked here:
//! required fields, unique names, step order, consistency of the expectations and that `FORMAT.md` names every field this test
//! checks. The semantics are played by the ports of the core, the observer and the command (packages W1 to W4).
use std::collections::BTreeSet;
use std::fs;
use std::path::PathBuf;

use serde_json::Value;

const KINDS: [&str; 5] = [
    "watch-cycles",
    "vectors",
    "observer-session",
    "http-reader",
    "run-loop",
];
const EVENT_KINDS: [&str; 15] = [
    "new-pr",
    "head-changed",
    "checks-green",
    "checks-red",
    "checks-pending-timeout",
    "mergeable-changed",
    "ready-state-changed",
    "base-changed",
    "merged",
    "closed",
    "restack-needed",
    "repos-changed",
    "watch-error",
    "watch-recovered",
    "watch-stalled",
];
const FUNCTIONS: [&str; 30] = [
    "classify_checks",
    "backoff_seconds",
    "status_freshness_seconds",
    "event_id",
    "id_counter",
    "workspace_id",
    "default_state_dir",
    "event_json",
    "restack_candidates",
    "merged_due_head_fork",
    "decode_pull_commits",
    "restack_with_hosting_branch_check",
    "human_line",
    "human_line_properties",
    "parse_config",
    "parse_remote",
    "split_remote",
    "valid_host",
    "resolve_repositories",
    "prepare_flags",
    "decode_pull_detail",
    "observe_default_branch",
    "encode_branch_segment",
    "branch_segment_valid",
    "assess_rate_limit",
    "rate_limited_until",
    "next_page_from_link",
    "parse_retry_after",
    "parse_rate_headers",
    "watch_read_endpoint_allowed",
];

const COMMON: &[&str] = &[
    "format_version",
    "name",
    "kind",
    "description",
    "source",
    "source.go_tests",
    "format_dependent",
];

/// Required fields per kind as paths: `[]` is every element of an array, `*` every value of an object.
fn required(kind: &str) -> Vec<&'static str> {
    match kind {
        "watch-cycles" => vec![
            "config",
            "restacker",
            "cycles",
            "expected_events",
            "expected_state_view",
            "state_view_ignore",
            "expected_journal_ordinals",
            "expect_unique_event_ids",
            "status_probes",
            "config.repos",
            "config.skipped",
            "config.host",
            "config.start_time",
            "config.interval_seconds",
            "config.pending_timeout_seconds",
            "config.detail_every",
            "config.ignore_authors",
            "config.ignore_labels",
            "config.stack_patterns",
            "config.mode",
            "restacker.rule",
            "cycles[].step",
            "cycles[].action",
            "cycles[].time",
            "cycles[].note",
            "cycles[].config_change",
            "cycles[].restart_before",
            "cycles[].file_op",
            "cycles[].crash",
            "cycles[].observation",
            "cycles[].observation.*.error",
            "cycles[].observation.*.default_branch",
            "cycles[].observation.*.prs",
            "cycles[].observation.*.gone",
            "cycles[].observation.*.prs[].number",
            "cycles[].observation.*.prs[].title",
            "cycles[].observation.*.prs[].author",
            "cycles[].observation.*.prs[].labels",
            "cycles[].observation.*.prs[].draft",
            "cycles[].observation.*.prs[].head",
            "cycles[].observation.*.prs[].head_ref",
            "cycles[].observation.*.prs[].base",
            "cycles[].observation.*.prs[].fork",
            "cycles[].observation.*.prs[].fork_known",
            "cycles[].observation.*.prs[].detail",
            "cycles[].observation.*.prs[].checks",
            "expected_events[].step",
            "expected_events[].result",
            "expected_events[].error_kind",
            "expected_events[].retry_in_seconds",
            "expected_events[].events",
            "expected_events[].printed",
            "expected_events[].hints",
            "expected_events[].state_after",
            "expected_events[].go_message",
            "expected_events[].events[].ordinal",
            "expected_events[].events[].step",
            "expected_events[].events[].time",
            "expected_events[].events[].kind",
            "expected_events[].events[].repo",
            "expected_events[].events[].pr",
            "expected_events[].events[].details",
            "expected_events[].events[].go",
            "expected_events[].events[].go.id",
            "expected_events[].events[].go.seq",
            "expected_state_view.cycle",
            "expected_state_view.repos_epoch",
            "expected_state_view.interval_seconds",
            "expected_state_view.repos",
            "expected_state_view.skipped",
            "expected_state_view.repo",
            "expected_state_view.run_health",
            "expected_state_view.repo.*.baselined",
            "expected_state_view.repo.*.since",
            "expected_state_view.repo.*.last_ok",
            "expected_state_view.repo.*.default_branch",
            "expected_state_view.repo.*.prs",
            "expected_state_view.repo.*.closed",
            "expected_state_view.repo.*.restack_due",
            "expected_state_view.repo.*.restack_done",
            "expected_state_view.repo.*.health",
            "expected_state_view.repo.*.health.failures",
            "expected_state_view.repo.*.health.episode",
            "expected_state_view.repo.*.health.error_open",
            "expected_state_view.repo.*.health.last_code",
            "expected_state_view.repo.*.health.stalled",
            "expected_state_view.repo.*.health.stall_episode",
            "expected_state_view.repo.*.health.next_try",
            "expected_state_view.repo.*.prs.*.number",
            "expected_state_view.repo.*.prs.*.generation",
            "expected_state_view.repo.*.prs.*.epochs",
            "expected_state_view.repo.*.prs.*.epochs.head",
            "expected_state_view.repo.*.prs.*.epochs.checks",
            "expected_state_view.repo.*.prs.*.epochs.mergeable",
            "expected_state_view.repo.*.prs.*.epochs.ready",
            "expected_state_view.repo.*.prs.*.epochs.base",
            "expected_state_view.repo.*.prs.*.class",
            "expected_state_view.repo.*.prs.*.class_candidate",
            "expected_state_view.repo.*.prs.*.pending_since",
            "expected_state_view.repo.*.prs.*.pending_names",
            "expected_state_view.repo.*.prs.*.timeout_fired",
            "expected_state_view.repo.*.prs.*.mergeable",
            "expected_state_view.repo.*.prs.*.mergeable_state",
            "expected_state_view.repo.*.prs.*.conflicting",
            "expected_state_view.repo.*.prs.*.behind",
            "expected_state_view.repo.*.prs.*.mergeable_candidate",
            "expected_state_view.repo.*.prs.*.detail_due",
            "expected_state_view.repo.*.prs.*.gone",
            "expected_state_view.run_health.failures",
        ],
        "vectors" => vec![
            "function",
            "cases",
            "cases[].name",
            "cases[].input",
            "cases[].expected",
        ],
        "observer-session" => vec![
            "config",
            "steps",
            "expected",
            "config.max_open_prs",
            "config.ignore_authors",
            "config.ignore_labels",
            "config.clock",
            "steps[].note",
            "steps[].world",
            "steps[].new_process",
            "steps[].hints",
            "steps[].hints.cycle",
            "steps[].hints.detail_every",
            "steps[].hints.known",
            "steps[].after_cycle_first",
            "steps[].context_canceled",
            "steps[].world.list_items",
            "steps[].world.list_error",
            "steps[].world.pull_details",
            "steps[].world.pull_detail_etags",
            "steps[].world.branch_tips",
            "steps[].world.check_runs",
            "steps[].world.commit_statuses",
            "steps[].world.commits",
            "steps[].world.pull_commits",
            "steps[].world.rate_limit",
            "steps[].world.call_errors",
            "expected[].step",
            "expected[].observation",
            "expected[].error",
            "expected[].calls",
            "expected[].diagnostics",
            "expected[].slow_after_cycle",
            "expected[].calls[].call",
            "expected[].calls[].conditional",
        ],
        "http-reader" => vec![
            "host",
            "routes",
            "calls",
            "expected",
            "routes[].uri",
            "routes[].status",
            "routes[].headers",
            "routes[].body",
            "routes[].body_repeat",
            "routes[].declared_content_length",
            "routes[].etag",
            "routes[].not_modified_on_matching_validator",
            "routes[].echo_authorization_header",
            "calls[].op",
            "calls[].repo",
            "calls[].number",
            "calls[].page",
            "calls[].per_page",
            "calls[].name",
            "calls[].sha",
            "calls[].if_none_match",
            "calls[].max_open_prs",
            "calls[].prior_list_state",
            "calls[].prior_per_page",
            "calls[].new_reader_before",
            "calls[].reader_already_halved",
            "calls[].set_routes",
            "calls[].note",
            "expected[].call",
            "expected[].error",
            "expected[].response",
            "expected[].list",
            "expected[].rate_info",
            "expected[].requests",
            "expected[].requests[].uri",
            "expected[].requests[].if_none_match",
        ],
        "run-loop" => vec![
            "config",
            "invocations",
            "expected",
            "config.repos",
            "config.host",
            "config.interval_seconds",
            "config.detail_every",
            "config.pending_timeout_seconds",
            "config.max_open_prs",
            "config.jitter_seconds",
            "config.start_time",
            "invocations[].note",
            "invocations[].action",
            "invocations[].mode",
            "invocations[].until_kinds",
            "invocations[].timeout_seconds",
            "invocations[].max_idle_seconds",
            "invocations[].human",
            "invocations[].baseline",
            "invocations[].advance_seconds",
            "invocations[].worlds",
            "invocations[].cancel_after_sleeps",
            "invocations[].state_dir_kind",
            "invocations[].state_file",
            "invocations[].reader_cannot_open",
            "invocations[].status_json",
            "invocations[].status_now_offset_seconds",
            "expected[].invocation",
            "expected[].exit_code",
            "expected[].error_mentions",
            "expected[].stdout_events",
            "expected[].stdout_lines",
            "expected[].sleeps_seconds",
            "expected[].status_fresh",
            "expected[].status_text",
        ],
        _ => vec![],
    }
}

fn top_level(kind: &str) -> BTreeSet<String> {
    let mut set: BTreeSet<String> = COMMON
        .iter()
        .filter(|p| !p.contains('.'))
        .map(|s| s.to_string())
        .collect();
    for p in required(kind) {
        if !p.contains('.') && !p.contains('[') {
            set.insert(p.to_string());
        }
    }
    set
}

fn fixtures_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/watch")
}

fn format_md() -> String {
    fs::read_to_string(fixtures_dir().join("FORMAT.md"))
        .expect("tests/fixtures/watch/FORMAT.md is missing")
}

/// Every scenario file (not index.json), sorted by name, with its parsed content.
fn scenarios() -> Vec<(String, Value)> {
    let mut out = Vec::new();
    let dir = fixtures_dir();
    let entries =
        fs::read_dir(&dir).unwrap_or_else(|e| panic!("cannot read {}: {e}", dir.display()));
    for e in entries {
        let p = e.unwrap().path();
        let name = p.file_name().unwrap().to_string_lossy().to_string();
        if !name.ends_with(".json") || name == "index.json" {
            continue;
        }
        let text = fs::read_to_string(&p).unwrap();
        let v: Value =
            serde_json::from_str(&text).unwrap_or_else(|e| panic!("{name}: not JSON: {e}"));
        assert!(text.ends_with("}\n"), "{name}: must end with one newline");
        out.push((name, v));
    }
    out.sort_by(|a, b| a.0.cmp(&b.0));
    out
}

/// The values a path selects. `[]` selects every element of an array, `*` every value of an object.
fn select<'a>(v: &'a Value, path: &str) -> Vec<&'a Value> {
    let mut cur = vec![v];
    for seg in path.split('.') {
        let (key, many) = match seg.strip_suffix("[]") {
            Some(k) => (k, true),
            None => (seg, false),
        };
        let mut next = Vec::new();
        for c in cur {
            let child = if key == "*" {
                match c.as_object() {
                    Some(o) => o.values().collect::<Vec<_>>(),
                    None => continue,
                }
            } else {
                match c.get(key) {
                    Some(x) => vec![x],
                    None => continue,
                }
            };
            for x in child {
                if many {
                    if let Some(a) = x.as_array() {
                        next.extend(a.iter());
                    }
                } else {
                    next.push(x);
                }
            }
        }
        cur = next;
    }
    cur
}

/// The parents of a path (everything before the last segment) and its last key.
fn split_last(path: &str) -> (Option<&str>, &str) {
    match path.rsplit_once('.') {
        Some((p, k)) => (Some(p), k.trim_end_matches("[]")),
        None => (None, path.trim_end_matches("[]")),
    }
}

fn check_required(v: &Value, name: &str, problems: &mut Vec<String>) {
    for path in COMMON
        .iter()
        .copied()
        .chain(required(v["kind"].as_str().unwrap_or("")))
    {
        // a container path that ends in [] is only a container; its members are checked by the longer paths
        let (parent, key) = split_last(path);
        let parents = match parent {
            Some(p) => select(v, p),
            None => vec![v],
        };
        for p in parents {
            if p.is_object() && p.get(key).is_none() {
                problems.push(format!("{name}: `{path}` is missing"));
                break;
            }
        }
    }
}

/// All problems of one scenario file.
fn check_scenario(v: &Value, file: &str) -> Vec<String> {
    let mut problems = Vec::new();
    let stem = file.trim_end_matches(".json");
    let Some(kind) = v["kind"].as_str() else {
        return vec![format!("{file}: `kind` is missing")];
    };
    if !KINDS.contains(&kind) {
        return vec![format!("{file}: unknown kind {kind}")];
    }
    check_required(v, file, &mut problems);
    if v["format_version"] != 1 {
        problems.push(format!("{file}: format_version must be 1"));
    }
    if v["name"].as_str() != Some(stem) {
        problems.push(format!("{file}: `name` must be the file name"));
    }
    if v["description"]
        .as_str()
        .is_none_or(|d| d.trim().is_empty())
    {
        problems.push(format!("{file}: `description` is empty"));
    }
    match v["source"]["go_tests"].as_array() {
        Some(a)
            if !a.is_empty()
                && a.iter()
                    .all(|t| t.as_str().is_some_and(|s| s.contains(".go:Test"))) => {}
        _ => problems.push(format!(
            "{file}: `source.go_tests` must list Go tests as file.go:TestName"
        )),
    }
    if !v["format_dependent"].is_array() {
        problems.push(format!("{file}: `format_dependent` must be a list"));
    }
    let known = top_level(kind);
    if let Some(o) = v.as_object() {
        for k in o.keys() {
            if !known.contains(k) {
                problems.push(format!("{file}: undocumented top-level field `{k}`"));
            }
        }
    }
    match kind {
        "watch-cycles" => check_cycles(v, file, &mut problems),
        "vectors" => check_vectors(v, file, &mut problems),
        "observer-session" => check_parallel(v, file, "steps", "expected", "step", &mut problems),
        "http-reader" => check_parallel(v, file, "calls", "expected", "call", &mut problems),
        "run-loop" => check_parallel(
            v,
            file,
            "invocations",
            "expected",
            "invocation",
            &mut problems,
        ),
        _ => {}
    }
    problems
}

fn arr<'a>(v: &'a Value, key: &str) -> &'a [Value] {
    v[key].as_array().map(|a| a.as_slice()).unwrap_or(&[])
}

fn check_parallel(
    v: &Value,
    file: &str,
    inputs: &str,
    outputs: &str,
    num: &str,
    problems: &mut Vec<String>,
) {
    let (a, b) = (arr(v, inputs), arr(v, outputs));
    if a.is_empty() {
        problems.push(format!("{file}: `{inputs}` is empty"));
    }
    if a.len() != b.len() {
        problems.push(format!(
            "{file}: {} `{inputs}` but {} `{outputs}`",
            a.len(),
            b.len()
        ));
    }
    for (i, e) in b.iter().enumerate() {
        if e[num].as_u64() != Some(i as u64 + 1) {
            problems.push(format!("{file}: `{outputs}[{i}].{num}` must be {}", i + 1));
        }
    }
}

fn check_vectors(v: &Value, file: &str, problems: &mut Vec<String>) {
    match v["function"].as_str() {
        Some(f) if FUNCTIONS.contains(&f) => {}
        other => problems.push(format!("{file}: unknown `function` {other:?}")),
    }
    let cases = arr(v, "cases");
    if cases.is_empty() {
        problems.push(format!("{file}: no `cases`"));
    }
    let mut seen = BTreeSet::new();
    for c in cases {
        let n = c["name"].as_str().unwrap_or("");
        if n.is_empty() || !seen.insert(n.to_string()) {
            problems.push(format!("{file}: case name `{n}` is empty or repeated"));
        }
    }
}

fn check_cycles(v: &Value, file: &str, problems: &mut Vec<String>) {
    let cycles = arr(v, "cycles");
    let expected = arr(v, "expected_events");
    if cycles.is_empty() {
        problems.push(format!("{file}: no `cycles`"));
    }
    if cycles.len() != expected.len() {
        problems.push(format!(
            "{file}: {} cycles but {} `expected_events`",
            cycles.len(),
            expected.len()
        ));
    }
    if !v["expected_state_view"].is_object() {
        problems.push(format!("{file}: `expected_state_view` must be an object"));
    }
    if v["expect_unique_event_ids"] != true {
        problems.push(format!("{file}: `expect_unique_event_ids` must be true"));
    }
    let mut last_time = "";
    for (i, c) in cycles.iter().enumerate() {
        if c["step"].as_u64() != Some(i as u64 + 1) {
            problems.push(format!("{file}: cycle {i}: `step` must be {}", i + 1));
        }
        if !matches!(c["action"].as_str(), Some("cycle" | "open")) {
            problems.push(format!("{file}: cycle {i}: unknown `action`"));
        }
        let t = c["time"].as_str().unwrap_or("");
        if t.len() != 20 || !t.ends_with('Z') || t < last_time {
            problems.push(format!(
                "{file}: cycle {i}: `time` {t:?} is not an RFC 3339 UTC time in order"
            ));
        }
        last_time = t;
        if !c["observation"].is_object() {
            problems.push(format!(
                "{file}: cycle {i}: `observation` must be an object"
            ));
        }
    }
    let mut ordinals = Vec::new();
    let mut ids = BTreeSet::new();
    for (i, e) in expected.iter().enumerate() {
        if e["step"].as_u64() != Some(i as u64 + 1) {
            problems.push(format!(
                "{file}: expected_events[{i}]: `step` must be {}",
                i + 1
            ));
        }
        if !matches!(e["result"].as_str(), Some("ok" | "crashed" | "error")) {
            problems.push(format!("{file}: expected_events[{i}]: unknown `result`"));
        }
        for ev in arr(e, "events") {
            ordinals.push(ev["ordinal"].as_u64().unwrap_or(0));
            if !ev["kind"]
                .as_str()
                .is_some_and(|k| EVENT_KINDS.contains(&k))
            {
                problems.push(format!(
                    "{file}: expected_events[{i}]: unknown event kind {:?}",
                    ev["kind"]
                ));
            }
            if ev["step"].as_u64() != Some(i as u64 + 1) {
                problems.push(format!(
                    "{file}: expected_events[{i}]: an event of another step"
                ));
            }
            if !ev["details"].is_object() {
                problems.push(format!(
                    "{file}: expected_events[{i}]: `details` must be an object"
                ));
            }
            if !ids.insert(ev["go"]["id"].as_str().unwrap_or("").to_string()) {
                problems.push(format!("{file}: expected_events[{i}]: a repeated event id"));
            }
        }
    }
    let journal: Vec<u64> = arr(v, "expected_journal_ordinals")
        .iter()
        .map(|x| x.as_u64().unwrap_or(0))
        .collect();
    let gapless: Vec<u64> = (1..=journal.len() as u64).collect();
    if journal != gapless || ordinals != journal {
        problems.push(format!(
            "{file}: event ordinals must be gapless and equal `expected_journal_ordinals`"
        ));
    }
    for e in expected {
        for p in arr(e, "printed") {
            if !p.as_u64().is_some_and(|n| journal.contains(&n)) {
                problems.push(format!(
                    "{file}: a printed ordinal {p} is not in the journal"
                ));
            }
        }
    }
}

fn sample(kind: &str) -> (String, Value) {
    let preferred = match kind {
        "watch-cycles" => "merged-and-restack-needed.json",
        "vectors" => "checks-classify-classes.json",
        "observer-session" => "observe-baseline-reads-every-pr-fully.json",
        "http-reader" => "http-conditional-round-trip.json",
        _ => "run-once-baseline-then-transitions.json",
    };
    scenarios()
        .into_iter()
        .find(|(n, _)| n == preferred)
        .unwrap_or_else(|| panic!("sample scenario {preferred} is missing"))
}

fn remove_path(v: &mut Value, path: &str) {
    let segs: Vec<&str> = path.split('.').collect();
    remove_rec(v, &segs);
}

fn remove_rec(v: &mut Value, segs: &[&str]) {
    let Some((seg, rest)) = segs.split_first() else {
        return;
    };
    let (key, many) = match seg.strip_suffix("[]") {
        Some(k) => (k, true),
        None => (*seg, false),
    };
    if rest.is_empty() {
        if let Some(o) = v.as_object_mut() {
            o.remove(key);
        }
        return;
    }
    let children: Vec<&mut Value> = if key == "*" {
        match v.as_object_mut() {
            Some(o) => o.values_mut().collect(),
            None => return,
        }
    } else {
        match v.get_mut(key) {
            Some(x) => vec![x],
            None => return,
        }
    };
    for c in children {
        if many {
            if let Some(a) = c.as_array_mut() {
                for e in a {
                    remove_rec(e, rest);
                }
            }
        } else {
            remove_rec(c, rest);
        }
    }
}

#[test]
fn every_scenario_file_passes_the_structure_check() {
    let all = scenarios();
    assert!(
        !all.is_empty(),
        "no scenario files in {}",
        fixtures_dir().display()
    );
    let mut problems = Vec::new();
    for (name, v) in &all {
        problems.extend(check_scenario(v, name));
    }
    assert!(
        problems.is_empty(),
        "{} problems:\n{}",
        problems.len(),
        problems.join("\n")
    );
}

#[test]
fn every_scenario_has_an_expected_event_sequence_or_state() {
    for (name, v) in scenarios() {
        let ok = match v["kind"].as_str().unwrap_or("") {
            "watch-cycles" => {
                v["expected_state_view"].is_object() && !arr(&v, "expected_events").is_empty()
            }
            "vectors" => !arr(&v, "cases").is_empty(),
            _ => !arr(&v, "expected").is_empty(),
        };
        assert!(ok, "{name}: neither expected events, state nor cases");
    }
}

#[test]
fn the_index_lists_every_file_with_its_name_and_kind() {
    let all = scenarios();
    let idx: Value = serde_json::from_str(
        &fs::read_to_string(fixtures_dir().join("index.json")).expect("index.json is missing"),
    )
    .unwrap();
    assert_eq!(idx["format_version"], 1);
    let list = arr(&idx, "scenarios");
    assert_eq!(
        idx["count"].as_u64(),
        Some(all.len() as u64),
        "index count differs from the files"
    );
    assert_eq!(list.len(), all.len());
    let mut names = BTreeSet::new();
    for ((file, v), entry) in all.iter().zip(list) {
        assert_eq!(
            entry["file"].as_str(),
            Some(file.as_str()),
            "index order or file differs"
        );
        assert_eq!(entry["name"], v["name"]);
        assert_eq!(entry["kind"], v["kind"]);
        assert!(
            names.insert(v["name"].as_str().unwrap().to_string()),
            "{file}: the name repeats"
        );
    }
    let mut by_kind = std::collections::BTreeMap::new();
    for (_, v) in &all {
        *by_kind
            .entry(v["kind"].as_str().unwrap().to_string())
            .or_insert(0u64) += 1;
    }
    for (k, n) in by_kind {
        assert_eq!(idx["by_kind"][&k].as_u64(), Some(n), "by_kind of {k}");
    }
}

#[test]
fn format_md_names_every_field_the_check_requires() {
    let doc = format_md();
    let mut fields = BTreeSet::new();
    for p in COMMON
        .iter()
        .copied()
        .chain(KINDS.iter().flat_map(|k| required(k)))
    {
        let (_, key) = split_last(p);
        if key != "*" {
            fields.insert(key.to_string());
        }
    }
    let mut missing = Vec::new();
    for f in fields
        .iter()
        .map(|s| s.as_str())
        .chain(KINDS)
        .chain(FUNCTIONS)
    {
        if !doc.contains(&format!("`{f}`")) {
            missing.push(f.to_string());
        }
    }
    assert!(missing.is_empty(), "FORMAT.md does not name: {missing:?}");
}

#[test]
fn every_function_of_a_vector_scenario_is_known_and_every_known_function_is_used() {
    let used: BTreeSet<String> = scenarios()
        .iter()
        .filter(|(_, v)| v["kind"] == "vectors")
        .filter_map(|(_, v)| v["function"].as_str().map(String::from))
        .collect();
    for f in FUNCTIONS {
        assert!(used.contains(f), "no vector scenario uses `{f}`");
    }
    assert_eq!(used.len(), FUNCTIONS.len(), "unknown functions: {used:?}");
}

#[test]
fn the_check_rejects_every_missing_required_field() {
    // counter-proof: removing any one required field from a good scenario of each kind makes the check fail
    for kind in KINDS {
        let (name, good) = sample(kind);
        assert!(
            check_scenario(&good, &name).is_empty(),
            "{name} must be good: {:?}",
            check_scenario(&good, &name)
        );
        let mut paths: Vec<&str> = COMMON.to_vec();
        paths.extend(required(kind));
        let mut tried = 0;
        for path in paths {
            if select(&good, path).is_empty() && !path.contains('[') && !path.contains('*') {
                panic!("{name}: the required path `{path}` does not exist in the sample");
            }
            if select(&good, path).is_empty() {
                continue; // an element-level path of an empty list in this sample; other scenarios exercise it
            }
            let mut bad = good.clone();
            remove_path(&mut bad, path);
            assert!(
                !check_scenario(&bad, &name).is_empty(),
                "{name}: removing `{path}` was not noticed"
            );
            tried += 1;
        }
        assert!(tried >= 10, "{kind}: only {tried} paths tried");
    }
}

#[test]
fn the_check_rejects_broken_order_names_and_counts() {
    let (name, good) = sample("watch-cycles");
    let mut v = good.clone();
    v["cycles"][0]["step"] = Value::from(7);
    assert!(!check_scenario(&v, &name).is_empty(), "a wrong step number");
    let mut v = good.clone();
    v["name"] = Value::from("other");
    assert!(
        !check_scenario(&v, &name).is_empty(),
        "a name that differs from the file"
    );
    let mut v = good.clone();
    v["expected_journal_ordinals"] = Value::from(vec![2u64, 1]);
    assert!(
        !check_scenario(&v, &name).is_empty(),
        "a journal out of order"
    );
    let mut v = good.clone();
    v["expected_events"].as_array_mut().unwrap().pop();
    assert!(
        !check_scenario(&v, &name).is_empty(),
        "fewer expectations than cycles"
    );
    let mut v = good.clone();
    v["extra"] = Value::from(1);
    assert!(
        !check_scenario(&v, &name).is_empty(),
        "an undocumented top-level field"
    );
    let mut v = good;
    v["cycles"][1]["time"] = Value::from("2000-01-01T00:00:00Z");
    assert!(
        !check_scenario(&v, &name).is_empty(),
        "a clock that runs backwards"
    );
}

#[test]
fn the_readme_names_the_number_of_scenarios() {
    let n = scenarios().len();
    let readme =
        fs::read_to_string(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("README.md")).unwrap();
    assert!(
        readme.contains(&format!("holds {n} scenarios")),
        "README.md must say `holds {n} scenarios` in the section `Watch scenarios (port corpus)`"
    );
}

// ---- the export driver ----

use std::path::Path;
use std::process::Command;

fn driver() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/oracle/watch_scenarios.sh")
}

/// Runs the driver; returns (exit code, stderr).
fn run_driver(args: &[&str], env: &[(&str, &str)]) -> (i32, String) {
    let mut cmd = Command::new("bash");
    cmd.arg(driver())
        .args(args)
        .env_remove("TMPDIR")
        .env_remove("GO")
        .env_remove("WATCH_SCENARIOS_KEEP")
        .env_remove("WATCH_SCENARIOS_WRITE_FIXTURES");
    for (k, v) in env {
        cmd.env(k, v);
    }
    let out = cmd.output().expect("bash");
    (
        out.status.code().unwrap_or(-1),
        String::from_utf8_lossy(&out.stderr).to_string(),
    )
}

/// A directory that looks like the Go module for the guards of the driver (nothing of it is ever compiled).
fn stub_module(root: &Path) -> PathBuf {
    let m = root.join("gosrc");
    fs::create_dir_all(m.join("internal/watch")).unwrap();
    fs::write(
        m.join("go.mod"),
        "module github.com/wstein/sourcesteward\n\ngo 1.27.2\n",
    )
    .unwrap();
    m
}

fn git_init(dir: &Path) {
    fs::create_dir_all(dir).unwrap();
    let st = Command::new("git")
        .args(["init", "-q"])
        .arg(dir)
        .status()
        .expect("git");
    assert!(st.success());
}

#[test]
fn the_driver_refuses_missing_empty_relative_and_wrong_source_arguments() {
    let t = tempfile::tempdir().unwrap();
    let m = stub_module(t.path());
    let out = t.path().join("out");
    let (m_s, out_s) = (m.to_str().unwrap(), out.to_str().unwrap());
    for (args, why, want) in [
        (vec![], "no arguments", "usage"),
        (vec![m_s], "one argument", "usage"),
        (vec!["", out_s], "empty GO_SRC", "usage"),
        (vec![m_s, ""], "empty OUTDIR", "usage"),
        (
            vec!["gosrc", out_s],
            "relative GO_SRC",
            "GO_SRC must be an absolute path",
        ),
        (
            vec!["/tmp/../tmp", out_s],
            "GO_SRC with ..",
            "GO_SRC must not contain '..'",
        ),
        (
            vec![t.path().to_str().unwrap(), out_s],
            "a directory that is not the module",
            "not the sourcesteward Go module",
        ),
        (
            vec![m_s, "out"],
            "relative OUTDIR",
            "OUTDIR must be an absolute path",
        ),
        (
            vec![m_s, "/tmp/x/../y"],
            "OUTDIR with ..",
            "OUTDIR must not contain '..'",
        ),
        (vec![m_s, "/"], "OUTDIR /", "OUTDIR must not be /"),
    ] {
        let (code, err) = run_driver(&args, &[]);
        assert_eq!(code, 2, "{why}: {err}");
        assert!(err.contains(want), "{why}: wanted `{want}` in: {err}");
    }
    assert!(!out.exists(), "a refused run must create nothing");
}

#[test]
fn the_driver_refuses_a_directory_inside_a_git_tree_that_does_not_ignore_it() {
    let t = tempfile::tempdir().unwrap();
    let m = stub_module(t.path());
    let repo = t.path().join("repo");
    git_init(&repo);
    let (code, err) = run_driver(
        &[m.to_str().unwrap(), repo.join("fixtures").to_str().unwrap()],
        &[],
    );
    assert_eq!(code, 2, "{err}");
    assert!(
        err.contains("inside a Git tree that does not ignore it"),
        "{err}"
    );
    assert!(!repo.join("fixtures").exists());
    // an ignored directory passes the guard; the run then fails at the Go step (GO=/bin/false) and writes nothing
    fs::write(repo.join(".gitignore"), "out/\n").unwrap();
    let tmp = tempfile::tempdir().unwrap();
    let (code, err) = run_driver(
        &[m.to_str().unwrap(), repo.join("out").to_str().unwrap()],
        &[
            ("TMPDIR", tmp.path().to_str().unwrap()),
            ("GO", "/bin/false"),
        ],
    );
    assert_eq!(code, 2, "{err}");
    assert!(
        !err.contains("inside a Git tree"),
        "an ignored directory must pass the guard: {err}"
    );
    assert!(err.contains("the export failed"), "{err}");
    assert!(!repo.join("out").exists());
    assert_eq!(
        fs::read_dir(tmp.path()).unwrap().count(),
        0,
        "the work directory must be removed after a failure"
    );
}

#[test]
fn the_driver_replaces_only_the_json_of_an_earlier_export_and_refuses_other_directories() {
    let t = tempfile::tempdir().unwrap();
    let m = stub_module(t.path());
    let tmp = tempfile::tempdir().unwrap();
    let env = [
        ("TMPDIR", tmp.path().to_str().unwrap()),
        ("GO", "/bin/false"),
    ];
    let foreign = t.path().join("foreign");
    fs::create_dir_all(&foreign).unwrap();
    fs::write(foreign.join("notes.txt"), "keep").unwrap();
    let (code, err) = run_driver(&[m.to_str().unwrap(), foreign.to_str().unwrap()], &env);
    assert_eq!(code, 2, "{err}");
    assert!(
        err.contains("OUTDIR is not empty and has no index.json of an earlier export"),
        "{err}"
    );
    assert_eq!(
        fs::read_to_string(foreign.join("notes.txt")).unwrap(),
        "keep"
    );
    // a directory of an earlier export is accepted; nothing in it is deleted before the new export succeeded
    let earlier = t.path().join("earlier");
    fs::create_dir_all(&earlier).unwrap();
    fs::write(earlier.join("index.json"), "{}").unwrap();
    fs::write(earlier.join("old.json"), "{}").unwrap();
    let (code, err) = run_driver(&[m.to_str().unwrap(), earlier.to_str().unwrap()], &env);
    assert_eq!(code, 2, "{err}");
    assert!(err.contains("the export failed"), "{err}");
    assert!(earlier.join("old.json").exists() && earlier.join("index.json").exists());
}

#[test]
fn the_driver_refuses_an_empty_relative_or_tracked_tmpdir() {
    let t = tempfile::tempdir().unwrap();
    let m = stub_module(t.path());
    let out = t.path().join("out");
    let (code, err) = run_driver(
        &[m.to_str().unwrap(), out.to_str().unwrap()],
        &[("TMPDIR", "")],
    );
    assert_eq!(code, 2, "{err}");
    assert!(err.contains("TMPDIR is set but empty"), "{err}");
    let (code, err) = run_driver(
        &[m.to_str().unwrap(), out.to_str().unwrap()],
        &[("TMPDIR", "tmp")],
    );
    assert_eq!(code, 2, "{err}");
    assert!(err.contains("TMPDIR must be an absolute path"), "{err}");
    let repo = t.path().join("repo");
    git_init(&repo);
    let (code, err) = run_driver(
        &[m.to_str().unwrap(), out.to_str().unwrap()],
        &[("TMPDIR", repo.to_str().unwrap())],
    );
    assert_eq!(code, 2, "{err}");
    assert!(
        err.contains("TMPDIR is inside a Git tree that does not ignore it"),
        "{err}"
    );
    assert!(!out.exists());
}

#[test]
fn the_driver_only_writes_the_tracked_fixtures_directory_with_the_explicit_variable() {
    let t = tempfile::tempdir().unwrap();
    let m = stub_module(t.path());
    // without the variable the tracked directory of this crate is refused when a Git tree surrounds it; the variable alone is not enough for another path
    let other = t.path().join("repo2");
    git_init(&other);
    let (code, err) = run_driver(
        &[m.to_str().unwrap(), other.join("x").to_str().unwrap()],
        &[("WATCH_SCENARIOS_WRITE_FIXTURES", "1")],
    );
    assert_eq!(code, 2, "{err}");
    assert!(
        err.contains("inside a Git tree that does not ignore it"),
        "{err}"
    );
}
