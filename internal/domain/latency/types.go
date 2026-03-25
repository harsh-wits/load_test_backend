package latency

import "time"

type Stage string

const (
	StageOnSelect  Stage = "on_select"
	StageOnInit    Stage = "on_init"
	StageOnConfirm Stage = "on_confirm"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeTimeout Outcome = "timeout"
)

type RunLatencyEvent struct {
	SessionID   string     `json:"session_id" bson:"session_id"`
	RunID       string     `json:"run_id" bson:"run_id"`
	Stage       Stage      `json:"stage" bson:"stage"`
	TxnID       string     `json:"txn_id" bson:"txn_id"`
	SentAt      time.Time  `json:"sent_at" bson:"sent_at"`
	ReceivedAt  *time.Time `json:"received_at,omitempty" bson:"received_at,omitempty"`
	LatencyMS   *int64     `json:"latency_ms,omitempty" bson:"latency_ms,omitempty"`
	Outcome     Outcome    `json:"outcome" bson:"outcome"`
	RecordedAt  time.Time  `json:"recorded_at" bson:"recorded_at"`
	TimeoutCause string    `json:"timeout_cause,omitempty" bson:"timeout_cause,omitempty"`
}

type RunLatencySummary struct {
	SessionID           string    `json:"session_id" bson:"session_id"`
	RunID               string    `json:"run_id" bson:"run_id"`
	Stage               Stage     `json:"stage" bson:"stage"`
	TimeoutThresholdMS  int64     `json:"timeout_threshold_ms" bson:"timeout_threshold_ms"`
	CutoffAt            time.Time `json:"cutoff_at" bson:"cutoff_at"`
	Total               int64     `json:"total" bson:"total"`
	SuccessCount        int64     `json:"success_count" bson:"success_count"`
	FailureCount        int64     `json:"failure_count" bson:"failure_count"`
	TimeoutCount        int64     `json:"timeout_count" bson:"timeout_count"`
	AvgMS               float64   `json:"avg_ms" bson:"avg_ms"`
	P90MS               float64   `json:"p90_ms" bson:"p90_ms"`
	P95MS               float64   `json:"p95_ms" bson:"p95_ms"`
	P99MS               float64   `json:"p99_ms" bson:"p99_ms"`
	ComputedAt          time.Time `json:"computed_at" bson:"computed_at"`
}

