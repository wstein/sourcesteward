// SPDX-License-Identifier: EUPL-1.2

// Package owner implements the owner scope of the stw tidy wizard (Spec 041): the derivation of an
// item's owner and scope, run-mode detection, the content-addressed plan id that binds
// authorized_scopes, and the typed confirmation read from /dev/tty. Ownership filters the view;
// it is not a safety exclusion, and mode detection is an accident guard, not a security boundary.
package owner

import (
	"path/filepath"
	"strings"
)

// Scope relates an item owner to the caller label.
type Scope string

// The scopes of an item.
const (
	ScopeOwn     Scope = "own"
	ScopeOther   Scope = "other"
	ScopeUnknown Scope = "unknown"
)

// Source says how an owner label was derived (owner_source).
type Source string

// The owner sources, in derivation order.
const (
	SourceTaskRecord     Source = "task-record"
	SourcePathConvention Source = "path-convention"
	SourceCommitAuthor   Source = "commit-author"
	SourceUnknown        Source = "unknown"
)

// Item is what the owner derivation reads: the owner label of the stw creation record or lock
// (empty when there is none), the worktree path, the branch and the branch tip author.
type Item struct {
	TaskOwner  string
	Path       string
	Branch     string
	AuthorHint string
}

// Owner is the derived owner of an item.
type Owner struct {
	Label  string
	Source Source
	Scope  Scope
}

type prefixLabel struct{ prefix, label string }

var (
	pathTable = []prefixLabel{{"/tmp/whr", "codex"}}
	// branchTable maps branch prefixes to a label.
	branchTable = []prefixLabel{{"codex/", "codex"}, {"claude/", "claude"}, {"claude-", "claude"}}
)

// Resolve derives the owner of an item; the first match wins: task record, path or branch
// convention, commit author hint, unknown. In agent mode only a task record can make an item own.
// A commit author is a hint (label "author:<name>") and never grants own.
func Resolve(it Item, caller string, agent bool) Owner {
	if it.TaskOwner != "" {
		return Owner{it.TaskOwner, SourceTaskRecord, scopeOf(it.TaskOwner, caller)}
	}
	if label := conventionLabel(it); label != "" {
		scope := scopeOf(label, caller)
		if agent && scope == ScopeOwn {
			scope = ScopeUnknown
		}
		return Owner{label, SourcePathConvention, scope}
	}
	if it.AuthorHint != "" {
		return Owner{"author:" + it.AuthorHint, SourceCommitAuthor, ScopeUnknown}
	}
	return Owner{"", SourceUnknown, ScopeUnknown}
}

func conventionLabel(it Item) string {
	if it.Path != "" {
		p := filepath.Clean(it.Path)
		for _, e := range pathTable {
			if strings.HasPrefix(p, e.prefix) {
				return e.label
			}
		}
	}
	for _, e := range branchTable {
		if strings.HasPrefix(it.Branch, e.prefix) {
			return e.label
		}
	}
	return ""
}

func scopeOf(label, caller string) Scope {
	if caller != "" && label == caller {
		return ScopeOwn
	}
	return ScopeOther
}
