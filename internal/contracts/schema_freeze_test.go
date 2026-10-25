// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// Spec 031 freezes the published v1 and v2 schemas: streamed Git reads, the
// proof classes and the report change no published byte of either contract.
// A deliberate schema change must update these digests in the same commit.
var frozenPublishedSchemas = map[string]string{
	"schemas/v1/hosting.schema.json":                "5cf8bd2e67e209ee5f39befc2a0b0765a1f0fa1bb8082a940d1993e360133ff7",
	"schemas/v1/inspect.schema.json":                "8e12ee38a3e065e1f2e47b88ec4b1668cd8a32d8879e4803870e8994a9cfcfae",
	"schemas/v1/inventory.schema.json":              "8a8aeb281ca24c91809ea8fc95e83655d5a459afce51b1993965c9fa25a805f8",
	"schemas/v1/pins.schema.json":                   "1ae773a4fa0c4d08a9948ff050600505165323865a44b213ae91b205204c055b",
	"schemas/v1/issues.schema.json":                 "10082037cbb97bc5311e52e9738daf3f774b0f3ee9ca97eb84d3a93d6b94f6ee",
	"schemas/v1/pulls.schema.json":                  "e478c1062270f0d265052bee428d50411033306a2fac2b15142ab5f90e67a9f8",
	"schemas/v1/release-build-manifest.schema.json": "ca9fc9e2087d421d7bc0be1bc341fcb76248af6668f7719b3260bd9d949a84fd",
	"schemas/v1/release-build.schema.json":          "f0f63a7ab8b7e19ae7437855e8da62c223c8f937a1740542bc52561cc9e88f73",
	"schemas/v1/release-check.schema.json":          "df1bf6730b4ad4e8495b2f970cc2774e6ccb03a093f9de4fc2db6f963048c5b4",
	"schemas/v1/release-config.schema.json":         "73ef31f244adfc57d32f80d0f67295a58efd96b1841547d5961e28f5e99ff7ee",
	"schemas/v1/release-dispatch.schema.json":       "089ad25133e195661fefd2f972984568483af7f4b2339f5f3ce70e1dc4b236c5",
	"schemas/v1/release-oci-result.schema.json":     "9286b63a73c9eb341c0546bacc3ef5372bf30603d650d84021f160068cf8ed6a",
	"schemas/v1/release-plan.schema.json":           "a7bfbc106acdef7deecc7f700c2bca9cf7ade054dbe696765c958cdba5df307f",
	"schemas/v1/release-record-result.schema.json":  "eaf0b4d6bbd52c61ae70a05b58b969e13ffd64a7d85e6d92b34406cc3592117d",
	"schemas/v1/release-record.schema.json":         "bd1c94371b3cf7489fdf1cce2f38b896317a0a3aecd054e1a1fbbd15bfb8192e",
	"schemas/v1/release-verify.schema.json":         "c6bd29dea143ea369b489f87341f22a1e57563423d5729a55701f855fcc659a3",
	"schemas/v1/repos.schema.json":                  "05b1ef5a78cb3b3dbc87a32c720188c0fd46f574cd742d4df9f5cad6d09c6182",
	"schemas/v1/restack-plan.schema.json":           "da0bdc60273a30a0f2e39482f534cb80f15f619451f2167ad0780cc44a2b5b48",
	"schemas/v1/status.schema.json":                 "529ba104508e6cffce1eb07dd6ff7325059510ce0eab58b48d5e956c4ce0ee8d",
	"schemas/v1/sync.schema.json":                   "96b2106f6721a2d2cf4edc5271c78841ed145decfeb31972cd78660a9080083d",
	"schemas/v1/tidy.schema.json":                   "7b32f169e81b5351c9b542af7b09025a068279de98f3441ec8966d1239ec1161",
	"schemas/v1/version.schema.json":                "886864837547ded4992af89e8e8e04d38fe7bf77724f91a8960a184c51a0659f",
	"schemas/v1/watch-event.schema.json":            "3dbea5a7e3c720d9d8f1ca528f1aa73d423502251efb34fe58aa2ff295fa77f3",
	"schemas/v1/workspace.schema.json":              "fb4d4623c7b3f329e596e7a3608116d10d3bb3c6801aaece6a073e154ccfd825",
	"schemas/v2/hosting-pr.schema.json":             "2baef8835871144bcfeac3e3bbc15fa6ead88c9b30bce9949b19ef7f8d6c3756",
	"schemas/v2/hosting-prs.schema.json":            "a4d2a263bc8470fe78757c639b359fe4a30a52e0a0638c67e747a6d8eb696047",
	"schemas/v2/inspect.schema.json":                "3824dbf58ee99eaa184218c292a8eff0e7c7ee1b7f455a77d9c1977f1478f595",
	"schemas/v2/status.schema.json":                 "d661a641a3c956ff7ca91ca0e5c0cd88d48e5bffae8a6d8bd5cad455efae183c",
	"schemas/v2/tidy-overview.schema.json":          "420b10a4b155d6f2b8a7cabbeb01b659c957b9431206ec4b5d6039d8e4ad5f0e",
	"schemas/v2/tidy.schema.json":                   "c964a85de0941f41ceef84b6cc57235de984c7084ea1b4bacc8ebd799de19f2d",
}

func TestPublishedV1AndV2SchemasAreByteFrozen(t *testing.T) {
	for _, dir := range []string{"v1", "v2"} {
		files, e := filepath.Glob(filepath.Join("..", "..", "schemas", dir, "*.json"))
		if e != nil || len(files) == 0 {
			t.Fatalf("schemas/%s: %v", dir, e)
		}
		for _, f := range files {
			rel := "schemas/" + dir + "/" + filepath.Base(f)
			b, e := os.ReadFile(f)
			if e != nil {
				t.Fatal(e)
			}
			sum := sha256.Sum256(b)
			if want, ok := frozenPublishedSchemas[rel]; !ok || hex.EncodeToString(sum[:]) != want {
				t.Fatalf("%s changed or is not frozen (digest %x)", rel, sum)
			}
		}
	}
	if len(frozenPublishedSchemas) != 30 {
		t.Fatalf("frozen schema inventory has %d entries, want 30", len(frozenPublishedSchemas))
	}
}
