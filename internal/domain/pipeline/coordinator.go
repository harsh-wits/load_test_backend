package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"seller_app_load_tester/internal/config"
	"seller_app_load_tester/internal/ports/seller"
	"seller_app_load_tester/internal/shared/runlog"
)

type TxnLinker func(runID, txnID string)

type BCoordinator struct {
	selectBatch SelectBatchService
	store       runlog.Store
	seller      seller.Client
	cfg         *config.Config
	linkTxn     TxnLinker
	throttle    Throttle
	sessionID   string
	coreVersion string
}

func NewBCoordinator(
	selectBatch SelectBatchService,
	store runlog.Store,
	sellerClient seller.Client,
	cfg *config.Config,
) *BCoordinator {
	return &BCoordinator{
		selectBatch: selectBatch,
		store:       store,
		seller:      sellerClient,
		cfg:         cfg,
		linkTxn:     func(string, string) {},
	}
}

func (c *BCoordinator) SetTxnLinker(fn TxnLinker) {
	if fn != nil {
		c.linkTxn = fn
	}
}

func (c *BCoordinator) SetThrottle(t Throttle, sessionID string) {
	c.throttle = t
	c.sessionID = sessionID
}

func (c *BCoordinator) SetCoreVersion(v string) {
	c.coreVersion = v
}

func (c *BCoordinator) SelectBatchFromExample(batchSize int) ([]SelectPayload, error) {
	path := filepath.Join("examples", "payloads", "select", "select.json")
	return c.selectBatch.GenerateBatchFromExample(path, batchSize)
}

func (c *BCoordinator) SelectBatchFromOnSearch(onSearch OnSearchPayload, batchSize int) ([]SelectPayload, error) {
	path := filepath.Join("examples", "payloads", "select", "select.json")
	return c.selectBatch.GenerateBatchFromOnSearch(onSearch, path, batchSize)
}

func (c *BCoordinator) SelectBatchFromOnSearchWithTxnID(onSearch OnSearchPayload, batchSize int, txnID string) ([]SelectPayload, error) {
	path := filepath.Join("examples", "payloads", "select", "select.json")
	return c.selectBatch.GenerateBatchFromOnSearchWithTxnID(onSearch, path, batchSize, txnID)
}

func (c *BCoordinator) Store() runlog.Store {
	return c.store
}

func (c *BCoordinator) preparePayload(action string, raw []byte) (toSend []byte, txnID string, err error) {
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, "", fmt.Errorf("decode %s envelope: %w", action, err)
	}
	ctxMap, _ := env["context"].(map[string]any)
	if ctxMap == nil {
		return raw, "", nil
	}
	if v, ok := ctxMap["transaction_id"]; ok && v != nil {
		txnID = fmt.Sprint(v)
	}
	if c.cfg.BAPURI != "" {
		ctxMap["bap_uri"] = c.cfg.BAPURI
	}
	if c.cfg.BAPID != "" {
		ctxMap["bap_id"] = c.cfg.BAPID
	}
	if c.coreVersion != "" {
		ctxMap["core_version"] = c.coreVersion
	} else if c.cfg.CoreVersion != "" {
		ctxMap["core_version"] = c.cfg.CoreVersion
	}
	updated, err := json.Marshal(env)
	if err != nil {
		return nil, txnID, fmt.Errorf("encode %s envelope: %w", action, err)
	}
	return updated, txnID, nil
}

func (c *BCoordinator) RunSelectStage(ctx context.Context, runID, baseURL string, batch []SelectPayload, maxInFlight int) []DispatchResult {
	log.Printf("[preorder] dispatching %d select requests concurrently (max_in_flight=%d)", len(batch), maxInFlight)

	payloads := make([][]byte, len(batch))
	for i, p := range batch {
		payloads[i] = []byte(p)
	}

	results := DispatchConcurrentThrottled(ctx, payloads, maxInFlight, c.throttle, c.sessionID,
		func(ctx context.Context, idx int, raw []byte) DispatchResult {
			toSend, txnID, err := c.preparePayload("select", raw)
			if err != nil {
				return DispatchResult{Index: idx, TxnID: txnID, Err: err}
			}
			if txnID == "" {
				return DispatchResult{Index: idx}
			}
			c.linkTxn(runID, txnID)
			_ = c.store.Record(runID, "pipeline_b", "select", txnID, toSend)
			_ = c.store.RecordTimestamp(runID, "pipeline_b", "select", txnID, time.Now().UTC())
			if err := c.seller.Select(ctx, baseURL, toSend); err != nil {
				return DispatchResult{Index: idx, TxnID: txnID, Err: err}
			}
			return DispatchResult{Index: idx, TxnID: txnID}
		})

	log.Printf("[preorder] select dispatch complete run_id=%s total=%d", runID, len(batch))
	return results
}

func (c *BCoordinator) RunInitStage(ctx context.Context, runID, baseURL string, batch []InitPayload, maxInFlight int) []DispatchResult {
	log.Printf("[preorder] dispatching %d init requests concurrently (max_in_flight=%d)", len(batch), maxInFlight)

	payloads := make([][]byte, len(batch))
	for i, p := range batch {
		payloads[i] = []byte(p)
	}

	results := DispatchConcurrentThrottled(ctx, payloads, maxInFlight, c.throttle, c.sessionID,
		func(ctx context.Context, idx int, raw []byte) DispatchResult {
			toSend, txnID, err := c.preparePayload("init", raw)
			if err != nil {
				return DispatchResult{Index: idx, TxnID: txnID, Err: err}
			}
			if txnID != "" {
				_ = c.store.Record(runID, "pipeline_b", "init", txnID, toSend)
				_ = c.store.RecordTimestamp(runID, "pipeline_b", "init", txnID, time.Now().UTC())
			}
			if err := c.seller.Init(ctx, baseURL, toSend); err != nil {
				return DispatchResult{Index: idx, TxnID: txnID, Err: err}
			}
			return DispatchResult{Index: idx, TxnID: txnID}
		})

	log.Printf("[preorder] init dispatch complete run_id=%s total=%d", runID, len(batch))
	return results
}

func (c *BCoordinator) RunConfirmStage(ctx context.Context, runID, baseURL string, batch []ConfirmPayload, maxInFlight int) []DispatchResult {
	log.Printf("[preorder] dispatching %d confirm requests concurrently (max_in_flight=%d)", len(batch), maxInFlight)

	payloads := make([][]byte, len(batch))
	for i, p := range batch {
		payloads[i] = []byte(p)
	}

	results := DispatchConcurrentThrottled(ctx, payloads, maxInFlight, c.throttle, c.sessionID,
		func(ctx context.Context, idx int, raw []byte) DispatchResult {
			toSend, txnID, err := c.preparePayload("confirm", raw)
			if err != nil {
				return DispatchResult{Index: idx, TxnID: txnID, Err: err}
			}
			if txnID != "" {
				_ = c.store.Record(runID, "pipeline_b", "confirm", txnID, toSend)
				_ = c.store.RecordTimestamp(runID, "pipeline_b", "confirm", txnID, time.Now().UTC())
			}
			if err := c.seller.Confirm(ctx, baseURL, toSend); err != nil {
				return DispatchResult{Index: idx, TxnID: txnID, Err: err}
			}
			return DispatchResult{Index: idx, TxnID: txnID}
		})

	log.Printf("[preorder] confirm dispatch complete run_id=%s total=%d", runID, len(batch))
	return results
}
