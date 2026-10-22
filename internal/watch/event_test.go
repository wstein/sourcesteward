// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	sha1 = "3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182"
	sha2 = "9d8c7b6a5f4e3d2c1b0a99887766554433221100"
)

func strp(s string) *string { return &s }
func intp(i int) *int       { return &i }
func boolp(b bool) *bool    { return &b }

// allKinds is the closed v1 enum of the contract with the exact detail keys of every kind.
var kindKeys = map[string][]string{
	KindNewPR:               {"title", "author", "draft", "base", "head_ref", "head", "fork"},
	KindHeadChanged:         {"old", "new"},
	KindChecksGreen:         {"head", "draft", "base", "total"},
	KindChecksRed:           {"head", "failing", "failing_total", "shape", "base", "draft"},
	KindChecksPendingTimout: {"head", "pending", "since"},
	KindMergeableChanged:    {"mergeable", "state", "conflicting", "behind", "base"},
	KindReadyStateChanged:   {"from", "to"},
	KindBaseChanged:         {"old", "new"},
	KindMerged:              {"merge_commit", "base", "head", "head_ref", "merged_at", "method_hint"},
	KindClosed:              {"head", "base"},
	KindRestackNeeded:       {"merged_pr", "base", "reason", "old_parent", "new_parent", "new_parent_source", "branch", "argv"},
	KindReposChanged:        {"added", "removed", "skipped"},
	KindWatchError:          {"code", "message", "failures", "retry_in_seconds", "repo"},
	KindWatchRecovered:      {"after_failures", "repo"},
	KindWatchStalled:        {"since", "threshold_seconds", "repo"},
}

func sampleEvents() []Event {
	r, pr := "o/r", 7
	mk := func(kind string, details any) Event {
		e := Event{SchemaVersion: 1, Seq: 1, ID: "x", Time: "2026-10-22T09:00:00Z", Repo: &r, PR: &pr, Kind: kind, Details: details}
		return e
	}
	return []Event{
		mk(KindNewPR, &NewPRDetails{Title: "t", Author: "a", Base: "main", HeadRef: "f", Head: sha1}),
		mk(KindHeadChanged, &HeadChangedDetails{Old: sha1, New: sha2}),
		mk(KindChecksGreen, &ChecksGreenDetails{Head: sha1, Base: "main", Total: 3}),
		mk(KindChecksRed, &ChecksRedDetails{Head: sha1, Failing: []string{"a"}, FailingTotal: 1, Shape: ShapeReal, Base: "main"}),
		mk(KindChecksPendingTimout, &PendingTimeoutDetails{Head: sha1, Pending: []string{"a"}, Since: "2026-10-22T09:00:00Z"}),
		mk(KindMergeableChanged, &MergeableDetails{Mergeable: nil, State: "dirty", Base: "main"}),
		mk(KindReadyStateChanged, &ReadyDetails{From: "draft", To: "ready"}),
		mk(KindBaseChanged, &BaseChangedDetails{Old: "a", New: "b"}),
		mk(KindMerged, &MergedDetails{MergeCommit: nil, Base: "main", Head: sha1, HeadRef: "f", MergedAt: "2026-10-22T09:00:00Z", MethodHint: "unknown"}),
		mk(KindClosed, &ClosedDetails{Head: sha1, Base: "main"}),
		mk(KindRestackNeeded, &RestackDetails{MergedPR: 3, Base: "main", Reason: ReasonContains, OldParent: sha1, NewParentSource: "merge_commit_sha", Branch: "f"}),
		{SchemaVersion: 1, Seq: 2, ID: "y", Time: "2026-10-22T09:00:00Z", Kind: KindReposChanged, Details: &ReposChangedDetails{}},
		mk(KindWatchError, &WatchErrorDetails{Code: "network", Message: "m", Failures: 1, RetryInSeconds: 60}),
		mk(KindWatchRecovered, &WatchRecoveredDetails{AfterFailures: 2}),
		mk(KindWatchStalled, &WatchStalledDetails{Since: "2026-10-22T09:00:00Z", ThresholdSeconds: 600, Repo: "o/r"}),
	}
}

func TestEveryKindHasExactlyTheContractKeysAndNeverOmitsOne(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range sampleEvents() {
		seen[e.Kind] = true
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var top map[string]json.RawMessage
		json.Unmarshal(raw, &top)
		keys := []string{}
		for k := range top {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if want := []string{"details", "id", "kind", "pr", "repo", "schema_version", "seq", "time"}; !reflect.DeepEqual(keys, want) {
			t.Fatalf("%s envelope keys %v", e.Kind, keys)
		}
		var d map[string]json.RawMessage
		json.Unmarshal(top["details"], &d)
		got := []string{}
		for k := range d {
			got = append(got, k)
		}
		want := append([]string{}, kindKeys[e.Kind]...)
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s details keys %v want %v", e.Kind, got, want)
		}
		if strings.Contains(string(top["details"]), `"failing":null`) || strings.Contains(string(top["details"]), `"pending":null`) {
			t.Fatalf("%s lists must be [] not null", e.Kind)
		}
	}
	if len(seen) != 15 || len(Kinds) != 15 {
		t.Fatalf("kinds covered %d, enum %d", len(seen), len(Kinds))
	}
}

func TestEventJSONRoundTripAndStrictDetails(t *testing.T) {
	for _, e := range sampleEvents() {
		raw, _ := json.Marshal(e)
		var back Event
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("%s: %v", e.Kind, err)
		}
		again, _ := json.Marshal(back)
		if string(again) != string(raw) {
			t.Fatalf("%s round trip\n%s\n%s", e.Kind, raw, again)
		}
	}
	for _, bad := range []string{
		`{"schema_version":1,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"head-changed","details":{"old":"a","new":"b","extra":1}}`,
		`{"schema_version":1,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"nope","details":{}}`,
		`{"schema_version":2,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"closed","details":{"head":"a","base":"b"}}`,
	} {
		var e Event
		if err := json.Unmarshal([]byte(bad), &e); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestEventIDs(t *testing.T) {
	cases := []struct{ got, want string }{
		{eventID("o/r", intp(41), KindNewPR, "g0"), "o/r#41:new-pr:g0"},
		{eventID("o/r", nil, KindWatchRecovered, "e1"), "o/r#-:watch-recovered:e1"},
		{eventID("", nil, KindWatchError, "rate-limited:e1:f1"), "-:watch-error:rate-limited:e1:f1"},
		{eventID("", nil, KindReposChanged, "p3"), "-:repos-changed:p3"},
		{keyHead(sha2, 4), sha2 + ":h4"},
		{keyChecks(sha1, 3), sha1 + ":c3"},
		{keyTimeout(sha1, 3), sha1 + ":c3:t"},
		{keyMergeable(sha1, 2), sha1 + ":m2"},
		{keyReady(5), "r5"},
		{keyBase(6), "b6"},
		{keyGeneration(1), "g1"},
		{keyMerged(strp(sha2)), sha2},
		{keyMerged(nil), "unknown"},
		{keyRestack(41, strp(sha2)), "41:" + sha2},
		{keyRestack(41, nil), "41:unknown"},
		{keyRepos(2), "p2"},
		{keyError("network", 2, 10), "network:e2:f10"},
		{keyRecoveredError(2), "e2"},
		{keyStall(3), "s3"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q want %q", c.got, c.want)
		}
	}
}

func TestParseIDCounters(t *testing.T) {
	if n, ok := idCounter("o/r#4:head-changed:"+sha1+":h12", 'h'); !ok || n != 12 {
		t.Fatal(n, ok)
	}
	if n, ok := idCounter("o/r#4:checks-pending-timeout:"+sha1+":c7:t", 'c'); !ok || n != 7 {
		t.Fatal(n, ok)
	}
	if n, ok := idCounter("-:watch-error:rate-limited:e3:f10", 'e'); !ok || n != 3 {
		t.Fatal(n, ok)
	}
	if _, ok := idCounter("o/r#4:new-pr:g0", 'h'); ok {
		t.Fatal("wrong letter must not parse")
	}
}
