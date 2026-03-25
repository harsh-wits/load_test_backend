package testing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"seller_app_load_tester/internal/config"
	"seller_app_load_tester/internal/domain/latency"
	"seller_app_load_tester/internal/domain/session"
)

type noopStore struct{}

func (n *noopStore) CreateSession(_ context.Context, _ *session.Session) error                           { return nil }
func (n *noopStore) GetSession(_ context.Context, _ string) (*session.Session, error)                    { return nil, nil }
func (n *noopStore) DeleteSession(_ context.Context, _ string) error                                     { return nil }
func (n *noopStore) GetSessionByBPP(_ context.Context, _ string) (*session.Session, error)               { return nil, nil }
func (n *noopStore) SetCatalogState(_ context.Context, _ string, _ *session.CatalogState) error          { return nil }
func (n *noopStore) GetCatalogState(_ context.Context, _ string) (*session.CatalogState, error)          { return nil, nil }
func (n *noopStore) SetPreorderState(_ context.Context, _ string, _ *session.PreorderState) error        { return nil }
func (n *noopStore) GetPreorderState(_ context.Context, _ string) (*session.PreorderState, error)        { return nil, nil }
func (n *noopStore) CreateRun(_ context.Context, _ *session.Run) error                                   { return nil }
func (n *noopStore) GetRun(_ context.Context, _ string) (*session.Run, error)                            { return nil, nil }
func (n *noopStore) UpdateRunStatus(_ context.Context, _, _ string) error                                { return nil }
func (n *noopStore) IncrMetric(_ context.Context, _, _, _ string, _ int64) error                         { return nil }
func (n *noopStore) GetMetrics(_ context.Context, _ string) (*session.RunMetrics, error)                 { return nil, nil }
func (n *noopStore) LinkTxn(_ context.Context, _, _, _ string) error                                     { return nil }
func (n *noopStore) GetTxnRoute(_ context.Context, _ string) (string, string, error)                     { return "", "", nil }
func (n *noopStore) SetDiscoveryPayload(_ context.Context, _ string, _ []byte) error                     { return nil }
func (n *noopStore) GetDiscoveryPayload(_ context.Context, _ string) ([]byte, error)                     { return nil, nil }
func (n *noopStore) SaveSession(_ context.Context, _ *session.Session) error                             { return nil }
func (n *noopStore) SaveRun(_ context.Context, _ *session.Run) error                                     { return nil }
func (n *noopStore) SaveCatalog(_ context.Context, _ string, _ []byte) error                             { return nil }
func (n *noopStore) GetCatalog(_ context.Context, _ string) ([]byte, error)                              { return nil, nil }
func (n *noopStore) GetRunHistory(_ context.Context, _ string) ([]*session.Run, error)                   { return nil, nil }
func (n *noopStore) GetRunByID(_ context.Context, _ string) (*session.Run, error)                        { return nil, nil }
func (n *noopStore) GetSessionsByBPP(_ context.Context, _ string, _, _ int) ([]*session.Session, int64, error) {
	return nil, 0, nil
}
func (n *noopStore) GetSessionByID(_ context.Context, _ string) (*session.Session, error) { return nil, nil }
func (n *noopStore) ExpireSessionsByBPP(_ context.Context, _ string) error                { return nil }
func (n *noopStore) HardDeleteSession(_ context.Context, _ string) error                  { return nil }
func (n *noopStore) HardDeleteSessionsByBPP(_ context.Context, _ string) (int64, error)   { return 0, nil }
func (n *noopStore) SaveRunPayload(_ context.Context, _ *session.RunPayload) error        { return nil }
func (n *noopStore) GetRunPayloads(_ context.Context, _ string) ([]*session.RunPayload, error) {
	return nil, nil
}

func (n *noopStore) UpsertRunLatencyEvent(_ context.Context, _ *latency.RunLatencyEvent) error {
	return nil
}
func (n *noopStore) UpsertRunLatencySummary(_ context.Context, _ *latency.RunLatencySummary) error {
	return nil
}
func (n *noopStore) GetRunLatencyEvents(_ context.Context, _ string, _ latency.Stage, _ []string) (map[string]*latency.RunLatencyEvent, error) {
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
	app := fiber.New()
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
}

