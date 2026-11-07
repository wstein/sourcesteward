// SPDX-License-Identifier: EUPL-1.2

// Package landcmd wires `stw land --stack`: flag validation, the GitHub read adapter, mode
// detection, the output and the exit codes.
package landcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/land"
)

// Reader is the one read the adapter needs; *hosting.WatchReader built by hosting.NewLandReader
// implements it and its guard admits the land read shapes only.
type Reader interface {
	LandRead(ctx context.Context, endpoint string) (hosting.WatchResponse, error)
}

var _ Reader = (*hosting.WatchReader)(nil)

// GitHubForge implements land.Forge over a Reader. It has no method that writes.
type GitHubForge struct {
	r    Reader
	repo string
}

var _ land.Forge = (*GitHubForge)(nil)

// NewGitHubForge returns the adapter for OWNER/NAME.
func NewGitHubForge(r Reader, repo string) *GitHubForge { return &GitHubForge{r: r, repo: repo} }

func status(err error) int {
	var we *hosting.WatchError
	if errors.As(err, &we) {
		return we.Status
	}
	return 0
}

func (g *GitHubForge) get(ctx context.Context, endpoint string, out any) error {
	res, err := g.r.LandRead(ctx, endpoint)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(res.Body, out); err != nil {
		return fmt.Errorf("malformed response")
	}
	return nil
}

// Repository reads the default branch and delete_branch_on_merge.
func (g *GitHubForge) Repository(ctx context.Context) (land.RepoFacts, error) {
	var doc struct {
		DefaultBranch       string `json:"default_branch"`
		DeleteBranchOnMerge *bool  `json:"delete_branch_on_merge"`
	}
	if err := g.get(ctx, "repos/"+g.repo, &doc); err != nil {
		return land.RepoFacts{}, err
	}
	return land.RepoFacts{DefaultBranch: doc.DefaultBranch, DeleteBranchOnMerge: doc.DeleteBranchOnMerge}, nil
}

// Pull reads pulls/N.
func (g *GitHubForge) Pull(ctx context.Context, n int) (land.PullFacts, error) {
	var doc struct {
		Number         int    `json:"number"`
		State          string `json:"state"`
		Merged         bool   `json:"merged"`
		Draft          bool   `json:"draft"`
		Mergeable      *bool  `json:"mergeable"`
		MergeableState string `json:"mergeable_state"`
		Rebaseable     *bool  `json:"rebaseable"`
		MergeCommit    string `json:"merge_commit_sha"`
		Head           struct {
			SHA  string `json:"sha"`
			Ref  string `json:"ref"`
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref  string `json:"ref"`
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
	}
	ep, err := hostingPull(g.repo, n)
	if err != nil {
		return land.PullFacts{}, err
	}
	if err := g.get(ctx, ep, &doc); err != nil {
		return land.PullFacts{}, err
	}
	if doc.Number != n {
		return land.PullFacts{}, fmt.Errorf("the response is for pull request %d, not %d", doc.Number, n)
	}
	p := land.PullFacts{Number: n, State: doc.State, Merged: doc.Merged, Draft: doc.Draft, HeadSHA: doc.Head.SHA, HeadRef: doc.Head.Ref, BaseRef: doc.Base.Ref,
		Mergeable: doc.Mergeable, MergeableState: doc.MergeableState, Rebaseable: doc.Rebaseable, MergeCommit: doc.MergeCommit}
	if doc.Head.Repo != nil {
		p.HeadRepo = doc.Head.Repo.FullName
	}
	if doc.Base.Repo != nil {
		p.BaseRepo = doc.Base.Repo.FullName
	}
	return p, nil
}

func hostingPull(repo string, n int) (string, error) {
	if n < 1 {
		return "", fmt.Errorf("invalid pull request number")
	}
	return fmt.Sprintf("repos/%s/pulls/%d", repo, n), nil
}

// OpenPulls reads up to five pages of open pull requests; five full pages read as truncated.
func (g *GitHubForge) OpenPulls(ctx context.Context) ([]land.OpenPull, bool, error) {
	var out []land.OpenPull
	for page := 1; page <= 5; page++ {
		ep, err := hosting.LandOpenPullsEndpoint(g.repo, page)
		if err != nil {
			return nil, false, err
		}
		var doc []struct {
			Number int `json:"number"`
			Base   struct {
				Ref string `json:"ref"`
			} `json:"base"`
			Head struct {
				Ref string `json:"ref"`
			} `json:"head"`
		}
		if err := g.get(ctx, ep, &doc); err != nil {
			return nil, false, err
		}
		for _, d := range doc {
			out = append(out, land.OpenPull{Number: d.Number, BaseRef: d.Base.Ref, HeadRef: d.Head.Ref})
		}
		if len(doc) < 100 {
			return out, false, nil
		}
	}
	return out, true, nil
}

func ghState(status, conclusion string) land.CheckState {
	if status != "completed" {
		return land.CheckPending
	}
	switch conclusion {
	case "success", "neutral", "skipped":
		return land.CheckSuccess
	}
	return land.CheckFailure
}

func worse(a, b land.CheckState) land.CheckState {
	rank := map[land.CheckState]int{"": 0, land.CheckSuccess: 1, land.CheckPending: 2, land.CheckFailure: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// Checks reads the check runs and statuses of a head (at most ten pages each).
func (g *GitHubForge) Checks(ctx context.Context, sha string) (land.CheckFacts, error) {
	if !land.ValidSHA(sha) {
		return land.CheckFacts{}, fmt.Errorf("invalid object id")
	}
	facts := land.CheckFacts{Contexts: map[string]land.CheckState{}}
	for page := 1; ; page++ {
		if page > 10 {
			return land.CheckFacts{}, fmt.Errorf("more than 1000 check runs")
		}
		var doc struct {
			Runs []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"check_runs"`
		}
		if err := g.get(ctx, fmt.Sprintf("repos/%s/commits/%s/check-runs?per_page=100&page=%d", g.repo, sha, page), &doc); err != nil {
			return land.CheckFacts{}, err
		}
		for _, r := range doc.Runs {
			facts.Contexts[r.Name] = worse(facts.Contexts[r.Name], ghState(r.Status, r.Conclusion))
		}
		if len(doc.Runs) < 100 {
			break
		}
	}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		if page > 10 {
			return land.CheckFacts{}, fmt.Errorf("more than 1000 statuses")
		}
		var doc []struct {
			Context string `json:"context"`
			State   string `json:"state"`
		}
		if err := g.get(ctx, fmt.Sprintf("repos/%s/commits/%s/statuses?per_page=100&page=%d", g.repo, sha, page), &doc); err != nil {
			return land.CheckFacts{}, err
		}
		for _, s := range doc {
			if seen[s.Context] { // newest first: the first entry per context is the current one
				continue
			}
			seen[s.Context] = true
			st := land.CheckFailure
			switch s.State {
			case "success":
				st = land.CheckSuccess
			case "pending":
				st = land.CheckPending
			}
			facts.Contexts[s.Context] = worse(facts.Contexts[s.Context], st)
		}
		if len(doc) < 100 {
			break
		}
	}
	return facts, nil
}

// Reviews reads the first page of reviews.
func (g *GitHubForge) Reviews(ctx context.Context, n int) ([]land.Review, error) {
	ep, err := hosting.LandReviewsEndpoint(g.repo, n)
	if err != nil {
		return nil, err
	}
	var doc []struct {
		User *struct {
			Login string `json:"login"`
		} `json:"user"`
		State    string `json:"state"`
		CommitID string `json:"commit_id"`
	}
	if err := g.get(ctx, ep, &doc); err != nil {
		return nil, err
	}
	out := make([]land.Review, 0, len(doc))
	for _, d := range doc {
		rv := land.Review{State: d.State, CommitID: d.CommitID}
		if d.User != nil {
			rv.Reviewer = d.User.Login
		}
		out = append(out, rv)
	}
	return out, nil
}

// Events reads up to three pages of issue events; three full pages are an error (the list may
// continue, and a late head change would be missed).
func (g *GitHubForge) Events(ctx context.Context, n int) ([]land.Event, error) {
	var out []land.Event
	for page := 1; page <= hosting.LandMaxListPages; page++ {
		ep, err := hosting.LandEventsEndpoint(g.repo, n, page)
		if err != nil {
			return nil, err
		}
		var doc []struct {
			Event string `json:"event"`
			Actor *struct {
				Login string `json:"login"`
			} `json:"actor"`
		}
		if err := g.get(ctx, ep, &doc); err != nil {
			return nil, err
		}
		for _, d := range doc {
			e := land.Event{Name: d.Event}
			if d.Actor != nil {
				e.Actor = d.Actor.Login
			}
			out = append(out, e)
		}
		if len(doc) < 100 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("more than %d issue events; the list is truncated", hosting.LandMaxListPages*100)
}

// Viewer reads the login of the authenticated user.
func (g *GitHubForge) Viewer(ctx context.Context) (string, error) {
	var doc struct {
		Login string `json:"login"`
	}
	if err := g.get(ctx, "user", &doc); err != nil {
		return "", err
	}
	return doc.Login, nil
}

type ruleDoc struct {
	Type       string `json:"type"`
	RulesetID  int    `json:"ruleset_id"`
	Parameters struct {
		Checks []struct {
			Context string `json:"context"`
		} `json:"required_status_checks"`
	} `json:"parameters"`
}

// rules reads up to three pages; three full pages are an error (a truncated list is not complete).
func (g *GitHubForge) rules(ctx context.Context, branch string) ([]ruleDoc, error) {
	var all []ruleDoc
	for page := 1; page <= hosting.LandMaxListPages; page++ {
		ep, err := hosting.LandRulesEndpoint(g.repo, branch, page)
		if err != nil {
			return nil, err
		}
		var doc []ruleDoc
		if err := g.get(ctx, ep, &doc); err != nil {
			return nil, err
		}
		all = append(all, doc...)
		if len(doc) < 100 {
			return all, nil
		}
	}
	return nil, fmt.Errorf("more than %d rules; the list is truncated", hosting.LandMaxListPages*100)
}

// protection reads the classic branch protection: contexts, and "none", "present" or "unreadable".
// The protection endpoint needs administration read. On a 403 the branch endpoint answers with read
// access: `protection.enabled` false is none, true carries the required contexts, a missing
// `protection` object is unreadable (Spec 043 FR-090). Any other failure is unreadable.
func (g *GitHubForge) protection(ctx context.Context, branch string) (contexts []string, state string) {
	ep, err := hosting.LandProtectionEndpoint(g.repo, branch)
	if err != nil {
		return nil, "unreadable"
	}
	var doc struct {
		Checks struct {
			Contexts []string `json:"contexts"`
			Checks   []struct {
				Context string `json:"context"`
			} `json:"checks"`
		} `json:"required_status_checks"`
	}
	if err := g.get(ctx, ep, &doc); err != nil {
		switch status(err) {
		case 404:
			return nil, "none" // "Branch not protected" is not an error (FR-012)
		case 403:
			return g.branchProtection(ctx, branch)
		}
		return nil, "unreadable"
	}
	contexts = append(contexts, doc.Checks.Contexts...)
	for _, c := range doc.Checks.Checks {
		contexts = append(contexts, c.Context)
	}
	return contexts, "present"
}

func (g *GitHubForge) branchProtection(ctx context.Context, branch string) (contexts []string, state string) {
	ep, err := hosting.LandBranchEndpoint(g.repo, branch)
	if err != nil {
		return nil, "unreadable"
	}
	var doc struct {
		Protected  *bool `json:"protected"`
		Protection *struct {
			Enabled *bool `json:"enabled"`
			Checks  struct {
				Contexts []string `json:"contexts"`
				Checks   []struct {
					Context string `json:"context"`
				} `json:"checks"`
			} `json:"required_status_checks"`
		} `json:"protection"`
	}
	if err := g.get(ctx, ep, &doc); err != nil {
		return nil, "unreadable"
	}
	if doc.Protection == nil || doc.Protection.Enabled == nil {
		if doc.Protected != nil && !*doc.Protected {
			return nil, "none" // no rule of any kind applies to the branch
		}
		return nil, "unreadable"
	}
	if !*doc.Protection.Enabled {
		return nil, "none"
	}
	contexts = append(contexts, doc.Protection.Checks.Contexts...)
	for _, c := range doc.Protection.Checks.Checks {
		contexts = append(contexts, c.Context)
	}
	return contexts, "present"
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Required is the union of the required contexts of the rules and of classic protection (FR-012).
func (g *GitHubForge) Required(ctx context.Context, branch string) (land.RequiredFacts, error) {
	rules, err := g.rules(ctx, branch)
	if err != nil {
		return land.RequiredFacts{}, nil // unreadable: Readable stays false, the plan refuses checks-unknown
	}
	var contexts []string
	for _, r := range rules {
		if r.Type == "required_status_checks" {
			for _, c := range r.Parameters.Checks {
				contexts = append(contexts, c.Context)
			}
		}
	}
	more, state := g.protection(ctx, branch)
	if state == "unreadable" {
		return land.RequiredFacts{}, nil
	}
	return land.RequiredFacts{Readable: true, Contexts: unique(append(contexts, more...))}, nil
}

// Rulesets reads the rule types in force, the bypass modes of their rulesets and the protection
// state (FR-067). Any unreadable part makes the facts unreadable.
func (g *GitHubForge) Rulesets(ctx context.Context, branch string) (land.RulesetFacts, error) {
	rules, err := g.rules(ctx, branch)
	if err != nil {
		return land.RulesetFacts{Protection: "unreadable"}, nil
	}
	_, state := g.protection(ctx, branch)
	facts := land.RulesetFacts{Protection: state, RuleTypes: []string{}, BypassModes: []string{}}
	ids := map[int]bool{}
	var types, modes []string
	for _, r := range rules {
		types = append(types, r.Type)
		ids[r.RulesetID] = true
	}
	if len(ids) > 8 {
		return land.RulesetFacts{Protection: state}, nil
	}
	order := make([]int, 0, len(ids))
	for id := range ids {
		order = append(order, id)
	}
	sort.Ints(order)
	for _, id := range order {
		ep, err := hosting.LandRulesetEndpoint(g.repo, id)
		if err != nil {
			return land.RulesetFacts{Protection: state}, nil
		}
		var doc struct {
			// A pointer: an absent key (a read without admin rights) is not an empty list.
			Bypass *[]struct {
				Mode string `json:"bypass_mode"`
			} `json:"bypass_actors"`
		}
		if err := g.get(ctx, ep, &doc); err != nil || doc.Bypass == nil {
			return land.RulesetFacts{Protection: state}, nil
		}
		for _, b := range *doc.Bypass {
			modes = append(modes, b.Mode)
		}
	}
	facts.RuleTypes, facts.BypassModes = unique(types), unique(modes)
	facts.Readable = state != "unreadable"
	return facts, nil
}
