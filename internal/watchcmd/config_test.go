// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const fullConfig = `schema_version = 1
repos = ["wstein/crewbook", "wstein/sourcesteward"]
interval = "90s"
detail_every = 4
max_open_prs = 500
[ignore]
authors = ["dependabot[bot]"]      # no events at all
labels  = ["no-watch"]
[shape]
stack_patterns = ["PR must target repository main"]
`

func TestParseConfigReadsEveryKey(t *testing.T) {
	c, err := ParseConfig([]byte(fullConfig))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Repos: []string{"wstein/crewbook", "wstein/sourcesteward"}, Interval: 90 * time.Second, DetailEvery: 4, MaxOpenPRs: 500,
		IgnoreAuthors: []string{"dependabot[bot]"}, IgnoreLabels: []string{"no-watch"}, StackPatterns: []string{"PR must target repository main"}}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("%+v", c)
	}
	min, err := ParseConfig([]byte("schema_version = 1\n"))
	if err != nil || min.MaxOpenPRs != 0 || min.Interval != 0 || len(min.Repos) != 0 {
		t.Fatalf("%+v %v", min, err)
	}
}

func TestParseConfigIsStrict(t *testing.T) {
	cases := map[string]string{
		"no schema_version":    "repos = [\"a/b\"]\n",
		"wrong version":        "schema_version = 2\n",
		"unknown key":          "schema_version = 1\nbogus = 1\n",
		"unknown ignore key":   "schema_version = 1\n[ignore]\nbots = [\"x\"]\n",
		"unknown shape key":    "schema_version = 1\n[shape]\ncheck_names = [\"x\"]\n",
		"unknown table":        "schema_version = 1\n[other]\na = 1\n",
		"duplicate key":        "schema_version = 1\nschema_version = 1\n",
		"repos wrong type":     "schema_version = 1\nrepos = \"a/b\"\n",
		"bad repo":             "schema_version = 1\nrepos = [\"a\"]\n",
		"interval not string":  "schema_version = 1\ninterval = 60\n",
		"interval too short":   "schema_version = 1\ninterval = \"5s\"\n",
		"interval garbage":     "schema_version = 1\ninterval = \"soon\"\n",
		"detail_every zero":    "schema_version = 1\ndetail_every = 0\n",
		"max_open_prs zero":    "schema_version = 1\nmax_open_prs = 0\n",
		"max_open_prs over":    "schema_version = 1\nmax_open_prs = 2001\n",
		"empty pattern":        "schema_version = 1\n[shape]\nstack_patterns = [\"\"]\n",
		"authors wrong type":   "schema_version = 1\n[ignore]\nauthors = 1\n",
		"invalid toml profile": "schema_version = 1\nrepos = [\n",
	}
	for name, doc := range cases {
		if _, err := ParseConfig([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseConfig([]byte("schema_version = 1\nrepos = [\"a\"]\n")); err == nil || !strings.Contains(err.Error(), "OWNER/NAME") {
		t.Fatalf("message %v", err)
	}
}
