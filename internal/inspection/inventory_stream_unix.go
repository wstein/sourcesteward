// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// prepareStreamCommand puts the command in its own process group and makes cancellation kill the whole
// group, so a wrapper that forks does not leave an orphan holding the output pipe.
func prepareStreamCommand(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		if err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		} else if err != nil {
			return os.ErrProcessDone
		}
		return nil
	}
}
