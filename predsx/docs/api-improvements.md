# API Improvements

This document covers the 7 improvements implemented on top of the original API boilerplate.

---

## 1. Cursor-Based Pagination

**Endpoint:** `GET /v1/markets`

**Problem:** The original handler used raw `offset` integers in query params. Offset pagination breaks when data is inserted mid-page — callers get skipped or duplicate rows.

**What changed:**
- Response format changed from a plain array to a page object:
  ```json
  { "data": [...], "next_cursor": "Mg==", "has_more": true }
  ```
- `next_cursor` is a base64-encoded opaque token (encodes the integer offset internally).
- Callers pass `?cursor=<token>` to fetch the next page. The raw `?offset=` param still works as a fallback.

**Helpers in `handlers/rest.go`:**
```go
func encodeCursor(offset int) string {
    return base64.URLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeCursor(cursor string) int {
    b, _ := base64.URLEncoding.DecodeString(cursor)
    n, _ := strconv.Atoi(string(b))
    return n
}
```

**Example — paginating through active markets:**
```bash
# Page 1 (limit=2)
curl "http://localhost:8088/v1/markets?limit=2&status=ACTIVE"
# → next_cursor: "Mg=="  (base64 of "2")

# Page 2
curl "http://localhost:8088/v1/markets?limit=2&cursor=Mg=="
# → next_cursor: "BA=="  (base64 of "4")

# Page 3
curl "http://localhost:8088/v1/markets?limit=2&cursor=BA=="
# → has_more: false — end of results
```

**Response structure:**
```json
{
  "data": [
    {
      "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
      "title": "Will Bitcoin exceed $100,000 by end of December 2024?",
      "status": "ACTIVE",
      "yes_price": 0.63,
      "no_price": 0.37
    },
    {
      "market_id": "0x1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c",
      "title": "Will Bitcoin ETF get SEC approval before June 2024?",
      "status": "ACTIVE",
      "yes_price": 0.88,
      "no_price": 0.12
    }
  ],
  "next_cursor": "Mg==",
  "has_more": true
}
```

**Files:** `handlers/rest.go`

> **Frontend note:** The response is no longer a plain array. Use `response.data` to access the market list.

---

## 2. Redis Cache Layer

**Problem:** Hot endpoints like `/markets/top` and `/markets/search` hit ClickHouse on every request. ClickHouse is fast but not designed for per-request low-latency reads.

**What changed:** A cache layer was added using two helpers:
```go
func (h *APIHandler) cacheGetJSON(ctx context.Context, key string, dest interface{}) bool {
    raw, err := h.Redis.Get(ctx, key).Result()
    if err != nil || raw == "" {
        return false
    }
    return json.Unmarshal([]byte(raw), dest) == nil
}

func (h *APIHandler) cacheSetJSON(ctx context.Context, key string, v interface{}, ttl time.Duration) {
    b, _ := json.Marshal(v)
    h.Redis.Set(ctx, key, string(b), ttl)
}
```

**Cache key patterns and TTLs:**

| Endpoint | Cache Key Pattern | TTL |
|----------|-------------------|-----|
| `GET /v1/markets` | `cache:markets:{exchange}:{status}:{limit}:{offset}` | 30s |
| `GET /v1/markets/top` | `cache:markets:top:{by}:{period}:{limit}` | 15s |
| `GET /v1/markets/search` | `cache:search:{q}:{exchange}:{status}:{limit}` | 30s |
| `GET /v1/markets/{id}/stats` | `cache:stats:{id}` | 30s |
| `GET /v1/markets/{id}/summary` | `cache:summary:{id}` | 10s |

**Example — cache hit header:**
```bash
# First request — goes to ClickHouse
curl -I "http://localhost:8088/v1/markets/top?by=volume&period=24h"
# HTTP/1.1 200 OK
# Content-Type: application/json
# (no X-Cache header)

# Second request within 15s — served from Redis
curl -I "http://localhost:8088/v1/markets/top?by=volume&period=24h"
# HTTP/1.1 200 OK
# Content-Type: application/json
# X-Cache: HIT
```

**Files:** `handlers/rest.go`

---

## 3. Rate Limiting

**Problem:** No protection against excessive requests — a single client could flood the API.

**What changed:** A new `RateLimit` middleware in `middleware/ratelimit.go` using the Redis INCR+EXPIRE pattern (sliding window, resets every 60 seconds).

- **Limit:** 60 requests/minute per client IP
- **IP detection order:** `X-Forwarded-For` → `X-Real-IP` → `RemoteAddr` (proxy-aware)
- **Fail open:** If Redis is unavailable, the request is allowed through
- **Headers:** Every response includes `X-RateLimit-Limit` and `X-RateLimit-Remaining`

**Redis keys used:**
```
ratelimit:{client-ip}  → INCR'd on each request, EXPIRE set to 60s on first hit
```

**Response headers on every `/v1/*` request:**
```
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 47
```

**When limit is exceeded (HTTP 429):**
```json
{ "error": "rate limit exceeded", "retry_after": "60s" }
```

**Example — hitting the limit:**
```bash
# 60th request — still allowed
curl -I "http://localhost:8088/v1/markets"
# X-RateLimit-Remaining: 1

# 61st request — blocked
curl "http://localhost:8088/v1/markets"
# HTTP/1.1 429 Too Many Requests
# {"error":"rate limit exceeded","retry_after":"60s"}
```

**Files:** `middleware/ratelimit.go`, `libs/redis-client/redis.go`

---

## 4. `GET /v1/markets/{id}` Auto-Enrichment

**Problem:** To show a market with its 24h stats, the frontend had to make two requests: `GET /v1/markets/{id}` and `GET /v1/markets/{id}/stats`.

**What changed:**
A shared `fetchMarketStats` helper queries `price_history_1m` for the last 24 hours and returns OHLCV aggregates. `GetMarket` now calls it automatically and embeds `stats_24h` in the response.

```go
// ClickHouse query inside fetchMarketStats
SELECT
    sum(volume)              AS volume_24h,
    sum(trade_count)         AS trades_24h,
    argMin(close, timestamp) AS open_price,
    argMax(close, timestamp) AS close_price,
    min(close)               AS low_24h,
    max(close)               AS high_24h
FROM price_history_1m
WHERE market_id IN (?, ?) AND timestamp >= ?   -- last 24h
```

**Example response — stats embedded inline:**
```bash
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b"
```
```json
{
  "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
  "title": "Will Bitcoin exceed $100,000 by end of December 2024?",
  "status": "ACTIVE",
  "yes_price": 0.63,
  "no_price": 0.37,
  "stats_24h": {
    "volume_24h": 12480000.0,
    "trades_24h": 4820,
    "open_price": 0.52,
    "close_price": 0.63,
    "low_24h": 0.48,
    "high_24h": 0.65,
    "price_change_pct": 21.15
  }
}
```

Stats are omitted (not `null`) if ClickHouse returns an error — the market response still succeeds.

**Files:** `handlers/rest.go`

---

## 5. WebSocket Per-Market Subscription Filtering

**Problem:** Every connected WebSocket client received every event for every market — wasteful for clients that only care about one or two markets.

**What changed:**

**Hub (`ws/hub.go`):**
- `broadcast` channel changed from `chan []byte` to `chan broadcastMsg{data []byte, marketID string}`
- New `subscribe` channel (`chan subscribeCmd`) handles client filter updates
- Broadcast loop skips clients whose subscription set does not include the event's `marketID`
- Clients with an empty subscription set receive all events (default)

**Client (`ws/client.go`):**
- Added `subscriptions map[string]bool` field to `Client`
- `readPump` parses incoming JSON and dispatches subscribe/unsubscribe commands

**Broadcaster (`ws/broadcaster.go`):**
- All 4 Kafka consumers call `hub.BroadcastToMarket(data, evt.MarketID)` instead of `hub.Broadcast(data)`

**Example usage from a browser client:**
```javascript
const ws = new WebSocket("ws://localhost:8088/stream");

// By default: receive all markets
ws.onmessage = (e) => console.log(JSON.parse(e.data));

// Subscribe to only 2 markets
ws.send(JSON.stringify({
  action: "subscribe",
  markets: [
    "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "0x3a4b2c1d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b"
  ]
}));
// Now only receives events for those 2 markets

// Unsubscribe — go back to receiving all markets
ws.send(JSON.stringify({ action: "unsubscribe", markets: [] }));
```

**Incoming trade event format (after subscription):**
```json
{
  "type": "trade",
  "data": {
    "trade_id": "0xf1e2d3c4b5a6978869504132231415161718191a",
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "price": 0.632,
    "size": 500.00,
    "side": "BUY",
    "token": "0xabcd1234..."
  },
  "timestamp": "2026-06-11T12:00:00Z"
}
```

**Files:** `ws/hub.go`, `ws/client.go`, `ws/broadcaster.go`

---

## 6. Debug Endpoint Authentication

**Problem:** `/debug/*` endpoints exposed raw internal data with no access control.

**What changed:** A `DebugAuth` middleware in `middleware/auth.go`:
1. Reads `DEBUG_TOKEN` from the environment
2. If unset → returns `403 debug endpoints disabled` (safe default)
3. If set → checks `X-Debug-Token` header or `?debug_token=` query param
4. Mismatch → returns `403 forbidden`

**To enable debug access:**
```yaml
# docker-compose.yml environment section for hub-api
- DEBUG_TOKEN=your-secret-token
```

**Calling a debug endpoint:**
```bash
# Via header
curl -H "X-Debug-Token: your-secret-token" http://localhost:8088/debug/markets

# Via query param
curl "http://localhost:8088/debug/markets?debug_token=your-secret-token"
```

**Example debug response — `/debug/markets`:**
```json
[
  {
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "yes_price": 0.63,
    "no_price": 0.37,
    "volume": 12480000.0
  },
  {
    "market_id": "0x3a4b2c1d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b",
    "yes_price": 1.0,
    "no_price": 0.0,
    "volume": 485320000.0
  }
]
```

**When `DEBUG_TOKEN` is not set:**
```bash
curl http://localhost:8088/debug/markets
# HTTP/1.1 403 Forbidden
# debug endpoints disabled
```

**Files:** `middleware/auth.go`, `main.go`

---

## 7. `GET /v1/markets/{id}/candles` — OHLCV Endpoint

**Problem:** No way for charting libraries (TradingView, lightweight-charts) to fetch historical candlestick data.

**What changed:** New `GetCandles` handler returning data in **TradingView UDF format**.

**ClickHouse query inside `GetCandles`:**
```go
SELECT timestamp, open, high, low, close, volume
FROM price_history_1m              -- or price_history_1h for resolution=60
WHERE market_id IN (?, ?) AND timestamp >= ? AND timestamp <= ?
ORDER BY timestamp ASC
LIMIT ?
```

**Query params:**
- `resolution`: `1` (1-minute bars) or `60`/`1h` (1-hour bars) — default `1`
- `from`: start Unix timestamp in seconds (default: now-24h)
- `to`: end Unix timestamp in seconds (default: now)
- `limit`: max candles, default 1000, cap 5000

**Example — fetch last 24h of 1-minute bars:**
```bash
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/candles?resolution=1&from=1749513600&to=1749600000"
```

**Response:**
```json
{
  "s": "ok",
  "t": [1749513600, 1749513660, 1749513720, 1749513780],
  "o": [0.610, 0.615, 0.612, 0.618],
  "h": [0.618, 0.620, 0.618, 0.622],
  "l": [0.608, 0.612, 0.609, 0.615],
  "c": [0.615, 0.612, 0.616, 0.620],
  "v": [14820.50, 9340.00, 11200.75, 8900.25],
  "resolution": "1"
}
```

**No-data response (TradingView UDF convention):**
```json
{ "s": "no_data" }
```

**Error response:**
```json
{ "s": "error", "errmsg": "clickhouse: context deadline exceeded" }
```

**TradingView integration snippet:**
```javascript
// In your TradingView datafeed getBars() callback
const resp = await fetch(
  `http://localhost:8088/v1/markets/${marketId}/candles?resolution=${resolution}&from=${from}&to=${to}`
);
const data = await resp.json();
if (data.s === "ok") {
  const bars = data.t.map((t, i) => ({
    time: t * 1000,
    open: data.o[i],
    high: data.h[i],
    low: data.l[i],
    close: data.c[i],
    volume: data.v[i],
  }));
  onHistoryCallback(bars, { noData: false });
}
```

**Files:** `handlers/rest.go`, `main.go`

---

## Redis Client Extension

To support rate limiting, two methods were added to the Redis client interface and the `*Client` struct in `libs/redis-client/redis.go`:

```go
Incr(ctx context.Context, key string) *redis.IntCmd
Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
```

**Rate limiter usage pattern:**
```go
// Increment counter for this IP
count, _ := redis.Incr(ctx, "ratelimit:"+clientIP).Result()
if count == 1 {
    // First hit — set 60-second expiry
    redis.Expire(ctx, "ratelimit:"+clientIP, 60*time.Second)
}
if count > 60 {
    // Reject
}
```

**File:** `libs/redis-client/redis.go`

---

## Summary Table

| # | Improvement | File(s) Changed |
|---|-------------|-----------------|
| 1 | Cursor-based pagination on `GET /v1/markets` | `handlers/rest.go` |
| 2 | Redis cache on 5 hot endpoints | `handlers/rest.go` |
| 3 | Rate limiting — 60 RPM per IP | `middleware/ratelimit.go`, `libs/redis-client/redis.go` |
| 4 | `GET /v1/markets/{id}` inline 24h stats | `handlers/rest.go` |
| 5 | WebSocket per-market subscription filtering | `ws/hub.go`, `ws/client.go`, `ws/broadcaster.go` |
| 6 | Debug endpoint auth via `DEBUG_TOKEN` | `middleware/auth.go`, `main.go` |
| 7 | `GET /v1/markets/{id}/candles` TradingView OHLCV | `handlers/rest.go`, `main.go` |
