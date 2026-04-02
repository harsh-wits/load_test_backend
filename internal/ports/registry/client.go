package registry

import (
	"context"
	"sync"
	"time"

	"crypto/ed25519"
)

type Record struct {
	SubscriberID string
	PubKey       ed25519.PublicKey
	ExpiresAt    time.Time
}

type Client interface {
	GetPublicKey(ctx context.Context, subscriberID string) (ed25519.PublicKey, error)
	GetPublicKeyByUKID(ctx context.Context, subscriberID, ukID string) (ed25519.PublicKey, error)
}

type mockClient struct {
	mu     sync.RWMutex
	record Record
}

func NewMockClient(pubKey ed25519.PublicKey, ttl time.Duration) Client {
	return &mockClient{
		record: Record{
			SubscriberID: "mock-subscriber",
			PubKey:       pubKey,
			ExpiresAt:    time.Now().Add(ttl),
		},
	}
}

func (c *mockClient) GetPublicKey(ctx context.Context, subscriberID string) (ed25519.PublicKey, error) {
	_ = ctx
	c.mu.RLock()
	defer c.mu.RUnlock()
	if time.Now().After(c.record.ExpiresAt) {
		return nil, nil
	}
	return c.record.PubKey, nil
}

func (c *mockClient) GetPublicKeyByUKID(ctx context.Context, subscriberID, ukID string) (ed25519.PublicKey, error) {
	_ = ukID
	return c.GetPublicKey(ctx, subscriberID)
}

