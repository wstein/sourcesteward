// SPDX-License-Identifier: EUPL-1.2

//go:build unix

package owner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

const helperEnv = "STW_OWNER_TEST_NO_CTTY"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) != "" {
		err := Confirm(DevTTY, "codex", "3f9c2a7e41b8")
		if errors.Is(err, ErrNoTerminal) {
			os.Exit(0)
		}
		os.Exit(3)
	}
	os.Exit(m.Run())
}

// A session without a controlling terminal cannot open /dev/tty: the default opener fails closed.
func TestDevTTYFailsClosedWithoutControllingTerminal(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v\n%s", err, out)
	}
}
