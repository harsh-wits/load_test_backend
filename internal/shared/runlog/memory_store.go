package runlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type MemoryStore struct {
	mu       sync.RWMutex
	data     map[string]map[string][]byte // "{runID}:{pipeline}:{action}" -> txnID -> payload
	counters map[string]*atomic.Int64
	timestamps map[string]map[string]time.Time // "{runID}:{pipeline}:{action}" -> txnID -> sent_at/received_at
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data:     make(map[string]map[string][]byte),
		counters: make(map[string]*atomic.Int64),
		timestamps: make(map[string]map[string]time.Time),
	}
}

func (s *MemoryStore) bucketKey(runID, pipeline, action string) string {
	return runID + ":" + pipeline + ":" + action
}

func (s *MemoryStore) Record(runID, pipeline, action, transactionID string, payload []byte) error {
	if runID == "" || pipeline == "" || action == "" || transactionID == "" {
		return nil
	}
	key := s.bucketKey(runID, pipeline, action)

	s.mu.Lock()
	bucket, ok := s.data[key]
	if !ok {
		bucket = make(map[string][]byte)
		s.data[key] = bucket
	}
	bucket[transactionID] = payload

	ctr, ok := s.counters[key]
	if !ok {
		ctr = &atomic.Int64{}
		s.counters[key] = ctr
	}
	s.mu.Unlock()

	ctr.Add(1)
	return nil
}

func (s *MemoryStore) RecordTimestamp(runID, pipeline, action, transactionID string, t time.Time) error {
	if runID == "" || pipeline == "" || action == "" || transactionID == "" {
		return nil
	}
	key := s.bucketKey(runID, pipeline, action)
	s.mu.Lock()
	bucket, ok := s.timestamps[key]
	if !ok {
		bucket = make(map[string]time.Time)
		s.timestamps[key] = bucket
	}
	bucket[transactionID] = t.UTC()
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) GetTimestamp(runID, pipeline, action, transactionID string) (time.Time, error) {
	key := s.bucketKey(runID, pipeline, action)
	s.mu.RLock()
	bucket := s.timestamps[key]
	s.mu.RUnlock()
	if bucket == nil {
		return time.Time{}, fmt.Errorf("no timestamp data for %s/%s/%s", runID, pipeline, action)
	}
	ts, ok := bucket[transactionID]
	if !ok {
		return time.Time{}, fmt.Errorf("timestamp not found for txn %s", transactionID)
	}
	return ts, nil
}

func (s *MemoryStore) ListTxnIDs(runID, pipeline, action string) ([]string, error) {
	key := s.bucketKey(runID, pipeline, action)
	s.mu.RLock()
	bucket := s.timestamps[key]
	s.mu.RUnlock()
	if bucket == nil {
		return nil, nil
	}
	out := make([]string, 0, len(bucket))
	for txnID := range bucket {
		out = append(out, txnID)
	}
	return out, nil
}

func (s *MemoryStore) Get(runID, pipeline, action, transactionID string) ([]byte, error) {
	key := s.bucketKey(runID, pipeline, action)
	s.mu.RLock()
	bucket := s.data[key]
	s.mu.RUnlock()
	if bucket == nil {
		return nil, fmt.Errorf("no data for %s/%s/%s", runID, pipeline, action)
	}
	v, ok := bucket[transactionID]
	if !ok {
		return nil, fmt.Errorf("txn %s not found in %s", transactionID, key)
	}
	return v, nil
}

func (s *MemoryStore) GetMulti(runID, pipeline, action string, txnIDs []string) (map[string][]byte, error) {
	key := s.bucketKey(runID, pipeline, action)
	s.mu.RLock()
	bucket := s.data[key]
	s.mu.RUnlock()
	if bucket == nil {
		return nil, nil
	}
	out := make(map[string][]byte, len(txnIDs))
	for _, id := range txnIDs {
		if v, ok := bucket[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

func (s *MemoryStore) Count(runID, pipeline, action string) (int, error) {
	key := s.bucketKey(runID, pipeline, action)
	s.mu.RLock()
	ctr := s.counters[key]
	s.mu.RUnlock()
	if ctr == nil {
		return 0, nil
	}
	return int(ctr.Load()), nil
}

func (s *MemoryStore) FlushToFilesystem(runID, rootDir string) error {
	s.mu.RLock()
	var keys []string
	prefix := runID + ":"
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	s.mu.RUnlock()

	for _, key := range keys {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 {
			continue
		}
		pipeline, action := parts[1], parts[2]
		dir := filepath.Join(rootDir, runID, pipeline, action)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}

		s.mu.RLock()
		bucket := s.data[key]
		txnIDs := make([]string, 0, len(bucket))
		for id := range bucket {
			txnIDs = append(txnIDs, id)
		}
		s.mu.RUnlock()

		for _, txnID := range txnIDs {
			s.mu.RLock()
			payload := s.data[key][txnID]
			s.mu.RUnlock()
			path := filepath.Join(dir, txnID+".json")
			if err := os.WriteFile(path, payload, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
		}
	}
	return nil
}

func (s *MemoryStore) Cleanup(runID string) {
	prefix := runID + ":"
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			delete(s.data, k)
		}
	}
	for k := range s.counters {
		if strings.HasPrefix(k, prefix) {
			delete(s.counters, k)
		}
	}
	for k := range s.timestamps {
		if strings.HasPrefix(k, prefix) {
			delete(s.timestamps, k)
		}
	}
}

func (s *MemoryStore) Export(runID string, fn func(pipeline, action, txnID string, payload []byte) error) error {
	if fn == nil {
		return nil
	}
	prefix := runID + ":"

	s.mu.RLock()
	defer s.mu.RUnlock()

	for key, bucket := range s.data {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 {
			continue
		}
		pipeline, action := parts[1], parts[2]
		for txnID, payload := range bucket {
			if err := fn(pipeline, action, txnID, payload); err != nil {
				return err
			}
		}
	}
	return nil
}
