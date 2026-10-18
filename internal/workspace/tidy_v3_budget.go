// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"
)

var errTidyV3ProofLimit = errors.New("v3 operation proof budget exhausted")

const (
	proofUser = iota
	proofMetadata
	proofFraming
	proofStderr
	proofChannels
)

type tidyV3ProofCounters struct {
	Observations, RawProcesses, UserPasses, MetadataPasses                         int
	UserBytes, MetadataBytes, FramingBytes, StderrBytes, TotalBytes, MaxReadBuffer int64
}

// One ledger belongs to the entire operation, never a sample or retry. Reads
// reserve allowance before touching a pipe, including stderr's concurrent reads.
// A single shared detection reservation serializes EOF/overrun probes when the
// total or a channel is exhausted; failed and in-flight reads remain accounted.
type tidyV3ProofBudget struct {
	mu                                                                      sync.Mutex
	maxObservations, observations, rawProcesses, userPasses, metadataPasses int
	currentRaw                                                              bool
	currentUserPasses, currentMetadataPasses                                int
	limits, counts, reserved                                                [proofChannels]int64
	totalLimit, total, totalReserved, maxReadBuffer                         int64
	detection                                                               bool
	failed                                                                  error
	changed                                                                 chan struct{}
}

func newTidyV3ProofBudget(apply bool) *tidyV3ProofBudget {
	n := int64(3)
	if apply {
		n = 8
	}
	return &tidyV3ProofBudget{maxObservations: int(n), limits: [proofChannels]int64{n * 192 * 1024 * 1024, n * (128*1024*1024 + 128*1024), n * 128 * 1024, n * 64 * 1024}, totalLimit: n * (320*1024*1024 + 320*1024), changed: make(chan struct{})}
}
func (b *tidyV3ProofBudget) beginObservation() error {
	if b == nil {
		return errTidyV3ProofLimit
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failed != nil {
		return b.failed
	}
	if b.observations >= b.maxObservations {
		b.failed = errTidyV3ProofLimit
		return b.failed
	}
	b.observations++
	b.currentRaw = false
	b.currentUserPasses = 0
	b.currentMetadataPasses = 0
	return nil
}
func (b *tidyV3ProofBudget) beginRawSample() error {
	if b == nil {
		return errTidyV3ProofLimit
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failed != nil {
		return b.failed
	}
	if b.observations == 0 || b.currentRaw {
		b.failed = errTidyV3ProofLimit
		return b.failed
	}
	b.rawProcesses++
	b.currentRaw = true
	return nil
}
func (b *tidyV3ProofBudget) beginUserPass() error     { return b.beginPass(false) }
func (b *tidyV3ProofBudget) beginMetadataPass() error { return b.beginPass(true) }
func (b *tidyV3ProofBudget) beginPass(metadata bool) error {
	if b == nil {
		return errTidyV3ProofLimit
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failed != nil {
		return b.failed
	}
	if b.observations == 0 || (!metadata && b.currentUserPasses >= 3) || (metadata && b.currentMetadataPasses >= 4) {
		b.failed = errTidyV3ProofLimit
		return b.failed
	}
	if metadata {
		b.currentMetadataPasses++
		b.metadataPasses++
	} else {
		b.currentUserPasses++
		b.userPasses++
	}
	return nil
}
func (b *tidyV3ProofBudget) consume(channel int, n int64) error {
	if b == nil || channel < 0 || channel >= proofChannels {
		return errTidyV3ProofLimit
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if n < 0 || n > math.MaxInt64-b.counts[channel] || n > math.MaxInt64-b.total {
		b.failed = errTidyV3ProofLimit
		return b.failed
	}
	b.counts[channel] += n
	b.total += n
	if b.counts[channel] > b.limits[channel]-b.reserved[channel] || b.total > b.totalLimit-b.totalReserved {
		b.failed = errTidyV3ProofLimit
	}
	return b.failed
}
func (b *tidyV3ProofBudget) consumeUserBytes(n int64) error     { return b.consume(proofUser, n) }
func (b *tidyV3ProofBudget) consumeMetadataBytes(n int64) error { return b.consume(proofMetadata, n) }
func (b *tidyV3ProofBudget) consumeFramingBytes(n int64) error  { return b.consume(proofFraming, n) }
func (b *tidyV3ProofBudget) consumeStderrBytes(n int64) error   { return b.consume(proofStderr, n) }
func (b *tidyV3ProofBudget) snapshot() tidyV3ProofCounters {
	if b == nil {
		return tidyV3ProofCounters{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return tidyV3ProofCounters{Observations: b.observations, RawProcesses: b.rawProcesses, UserPasses: b.userPasses, MetadataPasses: b.metadataPasses, UserBytes: b.counts[proofUser], MetadataBytes: b.counts[proofMetadata], FramingBytes: b.counts[proofFraming], StderrBytes: b.counts[proofStderr], TotalBytes: b.total, MaxReadBuffer: b.maxReadBuffer}
}

type tidyProofReader struct {
	ctx     context.Context
	budget  *tidyV3ProofBudget
	channel int
	source  io.Reader
}

func (b *tidyV3ProofBudget) reader(ctx context.Context, channel int, source io.Reader) io.Reader {
	return &tidyProofReader{ctx, b, channel, source}
}
func (r *tidyProofReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b := r.budget
	if b == nil || r.channel < 0 || r.channel >= proofChannels {
		return 0, errTidyV3ProofLimit
	}
	var allowance int64
	var detection bool
	for {
		if e := r.ctx.Err(); e != nil {
			return 0, e
		}
		b.mu.Lock()
		if b.failed != nil {
			e := b.failed
			b.mu.Unlock()
			return 0, e
		}
		channelRemaining := b.limits[r.channel] - b.counts[r.channel] - b.reserved[r.channel]
		totalRemaining := b.totalLimit - b.total - b.totalReserved
		remaining := min(channelRemaining, totalRemaining)
		allowance = min(int64(len(p)), remaining, 64*1024)
		if allowance <= 0 {
			// A reservation is not consumed evidence. Let the limiting reservations
			// settle before deciding whether the next byte is an overrun probe.
			pending := (channelRemaining <= 0 && b.reserved[r.channel] > 0) || (totalRemaining <= 0 && b.totalReserved > 0)
			if pending || b.detection {
				changed := b.changed
				b.mu.Unlock()
				select {
				case <-r.ctx.Done():
					return 0, r.ctx.Err()
				case <-changed:
					continue
				}
			}
			allowance = 1
			detection = true
			b.detection = true
		}
		b.reserved[r.channel] += allowance
		b.totalReserved += allowance
		if allowance > b.maxReadBuffer {
			b.maxReadBuffer = allowance
		}
		b.mu.Unlock()
		break
	}
	n, e := r.source.Read(p[:allowance])
	b.mu.Lock()
	b.reserved[r.channel] -= allowance
	b.totalReserved -= allowance
	b.counts[r.channel] += int64(n)
	b.total += int64(n)
	if detection {
		b.detection = false
	}
	if b.counts[r.channel] > b.limits[r.channel] || b.total > b.totalLimit {
		b.failed = errTidyV3ProofLimit
		e = b.failed
	}
	close(b.changed)
	b.changed = make(chan struct{})
	b.mu.Unlock()
	return n, e
}
