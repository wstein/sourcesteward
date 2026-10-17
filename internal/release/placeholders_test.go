// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"strings"
	"testing"
)

// The per-field placeholder table of the spec, written out independently of the implementation.
func TestPlaceholderFieldTable(t *testing.T) {
	universe := strings.Fields("version version_number commit commit12 source_date_epoch os arch target output out_dir artifact repository tag name digest")
	buildOnce := strings.Fields("version version_number commit commit12 source_date_epoch out_dir")
	buildPerTarget := append(append([]string{}, buildOnce...), "os", "arch", "target", "output")
	fields := []struct {
		name    string
		example string
		path    []any
		wrap    func(string) string
		allowed []string
	}{
		{"build.argv per-target", "go-cli", []any{"build", 0, "argv", 2}, nil, buildPerTarget},
		{"build.argv once", "skill-pack", []any{"build", 0, "argv", 1}, nil, buildOnce},
		{"build.env_set per-target", "go-cli", []any{"build", 0, "env_set", 0}, func(p string) string { return "A=" + p }, buildPerTarget},
		{"build.env_set once", "skill-pack", []any{"build", 0, "env_set"}, nil, buildOnce},
		{"target.output", "go-cli", []any{"target", 0, "output"}, func(p string) string { return "dist/release/" + p }, strings.Fields("version version_number commit12 os arch target")},
		{"target.result_file", "oci-bundle", []any{"target", 0, "result_file"}, func(p string) string { return "dist/release/" + p }, strings.Fields("version version_number commit12 os arch target")},
		{"target.staging_tag", "oci-bundle", []any{"target", 0, "staging_tag"}, nil, strings.Fields("commit commit12")},
		{"target.smoke_argv", "go-cli", []any{"target", 0, "smoke_argv", 1}, nil, strings.Fields("version version_number commit os arch target artifact out_dir")},
		{"promote.argv", "oci-bundle", []any{"promote", "argv", 0}, nil, strings.Fields("repository digest version version_number")},
		{"outputs.url_template", "go-cli", []any{"outputs", "url_template"}, nil, strings.Fields("repository tag name")},
	}
	for _, f := range fields {
		allowed := map[string]bool{}
		for _, name := range f.allowed {
			allowed[name] = true
		}
		for _, name := range universe {
			value := "x{" + name + "}y"
			if f.wrap != nil {
				value = f.wrap(value)
			}
			path := f.path
			mutate := set(value, path...)
			if f.name == "build.env_set once" {
				mutate = set(strs("A=x{"+name+"}y"), path...)
				path = append(append([]any{}, path...), 0)
			}
			_, got := lint(t, f.example, nil, mutate)
			pointer := ""
			for _, p := range path {
				switch k := p.(type) {
				case string:
					pointer += "/" + k
				case int:
					pointer += "/" + itoa(k)
				}
			}
			flagged := false
			for _, finding := range got {
				flagged = flagged || finding == "placeholder-unknown "+pointer
			}
			if flagged == allowed[name] {
				t.Errorf("%s: {%s} flagged=%v, allowed=%v (%v)", f.name, name, flagged, allowed[name], got)
			}
		}
	}
}
