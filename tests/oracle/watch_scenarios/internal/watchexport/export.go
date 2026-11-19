// SPDX-License-Identifier: EUPL-1.2

// Package watchexport is the shared part of the scenario exporter of `stw watch` (tests/oracle/watch_scenarios).
// It lives inside a COPY of the Go module (internal/ packages are importable only there) and is never part of
// the Go product. It writes deterministic JSON files: object keys sorted, two-space indent, no HTML escaping,
// one trailing newline.
package watchexport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FormatVersion is the version of the scenario format described in FORMAT.md.
const FormatVersion = 1

// Kinds of scenarios (FORMAT.md).
const (
	KindCycles   = "watch-cycles"
	KindVectors  = "vectors"
	KindObserver = "observer-session"
	KindHTTP     = "http-reader"
	KindRunLoop  = "run-loop"
)

// Source says which Go tests a scenario was derived from.
type Source struct {
	GoTests []string `json:"go_tests"`
}

// Common is the head every scenario starts with.
type Common struct {
	FormatVersion int    `json:"format_version"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Description   string `json:"description"`
	Source        Source `json:"source"`
	// FormatDependent lists the JSON field names (anywhere in the file) whose values are artifacts of the Go
	// file formats (ids, journal sequence numbers, hashes). A Rust port may differ there.
	FormatDependent []string `json:"format_dependent"`
}

// NewCommon builds the head. goTests are "file.go:TestName" strings relative to internal/.
func NewCommon(name, kind, desc string, goTests ...string) Common {
	return Common{FormatVersion: FormatVersion, Name: name, Kind: kind, Description: desc, Source: Source{GoTests: goTests}, FormatDependent: []string{}}
}

// Marshal renders v the way every scenario file is rendered.
func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// OutDir returns the target directory from the environment and creates it.
func OutDir() (string, error) {
	d := os.Getenv("STW_EXPORT_DIR")
	if d == "" || !filepath.IsAbs(d) {
		return "", fmt.Errorf("STW_EXPORT_DIR must be an absolute path, got %q", d)
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// Write stores one scenario as <dir>/<name>.json. The name is restricted to [a-z0-9._-].
func Write(dir, name string, v any) error {
	if name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789._-") != "" || strings.HasPrefix(name, ".") {
		return fmt.Errorf("bad scenario name %q", name)
	}
	raw, err := Marshal(v)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name+".json")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("scenario %s written twice", name)
	}
	return os.WriteFile(path, raw, 0o644)
}

// IndexEntry is one line of index.json.
type IndexEntry struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	File        string   `json:"file"`
	Description string   `json:"description"`
	GoTests     []string `json:"go_tests"`
	Size        int      `json:"size"` // cycles, cases or steps, whatever the kind counts
}

// Index is index.json.
type Index struct {
	FormatVersion int            `json:"format_version"`
	Count         int            `json:"count"`
	ByKind        map[string]int `json:"by_kind"`
	Scenarios     []IndexEntry   `json:"scenarios"`
}

// BuildIndex reads every <dir>/*.json except index.json and writes index.json.
func BuildIndex(dir string) (Index, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return Index{}, err
	}
	sort.Strings(files)
	idx := Index{FormatVersion: FormatVersion, ByKind: map[string]int{}, Scenarios: []IndexEntry{}}
	for _, f := range files {
		if filepath.Base(f) == "index.json" {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return idx, err
		}
		var m struct {
			Common
			Cycles []json.RawMessage `json:"cycles"`
			Cases  []json.RawMessage `json:"cases"`
			Steps  []json.RawMessage `json:"steps"`
			Calls  []json.RawMessage `json:"calls"`
			Invs   []json.RawMessage `json:"invocations"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return idx, fmt.Errorf("%s: %w", f, err)
		}
		size := len(m.Cycles) + len(m.Cases) + len(m.Steps) + len(m.Calls) + len(m.Invs)
		idx.Scenarios = append(idx.Scenarios, IndexEntry{Name: m.Name, Kind: m.Kind, File: filepath.Base(f), Description: m.Description, GoTests: m.Source.GoTests, Size: size})
		idx.ByKind[m.Kind]++
	}
	idx.Count = len(idx.Scenarios)
	raw, err := Marshal(idx)
	if err != nil {
		return idx, err
	}
	return idx, os.WriteFile(filepath.Join(dir, "index.json"), raw, 0o644)
}

// Case is one row of a vector scenario: the arguments of a pure function and what the Go code returned.
type Case struct {
	Name     string `json:"name"`
	Input    any    `json:"input"`
	Expected any    `json:"expected"`
}

// VectorDoc is a scenario of kind "vectors": one function, many cases.
type VectorDoc struct {
	Common
	Function string `json:"function"`
	Cases    []Case `json:"cases"`
}

// NewVectors starts a vector scenario. dependent names JSON fields that are artifacts of the Go file formats.
func NewVectors(name, function, desc string, dependent []string, goTests ...string) *VectorDoc {
	c := NewCommon(name, KindVectors, desc, goTests...)
	if dependent != nil {
		c.FormatDependent = dependent
	}
	return &VectorDoc{Common: c, Function: function, Cases: []Case{}}
}

// Add appends a case.
func (v *VectorDoc) Add(name string, input, expected any) {
	if name == "" {
		name = "(empty)"
	}
	v.Cases = append(v.Cases, Case{name, input, expected})
}
