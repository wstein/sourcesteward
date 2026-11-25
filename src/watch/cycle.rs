//! One poll cycle (W3) on top of the W1 core. A successful first read of a repository is a silent
//! baseline. Later reads emit `new-pr`, `head-changed`, and `closed`. A transient or fatal read
//! leaves the accepted list in place and records `watch-error`; the next success records
//! `watch-recovered`. Checks, mergeability, and restack stay for a later cycle: this one compares
//! the open list only.

use super::event::{
    event_id, format_utc, key_error, key_generation, key_head, key_recovered_error, Closed,
    Details, Event, HeadChanged, NewPr, WatchError, WatchRecovered,
};
use super::reader::{OpenPr, ReadFailure};
use super::state::{PrState, State};

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
