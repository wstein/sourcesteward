// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func runPruneV4History(cmd *cobra.Command, args []string, f tidyRestoreFlags) error {
	r := workspace.HistoryRegistrationPruneV4(cmd.Context(), workspace.RegistrationPruneV4HistoryOptions{Path: workspacePath(args), StateDir: f.stateDir, Limit: f.limit})
	return writeTidyRestoreValue(cmd, r, f.structured, r.Status, r.Command, func(w io.Writer) error { return renderPruneV4History(w, r) })
}

func renderPruneV4History(w io.Writer, r workspace.RegistrationPruneV4HistoryResult) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Tidy operation: %q\nContract: 4\nOperation: %q\nStatus: %q\nTotal: %d truncated=%t\n", r.Command, r.Operation, r.Status, r.Total, r.Truncated)
	for _, entry := range r.Entries {
		fmt.Fprintf(&text, "Journal: plan=%q time=%q phase=%q registration=%q gitdir=%q destination=%q recovery=%q payload=%q history_ref=%q history_oid=%q\n", entry.PlanID, entry.Time, entry.Phase, entry.Registration.Name, entry.Registration.GitDir, entry.Registration.Destination, entry.RecoveryStatus, entry.PayloadPath, entry.HistoryRef, entry.HistoryOID)
		for _, effect := range entry.Effects {
			fmt.Fprintf(&text, "  Recorded effect: action=%q target=%q intent=%q outcome=%q\n", effect.Action, effect.Target, effect.Intent, effect.Outcome)
		}
	}
	for _, truncation := range r.Truncation {
		fmt.Fprintf(&text, "Truncated: unit=%q shown=%d total_at_least=%d\n", truncation.Unit, truncation.Shown, truncation.Total)
	}
	for _, limitation := range r.Limitations {
		fmt.Fprintf(&text, "Limit: %q\n", limitation)
	}
	for _, diagnostic := range r.Diagnostics {
		fmt.Fprintf(&text, "Diagnostic: %q %q\n", diagnostic.Code, diagnostic.Message)
	}
	_, err := io.WriteString(w, text.String())
	return err
}
