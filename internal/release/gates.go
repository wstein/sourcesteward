// SPDX-License-Identifier: EUPL-1.2
package release

// Gate is an append-only release decision code and its evidence outcome.
type Gate struct {
	Code    string  `json:"code"`
	Outcome string  `json:"outcome"`
	Reason  *string `json:"reason"`
	Detail  *string `json:"detail"`
}

func gate(code, outcome, reason, detail string) Gate {
	g := Gate{Code: code, Outcome: outcome}
	if reason != "" {
		g.Reason = &reason
	}
	if detail != "" {
		g.Detail = &detail
	}
	return g
}

// Verdict gives failure precedence over missing evidence; neither can pass silently.
func Verdict(gates []Gate) string {
	verdict := "pass"
	for _, g := range gates {
		if g.Outcome == "fail" {
			return "fail"
		}
		if g.Outcome == "not-run" {
			verdict = "incomplete"
		}
	}
	return verdict
}
