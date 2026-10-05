 -- KEYS[1]: rate limit key (e.g. "rate_limit:sw:user123")
 -- ARGV[1]: max requests allowed per window (integer)
 -- ARGV[2]: window duration in milliseconds (integer)
 -- ARGV[3]: current unix timestamp in milliseconds (integer)
 -- ARGV[4]: key TTL in seconds (integer)
 -- ARGV[5]: unique member identifier (string)
 -- Returns: { allowed (0 or 1), remaining_quota, retry_after_ms }

local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local ttl_seconds = tonumber(ARGV[4])
local member = ARGV[5]

local window_start = now_ms - window_ms

-- Remove all reqs older than sliding window boundary
redis.call("ZREMRANGEBYSCORE", key, "-inf", window_start)

-- Count how many reqs in current window
local current_requests = redis.call("ZCARD", key)

local allowed = 0
local remaining = 0
local retry_after_ms = 0

if current_requests < limit then
    -- Under the limit so record it with timestamp as score
    redis.call("ZADD", key, now_ms, member)
    allowed = 1
    remaining = limit - (current_requests + 1)
    retry_after_ms = 0
else
    -- Limit reached - find the oldest req in this window
    allowed = 0
    remaining = 0
    local oldest = redis.call("ZRANGE", key, 0, 0, "WITHSCORES")
    if oldest and #oldest >= 2 then
        -- oldest[2] is the score/timestamp of oldest req in window
        local oldest_time = tonumber(oldest[2])
        retry_after_ms = math.max(0, (oldest_time + window_ms) - now_ms)
    else
        retry_after_ms = window_ms
    end
end

-- Refresh TTL so inactive keys dont persist in memory
redis.call("EXPIRE", key, ttl_seconds)

return { allowed, remaining, retry_after_ms }
