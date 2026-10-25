// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/pflag"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/workspace"
)

type pruneV4Flags struct{ name, gitdir string }

func addPruneV4Flags(cmd *cobra.Command, p *pruneV4Flags) {
	cmd.Flags().StringVar(&p.name, "prune-registration", "", "Contract 4: exactly one original stw-locked missing-directory registration name")
	cmd.Flags().StringVar(&p.gitdir, "registration-gitdir", "", "Contract 4: matching exact canonical registration gitdir")
	for _, name := range []string{"prune-registration", "registration-gitdir"} {
		f := cmd.Flags().Lookup(name)
		f.Value = &tidyV3CountedFlag{value: f.Value}
	}
}
func runPruneV4Plan(cmd *cobra.Command, args []string, contract string, p pruneV4Flags) (err error) {
	if contract != "4" || !cmd.Flags().Changed("contract") {
		return fmt.Errorf("registration-prune selectors require explicit --contract 4")
	}
	allowed := map[string]bool{"contract": true, "state-dir": true, "json": true, "prune-registration": true, "registration-gitdir": true}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if !allowed[f.Name] && err == nil {
			err = fmt.Errorf("--%s is not valid for contract-4 registration planning", f.Name)
		}
	})
	if err != nil {
		return err
	}
	// pflag's singleton recorder retains every occurrence, including equal values.
	for name := range allowed {
		if f := cmd.Flags().Lookup(name); f != nil {
			if value, ok := f.Value.(*tidyV3CountedFlag); ok && value.count > 1 {
				return fmt.Errorf("--%s must occur exactly once in contract 4", name)
			}
		}
	}
	if p.name == "" || p.gitdir == "" || cmd.Flag("state-dir").Value.String() == "" {
		return fmt.Errorf("--prune-registration, --registration-gitdir and --state-dir are required")
	}
	if len(p.name) > 4096 || strings.ContainsAny(p.name, "/\\\x00") || p.name == "." || p.name == ".." {
		return fmt.Errorf("--prune-registration needs one exact bounded native registration name")
	}
	state := cmd.Flag("state-dir").Value.String()
	if !canonicalTidyV4CLIPath(p.gitdir) || !canonicalTidyV4CLIPath(state) {
		return fmt.Errorf("--registration-gitdir and --state-dir need canonical absolute paths")
	}
	in, out := tidyTerminalFacts(cmd.InOrStdin(), cmd.OutOrStdout())
	mode := owner.Detect(owner.Env{StdinTTY: in, StdoutTTY: out, Environ: tidyEnviron(), OSUser: planset.Getenv("USER")})
	m := workspace.V3RunMode{Mode: string(mode.Mode), StdinTTY: mode.StdinTTY, StdoutTTY: mode.StdoutTTY, Markers: mode.Markers, Mine: mode.Mine, ByOwner: mode.ByOwner, Caller: mode.Caller, CallerSource: mode.CallerSource}
	r := workspace.PlanRegistrationPruneV4(cmd.Context(), workspace.RegistrationPruneV4Options{Path: workspacePath(args), StateDir: state, Name: p.name, GitDir: p.gitdir, Mode: m, Confirm: func(word, id string) error { return owner.Confirm(owner.DevTTY, word, id) }})
	if cmd.Flag("json").Value.String() == "true" {
		if e := json.NewEncoder(cmd.OutOrStdout()).Encode(r); e != nil {
			return runtimeFailure{fmt.Errorf("contract-4 output failed")}
		}
	} else {
		if r.Plan != nil {
			if _, e := fmt.Fprintf(cmd.OutOrStdout(), "Registration prune plan %q: %q at %q. Native state is unchanged; apply is unavailable in this slice.\n", r.Plan.PlanID, r.Plan.Registration.Name, r.Plan.Registration.GitDir); e != nil {
				return runtimeFailure{e}
			}
		}
	}
	if r.Status != "ok" {
		return runtimeFailure{fmt.Errorf("%q", r.Error())}
	}
	return nil
}

func canonicalTidyV4CLIPath(p string) bool {
	return len(p) <= 4096 && utf8.ValidString(p) && !strings.ContainsRune(p, 0) && filepath.IsAbs(p) && filepath.Clean(p) == p
}
