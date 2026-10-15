// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/inspection"
)

func inventoryCommand() *cobra.Command { return inventoryCommandWith(inspection.Inventory) }

func inventoryCommandWith(run func(context.Context, string, inspection.InventoryOptions) inspection.InventoryResult) *cobra.Command {
	var options inspection.InventoryOptions
	var jsonOutput bool
	command := &cobra.Command{
		Use: "inventory [PATH]", Short: "Inventory local Git worktrees or native jj workspaces without writes",
		Long: "Inventory bounded, sampled local Git worktrees or native jj workspaces and references.\nNo repository writes, snapshots, refresh, hooks or network. Git 2.46+ and native jj 0.45.1+\nare Tier 1 backends; GitButler is unsupported. Native jj requires read-only bubblewrap\n(Linux) or sandbox-exec (macOS), with private temporary configuration outside the target.\nThe total deadline is ten seconds. Native jj disk freshness remains unknown without snapshotting.\nPaths are literal; reference and revision selectors never evaluate user expressions.\nKnown false predicates exit 0; partial/unknown or operational failures exit 1; usage errors exit 2.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := inspection.ValidateInventoryOptions(options); err != nil {
				return err
			}
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			result := run(cmd.Context(), path, options)
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				err = renderInventoryText(cmd.OutOrStdout(), result)
			}
			if err != nil {
				return runtimeFailure{err}
			}
			if err = cmd.Context().Err(); err != nil {
				return runtimeFailure{err}
			}
			if result.Status != "ok" || (result.Query != nil && result.Query.Value == "unknown") {
				return runtimeFailure{result}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.BoolVar(&jsonOutput, "json", false, "Output one versioned inventory result, including operational errors")
	flags.StringVar(&options.Query, "query", "", "Predicate: clean, dirty, ref-exists or ancestor")
	flags.StringVar(&options.Ref, "ref", "", "Literal reference name for ref-exists")
	flags.StringVar(&options.From, "from", "", "Native source revision for ancestor")
	flags.StringVar(&options.To, "to", "", "Native destination revision for ancestor")
	flags.StringVar(&options.Base, "base", "", "Explicit native integration base")
	flags.StringVar(&options.Revision, "revision", "", "Native revision for containment or stored comparison")
	flags.StringArrayVar(&options.Remotes, "remote", nil, "Sample a named remote's local references (repeatable; never fetches)")
	flags.StringArrayVar(&options.Scopes, "scope", nil, "Scope: tracked, staged, disk, stored, untracked or ignored (repeatable)")
	flags.StringArrayVar(&options.Paths, "path", nil, "Literal relative path filter (repeatable; no glob expansion)")
	flags.StringVar(&options.Anchor, "anchor", "", "Path anchor: workspace (default) or cwd")
	return command
}

func renderInventoryText(w io.Writer, r inspection.InventoryResult) error {
	// Render into one buffer so formatting has no effects and writer errors terminate immediately.
	var text strings.Builder
	fmt.Fprintf(&text, "Inventory: %s\nBackend: %s\nInput: %q\nComplete: %t\nFreshness: %q\n", r.Status, r.Backend, r.InputPath, r.Boundary.Complete, r.Boundary.Freshness)
	for _, v := range r.Workspaces {
		fmt.Fprintf(&text, "Workspace: %q path=%s manager=%q registration=%q owner=%q commit=%s change=%s operation=%s tracked=%s untracked=%s conflicts=%s\n", v.Name, inventoryNullable(v.Path), v.Manager, v.Registration, v.TaskOwner, inventoryNullable(v.CommitID), inventoryNullable(v.ChangeID), inventoryNullable(v.OperationID), inventoryObservationText(v.Tracked), inventoryObservationText(v.Untracked), inventoryObservationText(v.Conflicts))
		for _, s := range v.Limitations {
			fmt.Fprintf(&text, "  Limitation: %q\n", s)
		}
	}
	for _, v := range r.References {
		fmt.Fprintf(&text, "Reference: %q kind=%q remote=%s commit=%s change=%s upstream=%s tracked=%s contains=%s merged=%s local-only=%s\n", v.Name, v.Kind, inventoryNullable(v.Remote), inventoryNullable(v.CommitID), inventoryNullable(v.ChangeID), inventoryNullable(v.Upstream), inventoryObservationText(v.Tracked), inventoryObservationText(v.ContainsRevision), inventoryObservationText(v.MergedIntoBase), inventoryObservationText(v.LocalOnly))
		for _, s := range v.Upstreams {
			fmt.Fprintf(&text, "  Upstream: %q\n", s)
		}
		for _, target := range v.AddedTargets {
			fmt.Fprintf(&text, "  Added target: commit=%q change=%s\n", target.CommitID, inventoryNullable(target.ChangeID))
		}
		for _, target := range v.RemovedTargets {
			fmt.Fprintf(&text, "  Removed target: commit=%q change=%s\n", target.CommitID, inventoryNullable(target.ChangeID))
		}
		for _, s := range v.Limitations {
			fmt.Fprintf(&text, "  Limitation: %q\n", s)
		}
	}
	for _, v := range r.Bases {
		fmt.Fprintf(&text, "Base: %q commit=%s change=%s provenance=%q selected=%t\n", v.Name, inventoryNullable(v.CommitID), inventoryNullable(v.ChangeID), v.Provenance, v.Selected)
		for _, s := range v.Limitations {
			fmt.Fprintf(&text, "  Limitation: %q\n", s)
		}
	}
	for _, v := range r.Files {
		fmt.Fprintf(&text, "File: %q old=%s scope=%q change=%q\n", v.Path, inventoryNullable(v.OldPath), v.Scope, v.Change)
		for _, s := range v.Limitations {
			fmt.Fprintf(&text, "  Limitation: %q\n", s)
		}
	}
	if r.Query != nil {
		fmt.Fprintf(&text, "Query: %q = %s reason=%q\n", r.Query.Predicate, r.Query.Value, r.Query.Reason)
		for _, s := range r.Query.Evidence {
			fmt.Fprintf(&text, "  Evidence: %q\n", s)
		}
	}
	for _, s := range r.Boundary.Limitations {
		fmt.Fprintf(&text, "Limitation: %q\n", s)
	}
	for _, d := range r.Diagnostics {
		fmt.Fprintf(&text, "Diagnostic: %q %q\n", d.Code, d.Message)
	}
	_, err := io.WriteString(w, text.String())
	return err
}
func inventoryNullable(value *string) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%q", *value)
}
func inventoryObservationText(value inspection.InventoryObservation) string {
	return fmt.Sprintf("%q (%q)", value.Value, value.Reason)
}
