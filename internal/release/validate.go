// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// TreeEntry is what exists at a path of the linted tree: Kind is "file", "dir" or "symlink"
// (with its Target).
type TreeEntry struct {
	Kind   string
	Target string
}

// Tree answers which entries exist in the tree the contract is linted against: the working tree
// for the working-tree form and the commit's tree for --commit. Entries covers exactly the given
// paths, without following a symlink at the last component, and omits absent paths.
type Tree interface {
	Entries(paths []string) (map[string]TreeEntry, error)
}

// ValidateOptions selects the product file being validated and the tree for declared inputs.
type ValidateOptions struct {
	// Product is the product id taken from the file name; "" is the default product (release.toml).
	Product string
	// Tree is optional; without it the allowlist is checked against declared inputs only.
	Tree Tree
}

var alias = map[string]bool{"latest": true, "stable": true, "edge": true, "main": true, "master": true}

var argvFields = []string{"argv", "smoke_argv"}

// ValidateContract applies the schema and every rule beyond it to a decoded contract document
// and returns the normalized contract when no error finding exists. Findings are ordered by
// JSON pointer, at most one per code and pointer, and cut at MaxFindings.
func ValidateContract(doc any, opts ValidateOptions) (*Contract, []Finding, error) {
	findings := validateConfigSchema(doc)
	root, ok := doc.(map[string]any)
	if !ok {
		return nil, finishFindings(findings), nil
	}
	findings = checkArgvArrays(root, findings)
	c := modelFrom(root)
	v := &validator{c: c, root: root, opts: opts}
	v.checkProduct()
	v.checkReferences()
	v.checkPlaceholders()
	v.checkEnvironment()
	v.checkPaths()
	v.checkFetch()
	v.checkPromote()
	v.out = append(v.out, checkVersion(c.Version)...)
	if err := v.checkAllowlist(); err != nil {
		return nil, nil, err
	}
	findings = finishFindings(append(findings, v.out...))
	for _, f := range findings {
		if f.Severity == severityError {
			return nil, findings, nil
		}
	}
	c.applyDefaults()
	return c, findings, nil
}

// finishFindings keeps the first finding per code and pointer, orders by pointer and cuts the list.
func finishFindings(findings []Finding) []Finding {
	seen := map[string]bool{}
	unique := findings[:0:0]
	for _, f := range findings {
		key := f.Code + "\x00" + f.Pointer
		if !seen[key] {
			seen[key] = true
			unique = append(unique, f)
		}
	}
	sortFindings(unique)
	if len(unique) > MaxFindings {
		dropped := len(unique) - MaxFindings
		unique = append(unique[:MaxFindings:MaxFindings], errorFinding("limit-exceeded", "", fmt.Sprintf("%d further findings are not shown", dropped)))
	}
	return unique
}

// checkArgvArrays reports a shell string, a non-array or an empty array where an argument vector
// is required, and drops the schema findings those values already produced at the same pointer.
func checkArgvArrays(root map[string]any, findings []Finding) []Finding {
	var pointers []string
	test := func(pointer string, value any, present bool) {
		if !present {
			return
		}
		if list, ok := value.([]any); !ok || len(list) == 0 {
			pointers = append(pointers, pointer)
		}
	}
	builds, _ := root["build"].([]any)
	for i, b := range builds {
		if m := asMap(b); m != nil {
			value, present := m["argv"]
			test(fmt.Sprintf("/build/%d/argv", i), value, present)
		}
	}
	targets, _ := root["target"].([]any)
	for i, t := range targets {
		if m := asMap(t); m != nil {
			value, present := m["smoke_argv"]
			test(fmt.Sprintf("/target/%d/smoke_argv", i), value, present)
		}
	}
	if promote := asMap(root["promote"]); promote != nil {
		value, present := promote["argv"]
		test("/promote/argv", value, present)
	}
	if len(pointers) == 0 {
		return findings
	}
	flagged := map[string]bool{}
	for _, p := range pointers {
		flagged[p] = true
	}
	kept := findings[:0:0]
	for _, f := range findings {
		if !flagged[f.Pointer] {
			kept = append(kept, f)
		}
	}
	for _, p := range pointers {
		kept = append(kept, errorFinding("argv-not-array", p, "an argument vector must be a non-empty array of strings; a shell string is never accepted"))
	}
	return kept
}

type validator struct {
	c    *Contract
	root map[string]any
	opts ValidateOptions
	out  []Finding
}

func (v *validator) add(code, pointer, format string, args ...any) {
	v.out = append(v.out, errorFinding(code, pointer, fmt.Sprintf(format, args...)))
}

func (v *validator) info(code, pointer, format string, args ...any) {
	f := errorFinding(code, pointer, fmt.Sprintf(format, args...))
	f.Severity = severityInfo
	v.out = append(v.out, f)
}

func (v *validator) checkProduct() {
	declared := v.c.Project.Product
	switch {
	case v.opts.Product == "" && declared != "":
		v.add("schema-violation", "/project/product", "project.product is not allowed in .stw/release.toml; a product file is .stw/release.<product>.toml")
	case v.opts.Product != "" && declared == "":
		v.add("schema-violation", "/project", "project.product is required in .stw/release.%s.* and must equal %q", clip(v.opts.Product), clip(v.opts.Product))
	case v.opts.Product != "" && declared != v.opts.Product:
		v.add("schema-violation", "/project/product", "project.product %q must equal the product %q named by the file", clip(declared), clip(v.opts.Product))
	}
}

func (v *validator) checkReferences() {
	builds := map[string]bool{}
	for i, b := range v.c.Build {
		if b.ID == "" {
			continue
		}
		if builds[b.ID] {
			v.add("duplicate-id", fmt.Sprintf("/build/%d/id", i), "build id %q is used more than once", clip(b.ID))
		}
		builds[b.ID] = true
	}
	targets := map[string]bool{}
	referenced := map[string]bool{}
	for i, t := range v.c.Target {
		if t.ID != "" {
			if targets[t.ID] {
				v.add("duplicate-id", fmt.Sprintf("/target/%d/id", i), "target id %q is used more than once", clip(t.ID))
			}
			targets[t.ID] = true
		}
		if t.Build == "" {
			continue
		}
		referenced[t.Build] = true
		if !builds[t.Build] {
			v.add("unknown-reference", fmt.Sprintf("/target/%d/build", i), "target %q names the build %q, which does not exist", clip(t.ID), clip(t.Build))
		}
	}
	for i, b := range v.c.Build {
		if b.ID != "" && !referenced[b.ID] {
			v.info("unused-build", fmt.Sprintf("/build/%d", i), "no target uses the build %q", clip(b.ID))
		}
	}
}

func (v *validator) checkPlaceholders() {
	check := func(pointer, value string, allowed map[string]bool, field string) {
		if problem := placeholderProblem(value, allowed); problem != "" {
			v.add("placeholder-unknown", pointer, "%s: %s", field, problem)
		}
	}
	for i, b := range v.c.Build {
		allowed := placeholdersBuild
		if b.Scope == "per-target" {
			allowed = placeholdersBuildPerTarget
		}
		for j, arg := range b.Argv {
			check(fmt.Sprintf("/build/%d/argv/%d", i, j), arg, allowed, "build.argv")
		}
		for j, entry := range b.EnvSet {
			_, value, _ := strings.Cut(entry, "=")
			check(fmt.Sprintf("/build/%d/env_set/%d", i, j), value, allowed, "build.env_set")
		}
	}
	for i, t := range v.c.Target {
		base := fmt.Sprintf("/target/%d", i)
		check(base+"/output", t.Output, placeholdersOutput, "target.output")
		check(base+"/result_file", t.ResultFile, placeholdersOutput, "target.result_file")
		check(base+"/staging_tag", t.StagingTag, placeholdersStagingTag, "target.staging_tag")
		for j, arg := range t.SmokeArgv {
			check(fmt.Sprintf("%s/smoke_argv/%d", base, j), arg, placeholdersSmoke, "target.smoke_argv")
		}
	}
	if v.c.Promote != nil {
		for j, arg := range v.c.Promote.Argv {
			check(fmt.Sprintf("/promote/argv/%d", j), arg, placeholdersPromote, "promote.argv")
		}
	}
	check("/outputs/url_template", v.c.Outputs.URLTemplate, placeholdersURL, "outputs.url_template")
}

// forbiddenEnvironmentName implements the build environment table: credential-like names and the
// names stw sets itself.
func forbiddenEnvironmentName(name string) string {
	upper := strings.ToUpper(name)
	// SSH_AUTH_SOCK is covered by the AUTH rule below.
	switch upper {
	case "SOURCE_DATE_EPOCH", "TZ", "LC_ALL":
		return "is set by stw itself"
	}
	for _, prefix := range []string{"ACTIONS_", "GITHUB_"} {
		if strings.HasPrefix(upper, prefix) {
			return "belongs to the CI runner"
		}
	}
	for _, word := range []string{"TOKEN", "SECRET", "KEY", "PASSWORD", "AUTH", "CREDENTIAL"} {
		if strings.Contains(upper, word) {
			return "looks like a credential"
		}
	}
	return ""
}

func (v *validator) checkEnvironment() {
	for i, b := range v.c.Build {
		for j, name := range b.EnvInherit {
			if why := forbiddenEnvironmentName(name); why != "" {
				v.add("env-name-forbidden", fmt.Sprintf("/build/%d/env_inherit/%d", i, j), "the environment name %q %s", clip(name), why)
			}
		}
		for j, entry := range b.EnvSet {
			name, _, _ := strings.Cut(entry, "=")
			if why := forbiddenEnvironmentName(name); why != "" {
				v.add("env-name-forbidden", fmt.Sprintf("/build/%d/env_set/%d", i, j), "the environment name %q %s", clip(name), why)
			}
		}
	}
}

// pathProblem checks the confinement of a repository path or path template.
func pathProblem(p string, allowDot bool) string {
	if p == "." && allowDot {
		return ""
	}
	// A leading slash (an absolute path) shows up as an empty first segment.
	for _, segment := range strings.Split(p, "/") {
		switch segment {
		case "":
			return "an empty path segment (or a leading or trailing slash) is not allowed"
		case "..":
			return "a .. path segment is not allowed"
		case ".":
			return "a . path segment is not allowed; write the normalized path"
		}
	}
	return ""
}

func (v *validator) checkPaths() {
	check := func(pointer, p, field string, allowDot bool) {
		if p == "" {
			return
		}
		if problem := pathProblem(p, allowDot); problem != "" {
			v.add("schema-violation", pointer, "path-confined: %s: %s", field, problem)
		}
	}
	check("/version/file", v.c.Version.File, "version.file", false)
	check("/ci/release_workflow", v.c.CI.ReleaseWorkflow, "ci.release_workflow", false)
	for i, b := range v.c.Build {
		check(fmt.Sprintf("/build/%d/working_dir", i), b.WorkingDir, "build.working_dir", true)
		for j, p := range b.LockFiles {
			check(fmt.Sprintf("/build/%d/lock_files/%d", i, j), p, "build.lock_files", false)
		}
	}
	if v.c.Readiness != nil {
		for j, p := range v.c.Readiness.Files {
			check(fmt.Sprintf("/readiness/files/%d", j), p, "readiness.files", false)
		}
	}
	const outDir = "dist/release/"
	for i, t := range v.c.Target {
		base := fmt.Sprintf("/target/%d", i)
		check(base+"/lock_file", t.LockFile, "target.lock_file", false)
		check(base+"/tool_list_file", t.ToolListFile, "target.tool_list_file", false)
		for _, field := range []struct{ name, value string }{{"output", t.Output}, {"result_file", t.ResultFile}} {
			if field.value == "" {
				continue
			}
			check(base+"/"+field.name, field.value, "target."+field.name, false)
			if !strings.HasPrefix(field.value, outDir) {
				v.add("schema-violation", base+"/"+field.name, "output-outside-out-dir: target.%s must be a path under %s", field.name, outDir)
			}
		}
	}
}

func (v *validator) checkFetch() {
	if v.c.Reproducibility.Policy != "required" {
		return
	}
	for i, b := range v.c.Build {
		if b.Network == "fetch" && len(b.LockFiles) == 0 {
			v.add("fetch-unpinned", fmt.Sprintf("/build/%d", i), "the fetch build %q needs lock_files when reproducibility is required", clip(b.ID))
		}
	}
}

func (v *validator) checkPromote() {
	hasImage := false
	for _, t := range v.c.Target {
		hasImage = hasImage || t.Kind == "oci-image"
	}
	_, hasPromote := v.root["promote"]
	switch {
	case hasImage && !hasPromote:
		v.add("schema-violation", "/promote", "a [promote] table is required when an oci-image target exists")
	case !hasImage && hasPromote:
		v.add("schema-violation", "/promote", "[promote] requires an oci-image target")
	}
	if v.c.Promote == nil || !hasImage {
		return
	}
	if list, ok := asMap(v.root["promote"])["argv"].([]any); !ok || len(list) == 0 {
		return // reported as argv-not-array
	}
	digest, version := false, false
	for j, arg := range v.c.Promote.Argv {
		digest = digest || strings.Contains(arg, "{digest}")
		version = version || strings.Contains(arg, "{version}") || strings.Contains(arg, "{version_number}")
		if alias[arg] {
			v.add("promote-argv-invalid", fmt.Sprintf("/promote/argv/%d", j), "promote.argv must not contain the moving alias %q; aliases are a separate step outside the release", arg)
		}
	}
	if !digest || !version {
		v.add("promote-argv-invalid", "/promote/argv", "promote.argv must contain {digest} and {version} or {version_number}")
	}
}

// declaredInput is a repository path a build reads, which an allowlist pattern must not cover.
type declaredInput struct {
	path string
	dir  bool
	what string
}

func (v *validator) declaredInputs() ([]declaredInput, []string) {
	inputs := []declaredInput{{".stw", true, "the release contract directory"}, {".github", true, "the workflow directory"}}
	file := func(p, what string) {
		if p != "" && ValidRepoPath(p) {
			inputs = append(inputs, declaredInput{p, false, what})
		}
	}
	if v.c.Version.Source == "file" {
		file(v.c.Version.File, "the version file")
	}
	file(v.c.CI.ReleaseWorkflow, "the release workflow")
	if v.c.Readiness != nil {
		for _, p := range v.c.Readiness.Files {
			file(p, "a readiness file")
		}
	}
	candidates := map[string]bool{}
	for _, b := range v.c.Build {
		for _, p := range b.LockFiles {
			file(p, "a build lock file")
		}
		dir := b.WorkingDir
		if dir != "" && dir != "." && ValidRepoPath(dir) {
			segments := strings.Split(dir, "/")
			for n := 1; n < len(segments); n++ {
				inputs = append(inputs, declaredInput{strings.Join(segments[:n], "/"), false, "a prefix of the working directory " + dir})
			}
			inputs = append(inputs, declaredInput{dir, true, "the working directory"})
		}
		for _, arg := range b.Argv {
			addArgvCandidates(candidates, arg, dir)
		}
	}
	for _, t := range v.c.Target {
		file(t.LockFile, "the image lock file")
		file(t.ToolListFile, "the image tool list")
		for _, arg := range t.SmokeArgv {
			addArgvCandidates(candidates, arg, "")
		}
	}
	paths := make([]string, 0, len(candidates))
	for p := range candidates {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return inputs, paths
}

// addArgvCandidates records the repository paths an argument might name, lexically cleaned and
// confined to the repository: as written and relative to the working directory.
func addArgvCandidates(into map[string]bool, arg, workingDir string) {
	if strings.ContainsAny(arg, "{}") || strings.HasPrefix(arg, "/") {
		return
	}
	add := func(p string) {
		if p = path.Clean(p); ValidRepoPath(p) {
			into[p] = true
		}
	}
	add(arg)
	if workingDir != "" && workingDir != "." {
		add(path.Join(workingDir, arg))
	}
}

const maxSymlinkHops = 16

// pathWalk follows one candidate path component by component through the tree.
type pathWalk struct {
	comps []string
	idx   int
	cur   string
	hops  int
	done  bool
}

// resolveCandidates finds, for each candidate, the repository path it denotes after symlinks
// inside the repository are followed. Every symlink met is an input, and so is the final target.
// A symlink that leaves the repository, is absolute, loops or is nested too deep cannot be
// resolved and fails closed: the second result is true and every pattern is then reported.
func resolveCandidates(tree Tree, candidates []string) (found []declaredInput, failClosed bool, err error) {
	walks := make([]*pathWalk, len(candidates))
	for i, c := range candidates {
		walks[i] = &pathWalk{comps: strings.Split(c, "/")}
	}
	cache := map[string]*TreeEntry{}
	for {
		var need []string
		seen := map[string]bool{}
		for _, w := range walks {
			if w.done {
				continue
			}
			p := path.Join(w.cur, w.comps[w.idx])
			if _, ok := cache[p]; !ok && !seen[p] {
				seen[p] = true
				need = append(need, p)
			}
		}
		if len(need) == 0 && allDone(walks) {
			return found, false, nil
		}
		sort.Strings(need)
		if len(need) > 0 {
			entries, err := tree.Entries(need)
			if err != nil {
				return nil, false, err
			}
			for _, p := range need {
				if e, ok := entries[p]; ok {
					cache[p] = &e
				} else {
					cache[p] = nil
				}
			}
		}
		for _, w := range walks {
			if w.done {
				continue
			}
			p := path.Join(w.cur, w.comps[w.idx])
			e := cache[p]
			last := w.idx == len(w.comps)-1
			switch {
			case e == nil:
				w.done = true
			case e.Kind == "symlink":
				found = append(found, declaredInput{p, false, "a symlink on the way to a path named in an argument vector"})
				target := e.Target
				w.hops++
				resolved := path.Join(path.Dir(p), target)
				if w.hops > maxSymlinkHops || strings.HasPrefix(target, "/") || resolved == ".." || strings.HasPrefix(resolved, "../") || resolved == "." {
					return found, true, nil
				}
				w.comps = append(strings.Split(resolved, "/"), w.comps[w.idx+1:]...)
				w.idx, w.cur = 0, ""
			case e.Kind == "dir":
				w.cur = p
				w.idx++
				if last {
					found = append(found, declaredInput{p, true, "the repository path " + p + " named in an argument vector"})
					w.done = true
				}
			default:
				if last {
					found = append(found, declaredInput{p, false, "the repository path " + p + " named in an argument vector"})
				}
				w.done = true
			}
		}
	}
}

func allDone(walks []*pathWalk) bool {
	for _, w := range walks {
		if !w.done {
			return false
		}
	}
	return true
}

func (v *validator) checkAllowlist() error {
	inputs, candidates := v.declaredInputs()
	failClosed := false
	if v.opts.Tree != nil && len(candidates) > 0 {
		found, closed, err := resolveCandidates(v.opts.Tree, candidates)
		if err != nil {
			return err
		}
		inputs, failClosed = append(inputs, found...), closed
	}
	for i, pattern := range v.c.Allowlist.Paths {
		pointer := fmt.Sprintf("/allowlist/paths/%d", i)
		if problem := PatternProblem(pattern); problem != "" {
			v.add("schema-violation", pointer, "allowlist-pattern: %s", problem)
			continue
		}
		if failClosed {
			v.add("allowlist-overlaps-input", pointer, "pattern %q cannot be proved apart from the build inputs: a symlink named in an argument vector leaves the repository, is absolute, loops or is nested too deep", clip(pattern))
			continue
		}
		for _, in := range inputs {
			overlap := PatternOverlapsFile(pattern, in.path)
			if in.dir {
				overlap = PatternOverlapsDirectory(pattern, in.path)
			}
			if overlap {
				v.add("allowlist-overlaps-input", pointer, "pattern %q matches the build input %q (%s)", clip(pattern), clip(in.path), in.what)
				break
			}
		}
	}
	return nil
}
