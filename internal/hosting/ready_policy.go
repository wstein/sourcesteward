// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wstein/sourcesteward/internal/readiness"
)

// readyCallerPolicy is only an immutable caller-selected baseline. It is not an
// authenticated active generation. Review policy semantics belong to readiness;
// this loader establishes file/source bindings and profile consistency only.
//
// T012 is partial: no installed profile has an accepted independent bootstrap or
// owner-isolated activation capability. In particular readyActivationChannel's
// Accepted observation option is not such a capability. Adding installed loading
// requires the T024/T025 source/permission proof and a separately reviewed seam;
// neither a candidate lock nor a caller-supplied bool may construct that proof.
type readyCallerPolicy struct {
	lock   readiness.Lock
	policy readiness.Policy
}

func (p readyCallerPolicy) binding() readiness.PolicyBinding {
	l, v := p.lock.Value(), p.policy.Value()
	return readiness.PolicyBinding{ID: v.PolicyID, Version: v.Version, Commit: l.PolicySource.Commit, SHA256: p.policy.SHA256(), Generation: l.Generation, LockSHA256: p.lock.SHA256(), Checker: l.Checker, Authority: "caller-supplied"}
}

// Even an exactly bound transition in a caller-selected file grants no owner
// approval. The old policy/checker remain the governing baseline; no proposed
// policy/checker source is fetched here. Privileged-root clearance is deferred.
func (p readyCallerPolicy) transitionApproved() bool { return false }

func loadReadyCallerPolicy(target, path string, expected readiness.Repository, s *readySources) (readyCallerPolicy, error) {
	l, err := readReadyLock(target, path)
	if err != nil {
		return readyCallerPolicy{}, err
	}
	v := l.Value()
	// Refuse before *any* network read. Candidate environment/repository/App
	// routing cannot select an activation channel or establish its authority.
	if v.Deployment.Profile != "none" {
		return readyCallerPolicy{}, errors.New("deployment-unverified")
	}
	if !validReadyLock(l) {
		return readyCallerPolicy{}, errors.New("lock-invalid")
	}
	if !sameReadyRepository(v.Repository, expected) || s == nil || s.api == nil || !strings.EqualFold(s.repo.Host, expected.Host) || !strings.EqualFold(s.api.host, expected.Host) || !strings.EqualFold(s.repo.Owner+"/"+s.repo.Name, expected.FullName) {
		return readyCallerPolicy{}, errors.New("repository-mismatch")
	}
	repo := s.repository()
	if repo.Source.Outcome != readyOK || !repo.Source.Complete || repo.Value.ID != v.Repository.ID {
		return readyCallerPolicy{}, errors.New("policy-source-untrusted")
	}
	parts := strings.Split(v.PolicySource.FullName, "/")
	source := s.forRepository(Repository{Host: v.Repository.Host, Owner: parts[0], Name: parts[1]})
	if strings.EqualFold(v.PolicySource.FullName, v.Repository.FullName) {
		if repo.Value.ID != v.PolicySource.RepositoryID {
			return readyCallerPolicy{}, errors.New("policy-source-untrusted")
		}
	} else {
		r := source.repository()
		if r.Source.Outcome != readyOK || !r.Source.Complete || r.Value.ID != v.PolicySource.RepositoryID {
			return readyCallerPolicy{}, errors.New("policy-source-untrusted")
		}
	}
	doc := source.policy(v.PolicySource.Commit)
	if doc.Source.Outcome != readyOK || !doc.Source.Complete {
		return readyCallerPolicy{}, errors.New("policy-source-untrusted")
	}
	// Compare parser-owned raw digest, never a reserialized JSON digest or an
	// untrusted envelope's claimed digest. The reader enforces fixed path/type.
	p, err := readiness.ParsePolicy(doc.Value.Raw)
	if err != nil {
		return readyCallerPolicy{}, errors.New("policy-invalid")
	}
	if p.SHA256() != v.PolicySource.SHA256 {
		return readyCallerPolicy{}, errors.New("policy-digest-mismatch")
	}
	pv := p.Value()
	if !sameReadyRepository(pv.Repository, v.Repository) || pv.Deployment.Profile != "none" || pv.Deployment.PublisherAppID != nil {
		return readyCallerPolicy{}, errors.New("policy-invalid")
	}
	return readyCallerPolicy{lock: l, policy: p}, nil
}

// resample performs one fixed reread using the same explicit selection and
// shared HTTP budget. No retry, fallback generation or favorable-state polling.
// T013 must propagate any failure to invalidate its complete review projection.
func (p readyCallerPolicy) resample(target, path string, expected readiness.Repository, s *readySources) error {
	next, err := loadReadyCallerPolicy(target, path, expected, s)
	if err != nil {
		return err
	}
	if !sameReadyLockBytes(p.lock, next.lock) || !bytes.Equal(p.policy.RawJSON(), next.policy.RawJSON()) {
		return errors.New("sample-changed")
	}
	return nil
}

// sameReadyLockBytes is a comparison prerequisite, never authentication. Both
// values must come from the strict immutable parser; no field subset, matching
// generation, normalized JSON or digest-only shortcut can excuse changed bytes.
// An installed loader must independently authenticate its remote operand first.
func sameReadyLockBytes(a, b readiness.Lock) bool {
	return validReadyLock(a) && validReadyLock(b) && bytes.Equal(a.RawJSON(), b.RawJSON())
}

func sameReadyRepository(a, b readiness.Repository) bool {
	return a.ID == b.ID && a.ID > 0 && strings.EqualFold(a.Host, b.Host) && strings.EqualFold(a.FullName, b.FullName) && a.BaseRef == b.BaseRef
}

func validReadyLock(l readiness.Lock) bool {
	if len(l.RawJSON()) == 0 {
		return false
	}
	v := l.Value()
	if !validHost(v.Repository.Host) || !validRepo(v.Repository.FullName) || !readyBaseRef(v.Repository.BaseRef) || !validRepo(v.PolicySource.FullName) {
		return false
	}
	switch v.Deployment.Profile {
	case "none":
		if v.Deployment.Environment != nil || v.Deployment.PublisherAppID != nil {
			return false
		}
	case "app":
		if v.Deployment.Environment == nil || v.Deployment.PublisherAppID == nil {
			return false
		}
	}
	if t := v.Transition; t != nil && (t.RepositoryID != v.Repository.ID || t.OldGeneration != v.Generation || t.OldPolicySHA256 != v.PolicySource.SHA256) {
		return false
	}
	return true
}

// readReadyLock reads only an explicitly selected regular outside-target file.
// Rooted parent access confines path races; no-follow and nonblocking open flags
// avoid following a substituted final link or blocking on a substituted FIFO on
// the supported Linux/macOS hosts. Metadata is compared around the bounded read.
func readReadyLock(target, path string) (readiness.Lock, error) {
	invalid := func() (readiness.Lock, error) { return readiness.Lock{}, errors.New("lock-invalid") }
	if path == "" || target == "" {
		return invalid()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return invalid()
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return invalid()
	}
	targetReal, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		return invalid()
	}
	targetInfo, err := os.Stat(targetReal)
	if err != nil || !targetInfo.IsDir() {
		return invalid()
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return invalid()
	}
	resolved := filepath.Join(parent, filepath.Base(abs))
	rel, err := filepath.Rel(targetReal, resolved)
	if err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return invalid()
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return invalid()
	}
	defer root.Close()
	parentInfo, err := root.Stat(".")
	if err != nil {
		return invalid()
	}
	name := filepath.Base(abs)
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() > 16*1024 {
		return invalid()
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return invalid()
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return invalid()
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16*1024+1))
	if err != nil || len(raw) > 16*1024 || int64(len(raw)) != before.Size() {
		return invalid()
	}
	after, err := root.Lstat(name)
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(opened, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return invalid()
	}
	// Recheck resolution after reading, including a moved/symlinked parent.
	endParent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil || endParent != parent {
		return invalid()
	}
	endParentInfo, err := os.Stat(endParent)
	if err != nil || !os.SameFile(parentInfo, endParentInfo) {
		return invalid()
	}
	endTarget, err := filepath.EvalSymlinks(targetAbs)
	if err != nil || endTarget != targetReal {
		return invalid()
	}
	endTargetInfo, err := os.Stat(endTarget)
	if err != nil || !os.SameFile(targetInfo, endTargetInfo) {
		return invalid()
	}
	l, err := readiness.ParseLock(raw)
	if err != nil {
		return invalid()
	}
	return l, nil
}
