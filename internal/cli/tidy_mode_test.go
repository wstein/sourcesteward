// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// Spec 041 T015: every flag registered by the tidy plan and apply commands is classified as
// wizard-only, shared or expert; a flag nobody classified fails here, so a new flag cannot slip
// into a layer by accident.
func TestEveryTidyFlagIsClassified(t *testing.T) {
	root := tidyCommand()
	for _, name := range []string{"plan", "apply"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "help" {
				return
			}
			if _, ok := tidyFlagClasses[f.Name]; !ok {
				t.Errorf("tidy %s: flag --%s is not classified as wizard, shared or expert", name, f.Name)
			}
		})
	}
	// No stale entry: every classified flag exists on plan or apply.
	known := map[string]bool{}
	for _, name := range []string{"plan", "apply"} {
		cmd, _, _ := root.Find([]string{name})
		cmd.Flags().VisitAll(func(f *pflag.Flag) { known[f.Name] = true })
	}
	var stale []string
	for name := range tidyFlagClasses {
		if !known[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("classified but not registered: %v", stale)
	}
}

// The classes of the specification, flag by flag.
func TestTidyFlagClasses(t *testing.T) {
	for _, name := range []string{"workspace", "mine", "by-owner", "member", "member-base", "only", "save", "yes"} {
		if tidyFlagClasses[name] != flagWizard {
			t.Errorf("--%s must be wizard-only", name)
		}
	}
	// --human is shared (Spec 041 CLI surface): valid in both layers, a plain text request.
	for _, name := range []string{"state-dir", "online", "timeout", "json", "human"} {
		if tidyFlagClasses[name] != flagShared {
			t.Errorf("--%s must be shared", name)
		}
	}
	for _, name := range []string{"base", "ref", "task", "owner", "remove-registration", "remove-files", "completed-worktree", "protect-worktree",
		"contract", "remote", "tracking-remote", "repo", "host", "pr", "integration-proof", "accept-whitespace-differences", "decisions",
		"consolidation-range", "report-dir", "report-max-entries", "report-max-bytes", "report-max-diff-bytes", "report-max-files",
		"report-max-lines", "report-max-commits", "plan", "authorize"} {
		if tidyFlagClasses[name] != flagExpert {
			t.Errorf("--%s must be expert", name)
		}
	}
}

// run executes the tidy command with a fake expert plan; it reports the plan options when the
// expert layer ran.
func runMode(args ...string) (err error, expertRan bool, got workspace.TidyPlanOptions) {
	cmd := tidyCommandWith(func(_ context.Context, o workspace.TidyPlanOptions) workspace.TidyResult {
		expertRan, got = true, o
		return workspace.TidyResult{SchemaVersion: 1, Command: "tidy-plan", Status: "ok", Backend: "git"}
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err = cmd.Execute()
	return
}

func TestTidyPlanLayerSelection(t *testing.T) {
	type row struct {
		args   []string
		layer  string // expert, wizard (not available yet: a usage error), usage
		expect string // a fragment of the usage error
	}
	rows := []row{
		{[]string{"plan", "--base", "HEAD"}, "expert", ""},
		{[]string{"plan", "--base", "HEAD", "--json"}, "expert", ""},
		{[]string{"plan", "/repo", "--base", "HEAD"}, "expert", ""},
		{[]string{"plan", "/repo", "--base", "HEAD", "--state-dir", "/s"}, "expert", ""},
		{[]string{"plan", "/repo"}, "usage", "--base is required"},
		{[]string{"plan"}, "wizard", ""},
		{[]string{"plan", "--state-dir", "/s"}, "wizard", ""},
		{[]string{"plan", "--json"}, "wizard", ""},
		{[]string{"plan", "--mine"}, "wizard", ""},
		{[]string{"plan", "--by-owner", "codex", "--by-owner", "claude", "--member", "a", "--only", "retire", "--save", "--human"}, "wizard", ""},
		{[]string{"plan", "--member-base", "a=refs/heads/trunk", "--workspace", "/w", "--online", "--timeout", "5s"}, "wizard", ""},
		{[]string{"plan", "--mine", "--base", "HEAD"}, "usage", "cannot be combined"},
		{[]string{"plan", "--save", "--task", "t"}, "usage", "cannot be combined"},
		{[]string{"plan", "--member", "a", "--repo", "r"}, "usage", "cannot be combined"},
		{[]string{"plan", "--by-owner", "x", "--owner", "o"}, "usage", "cannot be combined"},
		{[]string{"plan", "--mine", "--contract", "3"}, "usage", "cannot be combined"},
		{[]string{"plan", "/repo", "--mine"}, "usage", "cannot be used with a PATH"},
		{[]string{"plan", "/repo", "--save"}, "usage", "cannot be used with a PATH"},
		{[]string{"plan", "--json", "--human"}, "usage", "exclude each other"},
		{[]string{"plan", "--yes"}, "usage", "unknown flag"},
		{[]string{"plan", "--only", "retire,nonsense"}, "usage", "--only"},
		{[]string{"plan", "--only", ""}, "usage", "--only"},
		{[]string{"plan", "--member-base", "no-equals"}, "usage", "--member-base"},
		{[]string{"plan", "--by-owner", ""}, "usage", "--by-owner"},
		{[]string{"plan", "--member", ""}, "usage", "--member needs"},
		{[]string{"plan", "--workspace", ""}, "usage", "--workspace needs"},
		{[]string{"plan", "--base", "HEAD", "--human"}, "expert", ""},
		{[]string{"plan", "--human", "--base", "HEAD", "--json"}, "usage", "exclude each other"},
	}
	for _, r := range rows {
		w := newWizardWorld(t, true, true, "STW_OWNER=claude")
		err, ran, _ := runMode(r.args...)
		switch r.layer {
		case "expert":
			if err != nil || !ran {
				t.Errorf("%q: want the expert layer, err=%v ran=%v", r.args, err, ran)
			}
		case "wizard":
			// The wizard layer ran (against the stubbed analysis); the expert layer never did.
			if err != nil || ran || w.prepared != 1 {
				t.Errorf("%q: want the wizard layer, err=%v ran=%v prepared=%d", r.args, err, ran, w.prepared)
			}
		default:
			if !isUsage(err) || ran || !strings.Contains(err.Error(), r.expect) {
				t.Errorf("%q: want a usage error containing %q, err=%v ran=%v", r.args, r.expect, err, ran)
			}
		}
	}
}

// An expert invocation that succeeds today is byte-identical: the options reach the seam unchanged.
func TestTidyExpertPlanOptionsAreUnchanged(t *testing.T) {
	err, ran, o := runMode("plan", "--base", "HEAD", "--state-dir", "/s", "--ref", "r1")
	if err != nil || !ran || o.Base != "HEAD" || o.StateDir != "/s" || len(o.Refs) != 1 || o.Path != "." {
		t.Fatalf("%v %v %+v", err, ran, o)
	}
}

// --member and --by-owner are separate flags: their values never reach --repo or --owner.
func TestWizardFlagsDoNotCollideWithExpertOnes(t *testing.T) {
	cmd := tidyCommand()
	plan, _, _ := cmd.Find([]string{"plan"})
	for _, pair := range [][2]string{{"member", "repo"}, {"by-owner", "owner"}} {
		a, b := plan.Flags().Lookup(pair[0]), plan.Flags().Lookup(pair[1])
		if a == nil || b == nil || a == b {
			t.Fatalf("%v: %v %v", pair, a, b)
		}
	}
	if err := plan.Flags().Set("by-owner", "codex"); err != nil {
		t.Fatal(err)
	}
	if v := plan.Flags().Lookup("owner").Value.String(); v != "" {
		t.Errorf("--by-owner set --owner to %q", v)
	}
	if err := plan.Flags().Set("member", "m"); err != nil {
		t.Fatal(err)
	}
	if v := plan.Flags().Lookup("repo").Value.String(); v != "" {
		t.Errorf("--member set --repo to %q", v)
	}
}

func TestTidyApplyLayerSelection(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		usage  bool
		expect string
	}{
		{[]string{"apply", setID, "--yes", "--json"}, false, ""},
		{[]string{"apply", setID, "--human"}, false, ""},
		{[]string{"apply", setID, "--json", "--human"}, true, "exclude each other"},
		{[]string{"apply", setID, "--plan", "x"}, true, "cannot be combined"},
		{[]string{"apply", setID, "--mine"}, true, "unknown flag"},
		{[]string{"apply", "--plan", "x", "--yes", "--state-dir", "/s"}, true, ""},
		{[]string{"apply", "--human"}, true, "--plan is required"},
		{[]string{"apply", "--plan", "x", "--state-dir", "/s", "--human", "--json"}, true, "exclude each other"},
	} {
		got := fakeSet(t, okSet(), false, false)
		_, err := runTidy(t, "", tc.args...)
		if tc.usage {
			if !isUsage(err) || len(*got) != 0 || !strings.Contains(err.Error(), tc.expect) {
				t.Errorf("%q: want a usage error containing %q, err=%v calls=%d", tc.args, tc.expect, err, len(*got))
			}
			continue
		}
		if err != nil || len(*got) != 1 {
			t.Errorf("%q: err=%v calls=%d", tc.args, err, len(*got))
		}
	}
}
