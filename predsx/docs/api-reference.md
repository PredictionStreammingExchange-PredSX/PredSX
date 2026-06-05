# PredSX API Reference

Base URL: `http://localhost:8088`

All `/v1/*` routes are rate-limited at **60 requests/min per IP**.
All `/debug/*` routes require the `X-Debug-Token` header (see [Auth](#authentication)).

---

## Public Routes

### `GET /health`
Returns service status.

**Response**
```json
{
  "status": "ok",
  "service": "predsx-api",
  "timestamp": 1717600000
}
```

### `GET /metrics`
Prometheus metrics scrape endpoint.

---

## Markets

### `GET /v1/markets`
List markets, activity-ranked (most recently active first), with cursor-based pagination.

**Query params**

| Param | Default | Description |
|-------|---------|-------------|
| `limit` | `50` | Max results (cap: 200) |
| `cursor` | — | Opaque pagination token from previous `next_cursor` |
| `exchange` | `polymarket` | Exchange filter |
| `status` | — | Status filter (`ACTIVE`, `RESOLVED`, etc.) |
| `source` | `local` | `gamma` proxies directly to Polymarket Gamma API |

**Response**
```json
{
  "data": [ { "market_id": "...", "title": "...", ... } ],
  "next_cursor": "MTUw",
  "has_more": true
}
```

> **Breaking change from original:** Response is now an object with `data`, `next_cursor`, `has_more` — not a plain array.

---

### `GET /v1/markets/{id}`
Single market detail, auto-enriched with inline 24h stats (no second request needed).

**Response**
```json
{
  "market_id": "0x...",
  "title": "Will X happen?",
  "status": "ACTIVE",
  "stats_24h": {
    "volume_24h": 12500.50,
    "trades_24h": 340,
    "open_price": 0.52,
    "close_price": 0.61,
    "low_24h": 0.48,
    "high_24h": 0.65,
    "price_change_pct": 17.3
  }
}
```

Returns `404 market not found` if the market ID is not in Redis (`predsx:markets` set).

---

### `GET /v1/markets/top`
Top markets ranked by volume or trade count. Cached for **15 seconds**.

**Query params**

| Param | Default | Description |
|-------|---------|-------------|
| `by` | `volume` | Rank by: `volume` or `trades` |
| `period` | `24h` | Time window: `1h`, `24h`, `7d` |
| `limit` | `10` | Max results (cap: 50) |

**Response** — array of market objects with `total_volume` and `total_trades`.

---

### `GET /v1/markets/search`
Full-text search on market `title` and `question` fields. Cached for **30 seconds**.

**Query params**

| Param | Default | Description |
|-------|---------|-------------|
| `q` | — | Search term (required, else returns `[]`) |
| `limit` | `20` | Max results (cap: 100) |
| `exchange` | — | Filter by exchange |
| `status` | — | Filter by status |

---

### `GET /v1/markets/{id}/stats`
24h OHLCV stats for a market. Cached for **30 seconds**.

**Response**
```json
{
  "volume_24h": 12500.50,
  "trades_24h": 340,
  "open_price": 0.52,
  "close_price": 0.61,
  "low_24h": 0.48,
  "high_24h": 0.65,
  "price_change_pct": 17.3
}
```

---

### `GET /v1/markets/{id}/summary`
Market summary (price, orderbook snapshot, recent trades). Cached for **10 seconds**.

---

### `GET /v1/markets/{id}/candles`
OHLCV candles in **TradingView UDF format**.

**Query params**

| Param | Default | Description |
|-------|---------|-------------|
| `resolution` | `1` | `1` = 1-minute bars, `60` or `1h` = 1-hour bars |
| `from` | now-24h | Start time as Unix seconds |
| `to` | now | End time as Unix seconds |
| `limit` | `1000` | Max candles (cap: 5000) |

**Response**
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

Returns `{"s": "error", "errmsg": "..."}` on query failure.

**Data source:** `price_history_1m` (resolution=1) or `price_history_1h` (resolution=60) in ClickHouse.

---

### `GET /v1/markets/{id}/orderbook`
Live orderbook snapshot from Redis.

---

### `GET /v1/markets/{id}/price`
Current live price from Redis (`predsx:price:{id}`).

---

### `GET /v1/markets/{id}/price-history`
Historical price series from ClickHouse.

---

### `GET /v1/markets/{id}/trades`
Recent trades for a market.

---

### `GET /v1/markets/{id}/positions`
Open positions for a market.

---

### `GET /v1/markets/{id}/related`
Related markets.

---

### `GET /v1/markets/{id}/signals`
Signals for a specific market.

---

## Prices & Events

### `GET /v1/prices`
Batch live prices. Pass `ids=id1,id2,...` as query param.

### `GET /v1/events`
List events.

---

## Positions

### `GET /v1/positions`
All open positions.

### `GET /v1/positions/closed`
Closed positions.

---

## Debug Endpoints

All debug routes require authentication (see below).

| Route | Description |
|-------|-------------|
| `GET /debug/markets` | Raw market list from Redis/ClickHouse |
| `GET /debug/markets/{id}` | Raw market detail |
| `GET /debug/trades` | Raw trade events |
| `GET /debug/orderbook/{id}` | Raw orderbook data |
| `GET /debug/signals` | All signals |
| `GET /debug/signals/{id}` | Signals by market |

---

## WebSocket

### `GET /stream`
Upgrade to WebSocket. Receive real-time events for all markets by default.

**Incoming message — subscribe to specific markets**
```json
{ "action": "subscribe", "markets": ["0xabc...", "0xdef..."] }
```

**Incoming message — unsubscribe (receive all again)**
```json
{ "action": "unsubscribe", "markets": [] }
```

**Outbound envelope format**
```json
{
  "type": "trade" | "orderbook" | "price" | "signal",
  "data": { ... },
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

## Authentication

### Rate Limiting
All `/v1/*` routes are rate-limited at **60 requests/minute per IP**.

Response headers on every request:
```
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 47
```

When exceeded:
```json
HTTP 429
{ "error": "rate limit exceeded", "retry_after": "60s" }
```

### Debug Token
Set the `DEBUG_TOKEN` environment variable to enable debug endpoints. Pass the token via:
- Header: `X-Debug-Token: <token>`
- Query: `?debug_token=<token>`

If `DEBUG_TOKEN` is not set, all `/debug/*` routes return `403 debug endpoints disabled`.

---

## Cache Headers

Cached endpoints return `X-Cache: HIT` on subsequent requests within the TTL window.

| Endpoint | TTL |
|----------|-----|
| `GET /v1/markets` | 30s |
| `GET /v1/markets/top` | 15s |
| `GET /v1/markets/search` | 30s |
| `GET /v1/markets/{id}/stats` | 30s |
| `GET /v1/markets/{id}/summary` | 10s |
