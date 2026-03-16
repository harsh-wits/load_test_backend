package seller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"seller_app_load_tester/internal/shared/ondcauth"
)

type SigningConfig struct {
	PrivateKey   string
	SubscriberID string
	UniqueKeyID  string
	Enabled      bool
}

type HTTPClient struct {
	client  *http.Client
	signing SigningConfig
}

func NewHTTPClient(signing SigningConfig) *HTTPClient {
	if signing.Enabled && signing.PrivateKey != "" {
		log.Printf("[seller_client] signing enabled subscriber_id=%s unique_key_id=%s",
			signing.SubscriberID, signing.UniqueKeyID)
	} else {
		log.Printf("[seller_client] signing disabled")
	}
	return &HTTPClient{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		signing: signing,
	}
}

func (c *HTTPClient) Search(ctx context.Context, baseURL string, payload []byte) ([]byte, error) {
	return c.postWithBody(ctx, baseURL, "search", payload)
}

func (c *HTTPClient) Select(ctx context.Context, baseURL string, payload []byte) error {
	return c.post(ctx, baseURL, "select", payload)
}

func (c *HTTPClient) Init(ctx context.Context, baseURL string, payload []byte) error {
	return c.post(ctx, baseURL, "init", payload)
}

func (c *HTTPClient) Confirm(ctx context.Context, baseURL string, payload []byte) error {
	return c.post(ctx, baseURL, "confirm", payload)
}

func (c *HTTPClient) postWithBody(ctx context.Context, baseURL, action string, payload []byte) ([]byte, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("empty baseURL")
	}
	url := c.buildURL(baseURL, action)

	max := 2048
	if len(payload) < max {
		max = len(payload)
	}
	log.Printf("[seller_client] outbound %s payload url=%s body=%s", action, url, string(payload[:max]))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.signRequest(req, action, payload)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	log.Printf("[seller_client] POST %s status=%d body=%s", url, resp.StatusCode, string(respBody[:min(len(respBody), 1024)]))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return respBody, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return respBody, nil
}

func (c *HTTPClient) buildURL(baseURL, action string) string {
	if strings.Contains(baseURL, "<action>") {
		return strings.Replace(baseURL, "<action>", action, 1)
	}
	return strings.TrimRight(baseURL, "/") + "/" + action
}

func (c *HTTPClient) signRequest(req *http.Request, action string, payload []byte) {
	if !c.signing.Enabled || c.signing.PrivateKey == "" {
		return
	}
	authHeader, err := ondcauth.CreateAuthorisationHeader(
		string(payload), c.signing.PrivateKey, c.signing.SubscriberID, c.signing.UniqueKeyID)
	if err != nil {
		log.Printf("[seller_client] signing failed for %s: %v", action, err)
		return
	}
	req.Header.Set("Authorization", authHeader)
}

func (c *HTTPClient) post(ctx context.Context, baseURL, action string, payload []byte) error {
	_, err := c.postWithBody(ctx, baseURL, action, payload)
	return err
}
