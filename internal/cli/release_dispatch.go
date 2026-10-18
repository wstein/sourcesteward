// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/release"
)

func releaseDispatchCommand() *cobra.Command {
	return releaseDispatchCommandWithRunner(release.Dispatch)
}
func releaseDispatchCommandWithRunner(run func(context.Context, release.DispatchOptions) release.DispatchResult) *cobra.Command {
	var o release.DispatchOptions
	var jsonOutput bool
	command := &cobra.Command{Use: "dispatch (--commit C --version V | --tag T) [PATH]", Short: "Plan or explicitly apply one CI workflow dispatch", Long: "Evaluate fresh native and online build gates and print the exact request without sending it. --apply PLAN_ID recomputes the plan and sends one fixed POST only when the id matches and gates pass. A plan grants no build approval; explicit approval belongs to the caller's task context. Starts a draft-release build; no tag or publication. Exit: 0 pass/accepted; 1 refusal/failure; 2 usage; 3 failed plan gates.", Args: cobra.MaximumNArgs(1), SilenceUsage: true, SilenceErrors: true, RunE: func(cmd *cobra.Command, args []string) error {
		commit, tag := cmd.Flags().Changed("commit"), cmd.Flags().Changed("tag")
		if commit == tag {
			return errors.New("choose exactly --commit C --version V or --tag T")
		}
		if commit {
			if !release.IsFullCommitID(o.Commit) || o.Version == "" {
				return errors.New("--commit requires a full lowercase commit id and --version")
			}
		} else {
			if !validExplicitReleaseTag(o.Tag) || cmd.Flags().Changed("version") {
				return errors.New("--tag requires a valid tag and forbids --version")
			}
		}
		if cmd.Flags().Changed("apply") && (len(o.Apply) != 64 || !release.IsFullCommitID(o.Apply)) {
			return errors.New("--apply requires a plan id of 64 lowercase hexadecimal characters")
		}
		if len(args) == 1 {
			if !validReleasePath(args[0]) {
				return errors.New("path must be non-empty UTF-8 without NUL")
			}
			o.Path = args[0]
		}
		if err := hosting.ValidateOptions(hosting.Options{Kind: "repo", Repo: o.Repo, Host: o.Host}); err != nil {
			return err
		}
		if containsNUL(o.Version) {
			return errors.New("version must not contain NUL")
		}
		result := run(cmd.Context(), o)
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
			if result.Error != nil {
				return runtimeFailure{fmt.Errorf("release dispatch: %s: %s", result.Error.Code, result.Error.Message)}
			}
			return runtimeFailure{errors.New("release dispatch refused")}
		}
		if result.Verdict == "fail" {
			return releasePlanFailed{command: "dispatch"}
		}
		if result.Verdict != "pass" {
			return runtimeFailure{errors.New("release dispatch: incomplete")}
		}
		return nil
	}}
	command.Flags().StringVar(&o.Commit, "commit", "", "Build this full commit id of main")
	command.Flags().StringVar(&o.Tag, "tag", "", "Build this existing annotated tag (Git only)")
	command.Flags().StringVar(&o.Version, "version", "", "Version for commit mode")
	command.Flags().StringVar(&o.Product, "product", "", "Select this product")
	command.Flags().StringVar(&o.Repo, "repo", "", "Select GitHub repository OWNER/NAME")
	command.Flags().StringVar(&o.Host, "host", "", "Select GitHub host (Enterprise dispatch is unverified)")
	command.Flags().StringVar(&o.Apply, "apply", "", "Recompute and apply this matching plan id; requires explicit build approval")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Output one versioned result")
	return command
}

// --tag already determines the selector kind. Only the positional validator's
// abbreviated-hex ambiguity rule is bypassed; all other tag rules stay intact.
func validExplicitReleaseTag(tag string) bool {
	return validReleaseTag(tag) || (tag != "" && len(tag) <= 255 && strings.Trim(tag, "0123456789abcdefABCDEF") == "")
}
