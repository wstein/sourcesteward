// SPDX-License-Identifier: EUPL-1.2
package readycheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

func params(f *fakeForge, out *bytes.Buffer, human bool) Params {
	return Params{Input: Input{Repo: "o/r", Number: 7, Host: "github.com"}, Human: human, Out: out,
		Now:    func() time.Time { return t0 },
		Review: clearFor(head),
		Open:   func(context.Context, string, string) (Source, func(), error) { return f, func() {}, nil }}
}

func TestExecuteReadyPrintsJSONAndExitsZero(t *testing.T) {
	var out bytes.Buffer
	if err := Execute(context.Background(), params(newForge(), &out, false)); err != nil {
		t.Fatal(err)
	}
	var d Document
	if err := json.Unmarshal(out.Bytes(), &d); err != nil || !d.Ready || len(d.Conditions) != 7 {
		t.Fatalf("%v %s", err, out.String())
	}
}

func TestExecuteNotReadyPrintsTheDocumentAndExitsOne(t *testing.T) {
	f := newForge()
	f.pull = pullBody(true, "main", "Blocker: Independent review pending", head)
	var out bytes.Buffer
	err := Execute(context.Background(), params(f, &out, false))
	var fl *watchcmd.Failure
	if !errors.As(err, &fl) || fl.Code != watchcmd.ExitFailure || !strings.Contains(err.Error(), "blocker") {
		t.Fatalf("err = %v", err)
	}
	var d Document
	if json.Unmarshal(out.Bytes(), &d) != nil || d.Ready || status(d, CondBlocker) != StatusFail {
		t.Fatalf("document must still be printed: %s", out.String())
	}
}

func TestExecuteHumanListsEveryConditionWithAnAnswer(t *testing.T) {
	f := newForge()
	f.pull = pullBody(true, "feat/x", "Blocker: none", head)
	var out bytes.Buffer
	_ = Execute(context.Background(), params(f, &out, true))
	s := out.String()
	for _, want := range []string{"o/r#7", "Ready: no", "review", "pass", "base", "fail", "blocker", "rebaseable", "draft"} {
		if !strings.Contains(s, want) {
			t.Errorf("human output lacks %q:\n%s", want, s)
		}
	}
	out.Reset()
	_ = Execute(context.Background(), params(newForge(), &out, true))
	if !strings.Contains(out.String(), "Ready: yes") {
		t.Errorf("%s", out.String())
	}
}

func TestExecuteOpenFailureIsExitOneWithoutOutput(t *testing.T) {
	var out bytes.Buffer
	p := params(newForge(), &out, false)
	p.Open = func(context.Context, string, string) (Source, func(), error) {
		return nil, func() {}, errors.New("no gh")
	}
	err := Execute(context.Background(), p)
	var fl *watchcmd.Failure
	if !errors.As(err, &fl) || fl.Code != watchcmd.ExitFailure || out.Len() != 0 {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestPrepareValidates(t *testing.T) {
	cases := []struct {
		name string
		f    Flags
		args []string
	}{
		{"no number", Flags{Repo: "o/r"}, nil},
		{"zero", Flags{Repo: "o/r"}, []string{"0"}},
		{"not a number", Flags{Repo: "o/r"}, []string{"x1"}},
		{"leading plus", Flags{Repo: "o/r"}, []string{"+3"}},
		{"bad repo", Flags{Repo: "o"}, []string{"3"}},
		{"bad host", Flags{Repo: "o/r", Host: "a b"}, []string{"3"}},
		{"both modes", Flags{Repo: "o/r", Human: true, JSON: true}, []string{"3"}},
	}
	for _, tc := range cases {
		_, err := prepare(context.Background(), tc.f, tc.args, &bytes.Buffer{}, nil)
		if !errors.Is(err, watchcmd.ErrUsage) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	p, err := prepare(context.Background(), Flags{Repo: "o/r"}, []string{"12"}, &bytes.Buffer{}, nil)
	if err != nil || p.Input.Repo != "o/r" || p.Input.Number != 12 || p.Input.Host != watchcmd.DefaultHost {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestPrepareWithoutRepoNeedsExactlyOneWorkspaceRepository(t *testing.T) {
	one := func(context.Context, string) (stwsync.Workspace, error) {
		return stwsync.Workspace{Root: "/w", Members: []stwsync.Member{{Name: "a", Initialized: true, Remote: "https://github.com/o/a.git"}}}, nil
	}
	p, err := prepare(context.Background(), Flags{}, []string{"3"}, &bytes.Buffer{}, one)
	if err != nil || p.Input.Repo != "o/a" {
		t.Fatalf("%+v %v", p, err)
	}
	two := func(context.Context, string) (stwsync.Workspace, error) {
		return stwsync.Workspace{Root: "/w", Members: []stwsync.Member{
			{Name: "a", Initialized: true, Remote: "https://github.com/o/a.git"}, {Name: "b", Initialized: true, Remote: "https://github.com/o/b.git"}}}, nil
	}
	if _, err := prepare(context.Background(), Flags{}, []string{"3"}, &bytes.Buffer{}, two); !errors.Is(err, watchcmd.ErrUsage) {
		t.Fatalf("%v", err)
	}
}

func TestDefaultReviewSourceNeverPasses(t *testing.T) {
	if _, err := ReviewUnavailable(context.Background(), "o/r", 1, head); err == nil {
		t.Fatal("the placeholder must fail")
	}
}
