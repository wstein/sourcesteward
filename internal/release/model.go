// SPDX-License-Identifier: EUPL-1.2
package release

import "strings"

// Contract is the validated model of a release contract (data-model.md). The JSON names are the
// schema's, so a contract marshals to a document the schema accepts, with defaults filled in.
type Contract struct {
	SchemaVersion   int             `json:"schema_version"`
	Project         Project         `json:"project"`
	Version         Version         `json:"version"`
	Build           []Build         `json:"build"`
	Target          []Target        `json:"target"`
	Outputs         Outputs         `json:"outputs"`
	Reproducibility Reproducibility `json:"reproducibility"`
	Allowlist       Allowlist       `json:"allowlist"`
	CI              CI              `json:"ci"`
	Readiness       *Readiness      `json:"readiness,omitempty"`
	Promote         *Promote        `json:"promote,omitempty"`
}

type Project struct {
	Name    string `json:"name"`
	Product string `json:"product,omitempty"`
}

type Version struct {
	Scheme       string   `json:"scheme"`
	TagPrefix    string   `json:"tag_prefix,omitempty"`
	TagTemplate  string   `json:"tag_template,omitempty"`
	TagPattern   string   `json:"tag_pattern,omitempty"`
	Monotonicity string   `json:"monotonicity,omitempty"`
	Order        []string `json:"order,omitempty"`
	Source       string   `json:"source"`
	File         string   `json:"file,omitempty"`
	FilePattern  string   `json:"file_pattern,omitempty"`
	FileForm     string   `json:"file_form,omitempty"`
}

type Build struct {
	ID             string   `json:"id"`
	Scope          string   `json:"scope"`
	Argv           []string `json:"argv"`
	EnvInherit     []string `json:"env_inherit"`
	EnvSet         []string `json:"env_set,omitempty"`
	WorkingDir     string   `json:"working_dir"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Network        string   `json:"network"`
	NetworkHosts   []string `json:"network_hosts,omitempty"`
	LockFiles      []string `json:"lock_files,omitempty"`
}

type Target struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Role         string   `json:"role"`
	Group        string   `json:"group,omitempty"`
	Build        string   `json:"build"`
	OS           string   `json:"os,omitempty"`
	Arch         string   `json:"arch,omitempty"`
	Output       string   `json:"output,omitempty"`
	Runner       string   `json:"runner,omitempty"`
	SmokeArgv    []string `json:"smoke_argv,omitempty"`
	Repository   string   `json:"repository,omitempty"`
	Platforms    []string `json:"platforms,omitempty"`
	StagingTag   string   `json:"staging_tag,omitempty"`
	ResultFile   string   `json:"result_file,omitempty"`
	LockFile     string   `json:"lock_file,omitempty"`
	ToolListFile string   `json:"tool_list_file,omitempty"`
	SourceOffer  string   `json:"source_offer,omitempty"`
}

type Outputs struct {
	Checksums   string `json:"checksums"`
	URLTemplate string `json:"url_template"`
}

type Reproducibility struct {
	Policy          string `json:"policy"`
	SourceDateEpoch string `json:"source_date_epoch"`
}

type Allowlist struct {
	Paths []string `json:"paths"`
}

type CI struct {
	Workflows       []string `json:"workflows"`
	ReleaseWorkflow string   `json:"release_workflow"`
}

type Readiness struct {
	Files []string `json:"files"`
}

type Promote struct {
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// Defaults of the contract (config.md).
const (
	defaultChecksums   = "SHA256SUMS"
	defaultURLTemplate = "https://github.com/{repository}/releases/download/{tag}/{name}"
	defaultTimeout     = 1800
)

// tolerant accessors: the model is built from a document that may still violate the schema, so
// a value of the wrong type reads as absent and the schema finding reports it.

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func getString(m map[string]any, key string) string { s, _ := m[key].(string); return s }

func getInt(m map[string]any, key string) int {
	switch n := m[key].(type) {
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func getStrings(m map[string]any, key string) []string {
	list, _ := m[key].([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

// getTables returns the entries of an array of tables; an entry of another type is an empty table,
// so indexes stay aligned with the document.
func getTables(m map[string]any, key string) []map[string]any {
	list, _ := m[key].([]any)
	out := make([]map[string]any, len(list))
	for i, item := range list {
		out[i] = asMap(item)
		if out[i] == nil {
			out[i] = map[string]any{}
		}
	}
	return out
}

// modelFrom reads a contract document; it never fails and is correct only for a document that
// passes the schema.
func modelFrom(doc map[string]any) *Contract {
	c := &Contract{SchemaVersion: getInt(doc, "schema_version")}
	project, version := asMap(doc["project"]), asMap(doc["version"])
	c.Project = Project{Name: getString(project, "name"), Product: getString(project, "product")}
	c.Version = Version{
		Scheme: getString(version, "scheme"), TagPrefix: getString(version, "tag_prefix"), TagTemplate: getString(version, "tag_template"),
		TagPattern: getString(version, "tag_pattern"), Monotonicity: getString(version, "monotonicity"), Order: getStrings(version, "order"),
		Source: getString(version, "source"), File: getString(version, "file"), FilePattern: getString(version, "file_pattern"), FileForm: getString(version, "file_form"),
	}
	if len(c.Version.Order) == 0 {
		c.Version.Order = nil
	}
	for _, b := range getTables(doc, "build") {
		build := Build{
			ID: getString(b, "id"), Scope: getString(b, "scope"), Argv: getStrings(b, "argv"), EnvInherit: getStrings(b, "env_inherit"),
			EnvSet: getStrings(b, "env_set"), WorkingDir: getString(b, "working_dir"), TimeoutSeconds: getInt(b, "timeout_seconds"),
			Network: getString(b, "network"), NetworkHosts: getStrings(b, "network_hosts"), LockFiles: getStrings(b, "lock_files"),
		}
		if _, present := b["env_inherit"]; !present {
			build.EnvInherit = []string{"PATH", "HOME", "TMPDIR"}
		}
		if len(build.EnvSet) == 0 {
			build.EnvSet = nil
		}
		if len(build.NetworkHosts) == 0 {
			build.NetworkHosts = nil
		}
		if len(build.LockFiles) == 0 {
			build.LockFiles = nil
		}
		c.Build = append(c.Build, build)
	}
	for _, t := range getTables(doc, "target") {
		target := Target{
			ID: getString(t, "id"), Kind: getString(t, "kind"), Role: getString(t, "role"), Group: getString(t, "group"), Build: getString(t, "build"),
			OS: getString(t, "os"), Arch: getString(t, "arch"), Output: getString(t, "output"), Runner: getString(t, "runner"), SmokeArgv: getStrings(t, "smoke_argv"),
			Repository: getString(t, "repository"), Platforms: getStrings(t, "platforms"), StagingTag: getString(t, "staging_tag"), ResultFile: getString(t, "result_file"),
			LockFile: getString(t, "lock_file"), ToolListFile: getString(t, "tool_list_file"), SourceOffer: getString(t, "source_offer"),
		}
		if len(target.SmokeArgv) == 0 {
			target.SmokeArgv = nil
		}
		if len(target.Platforms) == 0 {
			target.Platforms = nil
		}
		c.Target = append(c.Target, target)
	}
	outputs, repro, allow, ci := asMap(doc["outputs"]), asMap(doc["reproducibility"]), asMap(doc["allowlist"]), asMap(doc["ci"])
	c.Outputs = Outputs{Checksums: getString(outputs, "checksums"), URLTemplate: getString(outputs, "url_template")}
	c.Reproducibility = Reproducibility{Policy: getString(repro, "policy"), SourceDateEpoch: getString(repro, "source_date_epoch")}
	c.Allowlist = Allowlist{Paths: getStrings(allow, "paths")}
	c.CI = CI{Workflows: getStrings(ci, "workflows"), ReleaseWorkflow: getString(ci, "release_workflow")}
	if readiness := asMap(doc["readiness"]); readiness != nil {
		c.Readiness = &Readiness{Files: getStrings(readiness, "files")}
	}
	if promote := asMap(doc["promote"]); promote != nil {
		c.Promote = &Promote{Argv: getStrings(promote, "argv"), TimeoutSeconds: getInt(promote, "timeout_seconds")}
	}
	return c
}

// applyDefaults fills the documented defaults of a valid contract.
func (c *Contract) applyDefaults() {
	if c.Version.Scheme == "pattern" && c.Version.Monotonicity == "" {
		c.Version.Monotonicity = "strict"
	}
	for i := range c.Build {
		b := &c.Build[i]
		if b.WorkingDir == "" {
			b.WorkingDir = "."
		}
		if b.TimeoutSeconds == 0 {
			b.TimeoutSeconds = defaultTimeout
		}
		if b.Network == "" {
			b.Network = "none"
		}
	}
	for i := range c.Target {
		if c.Target[i].Role == "" {
			c.Target[i].Role = "artifact"
		}
	}
	if c.Outputs.Checksums == "" {
		c.Outputs.Checksums = defaultChecksums
	}
	if c.Outputs.URLTemplate == "" {
		c.Outputs.URLTemplate = defaultURLTemplate
	}
}

// TagPrefixLiteral returns the literal text every tag of the product starts with: the tag_prefix
// plus the fixed leading "v" of a semver version, or the literal prefix of the template. The
// second result is false when the contract is too broken to tell.
func (c *Contract) TagPrefixLiteral() (string, bool) {
	switch c.Version.Scheme {
	case "semver":
		return c.Version.TagPrefix + "v", true
	case "calver":
		return c.Version.TagPrefix, true
	case "pattern":
		prefix, _, ok := splitTemplate(c.Version.TagTemplate)
		return prefix, ok
	}
	return "", false
}

// splitTemplate splits a tag template with exactly one {version} into its literal prefix and suffix.
func splitTemplate(template string) (prefix, suffix string, ok bool) {
	prefix, suffix, found := strings.Cut(template, "{version}")
	if !found || strings.ContainsAny(prefix+suffix, "{}") {
		return "", "", false
	}
	return prefix, suffix, true
}
