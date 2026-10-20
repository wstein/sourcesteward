// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
)

// v3HintSelector renders a hint as the flag text that selected it, escaped and
// bounded to the Spec 029 4,096-byte message limit.
func v3HintSelector(h V3PRHint) string {
	q := strconv.Quote(h.OID + "=" + strconv.FormatInt(h.Number, 10))
	if len(q) > 4096 {
		q = q[:4096]
	}
	return "--pr " + q[1:len(q)-1]
}

// normalizeV3PRHints keys hints by head OID: identical repeats collapse to one
// hint (Spec 031 FR-014) and the result is ordered by OID so equal selections
// store equal plans. Callers validate conflicts first.
func normalizeV3PRHints(hints []V3PRHint) []V3PRHint {
	byOID := map[string]V3PRHint{}
	for _, h := range hints {
		byOID[h.OID] = h
	}
	out := make([]V3PRHint, 0, len(byOID))
	for _, h := range byOID {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OID < out[j].OID })
	return out
}

type V3PlanOptions struct {
	Path, Base, StateDir               string
	Refs                               []string
	Task, Owner                        string
	CompletedWorktree                  string
	RemoveRegistration                 bool
	RemoveFiles                        string
	ProtectWorktrees                   []string
	Online                             bool
	Remote, TrackingRemote, Repo, Host string
	PRHints                            []V3PRHint
}
type V3ApplyOptions struct {
	StateDir, PlanID         string
	Authorize, Online        bool
	CompletedWorktree, Owner string
	ProtectWorktrees         []string
}

func validateV3PlanOptions(o V3PlanOptions) error {
	bad := func() error {
		return failure("invalid-options", "V3 options must select one exact mode, canonical paths and explicit online inputs.")
	}
	if !v3CanonicalPath(o.Path) || o.Base == "" || !validText(o.Base) || (o.StateDir != "" && !v3CanonicalPath(o.StateDir)) || !v3Paths(o.ProtectWorktrees) || (o.Host != "" && o.Host != "github.com") {
		return bad()
	}
	retire := o.Task != "" || o.CompletedWorktree != ""
	if o.Task != "" && o.CompletedWorktree != "" {
		return bad()
	}
	if retire {
		if o.StateDir == "" || !validID(o.Owner) || !o.RemoveRegistration || !v3CanonicalPath(o.RemoveFiles) || len(o.Refs) > 0 || o.TrackingRemote != "" {
			return bad()
		}
		if o.Task != "" {
			if !validID(o.Task) || len(o.ProtectWorktrees) > 0 {
				return bad()
			}
		} else if !v3CanonicalPath(o.CompletedWorktree) || o.RemoveFiles != o.CompletedWorktree {
			return bad()
		}
	} else if o.Owner != "" || o.RemoveRegistration || o.RemoveFiles != "" || len(o.ProtectWorktrees) > 0 {
		return bad()
	}
	if !o.Online {
		if o.Remote != "" || o.TrackingRemote != "" || o.Host != "" || o.Repo != "" || len(o.PRHints) > 0 {
			return bad()
		}
	} else {
		remote := o.Remote
		if o.TrackingRemote != "" {
			if remote != "" || len(o.PRHints) > 0 {
				return bad()
			}
			remote = o.TrackingRemote
		}
		if !validID(remote) {
			return bad()
		}
		if o.Repo != "" && !v3Repo(V3Repository{Host: "github.com", ID: 1, NodeID: "selector", Name: o.Repo}) {
			return bad()
		}
	}
	refs := map[string]bool{}
	for _, name := range o.Refs {
		prefix := "refs/heads/"
		if o.TrackingRemote != "" {
			prefix = "refs/remotes/" + o.TrackingRemote + "/"
		}
		if !v3Ref(name) || len(name) <= len(prefix) || name[:len(prefix)] != prefix || refs[name] {
			return bad()
		}
		refs[name] = true
	}
	hints := map[string]int64{}
	for _, h := range o.PRHints {
		if !v3OID(h.OID, "sha1") || h.Number <= 0 {
			return bad()
		}
		if n, ok := hints[h.OID]; ok && n != h.Number {
			return failure("invalid-options", fmt.Sprintf("%s conflicts with %s for head %s.", v3HintSelector(V3PRHint{OID: h.OID, Number: n}), v3HintSelector(h), h.OID))
		}
		hints[h.OID] = h.Number
	}
	if len(o.Refs)+len(o.PRHints)+len(o.ProtectWorktrees) > observationLimit {
		return bad()
	}
	return nil
}
func validateV3ApplyOptions(o V3ApplyOptions, p V3Plan) error {
	if !o.Authorize || o.StateDir != p.StateDir || o.PlanID != p.PlanID || !v3CanonicalPath(o.StateDir) || !validID(o.PlanID) || o.Online != (p.Online != nil) || !v3Paths(o.ProtectWorktrees) {
		return failure("authorization-required", "Apply requires exact stored identity and explicit local/online acknowledgments.")
	}
	if p.Mode == "legacy-retirement" {
		if p.Retirement == nil || p.Retirement.Provenance.Legacy == nil {
			return failure("invalid-plan", "Legacy provenance is missing.")
		}
		l := p.Retirement.Provenance.Legacy
		if o.CompletedWorktree != l.SelectedPath || o.Owner != l.Owner || !reflect.DeepEqual(o.ProtectWorktrees, l.ProtectedRoots) {
			return failure("completion-required", "Repeat the exact current completion owner, destination and complete protection set.")
		}
	} else if o.CompletedWorktree != "" || o.Owner != "" || len(o.ProtectWorktrees) > 0 {
		return failure("invalid-options", "Completion declarations apply only to legacy retirement.")
	}
	return nil
}
