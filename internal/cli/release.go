// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/release"
)

// releaseFindingsError is the typed exit-status-3 error of `stw release check`: the result was
// complete and carried error findings. The full result has already been written.
type releaseFindingsError struct{ count int }

func (e releaseFindingsError) Error() string {
	if e.count == 1 {
		return "release check: 1 error finding"
	}
	return fmt.Sprintf("release check: %d error findings", e.count)
}

// releaseCommand groups the release process commands of Spec 026. It stays hidden until the
// documentation work item (WI9) lists it, so a partial stack never advertises a half feature.
func releaseCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "release", Short: "Release process commands (check the project release contract)",
		Long:         "Release process commands. check lints the project release contract (.stw/release.toml) without writes, network or project commands.",
		Args:         cobra.NoArgs,
		SilenceUsage: true, SilenceErrors: true, Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	command.AddCommand(releaseCheckCommand())
	return command
}

func releaseCheckCommand() *cobra.Command {
	var jsonOutput bool
	var product, commit string
	command := &cobra.Command{
		Use: "check [PATH]", Short: "Lint the project release contract without writes",
		Long: "Lint the project release contract (.stw/release.toml, .stw/release.json or the named product files\n.stw/release.<product>.*) of the repository that owns PATH.\n" +
			"Without --commit the working-tree files are read; with --commit the blobs of that full commit id are read.\n" +
			"Read-only: no write, no network and no project command; the only processes are the ownership detection and, with --commit, fixed Git reads.\n" +
			"Git is supported for both forms; jj and GitButler lint the working-tree file (--commit is refused as unsupported-backend).\n" +
			"Exit statuses: 0 the contract is valid; 1 refusal or failure; 2 usage error; 3 findings.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options := release.CheckOptions{Product: product}
			if len(args) == 1 {
				if args[0] == "" || !utf8.ValidString(args[0]) || containsNUL(args[0]) {
					return errors.New("path must be a non-empty UTF-8 path; omit it to check the current directory")
				}
				options.Path = args[0]
			}
			if cmd.Flags().Changed("commit") {
				if !release.IsFullCommitID(commit) {
					return errors.New("--commit must be a full commit id of 40 or 64 lowercase hexadecimal characters")
				}
				options.Commit = commit
			}
			result := release.Check(cmd.Context(), options)
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				_, err = io.WriteString(cmd.OutOrStdout(), result.Text())
			}
			if err != nil {
				return runtimeFailure{err}
			}
			switch result.Status {
			case "refused", "error":
				return runtimeFailure{fmt.Errorf("release check %s: %s: %s", result.Status, result.Error.Code, result.Error.Message)}
			case "findings":
				return releaseFindingsError{result.Summary.Errors}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "Output one versioned JSON result (including refusals)")
	command.Flags().StringVar(&product, "product", "", "Lint only this product (an empty value is absent)")
	command.Flags().StringVar(&commit, "commit", "", "Read the contract blobs of this full commit id instead of the working tree")
	return command
}

func containsNUL(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return true
		}
	}
	return false
}
