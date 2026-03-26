package callbacks

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"

	"seller_app_load_tester/internal/domain/latency"
	"seller_app_load_tester/internal/shared/ondcauth"
)

const ondcTimestampLayout = "2006-01-02T15:04:05.000Z07:00"

func (c *Controller) verifyInbound(action string, ctx *fiber.Ctx) error {
	// saving time by returning early if verification is not enabled
	if !c.verification.Enabled {
		return nil
	}
	log.Printf("[callbacks] verification enabled=%t", c.verification.Enabled)
	authHeader := ctx.Get("Authorization")
	hasHeader := authHeader != ""
	log.Printf("[callbacks] verifying %s auth_header_present=%t verification_enabled=%t",
		action, hasHeader, c.verification.Enabled)

	if !hasHeader {
		return nil
	}

	body := ctx.Body()
	txnID := extractTxnID(body)

	err := ondcauth.VerifyAuthorisationHeader(authHeader, string(body), c.verification.PublicKey)
	if err != nil {
		log.Printf("[callbacks] %s verification FAILED txn_id=%s error=%v", action, txnID, err)
		return err
	}
	return nil
}

func (c *Controller) validatePayload(action string, ctx *fiber.Ctx) error {
	if c.validator == nil {
		return nil
	}
	return c.validator.Validate(action, ctx.Body())
}

func extractTxnID(body []byte) string {
	var env struct {
		Context struct {
			TransactionID string `json:"transaction_id"`
		} `json:"context"`
	}
	if json.Unmarshal(body, &env) == nil {
		return env.Context.TransactionID
	}
	return ""
}

func (c *Controller) recordCallback(action string, ctx *fiber.Ctx) {
	if c.store == nil || c.sessions == nil {
		return
	}
	body := ctx.Body()
	if len(body) == 0 {
		return
	}
	txnID := extractTxnID(body)
	if txnID == "" {
		return
	}

	runID, sessionID, err := c.sessions.GetTxnRoute(context.Background(), txnID)
	if err != nil {
		return
	}

	if err := c.store.Record(runID, "pipeline_b", action, txnID, body); err != nil {
		log.Printf("[callbacks] runlog record FAILED action=%s run=%s txn=%s error=%v", action, runID, txnID, err)
	}

	// Latency measured for callback actions only: sent_at comes from the paired request stage.
	c.recordLatencyEventOnCallback(action, runID, sessionID, txnID, body, latency.OutcomeSuccess)

	_ = c.sessions.IncrMetric(context.Background(), runID, action, "success", 1)

	if c.notifier != nil {
		c.notifier.Notify(runID, action)
	}
}

func (c *Controller) recordLatencyEventOnCallback(callbackAction, runID, sessionID, txnID string, body []byte, outcome latency.Outcome) {
	if c.store == nil || c.sessions == nil {
		return
	}

	persist := c.sessions.Persist()
	if persist == nil {
		return
	}

	requestAction := ""
	switch callbackAction {
	case "on_select":
		requestAction = "select"
	case "on_init":
		requestAction = "init"
	case "on_confirm":
		requestAction = "confirm"
	default:
		return
	}

	receivedAt := time.Now().UTC()
	const callbackTimeout = 30 * time.Second

	sentAt, sentErr := c.store.GetTimestamp(runID, "pipeline_b", requestAction, txnID)
	if sentErr != nil {
		sentAt = receivedAt
	}

	var latencyMs *int64
	if sentErr == nil {
		ms := receivedAt.Sub(sentAt).Milliseconds()
		latencyMs = &ms
	}

	effectiveOutcome := outcome
	if latencyMs != nil && *latencyMs >= callbackTimeout.Milliseconds() {
		effectiveOutcome = latency.OutcomeTimeout
	}

	// Upsert with `$setOnInsert` so late callbacks cannot overwrite already-classified outcomes.
	ev := &latency.RunLatencyEvent{
		SessionID:  sessionID,
		RunID:      runID,
		Stage:      latency.Stage(callbackAction),
		TxnID:      txnID,
		SentAt:     sentAt,
		ReceivedAt: &receivedAt,
		LatencyMS:  latencyMs,
		Outcome:    effectiveOutcome,
		RecordedAt: receivedAt,
	}
	// Body is currently unused, but keeping the call signature makes it easy to extend
	// later if you decide to store failure details.
	_ = body
	if err := persist.UpsertRunLatencyEvent(context.Background(), ev); err != nil {
		log.Printf("[callbacks] latency event upsert FAILED action=%s run=%s txn=%s error=%v", callbackAction, runID, txnID, err)
	}
}

func (c *Controller) recordCallbackFailure(action string, ctx *fiber.Ctx) {
	if c.store == nil || c.sessions == nil {
		return
	}
	body := ctx.Body()
	if len(body) == 0 {
		return
	}
	txnID := extractTxnID(body)
	if txnID == "" {
		return
	}
	runID, sessionID, err := c.sessions.GetTxnRoute(context.Background(), txnID)
	if err != nil {
		return
	}

	if err := c.store.Record(runID, "pipeline_b", action, txnID, body); err != nil {
		log.Printf("[callbacks] runlog record FAILED action=%s run=%s txn=%s error=%v", action, runID, txnID, err)
	}
	c.recordLatencyEventOnCallback(action, runID, sessionID, txnID, body, latency.OutcomeFailure)
}

func (c *Controller) writeAck(ctx *fiber.Ctx) error {
	body := ctx.Body()
	var full map[string]any
	if len(body) > 0 {
		_ = json.Unmarshal(body, &full)
	}
	ctxMap, _ := full["context"].(map[string]any)
	if ctxMap == nil {
		ctxMap = map[string]any{}
	}
	if _, ok := ctxMap["timestamp"]; !ok {
		ctxMap["timestamp"] = time.Now().UTC().Format(ondcTimestampLayout)
	}
	resp := map[string]any{
		"context": ctxMap,
		"message": map[string]any{
			"ack": map[string]any{"status": "ACK"},
		},
	}
	return ctx.Status(fiber.StatusOK).JSON(resp)
}

func (c *Controller) writeNack(ctx *fiber.Ctx, reason string) error {
	body := ctx.Body()
	var full map[string]any
	if len(body) > 0 {
		_ = json.Unmarshal(body, &full)
	}
	ctxMap, _ := full["context"].(map[string]any)
	if ctxMap == nil {
		ctxMap = map[string]any{}
	}
	if _, ok := ctxMap["timestamp"]; !ok {
		ctxMap["timestamp"] = time.Now().UTC().Format(ondcTimestampLayout)
	}
	resp := map[string]any{
		"context": ctxMap,
		"message": map[string]any{
			"ack": map[string]any{"status": "NACK"},
		},
		"error": map[string]any{
			"code":    "10001",
			"message": reason,
		},
	}
	return ctx.Status(fiber.StatusOK).JSON(resp)
}
