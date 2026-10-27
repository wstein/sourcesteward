// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// The seams of `stw tidy apply PLAN-ID`: the application of a stored plan set, its lookup for the
// prompt, and the terminal facts of mode detection. Tests replace them.
var (
	tidyApplyPlanSet = func(ctx context.Context, r planset.ApplyRequest) planset.ApplyResult {
		return planset.Apply(ctx, r)
	}
	tidyFindPlanSet = planset.Find
	tidyEnviron     = planset.Environ
	// tidyTerminalFacts reports whether stdin and stdout are terminals (isatty on each). A stream
	// that is not an *os.File, a pipe, a file and the null device are no terminal, so only a real
	// terminal run can be human mode; everything else is agent mode, which fails closed.
	tidyTerminalFacts = owner.TerminalFacts
)

var tidyPlanSetID = regexp.MustCompile(`^[0-9a-f]{12}$`)

// tidyPlanSetApplyExclusions are the flags of the expert apply that make no sense with a plan id.
var tidyPlanSetApplyExclusions = append([]string{"plan", "authorize", "contract"}, tidyV3ApplyFlags...)

func validateTidyPlanSetApply(cmd *cobra.Command, args []string, yes bool) error {
	if len(args) == 0 {
		if yes {
			return fmt.Errorf("--yes requires an explicit plan id on the command line")
		}
		return nil
	}
	if !tidyPlanSetID.MatchString(args[0]) {
		return fmt.Errorf("a plan id is 12 lower-case hexadecimal characters")
	}
	for _, name := range tidyPlanSetApplyExclusions {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--%s cannot be combined with a plan id", name)
		}
	}
	return nil
}

func runTidyApplyPlanSet(cmd *cobra.Command, id string, stateDir string, yes, structured bool) error {
	var flags []string
	if stateDir != "" {
		flags = append(flags, stateDir)
	}
	def, _ := derive.DefaultStateDir()
	// An explicit --state-dir is searched even when the derivation found it unsafe: opening it
	// then refuses it with the reason, instead of reporting the plan set as absent.
	dirs := append([]string{}, flags...)
	for _, s := range derive.Stores(flags, planset.Getenv("STW_STATE_DIR"), def) {
		if s.Issue == nil && s.Path != stateDir {
			dirs = append(dirs, s.Path)
		}
	}
	stdinTTY, stdoutTTY := tidyTerminalFacts(cmd.InOrStdin(), cmd.OutOrStdout())
	mode := owner.Detect(owner.Env{StdinTTY: stdinTTY, StdoutTTY: stdoutTTY, Environ: tidyEnviron(), OSUser: planset.Getenv("USER")})
	authorize := yes
	if !yes && mode.Mode == owner.ModeHuman {
		set, err := tidyFindPlanSet(dirs, id)
		if err == nil {
			terminal, err := openTidyTerminal()
			if err != nil {
				return runtimeFailure{err}
			}
			defer terminal.Close()
			if _, err := fmt.Fprintf(terminal, "Apply plan %s (%d sub-plans, %d items)? [y/N]: ", id, len(set.SubPlans), len(set.Items)); err != nil {
				return runtimeFailure{err}
			}
			line, _ := bufio.NewReader(io.LimitReader(tidySelectionReader{terminal}, 256)).ReadString('\n')
			if strings.TrimRight(line, "\r\n") != "y" {
				return runtimeFailure{fmt.Errorf("plan %s was not authorized; nothing was changed", id)}
			}
			authorize = true
		}
	}
	res := tidyApplyPlanSet(cmd.Context(), planset.ApplyRequest{StateDirs: dirs, PlanID: id, Mode: mode, Authorize: authorize, Confirm: tidyConfirmOpener, Applier: planset.V3Applier{ApplyFunc: workspace.ApplyTidyV3}})
	if err := writeTidyPlanSetResult(cmd.OutOrStdout(), res, structured); err != nil {
		return runtimeFailure{err}
	}
	if err := cmd.Context().Err(); err != nil {
		return runtimeFailure{err}
	}
	if res.Status != "ok" {
		return runtimeFailure{fmt.Errorf("tidy plan set %q: %q", res.PlanID, res.Status)}
	}
	return nil
}

func writeTidyPlanSetResult(w io.Writer, res planset.ApplyResult, structured bool) error {
	if structured {
		return json.NewEncoder(w).Encode(res)
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Tidy plan set: %q\nStatus: %q\n", res.PlanID, res.Status)
	if res.Code != "" {
		fmt.Fprintf(&text, "Code: %q\nMessage: %q\n", res.Code, res.Message)
	}
	for _, o := range res.Outcomes {
		fmt.Fprintf(&text, "Sub-plan: %q repo=%q kind=%q worktree=%q status=%q code=%q message=%q\n", o.SubPlan, o.Repo, o.Kind, o.Worktree, o.Status, o.Code, o.Message)
	}
	_, err := io.WriteString(w, text.String())
	return err
}
