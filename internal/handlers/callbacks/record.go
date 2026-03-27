package callbacks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"

	"seller_app_load_tester/internal/domain/latency"
	"seller_app_load_tester/internal/shared/ondcauth"
)

const ondcTimestampLayout = "2006-01-02T15:04:05.000Z07:00"

func (c *Controller) verifyInbound(action string, ctx *fiber.Ctx) error {
	// Verification is controlled per-session. Global flag is intentionally ignored.
	authHeader := ctx.Get("Authorization")
	hasHeader := authHeader != ""

	if !hasHeader {
		return nil
	}

	body := ctx.Body()
	txnID := extractTxnID(body)
	if txnID == "" {
		return nil
	}

	_, _, perSessionEnabled, err := c.sessions.GetTxnRoute(context.Background(), txnID)
	if err != nil {
		// If txn routing is missing for this txn, fail-open to avoid NACKing.
		return nil
	}
	if !perSessionEnabled {
		return nil
	}

	log.Printf("[callbacks] verifying %s txn_id=%s", action, txnID)

	err = ondcauth.VerifyAuthorisationHeader(authHeader, string(body), c.verification.PublicKey)
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
		Context any `json:"context"`
	}
	if json.Unmarshal(body, &env) != nil {
		return ""
	}

	ctxMap, _ := env.Context.(map[string]any)
	if ctxMap == nil {
		return ""
	}
	if v, ok := ctxMap["transaction_id"]; ok && v != nil {
		s := fmt.Sprint(v)
		return s
	}
	return ""
}

func (c *Controller) recordCallback(action string, ctx *fiber.Ctx, receivedAt time.Time) {
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

	runID, sessionID, _, err := c.sessions.GetTxnRoute(context.Background(), txnID)
	if err != nil {
		return
	}

	if err := c.store.Record(runID, "pipeline_b", action, txnID, body); err != nil {
		log.Printf("[callbacks] runlog record FAILED action=%s run=%s txn=%s error=%v", action, runID, txnID, err)
	}

	outcome := latency.OutcomeSuccess
	if ackStatus := extractInboundAckStatus(body); ackStatus != "" && ackStatus != "ACK" {
		outcome = latency.OutcomeFailure
	}

	// Latency measured for callback actions only: sent_at comes from the paired request stage.
	effectiveOutcome := c.recordLatencyEventOnCallback(action, runID, sessionID, txnID, body, receivedAt, outcome)

	// Counters should reflect the outcome we store in Mongo.
	_ = c.sessions.IncrMetric(context.Background(), runID, action, "sent", 1)
	switch effectiveOutcome {
	case latency.OutcomeSuccess:
		_ = c.sessions.IncrMetric(context.Background(), runID, action, "success", 1)
	case latency.OutcomeFailure:
		_ = c.sessions.IncrMetric(context.Background(), runID, action, "failure", 1)
	case latency.OutcomeTimeout:
		_ = c.sessions.IncrMetric(context.Background(), runID, action, "timeout", 1)
	default:
		_ = c.sessions.IncrMetric(context.Background(), runID, action, "failure", 1)
	}

	if c.notifier != nil {
		c.notifier.Notify(runID, action)
	}
}

func (c *Controller) recordLatencyEventOnCallback(callbackAction, runID, sessionID, txnID string, body []byte, receivedAt time.Time, outcome latency.Outcome) latency.Outcome {
	if c.store == nil || c.sessions == nil {
		return outcome
	}

	persist := c.sessions.Persist()
	if persist == nil {
		return outcome
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
		return outcome
	}

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
	if c.enqueueLatencyEvent(ev) {
		return effectiveOutcome
	}
	// Queue pressure fallback: keep correctness by writing synchronously if enqueue fails.
	if err := persist.UpsertRunLatencyEvent(context.Background(), ev); err != nil {
		log.Printf("[callbacks] latency sync fallback upsert FAILED action=%s run=%s txn=%s error=%v", callbackAction, runID, txnID, err)
	}

	return effectiveOutcome
}

func (c *Controller) recordCallbackFailure(action string, ctx *fiber.Ctx, receivedAt time.Time) {
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
	runID, sessionID, _, err := c.sessions.GetTxnRoute(context.Background(), txnID)
	if err != nil {
		return
	}

	if err := c.store.Record(runID, "pipeline_b", action, txnID, body); err != nil {
		log.Printf("[callbacks] runlog record FAILED action=%s run=%s txn=%s error=%v", action, runID, txnID, err)
	}

	effectiveOutcome := c.recordLatencyEventOnCallback(action, runID, sessionID, txnID, body, receivedAt, latency.OutcomeFailure)
	_ = c.sessions.IncrMetric(context.Background(), runID, action, "sent", 1)
	switch effectiveOutcome {
	case latency.OutcomeSuccess:
		_ = c.sessions.IncrMetric(context.Background(), runID, action, "success", 1)
	case latency.OutcomeFailure:
		_ = c.sessions.IncrMetric(context.Background(), runID, action, "failure", 1)
	case latency.OutcomeTimeout:
		_ = c.sessions.IncrMetric(context.Background(), runID, action, "timeout", 1)
	default:
		_ = c.sessions.IncrMetric(context.Background(), runID, action, "failure", 1)
	}

	if c.notifier != nil {
		c.notifier.Notify(runID, action)
	}
}

func extractInboundAckStatus(body []byte) string {
	var env struct {
		Message struct {
			Ack struct {
				Status string `json:"status"`
			} `json:"ack"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	return env.Message.Ack.Status
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
