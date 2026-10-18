// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
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
	command.AddCommand(releaseCheckCommand(), releasePlanCommand())
	return command
}

func releaseCheckCommand() *cobra.Command {
	var jsonOutput bool
	var product, commit string
	command := &cobra.Command{
		Use: "check [PATH]", Short: "Lint the project release contract without writes",
		Long: "Lint the project release contract (.stw/release.toml, .stw/release.json or the named product files\n.stw/release.<product>.*) of the repository that owns PATH.\n" +
			"Without --commit the working-tree files are read; with --commit the blobs of that full commit id are read.\n" +
			"Read-only: no write, no network and no project command; the only processes are the ownership detection and, with --commit, fixed isolated Git reads or operation-pinned native jj reads in the read-only sandbox.\n" +
			"Git and jj support commit reads; GitButler lints the working-tree file only.\n" +
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

type releasePlanFailed struct{}

func (releasePlanFailed) Error() string { return "release plan: gates failed" }

func releasePlanCommand() *cobra.Command {
	var options release.PlanOptions
	var jsonOutput bool
	command := &cobra.Command{Use: "plan <commit|tag> [PATH]", Short: "Explain release gates without execution", Long: "Read the evaluated commit's release contract and display gates and argv as data. No project command, build, write or offline network request. Offline net gates are not-run. A plan is not authorization.\nExit statuses: 0 pass; 1 refusal, failure or incomplete; 2 usage; 3 failed gates.", Args: cobra.RangeArgs(1, 2), SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Subject = args[0]
			commit := release.IsFullCommitID(options.Subject)
			if !commit && !validReleaseTag(options.Subject) {
				return errors.New("subject must be a full lowercase commit id or a valid tag name")
			}
			if commit {
				if options.Version == "" {
					return errors.New("--version is required with a commit")
				}
				if options.Mode == "publish" {
					return errors.New("--for publish requires a tag")
				}
				if options.Mode == "" {
					options.Mode = "build"
				}
			} else {
				if cmd.Flags().Changed("version") {
					return errors.New("--version is forbidden with a tag")
				}
				if options.Mode == "" {
					options.Mode = "publish"
				}
			}
			if options.Mode != "build" && options.Mode != "publish" {
				return errors.New("--for must be build or publish")
			}
			if cmd.Flags().Changed("record-commit") && (!release.IsFullCommitID(options.RecordCommit) || options.Mode != "publish") {
				return errors.New("--record-commit requires publish mode and a full lowercase commit id")
			}
			if len(args) == 2 {
				if args[1] == "" || !utf8.ValidString(args[1]) || containsNUL(args[1]) {
					return errors.New("path must be non-empty UTF-8 without NUL")
				}
				options.Path = args[1]
			}
			if err := hosting.ValidateOptions(hosting.Options{Kind: "repo", Repo: options.Repo}); err != nil {
				return err
			}
			result := release.Plan(cmd.Context(), options)
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				_, err = io.WriteString(cmd.OutOrStdout(), result.Text())
			}
			if err != nil {
				return runtimeFailure{err}
			}
			if result.Status != "ok" {
				return runtimeFailure{fmt.Errorf("release plan %s: %s: %s", result.Status, result.Error.Code, result.Error.Message)}
			}
			if result.Verdict == "incomplete" {
				return runtimeFailure{errors.New("release plan: incomplete")}
			}
			if result.Verdict == "fail" {
				return releasePlanFailed{}
			}
			return nil
		}}
	command.Flags().StringVar(&options.Product, "product", "", "Select this product")
	command.Flags().StringVar(&options.Version, "version", "", "Version to build (commit only)")
	command.Flags().StringVar(&options.Mode, "for", "", "build or publish (default depends on subject)")
	command.Flags().StringVar(&options.RecordCommit, "record-commit", "", "Read record from this full commit id (publish only)")
	command.Flags().StringVar(&options.Repo, "repo", "", "Select GitHub repository OWNER/NAME")
	command.Flags().BoolVar(&options.Online, "online", false, "Evaluate bounded read-only online gates")
	command.Flags().BoolVar(&options.WithDrafts, "with-drafts", false, "Assert token sees drafts")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Output one versioned JSON result")
	return command
}

func validReleaseTag(s string) bool {
	if s == "" || len(s) > 255 || !utf8.ValidString(s) || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.ContainsAny(s, " ~^:?*[\\") || strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.Contains(s, "//") || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	// Abbreviated hexadecimal identities are ambiguous input, not resolved as refs.
	if len(s) < 64 {
		hex := true
		for _, c := range s {
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				hex = false
			}
		}
		if hex {
			return false
		}
	}
	return true
}

func containsNUL(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return true
		}
	}
	return false
}
