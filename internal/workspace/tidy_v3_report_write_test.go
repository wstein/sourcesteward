// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// Spec 031 T029: the owner-named report directory and the bounded writer.

func reportNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestReportDirectoryRefusals(t *testing.T) {
	root := gitTempDir(t)
	dir := func() string {
		d, err := os.MkdirTemp(root, "case")
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	cases := map[string]func() (string, []string){
		"relative":      func() (string, []string) { return "reports", nil },
		"not canonical": func() (string, []string) { d := dir(); return d + "/../" + filepath.Base(d), nil },
		"missing":       func() (string, []string) { return filepath.Join(dir(), "absent"), nil },
		"a file": func() (string, []string) {
			p := filepath.Join(dir(), "file")
			os.WriteFile(p, nil, 0o600)
			return p, nil
		},
		"a symlink": func() (string, []string) {
			d := dir()
			l := filepath.Join(d, "link")
			os.Symlink(d, l)
			return l, nil
		},
		"markdown target exists": func() (string, []string) {
			d := dir()
			os.WriteFile(filepath.Join(d, "tidy-report.md"), nil, 0o600)
			return d, nil
		},
		"json target exists": func() (string, []string) {
			d := dir()
			os.WriteFile(filepath.Join(d, "tidy-report.json"), nil, 0o600)
			return d, nil
		},
		"target is a dangling symlink": func() (string, []string) {
			d := dir()
			os.Symlink(filepath.Join(d, "nowhere"), filepath.Join(d, "tidy-report.md"))
			return d, nil
		},
		"inside a named root": func() (string, []string) {
			d := dir()
			sub := filepath.Join(d, "inside")
			os.MkdirAll(sub, 0o700)
			return sub, []string{d}
		},
		"equal to a named root": func() (string, []string) { d := dir(); return d, []string{d} },
		"inside a repository": func() (string, []string) {
			d := dir()
			os.Mkdir(filepath.Join(d, ".git"), 0o700)
			sub := filepath.Join(d, "reports")
			os.MkdirAll(sub, 0o700)
			return sub, nil
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			path, roots := setup()
			err := checkReportDirectory(path, roots)
			var f operationError
			if err == nil || !errors.As(err, &f) || f.Code != "invalid-options" {
				t.Fatalf("%q accepted or wrong error: %v", path, err)
			}
		})
	}
	good := dir()
	other := gitTempDir(t)
	if err := checkReportDirectory(good, []string{other}); err != nil {
		t.Fatalf("a free directory outside every root: %v", err)
	}
}

func TestReportFilesArePrivateExclusiveAndLeaveNoTemporaryFile(t *testing.T) {
	dir := gitTempDir(t)
	if err := writeReportFiles(dir, []byte("# md\n"), []byte("{}\n"), defaultReportFileOps()); err != nil {
		t.Fatal(err)
	}
	if got := reportNames(t, dir); !reflect.DeepEqual(got, []string{"tidy-report.json", "tidy-report.md"}) {
		t.Fatalf("files %v", got)
	}
	for name, want := range map[string]string{"tidy-report.md": "# md\n", "tidy-report.json": "{}\n"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", name, st, err)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, name)); string(b) != want {
			t.Errorf("%s content %q", name, b)
		}
	}
	// A second run finds the targets and refuses before writing anything.
	if err := writeReportFiles(dir, []byte("other"), []byte("other"), defaultReportFileOps()); err == nil {
		t.Error("existing targets were overwritten")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "tidy-report.md")); string(b) != "# md\n" {
		t.Errorf("existing report changed: %q", b)
	}
}

func TestReportWriteFailureLeavesNothingBehind(t *testing.T) {
	boom := errors.New("disk full")
	t.Run("the second rename fails", func(t *testing.T) {
		dir := gitTempDir(t)
		ops := defaultReportFileOps()
		renames := 0
		ops.rename = func(from, to string) error {
			if renames++; renames == 2 {
				return boom
			}
			return os.Rename(from, to)
		}
		if err := writeReportFiles(dir, []byte("m"), []byte("j"), ops); !errors.Is(err, boom) {
			t.Fatalf("error %v", err)
		}
		if got := reportNames(t, dir); len(got) != 0 {
			t.Fatalf("partial files remain: %v", got)
		}
	})
	t.Run("a write fails halfway", func(t *testing.T) {
		dir := gitTempDir(t)
		ops := defaultReportFileOps()
		ops.write = func(f *os.File, b []byte) error { return boom }
		if err := writeReportFiles(dir, []byte("m"), []byte("j"), ops); !errors.Is(err, boom) {
			t.Fatalf("error %v", err)
		}
		if got := reportNames(t, dir); len(got) != 0 {
			t.Fatalf("partial files remain: %v", got)
		}
	})
	t.Run("the temporary name already exists", func(t *testing.T) {
		dir := gitTempDir(t)
		planted := filepath.Join(dir, ".planted.tmp")
		os.WriteFile(planted, []byte("keep"), 0o600)
		ops := defaultReportFileOps()
		ops.tempName = func(string) string { return planted }
		if err := writeReportFiles(dir, []byte("m"), []byte("j"), ops); err == nil {
			t.Fatal("an existing file was reused as a temporary file")
		}
		if b, _ := os.ReadFile(planted); string(b) != "keep" {
			t.Fatalf("existing file overwritten: %q", b)
		}
	})
	t.Run("the temporary name is a symlink", func(t *testing.T) {
		dir := gitTempDir(t)
		victim := filepath.Join(gitTempDir(t), "victim")
		os.WriteFile(victim, []byte("keep"), 0o600)
		ops := defaultReportFileOps()
		ops.tempName = func(string) string {
			p := filepath.Join(dir, ".planted.tmp")
			os.Symlink(victim, p)
			return p
		}
		if err := writeReportFiles(dir, []byte("m"), []byte("j"), ops); err == nil {
			t.Fatal("followed a symlink at the temporary name")
		}
		if b, _ := os.ReadFile(victim); string(b) != "keep" {
			t.Fatalf("symlink target written: %q", b)
		}
	})
}
