// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/wstein/sourcesteward/internal/hosting"
	"strings"
)

func recordPath(product, version string) string {
	if product != "" {
		return "docs/releases/" + product + "/" + version + ".json"
	}
	return "docs/releases/" + version + ".json"
}
func planReadFailure(r PlanResult, ctx context.Context, err error) PlanResult {
	code := "read-failed"
	r.Status = "error"
	if errors.Is(err, errReleaseLimit) || errors.Is(err, hosting.ErrReleaseLimit) {
		code = "release-limit"
		r.Status = "refused"
	}
	if ctx.Err() != nil {
		code = "canceled"
		r.Status = "refused"
	}
	r.Boundary.Complete = false
	r.AllowlistMatches = nil
	r.Error = &CheckError{code, err.Error()}
	return r
}
func planRefusal(r PlanResult, code, msg string) PlanResult {
	r.Status = "refused"
	r.Error = &CheckError{code, msg}
	r.Boundary.Complete = false
	return r
}

func evaluateTagPlan(ctx context.Context, o PlanOptions, root string, r PlanResult) PlanResult {
	r.Mode = o.Mode
	if r.Mode == "" {
		r.Mode = "publish"
	}
	tag := o.Subject
	r.Subject.Tag = &tag
	r.root = root
	if o.Online {
		return planRefusal(r, "online-unavailable", "online release gates are not implemented in this source slice")
	}
	commit, annotated, e := resolveGitTag(ctx, root, tag)
	if errors.Is(e, errTagUnresolved) {
		return planRefusal(r, "tag-unresolved", e.Error())
	}
	if e != nil {
		return planReadFailure(r, ctx, e)
	}
	r.Subject.TagCommit = &commit
	r.Subject.Commit = &commit
	outcome := "fail"
	if annotated {
		outcome = "pass"
	}
	r.Gates = append(r.Gates, gate("tag-not-annotated", outcome, "", ""))
	g, e := newGitSource(ctx, root, commit)
	if e != nil {
		return planReadFailure(r, ctx, e)
	}
	resolved := hosting.Inspect(ctx, root, hosting.Options{Kind: "repo", Repo: o.Repo, Host: o.Host})
	if resolved.Status != "ok" || resolved.Repository == nil {
		return planRefusal(r, "remote-unresolved", resolved.Error())
	}
	remote := selectedMainRemote(resolved)
	if remote == "" {
		return planRefusal(r, "remote-unresolved", "no local remote matches the selected repository")
	}
	c, findings, code, e := tagContract(g, o.Product, tag)
	if e != nil {
		return planReadFailure(r, ctx, e)
	}
	if code != "" {
		return planRefusal(r, code, "cannot select a release contract for tag")
	}
	version := ""
	if c != nil {
		scheme, e := CompileVersionScheme(c.Version)
		if e == nil {
			version, _, _ = scheme.ParseTag(tag)
		}
	}
	r.Subject.Version = version
	if c != nil && c.Project.Product != "" {
		p := c.Project.Product
		r.Subject.Product = &p
	}
	tagVersion := gate("tag-version-mismatch", "not-run", "depends-on-record-missing", "")
	if r.Mode == "build" {
		tagVersion = gate("tag-version-mismatch", "fail", "", "")
		if version != "" {
			tagVersion.Outcome = "pass"
		}
	}
	if c != nil && version == "" {
		tagVersion = gate("tag-version-mismatch", "fail", "tag-grammar", "tag does not match the selected product grammar")
	}
	r.Gates = append(r.Gates, tagVersion)
	main, ancestry, e := g.mainAncestry(remote)
	if e != nil {
		return planReadFailure(r, ctx, e)
	}
	ancestry.Code = "tag-not-on-main"
	r.Subject.MainRef = main
	r.Gates = append(r.Gates, ancestry)
	if r.Mode == "build" {
		buildOpts := o
		buildOpts.Subject = commit
		buildOpts.Version = version
		if c != nil {
			buildOpts.Product = c.Project.Product
		}
		r = evaluateCommitPlan(ctx, buildOpts, root, g, r)
		r.Subject.Tag = &tag
		r.Subject.TagCommit = &commit
		return r
	}
	configOutcome := "pass"
	if len(findings) > 0 || c == nil {
		configOutcome = "fail"
	}
	r.Gates = append(r.Gates, gate("config-invalid", configOutcome, "", ""))
	if c == nil || version == "" {
		for _, code := range []string{"record-missing", "record-invalid", "record-not-canonical", "build-commit-not-ancestor", "diff-not-allowlisted"} {
			r.Gates = append(r.Gates, gate(code, "not-run", "depends-on-config-invalid", ""))
		}
		return finishPublish(r)
	}
	r.contract = c
	matches, e := g.allowlistMatches(c.Allowlist.Paths)
	if e != nil {
		return planReadFailure(r, ctx, e)
	}
	r.AllowlistMatches = matches
	recordSource := g
	if o.RecordCommit != "" {
		recordSource, e = newGitSource(ctx, root, o.RecordCommit)
		if errors.Is(e, errCommitUnresolved) {
			return planRefusal(r, "commit-unresolved", e.Error())
		}
		if e != nil {
			return planReadFailure(r, ctx, e)
		}
		r.Subject.RecordCommit = &o.RecordCommit
	}
	path := recordPath(c.Project.Product, version)
	entry, _, e := recordSource.pathEntry(path)
	if e != nil {
		return planReadFailure(r, ctx, e)
	}
	if entry.Kind != "file" {
		r.Gates = append(r.Gates, gate("record-missing", "fail", "", path))
		for _, code := range []string{"record-invalid", "record-not-canonical", "build-commit-not-ancestor", "diff-not-allowlisted"} {
			r.Gates = append(r.Gates, gate(code, "not-run", "depends-on-record-missing", ""))
		}
		return finishPublish(r)
	}
	r.Gates = append(r.Gates, gate("record-missing", "pass", "", ""))
	raw, cut, e := recordSource.Read(entry, MaxReleaseDocumentBytes)
	if e != nil {
		return planReadFailure(r, ctx, e)
	}
	if cut {
		return planReadFailure(r, ctx, errReleaseLimit)
	}
	// Canonicality is independent from structural validity: retain a schema-valid model so
	// later ancestry and diff gates still execute when only formatting is wrong.
	doc, ff := ParseReleaseDocument(raw)
	canonical := true
	if len(ff) > 0 && ff[0].Code == "record-not-canonical" {
		canonical = false
		var v ReleaseRecord
		if e = json.Unmarshal(raw, &v); e == nil {
			doc = &v
			ff = nil
		}
	}
	record, ok := doc.(*ReleaseRecord)
	valid := ok && len(ff) == 0
	if valid {
		sums := map[string]string{}
		for _, a := range record.Assets {
			if a.File != nil {
				sums[a.File.Name] = a.File.SHA256
			}
		}
		sums[record.Manifest.Name] = record.Manifest.SHA256
		valid = len(ValidateRecord(*record, RecordContext{Contract: c, Repository: resolved.Repository.Owner + "/" + resolved.Repository.Name, SignerWorkflowFile: o.signerWorkflowFile, Checksums: sums})) == 0
		if record.Tag != tag {
			valid = false
		}
		r.record = record
		r.Subject.Commit = &record.Commit
		if record.Version == version && record.Tag == tag {
			r.Gates[1] = gate("tag-version-mismatch", "pass", "", "")
		} else {
			r.Gates[1] = gate("tag-version-mismatch", "fail", "", "")
		}
	}
	invalid := "fail"
	if valid {
		invalid = "pass"
	}
	r.Gates = append(r.Gates, gate("record-invalid", invalid, "", ""))
	canonicalOutcome := "fail"
	if canonical && ok {
		canonicalOutcome = "pass"
	}
	r.Gates = append(r.Gates, gate("record-not-canonical", canonicalOutcome, "", ""))
	if !ok {
		for _, code := range []string{"build-commit-not-ancestor", "diff-not-allowlisted"} {
			r.Gates = append(r.Gates, gate(code, "not-run", "depends-on-record-invalid", ""))
		}
		return finishPublish(r)
	}
	built, e := newGitSource(ctx, root, record.Commit)
	if errors.Is(e, errCommitUnresolved) {
		r.Gates = append(r.Gates, gate("build-commit-not-ancestor", "fail", "commit-unresolved", ""), gate("diff-not-allowlisted", "not-run", "depends-on-build-commit-not-ancestor", ""))
		return finishPublish(r)
	}
	if e != nil {
		return planReadFailure(r, ctx, e)
	}
	ancestryOK := true
	reason := ""
	if ancestry.Outcome == "not-run" {
		reason = *ancestry.Reason
	} else {
		ancestryOK, e = gitAncestor(ctx, root, built.commit, commit)
		if e != nil {
			return planReadFailure(r, ctx, e)
		}
		if o.RecordCommit != "" {
			asc, e := gitAncestor(ctx, root, commit, o.RecordCommit)
			if e != nil {
				return planReadFailure(r, ctx, e)
			}
			rm, rg, e := recordSource.mainAncestry(remote)
			_ = rm
			if e != nil {
				return planReadFailure(r, ctx, e)
			}
			ancestryOK = ancestryOK && asc && record.Commit == commit && rg.Outcome == "pass"
			if rg.Outcome == "not-run" {
				reason = *rg.Reason
			}
		}
	}
	ancestorOutcome := "pass"
	if !ancestryOK {
		ancestorOutcome = "fail"
	}
	if reason != "" {
		ancestorOutcome = "not-run"
	}
	r.Gates = append(r.Gates, gate("build-commit-not-ancestor", ancestorOutcome, reason, ""))
	if ancestorOutcome != "pass" {
		r.Gates = append(r.Gates, gate("diff-not-allowlisted", "not-run", "depends-on-build-commit-not-ancestor", ""))
		return finishPublish(r)
	}
	to := commit
	if o.RecordCommit != "" {
		to = o.RecordCommit
	}
	diff, e := gitDiff(ctx, root, record.Commit, to, c.Allowlist.Paths, path)
	if e != nil {
		return planReadFailure(r, ctx, e)
	}
	r.diff = diff
	diffOutcome := "pass"
	if len(diff.Offending) > 0 {
		diffOutcome = "fail"
	}
	r.Gates = append(r.Gates, gate("diff-not-allowlisted", diffOutcome, "", strings.Join(diff.Offending, ", ")))
	// The contract at C is authoritative for record fields and signer selection.
	bc, bf, bcode, e := planContract(built, c.Project.Product)
	if e != nil {
		return planReadFailure(r, ctx, e)
	}
	if bcode != "" || len(bf) > 0 || bc == nil {
		for i := range r.Gates {
			if r.Gates[i].Code == "config-invalid" {
				r.Gates[i].Outcome = "fail"
			}
		}
	} else {
		r.contract = bc
		sums := map[string]string{}
		for _, asset := range record.Assets {
			if asset.File != nil {
				sums[asset.File.Name] = asset.File.SHA256
			}
		}
		sums[record.Manifest.Name] = record.Manifest.SHA256
		recordValid := len(ValidateRecord(*record, RecordContext{Contract: bc, Repository: resolved.Repository.Owner + "/" + resolved.Repository.Name, SignerWorkflowFile: o.signerWorkflowFile, Checksums: sums})) == 0 && record.Tag == tag
		for i := range r.Gates {
			if r.Gates[i].Code == "record-invalid" {
				r.Gates[i].Outcome = "fail"
				if recordValid {
					r.Gates[i].Outcome = "pass"
				}
			}
		}
		trusted, _ := json.Marshal(bc)
		evaluated, _ := json.Marshal(c)
		if !bytes.Equal(trusted, evaluated) {
			for i := range r.Gates {
				if r.Gates[i].Code == "diff-not-allowlisted" {
					r.Gates[i].Outcome = "fail"
				}
			}
		}
	}
	return finishPublish(r)
}
func finishPublish(r PlanResult) PlanResult {
	for _, code := range []string{"ci-not-green", "version-not-monotonic", "already-published", "draft-missing"} {
		r.Gates = append(r.Gates, gate(code, "not-run", "needs-online", ""))
	}
	for _, code := range []string{"artifact-missing", "artifact-unexpected", "artifact-mismatch", "attestation-missing", "attestation-invalid"} {
		r.Gates = append(r.Gates, gate(code, "not-applicable", "use-verify", ""))
	}
	r.Status = "ok"
	r.Verdict = Verdict(r.Gates)
	r.Boundary.Complete = true
	return r
}
func tagContract(g *gitSource, product, tag string) (*Contract, []Finding, string, error) {
	if product != "" {
		return planContract(g, product)
	}
	entries, cut, e := g.StwEntries()
	if e != nil {
		return nil, nil, "", e
	}
	if cut {
		return nil, nil, "", errReleaseLimit
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name)
	}
	files, ff := DiscoverProducts(names)
	if len(ff) > 0 {
		return planContract(g, product)
	}
	if len(files) <= 1 {
		return planContract(g, product)
	}
	var selected *Contract
	var found []Finding
	for _, f := range files {
		c, findings, code, e := planContract(g, f.Product)
		if e != nil {
			return nil, nil, "", e
		}
		if code != "" || c == nil {
			continue
		}
		scheme, e := CompileVersionScheme(c.Version)
		if e != nil {
			continue
		}
		if _, _, ok := scheme.ParseTag(tag); ok {
			if selected != nil {
				return nil, nil, "product-required", nil
			}
			selected = c
			found = findings
		}
	}
	if selected == nil {
		return nil, nil, "product-unknown", nil
	}
	return selected, found, "", nil
}
