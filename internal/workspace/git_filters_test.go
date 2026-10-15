// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGitWorkspaceAdmitsUnusedFilterDefinitions(t *testing.T) {
	d := fixtureGit(t)
	sentinel := filepath.Join(gitTempDir(t), "helper-called")
	gitFixture(t, d, "config", "--global", "filter.lfs.smudge", "touch '"+sentinel+"'")
	gitFixture(t, d, "config", "--global", "filter.lfs.process", "touch '"+sentinel+"'")
	gitFixture(t, d, "config", "--global", "filter.lfs.required", "true")
	s, err := (gitAdapter{}).Observe(context.Background(), d, "HEAD")
	if err != nil {
		t.Fatalf("unused system/global filter definitions refused: %v", err)
	}
	dest := gitTempDir(t)
	n, err := (gitAdapter{}).Create(context.Background(), MutationPlan{TaskID: "unused-filter", SourcePath: d, Destination: dest, Expected: s})
	if err != nil || !n.Complete {
		t.Fatalf("unused filter checkout: %+v %v", n, err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("unused filter helper executed")
	}
}

func TestGitWorkspaceRefusesActiveCheckoutFilters(t *testing.T) {
	for _, scope := range []string{"stored", "info", "global-default"} {
		t.Run(scope, func(t *testing.T) {
			d := fixtureGit(t)
			var attrs string
			switch scope {
			case "stored":
				attrs = filepath.Join(d, ".gitattributes")
			case "info":
				attrs = filepath.Join(d, ".git", "info", "attributes")
			case "global-default":
				attrs = filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git", "attributes")
				if err := os.MkdirAll(filepath.Dir(attrs), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(attrs, []byte("file filter=hostile\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if scope == "stored" {
				gitFixture(t, d, "add", ".gitattributes")
				gitFixture(t, d, "commit", "-qm", "active filter attribute")
			}
			sentinel := filepath.Join(gitTempDir(t), "helper-called")
			gitFixture(t, d, "config", "filter.hostile.smudge", "touch '"+sentinel+"'")
			if _, err := (gitAdapter{}).Observe(context.Background(), d, "HEAD"); err == nil {
				t.Fatal("active checkout filter admitted")
			}
			if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
				t.Fatal("filter helper executed during attribute inspection")
			}
		})
	}
}

func TestGitWorkspaceUsesBaseAttributesNotSourceDisk(t *testing.T) {
	d := fixtureGit(t)
	sentinel := filepath.Join(gitTempDir(t), "helper-called")
	gitFixture(t, d, "config", "filter.hostile.smudge", "touch '"+sentinel+"'")
	if err := os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("file filter=hostile\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := (gitAdapter{}).Observe(context.Background(), d, "HEAD")
	if err != nil {
		t.Fatalf("untracked source attributes applied to stored base: %v", err)
	}
	dest := gitTempDir(t)
	if _, err := (gitAdapter{}).Create(context.Background(), MutationPlan{TaskID: "base-attributes", SourcePath: d, Destination: dest, Expected: s}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("source-only filter executed")
	}
	if b, err := os.ReadFile(filepath.Join(d, ".gitattributes")); err != nil || string(b) != "file filter=hostile\n" {
		t.Fatal("source untracked attributes changed")
	}
}
