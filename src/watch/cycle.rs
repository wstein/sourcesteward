//! One poll cycle (W3) on top of the W1 core. A successful first read of a repository is a silent
//! baseline. Later reads report new and closed PRs, head and readiness changes, and the observation's checks,
//! mergeability, and class-1 restack. A transient or fatal read leaves the accepted list in place
//! and records `watch-error`; the next success records `watch-recovered`.

use super::event::{
    event_id, format_utc, key_checks, key_error, key_generation, key_head, key_mergeable,
    key_merged, key_ready, key_recovered_error, key_restack, key_timeout, ChecksGreen, ChecksRed,
    Closed, Details, Event, HeadChanged, MergeableChanged, Merged, NewPr, PendingTimeout,
    ReadyStateChanged, RestackNeeded, WatchError, WatchRecovered, REASON_CHILD_OF_MERGED,
};
use super::reader::{OpenPr, ReadFailure};
use super::state::{Class, MergeCandidate, PrState, State};
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
pub fn advance_seen(
    state: &mut State,
    seen: &[Seen],
    now_secs: i64,
    pending_timeout_secs: u64,
) -> Vec<Event> {
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
                let ev = error_event(state, &item.repo, &now, &failure);
                state.apply(&ev);
                events.push(ev);
                continue;
            }
            Ok(obs) => obs,
        };
        if state.health_mut(&item.repo).error_open {
            let episode = state.health_mut(&item.repo).episode;
            let after = state.health_mut(&item.repo).failures;
            let ev = recovered_event(&item.repo, &now, episode, after);
            state.apply(&ev);
            events.push(ev);
        }
        let baselined = state
            .repo
            .get(&item.repo)
            .map(|rs| rs.baselined)
            .unwrap_or(false);
        if !baselined {
            install_observed(state, &item.repo, obs, &now);
        } else {
            let structural = events_from_observation(state, &item.repo, obs, &now);
            for ev in &structural {
                state.apply(ev);
            }
            events.extend(structural);
            // Candidates are bookkeeping, not events. They are stored after the structural
            // events have been applied, so a head change has already reset the old class.
            let volatile = note_volatile(state, &item.repo, obs, &now);
            for ev in &volatile {
                state.apply(ev);
            }
            events.extend(volatile);
            let timed = note_timeout(state, &item.repo, obs, &now, now_secs, pending_timeout_secs);
            for ev in &timed {
                state.apply(ev);
            }
            events.extend(timed);
            if let Some(rs) = state.repo.get_mut(&item.repo) {
                rs.last_ok = now.clone();
            }
        }
        remember_cond(state, &item.repo, obs.cond.as_ref());
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
        if stored.class == Class::Pending {
            stored.pending_since = now.to_string();
            if let Some(checks) = &pr.checks {
                stored.pending_names = pending_names(checks);
            }
        }
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

/// A class or a mergeability value is announced only on the second consecutive sighting.
/// The first sighting is stored on the PR. A null mergeability keeps the accepted value.
fn note_volatile(state: &mut State, repo: &str, obs: &RepoObservation, now: &str) -> Vec<Event> {
    let mut out = Vec::new();
    for pr in &obs.prs {
        let Some(stored) = state
            .repo
            .get_mut(repo)
            .and_then(|rs| rs.prs.get_mut(&pr.number))
        else {
            continue;
        };
        out.extend(note_mergeable(stored, repo, pr, now));
        out.extend(note_checks(stored, repo, pr, now));
    }
    out
}

fn note_mergeable(stored: &mut PrState, repo: &str, pr: &PrObs, now: &str) -> Option<Event> {
    let detail = pr.detail.as_ref()?;
    stored.detail_due = false;
    let Some(mergeable) = detail.mergeable else {
        if stored.mergeable_candidate.is_some() {
            stored.detail_due = true;
        }
        return None;
    };
    let behind = detail.state == "behind";
    if stored.mergeable.is_none() {
        stored.mergeable = Some(mergeable);
        stored.mergeable_state = detail.state.clone();
        stored.conflicting = detail.state == "dirty";
        stored.behind = behind;
        stored.mergeable_candidate = None;
        return None;
    }
    if stored.mergeable == Some(mergeable) && stored.behind == behind {
        stored.mergeable_state = detail.state.clone();
        stored.conflicting = detail.state == "dirty";
        stored.mergeable_candidate = None;
        return None;
    }
    let same_candidate = stored
        .mergeable_candidate
        .as_ref()
        .is_some_and(|c| c.mergeable == mergeable && c.behind == behind);
    if !same_candidate {
        stored.mergeable_candidate = Some(MergeCandidate {
            mergeable,
            behind,
            state: detail.state.clone(),
        });
        stored.detail_due = true;
        return None;
    }
    let epoch = stored.epochs.mergeable.saturating_add(1);
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
            mergeable: Some(mergeable),
            state: detail.state.clone(),
            conflicting: detail.state == "dirty",
            behind,
            base: pr.base.clone(),
        }),
    ))
}

fn note_checks(stored: &mut PrState, repo: &str, pr: &PrObs, now: &str) -> Option<Event> {
    let checks = pr.checks.as_ref()?;
    let observed = class_of(Some(checks));
    if observed == stored.class {
        stored.class_candidate = None;
        if observed == Class::Pending {
            stored.pending_names = pending_names(checks);
        }
        return None;
    }
    if stored.class_candidate.as_ref() != Some(&observed) {
        stored.class_candidate = Some(observed);
        return None;
    }
    let epoch = stored.epochs.checks.saturating_add(1);
    match observed {
        Class::Green => Some(Event::new(
            event_id(
                Some(repo),
                Some(pr.number),
                super::event::Kind::ChecksGreen,
                &key_checks(&pr.head, epoch),
            ),
            now.to_string(),
            Some(repo.to_string()),
            Some(pr.number),
            Details::ChecksGreen(ChecksGreen {
                head: pr.head.clone(),
                draft: pr.draft,
                base: pr.base.clone(),
                total: (checks.runs.len() + checks.statuses.len()) as u64,
            }),
        )),
        Class::Red => {
            let (failing_total, failing) = failing_names(checks);
            Some(Event::new(
                event_id(
                    Some(repo),
                    Some(pr.number),
                    super::event::Kind::ChecksRed,
                    &key_checks(&pr.head, epoch),
                ),
                now.to_string(),
                Some(repo.to_string()),
                Some(pr.number),
                Details::ChecksRed(ChecksRed {
                    head: pr.head.clone(),
                    failing_total,
                    failing,
                    shape: String::new(),
                    base: pr.base.clone(),
                    draft: pr.draft,
                }),
            ))
        }
        Class::Pending | Class::NoChecks => {
            stored.accept_class(observed, epoch, now);
            if observed == Class::Pending {
                stored.pending_names = pending_names(checks);
            }
            None
        }
        Class::Unobserved => None,
    }
}

fn note_timeout(
    state: &mut State,
    repo: &str,
    obs: &RepoObservation,
    now: &str,
    now_secs: i64,
    limit: u64,
) -> Vec<Event> {
    if limit == 0 {
        return Vec::new();
    }
    let mut out = Vec::new();
    for pr in &obs.prs {
        let Some(stored) = state
            .repo
            .get_mut(repo)
            .and_then(|rs| rs.prs.get_mut(&pr.number))
        else {
            continue;
        };
        if stored.class != Class::Pending || stored.timeout_fired || stored.pending_since.is_empty()
        {
            continue;
        }
        let since = crate::github::parse_utc(&stored.pending_since).unwrap_or(0);
        if now_secs.saturating_sub(since) <= i64::try_from(limit).unwrap_or(i64::MAX) {
            continue;
        }
        out.push(Event::new(
            event_id(
                Some(repo),
                Some(pr.number),
                super::event::Kind::ChecksPendingTimeout,
                &key_timeout(&stored.head, stored.epochs.checks),
            ),
            now.to_string(),
            Some(repo.to_string()),
            Some(pr.number),
            Details::ChecksPendingTimeout(PendingTimeout {
                head: stored.head.clone(),
                pending: stored.pending_names.clone(),
                since: stored.pending_since.clone(),
            }),
        ));
    }
    out
}

fn top_names(mut names: Vec<String>) -> (u64, Vec<String>) {
    let total = names.len() as u64;
    names.sort();
    names.truncate(20);
    (total, names)
}

fn failing_names(checks: &ChecksObs) -> (u64, Vec<String>) {
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
    top_names(names)
}

fn pending_names(checks: &ChecksObs) -> Vec<String> {
    let mut names = Vec::new();
    for run in &checks.runs {
        if run.status != "completed" {
            names.push(run.name.clone());
        }
    }
    for status in &checks.statuses {
        if status.state == "pending" {
            names.push(status.context.clone());
        }
    }
    top_names(names).1
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
            Some(prev) => {
                if prev.head != pr.head {
                    events.push(head_event(repo, prev, pr, now));
                }
                if prev.draft != pr.draft {
                    events.push(ready_event(repo, prev, pr, now));
                }
            }
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

fn ready_event(repo: &str, prev: &PrState, pr: &OpenPr, now: &str) -> Event {
    let epoch = prev.epochs.ready.saturating_add(1);
    let label = |draft| if draft { "draft" } else { "ready" }.to_string();
    Event::new(
        event_id(
            Some(repo),
            Some(pr.number),
            super::event::Kind::ReadyStateChanged,
            &key_ready(epoch),
        ),
        now.to_string(),
        Some(repo.to_string()),
        Some(pr.number),
        Details::ReadyStateChanged(ReadyStateChanged {
            from: label(prev.draft),
            to: label(pr.draft),
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
