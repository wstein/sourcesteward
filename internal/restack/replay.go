// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"bytes"
	"context"
	"strings"
)

// Replay (D7, FR-012): each moved commit is merged onto the new parent with `merge-tree
// --write-tree` and committed with `commit-tree`, both writing only into the private quarantine.
// The porcelain rebase is never used. The replay commits carry the original author and the
// fixed committer identity of the plan; they are never pushed and make no claim about the branch.

// replayed holds the per-commit outcome of the replay.
type replayOutcome struct {
	tip        string // last replay commit, or the new parent when nothing was replayed
	tipTree    string
	conflictAt int // index of the first conflicting commit, or -1
	conflict   *mergeConflict
}

type mergeConflict struct {
	items []ConflictItem
}

func (p *planner) replay(ctx context.Context) (replayOutcome, error) {
	out := replayOutcome{tip: *p.res.Input.NewParent.OID, conflictAt: -1}
	tree, err := p.git.text(ctx, "rev-parse", "--verify", out.tip+"^{tree}")
	if err != nil {
		return out, refuse("object-missing", "The tree of the new parent is not readable.", nil)
	}
	out.tipTree = tree
	p.replayed = make([]string, len(p.moved))
	for i, c := range p.moved {
		merged, conflict, err := p.mergeTree(ctx, c.parents[0], out.tip, c.oid)
		if err != nil {
			return out, err
		}
		if conflict != nil {
			out.conflictAt, out.conflict = i, conflict
			return out, nil
		}
		if merged == out.tipTree {
			p.emptyAfter = append(p.emptyAfter, c.oid) // already contained: a real rebase drops it
			continue
		}
		oid, err := p.commitTree(ctx, merged, out.tip, c)
		if err != nil {
			return out, err
		}
		p.replayed[i] = oid
		out.tip, out.tipTree = oid, merged
		p.replayedCount++
	}
	return out, nil
}

// mergeTree merges side b onto side a with the given merge base; a non-nil conflict means the
// merge stopped with conflicts (exit status 1).
func (p *planner) mergeTree(ctx context.Context, base, a, b string) (string, *mergeConflict, error) {
	o, err := p.git.run(ctx, "merge-tree", "--write-tree", "--name-only", "-z", "--merge-base="+base, a, b)
	if err != nil {
		return "", nil, err
	}
	switch o.code {
	case 0:
		tree, _, _ := bytes.Cut(o.out, []byte{0})
		return strings.TrimSpace(string(tree)), nil, nil
	case 1:
		conflict, err := parseMergeConflict(o.out)
		return "", conflict, err
	}
	return "", nil, refuse("replay-failed", "merge-tree could not predict the replay.", nil)
}

// parseMergeConflict reads `merge-tree --write-tree --name-only -z` output: the tree, the
// conflicted paths, an empty field, then the informational messages as
// `count NUL path... NUL type NUL message NUL`.
func parseMergeConflict(out []byte) (*mergeConflict, error) {
	fields := bytes.Split(out, []byte{0})
	i := 1
	var names []string
	for ; i < len(fields) && len(fields[i]) > 0; i++ {
		names = append(names, string(fields[i]))
	}
	if len(names) > MaxPaths {
		return nil, pathBound()
	}
	kinds := map[string]string{}
	for i++; i < len(fields); {
		n := 0
		for _, c := range fields[i] {
			if c < '0' || c > '9' || n > 1<<20 {
				n = -1
				break
			}
			n = n*10 + int(c-'0')
		}
		if n < 0 || i+n+2 >= len(fields) {
			break
		}
		paths := make([]string, 0, n)
		for j := 0; j < n && i+1+j < len(fields); j++ {
			paths = append(paths, string(fields[i+1+j]))
		}
		// The message names the kind precisely ("add/add", "modify/delete"); the type field says
		// only "contents" for several of them.
		kind := conflictKind(string(fields[i+n+2]))
		if kind != "" {
			for _, path := range paths {
				if _, seen := kinds[path]; !seen {
					kinds[path] = kind
				}
			}
		}
		i += n + 3
	}
	c := &mergeConflict{}
	for _, name := range names {
		kind := kinds[name]
		if kind == "" {
			kind = "unknown"
		}
		c.items = append(c.items, ConflictItem{Path: name, Kind: kind})
	}
	return c, nil
}

// conflictKind reads the kind from a message such as `CONFLICT (add/add): Merge conflict in a`.
func conflictKind(message string) string {
	rest, ok := strings.CutPrefix(message, "CONFLICT (")
	if !ok {
		return ""
	}
	kind, _, ok := strings.Cut(rest, ")")
	if !ok || kind == "" {
		return ""
	}
	return kind
}

// commitTree writes one replay commit into the quarantine.
func (p *planner) commitTree(ctx context.Context, tree, parent string, c rawCommit) (string, error) {
	env := []string{
		"GIT_AUTHOR_NAME=" + c.authorName, "GIT_AUTHOR_EMAIL=" + c.authorEmail, "GIT_AUTHOR_DATE=@" + c.authorDate,
		"GIT_COMMITTER_NAME=" + replayCommitterName, "GIT_COMMITTER_EMAIL=" + replayCommitterEmail, "GIT_COMMITTER_DATE=" + replayCommitterDate,
	}
	o, err := p.git.start(ctx, false, c.message, env, commandOutputLimit, "commit-tree", tree, "-p", parent)
	if err != nil {
		return "", err
	}
	oid := strings.TrimSpace(string(o.out))
	if o.code != 0 || !hexOID.MatchString(oid) {
		return "", refuse("replay-failed", "A replay commit could not be written to the quarantine.", map[string]any{"commit": c.oid})
	}
	return oid, nil
}
