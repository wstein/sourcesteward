// SPDX-License-Identifier: EUPL-1.2

package land

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// UsageError is an invalid command line (exit 2).
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

func usage(format string, a ...any) error { return &UsageError{Msg: fmt.Sprintf(format, a...)} }

var (
	numberGrammar = regexp.MustCompile(`\A[1-9][0-9]{0,8}\z`)
	shaGrammar    = regexp.MustCompile(`\A[0-9a-f]{40}\z`)
)

// ValidSHA reports whether s is a full lowercase 40-hex object id.
func ValidSHA(s string) bool { return shaGrammar.MatchString(s) }

// ParseChain validates --stack (bottom first) and the --reviewed NUMBER:SHA list (FR-060): two to
// eight distinct numbers, and a reviewed set equal to the list.
func ParseChain(list string, reviewed []string) ([]Item, error) {
	var numbers []int
	if list != "" {
		seen := map[int]bool{}
		for _, part := range strings.Split(list, ",") {
			if !numberGrammar.MatchString(part) {
				return nil, usage("--stack: invalid pull request number %q", part)
			}
			n, _ := strconv.Atoi(part)
			if seen[n] {
				return nil, usage("--stack: duplicate pull request %d", n)
			}
			seen[n] = true
			numbers = append(numbers, n)
		}
	}
	if len(numbers) < MinChain {
		return nil, usage("--stack needs at least %d pull requests (one pull request lands with --pr)", MinChain)
	}
	if len(numbers) > MaxChain {
		return nil, usage("--stack takes at most %d pull requests", MaxChain)
	}
	heads := map[int]string{}
	for _, r := range reviewed {
		num, head, ok := strings.Cut(r, ":")
		if !ok || !numberGrammar.MatchString(num) {
			return nil, usage("--reviewed %q: want NUMBER:SHA", r)
		}
		if !ValidSHA(head) {
			return nil, usage("--reviewed %q: the SHA must be 40 hexadecimal characters in lowercase", r)
		}
		n, _ := strconv.Atoi(num)
		if _, dup := heads[n]; dup {
			return nil, usage("--reviewed: duplicate entry for pull request %d", n)
		}
		heads[n] = head
	}
	items := make([]Item, 0, len(numbers))
	for _, n := range numbers {
		head, ok := heads[n]
		if !ok {
			return nil, usage("--reviewed NUMBER:SHA is missing for pull request %d", n)
		}
		items = append(items, Item{Number: n, Reviewed: head})
		delete(heads, n)
	}
	for n := range heads {
		return nil, usage("--reviewed names pull request %d, which is not in --stack", n)
	}
	return items, nil
}
