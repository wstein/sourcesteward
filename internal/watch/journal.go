// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const journalName = "journal.jsonl"

// journal is the append-only record of every emitted event: JSON Lines, events only, fsynced per line.
// The next seq is the seq of the last complete line plus one, so it is gapless and survives a lost state.json.
type journal struct {
	path string
	f    *os.File
	tail int64
	good int64 // length of the complete-line prefix; a longer file has a truncated tail to heal
}

func openJournal(dir string) (*journal, error) {
	path := filepath.Join(dir, journalName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	j := &journal{path: path, f: f}
	events, good, err := scanJournal(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	j.good = good
	if n := len(events); n > 0 {
		j.tail = events[n-1].Seq
	}
	return j, nil
}

// scanJournal reads the complete lines. A last line without a newline, or that does not decode, is a
// truncated write: it is ignored. Any other undecodable line, or a seq that is not previous+1, is corruption.
func scanJournal(r io.ReaderAt) ([]Event, int64, error) {
	raw, err := io.ReadAll(io.NewSectionReader(r, 0, 1<<62))
	if err != nil {
		return nil, 0, err
	}
	var events []Event
	var good int64
	for len(raw) > 0 {
		i := bytes.IndexByte(raw, '\n')
		if i < 0 {
			break // unterminated tail
		}
		var e Event
		if err := json.Unmarshal(raw[:i], &e); err != nil {
			return nil, 0, fmt.Errorf("journal line %d does not validate: %w", len(events)+1, err)
		}
		if want := int64(len(events)) + 1; e.Seq != want {
			return nil, 0, fmt.Errorf("journal seq %d where %d was expected", e.Seq, want)
		}
		events = append(events, e)
		good += int64(i) + 1
		raw = raw[i+1:]
	}
	return events, good, nil
}

// Tail is the seq of the last complete line (0 for an empty journal).
func (j *journal) Tail() int64 { return j.tail }

// Append assigns the next seq, writes the line and fsyncs before returning. An unterminated truncated tail
// left by an earlier crash is cut off first, so every line of the file validates.
func (j *journal) Append(e Event) (Event, error) {
	st, err := j.f.Stat()
	if err != nil {
		return e, err
	}
	if st.Size() != j.good {
		if err := j.f.Truncate(j.good); err != nil {
			return e, err
		}
	}
	e.SchemaVersion = 1
	e.Seq = j.tail + 1
	line, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	line = append(line, '\n')
	if _, err := j.f.WriteAt(line, j.good); err != nil {
		return e, err
	}
	if err := j.f.Sync(); err != nil {
		return e, err
	}
	j.good += int64(len(line))
	j.tail = e.Seq
	return e, nil
}

// After returns the complete events with seq greater than seq, in order.
func (j *journal) After(seq int64) ([]Event, error) {
	events, _, err := scanJournal(j.f)
	if err != nil {
		return nil, err
	}
	if seq >= int64(len(events)) {
		return nil, nil
	}
	if seq < 0 {
		seq = 0
	}
	return events[seq:], nil
}

func (j *journal) Close() error { return j.f.Close() }
