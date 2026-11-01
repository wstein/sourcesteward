// SPDX-License-Identifier: EUPL-1.2
package carryover

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRequestAcceptsOnlyFullLowercaseObjectIDs(t *testing.T) {
	good := []string{sha("1"), sha("2"), sha("3"), sha("a")}
	r, err := ParseRequest(good, "42", "origin")
	if err != nil || r.PR != 42 || r.OldBase != good[0] || r.NewHead != good[3] || r.Remote != "origin" {
		t.Fatalf("%+v %v", r, err)
	}
	for name, bad := range map[string]string{
		"leading dash":        "-" + sha("1")[1:],
		"option":              "--output=/tmp/x",
		"dash dash":           "--",
		"range":               sha("1")[:20] + ".." + sha("1")[22:],
		"space":               sha("1")[:20] + " " + sha("1")[21:],
		"ref name":            "main",
		"HEAD":                "HEAD",
		"short":               "abc1234",
		"uppercase":           strings.ToUpper(strings.Repeat("a", 40)),
		"41 characters":       sha("1") + "1",
		"trailing newline":    sha("1") + "\n",
		"revision suffix":     sha("1")[:38] + "^2",
		"empty":               "",
		"refs path":           "refs/heads/main",
		"colon":               sha("1")[:20] + ":" + sha("1")[21:],
		"nul":                 sha("1")[:20] + "\x00" + sha("1")[21:],
		"leading dash option": "-c core.pager=evil",
	} {
		for i := 0; i < 4; i++ {
			args := append([]string(nil), good...)
			args[i] = bad
			_, err := ParseRequest(args, "1", "")
			var u *UsageError
			if !errors.As(err, &u) {
				t.Errorf("%s at position %d: accepted (%v)", name, i, err)
			}
		}
	}
}

func TestParseRequestRejectsBadCounts(t *testing.T) {
	good := []string{sha("1"), sha("2"), sha("3"), sha("4")}
	for _, args := range [][]string{nil, good[:2], good[:3], append(append([]string{}, good...), sha("5"))} {
		if _, err := ParseRequest(args, "1", ""); err == nil {
			t.Errorf("%d arguments accepted", len(args))
		}
	}
}

func TestParseRequestValidatesPullNumberAndRemote(t *testing.T) {
	good := []string{sha("1"), sha("2"), sha("3"), sha("4")}
	for _, pr := range []string{"", "0", "-1", "01", "1 2", "1.5", "abc", "1000000000", "--x"} {
		if _, err := ParseRequest(good, pr, ""); err == nil {
			t.Errorf("pr %q accepted", pr)
		}
	}
	for _, remote := range []string{"-u", "--upload-pack=evil", "a b", "x..y", "a/b", "origin:x", "-"} {
		if _, err := ParseRequest(good, "1", remote); err == nil {
			t.Errorf("remote %q accepted", remote)
		}
	}
}
