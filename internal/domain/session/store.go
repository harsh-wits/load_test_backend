package session

import (
	"context"

	"seller_app_load_tester/internal/domain/latency"
)

type StateStore interface {
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	DeleteSession(ctx context.Context, id string) error
	GetSessionByBPP(ctx context.Context, bppID string) (*Session, error)

	SetCatalogState(ctx context.Context, sessionID string, state *CatalogState) error
	GetCatalogState(ctx context.Context, sessionID string) (*CatalogState, error)

	SetPreorderState(ctx context.Context, sessionID string, state *PreorderState) error
	GetPreorderState(ctx context.Context, sessionID string) (*PreorderState, error)

	CreateRun(ctx context.Context, r *Run) error
	GetRun(ctx context.Context, runID string) (*Run, error)
	UpdateRunStatus(ctx context.Context, runID, status string) error

	IncrMetric(ctx context.Context, runID, action, field string, delta int64) error
	GetMetrics(ctx context.Context, runID string) (*RunMetrics, error)

	LinkTxn(ctx context.Context, txnID, runID, sessionID string) error
	GetTxnRoute(ctx context.Context, txnID string) (runID, sessionID string, err error)

	SetDiscoveryPayload(ctx context.Context, txnID string, payload []byte) error
	GetDiscoveryPayload(ctx context.Context, txnID string) ([]byte, error)
}

type PersistStore interface {
	SaveSession(ctx context.Context, s *Session) error
	SaveRun(ctx context.Context, r *Run) error
	SaveCatalog(ctx context.Context, sessionID string, payload []byte) error
	GetCatalog(ctx context.Context, sessionID string) ([]byte, error)
	GetRunHistory(ctx context.Context, sessionID string) ([]*Run, error)
	GetRunByID(ctx context.Context, runID string) (*Run, error)
	GetSessionsByBPP(ctx context.Context, bppID string, page, pageSize int) ([]*Session, int64, error)
	GetSessionByID(ctx context.Context, id string) (*Session, error)
	ExpireSessionsByBPP(ctx context.Context, bppID string) error
	HardDeleteSession(ctx context.Context, id string) error
	HardDeleteSessionsByBPP(ctx context.Context, bppID string) (int64, error)
	SaveRunPayload(ctx context.Context, p *RunPayload) error
	GetRunPayloads(ctx context.Context, runID string) ([]*RunPayload, error)

	UpsertRunLatencyEvent(ctx context.Context, e *latency.RunLatencyEvent) error
	UpsertRunLatencySummary(ctx context.Context, s *latency.RunLatencySummary) error
	GetRunLatencyEvents(ctx context.Context, runID string, stage latency.Stage, txnIDs []string) (map[string]*latency.RunLatencyEvent, error)
	GetRunLatencySummaries(ctx context.Context, runID string) (map[latency.Stage]*latency.RunLatencySummary, error)
}
