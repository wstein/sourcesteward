// SPDX-License-Identifier: EUPL-1.2

package fanout

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// gate holds calls open and counts how many are in flight and the peak.
type gate struct {
	mu           sync.Mutex
	inFlight     int
	peak         int
	started      []int
	release      chan struct{}
	releaseCount int
}

func newGate() *gate { return &gate{release: make(chan struct{})} }

func (g *gate) enter(i int) {
	g.mu.Lock()
	g.inFlight++
	g.peak = max(g.peak, g.inFlight)
	g.started = append(g.started, i)
	g.mu.Unlock()
}

func (g *gate) leave() {
	g.mu.Lock()
	g.inFlight--
	g.mu.Unlock()
}

func (g *gate) snapshot() (inFlight, peak int, started []int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inFlight, g.peak, append([]int(nil), g.started...)
}

// Rule 1: at most width calls run at the same time, and exactly width do when there is work.
func TestDoNeverRunsMoreThanWidthCalls(t *testing.T) {
	for _, tc := range []struct {
		name           string
		n, width, want int
	}{
		{"width 3 of 10", 10, 3, 3}, {"width 1", 5, 1, 1}, {"width 0 is 1", 5, 0, 1}, {"negative width is 1", 5, -4, 1},
		{"width over n is n", 4, 100, 4}, {"width equals n", 4, 4, 4}, {"one item", 1, 8, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				g := newGate()
				results := make([]int, tc.n)
				done := make(chan error)
				go func() {
					done <- Do(context.Background(), tc.n, tc.width, func(_ context.Context, i int) {
						g.enter(i)
						<-g.release
						g.leave()
						results[i] = i + 1
					})
				}()
				synctest.Wait()
				if in, _, _ := g.snapshot(); in != tc.want {
					t.Errorf("in flight before any finished: %d, want %d", in, tc.want)
				}
				close(g.release)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if _, peak, started := g.snapshot(); peak != tc.want || len(started) != tc.n {
					t.Errorf("peak %d, started %d of %d", peak, len(started), tc.n)
				}
				for i, r := range results {
					if r != i+1 {
						t.Errorf("item %d did not run to completion", i)
					}
				}
			})
		})
	}
}

// Rule 6 and rule 5 for an empty run: no item exists to skip, so nothing is an error.
func TestDoWithoutItemsIsNilEvenOnADoneContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, n := range []int{0, -1} {
		if err := Do(ctx, n, 4, func(context.Context, int) { t.Error("called") }); err != nil {
			t.Errorf("n=%d: %v", n, err)
		}
	}
}

// Rule 6: with items and a done context nothing is started and no goroutine is left.
func TestDoOnADoneContextStartsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := Do(ctx, 5, 3, func(context.Context, int) { t.Error("called") }); !errors.Is(err, context.Canceled) {
			t.Fatalf("%v", err)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		if err := Do(ctx, 2, 2, func(context.Context, int) { t.Error("called") }); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%v", err)
		}
	})
}

// Rule 5: a cancellation after every item had started is not an error of Do.
func TestDoLateCancellationIsNotAnError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		g := newGate()
		done := make(chan error)
		go func() {
			done <- Do(ctx, 3, 3, func(_ context.Context, i int) { g.enter(i); <-g.release; g.leave() })
		}()
		synctest.Wait()
		if _, _, started := g.snapshot(); len(started) != 3 {
			t.Fatalf("started %v", started)
		}
		cancel()
		close(g.release)
		if err := <-done; err != nil {
			t.Fatalf("late cancellation returned %v", err)
		}
	})
}

// Rule 5, other side: a cancellation that skips an item is the caller's ctx.Err(); skipped items
// were never started and keep their zero value; started ones ran to completion.
func TestDoCancellationDuringSkipsTheRest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		out := make([]int, 6)
		err := Do(ctx, 6, 1, func(_ context.Context, i int) {
			out[i] = i + 1
			if i == 1 {
				cancel()
			}
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%v", err)
		}
		want := []int{1, 2, 0, 0, 0, 0}
		for i := range want {
			if out[i] != want[i] {
				t.Fatalf("out %v, want %v", out, want)
			}
		}
	})
}

// Rule 3 and 7: the first panic by index is re-raised unchanged after every worker stopped, the
// remaining items are not started, and the context of fn is cancelled so a cooperative fn stops.
func TestDoReRaisesTheLowestIndexPanicAfterAllWorkersStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		low, high := errors.New("low"), errors.New("high")
		var mu sync.Mutex
		var stopped []int
		var started []int
		var got any
		allStarted := make(chan struct{})
		func() {
			defer func() { got = recover() }()
			_ = Do(context.Background(), 6, 6, func(ctx context.Context, i int) {
				mu.Lock()
				started = append(started, i)
				if len(started) == 6 {
					close(allStarted)
				}
				mu.Unlock()
				<-allStarted
				switch i {
				case 4:
					panic(high)
				case 1:
					// The higher index panics first and its panic is recorded before the context of fn
					// is cancelled: the lowest index must still win.
					<-ctx.Done()
					panic(low)
				default:
					<-ctx.Done()
					mu.Lock()
					stopped = append(stopped, i)
					mu.Unlock()
				}
			})
		}()
		if got != low {
			t.Fatalf("re-raised %v, want the panic of the lowest index", got)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(stopped) != 4 {
			t.Errorf("cooperative workers stopped: %v", stopped)
		}
	})
}

// A panic with only one worker skips the remaining items instead of running them.
func TestDoPanicSkipsTheItemsNotYetStarted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var ran []int
		boom := errors.New("boom")
		var got any
		func() {
			defer func() { got = recover() }()
			_ = Do(context.Background(), 5, 1, func(_ context.Context, i int) {
				ran = append(ran, i)
				if i == 2 {
					panic(boom)
				}
			})
		}()
		if got != boom || len(ran) != 3 {
			t.Fatalf("got %v, ran %v", got, ran)
		}
	})
}

// Rule 4: Do adds no deadline; fn sees the deadline of the caller's context, none if there is none.
func TestDoAddsNoDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_ = Do(context.Background(), 2, 2, func(ctx context.Context, _ int) {
			if _, ok := ctx.Deadline(); ok {
				t.Error("fanout added a deadline")
			}
		})
		want := time.Now().Add(time.Hour)
		ctx, cancel := context.WithDeadline(context.Background(), want)
		defer cancel()
		type key struct{}
		ctx = context.WithValue(ctx, key{}, "v")
		_ = Do(ctx, 2, 2, func(ctx context.Context, _ int) {
			if d, ok := ctx.Deadline(); !ok || !d.Equal(want) || ctx.Value(key{}) != "v" {
				t.Errorf("deadline %v %v, value %v", d, ok, ctx.Value(key{}))
			}
		})
	})
}

// Rule 2: Do callers write out[i] only; every index is written exactly once, whatever the order.
func TestDoCoversEveryIndexOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const n = 50
		var mu sync.Mutex
		seen := make([]int, n)
		if err := Do(context.Background(), n, 7, func(_ context.Context, i int) {
			mu.Lock()
			seen[i]++
			mu.Unlock()
		}); err != nil {
			t.Fatal(err)
		}
		for i, c := range seen {
			if c != 1 {
				t.Errorf("item %d ran %d times", i, c)
			}
		}
	})
}
