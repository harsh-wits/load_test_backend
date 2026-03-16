package pipeline

import (
	"sync"
	"sync/atomic"
	"time"
)

type CallbackNotifier struct {
	mu       sync.Mutex
	counters map[string]*atomic.Int64
	channels map[string]chan struct{}
}

func NewCallbackNotifier() *CallbackNotifier {
	return &CallbackNotifier{
		counters: make(map[string]*atomic.Int64),
		channels: make(map[string]chan struct{}),
	}
}

func (n *CallbackNotifier) key(runID, action string) string {
	return runID + ":" + action
}

func (n *CallbackNotifier) getOrCreate(runID, action string) (*atomic.Int64, chan struct{}) {
	k := n.key(runID, action)
	n.mu.Lock()
	defer n.mu.Unlock()
	ctr, ok := n.counters[k]
	if !ok {
		ctr = &atomic.Int64{}
		n.counters[k] = ctr
	}
	ch, ok := n.channels[k]
	if !ok {
		ch = make(chan struct{}, 1)
		n.channels[k] = ch
	}
	return ctr, ch
}

func (n *CallbackNotifier) Notify(runID, action string) {
	ctr, ch := n.getOrCreate(runID, action)
	ctr.Add(1)
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (n *CallbackNotifier) WaitForCount(runID, action string, target int, timeout time.Duration) int {
	ctr, ch := n.getOrCreate(runID, action)
	if int(ctr.Load()) >= target {
		return int(ctr.Load())
	}

	deadline := time.After(timeout)
	for {
		select {
		case <-ch:
			if int(ctr.Load()) >= target {
				return int(ctr.Load())
			}
		case <-deadline:
			return int(ctr.Load())
		}
	}
}

func (n *CallbackNotifier) Reset(runID string) {
	prefix := runID + ":"
	n.mu.Lock()
	defer n.mu.Unlock()
	for k, ch := range n.channels {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			close(ch)
			delete(n.channels, k)
			delete(n.counters, k)
		}
	}
}
