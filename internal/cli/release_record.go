// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/release"
	"io"
)

type releaseRecordFailed struct{}

func (releaseRecordFailed) Error() string { return "release record: inputs disagree or check failed" }
func releaseRecordCommand() *cobra.Command {
	var o release.RecordOptions
	var jsonOutput bool
	c := &cobra.Command{Use: "record --commit C --version V --from DIR [PATH]", Short: "Create a canonical release record from existing files", Args: cobra.MaximumNArgs(1), SilenceUsage: true, SilenceErrors: true, RunE: func(cmd *cobra.Command, args []string) error {
		if !release.IsFullCommitID(o.Commit) || o.Version == "" || !validReleasePath(o.From) {
			return errors.New("--commit (full lowercase id), --version and --from are required")
		}
		if o.Write && cmd.Flags().Changed("check") {
			return errors.New("--write and --check are mutually exclusive")
		}
		if cmd.Flags().Changed("check") && !validReleasePath(o.Check) {
			return errors.New("--check requires a non-empty path")
		}
		if cmd.Flags().Changed("signer-workflow-file") && !release.ValidRepoPath(o.SignerWorkflowFile) {
			return errors.New("--signer-workflow-file must be a repository-relative path")
		}
		for _, v := range []struct {
			name string
			n    int64
		}{{"run-id", o.RunID}, {"run-attempt", o.RunAttempt}} {
			if cmd.Flags().Changed(v.name) && (v.n < 0 || v.name == "run-attempt" && v.n == 0) {
				return errors.New("run id must be nonnegative and run attempt must be positive")
			}
		}
		if len(args) == 1 {
			if !validReleasePath(args[0]) {
				return errors.New("path must be non-empty UTF-8 without NUL")
			}
			o.Path = args[0]
		}
		if e := hosting.ValidateOptions(hosting.Options{Kind: "repo", Repo: o.Repo}); e != nil {
			return e
		}
		o.RunIDSet = cmd.Flags().Changed("run-id")
		o.RunAttemptSet = cmd.Flags().Changed("run-attempt")
		r := release.Record(cmd.Context(), o)
		var e error
		if jsonOutput {
			e = json.NewEncoder(cmd.OutOrStdout()).Encode(r)
		} else {
			_, e = io.WriteString(cmd.OutOrStdout(), r.Text())
		}
		if e != nil {
			return runtimeFailure{e}
		}
		if r.Status == "failed" {
			return releaseRecordFailed{}
		}
		if r.Status != "ok" {
			return runtimeFailure{fmt.Errorf("release record: %s: %s", r.Error.Code, r.Error.Message)}
		}
		return nil
	}}
	c.Flags().StringVar(&o.Commit, "commit", "", "Built full commit id")
	c.Flags().StringVar(&o.Version, "version", "", "Built version")
	c.Flags().StringVar(&o.From, "from", "", "Directory containing build manifest, checksums and assets")
	c.Flags().StringVar(&o.Product, "product", "", "Select this product")
	c.Flags().StringVar(&o.Repo, "repo", "", "Repository OWNER/NAME (must equal manifest)")
	c.Flags().StringVar(&o.SignerWorkflowFile, "signer-workflow-file", "", "Explicit signer workflow path")
	c.Flags().Int64Var(&o.RunID, "run-id", 0, "Run id (must equal manifest)")
	c.Flags().Int64Var(&o.RunAttempt, "run-attempt", 0, "Run attempt (must equal manifest)")
	c.Flags().BoolVar(&o.Write, "write", false, "Create the record file exclusively; identical file is a no-op")
	c.Flags().StringVar(&o.Check, "check", "", "Compare a file with the canonical record")
	c.Flags().BoolVar(&jsonOutput, "json", false, "Output one versioned JSON result")
	return c
}
