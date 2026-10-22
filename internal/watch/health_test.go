// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWatchErrorEpisodes(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	e1 := one(t, r.must(failWith("network")), KindWatchError)
	if e1.ID != "o/r#-:watch-error:network:e1:f1" || e1.Repo == nil || e1.PR != nil {
		t.Fatalf("%+v", e1)
	}
	d := e1.Details.(*WatchErrorDetails)
	if d.Code != "network" || d.Failures != 1 || d.RetryInSeconds != 60 || d.Repo == nil || *d.Repo != "o/r" || !strings.Contains(d.Message, "boom") {
		t.Fatalf("%+v", d)
	}
	// the same code in the same episode is silent (the repository is in backoff or fails again)
	for i := 0; i < 3; i++ {
		none(t, r.must(failWith("network")))
	}
	// a new code fires again (after the backoff of the earlier failures)
	r.now = r.now.Add(10 * time.Minute)
	e2 := one(t, only(r.must(failWith("http-5xx")), KindWatchError), KindWatchError) // the long outage also stalls
	if !strings.Contains(e2.ID, ":http-5xx:e1:f") {
		t.Fatal(e2.ID)
	}
	r.now = r.now.Add(10 * time.Minute)
	rec := one(t, only(r.must(ok(mk(1))), KindWatchRecovered)[:1], KindWatchRecovered)
	if rec.ID != "o/r#-:watch-recovered:e1" || rec.Details.(*WatchRecoveredDetails).AfterFailures < 2 {
		t.Fatalf("%+v", rec)
	}
	// the accepted state was not touched while failing
	if len(r.state().Repo["o/r"].PRs) != 1 {
		t.Fatal("state touched")
	}
	// a second episode fires watch-error again with a new id
	r.now = r.now.Add(10 * time.Minute)
	e3 := one(t, only(r.must(failWith("network")), KindWatchError), KindWatchError)
	if e3.ID != "o/r#-:watch-error:network:e2:f1" {
		t.Fatal(e3.ID)
	}
	uniq(t, r.journalIDs())
}

func TestWatchErrorRepeatsEveryTenthFailure(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	var errs []string
	for i := 0; i < 400; i++ {
		for _, e := range r.must(failWith("network")) {
			if e.Kind == KindWatchError {
				errs = append(errs, e.ID)
			}
		}
		if len(errs) == 3 {
			break
		}
	}
	want := []string{"o/r#-:watch-error:network:e1:f1", "o/r#-:watch-error:network:e1:f10", "o/r#-:watch-error:network:e1:f20"}
	if !reflect.DeepEqual(errs, want) {
		t.Fatal(errs)
	}
}

func TestBackoffDoublesToTheCap(t *testing.T) {
	c := &cycle{w: &Watcher{o: Options{Interval: time.Minute}}}
	var got []time.Duration
	for f := 1; f <= 6; f++ {
		got = append(got, c.backoff(f))
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	c.w.o.Interval = 10 * time.Second
	if c.backoff(9) != 100*time.Second {
		t.Fatal("cap is 10 x interval when smaller than 10 minutes")
	}
}

func TestRepositoryInBackoffIsNotPolled(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	one(t, r.must(failWith("network")), KindWatchError)
	none(t, r.must(failWith("network"))) // polled again after 60 s, same code: silent, backoff now 120 s
	none(t, r.must(ok(mk(1))))           // within the backoff: not polled, so no recovery yet
	one(t, r.must(ok(mk(1))), KindWatchRecovered)
}

func TestWatchStalledOncePerStallAndBothEpisodesRecover(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	r.now = r.now.Add(11 * time.Minute)
	evs := r.must(failWith("network"))
	if got := kinds(evs); !reflect.DeepEqual(got, []string{KindWatchError, KindWatchStalled}) {
		t.Fatal(got)
	}
	st := evs[1]
	d := st.Details.(*WatchStalledDetails)
	if st.ID != "o/r#-:watch-stalled:s1" || d.Repo != "o/r" || d.ThresholdSeconds != 600 || d.Since != "2026-10-22T09:01:00Z" {
		t.Fatalf("%+v %s", d, st.ID)
	}
	for i := 0; i < 5; i++ {
		for _, e := range r.must(failWith("network")) {
			if e.Kind == KindWatchStalled {
				t.Fatal("stalled twice")
			}
		}
	}
	var rec []Event
	for len(rec) == 0 {
		rec = r.must(ok(mk(1)))
	}
	if got := ids(rec); !reflect.DeepEqual(got, []string{"o/r#-:watch-recovered:e1", "o/r#-:watch-recovered:s1"}) {
		t.Fatal(got)
	}
}

func TestRunWideRateLimitHoldsThePollAndRecoversWithoutARepo(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	r.set("o/r", snap{err: &ObserveError{Code: "rate-limited", Message: "reset at 10:00", RunWide: true, RetryIn: 5 * time.Minute}})
	r.mustOpen()
	res, err := r.w.Cycle(t.Context())
	r.now = r.now.Add(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	e := one(t, res.Events, KindWatchError)
	if e.ID != "-:watch-error:rate-limited:e1:f1" || e.Repo != nil || res.RetryIn != 5*time.Minute {
		t.Fatalf("%+v %v", e, res.RetryIn)
	}
	// within the hold nothing is polled
	r.set("o/r", ok(mk(1), mk(2)))
	r.now = r.now.Add(time.Minute)
	res, _ = r.w.Cycle(t.Context())
	none(t, res.Events)
	r.now = r.now.Add(6 * time.Minute)
	res, _ = r.w.Cycle(t.Context())
	if got := kinds(res.Events); !reflect.DeepEqual(got, []string{KindNewPR, KindWatchRecovered}) || res.Events[1].ID != "-:watch-recovered:e1" {
		t.Fatal(ids(res.Events))
	}
}

func TestMessageIsSanitizedAndBounded(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	r.set("o/r", snap{err: &ObserveError{Code: "network", Message: "bad\x00\n" + strings.Repeat("é", 600)}})
	evs, _ := r.run()
	m := one(t, evs, KindWatchError).Details.(*WatchErrorDetails).Message
	if len(m) > 512 || strings.ContainsAny(m, "\x00\n") {
		t.Fatalf("%d %q", len(m), m)
	}
}

func only(evs []Event, kind string) []Event {
	var out []Event
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}
