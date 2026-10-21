// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Spec 031 T013: the patch-id class on real repositories, through the
// production planning session and engine.

func wantVerdict(t *testing.T, v integrationVerdict, disposition, class string, reasons ...string) {
	t.Helper()
	if v.Disposition != disposition || v.Class != class || !reflect.DeepEqual(append([]string{}, v.Reasons...), append([]string{}, reasons...)) {
		t.Fatalf("verdict %+v, want %s %s %v", v, disposition, class, reasons)
	}
}

// rebaseFix builds a base main with an unrelated commit and a topic branch of
// three file-adding commits; pickOrder selects which topic commits main gets.
func rebaseFix(t *testing.T, pickOrder ...int) (*proofFix, []string) {
	f := newProofFix(t)
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	topic := []string{
		f.commit("add a", map[string]string{"a.txt": "alpha\n"}),
		f.commit("add b", map[string]string{"b.txt": "beta\n"}),
		f.commit("add c", map[string]string{"c.txt": "gamma\n"}),
	}
	f.checkout(f.main)
	f.commit("unrelated", map[string]string{"u.txt": "unrelated\n"})
	for _, i := range pickOrder {
		f.pick(topic[i])
	}
	return f, topic
}

func TestPatchIDRebaseMergeMatchesAllThreeCommits(t *testing.T) {
	f, _ := rebaseFix(t, 0, 1, 2)
	v := f.verdict(f.main, "topic", patchPolicy(false))
	wantVerdict(t, v, "proven", "patch-id")
	if v.Matched != 3 || v.Unmatched != 0 || v.Empty != 0 || v.SkippedMerges != 0 || len(v.Flags) != 0 {
		t.Fatalf("counts %+v", v)
	}
}

func TestPatchIDReorderedBaseSideStillMatches(t *testing.T) {
	f, _ := rebaseFix(t, 2, 0, 1)
	v := f.verdict(f.main, "topic", patchPolicy(false))
	wantVerdict(t, v, "proven", "patch-id")
	if v.Matched != 3 {
		t.Fatalf("matched %d", v.Matched)
	}
}

func TestPatchIDPartialMatch(t *testing.T) {
	f, _ := rebaseFix(t, 0)
	v := f.verdict(f.main, "topic", patchPolicy(false))
	wantVerdict(t, v, "needs-review", "", "partial-match")
	if v.Matched != 1 || v.Unmatched != 2 {
		t.Fatalf("counts %+v", v)
	}
}

func TestPatchIDNoCommitMatchesIsNotAReason(t *testing.T) {
	f, _ := rebaseFix(t)
	v := f.verdict(f.main, "topic", patchPolicy(false))
	if v.Disposition != "" || v.Matched != 0 || v.Unmatched != 3 || len(v.Reasons) != 0 {
		t.Fatalf("verdict %+v", v)
	}
}

// Multiset counts: add X, revert, add X against a base with add and revert.
func TestPatchIDMultisetCountsEachPatchOnce(t *testing.T) {
	f := newProofFix(t)
	f.commit("seed", map[string]string{"x.txt": "base\n"})
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	add := f.commit("add X", map[string]string{"x.txt": "base\nX\n"})
	revert := f.commit("revert X", map[string]string{"x.txt": "base\n"})
	f.commit("add X again", map[string]string{"x.txt": "base\nX\n"})
	f.checkout(f.main)
	f.commit("unrelated", map[string]string{"u.txt": "u\n"})
	f.pick(add)
	f.pick(revert)
	v := f.verdict(f.main, "topic", patchPolicy(false))
	wantVerdict(t, v, "needs-review", "", "multiset-mismatch")
	if v.Matched != 2 || v.Unmatched != 1 {
		t.Fatalf("counts %+v", v)
	}
}

func TestPatchIDMergeCommitInRangeNeverProves(t *testing.T) {
	f := newProofFix(t)
	mb := f.rev("HEAD")
	f.branch("side", mb)
	s1 := f.commit("side", map[string]string{"s.txt": "side\n"})
	f.branch("topic", mb)
	c1 := f.commit("topic", map[string]string{"t.txt": "topic\n"})
	f.git("merge", "--no-ff", "-qm", "merge side", "side")
	f.checkout(f.main)
	f.commit("unrelated", map[string]string{"u.txt": "u\n"})
	f.pick(c1)
	f.pick(s1)
	v := f.verdict(f.main, "topic", patchPolicy(false))
	wantVerdict(t, v, "needs-review", "", "merge-commits-in-range")
	if v.SkippedMerges != 1 || v.Matched != 2 || v.Unmatched != 0 {
		t.Fatalf("counts %+v", v)
	}
}

func TestPatchIDEmptyCommitIsCountedAndNeverMatches(t *testing.T) {
	f := newProofFix(t)
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	c1 := f.commit("real", map[string]string{"r.txt": "real\n"})
	f.git("commit", "-q", "--allow-empty", "-m", "empty")
	f.checkout(f.main)
	f.commit("unrelated", map[string]string{"u.txt": "u\n"})
	f.pick(c1)
	v := f.verdict(f.main, "topic", patchPolicy(false))
	wantVerdict(t, v, "proven", "patch-id")
	if v.Empty != 1 || v.Matched != 1 || v.Unmatched != 0 {
		t.Fatalf("counts %+v", v)
	}
	// Only empty commits: there is no non-empty branch commit, nothing is proven.
	f.checkout(f.main)
	mb2 := f.rev(f.main)
	f.branch("only-empty", mb2)
	f.git("commit", "-q", "--allow-empty", "-m", "empty only")
	g := f.verdict(f.main, "only-empty", patchPolicy(false))
	if g.Disposition != "" || g.Empty != 1 || g.Matched != 0 {
		t.Fatalf("only-empty verdict %+v", g)
	}
}

// Whitespace-only differences are patch-id equivalent and flagged; they delete
// only with the policy flag.
func TestPatchIDWhitespaceDifferencesAreFlaggedAndNeedAcceptance(t *testing.T) {
	variants := map[string][2]string{
		"indentation":     {"  echo hi\n", "echo hi\n"},
		"trailing":        {"echo hi \n", "echo hi\n"},
		"crlf":            {"echo hi\r\n", "echo hi\n"},
		"joined tokens":   {"rm -rf \"$D\" /\n", "rm -rf \"$D\"/\n"},
		"tabs and spaces": {"a\tb\n", "a b\n"},
	}
	for name, v := range variants {
		t.Run(name, func(t *testing.T) {
			f := newProofFix(t)
			mb := f.rev("HEAD")
			f.branch("topic", mb)
			f.commit("script", map[string]string{"s.sh": v[0]})
			f.checkout(f.main)
			f.commit("script landed", map[string]string{"s.sh": v[1]})
			got := f.verdict(f.main, "topic", patchPolicy(false))
			wantVerdict(t, got, "needs-review", "", "whitespace-differs")
			if !reflect.DeepEqual(got.Flags, []string{"whitespace-differs"}) || got.Matched != 1 {
				t.Fatalf("verdict %+v", got)
			}
			accepted := f.verdict(f.main, "topic", patchPolicy(true))
			wantVerdict(t, accepted, "proven", "patch-id")
			if !reflect.DeepEqual(accepted.Flags, []string{"accepted-whitespace-differences"}) {
				t.Fatalf("accepted verdict %+v", accepted)
			}
		})
	}
	f := newProofFix(t)
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	f.commit("script", map[string]string{"s.sh": "echo hi\n"})
	f.checkout(f.main)
	f.commit("script landed", map[string]string{"s.sh": "echo hi\n"})
	same := f.verdict(f.main, "topic", patchPolicy(false))
	wantVerdict(t, same, "proven", "patch-id")
	if len(same.Flags) != 0 {
		t.Fatalf("identical change flagged: %+v", same)
	}
}

// A binary change never matches a different binary change on the same path and
// never raises whitespace-differs; the same change matches; a commit that also
// edits whitespace in a text file is flagged (there is no binary exemption).
func TestPatchIDBinaryChangesMatchByContentOnly(t *testing.T) {
	bin := func(tail string) string { return "\x00\x01\x02" + tail }
	t.Run("different binary change", func(t *testing.T) {
		f := newProofFix(t)
		mb := f.rev("HEAD")
		f.branch("topic", mb)
		f.commit("bin", map[string]string{"b.bin": bin("A")})
		f.checkout(f.main)
		f.commit("other bin", map[string]string{"b.bin": bin("B")})
		v := f.verdict(f.main, "topic", patchPolicy(false))
		if v.Disposition != "" || v.Matched != 0 || v.Unmatched != 1 || len(v.Flags) != 0 || !reflect.DeepEqual(v.BinaryPaths, []string{"b.bin"}) {
			t.Fatalf("verdict %+v", v)
		}
	})
	t.Run("same binary change", func(t *testing.T) {
		f := newProofFix(t)
		mb := f.rev("HEAD")
		f.branch("topic", mb)
		c := f.commit("bin", map[string]string{"b.bin": bin("A")})
		f.checkout(f.main)
		f.commit("unrelated", map[string]string{"u.txt": "u\n"})
		f.pick(c)
		v := f.verdict(f.main, "topic", patchPolicy(false))
		wantVerdict(t, v, "proven", "patch-id")
		if len(v.Flags) != 0 || !reflect.DeepEqual(v.BinaryPaths, []string{"b.bin"}) {
			t.Fatalf("verdict %+v", v)
		}
	})
	t.Run("binary plus whitespace edit in a text file", func(t *testing.T) {
		f := newProofFix(t)
		mb := f.rev("HEAD")
		f.branch("topic", mb)
		f.commit("mixed", map[string]string{"b.bin": bin("A"), "t.txt": "a b\n"})
		f.checkout(f.main)
		f.commit("mixed landed", map[string]string{"b.bin": bin("A"), "t.txt": "a  b\n"})
		v := f.verdict(f.main, "topic", patchPolicy(false))
		wantVerdict(t, v, "needs-review", "", "whitespace-differs")
		if !reflect.DeepEqual(v.Flags, []string{"whitespace-differs"}) {
			t.Fatalf("verdict %+v", v)
		}
	})
}

func TestPatchIDDiffAttributeAndAttributeSourceReasons(t *testing.T) {
	for name, attr := range map[string]string{"driver": "*.dat diff=d\n", "set": "*.dat diff\n", "unset": "*.dat -diff\n"} {
		t.Run(name, func(t *testing.T) {
			f := newProofFix(t)
			mb := f.rev("HEAD")
			f.branch("topic", mb)
			c := f.commit("data", map[string]string{".gitattributes": attr, "x.dat": "1\n"})
			f.checkout(f.main)
			f.commit("unrelated", map[string]string{"u.txt": "u\n"})
			f.pick(c)
			v := f.verdict(f.main, "topic", patchPolicy(false))
			wantVerdict(t, v, "needs-review", "", "diff-attribute")
		})
	}
	t.Run("control without attributes", func(t *testing.T) {
		f := newProofFix(t)
		mb := f.rev("HEAD")
		f.branch("topic", mb)
		c := f.commit("data", map[string]string{"x.dat": "1\n"})
		f.checkout(f.main)
		f.commit("unrelated", map[string]string{"u.txt": "u\n"})
		f.pick(c)
		wantVerdict(t, f.verdict(f.main, "topic", patchPolicy(false)), "proven", "patch-id")
	})
	t.Run("worktree attributes are ignored, the tip tree decides", func(t *testing.T) {
		f := newProofFix(t)
		mb := f.rev("HEAD")
		f.branch("topic", mb)
		c := f.commit("data", map[string]string{"x.dat": "1\n"})
		f.checkout(f.main)
		f.commit("unrelated", map[string]string{"u.txt": "u\n"})
		f.pick(c)
		f.write(".gitattributes", "*.dat diff=d\n")
		wantVerdict(t, f.verdict(f.main, "topic", patchPolicy(false)), "proven", "patch-id")
	})
	t.Run("info/attributes content", func(t *testing.T) {
		f := newProofFix(t)
		mb := f.rev("HEAD")
		f.branch("topic", mb)
		c := f.commit("data", map[string]string{"x.dat": "1\n"})
		f.checkout(f.main)
		f.commit("unrelated", map[string]string{"u.txt": "u\n"})
		f.pick(c)
		f.write(".git/info/attributes", "# only a comment\n\n")
		wantVerdict(t, f.verdict(f.main, "topic", patchPolicy(false)), "proven", "patch-id")
		f.write(".git/info/attributes", "*.other -diff\n")
		wantVerdict(t, f.verdict(f.main, "topic", patchPolicy(false)), "needs-review", "", "attribute-source")
	})
}

// Mode-only changes and symlink retargets are distinguished by patch-id and are
// never silently treated as integrated.
func TestPatchIDDistinguishesModeAndSymlinkChanges(t *testing.T) {
	seed := func(f *proofFix) string {
		f.write("run.sh", "#!/bin/sh\n")
		f.write("other.sh", "#!/bin/sh\n")
		if err := os.Chmod(f.dir+"/other.sh", 0o755); err != nil {
			t.Fatal(err)
		}
		f.git("add", "-A")
		f.git("commit", "-qm", "scripts")
		if err := osSymlink("a", f.dir+"/link"); err != nil {
			t.Fatal(err)
		}
		f.git("add", "-A")
		f.git("commit", "-qm", "link")
		return f.rev("HEAD")
	}
	flip := func(f *proofFix, path, mode string) string {
		perm := os.FileMode(0o644)
		if mode == "+x" {
			perm = 0o755
		}
		if err := os.Chmod(f.dir+"/"+path, perm); err != nil {
			t.Fatal(err)
		}
		f.git("add", "-A")
		f.git("commit", "-qm", "chmod "+mode+" "+path)
		return f.rev("HEAD")
	}
	t.Run("same executable flip matches", func(t *testing.T) {
		f := newProofFix(t)
		mb := seed(f)
		f.branch("topic", mb)
		c := flip(f, "run.sh", "+x")
		f.checkout(f.main)
		f.commit("unrelated", map[string]string{"u.txt": "u\n"})
		f.pick(c)
		wantVerdict(t, f.verdictBare(f.main, "topic", patchPolicy(false)), "proven", "patch-id")
	})
	t.Run("missing flip is not integrated", func(t *testing.T) {
		f := newProofFix(t)
		mb := seed(f)
		f.branch("topic", mb)
		flip(f, "run.sh", "+x")
		f.checkout(f.main)
		f.commit("advance", map[string]string{"u.txt": "u\n"})
		v := f.verdictBare(f.main, "topic", patchPolicy(false))
		if v.Disposition != "" || v.Matched != 0 || v.Unmatched != 1 {
			t.Fatalf("verdict %+v", v)
		}
	})
	t.Run("opposite flip direction and other file differ", func(t *testing.T) {
		f := newProofFix(t)
		mb := seed(f)
		f.branch("topic", mb)
		flip(f, "run.sh", "+x")
		f.checkout(f.main)
		flip(f, "other.sh", "-x")
		v := f.verdictBare(f.main, "topic", patchPolicy(false))
		if v.Disposition != "" || v.Matched != 0 {
			t.Fatalf("verdict %+v", v)
		}
	})
	t.Run("symlink retarget", func(t *testing.T) {
		retarget := func(f *proofFix, target string) string {
			f.git("rm", "-q", "--cached", "link")
			if err := osRemove(f.dir + "/link"); err != nil {
				t.Fatal(err)
			}
			if err := osSymlink(target, f.dir+"/link"); err != nil {
				t.Fatal(err)
			}
			f.git("add", "-A")
			f.git("commit", "-qm", "retarget "+target)
			return f.rev("HEAD")
		}
		f := newProofFix(t)
		mb := seed(f)
		f.branch("topic", mb)
		c := retarget(f, "c")
		f.checkout(f.main)
		retarget(f, "b")
		different := f.verdictBare(f.main, "topic", patchPolicy(false))
		if different.Disposition != "" || different.Matched != 0 {
			t.Fatalf("different retarget matched: %+v", different)
		}
		f.git("reset", "-q", "--hard", mb)
		f.commit("unrelated", map[string]string{"u.txt": "u\n"})
		f.pick(c)
		wantVerdict(t, f.verdictBare(f.main, "topic", patchPolicy(false)), "proven", "patch-id")
	})
}

func TestPatchIDAmbiguousMergeBase(t *testing.T) {
	f := newProofFix(t)
	root := f.rev("HEAD")
	f.branch("x", root)
	f.commit("x0", map[string]string{"x.txt": "x\n"})
	f.branch("y", root)
	f.commit("y0", map[string]string{"y.txt": "y\n"})
	f.git("merge", "-qm", "x into y", "x")
	y1 := f.rev("HEAD")
	f.checkout("x")
	y0 := f.rev("y~1")
	f.git("merge", "-qm", "y0 into x", y0)
	_ = y1
	v := f.verdict("x", "y", patchPolicy(false))
	wantVerdict(t, v, "needs-review", "", "ambiguous-merge-base")
}

// Count bounds end to end: 1,024 branch-side and 4,096 base-side commits are
// accepted, one more of either is proof-limit.
func TestPatchIDCommitCountBounds(t *testing.T) {
	build := func(t *testing.T, branchN, baseN int) integrationVerdict {
		f := newProofFix(t)
		root := f.rev("HEAD")
		f.fastImport(importChain("m", branchN, "refs/heads/topic", root, 1000000) + importChain("m", baseN, "refs/heads/baseline", root, 2000000))
		return f.verdictBare("baseline", "topic", patchPolicy(false))
	}
	v := build(t, 1024, 4096)
	wantVerdict(t, v, "proven", "patch-id")
	if v.Matched != 1024 {
		t.Fatalf("matched %d", v.Matched)
	}
	wantVerdict(t, build(t, 1025, 4096), "needs-review", "", "proof-limit")
	wantVerdict(t, build(t, 1024, 4097), "needs-review", "", "proof-limit")
}

func TestPatchIDHardStopsAndObjectDirectory(t *testing.T) {
	setup := func(t *testing.T) *proofFix {
		f, _ := rebaseFix(t, 0, 1, 2)
		return f
	}
	t.Run("grafts", func(t *testing.T) {
		f := setup(t)
		f.write(".git/info/grafts", f.rev("HEAD")+"\n")
		wantVerdict(t, f.verdictBare(f.main, "topic", patchPolicy(false)), "retained", "", "history-override")
	})
	t.Run("shallow", func(t *testing.T) {
		f := setup(t)
		f.write(".git/shallow", f.rev("HEAD")+"\n")
		wantVerdict(t, f.verdictBare(f.main, "topic", patchPolicy(false)), "retained", "", "history-override")
	})
	t.Run("alternates", func(t *testing.T) {
		f := setup(t)
		f.write(".git/objects/info/alternates", f.dir+"/.git/objects\n")
		wantVerdict(t, f.verdictBare(f.main, "topic", patchPolicy(false)), "retained", "", "unsupported-metadata")
	})
	t.Run("object directory unavailable", func(t *testing.T) {
		f := setup(t)
		engine := defaultIntegrationEngine()
		engine.makeObjectDir = func() (string, error) { return "", fmt.Errorf("no space") }
		v, err := f.verdictWith(f.main, "topic", patchPolicy(false), engine, false)
		if err != nil {
			t.Fatal(err)
		}
		wantVerdict(t, v, "needs-review", "", "object-dir-unavailable")
	})
}

// The repository's object directory, refs and packed-refs are byte-identical
// before and after a proof run.
func TestPatchIDLeavesTheRepositoryUntouched(t *testing.T) {
	f, _ := rebaseFix(t, 0, 1, 2)
	f.git("pack-refs", "--all")
	before := snapshotTree(t, f.dir+"/.git", "index", "FETCH_HEAD")
	wantVerdict(t, f.verdict(f.main, "topic", patchPolicy(false)), "proven", "patch-id")
	after := snapshotTree(t, f.dir+"/.git", "index", "FETCH_HEAD")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("the proof wrote into the repository")
	}
}

// Every row of the command bounds table, at the bound (accepted) and one over
// (proof-limit), for SHA-1 and SHA-256 widths; malformed lines are refused.
func TestIntegrationCommandBoundsAtAndOverTheBound(t *testing.T) {
	for _, width := range []int{40, 64} {
		hex := func(c byte) string { return strings.Repeat(string(c), width) }
		path := func(n int) string { return strings.Repeat("p", n) }
		type row struct {
			spec   integrationSpec
			record func(pathLen int) string
			bad    string // a malformed record
			paths  bool   // the record ends in a path of adjustable length
		}
		rows := []row{
			{specMergeBase, func(int) string { return hex('a') }, "abc", false},
			{specRevParse, func(int) string { return hex('b') }, strings.ToUpper(hex('b')), false},
			{specCount, func(int) string { return "1234567" }, "12345678", false},
			{specLog, func(int) string { return hex('a') + " " + hex('b') + " " + hex('c') }, hex('a') + " " + hex('b'), false},
			{specPatchID, func(int) string { return hex('a') + " " + hex('b') }, hex('a') + hex('b'), false},
			{specRaw, func(n int) string { return ":100644 100755 " + hex('a') + " " + hex('b') + " M\t" + path(n) }, ":100644 100755 " + hex('a') + " " + hex('b') + " M\t" + path(4097), true},
			{specNameOnly, func(n int) string { return path(n) }, path(4097), true},
			{specNumstat, func(n int) string { return "-\t-\t" + path(n) }, "x\t1\tfile", true},
			{specLsTree, func(n int) string { return "160000 commit " + hex('a') + "\t" + path(n) }, "100644 tree?" + hex('a') + "\tp", true},
			{specMergeTree, func(int) string { return hex('a') }, hex('a') + "x", false},
		}
		for _, r := range rows {
			t.Run(fmt.Sprintf("%s/%d", r.spec.name, width), func(t *testing.T) {
				stream := func(n int, pathLen int, extra string) io.Reader {
					var b strings.Builder
					for i := 0; i < n; i++ {
						b.WriteString(r.record(pathLen))
						b.WriteByte(r.spec.sep)
					}
					b.WriteString(extra)
					return strings.NewReader(b.String())
				}
				// The longest path at which maxRecords records still fit the byte bound.
				pathLen := 4096
				if r.paths {
					for pathLen > 1 && int64(r.spec.maxRecords*(len(r.record(pathLen))+1)) > r.spec.bytes(width) {
						pathLen--
					}
				}
				count := 0
				accept := func([]byte) error { count++; return nil }
				if err := readIntegrationRecords(stream(r.spec.maxRecords, pathLen, ""), r.spec, width, accept); err != nil || count != r.spec.maxRecords {
					t.Fatalf("at the bound: %v after %d records", err, count)
				}
				if err := readIntegrationRecords(stream(r.spec.maxRecords+1, 1, ""), r.spec, width, accept); !errors.Is(err, errIntegrationLimit) {
					t.Fatalf("one record over: %v", err)
				}
				if r.paths && pathLen < 4096 {
					if err := readIntegrationRecords(stream(r.spec.maxRecords, pathLen+1, ""), r.spec, width, accept); !errors.Is(err, errIntegrationLimit) {
						t.Fatalf("one byte over the byte bound: %v", err)
					}
				}
				if r.paths {
					if err := readIntegrationRecords(stream(0, 0, r.record(4097)+string(r.spec.sep)), r.spec, width, accept); !errors.Is(err, errIntegrationLimit) {
						t.Fatalf("4,097-byte path: %v", err)
					}
				}
				if err := readIntegrationRecords(stream(0, 0, r.bad+string(r.spec.sep)), r.spec, width, accept); !errors.Is(err, errIntegrationLimit) {
					t.Fatalf("malformed record: %v", err)
				}
				if err := readIntegrationRecords(stream(1, 1, r.record(1)), r.spec, width, accept); !errors.Is(err, errIntegrationLimit) {
					t.Fatalf("truncated final record: %v", err)
				}
			})
		}
	}
}

func TestIntegrationCheckAttrStreamBounds(t *testing.T) {
	stream := func(paths []string, value string) io.Reader {
		var b strings.Builder
		for _, p := range paths {
			b.WriteString(p + "\x00diff\x00" + value + "\x00")
		}
		return strings.NewReader(b.String())
	}
	long := strings.Repeat("p", 4096)
	many := make([]string, 8192)
	for i := range many {
		many[i] = fmt.Sprintf("%s%d", long[:4000], i)
	}
	found, err := parseCheckAttrStream(stream(many, "unspecified"), many)
	if err != nil || found {
		t.Fatalf("8,192 paths at the bound: found=%t %v", found, err)
	}
	if _, err = parseCheckAttrStream(stream(many, "unspecified"), append(many, "x")); !errors.Is(err, errIntegrationLimit) {
		t.Fatalf("8,193 paths: %v", err)
	}
	if _, err = parseCheckAttrStream(stream([]string{long + "x"}, "set"), []string{long + "x"}); !errors.Is(err, errIntegrationLimit) {
		t.Fatalf("4,097-byte path: %v", err)
	}
	if found, err = parseCheckAttrStream(stream([]string{"a", "b"}, "d"), []string{"a", "b"}); err != nil || !found {
		t.Fatalf("driver value not reported: %t %v", found, err)
	}
	if _, err = parseCheckAttrStream(strings.NewReader("a\x00other\x00set\x00"), []string{"a"}); !errors.Is(err, errIntegrationLimit) {
		t.Fatalf("wrong attribute name: %v", err)
	}
	if _, err = parseCheckAttrStream(strings.NewReader("b\x00diff\x00set\x00"), []string{"a"}); !errors.Is(err, errIntegrationLimit) {
		t.Fatalf("wrong path: %v", err)
	}
}

// Unrelated histories have no merge base: nothing is proven.
func TestPatchIDWithoutAMergeBaseIsAmbiguous(t *testing.T) {
	f := newProofFix(t)
	f.git("checkout", "-q", "--orphan", "other")
	f.git("rm", "-rfq", ".")
	f.commit("unrelated root", map[string]string{"o.txt": "o\n"})
	for _, kinds := range [][]string{{"patch-id"}, {"merge-noop"}, {"no-net-change"}} {
		wantVerdict(t, f.verdict(f.main, "other", noopPolicy(kinds...)), "needs-review", "", "ambiguous-merge-base")
	}
}

// The whitespace difference is reported even when the class is otherwise
// unproven, so a reviewer sees it next to partial-match.
func TestPatchIDReportsWhitespaceWhenOtherwiseUnproven(t *testing.T) {
	f := newProofFix(t)
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	f.commit("script", map[string]string{"s.sh": "  echo hi\n"})
	f.commit("other", map[string]string{"o.txt": "only on the branch\n"})
	f.checkout(f.main)
	f.commit("script landed", map[string]string{"s.sh": "echo hi\n"})
	for _, accept := range []bool{false, true} {
		v := f.verdict(f.main, "topic", patchPolicy(accept))
		wantVerdict(t, v, "needs-review", "", "whitespace-differs", "partial-match")
		if !reflect.DeepEqual(v.Flags, []string{"whitespace-differs"}) || v.Matched != 1 || v.Unmatched != 1 {
			t.Fatalf("accept=%t verdict %+v", accept, v)
		}
	}
}
