// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"regexp"
	"strings"
)

// The printed command texts are inert and safe to paste into a POSIX shell (FR-006, D8). Every
// value stw knows is a single-quoted literal restricted to a conservative character set; the one
// unknown, the new commit id, is a shell variable that the owner must set (an unset or empty
// variable aborts before git runs). stw never runs these commands, never prints --force and
// never prints a bare --force-with-lease.
var textSafe = regexp.MustCompile(`^[A-Za-z0-9._/@+=,%-]+$`)

const leaseAssignment = "NEW_FULL_SHA=   # set to the full 40-hex id of the new branch tip before running the next line"

func (p *planner) texts() {
	branch := p.res.Input.Branch
	safe := textSafe.MatchString(branch.Ref) && textSafe.MatchString(p.opts.Remote) && textSafe.MatchString(branch.Short)
	if !safe || branch.OldHead == nil || !hexOID.MatchString(*branch.OldHead) {
		p.res.Diagnostics = append(p.res.Diagnostics, Diagnostic{"text-omitted", "The branch or remote name has characters that cannot be printed safely in a shell command; the command texts are omitted.", map[string]any{}})
		return
	}
	old, remote := *branch.OldHead, p.opts.Remote
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "") + "'" }
	p.res.Texts.LeasePush = &LeasePush{
		Assignment:     leaseAssignment,
		Text:           "git push --force-with-lease=" + q(branch.Ref+":"+old) + " " + q(remote) + " \"${NEW_FULL_SHA:?set to the full new commit id}:" + branch.Ref + "\"",
		ObservedOldSHA: old, NewSHAKnown: false,
	}
	p.res.Texts.Resync = &Resync{
		Fetch:           "git fetch --prune",
		RebaseOnto:      "git rebase --onto " + q(remote+"/"+branch.Short) + " " + q(old) + " " + q(branch.Short),
		ResetHard:       "git switch " + q(branch.Short) + " && git reset --hard " + q(remote+"/"+branch.Short),
		DestructiveNote: "DESTRUCTIVE for local-only work and uncommitted changes.",
	}
}
