package runlog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"seller_app_load_tester/internal/shared/redis"
)

type RedisStore struct {
	client redis.Client
	ttl    time.Duration
}

func NewRedisStore(client redis.Client, ttlSeconds int) *RedisStore {
	if ttlSeconds <= 0 {
		ttlSeconds = 600
	}
	return &RedisStore{
		client: client,
		ttl:    time.Duration(ttlSeconds) * time.Second,
	}
}

func (s *RedisStore) hashKey(runID, pipeline, action string) string {
	return fmt.Sprintf("runlog:%s:%s:%s", runID, pipeline, action)
}

func (s *RedisStore) timestampHashKey(runID, pipeline, action string) string {
	return fmt.Sprintf("runlog:%s:ts:%s:%s", runID, pipeline, action)
}

func (s *RedisStore) counterKey(runID, pipeline, action string) string {
	return fmt.Sprintf("runlog:%s:count:%s:%s", runID, pipeline, action)
}

func (s *RedisStore) Record(runID, pipeline, action, transactionID string, payload []byte) error {
	if runID == "" || pipeline == "" || action == "" || transactionID == "" {
		return nil
	}
	ctx := context.Background()
	hk := s.hashKey(runID, pipeline, action)
	if err := s.client.HSet(ctx, hk, transactionID, payload); err != nil {
		return fmt.Errorf("hset %s: %w", hk, err)
	}
	_ = s.client.Expire(ctx, hk, s.ttl)

	ck := s.counterKey(runID, pipeline, action)
	if _, err := s.client.Incr(ctx, ck); err != nil {
		return fmt.Errorf("incr %s: %w", ck, err)
	}
	_ = s.client.Expire(ctx, ck, s.ttl)
	return nil
}

func (s *RedisStore) RecordTimestamp(runID, pipeline, action, transactionID string, t time.Time) error {
	if runID == "" || pipeline == "" || action == "" || transactionID == "" {
		return nil
	}
	ctx := context.Background()
	hk := s.timestampHashKey(runID, pipeline, action)
	if err := s.client.HSet(ctx, hk, transactionID, []byte(strconv.FormatInt(t.UTC().UnixNano(), 10))); err != nil {
		return fmt.Errorf("hset %s: %w", hk, err)
	}
	_ = s.client.Expire(ctx, hk, s.ttl)
	return nil
}

func (s *RedisStore) GetTimestamp(runID, pipeline, action, transactionID string) (time.Time, error) {
	ctx := context.Background()
	hk := s.timestampHashKey(runID, pipeline, action)
	raw, err := s.client.HGet(ctx, hk, transactionID)
	if err != nil {
		return time.Time{}, err
	}
	if raw == nil {
		return time.Time{}, fmt.Errorf("timestamp not found for txn %s", transactionID)
	}
	var ns int64
	if _, err := fmt.Sscanf(string(raw), "%d", &ns); err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp for txn %s: %w", transactionID, err)
	}
	return time.Unix(0, ns).UTC(), nil
}

func (s *RedisStore) ListTxnIDs(runID, pipeline, action string) ([]string, error) {
	ctx := context.Background()
	hk := s.timestampHashKey(runID, pipeline, action)
	all, err := s.client.HGetAll(ctx, hk)
	if err != nil {
		return nil, err
	}
	txnIDs := make([]string, 0, len(all))
	for txnID := range all {
		txnIDs = append(txnIDs, txnID)
	}
	return txnIDs, nil
}

func (s *RedisStore) Get(runID, pipeline, action, transactionID string) ([]byte, error) {
	ctx := context.Background()
	data, err := s.client.HGet(ctx, s.hashKey(runID, pipeline, action), transactionID)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("txn %s not found", transactionID)
	}
	return data, nil
}

func (s *RedisStore) GetMulti(runID, pipeline, action string, txnIDs []string) (map[string][]byte, error) {
	if len(txnIDs) == 0 {
		return nil, nil
	}
	ctx := context.Background()
	vals, err := s.client.HMGet(ctx, s.hashKey(runID, pipeline, action), txnIDs...)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(txnIDs))
	for i, v := range vals {
		if v != nil {
			out[txnIDs[i]] = v
		}
	}
	return out, nil
}

func (s *RedisStore) Count(runID, pipeline, action string) (int, error) {
	ctx := context.Background()
	data, err := s.client.Get(ctx, s.counterKey(runID, pipeline, action))
	if err != nil {
		return 0, err
	}
	if data == nil {
		return 0, nil
	}
	var n int
	fmt.Sscanf(string(data), "%d", &n)
	return n, nil
}

func (s *RedisStore) FlushToFilesystem(runID, rootDir string) error {
	ctx := context.Background()
	actions := []string{
		"pipeline_b:select", "pipeline_b:on_select",
		"pipeline_b:init", "pipeline_b:on_init",
		"pipeline_b:confirm", "pipeline_b:on_confirm",
	}
	for _, pa := range actions {
		hk := fmt.Sprintf("runlog:%s:%s", runID, pa)
		all, err := s.client.HGetAll(ctx, hk)
		if err != nil || len(all) == 0 {
			continue
		}
		parts := filepath.SplitList(pa)
		if len(parts) == 0 {
			parts = []string{pa}
		}
		dir := filepath.Join(rootDir, runID, filepath.FromSlash(pa))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
		for txnID, payload := range all {
			p := filepath.Join(dir, txnID+".json")
			if err := os.WriteFile(p, payload, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", p, err)
			}
		}
	}
	return nil
}

func (s *RedisStore) Cleanup(runID string) {
	ctx := context.Background()
	_ = s.client.DelPattern(ctx, fmt.Sprintf("runlog:%s:*", runID))
}

func (s *RedisStore) Export(runID string, fn func(pipeline, action, txnID string, payload []byte) error) error {
	if fn == nil {
		return nil
	}
	ctx := context.Background()

	pattern := fmt.Sprintf("runlog:%s:*:*", runID)
	keys, err := s.client.Keys(ctx, pattern)
	if err != nil {
		return err
	}
	for _, key := range keys {
		var run, pipeline, action string
		if _, err := fmt.Sscanf(key, "runlog:%s:%s:%s", &run, &pipeline, &action); err != nil || run != runID {
			continue
		}
		all, err := s.client.HGetAll(ctx, key)
		if err != nil || len(all) == 0 {
			continue
		}
		for txnID, payload := range all {
			if err := fn(pipeline, action, txnID, payload); err != nil {
				return err
			}
		}
	}
	return nil
}
