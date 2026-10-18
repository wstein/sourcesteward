// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	encodingbinary "encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The locator learns dependencies from CAPTURED bytes only. Checksums, cache
// scalars, flags and opaque extension bodies remain untouched for native Git.
func TestBuildViewIndexDependencyFraming(t *testing.T) {
	for _, hashBytes := range []int{20, 32} {
		for _, version := range []uint32{2, 3, 4} {
			var data bytes.Buffer
			data.WriteString("DIRC")
			encodingbinary.Write(&data, encodingbinary.BigEndian, version)
			encodingbinary.Write(&data, encodingbinary.BigEndian, uint32(1))
			entry := make([]byte, 42+hashBytes)
			if version == 3 {
				encodingbinary.BigEndian.PutUint16(entry[40+hashBytes:], 0x4000)
				entry = append(entry, 0, 0)
			}
			if version == 4 {
				entry = append(entry, 0)
			}
			entry = append(entry, []byte("literal-link-in-name")...)
			entry = append(entry, 0)
			if version != 4 {
				for len(entry)%8 != 0 {
					entry = append(entry, 0)
				}
			}
			data.Write(entry)
			ext := func(name string, body []byte) {
				data.WriteString(name)
				encodingbinary.Write(&data, encodingbinary.BigEndian, uint32(len(body)))
				data.Write(body)
			}
			ext("ZZZZ", []byte("link\x00\x00\x00\x20opaque payload"))
			oid := bytes.Repeat([]byte{0x2a}, hashBytes)
			ext("link", append(oid, []byte("opaque bitmaps")...))
			data.Write(make([]byte, hashBytes))
			original := append([]byte{}, data.Bytes()...)
			f, err := os.CreateTemp(t.TempDir(), "captured-index")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err = f.Write(original); err != nil {
				t.Fatal(err)
			}
			name, err := buildViewIndexDependency(f, hashBytes)
			if err != nil {
				t.Fatal(err)
			}
			if name != "sharedindex."+strings.Repeat("2a", hashBytes) {
				t.Fatalf("wrong dependency %q", name)
			}
			after, err := os.ReadFile(f.Name())
			if err != nil || !bytes.Equal(after, original) {
				t.Fatal("locator changed captured index")
			}
			// Null link means no base; skipHash footer remains zero and is not repaired.
			start := 12 + len(entry) + 8 + len("link\x00\x00\x00\x20opaque payload") + 8
			if _, err := f.WriteAt(make([]byte, hashBytes), int64(start)); err != nil {
				t.Fatal(err)
			}
			name, err = buildViewIndexDependency(f, hashBytes)
			if err != nil || name != "" {
				t.Fatalf("null link invented a base: %q %v", name, err)
			}
		}
	}
}
func TestBuildViewIndexDependencyRejectsUnsafeFraming(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("DIRC"), append([]byte("DIRC\x00\x00\x00\x02\xff\xff\xff\xff"), make([]byte, 20)...), append(append([]byte("DIRC\x00\x00\x00\x02\x00\x00\x00\x00link\xff\xff\xff\xff"), 1), make([]byte, 20)...)} {
		f, err := os.CreateTemp(t.TempDir(), "captured-index")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err = f.Write(raw); err != nil {
			t.Fatal(err)
		}
		if _, err := buildViewIndexDependency(f, 20); err == nil {
			t.Fatal("unsafe dependency framing accepted")
		}
	}
}

func TestBuildViewIndexDependencyNativeCopies(t *testing.T) {
	for _, binary := range []string{"/usr/bin/git", "/usr/local/bin/git"} {
		if _, err := os.Stat(binary); err != nil {
			t.Skip("required existing native fixture Git unavailable")
		}
		for _, format := range []string{"sha1", "sha256"} {
			for _, version := range []string{"2", "3", "4"} {
				for _, skipHash := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/v%s/skipHash=%t", binary, format, version, skipHash), func(t *testing.T) {
						dir := t.TempDir()
						meta := filepath.Join(dir, ".git")
						env := append(buildViewEnvironment(os.Environ(), binary, t.TempDir()), "GIT_AUTHOR_NAME=Source test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Source test", "GIT_COMMITTER_EMAIL=test@example.invalid")
						prefix := []string{"--no-pager", "--no-replace-objects", "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "hook.post-index-change.enabled=false"}
						setup := func(args ...string) string {
							t.Helper()
							cmd := exec.Command(binary, append(prefix, args...)...)
							cmd.Dir = dir
							cmd.Env = env
							raw, e := cmd.CombinedOutput()
							if e != nil {
								t.Fatal("native fixture setup failed", e)
							}
							return strings.TrimSpace(string(raw))
						}
						setup("init", "-q", "--template=", "--object-format="+format, "-b", "main")
						for _, name := range []string{"tracked", "ordinary"} {
							if err := os.WriteFile(filepath.Join(dir, name), []byte("original\n"), 0600); err != nil {
								t.Fatal(err)
							}
						}
						setup("add", "tracked", "ordinary")
						setup("commit", "-qm", "source fixture")
						if version == "3" {
							setup("update-index", "--skip-worktree", "tracked")
						}
						setup("config", "index.skipHash", fmt.Sprint(skipHash))
						setup("update-index", "--index-version="+version)
						setup("update-index", "--split-index")
						// Source-native observation is explicit FIXTURE SETUP only. Production
						// never uses --shared-index-path or any source native index reader.
						expected := setup("rev-parse", "--shared-index-path")
						if expected != "" {
							expected = filepath.Base(expected)
						}
						// Native rev-parse spells a null-link pathname too. read_index_from
						// does not load that base: prove native enumeration succeeds without it.
						if expected == "sharedindex."+strings.Repeat("0", 40) || expected == "sharedindex."+strings.Repeat("0", 64) {
							expected = ""
						}
						expectedListing := []byte(setup("ls-files", "--stage", "-z", "--full-name", "--"))
						if !skipHash && (!bytes.Contains(expectedListing, []byte("\ttracked\x00")) || !bytes.Contains(expectedListing, []byte("\tordinary\x00"))) {
							t.Fatal("ordinary native fixture index missing entries")
						}
						private := t.TempDir()
						for _, name := range []string{"HEAD", "config"} {
							raw, e := os.ReadFile(filepath.Join(meta, name))
							if e != nil {
								t.Fatal(e)
							}
							if e = os.WriteFile(filepath.Join(private, name), raw, 0600); e != nil {
								t.Fatal(e)
							}
						}
						for _, name := range []string{"objects", "refs", "info"} {
							if err := os.Symlink(filepath.Join(meta, name), filepath.Join(private, name)); err != nil {
								t.Fatal(err)
							}
						}
						var bindings []*buildViewBinding
						copyOne := func(name string) *os.File {
							t.Helper()
							b, e := captureBuildViewPath(context.Background(), filepath.Join(meta, name), nil)
							if e != nil {
								t.Fatal(e)
							}
							bindings = append(bindings, b)
							t.Cleanup(func() { b.Close() })
							f, e := os.OpenFile(filepath.Join(private, name), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
							if e != nil {
								t.Fatal(e)
							}
							t.Cleanup(func() { f.Close() })
							if e = b.copyTo(f); e != nil {
								t.Fatal(e)
							}
							if e = os.Chtimes(f.Name(), time.Unix(0, 0), b.info.ModTime()); e != nil {
								t.Fatal(e)
							}
							return f
						}
						index := copyOne("index")
						var nativeHeader [12]byte
						if _, e := index.ReadAt(nativeHeader[:], 0); e != nil {
							t.Fatal(e)
						}
						actualVersion := strconv.FormatUint(uint64(encodingbinary.BigEndian.Uint32(nativeHeader[4:8])), 10)
						if actualVersion != version {
							t.Fatalf("native fixture did not create claimed version: actual=%s requested=%s", actualVersion, version)
						}
						hashBytes := 20
						if format == "sha256" {
							hashBytes = 32
						}
						dependency, err := buildViewIndexDependency(index, hashBytes)
						if err != nil {
							t.Fatal(err)
						}
						if dependency != expected {
							t.Fatalf("locator/native dependency mismatch: got %q expected %q", dependency, expected)
						}
						if dependency != "" {
							copyOne(dependency)
						}
						privateEnv := append(env, "GIT_DIR="+private, "GIT_COMMON_DIR="+private, "GIT_INDEX_FILE="+index.Name(), "GIT_WORK_TREE="+dir, "GIT_OBJECT_DIRECTORY="+filepath.Join(meta, "objects"))
						output, e := os.CreateTemp(t.TempDir(), "semantic-listing")
						if e != nil {
							t.Fatal(e)
						}
						defer output.Close()
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						status, _, e := runBuildViewNative(ctx, buildViewCommand{Executable: binary, Dir: dir, Env: privateEnv, Args: append(prefix, "ls-files", "--stage", "-z", "--full-name", "--")}, output)
						if e != nil || status != 0 {
							t.Fatalf("native private index validation failed: status=%d err=%v", status, e)
						}
						raw, e := os.ReadFile(output.Name())
						if e != nil || !bytes.Equal(raw, expectedListing) {
							t.Fatal("native private enumeration differs from original native semantics")
						}
						for _, b := range bindings {
							if e := b.validate(); e != nil {
								t.Fatal("private native observation changed source index", e)
							}
						}
					})
				}
			}
		}
	}
}

func TestBuildViewIndexDependencySkipsLargeOpaquePayload(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "captured-index")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var header bytes.Buffer
	header.WriteString("DIRC")
	encodingbinary.Write(&header, encodingbinary.BigEndian, uint32(2))
	encodingbinary.Write(&header, encodingbinary.BigEndian, uint32(0))
	header.WriteString("ZZZZ")
	const payload = uint32((2 << 30) + 1)
	encodingbinary.Write(&header, encodingbinary.BigEndian, payload)
	if _, err = f.Write(header.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(int64(header.Len()) + int64(payload) + 20); err != nil {
		t.Fatal(err)
	}
	name, err := buildViewIndexDependency(f, 20)
	if err != nil || name != "" {
		t.Fatalf("opaque sparse payload was parsed/capped: %q %v", name, err)
	}
}
