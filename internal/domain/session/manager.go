package session

import (
	"context"
	"time"

	"github.com/google/uuid"

	"seller_app_load_tester/internal/shared/apierror"
)

type Manager struct {
	state      StateStore
	persist    PersistStore
	sessionTTL time.Duration
}

func NewManager(state StateStore, persist PersistStore, sessionTTLSeconds int) *Manager {
	if sessionTTLSeconds <= 0 {
		sessionTTLSeconds = 3600
	}
	return &Manager{
		state:      state,
		persist:    persist,
		sessionTTL: time.Duration(sessionTTLSeconds) * time.Second,
	}
}

func (m *Manager) Create(ctx context.Context, bppID, bppURI string) (*Session, error) {
	if existing, _ := m.state.GetSessionByBPP(ctx, bppID); existing != nil {
		_ = m.state.DeleteSession(ctx, existing.ID)
	}
	_ = m.persist.ExpireSessionsByBPP(ctx, bppID)

	now := time.Now().UTC()
	s := &Session{
		ID:        uuid.NewString(),
		BPPID:     bppID,
		BPPURI:    bppURI,
		Status:    SessionActive,
		CreatedAt: now,
		ExpiresAt: now.Add(m.sessionTTL),
	}

	if err := m.state.CreateSession(ctx, s); err != nil {
		return nil, err
	}
	if err := m.state.SetCatalogState(ctx, s.ID, &CatalogState{Status: CatalogNone, UpdatedAt: now}); err != nil {
		return nil, err
	}
	if err := m.state.SetPreorderState(ctx, s.ID, &PreorderState{Status: PreorderIdle}); err != nil {
		return nil, err
	}
	_ = m.persist.SaveSession(ctx, s)
	return s, nil
}

func (m *Manager) Get(ctx context.Context, id string) (*Session, error) {
	s, err := m.state.GetSession(ctx, id)
	if err != nil {
		return nil, apierror.ErrSessionNotFound
	}
	if s.Status == SessionExpired || time.Now().UTC().After(s.ExpiresAt) {
		return nil, apierror.ErrSessionExpired
	}
	return s, nil
}

func (m *Manager) GetAny(ctx context.Context, id string) (*Session, error) {
	s, err := m.state.GetSession(ctx, id)
	if err != nil {
		s, err = m.persist.GetSessionByID(ctx, id)
		if err != nil {
			return nil, apierror.ErrSessionNotFound
		}
	}
	if s.Status == SessionActive && time.Now().UTC().After(s.ExpiresAt) {
		s.Status = SessionExpired
		_ = m.persist.SaveSession(ctx, s)
	}
	return s, nil
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	_, err := m.state.GetSession(ctx, id)
	if err != nil {
		_, err = m.persist.GetSessionByID(ctx, id)
		if err != nil {
			return apierror.ErrSessionNotFound
		}
	} else {
		_ = m.state.DeleteSession(ctx, id)
	}
	return m.persist.HardDeleteSession(ctx, id)
}

func (m *Manager) DeleteAllByBPP(ctx context.Context, bppID string) (int64, error) {
	if active, _ := m.state.GetSessionByBPP(ctx, bppID); active != nil {
		_ = m.state.DeleteSession(ctx, active.ID)
	}
	return m.persist.HardDeleteSessionsByBPP(ctx, bppID)
}

func (m *Manager) SetCatalogState(ctx context.Context, sessionID string, state *CatalogState) error {
	return m.state.SetCatalogState(ctx, sessionID, state)
}

func (m *Manager) GetCatalogState(ctx context.Context, sessionID string) (*CatalogState, error) {
	return m.state.GetCatalogState(ctx, sessionID)
}

func (m *Manager) SetPreorderState(ctx context.Context, sessionID string, state *PreorderState) error {
	return m.state.SetPreorderState(ctx, sessionID, state)
}

func (m *Manager) GetPreorderState(ctx context.Context, sessionID string) (*PreorderState, error) {
	return m.state.GetPreorderState(ctx, sessionID)
}

func (m *Manager) StartRun(ctx context.Context, sessionID, bppID string, rps, durationSec int) (*Run, error) {
	ps, err := m.state.GetPreorderState(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if ps.Status == PreorderRunning {
		return nil, apierror.ErrRunAlreadyActive
	}

	now := time.Now().UTC()
	r := &Run{
		ID:          uuid.NewString(),
		SessionID:   sessionID,
		BPPID:       bppID,
		RPS:         rps,
		DurationSec: durationSec,
		Status:      "running",
		StartedAt:   now,
	}
	if err := m.state.CreateRun(ctx, r); err != nil {
		return nil, err
	}
	if err := m.state.SetPreorderState(ctx, sessionID, &PreorderState{Status: PreorderRunning, ActiveRunID: r.ID}); err != nil {
		return nil, err
	}
	return r, nil
}

func (m *Manager) FinishRun(ctx context.Context, sessionID, runID, status string) error {
	_ = m.state.UpdateRunStatus(ctx, runID, status)
	_ = m.state.SetPreorderState(ctx, sessionID, &PreorderState{Status: PreorderIdle})

	metrics, _ := m.state.GetMetrics(ctx, runID)
	run, _ := m.state.GetRun(ctx, runID)
	if run != nil {
		run.Status = status
		run.CompletedAt = time.Now().UTC()
		if metrics != nil {
			run.Metrics = *metrics
		}
		_ = m.persist.SaveRun(ctx, run)
	}
	return nil
}

func (m *Manager) GetRun(ctx context.Context, runID string) (*Run, error) {
	r, err := m.state.GetRun(ctx, runID)
	if err != nil {
		return m.persist.GetRunByID(ctx, runID)
	}
	metrics, _ := m.state.GetMetrics(ctx, runID)
	if metrics != nil {
		r.Metrics = *metrics
	}
	return r, nil
}

func (m *Manager) StopRun(ctx context.Context, sessionID string) (string, error) {
	ps, err := m.state.GetPreorderState(ctx, sessionID)
	if err != nil || ps.Status != PreorderRunning || ps.ActiveRunID == "" {
		return "", apierror.ErrNoActiveRun
	}
	_ = m.state.SetPreorderState(ctx, sessionID, &PreorderState{Status: PreorderStopping, ActiveRunID: ps.ActiveRunID})
	_ = m.state.UpdateRunStatus(ctx, ps.ActiveRunID, "stopping")
	return ps.ActiveRunID, nil
}

func (m *Manager) LinkTxn(ctx context.Context, txnID, runID, sessionID string) error {
	return m.state.LinkTxn(ctx, txnID, runID, sessionID)
}

func (m *Manager) GetTxnRoute(ctx context.Context, txnID string) (string, string, error) {
	return m.state.GetTxnRoute(ctx, txnID)
}

func (m *Manager) IncrMetric(ctx context.Context, runID, action, field string, delta int64) error {
	return m.state.IncrMetric(ctx, runID, action, field, delta)
}

func (m *Manager) SetDiscoveryPayload(ctx context.Context, txnID string, payload []byte) error {
	return m.state.SetDiscoveryPayload(ctx, txnID, payload)
}

func (m *Manager) GetDiscoveryPayload(ctx context.Context, txnID string) ([]byte, error) {
	return m.state.GetDiscoveryPayload(ctx, txnID)
}

func (m *Manager) GetSessionsByBPP(ctx context.Context, bppID string, page, pageSize int) ([]*Session, int64, error) {
	sessions, total, err := m.persist.GetSessionsByBPP(ctx, bppID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now().UTC()
	activeInRedis, _ := m.state.GetSessionByBPP(ctx, bppID)
	for _, s := range sessions {
		if s.Status != SessionActive {
			continue
		}
		if activeInRedis != nil && activeInRedis.ID == s.ID {
			continue
		}
		if now.After(s.ExpiresAt) {
			s.Status = SessionExpired
			_ = m.persist.SaveSession(ctx, s)
		}
	}
	return sessions, total, nil
}

func (m *Manager) GetRunHistory(ctx context.Context, sessionID string) ([]*Run, error) {
	return m.persist.GetRunHistory(ctx, sessionID)
}

func (m *Manager) State() StateStore    { return m.state }
func (m *Manager) Persist() PersistStore { return m.persist }
