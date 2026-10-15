// SPDX-License-Identifier: EUPL-1.2
package inspection

import "strings"

type Evidence struct {
	Kind     string `json:"kind"`
	Location string `json:"location"`
}
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Head struct {
	State string  `json:"state"`
	Ref   *string `json:"ref"`
	OID   *string `json:"oid"`
}
type Repository struct {
	GitDir         string  `json:"git_dir"`
	CommonDir      string  `json:"common_dir"`
	Worktree       *string `json:"worktree"`
	Bare           bool    `json:"bare"`
	LinkedWorktree bool    `json:"linked_worktree"`
	ObjectFormat   string  `json:"object_format"`
	Head           Head    `json:"head"`
}
type Result struct {
	SchemaVersion int          `json:"schema_version"`
	Status        string       `json:"status"`
	Backend       string       `json:"backend"`
	InputPath     string       `json:"input_path"`
	Evidence      []Evidence   `json:"evidence"`
	Repository    *Repository  `json:"repository"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

func (r Result) Error() string {
	if len(r.Diagnostics) == 0 {
		return "inspection failed"
	}
	return r.Diagnostics[0].Message
}
func (r *Result) fail(code, msg string) Result {
	r.Status = "error"
	r.Repository = nil
	r.Diagnostics = []Diagnostic{{code, msg}}
	return *r
}
func ptr(s string) *string       { return &s }
func terminated(s string) string { return strings.TrimSuffix(s, "\n") }
