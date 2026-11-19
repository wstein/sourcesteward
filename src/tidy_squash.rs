//! Squash evidence, layer 1 (package 5a), for `stw tidy plan`; specification: `scratch/stw-tidy-squash/spec.md`, summary
//! in `spikes/stw/tidy-similarity/README.md` (package 5a). Pure library functions: no CLI, no plan, no network.
//!
//! A squash merge writes the whole branch as one commit on the base: new ID, one parent, the author time of the merge,
//! mostly the pull request number in the subject. Layer 1 reads what is cheap and exact: the merged pull request of the
//! branch (`evaluate_pr`: where its merge commit is, how far the branch moved after the pull request head) and the
//! subject of the commits on the base (`evaluate_subject`: the pull request number, the subject of a branch commit).
//! Neither proves that the content landed: that is layer 2 (package 5b). Nothing here decides anything.
use gix::ObjectId;

use crate::github::AnyPr;
use crate::repo;

/// The merged pull request of a branch, read from the list of pull requests (no request of its own).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct PrEvidence {
    /// `None`: the pull request was read; `Some(reason)`: there is none to read.
    pub not_read: Option<crate::tidy_rebase::ApiReason>,
    pub number: Option<u64>,
    /// `merge_commit_sha` of the pull request; `None` when GitHub sent none or it is no object ID.
    pub merge_commit: Option<ObjectId>,
    /// The merge commit is in the clone; `None` without a merge commit.
    pub merge_commit_in_clone: Option<bool>,
    /// The merge commit is an ancestor of the base tip and has exactly one parent (a squash or a one-commit rebase;
    /// two parents are a real merge commit, a different case). `None` where the commit is not in the clone.
    pub merge_commit_on_base: Option<bool>,
    pub merge_commit_parents: Option<usize>,
    /// The pull request head is the branch tip.
    pub head_is_tip: Option<bool>,
    /// The pull request head is the branch tip or an ancestor of it; `None` where the head is not in the clone.
    pub head_in_branch: Option<bool>,
    /// Commits of the branch that are not in the pull request head; `None` unless the head is in the branch.
    pub commits_after_head: Option<usize>,
}

impl PrEvidence {
    pub fn not_read(reason: crate::tidy_rebase::ApiReason) -> PrEvidence {
        PrEvidence {
            not_read: Some(reason),
            number: None,
            merge_commit: None,
            merge_commit_in_clone: None,
            merge_commit_on_base: None,
            merge_commit_parents: None,
            head_is_tip: None,
            head_in_branch: None,
            commits_after_head: None,
        }
    }
}

fn parse_id(hex: &str) -> Option<ObjectId> {
    ObjectId::from_hex(hex.as_bytes()).ok()
}

/// Whether `a` is an ancestor of `b`; `None` where a missing object (a shallow or partial clone) stops the walk.
fn ancestor(repo: &gix::Repository, a: ObjectId, b: ObjectId) -> Option<bool> {
    if !repo.has_object(a) || !repo.has_object(b) {
        return None;
    }
    repo::is_ancestor(repo, a, b).ok()
}

/// What the merged pull request `pr` says about the branch with tip `tip` and the base with tip `base_tip`. A walk
/// that fails on a missing object leaves its answer `None`.
pub fn evaluate_pr(
    repo: &gix::Repository,
    base_tip: ObjectId,
    tip: ObjectId,
    pr: &AnyPr,
) -> PrEvidence {
    let mut e = PrEvidence {
        not_read: None,
        number: Some(pr.number),
        merge_commit: None,
        merge_commit_in_clone: None,
        merge_commit_on_base: None,
        merge_commit_parents: None,
        head_is_tip: None,
        head_in_branch: None,
        commits_after_head: None,
    };
    if let Some(m) = parse_id(&pr.merge_commit_sha) {
        e.merge_commit = Some(m);
        let here = repo.has_object(m);
        e.merge_commit_in_clone = Some(here);
        if here {
            e.merge_commit_parents = repo.find_commit(m).ok().map(|c| c.parent_ids().count());
            e.merge_commit_on_base = match (ancestor(repo, m, base_tip), e.merge_commit_parents) {
                (Some(anc), Some(parents)) => Some(anc && parents == 1),
                _ => None,
            };
        }
    }
    if let Some(head) = parse_id(&pr.head_sha) {
        e.head_is_tip = Some(head == tip);
        e.head_in_branch = ancestor(repo, head, tip);
        if e.head_in_branch == Some(true) {
            e.commits_after_head = repo::count_range(repo, tip, head).ok();
        }
    }
    e
}

/// The merged pull request of `branch` that the evidence is read from: head branch `branch`, state merged, head in the
/// repository `slug` itself (a fork with the same branch name, or a deleted fork, whose `head.repo` is null, is not
/// this branch; the names are compared without regard to case; without a `slug` nothing is compared). `prefer` (the
/// pull request the plan already names) wins where it qualifies; else the latest merge time, then the higher number.
pub fn pick_pr<'a>(
    prs: &'a [AnyPr],
    branch: &str,
    slug: Option<&str>,
    prefer: Option<u64>,
) -> Option<&'a AnyPr> {
    let mut usable = prs.iter().filter(|p| {
        p.head_ref == branch
            && p.state == "MERGED"
            && slug.is_none_or(|s| p.head_repo.eq_ignore_ascii_case(s))
    });
    let first = usable.clone().find(|p| Some(p.number) == prefer);
    first.or_else(|| usable.by_ref().max_by_key(|p| (p.merged_at, p.number)))
}
