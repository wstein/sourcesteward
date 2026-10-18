// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
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
	p, failure := loadReadyPolicy(target, path, expected, s, nil)
	if failure != nil {
		return p, failure
	}
	return p, nil
}

func loadReadyPolicy(target, path string, expected readiness.Repository, s *readySources, sources *[]readySource) (readyCallerPolicy, *readyPolicyFailure) {
	l, failure := readReadyLockTyped(target, path)
	lockSource := readySource{Name: "policy-lock", Outcome: readyOK, Complete: true}
	if failure != nil {
		lockSource = *failure.Source
	}
	recordReadyPolicySource(sources, lockSource)
	if failure != nil {
		return readyCallerPolicy{}, failure
	}
	v := l.Value()
	// Refuse before *any* network read. Candidate environment/repository/App
	// routing cannot select an activation channel or establish its authority.
	if v.Deployment.Profile != "none" {
		src := readySource{Name: "activation", Outcome: readyUnsupported}
		recordReadyPolicySource(sources, src)
		return readyCallerPolicy{}, readyPolicyBindingFailure("deployment-unverified", "activation-capability", &src)
	}
	if !validReadyLock(l) {
		return readyCallerPolicy{}, readyPolicyBindingFailure("lock-invalid", "lock-binding", &lockSource)
	}
	if s == nil || s.api == nil {
		return readyCallerPolicy{}, &readyPolicyFailure{Code: "repository-mismatch", Reason: "evidence-incomplete", Stage: "client"}
	}
	if s.api.client == nil || s.ctx == nil || s.budget == nil {
		return readyCallerPolicy{}, &readyPolicyFailure{Code: "policy-source-untrusted", Reason: "evidence-incomplete", Stage: "client"}
	}
	if !sameReadyRepository(v.Repository, expected) || !strings.EqualFold(s.repo.Host, expected.Host) || !strings.EqualFold(s.api.host, expected.Host) || !strings.EqualFold(s.repo.Owner+"/"+s.repo.Name, expected.FullName) {
		return readyCallerPolicy{}, readyPolicyBindingFailure("repository-mismatch", "repository-binding", &lockSource)
	}
	if stopped := s.stopped(s.ctx); stopped != readyOK {
		src := readySource{Name: "policy-loading", Outcome: stopped}
		recordReadyPolicySource(sources, src)
		return readyCallerPolicy{}, readyPolicySourceFailure("policy-source-untrusted", "collection-budget", src)
	}
	repo := s.repository()
	recordReadyPolicySource(sources, repo.Source)
	if repo.Source.Outcome != readyOK || !repo.Source.Complete {
		return readyCallerPolicy{}, readyPolicySourceFailure("policy-source-untrusted", "repository-read", repo.Source)
	}
	if repo.Value.ID != v.Repository.ID {
		return readyCallerPolicy{}, readyPolicyBindingFailure("policy-source-untrusted", "repository-id", &repo.Source)
	}
	parts := strings.Split(v.PolicySource.FullName, "/")
	source := s.forRepository(Repository{Host: v.Repository.Host, Owner: parts[0], Name: parts[1]})
	if strings.EqualFold(v.PolicySource.FullName, v.Repository.FullName) {
		if repo.Value.ID != v.PolicySource.RepositoryID {
			return readyCallerPolicy{}, readyPolicyBindingFailure("policy-source-untrusted", "policy-repository-id", &repo.Source)
		}
	} else {
		r := source.repository()
		recordReadyPolicySource(sources, r.Source)
		if r.Source.Outcome != readyOK || !r.Source.Complete {
			return readyCallerPolicy{}, readyPolicySourceFailure("policy-source-untrusted", "policy-repository-read", r.Source)
		}
		if r.Value.ID != v.PolicySource.RepositoryID {
			return readyCallerPolicy{}, readyPolicyBindingFailure("policy-source-untrusted", "policy-repository-id", &r.Source)
		}
	}
	doc := source.policy(v.PolicySource.Commit)
	recordReadyPolicySource(sources, doc.Source)
	if doc.Source.Outcome != readyOK || !doc.Source.Complete {
		return readyCallerPolicy{}, readyPolicySourceFailure("policy-source-untrusted", "policy-read", doc.Source)
	}
	// Compare parser-owned raw digest, never a reserialized JSON digest or an
	// untrusted envelope's claimed digest. The reader enforces fixed path/type.
	p, err := readiness.ParsePolicy(doc.Value.Raw)
	if err != nil {
		return readyCallerPolicy{}, readyPolicyBindingFailure("policy-invalid", "policy-parse", &doc.Source)
	}
	if p.SHA256() != v.PolicySource.SHA256 {
		return readyCallerPolicy{}, readyPolicyBindingFailure("policy-digest-mismatch", "policy-digest", &doc.Source)
	}
	pv := p.Value()
	if !sameReadyRepository(pv.Repository, v.Repository) || pv.Deployment.Profile != "none" || pv.Deployment.PublisherAppID != nil {
		return readyCallerPolicy{}, readyPolicyBindingFailure("policy-invalid", "policy-binding", &doc.Source)
	}
	return readyCallerPolicy{lock: l, policy: p}, nil
}

// resample performs one fixed reread using the same explicit selection and
// shared HTTP budget. No retry, fallback generation or favorable-state polling.
// T013 must propagate any failure to invalidate its complete review projection.
func (p readyCallerPolicy) resample(target, path string, expected readiness.Repository, s *readySources) error {
	next := p.resampleEvidence(target, path, expected, s)
	if next.Failure != nil {
		return next.Failure
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
	l, failure := readReadyLockTyped(target, path)
	if failure != nil {
		return l, failure
	}
	return l, nil
}

func readReadyLockTyped(target, path string) (readiness.Lock, *readyPolicyFailure) {
	invalid := func() (readiness.Lock, *readyPolicyFailure) {
		src := readySource{Name: "policy-lock", Outcome: readyMalformed}
		return readiness.Lock{}, readyPolicyBindingFailure("lock-invalid", "lock-input", &src)
	}
	if path == "" || target == "" {
		return invalid()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	targetReal, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	targetInfo, err := os.Stat(targetReal)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	if !targetInfo.IsDir() {
		return invalid()
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	resolved := filepath.Join(parent, filepath.Base(abs))
	rel, err := filepath.Rel(targetReal, resolved)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return invalid()
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	defer root.Close()
	parentInfo, err := root.Stat(".")
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	name := filepath.Base(abs)
	before, err := root.Lstat(name)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	if before.Size() > 16*1024 {
		return readiness.Lock{}, readyPolicySourceFailure("lock-invalid", "lock-input", readySource{Name: "policy-lock", Outcome: readyLimit})
	}
	if !before.Mode().IsRegular() {
		return invalid()
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return invalid()
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16*1024+1))
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	if len(raw) > 16*1024 {
		return readiness.Lock{}, readyPolicySourceFailure("lock-invalid", "lock-input", readySource{Name: "policy-lock", Outcome: readyLimit})
	}
	if int64(len(raw)) != before.Size() {
		return invalid()
	}
	after, err := root.Lstat(name)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	if !after.Mode().IsRegular() || !os.SameFile(opened, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return invalid()
	}
	// Recheck resolution after reading, including a moved/symlinked parent.
	endParent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	if endParent != parent {
		return invalid()
	}
	endParentInfo, err := os.Stat(endParent)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	if !os.SameFile(parentInfo, endParentInfo) {
		return invalid()
	}
	endTarget, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	if endTarget != targetReal {
		return invalid()
	}
	endTargetInfo, err := os.Stat(endTarget)
	if err != nil {
		return readiness.Lock{}, readyPolicyNativeFailure(err)
	}
	if !os.SameFile(targetInfo, endTargetInfo) {
		return invalid()
	}
	l, err := readiness.ParseLock(raw)
	if err != nil {
		src := readySource{Name: "policy-lock", Outcome: readyMalformed}
		return readiness.Lock{}, readyPolicyBindingFailure("lock-invalid", "lock-parse", &src)
	}
	return l, nil
}
