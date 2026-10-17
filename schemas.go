// SPDX-License-Identifier: EUPL-1.2

// Package sourcesteward carries the published JSON Schemas that commands validate against at
// run time, embedded from the same files that are published on the project site.
package sourcesteward

import _ "embed"

// ReleaseConfigSchema is schemas/v1/release-config.schema.json, the source of truth of the
// release contract model (specs/026-release-process/contracts/config.md).
//
//go:embed schemas/v1/release-config.schema.json
var ReleaseConfigSchema []byte
