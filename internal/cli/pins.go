// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/inspection"
)

// strictFindingsError is the typed exit-status-3 error of `stw pins --strict`: the result was
// complete and carried error findings. The full result has already been written.
type strictFindingsError struct{ count int }

func (e strictFindingsError) Error() string {
	if e.count == 1 {
		return "strict: 1 error finding"
	}
	return fmt.Sprintf("strict: %d error findings", e.count)
}

func pinsCommand() *cobra.Command { return pinsCommandWith(inspection.Pins) }

func pinsCommandWith(run func(context.Context, inspection.PinsOptions) inspection.PinsResult) *cobra.Command {
	var jsonOutput, strict bool
	command := &cobra.Command{
		Use: "pins [PATH]", Short: "Audit the submodule pins of a Git superproject without writes",
		Long: "Report what each submodule pin records, what is checked out and whether the pinned commit is\ncontained in a remote-tracking ref the local repository already knows.\nRead-only: no network, fetch, hook, prompt or write; remote-tracking refs may be stale.\nGit worktrees are supported; jj, GitButler, ambiguous and bare repositories are refused.\nExit statuses: 0 complete result; 1 refusal, failure, cancellation or incomplete result;\n2 usage error; 3 with --strict, a complete result and at least one error finding.",
		Args: cobra.MaximumNArgs(1),
		// A usage error must leave standard output empty, even when the command runs outside the root.
		SilenceUsage: true, SilenceErrors: true,
		// Hidden until the final work item of Spec 021 completes conformance evidence and unhides it,
		// so a partial stack never advertises a half feature.
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options := inspection.PinsOptions{}
			if len(args) == 1 {
				if args[0] == "" {
					return errors.New("path must not be empty; omit it to audit the current directory")
				}
				options.Path = args[0]
			}
			if err := inspection.ValidatePinsOptions(options); err != nil {
				return err
			}
			result := run(cmd.Context(), options)
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				err = renderPinsText(cmd.OutOrStdout(), result)
			}
			if err != nil {
				return runtimeFailure{err}
			}
			if err = cmd.Context().Err(); err != nil {
				return runtimeFailure{err}
			}
			if result.Status != "ok" {
				return runtimeFailure{fmt.Errorf("pins %s: %s", result.Status, pinsErrorText(result))}
			}
			if strict {
				count := 0
				for _, f := range result.Findings {
					if f.Severity == "error" {
						count++
					}
				}
				if count > 0 {
					return strictFindingsError{count}
				}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.BoolVar(&jsonOutput, "json", false, "Output one versioned pins result, including refusals and operational errors")
	flags.BoolVar(&strict, "strict", false, "Exit 3 when a complete result has error findings; the result is still printed in full")
	return command
}
func pinsErrorText(r inspection.PinsResult) string {
	if r.Error != nil {
		return escapePinsText(r.Error.Code + ": " + r.Error.Message)
	}
	return "result is incomplete"
}

// escapePinsText makes repository-controlled text inert: control, format, separator and invalid
// bytes become visible escapes, so a path or ref cannot forge lines or terminal effects.
func escapePinsText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}
func pinsAbbrev(oid *string) string {
	if oid == nil {
		return "none"
	}
	if len(*oid) > 7 {
		return escapePinsText((*oid)[:7]) + "…"
	}
	return escapePinsText(*oid)
}

func renderPinsText(w io.Writer, r inspection.PinsResult) error {
	// Render into one buffer so formatting has no effects and a writer error ends the command.
	var text strings.Builder
	location := r.InputPath
	head := ""
	if r.Identity != nil && r.Identity.Repository != nil {
		if r.Identity.Repository.Worktree != nil {
			location = *r.Identity.Repository.Worktree
		}
		head = ", HEAD " + pinsAbbrev(r.Identity.Repository.Head.OID)
	}
	fmt.Fprintf(&text, "SourceSteward pins: %s (%s%s)\n", escapePinsText(location), escapePinsText(r.Backend), head)
	text.WriteString("effects: read-only, no network, no fetch\n")
	if r.Error != nil {
		fmt.Fprintf(&text, "%s: %s: %s\n", escapePinsText(r.Status), escapePinsText(r.Error.Code), escapePinsText(r.Error.Message))
	}
	if r.Status == "incomplete" {
		text.WriteString("incomplete: a bound was exhausted or state changed; the result does not cover every submodule\n")
	}
	if len(r.Submodules) > 0 {
		text.WriteString("\n")
	}
	for _, s := range r.Submodules {
		path := escapePinsText(s.Path)
		if s.Refusal != nil {
			fmt.Fprintf(&text, "%s  refused: %s: %s\n", path, escapePinsText(s.Refusal.Code), escapePinsText(s.Refusal.Message))
			continue
		}
		fmt.Fprintf(&text, "%s  pin %s  ", path, pinsAbbrev(s.Pin.HeadCommit))
		if s.Pin.StagedDiffers {
			fmt.Fprintf(&text, "staged %s  ", pinsAbbrev(s.Pin.IndexCommit))
		}
		if s.Checkout.State == "uninitialized" {
			text.WriteString("uninitialized\n")
		} else {
			relation := escapePinsText(s.Checkout.RelationToPin)
			if s.Checkout.Ahead != nil && s.Checkout.Behind != nil && s.Checkout.RelationToPin != "same" && s.Checkout.RelationToPin != "unknown" {
				relation += fmt.Sprintf(", ahead %d, behind %d", *s.Checkout.Ahead, *s.Checkout.Behind)
				if s.Checkout.Capped {
					relation += " (capped)"
				}
			}
			fmt.Fprintf(&text, "checkout %s (%s)  %s\n", pinsAbbrev(s.Checkout.HeadCommit), relation, pinsDirtyText(s.Checkout.Dirty))
		}
		switch s.Publication.Status {
		case "reachable":
			refs := make([]string, 0, len(s.Publication.ReachableFrom))
			for _, ref := range s.Publication.ReachableFrom {
				refs = append(refs, escapePinsText(strings.TrimPrefix(ref.Ref, "refs/remotes/")))
			}
			suffix := ""
			if s.Publication.Truncated {
				suffix = " (list truncated)"
			}
			fmt.Fprintf(&text, "  publication: reachable from %s%s\n", strings.Join(refs, ", "), suffix)
		case "unreachable":
			text.WriteString("  publication: UNREACHABLE (no remote-tracking ref contains it)\n")
		default:
			fmt.Fprintf(&text, "  publication: unknown (%s)\n", escapePinsText(s.Publication.Reason))
		}
	}
	if len(r.Submodules) > 0 || len(r.Findings) > 0 {
		fmt.Fprintf(&text, "\nfindings: %s, %s, %s\n", fmt.Sprintf("%d error", r.Summary.Errors), fmt.Sprintf("%d warning", r.Summary.Warnings), fmt.Sprintf("%d info", r.Summary.Infos))
	}
	for _, f := range r.Findings {
		subject := "-"
		if f.Submodule != nil {
			subject = escapePinsText(*f.Submodule)
		}
		fmt.Fprintf(&text, "  %-8s %s  %s: %s\n", escapePinsText(f.Severity), subject, escapePinsText(f.Code), escapePinsText(f.Message))
	}
	text.WriteString("note: remote-tracking refs may be stale; no fetch was made\n")
	_, err := io.WriteString(w, text.String())
	return err
}
func pinsDirtyText(d inspection.PinsDirty) string {
	switch {
	case d.Tracked == "present" || d.Untracked == "present":
		return "dirty"
	case d.Tracked == "unknown" || d.Untracked == "unknown":
		return "dirty state unknown"
	}
	return "clean"
}
