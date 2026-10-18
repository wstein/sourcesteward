// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"io"
	"net/http"
)

// readyMeterTransport sits before guardedTransport. It counts body.Read bytes
// actually delivered, including partial errors and the guard's overflow probe;
// it measures neither wire/header bytes nor unread Content-Length declarations.
// The sequential operation owns the shared budget and underlying client cleanup.
type readyMeterTransport struct {
	next   http.RoundTripper
	budget *readyReadBudget
}

func (m readyMeterTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := m.next.RoundTrip(req)
	if res != nil && res.Body != nil {
		res.Body = &readyMeterBody{ReadCloser: res.Body, budget: m.budget}
	}
	return res, err
}

type readyMeterBody struct {
	io.ReadCloser
	budget *readyReadBudget
}

func (b *readyMeterBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.budget.bodyBytes += n
	return n, err
}
