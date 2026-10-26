// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/analysis"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
)

// The online decision of the wizard (Spec 044 D13): online is the default of the human wizard
// only. `--offline` turns it off, an explicit `--online` is a flag as before, and every other
// context (`stw tidy plan`, `--json`, `--save`, agent mode, the expert layer) stays offline unless
// `--online` is given.

// The values of the derived `online` entry.
const (
	tidyOnlineDefault = "default"
	tidyOnlineFlag    = "flag"
	tidyOnlineOff     = "off"
)

// tidyOnlineChoice is the outcome of the decision. On says whether the observation and the pull
// request evidence read online; Value is empty outside the human wizard, where no derived entry
// is added (so the offline outputs stay byte for byte as they were).
type tidyOnlineChoice struct {
	On            bool
	Value, Source string
}

// resolveTidyOnline decides from the two flags and from whether this run is the human wizard (the
// interactive entry point, on terminals, no agent marker, no JSON). --online with --offline is a
// usage error.
func resolveTidyOnline(online, offline, humanWizard bool) (tidyOnlineChoice, error) {
	if online && offline {
		return tidyOnlineChoice{}, errors.New("--online and --offline exclude each other")
	}
	switch {
	case humanWizard && offline:
		return tidyOnlineChoice{Value: tidyOnlineOff, Source: "--offline"}, nil
	case humanWizard && online:
		return tidyOnlineChoice{On: true, Value: tidyOnlineFlag, Source: "--online"}, nil
	case humanWizard:
		return tidyOnlineChoice{On: true, Value: tidyOnlineDefault, Source: "human wizard"}, nil
	}
	return tidyOnlineChoice{On: online}, nil
}

// tidyOnlineFetch reads the pull request evidence of the open tips; tests replace it.
var tidyOnlineFetch = func(ctx context.Context, members []tidywizard.OnlineMember, cands []tidywizard.Candidate, budget *hosting.TidyRunBudget) tidywizard.OnlineOutcome {
	return tidywizard.FetchPREvidence(ctx, members, cands, tidywizard.OnlineOptions{Budget: budget})
}

// tidyOnlineMembers lists the analysed Git repositories that are read online.
func tidyOnlineMembers(prep analysis.Prepared, res tidywizard.Result) []tidywizard.OnlineMember {
	analysed := map[string]bool{}
	for _, r := range res.Repositories {
		if r.State == "analysed" {
			analysed[r.Name] = true
		}
	}
	var out []tidywizard.OnlineMember
	for _, s := range prep.Specs {
		if s.Skip == "" && s.Error == "" && s.Online && analysed[s.Name] && prep.Meta[s.Name].Backend == "git" {
			out = append(out, tidywizard.OnlineMember{Name: s.Name, Path: s.Path, Remote: s.Remote})
		}
	}
	return out
}

// tidyOnlineSummary combines what the observation and the pull request reads report into the one
// notice line and the derived `online` entry. A repository that fell back anywhere counts once.
func tidyOnlineSummary(choice tidyOnlineChoice, members []tidywizard.OnlineMember, res tidywizard.Result, out tidywizard.OnlineOutcome) (notice string, entry derive.Derived) {
	reasons := map[string]string{}
	for _, r := range res.Repositories {
		if r.OnlineFallback != "" {
			reasons[r.Name] = r.OnlineFallback
		}
	}
	for _, m := range out.Members {
		if m.State == tidywizard.MemberOffline {
			if _, seen := reasons[m.Name]; !seen {
				reasons[m.Name] = m.Reason
			}
		}
	}
	of := len(members)
	// A repository that fell back in the observation but is not in the list of members still counts.
	for name := range reasons {
		found := false
		for _, m := range members {
			if m.Name == name {
				found = true
			}
		}
		if !found {
			of++
		}
	}
	var words []string
	seen := map[string]bool{}
	all := make([]string, 0, len(reasons)+1)
	for _, r := range reasons {
		all = append(all, r)
	}
	// A stop of the run (a rate limit seen by the tracking reads, the cap) is the cause of the
	// repositories that failed with it.
	if stop := tidywizard.StopReason(out.Stopped); stop != "" && len(reasons) > 0 {
		all = append(all, stop)
	}
	for _, r := range all {
		if !seen[r] {
			seen[r] = true
			words = append(words, r)
		}
	}
	sort.Strings(words)
	notice = tidywizard.FormatOnlineNotice(of-len(reasons), of, words)
	entry = derive.Derived{Name: "online", Value: choice.Value, Source: choice.Source, Overridden: choice.Value == tidyOnlineFlag || choice.Value == tidyOnlineOff}
	switch {
	case of > 0 && len(reasons) == of:
		entry.Value, entry.Source = tidyOnlineOff, "fallback: "+strings.Join(words, ", ")
	case len(reasons) > 0:
		entry.Source = fmt.Sprintf("%s; offline for %d of %d repositories (%s)", choice.Source, len(reasons), of, strings.Join(words, ", "))
	}
	return notice, entry
}
