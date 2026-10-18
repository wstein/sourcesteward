// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestReleaseCredentialRefusalIsTypedBeforeHTTP(t *testing.T) {
	target, _ := fakeDispatchGH(t, false)
	calls := 0
	_, err := ReadReleaseWithTransport(context.Background(), target, Repository{Host: "github.com", Owner: "owner", Name: "repo"}, strings.Repeat("a", 40), []string{"ci.yml"}, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, "{}"), nil }))
	if !errors.Is(err, ErrAuthentication) || calls != 0 || err.Error() != "gh authentication unavailable; check selected-host credentials" {
		t.Fatalf("auth classification/diagnostic changed: calls=%d err=%v", calls, err)
	}
}
