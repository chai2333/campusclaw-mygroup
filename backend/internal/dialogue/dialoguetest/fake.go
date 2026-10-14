// Package dialoguetest provides a deterministic in-memory dialogue gateway
// for orchestration and API tests, so no external model access is required.
package dialoguetest

import (
	"context"
	"strings"
	"time"

	"campusclaw/internal/dialogue"
)

// FakeGateway replays canned delta fragments and records every request so
// orchestration tests can assert on the exact model input.
type FakeGateway struct {
	Deltas []string      // fragments emitted in order; the returned full text is their concatenation
	Delay  time.Duration // pause before each delta, for cancellation tests
	Err    error         // returned after all deltas have been emitted
	Full   string        // optional override of the returned accumulated text

	Calls       int
	LastRequest dialogue.Request
	DeltaCalls  int
}

func (f *FakeGateway) Stream(ctx context.Context, req dialogue.Request, onDelta func(text string) error) (string, error) {
	f.Calls++
	f.LastRequest = req
	var accumulated strings.Builder
	deltas := f.Deltas
	if len(deltas) == 0 && f.Full != "" {
		deltas = []string{f.Full}
	}
	for _, d := range deltas {
		if err := ctx.Err(); err != nil {
			return accumulated.String(), err
		}
		if f.Delay > 0 {
			select {
			case <-ctx.Done():
				return accumulated.String(), ctx.Err()
			case <-time.After(f.Delay):
			}
		}
		f.DeltaCalls++
		if onDelta != nil {
			if err := onDelta(d); err != nil {
				return accumulated.String(), err
			}
		}
		accumulated.WriteString(d)
	}
	if f.Err != nil {
		return accumulated.String(), f.Err
	}
	if f.Full != "" {
		return f.Full, nil
	}
	return accumulated.String(), nil
}
