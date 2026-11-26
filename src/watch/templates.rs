//! Event-specific template output. The watch journal remains the source of structured events.
use std::collections::BTreeMap;
use std::path::Path;

use serde_json::{Value, json};
use tinytemplate::TinyTemplate;

use super::event::{Details, Event, Kind};
use super::watch_toml::parse_templates_toml;
use crate::github::reads::escape_untrusted;

pub(crate) fn merge(
    mut configured: BTreeMap<String, String>,
    file: Option<&Path>,
    inline: &[String],
) -> Result<BTreeMap<String, String>, String> {
    if let Some(path) = file {
        let text = std::fs::read_to_string(path)
            .map_err(|e| format!("cannot read template file {}: {e}", path.display()))?;
        let entries = parse_templates_toml(&text)
            .map_err(|e| format!("template file {}: {e}", path.display()))?;
        configured.extend(entries);
    }
    let mut overrides = BTreeMap::new();
    for entry in inline {
        let (kind, text) = entry
            .split_once('=')
            .ok_or("--template-string requires KIND=TEXT")?;
        if overrides.insert(kind.to_owned(), text.to_owned()).is_some() {
            return Err(format!("duplicate inline template for {kind:?}"));
        }
    }
    configured.extend(overrides);
    Ok(configured)
}

pub struct Templates<'a> {
    engine: TinyTemplate<'a>,
    kinds: Vec<Kind>,
}

impl<'a> Templates<'a> {
    pub fn compile(sources: &'a BTreeMap<String, String>) -> Result<Self, String> {
        let mut engine = TinyTemplate::new();
        engine.set_default_formatter(&tinytemplate::format_unescaped);
        let mut kinds = Vec::new();
        for (name, text) in sources {
            let kind =
                Kind::parse(name).ok_or_else(|| format!("unknown template event {name:?}"))?;
            // TinyTemplate has no recursion limit for template calls.
            if text.match_indices("{{").any(|(offset, _)| {
                text[offset + 2..]
                    .trim_start_matches(|c: char| c.is_whitespace() || c == '-')
                    .split_whitespace()
                    .next()
                    == Some("call")
            }) {
                return Err(format!("template {name:?}: template calls are unsupported"));
            }
            engine
                .add_template(kind.as_str(), text)
                .map_err(|e| format!("template {name:?}: {e}"))?;
            kinds.push(kind);
        }
        let templates = Self { engine, kinds };
        for &kind in &templates.kinds {
            let details = sample_details(kind)?;
            let mut sample = Event::new(
                "example".into(),
                "2026-01-01T00:00:00Z".into(),
                Some("owner/repo".into()),
                Some(1),
                details,
            );
            let mut populated = context(&sample);
            populate_sample(&mut populated);
            match kind {
                Kind::ChecksRed => populated["details"]["failing"] = json!(["ci"]),
                Kind::ChecksPendingTimeout => populated["details"]["pending"] = json!(["ci"]),
                Kind::ReposChanged => {
                    populated["details"]["added"] = json!(["owner/repo"]);
                    populated["details"]["removed"] = json!(["owner/other"]);
                    populated["details"]["skipped"] =
                        json!([{"path":"other","reason":"no-origin"}]);
                }
                Kind::RestackNeeded => populated["details"]["argv"] = json!(["git"]),
                _ => {}
            }
            for value in [&context(&sample), &populated] {
                templates.render_context(kind, value)?;
            }
            sample.repo = None;
            sample.pr = None;
            templates.render_context(kind, &context(&sample))?;
        }
        Ok(templates)
    }

    pub fn contains(&self, kind: Kind) -> bool {
        self.kinds.contains(&kind)
    }

    fn render_context(&self, kind: Kind, value: &Value) -> Result<String, String> {
        self.engine
            .render(kind.as_str(), value)
            .map_err(|e| format!("template {:?}: {e}", kind.as_str()))
    }

    pub fn render(&self, event: &Event) -> Result<Option<String>, String> {
        let kind = event.kind();
        self.contains(kind)
            .then(|| self.render_context(kind, &context(event)))
            .transpose()
    }
}

fn sample_details(kind: Kind) -> Result<Details, String> {
    let fields = match kind {
        Kind::BranchHeadChanged => json!({"branch": "main", "old": "", "new": "", "epoch": 0}),
        Kind::WorkflowCompleted => json!({
            "run_id": 0, "attempt": 0, "name": "", "conclusion": "", "branch": "",
            "head": "", "trigger": "", "url": ""
        }),
        Kind::IssueChanged => json!({
            "change": "opened", "epoch": 0, "issue": {
                "number": 0, "title": "", "url": "", "updated_at": "",
                "comment_count": 0, "latest_comment": "", "labels": [""], "assignees": [""]
            }
        }),
        Kind::ReviewStateChanged
        | Kind::ReviewCommentAdded
        | Kind::DiscussionChanged
        | Kind::WikiChanged => {
            let activity = match kind {
                Kind::ReviewStateChanged => "review",
                Kind::ReviewCommentAdded => "review-comment",
                Kind::DiscussionChanged => "discussion",
                Kind::WikiChanged => "wiki",
                _ => unreachable!(),
            };
            json!({
                "record": {"kind": activity, "id": "", "version": "", "number": 0, "url": "", "summary": ""},
                "epoch": 0
            })
        }
        _ => json!({}),
    };
    Details::decode(kind, &fields.to_string())
        .map_err(|error| format!("sample for {}: {error}", kind.as_str()))
}

fn populate_sample(value: &mut Value) {
    match value {
        Value::Object(fields) => fields.values_mut().for_each(populate_sample),
        Value::Bool(v) => *v = true,
        Value::Number(_) => *value = json!(1),
        Value::String(_) | Value::Null => *value = json!("example"),
        Value::Array(_) => {}
    }
}

fn context(event: &Event) -> Value {
    let mut value = serde_json::to_value(event).expect("watch events serialize to JSON");
    if let Details::HeadChanged(details) = &event.details {
        value["old_head"] = json!(details.old);
        value["head"] = json!(details.new);
    }
    escape_strings(&mut value);
    value
}

fn escape_strings(value: &mut Value) {
    match value {
        Value::Object(fields) => fields.values_mut().for_each(escape_strings),
        Value::Array(items) => items.iter_mut().for_each(escape_strings),
        Value::String(text) => *text = escape_untrusted(text, super::event::MAX_DETAIL_TEXT).0,
        _ => {}
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::watch::event::{HeadChanged, NewPr};

    #[test]
    fn layered_overrides_apply_per_kind_and_keep_full_shas() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("templates.toml");
        std::fs::write(
            &path,
            "[templates]\nhead-changed = 'file'\nchecks-red = 'file {details.head}'\n",
        )
        .unwrap();
        let config = super::super::watch_toml::parse_watch_toml(
            "schema_version = 1\n[templates]\nhead-changed = 'config'\nmerged = 'config {pr}'\n",
        )
        .unwrap();
        let map = merge(
            config.templates,
            Some(&path),
            &["head-changed=inline {repo}#{pr} {old_head} -> {details.new}={head}".into()],
        )
        .unwrap();
        assert_eq!(map["merged"], "config {pr}");
        assert_eq!(map["checks-red"], "file {details.head}");
        let templates = Templates::compile(&map).unwrap();
        let event = Event::new(
            "id".into(),
            "time".into(),
            Some("o/r".into()),
            Some(1),
            Details::HeadChanged(HeadChanged {
                old: "a".repeat(40),
                new: "b".repeat(40),
            }),
        );
        assert_eq!(
            templates.render(&event).unwrap().unwrap(),
            format!(
                "inline o/r#1 {} -> {}={}",
                "a".repeat(40),
                "b".repeat(40),
                "b".repeat(40)
            )
        );
        let other = Event::new(
            "id".into(),
            "time".into(),
            None,
            None,
            Details::NewPr(NewPr::default()),
        );
        assert_eq!(templates.render(&other).unwrap(), None);
    }

    #[test]
    fn accepts_every_current_event_kind() {
        let sources = Kind::ALL
            .into_iter()
            .map(|kind| (kind.as_str().into(), "{kind}".into()))
            .collect();
        Templates::compile(&sources).unwrap();
    }

    #[test]
    fn rejects_duplicate_and_unknown_template_entries() {
        assert!(parse_templates_toml("[templates]\nmerged = 'a'\nmerged = 'b'").is_err());
        assert!(
            merge(
                BTreeMap::new(),
                None,
                &["merged=a".into(), "merged=b".into()]
            )
            .is_err()
        );
        for (event, text) in [
            ("unknown", "text"),
            ("merged", "{details.old}"),
            ("merged", "{unterminated"),
            ("merged", "{{ call merged with @root }}"),
        ] {
            assert!(
                Templates::compile(&[(event.into(), text.into())].into()).is_err(),
                "{event}: {text}"
            );
        }
    }
}
