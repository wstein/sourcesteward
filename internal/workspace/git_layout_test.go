// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures use native Git registration, including real initialized submodules.
func fixtureGitLayout(t *testing.T, layout string, linked, absolute bool) (string, string, string) {
	t.Helper()
	primary := fixtureGit(t)
	common := filepath.Join(primary, ".git")
	switch layout {
	case "submodule":
		super := fixtureGit(t)
		gitFixture(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", primary, "member")
		primary = filepath.Join(super, "member")
		common = filepath.Join(super, ".git", "modules", "member")
	case "separate":
		common = filepath.Join(gitTempDir(t), "metadata")
		if err := os.Rename(filepath.Join(primary, ".git"), common); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(primary, ".git"), []byte("gitdir: "+common+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitFixture(t, primary, "config", "user.name", "Fixture")
	gitFixture(t, primary, "config", "user.email", "fixture@example.invalid")
	if layout != "ordinary" || absolute {
		value := primary
		if !absolute {
			var err error
			value, err = filepath.Rel(common, primary)
			if err != nil {
				t.Fatal(err)
			}
		}
		gitFixture(t, primary, "config", "core.worktree", value)
	}
	source := primary
	if linked {
		source = filepath.Join(gitTempDir(t), "linked")
		gitFixture(t, primary, "worktree", "add", "--detach", source, "HEAD")
	}
	return source, primary, common
}

func TestGitLayoutNativeAdmissionAndDetachedCreation(t *testing.T) {
	for _, layout := range []string{"ordinary", "submodule", "separate"} {
		for _, linked := range []bool{false, true} {
			for _, absolute := range []bool{false, true} {
				name := layout
				if linked {
					name += "/linked"
				}
				if absolute {
					name += "/absolute"
				}
				t.Run(name, func(t *testing.T) {
					source, primary, common := fixtureGitLayout(t, layout, linked, absolute)
					id, err := gitNativeIdentity(context.Background(), source)
					if err != nil {
						t.Fatal(err)
					}
					if id.WorkspaceRoot != source || id.RepositoryDir != common || (linked && id.GitDir == common) {
						t.Fatalf("wrong native identities: %+v", id)
					}
					preserved := map[string][]byte{}
					for _, path := range []string{filepath.Join(primary, "file"), filepath.Join(common, "config"), filepath.Join(common, "index"), filepath.Join(id.GitDir, "index"), filepath.Join(id.GitDir, "HEAD")} {
						b, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						preserved[path] = b
					}
					s, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD")
					if err != nil {
						t.Fatal(err)
					}
					dest := gitTempDir(t)
					p := MutationPlan{TaskID: "native-layout", SourcePath: source, Destination: dest, Expected: s}
					n, err := (gitAdapter{}).Create(context.Background(), p)
					if err != nil || !n.Complete || !n.SourceUnchanged || n.Identity.HeadRef != "" || n.Identity.RepositoryDir != common || n.Identity.CommitID != s.BaseCommit {
						t.Fatalf("creation: %+v %v", n, err)
					}
					for path, want := range preserved {
						got, err := os.ReadFile(path)
						if err != nil || string(got) != string(want) {
							t.Fatalf("preserved state changed: %s", path)
						}
					}
				})
			}
		}
	}
}

// Replace only the inode: keep literal bytes and permission bits identical,
// and move the old file outside all observed repository/workspace metadata.
func replaceLayoutFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(gitTempDir(t), "previous")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, st.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

func TestGitLayoutConfigReplacementStalesCreation(t *testing.T) {
	source, _, common := fixtureGitLayout(t, "submodule", true, false)
	s, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(common, "config")
	replaceLayoutFile(t, path)
	dest := gitTempDir(t)
	if _, err := (gitAdapter{}).Create(context.Background(), MutationPlan{TaskID: "stale", SourcePath: source, Destination: dest, Expected: s}); err == nil {
		t.Fatal("same-byte replacement admitted")
	}
	entries, err := os.ReadDir(dest)
	if err != nil || len(entries) != 0 {
		t.Fatal("stale layout changed destination")
	}
}

func TestGitLayoutHostileMappingRefused(t *testing.T) {
	for _, kind := range []string{"duplicate", "included", "global", "redirect", "missing-backlink", "wrong-backlink", "wrong-commondir", "symlink-config", "hardlink-config", "oversized-config", "symlink-primary-pointer", "hardlink-current-pointer"} {
		t.Run(kind, func(t *testing.T) {
			source, primary, common := fixtureGitLayout(t, "submodule", true, false)
			id, err := gitNativeIdentity(context.Background(), source)
			if err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(common, "config")
			switch kind {
			case "duplicate":
				gitFixture(t, source, "config", "--add", "core.worktree", primary)
			case "included":
				gitFixture(t, source, "config", "--unset", "core.worktree")
				included := filepath.Join(gitTempDir(t), "included")
				os.WriteFile(included, []byte("[core]\nworktree = "+primary+"\n"), 0600)
				gitFixture(t, source, "config", "include.path", included)
			case "global":
				gitFixture(t, source, "config", "--unset", "core.worktree")
				gitFixture(t, source, "config", "--global", "core.worktree", primary)
			case "redirect":
				gitFixture(t, source, "config", "core.worktree", gitTempDir(t))
			case "missing-backlink":
				os.Remove(filepath.Join(id.GitDir, "gitdir"))
			case "wrong-backlink":
				os.WriteFile(filepath.Join(id.GitDir, "gitdir"), []byte(filepath.Join(primary, ".git")+"\n"), 0600)
			case "wrong-commondir":
				os.WriteFile(filepath.Join(id.GitDir, "commondir"), []byte("../../missing\n"), 0600)
			case "symlink-config", "hardlink-config":
				os.Rename(config, config+".original")
				if kind == "symlink-config" {
					os.Symlink(config+".original", config)
				} else {
					os.Link(config+".original", config)
				}
			case "oversized-config":
				f, err := os.OpenFile(config, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				f.WriteString("\n#" + strings.Repeat("x", 65536))
				f.Close()
			case "symlink-primary-pointer":
				os.Rename(filepath.Join(primary, ".git"), filepath.Join(primary, ".git.saved"))
				os.Symlink(filepath.Join(primary, ".git.saved"), filepath.Join(primary, ".git"))
			case "hardlink-current-pointer":
				os.Link(filepath.Join(source, ".git"), filepath.Join(source, ".git.alias"))
			}
			before, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD"); err == nil {
				t.Fatal("unsafe mapping admitted")
			}
			after, err := os.ReadFile(config)
			if err != nil || string(before) != string(after) {
				t.Fatal("refusal changed configuration")
			}
		})
	}
}

func TestGitLayoutVerifyRefusesReplacedConfiguration(t *testing.T) {
	source, _, common := fixtureGitLayout(t, "separate", true, false)
	s, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	p := MutationPlan{TaskID: "verify-layout", SourcePath: source, Destination: gitTempDir(t), Expected: s}
	if _, err := (gitAdapter{}).Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(common, "config")
	replaceLayoutFile(t, config)
	if _, err := (gitAdapter{}).Verify(context.Background(), p); err == nil {
		t.Fatal("completed verification ignored replaced source configuration")
	}
}

func TestGitLayoutNoKeyExternalMappingAndLiteralTraversal(t *testing.T) {
	for _, linked := range []bool{false, true} {
		source, _, _ := fixtureGitLayout(t, "separate", linked, false)
		gitFixture(t, source, "config", "--unset", "core.worktree")
		if _, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD"); err != nil {
			t.Fatal(err)
		}
	}
	parent := gitTempDir(t)
	real := filepath.Join(parent, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(parent, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := gitLayoutDirectory(parent, "alias/.."); err == nil {
		t.Fatal("cleaning hid a literal symlink traversal")
	}
}

func TestGitLayoutMalformedPointerRefused(t *testing.T) {
	for _, name := range []string{"commondir", "gitdir", ".git"} {
		for index, value := range []string{"", "\x00", "../..\nextra\n", strings.Repeat("x", 65537)} {
			t.Run(fmt.Sprintf("%s/%d", name, index), func(t *testing.T) {
				source, _, _ := fixtureGitLayout(t, "submodule", true, false)
				id, err := gitNativeIdentity(context.Background(), source)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(id.GitDir, name)
				if name == ".git" {
					path = filepath.Join(source, name)
				}
				if err := os.WriteFile(path, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD"); err == nil {
					t.Fatal("malformed metadata admitted")
				}
			})
		}
	}
}

func TestGitLayoutBacklinkLiteralTraversalRefused(t *testing.T) {
	for _, relative := range []bool{false, true} {
		for _, kind := range []string{"symlink", "missing", "regular-file"} {
			name := kind + "/absolute"
			if relative {
				name = kind + "/relative"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				source, _, common := fixtureGitLayout(t, "submodule", true, false)
				expected, err := (gitAdapter{}).Observe(ctx, source, "HEAD")
				if err != nil {
					t.Fatal(err)
				}
				id := expected.Identity
				outside := gitTempDir(t)
				sentinel := filepath.Join(outside, "sentinel")
				if err := os.WriteFile(sentinel, []byte("must remain unchanged\n"), 0600); err != nil {
					t.Fatal(err)
				}
				segment := "missing"
				switch kind {
				case "symlink":
					segment = "alias"
					if err := os.Symlink(outside, filepath.Join(source, segment)); err != nil {
						t.Fatal(err)
					}
				case "regular-file":
					segment = "regular"
					if err := os.WriteFile(filepath.Join(source, segment), []byte("file is not a parent directory\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				prefix := source
				if relative {
					prefix, err = filepath.Rel(id.GitDir, source)
					if err != nil {
						t.Fatal(err)
					}
				}
				backlink := filepath.Join(id.GitDir, "gitdir")
				if err := os.WriteFile(backlink, []byte(prefix+"/"+segment+"/../.git\n"), 0600); err != nil {
					t.Fatal(err)
				}
				before := map[string]layoutProtectedFile{}
				for _, path := range []string{sentinel, filepath.Join(source, "file"), filepath.Join(source, ".git"), filepath.Join(common, "config"), filepath.Join(common, "index"), filepath.Join(common, "HEAD"), filepath.Join(id.GitDir, "index"), filepath.Join(id.GitDir, "HEAD"), filepath.Join(id.GitDir, "commondir"), backlink} {
					before[path] = layoutProtectedFileAt(t, path)
				}
				refs := gitFixture(t, source, "for-each-ref", "--format=%(refname) %(objectname)")
				observed, observeErr := (gitAdapter{}).Observe(ctx, source, "HEAD")
				if observeErr == nil {
					t.Error("backlink traversal through an unproven parent was admitted")
					expected = observed
				}
				dest := gitTempDir(t)
				native, createErr := (gitAdapter{}).Create(ctx, MutationPlan{TaskID: "hostile-backlink", SourcePath: source, Destination: dest, Expected: expected})
				if createErr == nil || native.Complete || len(native.Effects) != 0 {
					t.Errorf("unproven backlink reached native mutation: %+v %v", native, createErr)
				}
				entries, err := os.ReadDir(dest)
				if err != nil || len(entries) != 0 {
					t.Error("refused backlink changed the reserved destination")
				}
				requireLayoutProtectedFiles(t, before)
				if got := gitFixture(t, source, "for-each-ref", "--format=%(refname) %(objectname)"); got != refs {
					t.Error("refused backlink changed refs")
				}
			})
		}
	}
}

func TestGitLayoutBacklinkLiteralDirectoryTraversalAdmitted(t *testing.T) {
	for _, relative := range []bool{false, true} {
		name := "absolute"
		if relative {
			name = "relative"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			source, primary, common := fixtureGitLayout(t, "submodule", true, false)
			id, err := gitNativeIdentity(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(source, "directory"), 0700); err != nil {
				t.Fatal(err)
			}
			prefix := source
			if relative {
				prefix, err = filepath.Rel(id.GitDir, source)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(id.GitDir, "gitdir"), []byte(prefix+"/directory/../.git\n"), 0600); err != nil {
				t.Fatal(err)
			}
			before := layoutProtectedFiles(t, source, primary, common)
			expected, err := (gitAdapter{}).Observe(ctx, source, "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			result, err := (gitAdapter{}).Create(ctx, MutationPlan{TaskID: "literal-directory", SourcePath: source, Destination: gitTempDir(t), Expected: expected})
			if err != nil || !result.Complete {
				t.Fatalf("proven literal directory traversal refused: %+v %v", result, err)
			}
			requireLayoutProtectedFiles(t, before)
		})
	}
}
