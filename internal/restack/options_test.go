// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"strings"
	"testing"
)

const hex40 = "0123456789abcdef0123456789abcdef01234567"

func goodOptions() Options {
	return Options{Branch: "refs/heads/feat/x", OldParent: "refs/heads/parent", NewParent: hex40, Remote: "origin"}
}

func TestValidateOptionsAcceptsExplicitInput(t *testing.T) {
	o := goodOptions()
	o.ExpectHead = hex40
	o.Identities = []string{"Owner Name <owner@example.org>"}
	if err := ValidateOptions(&o); err != nil {
		t.Fatal(err)
	}
	if o.Identities[0] != "Owner Name <owner@example.org>" {
		t.Fatalf("identity not kept verbatim: %q", o.Identities[0])
	}
}

func TestValidateOptionsRefusesNonExplicitRevisions(t *testing.T) {
	bad := []string{"HEAD", "HEAD~1", "main", "refs/heads/a..b", "refs/heads/a^", "refs/heads/a~2", "refs/heads/a@{1}", "0123456", strings.ToUpper(hex40), "refs/heads/a b", "refs/heads/a\nb", "refs/heads/", "refs/heads/a.lock", "refs/heads/a/", "refs/heads//a", "refs/heads/a:b", "refs/heads/-a" + "?", "", hex40 + "0"}
	for _, value := range bad {
		for field, set := range map[string]func(*Options){
			"old-parent": func(o *Options) { o.OldParent = value },
			"new-parent": func(o *Options) { o.NewParent = value },
		} {
			o := goodOptions()
			set(&o)
			if ValidateOptions(&o) == nil {
				t.Errorf("%s accepted %q", field, value)
			}
		}
	}
	for _, value := range append([]string{"main", "refs/tags/v1", "refs/remotes/origin/main", hex40}, bad...) {
		o := goodOptions()
		o.Branch = value
		if ValidateOptions(&o) == nil {
			t.Errorf("branch accepted %q", value)
		}
	}
}

func TestValidateOptionsRefusesBadFlags(t *testing.T) {
	cases := map[string]func(*Options){
		"expect-head short":   func(o *Options) { o.ExpectHead = "abc" },
		"expect-head ref":     func(o *Options) { o.ExpectHead = "refs/heads/x" },
		"remote slash":        func(o *Options) { o.Remote = "a/b" },
		"remote option":       func(o *Options) { o.Remote = "-x" },
		"remote empty":        func(o *Options) { o.Remote = "" },
		"identity no email":   func(o *Options) { o.Identities = []string{"Name"} },
		"identity empty name": func(o *Options) { o.Identities = []string{" <a@b>"} },
		"identity control":    func(o *Options) { o.Identities = []string{"N\x01 <a@b>"} },
		"identity extra":      func(o *Options) { o.Identities = []string{"N <a@b> trailing"} },
		"identity duplicate":  func(o *Options) { o.Identities = []string{"N <a@b>", "N <a@b>"} },
	}
	for name, mutate := range cases {
		o := goodOptions()
		mutate(&o)
		if ValidateOptions(&o) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
