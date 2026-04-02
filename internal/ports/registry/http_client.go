package registry

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"seller_app_load_tester/internal/shared/ondcauth"
)

type HTTPClient struct {
	baseURL string
	http    *http.Client

	signingPrivateKey string
	signingSubID      string
	signingUKID       string

	cacheTTL time.Duration

	mu    sync.RWMutex
	cache map[string]Record
}

type LookupRequest struct {
	SubscriberID string `json:"subscriber_id"`
	UKID         string `json:"ukId"`
}

type LookupResponseItem struct {
	SubscriberID      string `json:"subscriber_id"`
	UKID              string `json:"ukId"`
	SigningPublicKey  string `json:"signing_public_key"`
	SigningPublicKey2 string `json:"signingPublicKey"`
	PubKey            string `json:"pub_key"`
	ValidUntil        string `json:"valid_until"`
	ValidTo           string `json:"valid_to"`
}

func NewHTTPClient(baseURL string, signingPrivateKey, signingSubID, signingUKID string, cacheTTL time.Duration) *HTTPClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL != "" && !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	if cacheTTL <= 0 {
		cacheTTL = 10 * time.Minute
	}
	return &HTTPClient{
		baseURL:           baseURL,
		http:              &http.Client{Timeout: 10 * time.Second},
		signingPrivateKey: signingPrivateKey,
		signingSubID:      signingSubID,
		signingUKID:       signingUKID,
		cacheTTL:          cacheTTL,
		cache:             map[string]Record{},
	}
}

func (c *HTTPClient) GetPublicKey(ctx context.Context, subscriberID string) (ed25519.PublicKey, error) {
	_ = ctx
	_ = subscriberID
	return nil, fmt.Errorf("registry client requires subscriber_id and ukId")
}

func (c *HTTPClient) GetPublicKeyByUKID(ctx context.Context, subscriberID, ukID string) (ed25519.PublicKey, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("registry base URL not configured")
	}
	if subscriberID == "" || ukID == "" {
		return nil, fmt.Errorf("missing subscriber_id or ukId")
	}

	cacheKey := subscriberID + "|" + ukID
	if pk, ok := c.getCached(cacheKey); ok {
		return pk, nil
	}

	reqBody, err := json.Marshal(LookupRequest{SubscriberID: subscriberID, UKID: ukID})
	if err != nil {
		return nil, fmt.Errorf("marshal lookup request: %w", err)
	}

	url := c.baseURL + "lookup"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build lookup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.signingPrivateKey != "" && c.signingSubID != "" && c.signingUKID != "" {
		h, err := ondcauth.CreateAuthorisationHeader(string(reqBody), c.signingPrivateKey, c.signingSubID, c.signingUKID)
		if err != nil {
			return nil, fmt.Errorf("build registry auth header: %w", err)
		}
		req.Header.Set("Authorization", h)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry lookup request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("registry lookup non-2xx status=%d body=%s", resp.StatusCode, string(raw))
	}

	var items []LookupResponseItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parse registry lookup response: %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("registry lookup returned empty result")
	}

	pubKeyB64 := firstNonEmpty(items[0].SigningPublicKey, items[0].SigningPublicKey2, items[0].PubKey)
	if pubKeyB64 == "" {
		return nil, fmt.Errorf("registry lookup missing public key in response")
	}
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return nil, fmt.Errorf("decode registry public key: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid registry public key length %d (expected %d)", len(pubKeyBytes), ed25519.PublicKeySize)
	}

	exp := time.Now().Add(c.cacheTTL)
	if t, ok := parseExpiry(items[0]); ok && t.After(time.Now()) {
		exp = t
	}
	c.setCached(cacheKey, pubKeyBytes, exp)
	return pubKeyBytes, nil
}

func (c *HTTPClient) getCached(key string) (ed25519.PublicKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rec, ok := c.cache[key]
	if !ok || time.Now().After(rec.ExpiresAt) || len(rec.PubKey) == 0 {
		return nil, false
	}
	return rec.PubKey, true
}

func (c *HTTPClient) setCached(key string, pk ed25519.PublicKey, exp time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = Record{SubscriberID: key, PubKey: pk, ExpiresAt: exp}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func parseExpiry(it LookupResponseItem) (time.Time, bool) {
	raw := firstNonEmpty(it.ValidUntil, it.ValidTo)
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}

