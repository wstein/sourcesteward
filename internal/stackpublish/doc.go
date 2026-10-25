// SPDX-License-Identifier: EUPL-1.2

// Package stackpublish is the pure functional core of Spec 016 (stacked PR coordination): typed
// forge-effect plans, bounds, per-action guards and task ownership. It performs no I/O, reads no
// clock and starts no goroutine; callers pass every observation and the clock in. It does not
// reach the network, a process or the forge, so this slice implements no remote effect.
package stackpublish
