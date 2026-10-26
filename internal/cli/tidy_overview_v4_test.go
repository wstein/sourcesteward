// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
)

func overviewPrune(n int, label string, scope owner.Scope) tidywizard.RegistrationPrune {
	return tidywizard.RegistrationPrune{ID: fmt.Sprintf("%064x", n+1), Kind: "registration-prune", Repo: "a", Name: fmt.Sprintf("missing-%d", n), GitDir: fmt.Sprintf("/repo/.git/worktrees/missing-%d", n), Destination: fmt.Sprintf("/missing/%d", n), Owner: label, OwnerSource: owner.SourceTaskRecord, Scope: scope, Task: fmt.Sprintf("task-%d", n), Verdict: tidywizard.RegistrationPruneSafe, Reason: "registration-prune-ready", Proof: "owned-missing-registration", Effects: []string{"remove-registration"}, EvidenceDigest: strings.Repeat("a", 64), Limitations: []string{}}
}

func overviewSource(t *testing.T, result tidywizard.RegistrationPruneResult) *int {
	t.Helper()
	old := tidyRegistrationPrunes
	t.Cleanup(func() { tidyRegistrationPrunes = old })
	calls := new(int)
	tidyRegistrationPrunes = func(context.Context, []tidywizard.RepoSpec, []derive.Store, owner.Evidence, tidywizard.Config) tidywizard.RegistrationPruneResult {
		*calls++
		return result
	}
	return calls
}

func overviewDocument(t *testing.T, out string) render.DocumentV4 {
	t.Helper()
	var d render.DocumentV4
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&d); err != nil {
		t.Fatalf("document: %v\n%s", err, out)
	}
	return d
}

func TestTidyOverviewV4ReadOnlyOptInAndLegacyIsolation(t *testing.T) {
	for _, structured := range []bool{false, true} {
		t.Run(fmt.Sprint(structured), func(t *testing.T) {
			w := newInteractive(t, true, true, "STW_OWNER=codex")
			calls := overviewSource(t, tidywizard.RegistrationPruneResult{Rows: []tidywizard.RegistrationPrune{overviewPrune(0, "codex", owner.ScopeOwn)}})
			args := []string{"--contract", "4"}
			if structured {
				args = append(args, "--json")
			}
			out, err := runTidy(t, "must not be read", args...)
			if err != nil || *calls != 1 || w.prepared != 1 || len(w.planner.specs) != 0 || len(w.applied) != 0 || w.confirmOpened != 0 || len(storedSets(t, w.prep.PlanStore)) != 0 || strings.Contains(out, "Apply which?") {
				t.Fatalf("read-only: %v calls=%d\n%s", err, *calls, out)
			}
			if structured {
				d := overviewDocument(t, out)
				if d.SchemaVersion != 4 || len(d.RegistrationPrunes) != 1 || d.PlanID != "" {
					t.Fatalf("wrong overview: %+v", d)
				}
			} else if !strings.Contains(out, "missing-0") {
				t.Fatalf("missing text row: %s", out)
			}
			_, err = runTidy(t, "", "--json")
			if err != nil || *calls != 1 {
				t.Fatalf("legacy called v4 observer: %v calls=%d", err, *calls)
			}
		})
	}
}

func TestTidyOverviewV4ViewAndKindFilters(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   []string
		flags []string
		want  []string
	}{
		{"human", []string{"STW_OWNER=codex"}, nil, []string{"codex", "claude"}},
		{"agent", []string{"STW_OWNER=codex", "STW_AGENT=1"}, nil, []string{"codex"}},
		{"agent other view", []string{"STW_OWNER=codex", "STW_AGENT=1"}, []string{"--by-owner", "claude"}, []string{"claude"}},
		{"mine intersection", []string{"STW_OWNER=codex"}, []string{"--mine", "--by-owner", "claude"}, []string{}},
		{"branch only", []string{"STW_OWNER=codex"}, []string{"--only", "branches"}, []string{}},
		{"stale only", []string{"STW_OWNER=codex"}, []string{"--only", "stale-worktrees"}, []string{"codex", "claude"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newInteractive(t, true, true, tc.env...)
			overviewSource(t, tidywizard.RegistrationPruneResult{Rows: []tidywizard.RegistrationPrune{overviewPrune(0, "codex", owner.ScopeOwn), overviewPrune(1, "claude", owner.ScopeOther)}})
			args := append([]string{"--contract", "4", "--json"}, tc.flags...)
			out, err := runTidy(t, "", args...)
			if err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, r := range overviewDocument(t, out).RegistrationPrunes {
				got = append(got, r.Owner)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("view %v want %v", got, tc.want)
			}
		})
	}
}

func TestTidyOverviewV4RejectsBadSelectorsBeforeAnalysis(t *testing.T) {
	for _, tc := range []struct {
		flags  []string
		reason string
	}{
		{[]string{"--contract", "3"}, "--contract must be 4"},
		{[]string{"--contract="}, "--contract must be 4"},
		{[]string{"--contract", "4", "--contract", "4"}, "--contract must occur exactly once"},
		{[]string{"--contract", "4", "--save"}, "--save belongs"},
		{[]string{"--contract", "4", "--yes"}, "--yes belongs"},
		{[]string{"--contract", "4", "--json", "--human"}, "--json and --human"},
	} {
		t.Run(strings.Join(tc.flags, " "), func(t *testing.T) {
			w := newInteractive(t, true, true)
			calls := overviewSource(t, tidywizard.RegistrationPruneResult{})
			_, err := runTidy(t, "", tc.flags...)
			if !isUsage(err) || !strings.Contains(err.Error(), tc.reason) || w.prepared != 0 || *calls != 0 {
				t.Fatalf("refusal: %v prepared=%d calls=%d", err, w.prepared, *calls)
			}
		})
	}
}

func TestTidyOverviewV4SharesContextAndCallerAndReportsIncomplete(t *testing.T) {
	w := newInteractive(t, true, true, "STW_OWNER=codex")
	old := tidyRegistrationPrunes
	t.Cleanup(func() { tidyRegistrationPrunes = old })
	tidyRegistrationPrunes = func(ctx context.Context, repos []tidywizard.RepoSpec, stores []derive.Store, mode owner.Evidence, cfg tidywizard.Config) tidywizard.RegistrationPruneResult {
		if ctx != w.analyseCtx || !reflect.DeepEqual(cfg, w.gotCfg) || !reflect.DeepEqual(repos, w.prep.Specs) || !reflect.DeepEqual(stores, w.prep.Stores) || mode.Caller != cfg.Caller {
			t.Fatal("observer did not share authorized analysis inputs")
		}
		return tidywizard.RegistrationPruneResult{Rows: []tidywizard.RegistrationPrune{}, Incomplete: true, Notices: []string{"registration-prune-inventory-incomplete"}}
	}
	out, err := runTidy(t, "", "--contract", "4", "--json", "--timeout", "30s")
	if err == nil || !strings.Contains(err.Error(), "registration-prune-inventory-incomplete") {
		t.Fatalf("incomplete: %v\n%s", err, out)
	}
	d := overviewDocument(t, out)
	if d.SchemaVersion != 4 || !strings.Contains(out, "registration-prune-inventory-incomplete") {
		t.Fatalf("missing partial evidence: %s", out)
	}
}

// Even a terminal in human mode must not ask for input in this read-only slice.
func TestTidyOverviewV4NeverReadsInputAndReportsWriterFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writer io.Writer
		want   error
	}{
		{"complete", &bytes.Buffer{}, nil},
		{"short", shortWriter{0}, io.ErrShortWrite},
		{"closed", closedPipeWriter{}, io.ErrClosedPipe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newInteractive(t, true, true, "STW_OWNER=codex")
			overviewSource(t, tidywizard.RegistrationPruneResult{Rows: []tidywizard.RegistrationPrune{overviewPrune(0, "codex", owner.ScopeOwn)}})
			cmd := tidyCommand()
			cmd.SetOut(tc.writer)
			cmd.SetErr(io.Discard)
			cmd.SetIn(neverRead{t})
			cmd.SetArgs([]string{"--contract", "4", "--json"})
			err := cmd.Execute()
			if tc.want == nil {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var failure runtimeFailure
				if !errors.As(err, &failure) || !errors.Is(failure.error, tc.want) {
					t.Fatalf("writer failure: %v want %v", err, tc.want)
				}
			}
		})
	}
}

func TestTidyOverviewV4HiddenPruneDoesNotHideLegacyRow(t *testing.T) {
	w := newInteractive(t, true, true, "STW_OWNER=codex", "STW_AGENT=1")
	p := overviewPrune(0, "", owner.ScopeUnknown)
	p.Verdict, p.OwnerSource = tidywizard.RegistrationPruneBlocked, owner.SourceUnknown
	c := wcand("a", tidywizard.KindStaleWorktree, "missing", tidywizard.VerdictRetain, "codex", owner.SourceTaskRecord, owner.ScopeOwn)
	c.Path = p.Destination
	w.res.Candidates = []tidywizard.Candidate{c}
	overviewSource(t, tidywizard.RegistrationPruneResult{Rows: []tidywizard.RegistrationPrune{p}})
	out, err := runTidy(t, "", "--contract", "4", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := overviewDocument(t, out)
	if len(doc.RegistrationPrunes) != 0 || len(doc.Candidates) != 1 || doc.Candidates[0].ID != c.ID {
		t.Fatalf("hidden prune swallowed visible legacy row: %s", out)
	}
}

func TestTidyOverviewV4ReportsCutAndCancellationExit(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		json, textBytes, cancel bool
	}{
		{name: "json row bound", json: true}, {name: "text row bound"}, {name: "text byte bound", textBytes: true}, {name: "canceled after analysis", json: true, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newInteractive(t, true, true, "STW_OWNER=codex")
			rows := []tidywizard.RegistrationPrune{overviewPrune(0, "codex", owner.ScopeOwn)}
			if tc.textBytes {
				rows[0].Limitations = make([]string, 50000)
				for i := range rows[0].Limitations {
					rows[0].Limitations[i] = strings.Repeat("x", 120)
				}
			} else if !tc.cancel {
				for i := 1; i <= tidywizard.MaxCandidates; i++ {
					rows = append(rows, overviewPrune(i, "codex", owner.ScopeOwn))
				}
			}
			overviewSource(t, tidywizard.RegistrationPruneResult{Rows: rows})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				original := tidyRegistrationPrunes
				tidyRegistrationPrunes = func(ctx context.Context, repos []tidywizard.RepoSpec, stores []derive.Store, mode owner.Evidence, cfg tidywizard.Config) tidywizard.RegistrationPruneResult {
					result := original(ctx, repos, stores, mode, cfg)
					cancel()
					return result
				}
			}
			args := []string{"tidy", "--contract", "4"}
			if tc.json {
				args = append(args, "--json")
			}
			var out, diag bytes.Buffer
			code := Run(ctx, "dev", args, &out, &diag)
			reason := "output-limit"
			if tc.cancel {
				reason = "interrupted"
			}
			if code != 1 || !strings.Contains(diag.String(), reason) || out.Len() == 0 {
				t.Fatalf("code=%d reason=%s diagnostics=%s output bytes=%d", code, reason, diag.String(), out.Len())
			}
			if tc.json && !tc.cancel && !overviewDocument(t, out.String()).Truncated {
				t.Fatal("cut JSON not marked truncated")
			}
		})
	}
}

func TestTidyOverviewV4PreservesRepositoryFailureCause(t *testing.T) {
	for _, state := range []string{"error", "skipped"} {
		t.Run(state, func(t *testing.T) {
			w := newInteractive(t, true, true, "STW_OWNER=codex")
			w.res.Repositories[0].State = state
			w.res.Repositories[0].Reason = "base-ambiguous"
			overviewSource(t, tidywizard.RegistrationPruneResult{Incomplete: true, Notices: []string{"registration-prune-inventory-incomplete"}})
			out, err := runTidy(t, "", "--contract", "4", "--json")
			var failure runtimeFailure
			if !errors.As(err, &failure) || !strings.Contains(err.Error(), `repository "a"`) || (state == "skipped" && !strings.Contains(err.Error(), "base-ambiguous")) {
				t.Fatalf("lost repository failure: %v", err)
			}
			if !strings.Contains(out, "registration-prune-inventory-incomplete") {
				t.Fatalf("lost registration notice: %s", out)
			}
		})
	}
}
