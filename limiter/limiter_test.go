package limiter

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func setupRedis(t *testing.T) *redis.Client {
	t.Helper()

	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("failed to connect to redis: %v", err)
	}

	return client
}

func TestTokenBucketLimiter(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()

	tb := NewTokenBucketLimiter(client, 3, 2.0)
	key := "test:user:tb"

	t.Cleanup(func() {
		client.Del(ctx, "rate_limit:tb:"+key)
	})

	// Consume all 3 tokens in a burst
	for i := range 3 {
		res, err := tb.Allow(ctx, key)

		if err != nil {
			t.Fatalf("unexpected error on request %d: %v", i+1, err)
		}

		if !res.Allowed {
			t.Fatalf("request %d should've been allowed", i+1)
		}
	}

	// 4th req must be rejected
	res, err := tb.Allow(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error on 4th req: %v", err)
	}

	if res.Allowed {
		t.Fatalf("expected 4th req to be blocked but was allowed")
	}

	if res.ResetAfter <= 0 {
		t.Errorf("expected ResetAfter > 0, got %v", res.ResetAfter)
	}

	// Verify Redis TTL was set and is positive
	ttl, err := client.TTL(ctx, "rate_limit:tb:"+key).Result()
	if err != nil || ttl <= 0 {
		t.Errorf("expected positive TTL in redis, got %v (err: %v)", ttl, err)
	}

	// Wait 600ms (at 2 tokens/sec, ~1.2 tokens refill).
	time.Sleep(600 * time.Millisecond)

	res, err = tb.Allow(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error after refill: %v", err)

	}
	if !res.Allowed {
		t.Fatalf("expected request to be allowed after token refill")
	}
}

func TestSlidingWindowLimiter(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()

	// Parameters: limit = 2 requests per 500ms window
	window := 500 * time.Millisecond
	sw := NewSlidingWindowLimiter(client, 2, window)
	key := "test:user:sw"

	t.Cleanup(func() {
		client.Del(ctx, "rate_limit:sw:"+key)

	})

	// 1. First 2 requests within window must pass.
	for i := 0; i < 2; i++ {
		res, err := sw.Allow(ctx, key)

		if err != nil {

			t.Fatalf("unexpected error on request %d: %v", i+1, err)

		}
		if !res.Allowed {
			t.Fatalf("request %d should have been allowed", i+1)
		}
	}

	// 2. 3rd request in the same window must be rejected.
	res, err := sw.Allow(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error on 3rd request: %v", err)
	}
	if res.Allowed {
		t.Fatalf("expected 3rd request to be blocked, but was allowed")
	}

	// 3. Verify Redis TTL was set.
	ttl, err := client.TTL(ctx, "rate_limit:sw:"+key).Result()
	if err != nil || ttl <= 0 {
		t.Errorf("expected positive TTL in redis, got %v (err: %v)", ttl, err)
	}

	// 4. Sleep past the 500ms window so old requests slide out.
	time.Sleep(550 * time.Millisecond)

	res, err = sw.Allow(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error after window elapsed: %v", err)
	}
	if !res.Allowed {
		t.Fatalf("expected request to be allowed after sliding window cleared")
	}
}
