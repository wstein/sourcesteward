// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/wstein/sourcesteward/internal/inspection"
)

// planner carries the observations of one plan.
type planner struct {
	opts          Options
	res           *Result
	git           *gitRunner
	now           func() time.Time
	root          string // worktree root of the target checkout
	gdir          string // absolute git directory of the target worktree
	cdir          string // common git directory
	moved         []rawCommit
	touched       []string
	replayed      []string // quarantine commit per moved commit; empty when dropped or not replayed
	replayedCount int
	emptyAfter    []string
	inspect       func(context.Context, *planner) // test seam: sees the planner before the quarantine is removed
	quarantine    string
}

// Plan reads the explicit input and returns one restack-plan result. It writes nothing to the
// repository; its only writes are a private temporary directory that is removed on every path.
func Plan(ctx context.Context, o Options) Result { return plan(ctx, o, time.Now) }

func plan(ctx context.Context, o Options, now func() time.Time) Result {
	return planWith(ctx, o, now, nil)
}

func planWith(ctx context.Context, o Options, now func() time.Time, inspect func(context.Context, *planner)) Result {
	res := newResult(o, now)
	ctx, cancel := context.WithTimeout(ctx, TimeoutSeconds*time.Second)
	defer cancel()
	p := &planner{opts: o, res: &res, now: now, inspect: inspect}
	err := p.run(ctx)
	p.res.ObservedAt = now().UTC().Format(time.RFC3339)
	if err != nil {
		status, d, bound := diagnosticFor(err)
		p.res.Status, p.res.Outcome, p.res.Evidence = status, OutcomeNone, nil
		p.res.Diagnostics = append(p.res.Diagnostics, d)
		if bound != "" {
			p.res.Boundary.Reached = &bound
		}
		return *p.res
	}
	p.res.Status = StatusOK
	p.res.Outcome = decideOutcome(p.res)
	if encoded, e := json.Marshal(p.res); e != nil || len(encoded) > MaxResultBytes {
		bound := "max_result_bytes"
		p.res.Status, p.res.Outcome, p.res.Evidence = StatusIncomplete, OutcomeNone, nil
		p.res.Diagnostics = append(p.res.Diagnostics, Diagnostic{"bound-exceeded", "A documented bound was reached.", map[string]any{"bound": bound}})
		p.res.Boundary.Reached = &bound
	}
	return *p.res
}

func (p *planner) run(ctx context.Context) error {
	if err := p.admit(ctx); err != nil {
		return err
	}
	home, err := os.MkdirTemp("", "stw-restack-home-")
	if err != nil {
		return refuse("plan-environment-unavailable", "A private plan environment could not be created.", nil)
	}
	defer os.RemoveAll(home)
	objects, err := os.MkdirTemp("", "stw-restack-objects-")
	if err != nil {
		return refuse("plan-environment-unavailable", "A private plan environment could not be created.", nil)
	}
	defer os.RemoveAll(objects)
	bin, err := exec.LookPath("git")
	if err != nil {
		return refuse("git-unsupported", "The Git executable is unavailable.", nil)
	}
	p.git = &gitRunner{bin: bin, dir: p.root, home: home, objectDir: objects, alternates: filepath.Join(p.cdir, "objects"), now: p.now}
	if err := p.checkRepository(ctx); err != nil {
		return err
	}
	if err := p.checkTarget(ctx); err != nil {
		return err
	}
	if err := p.resolve(ctx); err != nil {
		return err
	}
	if err := p.checkAttributes(ctx); err != nil {
		return err
	}
	if err := p.collect(ctx); err != nil {
		return err
	}
	if err := p.effects(ctx); err != nil {
		return err
	}
	p.texts()
	if p.inspect != nil {
		p.inspect(ctx, p)
	}
	return nil
}

// admit decides ownership and canonical paths with the shared read-only inspection (FR-003, FR-014).
func (p *planner) admit(ctx context.Context) error {
	path := p.opts.Path
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return refuse("path-not-canonical", "The path cannot be made absolute.", nil)
	}
	if real, err := filepath.EvalSymlinks(abs); err != nil || real != abs {
		return refuse("path-not-canonical", "The path is not canonical: it contains a symbolic link or does not exist.", nil)
	}
	id := inspection.Inspect(ctx, abs)
	if err := ctx.Err(); err != nil {
		return err
	}
	p.res.Backend = id.Backend
	switch {
	case id.Backend == "jj" || id.Backend == "gitbutler":
		return refuse("unsupported-backend", "Restack plan supports native Git only; jj and GitButler are refused without a Git fallback.", map[string]any{"backend": id.Backend})
	case id.Backend == "ambiguous":
		return refuse("ambiguous-ownership", "The checkout is owned by more than one backend.", nil)
	case id.Status != "ok" || id.Backend != "git" || id.Repository == nil:
		code := "inspection-failed"
		if len(id.Diagnostics) > 0 && id.Diagnostics[0].Code == "not-repository" {
			code = "not-repository"
		}
		return refuse(code, "The path is not a readable native Git checkout.", nil)
	}
	r := id.Repository
	if r.Bare || r.Worktree == nil {
		return refuse("bare-repository", "A bare repository has no checkout to plan from.", nil)
	}
	if r.ObjectFormat != "sha1" {
		return refuse("object-format-unsupported", "Only SHA-1 repositories are supported.", map[string]any{"object_format": r.ObjectFormat})
	}
	for _, dir := range []string{*r.Worktree, r.GitDir, r.CommonDir} {
		if real, err := filepath.EvalSymlinks(dir); err != nil || real != dir {
			return refuse("symlink-hazard", "The checkout or its Git directory is reached through a symbolic link.", nil)
		}
	}
	p.res.Identity = &id
	p.root, p.gdir, p.cdir = *r.Worktree, r.GitDir, r.CommonDir
	return nil
}
