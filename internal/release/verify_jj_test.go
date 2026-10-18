// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"github.com/wstein/sourcesteward/internal/inspection"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyRefusesManagedBackendsWithoutFallback(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		for _, layout := range jjLayouts() {
			t.Run(lane+"/"+layout.name, func(t *testing.T) {
				useJJ(t, binary)
				home := t.TempDir()
				d, ok := layout.setup(t, home)
				if !ok {
					t.Skip("layout unavailable")
				}
				before := jjState(t, d, home)
				files := snapshot(t, d)
				r := Verify(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: d, Subject: "v0.2.0"}})
				if r.Status != "refused" || r.Error == nil || r.Error.Code != "unsupported-backend" || !strings.Contains(r.Error.Message, "CI run") || !strings.Contains(r.Error.Message, "plan <commit>") {
					t.Fatalf("%+v", r.Error)
				}
				if after := jjState(t, d, home); after != before {
					t.Fatal("jj operation or working copy changed")
				}
				if diff := differences(files, snapshot(t, d)); len(diff) > 0 {
					t.Fatal(diff)
				}
			})
		}
	}
	t.Run("gitbutler", func(t *testing.T) {
		d, _ := planRepo(t)
		if e := os.MkdirAll(filepath.Join(d, ".git/gitbutler"), 0755); e != nil {
			t.Fatal(e)
		}
		p := Plan(context.Background(), PlanOptions{Path: d, Subject: "v0.2.0"})
		if p.Error == nil || !strings.Contains(p.Error.Message, "CI run") {
			t.Fatalf("tag plan must name the CI alternative: %+v", p.Error)
		}
		r := Verify(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: d, Subject: "v0.2.0"}})
		if r.Status != "refused" || r.Error == nil || r.Error.Code != "unsupported-backend" || !strings.Contains(r.Error.Message, "CI run") {
			t.Fatal(r.Error)
		}
	})
}
func TestRecordReadsJJCommittedContractWithoutSnapshot(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		for _, layout := range jjLayouts() {
			t.Run(lane+"/"+layout.name, func(t *testing.T) {
				useJJ(t, binary)
				home := t.TempDir()
				d, ok := layout.setup(t, home)
				if !ok {
					t.Skip("layout unavailable")
				}
				_, _, dir := recordCommandFixture(t)
				write(t, d, ".stw/release.toml", exampleBytes(t, "go-cli"))
				if out, e := jjRun(t, d, home, "describe", "-m", "release fixture"); e != nil {
					t.Fatalf("%s %v", out, e)
				}
				c, e := jjRun(t, d, home, "--ignore-working-copy", "log", "-r", "@", "--no-graph", "-T", "commit_id")
				if e != nil {
					t.Fatal(e)
				}
				c = strings.TrimSpace(c)
				raw, e := os.ReadFile(filepath.Join(dir, "build-manifest.json"))
				if e != nil {
					t.Fatal(e)
				}
				doc, ff := ParseReleaseDocument(raw)
				if len(ff) > 0 {
					t.Fatal(ff)
				}
				manifest := doc.(*BuildManifest)
				manifest.Commit = c
				raw, e = EncodeReleaseDocument(manifest)
				if e != nil {
					t.Fatal(e)
				}
				os.WriteFile(filepath.Join(dir, "build-manifest.json"), raw, 0644)
				sums := ""
				for _, a := range manifest.Assets {
					sums += a.File.SHA256 + "  " + a.File.Name + "\n"
				}
				sums += hashBytes(raw) + "  build-manifest.json\n"
				os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums), 0644)
				write(t, d, "unsnapshotted", []byte("pending work"))
				before := jjState(t, d, home)
				files := snapshot(t, d)
				r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: manifest.Version, From: dir})
				if r.Status != "ok" {
					t.Fatalf("%+v", r.Error)
				}
				if after := jjState(t, d, home); after != before {
					t.Fatal("jj operation or working copy changed")
				}
				if diff := differences(files, snapshot(t, d)); len(diff) > 0 {
					t.Fatal(diff)
				}
				t.Logf("%s %s committed record source read without snapshot", lane, layout.name)
			})
		}
	}
}

func TestJJRecordLockHashStreamsCommittedBinaryBeyondCapture(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		t.Run(lane, func(t *testing.T) {
			useJJ(t, binary)
			home := t.TempDir()
			d, ok := jjLayouts()[0].setup(t, home)
			if !ok {
				t.Skip("layout unavailable")
			}
			data := bytes.Repeat([]byte{0, 255, 1, 254}, 40000)
			write(t, d, "lock.bin", data)
			if out, e := jjRun(t, d, home, "describe", "-m", "binary lock"); e != nil {
				t.Fatalf("%s %v", out, e)
			}
			c, e := jjRun(t, d, home, "--ignore-working-copy", "log", "-r", "@", "--no-graph", "-T", "commit_id")
			if e != nil {
				t.Fatal(e)
			}
			write(t, d, "lock.bin", []byte("working tree is different"))
			before := jjState(t, d, home)
			files := snapshot(t, d)
			e = inspection.WithJJReleaseTree(context.Background(), d, strings.TrimSpace(c), func(reader *inspection.JJReleaseReader) error {
				hash, e := committedHash(context.Background(), jjSource{reader}, "lock.bin")
				if e == nil && hash != hashBytes(data) {
					t.Fatalf("wrong committed binary hash %s", hash)
				}
				return e
			})
			if e != nil {
				t.Fatal(e)
			}
			if jjState(t, d, home) != before {
				t.Fatal("native hash changed operation or working copy")
			}
			if diff := differences(files, snapshot(t, d)); len(diff) > 0 {
				t.Fatal(diff)
			}
		})
	}
}
