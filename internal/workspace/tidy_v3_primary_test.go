// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTidyV3CanonicalPrimaryRegistrations(t *testing.T) {
	for _, layout := range []string{"ordinary", "separate", "submodule"} {
		for _, linked := range []bool{false, true} {
			for _, absolute := range []bool{false, true} {
				name := layout + "/primary"
				if linked {
					name = layout + "/linked"
				}
				if absolute {
					name += "/absolute"
				}
				t.Run(name, func(t *testing.T) {
					fixtureTidyV3Git(t) // Isolate GIT_*, HOME/XDG before layout setup.
					source, primary, _ := fixtureGitLayout(t, layout, linked, absolute)
					before, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD")
					if err != nil {
						t.Fatal(err)
					}
					got, err := observeTidyV3Git(context.Background(), source, "HEAD")
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, row := range got.Native.Registrations {
						if row.Path == primary {
							found = true
						}
					}
					if !found {
						t.Fatalf("canonical primary root missing: primary=%s source=%s rows=%+v", primary, source, got.Native.Registrations)
					}
					p := V3Plan{SourcePath: source, Expected: got.Expected, Mode: "local-refs"}
					if !validV3NativeState(got.Native, p, got.Expected) {
						t.Fatalf("canonical complete native state rejected %+v", got.Native)
					}
					after, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD")
					if err != nil || before != after {
						t.Fatal("read-only canonical projection changed legacy observation", err)
					}
					again, err := observeTidyV3Git(context.Background(), source, "HEAD")
					if err != nil || !reflect.DeepEqual(got, again) {
						t.Fatal("repeated projection changed", err)
					}
				})
			}
		}
	}
}

func TestTidyV3PrimaryNoKeyKeepsLegacyAdmission(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(map[bool]string{false: "primary", true: "linked"}[linked], func(t *testing.T) {
			fixtureTidyV3Git(t)
			source, primary, _ := fixtureGitLayout(t, "separate", linked, false)
			gitFixture(t, source, "config", "--unset", "core.worktree")
			old, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD")
			if err != nil {
				t.Fatal("legacy no-key refused", err)
			}
			got, err := observeTidyV3Git(context.Background(), source, "HEAD")
			if linked {
				if err == nil {
					t.Fatal("invented unknown primary binding", got)
				}
			} else {
				if err != nil || len(got.Native.Registrations) != 1 || got.Native.Registrations[0].Path != primary {
					t.Fatal("lost current-primary pointer authority", got, err)
				}
			}
			after, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD")
			if err != nil || old != after {
				t.Fatal("legacy admission/digest changed", err)
			}
		})
	}
}
func TestTidyV3PrimaryProjectionPreservesCompleteRowAndOrder(t *testing.T) {
	id := NativeIdentity{RepositoryDir: "/z-metadata"}
	rows := []V3Registration{{Path: "/linked", HeadOID: "linked"}, {Path: id.RepositoryDir, HeadOID: "primary", HeadRef: "refs/heads/main", Locked: true, LockReason: "literal", Bare: true, Prunable: true}}
	before := append([]V3Registration{}, rows...)
	got, err := projectTidyV3Primary(rows, id, gitPrimaryBinding{Root: "/a-primary"})
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	want := rows[1]
	want.Path = "/a-primary"
	if got[0] != want || got[1] != rows[0] || !reflect.DeepEqual(rows, before) {
		t.Fatal("projection changed native row or original inventory", got, rows)
	}
	for _, tc := range []struct {
		name    string
		rows    []V3Registration
		binding gitPrimaryBinding
	}{
		{"duplicate", append(append([]V3Registration{}, rows...), rows[1]), gitPrimaryBinding{Root: "/a-primary"}},
		{"collision", rows, gitPrimaryBinding{Root: "/linked"}},
		{"unknown", rows, gitPrimaryBinding{}},
		{"conflict", rows, gitPrimaryBinding{Root: "/a-primary", Conflicting: true}},
		{"noncanonical", rows, gitPrimaryBinding{Root: "/a/../primary"}},
		{"missing-primary", rows[:1], gitPrimaryBinding{Root: "/a-primary"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := projectTidyV3Primary(tc.rows, id, tc.binding); err == nil {
				t.Fatal("ambiguous projection admitted", got)
			}
		})
	}
}
func TestTidyV3PrimaryConflictingAuthorityDoesNotChangeLegacyLayout(t *testing.T) {
	fixtureTidyV3Git(t)
	source, _, common := fixtureGitLayout(t, "separate", false, true)
	id, err := gitNativeIdentity(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	other := gitTempDir(t)
	if err := os.WriteFile(filepath.Join(other, ".git"), []byte("gitdir: "+common+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, source, "config", "core.worktree", other)
	bin, err := gitExecutable()
	if err != nil {
		t.Fatal(err)
	}
	oldConfig, oldDigest, err := gitLayoutSafety(context.Background(), bin, id)
	if err != nil {
		t.Fatal("legacy layout proof unexpectedly changed", err)
	}
	var binding gitPrimaryBinding
	config, digest, err := gitLayoutSafetyWithPrimary(context.Background(), bin, id, &binding)
	if err != nil || config != oldConfig || digest != oldDigest || !binding.Conflicting || binding.Root != other {
		t.Fatal("lost independently established conflict", binding, err)
	}
	if _, err := projectTidyV3Primary([]V3Registration{{Path: common}}, id, binding); err == nil {
		t.Fatal("conflicting primary adopted")
	}
}
func TestTidyV3PrimaryReplacementStalesPlan(t *testing.T) {
	for _, layout := range []string{"separate", "submodule"} {
		for _, kind := range []string{"config", "pointer"} {
			t.Run(layout+"/"+kind, func(t *testing.T) {
				fixtureTidyV3Git(t)
				source, _, common := fixtureGitLayout(t, layout, false, false)
				c := defaultTidyV3RefsCoordinator()
				observe := c.observe
				calls := 0
				c.observe = func(ctx context.Context, path, base string) (tidyV3NativeObservation, error) {
					out, err := observe(ctx, path, base)
					calls++
					if err == nil && calls == 1 {
						target := filepath.Join(common, "config")
						if kind == "pointer" {
							target = filepath.Join(source, ".git")
						}
						replaceLayoutFile(t, target)
					}
					return out, err
				}
				r := c.plan(context.Background(), V3PlanOptions{Path: source, Base: "HEAD", StateDir: privateDir(t), ProtectWorktrees: []string{}})
				if r.Status == "ok" || r.Plan != nil || calls < 2 || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "evidence-changed" {
					t.Fatal("replacement admitted", r, calls)
				}
			})
		}
	}
}
