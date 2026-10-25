// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) read() time.Time { return c.now }

func newClock() *fakeClock { return &fakeClock{now: fixtureNow} }

func wantBound(t *testing.T, err error, reason Reason, bound string) {
	t.Helper()
	var b *BoundError
	if !errors.As(err, &b) || b.Reason != reason || (bound != "" && b.Bound != bound) {
		t.Fatalf("want bound error %s/%s, got %v", reason, bound, err)
	}
}

func TestDocumentedLimitsArePinned(t *testing.T) {
	want := Limits{Actions: 16, OpenLevels: 2, IdentityCommits: 200, Requests: 50, PagesPerCollection: 10, Records: 1000, ResponseBytes: 1 << 20, InputBytes: 64 << 10,
		DisplayBytes: 512, ErrorBytes: 2 << 10, OperationMillis: 60000, RequestMillis: 30000, LocalReadMillis: 5000, ReadWorkers: 4}
	if got := DefaultLimits(); got != want {
		t.Fatalf("%+v", got)
	}
	if err := DefaultLimits().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLimitsRefuseEveryZeroNegativeAndAboveCeilingValue(t *testing.T) {
	fields := map[string]func(*Limits, int){
		"actions": func(l *Limits, v int) { l.Actions = v }, "levels": func(l *Limits, v int) { l.OpenLevels = v },
		"commits": func(l *Limits, v int) { l.IdentityCommits = v }, "requests": func(l *Limits, v int) { l.Requests = v },
		"pages": func(l *Limits, v int) { l.PagesPerCollection = v }, "records": func(l *Limits, v int) { l.Records = v },
		"response": func(l *Limits, v int) { l.ResponseBytes = v }, "input": func(l *Limits, v int) { l.InputBytes = v },
		"display": func(l *Limits, v int) { l.DisplayBytes = v }, "error": func(l *Limits, v int) { l.ErrorBytes = v },
		"operation": func(l *Limits, v int) { l.OperationMillis = v }, "request": func(l *Limits, v int) { l.RequestMillis = v },
		"local": func(l *Limits, v int) { l.LocalReadMillis = v }, "workers": func(l *Limits, v int) { l.ReadWorkers = v },
	}
	ceilings := map[string]int{"actions": 16, "levels": 2, "commits": 200, "requests": 50, "pages": 10, "records": 1000, "response": 1 << 20, "input": 64 << 10, "display": 512, "error": 2 << 10, "operation": 60000, "request": 30000, "local": 5000, "workers": 4}
	for name, set := range fields {
		for _, v := range []int{0, -1, ceilings[name] + 1} {
			l := DefaultLimits()
			set(&l, v)
			if l.Validate() == nil {
				t.Errorf("%s=%d accepted", name, v)
			}
		}
		l := DefaultLimits()
		set(&l, ceilings[name])
		if err := l.Validate(); err != nil {
			t.Errorf("%s at its ceiling: %v", name, err)
		}
	}
}

func TestTimeoutOnlyShortensTheOperationBound(t *testing.T) {
	l, err := DefaultLimits().WithTimeout(10 * time.Second)
	if err != nil || l.OperationMillis != 10000 || l.RequestMillis != 30000 {
		t.Fatalf("%+v %v", l, err)
	}
	if l, err = DefaultLimits().WithTimeout(60 * time.Second); err != nil || l.OperationMillis != 60000 {
		t.Fatalf("%+v %v", l, err)
	}
	for _, d := range []time.Duration{0, -time.Second, 61 * time.Second, time.Hour} {
		if _, err := DefaultLimits().WithTimeout(d); err == nil {
			t.Errorf("%v accepted", d)
		}
	}
}

func TestRequestBudgetIsSharedAndNeverResets(t *testing.T) {
	b := NewBudget(DefaultLimits(), newClock().read)
	for i := 0; i < 50; i++ {
		if err := b.Request(); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	wantBound(t, b.Request(), ReasonBoundExceeded, "requests")
	if b.Requests() != 50 {
		t.Errorf("a refused request was counted: %d", b.Requests())
	}
	// The same budget serves every source and worker: a "second source" gets no fresh allowance.
	wantBound(t, b.Request(), ReasonBoundExceeded, "requests")
}

func TestPageRecordAndByteBounds(t *testing.T) {
	b := NewBudget(DefaultLimits(), newClock().read)
	for i := 0; i < 10; i++ {
		if err := b.Page("pulls"); err != nil {
			t.Fatalf("page %d: %v", i+1, err)
		}
	}
	wantBound(t, b.Page("pulls"), ReasonBoundExceeded, "pages")
	if err := b.Page("checks"); err != nil {
		t.Fatalf("pages are per collection: %v", err)
	}
	b = NewBudget(DefaultLimits(), newClock().read)
	if err := b.Records(1000); err != nil {
		t.Fatal(err)
	}
	wantBound(t, b.Records(1), ReasonBoundExceeded, "records")
	for _, kind := range []ByteKind{BytesRaw, BytesNormalized, BytesResult} {
		b = NewBudget(DefaultLimits(), newClock().read)
		if err := b.Bytes(kind, 1<<20); err != nil {
			t.Fatalf("%s at the limit: %v", kind, err)
		}
		wantBound(t, b.Bytes(kind, 1), ReasonBoundExceeded, string(kind)+"_bytes")
		// Each kind is its own bound.
		other := BytesRaw
		if kind == BytesRaw {
			other = BytesResult
		}
		if err := b.Bytes(other, 1); err != nil {
			t.Errorf("%s exhausted %s: %v", kind, other, err)
		}
	}
	b = NewBudget(DefaultLimits(), newClock().read)
	wantBound(t, b.Bytes("other", 1), ReasonBoundExceeded, "")
	wantBound(t, b.Records(-1), ReasonBoundExceeded, "")
}

func TestInputAndBodyBound(t *testing.T) {
	l := DefaultLimits()
	if err := CheckInput(l, 64<<10); err != nil {
		t.Fatal(err)
	}
	wantBound(t, CheckInput(l, 64<<10+1), ReasonBoundExceeded, "input_bytes")
	wantBound(t, CheckInput(l, -1), ReasonBoundExceeded, "input_bytes")
}

func TestOperationDeadlineUsesTheInjectedClockAndTheShorterWins(t *testing.T) {
	clock := newClock()
	b := NewBudget(DefaultLimits(), clock.read)
	if got := b.RequestTimeout(); got != 30*time.Second {
		t.Fatalf("request timeout %v", got)
	}
	if got := b.LocalReadTimeout(); got != 5*time.Second {
		t.Fatalf("local read timeout %v", got)
	}
	clock.now = fixtureNow.Add(45 * time.Second)
	if got := b.RequestTimeout(); got != 15*time.Second {
		t.Fatalf("the remaining operation deadline must win: %v", got)
	}
	clock.now = fixtureNow.Add(58 * time.Second)
	if got := b.LocalReadTimeout(); got != 2*time.Second {
		t.Fatalf("local read timeout %v", got)
	}
	if err := b.Request(); err != nil {
		t.Fatalf("still inside the operation bound: %v", err)
	}
	clock.now = fixtureNow.Add(60 * time.Second)
	wantBound(t, b.Err(), ReasonBoundExceeded, "operation_ms")
	wantBound(t, b.Request(), ReasonBoundExceeded, "operation_ms")
	if b.RequestTimeout() != 0 || b.LocalReadTimeout() != 0 {
		t.Error("an exhausted budget must grant no time")
	}
	short, _ := DefaultLimits().WithTimeout(3 * time.Second)
	clock = newClock()
	b = NewBudget(short, clock.read)
	if got := b.RequestTimeout(); got != 3*time.Second {
		t.Fatalf("--timeout shortens the request bound too: %v", got)
	}
}

func TestCancellationStopsEverythingAndIsNotABoundSuccess(t *testing.T) {
	b := NewBudget(DefaultLimits(), newClock().read)
	if b.Err() != nil {
		t.Fatal("a fresh budget is not exhausted")
	}
	b.Cancel()
	wantBound(t, b.Err(), ReasonCancelled, "")
	wantBound(t, b.Request(), ReasonCancelled, "")
	wantBound(t, b.Page("x"), ReasonCancelled, "")
	wantBound(t, b.Records(1), ReasonCancelled, "")
	wantBound(t, b.Bytes(BytesRaw, 1), ReasonCancelled, "")
	b.Cancel() // idempotent
	if b.Requests() != 0 {
		t.Error("a cancelled request was counted")
	}
}

func TestReadWorkersAreAtMostFour(t *testing.T) {
	l := DefaultLimits()
	for tasks, want := range map[int]int{0: 1, 1: 1, 2: 2, 4: 4, 5: 4, 100: 4, -3: 1} {
		if got := ReadWorkers(l, tasks); got != want {
			t.Errorf("tasks=%d: %d want %d", tasks, got, want)
		}
	}
	l.ReadWorkers = 2
	if got := ReadWorkers(l, 9); got != 2 {
		t.Errorf("a lower limit wins: %d", got)
	}
}

func TestResultsComeBackInInputIndexOrder(t *testing.T) {
	in := []Indexed[string]{{2, "c"}, {0, "a"}, {3, "d"}, {1, "b"}}
	got, err := OrderByInputIndex(in, 4)
	if err != nil || strings.Join(got, "") != "abcd" {
		t.Fatalf("%v %v", got, err)
	}
	if in[0].Index != 2 {
		t.Error("the input slice was reordered in place")
	}
	if got, err = OrderByInputIndex[string](nil, 0); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v %v", got, err)
	}
	for name, bad := range map[string][]Indexed[string]{
		"missing":      {{0, "a"}, {2, "c"}},
		"duplicate":    {{0, "a"}, {0, "b"}},
		"out of range": {{0, "a"}, {5, "b"}},
		"negative":     {{-1, "a"}, {0, "b"}},
		"short":        {{0, "a"}},
		"extra":        {{0, "a"}, {1, "b"}, {2, "c"}},
	} {
		if _, err := OrderByInputIndex(bad, 2); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestDisplayEscapesHostileTextAndShortensWithoutSplitting(t *testing.T) {
	got, trunc := Display("plain text", 512)
	if got != "plain text" || trunc {
		t.Fatalf("%q %v", got, trunc)
	}
	if got, _ = Display(`a\b`, 512); got != `a\\b` {
		t.Fatalf("a backslash must be escaped so escapes stay unambiguous: %q", got)
	}
	got, _ = Display("a\x1b[31mred\n\rb\x00c\x7fd", 512)
	if strings.ContainsAny(got, "\x1b\n\r\x00\x7f") || !strings.Contains(got, `\x1b`) || !strings.Contains(got, `\n`) {
		t.Fatalf("controls not escaped: %q", got)
	}
	got, _ = Display("safe\u202eevil\u2066x\u200f", 512)
	if strings.ContainsAny(got, "\u202e\u2066\u200f") || !strings.Contains(got, "\\u202e") {
		t.Fatalf("bidi controls not escaped: %q", got)
	}
	got, _ = Display("bad\xffbyte", 512)
	if strings.Contains(got, "\xff") || !strings.Contains(got, `\xff`) {
		t.Fatalf("invalid UTF-8 not escaped: %q", got)
	}
	long := strings.Repeat("é", 400) // 800 bytes
	got, trunc = Display(long, 512)
	if !trunc || len(got) > 512 || !strings.HasPrefix(long, got) {
		t.Fatalf("len %d trunc %v", len(got), trunc)
	}
	// A rune is never split, and an escape is never cut in half.
	got, trunc = Display(strings.Repeat("\x01", 300), 10)
	if !trunc || len(got) > 10 || strings.HasSuffix(got, `\`) || strings.Count(got, `\x01`)*4 != len(got) {
		t.Fatalf("escape split: %q", got)
	}
	if got, trunc = Display("exact", 5); got != "exact" || trunc {
		t.Fatalf("%q %v", got, trunc)
	}
	if got, trunc = Display("anything", 0); got != "" || !trunc {
		t.Fatalf("%q %v", got, trunc)
	}
}
