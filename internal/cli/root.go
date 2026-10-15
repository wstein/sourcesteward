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
)

type versionInformation struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	Version       string `json:"version"`
}

type runtimeFailure struct{ error }

// Run is the sole diagnostic/exit boundary. Each call owns a fresh command tree.
func Run(ctx context.Context, version string, args []string, stdout, stderr io.Writer) int {
	if err := ctx.Err(); err != nil {
		return report(stderr, err, 1)
	}
	root, helpError := newRoot(version, stdout, stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if *helpError != nil {
		return report(stderr, *helpError, 1)
	}
	if err != nil {
		var failure runtimeFailure
		if errors.As(err, &failure) {
			return report(stderr, err, 1)
		}
		return report(stderr, err, 2)
	}
	return 0
}

func report(stderr io.Writer, err error, code int) int {
	// A failed diagnostic write cannot change an already-failed command to success.
	fmt.Fprintln(stderr, "Error:", strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\r", " "), "\n", " "))
	return code
}

func newRoot(version string, stdout, stderr io.Writer) (*cobra.Command, *error) {
	if version == "" {
		version = "dev"
	}
	root := &cobra.Command{
		Use:           "stw",
		Short:         "Explainable version-control tools for humans and AI agents",
		Long:          "SourceSteward — Explainable version-control tools for humans and AI agents.\n\nThis foundation provides help and version reporting. Repository operations are planned.",
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
	root.AddCommand(versionCommand)
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
