// SPDX-License-Identifier: EUPL-1.2
package hosting

import "errors"

// ErrAuthentication marks credential acquisition failure before HTTP admission.
// Its typed wrappers preserve the existing human diagnostics byte for byte.
var ErrAuthentication = errors.New("hosting authentication unavailable")

type authenticationFailure string

func (e authenticationFailure) Error() string        { return string(e) }
func (e authenticationFailure) Is(target error) bool { return target == ErrAuthentication }
