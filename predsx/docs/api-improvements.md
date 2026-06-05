# API Improvements

This document covers the 7 improvements implemented on top of the original API boilerplate.

---

## 1. Cursor-Based Pagination

**Endpoint:** `GET /v1/markets`

**Problem:** The original handler used raw `offset` integers in query params. Offset pagination breaks when data is inserted mid-page — callers get skipped or duplicate rows.

**What changed:**
- Response format changed from a plain array to a page object:
  ```json
  { "data": [...], "next_cursor": "MTUw", "has_more": true }
  ```
- `next_cursor` is a base64-encoded opaque token (encodes the integer offset internally).
- Callers pass `?cursor=<token>` to fetch the next page. The raw `?offset=` param still works as a fallback.
- Helpers added: `encodeCursor(offset int) string` and `decodeCursor(cursor string) int`.

**Files:** `handlers/rest.go`

> **Frontend note:** The response is no longer a plain array. Use `response.data` to access the market list.

---

## 2. Redis Cache Layer

**Problem:** Hot endpoints like `/markets/top` and `/markets/search` hit ClickHouse on every request. ClickHouse is fast but not designed for per-request low-latency reads.

**What changed:** A cache layer was added using two helpers:
```go
func (h *APIHandler) cacheGetJSON(ctx, key, dest) bool
func (h *APIHandler) cacheSetJSON(ctx, key, value, ttl)
```

Cached endpoints and their TTLs:

| Endpoint | Cache Key Pattern | TTL |
|----------|-------------------|-----|
| `GET /v1/markets` | `cache:markets:{exchange}:{status}:{limit}:{offset}` | 30s |
| `GET /v1/markets/top` | `cache:markets:top:{by}:{period}:{limit}` | 15s |
| `GET /v1/markets/search` | `cache:search:{q}:{exchange}:{status}:{limit}` | 30s |
| `GET /v1/markets/{id}/stats` | `cache:stats:{id}` | 30s |
| `GET /v1/markets/{id}/summary` | `cache:summary:{id}` | 10s |

Cache hits set `X-Cache: HIT` response header. Cache misses fetch from ClickHouse and populate the cache.

**Files:** `handlers/rest.go`

---

## 3. Rate Limiting

**Problem:** No protection against excessive requests — a single client could flood the API.

**What changed:** A new `RateLimit` middleware was created using the Redis INCR+EXPIRE pattern (sliding window, resets every 60 seconds).

- **Limit:** 60 requests/minute per client IP
- **IP detection:** Checks `X-Forwarded-For`, then `X-Real-IP`, then `RemoteAddr` (proxy-aware)
- **Fail open:** If Redis is unavailable, the request is allowed through
- **Headers:** Every response includes `X-RateLimit-Limit` and `X-RateLimit-Remaining`
- **On limit exceeded:** Returns `HTTP 429` with JSON body

Applied to all `/v1/*` routes via gorilla/mux subrouter middleware.

**Files:** `middleware/ratelimit.go`, `libs/redis-client/redis.go` (added `Incr`, `Expire` to interface)

---

## 4. `GET /v1/markets/{id}` Auto-Enrichment

**Problem:** To show a market with its 24h stats, the frontend had to make two requests: `GET /v1/markets/{id}` and `GET /v1/markets/{id}/stats`.

**What changed:**
- A shared `fetchMarketStats(ctx, marketID, conditionID)` helper was extracted from `GetMarketStats`.
- `GetMarket` now calls `fetchMarketStats` and embeds the result as `stats_24h` in the response.
- `GetMarketStats` was refactored to use the same helper (no duplication).

`fetchMarketStats` queries `price_history_1m` for the last 24 hours and returns: `volume_24h`, `trades_24h`, `open_price`, `close_price`, `low_24h`, `high_24h`, `price_change_pct`. Returns `nil` on error (stats are omitted rather than breaking the market response).

**Files:** `handlers/rest.go`

---

## 5. WebSocket Per-Market Subscription Filtering

**Problem:** Every connected WebSocket client received every event for every market — wasteful for clients that only care about one or two markets.

**What changed:**

**Hub (`ws/hub.go`):**
- `broadcast` channel changed from `chan []byte` to `chan broadcastMsg{data []byte, marketID string}`
- New `subscribe` channel (`chan subscribeCmd`) handles client filter updates
- Broadcast loop skips clients whose subscription set does not include the event's `marketID`
- Clients with an empty subscription set receive all events (default behaviour)

**Client (`ws/client.go`):**
- Added `subscriptions map[string]bool` field to `Client`
- `readPump` now parses incoming JSON and dispatches subscribe/unsubscribe commands:
  ```json
  { "action": "subscribe",   "markets": ["0xabc...", "0xdef..."] }
  { "action": "unsubscribe", "markets": [] }
  ```

**Broadcaster (`ws/broadcaster.go`):**
- All 4 Kafka consumers (trades, orderbook, prices, signals) call `hub.BroadcastToMarket(data, evt.MarketID)` instead of `hub.Broadcast(data)`

**Files:** `ws/hub.go`, `ws/client.go`, `ws/broadcaster.go`

---

## 6. Debug Endpoint Authentication

**Problem:** `/debug/*` endpoints exposed raw internal data with no access control.

**What changed:** A `DebugAuth` middleware was added that:
1. Reads `DEBUG_TOKEN` from the environment
2. If unset → returns `403 debug endpoints disabled` (safe default — no accidental exposure)
3. If set → checks `X-Debug-Token` header or `?debug_token=` query param against the env value
4. Mismatch → returns `403 forbidden`

All `/debug/*` routes are registered under a separate gorilla/mux subrouter with this middleware applied.

**To enable debug access:**
```yaml
# docker-compose.yml environment section for hub-api
- DEBUG_TOKEN=your-secret-token
```

Then call with:
```
curl -H "X-Debug-Token: your-secret-token" http://localhost:8088/debug/markets
```

**Files:** `middleware/auth.go`, `main.go`

---

## 7. `GET /v1/markets/{id}/candles` — OHLCV Endpoint

**Problem:** No way for charting libraries (TradingView, lightweight-charts) to fetch historical candlestick data.

**What changed:** New `GetCandles` handler added, returning data in **TradingView UDF format**.

**Query params:**
- `resolution`: `1` (1-minute bars) or `60`/`1h` (1-hour bars)
- `from`: start Unix timestamp in seconds (default: now-24h)
- `to`: end Unix timestamp in seconds (default: now)
- `limit`: max candles, default 1000, cap 5000

**Response format:**
```json
{
  "s": "ok",
  "t": [1704067200, 1704067260],
  "o": [0.52, 0.53],
  "h": [0.55, 0.56],
  "l": [0.51, 0.52],
  "c": [0.53, 0.55],
  "v": [1200.0, 980.5],
  "resolution": "1"
}
```

Queries `price_history_1m` or `price_history_1h` in ClickHouse. Searches by both `market_id` and `condition_id` so candles are found regardless of which ID the storage service used.

On error returns `{"s": "error", "errmsg": "..."}` (TradingView UDF error convention).

**Files:** `handlers/rest.go`, `main.go`

---

## Redis Client Extension

To support rate limiting, two methods were added to the Redis client interface and the `*Client` struct:

```go
Incr(ctx context.Context, key string) *redis.IntCmd
Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
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
