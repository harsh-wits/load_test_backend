package session

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"seller_app_load_tester/internal/domain/latency"
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

func (m *Manager) Create(ctx context.Context, bppID, bppURI, coreVersion, domain string) (*Session, error) {
	if existing, _ := m.state.GetSessionByBPP(ctx, bppID); existing != nil {
		_ = m.state.DeleteSession(ctx, existing.ID)
	}
	_ = m.persist.ExpireSessionsByBPP(ctx, bppID)

	now := time.Now().UTC()
	s := &Session{
		ID:                    uuid.NewString(),
		BPPID:                 bppID,
		BPPURI:                bppURI,
		Status:                SessionActive,
		CreatedAt:             now,
		ExpiresAt:             now.Add(m.sessionTTL),
		CoreVersion:           coreVersion,
		Domain:                domain,
		VerificationEnabled:   false, // default: disabled; can be enabled per-session via API
		ErrorInjectionEnabled: true,  // default: enabled; can be disabled per-session via API
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

func (m *Manager) SetVerificationEnabled(ctx context.Context, sessionID string, enabled bool) (*Session, error) {
	s, err := m.GetAny(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	s.VerificationEnabled = enabled

	// Overwrite Redis state (CreateSession uses SET and is safe for updates).
	if err := m.state.CreateSession(ctx, s); err != nil {
		return nil, err
	}
	// Best-effort persistence to Mongo.
	_ = m.persist.SaveSession(ctx, s)
	return s, nil
}

func (m *Manager) SetErrorInjectionEnabled(ctx context.Context, sessionID string, enabled bool) (*Session, error) {
	s, err := m.GetAny(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	s.ErrorInjectionEnabled = enabled
	if err := m.state.CreateSession(ctx, s); err != nil {
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
		m.mergeRunLatencySummaries(ctx, run)
		run.JourneyMetrics = buildRunJourneyMetrics(run.Metrics)
		_ = m.persist.SaveRun(ctx, run)
	}
	return nil
}

func (m *Manager) mergeRunLatencySummaries(ctx context.Context, run *Run) {
	if m.persist == nil || run == nil {
		return
	}
	if run.Status != "completed" && run.Status != "failed" {
		return
	}

	sums, err := m.persist.GetRunLatencySummaries(ctx, run.ID)
	if err != nil {
		log.Printf("[run] get latency summaries FAILED run=%s error=%v", run.ID, err)
		return
	}

	if s := sums[latency.StageOnSelect]; s != nil {
		run.Metrics.OnSelect = ActionMetrics{
			Sent:    s.Total,
			Success: s.SuccessCount,
			Failure: s.FailureCount,
			Timeout: s.TimeoutCount,
			AvgMS:   s.AvgMS,
			P90MS:   s.P90MS,
			P95MS:   s.P95MS,
			P99MS:   s.P99MS,
		}
	}
	if s := sums[latency.StageOnInit]; s != nil {
		run.Metrics.OnInit = ActionMetrics{
			Sent:    s.Total,
			Success: s.SuccessCount,
			Failure: s.FailureCount,
			Timeout: s.TimeoutCount,
			AvgMS:   s.AvgMS,
			P90MS:   s.P90MS,
			P95MS:   s.P95MS,
			P99MS:   s.P99MS,
		}
	}
	if s := sums[latency.StageOnConfirm]; s != nil {
		run.Metrics.OnConfirm = ActionMetrics{
			Sent:    s.Total,
			Success: s.SuccessCount,
			Failure: s.FailureCount,
			Timeout: s.TimeoutCount,
			AvgMS:   s.AvgMS,
			P90MS:   s.P90MS,
			P95MS:   s.P95MS,
			P99MS:   s.P99MS,
		}
	}
}

func (m *Manager) GetRun(ctx context.Context, runID string) (*Run, error) {
	r, err := m.state.GetRun(ctx, runID)
	if err != nil {
		pr, perr := m.persist.GetRunByID(ctx, runID)
		if perr != nil {
			return nil, perr
		}
		pr.JourneyMetrics = buildRunJourneyMetrics(pr.Metrics)
		return pr, nil
	}
	metrics, _ := m.state.GetMetrics(ctx, runID)
	if metrics != nil {
		r.Metrics = *metrics
	}
	m.mergeRunLatencySummaries(ctx, r)
	r.JourneyMetrics = buildRunJourneyMetrics(r.Metrics)
	return r, nil
}

func buildRunJourneyMetrics(m RunMetrics) RunJourneyMetrics {
	build := func(req, cb ActionMetrics) JourneyActionMetrics {
		received := cb.Success + cb.Failure + cb.Timeout
		successPct := 0.0
		if req.Sent > 0 {
			successPct = (float64(cb.Success) / float64(req.Sent)) * 100
		}
		return JourneyActionMetrics{
			Sent:       req.Sent,
			Received:   received,
			Success:    cb.Success,
			Failure:    cb.Failure,
			Timeout:    cb.Timeout,
			AvgMS:      cb.AvgMS,
			P90MS:      cb.P90MS,
			P95MS:      cb.P95MS,
			P99MS:      cb.P99MS,
			SuccessPct: successPct,
		}
	}
	return RunJourneyMetrics{
		Select:  build(m.Select, m.OnSelect),
		Init:    build(m.Init, m.OnInit),
		Confirm: build(m.Confirm, m.OnConfirm),
	}
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

func (m *Manager) LinkTxn(ctx context.Context, txnID, runID, sessionID string, verificationEnabled bool) error {
	return m.state.LinkTxn(ctx, txnID, runID, sessionID, verificationEnabled)
}

func (m *Manager) GetTxnRoute(ctx context.Context, txnID string) (string, string, bool, error) {
	return m.state.GetTxnRoute(ctx, txnID)
}

func (m *Manager) IncrMetric(ctx context.Context, runID, action, field string, delta int64) error {
	return m.state.IncrMetric(ctx, runID, action, field, delta)
}

func (m *Manager) SetRunSystemMetrics(ctx context.Context, runID string, sys RunSystemMetrics) error {
	r, err := m.state.GetRun(ctx, runID)
	if err != nil || r == nil {
		r, err = m.persist.GetRunByID(ctx, runID)
		if err != nil || r == nil {
			return err
		}
	}
	r.SystemMetrics = sys
	if err := m.state.CreateRun(ctx, r); err != nil {
		return err
	}
	_ = m.persist.SaveRun(ctx, r)
	return nil
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

func (m *Manager) State() StateStore     { return m.state }
func (m *Manager) Persist() PersistStore { return m.persist }
