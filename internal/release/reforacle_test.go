// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// refValid asks Git itself whether refs/tags/<tag> is a valid reference name.
func refValid(t *testing.T, tag string) bool {
	t.Helper()
	cmd := exec.Command("git", "check-ref-format", "refs/tags/"+tag)
	cmd.Env = gitEnv()
	return cmd.Run() == nil
}

// versionCorpus holds candidate versions, including ones that break reference names.
var versionCorpus = []string{"1", "1.2", "1.2.3", "10.20.30", "2026.10.15", "2026.10.15-b98.1", "a", "ab-1", "1-", "1_", "1.", "1..2", ".1", "-1", "_1", "1.lock", "a.lock", "1.2.lock", "1.2.3.4.5", "v1", "0.0.1", "x.y", "1@{2", "1/2", "1.2-rc.1"}

// versionGroups are the version-group expressions of the generated contracts.
var versionGroups = []string{
	`(?P<major>[0-9]{1,9})`,
	`(?P<major>[0-9]{1,9})\.(?P<minor>[0-9]{1,9})`,
	`(?P<major>[0-9]{1,9})\.(?P<minor>[0-9]{1,9})(?:\.(?P<patch>[0-9]{1,9}))?`,
	`(?P<major>[0-9]{1,9})\.?`,
	`(?P<major>[0-9]{1,9})\.\.(?P<minor>[0-9])`,
	`(?P<major>[0-9]{1,9})[0-9.]{1,3}x`,
	`(?P<major>[0-9]{1,9})(?:\.[0-9]{1,3}){0,5}x`,
	`(?P<major>[0-9]{1,9})(?:-b[0-9]{1,3})?`,
	`(?P<major>[a-z0-9]{1,9})(?:\.lock)?`,
	`(?P<major>[0-9]{1,9})-`,
	`\.(?P<major>[0-9]{1,9})`,
	`(?P<major>[0-9]{1,9})[.-][0-9]`,
}

var templatePrefixes = []string{"", "a/", "a.lock/", "a./", "a..b/", "-x/", "a//", "/a", "a/b/", ".a/", "a.lock", "a@", "a.b-c/", "zlib-v", "tool/box-", "a/.b/", "a/b.lock/c-"}
var templateSuffixes = []string{"", "/", "//x", "/.x", ".", ".lock", "-x", ".x", "x/", "a/b", ".lock/y", "..x", "-", "_", "-b1", ".lockfile", "/x.lock", "x.lock"}

// TestAcceptedTemplatesGiveValidGitReferences generates pattern products and checks with
// git check-ref-format that every tag a contract that passes `check` can produce is a valid ref,
// except the documented residual: a version that ends in ".lock" when the suffix is empty.
func TestAcceptedTemplatesGiveValidGitReferences(t *testing.T) {
	accepted, tags := 0, 0
	for _, prefix := range templatePrefixes {
		for _, suffix := range templateSuffixes {
			for _, group := range versionGroups {
				pattern := `^` + regexp.QuoteMeta(prefix) + `(?P<version>` + group + `)` + regexp.QuoteMeta(suffix) + `$`
				template := prefix + "{version}" + suffix
				doc := loadExample(t, "mirror-zlib")
				setAt(t, doc, template, "version", "tag_template")
				setAt(t, doc, pattern, "version", "tag_pattern")
				setAt(t, doc, strs("major"), "version", "order")
				contract, findings, err := ValidateContract(doc, ValidateOptions{Product: "zlib"})
				if err != nil || contract == nil {
					_ = findings
					continue
				}
				accepted++
				re := regexp.MustCompile(pattern)
				for _, version := range versionCorpus {
					tag := prefix + version + suffix
					if !re.MatchString(tag) {
						continue
					}
					tags++
					if refValid(t, tag) {
						continue
					}
					if suffix == "" && strings.HasSuffix(version, ".lock") {
						continue // documented residual: not excludable from the expression
					}
					t.Errorf("accepted but invalid ref: template %q, group %q, tag %q", template, group, tag)
				}
			}
		}
	}
	if accepted < 100 || tags < 300 {
		t.Fatalf("the generator is vacuous: %d contracts accepted, %d tags checked", accepted, tags)
	}
}

func TestAcceptedTagPrefixesGiveValidGitReferences(t *testing.T) {
	prefixes := []string{"", "a/", "a.lock/", "a./", "a..b/", "-x/", "a//", "a/b/", ".a/", "a.b-c/", "a/b.lock/", "a._/", "a-/", "zlib/", "x.y.z/", "a/.b/", "a..", "a/b..c/"}
	accepted := 0
	for _, scheme := range []string{"semver", "calver"} {
		for _, prefix := range prefixes {
			doc := loadExample(t, "go-cli")
			setAt(t, doc, scheme, "version", "scheme")
			setAt(t, doc, prefix, "version", "tag_prefix")
			contract, _, err := ValidateContract(doc, ValidateOptions{})
			if err != nil || contract == nil {
				continue
			}
			accepted++
			for _, version := range []string{"v0.0.0", "v1.2.3", "v10.20.30", "2026.10.15", "2026.10.15-b98.1", "2026.10.15-b1"} {
				if (scheme == "semver") != strings.HasPrefix(version, "v") {
					continue
				}
				if tag := prefix + version; !refValid(t, tag) {
					t.Errorf("accepted but invalid ref: %s prefix %q, tag %q", scheme, prefix, tag)
				}
			}
		}
	}
	if accepted < 6 {
		t.Fatalf("only %d prefixes accepted", accepted)
	}
	_ = fmt.Sprint
}
