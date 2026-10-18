// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"encoding/json"
	"testing"
)

func TestBuildViewPrivateProtocolPreservesNativePathBytes(t *testing.T) {
	original := buildViewPath("/native/literal\xff\npath")
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored buildViewPath
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored != original {
		t.Fatal("private protocol changed native pathname bytes")
	}
	if err := json.Unmarshal([]byte(`"not base64!"`), &restored); err == nil {
		t.Fatal("malformed private pathname frame accepted")
	}
}
