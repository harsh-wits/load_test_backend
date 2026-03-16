package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	domainSession "seller_app_load_tester/internal/domain/session"
	"seller_app_load_tester/internal/shared/redis"
)

type RedisStore struct {
	r   redis.Client
	ttl time.Duration
}

func NewRedisStore(r redis.Client, sessionTTLSeconds int) *RedisStore {
	if sessionTTLSeconds <= 0 {
		sessionTTLSeconds = 3600
	}
	return &RedisStore{r: r, ttl: time.Duration(sessionTTLSeconds) * time.Second}
}

func sessionKey(id string) string    { return "session:" + id }
func bppIndexKey(bppID string) string { return "bpp_session:" + bppID }
func catalogKey(id string) string     { return "session:" + id + ":catalog" }
func preorderKey(id string) string    { return "session:" + id + ":preorder" }
func runKey(id string) string         { return "run:" + id }
func metricKey(runID, action, field string) string {
	return fmt.Sprintf("run:%s:m:%s:%s", runID, action, field)
}
func txnKey(txnID string) string       { return "txn:" + txnID }
func discoveryKey(txnID string) string { return "discovery:" + txnID }

func (s *RedisStore) CreateSession(ctx context.Context, sess *domainSession.Session) error {
	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	if err := s.r.Set(ctx, sessionKey(sess.ID), data, s.ttl); err != nil {
		return err
	}
	return s.r.Set(ctx, bppIndexKey(sess.BPPID), []byte(sess.ID), s.ttl)
}

func (s *RedisStore) GetSession(ctx context.Context, id string) (*domainSession.Session, error) {
	data, err := s.r.Get(ctx, sessionKey(id))
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("session not found")
	}
	var sess domainSession.Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *RedisStore) DeleteSession(ctx context.Context, id string) error {
	sess, err := s.GetSession(ctx, id)
	if err != nil {
		return err
	}
	_ = s.r.Del(ctx, bppIndexKey(sess.BPPID))
	_ = s.r.Del(ctx, sessionKey(id))
	_ = s.r.Del(ctx, catalogKey(id))
	_ = s.r.Del(ctx, preorderKey(id))
	return nil
}

func (s *RedisStore) GetSessionByBPP(ctx context.Context, bppID string) (*domainSession.Session, error) {
	data, err := s.r.Get(ctx, bppIndexKey(bppID))
	if err != nil || data == nil {
		return nil, fmt.Errorf("no session for bpp")
	}
	return s.GetSession(ctx, string(data))
}

func (s *RedisStore) SetCatalogState(ctx context.Context, sessionID string, state *domainSession.CatalogState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.r.Set(ctx, catalogKey(sessionID), data, s.ttl)
}

func (s *RedisStore) GetCatalogState(ctx context.Context, sessionID string) (*domainSession.CatalogState, error) {
	data, err := s.r.Get(ctx, catalogKey(sessionID))
	if err != nil || data == nil {
		return &domainSession.CatalogState{Status: domainSession.CatalogNone}, nil
	}
	var cs domainSession.CatalogState
	if err := json.Unmarshal(data, &cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

func (s *RedisStore) SetPreorderState(ctx context.Context, sessionID string, state *domainSession.PreorderState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.r.Set(ctx, preorderKey(sessionID), data, s.ttl)
}

func (s *RedisStore) GetPreorderState(ctx context.Context, sessionID string) (*domainSession.PreorderState, error) {
	data, err := s.r.Get(ctx, preorderKey(sessionID))
	if err != nil || data == nil {
		return &domainSession.PreorderState{Status: domainSession.PreorderIdle}, nil
	}
	var ps domainSession.PreorderState
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, err
	}
	return &ps, nil
}

func (s *RedisStore) CreateRun(ctx context.Context, r *domainSession.Run) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.r.Set(ctx, runKey(r.ID), data, s.ttl)
}

func (s *RedisStore) GetRun(ctx context.Context, runID string) (*domainSession.Run, error) {
	data, err := s.r.Get(ctx, runKey(runID))
	if err != nil || data == nil {
		return nil, fmt.Errorf("run not found")
	}
	var r domainSession.Run
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *RedisStore) UpdateRunStatus(ctx context.Context, runID, status string) error {
	r, err := s.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	r.Status = status
	if status == "completed" || status == "failed" {
		r.CompletedAt = time.Now().UTC()
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.r.Set(ctx, runKey(runID), data, s.ttl)
}

func (s *RedisStore) IncrMetric(ctx context.Context, runID, action, field string, delta int64) error {
	k := metricKey(runID, action, field)
	_, err := s.r.IncrBy(ctx, k, delta)
	if err != nil {
		return err
	}
	_ = s.r.Expire(ctx, k, s.ttl)
	return nil
}

func (s *RedisStore) GetMetrics(ctx context.Context, runID string) (*domainSession.RunMetrics, error) {
	actions := []string{"select", "on_select", "init", "on_init", "confirm", "on_confirm"}
	fields := []string{"sent", "success", "failure", "timeout"}

	m := &domainSession.RunMetrics{}
	for _, a := range actions {
		am := domainSession.ActionMetrics{}
		for _, f := range fields {
			k := metricKey(runID, a, f)
			raw, err := s.r.Get(ctx, k)
			if err != nil || raw == nil {
				continue
			}
			v, _ := strconv.ParseInt(string(raw), 10, 64)
			switch f {
			case "sent":
				am.Sent = v
			case "success":
				am.Success = v
			case "failure":
				am.Failure = v
			case "timeout":
				am.Timeout = v
			}
		}
		switch a {
		case "select":
			m.Select = am
		case "on_select":
			m.OnSelect = am
		case "init":
			m.Init = am
		case "on_init":
			m.OnInit = am
		case "confirm":
			m.Confirm = am
		case "on_confirm":
			m.OnConfirm = am
		}
	}
	return m, nil
}

func (s *RedisStore) LinkTxn(ctx context.Context, txnID, runID, sessionID string) error {
	val := runID + ":" + sessionID
	return s.r.Set(ctx, txnKey(txnID), []byte(val), 10*time.Minute)
}

func (s *RedisStore) GetTxnRoute(ctx context.Context, txnID string) (string, string, error) {
	data, err := s.r.Get(ctx, txnKey(txnID))
	if err != nil || data == nil {
		return "", "", fmt.Errorf("txn route not found")
	}
	parts := strings.SplitN(string(data), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid txn route format")
	}
	return parts[0], parts[1], nil
}

func (s *RedisStore) SetDiscoveryPayload(ctx context.Context, txnID string, payload []byte) error {
	return s.r.Set(ctx, discoveryKey(txnID), payload, s.ttl)
}

func (s *RedisStore) GetDiscoveryPayload(ctx context.Context, txnID string) ([]byte, error) {
	return s.r.Get(ctx, discoveryKey(txnID))
}
