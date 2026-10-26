// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/wstein/sourcesteward/internal/tidywizard/analysis"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

// Spec 041 T015, bare `stw tidy`: every flag the family command registers is classified.
func TestEveryBareTidyFlagIsClassified(t *testing.T) {
	registered := map[string]bool{}
	tidyCommand().Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		registered[f.Name] = true
		switch tidyFlagClass(tidyCommand(), f.Name) {
		case flagWizard, flagShared:
		default:
			t.Errorf("bare tidy: --%s is not a wizard or shared flag", f.Name)
		}
	})
	for _, name := range []string{"workspace", "mine", "by-owner", "member", "member-base", "only", "state-dir", "online", "timeout", "json", "human", "contract"} {
		if !registered[name] {
			t.Errorf("bare tidy does not accept --%s", name)
		}
	}
}

// The wizard flags reach the analysis of the bare wizard, with every --state-dir in order.
func TestBareTidyFlagsReachTheAnalysis(t *testing.T) {
	w := newInteractive(t, true, true)
	_, err := runTidy(t, "\n", "--workspace", "/other", "--member", "a", "--member", "b", "--member-base", "a=refs/heads/trunk",
		"--state-dir", "/s/one", "--state-dir", "/s/two", "--online", "--timeout", "30s")
	if err != nil {
		t.Fatal(err)
	}
	want := analysis.Options{Workspace: "/other", CWD: "/cwd", Members: []string{"a", "b"}, MemberBases: []string{"a=refs/heads/trunk"}, StateDirs: []string{"/s/one", "/s/two"}, Online: true}
	if !reflect.DeepEqual(w.gotOpt, want) || !w.hadDline {
		t.Errorf("got %+v deadline %v\nwant %+v", w.gotOpt, w.hadDline, want)
	}
	w = newInteractive(t, true, true)
	// Spec 044 D13: the human wizard (both streams on terminals) is online by default; only the
	// flag value of an explicit --online is not the default.
	if _, err = runTidy(t, "\n"); err != nil || w.hadDline || !w.gotOpt.Online || !w.gotOpt.OnlineDefault {
		t.Errorf("defaults: %v %+v", err, w.gotOpt)
	}
}

// Usage errors (exit 2): bad values, mutually exclusive output flags, expert flags, a PATH, and the
// plan-only and apply-only wizard flags; nothing is analysed.
func TestBareTidyUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"json and human":     {"--json", "--human"},
		"bad only":           {"--only", "nonsense"},
		"empty member":       {"--member", ""},
		"bad member-base":    {"--member-base", "noequals"},
		"empty workspace":    {"--workspace", ""},
		"zero timeout":       {"--timeout", "0s"},
		"expert base":        {"--base", "refs/heads/main"},
		"expert with wizard": {"--mine", "--task", "T"},
		"path":               {"/some/path"},
		"as":                 {"--as", "x"},
		"yes needs an id":    {"--yes"},
		"save is plan only":  {"--save"},
		"yes false":          {"--yes=false"},
		"save false":         {"--save=false"},
	} {
		w := newInteractive(t, true, true)
		_, err := runTidy(t, "a\ny\n", args...)
		if !isUsage(err) || w.prepared != 0 || len(w.applied) != 0 {
			t.Errorf("%s: err %v prepared %d applied %d", name, err, w.prepared, len(w.applied))
		}
	}
	w := newInteractive(t, true, true)
	_, err := runTidy(t, "", "--yes")
	if !isUsage(err) || !strings.Contains(err.Error(), "plan id") || w.prepared != 0 {
		t.Errorf("--yes names the missing id: %v", err)
	}
}

// --mine and --by-owner narrow the view; --mine selects agent mode, so no prompt is read.
func TestBareTidyMineAndByOwner(t *testing.T) {
	i := newInteractive(t, true, true, "STW_OWNER=claude")
	out, err := runTidy(t, "a\ny\n", "--mine")
	if err != nil || strings.Contains(out, "Apply which?") || len(i.applied) != 0 || len(storedSets(t, i.prep.PlanStore)) != 0 || i.gotCfg.Agent != true {
		t.Errorf("--mine: %v agent %v\n%s", err, i.gotCfg.Agent, out)
	}
	if strings.Contains(out, "whr1") {
		t.Errorf("--mine lists another owner:\n%s", out)
	}
	i = newInteractive(t, true, true)
	out, err = runTidy(t, "\n", "--by-owner", "codex")
	if err != nil || !strings.Contains(out, "whr1") || strings.Contains(out, "own1") || i.gotCfg.Agent {
		t.Errorf("--by-owner stays human and narrows: %v\n%s", err, out)
	}
	if _, err = runTidy(t, "\n", "--mine"); !isUsage(err) {
		t.Errorf("--mine without a caller label is a usage error: %v", err)
	}
}

// --only narrows the kinds in the bare wizard.
func TestBareTidyOnly(t *testing.T) {
	newInteractive(t, true, true)
	out, err := runTidy(t, "\n", "--only", "branches")
	if err != nil || !strings.Contains(out, "claude/old") || strings.Contains(out, "own1") {
		t.Errorf("%v\n%s", err, out)
	}
}

// --json prints the one overview document, never prompts, stores nothing and applies nothing.
func TestBareTidyJSONNeverPromptsOrStores(t *testing.T) {
	i := newInteractive(t, true, true)
	out, err := runTidy(t, "a\ny\n", "--json")
	if err != nil {
		t.Fatal(err)
	}
	d := docOf(t, out)
	if !verdicts(d)["worktree:a:own1"] || strings.Contains(out, "Apply which?") || d.PlanID != "" || len(i.applied) != 0 || len(storedSets(t, i.prep.PlanStore)) != 0 || d.Mode.Mode != owner.ModeHuman {
		t.Errorf("%+v\n%s", d.Mode, out)
	}
}

// --human forces the text overview, with the prompt on a terminal.
func TestBareTidyHumanIsText(t *testing.T) {
	newInteractive(t, true, true)
	out, err := runTidy(t, "\n", "--human")
	if err != nil || !strings.Contains(out, "Apply which?") {
		t.Errorf("%v\n%s", err, out)
	}
}

// The expert subcommands do not accept the wizard-only flags.
func TestExpertSubcommandsRejectWizardOnlyFlags(t *testing.T) {
	for _, sub := range [][]string{{"restore"}, {"history"}} {
		for _, flag := range []string{"--workspace", "--mine", "--by-owner", "--member", "--member-base", "--only", "--save", "--yes"} {
			args := append(append([]string{}, sub...), flag)
			if flag != "--mine" && flag != "--save" && flag != "--yes" {
				args = append(args, "x")
			}
			if _, err := runTidy(t, "", args...); !isUsage(err) {
				t.Errorf("%v: %v", args, err)
			}
		}
	}
	for _, flag := range []string{"--workspace", "--mine", "--by-owner", "--member", "--member-base", "--only", "--save"} {
		args := []string{"apply", "--plan", "x", flag}
		if flag != "--mine" && flag != "--save" {
			args = append(args, "x")
		}
		if _, err := runTidy(t, "", args...); !isUsage(err) {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestTidyContractFlagClassDependsOnCommand(t *testing.T) {
	cmd := tidyCommand()
	if tidyFlagClass(cmd, "contract") != flagShared {
		t.Fatal("bare contract selector must not be expert")
	}
	for _, name := range []string{"plan", "apply"} {
		sub, _, err := cmd.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		if tidyFlagClass(sub, "contract") != flagExpert {
			t.Fatalf("%s contract must keep expert dispatch", name)
		}
	}
}
