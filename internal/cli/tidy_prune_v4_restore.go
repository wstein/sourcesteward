// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func runPruneV4Restore(cmd *cobra.Command, args []string, f tidyRestoreFlags) (err error) {
	if len(args) != 0 {
		return fmt.Errorf("contract-4 restore binds its repository through --plan; no positional path")
	}
	allowed := map[string]bool{"contract": true, "plan": true, "state-dir": true, "apply": true, "authorize": true, "json": true}
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if !allowed[flag.Name] && err == nil {
			err = fmt.Errorf("--%s is not valid for contract-4 administrative restore", flag.Name)
		}
		if count, ok := flag.Value.(*tidyV3CountedFlag); ok && count.count > 1 && err == nil {
			err = fmt.Errorf("--%s must occur exactly once in contract 4", flag.Name)
		}
	})
	if err != nil {
		return err
	}
	if !canonicalTidyV4CLIPath(f.stateDir) || len(f.plan) != 32 {
		return fmt.Errorf("contract-4 restore requires canonical --state-dir and 32 lowercase hex --plan")
	}
	for _, r := range f.plan {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return fmt.Errorf("contract-4 --plan must be 32 lowercase hex characters")
		}
	}
	if f.apply != f.authorize {
		return fmt.Errorf("--apply and --authorize are required together")
	}
	in, out := tidyTerminalFacts(cmd.InOrStdin(), cmd.OutOrStdout())
	mode := owner.Detect(owner.Env{StdinTTY: in, StdoutTTY: out, Environ: tidyEnviron(), OSUser: planset.Getenv("USER")})
	m := workspace.V3RunMode{Mode: string(mode.Mode), StdinTTY: mode.StdinTTY, StdoutTTY: mode.StdoutTTY, Markers: mode.Markers, Mine: mode.Mine, ByOwner: mode.ByOwner, Caller: mode.Caller, CallerSource: mode.CallerSource}
	result := workspace.RestoreRegistrationPruneV4(cmd.Context(), workspace.RegistrationPruneV4RestoreOptions{StateDir: f.stateDir, PlanID: f.plan, Apply: f.apply, Authorize: f.authorize, Mode: m, Confirm: func(word, id string) error { return owner.Confirm(owner.DevTTY, word, id) }})
	if f.structured {
		if e := json.NewEncoder(cmd.OutOrStdout()).Encode(result); e != nil {
			return runtimeFailure{fmt.Errorf("contract-4 restore output failed")}
		}
	} else {
		if _, e := fmt.Fprintf(cmd.OutOrStdout(), "Administrative registration restore %q: %q.\n", f.plan, result.Status); e != nil {
			return runtimeFailure{e}
		}
		for _, a := range result.Actions {
			if _, e := fmt.Fprintf(cmd.OutOrStdout(), "%q: %q\n", a.Name, a.Detail); e != nil {
				return runtimeFailure{e}
			}
		}
		for _, limit := range result.Limitations {
			if _, e := fmt.Fprintf(cmd.OutOrStdout(), "Limit: %q\n", limit); e != nil {
				return runtimeFailure{e}
			}
		}
	}
	if result.Status != "ok" {
		return runtimeFailure{fmt.Errorf("%q", result.Error())}
	}
	return nil
}
