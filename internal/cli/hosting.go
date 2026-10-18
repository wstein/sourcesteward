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

type hostingOperation func(context.Context, string, hosting.Options) hosting.Result

func hostingCommand() *cobra.Command { return hostingCommandWith(false, hosting.Inspect) }
func hostingCommandWith(requireJJ bool, run hostingOperation) *cobra.Command {
	group := &cobra.Command{Use: "hosting", Short: "Resolve repositories and explicitly inspect GitHub PRs and CI without writes"}
	group.AddCommand(hostingCommands(requireJJ, run)...)
	return group
}
func hostingCommands(requireJJ bool, run hostingOperation) []*cobra.Command {
	var commands []*cobra.Command
	for _, kind := range []string{"repo", "pr", "ci"} {
		commands = append(commands, hostingLeaf(kind, requireJJ, run))
	}
	commands = append(commands, readyCommand(requireJJ, hosting.CheckReady))
	return commands
}
func hostingLeaf(kind string, requireJJ bool, run hostingOperation) *cobra.Command {
	options := hosting.Options{Kind: kind, RequireJJ: requireJJ}
	var jsonOutput bool
	use := kind + " [PATH]"
	if kind != "repo" {
		use = kind + " NUMBER [PATH]"
	}
	command := &cobra.Command{
		Use: use, Short: map[string]string{"repo": "Resolve local GitHub repository candidates without network", "pr": "Read one explicitly numbered GitHub pull request", "ci": "Read checks and commit statuses at one PR's exact head"}[kind],
		Long: "Read-only SourceSteward hosting inspection. Local ownership selects native Git or jj; gh jj requires jj.\nRepository resolution has no network effect. PR/CI use bounded go-gh HTTP reads with gh 2.102.0+ authentication.\nNo push, fetch, PR edit, merge or CI rerun. A complete sample does not establish merge eligibility.\nLimits: 30 seconds total, 1 MiB output, 10 pages per collection and 1000 CI records.\nGit/jj versions and enforced read-only sandbox requirements follow stw inspect.\nOperational errors include JSON observations with --json and exit 1; usage errors exit 2.",
		Args: func(cmd *cobra.Command, args []string) error {
			if kind == "repo" {
				return cobra.MaximumNArgs(1)(cmd, args)
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
			result := run(cmd.Context(), path, options)
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				err = renderHosting(cmd.OutOrStdout(), result)
			}
			if err != nil {
				return runtimeFailure{err}
			}
			if result.Status != "ok" {
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
	return command
}
func renderHosting(w io.Writer, r hosting.Result) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Hosting: %q status=%q backend=%q\nObserved: %q\nComplete: %t; network reads: %t\n", r.Kind, r.Status, r.Backend, r.ObservedAt, r.Boundary.Complete, r.Boundary.Network)
	if r.Identity != nil {
		for _, diagnostic := range r.Identity.Diagnostics {
			fmt.Fprintf(&text, "Native diagnostic: %q %q\n", diagnostic.Code, diagnostic.Message)
		}
		if v := r.Identity.JJRepository; v != nil {
			fmt.Fprintf(&text, "Native jj: workspace=%q change=%q commit=%q operation=%q\nWorking copy: %q\nNative limitation: %q\n", v.WorkspaceRoot, v.ChangeID, v.CommitID, v.OperationID, v.Observation.WorkingCopy, v.Observation.Limitation)
		}
		if v := r.Identity.Repository; v != nil {
			fmt.Fprintf(&text, "Native Git: head=%q ref=%q commit=%q bare=%t linked-worktree=%t\n", v.Head.State, hostingNullable(v.Head.Ref), hostingNullable(v.Head.OID), v.Bare, v.LinkedWorktree)
		}
	}

	if r.Repository != nil {
		v := r.Repository
		fmt.Fprintf(&text, "Repository: %q/%q/%q selection=%q\n", v.Host, v.Owner, v.Name, v.Selection)
	}
	for _, v := range r.Candidates {
		fmt.Fprintf(&text, "Candidate: remote=%q host=%q owner=%q repository=%q\n", v.Remote, v.Host, v.Owner, v.Name)
	}
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
