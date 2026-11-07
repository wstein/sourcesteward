// SPDX-License-Identifier: EUPL-1.2

package land

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// embeddedShapes is the single allowlist of measured repository shapes (Spec 043 decision 9,
// FR-031, FR-067). One data file, reviewed in a pull request, the only place. It stays empty until
// the owner-run measurement of research.md (T002, T021) records a shape with its evidence; an
// empty list means every apply is refused with land-unmeasured.
//
//go:embed shapes.json
var embeddedShapes []byte

// Shape is the normalized repository shape the allowlist is keyed by, never by repository name.
type Shape struct {
	Stack               bool
	DeleteBranchOnMerge string // true, false or unknown
	Protection          string // none, present or unreadable
	RuleTypes           []string
	BypassModes         []string
	// Readable is false when the rules, rulesets or protection could not be read (a 403 or a plan
	// limit). An unreadable shape never matches the allowlist.
	Readable bool
}

func normalized(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// ShapeOf normalizes the facts read from the forge. stack is true for the chain landing: a shape
// measured for the single landing does not allow the chain.
func ShapeOf(repo RepoFacts, rs RulesetFacts, stack bool) Shape {
	s := Shape{Stack: stack, DeleteBranchOnMerge: "unknown", Protection: rs.Protection, RuleTypes: normalized(rs.RuleTypes), BypassModes: normalized(rs.BypassModes)}
	if repo.DeleteBranchOnMerge != nil {
		s.DeleteBranchOnMerge = fmt.Sprint(*repo.DeleteBranchOnMerge)
	}
	if s.Protection == "" {
		s.Protection = "unreadable"
	}
	// delete_branch_on_merge is absent without administration read; the landing order does not
	// depend on it (FR-089), so unknown stays readable and is recorded as the value unknown.
	s.Readable = rs.Readable && s.Protection != "unreadable"
	return s
}

// Key is the canonical text form of the shape.
func (s Shape) Key() string {
	return fmt.Sprintf("stack=%t;delete_branch_on_merge=%s;protection=%s;rules=%s;bypass=%s",
		s.Stack, s.DeleteBranchOnMerge, s.Protection, strings.Join(s.RuleTypes, ","), strings.Join(s.BypassModes, ","))
}

// ShapeSet is a set of recorded shape keys.
type ShapeSet struct{ keys, native map[string]bool }

// NewShapeSet builds a set from keys (tests and the loader).
func NewShapeSet(keys ...string) *ShapeSet {
	set := &ShapeSet{keys: map[string]bool{}, native: map[string]bool{}}
	for _, k := range keys {
		set.keys[k] = true
	}
	return set
}

// Len is the number of recorded shapes.
func (s *ShapeSet) Len() int { return len(s.keys) }

// Has reports whether the shape is recorded as measured. An unreadable shape never matches.
func (s *ShapeSet) Has(shape Shape) bool {
	return s != nil && shape.Readable && s.keys[shape.Key()]
}

type shapeFile struct {
	SchemaVersion int `json:"schema_version"`
	Shapes        []struct {
		Key      string `json:"key"`
		Stack    bool   `json:"stack"`
		Native   bool   `json:"native"`
		Evidence string `json:"evidence"`
	} `json:"shapes"`
}

// LoadShapes parses an allowlist file. Every entry needs its key, the stack flag matching the key
// and a pointer to the recorded measurement.
func LoadShapes(raw []byte) (*ShapeSet, error) {
	var f shapeFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("shapes.json: %w", err)
	}
	if f.SchemaVersion != 1 {
		return nil, errors.New("shapes.json: unsupported schema_version")
	}
	set := NewShapeSet()
	for i, e := range f.Shapes {
		if e.Key == "" || e.Evidence == "" {
			return nil, fmt.Errorf("shapes.json: entry %d needs key and evidence", i)
		}
		if e.Stack != strings.HasPrefix(e.Key, "stack=true;") {
			return nil, fmt.Errorf("shapes.json: entry %d: stack flag does not match the key", i)
		}
		if e.Native && !e.Stack {
			return nil, fmt.Errorf("shapes.json: entry %d: native needs stack", i)
		}
		set.keys[e.Key] = true
		set.native[e.Key] = e.Native
	}
	return set, nil
}

// EmbeddedShapes returns the allowlist compiled into the binary; a malformed file is an empty set
// (fail closed).
func EmbeddedShapes() *ShapeSet {
	set, err := LoadShapes(embeddedShapes)
	if err != nil {
		return NewShapeSet()
	}
	return set
}
