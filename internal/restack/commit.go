// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// rawCommit is a commit object as stored, parsed just far enough for the plan.
type rawCommit struct {
	oid, tree      string
	parents        []string
	authorIdent    string // "Name <email>"
	authorName     string
	authorEmail    string
	authorDate     string // "<unix> <zone>"
	committerIdent string
	message        []byte
}

// parseCommit reads the output of `git cat-file commit`.
func parseCommit(oid string, raw []byte) (rawCommit, error) {
	c := rawCommit{oid: oid}
	head, message, found := bytes.Cut(raw, []byte("\n\n"))
	if !found {
		head, message = bytes.TrimSuffix(raw, []byte("\n")), nil
	}
	c.message = message
	for _, line := range strings.Split(string(head), "\n") {
		if strings.HasPrefix(line, " ") {
			continue // continuation of a multi-line header such as gpgsig
		}
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "tree":
			c.tree = value
		case "parent":
			c.parents = append(c.parents, value)
		case "author", "committer":
			ident, date, ok := splitIdent(value)
			if !ok {
				return c, fmt.Errorf("malformed %s line", key)
			}
			if key == "author" {
				c.authorIdent, c.authorDate = ident, date
				c.authorName, c.authorEmail, _ = strings.Cut(strings.TrimSuffix(ident, ">"), " <")
			} else {
				c.committerIdent = ident
			}
		}
	}
	if !hexOID.MatchString(c.tree) || c.authorIdent == "" || c.committerIdent == "" {
		return c, fmt.Errorf("malformed commit %s", oid)
	}
	return c, nil
}

// splitIdent splits "Name <email> 1700000000 +0000" into "Name <email>" and "1700000000 +0000".
func splitIdent(s string) (ident, date string, ok bool) {
	i := strings.LastIndex(s, "> ")
	if i < 0 || !strings.Contains(s[:i], " <") {
		return "", "", false
	}
	return s[:i+1], strings.TrimSpace(s[i+2:]), true
}

// subject is the first line of the message, bounded and made inert.
func subject(message []byte) string {
	line, _, _ := bytes.Cut(message, []byte("\n"))
	if len(line) > 200 {
		line = line[:200]
	}
	var b strings.Builder
	for len(line) > 0 {
		r, size := utf8.DecodeRune(line)
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, line[0])
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
		line = line[size:]
	}
	return b.String()
}

func movedCommit(c rawCommit) MovedCommit {
	sum := sha256.Sum256(firstLine(c.message))
	return MovedCommit{
		OID: c.oid, Parents: append([]string{}, c.parents...), Tree: c.tree, Author: c.authorIdent, Committer: c.committerIdent,
		Subject: subject(c.message), SubjectSHA256: hex.EncodeToString(sum[:]),
	}
}

func firstLine(b []byte) []byte {
	line, _, _ := bytes.Cut(b, []byte("\n"))
	return line
}
