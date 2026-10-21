// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"errors"
)

// refusal is a decided refusal (status error): a stable code, fixed text and bounded data.
type refusal struct {
	code    string
	message string
	details map[string]any
}

func (r refusal) Error() string { return r.code + ": " + r.message }

func refuse(code, message string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	return refusal{code, message, details}
}

// boundHit is a documented bound that stopped evidence collection (status incomplete).
type boundHit struct{ code, name string }

func (b boundHit) Error() string { return b.code + ": " + b.name }

func pathBound() error { return boundHit{"bound-exceeded", "max_paths"} }

// diagnosticFor maps a failure of the plan to its diagnostic and status.
func diagnosticFor(err error) (status string, d Diagnostic, bound string) {
	var r refusal
	var b boundHit
	switch {
	case errors.As(err, &r):
		return StatusError, Diagnostic{r.code, r.message, r.details}, ""
	case errors.As(err, &b):
		return StatusIncomplete, Diagnostic{b.code, "A documented bound was reached.", map[string]any{"bound": b.name}}, b.name
	case errors.Is(err, context.DeadlineExceeded):
		return StatusIncomplete, Diagnostic{"timeout", "The plan exceeded its time bound.", map[string]any{"bound": "timeout_seconds"}}, "timeout_seconds"
	case errors.Is(err, context.Canceled):
		return StatusIncomplete, Diagnostic{"canceled", "The plan was canceled.", map[string]any{}}, ""
	case errors.Is(err, errOutputBound):
		return StatusIncomplete, Diagnostic{"bound-exceeded", "A documented bound was reached.", map[string]any{"bound": "command_output"}}, "command_output"
	}
	return StatusError, Diagnostic{"git-failed", "A read-only Git command failed.", map[string]any{}}, ""
}
