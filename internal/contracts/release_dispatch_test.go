// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/release"
)

func TestReleaseDispatchResultSchema(t *testing.T) {
	s := schema(t, "release-dispatch")
	result := release.Dispatch(context.Background(), release.DispatchOptions{Path: t.TempDir(), Commit: strings.Repeat("a", 40), Version: "v1.0.0"})
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err = s.Validate(value); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"request", "plan_id", "applied", "workflow_url", "http_status", "approval", "gates", "would_build", "network"} {
		old := value[field]
		delete(value, field)
		if err = s.Validate(value); err == nil {
			t.Fatalf("missing required %s accepted", field)
		}
		value[field] = old
	}
	value["status"] = "ok"
	value["error"] = nil
	value["verdict"] = "pass"
	value["applied"] = true
	value["plan_id"] = strings.Repeat("b", 64)
	value["http_status"] = 204
	value["request"] = map[string]any{"method": "POST", "endpoint": "repos/o/r/actions/workflows/.release.yml/dispatches", "body": map[string]any{"ref": "main", "inputs": map[string]any{"mode": "commit", "product": "", "commit": strings.Repeat("a", 40), "tag": "", "version": "v1.0.0"}}}
	value["network"] = []any{map[string]any{"method": "GET", "endpoint": "read"}, map[string]any{"method": "POST", "endpoint": "dispatch"}}
	effects := value["boundary"].(map[string]any)["effects"].(map[string]any)
	effects["network"] = true
	effects["writes"] = true
	if err = s.Validate(value); err != nil {
		t.Fatalf("accepted dispatch sample refused: %v", err)
	}
	request := value["request"].(map[string]any)
	request["method"] = "DELETE"
	if err = s.Validate(value); err == nil {
		t.Fatal("non-POST request accepted")
	}
	request["method"] = "POST"
	body := request["body"].(map[string]any)
	body["ref"] = "topic"
	if err = s.Validate(value); err == nil {
		t.Fatal("non-main ref accepted")
	}
	body["ref"] = "main"
	value["plan_id"] = "short"
	if err = s.Validate(value); err == nil {
		t.Fatal("short plan id accepted")
	}
}
