package limiter

import (
	"context"
	_ "embed"
	"fmt"
	"math"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

var swSeq uint64

//go:embed scripts/sliding_window.lua
var slidingWindowLua string

var slidingWindowScript = redis.NewScript(slidingWindowLua)

type SlidingWindowLimiter struct {
	client *redis.Client
	limit  int64
	window time.Duration
	ttl    time.Duration
}

var _ RateLimiter = (*SlidingWindowLimiter)(nil)

func NewSlidingWindowLimiter(client *redis.Client, limit int64, window time.Duration) *SlidingWindowLimiter {
	ttl := window * 2 // set TTL to double the window size to give enough buffer for rolling queries

	return &SlidingWindowLimiter{
		client: client,
		limit:  limit,
		window: window,
		ttl:    ttl,
	}
}

func (sw *SlidingWindowLimiter) Allow(ctx context.Context, key string) (Result, error) {
	redisKey := fmt.Sprintf("rate_limit:sw:%s", key)
	now := time.Now()
	nowMs := now.UnixNano() / int64(time.Millisecond)
	windowMs := sw.window.Milliseconds()
	ttlSeconds := int64(math.Ceil(sw.ttl.Seconds()))

	seq := atomic.AddUint64(&swSeq, 1)
	member := fmt.Sprintf("%d-%d-%d", now.UnixNano(), seq, rand.Int63())

	rawResult, err := slidingWindowScript.Run(
		ctx, sw.client,
		[]string{redisKey},
		sw.limit,
		windowMs,
		nowMs,
		ttlSeconds,
		member,
	).Slice()

	if err != nil {
		return Result{}, fmt.Errorf("failed to run sliding window script: %w", err)
	}

	allowed := rawResult[0].(int64) == 1
	remaining := rawResult[1].(int64)
	retryAfterMs := rawResult[2].(int64)

	return Result{
		Allowed:    allowed,
		Remaining:  remaining,
		ResetAfter: time.Duration(retryAfterMs) * time.Millisecond,
	}, nil
}
