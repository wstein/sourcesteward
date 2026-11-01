// SPDX-License-Identifier: EUPL-1.2

package landcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/land"
	"github.com/wstein/sourcesteward/internal/snapcmd"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Wait bounds of --wait (FR-022); the plan does not wait, apply does.
const (
	DefaultWait = 30 * time.Second
	MaxWait     = 2 * time.Minute
	// DefaultHost is the only forge host of this slice.
	DefaultHost = "github.com"
)

// Flags are the parsed command line of `stw land`.
type Flags struct {
	Stack        string
	Reviewed     []string
	Pr           int
	Head         string
	Plan         bool
	Repo         string
	Path         string
	Remote       string
	DeleteBranch bool
	AckDraft     bool
	Yes          bool
	Wait         time.Duration
	WaitSet      bool
	JSON, Human  bool
	Host         string
}

// Params is a fully validated run plus the seams tests replace.
type Params struct {
	Flags Flags
	Chain []land.Item
	Host  string
	Out   io.Writer
	Diag  io.Writer
	Human bool

	// Transport is the in-process HTTP fixture of the default Open; nil means HTTPS.
	Transport hosting.Transport
	// Open builds the read-only forge reader (default: hosting.NewLandReaderWithTransport).
	Open func(ctx context.Context, target, host string) (Reader, func(), error)
	// Git replaces the Git runner (tests).
	Git land.Git
	// Environ is the process environment mode detection reads (default os.Environ).
	Environ []string
	// TTY reports whether stdin and stdout are terminals (default: the process streams).
	TTY func() (stdin, stdout bool)
	// Policy loads the trusted workspace policy. The default returns a policy that is not loaded,
	// which blocks the identity precondition: the policy store (Spec 043 T019/T020) is not built.
	Policy func(ctx context.Context, dir string) land.Policy
	// Shapes replaces the embedded allowlist of measured shapes (tests).
	Shapes *land.ShapeSet
	// Stdin and the output stream are what the default TTY reads.
	Stdin any
}

// Prepare validates the flags. Every error is a usage error (exit 2): nothing has been read.
func Prepare(_ context.Context, f Flags, out io.Writer) (Params, error) {
	var p Params
	switch {
	case f.Yes:
		return p, snapcmd.Usagef("--yes is not accepted: a landing is confirmed by typing the approval word on the terminal")
	case f.Pr != 0 || f.Head != "":
		if f.Stack != "" {
			return p, snapcmd.Usagef("--stack excludes --pr and --head")
		}
		return p, snapcmd.Usagef("landing one pull request (--pr, --head) is not implemented in this build; use --stack with at least two pull requests")
	case f.Stack == "":
		return p, snapcmd.Usagef("--stack NUMBER,NUMBER[,...] with one --reviewed NUMBER:SHA per pull request is required")
	case f.Human && f.JSON:
		return p, snapcmd.Usagef("--human and --json are mutually exclusive")
	case f.WaitSet && (f.Wait <= 0 || f.Wait > MaxWait):
		return p, snapcmd.Usagef("--wait must be positive and at most %s", MaxWait)
	case f.Repo != "" && !untrusted.ValidRepo(f.Repo):
		return p, snapcmd.Usagef("--repo %q is not OWNER/NAME", f.Repo)
	case f.Remote != "" && !land.RefName(f.Remote):
		return p, snapcmd.Usagef("--remote %q is not a remote name", f.Remote)
	}
	host := f.Host
	if host == "" {
		host = DefaultHost
	}
	if !watchcmd.ValidHost(host) {
		return p, snapcmd.Usagef("--host %q is not a host name", host)
	}
	chain, err := land.ParseChain(f.Stack, f.Reviewed)
	if err != nil {
		return p, fmt.Errorf("%w: %v", watchcmd.ErrUsage, err)
	}
	if f.Wait == 0 {
		f.Wait = DefaultWait
	}
	return Params{Flags: f, Chain: chain, Host: host, Out: out, Human: snapcmd.TerminalHuman(f.Human, f.JSON, out)}, nil
}

func (p *Params) mode() land.Mode {
	environ := p.Environ
	if environ == nil {
		environ = os.Environ()
	}
	in, out := false, false
	if p.TTY != nil {
		in, out = p.TTY()
	} else {
		in, out = owner.TerminalFacts(p.Stdin, p.Out)
	}
	ev := owner.Detect(owner.Env{StdinTTY: in, StdoutTTY: out, Environ: environ})
	markers := ev.Markers
	if markers == nil {
		markers = []string{}
	}
	return land.Mode{Mode: string(ev.Mode), StdinTTY: ev.StdinTTY, StdoutTTY: ev.StdoutTTY, Markers: markers}
}

// Execute runs the plan, or refuses the apply. The error is nil for exit 0 and a *watchcmd.Failure
// with code 1 when the plan is not landable or a read failed (the document is still printed).
//
// Owner-only rule (Spec 043 FR-001): without --plan the command is an apply. An agent-mode run is
// refused here, before the forge reader is opened and before Git runs: no credentialed read, no
// write. This slice has no apply; a human-mode apply is refused as apply-unavailable.
func Execute(ctx context.Context, p Params) error {
	mode := p.mode()
	f := p.Flags
	if !f.Plan {
		return p.refuseApply(mode)
	}
	dir := f.Path
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return snapcmd.Failure(watchcmd.ExitFailure, "cannot read the working directory")
		}
		dir = wd
	}
	var g land.Git = p.Git
	if g == nil {
		g = land.NewGit(dir)
	}
	remote, repo, err := p.resolve(ctx, g)
	if err != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "%v", err)
	}
	open := p.Open
	if open == nil {
		open = func(ctx context.Context, target, host string) (Reader, func(), error) {
			r, cleanup, err := hosting.NewLandReaderWithTransport(ctx, target, host, p.Transport)
			if err != nil {
				return nil, func() {}, err
			}
			return r, cleanup, nil
		}
	}
	reader, cleanup, err := open(ctx, dir, p.Host)
	if err != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "hosting-api-unavailable: %v", err)
	}
	defer cleanup()
	policy := land.Policy{}
	if p.Policy != nil {
		policy = p.Policy(ctx, dir)
	}
	opts := land.Options{Repo: repo, Remote: remote, Chain: p.Chain, AckDraft: f.AckDraft, Shapes: p.Shapes}
	report := land.Plan(ctx, opts, mode, policy, NewGitHubForge(reader, repo), g)
	report.Inputs.DeleteBr = f.DeleteBranch
	report.Inputs.WaitLimit = f.Wait.String()
	if err := p.print(report); err != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "cannot write the output")
	}
	if report.Landable || report.Result == land.ResultAlreadyLanded {
		return nil
	}
	return snapcmd.Failure(watchcmd.ExitFailure, "%s", summary(report))
}

// resolve picks the remote and the repository (--repo, else the remote URL).
func (p *Params) resolve(ctx context.Context, g land.Git) (remote, repo string, err error) {
	remote, repo = p.Flags.Remote, p.Flags.Repo
	if remote == "" {
		lister, ok := g.(interface {
			Remotes(context.Context) ([]string, error)
		})
		if !ok {
			remote = "origin"
		} else {
			remotes, err := lister.Remotes(ctx)
			if err != nil {
				return "", "", fmt.Errorf("cannot list the remotes: %v", err)
			}
			res := derive.Remote(".", remotes)
			if res.Ambiguity != nil {
				return "", "", fmt.Errorf("remote-ambiguous: pass --remote (candidates: %s)", strings.Join(res.Ambiguity.Candidates, ", "))
			}
			remote = res.Derived.Value
		}
	}
	if repo == "" {
		got, err := g.RemoteRepo(ctx, remote)
		if err != nil || got == "" {
			return "", "", fmt.Errorf("repo-unknown: pass --repo OWNER/NAME (remote %s is not a recognized GitHub URL)", untrusted.Esc(remote))
		}
		repo = got
	}
	return remote, repo, nil
}

// refuseApply prints the refusal document and exits 1. It reads nothing.
func (p *Params) refuseApply(mode land.Mode) error {
	code, detail := land.ReasonApplyUnavailable, "this build has no apply; use --plan"
	if mode.Agent() {
		code, detail = land.ReasonAgentMode, "apply is refused in agent mode; an agent may run only --plan"
	}
	f := p.Flags
	r := land.Report{
		SchemaVersion: land.SchemaVersion, Command: "land", Operation: "apply", Mode: mode, Repository: f.Repo,
		Inputs:        land.Inputs{Stack: stackNumbers(p.Chain), Reviewed: reviewedMap(p.Chain), AckDraft: f.AckDraft, Remote: f.Remote, DeleteBr: f.DeleteBranch, RangeMax: land.DefaultRangeBound},
		Preconditions: []land.Precondition{{ID: "mode", OK: false, Value: mode.Mode, Reason: code}},
		Chain:         []land.ChainEntry{}, Effects: []land.Effect{}, Children: []int{}, Result: land.ResultRefused,
		Reasons: []land.Reason{{Code: code, Detail: detail}}, Notes: []string{},
	}
	if r.Repository == "" {
		r.Repository = "unknown/unknown"
	}
	if err := p.print(r); err != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "cannot write the output")
	}
	return snapcmd.Failure(watchcmd.ExitFailure, "%s: %s", code, detail)
}

func stackNumbers(c []land.Item) []int {
	out := make([]int, 0, len(c))
	for _, it := range c {
		out = append(out, it.Number)
	}
	return out
}

func reviewedMap(c []land.Item) map[string]string {
	out := map[string]string{}
	for _, it := range c {
		out[fmt.Sprint(it.Number)] = it.Reviewed
	}
	return out
}

func summary(r land.Report) string {
	codes := make([]string, 0, len(r.Reasons))
	for _, x := range r.Reasons {
		codes = append(codes, x.Code)
	}
	if len(codes) == 0 {
		return "not landable: " + r.Result
	}
	return "not landable: " + strings.Join(codes, ", ")
}

func (p *Params) print(r land.Report) error {
	if p.Human {
		return writeHuman(p.Out, r)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = p.Out.Write(append(raw, '\n'))
	return err
}

func writeHuman(w io.Writer, r land.Report) error {
	var b strings.Builder
	e := untrusted.Esc
	stack := make([]string, 0, len(r.Inputs.Stack))
	for _, n := range r.Inputs.Stack {
		stack = append(stack, fmt.Sprint(n))
	}
	fmt.Fprintf(&b, "stw land --stack %s  %s  (%s, mode %s)\n", strings.Join(stack, ","), e(r.Repository), r.Operation, r.Mode.Mode)
	if r.DefaultTip != "" {
		fmt.Fprintf(&b, "default branch tip %s\n", r.DefaultTip[:8])
	}
	for _, c := range r.Chain {
		head := c.Head
		if len(head) > 8 {
			head = head[:8]
		}
		fmt.Fprintf(&b, "  #%d head %s base %s review:%s checks:%s state:%s -> %s\n", c.Number, e(head), e(c.BaseRef), c.ReviewEvidence, c.Checks, e(c.State), e(c.Outcome))
	}
	b.WriteString("preconditions:\n")
	for _, pre := range r.Preconditions {
		mark, subject := "ok  ", pre.ID
		if !pre.OK {
			mark = "FAIL"
		}
		if pre.Number != 0 {
			subject = fmt.Sprintf("%s #%d", pre.ID, pre.Number)
		}
		line := fmt.Sprintf("  %s %s: %s", mark, subject, e(pre.Value))
		if pre.Reason != "" {
			line += " [" + pre.Reason + "]"
		}
		b.WriteString(line + "\n")
	}
	for _, x := range r.Reasons {
		if x.Detail != "" {
			fmt.Fprintf(&b, "  reason %s: %s\n", x.Code, e(x.Detail))
		}
	}
	b.WriteString("effects:\n")
	for _, ef := range r.Effects {
		fmt.Fprintf(&b, "  %s %s: %s\n", ef.Kind, ef.Outcome, e(strings.Join(ef.Argv, " ")))
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "note: %s\n", e(n))
	}
	if r.PlanDigest != "" {
		fmt.Fprintf(&b, "plan digest %s\n", r.PlanDigest)
	}
	fmt.Fprintf(&b, "result: %s; landable %t; apply allowed %t\n", r.Result, r.Landable, r.ApplyAllowed)
	_, err := io.WriteString(w, b.String())
	return err
}
