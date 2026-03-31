package session

import "time"

type SessionStatus string

const (
	SessionActive  SessionStatus = "active"
	SessionExpired SessionStatus = "expired"
)

type CatalogStatus string

const (
	CatalogNone    CatalogStatus = "none"
	CatalogPending CatalogStatus = "pending"
	CatalogReady   CatalogStatus = "ready"
	CatalogFailed  CatalogStatus = "failed"
)

type PreorderStatus string

const (
	PreorderIdle     PreorderStatus = "idle"
	PreorderRunning  PreorderStatus = "running"
	PreorderStopping PreorderStatus = "stopping"
)

type Session struct {
	ID                    string        `json:"id" bson:"_id"`
	BPPID                 string        `json:"bpp_id" bson:"bpp_id"`
	BPPURI                string        `json:"bpp_uri" bson:"bpp_uri"`
	Status                SessionStatus `json:"status" bson:"status"`
	CreatedAt             time.Time     `json:"created_at" bson:"created_at"`
	ExpiresAt             time.Time     `json:"expires_at" bson:"expires_at"`
	CoreVersion           string        `json:"core_version,omitempty" bson:"core_version,omitempty"`
	Domain                string        `json:"domain,omitempty" bson:"domain,omitempty"`
	VerificationEnabled   bool          `json:"verification_enabled" bson:"verification_enabled"`
	ErrorInjectionEnabled bool          `json:"error_injection_enabled" bson:"error_injection_enabled"`
}

type CatalogState struct {
	Status     CatalogStatus `json:"status" bson:"status"`
	OnSearchID string        `json:"on_search_id,omitempty" bson:"on_search_id,omitempty"`
	UpdatedAt  time.Time     `json:"updated_at" bson:"updated_at"`
}

type PreorderState struct {
	Status      PreorderStatus `json:"status" bson:"status"`
	ActiveRunID string         `json:"active_run_id,omitempty" bson:"active_run_id,omitempty"`
}

type Run struct {
	ID             string            `json:"id" bson:"_id"`
	SessionID      string            `json:"session_id" bson:"session_id"`
	BPPID          string            `json:"bpp_id" bson:"bpp_id"`
	RPS            int               `json:"rps" bson:"rps"`
	DurationSec    int               `json:"duration_sec" bson:"duration_sec"`
	Status         string            `json:"status" bson:"status"`
	Metrics        RunMetrics        `json:"metrics" bson:"metrics"`
	SystemMetrics  RunSystemMetrics  `json:"system_metrics" bson:"system_metrics"`
	JourneyMetrics RunJourneyMetrics `json:"journey_metrics" bson:"journey_metrics"`
	StartedAt      time.Time         `json:"started_at" bson:"started_at"`
	CompletedAt    time.Time         `json:"completed_at,omitempty" bson:"completed_at,omitempty"`
}

type RunSystemMetrics struct {
	Throttle ThrottleMetrics `json:"throttle" bson:"throttle"`
}

type ThrottleMetrics struct {
	TargetRPS    int   `json:"target_rps" bson:"target_rps"`
	AcquireCalls int64 `json:"acquire_calls" bson:"acquire_calls"`
	Allowed      int64 `json:"allowed" bson:"allowed"`
	Retries      int64 `json:"retries" bson:"retries"`
	DenyGlobal   int64 `json:"deny_global" bson:"deny_global"`
	DenySession  int64 `json:"deny_session" bson:"deny_session"`
	DenyOther    int64 `json:"deny_other" bson:"deny_other"`
	WaitTotalMS  int64 `json:"wait_total_ms" bson:"wait_total_ms"`
	WaitMaxMS    int64 `json:"wait_max_ms" bson:"wait_max_ms"`
}

type JourneyActionMetrics struct {
	Sent       int64   `json:"sent" bson:"sent"`
	Received   int64   `json:"received" bson:"received"`
	Success    int64   `json:"success" bson:"success"`
	Failure    int64   `json:"failure" bson:"failure"`
	Timeout    int64   `json:"timeout" bson:"timeout"`
	AvgMS      float64 `json:"avg_ms" bson:"avg_ms"`
	P90MS      float64 `json:"p90_ms" bson:"p90_ms"`
	P95MS      float64 `json:"p95_ms" bson:"p95_ms"`
	P99MS      float64 `json:"p99_ms" bson:"p99_ms"`
	SuccessPct float64 `json:"success_pct" bson:"success_pct"`
}

type RunJourneyMetrics struct {
	Select  JourneyActionMetrics `json:"select" bson:"select"`
	Init    JourneyActionMetrics `json:"init" bson:"init"`
	Confirm JourneyActionMetrics `json:"confirm" bson:"confirm"`
}

type ActionMetrics struct {
	Sent    int64   `json:"sent" bson:"sent"`
	Success int64   `json:"success" bson:"success"`
	Failure int64   `json:"failure" bson:"failure"`
	Timeout int64   `json:"timeout" bson:"timeout"`
	AvgMS   float64 `json:"avg_ms" bson:"avg_ms"`
	P90MS   float64 `json:"p90_ms" bson:"p90_ms"`
	P95MS   float64 `json:"p95_ms" bson:"p95_ms"`
	P99MS   float64 `json:"p99_ms" bson:"p99_ms"`
}

type RunMetrics struct {
	Select    ActionMetrics `json:"select" bson:"select"`
	OnSelect  ActionMetrics `json:"on_select" bson:"on_select"`
	Init      ActionMetrics `json:"init" bson:"init"`
	OnInit    ActionMetrics `json:"on_init" bson:"on_init"`
	Confirm   ActionMetrics `json:"confirm" bson:"confirm"`
	OnConfirm ActionMetrics `json:"on_confirm" bson:"on_confirm"`
}

type RunPayload struct {
	ID        string    `json:"id" bson:"_id"`
	RunID     string    `json:"run_id" bson:"run_id"`
	SessionID string    `json:"session_id" bson:"session_id"`
	Stage     string    `json:"stage" bson:"stage"`
	Direction string    `json:"direction" bson:"direction"`
	TxnID     string    `json:"txn_id" bson:"txn_id"`
	Status    int       `json:"status" bson:"status"`
	Timestamp time.Time `json:"timestamp" bson:"timestamp"`
	Body      []byte    `json:"body" bson:"body"`
}
