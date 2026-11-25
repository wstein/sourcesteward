//! One poll cycle (W3) on top of the W1 core. A successful first read of a repository is a silent
//! baseline. Later reads emit `new-pr`, `head-changed`, and `closed`, and the observation's checks,
//! mergeability, and class-1 restack. A transient or fatal read leaves the accepted list in place
//! and records `watch-error`; the next success records `watch-recovered`.

use super::event::{
    event_id, format_utc, key_checks, key_error, key_generation, key_head, key_mergeable,
    key_merged, key_recovered_error, key_restack, ChecksGreen, ChecksRed, Closed, Details, Event,
    HeadChanged, MergeableChanged, Merged, NewPr, RestackNeeded, WatchError, WatchRecovered,
    REASON_CHILD_OF_MERGED,
};
use super::reader::{OpenPr, ReadFailure};
use super::state::{Class, PrState, State};
use crate::watch_reader::cond_store::CondStore;
use crate::watch_reader::obs::{
    checks_class, ChecksObs, GoneObs, ObserveFailure, PrObs, RepoObservation, CLASS_GREEN,
    CLASS_NONE, CLASS_PENDING, CLASS_RED,
};

/// One repository seen through `watch_reader`. Each repository appears once.
pub struct Seen {
    pub repo: String,
    pub result: Result<RepoObservation, ObserveFailure>,
}

/// Turns observations into open-list events plus checks, merge, and restack events.
/// A successful read stores its validator store on the repository for the next start.
pub fn advance_seen(state: &mut State, seen: &[Seen], now_secs: i64) -> Vec<Event> {
    let now = format_utc(now_secs);
    let mut events = Vec::new();
    for item in seen {
        let obs = match &item.result {
            Err(ObserveFailure::Canceled) => continue,
            Err(ObserveFailure::Error(err)) => {
                let failure = if err.retry_in > 0
                    || matches!(
                        err.code,
                        crate::watch_reader::http::Code::RateLimited
                            | crate::watch_reader::http::Code::Network
                            | crate::watch_reader::http::Code::Http5xx
                    ) {
                    ReadFailure::Transient {
                        code: err.code.as_str().to_string(),
                        retry_in_seconds: err.retry_in.max(1),
                        retry_until: None,
                    }
                } else {
                    ReadFailure::Fatal {
                        code: err.code.as_str().to_string(),
                    }
                };
                events.push(error_event(state, &item.repo, &now, &failure));
                continue;
            }
            Ok(obs) => obs,
        };
        if state.health_mut(&item.repo).error_open {
            let episode = state.health_mut(&item.repo).episode;
            let after = state.health_mut(&item.repo).failures;
            events.push(recovered_event(&item.repo, &now, episode, after));
        }
        let baselined = state
            .repo
            .get(&item.repo)
            .map(|rs| rs.baselined)
            .unwrap_or(false);
        if !baselined {
            install_observed(state, &item.repo, obs, &now);
        } else {
            events.extend(events_from_observation(state, &item.repo, obs, &now));
            if let Some(rs) = state.repo.get_mut(&item.repo) {
                rs.last_ok = now.clone();
            }
        }
        remember_cond(state, &item.repo, obs.cond.as_ref());
    }
    for ev in &events {
        state.apply(ev);
    }
    events
}

fn remember_cond(state: &mut State, repo: &str, cond: Option<&CondStore>) {
    let Some(store) = cond else {
        return;
    };
    if let Some(rs) = state.repo.get_mut(repo) {
        rs.conditional = Some(store.to_json());
    }
}

fn install_observed(state: &mut State, repo: &str, obs: &RepoObservation, now: &str) {
    let rs = state
        .repo
        .entry(repo.to_string())
        .or_insert_with(|| super::state::RepoState::new(now));
    rs.prs.clear();
    for pr in &obs.prs {
        let mut stored = pr_state(&open_of(pr));
        stored.class = class_of(pr.checks.as_ref());
        if let Some(detail) = &pr.detail {
            stored.mergeable = detail.mergeable;
            stored.mergeable_state = detail.state.clone();
            stored.conflicting = detail.state == "dirty";
            stored.behind = detail.state == "behind";
        }
        rs.prs.insert(pr.number, stored);
    }
    if !obs.default_branch.is_empty() {
        rs.default_branch = obs.default_branch.clone();
    }
    rs.baselined = true;
    rs.last_ok = now.to_string();
}

fn events_from_observation(
    state: &State,
    repo: &str,
    obs: &RepoObservation,
    now: &str,
) -> Vec<Event> {
    let open: Vec<OpenPr> = obs.prs.iter().map(open_of).collect();
    let mut events = diff_events(state, repo, &open, now);
    let mut replaced = Vec::new();
    for ev in events.drain(..) {
        if ev.kind() != super::event::Kind::Closed {
            replaced.push(ev);
            continue;
        }
        let number = ev.pr.unwrap_or(0);
        match obs.gone.get(&number).filter(|g| g.merged) {
            Some(gone) => {
                replaced.push(merged_event(repo, number, gone, now));
                replaced.extend(restack_events(repo, number, gone, obs, now));
            }
            None => replaced.push(ev),
        }
    }
    for pr in &obs.prs {
        replaced.extend(check_events(state, repo, pr, now));
        if let Some(ev) = mergeable_event(state, repo, pr, now) {
            replaced.push(ev);
        }
    }
    replaced
}

fn open_of(pr: &PrObs) -> OpenPr {
    OpenPr {
        number: pr.number,
        title: pr.title.clone(),
        author: pr.author.clone(),
        draft: pr.draft,
        base: pr.base.clone(),
        head_ref: pr.head_ref.clone(),
        head: pr.head.clone(),
        fork: pr.fork,
    }
}

fn class_of(checks: Option<&ChecksObs>) -> Class {
    match checks.map(|c| checks_class(&c.runs, &c.statuses)) {
        Some(CLASS_GREEN) => Class::Green,
        Some(CLASS_RED) => Class::Red,
        Some(CLASS_PENDING) => Class::Pending,
        Some(CLASS_NONE) => Class::NoChecks,
        _ => Class::Unobserved,
    }
}

fn previous<'a>(state: &'a State, repo: &str, number: u64) -> Option<&'a PrState> {
    state.repo.get(repo)?.prs.get(&number)
}

fn check_events(state: &State, repo: &str, pr: &PrObs, now: &str) -> Vec<Event> {
    let Some(checks) = &pr.checks else {
        return Vec::new();
    };
    let observed = class_of(Some(checks));
    let prev = previous(state, repo, pr.number);
    let head_changed = prev.map(|p| p.head != pr.head).unwrap_or(true);
    let accepted = if head_changed {
        Class::Unobserved
    } else {
        prev.map(|p| p.class).unwrap_or(Class::Unobserved)
    };
    if observed == accepted
        || matches!(
            observed,
            Class::Unobserved | Class::NoChecks | Class::Pending
        )
    {
        return Vec::new();
    }
    let epoch = prev.map(|p| p.epochs.checks).unwrap_or(0).saturating_add(1);
    let id = event_id(
        Some(repo),
        Some(pr.number),
        if observed == Class::Green {
            super::event::Kind::ChecksGreen
        } else {
            super::event::Kind::ChecksRed
        },
        &key_checks(&pr.head, epoch),
    );
    let ev = if observed == Class::Green {
        Event::new(
            id,
            now.to_string(),
            Some(repo.to_string()),
            Some(pr.number),
            Details::ChecksGreen(ChecksGreen {
                head: pr.head.clone(),
                draft: pr.draft,
                base: pr.base.clone(),
                total: (checks.runs.len() + checks.statuses.len()) as u64,
            }),
        )
    } else {
        let failing = failing_names(checks);
        Event::new(
            id,
            now.to_string(),
            Some(repo.to_string()),
            Some(pr.number),
            Details::ChecksRed(ChecksRed {
                head: pr.head.clone(),
                failing_total: failing.len() as u64,
                failing,
                shape: String::new(),
                base: pr.base.clone(),
                draft: pr.draft,
            }),
        )
    };
    vec![ev]
}

fn failing_names(checks: &ChecksObs) -> Vec<String> {
    let mut names = Vec::new();
    for run in &checks.runs {
        if run.status == "completed"
            && matches!(
                run.conclusion.as_str(),
                "failure" | "cancelled" | "timed_out" | "action_required" | "startup_failure"
            )
        {
            names.push(run.name.clone());
        }
    }
    for status in &checks.statuses {
        if status.state == "failure" || status.state == "error" {
            names.push(status.context.clone());
        }
    }
    names
}

fn mergeable_event(state: &State, repo: &str, pr: &PrObs, now: &str) -> Option<Event> {
    let detail = pr.detail.as_ref()?;
    let prev = previous(state, repo, pr.number);
    let head_changed = prev.map(|p| p.head != pr.head).unwrap_or(true);
    let same = prev.is_some_and(|p| {
        !head_changed && p.mergeable == detail.mergeable && p.mergeable_state == detail.state
    });
    if same {
        return None;
    }
    let epoch = prev
        .map(|p| p.epochs.mergeable)
        .unwrap_or(0)
        .saturating_add(1);
    Some(Event::new(
        event_id(
            Some(repo),
            Some(pr.number),
            super::event::Kind::MergeableChanged,
            &key_mergeable(&pr.head, epoch),
        ),
        now.to_string(),
        Some(repo.to_string()),
        Some(pr.number),
        Details::MergeableChanged(MergeableChanged {
            mergeable: detail.mergeable,
            state: detail.state.clone(),
            conflicting: detail.state == "dirty",
            behind: detail.state == "behind",
            base: pr.base.clone(),
        }),
    ))
}

fn merged_event(repo: &str, number: u64, gone: &GoneObs, now: &str) -> Event {
    Event::new(
        event_id(
            Some(repo),
            Some(number),
            super::event::Kind::Merged,
            &key_merged(gone.merge_commit.as_deref()),
        ),
        now.to_string(),
        Some(repo.to_string()),
        Some(number),
        Details::Merged(Merged {
            merge_commit: gone.merge_commit.clone(),
            base: gone.base.clone(),
            head: gone.head.clone(),
            head_ref: gone.head_ref.clone(),
            merged_at: gone.merged_at.clone(),
            method_hint: gone.method_hint.as_str().to_string(),
        }),
    )
}

fn restack_events(
    repo: &str,
    merged: u64,
    gone: &GoneObs,
    obs: &RepoObservation,
    now: &str,
) -> Vec<Event> {
    obs.prs
        .iter()
        .filter(|pr| pr.base == gone.head_ref && pr.number != merged)
        .map(|child| {
            Event::new(
                event_id(
                    Some(repo),
                    Some(child.number),
                    super::event::Kind::RestackNeeded,
                    &key_restack(merged, gone.merge_commit.as_deref()),
                ),
                now.to_string(),
                Some(repo.to_string()),
                Some(child.number),
                Details::RestackNeeded(RestackNeeded {
                    merged_pr: merged,
                    base: gone.base.clone(),
                    reason: REASON_CHILD_OF_MERGED.to_string(),
                    old_parent: gone.head.clone(),
                    new_parent: gone.merge_commit.clone(),
                    new_parent_source: if gone.merge_commit.is_some() {
                        "merge-commit".into()
                    } else {
                        String::new()
                    },
                    branch: child.head_ref.clone(),
                    argv: restack_argv(gone, child),
                }),
            )
        })
        .collect()
}

/// D-20261119-03. Null when the merge commit is unknown or the child is a fork.
fn restack_argv(gone: &GoneObs, child: &PrObs) -> Option<Vec<String>> {
    let onto = gone.merge_commit.clone()?;
    if child.fork {
        return None;
    }
    Some(vec![
        "git".into(),
        "-c".into(),
        "rebase.updateRefs=false".into(),
        "-c".into(),
        "rerere.enabled=false".into(),
        "rebase".into(),
        "--onto".into(),
        onto,
        gone.head.clone(),
        child.head_ref.clone(),
    ])
}

/// What the reader returned for one repository. Each repository appears once.
pub struct Poll {
    pub repo: String,
    pub result: Result<Vec<OpenPr>, ReadFailure>,
}

/// Turns `polls` into events and applies each one to `state` before returning, which is the order
/// `Core::commit` expects. `now_secs` is the clock of this cycle.
pub fn advance(state: &mut State, polls: &[Poll], now_secs: i64) -> Vec<Event> {
    let now = format_utc(now_secs);
    let mut events = Vec::new();
    for poll in polls {
        if let Err(failure) = &poll.result {
            events.push(error_event(state, &poll.repo, &now, failure));
            continue;
        }
        let prs = poll.result.as_ref().expect("ok");
        if state.health_mut(&poll.repo).error_open {
            let episode = state.health_mut(&poll.repo).episode;
            let after = state.health_mut(&poll.repo).failures;
            events.push(recovered_event(&poll.repo, &now, episode, after));
        }
        let baselined = state
            .repo
            .get(&poll.repo)
            .map(|rs| rs.baselined)
            .unwrap_or(false);
        if !baselined {
            install_baseline(state, &poll.repo, prs, &now);
        } else {
            events.extend(diff_events(state, &poll.repo, prs, &now));
            if let Some(rs) = state.repo.get_mut(&poll.repo) {
                rs.last_ok = now.clone();
            }
        }
    }
    for ev in &events {
        state.apply(ev);
    }
    events
}

fn install_baseline(state: &mut State, repo: &str, prs: &[OpenPr], now: &str) {
    let rs = state
        .repo
        .entry(repo.to_string())
        .or_insert_with(|| super::state::RepoState::new(now));
    rs.prs.clear();
    for pr in prs {
        rs.prs.insert(pr.number, pr_state(pr));
    }
    rs.baselined = true;
    rs.last_ok = now.to_string();
}

fn pr_state(pr: &OpenPr) -> PrState {
    PrState {
        number: pr.number,
        title: pr.title.clone(),
        author: pr.author.clone(),
        draft: pr.draft,
        head: pr.head.clone(),
        head_ref: pr.head_ref.clone(),
        base: pr.base.clone(),
        fork: pr.fork,
        ..PrState::default()
    }
}

fn error_event(state: &State, repo: &str, now: &str, failure: &ReadFailure) -> Event {
    let health = state.health.get(repo);
    let failures = health.map(|h| h.failures).unwrap_or(0).saturating_add(1);
    let episode = health.map(|h| h.episode).unwrap_or(0).saturating_add(1);
    let retry = match failure {
        ReadFailure::Transient {
            retry_until: Some(until),
            ..
        } => {
            let at = crate::github::parse_utc(now).unwrap_or(0);
            u64::try_from(until.saturating_sub(at)).unwrap_or(0)
        }
        ReadFailure::Transient {
            retry_in_seconds, ..
        } => *retry_in_seconds,
        ReadFailure::Fatal { .. } => 0,
    };
    Event::new(
        event_id(
            Some(repo),
            None,
            super::event::Kind::WatchError,
            &key_error(failure.code(), episode, failures),
        ),
        now.to_string(),
        Some(repo.to_string()),
        None,
        Details::WatchError(WatchError {
            code: failure.code().to_string(),
            message: failure.code().to_string(),
            failures,
            retry_in_seconds: retry,
            repo: Some(repo.to_string()),
        }),
    )
}

fn recovered_event(repo: &str, now: &str, episode: u64, after_failures: u64) -> Event {
    Event::new(
        event_id(
            Some(repo),
            None,
            super::event::Kind::WatchRecovered,
            &key_recovered_error(episode),
        ),
        now.to_string(),
        Some(repo.to_string()),
        None,
        Details::WatchRecovered(WatchRecovered {
            after_failures,
            repo: Some(repo.to_string()),
        }),
    )
}

fn diff_events(state: &State, repo: &str, prs: &[OpenPr], now: &str) -> Vec<Event> {
    let Some(rs) = state.repo.get(repo) else {
        return Vec::new();
    };
    let mut events = Vec::new();
    for pr in prs {
        match rs.prs.get(&pr.number) {
            None => events.push(new_pr_event(rs, repo, pr, now)),
            Some(prev) if prev.head != pr.head => events.push(head_event(repo, prev, pr, now)),
            Some(_) => {}
        }
    }
    for prev in rs.prs.values() {
        if !prs.iter().any(|p| p.number == prev.number) {
            events.push(closed_event(repo, prev, now));
        }
    }
    events
}

fn new_pr_event(rs: &super::state::RepoState, repo: &str, pr: &OpenPr, now: &str) -> Event {
    let generation = rs
        .closed_record(pr.number)
        .map(|(_, c)| c.generation.saturating_add(1))
        .unwrap_or(0);
    Event::new(
        event_id(
            Some(repo),
            Some(pr.number),
            super::event::Kind::NewPr,
            &key_generation(generation),
        ),
        now.to_string(),
        Some(repo.to_string()),
        Some(pr.number),
        Details::NewPr(NewPr {
            title: pr.title.clone(),
            author: pr.author.clone(),
            draft: pr.draft,
            base: pr.base.clone(),
            head_ref: pr.head_ref.clone(),
            head: pr.head.clone(),
            fork: pr.fork,
        }),
    )
}

fn head_event(repo: &str, prev: &PrState, pr: &OpenPr, now: &str) -> Event {
    let epoch = prev.epochs.head.saturating_add(1);
    Event::new(
        event_id(
            Some(repo),
            Some(pr.number),
            super::event::Kind::HeadChanged,
            &key_head(&pr.head, epoch),
        ),
        now.to_string(),
        Some(repo.to_string()),
        Some(pr.number),
        Details::HeadChanged(HeadChanged {
            old: prev.head.clone(),
            new: pr.head.clone(),
        }),
    )
}

fn closed_event(repo: &str, prev: &PrState, now: &str) -> Event {
    Event::new(
        event_id(
            Some(repo),
            Some(prev.number),
            super::event::Kind::Closed,
            &key_generation(prev.generation),
        ),
        now.to_string(),
        Some(repo.to_string()),
        Some(prev.number),
        Details::Closed(Closed {
            head: prev.head.clone(),
            base: prev.base.clone(),
        }),
    )
}
