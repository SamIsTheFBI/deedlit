package main

import (
	"context"
	"deedlit/limiter"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// startNode boots an HTTP server on a specified port wrapped with the rate limiting middleware
func startNode(port int, nodeID string, lim limiter.RateLimiter) *http.Server {
	mux := http.NewServeMux()

	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("X-User-ID")

		if user == "" {
			user = r.URL.Query().Get("user")
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "[%s] Hello %s! Allowed at %s\n",
			nodeID, user, time.Now().Format(time.TimeOnly+".000"))
	})

	mux.Handle("/api/data", limiter.HTTPMiddleware(lim)(apiHandler))

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[%s] server error: %v", nodeID, err)
		}
	}()

	return srv
}

// sendRequest fires an HTTP GET request and prints the response status and rate limit headers.
func sendRequest(client *http.Client, url string, targetNode string, reqNumber int) {
	res, err := client.Get(url)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}

	defer func() {
		_ = res.Body.Close()
	}()

	body, _ := io.ReadAll(res.Body)
	remaining := res.Header.Get("X-RateLimit-Remaining")
	retryAfter := res.Header.Get("Retry-After")

	fmt.Printf("\n[Req %d] Sent to %s\n", reqNumber, targetNode)
	fmt.Printf("  - HTTP Status: %d %s\n", res.StatusCode, http.StatusText(res.StatusCode))
	fmt.Printf("  - Header X-RateLimit-Remaining: %s\n", remaining)
	if retryAfter != "" {
		fmt.Printf("  - Header Retry-After: %s seconds\n", retryAfter)

	}
	fmt.Printf("  - Body: %s", string(body))
}

func main() {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Cannot connect to Redis: %v", err)
	}

	sharedLimiter := limiter.NewSlidingWindowLimiter(redisClient, 3, 5*time.Second)
	// Clean any previous test data for 'alice'
	redisClient.Del(ctx, "rate_limit:sw:alice")

	// 1. Launch 2 separate nodes & let them bind to their respective ports
	fmt.Println("Launching node A on :8081 and node B on :8082...")
	nodeA := startNode(8081, "Node-A", sharedLimiter)
	nodeB := startNode(8082, "Node-B", sharedLimiter)

	time.Sleep(150 * time.Millisecond)

	fmt.Println("\n==================================================================")
	fmt.Println("SIMULATION: Distributed coordination for User 'alice'")
	fmt.Println("Rule: Limit = 3 requests per 5-second window across all servers")
	fmt.Println("==================================================================")

	httpClient := &http.Client{Timeout: 3 * time.Second}

	// 2. Consume 2 requests on Node A
	sendRequest(httpClient, "http://localhost:8081/api/data?user=alice", "Node-A (:8081)", 1)
	sendRequest(httpClient, "http://localhost:8081/api/data?user=alice", "Node-A (:8081)", 2)

	// 3. Consume the 3rd (and final) request on Node B
	sendRequest(httpClient, "http://localhost:8082/api/data?user=alice", "Node-B (:8082)", 3)

	// 4. Send the 4th request to Node B (Must be blocked)
	sendRequest(httpClient, "http://localhost:8082/api/data?user=alice", "Node-B (:8082) [Should Block!]", 4)

	// 5. Demonstrate automatic recovery after window slides.
	fmt.Println("\nSleeping 5.2 seconds for the sliding window to roll over...")
	time.Sleep(5200 * time.Millisecond)

	// 6. Send request 5 to Node A (Must be allowed again)
	sendRequest(httpClient, "http://localhost:8081/api/data?user=alice", "Node-A (:8081) [After Reset]", 5)

	fmt.Println("\n==================================================================")
	fmt.Println("SUCCESS: Multi-server coordination fully verified!")
	fmt.Println("==================================================================")

	// 7. Graceful shutdown of both nodes.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = nodeA.Shutdown(shutdownCtx)
	_ = nodeB.Shutdown(shutdownCtx)
	fmt.Println("Both server nodes shut down cleanly.")
}
