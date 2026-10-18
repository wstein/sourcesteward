// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wstein/sourcesteward/internal/readiness"
)

func policyLoaderFixture(t *testing.T) (readiness.LockData, []byte, readiness.Repository) {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile("../../specs/028-ready-check/contracts/examples/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	p, err := readiness.ParsePolicy(read("policy"))
	if err != nil {
		t.Fatal(err)
	}
	pv := p.Value()
	pv.Repository.FullName = "o/r"
	pv.Repository.ID = 1
	raw := loaderJSON(t, pv)
	l, err := readiness.ParseLock(read("lock"))
	if err != nil {
		t.Fatal(err)
	}
	v := l.Value()
	v.Repository = pv.Repository
	v.PolicySource.FullName = "policies/r"
	v.PolicySource.RepositoryID = 2
	v.PolicySource.SHA256 = document(raw).Digest
	return v, raw, pv.Repository
}
func loaderJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func loaderLockFile(t *testing.T, raw []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lock.json")
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func loaderSources(t *testing.T, policy []byte, sourceID int, calls *[]string) *readySources {
	t.Helper()
	return readyFixture(t, func(r *http.Request) (int, string) {
		*calls = append(*calls, r.URL.RequestURI())
		switch r.URL.RequestURI() {
		case "/repos/o/r":
			return 200, `{"id":1,"full_name":"o/r","default_branch":"main","allow_rebase_merge":true,"allow_merge_commit":false,"allow_squash_merge":true}`
		case "/repos/policies/r":
			return 200, fmt.Sprintf(`{"id":%d,"full_name":"policies/r","default_branch":"main","allow_rebase_merge":true,"allow_merge_commit":false,"allow_squash_merge":true}`, sourceID)
		case "/repos/policies/r/contents/.stw/ready-policy.json?ref=" + readySHA:
			return 200, fmt.Sprintf(`{"type":"file","encoding":"base64","size":%d,"content":%q}`, len(policy), base64.StdEncoding.EncodeToString(policy))
		default:
			t.Errorf("unexpected read %s", r.URL.RequestURI())
			return 500, `{}`
		}
	})
}
func TestReadyCallerPolicyLoadsPinnedRawBytes(t *testing.T) {
	l, policy, repo := policyLoaderFixture(t)
	raw := loaderJSON(t, l)
	path := loaderLockFile(t, raw)
	target := t.TempDir()
	calls := []string{}
	s := loaderSources(t, policy, 2, &calls)
	got, err := loadReadyCallerPolicy(target, path, repo, s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.lock.RawJSON(), raw) || !bytes.Equal(got.policy.RawJSON(), policy) || got.binding().Authority != "caller-supplied" || got.binding().LockSHA256 != document(raw).Digest {
		t.Fatal("raw binding or caller authority lost")
	}
	if got.transitionApproved() {
		t.Fatal("caller lock granted owner approval")
	}
	if len(calls) != 3 || s.budget.requests != 3 {
		t.Fatalf("requests: %v", calls)
	}
	if err := got.resample(target, path, repo, s); err != nil {
		t.Fatal(err)
	}
}
func TestReadyPolicyLockFilesystemRefusals(t *testing.T) {
	l, _, _ := policyLoaderFixture(t)
	raw := loaderJSON(t, l)
	target := t.TempDir()
	inside := filepath.Join(target, "lock")
	if err := os.WriteFile(inside, raw, 0600); err != nil {
		t.Fatal(err)
	}
	outside := loaderLockFile(t, raw)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "target")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing-selection": "", "inside": inside, "alias-inside": filepath.Join(alias, "lock"), "symlink": link, "directory": t.TempDir(), "missing": outside + "missing", "oversize": loaderLockFile(t, bytes.Repeat([]byte(" "), 16385))} {
		t.Run(name, func(t *testing.T) {
			if _, err := readReadyLock(target, path); err == nil {
				t.Fatal("unsafe lock accepted")
			}
		})
	}
}

func TestReadyPolicyRejectsNonDirectoryTarget(t *testing.T) {
	l, _, _ := policyLoaderFixture(t)
	path := loaderLockFile(t, loaderJSON(t, l))
	if _, err := readReadyLock(loaderLockFile(t, []byte("target")), path); err == nil {
		t.Fatal("non-directory target accepted")
	}
}

func TestReadyPolicyRejectsFIFOWithoutOpeningIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readReadyLock(t.TempDir(), path); err == nil {
		t.Fatal("nonregular lock accepted")
	}
}
func TestReadyPolicySourceAndIdentityRefusals(t *testing.T) {
	for _, kind := range []string{"source-id", "digest", "policy-repository", "lock-repository", "profile-mismatch", "none-with-app"} {
		t.Run(kind, func(t *testing.T) {
			l, policy, repo := policyLoaderFixture(t)
			id := 2
			switch kind {
			case "source-id":
				id = 99
			case "digest":
				policy = append(policy, ' ')
			case "policy-repository", "profile-mismatch":
				p, _ := readiness.ParsePolicy(policy)
				v := p.Value()
				if kind == "policy-repository" {
					v.Repository.ID = 99
				} else {
					v.Deployment.Profile = "app"
					n := int64(7)
					v.Deployment.PublisherAppID = &n
				}
				policy = loaderJSON(t, v)
				l.PolicySource.SHA256 = document(policy).Digest
			case "lock-repository":
				l.Repository.ID = 99
			case "none-with-app":
				n := int64(7)
				l.Deployment.PublisherAppID = &n
			}
			calls := []string{}
			s := loaderSources(t, policy, id, &calls)
			if _, err := loadReadyCallerPolicy(t.TempDir(), loaderLockFile(t, loaderJSON(t, l)), repo, s); err == nil {
				t.Fatal("unbound policy accepted")
			}
		})
	}
}
func TestReadyInstalledProfilesNeverUseCandidateActivationRoute(t *testing.T) {
	for _, profile := range []string{"app", "required-workflow", "protected-source"} {
		t.Run(profile, func(t *testing.T) {
			l, _, repo := policyLoaderFixture(t)
			l.Deployment.Profile = profile
			env := "attacker_channel"
			app := int64(789)
			l.Deployment.Environment = &env
			l.Deployment.PublisherAppID = &app
			// Existing reader's Accepted flag is only an observation option. It is
			// deliberately absent from the loader API and cannot grant capability.
			channel := readyActivationChannel{Profile: profile, Environment: env, Accepted: true}
			if !channel.Accepted {
				t.Fatal("invalid negative control")
			}
			s := readyFixture(t, func(*http.Request) (int, string) { t.Fatal("candidate caused a network read"); return 500, `{}` })
			_, err := loadReadyCallerPolicy(t.TempDir(), loaderLockFile(t, loaderJSON(t, l)), repo, s)
			if err == nil || err.Error() != "deployment-unverified" {
				t.Fatalf("%v", err)
			}
		})
	}
}
func transitionFixture(l readiness.LockData) *readiness.Transition {
	return &readiness.Transition{RepositoryID: l.Repository.ID, Number: 42, HeadSHA: strings.Repeat("e", 40), OldGeneration: l.Generation, OldPolicySHA256: l.PolicySource.SHA256, NewPolicyCommit: strings.Repeat("f", 40), NewPolicySHA256: strings.Repeat("1", 64), NewCheckerSourceSHA: strings.Repeat("2", 40), NewCheckerArtifactSHA256: strings.Repeat("3", 64)}
}
func TestReadyCompleteLockComparisonR3(t *testing.T) {
	l, _, _ := policyLoaderFixture(t)
	l.Transition = transitionFixture(l)
	raw := loaderJSON(t, l)
	baseline, err := readiness.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !sameReadyLockBytes(baseline, baseline) {
		t.Fatal("equal control rejected")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(loaderJSON(t, l.Transition), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 9 {
		t.Fatal("transition field coverage changed")
	}
	for field := range fields {
		t.Run(field, func(t *testing.T) {
			var candidate map[string]any
			_ = json.Unmarshal(raw, &candidate)
			tr := candidate["transition"].(map[string]any)
			switch v := tr[field].(type) {
			case float64:
				tr[field] = v + 1
			case string:
				tr[field] = strings.Repeat("4", len(v))
			}
			other, e := readiness.ParseLock(loaderJSON(t, candidate))
			if e != nil {
				t.Fatal(e)
			}
			if sameReadyLockBytes(baseline, other) {
				t.Fatal("field subset authenticated")
			}
		})
	}
	l.Transition = nil
	null, _ := readiness.ParseLock(loaderJSON(t, l))
	space, _ := readiness.ParseLock(append(raw, ' '))
	for _, other := range []readiness.Lock{null, space, {}} {
		if sameReadyLockBytes(baseline, other) {
			t.Fatal("null/whitespace/zero equality")
		}
	}
	missing := bytes.Replace(raw, append([]byte(`,"transition":`), loaderJSON(t, transitionFixture(l))...), nil, 1)
	if bytes.Equal(raw, missing) {
		t.Fatal("missing-field negative did not change fixture")
	}
	if _, e := readiness.ParseLock(missing); e == nil {
		t.Fatal("missing transition accepted")
	}
}
func TestReadyCallerPolicyResampleRejectsAllR3Drift(t *testing.T) {
	l, policy, repo := policyLoaderFixture(t)
	l.Transition = transitionFixture(l)
	raw := loaderJSON(t, l)
	var fields map[string]any
	_ = json.Unmarshal(loaderJSON(t, l.Transition), &fields)
	for _, field := range append([]string{"whitespace", "null", "generation"}, func() []string {
		a := []string{}
		for k := range fields {
			a = append(a, k)
		}
		return a
	}()...) {
		t.Run(field, func(t *testing.T) {
			path := loaderLockFile(t, raw)
			target := t.TempDir()
			calls := []string{}
			s := loaderSources(t, policy, 2, &calls)
			got, e := loadReadyCallerPolicy(target, path, repo, s)
			if e != nil {
				t.Fatal(e)
			}
			var v map[string]any
			_ = json.Unmarshal(raw, &v)
			switch field {
			case "null":
				v["transition"] = nil
			case "generation":
				v["generation"] = float64(2)
			default:
				if field != "whitespace" {
					tr := v["transition"].(map[string]any)
					switch x := tr[field].(type) {
					case float64:
						tr[field] = x + 1
					case string:
						tr[field] = strings.Repeat("4", len(x))
					}
				}
			}
			next := loaderJSON(t, v)
			if field == "whitespace" {
				next = append(raw, ' ')
			}
			if e := os.WriteFile(path, next, 0600); e != nil {
				t.Fatal(e)
			}
			if e := got.resample(target, path, repo, s); e == nil {
				t.Fatal("drift preserved sample")
			}
			if got.transitionApproved() {
				t.Fatal("caller transition granted approval")
			}
		})
	}
}

func TestReadyCallerPolicyUnavailableSourcesNeverReturnSnapshot(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			l, _, repo := policyLoaderFixture(t)
			s := readyFixture(t, func(*http.Request) (int, string) { return status, `secret response body` })
			got, err := loadReadyCallerPolicy(t.TempDir(), loaderLockFile(t, loaderJSON(t, l)), repo, s)
			if err == nil || len(got.lock.RawJSON()) != 0 || len(got.policy.RawJSON()) != 0 || strings.Contains(err.Error(), "secret") {
				t.Fatal("unavailable source retained a snapshot or disclosed response")
			}
		})
	}
}

func TestReadyCallerPolicySameRepositoryStillVerifiesImmutableSourceID(t *testing.T) {
	for _, id := range []int64{1, 99} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			l, policy, repo := policyLoaderFixture(t)
			l.PolicySource.FullName = "o/r"
			l.PolicySource.RepositoryID = id
			calls := 0
			s := readyFixture(t, func(r *http.Request) (int, string) {
				calls++
				switch r.URL.RequestURI() {
				case "/repos/o/r":
					return 200, `{"id":1,"full_name":"o/r","default_branch":"main","allow_rebase_merge":true,"allow_merge_commit":false,"allow_squash_merge":true}`
				case "/repos/o/r/contents/.stw/ready-policy.json?ref=" + readySHA:
					return 200, fmt.Sprintf(`{"type":"file","encoding":"base64","size":%d,"content":%q}`, len(policy), base64.StdEncoding.EncodeToString(policy))
				default:
					t.Fatal("unapproved endpoint")
					return 500, `{}`
				}
			})
			_, err := loadReadyCallerPolicy(t.TempDir(), loaderLockFile(t, loaderJSON(t, l)), repo, s)
			if (err == nil) != (id == 1) || id == 1 && calls != 2 || id != 1 && calls != 1 {
				t.Fatalf("same-repository binding err=%v requests=%d", err, calls)
			}
		})
	}
}

func TestReadyCallerPolicyFinalRemoteFailureAndRawDrift(t *testing.T) {
	for _, kind := range []string{"unavailable", "whitespace"} {
		t.Run(kind, func(t *testing.T) {
			l, policy, repo := policyLoaderFixture(t)
			path, target := loaderLockFile(t, loaderJSON(t, l)), t.TempDir()
			calls := []string{}
			got, err := loadReadyCallerPolicy(target, path, repo, loaderSources(t, policy, 2, &calls))
			if err != nil {
				t.Fatal(err)
			}
			var final *readySources
			if kind == "unavailable" {
				final = readyFixture(t, func(*http.Request) (int, string) { return 403, `{}` })
			} else {
				final = loaderSources(t, append(policy, ' '), 2, &calls)
			}
			if err := got.resample(target, path, repo, final); err == nil {
				t.Fatal("failed final source preserved a successful sample")
			}
		})
	}
}
