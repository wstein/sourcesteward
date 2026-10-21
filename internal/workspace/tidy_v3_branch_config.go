// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"strings"
	"unicode/utf8"
)

// Branch evidence of the recovery journal (Spec 031 T045). Before a branch is
// deleted, the journal records the `branch.<name>.*` configuration and the
// reflog tip of that branch, so that a restore can bring the upstream
// configuration back. The record is evidence only: a read failure is recorded as
// `unavailable` and never stops the deletion. The branch name is data: it is
// matched exactly, never as a prefix of another branch's section.

const (
	branchConfigKeyLimit   = 16  // recorded keys per branch
	branchConfigValueLimit = 256 // bytes per recorded value
	branchConfigVarLimit   = 64  // bytes per variable name

	branchStatusRecorded    = "recorded"
	branchStatusUnavailable = "unavailable"
)

// V3BranchConfigItem is one recorded `branch.<name>.<var>` entry. Var is the
// lower-case variable name; the section prefix is derived from the plan's branch
// name and is not stored. A value cut at the bound is marked Truncated.
type V3BranchConfigItem struct {
	Var       string `json:"var"`
	Value     string `json:"value"`
	Truncated bool   `json:"truncated"`
}

// V3BranchEvidence is the evidence of one deleted branch. ConfigTruncated marks
// that more keys existed than the bound keeps. ReflogTip is the newest reflog
// object ID or `unavailable`.
type V3BranchEvidence struct {
	ConfigStatus    string               `json:"config_status"`
	Config          []V3BranchConfigItem `json:"config"`
	ConfigTruncated bool                 `json:"config_truncated"`
	BranchOID       string               `json:"branch_oid"`
	ReflogTip       string               `json:"reflog_tip"`
}

func cloneBranchEvidence(e *V3BranchEvidence) *V3BranchEvidence {
	if e == nil {
		return nil
	}
	c := *e
	c.Config = append([]V3BranchConfigItem{}, e.Config...)
	return &c
}

// branchConfigRegexp matches the keys of exactly one branch section, with every
// regular-expression metacharacter of the name escaped. Matching is repeated
// exactly in Go (see parseBranchConfig): the pattern only narrows the read.
func branchConfigRegexp(short string) string {
	var b strings.Builder
	b.WriteString(`^branch\.`)
	for _, r := range short {
		if strings.ContainsRune(`.[]\^$*+?(){}|`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteString(`\.`)
	return b.String()
}

func validBranchVar(v string) bool {
	if v == "" || len(v) > branchConfigVarLimit {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// parseBranchConfig reads the NUL separated `key\nvalue` records of
// `git config --null --get-regexp` and keeps those whose key is exactly
// `branch.<short>.<var>`: a variable name has no dot, so the keys of `a.b` (or
// the hostile section `a.remote`) never belong to `a`.
func parseBranchConfig(out, short string) (items []V3BranchConfigItem, truncated bool) {
	prefix := "branch." + short + "."
	items = []V3BranchConfigItem{}
	for _, record := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(record, "\n")
		rest, ok := strings.CutPrefix(key, prefix)
		if !ok || !validBranchVar(rest) {
			continue
		}
		if len(items) == branchConfigKeyLimit {
			truncated = true
			break
		}
		item := V3BranchConfigItem{Var: rest, Value: value}
		if len(value) > branchConfigValueLimit {
			cut := branchConfigValueLimit
			for cut > 0 && !utf8.ValidString(value[:cut]) {
				cut--
			}
			item.Value, item.Truncated = value[:cut], true
		}
		items = append(items, item)
	}
	return items, truncated
}

// readBranchEvidence collects the evidence of one branch with typed, read-only
// Git commands under the planning environment. It never fails: what cannot be
// read is recorded as unavailable.
func readBranchEvidence(ctx context.Context, bin string, id NativeIdentity, name, oid string) *V3BranchEvidence {
	ev := &V3BranchEvidence{ConfigStatus: branchStatusUnavailable, Config: []V3BranchConfigItem{}, BranchOID: oid, ReflogTip: branchStatusUnavailable}
	short, ok := strings.CutPrefix(name, restoreRefPrefix)
	if !ok || short == "" {
		return ev
	}
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return ev
	}
	defer closeSession()
	out, code, err := runSandboxed(ctx, bin, []string{"config", "--local", "--null", "--get-regexp", branchConfigRegexp(short)}, id.WorkspaceRoot, nil, nil)
	if err == nil && (code == 0 || code == 1) {
		ev.ConfigStatus = branchStatusRecorded
		if code == 0 {
			ev.Config, ev.ConfigTruncated = parseBranchConfig(out, short)
		}
	}
	out, code, err = runSandboxed(ctx, bin, []string{"reflog", "show", "--no-abbrev", "--format=%H", "-n", "1", name, "--"}, id.WorkspaceRoot, nil, nil)
	if tip := strings.TrimSpace(out); err == nil && code == 0 && validRestoreHexOID(tip) {
		ev.ReflogTip = tip
	}
	return ev
}

// validBranchEvidence is the journal validation of recorded evidence: the
// bounds, the exact variable syntax and the binding to the planned branch tip.
func validBranchEvidence(e V3BranchEvidence, c V3Candidate) bool {
	if e.BranchOID != c.OID || e.Config == nil || len(e.Config) > branchConfigKeyLimit || (e.ReflogTip != branchStatusUnavailable && !validRestoreHexOID(e.ReflogTip)) {
		return false
	}
	switch e.ConfigStatus {
	case branchStatusRecorded:
	case branchStatusUnavailable:
		if len(e.Config) != 0 || e.ConfigTruncated {
			return false
		}
	default:
		return false
	}
	if e.ConfigTruncated && len(e.Config) != branchConfigKeyLimit {
		return false
	}
	for _, item := range e.Config {
		if !validBranchVar(item.Var) || len(item.Value) > branchConfigValueLimit || !utf8.ValidString(item.Value) || strings.ContainsRune(item.Value, 0) {
			return false
		}
	}
	return true
}
