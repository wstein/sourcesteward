// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	sha1Empty = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	oidA      = "78981922613b2afb6025042ff6bd878ac1994e85"
)

func debugBlock(ctime, mtime, dev, ino, size, flags string) string {
	return "  ctime: " + ctime + "\n  mtime: " + mtime + "\n  dev: " + dev + "\tino: " + ino + "\n  uid: 1000\tgid: 1000\n  size: " + size + "\tflags: " + flags + "\n"
}

// feedAll runs the parser over the records of one stream, split the way the runner splits it.
func feedAll(stream string) ([]pinsIndexEntry, error) {
	var got []pinsIndexEntry
	p := &pinsIndexParser{}
	parts := strings.Split(stream, "\x00")
	for i, rec := range parts {
		last := i == len(parts)-1
		if last && rec == "" {
			break
		}
		if err := p.feed([]byte(rec), last, func(e pinsIndexEntry) error { got = append(got, e); return nil }); err != nil {
			return got, err
		}
	}
	return got, p.finish()
}

func TestPinsIndexParserReadsHeaderAndBlock(t *testing.T) {
	stream := "100644 " + oidA + " 0\ta\x00" + debugBlock("1792170736:717557760", "1792170737:5", "53", "10161055", "2", "0") +
		"100755 " + oidA + " 2\tn\nl\x00" + debugBlock("-5:0", "7:8", "1", "2", "4294967295", "40004000")
	got, err := feedAll(stream)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	e := got[0]
	if e.Mode != "100644" || e.OID != oidA || e.Stage != 0 || e.Path != "a" || e.CtimeSec != 1792170736 || e.CtimeNsec != 717557760 || e.MtimeSec != 1792170737 || e.MtimeNsec != 5 ||
		e.Dev != 53 || e.Ino != 10161055 || e.UID != 1000 || e.GID != 1000 || e.Size != 2 || e.Flags != 0 {
		t.Fatalf("%+v", e)
	}
	e = got[1]
	if e.Mode != "100755" || e.Stage != 2 || e.Path != "n\nl" || e.CtimeSec != -5 || e.Size != 4294967295 || e.Flags != 0x40004000 {
		t.Fatalf("%+v", e)
	}
}

func TestPinsIndexParserIsFailSafe(t *testing.T) {
	head := "100644 " + oidA + " 0\ta\x00"
	good := debugBlock("1:2", "3:4", "5", "6", "7", "0")
	for name, stream := range map[string]string{
		"header without tab":         "100644 " + oidA + " 0 a\x00" + good,
		"header with two fields":     "100644 " + oidA + "\ta\x00" + good,
		"short object name":          "100644 abc 0\ta\x00" + good,
		"unknown stage":              "100644 " + oidA + " 9\ta\x00" + good,
		"non numeric stage":          "100644 " + oidA + " x\ta\x00" + good,
		"empty path":                 "100644 " + oidA + " 0\t\x00" + good,
		"missing block":              head,
		"four line block":            head + strings.Join(strings.SplitAfter(good, "\n")[:4], ""),
		"six line block":             head + good + "  extra: 1\n",
		"renamed key":                head + strings.Replace(good, "ctime", "atime", 1),
		"no colon in time":           head + strings.Replace(good, "1:2", "12", 1),
		"non numeric time":           head + strings.Replace(good, "1:2", "x:2", 1),
		"extra space in time":        head + strings.Replace(good, "ctime: 1:2", "ctime:  1:2", 1),
		"space instead of tab":       head + strings.Replace(good, "ino: 6", " ino: 6", 1),
		"tab instead of space":       head + strings.Replace(good, "  ctime", "\tctime", 1),
		"negative size":              head + strings.Replace(good, "size: 7", "size: -7", 1),
		"size beyond 32 bits":        head + strings.Replace(good, "size: 7", "size: 4294967296", 1),
		"non hex flags":              head + strings.Replace(good, "flags: 0", "flags: zz", 1),
		"uppercase hex flags":        head + strings.Replace(good, "flags: 0", "flags: 4A", 1),
		"missing final newline":      head + strings.TrimSuffix(good, "\n"),
		"header followed by header":  head + head,
		"block with trailing header": head + good + "100644 " + oidA + " 0\tb",
		"carriage return":            head + strings.Replace(good, "\n", "\r\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := feedAll(stream)
			if err == nil {
				t.Fatalf("a malformed stream parsed: %+v", got)
			}
			for _, e := range got {
				if e.Path != "a" {
					t.Fatalf("a guessed entry was emitted: %+v", e)
				}
			}
		})
	}
}

func TestPinsIndexParserAcceptsAnEmptyIndex(t *testing.T) {
	if got, err := feedAll(""); err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

// The parser depends on a format Git documents as free to change. This test reads a real index on the Git
// in use and compares every field with what the filesystem reports, so a format change on any Git lane
// fails here instead of silently mis-classifying files.
func TestPinsIndexDebugFormatIsPinned(t *testing.T) {
	pinsIsolateHome(t)
	dir := pinsRealDir(t)
	git(t, dir, "init", "-q", "-b", "main")
	files := map[string]string{"plain.txt": "one\n", "empty": "", "dir/nested file.txt": "two\n", "tab\tname": "x", "new\nline": "yy", "utf8-é": "z"}
	for name, content := range files {
		writeFile(t, filepath.Join(dir, name), content)
	}
	mustOS(t, os.Chmod(filepath.Join(dir, "plain.txt"), 0o755))
	mustOS(t, os.Symlink("plain.txt", filepath.Join(dir, "link")))
	git(t, dir, "add", "-A")
	run := newPinsRunner(context.Background())
	repo := pinsGitRepo{GitDir: filepath.Join(dir, ".git"), WorkTree: dir}
	var got []pinsIndexEntry
	p := &pinsIndexParser{}
	_, err := run.run(pinsCall{Repo: repo, Args: []string{"ls-files", "--stage", "--debug", "-z"}, Each: func(rec []byte, last bool) error {
		return p.feed(rec, last, func(e pinsIndexEntry) error { got = append(got, e); return nil })
	}})
	if err != nil || p.finish() != nil {
		t.Fatalf("%v %v", err, p.finish())
	}
	if len(got) != len(files)+1 {
		t.Fatalf("%d entries for %d files: %+v", len(got), len(files)+1, got)
	}
	for _, e := range got {
		info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(e.Path)))
		if err != nil {
			t.Fatalf("%q: %v", e.Path, err)
		}
		st, ok := pinsStatOf(info)
		if !ok {
			t.Skip("no stat data on this platform")
		}
		wantMode := map[bool]string{true: "100755", false: "100644"}[info.Mode().Perm()&0o100 != 0]
		if info.Mode()&fs.ModeSymlink != 0 {
			wantMode = "120000"
		}
		if e.Mode != wantMode || e.Stage != 0 || e.Flags != 0 || e.Size != uint32(info.Size()) ||
			uint32(e.MtimeSec) != uint32(st.MtimeSec) || e.MtimeNsec != st.MtimeNsec || uint32(e.CtimeSec) != uint32(st.CtimeSec) || e.CtimeNsec != st.CtimeNsec ||
			e.Ino != uint32(st.Ino) || e.Dev != uint32(st.Dev) || e.UID != uint32(st.UID) || e.GID != uint32(st.GID) {
			t.Errorf("%q: index says %+v, filesystem says %+v mode %s", e.Path, e, st, wantMode)
		}
		if want := gitIn(t, dir, "ls-files", "-s", "--", e.Path); !strings.HasPrefix(want, e.Mode+" "+e.OID+" 0\t") {
			t.Errorf("%q: header %q", e.Path, want)
		}
	}
}
