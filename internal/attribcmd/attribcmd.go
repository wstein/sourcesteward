// SPDX-License-Identifier: EUPL-1.2
// Package attribcmd derives the attribution of the current run from the process environment and
// applies the remote-write gate. It is the only place the CLI layer reads the environment for it.
package attribcmd

import (
	"os"
	"time"

	"github.com/wstein/sourcesteward/internal/attribution"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// OriginForRun derives the attribution of the current run from the existing agent detection. Only
// the marker names and one session variable are read; no other variable value is used.
func OriginForRun(environ []string, stdinTTY, stdoutTTY bool, now time.Time) attribution.Origin {
	evidence := owner.Detect(owner.Env{StdinTTY: stdinTTY, StdoutTTY: stdoutTTY, Environ: environ})
	return attribution.FromEnviron(environ, evidence.Markers, evidence.Mode == owner.ModeHuman, now)
}

// Current is the origin of this process.
func Current() attribution.Origin {
	return OriginForRun(os.Environ(), isCharDevice(os.Stdin), isCharDevice(os.Stdout), time.Now())
}

func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// RemoteWriteGate refuses a remote write (a push, setting a PR ready) for a task whose creating
// agent session differs from the current one. Every command that writes remotely for a task calls
// it first. It is attribution for honest mix-ups, not a security boundary. A human run is always
// allowed; a task without a record or without a recorded session is not gated; an unreadable
// record refuses an agent run. The caller wraps the typed refusal as a CLI runtime failure, exit status 1 like the other typed refusals.
func RemoteWriteGate(read func(stateDir, task string) (workspace.TaskRecord, bool, error), stateDir, task string, current attribution.Origin) error {
	if current.Kind == attribution.KindHuman {
		return nil
	}
	record, found, err := read(stateDir, task)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err = attribution.Gate(record.Origin, current); err != nil {
		return err
	}
	return nil
}
