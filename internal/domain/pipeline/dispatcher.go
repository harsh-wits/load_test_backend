package pipeline

import (
	"context"
	"sync"
)

type DispatchResult struct {
	Index int
	TxnID string
	Err   error
}

type Throttle interface {
	Acquire(ctx context.Context, sessionID string) bool
}

func DispatchConcurrent(
	ctx context.Context,
	payloads [][]byte,
	maxInFlight int,
	sendFn func(ctx context.Context, index int, payload []byte) DispatchResult,
) []DispatchResult {
	return DispatchConcurrentThrottled(ctx, payloads, maxInFlight, nil, "", sendFn)
}

func DispatchConcurrentThrottled(
	ctx context.Context,
	payloads [][]byte,
	maxInFlight int,
	throttle Throttle,
	sessionID string,
	sendFn func(ctx context.Context, index int, payload []byte) DispatchResult,
) []DispatchResult {
	if maxInFlight <= 0 {
		maxInFlight = 256
	}

	results := make([]DispatchResult, len(payloads))
	sem := make(chan struct{}, maxInFlight)
	var wg sync.WaitGroup

outer:
	for i, p := range payloads {
		if ctx.Err() != nil {
			for j := i; j < len(payloads); j++ {
				results[j] = DispatchResult{Index: j, Err: ctx.Err()}
			}
			break
		}

		if throttle != nil {
			if !throttle.Acquire(ctx, sessionID) {
				results[i] = DispatchResult{Index: i, Err: context.DeadlineExceeded}
				continue
			}
		}

		select {
		case <-ctx.Done():
			for j := i; j < len(payloads); j++ {
				results[j] = DispatchResult{Index: j, Err: ctx.Err()}
			}
			break outer
		case sem <- struct{}{}:
		}

		if ctx.Err() != nil {
			for j := i; j < len(payloads); j++ {
				results[j] = DispatchResult{Index: j, Err: ctx.Err()}
			}
			break
		}

		wg.Add(1)
		go func(idx int, payload []byte) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = sendFn(ctx, idx, payload)
		}(i, p)
	}

	wg.Wait()
	return results
}
