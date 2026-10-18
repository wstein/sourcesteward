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

func TestReadyPolicyProvenanceGlobalBudgetAndLateStop(t *testing.T) {
	for _, seed := range []int{65, 66, 80} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			f := newCollectionFixture(t)
			f.s.budget.requests = seed
			got := f.collect()
			count := 0
			for _, src := range got.Sources {
				count += src.Requests
			}
			policy := 0
			for _, src := range got.PolicySources {
				policy += src.Requests
			}
			if policy != got.PolicyRequests || count+policy != got.Requests-seed || got.Requests > 80 {
				t.Fatal("global accounting double-counted or lost source requests")
			}
			if seed == 65 {
				if got.Review.State != "pass" || !got.Complete {
					t.Fatal("exact-budget positive refused")
				}
			} else {
				requireCollectionNonpass(t, got, "")
				if got.Complete {
					t.Fatal("incomplete budget passed")
				}
			}
		})
	}
	for _, kind := range []string{"canceled", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			f := newCollectionFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.s.ctx = ctx
			f.change = func(path string, n int, raw string) (int, string) {
				if path == "/repos/policies/r" && n == 2 {
					if kind == "canceled" {
						cancel()
					} else {
						f.s.budget.started = time.Now().Add(-31 * time.Second)
					}
				}
				return 200, raw
			}
			reason := "hosting-canceled"
			if kind == "deadline" {
				reason = "source-limit"
			}
			got := f.collect()
			requireCollectionNonpass(t, got, reason)
			last := got.PolicySources[len(got.PolicySources)-1]
			if last.Name != "repository" || last.Repository != "policies/r" || last.Complete {
				t.Fatal("late stop lost actual source")
			}
		})
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

func TestReadyPolicyProvenanceResampleAndCollectorConsumption(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprint(late), func(t *testing.T) {
			f := newCollectionFixture(t)
			f.change = func(path string, n int, raw string) (int, string) {
				if path == "/repos/policies/r" && (!late || n == 2) {
					return 403, `private-response`
				}
				return 200, raw
			}
			got := f.collect()
			requireCollectionNonpass(t, got, "source-denied")
			if got.Review.Reasons[0].Sources[0].Source != "repository" {
				t.Fatal("consumer replaced actual denied source with a generic label")
			}
			count := 0
			denied := false
			for _, src := range got.PolicySources {
				count += src.Requests
				if src.Name == "repository" && src.Repository == "policies/r" && src.Outcome == readyDenied {
					denied = true
				}
			}
			if count != got.PolicyRequests || !denied || got.Complete || strings.Contains(fmt.Sprint(got), "private-response") {
				t.Fatal("precise producer evidence lost/double-counted/leaked")
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
