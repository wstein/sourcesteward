// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Hazards (FR-014, Spec 029/031 safeguards). `merge-tree` may run a configured merge driver and
// reads attributes, so anything that could start a helper or change which bytes are merged
// refuses the plan before the first replay. Only names are reported; nothing is executed.

var (
	mergeDriverKey  = regexp.MustCompile(`^merge\..+\.(driver|name|recursive)$`)
	filterDriverKey = regexp.MustCompile(`^filter\..+\.(clean|smudge|process|required)$`)
	diffDriverKey   = regexp.MustCompile(`^diff\..+\.(command|textconv|cachetextconv|binary)$`)
	includeKey      = regexp.MustCompile(`^(include|includeif\..+)\.path$`)
)

// configHazard classifies one configuration key (as listed by `git config --list`: section and
// variable names lower-cased) and reports whether it refuses the plan.
func configHazard(key string) (name string, refuses bool) {
	switch {
	case key == "core.hookspath":
		return "core.hooksPath", true
	case key == "core.fsmonitor":
		return "core.fsmonitor", true
	case key == "core.attributesfile":
		return "core.attributesFile", true
	case includeKey.MatchString(key):
		return "include", true
	case strings.HasPrefix(key, "extensions."):
		return "extensions", true
	case mergeDriverKey.MatchString(key):
		return "merge-driver", true
	case filterDriverKey.MatchString(key):
		return "filter-driver", true
	case diffDriverKey.MatchString(key), key == "diff.external":
		return "diff-driver", true
	case key == "user.name" || key == "user.email":
		return "local-identity-config", false
	}
	return "", false
}

// checkRepository refuses configuration, metadata and history hazards of the repository.
func (p *planner) checkRepository(ctx context.Context) error {
	var refusing []string
	add := func(name string) {
		if !slices.Contains(refusing, name) {
			refusing = append(refusing, name)
		}
	}
	// The configuration is listed without includes and without the command-line pins.
	listed, err := p.git.start(ctx, true, nil, nil, commandOutputLimit, "config", "--list", "-z", "--no-includes")
	if err != nil {
		return err
	}
	if listed.code != 0 {
		return refuse("repository-hazard", "The repository configuration is not readable.", map[string]any{"hazards": []string{"config-unreadable"}})
	}
	for _, record := range bytes.Split(listed.out, []byte{0}) {
		key, _, _ := strings.Cut(string(record), "\n")
		name, refuses := configHazard(strings.ToLower(key))
		switch {
		case name == "":
		case refuses:
			add(name)
		case !slices.Contains(p.res.Effects.Hazards, name):
			p.res.Effects.Hazards = append(p.res.Effects.Hazards, name)
			p.res.Diagnostics = append(p.res.Diagnostics, Diagnostic{name, "A repository-local user.name or user.email exists; it is never read or used.", map[string]any{}})
		}
	}
	exists := func(parts ...string) bool {
		_, err := os.Lstat(filepath.Join(parts...))
		return err == nil || !os.IsNotExist(err)
	}
	if exists(p.cdir, "info", "grafts") {
		add("grafts")
	}
	if exists(p.cdir, "shallow") {
		add("shallow")
	}
	if exists(p.cdir, "objects", "info", "alternates") {
		add("alternates")
	}
	if info, err := os.Stat(filepath.Join(p.cdir, "info", "attributes")); err == nil && info.Size() > 0 {
		add("info-attributes")
	}
	replace, err := p.git.run(ctx, "for-each-ref", "--count=1", "--format=%(refname)", "refs/replace/")
	if err != nil {
		return err
	}
	if replace.code != 0 || strings.TrimSpace(string(replace.out)) != "" {
		add("replace-refs")
	}
	if len(refusing) > 0 {
		return refuse("repository-hazard", "The repository configuration or metadata could start a helper or change what is merged.", map[string]any{"hazards": refusing})
	}
	return nil
}

// touchedPaths lists the paths changed by the moved commits, sorted and unique.
func (p *planner) touchedPaths(ctx context.Context) ([]string, error) {
	var stdin bytes.Buffer
	for _, c := range p.moved {
		stdin.WriteString(c.oid + "\n")
	}
	o, err := p.git.start(ctx, false, stdin.Bytes(), nil, commandOutputLimit, "diff-tree", "--stdin", "--no-commit-id", "--name-only", "-r", "-z", "--no-renames")
	if err != nil {
		return nil, err
	}
	if o.code != 0 {
		return nil, refuse("object-missing", "A moved commit is not readable.", nil)
	}
	return nulPaths(o.out)
}

func nulPaths(out []byte) ([]string, error) {
	var paths []string
	for _, f := range bytes.Split(out, []byte{0}) {
		if len(f) > 0 {
			paths = append(paths, string(f))
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	if len(paths) > MaxPaths {
		return nil, pathBound()
	}
	return paths, nil
}

// checkAttributes refuses a merge, diff or filter attribute on any moved path, in the tree of the
// branch tip and in the tree of the new parent, and a moved .gitattributes file (the merge would
// use attributes that no pinned tree describes).
func (p *planner) checkAttributes(ctx context.Context) error {
	paths, err := p.touchedPaths(ctx)
	if err != nil {
		return err
	}
	p.touched = paths
	var refusing []string
	for _, f := range paths {
		if path.Base(f) == ".gitattributes" && !slices.Contains(refusing, "attributes-changed") {
			refusing = append(refusing, "attributes-changed")
		}
	}
	if len(paths) > 0 {
		input := []byte(strings.Join(paths, "\x00") + "\x00")
		for _, tree := range []string{*p.res.Input.Branch.OldHead, *p.res.Input.NewParent.OID} {
			o, err := p.git.start(ctx, false, input, nil, commandOutputLimit, "check-attr", "--source="+tree, "-z", "--stdin", "merge", "diff", "filter")
			if err != nil {
				return err
			}
			if o.code != 0 {
				return refuse("repository-hazard", "The attributes of the moved paths are not readable.", map[string]any{"hazards": []string{"attributes-unreadable"}})
			}
			fields := bytes.Split(o.out, []byte{0})
			for i := 0; i+2 < len(fields); i += 3 {
				if name := "attribute-" + string(fields[i+1]); string(fields[i+2]) != "unspecified" && !slices.Contains(refusing, name) {
					refusing = append(refusing, name)
				}
			}
		}
	}
	if len(refusing) > 0 {
		slices.Sort(refusing)
		return refuse("repository-hazard", "An attribute of a moved path could select a merge driver or filter.", map[string]any{"hazards": refusing})
	}
	return nil
}

var operationStates = []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG", "sequencer"}

// checkTarget refuses a dirty or mid-operation target and a branch held by another worktree.
func (p *planner) checkTarget(ctx context.Context) error {
	for _, name := range operationStates {
		if _, err := os.Lstat(filepath.Join(p.gdir, name)); err == nil {
			return refuse("operation-in-progress", "The target checkout is in the middle of a Git operation.", map[string]any{"state": name})
		}
	}
	status, err := p.git.runInWorktree(ctx, "status", "--porcelain=v2", "--untracked-files=no", "--no-renames", "-z")
	if err != nil {
		return err
	}
	if status.code != 0 {
		return refuse("target-dirty", "The state of the target checkout could not be read.", nil)
	}
	if len(status.out) > 0 {
		return refuse("target-dirty", "The target checkout has uncommitted changes to tracked files.", nil)
	}
	return p.listWorktrees(ctx)
}

func (p *planner) listWorktrees(ctx context.Context) error {
	o, err := p.git.run(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	if o.code != 0 {
		return refuse("worktree-list-failed", "The registered worktrees could not be listed.", nil)
	}
	var current *Worktree
	var holder string
	flush := func() {
		if current == nil {
			return
		}
		switch {
		case current.Path == p.root:
			current.State = "target"
		case current.Branch != nil && *current.Branch == p.opts.Branch:
			current.State = "holds-branch"
			if holder == "" {
				holder = current.Path
			}
		default:
			current.State = "other"
		}
		p.res.Effects.Worktrees = append(p.res.Effects.Worktrees, *current)
		current = nil
	}
	for _, field := range bytes.Split(o.out, []byte{0}) {
		key, value, _ := strings.Cut(string(field), " ")
		switch {
		case key == "worktree":
			flush()
			current = &Worktree{Path: value}
		case key == "branch" && current != nil:
			branch := value
			current.Branch = &branch
		}
	}
	flush()
	if holder != "" {
		return refuse("branch-checked-out-elsewhere", "The branch is checked out in another worktree; it is reported and never touched.", map[string]any{"path": holder})
	}
	return nil
}
