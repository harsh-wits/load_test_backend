package session

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"seller_app_load_tester/internal/shared/redis"
)

const dualBucketLua = `
local function acquire_bucket(key, max_tokens, refill_rate, now, requested)
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
end

local global_key = KEYS[1]
local session_key = KEYS[2]
local global_max = tonumber(ARGV[1])
local global_refill = tonumber(ARGV[2])
local session_max = tonumber(ARGV[3])
local session_refill = tonumber(ARGV[4])
local now = tonumber(ARGV[5])
local requested = tonumber(ARGV[6])

if acquire_bucket(global_key, global_max, global_refill, now, requested) == 0 then
    return {0, "global"}
end
if acquire_bucket(session_key, session_max, session_refill, now, requested) == 0 then
    return {0, "session"}
end
return {1, "ok"}
`

type RateLimiter struct {
	r          redis.Client
	scriptSHA  string
	globalMax  int
	sessionMax int
	allowed       atomic.Int64
	deniedGlobal  atomic.Int64
	deniedSession atomic.Int64
	deniedOther   atomic.Int64
	redisErrors   atomic.Int64
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
	sha, err := rl.r.ScriptLoad(ctx, dualBucketLua)
	if err != nil {
		return fmt.Errorf("load rate limiter script: %w", err)
	}
	rl.scriptSHA = sha
	log.Printf("[rate_limiter] loaded lua script sha=%s global_rps=%d per_session_rps=%d",
		sha, rl.globalMax, rl.sessionMax)
	return nil
}

func (rl *RateLimiter) Acquire(ctx context.Context, sessionID string) bool {
	ok, _ := rl.AcquireWithReason(ctx, sessionID)
	return ok
}

func (rl *RateLimiter) AcquireWithReason(ctx context.Context, sessionID string) (bool, string) {
	if rl.scriptSHA == "" {
		rl.allowed.Add(1)
		return true, "script_not_loaded"
	}

	now := time.Now().UnixMilli()
	out, err := rl.r.EvalSha(
		ctx,
		rl.scriptSHA,
		[]string{"ratelimit:global", "ratelimit:session:" + sessionID},
		rl.globalMax, rl.globalMax, rl.sessionMax, rl.sessionMax, now, 1,
	)
	if err != nil {
		rl.redisErrors.Add(1)
		rl.deniedOther.Add(1)
		return false, "redis_error"
	}

	allowed, reason := parseAcquireResult(out)
	if allowed {
		rl.allowed.Add(1)
		return true, reason
	}
	switch reason {
	case "global":
		rl.deniedGlobal.Add(1)
	case "session":
		rl.deniedSession.Add(1)
	default:
		rl.deniedOther.Add(1)
	}
	return false, reason
}

type RateLimiterStats struct {
	Allowed       int64
	DeniedGlobal  int64
	DeniedSession int64
	DeniedOther   int64
	RedisErrors   int64
}

func (rl *RateLimiter) Stats() RateLimiterStats {
	return RateLimiterStats{
		Allowed:       rl.allowed.Load(),
		DeniedGlobal:  rl.deniedGlobal.Load(),
		DeniedSession: rl.deniedSession.Load(),
		DeniedOther:   rl.deniedOther.Load(),
		RedisErrors:   rl.redisErrors.Load(),
	}
}

func parseAcquireResult(v any) (bool, string) {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return false, "unknown"
	}
	allowed := toInt(items[0]) == 1
	if len(items) < 2 {
		if allowed {
			return true, "ok"
		}
		return false, "unknown"
	}
	reason := fmt.Sprint(items[1])
	return allowed, reason
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
