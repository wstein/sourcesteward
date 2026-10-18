// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/inspection"
)

var errReleaseLimit = errors.New("release input exceeds a documented limit")

type PlanOptions struct {
	signerWorkflowFile                                        string
	Path, Subject, Product, Version, Mode, RecordCommit, Repo string
	Online, WithDrafts                                        bool
}
type MainRef struct {
	Remote  string  `json:"remote"`
	Commit  *string `json:"commit"`
	Fetched bool    `json:"fetched"`
}
type PlanSubject struct {
	Product      *string `json:"product"`
	Commit       *string `json:"commit"`
	Tag          *string `json:"tag"`
	Version      string  `json:"version"`
	TagCommit    *string `json:"tag_commit"`
	RecordCommit *string `json:"record_commit"`
	MainRef      MainRef `json:"main_ref"`
}
type PlanInvocation struct {
	Target *string  `json:"target"`
	Argv   []string `json:"argv"`
}
type PlanBuild struct {
	ID           string           `json:"id"`
	Scope        string           `json:"scope"`
	Network      string           `json:"network"`
	NetworkHosts []string         `json:"network_hosts"`
	LockFiles    []string         `json:"lock_files"`
	Invocations  []PlanInvocation `json:"invocations"`
}
type PlanRequest struct {
	Method   string `json:"method"`
	Endpoint string `json:"endpoint"`
}
type PlanAdvisory struct {
	Code   string `json:"code"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// AllowlistMatch discloses conservative tracked-leaf reach, not publication eligibility.
type AllowlistMatch struct {
	Pattern      string `json:"pattern"`
	TrackedFiles uint64 `json:"tracked_files"`
}

func newAllowlistMatches(patterns []string) []AllowlistMatch {
	rows := make([]AllowlistMatch, len(patterns))
	for i, p := range patterns {
		rows[i].Pattern = p
	}
	return rows
}
func countAllowlistPath(rows []AllowlistMatch, path string) {
	for i := range rows {
		if PatternOverlapsFile(rows[i].Pattern, path) {
			rows[i].TrackedFiles++
		}
	}
}

type PlanResult struct {
	record   *ReleaseRecord
	contract *Contract
	diff     DiffResult
	root     string

	AllowlistMatches []AllowlistMatch `json:"allowlist_matches"`
	SchemaVersion    int              `json:"schema_version"`
	Command          string           `json:"command"`
	Status           string           `json:"status"`
	Mode             string           `json:"mode"`
	Backend          string           `json:"backend"`
	Subject          PlanSubject      `json:"subject"`
	Gates            []Gate           `json:"gates"`
	Verdict          string           `json:"verdict"`
	WouldBuild       []PlanBuild      `json:"would_build"`
	WouldPublish     []string         `json:"would_publish"`
	Network          []PlanRequest    `json:"network"`
	Advisories       []PlanAdvisory   `json:"advisories"`
	Boundary         CheckBoundary    `json:"boundary"`
	Error            *CheckError      `json:"error"`
}

type planSource interface {
	allowlistMatches([]string) ([]AllowlistMatch, error)
	contractSource
	pathEntry(string) (sourceEntry, int64, error)
	commitTime() (string, error)
	mainAncestry(string) (MainRef, Gate, error)
}

// Plan evaluates a commit's contract and displays data only. No build command is executed.
func Plan(ctx context.Context, o PlanOptions) (r PlanResult) {
	r = PlanResult{SchemaVersion: 1, Command: "release-plan", Status: "error", Mode: "build", Backend: "unknown", Subject: PlanSubject{Version: o.Version}, Gates: []Gate{}, Verdict: "incomplete", WouldBuild: []PlanBuild{}, WouldPublish: []string{}, Network: []PlanRequest{}, Advisories: []PlanAdvisory{{"tag-ruleset", "unknown", "offline"}}, Boundary: CheckBoundary{Effects: CheckEffects{Processes: "detection+reads"}, Limits: CheckLimits{MaxBytes: MaxContractBytes, MaxFindings: MaxFindings}}}
	if o.Mode != "" {
		r.Mode = o.Mode
	}
	defer func() { r = escapePlan(r) }()
	fail := func(code, message string) PlanResult {
		r.Status = "refused"
		r.Boundary.Complete = false
		r.AllowlistMatches = nil
		r.Error = &CheckError{code, message}
		return r
	}
	readFail := func(err error) PlanResult {
		if ctx.Err() != nil {
			return fail("canceled", "release plan canceled")
		}
		if errors.Is(err, errJJCommittedSymlinkInput) {
			return fail("unsupported-backend", err.Error())
		}
		if errors.Is(err, errReleaseLimit) || errors.Is(err, inspection.ErrJJReleaseLimit) {
			return fail("release-limit", err.Error())
		}
		r.Status = "error"
		r.Boundary.Complete = false
		r.AllowlistMatches = nil
		r.Error = &CheckError{"read-failed", err.Error()}
		return r
	}
	if ctx.Err() != nil {
		return readFail(ctx.Err())
	}
	root, backend, problem := detectOwner(ctx, o.Path, true)
	r.Backend = backend
	if problem != nil {
		return fail(problem.code, problem.message)
	}
	if backend != "git" && backend != "jj" {
		if !IsFullCommitID(o.Subject) {
			return fail("unsupported-backend", "tag planning requires a Git-owned checkout or the authoritative CI run; plan <commit> works in jj")
		}
		return fail("unsupported-backend", "release planning requires native Git or jj ownership")
	}
	if !IsFullCommitID(o.Subject) {
		if backend == "jj" {
			return fail("unsupported-backend", "tag planning requires a Git-owned checkout or the authoritative CI run; plan <commit> works in jj")
		}
		return evaluateTagPlan(ctx, o, root, r)
	}
	r.Subject.Commit = &o.Subject
	if o.Online {
		return fail("online-unavailable", "online release gates are not implemented in this source slice")
	}
	var source planSource
	if backend == "git" {
		g, err := newGitSource(ctx, root, o.Subject)
		if errors.Is(err, errCommitUnresolved) {
			return fail("commit-unresolved", err.Error())
		}
		if err != nil {
			return readFail(err)
		}
		source = g
	} else {
		err := inspection.WithJJReleaseTree(ctx, root, o.Subject, func(reader *inspection.JJReleaseReader) error {
			r = evaluateCommitPlan(ctx, o, root, jjSource{reader}, r)
			return nil
		})
		if errors.Is(err, inspection.ErrJJReleaseCommitUnresolved) {
			return fail("commit-unresolved", err.Error())
		}
		if err != nil {
			return readFail(err)
		}
		return r
	}
	return evaluateCommitPlan(ctx, o, root, source, r)
}

func evaluateCommitPlan(ctx context.Context, o PlanOptions, root string, source planSource, r PlanResult) PlanResult {
	fail := func(code, message string) PlanResult {
		r.Status = "refused"
		r.Boundary.Complete = false
		r.AllowlistMatches = nil
		r.Error = &CheckError{code, message}
		return r
	}
	readFail := func(err error) PlanResult {
		if ctx.Err() != nil {
			return fail("canceled", "release plan canceled")
		}
		if errors.Is(err, errJJCommittedSymlinkInput) {
			return fail("unsupported-backend", err.Error())
		}
		if errors.Is(err, errReleaseLimit) || errors.Is(err, inspection.ErrJJReleaseLimit) {
			return fail("release-limit", err.Error())
		}
		r.Status = "error"
		r.Boundary.Complete = false
		r.AllowlistMatches = nil
		r.Error = &CheckError{"read-failed", err.Error()}
		return r
	}
	resolved := hosting.Inspect(ctx, root, hosting.Options{Kind: "repo", Repo: o.Repo})
	if resolved.Status != "ok" || resolved.Repository == nil {
		return fail("remote-unresolved", resolved.Error())
	}
	remote := selectedMainRemote(resolved)
	if remote == "" {
		return fail("remote-unresolved", "no local remote matches the selected repository")
	}
	main, ancestry, err := source.mainAncestry(remote)
	if err != nil {
		return readFail(err)
	}
	r.Subject.MainRef = main
	r.Gates = append(r.Gates, ancestry)
	c, findings, code, err := planContract(source, o.Product)
	if err != nil {
		return readFail(err)
	}
	if code != "" {
		return fail(code, "cannot select a release contract at the evaluated commit")
	}
	if len(findings) > 0 {
		r.Gates = append(r.Gates, gate("config-invalid", "fail", "check-findings", findings[0].Code+": "+findings[0].Message))
		for _, code := range []string{"version-invalid", "version-file-mismatch", "readiness-missing"} {
			r.Gates = append(r.Gates, gate(code, "not-run", "config-invalid", ""))
		}
	} else {
		r.Gates = append(r.Gates, gate("config-invalid", "pass", "", ""))
		matches, err := source.allowlistMatches(c.Allowlist.Paths)
		if err != nil {
			return readFail(err)
		}
		r.AllowlistMatches = matches
		if c.Project.Product != "" {
			p := c.Project.Product
			r.Subject.Product = &p
		}
		scheme, err := CompileVersionScheme(c.Version)
		if err != nil {
			return readFail(err)
		}
		valid := scheme.Valid(o.Version)
		outcome := "pass"
		if !valid {
			outcome = "fail"
		}
		r.Gates = append(r.Gates, gate("version-invalid", outcome, "", ""))
		versionGate, err := versionFileGate(source, c, o.Version)
		if err != nil {
			return readFail(err)
		}
		r.Gates = append(r.Gates, versionGate)
		ready, err := readinessGate(source, c)
		if err != nil {
			return readFail(err)
		}
		r.Gates = append(r.Gates, ready)
		if valid {
			epoch, err := source.commitTime()
			if err != nil {
				return readFail(err)
			}
			r.WouldBuild = BuildInvocations(c, root, o.Subject, o.Version, epoch)
		}
	}
	for _, code := range []string{"version-not-monotonic", "release-exists", "ci-not-green"} {
		r.Gates = append(r.Gates, gate(code, "not-run", "needs-online", ""))
	}
	r.Status = "ok"
	r.Verdict = Verdict(r.Gates)
	r.Boundary.Complete = true
	return r
}

// selectedMainRemote names a local remote agreeing with hosting's selected repository.
// Prefer origin among equivalent aliases, then use lexical order for reproducibility.
func selectedMainRemote(r hosting.Result) string {
	names := map[string]bool{}
	for _, c := range r.Candidates {
		if strings.EqualFold(c.Host, r.Repository.Host) && strings.EqualFold(c.Owner, r.Repository.Owner) && strings.EqualFold(c.Name, r.Repository.Name) {
			names[c.Remote] = true
		}
	}
	if names["origin"] {
		return "origin"
	}
	var sorted []string
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	if len(sorted) > 0 {
		return sorted[0]
	}
	return ""
}

func planContract(source contractSource, product string) (*Contract, []Finding, string, error) {
	entries, cut, err := source.StwEntries()
	if err != nil {
		return nil, nil, "", err
	}
	if cut {
		return nil, nil, "", errReleaseLimit
	}
	names := []string{}
	byName := map[string]sourceEntry{}
	for _, e := range entries {
		names = append(names, e.Name)
		byName[e.Name] = e
	}
	files, findings := DiscoverProducts(names)
	selected, code := SelectProduct(files, product, true)
	if code != "" {
		return nil, nil, code, nil
	}
	if len(files) == 0 && len(findings) == 0 {
		return nil, nil, "config-missing", nil
	}
	if product != "" {
		findings = DiscoveryFindingsFor(findings, product)
	}
	if len(selected) == 0 {
		return nil, findings, "", nil
	}
	file := selected[0]
	c, _, ff, err := lintFile(source, file, byName[strings.TrimPrefix(file.Path, ".stw/")])
	findings = append(findings, ff...)
	errorsOnly := []Finding{}
	for _, f := range findings {
		if f.Code == "limit-exceeded" {
			return nil, nil, "", errReleaseLimit
		}
		if f.Severity == severityError {
			errorsOnly = append(errorsOnly, f)
		}
	}
	return c, errorsOnly, "", err
}

func versionFileGate(s planSource, c *Contract, version string) (Gate, error) {
	if c.Version.Source != "file" {
		return gate("version-file-mismatch", "not-applicable", "input-version", "version source is input"), nil
	}
	e, _, err := s.pathEntry(c.Version.File)
	if err != nil {
		return Gate{}, err
	}
	if e.Kind != "file" {
		return gate("version-file-mismatch", "fail", "version-file-absent", c.Version.File), nil
	}
	data, cut, err := s.Read(e, MaxContractBytes)
	if err != nil {
		return Gate{}, err
	}
	if cut {
		return Gate{}, errReleaseLimit
	}
	re, err := regexp.Compile(c.Version.FilePattern)
	if err != nil {
		return Gate{}, err
	}
	matches := re.FindAllSubmatch(data, -1)
	want := version
	if c.Version.FileForm == "without-v" {
		want = strings.TrimPrefix(version, "v")
	}
	outcome := "fail"
	if len(matches) == 1 && len(matches[0]) == 2 && string(matches[0][1]) == want {
		outcome = "pass"
	}
	return gate("version-file-mismatch", outcome, "", c.Version.File), nil
}

func readinessGate(s planSource, c *Contract) (Gate, error) {
	if c.Readiness == nil {
		return gate("readiness-missing", "not-applicable", "no-readiness", "no [readiness] table"), nil
	}
	missing := []string{}
	for _, p := range c.Readiness.Files {
		e, n, err := s.pathEntry(p)
		if err != nil {
			return Gate{}, err
		}
		if e.Kind != "file" || n == 0 {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return gate("readiness-missing", "fail", "not-nonempty-regular", strings.Join(missing, ", ")), nil
	}
	return gate("readiness-missing", "pass", "", ""), nil
}

// ExpandPlaceholders substitutes the closed, validated placeholder set in one pass.
// Inserted values are never recursively interpreted.
func ExpandPlaceholders(text string, values map[string]string) string {
	var b strings.Builder
	for len(text) > 0 {
		i := strings.IndexByte(text, '{')
		if i < 0 {
			b.WriteString(text)
			break
		}
		b.WriteString(text[:i])
		j := strings.IndexByte(text[i:], '}')
		if j < 0 {
			b.WriteString(text[i:])
			break
		}
		key := text[i+1 : i+j]
		value, ok := values[key]
		if !ok {
			b.WriteString(text[i : i+j+1])
		} else {
			b.WriteString(value)
		}
		text = text[i+j+1:]
	}
	return b.String()
}

// BuildInvocations describes argv; it neither starts processes nor creates output paths.
func BuildInvocations(c *Contract, root, commit, version, epoch string) []PlanBuild {
	out := []PlanBuild{}
	for _, b := range c.Build {
		p := PlanBuild{ID: b.ID, Scope: b.Scope, Network: b.Network, NetworkHosts: append([]string{}, b.NetworkHosts...), LockFiles: append([]string{}, b.LockFiles...), Invocations: []PlanInvocation{}}
		values := map[string]string{"version": version, "version_number": strings.TrimPrefix(version, "v"), "commit": commit, "commit12": commit[:min(12, len(commit))], "source_date_epoch": epoch, "out_dir": outputDirectory(root)}
		invoke := func(target *string) {
			argv := []string{}
			for _, a := range b.Argv {
				argv = append(argv, ExpandPlaceholders(a, values))
			}
			p.Invocations = append(p.Invocations, PlanInvocation{target, argv})
		}
		if b.Scope == "once" {
			invoke(nil)
		} else {
			for _, t := range c.Target {
				if t.Build != b.ID {
					continue
				}
				values["os"], values["arch"], values["target"] = t.OS, t.Arch, t.ID
				values["output"] = ExpandPlaceholders(t.Output, values)
				id := t.ID
				invoke(&id)
			}
		}
		out = append(out, p)
	}
	return out
}

func escapePlan(r PlanResult) PlanResult {
	r.Subject.Version = Escape(r.Subject.Version)
	for i := range r.AllowlistMatches {
		r.AllowlistMatches[i].Pattern = Escape(r.AllowlistMatches[i].Pattern)
	}
	for _, s := range []*string{r.Subject.Product, r.Subject.Commit, r.Subject.Tag, r.Subject.TagCommit, r.Subject.RecordCommit, r.Subject.MainRef.Commit} {
		if s != nil {
			*s = Escape(*s)
		}
	}
	r.Subject.MainRef.Remote = Escape(r.Subject.MainRef.Remote)
	for i := range r.Gates {
		if r.Gates[i].Reason != nil {
			*r.Gates[i].Reason = Escape(*r.Gates[i].Reason)
		}
		if r.Gates[i].Detail != nil {
			*r.Gates[i].Detail = Escape(*r.Gates[i].Detail)
		}
	}
	for i := range r.WouldBuild {
		b := &r.WouldBuild[i]
		b.ID = Escape(b.ID)
		for j := range b.NetworkHosts {
			b.NetworkHosts[j] = Escape(b.NetworkHosts[j])
		}
		for j := range b.LockFiles {
			b.LockFiles[j] = Escape(b.LockFiles[j])
		}
		for j := range b.Invocations {
			v := &b.Invocations[j]
			if v.Target != nil {
				*v.Target = Escape(*v.Target)
			}
			for k := range v.Argv {
				v.Argv[k] = Escape(v.Argv[k])
			}
		}
	}
	if r.Error != nil {
		r.Error.Message = Escape(r.Error.Message)
	}
	return r
}

func (r PlanResult) Text() string {
	var b strings.Builder
	subject := ""
	if r.Subject.Commit != nil {
		subject = *r.Subject.Commit
	}
	if r.Subject.Tag != nil {
		subject = *r.Subject.Tag
	}
	fmt.Fprintf(&b, "SourceSteward release plan: %s %s as %s (%s)\neffects: read-only; no project command executed; nothing built; network: none (offline)\n", r.Mode, subject, r.Subject.Version, r.Backend)
	b.WriteString("allowlist reach (tracked leaves; not publication eligibility)\n")
	if r.AllowlistMatches == nil {
		b.WriteString("  unavailable\n")
	} else {
		for _, row := range r.AllowlistMatches {
			fmt.Fprintf(&b, "  %q: %d tracked files\n", Escape(row.Pattern), row.TrackedFiles)
		}
	}
	if r.Error != nil {
		fmt.Fprintf(&b, "%s: %s: %s\n", r.Status, r.Error.Code, r.Error.Message)
		return b.String()
	}
	main := "absent"
	if r.Subject.MainRef.Commit != nil {
		main = *r.Subject.MainRef.Commit
	}
	fmt.Fprintf(&b, "main (%s) = %s, recorded locally, no fetch was made\n\ngates\n", r.Subject.MainRef.Remote, main)
	for _, g := range r.Gates {
		fmt.Fprintf(&b, "  %-15s %s", g.Outcome, g.Code)
		if g.Reason != nil {
			fmt.Fprintf(&b, "  %s", *g.Reason)
		}
		if g.Detail != nil {
			fmt.Fprintf(&b, "  %s", *g.Detail)
		}
		b.WriteByte('\n')
	}
	b.WriteString("\nwould build (nothing executed; argv is data)\n")
	for _, p := range r.WouldBuild {
		fmt.Fprintf(&b, "  build %s (%s); network: %s; hosts: %q; lock files: %q\n", p.ID, p.Scope, p.Network, p.NetworkHosts, p.LockFiles)
		for _, v := range p.Invocations {
			fmt.Fprintf(&b, "    argv: %q\n", v.Argv)
		}
	}
	for _, a := range r.Advisories {
		fmt.Fprintf(&b, "advisory %s: %s (%s)\n", a.Code, a.State, a.Detail)
	}
	fmt.Fprintf(&b, "verdict: %s\n", r.Verdict)
	return b.String()
}
