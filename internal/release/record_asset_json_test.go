// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"encoding/json"
	"testing"
)

func TestRecordAssetJSONPreservesClosedUnion(t *testing.T) {
	for _, a := range []RecordAsset{{File: &FileAsset{Kind: "file"}}, {Image: &OCIImageAsset{Kind: "oci-image"}}} {
		raw, e := json.Marshal(a)
		if e != nil {
			t.Fatal(e)
		}
		var model map[string]any
		if e = json.Unmarshal(raw, &model); e != nil {
			t.Fatal(e)
		}
		if model["kind"] == nil || model["File"] != nil || model["Image"] != nil {
			t.Fatalf("%s", raw)
		}
	}
	for _, a := range []RecordAsset{{}, {File: &FileAsset{}, Image: &OCIImageAsset{}}} {
		if _, e := json.Marshal(a); e == nil {
			t.Fatal("accepted ambiguous asset union")
		}
	}
}
