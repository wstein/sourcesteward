// SPDX-License-Identifier: EUPL-1.2

package conflicts

import (
	"fmt"
	"strings"
	"unicode"
)

// Step restacks one pull request. Rebase and Verify may run at once; Push rewrites the remote head
// and runs only after the independent review of the new head. The plan is text: nothing here runs.
type Step struct {
	Number  int
	HeadRef string
	OldHead string
	Why     string   // StateConflicts or "child of #n, restacked after it"
	Skip    string   // set when no command is offered; the reason
	Rebase  []string // fetch, switch, rebase --onto
	Verify  []string // range-diff old against new
	Push    string   // push with --force-with-lease=<ref>:<old head>
}

// Plan is the restack plan of one stack that contains a conflict, with the brief for the agent.
type Plan struct {
	Repo    string
	Remote  string
	Root    int
	Steps   []Step
	GhStack []string // the same restack with gh stack, for pull requests that form a gh stack
	Brief   string   // Markdown
}

// BuildPlans returns one plan per stack that contains a pull request conflicting with its base: the
// lowest conflicting level and every level above it, bottom first. Levels below it are not touched.
// The commands use `git rebase --onto <new> <old>` with the old SHAs from the probe, so they are
// valid only while the heads and the base are unchanged; the brief says so.
func BuildPlans(repo, remote string, nodes []Node) []Plan {
	byNumber := map[int]Node{}
	for _, n := range nodes {
		byNumber[n.Number] = n
	}
	var plans []Plan
	for _, st := range BuildStacks(nodes) {
		p := Plan{Repo: repo, Remote: remote, Root: st.Root}
		inPlan := map[int]bool{}
		for _, l := range st.Levels {
			n := byNumber[l.Number]
			parentIn := l.Parent != 0 && inPlan[l.Parent]
			if !parentIn && n.Finding.State != StateConflicts {
				continue
			}
			inPlan[l.Number] = true
			step := Step{Number: n.Number, HeadRef: n.HeadRef, OldHead: n.Head, Why: StateConflicts}
			onto, old := "origin/"+n.Base, n.Finding.MergeBase
			if parentIn {
				pn := byNumber[l.Parent]
				step.Why = fmt.Sprintf("child of #%d, restacked after it", l.Parent)
				onto, old = pn.HeadRef, pn.Head
			}
			p.Steps = append(p.Steps, finishStep(step, remote, n, onto, old, parentIn))
		}
		if len(p.Steps) == 0 {
			continue
		}
		if len(st.Levels) > 1 {
			p.GhStack = []string{fmt.Sprintf("gh stack checkout %d", st.Root), "gh stack rebase", "gh stack push"}
		}
		p.Brief = brief(p, byNumber)
		plans = append(plans, p)
	}
	return plans
}

func finishStep(s Step, remote string, n Node, onto, old string, parentIn bool) Step {
	switch {
	case n.Fork:
		s.Skip = "fork pull request: the head branch is not in this repository"
	case !ValidBranch(n.HeadRef) || !ValidBranch(n.Base) || !ValidSHA(n.Head) || !ValidSHA(old) || !remoteName.MatchString(remote) || (parentIn && !ValidBranch(onto)):
		s.Skip = "a branch name or commit is not safe to put in a command; restack by hand"
	}
	if s.Skip != "" {
		return s
	}
	s.Rebase = []string{
		"git fetch " + remote,
		"git switch " + n.HeadRef,
		fmt.Sprintf("git rebase --onto %s %s %s", onto, old, n.HeadRef),
	}
	s.Verify = []string{fmt.Sprintf("git range-diff %s..%s %s", old, n.Head, onto+".."+n.HeadRef)}
	s.Push = fmt.Sprintf("git push --force-with-lease=%s:%s %s %s", n.HeadRef, n.Head, remote, n.HeadRef)
	return s
}

// code quotes untrusted text as inline code: backticks and every control, format, line and paragraph
// separator character (C0, DEL, C1, bidi controls, U+2028, U+2029) are replaced, because the brief is
// printed to the terminal as it is.
func code(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '`':
			b.WriteRune('\'')
		case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp):
			b.WriteRune('?')
		default:
			b.WriteRune(r)
		}
	}
	return "`" + b.String() + "`"
}

func brief(p Plan, nodes map[int]Node) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	var numbers []string
	for _, s := range p.Steps {
		numbers = append(numbers, fmt.Sprintf("#%d", s.Number))
	}
	w("# Restack brief: %s %s", p.Repo, strings.Join(numbers, ", "))
	w("")
	w("The base of these pull requests changed after they were reviewed. They no longer merge with it, so each has a")
	w("new head after the restack and needs a new review. This brief is generated from a read-only probe; the SHAs")
	w("below are valid only while the heads and the base are unchanged. Run `git fetch` and compare before you start.")
	w("")
	w("## Conflicts with the base")
	for _, s := range p.Steps {
		n := nodes[s.Number]
		if n.Finding.State != StateConflicts {
			continue
		}
		w("- #%d (%s, head %s) conflicts with %s at %s (merge base %s):", n.Number, code(n.HeadRef), code(n.Head), code(n.Base), code(n.Finding.BaseSHA), code(n.Finding.MergeBase))
		for _, f := range n.Finding.Files {
			last := "no base commit found"
			if f.BaseCommit != "" {
				last = "last changed on the base by " + code(f.BaseCommit)
			}
			w("  - %s, %s", code(f.Path), last)
		}
		if n.Finding.Truncated {
			w("  - more files than listed")
		}
	}
	w("")
	w("## Order")
	for i, s := range p.Steps {
		w("%d. #%d %s: %s", i+1, s.Number, code(s.HeadRef), s.Why)
	}
	w("")
	w("## Commands")
	w("Run per pull request, bottom first. Push only after the review below.")
	for _, s := range p.Steps {
		w("")
		w("#%d (%s)", s.Number, s.Why)
		w("```sh")
		if s.Skip != "" {
			w("# no commands: %s", s.Skip)
		}
		for _, c := range s.Rebase {
			w("%s", c)
		}
		for _, c := range s.Verify {
			w("%s", c)
		}
		if s.Push != "" {
			w("# after the review of the new head:")
			w("%s", s.Push)
		}
		w("```")
	}
	if len(p.GhStack) > 0 {
		w("")
		w("If these pull requests are a gh stack, `gh stack` does the same cascade: `%s`. Check the result with range-diff", strings.Join(p.GhStack, "`, `"))
		w("and push with the lease from above if it does not set one.")
	}
	w("")
	w("## Required checks")
	w("- Take over the conflict wording of the base unchanged: where the base changed text, keep the base text as it stands and re-apply only the pull request's own change on top. Do not rephrase it.")
	w("- Compare old and new with the `git range-diff` lines above. Only the conflict resolution may differ.")
	w("- Run the tests and checks of the repository on the new head.")
	w("- Put each restacked pull request back to Draft (`gh pr ready <number> --undo`) until its new head is reviewed.")
	w("- Author and committer of every new commit are only the owner: `git log --format='%%an <%%ae> | %%cn <%%ce>' origin/<base>..HEAD`.")
	w("- No session link and no session trailer in commits, pull request text or comments.")
	w("- A new head needs a new independent review before the push and before Ready; use the strong model when the change touches rule, contract or security text.")
	w("- Push only your own branch, only with the lease from the commands, never a plain force.")
	return b.String()
}
