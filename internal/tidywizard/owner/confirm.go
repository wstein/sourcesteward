// SPDX-License-Identifier: EUPL-1.2

package owner

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Typed confirmation errors.
var (
	// ErrNoTerminal means there is no controlling terminal; the confirmation fails closed.
	ErrNoTerminal = errors.New("no controlling terminal for the confirmation")
	// ErrConfirmationRefused means the typed words were not the expected ones.
	ErrConfirmationRefused = errors.New("confirmation refused")
)

// Opener opens the terminal the confirmation is read from.
type Opener func() (io.ReadWriteCloser, error)

// DevTTY opens the controlling terminal /dev/tty, never stdin or stdout. A failure is ErrNoTerminal.
func DevTTY() (io.ReadWriteCloser, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoTerminal, err)
	}
	return f, nil
}

const prefixLen = 6

// maxAnswer bounds the line read from the terminal.
const maxAnswer = 256

// unknownWord is the confirmation word for every item of scope unknown, and for an owner label
// that cannot be typed as one word.
const unknownWord = "unknown"

// ConfirmWord returns the word typed before the plan-id prefix: the owner label for scope other,
// the literal word "unknown" for scope unknown and for a label that is empty or not one word.
func ConfirmWord(o Owner) string {
	if o.Scope != ScopeOther || !oneWord(o.Label) {
		return unknownWord
	}
	return o.Label
}

func oneWord(s string) bool {
	return s != "" && !strings.ContainsAny(s, " \t\r\n")
}

// Confirm asks on the terminal for the confirmation word (see ConfirmWord) followed by the first 6 characters of the plan
// id (for example "codex 3f9c2a"). A bare y, anything else, or no terminal refuses; stdin is never
// read. The caller asks once per confirmation word.
func Confirm(open Opener, word, planID string) error {
	if !oneWord(word) {
		return fmt.Errorf("confirmation word %q cannot be typed as one word", word)
	}
	if len(planID) < prefixLen {
		return fmt.Errorf("plan id %q is shorter than %d characters", planID, prefixLen)
	}
	want := word + " " + planID[:prefixLen]
	t, err := open()
	if err != nil {
		if errors.Is(err, ErrNoTerminal) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrNoTerminal, err)
	}
	defer t.Close()
	if _, err = fmt.Fprintf(t, "Foreign items (%s). Type %q to confirm: ", word, want); err != nil {
		return fmt.Errorf("%w: %v", ErrNoTerminal, err)
	}
	line, tooLong, err := ReadAnswer(bufio.NewReader(t), maxAnswer)
	if errors.Is(err, io.EOF) {
		return ErrConfirmationRefused // an answer cut off by the end of input is no answer
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoTerminal, err)
	}
	if tooLong {
		return ErrConfirmationRefused // an over-long line is refused, not truncated
	}
	if strings.TrimSpace(line) != want {
		return ErrConfirmationRefused
	}
	return nil
}

// YesHonoured reports whether --yes may stand for the confirmation: never for a plan with an
// other or unknown item.
func YesHonoured(scopes []Scope) bool {
	for _, s := range scopes {
		if s == ScopeOther || s == ScopeUnknown {
			return false
		}
	}
	return true
}
