// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"fmt"
	"github.com/wstein/sourcesteward/internal/hosting"
	"path"
	"strings"
	"time"
)

// Plan acquires online evidence only after complete native ownership and committed
// configuration selection. The offline path never initializes credentials.
func Plan(ctx context.Context, o PlanOptions) PlanResult {
	if o.Online {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
	}
	return planWithReader(ctx, o, hosting.ReadRelease)
}

type releaseReader func(context.Context, string, hosting.Repository, string, []string) (hosting.ReleaseEvidence, error)

func planWithReader(ctx context.Context, o PlanOptions, reader releaseReader) PlanResult {
	offline := o
	offline.Online = false
	r := planOffline(ctx, offline)
	if o.Online {
		r = applyOnlinePlanWithReader(ctx, o, r, false, reader)
	}
	return escapePlan(r)
}
func applyOnlinePlan(ctx context.Context, o PlanOptions, r PlanResult, published bool) PlanResult {
	return applyOnlinePlanWithReader(ctx, o, r, published, hosting.ReadRelease)
}
func applyOnlinePlanWithReader(ctx context.Context, o PlanOptions, r PlanResult, published bool, reader releaseReader) PlanResult {
	if r.Status != "ok" || r.contract == nil || r.Subject.Commit == nil {
		return r
	}
	for _, g := range r.Gates {
		if (g.Code == "config-invalid" || g.Code == "version-invalid" || g.Code == "record-invalid") && g.Outcome != "pass" {
			return r
		}
	}
	resolved := hosting.Inspect(ctx, r.root, hosting.Options{Kind: "repo", Repo: o.Repo, Host: o.Host})
	if resolved.Status != "ok" || resolved.Repository == nil {
		return planRefusal(r, "remote-unresolved", resolved.Error())
	}
	e, err := reader(ctx, r.root, *resolved.Repository, *r.Subject.Commit, r.contract.CI.Workflows)
	for _, req := range e.Network {
		r.Network = append(r.Network, PlanRequest{req.Method, req.Endpoint})
	}
	r.Boundary.Effects.Network = len(r.Network) > 0
	if ctx.Err() != nil {
		return planReadFailure(r, ctx, ctx.Err())
	}
	if err != nil {
		return planReadFailure(r, ctx, fmt.Errorf("online release evidence unavailable: %w", err))
	}
	o.Version = r.Subject.Version
	o.Mode = r.Mode
	gates, err := onlineGatesAtCommit(o, r.contract, r.record, e, published, *r.Subject.Commit)
	if err != nil {
		return planReadFailure(r, ctx, err)
	}
	replaceGates(&r, gates)
	r.Advisories = []PlanAdvisory{rulesetAdvisory(r.contract, e)}
	r.Verdict = Verdict(r.Gates)
	return r
}
func replaceGates(r *PlanResult, gates []Gate) {
	for _, g := range gates {
		found := false
		for i := range r.Gates {
			if r.Gates[i].Code == g.Code {
				r.Gates[i] = g
				found = true
			}
		}
		if !found {
			r.Gates = append(r.Gates, g)
		}
	}
}
func onlineGates(o PlanOptions, c *Contract, record *ReleaseRecord, e hosting.ReleaseEvidence, published bool) ([]Gate, error) {
	commit := o.Subject
	if record != nil {
		commit = record.Commit
	}
	return onlineGatesAtCommit(o, c, record, e, published, commit)
}
func onlineGatesAtCommit(o PlanOptions, c *Contract, record *ReleaseRecord, e hosting.ReleaseEvidence, published bool, commit string) ([]Gate, error) {
	s, err := CompileVersionScheme(c.Version)
	if err != nil {
		return nil, err
	}
	names := append([]string{}, e.Tags...)
	tag, err := s.Tag(o.Version)
	if err != nil {
		return nil, err
	}
	exists := false
	publishedExists := false
	draftComplete := false
	files := map[string]bool{}
	if record != nil {
		for _, f := range recordFiles(*record) {
			files[f.Name] = true
		}
	}
	for _, t := range e.Tags {
		if t == tag && !o.tagSubject() {
			exists = true
		}
	}
	for _, release := range e.Releases {
		if release.Draft == nil {
			return nil, fmt.Errorf("incomplete release draft evidence")
		}
		names = append(names, release.Tag)
		if release.Tag != tag {
			continue
		}
		if !*release.Draft {
			publishedExists = true
			exists = true
			continue
		}
		if o.WithDrafts {
			exists = true
		}
		missing := map[string]bool{}
		for n := range files {
			missing[n] = true
		}
		for _, a := range release.Assets {
			delete(missing, a.Name)
		}
		if len(missing) == 0 {
			draftComplete = true
		}
	}
	subject := ""
	if o.Mode == "publish" || o.tagSubject() {
		subject = o.Subject
	}
	monotonic, err := s.CheckMonotonicity(o.Version, names, subject)
	if err != nil {
		return nil, err
	}
	outcome := "pass"
	if !monotonic.Monotonic {
		outcome = "fail"
	}
	gates := []Gate{gate("version-not-monotonic", outcome, "", fmt.Sprintf("matched %d names; ignored %d nonmatching names", monotonic.Matched, monotonic.Ignored))}
	if o.Mode != "publish" {
		reason := ""
		if !o.WithDrafts {
			reason = "drafts-not-checked"
		}
		outcome = "pass"
		if exists {
			outcome = "fail"
		}
		gates = append(gates, gate("release-exists", outcome, reason, "tags and published releases checked; drafts checked only with --with-drafts"))
	} else {
		outcome = "pass"
		if publishedExists {
			outcome = "fail"
		}
		gates = append(gates, gate("already-published", outcome, "", ""))
		outcome = "fail"
		detail := "absent or not visible"
		if o.WithDrafts {
			detail = "absent"
		}
		if draftComplete {
			outcome = "pass"
			detail = "draft covers recorded files"
		}
		gates = append(gates, gate("draft-missing", outcome, "", detail))
	}
	ci := gate("ci-not-green", "pass", "", "")
	for _, workflow := range c.CI.Workflows {
		found := false
		pending := false
		failed := false
		for _, run := range e.Runs {
			if run.Workflow != workflow || run.HeadSHA != commit || run.Event != "push" || run.Branch != "main" {
				continue
			}
			if run.Status != "completed" {
				pending = true
			} else if run.Conclusion != nil && *run.Conclusion == "success" {
				found = true
			} else {
				failed = true
			}
		}
		if !found {
			reason := "ci-run-absent"
			if pending {
				reason = "ci-run-pending"
			} else if failed {
				reason = "ci-run-failed"
			}
			ci = gate("ci-not-green", "fail", reason, workflow)
			break
		}
	}
	gates = append(gates, ci)
	if published {
		for i := range gates {
			switch gates[i].Code {
			case "version-not-monotonic", "already-published", "draft-missing":
				gates[i] = gate(gates[i].Code, "not-applicable", "published-audit", "")
			}
		}
	}
	return gates, nil
}

func onlineText(r PlanResult) string {
	if r.Boundary.Effects.Network {
		return "bounded read-only online evidence"
	}
	return "none (offline)"
}

func rulesetAdvisory(c *Contract, e hosting.ReleaseEvidence) PlanAdvisory {
	a := PlanAdvisory{"tag-ruleset", "unknown", "endpoint shape and permissions unverified; owner-only bypass cannot be verified; confirm manually"}
	if !e.RulesetsKnown {
		return a
	}
	prefix, ok := c.TagPrefixLiteral()
	if !ok {
		return a
	}
	probe := "refs/tags/" + prefix + "1"
	a.State = "absent"
	for _, r := range e.Rulesets {
		if r.Target != "tag" || r.Enforcement != "active" {
			continue
		}
		if r.Conditions == nil || r.Conditions.RefName == nil || r.Rules == nil {
			a.State = "unknown"
			continue
		}
		included := false
		for _, glob := range r.Conditions.RefName.Include {
			match, err := path.Match(glob, probe)
			if err != nil {
				a.State = "unknown"
				continue
			}
			included = included || match
		}
		for _, glob := range r.Conditions.RefName.Exclude {
			match, err := path.Match(glob, probe)
			if err != nil {
				a.State = "unknown"
			}
			if match {
				included = false
			}
		}
		if !included {
			continue
		}
		missing := []string{}
		for _, want := range []string{"creation", "update", "deletion"} {
			found := false
			for _, rule := range r.Rules {
				found = found || rule.Type == want
			}
			if !found {
				missing = append(missing, want)
			}
		}
		if len(missing) == 0 {
			a.State = "present"
			return a
		}
		a.State = "partial"
		a.Detail += "; missing " + strings.Join(missing, ", ")
	}
	return a
}
