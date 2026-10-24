// SPDX-License-Identifier: EUPL-1.2
// Package attribcmd derives the attribution of the current run from the process environment and
// applies the remote-write gate. It is the only place the CLI layer reads the environment for it.
package attribcmd

import (
	"os"
	"time"

	"github.com/mattn/go-isatty"
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
	return originForStreams(os.Environ(), os.Stdin, os.Stdout, time.Now())
}

// originForStreams reads the terminal facts of the two streams (isatty, so the null device is no
// terminal) and derives the origin.
func originForStreams(environ []string, stdin, stdout any, now time.Time) attribution.Origin {
	return OriginForRun(environ, isTerminal(stdin), isTerminal(stdout), now)
}

// isTerminal is isatty on a value with a file descriptor; everything else is no terminal.
func isTerminal(v any) bool {
	f, ok := v.(interface{ Fd() uintptr })
	return ok && isatty.IsTerminal(f.Fd())
}

// RemoteWriteGate refuses a remote write (a push, setting a PR ready) for a task whose creating
// agent session differs from the current one. No command calls it yet (Spec 042 T004), so nothing is
// enforced today; every command that writes remotely for a task must call it first. It is attribution for honest mix-ups, not a security boundary. A human run is always
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
