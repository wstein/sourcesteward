// SPDX-License-Identifier: EUPL-1.2

// Package gitversion parses Git version numbers. It has no dependencies, so tests of any package may use it.
package gitversion

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse returns the major and minor number of a Git version number such as "2.43.0" or "2.39.5". The
// first two dot-separated fields must be plain decimal digits.
func Parse(v string) (major, minor int, err error) {
	p := strings.Split(v, ".")
	if len(p) < 2 {
		return 0, 0, fmt.Errorf("invalid Git version %q", v)
	}
	if major, err = digits(p[0]); err != nil {
		return 0, 0, fmt.Errorf("invalid Git version %q", v)
	}
	if minor, err = digits(p[1]); err != nil {
		return 0, 0, fmt.Errorf("invalid Git version %q", v)
	}
	return major, minor, nil
}

// AtLeast reports whether the Git version number v is at least major.minor; an invalid v is not.
func AtLeast(v string, major, minor int) bool {
	ma, mi, err := Parse(v)
	return err == nil && (ma > major || (ma == major && mi >= minor))
}

// digits parses a non-empty string of ASCII decimal digits; strconv.Atoi would also take a sign.
func digits(s string) (int, error) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, strconv.ErrSyntax
	}
	return strconv.Atoi(s)
}
