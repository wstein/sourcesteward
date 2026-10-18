// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
)

type readyOperation func(context.Context, string, hosting.ReadyOptions) hosting.ReadyResult

func readyCommand(requireJJ bool, run readyOperation) *cobra.Command {
	o := hosting.ReadyOptions{Options: hosting.Options{RequireJJ: requireJJ}}
	var asJSON bool
	cmd := &cobra.Command{Use: "ready-check NUMBER [PATH]", Short: "Assess caller-policy review evidence for one explicit PR", Long: "Bounded read-only caller-policy assessment. CI and effective constraints remain unknown; this increment cannot establish full eligibility. Installed profiles are unsupported before authentication. Native Git/jj ownership and gh jj's jj-only rule apply. No PR edit, check publication, build or activation. Limits: 30 seconds, 80 HTTP requests and 1 MiB output.", Args: func(_ *cobra.Command, args []string) error {
		if err := cobra.RangeArgs(1, 2)(nil, args); err != nil {
			return err
		}
		if args[0] == "" || strings.Trim(args[0], "0123456789") != "" {
			return fmt.Errorf("NUMBER must be a positive decimal integer")
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n <= 0 {
			return fmt.Errorf("NUMBER must be a positive decimal integer")
		}
		o.Number = n
		return hosting.ValidateReadyOptions(o)
	}, RunE: func(cmd *cobra.Command, args []string) error {
		path := ""
		if len(args) == 2 {
			path = args[1]
		}
		result := run(cmd.Context(), path, o)
		var err error
		if asJSON {
			var raw []byte
			raw, err = json.Marshal(result)
			if err == nil {
				err = writeReady(cmd.OutOrStdout(), string(raw)+"\n")
			}
		} else {
			err = renderReady(cmd.OutOrStdout(), result)
		}
		if err != nil {
			return runtimeFailure{err}
		}
		if result.Decision != "eligible" {
			return runtimeFailure{fmt.Errorf("Ready assessment is %s", result.Decision)}
		}
		return nil
	}}
	flags := cmd.Flags()
	flags.BoolVar(&asJSON, "json", false, "Output the versioned Ready assessment including operational failures")
	flags.StringVar(&o.PolicyLock, "policy-lock", "", "Explicit caller-owned policy lock outside the inspected target (required)")
	flags.StringVar(&o.Repo, "repo", "", "Explicit OWNER/NAME; does not override native ownership")
	flags.StringVar(&o.Host, "host", "github.com", "Explicit GitHub hostname")
	flags.StringVar(&o.Remote, "remote", "", "Select one native remote")
	return cmd
}
func writeReady(w io.Writer, text string) error {
	if len(text) > 1024*1024 {
		return fmt.Errorf("Ready output exceeded the one MiB limit")
	}
	n, err := io.WriteString(w, text)
	if err == nil && n != len(text) {
		return io.ErrShortWrite
	}
	return err
}
func renderReady(w io.Writer, r hosting.ReadyResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Ready: %q authority=%q observed=%q backend=%q\n", r.Decision, r.Policy.Authority, r.ObservedAt, hostingNullable(r.Backend))
	if len(r.Identity) > 0 {
		fmt.Fprintf(&b, "Native identity: %q\n", string(r.Identity))
	}
	if p := r.Repository; p != nil {
		fmt.Fprintf(&b, "Repository: %q/%q id=%d base=%q\n", p.Host, p.FullName, p.ID, p.BaseRef)
	}
	if p := r.PullRequest; p != nil {
		headRepo := "unknown"
		if p.HeadRepositoryID != nil {
			headRepo = strconv.FormatInt(*p.HeadRepositoryID, 10)
		}
		fmt.Fprintf(&b, "PR: %d head=%q ref=%q repository=%q base=%q ref=%q repository=%d state=%q merged=%t draft=%t\n", p.Number, p.HeadSHA, p.HeadRef, headRepo, p.BaseSHA, p.BaseRef, p.BaseRepositoryID, p.State, p.Merged, p.Draft)
	}
	fmt.Fprintf(&b, "Policy: %q version=%d generation=%d commit=%q sha256=%q lock=%q\nChecker: source=%q artifact=%q\n", r.Policy.ID, r.Policy.Version, r.Policy.Generation, r.Policy.Commit, r.Policy.SHA256, r.Policy.LockSHA256, r.Policy.Checker.SourceSHA, r.Policy.Checker.ArtifactSHA256)
	for _, view := range []struct {
		name  string
		value hosting.ReadyComponent
	}{{"review", r.Components.Review}, {"ci", r.Components.CI}, {"constraints", r.Components.Constraints}, {"deployment", r.Components.Deployment}} {
		fmt.Fprintf(&b, "Component: %q state=%q complete=%t\n", view.name, view.value.State, view.value.Complete)
		for _, reason := range view.value.Reasons {
			fmt.Fprintf(&b, "Reason: %q subject=%q\n", reason.Code, reason.Subject)
			for _, source := range reason.Sources {
				fmt.Fprintf(&b, "Source reference: %q field=%q\n", source.Source, source.Field)
			}
		}
	}
	rawBool := func(v *bool) string {
		if v == nil {
			return "unknown"
		}
		return strconv.FormatBool(*v)
	}
	fmt.Fprintf(&b, "GitHub reported (initial PR): mergeable=%q rebaseable=%q state=%q\n", rawBool(r.GitHubReported.Mergeable), rawBool(r.GitHubReported.Rebaseable), r.GitHubReported.MergeableState)
	fmt.Fprintf(&b, "Review report: %q sha256=%q tier=%q model=%q effort=%q runtime_attestation=%q\n", r.ReviewEvidence.ReportID, r.ReviewEvidence.ReportSHA256, r.ReviewEvidence.Tier, r.ReviewEvidence.ConfiguredModel.ID, r.ReviewEvidence.ConfiguredModel.Effort, r.ReviewEvidence.RuntimeAttestation)
	fmt.Fprintf(&b, "Complete: %t; network attempted: %t; requests=%d body-read-bytes=%d collection-pages=%d parsed-rows=%d\n", r.Boundary.Complete, r.Boundary.NetworkAttempted, r.Boundary.Usage.Requests, r.Boundary.Usage.Bytes, r.Boundary.Usage.Pages, r.Boundary.Usage.Records)
	for _, d := range r.Diagnostics {
		fmt.Fprintf(&b, "Diagnostic: %q %q\n", d.Code, d.Message)
	}
	for _, limit := range r.Boundary.Limitations {
		fmt.Fprintf(&b, "Limitation: %q\n", limit)
	}
	b.WriteString("Eligibility under the named policy at the sampled head and base; this grants no permission and does not predict a later merge. Required GitHub checks restrict merge, not the Ready UI transition.\n")
	return writeReady(w, b.String())
}
