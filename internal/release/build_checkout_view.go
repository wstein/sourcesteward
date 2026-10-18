// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
)

// Native config output is semantic data, never a diagnostic. Keep full streams
// on private disk and refer to records by offsets, including arbitrarily long
// subsection names and values. The paired --name-only stream disambiguates keys
// containing LF from the LF separating a key and its value.
type buildViewField struct{ offset, length int64 }
type buildViewFrames struct {
	file   *os.File
	reader *bufio.Reader
	offset int64
}

func newBuildViewFrames(f *os.File) *buildViewFrames {
	return &buildViewFrames{file: f, reader: bufio.NewReader(io.NewSectionReader(f, 0, 1<<63-1))}
}
func (r *buildViewFrames) next() (buildViewField, error) {
	f := buildViewField{offset: r.offset}
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			if err == io.EOF && f.length != 0 {
				return f, io.ErrUnexpectedEOF
			}
			return f, err
		}
		r.offset++
		if b == 0 {
			return f, nil
		}
		f.length++
	}
}
func (f buildViewField) reader(file *os.File) *io.SectionReader {
	return io.NewSectionReader(file, f.offset, f.length)
}
func equalBuildViewFields(a *os.File, x buildViewField, b *os.File, y buildViewField) bool {
	if x.length != y.length {
		return false
	}
	ar, br := x.reader(a), y.reader(b)
	ab, bb := make([]byte, 16384), make([]byte, 16384)
	for left := x.length; left > 0; {
		n := int64(len(ab))
		if left < n {
			n = left
		}
		if _, err := io.ReadFull(ar, ab[:n]); err != nil {
			return false
		}
		if _, err := io.ReadFull(br, bb[:n]); err != nil {
			return false
		}
		if !bytes.Equal(ab[:n], bb[:n]) {
			return false
		}
		left -= n
	}
	return true
}
func buildViewFieldIs(f *os.File, x buildViewField, s string) bool {
	if x.length != int64(len(s)) {
		return false
	}
	b := make([]byte, len(s))
	_, err := f.ReadAt(b, x.offset)
	return err == nil && string(b) == s
}

func renderBuildViewConfig(names, values *os.File, local, worktree io.Writer) error {
	n, v := newBuildViewFrames(names), newBuildViewFrames(values)
	invalid := errors.New("native configuration captures disagree or are malformed")
	for {
		scope, err := n.next()
		if err == io.EOF {
			_, err = v.next()
			if err == io.EOF {
				return nil
			}
			return invalid
		}
		if err != nil {
			return invalid
		}
		origin, err := n.next()
		if err != nil {
			return invalid
		}
		key, err := n.next()
		if err != nil {
			return invalid
		}
		vs, err := v.next()
		if err != nil || !equalBuildViewFields(names, scope, values, vs) {
			return invalid
		}
		vo, err := v.next()
		if err != nil || !equalBuildViewFields(names, origin, values, vo) {
			return invalid
		}
		record, err := v.next()
		if err != nil || record.length < key.length {
			return invalid
		}
		prefix := buildViewField{record.offset, key.length}
		if !equalBuildViewFields(names, key, values, prefix) {
			return invalid
		}
		hasValue := record.length > key.length
		value := buildViewField{offset: record.offset + key.length, length: record.length - key.length}
		if hasValue {
			var sep [1]byte
			if _, err := values.ReadAt(sep[:], value.offset); err != nil || sep[0] != '\n' {
				return invalid
			}
			value.offset++
			value.length--
		}
		var out io.Writer
		switch {
		case buildViewFieldIs(names, scope, "local"):
			out = local
		case buildViewFieldIs(names, scope, "worktree"):
			out = worktree
		case buildViewFieldIs(names, scope, "command"):
			continue
		default:
			return invalid
		}
		// Locate section and variable without retaining a possibly large subsection.
		first, last := int64(-1), int64(-1)
		r := bufio.NewReader(key.reader(names))
		for i := int64(0); i < key.length; i++ {
			b, e := r.ReadByte()
			if e != nil {
				return invalid
			}
			if b == '.' {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first <= 0 || last == key.length-1 {
			return invalid
		}
		section := buildViewField{key.offset, first}
		variable := buildViewField{key.offset + last + 1, key.length - last - 1}
		if buildViewFieldIs(names, section, "include") && buildViewFieldIs(names, variable, "path") {
			continue
		}
		if buildViewFieldIs(names, section, "includeif") && buildViewFieldIs(names, variable, "path") {
			continue
		}
		// Git config keys are normalized by native LIST; no locale-dependent match.
		if buildViewFieldIs(names, section, "filter") && (buildViewFieldIs(names, variable, "clean") || buildViewFieldIs(names, variable, "process")) && hasValue && value.length != 0 {
			return errors.New("configured checkout conversion helper requires explicit project execution")
		}
		if err := writeBuildViewConfigRecord(out, names, section, variable, first, last, key, values, value, hasValue); err != nil {
			return err
		}
	}
}
func writeBuildViewConfigRecord(out io.Writer, names *os.File, section, variable buildViewField, first, last int64, key buildViewField, values *os.File, value buildViewField, has bool) error {
	var failure error
	write := func(s string) {
		if failure == nil {
			_, failure = io.WriteString(out, s)
		}
	}
	copyField := func(file *os.File, f buildViewField) {
		if failure == nil {
			_, failure = io.Copy(out, f.reader(file))
		}
	}
	write("[")
	copyField(names, section)
	if first != last {
		write(" \"")
		if failure == nil {
			failure = escapeBuildViewConfig(out, buildViewField{key.offset + first + 1, last - first - 1}.reader(names), false)
		}
		write("\"")
	}
	write("]\n")
	copyField(names, variable)
	if has {
		write(" = \"")
		if failure == nil {
			failure = escapeBuildViewConfig(out, value.reader(values), true)
		}
		write("\"")
	}
	write("\n")
	return failure
}
func escapeBuildViewConfig(out io.Writer, in io.Reader, value bool) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	for {
		b, err := r.ReadByte()
		if err == io.EOF {
			return w.Flush()
		}
		if err != nil {
			return err
		}
		var text string
		switch b {
		case '\\':
			text = `\\`
		case '"':
			text = `\"`
		case '\n':
			if value {
				text = `\n`
			}
		case '\t':
			if value {
				text = `\t`
			}
		case '\b':
			if value {
				text = `\b`
			}
		}
		if text != "" {
			_, err = w.WriteString(text)
		} else {
			err = w.WriteByte(b)
		}
		if err != nil {
			return err
		}
	}
}
