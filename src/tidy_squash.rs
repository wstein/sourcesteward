//! Squash evidence, layer 1 (package 5a), for `stw tidy plan`; specification: `scratch/stw-tidy-squash/spec.md`, summary
//! in `spikes/stw/tidy-similarity/README.md` (package 5a). Pure library functions: no CLI, no plan, no network.
//!
//! A squash merge writes the whole branch as one commit on the base: new ID, one parent, the author time of the merge,
//! mostly the pull request number in the subject. Layer 1 reads what is cheap and exact: the merged pull request of the
//! branch (`evaluate_pr`: where its merge commit is, how far the branch moved after the pull request head) and the
//! subject of the commits on the base (`evaluate_subject`: the pull request number, the subject of a branch commit).
//! Neither proves that the content landed: that is layer 2 (package 5b). Nothing here decides anything.
use std::collections::{HashMap, HashSet};

use gix::bstr::ByteSlice;
use gix::ObjectId;

use crate::error::Result;
use crate::github::AnyPr;
use crate::repo;
use crate::tidy_rebase::{read_head, subject_of, RebaseIndex, RB_SUBJECT_CHARS};
use crate::tidy_similarity::{mask_bytes, trim_line};

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

// ---- the subject ----

/// The number `N` of a subject that ends on `(#N)` (white space at the end is ignored); `None` for anything else.
pub fn pr_number_suffix(subject: &[u8]) -> Option<u64> {
    split_suffix(subject).map(|(_, n)| n)
}

/// `(subject without the suffix, N)` for a subject that ends on `(#N)`.
fn split_suffix(subject: &[u8]) -> Option<(&[u8], u64)> {
    let s = trim_line(subject);
    let inner = s.strip_suffix(b")")?;
    let open = inner.windows(2).rposition(|w| w == b"(#")?;
    let digits = &inner[open + 2..];
    if digits.is_empty() || !digits.iter().all(u8::is_ascii_digit) {
        return None;
    }
    let n = std::str::from_utf8(digits).ok()?.parse().ok()?;
    Some((&s[..open], n))
}

/// A subject reduced to its title for comparison: the suffix ` (#N)` of a GitHub squash, the prefix `<digits>: ` of
/// a JBS issue (openjdk) and leading `[group]` words (llvm) are removed, white space at the ends is trimmed. A subject
/// that is nothing but such decoration has an empty title, which is never compared.
pub fn normalize(subject: &[u8]) -> Vec<u8> {
    let mut s = trim_line(subject);
    if let Some((rest, _)) = split_suffix(s) {
        s = trim_line(rest);
    }
    loop {
        let digits = s.iter().take_while(|b| b.is_ascii_digit()).count();
        if digits > 0 && (s[digits..].starts_with(b": ") || &s[digits..] == b":") {
            s = trim_line(&s[digits + 1..]);
            continue;
        }
        if s.starts_with(b"[") {
            if let Some(close) = s.iter().position(|b| *b == b']') {
                s = trim_line(&s[close + 1..]);
                continue;
            }
        }
        break;
    }
    s.to_vec()
}

/// The first-parent chain of the base with the indexes the subject evidence needs (no diff is read).
pub struct SquashIndex {
    chain: RebaseIndex,
    /// Title -> positions on the chain (ascending: newest first) of the non-merge commits with it.
    by_title: HashMap<Vec<u8>, Vec<u32>>,
    /// Number of `(#N)` at the end of a subject -> positions of the non-merge commits.
    by_number: HashMap<u64, Vec<u32>>,
}

impl SquashIndex {
    /// Walk the chain from `base_tip` (`RebaseIndex::build`: `since` is the oldest author time wanted, at most
    /// `max_window` commits).
    pub fn build(
        repo: &gix::Repository,
        base_tip: ObjectId,
        since: Option<i64>,
        max_window: usize,
    ) -> Result<SquashIndex> {
        let chain = RebaseIndex::build(repo, base_tip, since, max_window)?;
        let mut by_title: HashMap<Vec<u8>, Vec<u32>> = HashMap::new();
        let mut by_number: HashMap<u64, Vec<u32>> = HashMap::new();
        for pos in 0..chain.len() as u32 {
            let Some(h) = chain.head(pos).filter(|h| h.parents <= 1) else {
                continue;
            };
            let title = normalize(&h.key.subject);
            // a title that is empty is never looked up (`evaluate_subject` leaves it out of the titles of the branch)
            by_title.entry(title).or_default().push(pos);
            if let Some(n) = pr_number_suffix(&h.key.subject) {
                by_number.entry(n).or_default().push(pos);
            }
        }
        Ok(SquashIndex {
            chain,
            by_title,
            by_number,
        })
    }

    /// Commits of the chain that were read.
    pub fn len(&self) -> usize {
        self.chain.len()
    }

    pub fn is_empty(&self) -> bool {
        self.chain.is_empty()
    }

    /// The walk ended at the window limit: older commits were not looked at.
    pub fn window_cut(&self) -> bool {
        self.chain.window_cut()
    }
}

/// What the subject evidence needs of the branch: the subjects of its non-merge commits and its oldest author time.
#[derive(Clone, Debug, Default)]
pub struct BranchCommits {
    /// Subjects (first line, white space at the right removed) of the non-merge commits of `base..tip`.
    pub subjects: Vec<Vec<u8>>,
    /// The oldest author time over all commits of `base..tip` (merges included).
    pub oldest_author_secs: Option<i64>,
}

pub fn branch_commits(
    repo: &gix::Repository,
    base_tip: ObjectId,
    tip: ObjectId,
) -> Result<BranchCommits> {
    let mut out = BranchCommits::default();
    for info in repo.rev_walk([tip]).with_hidden([base_tip]).all()? {
        let h = read_head(repo, info?.id)?;
        out.oldest_author_secs = Some(
            out.oldest_author_secs
                .map_or(h.key.secs, |o| o.min(h.key.secs)),
        );
        if h.parents <= 1 {
            out.subjects.push(h.key.subject);
        }
    }
    Ok(out)
}

/// The pull request as the subject evidence uses it.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct PrRef {
    pub number: u64,
    /// The merge commit of the pull request where it is on the base with one parent (`merge_commit_on_base`).
    pub merge_commit: Option<ObjectId>,
}

/// How the candidate was found, strongest first.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SubjectBasis {
    /// The merge commit of the pull request, on the base with one parent: the sure candidate.
    MergeCommit,
    /// The subject ends on `(#N)` with the number of the pull request.
    PrNumber,
    /// The title of the subject equals the title of a commit of the branch.
    Subject,
}

impl SubjectBasis {
    pub fn as_str(self) -> &'static str {
        match self {
            SubjectBasis::MergeCommit => "merge-commit",
            SubjectBasis::PrNumber => "pr-number",
            SubjectBasis::Subject => "subject",
        }
    }
}

/// The subject evidence for one branch.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SubjectEvidence {
    pub candidate: Option<ObjectId>,
    pub basis: Option<SubjectBasis>,
    /// The subject of the candidate, masked (control, direction characters, bytes that are not UTF-8) and cut.
    pub candidate_subject: String,
    /// Commits on the base that the basis found (before one was chosen).
    pub candidates: usize,
    /// The number at the end of the subject of the candidate, whatever it is.
    pub pr_number_in_subject: Option<u64>,
    /// The title of the candidate equals the title of a commit of the branch.
    pub subject_equal: bool,
    pub subjects_total: usize,
    /// Commits of the branch whose subject is a line `* <subject>` of the body of the candidate.
    pub subjects_in_body: usize,
    /// The body of the candidate has `Pull-request: <url>/pull/N` with the number of the pull request.
    pub pull_request_trailer: bool,
    /// Commits of the base that were read, and whether the walk ended at its limit.
    pub window_commits: usize,
    pub window_cut: bool,
}

/// `Pull-request: .../pull/N` (spr, llvm): the number `N`.
fn trailer_number(line: &[u8]) -> Option<u64> {
    let url = trim_line(line.strip_prefix(b"Pull-request:")?);
    let mut parts = url.rsplit(|b| *b == b'/');
    let n = parts.next()?;
    if parts.next()? != b"pull" || n.is_empty() || !n.iter().all(u8::is_ascii_digit) {
        return None;
    }
    std::str::from_utf8(n).ok()?.parse().ok()
}

/// The subject evidence of a branch. A candidate is a non-merge commit on the first-parent chain of the base whose
/// author time is at most `window_secs` older than the oldest commit of the branch (newer is fine). In this order:
/// the merge commit of the pull request (`pr.merge_commit`); a commit whose subject ends on `(#N)` with the number
/// of the pull request; a commit whose title equals the title of a commit of the branch. Of several the oldest counts
/// (ties by commit ID). This is no proof that the content landed.
pub fn evaluate_subject(
    repo: &gix::Repository,
    idx: &SquashIndex,
    bc: &BranchCommits,
    pr: Option<&PrRef>,
    window_secs: i64,
) -> Result<SubjectEvidence> {
    let floor = bc.oldest_author_secs.map(|o| o.saturating_sub(window_secs));
    let in_window = |pos: &u32| {
        idx.chain
            .head(*pos)
            .is_some_and(|h| floor.is_none_or(|f| h.key.secs >= f))
    };
    let oldest = |list: Vec<u32>| -> (Option<ObjectId>, usize) {
        let n = list.len();
        let best = list
            .into_iter()
            .filter_map(|p| idx.chain.head(p))
            .min_by_key(|h| (h.key.secs, h.id))
            .map(|h| h.id);
        (best, n)
    };
    let titles: HashSet<Vec<u8>> = bc
        .subjects
        .iter()
        .map(|s| normalize(s))
        .filter(|t| !t.is_empty())
        .collect();
    let mut found: (Option<ObjectId>, usize, Option<SubjectBasis>) = (None, 0, None);
    if let Some(m) = pr.and_then(|p| p.merge_commit) {
        found = (Some(m), 1, Some(SubjectBasis::MergeCommit));
    }
    if found.0.is_none() {
        if let Some(p) = pr {
            let list: Vec<u32> = idx
                .by_number
                .get(&p.number)
                .into_iter()
                .flatten()
                .copied()
                .filter(in_window)
                .collect();
            let (id, n) = oldest(list);
            if id.is_some() {
                found = (id, n, Some(SubjectBasis::PrNumber));
            }
        }
    }
    if found.0.is_none() {
        let mut list: Vec<u32> = titles
            .iter()
            .filter_map(|t| idx.by_title.get(t))
            .flatten()
            .copied()
            .filter(in_window)
            .collect();
        list.sort_unstable();
        list.dedup();
        let (id, n) = oldest(list);
        if id.is_some() {
            found = (id, n, Some(SubjectBasis::Subject));
        }
    }
    let mut e = SubjectEvidence {
        candidate: found.0,
        basis: found.2,
        candidate_subject: String::new(),
        candidates: found.1,
        pr_number_in_subject: None,
        subject_equal: false,
        subjects_total: bc.subjects.len(),
        subjects_in_body: 0,
        pull_request_trailer: false,
        window_commits: idx.len(),
        window_cut: idx.window_cut(),
    };
    if let Some(id) = found.0 {
        let commit = repo.find_commit(id)?;
        let msg = commit.message_raw()?;
        let subject = subject_of(msg);
        e.candidate_subject = mask_bytes(&subject, RB_SUBJECT_CHARS);
        e.pr_number_in_subject = pr_number_suffix(&subject);
        let title = normalize(&subject);
        e.subject_equal = !title.is_empty() && titles.contains(&title);
        let mut listed: HashSet<&[u8]> = HashSet::new();
        for line in msg.lines().skip(1) {
            let line = trim_line(line);
            if let Some(item) = line.strip_prefix(b"* ") {
                listed.insert(trim_line(item));
            }
            if let (Some(n), Some(p)) = (trailer_number(line), pr) {
                e.pull_request_trailer |= n == p.number;
            }
        }
        e.subjects_in_body = bc
            .subjects
            .iter()
            .filter(|s| !s.is_empty() && listed.contains(trim_line(s)))
            .count();
    }
    Ok(e)
}
