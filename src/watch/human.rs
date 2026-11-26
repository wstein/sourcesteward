//! The Go watcher's one-line event format. The journal uses the JSON event format.
use super::event::{Details, Event};
use crate::github::reads::escape_untrusted;

fn esc(value: &str) -> String {
    let (mut text, truncated) = escape_untrusted(value, 200);
    if truncated {
        text.push('…');
    }
    text
}

fn short(value: &str) -> String {
    esc(&value.chars().take(7).collect::<String>())
}

fn names(values: &[String]) -> String {
    values
        .iter()
        .map(|value| esc(value))
        .collect::<Vec<_>>()
        .join(", ")
}

fn simple_branch(branch: &str) -> bool {
    !branch.is_empty()
        && branch.len() <= 200
        && !branch.starts_with('-')
        && branch
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'_' | b'/' | b'-'))
}

fn full_sha(sha: &str) -> bool {
    matches!(sha.len(), 40 | 64)
        && sha
            .bytes()
            .all(|b| b.is_ascii_digit() || matches!(b, b'a'..=b'f'))
}

pub(super) fn render(event: &Event) -> String {
    let time = event
        .time
        .get(11..19)
        .filter(|_| event.time.ends_with('Z'))
        .unwrap_or(&event.time);
    let repo = event.repo.as_deref().map(esc).unwrap_or_else(|| "-".into());
    let scope = match (event.repo.is_some(), event.pr) {
        (true, Some(pr)) => format!("{repo}#{pr}"),
        _ => repo,
    };
    let summary = match &event.details {
        Details::NewPr(d) => format!(
            "{} by {} into {}{}",
            esc(&d.title),
            esc(&d.author),
            esc(&d.base),
            if d.draft { " (draft)" } else { "" }
        ),
        Details::HeadChanged(d) => format!("{} -> {}", short(&d.old), short(&d.new)),
        Details::ChecksGreen(d) => format!("{} checks on {}", d.total, short(&d.head)),
        Details::ChecksRed(d) => {
            let mut text = names(&d.failing);
            let more = d.failing_total.saturating_sub(d.failing.len() as u64);
            if more > 0 {
                text.push_str(&format!(" and {more} more"));
            }
            format!("{text} ({})", esc(&d.shape))
        }
        Details::ChecksPendingTimeout(d) => {
            format!("{} pending since {}", names(&d.pending), esc(&d.since))
        }
        Details::MergeableChanged(d) => format!(
            "mergeable={} state={}{}",
            match d.mergeable {
                Some(true) => "true",
                Some(false) => "false",
                None => "unknown",
            },
            esc(&d.state),
            if d.conflicting {
                " (conflicting)"
            } else if d.behind {
                " (behind)"
            } else {
                ""
            }
        ),
        Details::ReadyStateChanged(d) => format!("{} -> {}", esc(&d.from), esc(&d.to)),
        Details::BaseChanged(d) => format!("{} -> {}", esc(&d.old), esc(&d.new)),
        Details::Merged(d) => format!(
            "into {} as {} ({})",
            esc(&d.base),
            d.merge_commit
                .as_deref()
                .map(short)
                .unwrap_or_else(|| "unknown".into()),
            esc(&d.method_hint)
        ),
        Details::Closed(d) => format!("without merge (base {})", esc(&d.base)),
        Details::RestackNeeded(d) => {
            let command = match (d.argv.as_ref(), d.new_parent.as_deref()) {
                (Some(_), Some(parent))
                    if full_sha(parent) && full_sha(&d.old_parent) && simple_branch(&d.branch) =>
                {
                    format!(
                        "git rebase --onto {} {} {}",
                        short(parent),
                        short(&d.old_parent),
                        esc(&d.branch)
                    )
                }
                _ => format!("no command ({})", esc(&d.branch)),
            };
            format!("after #{}: {command}", d.merged_pr)
        }
        Details::ReposChanged(d) => format!(
            "+{} -{} skipped {}",
            d.added.len(),
            d.removed.len(),
            d.skipped.len()
        ),
        Details::WatchError(d) => format!(
            "{}: {} (failure {}, retry in {}s)",
            esc(&d.code),
            esc(&d.message),
            d.failures,
            d.retry_in_seconds
        ),
        Details::WatchRecovered(d) => format!("after {} failures", d.after_failures),
        Details::ReviewStateChanged(d)
        | Details::ReviewCommentAdded(d)
        | Details::DiscussionChanged(d) => {
            format!(
                "#{} {} ({})",
                d.record.number,
                esc(&d.record.summary),
                esc(&d.record.url)
            )
        }
        Details::BranchHeadChanged(d) => {
            format!("{} {} -> {}", esc(&d.branch), short(&d.old), short(&d.new))
        }
        Details::WikiChanged(d) => format!("{} ({})", esc(&d.record.summary), esc(&d.record.url)),
        Details::WorkflowCompleted(d) => format!(
            "{} {} on {} ({})",
            esc(&d.name),
            esc(&d.conclusion),
            esc(&d.branch),
            esc(&d.url)
        ),
        Details::IssueChanged(d) => format!(
            "issue #{} {:?} {}",
            d.issue.number,
            d.change,
            esc(&d.issue.title)
        ),
        Details::WatchStalled(d) => format!("no success since {}", esc(&d.since)),
    };
    format!("{} {scope} {} {summary}", esc(time), event.kind().as_str())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::watch::event::{NewPr, RestackNeeded};

    #[test]
    fn go_restack_hint_keeps_the_command_on_one_line() {
        let mut event = Event::new(
            "id".into(),
            "2026-11-25T10:20:30Z".into(),
            Some("o/r".into()),
            Some(7),
            Details::RestackNeeded(RestackNeeded {
                merged_pr: 5,
                old_parent: "a".repeat(40),
                new_parent: Some("b".repeat(40)),
                branch: "topic".into(),
                argv: Some(vec![]),
                ..Default::default()
            }),
        );
        assert_eq!(
            render(&event),
            "10:20:30 o/r#7 restack-needed after #5: git rebase --onto bbbbbbb aaaaaaa topic"
        );
        if let Details::RestackNeeded(ref mut details) = event.details {
            details.branch = "topic;unexpected".into();
        }
        assert_eq!(
            render(&event),
            "10:20:30 o/r#7 restack-needed after #5: no command (topic;unexpected)"
        );
    }

    #[test]
    fn remote_text_is_escaped_and_bounded() {
        let event = Event::new(
            "id".into(),
            "2026-11-25T10:20:30Z".into(),
            Some("o/r".into()),
            Some(7),
            Details::NewPr(NewPr {
                title: format!("a\x1b\n\u{202e}{}", "é".repeat(300)),
                author: "alice".into(),
                base: "main".into(),
                draft: true,
                ..Default::default()
            }),
        );
        let line = render(&event);
        assert!(line.contains(r"a\u{1b}\n\u{202e}"), "{line}");
        assert!(line.ends_with("… by alice into main (draft)"), "{line}");
        assert_eq!(line.lines().count(), 1);
    }
}
