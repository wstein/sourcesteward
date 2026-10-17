// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"strings"
	"testing"
)

// replaceIn edits the string at path with strings.Replace(old, new, 1).
func replaceIn(old, new string, path ...any) func(*testing.T, map[string]any) {
	return func(t *testing.T, doc map[string]any) {
		current := navigate(t, doc, path...).(string)
		if !strings.Contains(current, old) {
			t.Fatalf("%q does not contain %q", current, old)
		}
		setAt(t, doc, strings.Replace(current, old, new, 1), path...)
	}
}

const zlibPattern = `^zlib-v(?P<version>(?P<major>[0-9]{1,9})\.(?P<minor>[0-9]{1,9})(?:\.(?P<patch>[0-9]{1,9}))?)$`

func zlibWith(pattern string, extra ...func(*testing.T, map[string]any)) func(*testing.T, map[string]any) {
	return both(append([]func(*testing.T, map[string]any){set(pattern, "version", "tag_pattern")}, extra...)...)
}

func schemeCases() []semanticCase {
	tp := "/version/tag_pattern"
	return []semanticCase{
		{name: "tag_template without version", example: "oci-bundle", mutate: set("toolbox/", "version", "tag_template"), want: []string{"scheme-invalid /version/tag_template"}, msg: "exactly once"},
		{name: "tag_template with two versions", example: "oci-bundle", mutate: set("toolbox/{version}{version}", "version", "tag_template"), want: []string{"scheme-invalid /version/tag_template"}, msg: "exactly once"},
		{name: "tag_template with another placeholder", example: "oci-bundle", mutate: set("toolbox/{version}/{x}", "version", "tag_template"), want: []string{"scheme-invalid /version/tag_template"}, msg: "exactly once"},
		{name: "tag_template with empty prefix", example: "oci-bundle", mutate: set("{version}", "version", "tag_template"), want: []string{"scheme-invalid " + tp}},
		{name: "tag_pattern does not compile", example: "mirror-zlib", mutate: set("(", "version", "tag_pattern"), want: []string{"scheme-invalid " + tp}, msg: "does not compile"},
		{name: "tag_pattern is not anchored at the start", example: "mirror-zlib", mutate: zlibWith(strings.TrimPrefix(zlibPattern, "^")), want: []string{"scheme-invalid " + tp}, msg: "must have the form"},
		{name: "tag_pattern is not anchored at the end", example: "mirror-zlib", mutate: zlibWith(strings.TrimSuffix(zlibPattern, "$")), want: []string{"scheme-invalid " + tp}, msg: "must have the form"},
		{name: "tag_pattern without a version group", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<version>", "(?:", 1)), want: []string{"scheme-invalid " + tp}, msg: "exactly one top-level capture group"},
		{name: "tag_pattern with two version groups", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<patch>", "(?P<version>", 1)), want: []string{"scheme-invalid " + tp}, msg: "used more than once"},
		{name: "tag_pattern prefix differs from the template", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "zlib-v", "zlib-w", 1)), want: []string{"scheme-invalid " + tp}, msg: "must equal those of tag_template"},
		{name: "tag_pattern with a suffix the template lacks", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, ")$", ")-x$", 1)), want: []string{"scheme-invalid " + tp}, msg: "must equal those of tag_template"},
		{name: "tag_pattern suffix matches the template", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, ")$", ")-x$", 1), set("zlib-v{version}-x", "version", "tag_template"))},
		{name: "unescaped dot in the literal prefix", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "zlib-v", "zlib.ng-v", 1), set("zlib.ng-v{version}", "version", "tag_template")), want: []string{"scheme-invalid " + tp}, msg: "must be literal text"},
		{name: "escaped dot in the literal prefix", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "zlib-v", `zlib\.ng-v`, 1), set("zlib.ng-v{version}", "version", "tag_template"))},
		{name: "case-insensitive pattern", example: "mirror-zlib", mutate: zlibWith("(?i)" + zlibPattern), want: []string{"scheme-invalid " + tp}, msg: "case-insensitive"},
		{name: "class in the literal prefix", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "zlib-v", "zli[a-c]-v", 1)), want: []string{"scheme-invalid " + tp}, msg: "must be literal text"},
		{name: "top-level alternation", example: "mirror-zlib", mutate: zlibWith(zlibPattern + "|^other"), want: []string{"scheme-invalid " + tp}, msg: "must have the form"},
		{name: "version group wrapped in another group", example: "mirror-zlib", mutate: zlibWith(strings.Replace(strings.Replace(zlibPattern, "(?P<version>", "((?P<version>", 1), "?)$", "?))$", 1)), want: []string{"scheme-invalid " + tp}, msg: "exactly one top-level capture group"},
		{name: "order name without a group", example: "mirror-zlib", mutate: set(strs("major", "bogus", "patch"), "version", "order"), want: []string{"scheme-invalid /version/order/1"}, msg: "not a named group"},
		{name: "duplicate order name", example: "mirror-zlib", mutate: set(strs("major", "major"), "version", "order"), want: []string{"scheme-invalid /version/order/1"}, msg: "listed twice"},
		{name: "order group of ten digits", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>[0-9]{1,10})", 1)), want: []string{"scheme-invalid /version/order/1"}, msg: "more than 9 digits"},
		{name: "order group that may be empty", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>[0-9]{0,9})", 1)), want: []string{"scheme-invalid /version/order/1"}, msg: "empty string"},
		{name: "order group that allows a letter", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>[0-9a]{1,9})", 1)), want: []string{"scheme-invalid /version/order/1"}, msg: "decimal digits"},
		{name: "order group that is unbounded", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>[0-9]+)", 1)), want: []string{"scheme-invalid /version/order/1", "scheme-invalid " + tp}, msg: "more than 9 digits"},
		{name: "order group of digit alternatives", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>1|22|0[1-9])", 1))},
		{name: "order group of a digit literal", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>0)", 1))},
		{name: "order group of an escaped digit class", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", `(?P<minor>\d{1,9})`, 1))},
		{name: "version group may match any character", example: "mirror-zlib", mutate: zlibWith(`^zlib-v(?P<version>(?P<major>[0-9]{1,9}).+)$`, set(strs("major"), "version", "order")), want: []string{"scheme-invalid " + tp}, msg: "outside A-Z"},
		{name: "version group longer than 64 characters", example: "mirror-zlib", mutate: zlibWith(`^zlib-v(?P<version>(?P<major>[0-9]{1,9})x{60})$`, set(strs("major"), "version", "order")), want: []string{"scheme-invalid " + tp}, msg: "more than 64 characters"},
		{name: "version group of exactly 64 characters", example: "mirror-zlib", mutate: zlibWith(`^zlib-v(?P<version>(?P<major>[0-9]{1,9})x{55})$`, set(strs("major"), "version", "order"))},
		{name: "version group that may be empty", example: "mirror-zlib", mutate: zlibWith(`^zlib-v(?P<version>(?P<major>[0-9]{1,9})?)$`, set(strs("major"), "version", "order")), want: []string{"scheme-invalid " + tp}, msg: "empty string"},
		{name: "version group with a space", example: "mirror-zlib", mutate: zlibWith(`^zlib-v(?P<version>(?P<major>[0-9]{1,9}) )$`, set(strs("major"), "version", "order")), want: []string{"scheme-invalid " + tp}, msg: "outside A-Z"},
		{name: "version group with a slash", example: "mirror-zlib", mutate: zlibWith(`^zlib-v(?P<version>(?P<major>[0-9]{1,9})/x)$`, set(strs("major"), "version", "order")), want: []string{"scheme-invalid " + tp}, msg: "outside A-Z"},
		{name: "tag_pattern without any group", example: "mirror-zlib", mutate: zlibWith(`^zlib-v$`), want: []string{"scheme-invalid " + tp}, msg: "no top-level"},
		{name: "second top-level group", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, ")$", ")(?P<extra>x)$", 1)), want: []string{"scheme-invalid " + tp}, msg: "exactly one top-level capture group"},
		{name: "unnamed top-level group", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<version>", "(", 1)), want: []string{"scheme-invalid " + tp}, msg: "exactly one top-level capture group"},
		{name: "unanchored pattern without a prefix", example: "mirror-zlib", mutate: zlibWith(`(?P<version>(?P<major>[0-9]{1,9}))$`, set("{version}", "version", "tag_template"), set(strs("major"), "version", "order")), want: []string{"scheme-invalid " + tp}, msg: "must have the form"},
		{name: "pattern without an end anchor and a suffix", example: "mirror-zlib", mutate: zlibWith(`^zlib-v(?P<version>(?P<major>[0-9]{1,9}))`, set(strs("major"), "version", "order")), want: []string{"scheme-invalid " + tp}, msg: "must have the form"},
		{name: "case-folded literal inside the version group", example: "mirror-zlib", mutate: zlibWith(`^zlib-v(?P<version>(?P<major>[0-9]{1,9})(?i:k))$`, set(strs("major"), "version", "order")), want: []string{"scheme-invalid " + tp}, msg: "outside A-Z"},
		{name: "bounded any-character in the version group", example: "mirror-zlib", mutate: zlibWith(`^zlib-v(?P<version>(?P<major>[0-9]{1,9}).{1,5})$`, set(strs("major"), "version", "order")), want: []string{"scheme-invalid " + tp}, msg: "outside A-Z"},
		{name: "order group alternative that may be empty", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>[0-9]{0,2}|[0-9]{3})", 1)), want: []string{"scheme-invalid /version/order/1"}, msg: "empty string"},
		{name: "order group alternative that is unbounded", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>[0-9]{2}|[0-9]{3,})", 1)), want: []string{"scheme-invalid /version/order/1", "scheme-invalid " + tp}, msg: "more than 9 digits"},
		{name: "order group repeated plus", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>(?:[0-9]{9})+)", 1)), want: []string{"scheme-invalid /version/order/1", "scheme-invalid " + tp}, msg: "more than 9 digits"},
		{name: "order group repeated twice", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>(?:[0-9]{5}){2})", 1)), want: []string{"scheme-invalid /version/order/1"}, msg: "more than 9 digits"},
		{name: "order group of exactly nine digits in two repeats", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>(?:[0-9]{3}){3})", 1))},
		{name: "order group made of optional digits", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>[0-9]?)", 1)), want: []string{"scheme-invalid /version/order/1"}, msg: "empty string"},
		{name: "order group of starred digits", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "(?P<minor>[0-9]{1,9})", "(?P<minor>[0-9]*)", 1)), want: []string{"scheme-invalid /version/order/1", "scheme-invalid " + tp}, msg: "empty string"},
		{name: "template with braces around nothing", example: "oci-bundle", mutate: set("toolbox/{version}{}", "version", "tag_template"), want: []string{"scheme-invalid /version/tag_template"}, msg: "exactly once"},
		{name: "literal outside the allowed characters", example: "mirror-zlib", mutate: zlibWith(strings.Replace(zlibPattern, "zlib-v", "zlib@v", 1), set("zlib@v{version}", "version", "tag_template")), want: []string{"schema-violation /version/tag_template", "scheme-invalid " + tp}, msg: "may use only"},
		{name: "file_pattern does not compile", example: "polyglot", mutate: set("(", "version", "file_pattern"), want: []string{"scheme-invalid /version/file_pattern"}, msg: "does not compile"},
		{name: "file_pattern without a group", example: "polyglot", mutate: set(`version: "[0-9.]+"`, "version", "file_pattern"), want: []string{"scheme-invalid /version/file_pattern"}, msg: "exactly one capture group"},
		{name: "file_pattern with two groups", example: "polyglot", mutate: set(`(version): "([0-9.]+)"`, "version", "file_pattern"), want: []string{"scheme-invalid /version/file_pattern"}, msg: "exactly one capture group"},
		{name: "file_pattern with a non-capturing group", example: "polyglot", mutate: set(`(?:version): "([0-9.]+)"`, "version", "file_pattern")},
		{name: "file_pattern with a named group", example: "polyglot", mutate: set(`version: "(?P<v>[0-9.]+)"`, "version", "file_pattern")},
	}
}

func allowlistCases() []semanticCase {
	scripts := fakeTree{"scripts/release-package.sh": "file", "scripts/release-smoke.sh": "file", "scripts": "dir"}
	withPattern := func(p string) func(*testing.T, map[string]any) {
		return appendTo(p, "allowlist", "paths")
	}
	overlap := "allowlist-overlaps-input /allowlist/paths/3"
	invalid := "schema-violation /allowlist/paths/3"
	return []semanticCase{
		{name: "pattern covering an argv file", example: "go-cli", tree: scripts, mutate: withPattern("scripts/**"), want: []string{overlap}},
		{name: "pattern covering a smoke_argv file only", example: "go-cli", tree: fakeTree{"scripts/release-smoke.sh": "file"}, mutate: withPattern("scripts/*.sh"), want: []string{overlap}},
		{name: "argv file that is not in the tree is not an input", example: "go-cli", tree: fakeTree{}, mutate: withPattern("scripts/**")},
		{name: "no tree means declared inputs only", example: "go-cli", mutate: withPattern("scripts/**")},
		{name: "pattern covering an argv directory", example: "go-cli", tree: fakeTree{"scripts": "dir"}, mutate: both(withPattern("scripts/**"), appendTo("scripts", "build", 0, "argv")), want: []string{overlap}},
		{name: "argv word that is a placeholder path is ignored", example: "go-cli", tree: scripts, mutate: both(withPattern("out/**"), appendTo("{out_dir}/x", "build", 0, "argv"))},
		{name: "pattern covering .stw", example: "go-cli", mutate: withPattern(".stw/**"), want: []string{overlap}},
		{name: "pattern covering .stw with other case", example: "go-cli", mutate: withPattern(".STW/**"), want: []string{overlap}},
		{name: "pattern covering .github", example: "go-cli", mutate: withPattern(".github/*"), want: []string{overlap}},
		{name: "pattern covering everything", example: "go-cli", mutate: withPattern("**"), want: []string{overlap}},
		{name: "star at the root", example: "go-cli", mutate: withPattern("*"), want: []string{overlap}},
		{name: "pattern covering the version file", example: "polyglot", mutate: withPattern("web/**"), want: []string{"allowlist-overlaps-input /allowlist/paths/2"}},
		{name: "pattern naming the version file", example: "polyglot", mutate: withPattern("web/mix.exs"), want: []string{"allowlist-overlaps-input /allowlist/paths/2"}},
		{name: "pattern covering a readiness file", example: "skill-pack", mutate: withPattern("readiness/**"), want: []string{"allowlist-overlaps-input /allowlist/paths/2"}},
		{name: "pattern covering a shared readiness file", example: "skill-pack", mutate: withPattern("schemas/*"), want: []string{"allowlist-overlaps-input /allowlist/paths/2"}},
		{name: "pattern covering a build lock file", example: "mirror-zlib", mutate: withPattern("sources/**"), want: []string{"allowlist-overlaps-input /allowlist/paths/1"}},
		{name: "pattern covering the image lock file", example: "oci-bundle", mutate: withPattern("bundles/**"), want: []string{"allowlist-overlaps-input /allowlist/paths/1"}},
		{name: "pattern naming the tool list file", example: "oci-bundle", mutate: withPattern("bundles/toolbox/tools.json"), want: []string{"allowlist-overlaps-input /allowlist/paths/1"}},
		{name: "pattern covering a working directory", example: "polyglot", mutate: both(set("tools/x", "build", 2, "working_dir"), withPattern("tools/x/**")), want: []string{"allowlist-overlaps-input /allowlist/paths/2"}},
		{name: "pattern naming a prefix of a working directory", example: "polyglot", mutate: both(set("tools/x", "build", 2, "working_dir"), withPattern("tools")), want: []string{"allowlist-overlaps-input /allowlist/paths/2"}},
		{name: "pattern below a working directory prefix only", example: "polyglot", mutate: both(set("tools/x", "build", 2, "working_dir"), withPattern("tools/y/**"))},
		{name: "unrelated pattern", example: "polyglot", mutate: withPattern("docs/guides/**")},
		{name: "pattern covering a release workflow outside .github", example: "go-cli", mutate: both(set("ops/release.yml", "ci", "release_workflow"), withPattern("ops/*")), want: []string{overlap}},
		{name: "pattern naming the image lock file exactly", example: "oci-bundle", mutate: withPattern("bundles/toolbox/tools.lock.json"), want: []string{"allowlist-overlaps-input /allowlist/paths/1"}},
		{name: "argv word with a placeholder is never probed", example: "go-cli", tree: fakeTree{"{out_dir}/x": "file"}, mutate: both(withPattern("*dir*/x"), appendTo("{out_dir}/x", "build", 0, "argv"))},
		{name: "argv path with a ./ prefix", example: "go-cli", tree: fakeTree{"scripts/other.sh": "file"}, mutate: both(withPattern("scripts/o*.sh"), appendTo("./scripts/other.sh", "build", 0, "argv")), want: []string{overlap}},
		{name: "argv path relative to the working directory", example: "go-cli", tree: fakeTree{"scripts/release.sh": "file"}, mutate: both(set("web", "build", 0, "working_dir"), withPattern("scripts/**"), appendTo("../scripts/release.sh", "build", 0, "argv")), want: []string{overlap}},
		{name: "argv directory is matched as a directory", example: "go-cli", tree: fakeTree{"scripts": "dir"}, mutate: both(withPattern("scripts/*.sh"), appendTo("scripts", "build", 0, "argv")), want: []string{overlap}},
		{name: "pattern with a trailing slash", example: "go-cli", mutate: withPattern("docs/"), want: []string{invalid}},
		{name: "pattern with a double star inside a segment", example: "go-cli", mutate: withPattern("a**b"), want: []string{invalid}},
		{name: "pattern with a dot-dot segment", example: "go-cli", mutate: withPattern("../x"), want: []string{invalid}},
		{name: "pattern with an empty segment", example: "go-cli", mutate: withPattern("docs//x"), want: []string{invalid}},
		{name: "pattern with a question mark", example: "go-cli", mutate: withPattern("a?b"), want: []string{invalid}},
		{name: "pattern naming a .git directory", example: "go-cli", mutate: withPattern(".git/**"), want: []string{invalid}},
	}
}
