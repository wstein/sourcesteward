// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/untrusted"
)

// maxURLBytes bounds a URL of an item; a longer one is dropped rather than cut.
const maxURLBytes = 2048

// target holds the values a suggested command may be built from. Every value is validated again by
// buildCommand, so a command never contains an unvalidated string.
type target struct {
	Slug     string // OWNER/NAME
	Path     string // repository path of a local finding
	Root     string // workspace root
	Worktree string // path of a linked worktree
	Number   int    // pull request number
	Tag      string
	Branch   string
	SHA      string
	Source   Source // source of a read-error
}

// shellWord returns s as one shell word: unchanged when it only has characters of [A-Za-z0-9._/-],
// single-quoted otherwise. It refuses the empty string, a leading `-` (an option for the command),
// control or format characters (including bidirectional overrides and newlines) and invalid UTF-8.
func shellWord(s string) (string, bool) {
	if s == "" || s[0] == '-' || !utf8.ValidString(s) {
		return "", false
	}
	plain := true
	for _, r := range s {
		if unsafeRune(r) {
			return "", false
		}
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._/-", r)) {
			plain = false
		}
	}
	if plain {
		return s, true
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", true
}

// refWord returns a tag or branch name as one shell word. The name must pass the release-tag validation of
// Spec 026 (a name Git accepts as a ref, at most 255 bytes, not an abbreviated hexadecimal identity) and the
// shell-word rules; anything else, a forge string included, never enters a command. The only difference from
// Spec 026 is that a name with control, format or line-separator characters is refused here too, because the
// result is printed.
func refWord(s string) (string, bool) {
	if !untrusted.ValidReleaseTag(s) {
		return "", false
	}
	return shellWord(s)
}

// unsafeRune reports whether r must never appear in a command, an id or a URL: a control, format or
// line-separator character (bidirectional overrides included).
func unsafeRune(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
}

// isSHA reports whether s is a full lowercase hexadecimal object id (SHA-1 or SHA-256).
func isSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// httpsURL returns s when it is an https URL without whitespace or control characters and at most 2048
// bytes, and the empty string otherwise.
func httpsURL(s string) string {
	if len(s) > maxURLBytes || !strings.HasPrefix(s, "https://") || !utf8.ValidString(s) {
		return ""
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unsafeRune(r) {
			return ""
		}
	}
	return s
}

// buildCommand returns the suggested command of a category and its effect, or two empty strings when
// the category has none or a value it needs does not validate. `stw` never runs the command.
func buildCommand(c Category, t target) (string, Effect) {
	repo := func() (string, bool) { return t.Slug, untrusted.ValidRepo(t.Slug) }
	path := func() (string, bool) { return shellWord(t.Path) }
	switch c {
	case RepoError, GitlinkDrift:
		return with(path, func(p string) string { return "stw status --offline " + p }, Read)
	case LocalOperation, LocalChanges, LocalDetached, LocalUntracked:
		return with(path, func(p string) string { return "git -C " + p + " status" }, Read)
	case LocalBehind, MainBehindOrigin:
		return with(path, func(p string) string { return "stw sync --plan " + p }, Read)
	case LocalNoUpstream:
		return with(path, func(p string) string { return "git -C " + p + " branch -vv" }, Read)
	case LocalStash:
		return with(path, func(p string) string { return "git -C " + p + " stash list" }, Read)
	case BranchesUnmerged, WorktreeStale:
		return with(path, func(p string) string { return "stw tidy plan " + p }, Read)
	case WorktreeDirty:
		return with(func() (string, bool) { return shellWord(t.Worktree) }, func(w string) string { return "git -C " + w + " status" }, Read)
	case GitlinkAhead:
		root, ok := shellWord(t.Root)
		p, okPath := path()
		if !ok || !okPath {
			return "", ""
		}
		return "git -C " + root + " add " + p, Owner
	case MainRed:
		b, okRef := refWord(t.Branch)
		if r, ok := repo(); ok && okRef {
			return "gh run list --repo " + r + " --branch " + b, Read
		}
	case ReleaseRunFailed:
		if r, ok := repo(); ok && isSHA(t.SHA) {
			return "gh run list --repo " + r + " --commit " + t.SHA, Read
		}
	case ReadError:
		if t.Source == SourcePulls {
			return with(repo, func(r string) string { return "stw pulls --repo " + r }, Read)
		}
	case PRConflict, PRBlocked, PRChecksPending, PRUnread:
		return with(repo, func(r string) string { return "stw pulls --repo " + r }, Read)
	case PRBot:
		return with(repo, func(r string) string { return "stw pulls --repo " + r + " --bots-only" }, Read)
	case IssueSecurity, IssueAssigned, IssueBug:
		if r, ok := repo(); ok && t.Number > 0 {
			return "gh issue view " + strconv.Itoa(t.Number) + " -R " + r, Read
		}
	case PRReady:
		if r, ok := repo(); ok && t.Number > 0 {
			return "gh pr merge " + strconv.Itoa(t.Number) + " -R " + r + " --rebase", Owner
		}
	case ReleaseDraft:
		tag, okRef := refWord(t.Tag)
		if r, ok := repo(); ok && okRef {
			return "gh release edit " + tag + " -R " + r + " --draft=false", Owner
		}
	}
	return "", ""
}

// with builds a command from one validated value.
func with(value func() (string, bool), format func(string) string, e Effect) (string, Effect) {
	v, ok := value()
	if !ok {
		return "", ""
	}
	return format(v), e
}
