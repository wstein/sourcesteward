// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/inspection"
)

const recordLimit = 1024 * 1024
const observationLimit = 1024

type PlanOptions struct{ Path, Base, Destination, Task, Owner, StateDir string }
type ApplyOptions struct {
	StateDir, PlanID string
	Authorize        bool
}
type Options struct{ Path, StateDir string }
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type ActionOutcome struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
type NativeIdentity struct {
	Backend       string `json:"backend"`
	RepositoryDir string `json:"repository_dir"`
	WorkspaceRoot string `json:"workspace_root"`
	WorkspaceName string `json:"workspace_name"`
	GitDir        string `json:"git_dir"`
	HeadRef       string `json:"head_ref"`
	CommitID      string `json:"commit_id"`
	ChangeID      string `json:"change_id"`
	OperationID   string `json:"operation_id"`
	ObjectFormat  string `json:"object_format"`
}
type Snapshot struct {
	Identity           NativeIdentity `json:"identity"`
	BaseSelector       string         `json:"base_selector"`
	BaseCommit         string         `json:"base_commit"`
	BaseChange         string         `json:"base_change"`
	ReferenceDigest    string         `json:"reference_digest"`
	RegistrationDigest string         `json:"registration_digest"`
	SafetyDigest       string         `json:"safety_digest"`
}
type PathIdentity struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}
type MutationPlan struct {
	SchemaVersion       int           `json:"schema_version"`
	PlanID              string        `json:"plan_id"`
	TaskID              string        `json:"task_id"`
	Owner               string        `json:"owner"`
	SourcePath          string        `json:"source_path"`
	Destination         string        `json:"destination"`
	StateDir            string        `json:"state_dir"`
	Backend             string        `json:"backend"`
	Reason              string        `json:"reason"`
	Digest              string        `json:"digest"`
	Expected            Snapshot      `json:"expected"`
	DestinationParent   PathIdentity  `json:"destination_parent"`
	DestinationIdentity *PathIdentity `json:"destination_identity"`
	Effects             []string      `json:"effects"`
	RecoveryLimits      []string      `json:"recovery_limits"`
}
type NativeWorkspace struct {
	Identity        NativeIdentity `json:"identity"`
	Complete        bool           `json:"complete"`
	SourceUnchanged bool           `json:"source_unchanged"`
	Phase           string         `json:"phase"`
	Effects         []string       `json:"effects"`
	Limitations     []string       `json:"limitations"`
}
type TaskRecord struct {
	SchemaVersion       int                         `json:"schema_version"`
	TaskID              string                      `json:"task_id"`
	Owner               string                      `json:"owner"`
	PlanID              string                      `json:"plan_id"`
	Backend             string                      `json:"backend"`
	Destination         string                      `json:"destination"`
	RecoveryPointer     string                      `json:"recovery_pointer"`
	Native              NativeIdentity              `json:"native"`
	DestinationIdentity PathIdentity                `json:"destination_identity"`
	BaseCommit          string                      `json:"base_commit"`
	BaseChange          string                      `json:"base_change"`
	State               string                      `json:"state"`
	Inventory           *inspection.InventoryResult `json:"inventory"`
	RemainingWork       []string                    `json:"remaining_work"`
	RecoveryLimits      []string                    `json:"recovery_limits"`
}
type Result struct {
	SchemaVersion int             `json:"schema_version"`
	Command       string          `json:"command"`
	Status        string          `json:"status"`
	Backend       string          `json:"backend"`
	Plan          *MutationPlan   `json:"plan"`
	Task          *TaskRecord     `json:"task"`
	Actions       []ActionOutcome `json:"actions"`
	Diagnostics   []Diagnostic    `json:"diagnostics"`
	Limitations   []string        `json:"limitations"`
}
type nativeAdapter interface {
	Observe(context.Context, string, string) (Snapshot, error)
	Create(context.Context, MutationPlan) (NativeWorkspace, error)
	Verify(context.Context, MutationPlan) (NativeWorkspace, error)
}
type operationError struct{ Code, Message string }

func (e operationError) Error() string   { return e.Message }
func failure(code, message string) error { return operationError{code, message} }
func (r Result) Error() string {
	if len(r.Diagnostics) > 0 {
		return r.Diagnostics[0].Message
	}
	return "workspace operation failed"
}
func newResult(command string) Result {
	return Result{SchemaVersion: 1, Command: command, Status: "error", Backend: "unknown", Actions: []ActionOutcome{}, Diagnostics: []Diagnostic{}, Limitations: []string{}}
}
func refuse(r Result, code, message string) Result {
	r.Status = "refused"
	r.Diagnostics = []Diagnostic{{code, message}}
	return r
}
func failResult(r Result, err error) Result {
	var e operationError
	code, message := "workspace-failed", "workspace operation could not complete"
	if errors.As(err, &e) {
		code, message = e.Code, e.Message
	} else if err != nil {
		// An unclassified internal failure still names its cause; the text is
		// the bounded, valid-UTF-8 prefix of the error, never a stack or a path
		// listing.
		if cause := causeText(err); cause != "" {
			message += ": " + cause
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code, message = "canceled", "workspace operation canceled or deadline exceeded"
	}
	r.Status = "error"
	r.Diagnostics = []Diagnostic{{code, message}}
	return r
}
func causeText(err error) string {
	text := strings.TrimSpace(err.Error())
	if len(text) > 240 {
		text = strings.ToValidUTF8(text[:240], "")
	}
	if !validText(text) || strings.ContainsAny(text, "\r\n") {
		return ""
	}
	return text
}
func validID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for i, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		if i > 0 && (c == '.' || c == '_' || c == '-') {
			continue
		}
		return false
	}
	return true
}
func validText(s string) bool {
	return len(s) <= 4096 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func bounded(r Result) Result {
	b, e := json.Marshal(r)
	if e == nil && len(b) <= recordLimit && resultRecordCount(r) <= observationLimit {
		return r
	}
	recovery := ""
	if r.Plan != nil && validText(r.Plan.StateDir) && validID(r.Plan.PlanID) {
		recovery = "Recovery journal: " + r.Plan.StateDir + "/operations/" + r.Plan.PlanID + ".json"
	}
	r.Status = "partial"
	r.Diagnostics = []Diagnostic{{"result-limit", "Workspace output exceeded its one-MiB bound; omitted evidence cannot authorize recovery."}}
	if r.Task != nil {
		r.Task.Inventory = nil
		r.Task.RemainingWork = []string{"Inventory omitted because the result bound was exceeded; retain the workspace."}
	}
	b, e = json.Marshal(r)
	if e == nil && len(b) <= recordLimit && resultRecordCount(r) <= observationLimit {
		return r
	}
	r.Plan = nil
	r.Task = nil
	if len(r.Actions) > observationLimit-8 {
		r.Actions = r.Actions[:observationLimit-8]
	}
	for i := range r.Actions {
		r.Actions[i].Name = shortText(r.Actions[i].Name, 32)
		switch r.Actions[i].Status {
		case "completed", "incomplete", "refused", "planned":
		default:
			r.Actions[i].Status = "incomplete"
		}
		r.Actions[i].Detail = "Effect evidence abbreviated; inspect the durable operation journal."
	}
	r.Limitations = []string{"Output evidence was bounded; inspect durable state before deciding any recovery."}
	if recovery != "" {
		r.Limitations = append(r.Limitations, recovery)
	}
	b, e = json.Marshal(r)
	if e == nil && len(b) <= recordLimit && resultRecordCount(r) <= observationLimit {
		return r
	}
	// The fixed envelope is the final fail-closed bound, including JSON escaping.
	safe := newResult("workspace-apply")
	switch r.Command {
	case "workspace-plan", "workspace-apply", "workspace-handoff", "workspace-retire-plan":
		safe.Command = r.Command
	}
	switch r.Backend {
	case "git", "jj", "unknown", "ambiguous", "gitbutler":
		safe.Backend = r.Backend
	}
	safe.Status = "partial"
	safe.Diagnostics = r.Diagnostics
	safe.Actions = []ActionOutcome{{"known-effects", "incomplete", "Some completed effects were omitted from bounded output; inspect the durable journal."}}
	safe.Limitations = []string{"No omitted evidence authorizes mutation or recovery."}
	if recovery != "" {
		safe.Limitations = append(safe.Limitations, recovery)
	}
	return safe
}

func shortText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

func resultRecordCount(r Result) int {
	count := len(r.Actions) + len(r.Diagnostics) + len(r.Limitations)
	if r.Plan != nil {
		count += len(r.Plan.Effects) + len(r.Plan.RecoveryLimits)
	}
	if r.Task != nil {
		count += len(r.Task.RemainingWork) + len(r.Task.RecoveryLimits)
		if i := r.Task.Inventory; i != nil {
			count += len(i.Workspaces) + len(i.References) + len(i.Bases) + len(i.Files) + len(i.Diagnostics)
		}
	}
	return count
}
