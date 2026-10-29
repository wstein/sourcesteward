// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

func TestTidyOverviewV4RiskRefusalReasonsAndExit(t *testing.T) {
	for _, tc := range []struct {
		verdict       tidywizard.RegistrationPruneVerdict
		reason, level string
	}{
		{tidywizard.RegistrationPruneRetain, "registration-prune-ownership-unproven", "kept"},
		{tidywizard.RegistrationPruneRetain, "registration-prune-absence-unproven", "kept"},
		{tidywizard.RegistrationPruneBlocked, "registration-prune-changed", "red"},
		{tidywizard.RegistrationPruneBlocked, "registration-prune-inventory-blocked", "red"},
		{tidywizard.RegistrationPruneBlocked, "registration-prune-recovery-unavailable", "red"},
		{tidywizard.RegistrationPruneUnknown, "registration-prune-unsupported", "red"},
	} {
		for _, incomplete := range []bool{false, true} {
			for _, structured := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/incomplete=%v/json=%v", tc.reason, incomplete, structured), func(t *testing.T) {
					w := newInteractive(t, true, true, "STW_OWNER=codex")
					row := overviewPrune(0, "", owner.ScopeUnknown)
					row.OwnerSource = owner.SourceUnknown
					row.Verdict = tc.verdict
					row.Reason = tc.reason
					row.Proof = "none"
					row.Effects = nil
					row.EvidenceDigest = ""
					result := tidywizard.RegistrationPruneResult{Rows: []tidywizard.RegistrationPrune{row}, Incomplete: incomplete}
					if incomplete {
						result.Notices = []string{"registration-prune-inventory-incomplete"}
					}
					overviewSource(t, result)
					args := []string{"tidy", "--contract", "4"}
					if structured {
						args = append(args, "--json")
					}
					var out, diag bytes.Buffer
					code := Run(context.Background(), "dev", args, &out, &diag)
					want := 0
					if incomplete {
						want = 1
					}
					if code != want || !strings.Contains(out.String(), tc.reason) || (incomplete && !strings.Contains(diag.String(), "registration-prune-inventory-incomplete")) {
						t.Fatalf("exit=%d want=%d diag=%s output=%s", code, want, &diag, &out)
					}
					if structured {
						d := overviewDocument(t, out.String())
						p := d.RegistrationPrunes[0]
						if p.Risk != tc.level || p.Reason != tc.reason {
							t.Fatalf("annotation=%+v", p)
						}
						if tc.level == "kept" && len(p.RiskReasons) != 0 {
							t.Fatal("kept refusal gained reasons")
						}
					} else if !strings.Contains(out.String(), "risk "+tc.level) {
						t.Fatal("text lost risk")
					}
					if len(w.planner.specs) != 0 || len(w.applied) != 0 || w.confirmOpened != 0 {
						t.Fatal("read-only risk overview performed an action")
					}
				})
			}
		}
	}
}

func TestTidyOverviewV4RiskCountsFollowViewFilters(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
		count int
	}{
		{"human", nil, 2}, {"mine", []string{"--mine"}, 1}, {"other", []string{"--by-owner", "claude"}, 1}, {"excluded kind", []string{"--only", "branches"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newInteractive(t, true, true, "STW_OWNER=codex")
			rows := []tidywizard.RegistrationPrune{overviewPrune(0, "codex", owner.ScopeOwn), overviewPrune(1, "claude", owner.ScopeOther)}
			for i := range rows {
				p := &rows[i]
				p.ID = tidywizard.RegistrationPruneID(p.Repo, p.Name, p.GitDir)
			}
			overviewSource(t, tidywizard.RegistrationPruneResult{Rows: rows})
			args := append([]string{"--contract", "4", "--json"}, tc.flags...)
			out, err := runTidy(t, "must not be read", args...)
			if err != nil {
				t.Fatal(err)
			}
			d := overviewDocument(t, out)
			c := d.Summary.RegistrationPrunes
			if c == nil || c.Orange != tc.count || c.Green+c.Yellow+c.Red+c.Kept != 0 {
				t.Fatalf("counts=%+v", c)
			}
		})
	}
}
