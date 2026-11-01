// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/landcmd"
)

// landTransport and landHook are test seams: production leaves the transport nil (HTTPS) and the hook unset.
var (
	landTransport hosting.Transport
	landHook      func(*landcmd.Params)
)

func landCommand() *cobra.Command {
	var f landcmd.Flags
	command := &cobra.Command{
		Use:   "land --stack NUMBER,NUMBER[,...] --reviewed NUMBER:SHA [--reviewed NUMBER:SHA ...] [--plan] [--repo OWNER/NAME] [--remote NAME] [--ack-draft] [--delete-branch] [--wait DURATION] [PATH] [--human | --json]",
		Short: "Owner only: plan the landing of a linear chain of reviewed pull requests with one fast-forward push of the tip",
		Long: "Owner-only command (Spec 043). --stack lands a whole linear chain of reviewed, separately stacked pull requests with ONE fast-forward push of the tip to the\n" +
			"default branch; GitHub then marks each pull request whose head is in the default branch as merged. No rebase, no new SHAs, no restack, no retarget.\n" +
			"An agent may run only --plan: it is read-only, allowed in agent mode and labelled mode agent. It reads GitHub (pull requests, checks, rules, rulesets, reviews, events)\n" +
			"and runs one disclosed fetch into refs/stw/land/ (objects and that namespace only; no FETCH_HEAD, no user ref). Without --plan the command is an apply:\n" +
			"it is refused in agent mode (also without a terminal, with STW_AGENT or an agent marker) before any read, and --yes is never accepted.\n" +
			"The plan lists every failing precondition with a typed reason: head-changed, chain-not-linear, object-missing, not-fast-forward, stack-side-child,\n" +
			"checks-not-green, tip-checks-pending, head-changed-no-checks, checks-missing, ruleset-forbids-push, land-unmeasured, range-mismatch, identity-mismatch, and more.\n" +
			"A repository shape that is not recorded as measured in internal/land/shapes.json is land-unmeasured and cannot apply; the allowlist is empty today.\n" +
			"--reviewed NUMBER:SHA is the owner's statement that an independent review of exactly that head was CLEAR; stw verifies the head, not the review.\n" +
			"Exit statuses: 0 the plan is landable or the chain is already landed, 1 a precondition fails, a read failed or an apply is refused, 2 usage error.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				f.Path = args[0]
			}
			f.WaitSet = cmd.Flags().Changed("wait")
			params, err := landcmd.Prepare(cmd.Context(), f, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			params.Diag, params.Transport, params.Stdin = cmd.ErrOrStderr(), landTransport, cmd.InOrStdin()
			if landHook != nil {
				landHook(&params)
			}
			return landcmd.Execute(cmd.Context(), params)
		},
	}
	flags := command.Flags()
	flags.StringVar(&f.Stack, "stack", "", "Pull request numbers of the chain, comma separated, bottom first, tip last (2 to 8)")
	flags.StringArrayVar(&f.Reviewed, "reviewed", nil, "NUMBER:SHA, the full head SHA reviewed CLEAR, once per listed pull request")
	flags.IntVar(&f.Pr, "pr", 0, "Landing one pull request is not implemented in this build; excluded by --stack")
	flags.StringVar(&f.Head, "head", "", "Landing one pull request is not implemented in this build; excluded by --stack")
	flags.BoolVar(&f.Plan, "plan", false, "Read-only plan; the only form an agent may run")
	flags.StringVar(&f.Repo, "repo", "", "OWNER/NAME (default: the repository of the remote)")
	flags.StringVar(&f.Remote, "remote", "", "Remote name (default: origin, else the single remote)")
	flags.BoolVar(&f.DeleteBranch, "delete-branch", false, "Also delete each landed pull request's own head branch after all are merged (apply only)")
	flags.BoolVar(&f.AckDraft, "ack-draft", false, "Acknowledge that a listed pull request is a Draft")
	flags.DurationVar(&f.Wait, "wait", landcmd.DefaultWait, "Bounded wait for GitHub to mark each pull request merged (maximum 2m; apply only)")
	flags.StringVar(&f.Host, "host", landcmd.DefaultHost, "GitHub host")
	flags.BoolVar(&f.Yes, "yes", false, "Never accepted: a landing is confirmed by typing the approval word on the terminal")
	flags.BoolVar(&f.Human, "human", false, "Print the human table (default on a terminal)")
	flags.BoolVar(&f.JSON, "json", false, "Print one JSON document (default otherwise)")
	_ = flags.MarkHidden("yes")
	_ = flags.MarkHidden("pr")
	_ = flags.MarkHidden("head")
	return command
}
