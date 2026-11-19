//! `State::apply`: the one place where an event changes the state. A live cycle calls it for every event it emits and the
//! replay after a crash calls it for every journal line past `applied_seq`, so both reach the same state by construction
//! (the "Replay effects" table of Spec 035).
//!
//! Two rules make it safe to apply an event more than once (the cycle applies it when it emits it, the commit applies it
//! again after the journal append, a replay applies it a third time): counters are raised to the value the id carries and
//! never lowered, and every other effect sets a value or removes an entry; none adds up.
use super::event::{id_counter, key_merged, Details, Event};
use super::state::{Class, ClosedPr, PrState, RepoState, RestackDue, State};

impl State {
    /// Applies the transition of `ev`. Events without a repository and PR (`repos-changed`, the health events) change the
    /// run-level state; events of a repository whose record is missing (a rebuild of lost state) create it, because the
    /// counters of its ids still matter.
    pub fn apply(&mut self, ev: &Event) {
        match &ev.details {
            Details::ReposChanged(_) => return self.apply_repos(ev),
            Details::WatchError(_) | Details::WatchRecovered(_) | Details::WatchStalled(_) => {
                return self.apply_health(ev)
            }
            _ => {}
        }
        let (Some(repo), Some(n)) = (ev.repo.as_deref(), ev.pr) else {
            return;
        };
        let rs = self
            .repo
            .entry(repo.to_string())
            .or_insert_with(|| RepoState::new(&ev.time));
        match &ev.details {
            Details::NewPr(d) => {
                let generation = id_counter(&ev.id, 'g').unwrap_or(0);
                if !rs.prs.contains_key(&n) {
                    let mut p = PrState {
                        number: n,
                        class: Class::NoChecks,
                        ..PrState::default()
                    };
                    if let Some((_, rec)) = rs.closed_record(n) {
                        p.epochs = p.epochs.max(rec.epochs);
                    }
                    rs.prs.insert(n, p);
                }
                let p = rs.prs.get_mut(&n).expect("inserted above");
                p.title = d.title.clone();
                p.author = d.author.clone();
                p.draft = d.draft;
                p.base = d.base.clone();
                p.head_ref = d.head_ref.clone();
                p.head = d.head.clone();
                p.fork = d.fork;
                p.generation = generation;
                p.detail_due = true;
                p.gone = false;
            }
            Details::Merged(d) => {
                // An unknown head repository (the PR was never observed open) yields no first-class children.
                let head_fork = rs.prs.get(&n).map(|p| p.fork).unwrap_or(true);
                rs.prs.remove(&n);
                let dup = rs.restack_due.iter().any(|x| {
                    x.merged_pr == n
                        && key_merged(x.merge_commit.as_deref())
                            == key_merged(d.merge_commit.as_deref())
                });
                if !dup {
                    rs.restack_due.push(RestackDue {
                        merged_pr: n,
                        merge_commit: d.merge_commit.clone(),
                        old_parent: d.head.clone(),
                        base: d.base.clone(),
                        head_ref: d.head_ref.clone(),
                        head_fork,
                    });
                }
                for o in rs.prs.values_mut() {
                    o.detail_due = true;
                }
            }
            Details::Closed(_) => {
                let generation = id_counter(&ev.id, 'g').unwrap_or(0);
                let mut rec = ClosedPr {
                    number: n,
                    generation,
                    epochs: rs.prs.get(&n).map(|p| p.epochs).unwrap_or_default(),
                };
                if let Some((_, old)) = rs.closed_record(n) {
                    rec.epochs = rec.epochs.max(old.epochs);
                }
                rs.record_closed(rec);
                rs.prs.remove(&n);
            }
            Details::RestackNeeded(_) => {
                if !rs.restack_done.contains(&ev.id) {
                    rs.restack_done.push(ev.id.clone());
                }
            }
            _ => {
                // A lost state: keep the counters of a PR whose `new-pr` was never journalled.
                let p = rs.prs.entry(n).or_insert_with(|| PrState {
                    number: n,
                    ..PrState::default()
                });
                apply_pr(p, ev);
            }
        }
    }

    fn apply_repos(&mut self, ev: &Event) {
        let _ = ev;
    }

    fn apply_health(&mut self, ev: &Event) {
        let _ = ev;
    }
}

fn raise(dst: &mut u64, v: Option<u64>) {
    if let Some(v) = v {
        *dst = (*dst).max(v);
    }
}

impl PrState {
    /// Forgets the check class and the debounce candidates of the old head.
    pub fn reset_checks(&mut self) {
        self.class = Class::Unobserved;
        self.class_candidate = None;
        self.pending_since.clear();
        self.pending_names.clear();
        self.timeout_fired = false;
        self.mergeable_candidate = None;
    }

    /// An accepted class change: the checks epoch grows on every one, also to pending or none.
    pub fn accept_class(&mut self, class: Class, epoch: u64, now: &str) {
        self.class = class;
        self.class_candidate = None;
        self.timeout_fired = false;
        self.pending_since.clear();
        if class == Class::Pending {
            self.pending_since = now.to_string();
        } else {
            self.pending_names.clear();
        }
        self.epochs.checks = self.epochs.checks.max(epoch);
    }
}

fn apply_pr(p: &mut PrState, ev: &Event) {
    match &ev.details {
        Details::HeadChanged(d) => {
            raise(&mut p.epochs.head, id_counter(&ev.id, 'h'));
            p.head = d.new.clone();
            p.reset_checks();
            p.detail_due = true;
        }
        Details::ChecksGreen(d) => checks_event(p, ev, &d.head, Class::Green),
        Details::ChecksRed(d) => checks_event(p, ev, &d.head, Class::Red),
        Details::ChecksPendingTimeout(d) => {
            let e = id_counter(&ev.id, 'c');
            raise(&mut p.epochs.checks, e);
            if p.head.is_empty() {
                p.head = d.head.clone();
            }
            if d.head != p.head {
                return;
            }
            p.class = Class::Pending;
            p.class_candidate = None;
            p.pending_since = d.since.clone();
            p.timeout_fired = true;
            p.pending_names = d.pending.clone();
        }
        Details::MergeableChanged(d) => {
            raise(&mut p.epochs.mergeable, id_counter(&ev.id, 'm'));
            if d.mergeable.is_some() {
                p.mergeable = d.mergeable;
            }
            p.mergeable_state = d.state.clone();
            p.conflicting = d.conflicting;
            p.behind = d.behind;
            p.mergeable_candidate = None;
        }
        Details::ReadyStateChanged(d) => {
            raise(&mut p.epochs.ready, id_counter(&ev.id, 'r'));
            p.draft = d.to == "draft";
        }
        Details::BaseChanged(d) => {
            raise(&mut p.epochs.base, id_counter(&ev.id, 'b'));
            p.base = d.new.clone();
            p.detail_due = true;
        }
        _ => {}
    }
}

/// `checks-green` and `checks-red`: the class of the current head is accepted; for another head only the counter is kept.
fn checks_event(p: &mut PrState, ev: &Event, head: &str, class: Class) {
    let e = id_counter(&ev.id, 'c');
    if p.head.is_empty() {
        // a PR rebuilt from the journal alone (lost state) learns its head here
        p.head = head.to_string();
    }
    if head != p.head {
        raise(&mut p.epochs.checks, e);
        return;
    }
    p.accept_class(class, e.unwrap_or(0), "");
}
