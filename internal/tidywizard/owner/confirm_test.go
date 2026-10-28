// SPDX-License-Identifier: EUPL-1.2

package owner

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

type fakeTTY struct {
	in  *strings.Reader
	out strings.Builder
}

func (f *fakeTTY) Read(p []byte) (int, error)  { return f.in.Read(p) }
func (f *fakeTTY) Write(p []byte) (int, error) { return f.out.Write(p) }
func (f *fakeTTY) Close() error                { return nil }

func tty(input string) (Opener, *fakeTTY) {
	f := &fakeTTY{in: strings.NewReader(input)}
	return func() (io.ReadWriteCloser, error) { return f, nil }, f
}

const planID = "3f9c2a7e41b8"

func TestConfirmAcceptsLabelAndPrefix(t *testing.T) {
	open, f := tty("codex 3f9c2a\n")
	if err := Confirm(open, "codex", planID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "codex 3f9c2a") {
		t.Fatalf("prompt does not name the expected words: %q", f.out.String())
	}
	if strings.Contains(f.out.String(), planID) {
		t.Fatalf("prompt prints more than the 6-character prefix: %q", f.out.String())
	}
}

func TestConfirmRefusesAnythingElse(t *testing.T) {
	for _, in := range []string{"y\n", "Y\n", "yes\n", "\n", "", "codex\n", "3f9c2a\n", "codex 3f9c2b\n", "claude 3f9c2a\n", "codex  3f9c2a x\n", "codex 3f9c2a7e41b8\n", "3f9c2a codex\n", "CODEX 3F9C2A\n", "codex 3F9C2A\n", "codex 3f9c2a" + strings.Repeat(" ", 300) + "\n"} {
		open, _ := tty(in)
		if err := Confirm(open, "codex", planID); !errors.Is(err, ErrConfirmationRefused) {
			t.Errorf("input %q: err = %v, want ErrConfirmationRefused", in, err)
		}
	}
}

func TestConfirmNeverReadsStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = w.WriteString("codex 3f9c2a\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	open, _ := tty("")
	if err = Confirm(open, "codex", planID); !errors.Is(err, ErrConfirmationRefused) {
		t.Fatalf("stdin answered the confirmation: %v", err)
	}
	rest, _ := io.ReadAll(r)
	if string(rest) != "codex 3f9c2a\n" {
		t.Fatalf("stdin was consumed: %q", rest)
	}
}

func TestConfirmFailsClosedWithoutTerminal(t *testing.T) {
	open := func() (io.ReadWriteCloser, error) { return nil, errors.New("no such device") }
	if err := Confirm(open, "codex", planID); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("err = %v, want ErrNoTerminal", err)
	}
}

func TestConfirmRejectsUnusableInputs(t *testing.T) {
	open, _ := tty("codex 3f9c2a\n")
	openSpaced, _ := tty("co dex 3f9c2a\n")
	if err := Confirm(open, "", planID); err == nil {
		t.Fatal("empty label accepted")
	}
	if err := Confirm(open, "codex", "3f9c2"); err == nil {
		t.Fatal("plan id shorter than 6 characters accepted")
	}
	if err := Confirm(openSpaced, "co dex", planID); err == nil || errors.Is(err, ErrConfirmationRefused) {
		t.Fatalf("label with a space must be refused before asking, got %v", err)
	}
}

func TestYesIsNotHonouredForForeignItems(t *testing.T) {
	cases := []struct {
		scopes []Scope
		want   bool
	}{
		{nil, true},
		{[]Scope{ScopeOwn}, true},
		{[]Scope{ScopeOwn, ScopeOther}, false},
		{[]Scope{ScopeUnknown}, false},
		{[]Scope{ScopeOther, ScopeUnknown}, false},
	}
	for _, c := range cases {
		if got := YesHonoured(c.scopes); got != c.want {
			t.Errorf("YesHonoured(%v) = %v, want %v", c.scopes, got, c.want)
		}
	}
}

func TestConfirmNoTerminalWrappedErrorFailsClosed(t *testing.T) {
	open := func() (io.ReadWriteCloser, error) { return nil, fmt.Errorf("%w: x", ErrNoTerminal) }
	if err := Confirm(open, "codex", planID); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("err = %v, want ErrNoTerminal", err)
	}
}

func TestConfirmWord(t *testing.T) {
	cases := []struct {
		o    Owner
		want string
	}{
		{Owner{"codex", SourcePathConvention, ScopeOther}, "codex"},
		{Owner{"", SourceUnknown, ScopeUnknown}, "unknown"},
		{Owner{"author:Werner Stein", SourceCommitAuthor, ScopeUnknown}, "unknown"},
		{Owner{"codex", SourceTaskRecord, ScopeUnknown}, "unknown"},
		{Owner{"my agent", SourceTaskRecord, ScopeOther}, "unknown"},
		{Owner{"", SourceTaskRecord, ScopeOther}, "unknown"},
		{Owner{"unknown", SourceTaskRecord, ScopeOther}, "unknown"},
	}
	for _, c := range cases {
		if got := ConfirmWord(c.o); got != c.want {
			t.Errorf("ConfirmWord(%+v) = %q, want %q", c.o, got, c.want)
		}
	}
}

func TestUnknownItemsAreConfirmable(t *testing.T) {
	for _, it := range []Item{{}, {AuthorHint: "Werner Stein"}} {
		o := Resolve(it, "claude", false)
		open, _ := tty("unknown 3f9c2a\n")
		if err := Confirm(open, ConfirmWord(o), planID); err != nil {
			t.Errorf("item %+v: %v", it, err)
		}
	}
}

// A confirmation cut off by the end of input is refused, also when the words are right.
func TestConfirmWithoutNewlineIsRefused(t *testing.T) {
	for _, in := range []string{"codex 3f9c2a", "codex 3f9c2a\r"} {
		open, _ := tty(in)
		if err := Confirm(open, "codex", planID); !errors.Is(err, ErrConfirmationRefused) {
			t.Errorf("input %q: err = %v, want ErrConfirmationRefused", in, err)
		}
	}
	open, _ := tty("codex 3f9c2a\r\n")
	if err := Confirm(open, "codex", planID); err != nil {
		t.Errorf("CRLF still confirms: %v", err)
	}
}

type errTTY struct {
	fakeTTY
	data string
}

func (e *errTTY) Read(p []byte) (int, error) {
	if e.data == "" {
		return 0, errors.New("i/o failure")
	}
	n := copy(p, e.data)
	e.data = e.data[n:]
	return n, nil
}

func TestConfirmReadErrorAfterTheWordsIsNoConfirmation(t *testing.T) {
	f := &errTTY{data: "codex 3f9c2a"}
	open := func() (io.ReadWriteCloser, error) { return f, nil }
	if err := Confirm(open, "codex", planID); err == nil {
		t.Fatal("confirmed")
	}
}
