package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	appconfig "seller_app_load_tester/internal/config"
)

type Client interface {
	Ping(ctx context.Context) error
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Get(ctx context.Context, key string) ([]byte, error)
	Del(ctx context.Context, key string) error
	Keys(ctx context.Context, pattern string) ([]string, error)
	HSet(ctx context.Context, key, field string, value []byte) error
	HGet(ctx context.Context, key, field string) ([]byte, error)
	HGetAll(ctx context.Context, key string) (map[string][]byte, error)
	HMGet(ctx context.Context, key string, fields ...string) ([][]byte, error)
	Incr(ctx context.Context, key string) (int64, error)
	IncrBy(ctx context.Context, key string, value int64) (int64, error)
	SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	Exists(ctx context.Context, key string) (bool, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	DelPattern(ctx context.Context, pattern string) error
	EvalSha(ctx context.Context, sha string, keys []string, args ...any) (any, error)
	ScriptLoad(ctx context.Context, script string) (string, error)
	Close() error
}

type client struct {
	raw *redis.Client
}

func NewClient(cfg *appconfig.Config) (Client, error) {
	opts, err := redis.ParseURL(fmt.Sprintf("redis://%s", strings.TrimPrefix(cfg.RedisURL, "redis://")))
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	opts.Password = cfg.RedisPassword
	opts.DB = cfg.RedisDB

	rc := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rc.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &client{raw: rc}, nil
}

func (c *client) Ping(ctx context.Context) error {
	return c.raw.Ping(ctx).Err()
}

func (c *client) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.raw.Set(ctx, key, value, ttl).Err()
}

func (c *client) Get(ctx context.Context, key string) ([]byte, error) {
	res, err := c.raw.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	return res, err
}

func (c *client) Del(ctx context.Context, key string) error {
	return c.raw.Del(ctx, key).Err()
}

func (c *client) Keys(ctx context.Context, pattern string) ([]string, error) {
	var cursor uint64
	var keys []string
	for {
		k, nextCursor, err := c.raw.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, k...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return keys, nil
}

func (c *client) HSet(ctx context.Context, key, field string, value []byte) error {
	return c.raw.HSet(ctx, key, field, value).Err()
}

func (c *client) HGet(ctx context.Context, key, field string) ([]byte, error) {
	res, err := c.raw.HGet(ctx, key, field).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	return res, err
}

func (c *client) HGetAll(ctx context.Context, key string) (map[string][]byte, error) {
	res, err := c.raw.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(res))
	for k, v := range res {
		out[k] = []byte(v)
	}
	return out, nil
}

func (c *client) HMGet(ctx context.Context, key string, fields ...string) ([][]byte, error) {
	res, err := c.raw.HMGet(ctx, key, fields...).Result()
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(res))
	for i, v := range res {
		if v != nil {
			out[i] = []byte(v.(string))
		}
	}
	return out, nil
}

func (c *client) Incr(ctx context.Context, key string) (int64, error) {
	return c.raw.Incr(ctx, key).Result()
}

func (c *client) IncrBy(ctx context.Context, key string, value int64) (int64, error) {
	return c.raw.IncrBy(ctx, key, value).Result()
}

func (c *client) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	return c.raw.SetNX(ctx, key, value, ttl).Result()
}

func (c *client) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.raw.Exists(ctx, key).Result()
	return n > 0, err
}

func (c *client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.raw.Expire(ctx, key, ttl).Err()
}

func (c *client) DelPattern(ctx context.Context, pattern string) error {
	keys, err := c.Keys(ctx, pattern)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	return c.raw.Del(ctx, keys...).Err()
}

func (c *client) EvalSha(ctx context.Context, sha string, keys []string, args ...any) (any, error) {
	return c.raw.EvalSha(ctx, sha, keys, args...).Result()
}

func (c *client) ScriptLoad(ctx context.Context, script string) (string, error) {
	return c.raw.ScriptLoad(ctx, script).Result()
}

func (c *client) Close() error {
	return c.raw.Close()
}

