// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"encoding/json"
	"github.com/wstein/sourcesteward/internal/release"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseBuildResultSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(root(t), "schemas/v1/release-build.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	c := compiler()
	if err := c.AddResource(base+"release-build.schema.json", value(t, raw)); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(base + "release-build.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	// Outside CI refuses before inspecting any project or starting a project command.
	t.Setenv("GITHUB_ACTIONS", "false")
	r := release.BuildRelease(t.Context(), release.BuildOptions{Mode: "ci"})
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	doc := value(t, encoded)
	if err := s.Validate(doc); err != nil {
		t.Fatal(err)
	}
	m := doc.(map[string]any)
	m["schema_version"] = 2
	if s.Validate(m) == nil {
		t.Fatal("future version accepted")
	}
	m["schema_version"] = 1
	m["status"] = "invented"
	if s.Validate(m) == nil {
		t.Fatal("unknown status accepted")
	}
}
