// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"net/http"
	"time"
)

// InspectPRStatus is the explicit schema 2 one-shot PR observation: one numbered
// GET after native resolution and authentication, optionally bound to an expected head.
func InspectPRStatus(ctx context.Context, path string, o Options) PRStatusResult {
	return InspectPRStatusWithTransport(ctx, path, o, nil)
}

// InspectPRStatusWithTransport permits an in-process HTTP fixture; nil selects HTTPS.
func InspectPRStatusWithTransport(ctx context.Context, path string, o Options, rt http.RoundTripper) PRStatusResult {
	return inspectPRStatus(ctx, path, o, productionDependencies(rt))
}

func inspectPRStatus(parent context.Context, path string, o Options, d dependencies) (r PRStatusResult) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	r = newPRStatusResult()
	defer func() {
		if o.ExpectHead != "" {
			expect := o.ExpectHead
			r.Observation.ExpectedHead = &expect
			r.Observation.Expectation = "unobserved"
			if r.PullRequest != nil {
				if r.PullRequest.HeadSHA == expect {
					r.Observation.Expectation = "matched"
				} else {
					r.Observation.Expectation = "mismatched"
					r.fail("hosting-head-mismatch", "PR head does not match the expected head", "expectation-mismatch")
				}
			}
		}
		if ctx.Err() != nil {
			r.fail("hosting-canceled", "hosting inspection canceled or exceeded its deadline", "canceled")
		}
		r.ObservedAt = time.Now().UTC().Format(time.RFC3339)
	}()
	failed := func(sel Result) PRStatusResult {
		r.Backend, r.Identity, r.Repository = sel.Backend, sel.Identity, sel.Repository
		r.Candidates = sel.Candidates
		code, msg := "hosting-native-failed", "hosting inspection failed"
		if len(sel.Diagnostics) > 0 {
			code, msg = sel.Diagnostics[0].Code, sel.Diagnostics[0].Message
		}
		r.fail(code, msg, "failed")
		return r
	}
	sel := newResult("pr")
	target, ok := resolve(ctx, path, o, d, &sel)
	if !ok {
		return failed(sel)
	}
	api, cleanup, err := d.api(ctx, target, sel.Repository.Host)
	if err != nil {
		sel.Diagnostics = nil
		sel.fail("hosting-api-unavailable", err.Error())
		return failed(sel)
	}
	defer cleanup()
	return readPRStatusOnce(ctx, api, sel, o.Number, nil)
}
