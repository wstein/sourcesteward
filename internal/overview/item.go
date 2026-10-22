// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// newItem builds an item: the priority comes from the category row, the id is CATEGORY:REPO:PART (never
// time based) and the command from the validated values of t. part names the subject inside the repository
// and must already be an id component (see idToken); repo is made one here. The caller passes Title already
// escaped and URL already checked.
func newItem(c Category, repo string, s Subject, part string, t target) Item {
	p, _ := PriorityOf(c)
	it := Item{ID: string(c) + ":" + idToken(repo, validName) + ":" + part, Priority: p, Category: c, Repo: repo, Subject: s}
	it.Command, it.Effect = buildCommand(c, t)
	return it
}

// validRef is the id test of a tag or branch name: the one the commands use.
func validRef(s string) bool {
	_, ok := refWord(s)
	return ok
}

// validName is the id test of a repository name or a path: the shell-word test of the commands, and no `:`
// (the separator of the id) and at most 255 bytes.
func validName(s string) bool {
	_, ok := shellWord(s)
	return ok && !strings.Contains(s, ":") && len(s) <= 255
}

// idToken returns s when valid(s) holds and a stable fingerprint of s otherwise: `h-` and the first 12
// hexadecimal digits of the SHA-256 of s. An id therefore never holds a raw forge or path string that
// failed validation (control characters, `:`, 10 kB of text); the raw string stays in the title and the
// subject. The fingerprint depends on the string only, never on time or order.
func idToken(s string, valid func(string) bool) string {
	if valid(s) {
		return s
	}
	return fingerprint(s)
}

func fingerprint(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "h-" + hex.EncodeToString(sum[:])[:12]
}
