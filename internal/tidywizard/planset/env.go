// SPDX-License-Identifier: EUPL-1.2

package planset

import "os"

// Environ and Getenv give the command layer the process environment without importing os there
// (the architecture check keeps internal/cli away from it).
func Environ() []string { return os.Environ() }

// Getenv returns the value of the environment variable, or "".
func Getenv(name string) string { return os.Getenv(name) }
