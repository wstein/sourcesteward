// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestPlanPrintsTheMatchingApplyCallAndAppliesNothing(t *testing.T) {
	c, f, o := fixtureCoordinator(t)
	r := c.plan(context.Background(), o)
	if r.Status != "ok" || r.Next == nil {
		t.Fatalf("plan %+v", r)
	}
	want := []string{"stw", "workspace", "apply", "--plan", r.Plan.PlanID, "--state-dir", o.StateDir, "--authorize"}
	if !reflect.DeepEqual(r.Next.Argv, want) {
		t.Fatalf("argv %q want %q", r.Next.Argv, want)
	}
	if r.Next.Command != strings.Join(want, " ") {
		t.Fatalf("command %q", r.Next.Command)
	}
	if f.creates != 0 {
		t.Fatal("planning applied something")
	}
}

func TestNextCallQuotesArgumentsForTheShell(t *testing.T) {
	got := nextCall("id1", "/tmp/my state/it's").Command
	want := `stw workspace apply --plan id1 --state-dir '/tmp/my state/it'\''s' --authorize`
	if got != want {
		t.Fatalf("command %q want %q", got, want)
	}
}

func TestRefusedPlanOffersNoNextCall(t *testing.T) {
	c, _, o := fixtureCoordinator(t)
	o.Task = ""
	if r := c.plan(context.Background(), o); r.Status == "ok" || r.Next != nil {
		t.Fatalf("refused plan carries a call: %+v", r)
	}
}

func TestNextCallQuotesWithSingleQuotesOnlySoThatPOSIXShIsEnough(t *testing.T) {
	// Each argument is one word for sh and dash: no $'...' form, a quote becomes '\''.
	c := nextCall("id1", "/tmp/a'b\\c d")
	want := `stw workspace apply --plan id1 --state-dir '/tmp/a'\''b\c d' --authorize`
	if c.Command != want || strings.Contains(c.Command, "$'") {
		t.Fatalf("command %q want %q", c.Command, want)
	}
}

func TestNextCallOffersNoTextForUnprintableArguments(t *testing.T) {
	for _, dir := range []string{"/tmp/a\nb", "/tmp/a\x1bb", "/tmp/a\u0085b", "/tmp/a\u202eb", "/tmp/a\u200fb", "/tmp/a\x7fb"} {
		c := nextCall("id1", dir)
		if c.Command != "" || len(c.Argv) != 8 || c.Argv[6] != dir {
			t.Fatalf("%q: %+v", dir, c)
		}
	}
	if c := nextCall("id1", "/tmp/caf\u00e9"); c.Command == "" || !strings.Contains(c.Command, "'/tmp/caf\u00e9'") {
		t.Fatalf("printable non-ASCII refused: %+v", c)
	}
}

func TestBoundedResultDropsTheNextCallWithThePlan(t *testing.T) {
	r := newResult("workspace-plan")
	r.Status = "ok"
	r.Plan = &MutationPlan{PlanID: "p", StateDir: "/s"}
	r.Next = nextCall("p", "/s")
	for i := 0; i < observationLimit+1; i++ {
		r.Actions = append(r.Actions, ActionOutcome{"a", "completed", "d"})
	}
	if got := bounded(r); got.Next != nil || got.Plan != nil || got.Status != "partial" {
		t.Fatalf("bounded kept the call of a dropped plan: %+v", got)
	}
}

// Every shell metacharacter must be quoted, and the quoted text must parse back to exactly
// argv in a POSIX sh. The unquoted set is pinned by the positive control.
func TestNextCallQuotesEveryShellMetacharacter(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	parse := func(command string) []string {
		out, err := exec.Command(sh, "-c", `for a in `+command+`; do printf '%s\0' "$a"; done`).Output()
		if err != nil {
			t.Fatalf("sh: %v (%s)", err, command)
		}
		return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	}
	for _, c := range []string{" ", "$", ";", "`", "*", "?", "[", "]", "~", "!", `"`, `\`, "&", "|", "<", ">", "(", ")", "{", "}", "#", "^", "'", "\t"} {
		for _, id := range []string{"/tmp/x" + c + "y", c + "lead", "trail" + c} {
			call := nextCall("id1", id)
			if call.Command == "" {
				if c == "\t" {
					continue // not printable: text withheld
				}
				t.Fatalf("%q: no text", id)
			}
			if !strings.Contains(call.Command, "'") {
				t.Fatalf("%q left unquoted: %s", id, call.Command)
			}
			if got := parse(call.Command); !reflect.DeepEqual(got, call.Argv) {
				t.Fatalf("%q parses to %q want %q", id, got, call.Argv)
			}
		}
	}
	if c := nextCall("a.b_c-1", "/tmp/a-b_c.d/e=f:g@h%i+j,k").Command; strings.Contains(c, "'") {
		t.Fatalf("shell-safe characters were quoted: %s", c)
	}
}
