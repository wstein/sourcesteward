// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"

	"github.com/wstein/sourcesteward/internal/release"
)

// RunPrivateRoute lets the command entry point hand the private native route of the build
// checkout comparison (spec 026 T031/T034) to internal/release before any command parsing.
// It is not a command: it reports handled only for argv0 "git" with the private role
// variable, and a successful route replaces the process.
func RunPrivateRoute(ctx context.Context) (handled bool, status int) {
	return release.RunBuildCheckoutInternal(ctx)
}
