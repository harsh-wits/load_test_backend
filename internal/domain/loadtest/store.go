package loadtest

import (
	"fmt"
	"sync"
	"time"

	"seller_app_load_tester/internal/domain/latency"
)

type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusStopped   Status = "stopped"
	StatusError     Status = "error"
)

const sloThresholdMs = 350

// Run holds the live state of a single load run.
type Run struct {
	ID       string
	Action   string
	Planned  int
	Started  time.Time

	mu        sync.Mutex
	status    Status
	dispatched int
	ack        int
	nack       int
	errCount   int
	latencies  []int64
	ledger     []LedgerRow
	endedAt    time.Time
	cancel     func()
}

// SetCancel wires the cancel function used by Stop.
func (r *Run) SetCancel(fn func()) { r.cancel = fn }

func (r *Run) markDispatched() {
	r.mu.Lock()
	r.dispatched++
	r.mu.Unlock()
}

func (r *Run) record(row LedgerRow) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch row.AckStatus {
	case "ACK":
		r.ack++
	case "NACK":
		r.nack++
	default:
		r.errCount++
	}
	if row.Error == "" {
		r.latencies = append(r.latencies, row.LatencyMs)
	}
	r.ledger = append(r.ledger, row)
}

func (r *Run) setStatus(s Status) {
	r.mu.Lock()
	r.status = s
	if s != StatusRunning && r.endedAt.IsZero() {
		r.endedAt = time.Now()
	}
	r.mu.Unlock()
}

// Stop requests cancellation of the run.
func (r *Run) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.setStatus(StatusStopped)
}

// Ledger returns a copy of the recorded rows.
func (r *Run) Ledger() []LedgerRow {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LedgerRow, len(r.ledger))
	copy(out, r.ledger)
	return out
}

// Snapshot renders the live metrics view for GET /loadtest/:id.
func (r *Run) Snapshot() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()

	completed := r.ack + r.nack + r.errCount
	end := r.endedAt
	if end.IsZero() {
		end = time.Now()
	}
	elapsed := end.Sub(r.Started).Seconds()
	achieved := 0.0
	if elapsed > 0 {
		achieved = float64(completed) / elapsed
	}

	avg, p90, p95, p99 := latency.ComputeSummaryFromSuccessLatenciesMs(r.latencies)
	p50, _, max := latency.ComputeP50P95MaxFromSuccessLatenciesMs(r.latencies)
	var min int64
	for i, v := range r.latencies {
		if i == 0 || v < min {
			min = v
		}
	}

	warnings := []string{}
	if completed > 0 {
		badRate := float64(r.nack+r.errCount) / float64(completed)
		if badRate > 0.05 {
			warnings = append(warnings, fmt.Sprintf("high NACK/error rate %.0f%% — check bap_uri registration, signing, and payload validity", badRate*100))
		}
	}

	pass := len(r.latencies) > 0 && p95 < sloThresholdMs
	return map[string]any{
		"run_id":           r.ID,
		"action":           r.Action,
		"status":           string(r.status),
		"planned_requests": r.Planned,
		"elapsed_sec":      round2(elapsed),
		"metrics": map[string]any{
			"sent":         r.dispatched,
			"ack":          r.ack,
			"nack":         r.nack,
			"error":        r.errCount,
			"inflight":     r.dispatched - completed,
			"achieved_qps": round2(achieved),
			"client_latency_ms": map[string]any{
				"avg": avg, "p50": p50, "p90": p90, "p95": p95, "p99": p99, "min": min, "max": max,
			},
		},
		"slo":      map[string]any{"metric": "p95", "threshold_ms": sloThresholdMs, "pass": pass, "note": "client-side cross-check; seller logs are authoritative"},
		"warnings": warnings,
	}
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

// Store is an in-memory registry of load runs.
type Store struct {
	mu   sync.RWMutex
	runs map[string]*Run
}

func NewStore() *Store { return &Store{runs: map[string]*Run{}} }

func (s *Store) Add(r *Run) {
	s.mu.Lock()
	s.runs[r.ID] = r
	s.mu.Unlock()
}

func (s *Store) Get(id string) (*Run, bool) {
	s.mu.RLock()
	r, ok := s.runs[id]
	s.mu.RUnlock()
	return r, ok
}
