//! Rebase-merge proof (package 1b): identity, run, mapping, pull request. Rebase merges are rebuilt with gix:
//! the same author, author time and subject on a new parent with a new committer and committer time.
use gix::ObjectId;
use stw::tidy_rebase::*;

use crate::common::{Fx, Stamp};

const T0: i64 = 1_700_000_130; // 30 s into a minute: one second more never crosses a minute
const NAME: &str = "Werner Stein";
const MAIL: &str = "w@example.com";
const MERGE_SECS: i64 = T0 + 10_000;

/// One commit of a branch: its own file with 20 lines, so that the patch is the same on any parent.
#[derive(Clone)]
struct C {
    subject: Vec<u8>,
    file: String,
    body: String,
    secs: i64,
    name: &'static str,
    mail: &'static str,
    offset: i32,
}

fn lines(tag: &str, n: usize) -> String {
    (0..n).map(|k| format!("{tag} line {k}\n")).collect()
}

fn c(i: usize) -> C {
    C {
        subject: format!("step {i}").into_bytes(),
        file: format!("f{i}.txt"),
        body: lines(&format!("c{i}"), 20),
        secs: T0 + 100 * i as i64,
        name: NAME,
        mail: MAIL,
        offset: 0,
    }
}

fn cs(n: usize) -> Vec<C> {
    (0..n).map(c).collect()
}

fn put(fx: &Fx, parent: Option<ObjectId>, c: &C, committer: Stamp) -> ObjectId {
    let author = Stamp::new(c.name, c.mail, c.secs).tz(c.offset);
    fx.commit_at(
        parent,
        &[(c.file.as_str(), Some(c.body.as_str()))],
        &c.subject,
        author,
        committer,
    )
}

fn base(fx: &Fx) -> ObjectId {
    let s = Stamp::new("Other", "o@example.com", T0 - 10_000);
    fx.commit_at(None, &[("base.txt", Some("base\n"))], "base", s, s)
}

/// The commits on `parent`, committed when they were written.
fn branch(fx: &Fx, parent: ObjectId, list: &[C]) -> Vec<ObjectId> {
    let mut out: Vec<ObjectId> = Vec::new();
    for c in list {
        let p = out.last().copied().unwrap_or(parent);
        out.push(put(fx, Some(p), c, Stamp::new(c.name, c.mail, c.secs)));
    }
    out
}

/// A rebase merge: the same commits on `parent`, a new committer and the merge time (one second apart).
fn land(fx: &Fx, parent: ObjectId, list: &[C], merge_secs: i64) -> Vec<ObjectId> {
    let mut out: Vec<ObjectId> = Vec::new();
    for (k, c) in list.iter().enumerate() {
        let p = out.last().copied().unwrap_or(parent);
        let m = Stamp::new("Merger", "merger@example.com", merge_secs + k as i64);
        out.push(put(fx, Some(p), c, m));
    }
    out
}

/// A commit of somebody else on `parent`.
fn foreign(fx: &Fx, parent: ObjectId, i: usize) -> ObjectId {
    let s = Stamp::new("Other", "o@example.com", T0 + 50 + i as i64);
    fx.commit_at(
        Some(parent),
        &[(format!("g{i}.txt").as_str(), Some("g\n"))],
        format!("foreign {i}"),
        s,
        s,
    )
}

fn last(v: &[ObjectId]) -> ObjectId {
    *v.last().unwrap()
}

// ---- the index ----

#[test]
fn the_index_is_the_first_parent_chain_from_the_tip() {
    let fx = Fx::new();
    let b = base(&fx);
    let f = foreign(&fx, b, 1);
    let l = land(&fx, f, &cs(2), MERGE_SECS);
    let repo = fx.repo();
    let idx = RebaseIndex::build(&repo, last(&l), None, RB_MAX_WINDOW).unwrap();
    assert_eq!(idx.len(), 4);
    assert_eq!(idx.stop(), Stop::Root);
    assert_eq!(idx.pos_of(last(&l)), Some(0));
    assert_eq!(idx.pos_of(l[0]), Some(1));
    assert_eq!(idx.pos_of(f), Some(2));
    assert_eq!(idx.pos_of(b), Some(3));
    let h = read_head(&repo, l[0]).unwrap();
    assert_eq!(idx.with_key(&h.key), &[1]);
    assert_eq!(idx.head(1).unwrap().id, l[0]);
}

#[test]
fn a_head_has_the_lower_case_mail_the_exact_name_and_the_right_trimmed_first_line() {
    let fx = Fx::new();
    let s = Stamp::new("Ada  Lovelace", "Ada@Example.COM", T0).tz(3600);
    let id = fx.commit_at(
        None,
        &[("a", Some("a\n"))],
        "Subject with tail \t \r\n\nbody line\n",
        s,
        s,
    );
    let h = read_head(&fx.repo(), id).unwrap();
    assert_eq!(h.name, b"Ada  Lovelace");
    assert_eq!(h.key.mail, b"ada@example.com");
    assert_eq!(h.key.secs, T0);
    assert_eq!(h.key.subject, b"Subject with tail");
    assert_eq!(h.offset, 3600);
    assert_eq!(h.committer_secs, T0);
    assert_eq!(h.parents, 0);
    assert_eq!(
        subject_of(b"  keeps the left side  \nrest"),
        b"  keeps the left side"
    );
    assert_eq!(subject_of(b""), b"");
}

#[test]
fn a_merge_commit_on_the_chain_is_a_position_but_never_a_target() {
    let fx = Fx::new();
    let b = base(&fx);
    let side = foreign(&fx, b, 1);
    let tipc = foreign(&fx, b, 2);
    let s = Stamp::new(NAME, MAIL, T0 + 100);
    let m = fx.commit_at_parents(vec![tipc, side], Some(tipc), &[], "step 1", s, s);
    let repo = fx.repo();
    let idx = RebaseIndex::build(&repo, m, None, RB_MAX_WINDOW).unwrap();
    assert_eq!(idx.pos_of(m), Some(0), "the merge is a position");
    assert_eq!(
        idx.len(),
        3,
        "the first-parent chain: merge, foreign 2, base"
    );
    let h = read_head(&repo, m).unwrap();
    assert_eq!(h.parents, 2);
    assert!(idx.with_key(&h.key).is_empty(), "but nobody can match it");
}

/// `n` commits of somebody else on `parent`, author (and committer) seconds `secs`, `secs + 1`, ...
fn old_commits(fx: &Fx, mut parent: ObjectId, n: usize, secs: i64) -> ObjectId {
    for k in 0..n {
        let s = Stamp::new("Other", "o@example.com", secs + k as i64);
        parent = fx.commit_at(
            Some(parent),
            &[(format!("o{k}.txt").as_str(), Some("o\n"))],
            format!("old {k}"),
            s,
            s,
        );
    }
    parent
}

#[test]
fn an_old_committer_time_does_not_end_the_walk() {
    // `--committer-date-is-author-date` and old clocks: the committer time says nothing about the target
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let mut br = Vec::new();
    for c in &list {
        let p = br.last().copied().unwrap_or(b);
        br.push(put(
            &fx,
            Some(p),
            c,
            Stamp::new(NAME, MAIL, T0 - 6 * 86_400),
        ));
    }
    let l = land(&fx, b, &list, T0 - 5 * 86_400);
    let f = foreign(&fx, last(&l), 1);
    let repo = fx.repo();
    let idx = RebaseIndex::build(&repo, f, Some(T0), RB_MAX_WINDOW).unwrap();
    assert_eq!((idx.len(), idx.stop()), (5, Stop::Root));
    let r = run(&fx, f, last(&br));
    assert_eq!(r.mapped, 3);
    assert_eq!(r.strength, Strength::Strong);
}

#[test]
fn a_run_of_old_author_times_ends_the_walk_a_single_one_does_not() {
    let fx = Fx::new();
    let b = base(&fx);
    let repo = fx.repo();
    let n = RB_STOP_RUN;
    let ancient = 1_000;
    let walk = |tip| RebaseIndex::build(&repo, tip, Some(T0), RB_MAX_WINDOW).unwrap();
    // n old commits in a row below two landed commits: stop (the run itself is indexed)
    let tip = last(&land(
        &fx,
        old_commits(&fx, b, n, ancient),
        &cs(2),
        MERGE_SECS,
    ));
    let idx = walk(tip);
    assert_eq!((idx.len(), idx.stop()), (n + 2, Stop::Time));
    // n - 1 old, a recent commit, n - 1 old: no stop, the root is reached
    let low = old_commits(&fx, b, n - 1, ancient);
    let mid = foreign(&fx, low, 7);
    let tip = last(&land(
        &fx,
        old_commits(&fx, mid, n - 1, ancient),
        &cs(2),
        MERGE_SECS,
    ));
    let idx = walk(tip);
    assert_eq!(idx.stop(), Stop::Root);
    assert_eq!(idx.len(), 2 + (n - 1) + 1 + (n - 1) + 1);
    // a single old commit never stops
    let tip = last(&land(
        &fx,
        old_commits(&fx, b, 1, ancient),
        &cs(2),
        MERGE_SECS,
    ));
    assert_eq!(walk(tip).stop(), Stop::Root);
    // without a time bound the chain is walked to its root
    let tip = last(&land(
        &fx,
        old_commits(&fx, b, n, ancient),
        &cs(2),
        MERGE_SECS,
    ));
    let idx = RebaseIndex::build(&repo, tip, None, RB_MAX_WINDOW).unwrap();
    assert_eq!(idx.stop(), Stop::Root);
}

#[test]
fn the_age_bound_is_a_day_before_the_oldest_author_time_inclusive() {
    let fx = Fx::new();
    let b = base(&fx);
    let repo = fx.repo();
    let n = RB_STOP_RUN as i64;
    let bound = T0 - RB_SLACK_SECS;
    let walk = |first: i64| {
        let lo = old_commits(&fx, b, RB_STOP_RUN, first);
        let tip = last(&land(&fx, lo, &cs(1), MERGE_SECS));
        RebaseIndex::build(&repo, tip, Some(T0), RB_MAX_WINDOW)
            .unwrap()
            .stop()
    };
    // the newest of the run is exactly at the bound: not old, the run is broken
    assert_eq!(walk(bound - n + 1), Stop::Root);
    // one second older: all are old
    assert_eq!(walk(bound - n), Stop::Time);
}

#[test]
fn the_walk_stops_at_the_window_limit_and_says_so() {
    let fx = Fx::new();
    let b = base(&fx);
    let l = land(&fx, b, &cs(5), MERGE_SECS);
    let repo = fx.repo();
    let idx = RebaseIndex::build(&repo, last(&l), None, 3).unwrap();
    assert_eq!(idx.len(), 3);
    assert_eq!(idx.stop(), Stop::Window);
    assert!(idx.window_cut());
    let idx = RebaseIndex::build(&repo, last(&l), None, 6).unwrap();
    assert!(!idx.window_cut(), "the whole chain fits");
    assert_eq!(idx.len(), 6);
    let idx = RebaseIndex::build(&repo, last(&l), None, 7).unwrap();
    assert_eq!(idx.stop(), Stop::Root);
}

#[test]
fn a_missing_parent_object_ends_the_walk_without_an_error() {
    let fx = Fx::new();
    // a shallow or partial clone: the parent is named but not there
    let ghost: ObjectId = "1234567890123456789012345678901234567890".parse().unwrap();
    let s = Stamp::new(NAME, MAIL, T0);
    let edge = fx.commit_at_parents(vec![ghost], None, &[("e", Some("e\n"))], "edge", s, s);
    let tipc = fx.commit_at(Some(edge), &[("t", Some("t\n"))], "tip", s, s);
    let repo = fx.repo();
    let idx = RebaseIndex::build(&repo, tipc, None, RB_MAX_WINDOW).unwrap();
    assert_eq!(idx.len(), 2, "tip and the edge commit are indexed");
    assert_eq!(idx.stop(), Stop::Missing(ghost));
    assert!(
        RebaseIndex::build(&repo, ghost, None, RB_MAX_WINDOW).is_err(),
        "a missing tip is an error"
    );
}

/// A shallow boundary: `edge` has the key of `c(0)` but its parent object is not there.
fn shallow_edge(fx: &Fx) -> (ObjectId, ObjectId) {
    let ghost: ObjectId = "1234567890123456789012345678901234567890".parse().unwrap();
    let c0 = c(0);
    let author = Stamp::new(c0.name, c0.mail, c0.secs);
    let edge = fx.commit_at_parents(
        vec![ghost],
        None,
        &[(c0.file.as_str(), Some(c0.body.as_str()))],
        &c0.subject,
        author,
        Stamp::new("Merger", "merger@example.com", MERGE_SECS),
    );
    (edge, ghost)
}

#[test]
fn the_boundary_commit_of_a_shallow_clone_is_skipped_not_an_error() {
    let fx = Fx::new();
    let (edge, ghost) = shallow_edge(&fx);
    let list = cs(2);
    let l = land(&fx, edge, &list[1..], MERGE_SECS + 10);
    let base_tip = last(&l);
    // the branch (with its own root commit): its first commit is the one at the boundary, the second landed on top of it
    let b = base(&fx);
    let br = branch(&fx, b, &list);
    let repo = fx.repo();
    let side = branch_side(&repo, base_tip, last(&br)).unwrap();
    let index =
        RebaseIndex::build(&repo, base_tip, side.oldest_author_secs, RB_MAX_WINDOW).unwrap();
    assert_eq!(index.stop(), Stop::Missing(ghost));
    // no error from the patch IDs (the boundary commit has no patch to read)
    let hits = patch_hits(&repo, &index, &side.commits).unwrap();
    assert_eq!(
        hits.get(&br[1]),
        Some(&last(&l)),
        "the commit above the boundary has its patch"
    );
    let mut cache = SimCache::default();
    let r = evaluate(&repo, &mut cache, &index, &side, &hits, &Limits::default()).unwrap();
    // the identity finds the boundary commit; its content cannot be read: not vouched for
    assert_eq!(r.mapping[1].main, Some(edge));
    assert_eq!(r.mapping[1].value, None);
    assert_eq!(
        (r.n, r.mapped),
        (3, 2),
        "the base commit of the fixture branch is not on main"
    );
    assert!(!r.content_ok);
    assert_eq!(r.strength, Strength::Hint);
    assert_eq!(r.stop, Stop::Missing(ghost), "the reason is in the report");
}

// ---- the branch side ----

#[test]
fn the_branch_side_is_the_non_merge_commits_oldest_first() {
    let fx = Fx::new();
    let b = base(&fx);
    let m1 = foreign(&fx, b, 1);
    let list = cs(3);
    let b0 = put(&fx, Some(b), &list[0], Stamp::new(NAME, MAIL, list[0].secs));
    // a merge of main into the branch (its second parent is on main: not part of the branch)
    let s = Stamp::new(NAME, MAIL, T0 + 150);
    let mg = fx.commit_at_parents(vec![b0, m1], Some(b0), &[], "Merge main", s, s);
    let b1 = put(
        &fx,
        Some(mg),
        &list[1],
        Stamp::new(NAME, MAIL, list[1].secs),
    );
    let empty = fx.commit_at(
        Some(b1),
        &[],
        "empty",
        Stamp::new(NAME, MAIL, T0 + 250),
        Stamp::new(NAME, MAIL, T0 + 250),
    );
    let b2 = put(
        &fx,
        Some(empty),
        &list[2],
        Stamp::new(NAME, MAIL, list[2].secs),
    );
    let side = branch_side(&fx.repo(), m1, b2).unwrap();
    assert_eq!(side.commits, vec![b0, b1, empty, b2]);
    assert_eq!(
        side.skipped,
        SideSkipped {
            merges: 1,
            empty: 1
        }
    );
    assert_eq!(side.oldest_author_secs, Some(T0));
    let none = branch_side(&fx.repo(), b2, b2).unwrap();
    assert!(none.commits.is_empty());
    assert_eq!(none.oldest_author_secs, None);
}

#[test]
fn the_branch_side_puts_parents_before_children_even_with_equal_times() {
    let fx = Fx::new();
    let b = base(&fx);
    // two commits with the same author time on top of each other, then a side commit with an older time
    let mut x = c(0);
    x.secs = T0 + 500;
    let mut y = c(1);
    y.secs = T0 + 500;
    let mut z = c(2);
    z.secs = T0 + 100;
    let xs = Stamp::new(NAME, MAIL, T0 + 500);
    let id_x = put(&fx, Some(b), &x, xs);
    let id_y = put(&fx, Some(id_x), &y, xs);
    let id_z = put(&fx, Some(b), &z, Stamp::new(NAME, MAIL, T0 + 100));
    let s = Stamp::new(NAME, MAIL, T0 + 600);
    let mg = fx.commit_at_parents(vec![id_y, id_z], Some(id_y), &[], "Merge", s, s);
    let side = branch_side(&fx.repo(), b, mg).unwrap();
    assert_eq!(side.commits.len(), 3);
    let pos = |id| side.commits.iter().position(|c| *c == id).unwrap();
    assert!(pos(id_x) < pos(id_y));
    assert!(
        pos(id_z) < pos(id_x),
        "the older author time goes first among the ready commits"
    );
}

// ---- patch IDs ----

#[test]
fn equal_patches_pair_branch_commits_with_chain_commits_oldest_with_oldest() {
    let fx = Fx::new();
    let b = base(&fx);
    // the same patch twice (same file content in a new file with another name is another patch: use one file)
    let one = C {
        file: "same.txt".into(),
        body: lines("s", 5),
        ..c(0)
    };
    let two = C {
        subject: b"again".to_vec(),
        secs: T0 + 100,
        ..one.clone()
    };
    let br = {
        let x = put(&fx, Some(b), &one, Stamp::new(NAME, MAIL, one.secs));
        // the second commit changes the file back and forth so that its patch is the same as the first one's
        let y = fx.commit_at(
            Some(x),
            &[("same.txt", None)],
            "remove",
            Stamp::new(NAME, MAIL, T0 + 50),
            Stamp::new(NAME, MAIL, T0 + 50),
        );
        let z = put(&fx, Some(y), &two, Stamp::new(NAME, MAIL, two.secs));
        vec![x, y, z]
    };
    let main = {
        let x = put(
            &fx,
            Some(b),
            &one,
            Stamp::new("M", "m@example.com", MERGE_SECS),
        );
        let y = fx.commit_at(
            Some(x),
            &[("same.txt", None)],
            "remove",
            Stamp::new(NAME, MAIL, T0 + 50),
            Stamp::new("M", "m@example.com", MERGE_SECS + 1),
        );
        let z = put(
            &fx,
            Some(y),
            &two,
            Stamp::new("M", "m@example.com", MERGE_SECS + 2),
        );
        vec![x, y, z]
    };
    let repo = fx.repo();
    let idx = RebaseIndex::build(&repo, last(&main), None, RB_MAX_WINDOW).unwrap();
    let hits = patch_hits(&repo, &idx, &br).unwrap();
    assert_eq!(hits.len(), 3);
    assert_eq!(
        hits[&br[0]], main[0],
        "the oldest of two equal patches meets the oldest"
    );
    assert_eq!(hits[&br[2]], main[2]);
    assert_eq!(hits[&br[1]], main[1]);
}

#[test]
fn a_commit_without_an_equal_patch_has_no_hit() {
    let fx = Fx::new();
    let b = base(&fx);
    let br = branch(&fx, b, &[c(0)]);
    let mut changed = c(0);
    changed.body = lines("other", 20);
    let main = land(&fx, b, &[changed], MERGE_SECS);
    let repo = fx.repo();
    let idx = RebaseIndex::build(&repo, last(&main), None, RB_MAX_WINDOW).unwrap();
    assert!(patch_hits(&repo, &idx, &br).unwrap().is_empty());
    assert!(patch_hits(&repo, &idx, &[]).unwrap().is_empty());
}

// ---- (a) identity ----

use std::collections::HashMap;
use stw::tidy_similarity::{Limits, SimCache};

/// Index, branch side and (optionally) patch hits for `base..tip` against `main`, then the proof.
fn run_full(
    fx: &Fx,
    main: ObjectId,
    tip: ObjectId,
    hits: bool,
    window: usize,
) -> (RebaseReport, RebaseIndex) {
    let repo = fx.repo();
    let side = branch_side(&repo, main, tip).unwrap();
    let index = RebaseIndex::build(&repo, main, side.oldest_author_secs, window).unwrap();
    let hit = if hits {
        patch_hits(&repo, &index, &side.commits).unwrap()
    } else {
        HashMap::new()
    };
    let mut cache = SimCache::default();
    let rep = evaluate(&repo, &mut cache, &index, &side, &hit, &Limits::default()).unwrap();
    (rep, index)
}

/// The identity proof alone (no patch IDs).
fn run(fx: &Fx, main: ObjectId, tip: ObjectId) -> RebaseReport {
    run_full(fx, main, tip, false, RB_MAX_WINDOW).0
}

fn run_hits(fx: &Fx, main: ObjectId, tip: ObjectId) -> RebaseReport {
    run_full(fx, main, tip, true, RB_MAX_WINDOW).0
}

fn positions(r: &RebaseReport) -> Vec<Option<u32>> {
    r.mapping.iter().map(|e| e.position).collect()
}

fn mapped_methods(r: &RebaseReport) -> Vec<&'static str> {
    r.mapping.iter().map(|e| e.method.as_str()).collect()
}

#[test]
fn a_rebase_merge_is_found_by_author_mail_time_and_subject() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(
        (r.n, r.mapped, r.by_identity, r.by_patch_id, r.unmapped),
        (3, 3, 3, 0, 0)
    );
    assert_eq!(mapped_methods(&r), ["identity"; 3]);
    // the oldest commit has the greatest position
    assert_eq!(positions(&r), [Some(2), Some(1), Some(0)]);
    for (e, (bc, m)) in r.mapping.iter().zip(br.iter().zip(&l)) {
        assert_eq!((e.commit, e.main), (*bc, Some(*m)));
        assert!(e.identity && !e.ambiguous && !e.tz_differs);
    }
}

#[test]
fn an_equal_patch_comes_first_and_the_identity_is_noted_on_the_same_target() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let r = run_hits(&fx, last(&l), last(&br));
    assert_eq!((r.mapped, r.by_patch_id, r.by_identity), (3, 3, 0));
    assert_eq!(mapped_methods(&r), ["patch-id"; 3]);
    assert!(
        r.mapping.iter().all(|e| e.identity),
        "the identity key matches the same commit"
    );
    assert_eq!(positions(&r), [Some(2), Some(1), Some(0)]);
}

#[test]
fn an_equal_patch_on_another_commit_wins_over_the_identity() {
    let fx = Fx::new();
    let b = base(&fx);
    let one = c(0);
    let br = branch(&fx, b, std::slice::from_ref(&one));
    // main: first a cherry-pick of the content with other author data, then the commit with the identity but other content
    let pick = C {
        secs: one.secs + 7,
        subject: b"picked".to_vec(),
        ..one.clone()
    };
    let m1 = land(&fx, b, &[pick], MERGE_SECS);
    let ident = C {
        body: lines("different", 20),
        ..one.clone()
    };
    let m2 = land(&fx, last(&m1), &[ident], MERGE_SECS + 100);
    let r = run_hits(&fx, last(&m2), last(&br));
    assert_eq!(r.mapping[0].main, Some(m1[0]));
    assert_eq!(r.mapping[0].method, Method::PatchId);
    assert!(!r.mapping[0].identity);
    // without patch IDs the identity decides
    let r = run(&fx, last(&m2), last(&br));
    assert_eq!(r.mapping[0].main, Some(m2[0]));
    assert_eq!(r.mapping[0].method, Method::Identity);
}

#[test]
fn an_amend_in_the_branch_before_landing_keeps_the_author_time() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    // committed again an hour later (amend, rebase): the committer changes, the author does not
    let mut parent = b;
    let mut br = Vec::new();
    for c in &list {
        parent = put(&fx, Some(parent), c, Stamp::new(NAME, MAIL, c.secs + 3600));
        br.push(parent);
    }
    let l = land(&fx, b, &list, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!((r.mapped, r.by_identity), (3, 3));
}

#[test]
fn an_amend_after_landing_changes_the_author_time_and_there_is_no_identity() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let l = land(&fx, b, &list, MERGE_SECS);
    let mut again = list.clone();
    for c in &mut again {
        c.secs += 5_000; // `--reset-author`, `--date`: a new author time
        c.body = lines(&format!("new-{}", c.file), 20);
    }
    let br = branch(&fx, b, &again);
    let r = run_hits(&fx, last(&l), last(&br));
    assert_eq!(
        (r.mapped, r.by_identity, r.by_patch_id, r.unmapped),
        (0, 0, 0, 3)
    );
    assert!(r
        .mapping
        .iter()
        .all(|e| e.main.is_none() && e.method == Method::None));
}

#[test]
fn one_second_more_or_less_is_another_commit() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(2);
    let br = branch(&fx, b, &list);
    let mut shifted = list.clone();
    shifted[1].secs += 1;
    let l = land(&fx, b, &shifted, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(r.mapped, 1);
    assert!(r.mapping[0].main.is_some() && r.mapping[1].main.is_none());
}

#[test]
fn the_mail_is_compared_in_lower_case_and_the_name_exactly() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let mut main_side = list.clone();
    main_side[0].mail = "W@EXAMPLE.com"; // same mail, other case
    main_side[1].mail = "w@example.org"; // another mail
    main_side[2].name = "werner stein"; // another name
    let l = land(&fx, b, &main_side, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(
        r.mapping[0].main,
        Some(l[0]),
        "the mail case does not matter"
    );
    assert_eq!(r.mapping[1].main, None, "another mail is another author");
    assert_eq!(r.mapping[2].main, None, "another name is another author");
}

#[test]
fn the_subject_is_the_whole_first_line_right_trimmed() {
    let fx = Fx::new();
    let b = base(&fx);
    let a = C {
        subject: b"feat(core): a long subject that only differs at the very end - one".to_vec(),
        ..c(0)
    };
    let main_a = C {
        subject: b"feat(core): a long subject that only differs at the very end - two".to_vec(),
        ..a.clone()
    };
    let t = C {
        subject: b"tail".to_vec(),
        ..c(1)
    };
    let t_main = C {
        subject: b"tail \t\r".to_vec(),
        ..t.clone()
    };
    let br = branch(&fx, b, &[a, t]);
    let l = land(&fx, b, &[main_a, t_main], MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(
        r.mapping[0].main, None,
        "a subject is compared whole, not by its first characters"
    );
    assert_eq!(
        r.mapping[1].main,
        Some(l[1]),
        "trailing white space is not part of the subject"
    );
}

#[test]
fn a_squash_subject_with_a_pull_request_number_is_not_the_subject_of_the_commit() {
    let fx = Fx::new();
    let b = base(&fx);
    let one = c(0);
    let br = branch(&fx, b, std::slice::from_ref(&one));
    let sq = C {
        subject: b"step 0 (#7)".to_vec(),
        ..one.clone()
    };
    let l = land(&fx, b, &[sq], MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(
        r.mapped, 0,
        "a rebase does not change the subject: (#N) belongs to a squash"
    );
}

#[test]
fn several_commits_with_one_subject_are_found_by_their_seconds() {
    let fx = Fx::new();
    let b = base(&fx);
    let mut list = cs(3);
    for c in &mut list {
        c.subject = b"wip".to_vec();
    }
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!((r.mapped, r.ambiguous), (3, 0));
    assert_eq!(positions(&r), [Some(2), Some(1), Some(0)]);
}

#[test]
fn the_same_second_in_two_commits_needs_the_subject() {
    let fx = Fx::new();
    let b = base(&fx);
    let mut list = cs(3);
    list[1].secs = list[0].secs; // 14 of 31 measured pull requests have this
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!((r.mapped, r.ambiguous), (3, 0));
    // the first commit is not on main, the second is: the subject keeps them apart
    let l = land(&fx, b, &list[1..], MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(r.mapping[0].main, None);
    assert_eq!(r.mapping[1].main, Some(l[0]));
    assert_eq!(r.mapping[2].main, Some(l[1]));
}

fn twins() -> Vec<C> {
    let mut list = cs(3);
    list[0].subject = b"wip".to_vec();
    list[1].subject = b"wip".to_vec();
    list[1].secs = list[0].secs; // the same mail, second and subject twice
    list
}

#[test]
fn the_same_key_twice_is_a_multiset_and_the_neighbour_decides() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = twins();
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!((r.mapped, r.ambiguous), (3, 0));
    assert_eq!(r.mapping[0].main, Some(l[0]));
    assert_eq!(r.mapping[1].main, Some(l[1]));
    assert_eq!(r.mapping[2].main, Some(l[2]));
    // the unique commit is the oldest: the twins follow it
    let u = C {
        secs: T0 - 100,
        ..c(0)
    };
    let t1 = C {
        subject: b"wip".to_vec(),
        secs: T0 + 500,
        ..c(1)
    };
    let t2 = C {
        subject: b"wip".to_vec(),
        secs: T0 + 500,
        ..c(2)
    };
    let list = [u, t1, t2];
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!((r.mapped, r.ambiguous), (3, 0), "{:?}", positions(&r));
    assert_eq!(positions(&r), [Some(2), Some(1), Some(0)]);
}

#[test]
fn the_same_key_twice_without_a_neighbour_is_ambiguous_and_not_mapped() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = &twins()[..2];
    let br = branch(&fx, b, list);
    let l = land(&fx, b, list, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!((r.mapped, r.ambiguous, r.unmapped), (0, 2, 2));
    assert!(r
        .mapping
        .iter()
        .all(|e| e.ambiguous && e.main.is_none() && e.method == Method::None));
}

#[test]
fn a_position_is_taken_once_two_branch_commits_do_not_share_one_main_commit() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = &twins()[..2];
    let br = branch(&fx, b, list);
    // main has only one commit with that key
    let l = land(&fx, b, &list[..1], MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(r.mapped, 1);
    assert_eq!(r.mapping[0].main, Some(l[0]), "the older commit takes it");
    assert_eq!(r.mapping[1].main, None);
    assert_eq!(r.unmapped, 1);
}

#[test]
fn a_zone_offset_that_differs_is_noted_and_the_epoch_second_decides() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(2);
    let br = branch(&fx, b, &list);
    let mut main_side = list.clone();
    main_side[1].offset = 7200;
    let l = land(&fx, b, &main_side, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(r.mapped, 2);
    assert!(!r.mapping[0].tz_differs);
    assert!(r.mapping[1].tz_differs);
}

#[test]
fn a_commit_off_the_first_parent_chain_is_not_a_target() {
    // the identity is looked up on the first-parent chain of the base only
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(1);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let side_commit = foreign(&fx, b, 1);
    let s = Stamp::new("M", "m@example.com", MERGE_SECS + 50);
    // the landed commit is only the second parent of a merge: not on the first-parent chain
    let mg = fx.commit_at_parents(
        vec![side_commit, last(&l)],
        Some(side_commit),
        &[],
        "Merge",
        s,
        s,
    );
    let r = run(&fx, mg, last(&br));
    assert_eq!(r.mapped, 0);
}

// ---- (b) one contiguous run, strength, class ----

#[test]
fn a_clean_rebase_merge_is_a_strong_proof_and_the_class_is_review() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    for r in [
        run(&fx, last(&l), last(&br)),
        run_hits(&fx, last(&l), last(&br)),
    ] {
        assert_eq!(r.strength, Strength::Strong);
        assert!(r.contiguous && r.in_order && !r.partial && r.content_ok && !r.modified);
        assert_eq!((r.gap, r.runs, r.run_len), (0, 1, 3));
        assert_eq!(evidence_class(&r), Some(("rebase-identity", "review")));
    }
}

#[test]
fn a_hint_or_nothing_gives_no_class_and_the_decision_is_never_delete() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let none = run(&fx, b, last(&br));
    assert_eq!(none.strength, Strength::None);
    assert_eq!(evidence_class(&none), None);
    let l = land(&fx, b, &list[..2], MERGE_SECS);
    let hint = run(&fx, last(&l), last(&br));
    assert_eq!(hint.strength, Strength::Hint);
    assert_eq!(evidence_class(&hint), None);
    assert_eq!(RB_DECISION, "review");
    assert_ne!(RB_DECISION, "delete");
}

#[test]
fn a_foreign_commit_between_the_targets_breaks_the_run() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let l1 = land(&fx, b, &list[..2], MERGE_SECS);
    let f = foreign(&fx, last(&l1), 1);
    let l2 = land(&fx, f, &list[2..], MERGE_SECS + 100);
    let r = run(&fx, last(&l2), last(&br));
    assert_eq!(r.mapped, 3);
    assert_eq!(positions(&r), [Some(3), Some(2), Some(0)]);
    assert!(!r.contiguous);
    assert!(r.in_order);
    assert_eq!(r.gap, 1);
    assert_eq!((r.runs, r.run_len), (2, 2));
    assert_eq!(
        r.strength,
        Strength::Hint,
        "identity without a run is a hint"
    );
    assert_eq!(evidence_class(&r), None);
}

#[test]
fn the_same_commits_in_another_order_are_a_hint() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let swapped = [list[1].clone(), list[0].clone(), list[2].clone()];
    let l = land(&fx, b, &swapped, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(r.mapped, 3);
    assert_eq!(positions(&r), [Some(1), Some(2), Some(0)]);
    assert!(r.contiguous, "the positions are one block");
    assert!(!r.in_order);
    assert_eq!(r.strength, Strength::Hint);
}

#[test]
fn a_merge_commit_on_main_between_the_targets_is_a_gap() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let l1 = land(&fx, b, &list[..1], MERGE_SECS);
    let side = foreign(&fx, b, 1);
    let s = Stamp::new("M", "m@example.com", MERGE_SECS + 50);
    // a merge commit with the key of the third commit: it must not become its target
    let key3 = Stamp::new(NAME, MAIL, list[2].secs);
    let mg = fx.commit_at_parents(
        vec![last(&l1), side],
        Some(last(&l1)),
        &[],
        "step 2",
        key3,
        s,
    );
    let l2 = land(&fx, mg, &list[1..2], MERGE_SECS + 100);
    let r = run(&fx, last(&l2), last(&br));
    assert_eq!(positions(&r), [Some(2), Some(0), None]);
    assert_eq!(r.mapping[2].main, None, "a merge commit is never a target");
    assert_eq!((r.mapped, r.gap), (2, 1));
    assert!(!r.contiguous);
    assert_eq!(r.strength, Strength::Hint);
}

#[test]
fn a_part_of_the_branch_that_landed_as_one_run_is_partial() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(5);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list[..3], MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!((r.n, r.mapped, r.unmapped), (5, 3, 2));
    assert!(r.partial && r.contiguous && r.in_order);
    assert_eq!((r.runs, r.run_len, r.gap), (1, 3, 0));
    assert_eq!(
        r.strength,
        Strength::Hint,
        "all commits must be mapped for a proof"
    );
    // the rest is the newest part of the branch
    assert!(r.mapping[3].main.is_none() && r.mapping[4].main.is_none());
}

#[test]
fn nothing_found_is_none_and_every_commit_is_unmapped() {
    let fx = Fx::new();
    let b = base(&fx);
    let br = branch(&fx, b, &cs(3));
    let f = foreign(&fx, b, 1);
    let r = run(&fx, f, last(&br));
    assert_eq!((r.n, r.mapped, r.unmapped), (3, 0, 3));
    assert_eq!(r.strength, Strength::None);
    assert!(!r.contiguous && !r.partial);
    assert_eq!((r.gap, r.runs, r.run_len), (0, 0, 0));
}

#[test]
fn an_empty_branch_has_no_proof() {
    let fx = Fx::new();
    let b = base(&fx);
    let r = run(&fx, b, b);
    assert_eq!((r.n, r.mapped), (0, 0));
    assert_eq!(r.strength, Strength::None);
}

#[test]
fn one_commit_is_enough_for_a_proof_when_it_has_the_identity() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(1);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(r.strength, Strength::Strong);
    // an equal patch alone (another author time) is not an identity
    let mut other = list.clone();
    other[0].secs += 40;
    let l = land(&fx, b, &other, MERGE_SECS);
    let r = run_hits(&fx, last(&l), last(&br));
    assert_eq!((r.mapped, r.by_patch_id, r.by_identity), (1, 1, 0));
    assert!(!r.mapping[0].identity);
    assert_eq!(r.strength, Strength::Hint);
}

#[test]
fn merge_commits_of_the_branch_are_not_counted_and_empty_commits_are() {
    let fx = Fx::new();
    let b = base(&fx);
    let m1 = foreign(&fx, b, 1);
    let list = cs(2);
    let b0 = put(&fx, Some(b), &list[0], Stamp::new(NAME, MAIL, list[0].secs));
    let s = Stamp::new(NAME, MAIL, T0 + 120);
    let mg = fx.commit_at_parents(vec![b0, m1], Some(b0), &[], "Merge main", s, s);
    let es = Stamp::new(NAME, MAIL, T0 + 150);
    let be = fx.commit_at(Some(mg), &[], "empty", es, es);
    let b1 = put(
        &fx,
        Some(be),
        &list[1],
        Stamp::new(NAME, MAIL, list[1].secs),
    );
    // main: m1, then the same three commits written again, the empty one included
    let merger = |k: i64| Stamp::new("M", "m@example.com", MERGE_SECS + k);
    let x = put(&fx, Some(m1), &list[0], merger(0));
    let y = fx.commit_at(Some(x), &[], "empty", es, merger(1));
    let z = put(&fx, Some(y), &list[1], merger(2));
    let r = run(&fx, z, b1);
    assert_eq!(
        r.skipped,
        SideSkipped {
            merges: 1,
            empty: 1
        }
    );
    assert_eq!((r.n, r.mapped), (3, 3));
    assert_eq!(r.strength, Strength::Strong);
}

// ---- (c) content value, runs filled by similarity, mapping table, remaining ----

/// `lines(tag, 20)` with the first `changed` lines replaced.
fn edited(tag: &str, changed: usize) -> String {
    (0..20)
        .map(|k| {
            if k < changed {
                format!("edit {tag} {k}\n")
            } else {
                format!("{tag} line {k}\n")
            }
        })
        .collect()
}

/// The same three commits, the middle one landed with `changed` of its 20 lines different.
fn resolved(changed: usize) -> RebaseReport {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let mut main_side = list.clone();
    main_side[1].body = edited("c1", changed);
    let l = land(&fx, b, &main_side, MERGE_SECS);
    run(&fx, last(&l), last(&br))
}

#[test]
fn a_conflict_resolution_keeps_the_identity_with_a_content_value() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let mut main_side = list.clone();
    main_side[1].body = edited("c1", 2); // 18 of 20 lines equal: 90.0 %
    let l = land(&fx, b, &main_side, MERGE_SECS);
    // the patch ID differs for the middle commit, the identity carries it
    let r = run_hits(&fx, last(&l), last(&br));
    assert_eq!(mapped_methods(&r), ["patch-id", "identity", "patch-id"]);
    assert_eq!(r.mapping[1].value, Some(900));
    assert_eq!(r.mapping[0].value, Some(1000));
    // 90.0 % is a modified commit: a hint, no proof
    assert_eq!(r.strength, Strength::Hint);
    assert!(!r.content_ok && r.modified);
}

#[test]
fn a_proof_needs_95_percent_per_commit_70_to_94_is_modified_below_is_no_proof() {
    assert_eq!(RB_CONTENT_HIGH, 95);
    // 1 of 20 lines changed: Dice 95.0 %, inclusive
    let r = resolved(1);
    assert_eq!(r.mapping[1].value, Some(950));
    assert!(r.content_ok && !r.modified);
    assert_eq!(r.strength, Strength::Strong);
    // 2 of 20: 90.0 %, modified
    let r = resolved(2);
    assert_eq!(r.mapping[1].value, Some(900));
    assert!(!r.content_ok && r.modified);
    assert_eq!((r.mapped, r.strength), (3, Strength::Hint));
    assert_eq!(evidence_class(&r), None);
    // 6 of 20: 70.0 % is still modified, 7 of 20: 65.0 % is not
    let r = resolved(6);
    assert_eq!(r.mapping[1].value, Some(700));
    assert!(r.modified);
    let r = resolved(7);
    assert_eq!(r.mapping[1].value, Some(650));
    assert!(!r.content_ok && !r.modified);
    assert_eq!(r.mapped, 3, "the identity still maps it");
    assert_eq!(r.strength, Strength::Hint);
}

/// One commit amended after it landed: the author is unchanged, the committer is `committer_secs`; `changed` of its
/// 20 lines differ from the landed version.
fn amended(changed: usize, committer_secs: i64, hits: bool) -> (RebaseReport, ObjectId) {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list[..2]);
    let l = land(&fx, b, &list, MERGE_SECS);
    let mut v = list[2].clone();
    v.body = edited("c2", changed);
    let amend = put(
        &fx,
        Some(last(&br)),
        &v,
        Stamp::new(NAME, MAIL, committer_secs),
    );
    let r = if hits {
        run_hits(&fx, last(&l), amend)
    } else {
        run(&fx, last(&l), amend)
    };
    (r, l[2])
}

#[test]
fn an_amend_after_landing_keeps_the_author_time_and_is_at_most_a_hint() {
    // 19 of 20 lines equal (95.0 %): the content alone would pass; the committer time is later than the one on main
    let (r, target) = amended(1, MERGE_SECS + 3600, false);
    assert_eq!(
        r.mapping[2].main,
        Some(target),
        "the identity still finds it"
    );
    assert!(r.mapping[2].later);
    assert!(!r.mapping[0].later && !r.mapping[1].later);
    assert_eq!(r.mapped, 3);
    assert_eq!(r.strength, Strength::Hint);
    assert_eq!(evidence_class(&r), None);
    // a version from before the merge (a conflict resolution in the branch) is no later version
    let (r, _) = amended(1, MERGE_SECS - 3600, false);
    assert!(!r.mapping[2].later);
    assert_eq!(r.strength, Strength::Strong);
    // the committer time equal to the one on main is not later
    let (r, _) = amended(1, MERGE_SECS + 2, false);
    assert!(!r.mapping[2].later);
    // an equal patch is the landed version whatever the committer time says (rebased again after landing)
    let (r, _) = amended(0, MERGE_SECS + 3600, true);
    assert_eq!(r.mapping[2].method, Method::PatchId);
    assert!(!r.mapping[2].later);
    assert_eq!(r.strength, Strength::Strong);
}

#[test]
fn an_empty_commit_against_a_commit_with_changes_is_not_a_proof() {
    let fx = Fx::new();
    let b = base(&fx);
    let es = Stamp::new(NAME, MAIL, T0);
    let empty = fx.commit_at(Some(b), &[], "step 0", es, es);
    let l = land(&fx, b, &[c(0)], MERGE_SECS);
    let r = run(&fx, last(&l), empty);
    assert_eq!(r.mapped, 1);
    assert_eq!(r.mapping[0].value, Some(0));
    assert!(!r.content_ok);
    assert_eq!(r.strength, Strength::Hint);
}

#[test]
fn a_commit_too_large_to_compare_is_not_vouched_for() {
    let fx = Fx::new();
    let b = base(&fx);
    let huge = "x".repeat(1_100_000) + "\n";
    let one = C { body: huge, ..c(0) };
    let br = branch(&fx, b, std::slice::from_ref(&one));
    let l = land(&fx, b, &[one], MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(r.mapped, 1);
    assert_eq!(r.mapping[0].value, None);
    assert!(!r.content_ok);
    assert_eq!(r.strength, Strength::Hint);
}

/// c0, a commit of somebody else with the content of c1, c2: on main, and the branch c0, c1, c2.
fn between(foreign_body: Option<String>) -> RebaseReport {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let l1 = land(&fx, b, &list[..1], MERGE_SECS);
    let other = C {
        name: "Other",
        mail: "o@example.com",
        subject: b"unrelated".to_vec(),
        body: foreign_body.unwrap_or_else(|| list[1].body.clone()),
        ..list[1].clone()
    };
    let g = land(&fx, last(&l1), &[other], MERGE_SECS + 10);
    let l2 = land(&fx, last(&g), &list[2..], MERGE_SECS + 20);
    run(&fx, last(&l2), last(&br))
}

#[test]
fn a_gap_of_one_between_two_mapped_commits_is_filled_by_similarity() {
    let r = between(None);
    assert_eq!(mapped_methods(&r), ["identity", "similarity", "identity"]);
    assert_eq!(r.mapping[1].value, Some(1000));
    assert_eq!(r.mapping[1].position, Some(1));
    assert!(!r.mapping[1].identity);
    assert_eq!((r.by_identity, r.by_similarity, r.mapped), (2, 1, 3));
    assert_eq!(r.strength, Strength::Strong);
    assert_eq!(r.runs, 1);
    // below the low threshold of 70 % the commit stays unmapped
    let r = between(Some(edited("c1", 8))); // 12 of 20 equal: 60.0 %
    assert_eq!(r.mapped, 2);
    assert!(r.mapping[1].main.is_none());
    assert_eq!(r.strength, Strength::Hint);
    // exactly 70 % is enough
    let r = between(Some(edited("c1", 6))); // 14 of 20 equal
    assert_eq!(r.mapping[1].method, Method::Similarity);
    assert_eq!(r.mapping[1].value, Some(700));
}

#[test]
fn similarity_is_not_used_without_the_neighbours_on_both_sides_of_one_free_position() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let l1 = land(&fx, b, &list[..1], MERGE_SECS);
    let x = foreign(&fx, last(&l1), 1);
    let twin = C {
        name: "Other",
        mail: "o@example.com",
        subject: b"unrelated".to_vec(),
        ..list[1].clone()
    };
    let g = land(&fx, x, std::slice::from_ref(&twin), MERGE_SECS + 10);
    let l2 = land(&fx, last(&g), &list[2..], MERGE_SECS + 20);
    // two free positions between the neighbours: the content twin is not tied to them
    let r = run(&fx, last(&l2), last(&br));
    assert_eq!(r.mapped, 2);
    assert!(r.mapping[1].main.is_none());
    assert_eq!(r.by_similarity, 0);
    // the last commit has no neighbour behind it
    let l3 = land(&fx, b, &list[..2], MERGE_SECS);
    let g3 = land(
        &fx,
        last(&l3),
        &[C {
            name: "Other",
            mail: "o@example.com",
            subject: b"unrelated".to_vec(),
            ..list[2].clone()
        }],
        MERGE_SECS + 10,
    );
    let r = run(&fx, last(&g3), last(&br));
    assert_eq!(r.mapped, 2);
    assert_eq!(r.by_similarity, 0);
}

#[test]
fn the_mapping_has_one_row_per_commit_with_masked_subjects() {
    let fx = Fx::new();
    let b = base(&fx);
    let mut list = cs(3);
    list[0].subject = b"bad \x1b[31m \xff end \xe2\x80\xae".to_vec();
    list[1].subject = format!("long {}", "x".repeat(200)).into_bytes();
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(r.mapping.len(), 3);
    assert_eq!(r.mapped, 3);
    assert_eq!(r.mapping[0].subject, "bad \\u{1b}[31m \\xff end \\u{202e}");
    assert_eq!(r.mapping[1].subject.chars().count(), RB_SUBJECT_CHARS + 1);
    assert!(r.mapping[1].subject.ends_with('\u{2026}'));
    assert_eq!(r.mapping[2].subject, "step 2");
}

#[test]
fn what_is_not_mapped_is_listed_with_its_files() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let big_files: Vec<String> = (0..25).map(|k| format!("d/{k:02}.txt")).collect();
    let edits: Vec<(&str, Option<&str>)> = big_files
        .iter()
        .map(|f| (f.as_str(), Some("x\n")))
        .collect();
    let s = Stamp::new(NAME, MAIL, T0 + 300);
    let big = fx.commit_at(
        Some(last(&branch(&fx, b, &list))),
        &edits,
        "big\x1b[0m change",
        s,
        s,
    );
    let br_tip = big;
    let l = land(&fx, b, &list[..2], MERGE_SECS);
    let r = run(&fx, last(&l), br_tip);
    assert_eq!((r.n, r.mapped, r.unmapped), (4, 2, 2));
    assert_eq!(r.remaining.len(), 2);
    assert_eq!(r.remaining[0].subject, "step 2");
    assert_eq!(r.remaining[0].files, ["f2.txt"]);
    assert_eq!(r.remaining[0].files_total, 1);
    assert_eq!(r.remaining[1].subject, "big\\u{1b}[0m change");
    assert_eq!(r.remaining[1].files.len(), RB_MAX_REMAINING_FILES);
    assert_eq!(r.remaining[1].files_total, 25);
    assert_eq!(r.remaining[1].files[0], "d/00.txt");
    assert_eq!(r.remaining[1].files[19], "d/19.txt");
    assert_eq!(r.remaining[1].commit, br_tip);
}

#[test]
fn nothing_remains_when_everything_is_mapped_and_ambiguous_commits_remain() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(2);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    assert!(run(&fx, last(&l), last(&br)).remaining.is_empty());
    let tw = &twins()[..2];
    let br = branch(&fx, b, tw);
    let l = land(&fx, b, tw, MERGE_SECS);
    let r = run(&fx, last(&l), last(&br));
    assert_eq!(
        r.remaining.len(),
        2,
        "an ambiguous commit is not mapped: it remains"
    );
}

#[test]
fn the_same_input_gives_the_same_report() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = twins();
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let a = run_hits(&fx, last(&l), last(&br));
    let c = run_hits(&fx, last(&l), last(&br));
    assert_eq!(a.mapping, c.mapping);
    assert_eq!(a.remaining, c.remaining);
}

// ---- (d) the pull request, landed_via ----

fn pr_commit(c: &C, id: ObjectId) -> PrCommit {
    PrCommit {
        id,
        name: c.name.as_bytes().to_vec(),
        mail: c.mail.as_bytes().to_vec(),
        secs: c.secs,
        subject: c.subject.clone(),
    }
}

fn pr(list: &[C], ids: &[ObjectId], merge_commit: ObjectId) -> PrInput {
    PrInput {
        number: 7,
        merged: true,
        merge_commit,
        merged_at: Some(MERGE_SECS + 5),
        head: last(ids),
        commits: list
            .iter()
            .zip(ids)
            .map(|(c, i)| pr_commit(c, *i))
            .collect(),
    }
}

fn api(fx: &Fx, base_tip: ObjectId, tip: ObjectId, input: PrInput) -> ApiReport {
    let repo = fx.repo();
    let side = branch_side(&repo, base_tip, tip).unwrap();
    evaluate_api(&repo, base_tip, &side, tip, &ApiInput::Pr(input)).unwrap()
}

#[test]
fn a_pull_request_with_one_parent_and_identity_equal_commits_is_a_rebase() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let f = foreign(&fx, last(&l), 1);
    let rep = api(&fx, f, last(&br), pr(&list, &br, last(&l)));
    assert_eq!(rep.not_read, None);
    assert_eq!(rep.kind, LandedKind::Rebase);
    assert_eq!(
        (rep.pr, rep.k, rep.merge_parents),
        (Some(7), Some(3), Some(1))
    );
    assert_eq!(rep.merge_commit, Some(last(&l)));
    assert_eq!(
        (rep.tip_matches_run, rep.head_in_branch),
        (Some(true), Some(true))
    );
    assert_eq!(rep.mapping.len(), 3);
    for (j, e) in rep.mapping.iter().enumerate() {
        assert_eq!(
            (e.commit, e.main, e.method),
            (br[j], Some(l[j]), Method::Api)
        );
        assert_eq!(
            e.position,
            Some(3 - j as u32),
            "counted from the base tip: one foreign commit first"
        );
    }
    assert!(rep.remaining.is_empty());
    let via = landed_via(false, None, Some(&rep));
    assert_eq!(
        (via.kind, via.basis, via.strength),
        (LandedKind::Rebase, Basis::PrApi, Strength::Strong)
    );
}

#[test]
fn commits_after_the_pull_request_head_remain() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(4);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list[..3], MERGE_SECS);
    let mut input = pr(&list[..3], &br[..3], last(&l));
    let rep = api(&fx, last(&l), last(&br), input.clone());
    assert_eq!(rep.kind, LandedKind::Rebase);
    assert_eq!(rep.mapping.len(), 3);
    assert_eq!(rep.remaining.len(), 1);
    assert_eq!(rep.remaining[0].commit, br[3]);
    assert_eq!(rep.remaining[0].files, ["f3.txt"]);
    assert_eq!(rep.head_in_branch, Some(true));
    // a head that is not in the branch (the branch was rewritten after the merge)
    input.head = foreign(&fx, b, 9);
    let rep = api(&fx, last(&l), last(&br), input);
    assert_eq!(rep.head_in_branch, Some(false));
}

#[test]
fn a_merge_commit_with_two_parents_is_a_merge() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(2);
    let br = branch(&fx, b, &list);
    let f = foreign(&fx, b, 1);
    let s = Stamp::new("M", "m@example.com", MERGE_SECS);
    let m = fx.commit_at_parents(
        vec![f, last(&br)],
        Some(last(&br)),
        &[],
        "Merge pull request #7",
        s,
        s,
    );
    let rep = api(&fx, m, last(&br), pr(&list, &br, m));
    assert_eq!(rep.kind, LandedKind::Merge);
    assert_eq!(rep.merge_parents, Some(2));
    assert!(rep.mapping.is_empty());
    let via = landed_via(false, None, Some(&rep));
    assert_eq!(
        (via.kind, via.basis, via.strength),
        (LandedKind::Merge, Basis::PrApi, Strength::Strong)
    );
}

#[test]
fn a_commit_with_the_time_of_the_merge_is_a_squash() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let m = Stamp::new("Merger", "merger@example.com", MERGE_SECS + 5);
    let mut all = String::new();
    for c in &list {
        all += &c.body;
    }
    let sq_author = Stamp::new("GitHub", "noreply@github.com", MERGE_SECS + 6);
    let sq = fx.commit_at(
        Some(b),
        &[("all.txt", Some(all.as_str()))],
        "step 2 (#7)",
        sq_author,
        m,
    );
    let rep = api(&fx, sq, last(&br), pr(&list, &br, sq));
    assert_eq!(
        (rep.kind, rep.tip_matches_run),
        (LandedKind::Squash, Some(false))
    );
    let via = landed_via(false, None, Some(&rep));
    assert_eq!(
        (via.kind, via.basis, via.strength),
        (LandedKind::Squash, Basis::PrApi, Strength::Strong)
    );
    // three seconds off is not the merge time any more
    let sq_author = Stamp::new("GitHub", "noreply@github.com", MERGE_SECS + 8);
    let sq2 = fx.commit_at(
        Some(b),
        &[("all.txt", Some(all.as_str()))],
        "step 2 (#7)",
        sq_author,
        m,
    );
    let rep = api(&fx, sq2, last(&br), pr(&list, &br, sq2));
    assert_eq!(rep.kind, LandedKind::Unknown);
    let mut no_time = pr(&list, &br, sq);
    no_time.merged_at = None;
    assert_eq!(api(&fx, sq, last(&br), no_time).kind, LandedKind::Unknown);
}

#[test]
fn the_commits_behind_the_merge_commit_must_equal_the_pull_request_commits() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let mut other = list.clone();
    other[2].subject = b"another subject".to_vec();
    let l = land(&fx, b, &other, MERGE_SECS);
    let mut input = pr(&list, &br, last(&l));
    input.merged_at = Some(MERGE_SECS - 5_000);
    let rep = api(&fx, last(&l), last(&br), input);
    assert_eq!(rep.tip_matches_run, Some(false));
    assert_eq!(rep.kind, LandedKind::Unknown);
    assert!(rep.mapping.is_empty());
    // fewer commits on main than the pull request has
    let l = land(&fx, b, &list[..2], MERGE_SECS);
    let mut input = pr(&list, &br, last(&l));
    input.merged_at = None;
    assert_eq!(
        api(&fx, last(&l), last(&br), input).kind,
        LandedKind::Unknown
    );
    // no commits at all
    let mut empty = pr(&list, &br, last(&l));
    empty.commits.clear();
    empty.merged_at = None;
    assert_eq!(
        api(&fx, last(&l), last(&br), empty).kind,
        LandedKind::Unknown
    );
}

#[test]
fn a_shallow_boundary_below_the_merge_commit_is_not_an_error() {
    let fx = Fx::new();
    let (edge, _) = shallow_edge(&fx);
    let list = cs(5);
    let b = base(&fx);
    let br = branch(&fx, b, &list);
    let l = land(&fx, edge, &list[2..], MERGE_SECS + 10);
    // the pull request has 5 commits, main shows 3, the boundary commit and then nothing readable
    let mut input = pr(&list, &br, last(&l));
    input.merged_at = None;
    let rep = api(&fx, last(&l), last(&br), input);
    assert_eq!(rep.not_read, None);
    assert_eq!(rep.kind, LandedKind::Unknown);
    assert_eq!(rep.tip_matches_run, Some(false));
}

#[test]
fn without_a_pull_request_mapping_the_reason_is_kept_and_nothing_fails() {
    let fx = Fx::new();
    let b = base(&fx);
    let br = branch(&fx, b, &cs(1));
    let repo = fx.repo();
    let side = branch_side(&repo, b, last(&br)).unwrap();
    for reason in [
        ApiReason::Offline,
        ApiReason::NoToken,
        ApiReason::NoPullRequest,
        ApiReason::RateLimited,
    ] {
        let rep = evaluate_api(&repo, b, &side, last(&br), &ApiInput::NotRead(reason)).unwrap();
        assert_eq!(rep.not_read, Some(reason));
        assert_eq!(rep.kind, LandedKind::Unknown);
        assert!(rep.mapping.is_empty() && rep.pr.is_none());
    }
    assert_eq!(ApiReason::Offline.as_str(), "offline");
    assert_eq!(ApiReason::RateLimited.as_str(), "rate-limited");
    assert_eq!(
        ApiReason::MergeCommitNotInClone.as_str(),
        "merge-commit-not-in-clone"
    );
}

#[test]
fn a_pull_request_that_cannot_be_checked_locally_says_why() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(2);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let good = pr(&list, &br, last(&l));
    let mut open = good.clone();
    open.merged = false;
    assert_eq!(
        api(&fx, last(&l), last(&br), open).not_read,
        Some(ApiReason::NotMerged)
    );
    let mut ghost = good.clone();
    ghost.merge_commit = "1234567890123456789012345678901234567890".parse().unwrap();
    assert_eq!(
        api(&fx, last(&l), last(&br), ghost).not_read,
        Some(ApiReason::MergeCommitNotInClone)
    );
    // a commit that is not on the first-parent chain of the base
    let side = foreign(&fx, b, 3);
    let mut off = good.clone();
    off.merge_commit = side;
    assert_eq!(
        api(&fx, last(&l), last(&br), off).not_read,
        Some(ApiReason::NotOnBase)
    );
    assert_eq!(api(&fx, last(&l), last(&br), good).not_read, None);
}

#[test]
fn landed_via_follows_the_precedence() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(3);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list, MERGE_SECS);
    let strong = run(&fx, last(&l), last(&br));
    assert_eq!(strong.strength, Strength::Strong);
    let nothing = landed_via(false, None, None);
    assert_eq!(
        (nothing.kind, nothing.basis, nothing.strength),
        (LandedKind::Unknown, Basis::None, Strength::None)
    );
    let v = landed_via(true, Some(&strong), None);
    assert_eq!(
        (v.kind, v.basis, v.strength),
        (LandedKind::Merge, Basis::Ancestor, Strength::Strong)
    );
    let v = landed_via(false, Some(&strong), None);
    assert_eq!(
        (v.kind, v.basis, v.strength),
        (LandedKind::Rebase, Basis::CommitMap, Strength::Strong)
    );
    // a pull request that was not read says nothing
    let mut squash = evaluate_api(
        &fx.repo(),
        b,
        &branch_side(&fx.repo(), b, last(&br)).unwrap(),
        last(&br),
        &ApiInput::NotRead(ApiReason::Offline),
    )
    .unwrap();
    squash.kind = LandedKind::Squash;
    assert_eq!(
        landed_via(false, None, Some(&squash)).kind,
        LandedKind::Unknown
    );
    // a strong run beats a pull request that looks like a squash
    squash.not_read = None;
    assert_eq!(
        landed_via(false, Some(&strong), Some(&squash)).kind,
        LandedKind::Rebase
    );
    assert_eq!(
        landed_via(false, None, Some(&squash)).kind,
        LandedKind::Squash
    );
}

#[test]
fn a_partial_or_weak_mapping_is_unknown() {
    let fx = Fx::new();
    let b = base(&fx);
    let list = cs(5);
    let br = branch(&fx, b, &list);
    let l = land(&fx, b, &list[..3], MERGE_SECS);
    let partial = run(&fx, last(&l), last(&br));
    assert!(partial.partial);
    let v = landed_via(false, Some(&partial), None);
    assert_eq!(
        (v.kind, v.basis, v.strength),
        (LandedKind::Unknown, Basis::None, Strength::None)
    );
    // identity without a run
    let f = foreign(&fx, last(&l), 1);
    let l2 = land(&fx, f, &list[3..], MERGE_SECS + 50);
    let hint = run(&fx, last(&l2), last(&br));
    assert_eq!((hint.mapped, hint.strength), (5, Strength::Hint));
    assert_eq!(
        landed_via(false, Some(&hint), None).kind,
        LandedKind::Unknown
    );
}

#[test]
fn one_commit_with_an_equal_patch_and_no_identity_is_a_hint_for_a_squash() {
    let fx = Fx::new();
    let b = base(&fx);
    let one = cs(1);
    let br = branch(&fx, b, &one);
    let mut other = one.clone();
    other[0].secs += 40;
    let l = land(&fx, b, &other, MERGE_SECS);
    let r = run_hits(&fx, last(&l), last(&br));
    let v = landed_via(false, Some(&r), None);
    assert_eq!(
        (v.kind, v.basis, v.strength),
        (LandedKind::Unknown, Basis::SingleCommitMap, Strength::Hint)
    );
    // three such commits are not that case
    let three = cs(3);
    let br = branch(&fx, b, &three);
    let mut shifted = three.clone();
    for c in &mut shifted {
        c.secs += 40;
    }
    let l = land(&fx, b, &shifted, MERGE_SECS);
    let r = run_hits(&fx, last(&l), last(&br));
    assert_eq!(landed_via(false, Some(&r), None).basis, Basis::None);
}
