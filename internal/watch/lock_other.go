//go:build !unix

// SPDX-License-Identifier: EUPL-1.2
package watch

import "errors"

// Supported is false where there is no flock (Windows): stw watch refuses to run there.
const Supported = false

// ErrUnsupported is returned by the lock stubs.
var ErrUnsupported = errors.New("stw watch is not supported on this platform")

type dirLock struct{}

func acquireLock(string) (*dirLock, error) { return nil, ErrUnsupported }

func (l *dirLock) Close() error { return nil }

func lockHolder(string) (bool, int) { return false, 0 }
