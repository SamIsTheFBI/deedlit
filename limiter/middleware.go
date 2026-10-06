package limiter

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
)

func HTTPMiddleware(lim RateLimiter) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// prioritize X-User-ID header, then query param ?user=, fallback to client IP
			userID := r.Header.Get("X-User-ID")

			if userID == "" {
				userID = r.URL.Query().Get("user")
			}

			if userID == "" {
				userID = r.RemoteAddr
			}

			// evaluate quota with req's context
			res, err := lim.Allow(r.Context(), userID)
			if err != nil {
				log.Printf("Rate limiter evaluation failed: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))

			if !res.Allowed {
				// Retry-After header expects duration in whole seconds so rounded up int64
				retryAfterSec := max(int64(math.Ceil(res.ResetAfter.Seconds())), 1)

				w.Header().Set("Retry-After", strconv.FormatInt(retryAfterSec, 10))
				http.Error(w, fmt.Sprintf("Too many requests. Try again in %d seconds\n", retryAfterSec), http.StatusTooManyRequests)
				return
			}

			h.ServeHTTP(w, r)
		})
	}
}
