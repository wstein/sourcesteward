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
	command := &cobra.Command{Use: "inspect [PATH]", Short: "Inspect local repository identity without writes", Long: "Inspect local repository identity without repository writes, snapshots, hooks or network.\nGit 2.46+ and native jj 0.45.1+ are Tier 1 backends; GitButler remains unsupported.\nNative jj requires read-only bubblewrap (Linux) or sandbox-exec (macOS), with a ten-second deadline.\nPrivate temporary configuration outside the inspected repository is created and removed.\njj reads stored unsnapshotted state; disk freshness is unknown.\nIdentity is sampled twice; metadata races and ABA changes are not transactionally detected.", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
			if r.JJRepository != nil {
				repo := r.JJRepository
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Backend: jj\nWorkspace: %q\nWorkspace name: %q\njj metadata: %q\nShared repository: %q\nChange ID: %s\nCommit ID: %s\nOperation ID: %s\nObservation: %s\n", repo.WorkspaceRoot, repo.WorkspaceName, repo.MetadataDir, repo.RepositoryDir, repo.ChangeID, repo.CommitID, repo.OperationID, repo.Observation.Limitation); err != nil {
					return runtimeFailure{err}
				}
				return nil
			}
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
