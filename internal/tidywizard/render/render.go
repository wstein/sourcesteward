// SPDX-License-Identifier: EUPL-1.2

// Package render builds the tidy-overview document of Spec 041 from a classification and renders
// it as text and JSON. It is pure: it reads nothing and writes nothing. Every string that comes
// from outside the program (paths, branch names, tasks, owners, evidence, notices) passes the
// shared `esc` and its 120-character bound in the text; JSON is encoded by encoding/json.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// Bounds of the overview.
const (
	// MaxDocumentBytes bounds the JSON document (Spec 041 JSON section).
	MaxDocumentBytes = 4 << 20
	// MaxRowsPerRepository is the number of rows the text shows per repository inside each owner group.
	MaxRowsPerRepository = 10
)

// RepoMeta is what the overview shows about a repository besides its analysis result.
type RepoMeta struct {
	Path, Backend, Base, Remote string
}

// Input is everything Build needs.
type Input struct {
	Workspace string
	Derived   []derive.Derived
	Repos     map[string]RepoMeta
	Result    tidywizard.Result
	Mode      owner.Evidence
	PlanID    string
	// Notices are run-level notices; the notices of each repository are added by Build.
	Notices []string
	// Now is the clock of the run. When it is set, Build adds the risk fields of Spec 044 to the
	// candidates and the counts per level to the summary; without it the document is the one of
	// Spec 041, byte for byte.
	Now time.Time
}

// Repository is the repositories[] entry of the document.
type Repository struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Backend string `json:"backend"`
	Base    string `json:"base"`
	Remote  string `json:"remote"`
	State   string `json:"state"`
	Reason  string `json:"reason"`
}

// Summary counts the candidates per verdict.
type Summary struct {
	Safe     int `json:"safe"`
	Retain   int `json:"retain"`
	Decision int `json:"decision"`
	// Risk counts the candidates per risk level (Spec 044); set only with the run clock.
	Risk *RiskCounts `json:"risk,omitempty"`
}

// RiskCounts is the number of candidates per risk level.
type RiskCounts struct {
	Green  int `json:"green"`
	Yellow int `json:"yellow"`
	Orange int `json:"orange"`
	Red    int `json:"red"`
	Kept   int `json:"kept"`
}

// Document is the tidy-overview document (schemas/v2/tidy-overview.schema.json).
type Document struct {
	SchemaVersion int                    `json:"schema_version"`
	Command       string                 `json:"command"`
	Workspace     string                 `json:"workspace"`
	Derived       []derive.Derived       `json:"derived"`
	Repositories  []Repository           `json:"repositories"`
	Candidates    []tidywizard.Candidate `json:"candidates"`
	Summary       Summary                `json:"summary"`
	Mode          owner.Evidence         `json:"mode"`
	PlanID        string                 `json:"plan_id"`
	Notices       []string               `json:"notices"`
	Truncated     bool                   `json:"truncated"`

	// notices of each repository, for the text (not part of the JSON).
	repoNotices map[string][]string
	runNotices  []string
	// assessed is the risk of every candidate and groups the groupable ones (Spec 044); both are
	// computed whether or not the JSON carries them, because the text needs them.
	assessed map[string]risk.Assessment
	groups   []risk.Group
	groupOf  map[string]string
}

// Build assembles the document. No array is nil. The candidates keep the order of the result
// (repository, then id); more than MaxCandidates are cut and flagged. The summary counts every
// candidate it receives, also those a cut made here or in JSON leaves out of the list; the 4096 cut
// of the classifier happens upstream of this package and is aligned when the pipeline is wired (T019).
func Build(in Input) Document {
	d := Document{
		SchemaVersion: 1, Command: "tidy-overview", Workspace: in.Workspace,
		Derived: append([]derive.Derived{}, in.Derived...), Repositories: []Repository{},
		Candidates: []tidywizard.Candidate{}, Mode: in.Mode, PlanID: in.PlanID,
		Notices: append([]string{}, in.Notices...), runNotices: append([]string{}, in.Notices...), Truncated: in.Result.Truncated, repoNotices: map[string][]string{},
	}
	if d.Mode.Markers == nil {
		d.Mode.Markers = []string{}
	}
	if d.Mode.ByOwner == nil {
		d.Mode.ByOwner = []string{}
	}
	for _, r := range in.Result.Repositories {
		meta := in.Repos[r.Name]
		d.Repositories = append(d.Repositories, Repository{Name: r.Name, Path: meta.Path, Backend: meta.Backend, Base: meta.Base, Remote: meta.Remote, State: r.State, Reason: r.Reason})
		d.repoNotices[r.Name] = r.EscapedNotices()
		for _, n := range r.Notices {
			d.Notices = append(d.Notices, r.Name+": "+n)
		}
	}
	d.assess(in)
	late := 0
	for _, c := range in.Result.Candidates {
		if c.Reason == "evidence-incomplete" && (c.Evidence == "deadline-exceeded" || c.Evidence == "proof-budget-exhausted") {
			late++
		}
		// The summary counts every analysed candidate, before any cut; `truncated` says that the
		// candidates list is partial.
		switch c.Verdict {
		case tidywizard.VerdictSafe:
			d.Summary.Safe++
		case tidywizard.VerdictRetain:
			d.Summary.Retain++
		case tidywizard.VerdictDecision:
			d.Summary.Decision++
		}
		if len(d.Candidates) == tidywizard.MaxCandidates {
			d.Truncated = true
			continue
		}
		if c.Effects == nil {
			c.Effects = []string{}
		}
		d.Candidates = append(d.Candidates, d.withRisk(c, !in.Now.IsZero()))
	}
	if late > 0 {
		// A fixed text with counts only. The items stay retained (evidence-incomplete); the hint
		// names the flag that widens the observation window. The exhausted proof budget counts too:
		// the phase budget is the rest of that window, and the reason does not tell the phase from the tip budget.
		n := fmt.Sprintf("%d of %d not judged (deadline or proof budget); retry with a larger window, for example --timeout 15m", late, len(in.Result.Candidates))
		d.Notices = append(d.Notices, n)
		d.runNotices = append(d.runNotices, n)
	}
	return d
}

// JSON encodes the document. A document over MaxDocumentBytes is cut to the candidates that fit,
// flagged truncated, and the second result tells the caller (exit 1, output-limit).
func JSON(d Document) (out []byte, truncated bool, err error) {
	encode := func(d Document) ([]byte, error) {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(d); err != nil {
			return nil, err
		}
		return escapeInvisible(buf.Bytes()), nil
	}
	b, err := encode(d)
	if err != nil {
		return nil, false, err
	}
	for len(b) > MaxDocumentBytes && len(d.Candidates) > 0 {
		d.Truncated = true
		d.Candidates = d.Candidates[:len(d.Candidates)*3/4]
		if b, err = encode(d); err != nil {
			return nil, false, err
		}
	}
	if len(b) > MaxDocumentBytes {
		return nil, false, fmt.Errorf("the overview exceeds %d bytes without candidates", MaxDocumentBytes)
	}
	return b, d.Truncated, nil
}

// NumberedCandidate is a row the prompts can choose.
type NumberedCandidate struct {
	Number    int
	Candidate tidywizard.Candidate
}

// Numbered numbers the safe and decision rows in the order of the text (owner group, then repository, then the order of the analysis), from 1.
// Retain rows and the rows of repositories that were skipped or failed get no number.
func Numbered(d Document) []NumberedCandidate {
	out := []NumberedCandidate{}
	for _, c := range displayOrder(d) {
		if c.Verdict == tidywizard.VerdictSafe || c.Verdict == tidywizard.VerdictDecision {
			out = append(out, NumberedCandidate{Number: len(out) + 1, Candidate: c})
		}
	}
	return out
}

// ownerGroup is the candidates of one owner heading, split by repository in the order of the document.
type ownerGroup struct {
	label, suffix string // the heading is `owner LABEL SUFFIX`; the label is escaped alone
	repos         []repoRows
}

type repoRows struct {
	name       string
	candidates []tidywizard.Candidate
}

// ownerKey orders and names the owner group of a candidate (FR-006a): own scope first, then the
// other owners sorted by label, `unknown` last.
func ownerKey(c tidywizard.Candidate) (rank int, label, suffix string) {
	switch {
	case c.Scope == owner.ScopeOwn && c.Owner != "":
		return 0, c.Owner, "(own)"
	case c.Scope == owner.ScopeOther && c.Owner != "":
		return 1, c.Owner, "(other)"
	}
	return 2, "", ""
}

// ownerGroups groups the candidates of the repositories of the document by owner. The order does
// not depend on the order of the candidates: groups by rank and label, repositories by the
// document, rows by the order of the analysis.
func ownerGroups(d Document) []ownerGroup {
	type key struct {
		rank  int
		label string
	}
	byKey := map[key]*ownerGroup{}
	var keys []key
	for _, r := range d.Repositories {
		for _, c := range d.Candidates {
			if c.Repo != r.Name {
				continue
			}
			rank, label, suffix := ownerKey(c)
			k := key{rank, label}
			g := byKey[k]
			if g == nil {
				g = &ownerGroup{label: label, suffix: suffix}
				byKey[k] = g
				keys = append(keys, k)
			}
			if n := len(g.repos); n == 0 || g.repos[n-1].name != r.Name {
				g.repos = append(g.repos, repoRows{name: r.Name})
			}
			g.repos[len(g.repos)-1].candidates = append(g.repos[len(g.repos)-1].candidates, c)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].rank != keys[j].rank {
			return keys[i].rank < keys[j].rank
		}
		return keys[i].label < keys[j].label
	})
	out := make([]ownerGroup, 0, len(keys))
	for _, k := range keys {
		out = append(out, *byKey[k])
	}
	return out
}

// displayOrder is the candidates in the order of the text: by owner group, then repository.
func displayOrder(d Document) []tidywizard.Candidate {
	out := []tidywizard.Candidate{}
	for _, g := range ownerGroups(d) {
		for _, r := range g.repos {
			out = append(out, r.candidates...)
		}
	}
	return out
}

// TextOptions tunes the text. The zero value is the default: plain text, no color.
type TextOptions struct {
	// Color wraps already escaped text in SGR sequences (verdicts, headings, reasons, effects). The
	// caller sets it only for a terminal without NO_COLOR; with it off the bytes are the plain text.
	Color bool
}

// The styles of the colored text.
const (
	styleBold   = "1"
	styleDim    = "2"
	styleRed    = "31"
	styleGreen  = "32"
	styleYellow = "33"
)

// painter returns the function that styles a string that has already passed esc. It is the only
// place that writes an escape sequence, so no text from outside can add one.
func painter(on bool) func(style, s string) string {
	if !on {
		return func(_, s string) string { return s }
	}
	return func(style, s string) string { return "\x1b[" + style + "m" + s + "\x1b[0m" }
}

func verdictStyle(v tidywizard.Verdict) string {
	switch v {
	case tidywizard.VerdictSafe:
		return styleGreen
	case tidywizard.VerdictDecision:
		return styleYellow
	}
	return styleDim
}

// Text renders the overview for a terminal. Every string from outside passes esc and its bound.
func Text(d Document, opts TextOptions) string {
	e := untrusted.Esc
	paint := painter(opts.Color)
	var b strings.Builder
	fmt.Fprintf(&b, "Workspace: %s\n", e(d.Workspace))
	if len(d.Derived) > 0 {
		parts := make([]string, 0, len(d.Derived))
		for _, v := range d.Derived {
			parts = append(parts, fmt.Sprintf("%s %s (%s)", e(v.Name), e(v.Value), e(v.Source)))
		}
		fmt.Fprintf(&b, "Derived:   %s\n", strings.Join(parts, ", "))
	}
	fmt.Fprintf(&b, "Mode:      %s\n", modeLine(d.Mode))
	skipped, failed := 0, 0
	for _, r := range d.Repositories {
		switch r.State {
		case "skipped":
			skipped++
		case "error":
			failed++
		}
	}
	fmt.Fprintf(&b, "Analysed %d repositories (%d skipped, %d with errors)\n", len(d.Repositories), skipped, failed)
	numbers := map[string]int{}
	for _, n := range Numbered(d) {
		numbers[n.Candidate.ID] = n.Number
	}
	b.WriteString("\nRepositories:\n")
	for _, r := range d.Repositories {
		switch r.State {
		case "skipped":
			fmt.Fprintf(&b, "%s   %s\n", paint(styleBold, e(r.Name)), paint(styleYellow, "skipped: "+e(r.Reason)))
		case "error":
			fmt.Fprintf(&b, "%s   %s\n", paint(styleBold, e(r.Name)), paint(styleRed, "error: "+e(r.Reason)))
		default:
			fmt.Fprintf(&b, "%s   base %s\n", paint(styleBold, e(r.Name)), baseOrNone(r.Base, e))
		}
		for _, n := range d.repoNotices[r.Name] {
			fmt.Fprintf(&b, "  notice: %s\n", n)
		}
	}
	for _, g := range ownerGroups(d) {
		if g.suffix == "" {
			b.WriteString("\n" + paint(styleDim, "owner unknown") + "\n")
		} else {
			fmt.Fprintf(&b, "\n%s\n", paint(styleDim, fmt.Sprintf("owner %s %s", e(g.label), g.suffix)))
		}
		for _, r := range g.repos {
			fmt.Fprintf(&b, "  %s\n", paint(styleBold, e(r.name)))
			for i, c := range r.candidates {
				if i == MaxRowsPerRepository {
					fmt.Fprintf(&b, "    and %d more (use --json or details)\n", len(r.candidates)-i)
					break
				}
				b.WriteString("  " + row(c, numbers[c.ID], e, paint))
			}
		}
	}
	own, other := 0, 0
	for _, c := range d.Candidates {
		if c.Verdict == tidywizard.VerdictSafe {
			if c.Scope == owner.ScopeOwn {
				own++
			} else {
				other++
			}
		}
	}
	fmt.Fprintf(&b, "\nsummary: %d safe (%d own, %d other), %d retain", d.Summary.Safe, own, other, d.Summary.Retain)
	if d.Summary.Decision > 0 {
		fmt.Fprintf(&b, ", %d decision", d.Summary.Decision)
	}
	b.WriteString("\n")
	for _, n := range d.runNotices {
		fmt.Fprintf(&b, "notice: %s\n", e(n))
	}
	if d.Truncated {
		b.WriteString("truncated: the overview is cut (use a narrower selection)\n")
	}
	return b.String()
}

func baseOrNone(base string, e func(string) string) string {
	if base == "" {
		return "(none)"
	}
	return e(base)
}

func modeLine(m owner.Evidence) string {
	tty := func(on bool) string {
		if on {
			return "tty"
		}
		return "no tty"
	}
	markers := "no agent marker"
	if len(m.Markers) > 0 {
		esc := make([]string, 0, len(m.Markers))
		for _, v := range m.Markers {
			esc = append(esc, untrusted.Esc(v))
		}
		markers = "agent marker " + strings.Join(esc, ", ")
	}
	return fmt.Sprintf("%s (stdin %s, stdout %s, %s)", untrusted.Esc(string(m.Mode)), tty(m.StdinTTY), tty(m.StdoutTTY), markers)
}

// row renders one candidate. number 0 means no number (a retain row).
func row(c tidywizard.Candidate, number int, e func(string) string, paint func(style, s string) string) string {
	label := "[-]"
	if number > 0 {
		label = fmt.Sprintf("[%d]", number)
	}
	subject := c.Path
	if c.Kind == tidywizard.KindBranch || c.Kind == tidywizard.KindTrackingRef {
		subject = c.Branch
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%s %s %-12s %s", label, paint(verdictStyle(c.Verdict), fmt.Sprintf("%-8s", e(string(c.Verdict)))), e(string(c.Kind)), e(subject)))
	if c.Task != "" {
		parts = append(parts, "task "+e(c.Task))
	}
	if c.Kind == tidywizard.KindWorktree && c.Branch != "" {
		parts = append(parts, "branch "+e(c.Branch))
	}
	if c.Scope != owner.ScopeOwn {
		o := c.Owner
		if o == "" {
			o = "unknown"
		}
		parts = append(parts, fmt.Sprintf("owner %s (%s) [%s]", e(o), e(string(c.OwnerSource)), e(string(c.Scope))))
	}
	var what []string
	if c.Verdict == tidywizard.VerdictSafe && c.Proof != tidywizard.ProofNone && c.Proof != "" {
		what = append(what, "integrated ("+e(string(c.Proof))+")")
	}
	if c.Reason != "" {
		what = append(what, paint(styleDim, e(c.Reason)))
	}
	if c.Evidence != "" {
		what = append(what, paint(styleDim, e(c.Evidence)))
	}
	if len(what) > 0 {
		parts = append(parts, strings.Join(what, ": "))
	}
	if len(c.Effects) > 0 {
		fx := make([]string, 0, len(c.Effects))
		for _, f := range c.Effects {
			fx = append(fx, e(f))
		}
		parts = append(parts, "effects "+paint(styleBold, strings.Join(fx, ", ")))
	}
	if c.Verdict == tidywizard.VerdictSafe && c.Scope != owner.ScopeOwn {
		parts = append(parts, "not preselected")
	}
	return "  " + strings.Join(parts, "  ") + "\n"
}

// escapeInvisible writes the format, separator and C1 control characters that encoding/json leaves
// raw (bidirectional overrides, zero-width characters, U+0085) as \uXXXX, so that raw JSON shown
// on a terminal cannot reorder or hide text. The document stays valid and decodes to the same
// strings. Such characters can only occur inside JSON strings.
func escapeInvisible(b []byte) []byte {
	var out bytes.Buffer
	for _, r := range string(b) {
		if r == unicode.ReplacementChar || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || unicode.Is(unicode.Cc, r) && r > 0x7e {
			if r == unicode.ReplacementChar {
				out.WriteRune(r)
				continue
			}
			if r > 0xffff {
				// JSON writes a character above the basic plane as a UTF-16 surrogate pair.
				hi, lo := utf16.EncodeRune(r)
				fmt.Fprintf(&out, "\\u%04x\\u%04x", hi, lo)
				continue
			}
			fmt.Fprintf(&out, "\\u%04x", r)
			continue
		}
		out.WriteRune(r)
	}
	return out.Bytes()
}
