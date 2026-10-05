-- KEYS[1]: rate limit key (e.g. "rate_limit:tb:user123")
-- ARGV[1]: bucket capacity (number)
-- ARGV[2]: refill rate in tokens/sec (number)
-- ARGV[3]: current unix timestamp in seconds with fractional precision (number)
-- ARGV[4]: requested tokens, usually 1 (number)
-- ARGV[5]: TTL in seconds (integer)
-- Returns: { allowed (0 or 1), remaining_tokens, retry_after_ms }

local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

-- Fetch current bucket state from Redis hash.
local data = redis.call("HMGET", key, "tokens", "last_updated")
local tokens = tonumber(data[1])
local last_updated = tonumber(data[2])

if tokens == nil then
    -- First request for this key: start with a full bucket.
    tokens = capacity
    last_updated = now
else
    -- Compute replenished tokens based on elapsed time.
    local elapsed = math.max(0, now - last_updated)
    tokens = math.min(capacity, tokens + (elapsed * refill_rate))
    last_updated = now
end

local allowed = 0
local retry_after_ms = 0

if tokens >= requested then
    -- Sufficient tokens: consume and allow.
    tokens = tokens - requested
    allowed = 1
else
    -- Insufficient tokens: calculate wait time until enough tokens replenish.
    local missing = requested - tokens
    retry_after_ms = math.ceil((missing / refill_rate) * 1000)
end

-- Save updated state and refresh the key's TTL.
redis.call("HMSET", key, "tokens", tokens, "last_updated", last_updated)
redis.call("EXPIRE", key, ttl)

return { allowed, math.floor(tokens), retry_after_ms }
