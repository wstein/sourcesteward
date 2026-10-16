// SPDX-License-Identifier: EUPL-1.2
package inspection

import "testing"

func reposTestGitIdentity() Result {
	wt, oid, ref := "/w/r", "1111111111111111111111111111111111111111", "refs/heads/main"
	return Result{Status: "ok", Backend: "git", Repository: &Repository{GitDir: "/w/r/.git", CommonDir: "/w/r/.git", Worktree: &wt, Head: Head{State: "attached", Ref: &ref, OID: &oid}}}
}

func reposTestJJIdentity() Result {
	return Result{Status: "ok", Backend: "jj", JJRepository: &JJRepository{WorkspaceRoot: "/w/j", RepositoryDir: "/w/j/.jj/repo", OperationID: "op1"}}
}

// verifyIdentityUnchanged compares every field FR-008 names: a change of any one is a different repository.
func TestReposIdentityComparisonCoversEveryField(t *testing.T) {
	str := func(s string) *string { return &s }
	type mutate func(*Result)
	for _, tc := range []struct {
		name  string
		prior func() Result
		mut   mutate
		same  bool
	}{
		{"identical git", reposTestGitIdentity, func(*Result) {}, true},
		{"git dir", reposTestGitIdentity, func(r *Result) { r.Repository.GitDir = "/w/other/.git" }, false},
		{"common dir", reposTestGitIdentity, func(r *Result) { r.Repository.CommonDir = "/w/other/.git" }, false},
		{"worktree", reposTestGitIdentity, func(r *Result) { r.Repository.Worktree = str("/w/other") }, false},
		{"worktree gone", reposTestGitIdentity, func(r *Result) { r.Repository.Worktree = nil }, false},
		{"head oid", reposTestGitIdentity, func(r *Result) { r.Repository.Head.OID = str("2222222222222222222222222222222222222222") }, false},
		{"head oid gone", reposTestGitIdentity, func(r *Result) { r.Repository.Head.OID = nil }, false},
		{"head ref", reposTestGitIdentity, func(r *Result) { r.Repository.Head.Ref = str("refs/heads/other") }, false},
		{"head ref gone", reposTestGitIdentity, func(r *Result) { r.Repository.Head.Ref = nil }, false},
		{"backend", reposTestGitIdentity, func(r *Result) { r.Backend = "jj" }, false},
		{"identical jj", reposTestJJIdentity, func(*Result) {}, true},
		{"jj workspace root", reposTestJJIdentity, func(r *Result) { r.JJRepository.WorkspaceRoot = "/w/other" }, false},
		{"jj repository dir", reposTestJJIdentity, func(r *Result) { r.JJRepository.RepositoryDir = "/w/other/.jj/repo" }, false},
		{"jj operation id", reposTestJJIdentity, func(r *Result) { r.JJRepository.OperationID = "op2" }, false},
	} {
		prior, later := tc.prior(), tc.prior()
		tc.mut(&later)
		inv := InventoryResult{Identity: later}
		if got := verifyIdentityUnchanged(prior, inv); got != tc.same {
			t.Errorf("%s: unchanged=%v, want %v", tc.name, got, tc.same)
		}
	}
	// An embedded identity that is not ok is not compared: the inventory keeps its own error.
	if !verifyIdentityUnchanged(reposTestGitIdentity(), InventoryResult{Identity: Result{Status: "error"}}) {
		t.Error("an error identity must not be compared")
	}
}
