//go:build !unix

// SPDX-License-Identifier: EUPL-1.2
package watchcmd

// stw watch does not run here (watch.Supported is false); the flags only keep the package compiling.
const (
	openNonBlock = 0
	openNoFollow = 0
)
