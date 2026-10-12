# Distributed Rate Limiter in Go & Redis

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Redis](https://img.shields.io/badge/Redis-v7.0+-DC382D?style=flat&logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A high-performance, race-condition-proof distributed rate limiting engine built in Go. Designed to coordinate traffic limits across multiple stateless application servers using Redis and atomic Lua scripts.

Supports pluggable rate-limiting strategies (**Token Bucket** and **Sliding Window Log**), RFC-compliant HTTP middleware, dynamic key expiration (TTLs) to prevent memory leaks, and sub-millisecond multi-server synchronization.

---

## Architecture

In microservice environments, local in-memory rate limiters fail because traffic is spread across multiple nodes behind a load balancer. This system keeps application servers completely stateless by centralizing quota evaluation in Redis via atomic Lua scripts:

```text
                        Incoming Traffic (Load Balancer)
                                     │
                    ┌────────────────┴────────────────┐
                    ▼                                 ▼
         Server Node A (:8081)             Server Node B (:8082)
                    │                                 │
                    │   HTTP Rate Limiting Middleware │
                    ▼                                 ▼
         ┌────────────────────────────────────────────────────┐
         │        Shared Redis Atomic Consensus Layer         │
         │  • Embedded Lua Scripts (EVALSHA single round-trip)│
         │  • Dynamic TTL Expiration (Zero Memory Leaks)      │
         └────────────────────────────────────────────────────┘
```

---

## Key Features & Engineering Highlights

- **Atomic Multi-Server Coordination:** Eliminates read-modify-write race conditions across server nodes by executing limit evaluation atomically inside Redis via Lua scripts.
- **Embedded Lua Scripts (`//go:embed`):** Scripts are compiled directly into the Go binary at build time using modern `go:embed`, ensuring single-binary portability with zero runtime filesystem dependencies.
- **Zero Background Timers (Lazy Refill):** Rather than spawning millions of background timers for active users, the Token Bucket calculates token replenishment mathematically on-demand upon request arrival ($\text{elapsed} \times \text{refill\_rate}$).
- **Collision-Proof Sliding Window:** Solves member overwrites in Redis Sorted Sets (`ZSET`) under high concurrency by generating member IDs composed of `Nanoseconds + Atomic Monotonic Counter + Random Noise`.
- **Memory Leak Protection via Dynamic TTLs:** Redis keys automatically calculate and refresh their TTL based on refill/window duration, ensuring inactive client records are evicted automatically.
- **Plug-and-Play HTTP Middleware:** Implements idiomatic `func(http.Handler) http.Handler` middleware supporting `X-User-ID`, query parameters, and IP fallback with standard RFC headers (`429 Too Many Requests`, `Retry-After`, `X-RateLimit-Remaining`).

---

## Algorithms Implemented

### 1. Token Bucket (`limiter/token_bucket.go`)
- **Best for:** Smoothing out traffic while permitting controlled bursts (e.g. initial page loads).
- **How it works:** Each client has a bucket of capacity $C$ refilled at rate $R$ tokens/sec. When a request arrives, tokens are refilled lazily:
  $$\text{tokens} = \min(C, \text{current} + (\text{now} - \text{last\_updated}) \times R)$$
- If $\ge 1$ token exists, it is decremented and allowed. Otherwise, returns wait duration until the next token.

### 2. Sliding Window Log (`limiter/sliding_window.go`)
- **Best for:** Strict API quotas (e.g., maximum 10 requests per 60 seconds) with zero boundary burst flaws.
- **How it works:** Uses a Redis Sorted Set (`ZSET`) where scores are millisecond timestamps:
  1. `ZREMRANGEBYSCORE`: Prunes all timestamps older than $(\text{now} - \text{window})$.
  2. `ZCARD`: Counts requests remaining in the active rolling window.
  3. `ZADD`: Records the new request timestamp if under limit; otherwise calculates wait duration until the oldest request slides out.

---

## Project Structure

```text
deedlit/
├── cmd/
│   ├── server/
│   │   └── main.go       # Standalone configurable server (CLI flags: -port, -algo, -node)
│   └── demo/
│       └── main.go       # Multi-server coordination simulation runner
├── limiter/
│   ├── limiter.go        # RateLimiter interface & Result struct
│   ├── middleware.go     # HTTP Middleware with standard 429 & Retry-After headers
│   ├── token_bucket.go   # Distributed Token Bucket implementation
│   ├── sliding_window.go # Distributed Sliding Window Log implementation
│   ├── limiter_test.go   # Integration test suite against live Redis
│   └── scripts/
│       ├── token_bucket.lua   # Atomic Lua script for Token Bucket
│       └── sliding_window.lua # Atomic Lua script for Sliding Window
├── go.mod
└── go.sum
```

---

## Quickstart

### 1. Prerequisites
Ensure Go 1.22+ and Redis are installed and running:
```bash
# Start Redis locally
brew services start redis  # or: docker run -d -p 6379:6379 redis:alpine
redis-cli ping             # Returns PONG
```

### 2. Run Integration Tests
Validates burst exhaustion, Redis TTL creation, and replenishment intervals:
```bash
go test -v ./limiter/...
```

### 3. Run the Multi-Server Coordination Demo
Boots two isolated HTTP server nodes on ports `:8081` and `:8082`, and demonstrates real-time cross-node limit enforcement:
```bash
go run cmd/demo/main.go
```

**Sample Output:**
```text
Launching node A on :8081 and node B on :8082...

==================================================================
SIMULATION: Distributed coordination for User 'alice'
Rule: Limit = 3 requests per 5-second window across all servers
==================================================================

[Req 1] Sent to Node-A (:8081)
  - HTTP Status: 200 OK
  - Header X-RateLimit-Remaining: 2

[Req 2] Sent to Node-A (:8081)
  - HTTP Status: 200 OK
  - Header X-RateLimit-Remaining: 1

[Req 3] Sent to Node-B (:8082)
  - HTTP Status: 200 OK
  - Header X-RateLimit-Remaining: 0

[Req 4] Sent to Node-B (:8082) [Should Block!]
  - HTTP Status: 429 Too Many Requests
  - Header X-RateLimit-Remaining: 0
  - Header Retry-After: 5 seconds

Sleeping 5.2 seconds for the sliding window to roll over...

[Req 5] Sent to Node-A (:8081) [After Reset]
  - HTTP Status: 200 OK
  - Header X-RateLimit-Remaining: 2
```

### 4. Running a Standalone Server Node
You can run standalone instances configured via CLI flags:
```bash
go run cmd/server/main.go -port=8080 -algo=sliding_window -node=Node-1
```

Test with `curl`:
```bash
curl -i "http://localhost:8080/api/data?user=alice"
```
