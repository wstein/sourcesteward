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

#[test]
fn the_walk_stops_a_day_before_the_oldest_author_time_and_not_earlier() {
    let fx = Fx::new();
    let b = base(&fx);
    let old = fx.commit_at(
        Some(b),
        &[("old.txt", Some("o\n"))],
        "old",
        Stamp::new("Other", "o@example.com", 1_000),
        Stamp::new("Other", "o@example.com", 1_000),
    );
    // a committer clock an hour behind the author time is inside the slack
    let l = land(&fx, old, &cs(2), T0 - 3600);
    let repo = fx.repo();
    let idx = RebaseIndex::build(&repo, last(&l), Some(T0), RB_MAX_WINDOW).unwrap();
    assert_eq!(idx.stop(), Stop::Time);
    assert_eq!(
        idx.len(),
        2,
        "both landed commits are indexed, the old ones are not"
    );
    assert!(idx.pos_of(old).is_none());
    // exactly at the bound is still indexed
    let l2 = land(&fx, old, &cs(1), T0 - RB_SLACK_SECS);
    let idx = RebaseIndex::build(&repo, last(&l2), Some(T0), RB_MAX_WINDOW).unwrap();
    assert_eq!(idx.len(), 1, "the commit at the bound and nothing older");
    let l3 = land(&fx, old, &cs(1), T0 - RB_SLACK_SECS - 1);
    let idx = RebaseIndex::build(&repo, last(&l3), Some(T0), RB_MAX_WINDOW).unwrap();
    assert_eq!(idx.len(), 0, "one second below the bound is not indexed");
    // without a time bound the chain is walked to its root
    let idx = RebaseIndex::build(&repo, last(&l), None, RB_MAX_WINDOW).unwrap();
    assert_eq!((idx.len(), idx.stop()), (4, Stop::Root));
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
