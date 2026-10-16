// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
)

type versionInformation struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	Version       string `json:"version"`
}

type runtimeFailure struct{ error }

const agentHints = `
Are you an AI? Use these resources ONLY IF your task specifically asks you to:
  Help a human understand, set up or use SourceSteward:
    https://raw.githubusercontent.com/wstein/sourcesteward/main/docs/agent-guide.md
  Debug or investigate a problem with SourceSteward:
    https://raw.githubusercontent.com/wstein/sourcesteward/main/llms.txt
`

// Run is the sole diagnostic/exit boundary. Each call owns a fresh command tree.
func Run(ctx context.Context, version string, args []string, stdout, stderr io.Writer) int {
	return runCLI(ctx, version, args, stdout, stderr, false)
}

// RunGHJJ is the optional gh extension entry point, sharing operations and diagnostics.
func RunGHJJ(ctx context.Context, version string, args []string, stdout, stderr io.Writer) int {
	return runCLI(ctx, version, args, stdout, stderr, true)
}
func runCLI(ctx context.Context, version string, args []string, stdout, stderr io.Writer, extension bool) int {
	return runCLIWithHosting(ctx, version, args, stdout, stderr, extension, hosting.Inspect)
}

// runCLIWithHosting keeps HTTP fixture injection private to the shared command boundary.
func runCLIWithHosting(ctx context.Context, version string, args []string, stdout, stderr io.Writer, extension bool, operation hostingOperation) int {
	if err := ctx.Err(); err != nil {
		return report(stderr, err, 1)
	}
	root, helpError := newRootWithHosting(version, stdout, stderr, operation)
	if extension {
		root.Use = "gh-jj"
		root.Short = "SourceSteward read-only Jujutsu/GitHub inspection (gh jj)"
		root.Long = "SourceSteward — gh jj. Native jj repository resolution and explicit read-only PR/CI inspection."
		for _, command := range root.Commands() {
			if command.Name() != "help" && command.Name() != "version" {
				root.RemoveCommand(command)
			}
		}
		root.AddCommand(hostingCommands(true, operation)...)
	}
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if *helpError != nil {
		return report(stderr, *helpError, 1)
	}
	if err != nil {
		return exitStatus(stderr, err)
	}
	return 0
}

// exitStatus maps a command error to its documented status: runtime failures 1, the typed strict
// findings error 3 (checked before the fallback), anything else a usage error 2.
func exitStatus(stderr io.Writer, err error) int {
	var failure runtimeFailure
	if errors.As(err, &failure) {
		return report(stderr, err, 1)
	}
	var strict strictFindingsError
	if errors.As(err, &strict) {
		return report(stderr, err, 3)
	}
	return report(stderr, err, 2)
}

func report(stderr io.Writer, err error, code int) int {
	// A failed diagnostic write cannot change an already-failed command to success.
	fmt.Fprintln(stderr, "Error:", strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\r", " "), "\n", " "))
	return code
}

func newRoot(version string, stdout, stderr io.Writer) (*cobra.Command, *error) {
	return newRootWithHosting(version, stdout, stderr, hosting.Inspect)
}
func newRootWithHosting(version string, stdout, stderr io.Writer, operation hostingOperation) (*cobra.Command, *error) {
	if version == "" {
		version = "dev"
	}
	root := &cobra.Command{
		Use:           "stw",
		Short:         "Explainable version-control tools for humans and AI agents",
		Long:          "SourceSteward — Explainable version-control tools for humans and AI agents.\n\nProvides help, version reporting and bounded read-only native Git/jj identity inspection and inventory, plus reviewable isolated task workspace creation. Evidence-based tidy reference planning is available; authorized local reference cleanup is available; owned workspace retirement uses separate registration/files selections; GitButler integration remains planned.",
		SilenceErrors: true, SilenceUsage: true,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetIn(strings.NewReader(""))
	root.CompletionOptions.DisableDefaultCmd = true
	var helpError error
	// Cobra's help callback has no error return. Preserve output failures for Run.
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		description := cmd.Long
		if description == "" {
			description = cmd.Short
		}
		_, helpError = fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n%s", description, cmd.UsageString())
		if helpError == nil && cmd == root {
			_, helpError = io.WriteString(cmd.OutOrStdout(), agentHints)
		}
	})
	info := versionInformation{SchemaVersion: 1, Name: "SourceSteward", Version: version}
	jsonOutput := false
	versionCommand := &cobra.Command{
		Use: "version", Short: "Show SourceSteward version information", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cmd.Context().Err(); err != nil {
				return runtimeFailure{err}
			}
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(info)
			} else {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", info.Name, info.Version)
			}
			if err != nil {
				return runtimeFailure{err}
			}
			return nil
		},
	}
	versionCommand.Flags().BoolVar(&jsonOutput, "json", false, "Output one versioned JSON object")
	root.AddCommand(versionCommand, inspectCommand(), inventoryCommand(), workspaceCommand(), tidyCommand(), pinsCommand(), hostingCommandWith(false, operation))
	root.SetHelpCommand(&cobra.Command{
		Use: "help [command]", Short: "Help about a command", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, remaining, err := root.Find(args)
			if err != nil || len(remaining) != 0 || target == nil {
				return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
			}
			return target.Help()
		},
	})
	return root, &helpError
}
