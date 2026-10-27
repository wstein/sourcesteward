// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"reflect"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

// Every human answer and typed word of the tidy wizard and of the separate apply is read through
// tidyConfirmOpener (D16, FR-018). No test override is installed here, so this is the production
// value: it must be the controlling terminal, never a reader of stdin.
func TestProductionTerminalOpenerIsTheControllingTerminal(t *testing.T) {
	if reflect.ValueOf(tidyConfirmOpener).Pointer() != reflect.ValueOf(owner.DevTTY).Pointer() {
		t.Fatal("the production tidyConfirmOpener must be owner.DevTTY (the controlling terminal), not stdin")
	}
}
