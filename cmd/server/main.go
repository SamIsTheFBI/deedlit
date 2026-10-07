package main

import (
	"context"
	"deedlit/limiter"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	nodeID := flag.String("node", "Node-1", "Identifier for this server instance")
	algo := flag.String("algo", "sliding_window", "Rate limit algorithm: 'sliding_window' or 'token_bucket'")
	redisAddr := flag.String("redis", "localhost:6379", "Redis server address")
	flag.Parse()

	redisClient := redis.NewClient(&redis.Options{
		Addr: *redisAddr,
	})

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("[%s] Cannot connect to Redis at %s: %v", *nodeID, *redisAddr, err)
	}

	var rateLimiter limiter.RateLimiter
	switch *algo {
	case "token_bucket":
		// Capacity: 5 tokens, Refill: 2 tokens/s
		rateLimiter = limiter.NewTokenBucketLimiter(redisClient, 5, 2.0)
		log.Printf("[%s] Initialized Token Bucket Limiter (Capacity: 5, Refill: 2.0/s)", *nodeID)
	case "sliding_window":
		// Limit: 5 reqs per 10s rolling window
		rateLimiter = limiter.NewSlidingWindowLimiter(redisClient, 5, 10*time.Second)
		log.Printf("[%s] Initialized Sliding Window Limiter (Limit: 5 reqs / 10s)", *nodeID)
	default:
		log.Fatalf("Unknown algorithm: %s (choose 'token_bucket' or 'sliding_window')", *algo)
	}

	mux := http.NewServeMux()

	// Unprotected health-check endpoint.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, _ = fmt.Fprintf(w, "[%s] Status: Healthy\n", *nodeID)

	})

	// Protected business API endpoint wrapped with rate limiting middleware.
	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("X-User-ID")

		if user == "" {
			user = r.URL.Query().Get("user")
		}

		if user == "" {
			user = r.RemoteAddr
		}

		w.WriteHeader(http.StatusOK)

		_, _ = fmt.Fprintf(w, "[%s] Successfully processed request for '%s' at %s\n",
			*nodeID, user, time.Now().Format("10:10:10.000"))
	})

	// Wrap apiHandler with limiter.HTTPMiddleware.
	mux.Handle("/api/data", limiter.HTTPMiddleware(rateLimiter)(apiHandler))

	serverAddr := fmt.Sprintf(":%d", *port)
	log.Printf("[%s] Server listening on http://localhost%s ...", *nodeID, serverAddr)
	if err := http.ListenAndServe(serverAddr, mux); err != nil {
		log.Fatalf("[%s] Server failed: %v", *nodeID, err)
	}
}
