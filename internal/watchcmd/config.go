// SPDX-License-Identifier: EUPL-1.2

// Package watchcmd wires `stw watch`: configuration, repository discovery, the hosting observer, the
// poll loop with its modes and exit codes, the human line format and the --status report. The
// polling core lives in internal/watch; every network read goes through internal/hosting.
package watchcmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/release"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// MinInterval is the smallest polling interval (Spec 035).
const MinInterval = 10 * time.Second

// DefaultMaxOpenPRs is the per-repository bound of open pull requests.
const DefaultMaxOpenPRs = 2000

// Config is `.stw/watch.toml`. Zero values mean "not set" (the flag or built-in default applies).
type Config struct {
	Repos         []string
	Interval      time.Duration
	DetailEvery   int
	MaxOpenPRs    int
	IgnoreAuthors []string
	IgnoreLabels  []string
	StackPatterns []string
}

// ParseConfig reads the strict profile of `.stw/watch.toml`: unknown keys and tables, wrong types,
// duplicate keys and a missing or different `schema_version` are errors (usage errors for the CLI).
func ParseConfig(data []byte) (Config, error) {
	var c Config
	doc, findings := release.ParseTOML(data)
	if len(findings) > 0 {
		return c, fmt.Errorf("watch.toml: %s", findings[0].Message)
	}
	known := map[string]bool{"schema_version": true, "repos": true, "interval": true, "detail_every": true, "max_open_prs": true, "ignore": true, "shape": true}
	for _, k := range sortedKeys(doc) {
		if !known[k] {
			return c, fmt.Errorf("watch.toml: unknown key %q", k)
		}
	}
	if v, ok := doc["schema_version"].(int64); !ok || v != 1 {
		return c, errors.New("watch.toml: schema_version = 1 is required")
	}
	var err error
	if v, ok := doc["repos"]; ok {
		list, err := stringList(v, "repos")
		if err != nil {
			return c, err
		}
		for _, r := range list {
			if !untrusted.ValidRepo(r) {
				return c, fmt.Errorf("watch.toml: repos entry %q is not OWNER/NAME", r)
			}
		}
		c.Repos = list
	}
	if v, ok := doc["interval"]; ok {
		s, isString := v.(string)
		if !isString {
			return c, errors.New("watch.toml: interval must be a string such as \"60s\"")
		}
		if c.Interval, err = ParseInterval(s); err != nil {
			return c, fmt.Errorf("watch.toml: interval: %w", err)
		}
	}
	if v, ok := doc["detail_every"]; ok {
		n, isInt := v.(int64)
		if !isInt || n < 1 || n > 1_000_000 {
			return c, errors.New("watch.toml: detail_every must be an integer of at least 1")
		}
		c.DetailEvery = int(n)
	}
	if v, ok := doc["max_open_prs"]; ok {
		n, isInt := v.(int64)
		if !isInt || n < 1 || n > DefaultMaxOpenPRs {
			return c, fmt.Errorf("watch.toml: max_open_prs must be an integer from 1 to %d", DefaultMaxOpenPRs)
		}
		c.MaxOpenPRs = int(n)
	}
	if v, ok := doc["ignore"]; ok {
		t, isTable := v.(map[string]any)
		if !isTable {
			return c, errors.New("watch.toml: [ignore] must be a table")
		}
		if c.IgnoreAuthors, c.IgnoreLabels, err = ignoreLists(t); err != nil {
			return c, err
		}
	}
	if v, ok := doc["shape"]; ok {
		t, isTable := v.(map[string]any)
		if !isTable {
			return c, errors.New("watch.toml: [shape] must be a table")
		}
		for _, k := range sortedKeys(t) {
			if k != "stack_patterns" {
				return c, fmt.Errorf("watch.toml: unknown key %q in [shape]", k)
			}
		}
		if p, ok := t["stack_patterns"]; ok {
			if c.StackPatterns, err = stringList(p, "shape.stack_patterns"); err != nil {
				return c, err
			}
			for _, s := range c.StackPatterns {
				if s == "" {
					return c, errors.New("watch.toml: shape.stack_patterns must not contain an empty string")
				}
			}
		}
	}
	return c, nil
}

func ignoreLists(t map[string]any) (authors, labels []string, err error) {
	for _, k := range sortedKeys(t) {
		if k != "authors" && k != "labels" {
			return nil, nil, fmt.Errorf("watch.toml: unknown key %q in [ignore]", k)
		}
	}
	if v, ok := t["authors"]; ok {
		if authors, err = stringList(v, "ignore.authors"); err != nil {
			return nil, nil, err
		}
	}
	if v, ok := t["labels"]; ok {
		if labels, err = stringList(v, "ignore.labels"); err != nil {
			return nil, nil, err
		}
	}
	return authors, labels, nil
}

// ParseInterval parses a Go duration of at least MinInterval.
func ParseInterval(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration such as 60s", s)
	}
	if d < MinInterval {
		return 0, fmt.Errorf("%s is below the minimum of %s", s, MinInterval)
	}
	return d, nil
}

func stringList(v any, name string) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("watch.toml: %s must be an array of strings", name)
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("watch.toml: %s must be an array of strings", name)
		}
		out = append(out, s)
	}
	return out, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dedupe removes repeated repository names (compared case-insensitively), keeping the first spelling and the order.
func dedupe(repos []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range repos {
		if k := strings.ToLower(r); !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	return out
}
