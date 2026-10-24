// SPDX-License-Identifier: EUPL-1.2

package owner

import "github.com/mattn/go-isatty"

// IsTerminal reports whether v is a terminal: it must expose a file descriptor (an *os.File) and
// that descriptor must be a terminal by isatty. A buffer, a pipe, a regular file and a character
// device that is no terminal (the null device) are not, and neither is a closed or invalid
// descriptor, so every doubt reads as "no terminal" and mode detection stays on the agent side.
func IsTerminal(v any) bool {
	f, ok := v.(interface{ Fd() uintptr })
	if !ok || f == nil {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

// TerminalFacts reports whether the command's input and output are terminals, the two facts
// [Detect] reads. Each stream is judged on its own; neither is ever inferred from the other.
func TerminalFacts(stdin, stdout any) (stdinTTY, stdoutTTY bool) {
	return IsTerminal(stdin), IsTerminal(stdout)
}
