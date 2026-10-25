// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixturePruneV4(t *testing.T) (RegistrationPruneV4Options, RegistrationPruneV4Plan) {
	t.Helper()
	o, _, _, _ := fixturePruneObservation(t)
	mode := V3RunMode{Mode: "agent", Markers: []string{}, ByOwner: []string{}, Caller: o.Owner, CallerSource: "explicit"}
	opts := RegistrationPruneV4Options{Path: o.Source, StateDir: o.StateDir, Name: o.Registrations[0], GitDir: filepath.Join(o.Source, ".git", "worktrees", o.Registrations[0]), Mode: mode}
	r := PlanRegistrationPruneV4(context.Background(), opts)
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("fixture contract-4 plan: %+v", r)
	}
	return opts, *r.Plan
}
func pruneV4PlanPath(p RegistrationPruneV4Plan) string {
	return filepath.Join(p.StateDir, pruneV4Namespace, "plans", p.PlanID+".json")
}
func clonePruneV4(t *testing.T, p RegistrationPruneV4Plan) RegistrationPruneV4Plan {
	t.Helper()
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	var q RegistrationPruneV4Plan
	if e = json.Unmarshal(b, &q); e != nil {
		t.Fatal(e)
	}
	return q
}
func savePruneV4Fixture(t *testing.T, p RegistrationPruneV4Plan, target ...string) {
	t.Helper()
	var e error
	p.Digest, e = pruneV4Digest("stw-registration-plan-v4", p, "digest")
	if e != nil {
		t.Fatal(e)
	}
	b, e := pruneV4Canonical(p, "")
	if e != nil {
		t.Fatal(e)
	}
	path := pruneV4PlanPath(p)
	if len(target) > 0 {
		path = target[0]
	}
	if e = os.WriteFile(path, append(b, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}

func TestRegistrationPruneV4PlanRoundTrip(t *testing.T) {
	_, p := fixturePruneV4(t)
	loaded, e := ReadRegistrationPruneV4Plan(p.StateDir, p.PlanID)
	if e != nil || !reflect.DeepEqual(p, loaded) {
		t.Fatalf("roundtrip: %v", e)
	}
	raw, e := os.ReadFile(pruneV4PlanPath(p))
	if e != nil {
		t.Fatal(e)
	}
	canonical, e := pruneV4Canonical(p, "")
	if e != nil || !bytes.Equal(raw, append(canonical, '\n')) {
		t.Fatalf("stored canonical bytes: %v", e)
	}
	for path, mode := range map[string]os.FileMode{filepath.Join(p.StateDir, pruneV4Namespace): 0700, filepath.Join(p.StateDir, pruneV4Namespace, "plans"): 0700, pruneV4PlanPath(p): 0600} {
		info, e := os.Lstat(path)
		if e != nil || info.Mode().Perm() != mode {
			t.Fatalf("private mode %s: %v %v", path, info, e)
		}
	}
	if p.Expected.CommonDirMount.Mount == "" || p.Expected.CommonDirMount.Identity.Path != p.Expected.SourceSnapshot.Identity.RepositoryDir {
		t.Fatal("common-directory mount witness missing")
	}
}

func TestRegistrationPruneV4ReaderRequiresZeroValuedFields(t *testing.T) {
	_, p := fixturePruneV4(t)
	raw, e := os.ReadFile(pruneV4PlanPath(p))
	if e != nil {
		t.Fatal(e)
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if e = decoder.Decode(&value); e != nil {
		t.Fatal(e)
	}
	// The original digest remains correct after decode when a required false field
	// is absent. This must fail on required shape, not on an unrelated digest.
	delete(value["mode"].(map[string]any), "stdin_tty")
	b, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(pruneV4PlanPath(p), append(b, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	_, e = ReadRegistrationPruneV4Plan(p.StateDir, p.PlanID)
	assertPruneReason(t, e, "prune-plan-refused", "")
}

func TestRegistrationPruneV4CommonMountDetectsChangedObservation(t *testing.T) {
	o, _, task, _ := fixturePruneObservation(t)
	for _, kind := range []string{"mount", "descriptor", "mode"} {
		t.Run(kind, func(t *testing.T) {
			ctx := pruneHookContext(func(stage, path string, v any) {
				if stage != "common-mount-observed" {
					return
				}
				w := v.(*pruneDirectoryWitness)
				switch kind {
				case "mount":
					w.Mount += "-changed"
				case "descriptor":
					w.Identity.Inode++
				case "mode":
					w.Mode ^= 0200
				}
			})
			_, e := observePruneV4Common(ctx, task.Native.RepositoryDir)
			assertPruneReason(t, e, "prune-mount-changed", "Common-directory mount or descriptor/path binding changed.")
		})
	}
	if _, e := observePruneV4Common(context.Background(), filepath.Join(o.Source, ".git")); e != nil {
		t.Fatal(e)
	}
}

func TestRegistrationPruneV4StrictReaderRefusesResignedInvalidPlans(t *testing.T) {
	_, p := fixturePruneV4(t)
	cases := map[string]func(*RegistrationPruneV4Plan){

		"state-dir": func(q *RegistrationPruneV4Plan) {
			q.StateDir += "-other"
			q.PayloadPath = filepath.Join(q.StateDir, pruneV4Namespace, "payloads", q.PlanID)
		},
		"backend":      func(q *RegistrationPruneV4Plan) { q.Backend = "jj" },
		"invalid-mode": func(q *RegistrationPruneV4Plan) { q.Mode.Mode = "robot" },
		"missing-caller": func(q *RegistrationPruneV4Plan) {
			q.Mode.Caller = ""
			q.Mode.Mode = "human"
			q.Registration.Scope = "other"
			q.AuthorizedScopes = []string{"other"}
		},
		"nil-recovery-limits":   func(q *RegistrationPruneV4Plan) { q.RecoveryLimits = nil },
		"empty-recovery-limits": func(q *RegistrationPruneV4Plan) { q.RecoveryLimits = []string{} },
		"owner-source":          func(q *RegistrationPruneV4Plan) { q.Registration.OwnerSource = "inferred" },
		"agent-other-scope": func(q *RegistrationPruneV4Plan) {
			q.Mode.Caller = "other-agent"
			q.Registration.Scope = "other"
			q.AuthorizedScopes = []string{"other"}
		},
		"human-other-no-authorization": func(q *RegistrationPruneV4Plan) {
			q.Mode.Mode = "human"
			q.Mode.Caller = "other-human"
			q.Registration.Scope = "other"
		},
		"own-scope-with-authorization": func(q *RegistrationPruneV4Plan) { q.AuthorizedScopes = []string{"other"} },
		"creation-digest":              func(q *RegistrationPruneV4Plan) { q.Expected.CreationDigest = "short" },
		"journal-digest":               func(q *RegistrationPruneV4Plan) { q.Expected.CreationJournalDigest = "short" },
		"absence-destination":          func(q *RegistrationPruneV4Plan) { q.Expected.Absence.Destination += "-other" },
		"absence-parent": func(q *RegistrationPruneV4Plan) {
			q.Expected.Absence.Parent = q.Expected.Absence.Ancestors[0].Identity
			q.Expected.Absence.Ancestors = q.Expected.Absence.Ancestors[:1]
		},
		"nil-native-refs": func(q *RegistrationPruneV4Plan) {
			q.Expected.Native.References = nil
			q.Expected.SourceSnapshot = v3SnapshotWithState(q.Expected.SourceSnapshot, q.Expected.Native)
		},
		"nil-native-registrations": func(q *RegistrationPruneV4Plan) {
			q.Expected.Native.Registrations = nil
			q.Expected.SourceSnapshot = v3SnapshotWithState(q.Expected.SourceSnapshot, q.Expected.Native)
		},
		"nil-native-protected-roots": func(q *RegistrationPruneV4Plan) {
			q.Expected.Native.ProtectedRoots = nil
			q.Expected.SourceSnapshot = v3SnapshotWithState(q.Expected.SourceSnapshot, q.Expected.Native)
		},
		"last-ancestor-parent": func(q *RegistrationPruneV4Plan) { q.Expected.Absence.Parent.Inode++ },
		"metadata-traversal":   func(q *RegistrationPruneV4Plan) { q.Expected.Metadata[0].Path = "../../outside" },
		"metadata-nul":         func(q *RegistrationPruneV4Plan) { q.Expected.Metadata[0].Path = "a\x00" },
		"metadata-long-path":   func(q *RegistrationPruneV4Plan) { q.Expected.Metadata[0].Path = strings.Repeat("a", 4097) },
		"duplicate-ancestor": func(q *RegistrationPruneV4Plan) {
			a := q.Expected.Absence.Ancestors
			q.Expected.Absence.Ancestors = append([]PruneV4Directory{a[0]}, a...)
		},
		"reordered-ancestors": func(q *RegistrationPruneV4Plan) { a := q.Expected.Absence.Ancestors; a[0], a[1] = a[1], a[0] },
		"metadata-order":      func(q *RegistrationPruneV4Plan) { m := q.Expected.Metadata; m[0], m[1] = m[1], m[0] },
		"metadata-kind":       func(q *RegistrationPruneV4Plan) { q.Expected.Metadata[0].Kind = "device" },
		"metadata-file-bound": func(q *RegistrationPruneV4Plan) {
			for i, m := range q.Expected.Metadata {
				if m.Kind == "file" {
					q.Expected.MetadataBytes += (64 << 20) + 1 - m.Size
					q.Expected.Metadata[i].Size = (64 << 20) + 1
					return
				}
			}
		},
		"metadata-total-bound": func(q *RegistrationPruneV4Plan) {
			var m PruneV4Metadata
			for _, v := range q.Expected.Metadata {
				if v.Kind == "file" {
					m = v
					break
				}
			}
			m.Size = 64 << 20
			q.Expected.Metadata = nil
			for i := 0; i < 5; i++ {
				m.Path = fmt.Sprintf("f%d", i)
				q.Expected.Metadata = append(q.Expected.Metadata, m)
			}
			q.Expected.MetadataBytes = 5 * (64 << 20)
		},
		"metadata-count-bound": func(q *RegistrationPruneV4Plan) {
			q.Expected.Metadata = nil
			for i := 0; i < 1025; i++ {
				q.Expected.Metadata = append(q.Expected.Metadata, PruneV4Metadata{Path: fmt.Sprintf("f%04d", i), Kind: "absent"})
			}
			q.Expected.MetadataBytes = 0
		},
		"sibling-path": func(q *RegistrationPruneV4Plan) { q.Expected.ProtectedSiblings[0].Identity.Path += "-other" },
		"sibling-order": func(q *RegistrationPruneV4Plan) {
			m := q.Expected.ProtectedSiblings[0]
			a, b := m, m
			a.Name = "a"
			a.Identity.Path = filepath.Join(q.Expected.SourceSnapshot.Identity.RepositoryDir, "worktrees", "a")
			b.Name = "b"
			b.Identity.Path = filepath.Join(q.Expected.SourceSnapshot.Identity.RepositoryDir, "worktrees", "b")
			q.Expected.ProtectedSiblings = []PruneV4Sibling{b, a}
		},
		"head-oid":        func(q *RegistrationPruneV4Plan) { q.Expected.ResolvedHEAD = "short" },
		"markers-order":   func(q *RegistrationPruneV4Plan) { q.Mode.Markers = []string{"z", "a"} },
		"owners-order":    func(q *RegistrationPruneV4Plan) { q.Mode.ByOwner = []string{"z", "a"} },
		"old-version":     func(q *RegistrationPruneV4Plan) { q.SchemaVersion = 3 },
		"wrong-operation": func(q *RegistrationPruneV4Plan) { q.Operation = "tidy-v3" },
		"common-mount":    func(q *RegistrationPruneV4Plan) { q.Expected.CommonDirMount.Mount = "" },
		"common-path":     func(q *RegistrationPruneV4Plan) { q.Expected.CommonDirMount.Identity.Path = q.SourcePath },
		"scope":           func(q *RegistrationPruneV4Plan) { q.Registration.Scope = "other" },
		"source":          func(q *RegistrationPruneV4Plan) { q.SourcePath += "-other" },
		"gitdir":          func(q *RegistrationPruneV4Plan) { q.Registration.GitDir += "-other" },
		"payload":         func(q *RegistrationPruneV4Plan) { q.PayloadPath = filepath.Dir(q.StateDir) },
		"native-digest":   func(q *RegistrationPruneV4Plan) { q.Expected.Native.References = []V3Reference{} },
		"nil-metadata":    func(q *RegistrationPruneV4Plan) { q.Expected.Metadata = nil },
		"metadata-bytes":  func(q *RegistrationPruneV4Plan) { q.Expected.MetadataBytes++ },
		"effect":          func(q *RegistrationPruneV4Plan) { q.Effects = []string{"remove-files"} },
		"task-digest":     func(q *RegistrationPruneV4Plan) { q.Expected.TaskDigest = "short" },
		"nil-ancestors":   func(q *RegistrationPruneV4Plan) { q.Expected.Absence.Ancestors = nil },
	}

	for _, field := range []string{"digest", "physical", "size"} {
		field := field
		cases["absent-"+field] = func(q *RegistrationPruneV4Plan) {
			m := &q.Expected.Metadata[0]
			if m.Kind == "file" {
				q.Expected.MetadataBytes -= m.Size
			}
			m.Kind = "absent"
			m.Digest = ""
			m.Physical = V3PhysicalIdentity{}
			m.Size = 0
			switch field {
			case "digest":
				m.Digest = strings.Repeat("a", 64)
			case "physical":
				m.Physical.Inode = 1
			case "size":
				m.Size = 1
			}
		}
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			q := clonePruneV4(t, p)
			mutate(&q)
			savePruneV4Fixture(t, q, pruneV4PlanPath(p))
			_, e := ReadRegistrationPruneV4Plan(p.StateDir, p.PlanID)
			assertPruneReason(t, e, "prune-plan-refused", "Contract-4 plan version, operation, witness, scope or digest is invalid.")
		})
	}
}

func TestRegistrationPruneV4ReaderNeverFallsBackOrCreates(t *testing.T) {
	_, p := fixturePruneV4(t)
	raw, e := os.ReadFile(pruneV4PlanPath(p))
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"unknown-field", "duplicate-key", "trailing-data", "wrong-case", "invalid-utf8", "indented", "missing-lf", "extra-lf", "missing-file", "symlink", "wide-mode"} {
		t.Run(kind, func(t *testing.T) {
			path := pruneV4PlanPath(p)
			_ = os.Remove(path)
			b := append([]byte{}, raw...)
			switch kind {
			case "unknown-field":
				b = append([]byte(`{"not_a_contract_field":true,`), raw[1:]...)
			case "duplicate-key":
				b = append([]byte(`{"schema_version":4,`), raw[1:]...)
			case "trailing-data":
				b = append(b, []byte("{}")...)
			case "wrong-case":
				b = bytes.Replace(b, []byte(`"schema_version"`), []byte(`"Schema_version"`), 1)
			case "invalid-utf8":
				// Re-sign the plan with the decoder's replacement character so neither
				// the digest nor another semantic field masks invalid raw UTF-8.
				q := clonePruneV4(t, p)
				q.RecoveryLimits = []string{"\ufffd"}
				savePruneV4Fixture(t, q)
				b, _ = os.ReadFile(path)
				b = bytes.Replace(b, []byte("\ufffd"), []byte{0xff}, 1)
			case "indented":
				var pretty bytes.Buffer
				pruneMust(t, json.Indent(&pretty, raw, "", "  "))
				b = pretty.Bytes()
			case "missing-lf":
				b = bytes.TrimSuffix(b, []byte("\n"))
			case "extra-lf":
				b = append(b, '\n')
			case "symlink":
				outside := filepath.Join(t.TempDir(), "foreign")
				if e = os.WriteFile(outside, raw, 0600); e != nil {
					t.Fatal(e)
				}
				if e = os.Symlink(outside, path); e != nil {
					t.Fatal(e)
				}
			}
			if kind != "symlink" && kind != "missing-file" {
				if e = os.WriteFile(path, b, 0600); e != nil {
					t.Fatal(e)
				}
				if kind == "wide-mode" {
					if e = os.Chmod(path, 0644); e != nil {
						t.Fatal(e)
					}
				}
			}
			_, e = ReadRegistrationPruneV4Plan(p.StateDir, p.PlanID)
			switch kind {
			case "missing-file":
				if !errors.Is(e, os.ErrNotExist) {
					t.Fatalf("want missing-file reason, got %v", e)
				}
			case "symlink", "wide-mode":
				assertPruneReason(t, e, "invalid-state", "Record must be a private bounded single-link regular file.")
			case "duplicate-key", "trailing-data", "wrong-case":
				assertPruneReason(t, e, "invalid-state", "")
			case "indented", "missing-lf", "extra-lf":
				assertPruneReason(t, e, "prune-plan-refused", "Contract-4 record must use canonical JSON followed by one LF.")
			default:
				assertPruneReason(t, e, "prune-plan-refused", "Contract-4 record contains incompatible JSON.")
			}
			for _, name := range []string{"journals", "payloads", "restores"} {
				if _, e = os.Lstat(filepath.Join(p.StateDir, pruneV4Namespace, name)); !os.IsNotExist(e) {
					t.Fatalf("reader created %s: %v", name, e)
				}
			}
		})
	}
}

func TestRegistrationPruneV4DigestDomainsAndCanonicalKeys(t *testing.T) {
	a := map[string]any{"b": "\x00\nλ", "a": json.Number("4")}
	b := map[string]any{"a": json.Number("4"), "b": "\x00\nλ"}
	x, e := pruneV4Canonical(a, "")
	if e != nil {
		t.Fatal(e)
	}
	y, e := pruneV4Canonical(b, "")
	if e != nil || !bytes.Equal(x, y) {
		t.Fatal("object order affected canonical encoding")
	}
	if pruneV4Hash("stw-registration-plan-v4", x) == pruneV4Hash("stw-registration-plan-file-v4", x) {
		t.Fatal("digest domains collapsed")
	}
	if strings.ContainsRune(string(x), '\n') {
		t.Fatal("canonical JSON has a raw LF")
	}
}

func TestRegistrationPruneV4HumanOtherScopeNeedsConfirmation(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "refused", true: "accepted"}[accept], func(t *testing.T) {
			opts, original := fixturePruneV4(t)
			opts.Mode.Mode, opts.Mode.Caller = "human", "different-owner"
			calls := 0
			opts.Confirm = func(word, id string) error {
				calls++
				if word != original.Registration.Owner || !pruneV4ID(id, 32) {
					t.Fatalf("confirmation binding: %q %q", word, id)
				}
				if !accept {
					return context.Canceled
				}
				return nil
			}
			result := PlanRegistrationPruneV4(context.Background(), opts)
			if calls != 1 {
				t.Fatalf("confirmation calls=%d", calls)
			}
			entries, err := os.ReadDir(filepath.Dir(pruneV4PlanPath(original)))
			if err != nil {
				t.Fatal(err)
			}
			if !accept {
				if result.Status != "refused" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "prune-scope-refused" || result.Diagnostics[0].Message != "Typed owner and plan confirmation was refused." || len(entries) != 1 {
					t.Fatalf("refused confirmation persisted or wrong reason: %+v, plans=%d", result, len(entries))
				}
			} else if result.Status != "ok" || result.Plan == nil || result.Plan.Registration.Scope != "other" || !reflect.DeepEqual(result.Plan.AuthorizedScopes, []string{"other"}) || len(entries) != 2 {
				t.Fatalf("accepted confirmation/scope: %+v, plans=%d", result, len(entries))
			}
		})
	}
}
