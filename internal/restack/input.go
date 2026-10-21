// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"strings"
)

// resolve turns the explicit input into commit ids and the moved range (FR-011, FR-014).
func (p *planner) resolve(ctx context.Context) error {
	in := &p.res.Input
	head, err := p.commitOf(ctx, p.opts.Branch, "branch")
	if err != nil {
		return err
	}
	in.Branch.OldHead = &head
	if p.opts.ExpectHead != "" && p.opts.ExpectHead != head {
		return refuse("head-changed", "The branch tip differs from --expect-head.", map[string]any{"observed": head, "expected": p.opts.ExpectHead})
	}
	oldParent, err := p.commitOf(ctx, p.opts.OldParent, "old parent")
	if err != nil {
		return err
	}
	in.OldParent.OID = &oldParent
	newParent, err := p.commitOf(ctx, p.opts.NewParent, "new parent")
	if err != nil {
		return err
	}
	in.NewParent.OID = &newParent
	if head == oldParent {
		return refuse("empty-moved-range", "The branch tip equals the old parent: nothing to restack.", nil)
	}
	switch o, err := p.git.run(ctx, "merge-base", "--is-ancestor", oldParent, head); {
	case err != nil:
		return err
	case o.code == 1:
		return refuse("old-parent-not-ancestor", "The old parent is not an ancestor of the branch tip.", nil)
	case o.code != 0:
		return refuse("object-missing", "The history between the old parent and the branch tip is not readable.", nil)
	}
	switch o, err := p.git.run(ctx, "merge-base", oldParent, newParent); {
	case err != nil:
		return err
	case o.code == 1:
		return refuse("unrelated-histories", "The old parent and the new parent share no history.", nil)
	case o.code != 0:
		return refuse("object-missing", "The history of the parents is not readable.", nil)
	default:
		in.Base = &Base{Old: oldParent, New: strings.TrimSpace(string(o.out))}
	}
	return p.collectRange(ctx, oldParent, head)
}

// commitOf resolves one explicit identifier to exactly one commit id.
func (p *planner) commitOf(ctx context.Context, given, what string) (string, error) {
	if hexOID.MatchString(given) {
		for _, namespace := range []string{"refs/heads/", "refs/tags/", "refs/remotes/"} {
			o, err := p.git.run(ctx, "for-each-ref", "--format=%(refname)", namespace+given)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(string(o.out)) != "" {
				return "", refuse("ref-ambiguous", "A ref is named like the commit id.", map[string]any{"which": what})
			}
		}
		o, err := p.git.run(ctx, "cat-file", "-t", given)
		if err != nil {
			return "", err
		}
		if o.code != 0 {
			return "", refuse("object-missing", "The object does not exist in the repository.", map[string]any{"which": what})
		}
		if strings.TrimSpace(string(o.out)) != "commit" {
			return "", refuse("ref-not-commit", "The object is not a commit.", map[string]any{"which": what})
		}
		return given, nil
	}
	o, err := p.git.run(ctx, "for-each-ref", "--format=%(refname)%00%(objecttype)", given)
	if err != nil {
		return "", err
	}
	if o.code != 0 {
		return "", refuse("object-missing", "The ref points to an object that is not readable.", map[string]any{"which": what})
	}
	var kind string
	matches := 0
	for _, line := range strings.Split(strings.TrimSpace(string(o.out)), "\n") {
		if name, objectType, ok := strings.Cut(line, "\x00"); ok && name == given {
			matches++
			kind = objectType
		}
	}
	switch {
	case matches == 0:
		return "", refuse("ref-not-found", "The ref does not exist.", map[string]any{"which": what})
	case matches > 1:
		return "", refuse("ref-ambiguous", "The ref name is not unique.", map[string]any{"which": what})
	case kind != "commit" && kind != "tag":
		return "", refuse("ref-not-commit", "The ref does not peel to a commit.", map[string]any{"which": what})
	}
	oid, err := p.git.run(ctx, "rev-parse", "--verify", "--end-of-options", given+"^{commit}")
	if err != nil {
		return "", err
	}
	if oid.code != 0 {
		return "", refuse("ref-not-commit", "The ref does not peel to a commit.", map[string]any{"which": what})
	}
	return strings.TrimSpace(string(oid.out)), nil
}

// collectRange reads old_parent..old_head, oldest first, and refuses merges and oversize ranges.
func (p *planner) collectRange(ctx context.Context, oldParent, head string) error {
	spec := oldParent + ".." + head
	count, err := p.git.run(ctx, "rev-list", "--count", spec)
	if err != nil {
		return err
	}
	if count.code != 0 {
		return refuse("object-missing", "A commit of the moved range is not readable.", nil)
	}
	n := 0
	for _, c := range strings.TrimSpace(string(count.out)) {
		n = n*10 + int(c-'0')
	}
	if n > MaxCommits {
		return boundHit{"range-too-large", "max_commits"}
	}
	merges, err := p.git.run(ctx, "rev-list", "--count", "--merges", spec)
	if err != nil {
		return err
	}
	if merges.code != 0 || strings.TrimSpace(string(merges.out)) != "0" {
		return refuse("merge-commit-in-range", "The moved range contains a merge commit.", nil)
	}
	list, err := p.git.must(ctx, "rev-list", "--topo-order", "--reverse", spec)
	if err != nil {
		return refuse("object-missing", "A commit of the moved range is not readable.", nil)
	}
	for _, oid := range strings.Fields(string(list)) {
		raw, err := p.git.must(ctx, "cat-file", "commit", oid)
		if err != nil {
			return refuse("object-missing", "A commit of the moved range is not readable.", nil)
		}
		c, perr := parseCommit(oid, raw)
		if perr != nil || len(c.parents) != 1 {
			return refuse("merge-commit-in-range", "A moved commit has an unsupported shape.", map[string]any{"commit": oid})
		}
		p.moved = append(p.moved, c)
		p.res.Input.Moved.Commits = append(p.res.Input.Moved.Commits, movedCommit(c))
	}
	p.res.Input.Moved.Count = len(p.moved)
	return nil
}
