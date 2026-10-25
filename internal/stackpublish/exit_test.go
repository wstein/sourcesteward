// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import "testing"

// The unified toolbelt exit contract (revision 2): 0 complete, 1 everything else that is not a
// usage error, 2 usage. There is no 3; the hidden Spec 015 v1 planner's current 3 is separate.
func TestExitCodesAreUnifiedOnZeroOneTwo(t *testing.T) {
	want := map[Status]int{StatusComplete: 0, StatusRefused: 1, StatusIncomplete: 1, StatusPartial: 1, StatusCancelled: 1, StatusFailed: 1, StatusUnknown: 1, StatusUsage: 2}
	for status, code := range want {
		if got := ExitCode(status); got != code {
			t.Errorf("%s: %d want %d", status, got, code)
		}
	}
	for _, status := range []Status{"", "ok", "strict-findings", "complete "} {
		if got := ExitCode(status); got != 1 {
			t.Errorf("%q must fail closed as 1, got %d", status, got)
		}
	}
	if ExitComplete != 0 || ExitRefused != 1 || ExitUsage != 2 {
		t.Error("exit constants")
	}
}

func TestValidationErrorIsTypedAndNeverCarriesInputText(t *testing.T) {
	err := &ValidationError{Code: "x-code", Field: "actions[0].target", Usage: true}
	if msg := err.Error(); msg == "" || msg != "x-code: actions[0].target" {
		t.Fatalf("%q", msg)
	}
}
