// SPDX-License-Identifier: EUPL-1.2

// Package fanout is the one place SourceSteward starts goroutines (Spec 040).
package fanout

import (
	"context"
	"runtime"
	"sync"
)

// Do runs fn(ctx, 0..n-1) with at most width calls at the same time and returns when every started
// call has returned. Items that were not started because ctx ended are not run; Do then returns
// ctx.Err(). If every item had started before ctx ended, Do returns nil: a late cancellation is not
// an error of this call. A panic in fn is recovered in its worker, the remaining items are
// cancelled, and the first panic (by index) is re-raised, unchanged, in the caller after all
// workers have stopped; a runtime.Goexit in fn ends the caller the same way, after all workers
// have stopped. width < 1 is 1; width > n is n; n < 1 runs nothing and returns nil.
//
// The context fn receives is cancelled when the first panic or runtime.Goexit is recorded. Do adds no deadline of its
// own. Callers write out[i] only; they never append to a shared slice.
func Do(ctx context.Context, n, width int, fn func(ctx context.Context, i int)) error {
	if n < 1 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	width = min(max(width, 1), n)
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		next     int
		panicked bool
		panicIdx int
		panicVal any
		goexit   bool
		wg       sync.WaitGroup
	)
	// claim hands out the next index, or reports that there is none (all handed out, or the
	// context ended and the remaining items are skipped).
	claim := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		if next >= n || workCtx.Err() != nil {
			return 0, false
		}
		i := next
		next++
		return i, true
	}
	run := func(i int) {
		returned, recovered := false, false
		defer func() {
			if returned || recovered {
				return
			}
			// Neither a return nor a recovered panic: fn ended its goroutine with runtime.Goexit
			// (t.FailNow, for example). Remember it, cancel the rest, and end the caller the same
			// way once every worker has stopped.
			mu.Lock()
			goexit = true
			mu.Unlock()
			cancel()
		}()
		func() {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				recovered = true
				mu.Lock()
				if !panicked || i < panicIdx {
					panicked, panicIdx, panicVal = true, i, r
				}
				mu.Unlock()
				cancel()
			}()
			fn(workCtx, i)
			returned = true
		}()
	}
	wg.Add(width)
	for w := 0; w < width; w++ {
		go func() {
			defer wg.Done()
			for {
				i, ok := claim()
				if !ok {
					return
				}
				run(i)
			}
		}()
	}
	wg.Wait()
	if panicked {
		panic(panicVal)
	}
	if goexit {
		runtime.Goexit()
	}
	if next < n {
		// At least one item was skipped, and only the end of the caller's context (a panic is
		// handled above) skips an item.
		return ctx.Err()
	}
	return nil
}
