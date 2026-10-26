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

func runPruneV4Apply(cmd *cobra.Command, id, state string, authorize, structured bool) (err error) {
	allowed := map[string]bool{"contract": true, "plan": true, "state-dir": true, "authorize": true, "json": true}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if !allowed[f.Name] && err == nil {
			err = fmt.Errorf("--%s is not valid for contract-4 registration apply", f.Name)
		}
		if value, ok := f.Value.(*tidyV3CountedFlag); ok && value.count > 1 && err == nil {
			err = fmt.Errorf("--%s must occur exactly once in contract 4", f.Name)
		}
	})
	if err != nil {
		return err
	}
	if len(id) != 32 || !canonicalTidyV4CLIPath(state) {
		return fmt.Errorf("contract-4 apply requires --plan with 32 lowercase hex characters and canonical --state-dir")
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return fmt.Errorf("contract-4 --plan must be 32 lowercase hex characters")
		}
	}
	in, out := tidyTerminalFacts(cmd.InOrStdin(), cmd.OutOrStdout())
	mode := owner.Detect(owner.Env{StdinTTY: in, StdoutTTY: out, Environ: tidyEnviron(), OSUser: planset.Getenv("USER")})
	m := workspace.V3RunMode{Mode: string(mode.Mode), StdinTTY: mode.StdinTTY, StdoutTTY: mode.StdoutTTY, Markers: mode.Markers, Mine: mode.Mine, ByOwner: mode.ByOwner, Caller: mode.Caller, CallerSource: mode.CallerSource}
	r := workspace.ApplyRegistrationPruneV4(cmd.Context(), workspace.RegistrationPruneV4ApplyOptions{StateDir: state, PlanID: id, Authorize: authorize, Mode: m, Confirm: func(word, id string) error { return owner.Confirm(owner.DevTTY, word, id) }})
	if structured {
		if e := json.NewEncoder(cmd.OutOrStdout()).Encode(r); e != nil {
			return runtimeFailure{fmt.Errorf("contract-4 output failed")}
		}
	} else {
		if _, e := fmt.Fprintf(cmd.OutOrStdout(), "Registration prune %q: %q.\n", id, r.Status); e != nil {
			return runtimeFailure{e}
		}
		for _, a := range r.Actions {
			if _, e := fmt.Fprintf(cmd.OutOrStdout(), "%q: %q\n", a.Name, a.Detail); e != nil {
				return runtimeFailure{e}
			}
		}
	}
	if r.Status != "ok" {
		return runtimeFailure{fmt.Errorf("%q", r.Error())}
	}
	return nil
}
