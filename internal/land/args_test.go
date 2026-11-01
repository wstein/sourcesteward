// SPDX-License-Identifier: EUPL-1.2

package land

import (
	"errors"
	"strings"
	"testing"
)

func rv(n string, c byte) string { return n + ":" + sha(c) }

func TestParseChainAccepts(t *testing.T) {
	items, err := ParseChain("12,13,14", []string{rv("14", 'c'), rv("12", 'a'), rv("13", 'b')})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0] != (Item{12, sha('a')}) || items[2] != (Item{14, sha('c')}) {
		t.Fatalf("items %+v", items)
	}
}

func TestParseChainUsageErrors(t *testing.T) {
	ok := []string{rv("12", 'a'), rv("13", 'b')}
	cases := map[string]struct {
		list string
		rev  []string
		want string
	}{
		"single number":      {"12", []string{rv("12", 'a')}, "at least 2"},
		"nine":               {"1,2,3,4,5,6,7,8,9", nil, "at most 8"},
		"duplicate":          {"12,12", []string{rv("12", 'a'), rv("12", 'b')}, "duplicate"},
		"not a number":       {"12,x", ok, "invalid pull request number"},
		"zero":               {"12,0", ok, "invalid pull request number"},
		"leading zero":       {"12,013", ok, "invalid pull request number"},
		"empty element":      {"12,,13", ok, "invalid pull request number"},
		"missing reviewed":   {"12,13", ok[:1], "--reviewed"},
		"extra reviewed":     {"12,13", append(append([]string{}, ok...), rv("14", 'c')), "not in --stack"},
		"duplicate reviewed": {"12,13", []string{ok[0], ok[0], ok[1]}, "duplicate"},
		"short sha":          {"12,13", []string{"12:abc", ok[1]}, "40 hexadecimal"},
		"upper sha":          {"12,13", []string{"12:" + strings.ToUpper(sha('a')), ok[1]}, "40 hexadecimal"},
		"no colon":           {"12,13", []string{sha('a'), ok[1]}, "NUMBER:SHA"},
		"empty list":         {"", nil, "at least 2"},
	}
	for name, c := range cases {
		_, err := ParseChain(c.list, c.rev)
		var u *UsageError
		if !errors.As(err, &u) || !strings.Contains(u.Error(), c.want) {
			t.Errorf("%s: %v, want usage error containing %q", name, err, c.want)
		}
	}
}
