// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

var hookNames = []string{"pre-rebase", "post-rewrite", "reference-transaction", "post-checkout"}

// effects discloses what a later porcelain restack would touch (FR-013 g, h): local branches that
// contain the old head (descendants that apply would not move automatically), and the hooks a
// porcelain rebase would run, by presence only. Nothing here runs or reads a hook.
func (p *planner) effects(ctx context.Context) error {
	head := *p.res.Input.Branch.OldHead
	o, err := p.git.run(ctx, "for-each-ref", "--contains="+head, "--format=%(refname)%00%(objectname)", "refs/heads/")
	if err != nil {
		return err
	}
	if o.code != 0 {
		return refuse("object-missing", "The refs containing the branch tip could not be listed.", nil)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(o.out)), "\n") {
		if ref, oid, ok := strings.Cut(line, "\x00"); ok && ref != p.opts.Branch {
			if len(p.res.Effects.AffectedRefs) >= MaxPaths {
				return pathBound()
			}
			p.res.Effects.AffectedRefs = append(p.res.Effects.AffectedRefs, AffectedRef{Ref: ref, OID: oid, ContainsOldHead: true})
		}
	}
	for _, name := range hookNames {
		_, err := os.Lstat(filepath.Join(p.cdir, "hooks", name))
		present := err == nil
		p.res.Effects.Hooks = append(p.res.Effects.Hooks, HookInfo{Name: name, Present: present, WouldRunByPorcelainRebase: present, RunBySTW: false})
	}
	return nil
}
