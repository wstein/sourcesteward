// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"errors"
	"os"

	"github.com/wstein/sourcesteward/internal/readiness"
)

// Provenance contains only fixed classifications and bounded reader metadata,
// never a path, raw error/body, token or authentication/activation assertion.
// A nil Source means upstream proof is unavailable, not proven absent.
type readyPolicyFailure struct {
	Code, Reason, Stage string
	Source              *readySource
}

func (f *readyPolicyFailure) Error() string { return f.Code }

func (f *readyPolicyFailure) referenceSource() string {
	if f.Source != nil {
		return f.Source.Name
	}
	return "policy"
}

type readyPolicyLoad struct {
	Value    readyCallerPolicy
	Sources  []readySource
	Requests int
	Failure  *readyPolicyFailure
}

func readyPolicyBindingFailure(code, stage string, source *readySource) *readyPolicyFailure {
	f := &readyPolicyFailure{Code: code, Reason: code, Stage: stage}
	if source != nil {
		copy := *source
		f.Source = &copy
	}
	return f
}

func readyPolicySourceFailure(code, stage string, source readySource) *readyPolicyFailure {
	f := readyPolicyBindingFailure(code, stage, &source)
	f.Reason = readyCollectionCode(source.Outcome)
	if source.Outcome == readyTransport {
		f.Reason = "source-failed"
	}
	return f
}

func readyPolicyNativeFailure(err error) *readyPolicyFailure {
	outcome := readyTransport
	if errors.Is(err, os.ErrPermission) {
		outcome = readyDenied
	}
	if errors.Is(err, os.ErrNotExist) {
		outcome = readyNotFoundAmbiguous
	}
	return readyPolicySourceFailure("lock-invalid", "lock-io", readySource{Name: "policy-lock", Outcome: outcome})
}

func recordReadyPolicySource(sources *[]readySource, source readySource) {
	if sources != nil {
		*sources = append(*sources, source)
	}
}

func loadReadyPolicyEvidence(target, path string, expected readiness.Repository, s *readySources) (out readyPolicyLoad) {
	before := 0
	if s != nil && s.budget != nil {
		before = s.budget.requests
	}
	out.Value, out.Failure = loadReadyPolicy(target, path, expected, s, &out.Sources)
	if s != nil && s.budget != nil {
		out.Requests = s.budget.requests - before
	}
	return
}

func (p readyCallerPolicy) resampleEvidence(target, path string, expected readiness.Repository, s *readySources) readyPolicyLoad {
	out := loadReadyPolicyEvidence(target, path, expected, s)
	if out.Failure == nil && (!sameReadyLockBytes(p.lock, out.Value.lock) || !bytes.Equal(p.policy.RawJSON(), out.Value.policy.RawJSON())) {
		out.Failure = readyPolicyBindingFailure("sample-changed", "resample", &out.Sources[0])
		out.Value = readyCallerPolicy{}
	}
	return out
}
