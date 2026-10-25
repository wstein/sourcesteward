// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"unicode/utf8"
)

const pruneV4Namespace = "tidy-registration-v4"

// RegistrationPruneV4Options names exactly one registration. Confirm is used
// only by a human planning another recorded owner's registration; it grants no apply authority.
type RegistrationPruneV4Options struct {
	Path, StateDir, Name, GitDir string
	Mode                         V3RunMode
	Confirm                      func(word, planID string) error
}

type PruneV4Registration struct {
	Name        string `json:"name"`
	GitDir      string `json:"gitdir"`
	Destination string `json:"destination"`
	Task        string `json:"task"`
	Owner       string `json:"owner"`
	OwnerSource string `json:"owner_source"`
	Scope       string `json:"scope"`
}
type PruneV4Directory struct {
	Identity PathIdentity `json:"identity"`
	UID      uint64       `json:"uid"`
	GID      uint64       `json:"gid"`
	Mode     uint32       `json:"mode"`
	Mount    string       `json:"mount"`
}
type PruneV4Absence struct {
	Destination string             `json:"destination"`
	Parent      PathIdentity       `json:"parent"`
	Ancestors   []PruneV4Directory `json:"ancestors"`
}
type PruneV4Metadata struct {
	Path     string             `json:"path"`
	Kind     string             `json:"kind"`
	Digest   string             `json:"digest"`
	Physical V3PhysicalIdentity `json:"physical"`
	Identity PathIdentity       `json:"identity"`
	Mode     uint32             `json:"mode"`
	Size     int64              `json:"size"`
}
type PruneV4Sibling struct {
	Name     string       `json:"name"`
	Identity PathIdentity `json:"identity"`
}

// PruneV4Expected keeps the shared v3 native state and snapshot typed. Future
// apply must consume the shared verified-prior-effect derivation (#285), not
// rewrite this immutable baseline or implement a second delta engine here.
type PruneV4Expected struct {
	CreationDigest        string            `json:"creation_digest"`
	CreationJournalDigest string            `json:"creation_journal_digest"`
	TaskDigest            string            `json:"task_digest"`
	SourceSnapshot        Snapshot          `json:"source_snapshot"`
	Native                V3NativeState     `json:"native"`
	Registration          PathIdentity      `json:"registration"`
	CommonDirMount        PruneV4Directory  `json:"common_dir_mount"`
	Absence               PruneV4Absence    `json:"absence"`
	Metadata              []PruneV4Metadata `json:"metadata"`
	MetadataBytes         int64             `json:"metadata_bytes"`
	ProtectedSiblings     []PruneV4Sibling  `json:"protected_siblings"`
	ResolvedHEAD          string            `json:"resolved_head"`
	HeadRef               string            `json:"head_ref"`
}
type RegistrationPruneV4Plan struct {
	SchemaVersion    int                 `json:"schema_version"`
	Operation        string              `json:"operation"`
	PlanID           string              `json:"plan_id"`
	Digest           string              `json:"digest"`
	SourcePath       string              `json:"source_path"`
	StateDir         string              `json:"state_dir"`
	Backend          string              `json:"backend"`
	Mode             V3RunMode           `json:"mode"`
	AuthorizedScopes []string            `json:"authorized_scopes"`
	Registration     PruneV4Registration `json:"registration"`
	Expected         PruneV4Expected     `json:"expected"`
	PayloadPath      string              `json:"payload_path"`
	Effects          []string            `json:"effects"`
	RecoveryLimits   []string            `json:"recovery_limits"`
}
type RegistrationPruneV4Result struct {
	SchemaVersion int                      `json:"schema_version"`
	Command       string                   `json:"command"`
	Operation     string                   `json:"operation"`
	Status        string                   `json:"status"`
	Backend       string                   `json:"backend"`
	Plan          *RegistrationPruneV4Plan `json:"plan"`
	Actions       []ActionOutcome          `json:"actions"`
	Diagnostics   []Diagnostic             `json:"diagnostics"`
	Limitations   []string                 `json:"limitations"`
}

func (r RegistrationPruneV4Result) Error() string {
	if len(r.Diagnostics) > 0 {
		return r.Diagnostics[0].Message
	}
	return "Registration prune planning refused."
}

func pruneV4Canonical(v any, omit string) ([]byte, error) {
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > v3RecordLimit {
		return nil, failure("record-limit", "Contract-4 record exceeds its encoded bound.")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if e = dec.Decode(&value); e != nil {
		return nil, e
	}
	if omit != "" {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, failure("invalid-state", "Digest requires a typed record.")
		}
		delete(m, omit)
	}
	return json.Marshal(value)
}
func pruneV4Hash(domain string, b []byte) string {
	h := sha256.New()
	_, _ = io.WriteString(h, domain+"\n")
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func pruneV4Digest(domain string, v any, omit string) (string, error) {
	b, e := pruneV4Canonical(v, omit)
	if e != nil {
		return "", e
	}
	return pruneV4Hash(domain, b), nil
}
func pruneV4ID(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func pruneV4ValidDirectory(d PruneV4Directory) bool {
	return v3CanonicalPath(d.Identity.Path) && d.Identity.Inode != 0 && d.Mount != "" && validText(d.Mount) && d.Mode != 0
}

// ReadRegistrationPruneV4Plan is a strict read-only operation. It never tries
// another namespace/version, creates recovery state, or grants apply authority.
func ReadRegistrationPruneV4Plan(state, id string) (RegistrationPruneV4Plan, error) {
	var p RegistrationPruneV4Plan
	if !pruneV4ID(id, 32) {
		return p, failure("prune-plan-refused", "Contract-4 plan ID must be 32 lowercase hex characters.")
	}
	parent, e := openStoreKinds(state, false, []string{pruneV4Namespace})
	if e != nil {
		return p, e
	}
	defer parent.Close()
	child, e := openStoreKinds(filepath.Join(state, pruneV4Namespace), false, []string{"plans"})
	if e != nil {
		return p, e
	}
	defer child.Close()
	if e = child.read("plans", id, &p); e != nil {
		return p, e
	}
	if e = validatePruneV4Plan(p, state, id); e != nil {
		return RegistrationPruneV4Plan{}, e
	}
	if e = parent.validate(); e != nil {
		return RegistrationPruneV4Plan{}, e
	}
	return p, nil
}
func validatePruneV4Plan(p RegistrationPruneV4Plan, state, id string) error {
	bad := func() error {
		return failure("prune-plan-refused", "Contract-4 plan version, operation, witness, scope or digest is invalid.")
	}
	if p.SchemaVersion != 4 || p.Operation != "registration-prune" || p.PlanID != id || !pruneV4ID(id, 32) || p.StateDir != state || !v3CanonicalPath(state) || !v3CanonicalPath(p.SourcePath) || p.Backend != "git" || !pruneV4ID(p.Digest, 64) {
		return bad()
	}
	if ValidateV3RunMode(&p.Mode) != nil || p.Mode.Caller == "" || p.AuthorizedScopes == nil || p.Effects == nil || !reflect.DeepEqual(p.Effects, []string{"remove-registration"}) || p.RecoveryLimits == nil || len(p.RecoveryLimits) == 0 {
		return bad()
	}
	r, e := p.Registration, p.Expected
	if !validText(r.Name) || r.Name == "" || filepath.Base(r.Name) != r.Name || !validID(r.Task) || !validID(r.Owner) || r.OwnerSource != "task-record" || !v3CanonicalPath(r.GitDir) || !v3CanonicalPath(r.Destination) || r.GitDir != filepath.Join(e.SourceSnapshot.Identity.RepositoryDir, "worktrees", r.Name) || e.Registration.Path != r.GitDir || e.SourceSnapshot.Identity.Backend != "git" || e.SourceSnapshot.Identity.WorkspaceRoot != p.SourcePath {
		return bad()
	}
	scope := "other"
	if p.Mode.Caller == r.Owner {
		scope = "own"
	}
	if r.Scope != scope {
		return bad()
	}
	if scope == "other" && (p.Mode.Mode != "human" || !reflect.DeepEqual(p.AuthorizedScopes, []string{"other"})) {
		return bad()
	}
	if scope == "own" && len(p.AuthorizedScopes) != 0 {
		return bad()
	}
	if !pruneV4ID(e.CreationDigest, 64) || !pruneV4ID(e.CreationJournalDigest, 64) || !pruneV4ID(e.TaskDigest, 64) || !pruneV4ValidDirectory(e.CommonDirMount) || e.CommonDirMount.Identity.Path != e.SourceSnapshot.Identity.RepositoryDir || e.Absence.Destination != r.Destination || e.Absence.Parent.Path != filepath.Dir(r.Destination) || len(e.Absence.Ancestors) == 0 {
		return bad()
	}
	if e.Metadata == nil || len(e.Metadata) == 0 || e.MetadataBytes < 0 || e.ProtectedSiblings == nil || e.Native.References == nil || e.Native.Registrations == nil || e.Native.ProtectedRoots == nil || p.PayloadPath != filepath.Join(state, pruneV4Namespace, "payloads", id) {
		return bad()
	}
	for _, d := range e.Absence.Ancestors {
		if !pruneV4ValidDirectory(d) {
			return bad()
		}
	}
	if e.Absence.Ancestors[len(e.Absence.Ancestors)-1].Identity != e.Absence.Parent {
		return bad()
	}
	sum := int64(0)
	last := ""
	for _, m := range e.Metadata {
		if m.Path == "" || m.Path <= last || m.Size < 0 || m.Size > 64<<20 || m.Kind != "file" && m.Kind != "directory" && m.Kind != "absent" || m.Kind == "file" && !pruneV4ID(m.Digest, 64) {
			return bad()
		}
		last = m.Path
		if m.Kind == "file" {
			sum += m.Size
		}
	}
	if sum != e.MetadataBytes || sum > 256<<20 || len(e.Metadata) > 1024 {
		return bad()
	}
	last = ""
	for _, s := range e.ProtectedSiblings {
		if s.Name == "" || s.Name <= last || s.Identity.Path != filepath.Join(e.SourceSnapshot.Identity.RepositoryDir, "worktrees", s.Name) || s.Identity.Inode == 0 {
			return bad()
		}
		last = s.Name
	}
	if e.ResolvedHEAD == "" || !fullGitOID(e.ResolvedHEAD, e.SourceSnapshot.Identity.ObjectFormat) {
		return bad()
	}
	if e.SourceSnapshot != v3SnapshotWithState(e.SourceSnapshot, e.Native) {
		return bad()
	}
	if !sort.StringsAreSorted(p.Mode.Markers) || !sort.StringsAreSorted(p.Mode.ByOwner) {
		return bad()
	}
	digest, err := pruneV4Digest("stw-registration-plan-v4", p, "digest")
	if err != nil || digest != p.Digest {
		return bad()
	}
	return nil
}

func writePruneV4Plan(parent, child *store, p RegistrationPruneV4Plan) error {
	if e := validatePruneV4Plan(p, p.StateDir, p.PlanID); e != nil {
		return e
	}
	raw, e := pruneV4Canonical(p, "")
	if e != nil {
		return e
	}
	raw = append(raw, '\n')
	if len(raw) > v3RecordLimit {
		return failure("record-limit", "Stored contract-4 plan exceeds its bound.")
	}
	if e = parent.validate(); e != nil {
		return e
	}
	if e = child.validate(); e != nil {
		return e
	}
	dir := child.dirs["plans"]
	name := p.PlanID + ".json"
	f, e := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollow, 0600)
	if e != nil {
		return failure("prune-store-refused", "Cannot exclusively create the private contract-4 plan.")
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = dir.Remove(name)
		}
	}()
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	if e == nil {
		e = f.Close()
	}
	if e == nil {
		e = parent.validate()
	}
	if e == nil {
		e = child.validate()
	}
	if e == nil {
		e = syncDirectory(dir)
	}
	if e != nil {
		return failure("prune-store-refused", "Cannot durably store the complete private contract-4 plan.")
	}
	ok = true
	return nil
}

func pruneV4Result() RegistrationPruneV4Result {
	return RegistrationPruneV4Result{SchemaVersion: 4, Command: "tidy-plan", Operation: "registration-prune", Status: "refused", Backend: "unknown", Actions: []ActionOutcome{}, Diagnostics: []Diagnostic{}, Limitations: []string{}}
}
func failPruneV4(r RegistrationPruneV4Result, e error) RegistrationPruneV4Result {
	code := tidyErrorCode(e)
	if code == "" {
		code = "prune-plan-refused"
	}
	r.Status = "refused"
	r.Plan = nil
	r.Diagnostics = []Diagnostic{{Code: code, Message: pruneV4Message(e)}}
	return r
}
func pruneV4Message(e error) string {
	var op operationError
	if errors.As(e, &op) {
		return op.Message
	}
	if errors.Is(e, context.Canceled) {
		return "Registration prune planning canceled."
	}
	if errors.Is(e, context.DeadlineExceeded) {
		return "Registration prune planning deadline exceeded."
	}
	return "Registration prune planning failed; native state is unchanged."
}

func validatePruneV4Options(o RegistrationPruneV4Options) error {
	if !validText(o.Path) || o.Path == "" || !v3CanonicalPath(o.StateDir) || !validText(o.Name) || o.Name == "" || filepath.Base(o.Name) != o.Name || o.Name == "." || o.Name == ".." || !v3CanonicalPath(o.GitDir) || !utf8.ValidString(o.Name) {
		return failure("prune-selection-refused", "Exact registration name, canonical gitdir and private state directory are required.")
	}
	if ValidateV3RunMode(&o.Mode) != nil || o.Mode.Caller == "" {
		return failure("prune-scope-refused", "Complete caller mode and a recorded owner label are required.")
	}
	return nil
}
