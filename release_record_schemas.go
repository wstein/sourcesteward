// SPDX-License-Identifier: EUPL-1.2
package sourcesteward

import _ "embed"

// ReleaseRecordSchema is the unpublished version 1 record contract.
//
//go:embed schemas/v1/release-record.schema.json
var ReleaseRecordSchema []byte

// ReleaseBuildManifestSchema is the unpublished version 1 manifest contract.
//
//go:embed schemas/v1/release-build-manifest.schema.json
var ReleaseBuildManifestSchema []byte

// ReleaseOCIResultSchema is the unpublished version 1 OCI result contract.
//
//go:embed schemas/v1/release-oci-result.schema.json
var ReleaseOCIResultSchema []byte
