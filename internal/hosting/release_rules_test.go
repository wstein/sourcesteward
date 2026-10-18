// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"testing"
)

func TestRulesetReadAdvisoryFailureIndependent(t *testing.T) {
	rows, err := readReleaseRules(context.Background(), apiReaderFunc(func(context.Context, string) ([]byte, error) {
		return []byte(`[{"target":"tag","enforcement":"active","conditions":{"ref_name":{"include":["refs/tags/v*"],"exclude":[]}},"rules":[{"type":"creation"},{"type":"update"},{"type":"deletion"}]}]`), nil
	}), "o/r")
	if err != nil || len(rows) != 1 || rows[0].Target != "tag" {
		t.Fatalf("%+v %v", rows, err)
	}
}
