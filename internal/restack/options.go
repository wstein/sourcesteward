// SPDX-License-Identifier: EUPL-1.2

// Package restack implements the read-only `stw restack plan` of Spec 015: for native Git it
// predicts, with plumbing in a private object quarantine, the restack of one branch after its
// parent was rebase-merged, and reports evidence. It never writes a ref, the index, the worktree,
// the configuration or the repository object store, and it never contacts a network.
package restack

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Options is the explicit minimal input of FR-011.
type Options struct {
	// Path is the checkout to read; empty means the current directory.
	Path string
	// Branch is the fully qualified refs/heads/... ref to restack.
	Branch string
	// OldParent and NewParent are a 40-hex commit id or a fully qualified ref.
	OldParent, NewParent string
	// ExpectHead, when set, pins the observed branch tip.
	ExpectHead string
	// Remote only names the remote in the printed texts; it is never contacted.
	Remote string
	// Identities are the only source of expected author/committer identities (FR-015).
	Identities []string
}

var (
	hexOID       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	remoteName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	identityForm = regexp.MustCompile(`^([^<>]*[^<>\s]) <([^<>\s]+)>$`)
)

// qualifiedRef reports whether s is a fully qualified, conservative ref name: it starts with
// refs/, has at least one further component and contains no revision-expression or option
// syntax, whitespace, control character or Git-forbidden sequence.
func qualifiedRef(s string) bool {
	if !strings.HasPrefix(s, "refs/") || len(s) > 255 || !utf8.ValidString(s) {
		return false
	}
	if strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.HasSuffix(s, ".lock") ||
		strings.Contains(s, "..") || strings.Contains(s, "//") || strings.Contains(s, "@{") || strings.Contains(s, "/.") {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	return len(strings.Split(s, "/")) >= 3
}

func parentSyntax(s string) bool { return hexOID.MatchString(s) || qualifiedRef(s) }

// ValidateOptions reports a syntax error (exit status 2) without touching Git. The remote must
// already carry its default.
func ValidateOptions(o *Options) error {
	if !strings.HasPrefix(o.Branch, "refs/heads/") || len(o.Branch) == len("refs/heads/") || !qualifiedRef(o.Branch) {
		return errors.New("--branch must be a fully qualified refs/heads/... ref")
	}
	if !parentSyntax(o.OldParent) {
		return errors.New("--old-parent must be a 40-hex commit id or a fully qualified ref")
	}
	if !parentSyntax(o.NewParent) {
		return errors.New("--new-parent must be a 40-hex commit id or a fully qualified ref")
	}
	if o.ExpectHead != "" && !hexOID.MatchString(o.ExpectHead) {
		return errors.New("--expect-head must be a 40-hex commit id")
	}
	if !remoteName.MatchString(o.Remote) {
		return errors.New("--remote must be a plain remote name")
	}
	seen := map[string]bool{}
	for _, identity := range o.Identities {
		if !validIdentity(identity) {
			return fmt.Errorf("--identity must be \"NAME <EMAIL>\" without control characters: %q", identity)
		}
		if seen[identity] {
			return fmt.Errorf("--identity is repeated: %q", identity)
		}
		seen[identity] = true
	}
	return nil
}

func validIdentity(s string) bool {
	if !utf8.ValidString(s) || !identityForm.MatchString(s) {
		return false
	}
	for _, r := range s {
		if r < ' ' || r == 0x7f {
			return false
		}
	}
	return true
}
