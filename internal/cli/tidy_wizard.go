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
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/analysis"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// The seams of the wizard-layer `tidy plan`; tests replace them.
var (
	tidyPrepare = func(ctx context.Context, o analysis.Options) (analysis.Prepared, error) {
		return analysis.Prepare(ctx, o, analysis.DefaultDeps())
	}
	tidyAnalyse = analysis.Run
	// The working directory, absolute (the CLI imports no os: filepath.Abs resolves ".").
	tidyGetwd = func() (string, error) { return filepath.Abs(".") }
	tidyNow   = time.Now
	// The clock of the progress timings, and the terminal test of stderr (an *os.File that is a terminal).
	tidyProgressNow      = time.Now
	tidyOpenTrace        = workspace.OpenTraceFile
	tidyStderrIsTerminal = owner.IsTerminal
	tidyPlannerFor       = func(p analysis.Prepared) planset.Planner {
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

// wizardRun is the read-only analysis both wizard entry points start from.
type wizardRun struct {
	ctx        context.Context
	cancel     context.CancelFunc
	prep       analysis.Prepared
	res        tidywizard.Result
	mode       owner.Evidence
	cfg        tidywizard.Config
	timeoutSet bool
	timeout    time.Duration
	// online is the pull request evidence of the human wizard (empty when it ran offline).
	online tidywizard.OnlineOutcome
	// budget is the run budget of the online reads, shared by the tracking reads of the
	// observation and the pull request reads (nil when the run is offline).
	budget *hosting.TidyRunBudget
	// now is the one clock of the run: the classifier and the risk ladder read the same instant.
	now time.Time
}

// readOnline reads the pull request evidence of the open tips when the human wizard is online,
// says on stderr, once, how many repositories were checked and why the others are judged offline,
// and records the derived online entry. A failure is never an error: the levels are the offline
// ones (Spec 044 D13).
func (r *wizardRun) readOnline(cmd *cobra.Command, choice tidyOnlineChoice) {
	if !choice.On {
		r.prep.Derived = append(append([]derive.Derived{}, r.prep.Derived...), derive.Derived{Name: "online", Value: choice.Value, Source: choice.Source, Overridden: true})
		return
	}
	members := tidyOnlineMembers(r.prep, r.res)
	out := tidyOnlineFetch(r.ctx, members, r.res.Candidates, r.budget)
	r.online = out
	// The merged pull request of a tip is evidence for the risk ladder and the `pr` member of the JSON.
	out.AttachPR(r.res.Candidates)
	notice, entry := tidyOnlineSummary(choice, members, r.res, out)
	r.prep.Derived = append(append([]derive.Derived{}, r.prep.Derived...), entry)
	if notice != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), notice)
	}
}

// startWizard detects the mode, derives the caller, analyses the workspace read only and narrows the
// view (--only, --mine, --by-owner). The caller must call cancel.
func startWizard(cmd *cobra.Command, w *tidyWizardOptions, timeout time.Duration) (*wizardRun, error) {
	if cmd.Flags().Changed("timeout") && timeout <= 0 {
		return nil, fmt.Errorf("--timeout must be a positive duration")
	}
	if w.quiet && w.progress {
		return nil, fmt.Errorf("--quiet and --progress exclude each other")
	}
	if timeout > workspace.ObservationTimeoutMax {
		return nil, fmt.Errorf("--timeout must not exceed %s", workspace.ObservationTimeoutMax)
	}
	stdinTTY, stdoutTTY := tidyTerminalFacts(cmd.InOrStdin(), cmd.OutOrStdout())
	environ := tidyEnviron()
	mode := owner.Detect(owner.Env{StdinTTY: stdinTTY, StdoutTTY: stdoutTTY, Environ: environ, OSUser: planset.Getenv("USER"), Mine: w.mine, ByOwner: w.byOwner})
	// Online is the default of the human wizard only (Spec 044 D13); every other run is offline unless --online.
	humanWizard := w.interactive && !w.structured && mode.Mode == owner.ModeHuman
	online, err := resolveTidyOnline(cmd.Flags().Changed("online"), w.offline, humanWizard)
	if err != nil {
		return nil, err
	}
	caller := derive.Caller(environValue(environ, "STW_OWNER"), planset.Getenv("USER"), mode.Mode == owner.ModeAgent)
	if w.mine && caller.Value == "" {
		return nil, fmt.Errorf("--mine needs the caller label: set $STW_OWNER")
	}
	cwd, err := tidyGetwd()
	if err != nil {
		return nil, runtimeFailure{err}
	}
	r := &wizardRun{ctx: cmd.Context(), cancel: func() {}, mode: mode, timeoutSet: cmd.Flags().Changed("timeout"), timeout: timeout}
	if r.timeoutSet {
		r.ctx, r.cancel = context.WithTimeout(r.ctx, timeout)
		// The same value is the observation window of every repository (it replaces the scaled default).
		r.ctx = workspace.WithObservationTimeout(r.ctx, timeout)
	}
	// Progress goes to stderr only: a live block on a terminal that takes cursor movement, plain
	// lines for a verbose run that is not on one (and on a dumb terminal), nothing at all with --json. The summary of --verbose follows the analysis.
	stderr := cmd.ErrOrStderr()
	progressMode := tidyProgressOff
	switch {
	case w.structured || w.quiet:
	case tidyStderrIsTerminal(stderr) && !tidyDumbTerminal(environ):
		progressMode = tidyProgressLive
	case tidyStderrIsTerminal(stderr):
		// TERM unset or dumb: no cursor movement, the plain lines come every few seconds.
		progressMode = tidyProgressPlain
	case w.verbose || w.progress:
		progressMode = tidyProgressPlain
	}
	prog := newTidyProgress(stderr, progressMode, tidyProgressNow)
	if progressMode == tidyProgressLive {
		prog.color = stwsync.ColorEnabled(stderr)
		prog.ascii = !tidyUTF8Locale(environ)
		prog.size = func() (int, int) { return stwsync.TerminalSize(stderr) }
	}
	r.ctx = workspace.WithObserveProgress(r.ctx, prog.Event)
	if w.verbose || w.trace != "" {
		r.ctx = workspace.WithGitTrace(r.ctx, prog.Git)
	}
	if humanWizard && online.On {
		// One budget for the whole run: the tracking reads of the observation and the pull request
		// reads count against the same 512 requests and stop together (Spec 044 D13).
		r.budget = hosting.NewTidyRunBudget(nil, hosting.TidyRunMaxRequests)
		r.ctx = hosting.WithTidyRunBudget(r.ctx, r.budget)
		cancel := r.cancel
		r.cancel = func() { cancel(); r.budget.Close() }
	}
	preparing := tidyProgressNow()
	prep, err := tidyPrepare(r.ctx, analysis.Options{Workspace: w.workspace, CWD: cwd, Members: w.member, MemberBases: w.memberBase, StateDirs: w.stateDirs, Online: online.On, OnlineDefault: online.Value == tidyOnlineDefault})
	prog.Prepared(tidyProgressNow().Sub(preparing))
	if err == nil && w.trace != "" {
		// The trace file is created only now, when the repositories are known: a new file inside
		// one of them would be an untracked file that changes what the analysis finds.
		roots := []string{prep.Workspace}
		for _, spec := range prep.Specs {
			roots = append(roots, spec.Path)
		}
		if root, inside := tracePathInside(cwd, w.trace, roots); inside {
			err = &analysis.UsageError{Code: "trace-inside-repository", Message: fmt.Sprintf("--trace %q is inside %q; write the trace outside the workspace and its repositories", w.trace, root)}
		} else if f, openErr := tidyOpenTrace(w.trace); openErr != nil {
			err = &analysis.UsageError{Code: "trace-unusable", Message: openErr.Error()}
		} else {
			defer f.Close()
			prog.trace = f
		}
	}
	if err != nil {
		r.cancel()
		prog.Finish()
		// A deadline or an interrupt comes first: discovery reports its context ending as a
		// missing workspace, which it is not.
		if cause := interruption(r.ctx, r.timeoutSet, timeout); cause != nil {
			return nil, runtimeFailure{cause}
		}
		// The message carries paths and error text from outside: it passes esc, whole.
		safe := errors.New(escFull(err.Error()))
		var usage *analysis.UsageError
		if errors.As(err, &usage) {
			return nil, safe
		}
		return nil, runtimeFailure{safe}
	}
	r.prep = prep
	prog.SetRepos(prep.Names)
	r.cfg = tidywizard.Config{Caller: caller.Value, Agent: mode.Mode == owner.ModeAgent, CWD: cwd, Now: tidyNow()}
	r.res = tidyAnalyse(r.ctx, prep, r.cfg)
	r.now = r.cfg.Now
	prog.Finish()
	if w.verbose {
		prog.Summary()
	}
	if humanWizard && online.Value != "" {
		r.readOnline(cmd, online)
	}
	r.res.Candidates = narrowView(r.res.Candidates, w, caller.Value)
	return r, nil
}

// runTidyWizardPlan is `stw tidy plan` in the wizard layer: it analyses the workspace read only,
// narrows the view (--only, --mine, --by-owner), optionally stores the plan set of the safe own
// items (--save), prints the overview as text or JSON, and never applies anything.
func runTidyWizardPlan(cmd *cobra.Command, w *tidyWizardOptions, structured bool, timeout time.Duration) error {
	w.structured = structured
	run, err := startWizard(cmd, w, timeout)
	if err != nil {
		return err
	}
	defer run.cancel()
	ctx, prep, res, mode := run.ctx, run.prep, run.res, run.mode
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
	doc := render.Build(render.Input{Workspace: prep.Workspace, Derived: prep.Derived, Repos: prep.Meta, Result: res, Mode: mode, PlanID: planID, Notices: append(append([]string{}, prep.Notices...), notices...), Now: run.now})
	cut, err := writeWizardOverview(cmd.OutOrStdout(), doc, structured, w.rows, planID, stored)
	if err != nil {
		return err
	}
	// The overview is always printed first; what went wrong is reported after it (exit 1).
	if cause := interruption(ctx, run.timeoutSet, timeout); cause != nil {
		// The cause comes first; a save error that followed it is only detail.
		if saveErr != nil {
			cause = fmt.Errorf("%w (storing the plan set failed: %s)", cause, escFull(saveErr.Error()))
		}
		return runtimeFailure{cause}
	}
	if saveErr != nil {
		return runtimeFailure{errors.New(escFull(saveErr.Error()))}
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

// maxEscapedMessage bounds the input of one escaped message (characters): a longer one is cut
// with "...", so an enormous error cannot flood the terminal.
const maxEscapedMessage = 4096

// escFull escapes a message that carries outside text without cutting it at esc's 120-character
// bound: the message is escaped in pieces of at most 100 characters and joined, up to
// maxEscapedMessage characters in all.
func escFull(s string) string {
	var b strings.Builder
	rs := []rune(s)
	cut := false
	if len(rs) > maxEscapedMessage {
		rs, cut = rs[:maxEscapedMessage], true
	}
	for len(rs) > 0 {
		n := min(len(rs), 100)
		b.WriteString(untrusted.Esc(string(rs[:n])))
		rs = rs[n:]
	}
	if cut {
		b.WriteString("...")
	}
	return b.String()
}

// tidyDumbTerminal reports whether TERM is unset, empty or dumb: such a terminal gets no escape
// sequences (the rule of internal/statuscmd/terminal.go).
func tidyDumbTerminal(environ []string) bool {
	t := environValue(environ, "TERM")
	return t == "" || t == "dumb"
}

// tidyUTF8Locale reports whether the first of LC_ALL, LC_CTYPE and LANG that is set names UTF-8.
func tidyUTF8Locale(environ []string) bool {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := environValue(environ, name); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
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
	return set.PlanID, summarize(set, res.Candidates), "", nil
}

// writeWizardOverview prints the overview and reports whether the JSON had to be cut to its bound.
func writeWizardOverview(out io.Writer, doc render.Document, structured, rows bool, planID, stored string) (cut bool, err error) {
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
	text := tidyOverviewText(out, doc, rows)
	if planID != "" {
		text += fmt.Sprintf("\nStored plan set %s (%s). Nothing has been changed.\nApply it with: stw tidy apply %s\n", planID, stored, planID)
	}
	if _, err := io.WriteString(out, text); err != nil {
		return false, runtimeFailure{err}
	}
	return false, nil
}

// tidyOverviewText is the human text of the overview: the grouped overview followed by the retain
// table (Spec 044), or with --rows the row view of Spec 041, byte for byte as before.
func tidyOverviewText(out io.Writer, doc render.Document, rows bool) string {
	if rows {
		return render.Text(doc, tidyTextOptions(out))
	}
	opts := render.OverviewOptions{Color: tidyTextOptions(out).Color}
	text := render.Overview(doc, opts)
	if table := render.RetainTable(doc, opts); table != "" {
		text += "\n" + table
	}
	return text
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
