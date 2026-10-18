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
	"unicode/utf8"
)

func releaseVerifyCommand() *cobra.Command {
	var o release.VerifyOptions
	var jsonOutput bool
	c := &cobra.Command{Use: "verify <tag> [PATH]", Short: "Verify a release without building or publishing", Args: cobra.RangeArgs(1, 2), SilenceUsage: true, SilenceErrors: true, RunE: func(cmd *cobra.Command, args []string) error {
		if release.IsFullCommitID(args[0]) || !validReleaseTag(args[0]) {
			return errors.New("verify requires a valid tag name")
		}
		o.Subject = args[0]
		if len(args) == 2 {
			if !validReleasePath(args[1]) {
				return errors.New("path must be non-empty UTF-8 without NUL")
			}
			o.Path = args[1]
		}
		if cmd.Flags().Changed("record-commit") && !release.IsFullCommitID(o.RecordCommit) {
			return errors.New("--record-commit must be a full lowercase commit id")
		}
		if (cmd.Flags().Changed("signer-workflow-file") || cmd.Flags().Changed("source-ref")) && !o.Published {
			return errors.New("--signer-workflow-file and --source-ref require --published")
		}
		if cmd.Flags().Changed("artifacts") && !validReleasePath(o.Artifacts) {
			return errors.New("--artifacts must be a non-empty path")
		}
		if cmd.Flags().Changed("signer-workflow-file") && !release.ValidRepoPath(o.SignerWorkflowFile) {
			return errors.New("--signer-workflow-file must be a repository-relative file path")
		}
		if cmd.Flags().Changed("source-ref") && (!validReleasePath(o.SourceRef) || o.SourceRef == "") {
			return errors.New("--source-ref must be non-empty UTF-8 without NUL")
		}
		if e := hosting.ValidateOptions(hosting.Options{Kind: "repo", Repo: o.Repo}); e != nil {
			return e
		}
		r := release.Verify(cmd.Context(), o)
		var e error
		if jsonOutput {
			e = json.NewEncoder(cmd.OutOrStdout()).Encode(r)
		} else {
			_, e = io.WriteString(cmd.OutOrStdout(), r.Text())
		}
		if e != nil {
			return runtimeFailure{e}
		}
		if r.Status != "ok" {
			return runtimeFailure{fmt.Errorf("release verify: %s: %s", r.Error.Code, r.Error.Message)}
		}
		if r.Verdict == "fail" {
			return releasePlanFailed{command: "verify"}
		}
		if r.Verdict == "incomplete" {
			return runtimeFailure{errors.New("release verify: incomplete")}
		}
		return nil
	}}
	c.Flags().StringVar(&o.Product, "product", "", "Select this product")
	c.Flags().StringVar(&o.RecordCommit, "record-commit", "", "Read record at this full commit id")
	c.Flags().StringVar(&o.Repo, "repo", "", "Select repository OWNER/NAME")
	c.Flags().StringVar(&o.Artifacts, "artifacts", "", "Directory of release files")
	c.Flags().BoolVar(&o.Online, "online", false, "Evaluate online evidence")
	c.Flags().BoolVar(&o.WithDrafts, "with-drafts", false, "Assert token sees drafts")
	c.Flags().BoolVar(&o.Published, "published", false, "Audit an already published release")
	c.Flags().StringVar(&o.SignerWorkflowFile, "signer-workflow-file", "", "Explicit signer workflow path (published audit only)")
	c.Flags().StringVar(&o.SourceRef, "source-ref", "", "Explicit attestation source ref (published audit only)")
	c.Flags().BoolVar(&jsonOutput, "json", false, "Output one versioned JSON result")
	return c
}
func validReleasePath(s string) bool { return s != "" && utf8.ValidString(s) && !containsNUL(s) }
