// SPDX-License-Identifier: EUPL-1.2
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// DocumentV4 is the explicit, unpublished overview-4 envelope. Summary retains
// its legacy candidate meaning; registration rows carry their separate verdicts.
type DocumentV4 struct {
	Document
	RegistrationPrunes []tidywizard.RegistrationPrune `json:"registration_prunes"`
}

// BuildV4 preserves the legacy builder and omits legacy worktree rows represented
// by exact repository/destination registration rows. Input slices are not mutated.
func BuildV4(in Input, rows []tidywizard.RegistrationPrune) DocumentV4 {
	type target struct{ repo, path string }
	represented := map[target]bool{}
	prunes := append([]tidywizard.RegistrationPrune{}, rows...)
	for i := range prunes {
		represented[target{prunes[i].Repo, prunes[i].Destination}] = true
		prunes[i].Effects = append([]string{}, prunes[i].Effects...)
		prunes[i].Limitations = append([]string{}, prunes[i].Limitations...)
	}
	sort.SliceStable(prunes, func(i, j int) bool {
		a, b := prunes[i], prunes[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.GitDir != b.GitDir {
			return a.GitDir < b.GitDir
		}
		return a.ID < b.ID
	})
	candidates := []tidywizard.Candidate{}
	for _, c := range in.Result.Candidates {
		if (c.Kind == tidywizard.KindWorktree || c.Kind == tidywizard.KindStaleWorktree) && represented[target{c.Repo, c.Path}] {
			continue
		}
		candidates = append(candidates, c)
	}
	in.Result.Candidates = candidates
	d := DocumentV4{Document: Build(in), RegistrationPrunes: prunes}
	d.SchemaVersion = 4
	if len(d.RegistrationPrunes) > tidywizard.MaxCandidates {
		d.RegistrationPrunes = d.RegistrationPrunes[:tidywizard.MaxCandidates]
		d.Truncated = true
	}
	left := tidywizard.MaxCandidates - len(d.RegistrationPrunes)
	if len(d.Candidates) > left {
		d.Candidates = d.Candidates[:left]
		d.Truncated = true
	}
	return d
}

// JSONV4 keeps both candidate arrays within the common document byte limit.
// No legacy manifest ID is reinterpreted as a version-2 plan-set identity.
func JSONV4(d DocumentV4) ([]byte, bool, error) {
	if d.PlanID != "" {
		if len(d.PlanID) != 64 || strings.IndexFunc(d.PlanID, func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') }) >= 0 {
			return nil, false, fmt.Errorf("overview-4 plan id requires the full lowercase 64-hex manifest identity")
		}
	}
	encode := func() ([]byte, error) {
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		e.SetIndent("", "  ")
		if err := e.Encode(d); err != nil {
			return nil, err
		}
		return escapeInvisible(b.Bytes()), nil
	}
	b, err := encode()
	if err != nil {
		return nil, false, err
	}
	for len(b) > MaxDocumentBytes && (len(d.Candidates) > 0 || len(d.RegistrationPrunes) > 0) {
		d.Truncated = true
		if len(d.Candidates) > 0 {
			d.Candidates = d.Candidates[:len(d.Candidates)*3/4]
		}
		if len(d.RegistrationPrunes) > 0 {
			d.RegistrationPrunes = d.RegistrationPrunes[:len(d.RegistrationPrunes)*3/4]
		}
		b, err = encode()
		if err != nil {
			return nil, false, err
		}
	}
	if len(b) > MaxDocumentBytes {
		return nil, false, fmt.Errorf("overview-4 exceeds %d bytes without candidate rows", MaxDocumentBytes)
	}
	return b, d.Truncated, nil
}

type v4TextWriter struct {
	b   strings.Builder
	cut bool
}

const v4CutNotice = "output-limit: the overview was cut to the document byte bound\n"

func (w *v4TextWriter) line(format string, args ...any) bool {
	if w.cut {
		return false
	}
	line := fmt.Sprintf(format, args...) + "\n"
	if w.b.Len()+len(line)+len(v4CutNotice) > MaxDocumentBytes {
		w.cut = true
		return false
	}
	w.b.WriteString(line)
	return true
}

type v4DisplayRow struct {
	legacy *tidywizard.Candidate
	prune  *tidywizard.RegistrationPrune
}
type v4DisplayGroup struct {
	rank        int
	owner, repo string
	rows        []v4DisplayRow
}

func v4DisplayGroups(d DocumentV4) []v4DisplayGroup {
	type key struct {
		rank        int
		owner, repo string
	}
	groups := map[key][]v4DisplayRow{}
	add := func(scope owner.Scope, label, repo string, row v4DisplayRow) {
		rank := 2
		if label != "" {
			if scope == owner.ScopeOwn {
				rank = 0
			} else if scope == owner.ScopeOther {
				rank = 1
			}
		}
		if rank == 2 {
			label = ""
		}
		k := key{rank, label, repo}
		groups[k] = append(groups[k], row)
	}
	for i := range d.Candidates {
		c := &d.Candidates[i]
		add(c.Scope, c.Owner, c.Repo, v4DisplayRow{legacy: c})
	}
	for i := range d.RegistrationPrunes {
		p := &d.RegistrationPrunes[i]
		add(p.Scope, p.Owner, p.Repo, v4DisplayRow{prune: p})
	}
	out := []v4DisplayGroup{}
	for k, rows := range groups {
		out = append(out, v4DisplayGroup{k.rank, k.owner, k.repo, rows})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.owner != b.owner {
			return a.owner < b.owner
		}
		return a.repo < b.repo
	})
	return out
}

// TextV4 renders typed registration rows without treating them as integration
// candidates or offering an implicit selection. Both field and document bounds hold.
func TextV4(d DocumentV4, _ TextOptions) (string, bool) {
	e := untrusted.Esc
	w := v4TextWriter{}
	w.line("Workspace: %s", e(d.Workspace))
	for _, v := range d.Derived {
		if !w.line("Derived: %s %s (%s)", e(v.Name), e(v.Value), e(v.Source)) {
			break
		}
	}
	w.line("Mode: %s", modeLine(d.Mode))
	for _, r := range d.Repositories {
		if !w.line("Repository: %s backend=%s base=%s state=%s reason=%s", e(r.Name), e(r.Backend), baseOrNone(r.Base, e), e(r.State), e(r.Reason)) {
			break
		}
		for _, n := range d.repoNotices[r.Name] {
			if !w.line("  notice: %s", n) {
				break
			}
		}
	}
rows:
	for _, g := range v4DisplayGroups(d) {
		label := e(g.owner)
		suffix := "unknown"
		if g.rank == 0 {
			suffix = "own"
		} else if g.rank == 1 {
			suffix = "other"
		}
		if g.rank == 2 {
			label = "unknown"
		}
		if !w.line("\nowner %s (%s) / repository %s", label, suffix, e(g.repo)) {
			break
		}
		for i, r := range g.rows {
			if i == MaxRowsPerRepository {
				w.line("  and %d more (use --json for the complete bounded rows)", len(g.rows)-i)
				break
			}
			if r.legacy != nil {
				if !w.line("  %s", strings.TrimSuffix(row(*r.legacy, 0, e), "\n")) {
					break rows
				}
				continue
			}
			p := r.prune
			if !w.line("  [-] %s %s %s at %s", e(string(p.Verdict)), e(p.Kind), e(p.Name), e(p.Destination)) {
				break rows
			}
			if !w.line("    gitdir %s task %s reason %s proof %s", e(p.GitDir), e(p.Task), e(p.Reason), e(p.Proof)) {
				break rows
			}
			for _, effect := range p.Effects {
				if !w.line("    effect: %s", e(effect)) {
					break rows
				}
			}
			for _, limit := range p.Limitations {
				if !w.line("    limitation: %s", e(limit)) {
					break rows
				}
			}
		}
	}
	w.line("\nlegacy candidates: %d safe, %d retain, %d decision", d.Summary.Safe, d.Summary.Retain, d.Summary.Decision)
	counts := map[tidywizard.RegistrationPruneVerdict]int{}
	for _, p := range d.RegistrationPrunes {
		counts[p.Verdict]++
	}
	w.line("registration prunes: %d safe, %d retain, %d blocked, %d unknown", counts[tidywizard.RegistrationPruneSafe], counts[tidywizard.RegistrationPruneRetain], counts[tidywizard.RegistrationPruneBlocked], counts[tidywizard.RegistrationPruneUnknown])
	for _, n := range d.runNotices {
		if !w.line("notice: %s", e(n)) {
			break
		}
	}
	if d.Truncated {
		w.line("truncated: the overview is partial; omitted rows are not selected")
	}
	if w.cut {
		w.b.WriteString(v4CutNotice)
	}
	return w.b.String(), w.cut || d.Truncated
}
