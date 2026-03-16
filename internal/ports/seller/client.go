package seller

import "context"

type Client interface {
	Search(ctx context.Context, baseURL string, payload []byte) ([]byte, error)
	Select(ctx context.Context, baseURL string, payload []byte) error
	Init(ctx context.Context, baseURL string, payload []byte) error
	Confirm(ctx context.Context, baseURL string, payload []byte) error
}

