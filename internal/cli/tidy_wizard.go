// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/analysis"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// The seams of the wizard-layer `tidy plan`; tests replace them.
var (
	tidyPrepare = func(ctx context.Context, o analysis.Options) (analysis.Prepared, error) {
		return analysis.Prepare(ctx, o, analysis.DefaultDeps())
	}
	tidyAnalyse = analysis.Run
	// The working directory, absolute (the CLI imports no os: filepath.Abs resolves ".").
	tidyGetwd      = func() (string, error) { return filepath.Abs(".") }
	tidyNow        = time.Now
	tidyPlannerFor = func(p analysis.Prepared) planset.Planner {
		repos := []planset.RepoSpec{}
		for _, s := range p.Specs {
			if s.Skip == "" {
				repos = append(repos, planset.RepoSpec{Name: s.Name, Path: s.Path, Base: s.Base, Remote: p.Meta[s.Name].Remote, Online: s.Online})
			}
		}
		return planset.NewV3Planner(repos, p.Stores)
	}
)

// recordingValue remembers every occurrence of a repeatable flag while the wrapped value keeps its
// single-value behaviour for the expert layer (the wizard's --state-dir is repeatable, the expert
// one is not).
type recordingValue struct {
	pflag.Value
	all *[]string
}

func (v recordingValue) Set(s string) error {
	*v.all = append(*v.all, s)
	return v.Value.Set(s)
}

// recordStateDirs makes `--state-dir` of plan record every occurrence, for the wizard layer.
func recordStateDirs(cmd *cobra.Command, all *[]string) {
	if flag := cmd.Flags().Lookup("state-dir"); flag != nil {
		flag.Value = recordingValue{Value: flag.Value, all: all}
	}
}

// runTidyWizardPlan is `stw tidy plan` in the wizard layer: it analyses the workspace read only,
// narrows the view (--only, --mine, --by-owner), optionally stores the plan set of the safe own
// items (--save), prints the overview as text or JSON, and never applies anything.
func runTidyWizardPlan(cmd *cobra.Command, w *tidyWizardOptions, structured bool, timeout time.Duration) error {
	if cmd.Flags().Changed("timeout") && timeout <= 0 {
		return fmt.Errorf("--timeout must be a positive duration")
	}
	stdinTTY, stdoutTTY := tidyTerminalFacts(cmd.InOrStdin(), cmd.OutOrStdout())
	environ := tidyEnviron()
	mode := owner.Detect(owner.Env{StdinTTY: stdinTTY, StdoutTTY: stdoutTTY, Environ: environ, OSUser: planset.Getenv("USER"), Mine: w.mine, ByOwner: w.byOwner})
	caller := derive.Caller(environValue(environ, "STW_OWNER"), planset.Getenv("USER"), mode.Mode == owner.ModeAgent)
	if w.mine && caller.Value == "" {
		return fmt.Errorf("--mine needs the caller label: set $STW_OWNER")
	}
	cwd, err := tidyGetwd()
	if err != nil {
		return runtimeFailure{err}
	}
	ctx := cmd.Context()
	if cmd.Flags().Changed("timeout") {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	prep, err := tidyPrepare(ctx, analysis.Options{Workspace: w.workspace, CWD: cwd, Members: w.member, MemberBases: w.memberBase, StateDirs: w.stateDirs, Online: cmd.Flags().Changed("online")})
	if err != nil {
		// A deadline or an interrupt comes first: discovery reports its context ending as a
		// missing workspace, which it is not.
		if cause := interruption(ctx, cmd.Flags().Changed("timeout"), timeout); cause != nil {
			return runtimeFailure{cause}
		}
		// The message carries paths and error text from outside: it passes esc, whole.
		safe := errors.New(escFull(err.Error()))
		var usage *analysis.UsageError
		if errors.As(err, &usage) {
			return safe
		}
		return runtimeFailure{safe}
	}
	res := tidyAnalyse(ctx, prep, tidywizard.Config{Caller: caller.Value, Agent: mode.Mode == owner.ModeAgent, CWD: cwd, Now: tidyNow()})
	res.Candidates = narrowView(res.Candidates, w, caller.Value)

	planID, stored := "", ""
	var notices []string
	var saveErr error
	if w.save {
		var saveNote string
		planID, stored, saveNote, saveErr = savePlanSet(ctx, prep, res, mode)
		if saveNote != "" {
			notices = append(notices, saveNote)
		}
	}
	doc := render.Build(render.Input{Workspace: prep.Workspace, Derived: prep.Derived, Repos: prep.Meta, Result: res, Mode: mode, PlanID: planID, Notices: append(append([]string{}, prep.Notices...), notices...)})
	cut, err := writeWizardOverview(cmd.OutOrStdout(), doc, structured, planID, stored)
	if err != nil {
		return err
	}
	// The overview is always printed first; what went wrong is reported after it (exit 1).
	if saveErr != nil {
		return runtimeFailure{errors.New(escFull(saveErr.Error()))}
	}
	if cause := interruption(ctx, cmd.Flags().Changed("timeout"), timeout); cause != nil {
		return runtimeFailure{cause}
	}
	return wizardExit(doc.Truncated || cut, prep, res)
}

// interruption names why the context ended (--timeout, a deadline of the caller, or an interrupt),
// or is nil while it is alive.
func interruption(ctx context.Context, timeoutSet bool, timeout time.Duration) error {
	switch ctx.Err() {
	case nil:
		return nil
	case context.DeadlineExceeded:
		if timeoutSet {
			return fmt.Errorf("--timeout of %s elapsed during the analysis; nothing was changed", timeout)
		}
		return errors.New("the deadline elapsed during the analysis; nothing was changed")
	}
	return errors.New("interrupted; nothing was changed")
}

// escFull escapes a message that carries outside text without cutting it: esc bounds its input to
// 120 characters, so the message is escaped in pieces of at most 100 characters and joined.
func escFull(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for len(rs) > 0 {
		n := min(len(rs), 100)
		b.WriteString(untrusted.Esc(string(rs[:n])))
		rs = rs[n:]
	}
	return b.String()
}

func environValue(environ []string, name string) string {
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v
		}
	}
	return ""
}

// narrowView applies --only, --mine and --by-owner to the candidates. They narrow the view, never
// the analysis, and never select the run mode.
func narrowView(in []tidywizard.Candidate, w *tidyWizardOptions, caller string) []tidywizard.Candidate {
	kinds := map[tidywizard.Kind]bool{}
	if w.only != "" {
		for _, k := range strings.Split(w.only, ",") {
			switch k {
			case "retire":
				kinds[tidywizard.KindWorktree] = true
			case "branches":
				kinds[tidywizard.KindBranch] = true
			case "tracking":
				kinds[tidywizard.KindTrackingRef] = true
			case "stale-worktrees":
				kinds[tidywizard.KindStaleWorktree] = true
			}
		}
	}
	owners := map[string]bool{}
	for _, o := range w.byOwner {
		owners[o] = true
	}
	out := []tidywizard.Candidate{}
	for _, c := range in {
		if len(kinds) > 0 && !kinds[c.Kind] {
			continue
		}
		if w.mine && c.Owner != caller {
			continue
		}
		if len(owners) > 0 && !owners[c.Owner] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// savePlanSet stores the plan set of the safe items of scope own (in agent mode only owners from a
// task record) in the plan store and returns its id and the line that says how much it covers.
// Nothing selectable stores nothing and says so in a note. An error is returned for the caller to
// report after the overview.
func savePlanSet(ctx context.Context, prep analysis.Prepared, res tidywizard.Result, mode owner.Evidence) (id, stored, note string, err error) {
	if prep.PlanStore == "" {
		return "", "", "", errors.New("no valid state directory to store the plan set in (state-dir-unsafe); give --state-dir")
	}
	chosen := planset.SelectSafe(res.Candidates, mode)
	if len(chosen) == 0 {
		return "", "", "nothing to save: no safe item of your own", nil
	}
	set, err := planset.Build(ctx, planset.Request{Candidates: res.Candidates, Chosen: chosen, Derived: prep.Derived, Mode: mode}, tidyPlannerFor(prep))
	if err != nil {
		return "", "", "", err
	}
	if err := planset.Store(prep.PlanStore, set); err != nil {
		return "", "", "", err
	}
	byID := map[string]tidywizard.Candidate{}
	for _, c := range res.Candidates {
		byID[c.ID] = c
	}
	effects, repos := 0, map[string]bool{}
	for _, it := range set.Items {
		c := byID[it.ID]
		effects += len(c.Effects)
		repos[c.Repo] = true
	}
	return set.PlanID, fmt.Sprintf("%d effects in %d repositories", effects, len(repos)), "", nil
}

// writeWizardOverview prints the overview and reports whether the JSON had to be cut to its bound.
func writeWizardOverview(out io.Writer, doc render.Document, structured bool, planID, stored string) (cut bool, err error) {
	if structured {
		b, cut, err := render.JSON(doc)
		if err != nil {
			return false, runtimeFailure{err}
		}
		if _, err := out.Write(b); err != nil {
			return false, runtimeFailure{err}
		}
		return cut, nil
	}
	text := render.Text(doc, render.TextOptions{})
	if planID != "" {
		text += fmt.Sprintf("\nStored plan set %s (%s). Nothing has been changed.\nApply it with: stw tidy apply %s\n", planID, stored, planID)
	}
	if _, err := io.WriteString(out, text); err != nil {
		return false, runtimeFailure{err}
	}
	return false, nil
}

// wizardExit is exit 1 after the overview when the evidence is incomplete: a repository error or
// one skipped for a base or remote that could not be derived, a state directory that was
// refused, or a result that was cut. A complete analysis is exit 0.
func wizardExit(truncated bool, prep analysis.Prepared, res tidywizard.Result) error {
	if truncated {
		return runtimeFailure{errors.New("output-limit: the overview was cut to the bounds of the document")}
	}
	if derive.HasUnsafe(prep.Stores) {
		return runtimeFailure{errors.New("state-dir-unsafe: a state directory was skipped")}
	}
	for _, r := range res.Repositories {
		switch {
		case r.State == "error":
			return runtimeFailure{fmt.Errorf("repository %q could not be analysed", r.Name)}
		case r.State == "skipped" && (r.Reason == "base-ambiguous" || r.Reason == "remote-ambiguous" || r.Reason == "base-missing"):
			return runtimeFailure{fmt.Errorf("repository %q was not analysed: %s", r.Name, r.Reason)}
		}
	}
	return nil
}
