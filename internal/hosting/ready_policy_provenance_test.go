// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestReadyPolicyProvenanceHTTPStagesAndAccounting(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500} {
		for index, path := range []string{"/repos/o/r", "/repos/policies/r", "/repos/policies/r/contents/.stw/ready-policy.json?ref=" + readySHA} {
			t.Run(fmt.Sprintf("%d/%d", status, index), func(t *testing.T) {
				f := newCollectionFixture(t)
				f.change = func(p string, n int, raw string) (int, string) {
					if p == path {
						return status, `private-server-token`
					}
					return 200, raw
				}
				got := loadReadyPolicyEvidence(f.target, f.lockPath, f.lock.Repository, f.s)
				outcome, reason := readyDenied, "source-denied"
				if status == 404 {
					outcome, reason = readyNotFoundAmbiguous, "evidence-incomplete"
				}
				if status == 500 {
					outcome, reason = readyTransport, "source-failed"
				}
				if got.Failure == nil || got.Failure.Reason != reason || got.Failure.Source == nil || got.Failure.Source.Outcome != outcome || got.Requests != index+1 || got.Failure.Error() != "policy-source-untrusted" {
					t.Fatalf("%+v", got)
				}
				if got.Failure.Stage != []string{"repository-read", "policy-repository-read", "policy-read"}[index] || got.Failure.Source.Host != "github.com" {
					t.Fatal("actual producer identity lost")
				}
				count := 0
				for _, src := range got.Sources {
					count += src.Requests
					if src.Outcome == readyAbsentProven {
						t.Fatal("failure proved absence")
					}
				}
				if count != got.Requests || strings.Contains(fmt.Sprint(got), "private-server-token") || len(got.Value.policy.RawJSON()) != 0 {
					t.Fatal("accounting/privacy/partial snapshot failure")
				}
			})
		}
	}
}

func TestReadyPolicyProvenanceOpaqueErrorsAndNativePrivacy(t *testing.T) {
	f := newCollectionFixture(t)
	client, err := newSDKClient("github.com", "fixture-token", t.TempDir(), roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private-token /private/path") }))
	if err != nil {
		t.Fatal(err)
	}
	f.s.api.client = client
	got := loadReadyPolicyEvidence(f.target, f.lockPath, f.lock.Repository, f.s)
	if got.Failure == nil || got.Failure.Reason != "source-failed" || got.Failure.Source.Outcome != readyTransport || got.Requests != 1 || strings.Contains(fmt.Sprint(got), "private-token") {
		t.Fatal("opaque transport misclassified/disclosed")
	}
	_, err = readReadyLock(f.target, f.lockPath+"private-path-marker")
	var failure *readyPolicyFailure
	if !errors.As(err, &failure) || failure.Source.Outcome != readyNotFoundAmbiguous || failure.Reason != "evidence-incomplete" || strings.Contains(fmt.Sprintf("%+v", failure), "private-path-marker") {
		t.Fatal("native failure lost type/privacy")
	}
	if err := os.WriteFile(f.lockPath, []byte(`{"sensitive":"private-source-contents"}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = readReadyLock(f.target, f.lockPath)
	if !errors.As(err, &failure) || failure.Stage != "lock-parse" || failure.Reason != "lock-invalid" || strings.Contains(fmt.Sprint(failure), "private-source-contents") {
		t.Fatal("parse failure lost producer privacy")
	}
	for _, cause := range []error{os.ErrPermission, errors.New("private-native-token")} {
		failure = readyPolicyNativeFailure(cause)
		want := readyDenied
		if !errors.Is(cause, os.ErrPermission) {
			want = readyTransport
		}
		if failure.Source.Outcome != want || strings.Contains(fmt.Sprint(failure), "private-native-token") {
			t.Fatal("native producer error class leaked/collapsed")
		}
	}
}

func TestReadyPolicyProvenanceBoundsUnsupportedAndBindings(t *testing.T) {
	for _, kind := range []string{"canceled", "deadline", "requests", "installed", "missing-client", "digest", "transition", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			f := newCollectionFixture(t)
			reason := "source-limit"
			switch kind {
			case "canceled":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				f.s.ctx = ctx
				reason = "hosting-canceled"
			case "deadline":
				f.s.budget.started = time.Now().Add(-31 * time.Second)
			case "requests":
				f.s.budget.requests = 80
			case "installed":
				f.lock.Deployment.Profile = "app"
				env := "attacker"
				id := int64(99)
				f.lock.Deployment.Environment = &env
				f.lock.Deployment.PublisherAppID = &id
				reason = "deployment-unverified"
			case "missing-client":
				f.s.api = nil
				reason = "evidence-incomplete"
			case "digest":
				f.lock.PolicySource.SHA256 = strings.Repeat("1", 64)
				reason = "policy-digest-mismatch"
			case "transition":
				f.lock.Transition = transitionFixture(f.lock)
				f.lock.Transition.OldGeneration++
				reason = "lock-invalid"
			}
			if err := os.WriteFile(f.lockPath, loaderJSON(t, f.lock), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "oversize" {
				if err := os.WriteFile(f.lockPath, []byte(strings.Repeat(" ", 16385)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got := loadReadyPolicyEvidence(f.target, f.lockPath, f.lock.Repository, f.s)
			if got.Failure == nil || got.Failure.Reason != reason {
				t.Fatalf("%s: %+v", kind, got)
			}
			if kind == "installed" && len(f.calls) != 0 {
				t.Fatal("candidate activation routed HTTP")
			}
			if kind == "missing-client" && got.Failure.Source != nil {
				t.Fatal("missing upstream client invented source/authentication proof")
			}
			if (kind == "digest" || kind == "transition") && (!got.Failure.Source.Complete || got.Failure.Source.Outcome != readyOK) {
				t.Fatal("binding mismatch invented a failed source read")
			}
		})
	}
}

func TestReadyPolicyProvenanceResample(t *testing.T) {
	f := newCollectionFixture(t)
	initial := loadReadyPolicyEvidence(f.target, f.lockPath, f.lock.Repository, f.s)
	if initial.Failure != nil {
		t.Fatal(initial.Failure)
	}
	if err := os.WriteFile(f.lockPath, append(loaderJSON(t, f.lock), ' '), 0600); err != nil {
		t.Fatal(err)
	}
	final := initial.Value.resampleEvidence(f.target, f.lockPath, f.lock.Repository, f.s)
	if final.Failure == nil || final.Failure.Reason != "sample-changed" || len(final.Value.lock.RawJSON()) != 0 || final.Requests != 3 {
		t.Fatal("full raw resample drift lost")
	}
}
