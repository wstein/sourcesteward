// SPDX-License-Identifier: EUPL-1.2

package owner

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadAnswerLimitBoundary(t *testing.T) {
	for _, tc := range []struct {
		n       int
		eol     string
		tooLong bool
	}{{255, "\n", false}, {256, "\n", false}, {257, "\n", true}, {256, "\r\n", false}, {257, "\r\n", true}} {
		line, tooLong, err := ReadAnswer(bufio.NewReader(strings.NewReader(strings.Repeat("x", tc.n)+tc.eol)), 256)
		if err != nil || tooLong != tc.tooLong || (!tooLong && line != strings.Repeat("x", tc.n)) {
			t.Errorf("%d bytes + %q: len=%d tooLong=%v err=%v", tc.n, tc.eol, len(line), tooLong, err)
		}
	}
}

func TestReadAnswerDrainsAnOverLongLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(strings.Repeat("x", 5000) + "y\n"))
	if _, tooLong, err := ReadAnswer(r, 256); err != nil || !tooLong {
		t.Fatalf("tooLong=%v err=%v", tooLong, err)
	}
	if line, _, err := ReadAnswer(r, 256); !errors.Is(err, io.EOF) || line != "" {
		t.Fatalf("the tail of the long line was read as an answer: %q %v", line, err)
	}
}

func TestReadAnswerNewlineAndEOF(t *testing.T) {
	for in, want := range map[string]string{"y\n": "y", "y\r\n": "y", "\n": "", "y\r\r\n": "y\r"} {
		if got, _, err := ReadAnswer(bufio.NewReader(strings.NewReader(in)), 256); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "y", "y\r"} {
		if got, _, err := ReadAnswer(bufio.NewReader(strings.NewReader(in)), 256); !errors.Is(err, io.EOF) || got != "" {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
}
