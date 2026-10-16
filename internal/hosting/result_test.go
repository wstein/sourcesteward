// SPDX-License-Identifier: EUPL-1.2
package hosting

import "testing"

func TestValidateOptions(t *testing.T) {
	for _, o := range []Options{{Kind: "repo"}, {Kind: "pr", Number: 1, Repo: "a/b"}, {Kind: "ci", Number: 2, Host: "github.example.com", Remote: "upstream"}} {
		if err := ValidateOptions(o); err != nil {
			t.Fatalf("%+v: %v", o, err)
		}
	}
	for _, o := range []Options{{Kind: "push"}, {Kind: "repo", Number: 1}, {Kind: "pr"}, {Kind: "ci", Number: -1}, {Kind: "repo", Repo: "a/b/c"}, {Kind: "repo", Repo: "../b"}, {Kind: "repo", Host: "evil.com/path"}, {Kind: "repo", Host: "https://github.com"}, {Kind: "repo", Host: "localhost"}, {Kind: "repo", Remote: "bad\nname"}} {
		if ValidateOptions(o) == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
}
