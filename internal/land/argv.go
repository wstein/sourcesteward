// SPDX-License-Identifier: EUPL-1.2

package land

import (
	"strconv"
	"strings"
)

// The argument vectors of the commands landing runs. They are built here and nowhere else, so the
// plan shows exactly what the Git runner executes, and one test pins the whole set (FR-033).

// PrivateRefPrefix is the only namespace the planning fetch writes refs into (FR-002).
const PrivateRefPrefix = "refs/stw/land/"

// BaseRef is the private ref of the fetched default branch.
func BaseRef() string { return PrivateRefPrefix + "base" }

// PullRef is the private ref of the fetched head of pull request n.
func PullRef(n int) string { return PrivateRefPrefix + "pr-" + strconv.Itoa(n) }

// FetchArgs is the planning fetch (FR-016): objects and the private namespace only, no
// FETCH_HEAD, no tags, no user ref.
func FetchArgs(remote, branch string, numbers []int) []string {
	args := []string{"fetch", "--no-write-fetch-head", "--no-tags", "--no-recurse-submodules", "--refmap=", remote, "+refs/heads/" + branch + ":" + BaseRef()}
	for _, n := range numbers {
		args = append(args, "+refs/pull/"+strconv.Itoa(n)+"/head:"+PullRef(n))
	}
	return args
}

// PushArgs is the one landing push (FR-070): a plain push of the tip to the fully qualified default
// branch. No force mode, no lease, no plus refspec, no second ref.
func PushArgs(remote, tip, branch string) []string {
	return []string{"push", "--porcelain", remote, tip + ":refs/heads/" + branch}
}

// RefName reports whether s is a plain remote or branch name that can stand in an argument vector:
// non-empty, no leading dash, only the characters of ordinary Git names and no "..".
func RefName(s string) bool {
	if s == "" || s[0] == '-' || s[0] == '/' || strings.Contains(s, "..") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".lock") {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-', c == '/':
		default:
			return false
		}
	}
	return true
}

// Argv prefixes "git" for display.
func Argv(args []string) []string { return append([]string{"git"}, args...) }

// RetargetArgv is the base edit of one native item (FR-086): the pull request's base becomes the
// default branch. Only the base changes; the head, title and body are untouched.
func RetargetArgv(number int, branch string) []string {
	return []string{"gh", "pr", "edit", strconv.Itoa(number), "--base", branch}
}
