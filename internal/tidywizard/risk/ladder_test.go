// SPDX-License-Identifier: EUPL-1.2
package risk

import (
	"reflect"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

func TestLadder(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	base := Facts{Kind: "worktree", Verdict: "safe", Proof: "ancestor", Scope: owner.ScopeOwn, HasCreationRecord: true}
	cases := []struct {
		name    string
		change  func(*Facts)
		level   Level
		reasons []string
	}{
		{"ancestor", func(*Facts) {}, Green, []string{"proof-ancestor"}},
		{"no net change", func(f *Facts) { f.Proof = "no-net-change" }, Green, []string{"proof-no-net-change"}},
		{"tracking branch absent", func(f *Facts) { f.Kind = "tracking-ref"; f.Proof = "github-branch-absent" }, Green, []string{"proof-github-branch-absent"}},
		{"no work", func(f *Facts) { f.Proof = "none"; f.NoWork = true }, Green, []string{"no-own-commit"}},
		{"merged head", func(f *Facts) { f.Proof = "patch-id"; f.MergedHead = true }, Green, []string{"pr-merged-head"}},
		{"patch id", func(f *Facts) { f.Proof = "patch-id" }, Yellow, []string{"proof-equivalent"}},
		{"merge noop", func(f *Facts) { f.Proof = "merge-noop" }, Yellow, []string{"proof-equivalent"}},
		{"gone changes no level", func(f *Facts) { f.Kind = "branch"; f.Proof = "patch-id"; f.UpstreamGone = true }, Yellow, []string{"proof-equivalent", "upstream-gone"}},
		{"gone keeps strong green", func(f *Facts) { f.Kind = "branch"; f.UpstreamGone = true }, Green, []string{"proof-ancestor", "upstream-gone"}},
		{"gone is branch only", func(f *Facts) { f.UpstreamGone = true }, Green, []string{"proof-ancestor"}},
		{"gone cannot prove", func(f *Facts) { f.Kind = "branch"; f.Verdict = "retain"; f.Proof = "none"; f.UpstreamGone = true }, Red, []string{"not-integrated", "upstream-gone"}},
		{"R1 strong", func(f *Facts) { f.Scope = owner.ScopeOther; f.LastActivity = now.Add(-48 * time.Hour) }, Yellow, []string{"proof-ancestor", "recent-activity"}},
		{"R1 equivalent", func(f *Facts) {
			f.Scope = owner.ScopeUnknown
			f.Proof = "patch-id"
			f.LastActivity = now.Add(-48 * time.Hour)
		}, Orange, []string{"proof-equivalent", "recent-activity"}},
		{"R1 own exempt", func(f *Facts) { f.LastActivity = now.Add(-48 * time.Hour) }, Green, []string{"proof-ancestor"}},
		{"R1 branch exempt", func(f *Facts) {
			f.Kind = "branch"
			f.Scope = owner.ScopeOther
			f.LastActivity = now.Add(-48 * time.Hour)
		}, Green, []string{"proof-ancestor"}},
		{"R2", func(f *Facts) { f.Proof = "patch-id"; f.Scope = owner.ScopeUnknown; f.HasCreationRecord = false }, Orange, []string{"proof-equivalent", "nobody-vouches"}},
		{"R2 strong exempt", func(f *Facts) { f.Scope = owner.ScopeUnknown; f.HasCreationRecord = false }, Green, []string{"proof-ancestor"}},
		{"R2 path convention vouches", func(f *Facts) {
			f.Proof = "patch-id"
			f.Scope = owner.ScopeUnknown
			f.HasCreationRecord = false
			f.OwnerSource = owner.SourcePathConvention
		}, Yellow, []string{"proof-equivalent"}},
		{"R2 branch exempt", func(f *Facts) {
			f.Kind = "branch"
			f.Proof = "patch-id"
			f.Scope = owner.ScopeUnknown
			f.HasCreationRecord = false
		}, Yellow, []string{"proof-equivalent"}},
		{"cap at orange", func(f *Facts) {
			f.Proof = "patch-id"
			f.Scope = owner.ScopeUnknown
			f.HasCreationRecord = false
			f.LastActivity = now.Add(-48 * time.Hour)
		}, Orange, []string{"proof-equivalent", "recent-activity", "nobody-vouches"}},
		{"R4 strong", func(f *Facts) { f.Verdict = "decision"; f.Reason = "lock-mismatch" }, Orange, []string{"proof-ancestor", "decision-row"}},
		{"R4 equivalent", func(f *Facts) { f.Verdict = "decision"; f.Proof = "merge-noop"; f.Reason = "lock-mismatch" }, Orange, []string{"proof-equivalent", "decision-row"}},
		{"record missing no proof", func(f *Facts) { f.Verdict = "retain"; f.Proof = "none"; f.Reason = "registration-record-missing" }, Red, []string{"not-integrated"}},
		{"not judged", func(f *Facts) { f.Verdict = "retain"; f.Reason = "evidence-incomplete" }, Red, []string{"not-judged"}},
		{"dirt under structural reason", func(f *Facts) { f.Verdict = "retain"; f.Reason = "registration-record-missing"; f.Dirty = true }, Red, []string{"uncommitted-files"}},
		{"extras under structural reason", func(f *Facts) { f.Verdict = "retain"; f.Reason = "locked-foreign"; f.IgnoredExtras = true }, Red, []string{"extra-files"}},
		{"incomplete under structural reason", func(f *Facts) { f.Verdict = "retain"; f.Reason = "protected"; f.Incomplete = true }, Red, []string{"not-judged"}},
		{"all red facts", func(f *Facts) {
			f.Verdict = "retain"
			f.Proof = "none"
			f.Dirty = true
			f.IgnoredExtras = true
			f.Incomplete = true
		}, Red, []string{"not-integrated", "uncommitted-files", "extra-files", "not-judged"}},
		{"dirty safe cannot be lifted", func(f *Facts) { f.Dirty = true; f.MergedHead = true }, Red, []string{"uncommitted-files"}},
		{"unmerged remains red with merged hint", func(f *Facts) { f.Verdict = "retain"; f.Proof = "none"; f.Reason = "unmerged"; f.MergedHead = true }, Red, []string{"not-integrated"}},
		{"dirty reason without exposed fact", func(f *Facts) { f.Verdict = "retain"; f.Reason = "dirty" }, Red, []string{"uncommitted-files"}},
		{"extras reason without exposed fact", func(f *Facts) { f.Verdict = "retain"; f.Reason = "ignored-extras" }, Red, []string{"extra-files"}},
		{"primary", func(f *Facts) { f.Verdict = "retain"; f.Proof = "none"; f.Primary = true }, Kept, []string{}},
		{"gone stale", func(f *Facts) {
			f.Kind = "stale-worktree"
			f.Verdict = "retain"
			f.Proof = "none"
			f.DirectoryMissing = true
		}, Kept, []string{}},
		{"integrated structural", func(f *Facts) { f.Verdict = "retain"; f.Reason = "registration-record-missing" }, Kept, []string{}},
		{"unknown proof", func(f *Facts) { f.Proof = "future-proof" }, Red, []string{"not-judged"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			tc.change(&f)
			got := Evaluate(f, now)
			if got.Level != tc.level || !reflect.DeepEqual(got.Reasons, tc.reasons) {
				t.Fatalf("got %+v want %s %v", got, tc.level, tc.reasons)
			}
		})
	}
}

func TestRecentWindowBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		age  time.Duration
		want Level
	}{
		{"24h minus one", 24*time.Hour - time.Nanosecond, Green}, {"24h", 24 * time.Hour, Green},
		{"24h plus one", 24*time.Hour + time.Nanosecond, Yellow}, {"72h", 72 * time.Hour, Yellow},
		{"72h plus one", 72*time.Hour + time.Nanosecond, Green}, {"future", -time.Hour, Green},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(Facts{Kind: "worktree", Verdict: "safe", Proof: "ancestor", Scope: owner.ScopeOther, LastActivity: now.Add(-tc.age)}, now)
			if got.Level != tc.want {
				t.Fatalf("age %s got %+v want %s", tc.age, got, tc.want)
			}
		})
	}
	f := Facts{Kind: "worktree", Verdict: "safe", Proof: "ancestor", Scope: owner.ScopeOther}
	if got := Evaluate(f, now); got.Level != Green {
		t.Fatalf("missing timestamp invented activity: %+v", got)
	}
}
