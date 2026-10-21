// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/inspection"
)

type hostingOperation func(context.Context, string, hosting.Options) hosting.Result
type hostingStatusOperation func(context.Context, string, hosting.Options) hosting.PRStatusResult
type hostingLookupOperation func(context.Context, string, hosting.Options) hosting.LookupResult

// hostingTransport is a test seam for in-process HTTP fixtures; production leaves it nil (HTTPS).
var hostingTransport http.RoundTripper

func defaultHostingStatus(ctx context.Context, path string, o hosting.Options) hosting.PRStatusResult {
	return hosting.InspectPRStatusWithTransport(ctx, path, o, hostingTransport)
}
func defaultHostingLookup(ctx context.Context, path string, o hosting.Options) hosting.LookupResult {
	return hosting.InspectLookupWithTransport(ctx, path, o, hostingTransport)
}

func hostingCommand() *cobra.Command { return hostingCommandWith(false, hosting.Inspect) }
func hostingCommandWith(requireJJ bool, run hostingOperation) *cobra.Command {
	return hostingCommandWithOps(requireJJ, run, defaultHostingStatus, defaultHostingLookup)
}
func hostingCommandWithOps(requireJJ bool, run hostingOperation, status hostingStatusOperation, lookup hostingLookupOperation) *cobra.Command {
	group := &cobra.Command{Use: "hosting", Short: "Resolve repositories and explicitly inspect GitHub PRs and CI without writes"}
	group.AddCommand(hostingCommandsWithOps(requireJJ, run, status, lookup)...)
	return group
}
func hostingCommands(requireJJ bool, run hostingOperation) []*cobra.Command {
	return hostingCommandsWithOps(requireJJ, run, defaultHostingStatus, defaultHostingLookup)
}
func hostingCommandsWithOps(requireJJ bool, run hostingOperation, status hostingStatusOperation, lookup hostingLookupOperation) []*cobra.Command {
	var commands []*cobra.Command
	for _, kind := range []string{"repo", "pr", "prs", "ci"} {
		commands = append(commands, hostingLeaf(kind, requireJJ, run, status, lookup))
	}
	return commands
}
func hostingLeaf(kind string, requireJJ bool, run hostingOperation, status hostingStatusOperation, lookup hostingLookupOperation) *cobra.Command {
	options := hosting.Options{Kind: kind, RequireJJ: requireJJ}
	var jsonOutput, compact, watch bool
	var jsonVersion int
	var expectHead string
	use := kind + " [PATH]"
	switch kind {
	case "pr", "ci":
		use = kind + " NUMBER [PATH]"
	case "prs":
		use = "prs SHA [PATH]"
	}
	command := &cobra.Command{
		Use: use, Short: map[string]string{"repo": "Resolve local GitHub repository candidates without network", "pr": "Read one explicitly numbered GitHub pull request", "prs": "List the pull requests GitHub associates with one commit", "ci": "Read checks and commit statuses at one PR's exact head"}[kind],
		Long: "Read-only SourceSteward hosting inspection. Local ownership selects native Git or jj; gh jj requires jj.\nRepository resolution has no network effect. PR/CI use bounded go-gh HTTP reads with gh 2.102.0+ authentication.\nNo push, fetch, PR edit, merge or CI rerun. A complete sample does not establish merge eligibility.\nLimits: 30 seconds total, 1 MiB output, 10 pages per collection and 1000 CI records.\nGit/jj versions and enforced read-only sandbox requirements follow stw inspect.\nOperational errors include JSON observations with --json and exit 1; usage errors exit 2.\nA rate limit or any other HTTP failure stops at once: there is no retry, backoff or wait.",
		Args: func(cmd *cobra.Command, args []string) error {
			switch kind {
			case "repo":
				return cobra.MaximumNArgs(1)(cmd, args)
			case "prs":
				if err := cobra.RangeArgs(1, 2)(cmd, args); err != nil {
					return err
				}
				options.HeadSHA = args[0]
				return hosting.ValidateOptions(options)
			}
			if err := cobra.RangeArgs(1, 2)(cmd, args); err != nil {
				return err
			}
			for _, c := range args[0] {
				if c < '0' || c > '9' {
					return fmt.Errorf("NUMBER must be a positive decimal integer")
				}
			}
			n, err := strconv.Atoi(args[0])
			if err != nil || n <= 0 {
				return fmt.Errorf("NUMBER must be a positive decimal integer")
			}
			options.Number = n
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			enhanced := false
			if kind == "pr" {
				if watch {
					return fmt.Errorf("--watch is not available in this release; use scripts/ci-wait.sh for long waits")
				}
				if cmd.Flags().Changed("json-version") && !jsonOutput {
					return fmt.Errorf("--json-version requires --json")
				}
				if jsonVersion != 1 && jsonVersion != 2 {
					return fmt.Errorf("--json-version must be 1 or 2")
				}
				if compact && jsonOutput {
					return fmt.Errorf("--compact conflicts with --json")
				}
				if cmd.Flags().Changed("expect-head") && expectHead == "" {
					return fmt.Errorf("--expect-head must be a full 40-character lowercase SHA")
				}
				if jsonOutput && jsonVersion == 1 && cmd.Flags().Changed("expect-head") {
					return fmt.Errorf("--expect-head with --json requires --json-version 2")
				}
				enhanced = compact || cmd.Flags().Changed("expect-head") || (jsonOutput && jsonVersion == 2)
				options.Status = enhanced
				options.ExpectHead = expectHead
			}
			if kind == "prs" && jsonOutput && compact {
				return fmt.Errorf("--compact conflicts with --json")
			}
			if err := hosting.ValidateOptions(options); err != nil {
				return err
			}
			path := ""
			if kind != "repo" {
				args = args[1:]
			}
			if len(args) > 0 {
				path = args[0]
			}
			out := cmd.OutOrStdout()
			var err error
			ok := true
			switch {
			case kind == "prs":
				r := lookup(cmd.Context(), path, options)
				ok = r.Status == "ok"
				if jsonOutput {
					err = json.NewEncoder(out).Encode(r)
				} else {
					err = renderLookup(out, r, compact)
				}
			case enhanced:
				r := status(cmd.Context(), path, options)
				ok = r.Status == "ok"
				if jsonOutput {
					err = json.NewEncoder(out).Encode(r)
				} else {
					err = renderPRStatus(out, r, compact)
				}
			default:
				result := run(cmd.Context(), path, options)
				ok = result.Status == "ok"
				if jsonOutput {
					err = json.NewEncoder(out).Encode(result)
				} else {
					err = renderHosting(out, result)
				}
			}
			if err != nil {
				return runtimeFailure{err}
			}
			if !ok {
				return runtimeFailure{fmt.Errorf("hosting %s inspection incomplete", kind)}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.BoolVar(&jsonOutput, "json", false, "Output one versioned hosting observation including operational errors")
	flags.StringVar(&options.Repo, "repo", "", "Explicit OWNER/NAME (does not override local ownership)")
	flags.StringVar(&options.Host, "host", "github.com", "Explicit GitHub hostname; remote text never selects an authentication host")
	flags.StringVar(&options.Remote, "remote", "", "Select one native remote; no origin convention")
	if kind == "pr" {
		flags.IntVar(&jsonVersion, "json-version", 1, "JSON schema version: 1 (default, unchanged) or 2 (lifecycle, merge evidence, expected head)")
		flags.StringVar(&expectHead, "expect-head", "", "Fail unless the PR head is this full 40-character lowercase SHA (the result SHA is never compared)")
		flags.BoolVar(&watch, "watch", false, "Not available in this release")
		flags.MarkHidden("watch")
	}
	if kind == "pr" || kind == "prs" {
		flags.BoolVar(&compact, "compact", false, "Print one escaped line per PR instead of the detailed text")
	}
	return command
}
func renderHosting(w io.Writer, r hosting.Result) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Hosting: %q status=%q backend=%q\nObserved: %q\nComplete: %t; network reads: %t\n", r.Kind, r.Status, r.Backend, r.ObservedAt, r.Boundary.Complete, r.Boundary.Network)
	writeIdentity(&text, r.Identity, r.Repository, r.Candidates)
	if r.PullRequest != nil {
		p := r.PullRequest
		fmt.Fprintf(&text, "PR: %d title=%q state=%q draft=%t\nHead: %q %q at %q\nBase: %q %q at %q\nURL: %q\n", p.Number, p.Title, p.State, p.Draft, p.HeadRepository, p.HeadRef, p.HeadSHA, p.BaseRepository, p.BaseRef, p.BaseSHA, p.URL)
	}
	for _, v := range r.Checks {
		fmt.Fprintf(&text, "Check: %d name=%q status=%q conclusion=%q head=%q\n", v.ID, v.Name, v.Status, hostingNullable(v.Conclusion), v.HeadSHA)
	}
	for _, v := range r.Statuses {
		fmt.Fprintf(&text, "Status: %d context=%q state=%q description=%q\n", v.ID, v.Context, v.State, v.Description)
	}
	for _, v := range r.Diagnostics {
		fmt.Fprintf(&text, "Diagnostic: %q %q\n", v.Code, v.Message)
	}
	for _, v := range r.Boundary.Limitations {
		fmt.Fprintf(&text, "Limitation: %q\n", v)
	}
	_, err := io.WriteString(w, text.String())
	return err
}
func hostingNullable(v *string) string {
	if v == nil {
		return "unknown"
	}
	return *v
}

func writeIdentity(text *strings.Builder, id *inspection.Result, repo *hosting.Repository, candidates []hosting.Candidate) {
	if id != nil {
		for _, diagnostic := range id.Diagnostics {
			fmt.Fprintf(text, "Native diagnostic: %q %q\n", diagnostic.Code, diagnostic.Message)
		}
		if v := id.JJRepository; v != nil {
			fmt.Fprintf(text, "Native jj: workspace=%q change=%q commit=%q operation=%q\nWorking copy: %q\nNative limitation: %q\n", v.WorkspaceRoot, v.ChangeID, v.CommitID, v.OperationID, v.Observation.WorkingCopy, v.Observation.Limitation)
		}
		if v := id.Repository; v != nil {
			fmt.Fprintf(text, "Native Git: head=%q ref=%q commit=%q bare=%t linked-worktree=%t\n", v.Head.State, hostingNullable(v.Head.Ref), hostingNullable(v.Head.OID), v.Bare, v.LinkedWorktree)
		}
	}
	if repo != nil {
		v := repo
		fmt.Fprintf(text, "Repository: %q/%q/%q selection=%q\n", v.Host, v.Owner, v.Name, v.Selection)
	}
	for _, v := range candidates {
		fmt.Fprintf(text, "Candidate: remote=%q host=%q owner=%q repository=%q\n", v.Remote, v.Host, v.Owner, v.Name)
	}
}
