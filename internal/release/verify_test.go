// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyRequiresFilesAndOnline(t *testing.T) {
	d, _ := planRepo(t)
	git(t, d, "tag", "-a", "v1.2.3", "-m", "release")
	r := Verify(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: d, Subject: "v1.2.3"}})
	if r.Command != "release-verify" || r.Status != "ok" || findGate(r.PlanResult, "artifact-missing") != "not-run" || r.Verdict != "fail" {
		t.Fatalf("%+v", r)
	}
}

type onlineFixture struct{ fail string }

func (s onlineFixture) VerifyRelease(_ context.Context, _ VerifyOptions, _ ReleaseRecord, _ *Contract) ([]Gate, []AttestationObservation, error) {
	g := []Gate{}
	for _, code := range []string{"ci-not-green", "version-not-monotonic", "already-published", "draft-missing", "attestation-missing", "attestation-invalid"} {
		outcome := "pass"
		if code == s.fail {
			outcome = "fail"
		}
		g = append(g, gate(code, outcome, "fixture", ""))
	}
	return g, []AttestationObservation{}, nil
}
func verificationFixture(t *testing.T) (string, string) {
	t.Helper()
	d, c, dir := recordCommandFixture(t)
	r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir})
	if r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	write(t, d, r.Path, r.Canonical)
	end := commitAll(t, d)
	git(t, d, "remote", "add", "origin", "https://github.com/wstein/sourcesteward.git")
	git(t, d, "update-ref", "refs/remotes/origin/main", end)
	git(t, d, "tag", "-a", "v0.2.0", "-m", "release")
	return d, dir
}
func TestVerificationMatrixOnlineGates(t *testing.T) {
	d, dir := verificationFixture(t)
	for _, fail := range []string{"", "ci-not-green", "version-not-monotonic", "already-published", "draft-missing", "attestation-missing", "attestation-invalid"} {
		r := VerifyWithOnline(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: d, Subject: "v0.2.0", Online: true}, Artifacts: dir}, onlineFixture{fail})
		want := "pass"
		if fail != "" {
			want = "fail"
		}
		if r.Status != "ok" || r.Verdict != want {
			t.Fatalf("%s: %+v", fail, r.PlanResult)
		}
	}
}
func TestVerifyChecksumCoverageAndFileMutation(t *testing.T) {
	for _, what := range []string{"modified", "missing", "extra", "symlink", "checksums"} {
		t.Run(what, func(t *testing.T) {
			d, dir := verificationFixture(t)
			files, e := os.ReadDir(dir)
			if e != nil {
				t.Fatal(e)
			}
			asset := ""
			for _, f := range files {
				if f.Name() != "SHA256SUMS" && f.Name() != "build-manifest.json" {
					asset = f.Name()
					break
				}
			}
			switch what {
			case "modified":
				os.WriteFile(filepath.Join(dir, asset), []byte("changed"), 0644)
			case "missing":
				os.Remove(filepath.Join(dir, asset))
			case "extra":
				os.WriteFile(filepath.Join(dir, "extra"), nil, 0644)
			case "symlink":
				os.Remove(filepath.Join(dir, asset))
				os.Symlink("absent", filepath.Join(dir, asset))
			case "checksums":
				os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(strings.Repeat("0", 64)+"  forged\n"), 0644)
			}
			r := VerifyWithOnline(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: d, Subject: "v0.2.0", Online: true}, Artifacts: dir}, onlineFixture{})
			if r.Status != "ok" || r.Verdict != "fail" {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestTagPlanVerifyOfflineEffects(t *testing.T) {
	d, dir := verificationFixture(t)
	makeIndexStale(t, d)
	markers := t.TempDir()
	plantSentinels(t, d, markers)
	log := logGit(t)
	count, proxy := listener(t)
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, proxy)
	}
	before := snapshot(t, d)
	for _, kind := range []string{"plan", "verify"} {
		var p PlanResult
		if kind == "plan" {
			p = Plan(context.Background(), PlanOptions{Path: d, Subject: "v0.2.0"})
		} else {
			p = Verify(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: d, Subject: "v0.2.0"}, Artifacts: dir}).PlanResult
		}
		if p.Status != "ok" {
			t.Fatalf("%+v", p.Error)
		}
	}
	if diff := differences(before, snapshot(t, d)); len(diff) > 0 {
		t.Fatal(diff)
	}
	if fired, _ := filepath.Glob(filepath.Join(markers, "fired-*")); len(fired) > 0 {
		t.Fatal(fired)
	}
	if count.Load() != 0 {
		t.Fatal("offline network attempted")
	}
	raw, e := os.ReadFile(log)
	if e != nil {
		t.Fatal(e)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "ARGS ") {
			continue
		}
		sub := gitSubcommand(strings.TrimPrefix(line, "ARGS "))
		if !detectionGit[sub] && !readGit[sub] && sub != "merge-base" && sub != "diff-tree" && sub != "config" && sub != "remote" {
			t.Fatalf("unexpected process %s", line)
		}
	}
}

func TestVerifyPublishedSignerOverride(t *testing.T) {
	d, c, dir := recordCommandFixture(t)
	r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir, SignerWorkflowFile: ".github/workflows/release-bootstrap.yml"})
	if r.Status != "ok" {
		t.Fatal(r.Error)
	}
	write(t, d, r.Path, r.Canonical)
	end := commitAll(t, d)
	git(t, d, "remote", "add", "origin", "https://github.com/wstein/sourcesteward.git")
	git(t, d, "update-ref", "refs/remotes/origin/main", end)
	git(t, d, "tag", "-a", "v0.2.0", "-m", "release")
	v := Verify(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: d, Subject: "v0.2.0"}, Artifacts: dir, Published: true, SignerWorkflowFile: ".github/workflows/release-bootstrap.yml"})
	if findGate(v.PlanResult, "record-invalid") != "pass" {
		t.Fatal(v.Gates)
	}
	for _, code := range []string{"already-published", "draft-missing", "version-not-monotonic"} {
		if findGate(v.PlanResult, code) != "not-applicable" {
			t.Fatal(v.Gates)
		}
	}
}

func TestVerificationMatrixSC004LocalGates(t *testing.T) {
	for _, mutation := range []string{"lightweight", "not-main", "code", "workflow", "config", "mode", "symlink", "gitlink", "wrong-version"} {
		t.Run(mutation, func(t *testing.T) {
			d, c, dir := recordCommandFixture(t)
			rr := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir})
			if rr.Status != "ok" {
				t.Fatal(rr.Error)
			}
			switch mutation {
			case "code":
				write(t, d, "code.go", []byte("changed"))
			case "workflow":
				write(t, d, ".github/workflows/ci.yml", []byte("changed"))
			case "config":
				write(t, d, ".stw/release.toml", append(exampleBytes(t, "go-cli"), []byte("\n# modified\n")...))
			case "mode":
				if e := os.Chmod(filepath.Join(d, "docs/guide.md"), 0755); e != nil {
					t.Fatal(e)
				}
			case "symlink":
				if e := os.Symlink("guide.md", filepath.Join(d, "docs/link")); e != nil {
					t.Fatal(e)
				}
			case "wrong-version":
				rr.Record.Version = "v9.9.9"
				rr.Canonical, _ = EncodeReleaseDocument(rr.Record)
			}
			write(t, d, rr.Path, rr.Canonical)
			git(t, d, "add", "-A")
			if mutation == "gitlink" {
				git(t, d, "update-index", "--add", "--cacheinfo", "160000,"+c+",docs/module")
			}
			git(t, d, "commit", "-qm", "record and mutation")
			end := strings.TrimSpace(git(t, d, "rev-parse", "HEAD"))
			git(t, d, "remote", "add", "origin", "https://github.com/wstein/sourcesteward.git")
			main := end
			if mutation == "not-main" {
				main = c
			}
			git(t, d, "update-ref", "refs/remotes/origin/main", main)
			if mutation == "lightweight" {
				git(t, d, "tag", "v0.2.0")
			} else {
				git(t, d, "tag", "-a", "v0.2.0", "-m", "release")
			}
			v := VerifyWithOnline(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: d, Subject: "v0.2.0", Online: true}, Artifacts: dir}, onlineFixture{})
			code := "diff-not-allowlisted"
			switch mutation {
			case "lightweight":
				code = "tag-not-annotated"
			case "not-main":
				code = "tag-not-on-main"
			case "wrong-version":
				code = "tag-version-mismatch"
			}
			if v.Status != "ok" || v.Verdict != "fail" || findGate(v.PlanResult, code) != "fail" {
				t.Fatalf("%+v", v.PlanResult)
			}
		})
	}
}

func TestVerifyEscapesArtifactReadFailure(t *testing.T) {
	d, _ := verificationFixture(t)
	hostile := filepath.Join(t.TempDir(), "missing\nforged\x1b")
	r := Verify(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: d, Subject: "v0.2.0"}, Artifacts: hostile})
	if r.Error == nil {
		t.Fatal("missing read error")
	}
	if strings.ContainsAny(r.Error.Message, "\n\x1b") {
		t.Fatalf("raw hostile text in error: %q", r.Error.Message)
	}
}
