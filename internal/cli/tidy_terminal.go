// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bufio"
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
		return nil, fmt.Errorf("%w; nothing was stored or applied. Run `stw tidy plan --save`, then apply from a terminal", owner.ErrNoTerminal)
	}
	return terminal, nil
}

// tidyTerminalAvailable runs before Build, which can write sub-plans even before it asks a word.
func tidyTerminalAvailable() error {
	terminal, err := openTidyTerminal()
	if err != nil {
		return err
	}
	return terminal.Close()
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
