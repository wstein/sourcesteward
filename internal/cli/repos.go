// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/inspection"
)

func reposCommand() *cobra.Command { return reposCommandWith(inspection.Repos) }

func reposCommandWith(run func(context.Context, inspection.ReposOptions) inspection.ReposResult) *cobra.Command {
	var jsonOutput, submodules bool
	command := &cobra.Command{
		Use:          "repos PATH... [--submodules] [--json]",
		Short:        "Inventory and aggregate facts over multiple repositories in an isolated bounded envelope",
		Long:         "Bounded read-only multi-repository inventory across Git and jj repositories.\nDiscovers direct submodule paths with --submodules without recursion.\nExit statuses: 0 overall ok, 1 partial or error result, 2 usage error.",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options := inspection.ReposOptions{
				Paths:      args,
				Submodules: submodules,
			}
			if err := inspection.ValidateReposOptions(options); err != nil {
				return err
			}
			result := run(cmd.Context(), options)
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				err = renderReposText(cmd.OutOrStdout(), result)
			}
			if err != nil {
				return runtimeFailure{err}
			}
			if err = cmd.Context().Err(); err != nil {
				return runtimeFailure{err}
			}
			if result.Status != "ok" {
				return runtimeFailure{fmt.Errorf("repos %s: %d entries", result.Status, len(result.Entries))}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.BoolVar(&jsonOutput, "json", false, "Output one versioned JSON object")
	flags.BoolVar(&submodules, "submodules", false, "Add direct registered submodule paths of Git-owned repositories")
	return command
}

// escapeReposLine makes repository text safe for a terminal line: control characters, newlines, U+2028 and
// format characters become escapes, invalid UTF-8 bytes become \xNN. The line is cut at 512 bytes after
// escaping, between whole escaped characters, so it is never split inside a rune or an escape.
func escapeReposLine(s string) string {
	const limit = 512
	var pieces []string
	total := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		var p string
		switch {
		case r == utf8.RuneError && size == 1:
			p = fmt.Sprintf(`\x%02x`, s[i])
		case r == '\\':
			p = `\\`
		case r == '\n':
			p = `\n`
		case r == '\r':
			p = `\r`
		case r == '\t':
			p = `\t`
		case r < 0x20 || r == 0x7f:
			p = fmt.Sprintf(`\x%02x`, r)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			p = fmt.Sprintf(`\u%04x`, r)
		default:
			p = s[i : i+size]
		}
		pieces = append(pieces, p)
		total += len(p)
		i += size
	}
	var b strings.Builder
	if total <= limit {
		for _, p := range pieces {
			b.WriteString(p)
		}
		return b.String()
	}
	for _, p := range pieces {
		if b.Len()+len(p) > limit-len("...") {
			break
		}
		b.WriteString(p)
	}
	return b.String() + "..."
}

func reposAbbrevOID(oid *string) string {
	if oid == nil {
		return "none"
	}
	s := *oid
	if len(s) > 7 {
		s = s[:7]
	}
	return escapeReposLine(s)
}

func renderReposText(w io.Writer, r inspection.ReposResult) error {
	var b strings.Builder
	b.WriteString(escapeReposLine(fmt.Sprintf("SourceSteward repos: %s (%d entries)", r.Status, len(r.Entries))) + "\n")
	b.WriteString("effects: read-only, no network, no fetch, no repository writes\n")

	for _, entry := range r.Entries {
		linesRendered := 0
		var header strings.Builder
		if entry.Origin == "submodule" && entry.Parent != nil {
			fmt.Fprintf(&header, "[%d] %s  submodule of [%d]  %s", entry.Index, entry.Path, *entry.Parent, entry.Status)
		} else {
			fmt.Fprintf(&header, "[%d] %s  %s  %s", entry.Index, entry.Path, entry.Backend, entry.Status)
		}

		if entry.Facts != nil {
			headDesc := entry.Facts.HeadState
			if entry.Facts.HeadRef != nil {
				ref := *entry.Facts.HeadRef
				if strings.HasPrefix(ref, "refs/heads/") {
					ref = strings.TrimPrefix(ref, "refs/heads/")
				}
				headDesc = ref
			}
			if entry.Facts.HeadCommitID != nil {
				headDesc += " " + reposAbbrevOID(entry.Facts.HeadCommitID)
			}
			fmt.Fprintf(&header, "  head %s  dirty=%s  workspaces %d", headDesc, entry.Facts.Dirty.Value, entry.Facts.Workspaces.Total)
		} else if entry.Error != nil && entry.Status != "ok" {
			fmt.Fprintf(&header, "  %s", entry.Error.Code)
		}

		b.WriteString(escapeReposLine(header.String()) + "\n")
		linesRendered++

		if entry.Error != nil && entry.Status != "ok" && linesRendered < 24 {
			b.WriteString(escapeReposLine(fmt.Sprintf("      %s: %s", entry.Error.Code, entry.Error.Message)) + "\n")
			linesRendered++
		}

		for _, note := range entry.Notes {
			if linesRendered >= 24 {
				break
			}
			b.WriteString(escapeReposLine("      "+note) + "\n")
			linesRendered++
		}

		if entry.Inventory != nil && linesRendered < 24 {
			attentionCount := 0
			for _, ws := range entry.Inventory.Workspaces {
				kind := inspection.ReposWorkspaceKind(ws, entry.Backend)
				if kind == "" {
					continue
				}
				attentionCount++
				if attentionCount <= 16 && linesRendered < 24 {
					name := ws.Name
					if ws.Path != nil && *ws.Path != "" {
						name = *ws.Path
					}
					b.WriteString(escapeReposLine(fmt.Sprintf("      workspace %s (%s)", name, kind)) + "\n")
					linesRendered++
				}
			}
			if attentionCount > 16 && linesRendered < 24 {
				b.WriteString(escapeReposLine(fmt.Sprintf("      +%d more", attentionCount-16)) + "\n")
				linesRendered++
			}
		}
	}

	summaryLine := fmt.Sprintf("summary: ok %d, partial %d, error %d, skipped %d; dirty %d; stale %d; foreign %d; conflicted refs %d",
		r.Summary.ByStatus.Ok, r.Summary.ByStatus.Partial, r.Summary.ByStatus.Error, r.Summary.ByStatus.Skipped,
		r.Summary.Dirty.True, r.Summary.StaleWorkspaces, r.Summary.ForeignWorkspaces, r.Summary.ConflictedReferences)
	b.WriteString(escapeReposLine(summaryLine) + "\n")

	if len(r.Boundary.Limitations) > 0 {
		limLine := "limitations: " + strings.Join(r.Boundary.Limitations, "; ")
		b.WriteString(escapeReposLine(limLine) + "\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}
