// SPDX-License-Identifier: EUPL-1.2

package owner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
)

// ErrAgentMode is returned when an operation reserved for the human path is attempted in agent mode.
var ErrAgentMode = errors.New("only the human path may do this")

// Entry is a candidate identifier with its derived owner.
type Entry struct {
	ID    string
	Owner Owner
}

// View is the listing for the run. --by-owner narrows it to the items of those labels in any mode
// (a human may then choose them; an agent only sees them). --mine narrows it to own items. Agent
// mode without --by-owner shows own items only, where own requires a task record.
func View(entries []Entry, ev Evidence) []Entry {
	labels := map[string]bool{}
	for _, l := range ev.ByOwner {
		labels[l] = true
	}
	out := []Entry{}
	for _, e := range entries {
		switch {
		case len(labels) > 0 && !labels[e.Owner.Label]:
			continue
		case ev.Mode == ModeAgent && (ev.Mine || len(labels) == 0) && !ownForAgent(e):
			continue
		case ev.Mine && e.Owner.Scope != ScopeOwn:
			continue
		}
		out = append(out, e)
	}
	return out
}

// ownForAgent is the only way an item is own for an agent: the owner comes from the task record.
func ownForAgent(e Entry) bool {
	return e.Owner.Scope == ScopeOwn && e.Owner.Source == SourceTaskRecord
}

// Plannable returns the entries a run may put into a plan or --save. A human may choose anything
// (the proofs are identical for every owner). Agent mode plans own items only, and own requires
// owner_source task-record: other, unknown and path or author guesses are never included.
func Plannable(entries []Entry, ev Evidence) []Entry {
	out := []Entry{}
	for _, e := range entries {
		if ev.Mode == ModeAgent && !ownForAgent(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Authorize returns the sorted authorized_scopes for a plan. It is the only writer of that field
// and refuses in agent mode; only other and unknown need authorization.
func Authorize(ev Evidence, scopes ...Scope) ([]Scope, error) {
	if ev.Mode != ModeHuman {
		return nil, ErrAgentMode
	}
	seen := map[Scope]bool{}
	out := []Scope{}
	for _, s := range scopes {
		if s != ScopeOther && s != ScopeUnknown {
			return nil, fmt.Errorf("scope %q does not need authorization", s)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// PlanContent is what the content-addressed plan id binds: the sorted sub-plan ids, the derived
// values, the selected items, authorized_scopes and the mode evidence.
type PlanContent struct {
	SubPlans         []string         `json:"sub_plans"`
	Derived          []derive.Derived `json:"derived"`
	Selected         []string         `json:"selected"`
	AuthorizedScopes []Scope          `json:"authorized_scopes"`
	Mode             Evidence         `json:"mode"`
}

// PlanID returns the content-addressed id: 12 hex characters of the SHA-256 of the canonical
// content. Editing any bound field, authorized_scopes included, changes the id.
func PlanID(c PlanContent) string {
	return PlanDigest(c)[:12]
}

// PlanDigest returns the full SHA-256 of the same canonical content that PlanID binds.
func PlanDigest(c PlanContent) string {
	c.SubPlans = sortedCopy(c.SubPlans)
	c.Selected = sortedCopy(c.Selected)
	scopes := append([]Scope{}, c.AuthorizedScopes...)
	sort.Slice(scopes, func(i, j int) bool { return scopes[i] < scopes[j] })
	c.AuthorizedScopes = scopes
	c.Mode.Markers = sortedCopy(c.Mode.Markers)
	c.Mode.ByOwner = sortedCopy(c.Mode.ByOwner)
	if c.Derived == nil {
		c.Derived = []derive.Derived{}
	}
	b, _ := json.Marshal(c) // plain strings and bools: cannot fail
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}
