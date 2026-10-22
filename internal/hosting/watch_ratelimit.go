// SPDX-License-Identifier: EUPL-1.2
package hosting

import "time"

// RateAdvice is the slowdown policy of Spec 035 as a typed signal. This layer never
// sleeps or schedules: the watcher decides what to do with the advice.
type RateAdvice int

const (
	// AdviceNone: no action.
	AdviceNone RateAdvice = iota
	// AdviceSlow: remaining is below 10 percent of the limit; double the interval
	// of the next cycle (cap 10 minutes, applied by the caller).
	AdviceSlow
	// AdviceWait: remaining is below 50; wait until RateDecision.Until and report
	// watch-error rate-limited (once, re-armed after a successful cycle).
	AdviceWait
)

// Thresholds of Spec 035 (Rate limit, errors, offline).
const (
	RateWaitBelow  = 50
	rateResetSlack = 5 * time.Second
)

// RateDecision is the outcome of AssessRateLimit.
type RateDecision struct {
	Advice RateAdvice
	// Until is reset plus 5 s for AdviceWait, zero otherwise.
	Until time.Time
}

// AssessRateLimit maps the state read from rate_limit (or from response headers) to
// the policy: remaining < 50 waits for the reset, remaining < 10 percent of the limit
// slows down, anything else (or unknown state) needs no action.
func AssessRateLimit(info RateInfo) RateDecision {
	if !info.Known {
		return RateDecision{}
	}
	if info.Remaining < RateWaitBelow {
		return RateDecision{Advice: AdviceWait, Until: info.Reset.Add(rateResetSlack)}
	}
	if info.Remaining*10 < info.Limit {
		return RateDecision{Advice: AdviceSlow}
	}
	return RateDecision{}
}

// RateLimitedUntil is when to try again after a CodeRateLimited error (a 403 or 429
// with rate-limit evidence): Retry-After if given, else the reset time, each plus
// 5 s; with no usable evidence one minute plus 5 s from now.
func RateLimitedUntil(e *WatchError, now time.Time) time.Time {
	if e.RetryAfter > 0 {
		return now.Add(e.RetryAfter + rateResetSlack)
	}
	if e.RateLimit.Known && e.RateLimit.Reset.After(now) {
		return e.RateLimit.Reset.Add(rateResetSlack)
	}
	return now.Add(time.Minute + rateResetSlack)
}
