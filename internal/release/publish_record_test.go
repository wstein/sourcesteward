// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"strings"
	"testing"
)

func TestRecordCommitModeAndSourceMutation(t *testing.T) {
	for _, mutation := range []string{"valid", "source", "config", "not-descendant", "not-main", "wrong-built-commit"} {
		t.Run(mutation, func(t *testing.T) {
			d, c, dir := recordCommandFixture(t)
			rr := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir})
			if rr.Status != "ok" {
				t.Fatal(rr.Error)
			}
			git(t, d, "tag", "-a", "v0.2.0", "-m", "tag-first")
			if mutation == "source" {
				write(t, d, "source.go", []byte("changed"))
			}
			if mutation == "config" {
				write(t, d, ".stw/release.toml", append(exampleBytes(t, "go-cli"), []byte("\n# changed\n")...))
			}
			if mutation == "wrong-built-commit" {
				rr.Record.Commit = strings.Repeat("f", 40)
				rr.Canonical, _ = EncodeReleaseDocument(rr.Record)
			}
			write(t, d, rr.Path, rr.Canonical)
			recordCommit := commitAll(t, d)
			git(t, d, "remote", "add", "origin", "https://github.com/wstein/sourcesteward.git")
			main := recordCommit
			if mutation == "not-main" {
				main = c
			}
			git(t, d, "update-ref", "refs/remotes/origin/main", main)
			if mutation == "not-descendant" {
				git(t, d, "checkout", "--orphan", "other")
				git(t, d, "rm", "-rf", ".")
				write(t, d, rr.Path, rr.Canonical)
				recordCommit = commitAll(t, d)
				git(t, d, "update-ref", "refs/remotes/origin/main", recordCommit)
			}
			r := Plan(context.Background(), PlanOptions{Path: d, Subject: "v0.2.0", RecordCommit: recordCommit})
			if r.Status != "ok" {
				t.Fatalf("%+v", r)
			}
			if mutation == "valid" {
				if findGate(r, "build-commit-not-ancestor") != "pass" || findGate(r, "diff-not-allowlisted") != "pass" {
					t.Fatal(r.Gates)
				}
			} else if mutation == "source" || mutation == "config" {
				if findGate(r, "diff-not-allowlisted") != "fail" {
					t.Fatal(r.Gates)
				}
			} else if findGate(r, "build-commit-not-ancestor") != "fail" {
				t.Fatal(r.Gates)
			}
		})
	}
}

func TestPublishAcceptsReplayedDocumentationCommit(t *testing.T) {
	d, c, dir := recordCommandFixture(t)
	r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir})
	if r.Status != "ok" {
		t.Fatal(r.Error)
	}
	git(t, d, "checkout", "-qb", "record-review")
	write(t, d, r.Path, r.Canonical)
	original := commitAll(t, d)
	git(t, d, "checkout", "-q", "main")
	write(t, d, "docs/prerequisite.md", []byte("another documentation change\n"))
	commitAll(t, d)
	git(t, d, "cherry-pick", original)
	end := strings.TrimSpace(git(t, d, "rev-parse", "HEAD"))
	if original == end {
		t.Fatal("fixture did not rewrite the reviewed SHA")
	}
	oldAncestor, e := gitAncestor(context.Background(), d, original, end)
	if e != nil || oldAncestor {
		t.Fatalf("original commit unexpectedly retained: %v %v", oldAncestor, e)
	}
	git(t, d, "remote", "add", "origin", "https://github.com/wstein/sourcesteward.git")
	git(t, d, "update-ref", "refs/remotes/origin/main", end)
	git(t, d, "tag", "-a", "v0.2.0", "-m", "release")
	p := Plan(context.Background(), PlanOptions{Path: d, Subject: "v0.2.0"})
	if findGate(p, "build-commit-not-ancestor") != "pass" || findGate(p, "diff-not-allowlisted") != "pass" {
		t.Fatal(p.Gates)
	}
}

func TestPublishRecordGateMutations(t *testing.T) {
	for _, mutation := range []string{"format", "unknown-key", "repository", "signer", "oversize"} {
		t.Run(mutation, func(t *testing.T) {
			d, c, dir := recordCommandFixture(t)
			r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir})
			if r.Status != "ok" {
				t.Fatal(r.Error)
			}
			switch mutation {
			case "format":
				r.Canonical = append([]byte(" "), r.Canonical...)
			case "unknown-key":
				r.Canonical = []byte(strings.Replace(string(r.Canonical), "{", "{\n  \"unknown\": 1,", 1))
			case "repository":
				r.Record.Repository = "forged/repo"
				r.Canonical, _ = EncodeReleaseDocument(r.Record)
			case "signer":
				r.Record.Attestation.SignerWorkflow = "forged/repo/workflow.yml"
				r.Canonical, _ = EncodeReleaseDocument(r.Record)
			case "oversize":
				r.Canonical = append(r.Canonical, []byte(strings.Repeat(" ", MaxReleaseDocumentBytes))...)
			}
			write(t, d, r.Path, r.Canonical)
			end := commitAll(t, d)
			git(t, d, "remote", "add", "origin", "https://github.com/wstein/sourcesteward.git")
			git(t, d, "update-ref", "refs/remotes/origin/main", end)
			git(t, d, "tag", "-a", "v0.2.0", "-m", "release")
			p := Plan(context.Background(), PlanOptions{Path: d, Subject: "v0.2.0"})
			if mutation == "oversize" {
				if p.Status != "refused" || p.Error == nil || p.Error.Code != "release-limit" {
					t.Fatalf("%+v", p)
				}
				return
			}
			code := "record-invalid"
			if mutation == "format" {
				code = "record-not-canonical"
			}
			if p.Status != "ok" || findGate(p, code) != "fail" {
				t.Fatalf("%+v", p)
			}
		})
	}
}
