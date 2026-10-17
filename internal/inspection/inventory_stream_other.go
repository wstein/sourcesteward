// SPDX-License-Identifier: EUPL-1.2
//go:build !(linux || darwin)

package inspection

import "os/exec"

// prepareStreamCommand keeps the default single-process cancellation where process groups are unavailable.
func prepareStreamCommand(c *exec.Cmd) {}
