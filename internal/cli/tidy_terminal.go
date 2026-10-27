// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/prompt"
)

// openTidyTerminal is shared by every human store and apply question. The opener is also the
// seam used for typed words, so a run never substitutes stdin for the controlling terminal.
func openTidyTerminal() (io.ReadWriteCloser, error) {
	var terminal io.ReadWriteCloser
	err := owner.ErrNoTerminal
	if tidyConfirmOpener != nil {
		terminal, err = tidyConfirmOpener()
	}
	if err != nil {
		return nil, owner.ErrNoTerminal
	}
	return terminal, nil
}

// tidyTerminalAvailable runs before Build, which can write sub-plans even before it asks a word.
// Nothing is stored yet, so the way on is to store a plan set where no terminal is needed.
func tidyTerminalAvailable() error {
	terminal, err := openTidyTerminal()
	if err != nil {
		return fmt.Errorf("%w. Run `stw tidy plan --save`, then apply from a terminal; nothing was stored or applied", err)
	}
	return terminal.Close()
}

// storedNeedsTerminal words a failure at a question that comes after the plan set is stored: the
// plan exists, so the way on is to apply it from a terminal, not to save a plan again.
func storedNeedsTerminal(err error, planID, store string) error {
	if !errors.Is(err, owner.ErrNoTerminal) {
		return err
	}
	command := "stw tidy apply " + planID
	if store != "" {
		command += " --state-dir " + shellWord(store)
	}
	return fmt.Errorf("%w; the plan set %s is stored and nothing was applied; apply it from a terminal with `%s`", owner.ErrNoTerminal, planID, command)
}

func tidyTerminalQuestion(question string, quit bool) (bool, error) {
	terminal, err := openTidyTerminal()
	if err != nil {
		return false, err
	}
	defer terminal.Close()
	in := bufio.NewReader(tidySelectionReader{terminal})
	if quit {
		return prompt.YesNoQuit(in, terminal, question, true)
	}
	return prompt.YesNo(in, terminal, question, true)
}

// tidySelectionReader prevents bufio read-ahead across the selection/authorization boundary.
// Later answers stay in their source stream until the question that actually needs them.
type tidySelectionReader struct{ io.Reader }

func (r tidySelectionReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}
