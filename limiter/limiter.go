package limiter

import (
	"context"
	"time"
)

type Result struct {
	Allowed    bool
	Remaining  int64
	ResetAfter time.Duration
}

type RateLimiter interface {
	Allow(ctx context.Context, key string) (Result, error)
}
