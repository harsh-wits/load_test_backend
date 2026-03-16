package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

type InitPayload []byte
type ConfirmPayload []byte

func BuildInitBatchFromExample(examplePath string, transactionIDs []string) ([]InitPayload, error) {
	raw, err := buildBatchFromExample(examplePath, transactionIDs, "init")
	if err != nil {
		return nil, err
	}
	out := make([]InitPayload, len(raw))
	for i := range raw {
		out[i] = InitPayload(raw[i])
	}
	return out, nil
}

func BuildConfirmBatchFromExample(examplePath string, transactionIDs []string) ([]ConfirmPayload, error) {
	raw, err := buildBatchFromExample(examplePath, transactionIDs, "confirm")
	if err != nil {
		return nil, err
	}
	out := make([]ConfirmPayload, len(raw))
	for i := range raw {
		out[i] = ConfirmPayload(raw[i])
	}
	return out, nil
}

func buildBatchFromExample(examplePath string, transactionIDs []string, action string) ([][]byte, error) {
	if len(transactionIDs) == 0 {
		return nil, nil
	}
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		return nil, fmt.Errorf("read example %s: %w", action, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal example %s: %w", action, err)
	}
	ctx, _ := m["context"].(map[string]any)
	if ctx == nil {
		return nil, fmt.Errorf("example %s: missing context", action)
	}
	out := make([][]byte, 0, len(transactionIDs))
	for _, txnID := range transactionIDs {
		var clone map[string]any
		if err := json.Unmarshal(raw, &clone); err != nil {
			return nil, fmt.Errorf("clone example %s: %w", action, err)
		}
		ctx, _ := clone["context"].(map[string]any)
		if ctx != nil {
			ctx["transaction_id"] = txnID
			ctx["message_id"] = uuid.NewString()
			ctx["timestamp"] = time.Now().UTC().Format(ondcTimestampLayout)
			ctx["action"] = action
			if _, ok := ctx["ttl"]; !ok {
				ctx["ttl"] = "PT30S"
			}
		}
		payload, err := json.Marshal(clone)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", action, err)
		}
		out = append(out, payload)
	}
	return out, nil
}
