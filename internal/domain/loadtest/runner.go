package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"seller_app_load_tester/internal/shared/ondcauth"
)

// SigningConfig holds the ONDC signing identity for outbound requests.
type SigningConfig struct {
	Enabled      bool
	PrivateKey   string
	SubscriberID string
	UniqueKeyID  string
}

// Runner dispatches generated payloads at a target rate.
type Runner struct {
	client      *http.Client
	signing     SigningConfig
	maxInFlight int
}

func NewRunner(signing SigningConfig, maxInFlight int, requestTimeout time.Duration) *Runner {
	if maxInFlight <= 0 {
		maxInFlight = 256
	}
	return &Runner{
		client:      &http.Client{Timeout: requestTimeout},
		signing:     signing,
		maxInFlight: maxInFlight,
	}
}

// Execute runs the load in a blocking loop (call in a goroutine). It generates
// `run.Planned` payloads, paces them at req.QPS (if set), signs (if req.Sign),
// POSTs each to {bpp_uri}/{action}, and records ledger rows on the run.
func (rn *Runner) Execute(ctx context.Context, req StartRequest, gen *Generator, run *Run) {
	run.setStatus(StatusRunning)

	var interval time.Duration
	if req.QPS > 0 {
		interval = time.Second / time.Duration(req.QPS)
	}
	var ticker *time.Ticker
	if interval > 0 {
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
	}

	url := strings.TrimRight(req.BPPURI, "/") + "/" + string(req.Action)
	sem := make(chan struct{}, rn.maxInFlight)
	var wg sync.WaitGroup

	stopped := false
	for i := 0; i < run.Planned; i++ {
		if ctx.Err() != nil {
			stopped = true
			break
		}
		if ticker != nil {
			select {
			case <-ctx.Done():
				stopped = true
			case <-ticker.C:
			}
			if stopped {
				break
			}
		}

		payload, txnID, msgID, err := gen.Generate(req.Action)
		if err != nil {
			run.markDispatched()
			run.record(LedgerRow{
				Action: string(req.Action), SentAtUnixMs: nowMs(), SentAtUTC: nowUTC(),
				AckStatus: "error", Error: "generate: " + err.Error(),
			})
			continue
		}

		run.markDispatched()
		sem <- struct{}{}
		wg.Add(1)
		go func(payload []byte, txnID, msgID string) {
			defer wg.Done()
			defer func() { <-sem }()
			rn.sendOne(ctx, url, req, payload, txnID, msgID, run)
		}(payload, txnID, msgID)
	}

	wg.Wait()
	if stopped {
		run.setStatus(StatusStopped)
	} else {
		run.setStatus(StatusCompleted)
	}
}

func (rn *Runner) sendOne(ctx context.Context, url string, req StartRequest, payload []byte, txnID, msgID string, run *Run) {
	row := LedgerRow{
		TransactionID: txnID,
		MessageID:     msgID,
		Action:        string(req.Action),
		SentAtUnixMs:  nowMs(),
		SentAtUTC:     nowUTC(),
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		row.AckStatus = "error"
		row.Error = "build request: " + err.Error()
		run.record(row)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.Sign && rn.signing.Enabled && rn.signing.PrivateKey != "" {
		if h, err := ondcauth.CreateAuthorisationHeader(string(payload), rn.signing.PrivateKey, rn.signing.SubscriberID, rn.signing.UniqueKeyID); err == nil {
			httpReq.Header.Set("Authorization", h)
		}
	}

	start := time.Now()
	resp, err := rn.client.Do(httpReq)
	row.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		row.AckStatus = "error"
		row.Error = err.Error()
		run.record(row)
		return
	}
	defer resp.Body.Close()
	row.HTTPStatus = resp.StatusCode

	body := make([]byte, 0, 512)
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			body = append(body, buf[:n]...)
		}
		if len(body) > 64*1024 || rerr != nil {
			break
		}
	}

	row.AckStatus = classify(resp.StatusCode, body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		row.Error = "http " + resp.Status
	}
	// Capture the seller's response for non-ACK outcomes so the reason
	// (if the seller returns one) shows up in the ledger.
	if row.AckStatus != "ACK" && len(body) > 0 {
		b := body
		if len(b) > 1024 {
			b = b[:1024]
		}
		row.ResponseBody = string(b)
	}
	run.record(row)
}

// classify maps an HTTP response to ACK / NACK / error.
func classify(status int, body []byte) string {
	if status < 200 || status >= 300 {
		return "error"
	}
	var env struct {
		Message struct {
			Ack struct {
				Status string `json:"status"`
			} `json:"ack"`
		} `json:"message"`
	}
	if json.Unmarshal(body, &env) == nil && strings.EqualFold(env.Message.Ack.Status, "ACK") {
		return "ACK"
	}
	return "NACK"
}

func nowMs() int64    { return time.Now().UTC().UnixMilli() }
func nowUTC() string  { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
