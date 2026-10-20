// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/workspace"
)

var tidyV3PlanFlags = []string{"online", "remote", "tracking-remote", "repo", "host", "pr", "completed-worktree", "protect-worktree"}
var tidyV3ApplyFlags = []string{"online", "completed-worktree", "owner", "protect-worktree"}

// Counting wraps the existing typed flag value without changing Cobra parsing.
// Only the explicit v3 branch rejects repeats; old contracts retain their meaning.
type tidyV3FlagValue interface {
	String() string
	Type() string
	Set(string) error
}
type tidyV3CountedFlag struct {
	value tidyV3FlagValue
	count int
}

func (v *tidyV3CountedFlag) String() string     { return v.value.String() }
func (v *tidyV3CountedFlag) Type() string       { return v.value.Type() }
func (v *tidyV3CountedFlag) Set(s string) error { v.count++; return v.value.Set(s) }

var tidyV3Singletons = []string{"contract", "base", "state-dir", "task", "owner", "remove-registration", "remove-files", "json", "online", "remote", "tracking-remote", "repo", "host", "completed-worktree", "plan", "authorize"}

func trackTidyV3Singletons(cmd *cobra.Command) {
	for _, name := range tidyV3Singletons {
		if flag := cmd.Flags().Lookup(name); flag != nil {
			flag.Value = &tidyV3CountedFlag{value: flag.Value}
		}
	}
}

// Explicitly supplied empty scalar selectors cannot silently choose a different
// mode. Cobra retains flag presence independently of its zero value.
func validateTidyV3PresentFlags(cmd *cobra.Command) error {
	for _, name := range tidyV3Singletons {
		if flag := cmd.Flags().Lookup(name); flag != nil {
			if value, ok := flag.Value.(*tidyV3CountedFlag); ok && value.count > 1 {
				return fmt.Errorf("--%s must occur exactly once in contract 3", name)
			}
		}
	}
	for _, name := range []string{"base", "state-dir", "task", "owner", "remove-files", "remote", "tracking-remote", "repo", "host", "completed-worktree"} {
		flag := cmd.Flags().Lookup(name)
		if flag != nil && flag.Changed && flag.Value.String() == "" {
			return fmt.Errorf("--%s requires a nonempty value", name)
		}
	}
	return nil
}
func rejectTidyV3Flags(cmd *cobra.Command, names []string) error {
	for _, name := range names {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--%s requires --contract 3", name)
		}
	}
	return nil
}
func addTidyV3PlanFlags(cmd *cobra.Command, contract *string, o *workspace.V3PlanOptions, hints *[]string) {
	f := cmd.Flags()
	f.StringVar(contract, "contract", "", "Select exactly contract 3; omission preserves existing offline contracts")
	f.BoolVar(&o.Online, "online", false, "Explicitly permit bounded authenticated GitHub reads")
	f.StringVar(&o.Remote, "remote", "", "Select the configured remote for online merged-head evidence")
	f.StringVar(&o.TrackingRemote, "tracking-remote", "", "Select only local tracking refs of this remote; requires --online")
	f.StringVar(&o.Repo, "repo", "", "Expected OWNER/NAME; must agree with the selected remote")
	f.StringVar(&o.Host, "host", "", "Selected online host; initially github.com only")
	f.StringArrayVar(hints, "pr", nil, "Select an exact full head OID=positive PR number (repeatable)")
	f.StringVar(&o.CompletedWorktree, "completed-worktree", "", "Exact completed legacy path; asserts exclusive retirement authority")
	f.StringArrayVar(&o.ProtectWorktrees, "protect-worktree", []string{}, "Declare every caller-known active absolute workspace (repeatable)")
}
func addTidyV3ApplyFlags(cmd *cobra.Command, contract *string, o *workspace.V3ApplyOptions) {
	f := cmd.Flags()
	f.StringVar(contract, "contract", "", "Apply only an immutable contract 3 plan")
	f.BoolVar(&o.Online, "online", false, "Acknowledge exactly the stored plan's authenticated GitHub reads")
	f.StringVar(&o.CompletedWorktree, "completed-worktree", "", "Reassert the exact legacy completion path and exclusive authority")
	f.StringVar(&o.Owner, "owner", "", "Repeat the legacy completion owner label")
	f.StringArrayVar(&o.ProtectWorktrees, "protect-worktree", []string{}, "Repeat every exact legacy active-workspace protection (repeatable)")
}
func tidyV3Path(s string) bool {
	return s != "" && len(s) <= 4096 && utf8.ValidString(s) && !strings.ContainsRune(s, 0) && filepath.IsAbs(s) && filepath.Clean(s) == s
}
func tidyV3Paths(paths []string) bool {
	seen := map[string]bool{}
	for _, path := range paths {
		if !tidyV3Path(path) || seen[path] {
			return false
		}
		seen[path] = true
	}
	return true
}
func tidyV3Ref(s string) bool {
	if s == "" || len(s) > 4096 || !utf8.ValidString(s) || !strings.HasPrefix(s, "refs/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.ContainsAny(s, " ~^:?*[\\") || strings.Contains(s, "//") {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
func validateTidyV3PlanCLI(args []string, o *workspace.V3PlanOptions, hints []string) error {
	bad := func() error {
		return fmt.Errorf("contract 3 requires one exact mode, canonical paths and explicit online selectors")
	}
	if len(o.Refs)+len(hints)+len(o.ProtectWorktrees) > 1024 {
		return fmt.Errorf("contract 3 exceeds 1024 selectors")
	}
	if err := validateWorkspaceCLI("base", o.Base); err != nil {
		return err
	}
	if (len(args) > 0 && !tidyV3Path(o.Path)) || (o.StateDir != "" && !tidyV3Path(o.StateDir)) || !tidyV3Paths(o.ProtectWorktrees) {
		return bad()
	}
	retirement := o.Task != "" || o.CompletedWorktree != ""
	if retirement {
		if o.Task != "" && o.CompletedWorktree != "" || o.StateDir == "" || !o.RemoveRegistration || !tidyV3Path(o.RemoveFiles) || len(o.Refs) > 0 || o.TrackingRemote != "" {
			return bad()
		}
		if err := validateWorkspaceCLI("owner", o.Owner); err != nil {
			return err
		}
		if o.Task != "" {
			if err := validateWorkspaceCLI("task", o.Task); err != nil {
				return err
			}
			if len(o.ProtectWorktrees) > 0 {
				return bad()
			}
		} else if !tidyV3Path(o.CompletedWorktree) || o.CompletedWorktree != o.RemoveFiles {
			return bad()
		}
	} else if o.Owner != "" || o.RemoveRegistration || o.RemoveFiles != "" || len(o.ProtectWorktrees) > 0 {
		return bad()
	}
	if !o.Online {
		if o.Remote != "" || o.TrackingRemote != "" || o.Host != "" || o.Repo != "" || len(hints) > 0 {
			return bad()
		}
	} else {
		remote := o.Remote
		if o.TrackingRemote != "" {
			if remote != "" || len(hints) > 0 {
				return bad()
			}
			remote = o.TrackingRemote
		}
		if err := validateWorkspaceCLI("owner", remote); err != nil {
			return fmt.Errorf("online evidence requires one valid remote identifier")
		}
		if o.Host != "" && o.Host != "github.com" {
			return bad()
		}
		if o.Repo != "" {
			parts := strings.Split(o.Repo, "/")
			if len(parts) != 2 {
				return bad()
			}
			for _, part := range parts {
				if err := validateWorkspaceCLI("owner", part); err != nil {
					return bad()
				}
			}
		}
	}
	prefix := "refs/heads/"
	if o.TrackingRemote != "" {
		prefix = "refs/remotes/" + o.TrackingRemote + "/"
	}
	seen := map[string]bool{}
	for _, ref := range o.Refs {
		if !tidyV3Ref(ref) || !strings.HasPrefix(ref, prefix) || len(ref) <= len(prefix) || seen[ref] {
			return bad()
		}
		seen[ref] = true
	}
	o.PRHints = []workspace.V3PRHint{}
	selectors := map[string]string{}
	for _, hint := range hints {
		oid, number, ok := strings.Cut(hint, "=")
		if !ok || len(oid) != 40 || strings.Trim(oid, "0") == "" || number == "" {
			return tidyV3MalformedHint(hint)
		}
		for _, c := range oid {
			if !strings.ContainsRune("0123456789abcdef", c) {
				return tidyV3MalformedHint(hint)
			}
		}
		for _, c := range number {
			if c < '0' || c > '9' {
				return tidyV3MalformedHint(hint)
			}
		}
		n, err := strconv.ParseInt(number, 10, 64)
		if err != nil || n <= 0 {
			return tidyV3MalformedHint(hint)
		}
		if first, ok := selectors[oid]; ok {
			if first == hint {
				continue
			}
			return fmt.Errorf("--pr %s conflicts with --pr %s for head %s", tidyV3Quote(first), tidyV3Quote(hint), oid)
		}
		o.PRHints = append(o.PRHints, workspace.V3PRHint{OID: oid, Number: n})
		selectors[oid] = hint
	}
	return nil
}
func validateTidyV3ApplyCLI(o workspace.V3ApplyOptions) error {
	if err := validateWorkspaceCLI("plan", o.PlanID, "state-dir", o.StateDir); err != nil {
		return err
	}
	if !tidyV3Path(o.StateDir) || !tidyV3Paths(o.ProtectWorktrees) || len(o.ProtectWorktrees) > 1024 {
		return fmt.Errorf("contract 3 requires canonical state and protection paths")
	}
	if o.CompletedWorktree != "" {
		if !tidyV3Path(o.CompletedWorktree) {
			return fmt.Errorf("--completed-worktree requires an exact absolute path")
		}
		return validateWorkspaceCLI("owner", o.Owner)
	}
	if o.Owner != "" || len(o.ProtectWorktrees) > 0 {
		return fmt.Errorf("completion owner/protections require --completed-worktree")
	}
	return nil
}
func writeTidyV3Result(cmd *cobra.Command, r workspace.V3Result, structured bool) error {
	var err error
	if structured {
		err = json.NewEncoder(cmd.OutOrStdout()).Encode(r)
	} else {
		err = renderTidyV3Text(cmd.OutOrStdout(), r)
	}
	if err != nil {
		return runtimeFailure{err}
	}
	if err = cmd.Context().Err(); err != nil {
		return runtimeFailure{err}
	}
	if r.Status != "ok" {
		// Some syntax relationships require the typed native observation (for
		// example a hint whose OID is not among the selected heads). They
		// remain usage errors, and cannot acquire online or local authority.
		if r.Plan == nil && !r.Boundary.NetworkAttempted && len(r.Actions) == 0 {
			for _, d := range r.Diagnostics {
				if d.Code == "invalid-options" {
					return fmt.Errorf("%s", d.Message)
				}
			}
		}
		return runtimeFailure{fmt.Errorf("tidy operation %q: %q", r.Command, r.Status)}
	}
	return nil
}
func renderTidyV3Text(w io.Writer, r workspace.V3Result) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Tidy operation: %q\nContract: %d\nStatus: %q\nBackend: %q\nComplete: %t\nNetwork attempted: %t\nFreshness: %q\nObserved at: %q\n", r.Command, r.SchemaVersion, r.Status, r.Backend, r.Boundary.Complete, r.Boundary.NetworkAttempted, r.Boundary.Freshness, r.Boundary.ObservedAt)
	fmt.Fprintf(&text, "Measured budget: native_processes=%d native_bytes=%d http_requests=%d http_bytes=%d observations=%d user_bytes=%d metadata_bytes=%d framing_bytes=%d stderr_bytes=%d\n", r.Boundary.Budget.NativeProcesses, r.Boundary.Budget.NativeBytes, r.Boundary.Budget.HTTPRequests, r.Boundary.Budget.HTTPBytes, r.Boundary.Budget.Observations, r.Boundary.Budget.UserBytes, r.Boundary.Budget.MetadataBytes, r.Boundary.Budget.FramingBytes, r.Boundary.Budget.StderrBytes)
	if p := r.Plan; p != nil {
		fmt.Fprintf(&text, "Plan: %q\nDigest: %q\nMode: %q\nSource: %q\nState directory: %q\nBase selector: %q\nBase commit: %q\n", p.PlanID, p.Digest, p.Mode, p.SourcePath, p.StateDir, p.Expected.BaseSelector, p.Expected.BaseCommit)
		if p.Online != nil {
			fmt.Fprintf(&text, "Online repository: host=%q name=%q id=%d node_id=%q remote=%q\n", p.Online.Repository.Host, p.Online.Repository.Name, p.Online.Repository.ID, p.Online.Repository.NodeID, p.Online.Remote)
		}
		for _, c := range p.Candidates {
			fmt.Fprintf(&text, "Candidate: %q kind=%q oid=%q decision=%q reason=%q proof=%q\n", c.Name, c.Kind, c.OID, c.Decision, c.Reason, c.Proof)
			for _, path := range c.WorkspacePaths {
				fmt.Fprintf(&text, "  Workspace: %q\n", path)
			}
			if c.Evidence != nil {
				data, err := json.Marshal(c.Evidence)
				if err != nil {
					return err
				}
				fmt.Fprintf(&text, "  Proof evidence: %q\n", data)
			}
		}
		if r := p.Retirement; r != nil {
			fmt.Fprintf(&text, "Retirement: provenance=%q destination=%q registration=%q target=%q content_policy=%q registration_selected=%t files_selected=%q\n", r.Provenance.Kind, r.Destination.Path, r.Registration.Path, r.Target.CommitID, r.ContentPolicy, r.RemoveRegistration, r.RemoveFiles)
			facts, err := json.Marshal(r)
			if err != nil {
				return err
			}
			// Quote the complete evidence as opaque text, including Unicode format
			// controls. Human review retains every typed fact without terminal effects.
			fmt.Fprintf(&text, "Retirement facts: %q\n", facts)
		}
		for _, e := range p.Effects {
			fmt.Fprintf(&text, "Effect: %q\n", e)
		}
		for _, limit := range p.RecoveryLimits {
			fmt.Fprintf(&text, "Recovery limit: %q\n", limit)
		}
	}
	for _, a := range r.Actions {
		fmt.Fprintf(&text, "Action: %q status=%q detail=%q\n", a.Name, a.Status, a.Detail)
	}
	for _, d := range r.Diagnostics {
		fmt.Fprintf(&text, "Diagnostic: %q %q\n", d.Code, d.Message)
	}
	for _, l := range r.Limitations {
		fmt.Fprintf(&text, "Limitation: %q\n", l)
	}
	_, err := io.WriteString(w, text.String())
	return err
}

// tidyV3Quote escapes a flag value and bounds it to the 4,096-byte message limit.
func tidyV3Quote(v string) string {
	q := strconv.Quote(v)
	if len(q) > 4096 {
		q = q[:4096]
	}
	return q
}
func tidyV3MalformedHint(v string) error {
	return fmt.Errorf("--pr %s is not FULL_OID=POSITIVE_NUMBER", tidyV3Quote(v))
}
