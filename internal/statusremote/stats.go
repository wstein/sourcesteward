// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
)

// SourceStats records remote API calls, caching, latency, and rate-limit consumption for one source.
type SourceStats struct {
	Repo       string
	Source     string
	Requests   int
	Cached304s int
	Latency    time.Duration
	Consumed   int
}

// RemoteStats holds overall and per-source statistics of the remote collection.
type RemoteStats struct {
	Requests   int
	Cached304s int
	Sources    []SourceStats
	RateLimit  hosting.RateInfo
}

type sourceKey struct {
	repo   string
	source string
}

type sourceContextKey struct{}

// WithSource attaches the repository slug and source identifier to the context.
func WithSource(ctx context.Context, repo string, source overview.Source) context.Context {
	return context.WithValue(ctx, sourceContextKey{}, sourceKey{repo: repo, source: string(source)})
}

// WithNamedSource attaches the repository slug and an arbitrary source identifier (like "viewer") to the context.
func WithNamedSource(ctx context.Context, repo, source string) context.Context {
	return context.WithValue(ctx, sourceContextKey{}, sourceKey{repo: repo, source: source})
}

func sourceFromContext(ctx context.Context) (string, string) {
	if k, ok := ctx.Value(sourceContextKey{}).(sourceKey); ok {
		return k.repo, k.source
	}
	return "", ""
}
