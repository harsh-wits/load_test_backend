package testing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"seller_app_load_tester/internal/config"
	"seller_app_load_tester/internal/domain/latency"
	"seller_app_load_tester/internal/domain/session"
	"seller_app_load_tester/internal/shared/apierror"
)

type noopStore struct{}

func (n *noopStore) CreateSession(_ context.Context, _ *session.Session) error { return nil }
func (n *noopStore) GetSession(_ context.Context, _ string) (*session.Session, error) {
	return nil, nil
}
func (n *noopStore) DeleteSession(_ context.Context, _ string) error { return nil }
func (n *noopStore) GetSessionByBPP(_ context.Context, _ string) (*session.Session, error) {
	return nil, nil
}
func (n *noopStore) SetCatalogState(_ context.Context, _ string, _ *session.CatalogState) error {
	return nil
}
func (n *noopStore) GetCatalogState(_ context.Context, _ string) (*session.CatalogState, error) {
	return nil, nil
}
func (n *noopStore) SetPreorderState(_ context.Context, _ string, _ *session.PreorderState) error {
	return nil
}
func (n *noopStore) GetPreorderState(_ context.Context, _ string) (*session.PreorderState, error) {
	return nil, nil
}
func (n *noopStore) CreateRun(_ context.Context, _ *session.Run) error           { return nil }
func (n *noopStore) GetRun(_ context.Context, _ string) (*session.Run, error)    { return nil, nil }
func (n *noopStore) UpdateRunStatus(_ context.Context, _, _ string) error        { return nil }
func (n *noopStore) IncrMetric(_ context.Context, _, _, _ string, _ int64) error { return nil }
func (n *noopStore) GetMetrics(_ context.Context, _ string) (*session.RunMetrics, error) {
	return nil, nil
}
func (n *noopStore) LinkTxn(_ context.Context, _, _, _ string, _ bool) error { return nil }
func (n *noopStore) GetTxnRoute(_ context.Context, _ string) (string, string, bool, error) {
	return "", "", false, nil
}
func (n *noopStore) SetDiscoveryPayload(_ context.Context, _ string, _ []byte) error { return nil }
func (n *noopStore) GetDiscoveryPayload(_ context.Context, _ string) ([]byte, error) { return nil, nil }
func (n *noopStore) SaveSession(_ context.Context, _ *session.Session) error         { return nil }
func (n *noopStore) SaveRun(_ context.Context, _ *session.Run) error                 { return nil }
func (n *noopStore) SaveCatalog(_ context.Context, _ string, _ []byte) error         { return nil }
func (n *noopStore) GetCatalog(_ context.Context, _ string) ([]byte, error)          { return nil, nil }
func (n *noopStore) GetRunHistory(_ context.Context, _ string) ([]*session.Run, error) {
	return nil, nil
}
func (n *noopStore) GetRunByID(_ context.Context, _ string) (*session.Run, error) { return nil, nil }
func (n *noopStore) GetSessionsByBPP(_ context.Context, _ string, _, _ int) ([]*session.Session, int64, error) {
	return nil, 0, nil
}
func (n *noopStore) GetSessionByID(_ context.Context, _ string) (*session.Session, error) {
	return nil, nil
}
func (n *noopStore) ExpireSessionsByBPP(_ context.Context, _ string) error { return nil }
func (n *noopStore) HardDeleteSession(_ context.Context, _ string) error   { return nil }
func (n *noopStore) HardDeleteSessionsByBPP(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (n *noopStore) SaveRunPayload(_ context.Context, _ *session.RunPayload) error { return nil }
func (n *noopStore) GetRunPayloads(_ context.Context, _ string) ([]*session.RunPayload, error) {
	return nil, nil
}

func (n *noopStore) UpsertRunLatencyEvent(_ context.Context, _ *latency.RunLatencyEvent) error {
	return nil
}
func (n *noopStore) UpsertRunLatencyEventsBulk(_ context.Context, _ []*latency.RunLatencyEvent) error {
	return nil
}
func (n *noopStore) UpsertRunLatencySummary(_ context.Context, _ *latency.RunLatencySummary) error {
	return nil
}
func (n *noopStore) GetRunLatencyEvents(_ context.Context, _ string, _ latency.Stage, _ []string) (map[string]*latency.RunLatencyEvent, error) {
	return map[string]*latency.RunLatencyEvent{}, nil
}
func (n *noopStore) GetRunLatencyEventsForStage(_ context.Context, _ string, _ latency.Stage) (map[string]*latency.RunLatencyEvent, error) {
	return map[string]*latency.RunLatencyEvent{}, nil
}
func (n *noopStore) GetRunLatencySummaries(_ context.Context, _ string) (map[latency.Stage]*latency.RunLatencySummary, error) {
	return map[latency.Stage]*latency.RunLatencySummary{}, nil
}

func newTestController() *Controller {
	cfg := &config.Config{
		CoreVersion: "1.2.0",
		Domain:      "ONDC:RET10",
	}
	mgr := session.NewManager(&noopStore{}, &noopStore{}, int(time.Hour.Seconds()))
	return NewController(cfg, mgr, nil, nil, nil, nil, nil)
}

func TestCreateSessionDefaultsFromConfig(t *testing.T) {
	ctrl := newTestController()
	app := fiber.New(fiber.Config{ErrorHandler: apierror.ErrorHandler()})
	ctrl.Register(app)

	body := map[string]any{
		"bpp_id":  "bpp-1",
		"bpp_uri": "https://bpp.example.com",
	}
	buf, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/sessions", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("expected status 201, got %d", resp.StatusCode)
	}
	var created session.Session
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !created.ErrorInjectionEnabled {
		t.Fatalf("expected error_injection_enabled true by default")
	}
}

type toggleStore struct {
	noopStore
	sess     *session.Session
	preorder *session.PreorderState
}

func (s *toggleStore) CreateSession(_ context.Context, in *session.Session) error {
	if in == nil {
		return nil
	}
	cp := *in
	s.sess = &cp
	return nil
}

func (s *toggleStore) GetSession(_ context.Context, id string) (*session.Session, error) {
	if s.sess == nil || s.sess.ID != id {
		return nil, fmt.Errorf("session not found")
	}
	cp := *s.sess
	return &cp, nil
}

func (s *toggleStore) GetSessionByID(_ context.Context, id string) (*session.Session, error) {
	return s.GetSession(context.Background(), id)
}

func (s *toggleStore) GetPreorderState(_ context.Context, _ string) (*session.PreorderState, error) {
	if s.preorder == nil {
		return &session.PreorderState{Status: session.PreorderIdle}, nil
	}
	cp := *s.preorder
	return &cp, nil
}

func TestSetSessionErrorInjectionRejectsWhenRunActive(t *testing.T) {
	store := &toggleStore{
		sess: &session.Session{
			ID:        "s-1",
			BPPID:     "bpp-1",
			BPPURI:    "https://bpp.example.com",
			Status:    session.SessionActive,
			CreatedAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		},
		preorder: &session.PreorderState{Status: session.PreorderRunning, ActiveRunID: "r-1"},
	}
	mgr := session.NewManager(store, store, int(time.Hour.Seconds()))
	ctrl := NewController(&config.Config{CoreVersion: "1.2.0", Domain: "ONDC:RET10"}, mgr, nil, nil, nil, nil, nil)
	app := fiber.New(fiber.Config{ErrorHandler: apierror.ErrorHandler()})
	ctrl.Register(app)

	req := httptest.NewRequest("PUT", "/sessions/s-1/error_injection", bytes.NewReader([]byte(`{"enabled":true}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 409 {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}
}

func TestSetSessionErrorInjectionUpdatesSession(t *testing.T) {
	store := &toggleStore{
		sess: &session.Session{
			ID:        "s-1",
			BPPID:     "bpp-1",
			BPPURI:    "https://bpp.example.com",
			Status:    session.SessionActive,
			CreatedAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		},
		preorder: &session.PreorderState{Status: session.PreorderIdle},
	}
	mgr := session.NewManager(store, store, int(time.Hour.Seconds()))
	ctrl := NewController(&config.Config{CoreVersion: "1.2.0", Domain: "ONDC:RET10"}, mgr, nil, nil, nil, nil, nil)
	app := fiber.New(fiber.Config{ErrorHandler: apierror.ErrorHandler()})
	ctrl.Register(app)

	req := httptest.NewRequest("PUT", "/sessions/s-1/error_injection", bytes.NewReader([]byte(`{"enabled":true}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	var updated session.Session
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !updated.ErrorInjectionEnabled {
		t.Fatalf("expected error_injection_enabled true")
	}
}

type reportStore struct {
	noopStore
	session *session.Session
	run     *session.Run
	metrics *session.RunMetrics
	sums    map[latency.Stage]*latency.RunLatencySummary
}

func (r *reportStore) GetSession(_ context.Context, id string) (*session.Session, error) {
	if r.session == nil || r.session.ID != id {
		return nil, fmt.Errorf("session not found")
	}
	return r.session, nil
}

func (r *reportStore) GetRun(_ context.Context, runID string) (*session.Run, error) {
	if r.run == nil || r.run.ID != runID {
		return nil, fmt.Errorf("run not found")
	}
	return r.run, nil
}

func (r *reportStore) GetMetrics(_ context.Context, runID string) (*session.RunMetrics, error) {
	if r.run == nil || r.run.ID != runID || r.metrics == nil {
		return nil, nil
	}
	return r.metrics, nil
}

func (r *reportStore) GetRunLatencySummaries(_ context.Context, runID string) (map[latency.Stage]*latency.RunLatencySummary, error) {
	if r.run == nil || r.run.ID != runID {
		return map[latency.Stage]*latency.RunLatencySummary{}, nil
	}
	return r.sums, nil
}

func TestGetRunReportReturnsComprehensiveJSON(t *testing.T) {
	sess := &session.Session{
		ID:                  "s-1",
		BPPID:               "bpp-1",
		BPPURI:              "https://bpp.example.com",
		Status:              session.SessionActive,
		VerificationEnabled: true,
		CreatedAt:           time.Now().UTC(),
		ExpiresAt:           time.Now().UTC().Add(time.Hour),
		CoreVersion:         "1.2.0",
		Domain:              "ONDC:RET10",
	}
	run := &session.Run{
		ID:          "r-1",
		SessionID:   "s-1",
		BPPID:       "bpp-1",
		RPS:         10,
		DurationSec: 2,
		Status:      "completed",
		SystemMetrics: session.RunSystemMetrics{
			Throttle: session.ThrottleMetrics{
				TargetRPS: 10,
				Allowed:   20,
			},
		},
	}
	metrics := &session.RunMetrics{
		Select:   session.ActionMetrics{Sent: 20, Success: 20},
		OnSelect: session.ActionMetrics{Sent: 20, Success: 19, Timeout: 1, AvgMS: 15},
	}
	sums := map[latency.Stage]*latency.RunLatencySummary{
		latency.StageOnSelect: {
			Stage:        latency.StageOnSelect,
			Total:        20,
			SuccessCount: 19,
			TimeoutCount: 1,
			AvgMS:        15,
		},
	}

	store := &reportStore{session: sess, run: run, metrics: metrics, sums: sums}
	mgr := session.NewManager(store, store, int(time.Hour.Seconds()))
	ctrl := NewController(&config.Config{CoreVersion: "1.2.0", Domain: "ONDC:RET10"}, mgr, nil, nil, nil, nil, nil)
	app := fiber.New(fiber.Config{ErrorHandler: apierror.ErrorHandler()})
	ctrl.Register(app)

	req := httptest.NewRequest("GET", "/sessions/s-1/runs/r-1/report", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, key := range []string{"session", "journey", "run", "system", "generated_at"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("expected key %q in report response", key)
		}
	}
	journey, _ := body["journey"].(map[string]any)
	for _, k := range []string{"select", "init", "confirm"} {
		if journey == nil || journey[k] == nil {
			t.Fatalf("expected journey.%s in report", k)
		}
	}
}

func TestGetRunReportRejectsRunOutsideSession(t *testing.T) {
	sess := &session.Session{
		ID:        "s-1",
		BPPID:     "bpp-1",
		BPPURI:    "https://bpp.example.com",
		Status:    session.SessionActive,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	run := &session.Run{
		ID:        "r-1",
		SessionID: "s-2",
		Status:    "completed",
	}

	store := &reportStore{session: sess, run: run, metrics: &session.RunMetrics{}}
	mgr := session.NewManager(store, store, int(time.Hour.Seconds()))
	ctrl := NewController(&config.Config{CoreVersion: "1.2.0", Domain: "ONDC:RET10"}, mgr, nil, nil, nil, nil, nil)
	app := fiber.New(fiber.Config{ErrorHandler: apierror.ErrorHandler()})
	ctrl.Register(app)

	req := httptest.NewRequest("GET", "/sessions/s-1/runs/r-1/report", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestGetRunReportByRunIDWithoutSessionID(t *testing.T) {
	sess := &session.Session{
		ID:        "s-1",
		BPPID:     "bpp-1",
		BPPURI:    "https://bpp.example.com",
		Status:    session.SessionActive,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	run := &session.Run{
		ID:          "r-1",
		SessionID:   "s-1",
		BPPID:       "bpp-1",
		RPS:         5,
		DurationSec: 2,
		Status:      "completed",
	}
	store := &reportStore{session: sess, run: run, metrics: &session.RunMetrics{}, sums: map[latency.Stage]*latency.RunLatencySummary{}}
	mgr := session.NewManager(store, store, int(time.Hour.Seconds()))
	ctrl := NewController(&config.Config{CoreVersion: "1.2.0", Domain: "ONDC:RET10"}, mgr, nil, nil, nil, nil, nil)
	app := fiber.New(fiber.Config{ErrorHandler: apierror.ErrorHandler()})
	ctrl.Register(app)

	req := httptest.NewRequest("GET", "/runs/r-1/report", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}
