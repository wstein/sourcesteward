// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
)

var tidyRegistrationPrunes = tidywizard.RegistrationPrunes

// runTidyOverviewV4 is the explicit read-only first slice. A future manifest-2
// selection path must dispatch separately; this path never plans or applies.
func runTidyOverviewV4(cmd *cobra.Command, o *tidyBareOptions) error {
	run, err := startWizard(cmd, &o.tidyWizardOptions, o.timeout)
	if err != nil {
		return err
	}
	defer run.cancel()
	observed := tidyRegistrationPrunes(run.ctx, run.prep.Specs, run.prep.Stores, run.mode, run.cfg)
	rows := overviewPruneView(observed.Rows, run.mode, o.only)
	notices := append(append([]string{}, run.prep.Notices...), observed.Notices...)
	doc := render.BuildV4(render.Input{Workspace: run.prep.Workspace, Derived: run.prep.Derived, Repos: run.prep.Meta, Result: run.res, Mode: run.mode, Notices: notices}, rows)
	var output []byte
	var cut bool
	if o.json {
		output, cut, err = render.JSONV4(doc)
	} else {
		var text string
		text, cut = render.TextV4(doc, render.TextOptions{})
		output = []byte(text)
	}
	if err != nil {
		return runtimeFailure{err}
	}
	n, err := cmd.OutOrStdout().Write(output)
	if err != nil {
		return runtimeFailure{err}
	}
	if n != len(output) {
		return runtimeFailure{io.ErrShortWrite}
	}
	if cause := interruption(run.ctx, run.timeoutSet, o.timeout); cause != nil {
		return runtimeFailure{cause}
	}
	if err := wizardExit(doc.Truncated || cut, run.prep, run.res); err != nil {
		return err
	}
	if observed.Incomplete {
		return runtimeFailure{errors.New("registration-prune-inventory-incomplete: registration evidence is incomplete; nothing was changed")}
	}
	return nil
}

func overviewPruneView(rows []tidywizard.RegistrationPrune, mode owner.Evidence, only string) []tidywizard.RegistrationPrune {
	out := []tidywizard.RegistrationPrune{}
	if only != "" {
		admitted := false
		for _, kind := range strings.Split(only, ",") {
			if kind == "stale-worktrees" {
				admitted = true
			}
		}
		if !admitted {
			return out
		}
	}
	entries := make([]owner.Entry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, owner.Entry{ID: row.ID, Owner: owner.Owner{Label: row.Owner, Source: row.OwnerSource, Scope: row.Scope}})
	}
	visible := map[string]bool{}
	for _, entry := range owner.View(entries, mode) {
		visible[entry.ID] = true
	}
	for _, row := range rows {
		if visible[row.ID] {
			out = append(out, row)
		}
	}
	return out
}
