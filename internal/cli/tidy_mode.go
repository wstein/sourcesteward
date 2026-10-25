// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagClass is the layer a tidy flag belongs to (Spec 041, mode selection).
type flagClass int

const (
	// flagExpert flags exist today and select the expert layer (Spec 011/029/contract 3).
	flagExpert flagClass = iota + 1
	// flagWizard flags are new and only valid in the wizard layer.
	flagWizard
	// flagShared flags are valid in both layers.
	flagShared
)

// tidyFlagClasses classifies every flag of `tidy plan` and `tidy apply`. A flag that is not here
// fails TestEveryTidyFlagIsClassified, so a new flag cannot join a layer by accident.
var tidyFlagClasses = map[string]flagClass{
	// wizard-only
	"workspace": flagWizard, "mine": flagWizard, "by-owner": flagWizard, "member": flagWizard, "member-base": flagWizard,
	"only": flagWizard, "save": flagWizard, "yes": flagWizard,
	// shared (--human forces text, which is also what the expert layer prints without --json)
	"state-dir": flagShared, "online": flagShared, "timeout": flagShared, "json": flagShared, "human": flagShared,
	// expert
	"base": flagExpert, "ref": flagExpert, "task": flagExpert, "owner": flagExpert, "remove-registration": flagExpert,
	"remove-files": flagExpert, "completed-worktree": flagExpert, "protect-worktree": flagExpert, "contract": flagExpert,
	"remote": flagExpert, "tracking-remote": flagExpert, "repo": flagExpert, "host": flagExpert, "pr": flagExpert,
	"integration-proof": flagExpert, "accept-whitespace-differences": flagExpert, "decisions": flagExpert,
	"consolidation-range": flagExpert, "report-dir": flagExpert, "report-max-entries": flagExpert,
	"report-max-bytes": flagExpert, "report-max-diff-bytes": flagExpert, "report-max-files": flagExpert,
	"report-max-lines": flagExpert, "report-max-commits": flagExpert, "plan": flagExpert, "authorize": flagExpert,
}

// tidyWizardOptions holds the values of the wizard-only flags of `tidy plan`.
type tidyWizardOptions struct {
	workspace  string
	mine, save bool
	human      bool
	byOwner    []string
	member     []string
	memberBase []string
	only       string
	// stateDirs records every --state-dir of the command line, for the wizard layer.
	stateDirs []string
}

func addTidyWizardPlanFlags(cmd *cobra.Command, o *tidyWizardOptions) {
	f := cmd.Flags()
	f.StringVar(&o.workspace, "workspace", "", "Workspace to use (wizard layer; default: discovery from the working directory)")
	f.BoolVar(&o.mine, "mine", false, "Only items owned by the caller's owner label (wizard layer; the narrowing for agents)")
	f.StringArrayVar(&o.byOwner, "by-owner", nil, "Only items of this owner label (wizard layer, repeatable); narrows the view, never selects the run mode")
	f.StringArrayVar(&o.member, "member", nil, "Restrict to a member repository (wizard layer, repeatable; the workspace repository is .)")
	f.StringArrayVar(&o.memberBase, "member-base", nil, "Override the derived base of one member as NAME=REF (wizard layer, repeatable)")
	f.StringVar(&o.only, "only", "", "Comma list of retire, branches, tracking, stale-worktrees (wizard layer; default all)")
	f.BoolVar(&o.save, "save", false, "Store the plan set of the safe items of scope own (wizard layer)")
	f.BoolVar(&o.human, "human", false, "Force text output; excludes --json")
}

// changedTidyFlags returns the names of the flags set on the command line, sorted.
func changedTidyFlags(cmd *cobra.Command) []string {
	var out []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if f.Name != "help" {
			out = append(out, f.Name)
		}
	})
	sort.Strings(out)
	return out
}

var tidyOnlyKinds = map[string]bool{"retire": true, "branches": true, "tracking": true, "stale-worktrees": true}

// validateTidyWizardValues checks the values of the wizard-only flags that were given.
func validateTidyWizardValues(cmd *cobra.Command, o *tidyWizardOptions) error {
	changed := func(name string) bool { return cmd.Flags().Changed(name) }
	if changed("only") {
		// An empty list splits into one empty kind, which is rejected like any unknown kind.
		for _, kind := range strings.Split(o.only, ",") {
			if !tidyOnlyKinds[kind] {
				return fmt.Errorf("--only: unknown kind %q (retire, branches, tracking, stale-worktrees)", kind)
			}
		}
	}
	for _, name := range []string{"by-owner", "member"} {
		values := o.byOwner
		if name == "member" {
			values = o.member
		}
		for _, v := range values {
			if v == "" {
				return fmt.Errorf("--%s needs a nonempty value", name)
			}
		}
	}
	for _, v := range o.memberBase {
		name, ref, ok := strings.Cut(v, "=")
		if !ok || name == "" || ref == "" {
			return fmt.Errorf("--member-base needs NAME=REF, got %q", v)
		}
	}
	if changed("workspace") && o.workspace == "" {
		return fmt.Errorf("--workspace needs a nonempty path")
	}
	return nil
}

// tidyPlanLayer is the layer a `tidy plan` command line selects.
type tidyPlanLayer int

const (
	tidyExpert tidyPlanLayer = iota
	tidyWizard
)

// selectTidyPlanLayer applies the mode selection of the specification: a command line is the
// wizard layer only if it has no positional PATH and every flag it carries is a wizard-only or
// shared flag; any other flag, or any PATH, selects the expert layer exactly as today. Wizard-only
// flags together with expert flags or a PATH are a usage error.
func selectTidyPlanLayer(cmd *cobra.Command, args []string, o *tidyWizardOptions) (tidyPlanLayer, error) {
	if cmd.Flags().Changed("json") && cmd.Flags().Changed("human") {
		return tidyExpert, fmt.Errorf("--json and --human exclude each other")
	}
	var wizardOnly, expert []string
	for _, name := range changedTidyFlags(cmd) {
		switch tidyFlagClasses[name] {
		case flagWizard:
			wizardOnly = append(wizardOnly, "--"+name)
		case flagExpert:
			expert = append(expert, "--"+name)
		}
	}
	switch {
	case len(wizardOnly) > 0 && len(args) > 0:
		return tidyExpert, fmt.Errorf("%s is a wizard flag and cannot be used with a PATH; drop the PATH, or use the expert layer without it", wizardOnly[0])
	case len(wizardOnly) > 0 && len(expert) > 0:
		return tidyExpert, fmt.Errorf("%s (wizard layer) cannot be combined with %s (expert layer)", wizardOnly[0], expert[0])
	case len(wizardOnly) > 0 || (len(args) == 0 && len(expert) == 0):
		if err := validateTidyWizardValues(cmd, o); err != nil {
			return tidyExpert, err
		}
		return tidyWizard, nil
	}
	return tidyExpert, nil
}

// tidyBareOptions holds the flags of bare `stw tidy`, the interactive wizard: the wizard-only
// flags and the shared ones (the state directories are repeatable here).
type tidyBareOptions struct {
	tidyWizardOptions
	json    bool
	timeout time.Duration
}

func addTidyBareFlags(cmd *cobra.Command, o *tidyBareOptions) {
	f := cmd.Flags()
	f.StringVar(&o.workspace, "workspace", "", "Workspace to use (default: discovery from the working directory)")
	f.BoolVar(&o.mine, "mine", false, "Only items owned by the caller's owner label; selects agent mode (no prompt)")
	f.StringArrayVar(&o.byOwner, "by-owner", nil, "Only items of this owner label (repeatable); narrows the view, never selects the run mode")
	f.StringArrayVar(&o.member, "member", nil, "Restrict to a member repository (repeatable; the workspace repository is .)")
	f.StringArrayVar(&o.memberBase, "member-base", nil, "Override the derived base of one member as NAME=REF (repeatable)")
	f.StringVar(&o.only, "only", "", "Comma list of retire, branches, tracking, stale-worktrees (default all)")
	f.StringArrayVar(&o.stateDirs, "state-dir", nil, "Extra private state directory holding creation records (repeatable; the first valid one stores plans)")
	f.Bool("online", false, "Permit the contract-3 authenticated reads (default off)")
	f.DurationVar(&o.timeout, "timeout", 0, "Deadline over the whole analysis; it also replaces the scaled observation window of each repository (at most 1h); without it the proof phase may run 60 s past the window")
	f.BoolVar(&o.json, "json", false, "Print the overview as one document; asks nothing, stores nothing")
	f.BoolVar(&o.human, "human", false, "Force text output; excludes --json")
	// save and yes are registered hidden, unbound, only to refuse them whatever their value.
	f.Bool("save", false, "")
	f.Bool("yes", false, "")
	_ = f.MarkHidden("save")
	_ = f.MarkHidden("yes")
}

// runTidyBare is bare `stw tidy`. The flag values are validated before anything is analysed.
func runTidyBare(cmd *cobra.Command, o *tidyBareOptions) error {
	if cmd.Flags().Changed("yes") {
		return fmt.Errorf("--yes belongs to `stw tidy apply PLAN-ID` and requires an explicit plan id")
	}
	if cmd.Flags().Changed("save") {
		return fmt.Errorf("--save belongs to `stw tidy plan`; bare `stw tidy` stores the plan set you choose")
	}
	if cmd.Flags().Changed("json") && cmd.Flags().Changed("human") {
		return fmt.Errorf("--json and --human exclude each other")
	}
	if err := validateTidyWizardValues(cmd, &o.tidyWizardOptions); err != nil {
		return err
	}
	if o.json {
		return runTidyWizardPlan(cmd, &o.tidyWizardOptions, true, o.timeout)
	}
	return runTidyInteractive(cmd, &o.tidyWizardOptions, o.timeout)
}
