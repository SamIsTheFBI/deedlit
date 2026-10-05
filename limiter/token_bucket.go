package limiter

import (
	"context"
	_ "embed"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed scripts/token_bucket.lua
var tokenBucketLua string

var tokenBucketScript = redis.NewScript(tokenBucketLua)

type TokenBucketLimiter struct {
	client     *redis.Client
	capacity   int64
	refillRate float64
	ttl        time.Duration
}

var _ RateLimiter = (*TokenBucketLimiter)(nil)

func NewTokenBucketLimiter(client *redis.Client, capacity int64, refillRate float64) *TokenBucketLimiter {
	fillSeconds := math.Ceil(float64(capacity) / refillRate)
	ttl := time.Duration(fillSeconds*2) * time.Second

	return &TokenBucketLimiter{
		client:     client,
		capacity:   capacity,
		refillRate: refillRate,
		ttl:        ttl,
	}
}

// Allow evaluates whether a request identified by key is allowed to proceed.
func (tb *TokenBucketLimiter) Allow(ctx context.Context, key string) (Result, error) {
	redisKey := fmt.Sprintf("rate_limit:tb:%s", key)
	nowSeconds := float64(time.Now().UnixNano()) / 1e9
	ttlSeconds := int64(math.Ceil(tb.ttl.Seconds()))

	rawResult, err := tokenBucketScript.Run(
		ctx, tb.client,
		[]string{redisKey},
		tb.capacity,
		tb.refillRate,
		nowSeconds,
		1,
		ttlSeconds,
	).Slice()

	if err != nil {
		return Result{}, fmt.Errorf("failed to run token bucket script: %w", err)
	}

	// Unpack lua script's return values
	allowed := rawResult[0].(int64) == 1
	remaining := rawResult[1].(int64)
	retryAfterMs := rawResult[2].(int64)

	return Result{
		Allowed:    allowed,
		Remaining:  remaining,
		ResetAfter: time.Duration(retryAfterMs) * time.Millisecond,
	}, nil
}
