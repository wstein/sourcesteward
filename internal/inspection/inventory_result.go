// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

const inventoryRecordLimit = 1024
const inventoryResultLimit = 1024 * 1024

type InventoryOptions struct {
	Query, Ref, From, To, Base, Revision string
	Remotes, Scopes, Paths               []string
	Anchor                               string
}
type InventoryObservation struct {
	Value  string `json:"value"`
	Reason string `json:"reason"`
}
type InventoryLimits struct {
	DeadlineSeconds int `json:"deadline_seconds"`
	CommandBytes    int `json:"command_bytes"`
	ResultBytes     int `json:"result_bytes"`
	Records         int `json:"records"`
}
type InventoryBoundary struct {
	Complete    bool            `json:"complete"`
	Freshness   string          `json:"freshness"`
	Limitations []string        `json:"limitations"`
	Limits      InventoryLimits `json:"limits"`
}
type InventoryWorkspace struct {
	Path         *string              `json:"path"`
	Name         string               `json:"name"`
	Manager      string               `json:"manager"`
	Registration string               `json:"registration"`
	TaskOwner    string               `json:"task_owner"`
	CommitID     *string              `json:"commit_id"`
	ChangeID     *string              `json:"change_id"`
	OperationID  *string              `json:"operation_id"`
	Tracked      InventoryObservation `json:"tracked"`
	Untracked    InventoryObservation `json:"untracked"`
	Conflicts    InventoryObservation `json:"conflicts"`
	Limitations  []string             `json:"limitations"`
}
type InventoryTarget struct {
	CommitID string  `json:"commit_id"`
	ChangeID *string `json:"change_id"`
}
type InventoryReference struct {
	Name             string               `json:"name"`
	Kind             string               `json:"kind"`
	Remote           *string              `json:"remote"`
	CommitID         *string              `json:"commit_id"`
	ChangeID         *string              `json:"change_id"`
	Upstream         *string              `json:"upstream"`
	Upstreams        []string             `json:"upstreams"`
	AddedTargets     []InventoryTarget    `json:"added_targets"`
	RemovedTargets   []InventoryTarget    `json:"removed_targets"`
	Tracked          InventoryObservation `json:"tracked"`
	ContainsRevision InventoryObservation `json:"contains_revision"`
	MergedIntoBase   InventoryObservation `json:"merged_into_base"`
	LocalOnly        InventoryObservation `json:"local_only"`
	Limitations      []string             `json:"limitations"`
}
type InventoryBase struct {
	Name        string   `json:"name"`
	CommitID    *string  `json:"commit_id"`
	ChangeID    *string  `json:"change_id"`
	Provenance  string   `json:"provenance"`
	Selected    bool     `json:"selected"`
	Limitations []string `json:"limitations"`
}
type InventoryFile struct {
	Path        string   `json:"path"`
	OldPath     *string  `json:"old_path"`
	Scope       string   `json:"scope"`
	Change      string   `json:"change"`
	Limitations []string `json:"limitations"`
}
type InventoryQuery struct {
	Predicate string   `json:"predicate"`
	Value     string   `json:"value"`
	Reason    string   `json:"reason"`
	Evidence  []string `json:"evidence"`
}
type InventoryResult struct {
	SchemaVersion int                  `json:"schema_version"`
	Command       string               `json:"command"`
	Status        string               `json:"status"`
	Backend       string               `json:"backend"`
	InputPath     string               `json:"input_path"`
	Identity      Result               `json:"identity"`
	Boundary      InventoryBoundary    `json:"boundary"`
	Workspaces    []InventoryWorkspace `json:"workspaces"`
	References    []InventoryReference `json:"references"`
	Bases         []InventoryBase      `json:"bases"`
	Files         []InventoryFile      `json:"files"`
	Query         *InventoryQuery      `json:"query"`
	Diagnostics   []Diagnostic         `json:"diagnostics"`
}

func newInventoryResult(identity Result) InventoryResult {
	return InventoryResult{SchemaVersion: 1, Command: "inventory", Status: "ok", Backend: identity.Backend, InputPath: identity.InputPath, Identity: identity,
		Boundary:   InventoryBoundary{Complete: true, Freshness: "sampled-nontransactional", Limitations: []string{"Sampled observations are not transactional; concurrent and ABA changes may be missed."}, Limits: InventoryLimits{10, outputLimit, inventoryResultLimit, inventoryRecordLimit}},
		Workspaces: []InventoryWorkspace{}, References: []InventoryReference{}, Bases: []InventoryBase{}, Files: []InventoryFile{}, Diagnostics: []Diagnostic{}}
}
func known(value bool) InventoryObservation      { return InventoryObservation{fmt.Sprint(value), ""} }
func unknown(reason string) InventoryObservation { return InventoryObservation{"unknown", reason} }
func (r InventoryResult) Error() string {
	if len(r.Diagnostics) > 0 {
		return r.Diagnostics[0].Message
	}
	if len(r.Boundary.Limitations) > 0 {
		return r.Boundary.Limitations[len(r.Boundary.Limitations)-1]
	}
	return "inventory incomplete"
}
func (r *InventoryResult) partial(reason string) {
	if r.Status != "error" {
		r.Status = "partial"
	}
	r.Boundary.Complete = false
	for _, v := range r.Boundary.Limitations {
		if v == reason {
			return
		}
	}
	r.Boundary.Limitations = append(r.Boundary.Limitations, reason)
}
func (r *InventoryResult) fail(code, msg string) InventoryResult {
	r.Status = "error"
	r.Boundary.Complete = false
	r.Diagnostics = append(r.Diagnostics, Diagnostic{code, msg})
	if r.Query != nil {
		r.Query.Value = "unknown"
		r.Query.Reason = msg
	}
	return *r
}
func checkInventoryLimits(r InventoryResult) error {
	n := len(r.Workspaces) + len(r.References) + len(r.Bases) + len(r.Files)
	for _, ref := range r.References {
		n += len(ref.AddedTargets) + len(ref.RemovedTargets) + len(ref.Upstreams)
	}
	if n > inventoryRecordLimit {
		return errors.New("inventory record limit exceeded")
	}
	budget := inventoryResultLimit
	if err := inventoryJSONBudget(reflect.ValueOf(r), &budget); err != nil {
		return err
	}

	return nil
}
func inventoryPathSelected(path string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, filter := range filters {
		if filter == "." || path == filter || strings.HasPrefix(path, filter+"/") {
			return true
		}
	}
	return false
}

// inventoryJSONBudget checks a conservative encoded-size bound before encoding allocates.
// All shipped result fields are fixed typed values; unsupported kinds fail closed.
func inventoryJSONBudget(v reflect.Value, remaining *int) error {
	spend := func(n int) error {
		if n > *remaining {
			return errors.New("inventory result byte limit exceeded")
		}
		*remaining -= n
		return nil
	}
	if !v.IsValid() {
		return spend(4)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return spend(4)
		}
		return inventoryJSONBudget(v.Elem(), remaining)
	case reflect.String:
		s := v.String()
		if len(s) > *remaining {
			return errors.New("inventory result byte limit exceeded")
		}
		if !utf8.ValidString(s) {
			return errors.New("inventory metadata is not valid UTF-8")
		}
		if e := spend(2); e != nil {
			return e
		}
		for _, r := range s {
			n := utf8.RuneLen(r)
			switch r {
			case '\b', '\f', '\n', '\r', '\t', '"', '\\':
				n = 2
			case '<', '>', '&', '\u2028', '\u2029':
				n = 6
			default:
				if r < 0x20 {
					n = 6
				}
			}
			if e := spend(n); e != nil {
				return e
			}
		}
		return nil
	case reflect.Bool:
		return spend(5)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return spend(20)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return spend(4)
		}
		if e := spend(2); e != nil {
			return e
		}
		for i := 0; i < v.Len(); i++ {
			if e := spend(1); e != nil {
				return e
			}
			if e := inventoryJSONBudget(v.Index(i), remaining); e != nil {
				return e
			}
		}
		return nil
	case reflect.Struct:
		if e := spend(2); e != nil {
			return e
		}
		typ := v.Type()
		for i := 0; i < v.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("json")
			name, _, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = typ.Field(i).Name
			}
			if e := spend(len(name) + 4); e != nil {
				return e
			}
			if e := inventoryJSONBudget(v.Field(i), remaining); e != nil {
				return e
			}
		}
		return nil
	default:
		return errors.New("unsupported inventory result value")
	}
}
