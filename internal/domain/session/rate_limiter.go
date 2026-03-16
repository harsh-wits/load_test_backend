package session

import (
	"context"
	"fmt"
	"log"
	"time"

	"seller_app_load_tester/internal/shared/redis"
)

const tokenBucketLua = `
local key = KEYS[1]
local max_tokens = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local bucket = redis.call('HMGET', key, 'tokens', 'last')
local tokens = tonumber(bucket[1])
local last = tonumber(bucket[2])

if tokens == nil then
    tokens = max_tokens
    last = now
end

local elapsed = now - last
local refill = elapsed * refill_rate / 1000
tokens = math.min(max_tokens, tokens + refill)

if tokens < requested then
    redis.call('HMSET', key, 'tokens', tokens, 'last', now)
    redis.call('EXPIRE', key, 10)
    return 0
end

tokens = tokens - requested
redis.call('HMSET', key, 'tokens', tokens, 'last', now)
redis.call('EXPIRE', key, 10)
return 1
`

type RateLimiter struct {
	r          redis.Client
	scriptSHA  string
	globalMax  int
	sessionMax int
}

func NewRateLimiter(r redis.Client, globalRPS, perSessionRPS int) *RateLimiter {
	if globalRPS <= 0 {
		globalRPS = 2000
	}
	if perSessionRPS <= 0 {
		perSessionRPS = 150
	}
	return &RateLimiter{
		r:          r,
		globalMax:  globalRPS,
		sessionMax: perSessionRPS,
	}
}

func (rl *RateLimiter) Init(ctx context.Context) error {
	sha, err := rl.r.ScriptLoad(ctx, tokenBucketLua)
	if err != nil {
		return fmt.Errorf("load rate limiter script: %w", err)
	}
	rl.scriptSHA = sha
	log.Printf("[rate_limiter] loaded lua script sha=%s global_rps=%d per_session_rps=%d",
		sha, rl.globalMax, rl.sessionMax)
	return nil
}

func (rl *RateLimiter) Acquire(ctx context.Context, sessionID string) bool {
	if rl.scriptSHA == "" {
		return true
	}

	now := time.Now().UnixMilli()

	globalOk, _ := rl.r.EvalSha(ctx, rl.scriptSHA,
		[]string{"ratelimit:global"},
		rl.globalMax, rl.globalMax, now, 1,
	)
	if toInt(globalOk) == 0 {
		return false
	}

	sessionOk, _ := rl.r.EvalSha(ctx, rl.scriptSHA,
		[]string{"ratelimit:session:" + sessionID},
		rl.sessionMax, rl.sessionMax, now, 1,
	)
	return toInt(sessionOk) == 1
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}
