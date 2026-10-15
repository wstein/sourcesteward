// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/inspection"
)

func inspectCommand() *cobra.Command {
	jsonOutput := false
	command := &cobra.Command{Use: "inspect [PATH]", Short: "Inspect local repository identity without writes", Long: "Inspect local repository identity without writes, index refresh, hooks or network.\nGit 2.46+ is required. Recognized jj/GitButler workspaces are reported as unsupported.\nHEAD is sampled twice; metadata races and ABA changes are not transactionally detected.", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path := ""
		if len(args) == 1 {
			path = args[0]
		}
		r := inspection.Inspect(cmd.Context(), path)
		if err := cmd.Context().Err(); err != nil && r.Status == "ok" {
			return runtimeFailure{err}
		}
		if jsonOutput {
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(r); err != nil {
				return runtimeFailure{err}
			}
		} else if r.Status == "ok" {
			repo := r.Repository
			workspace := "bare"
			if repo.Worktree != nil {
				workspace = fmt.Sprintf("%q", *repo.Worktree)
			}
			head := repo.Head.State
			if repo.Head.Ref != nil {
				head += fmt.Sprintf(" %q", *repo.Head.Ref)
			}
			if repo.Head.OID != nil {
				head += " " + *repo.Head.OID
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Backend: %s\nWorktree: %s\nGit directory: %q\nCommon directory: %q\nBare: %t\nLinked worktree: %t\nObject format: %s\nHEAD: %s\n", r.Backend, workspace, repo.GitDir, repo.CommonDir, repo.Bare, repo.LinkedWorktree, repo.ObjectFormat, head); err != nil {
				return runtimeFailure{err}
			}
		}
		if r.Status != "ok" {
			return runtimeFailure{r}
		}
		return nil
	}}
	command.Flags().BoolVar(&jsonOutput, "json", false, "Output one versioned inspection result (including operational errors)")
	return command
}
