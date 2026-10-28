// SPDX-License-Identifier: EUPL-1.2

package owner

import (
	"bufio"
	"bytes"
	"errors"
)

// ReadAnswer reads one terminated answer line from a terminal. The line ends at its newline,
// which is removed together with a carriage return in front of it. The end of input before the
// newline is io.EOF, whatever was typed before it: an answer cut off by EOF (for example "y"
// without a newline) is no answer and never consent (Issue 326). Any other read error is
// returned as it is. At most limit bytes of the line are kept; tooLong reports that more were
// typed, and the rest of the line is drained.
func ReadAnswer(in *bufio.Reader, limit int) (line string, tooLong bool, err error) {
	var kept []byte
	for {
		part, err := in.ReadSlice('\n')
		if err == nil {
			part = bytes.TrimSuffix(bytes.TrimSuffix(part, []byte("\n")), []byte("\r"))
		} else if !errors.Is(err, bufio.ErrBufferFull) {
			return "", tooLong, err
		}
		if room := limit - len(kept); len(part) > room {
			tooLong = true
			part = part[:room]
		}
		kept = append(kept, part...)
		if err == nil {
			return string(kept), tooLong, nil
		}
	}
}
