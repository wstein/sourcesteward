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
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// Bounds of the overview.
const (
	// MaxDocumentBytes bounds the JSON document (Spec 041 JSON section).
	MaxDocumentBytes = 4 << 20
	// MaxRowsPerRepository is the number of rows the text shows per repository.
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
}

// Build assembles the document. No array is nil. The candidates keep the order of the result
// (repository, then id); more than MaxCandidates are cut and flagged. The summary counts every
// analysed candidate, also those a cut (here or in JSON) leaves out of the list.
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
	for _, c := range in.Result.Candidates {
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
		d.Candidates = append(d.Candidates, c)
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

// Numbered numbers the safe and decision rows in display order (repository, then id), from 1.
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

// displayOrder is the candidates grouped by the repositories in the order of the document.
func displayOrder(d Document) []tidywizard.Candidate {
	out := []tidywizard.Candidate{}
	for _, r := range d.Repositories {
		for _, c := range d.Candidates {
			if c.Repo == r.Name {
				out = append(out, c)
			}
		}
	}
	return out
}

// TextOptions tunes the text. The zero value is the default.
type TextOptions struct{}

// Text renders the overview for a terminal. Every string from outside passes esc and its bound.
func Text(d Document, _ TextOptions) string {
	e := untrusted.Esc
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
	for _, r := range d.Repositories {
		b.WriteString("\n")
		switch r.State {
		case "skipped":
			fmt.Fprintf(&b, "%s   skipped: %s\n", e(r.Name), e(r.Reason))
		case "error":
			fmt.Fprintf(&b, "%s   error: %s\n", e(r.Name), e(r.Reason))
		default:
			fmt.Fprintf(&b, "%s   base %s\n", e(r.Name), baseOrNone(r.Base, e))
		}
		for _, n := range d.repoNotices[r.Name] {
			fmt.Fprintf(&b, "  notice: %s\n", n)
		}
		shown, more := 0, 0
		for _, c := range d.Candidates {
			if c.Repo != r.Name {
				continue
			}
			if shown == MaxRowsPerRepository {
				more++
				continue
			}
			shown++
			b.WriteString(row(c, numbers[c.ID], e))
		}
		if more > 0 {
			fmt.Fprintf(&b, "  and %d more (use --json or details)\n", more)
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
func row(c tidywizard.Candidate, number int, e func(string) string) string {
	label := "[-]"
	if number > 0 {
		label = fmt.Sprintf("[%d]", number)
	}
	subject := c.Path
	if c.Kind == tidywizard.KindBranch || c.Kind == tidywizard.KindTrackingRef {
		subject = c.Branch
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%s %-8s %-12s %s", label, e(string(c.Verdict)), e(string(c.Kind)), e(subject)))
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
		what = append(what, e(c.Reason))
	}
	if c.Evidence != "" {
		what = append(what, e(c.Evidence))
	}
	if len(what) > 0 {
		parts = append(parts, strings.Join(what, ": "))
	}
	if len(c.Effects) > 0 {
		fx := make([]string, 0, len(c.Effects))
		for _, f := range c.Effects {
			fx = append(fx, e(f))
		}
		parts = append(parts, "effects "+strings.Join(fx, ", "))
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
