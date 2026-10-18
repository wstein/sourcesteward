// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"github.com/santhosh-tekuri/jsonschema/v6"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var recordSchemaNames = []string{"release-record", "release-build-manifest", "release-oci-result"}

func recordSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	c := compiler()
	for _, n := range recordSchemaNames {
		raw, err := os.ReadFile(filepath.Join(root(t), "schemas/v1", n+".schema.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource(base+n+".schema.json", value(t, raw)); err != nil {
			t.Fatal(err)
		}
	}
	s, err := c.Compile(base + name + ".schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestReleaseRecordSchemasEqualContract(t *testing.T) {
	raw, err := os.ReadFile(releaseSpecFile(t, "contracts/record.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := strings.Split(string(raw), "```json\n")
	for i, n := range recordSchemaNames {
		want := strings.Split(blocks[i+1], "```")[0]
		got, err := os.ReadFile(filepath.Join(root(t), "schemas/v1", n+".schema.json"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s differs from normative block", n)
		}
	}
}
func TestReleaseRecordExamplesAndMutants(t *testing.T) {
	for _, file := range []string{"go-cli.record", "oci-bundle.record", "polyglot.record", "skill-pack.record", "mirror-zlib.record", "mirror-busybox.record", "go-cli.manifest", "mirror-zlib.manifest", "oci-bundle.result"} {
		t.Run(file, func(t *testing.T) {
			name := "release-record"
			if strings.HasSuffix(file, "manifest") {
				name = "release-build-manifest"
			}
			if strings.HasSuffix(file, "result") {
				name = "release-oci-result"
			}
			s := recordSchema(t, name)
			raw, err := os.ReadFile(releaseSpecFile(t, "examples/"+file+".json"))
			if err != nil {
				t.Fatal(err)
			}
			doc := value(t, raw)
			if err := s.Validate(doc); err != nil {
				t.Fatal(err)
			}
			m := doc.(map[string]any)
			m["unknown"] = true
			if s.Validate(m) == nil {
				t.Error("unknown key accepted")
			}
			delete(m, "unknown")
			m["schema_version"] = 2
			if s.Validate(m) == nil {
				t.Error("future schema accepted")
			}
			m["schema_version"] = 1
			if assets, ok := m["assets"].([]any); ok {
				for _, v := range assets {
					a := v.(map[string]any)
					if a["kind"] == "file" {
						a["index_digest"] = "sha256:abc"
						if s.Validate(m) == nil {
							t.Error("file index digest accepted")
						}
						delete(a, "index_digest")
						a["sha256"] = "abc"
						if s.Validate(m) == nil {
							t.Error("short digest accepted")
						}
						break
					} else {
						a["tags"] = []any{}
						if s.Validate(m) == nil {
							t.Error("empty tags accepted")
						}
						break
					}
				}
			} else {
				m["index_digest"] = "sha256:abc"
				if s.Validate(m) == nil {
					t.Error("short OCI digest accepted")
				}
			}
		})
	}
}
