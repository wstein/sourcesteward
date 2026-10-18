// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReleaseFinalAdvisoryPropagatesCancellation(t *testing.T) {
	for _, stop := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(stop.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sha := strings.Repeat("a", 40)
			api := apiReaderFunc(func(_ context.Context, p string) ([]byte, error) {
				if strings.Contains(p, "/rulesets?") {
					if errors.Is(stop, context.Canceled) {
						cancel()
					}
					return nil, stop
				}
				if strings.HasSuffix(p, "/main") {
					return []byte(`{"ref":"refs/heads/main","object":{"type":"commit","sha":"` + sha + `"}}`), nil
				}
				return []byte(`[]`), nil
			})
			_, err := readRelease(ctx, api, "o/r", sha, nil)
			if !errors.Is(err, stop) {
				t.Fatalf("final advisory swallowed %v: %v", stop, err)
			}
		})
	}
}
func TestReleaseFinalAdvisoryPermissionFailureRemainsUnknown(t *testing.T) {
	sha := strings.Repeat("a", 40)
	api := apiReaderFunc(func(_ context.Context, p string) ([]byte, error) {
		if strings.Contains(p, "/rulesets?") {
			return nil, errors.New("fixture permission denied")
		}
		if strings.HasSuffix(p, "/main") {
			return []byte(`{"ref":"refs/heads/main","object":{"type":"commit","sha":"` + sha + `"}}`), nil
		}
		return []byte(`[]`), nil
	})
	got, err := readRelease(context.Background(), api, "o/r", sha, nil)
	if err != nil || got.RulesetsKnown {
		t.Fatalf("advisory affected required evidence: %+v %v", got, err)
	}
}
