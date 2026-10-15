// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"io"
	"sort"

	"github.com/spf13/cobra"
)

// CommandDocumentation is build-time metadata, not a runtime CLI result contract.
type CommandDocumentation struct {
	Path  string `json:"path"`
	Short string `json:"short"`
	Long  string `json:"long"`
	Usage string `json:"usage"`
	Flags string `json:"flags"`
}

// CommandReference constructs the actual Cobra tree without executing callbacks.
// It has no repository input and performs no filesystem or network operations.
func CommandReference() []CommandDocumentation {
	root, _ := newRoot("dev", io.Discard, io.Discard)
	root.InitDefaultHelpCmd()
	var result []CommandDocumentation
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		command.InitDefaultHelpFlag()
		description := command.Long
		if description == "" {
			description = command.Short
		}
		result = append(result, CommandDocumentation{Path: command.CommandPath(), Short: command.Short, Long: description, Usage: command.UseLine(), Flags: command.LocalFlags().FlagUsagesWrapped(100) + command.InheritedFlags().FlagUsagesWrapped(100)})
		for _, child := range command.Commands() {
			if !child.Hidden {
				walk(child)
			}
		}
	}
	walk(root)
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}
