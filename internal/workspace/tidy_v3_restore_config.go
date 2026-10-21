// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"strings"
)

// Branch configuration of a restore (Spec 031 T045). The plan reads the evidence
// the journal recorded and says per key whether it can be reapplied; it shows key
// names and the remote name, never other values. Only the keys that tie a
// branch to its upstream are ever reapplied.

// Key states of the restore plan.
const (
	configReapply         = "reapply"
	configSkipRemote      = "skip-remote-missing"
	configSkipNotRecorded = "skip-not-recorded" // not one of the reapplied keys
	configSkipTruncated   = "skip-truncated"
	configSkipMulti       = "skip-multi-valued"
	configSkipSet         = "skip-already-set"
	configSkipRenamed     = "skip-renamed"
	configSkipNotRestored = "skip-not-restorable"

	configApplied = "applied"
	configSkipped = "skipped"
	configFailed  = "failed"
)

// reappliedConfigVars are the only variables a restore writes back.
var reappliedConfigVars = []string{"remote", "merge", "rebase", "pushremote"}

func reappliedConfigVar(v string) bool {
	for _, k := range reappliedConfigVars {
		if k == v {
			return true
		}
	}
	return false
}

// V3RestoreConfigKey is one recorded key of a deleted branch. Remote names the
// remote a `remote` or `pushremote` key points to; no other value is shown.
// Outcome is set by an apply.
type V3RestoreConfigKey struct {
	Key     string  `json:"key"`
	Remote  *string `json:"remote,omitempty"`
	State   string  `json:"state"`
	Outcome string  `json:"outcome,omitempty"`
}

// V3RestoreBranchConfig is the recorded evidence of one entry as the plan shows it.
type V3RestoreBranchConfig struct {
	Status    string               `json:"status"`
	ReflogTip string               `json:"reflog_tip"`
	BranchOID string               `json:"branch_oid"`
	Truncated bool                 `json:"truncated"`
	Keys      []V3RestoreConfigKey `json:"keys"`
}

// readRemotes lists the remotes of the clone by name.
func (n restoreNative) readRemotes(ctx context.Context) (map[string]bool, error) {
	text, code, err := n.run(ctx, []string{"remote"})
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, failure("git-query-refused", "Native remote reads were refused.")
	}
	remotes := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if line != "" {
			remotes[line] = true
		}
	}
	return remotes, nil
}

// readBranchVars returns the variables that `branch.<short>.*` holds now.
func (n restoreNative) readBranchVars(ctx context.Context, short string) (map[string]bool, error) {
	text, code, err := n.run(ctx, []string{"config", "--local", "--null", "--get-regexp", branchConfigRegexp(short)})
	if err != nil {
		return nil, err
	}
	if code != 0 && code != 1 {
		return nil, failure("git-query-refused", "Native configuration reads were refused.")
	}
	vars := map[string]bool{}
	items, _ := parseBranchConfig(text, short)
	for _, item := range items {
		vars[item.Var] = true
	}
	return vars, nil
}

// planBranchConfig derives the key states of one entry from its recorded
// evidence, the remotes and the keys the branch holds now. Entries whose branch
// will not be created get `skip-not-restorable`.
func planBranchConfig(item restoreItem, restorable bool, remotes, current map[string]bool) *V3RestoreBranchConfig {
	ev := item.journal.Branch
	if ev == nil {
		return nil
	}
	short := strings.TrimPrefix(item.origin, restoreRefPrefix)
	bc := &V3RestoreBranchConfig{Status: ev.ConfigStatus, ReflogTip: ev.ReflogTip, BranchOID: ev.BranchOID, Truncated: ev.ConfigTruncated, Keys: []V3RestoreConfigKey{}}
	count := map[string]int{}
	var recordedRemote *V3BranchConfigItem
	for i, item := range ev.Config {
		count[item.Var]++
		if item.Var == "remote" && recordedRemote == nil {
			recordedRemote = &ev.Config[i]
		}
	}
	remoteUsable := func(name string) bool { return name == "." || remotes[name] }
	for _, k := range ev.Config {
		key := V3RestoreConfigKey{Key: "branch." + short + "." + k.Var}
		if (k.Var == "remote" || k.Var == "pushremote") && !k.Truncated && validText(k.Value) && !strings.ContainsAny(k.Value, "\x00\r\n") {
			remote := k.Value
			key.Remote = &remote
		}
		switch {
		case !reappliedConfigVar(k.Var):
			key.State = configSkipNotRecorded
		case !restorable:
			key.State = configSkipNotRestored
		case item.entry.Ref != item.origin:
			key.State = configSkipRenamed
		case k.Truncated:
			key.State = configSkipTruncated
		case count[k.Var] > 1:
			key.State = configSkipMulti
		case current[k.Var]:
			key.State = configSkipSet
		case (k.Var == "remote" || k.Var == "pushremote") && !remoteUsable(k.Value):
			key.State = configSkipRemote
		case (k.Var == "merge" || k.Var == "rebase") && recordedRemote != nil && (recordedRemote.Truncated || !remoteUsable(recordedRemote.Value)):
			key.State = configSkipRemote
		default:
			key.State = configReapply
		}
		bc.Keys = append(bc.Keys, key)
	}
	return bc
}

// planBranchConfigs sets the key states of every item whose journal carries
// evidence. The reads happen once for the remotes and once per entry with a
// candidate key.
func (n restoreNative) planBranchConfigs(ctx context.Context, items []restoreItem) error {
	var remotes map[string]bool
	for i := range items {
		item := &items[i]
		if item.journal.Branch == nil {
			continue
		}
		restorable := item.entry.State == restoreRestorable
		var current map[string]bool
		if restorable && len(item.journal.Branch.Config) > 0 {
			var err error
			if remotes == nil {
				if remotes, err = n.readRemotes(ctx); err != nil {
					return err
				}
			}
			if current, err = n.readBranchVars(ctx, strings.TrimPrefix(item.origin, restoreRefPrefix)); err != nil {
				return err
			}
		}
		item.entry.BranchConfig = planBranchConfig(*item, restorable, remotes, current)
	}
	return nil
}

// restoreConfigResult is the per-key outcome the restore journal records.
type restoreConfigResult struct {
	Var     string `json:"var"`
	Outcome string `json:"outcome"`
}

func hasConfigFailure(e V3RestoreEntry) bool {
	if e.BranchConfig == nil {
		return false
	}
	for _, k := range e.BranchConfig.Keys {
		if k.Outcome == configFailed {
			return true
		}
	}
	return false
}

// setBranchConfig writes one key of the local configuration. The key and the
// value are separate arguments after `--`; only the configuration file and its
// lock are writable.
func (n restoreNative) setBranchConfig(ctx context.Context, key, value string) error {
	ro, err := gitTidyWriteProtectionFiles(n.id, nil, []string{"config"})
	if err != nil {
		return err
	}
	args := []string{"config", "--local", "--", key, value}
	if n.observe != nil {
		n.observe(args)
	}
	_, code, err := runSandboxed(ctx, n.bin, args, n.id.WorkspaceRoot, []string{n.id.RepositoryDir}, ro)
	if err != nil {
		return gitExecutionError(err)
	}
	if code != 0 {
		return failure("restore-config-failed", "A configuration key could not be written.")
	}
	return nil
}

// reapplyConfig reapplies the keys of one restored entry that the plan marks
// `reapply`, after re-evaluating them against the remotes and keys of the clone
// right now. A failure never undoes the branch: it is reported per key and as a
// diagnostic. The result lists the variables with their outcome for the restore
// journal.
func (c tidyV3RestoreCoordinator) reapplyConfig(ctx context.Context, n restoreNative, item *restoreItem, r *V3RestoreResult, restorable bool) []restoreConfigResult {
	ev := item.journal.Branch
	if ev == nil || len(ev.Config) == 0 {
		return nil
	}
	short := strings.TrimPrefix(item.origin, restoreRefPrefix)
	remotes, rerr := n.readRemotes(ctx)
	current, cerr := n.readBranchVars(ctx, short)
	bc := planBranchConfig(*item, restorable, remotes, current)
	results := []restoreConfigResult{}
	failed := false
	for i := range bc.Keys {
		k := &bc.Keys[i]
		k.Outcome = configSkipped
		if k.State != configReapply {
			results = append(results, restoreConfigResult{Var: ev.Config[i].Var, Outcome: k.Outcome})
			continue
		}
		var err error
		if rerr != nil || cerr != nil {
			err = failure("restore-config-failed", "The clone could not be read before the key was reapplied.")
		} else {
			err = n.setBranchConfig(ctx, k.Key, ev.Config[i].Value)
		}
		if err != nil {
			k.Outcome, failed = configFailed, true
		} else {
			k.Outcome = configApplied
		}
		results = append(results, restoreConfigResult{Var: ev.Config[i].Var, Outcome: k.Outcome})
	}
	item.entry.BranchConfig = bc
	if failed && !hasDiagnostic(r.Diagnostics, "restore-config-failed") {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "restore-config-failed", Message: "Branch configuration could not be reapplied for at least one restored branch; the branches were created."})
	}
	return results
}

func hasDiagnostic(ds []Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}
