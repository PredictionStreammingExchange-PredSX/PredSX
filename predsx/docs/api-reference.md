# PredSX API Reference

Base URL: `http://localhost:8088`

All `/v1/*` routes are rate-limited at **60 requests/min per IP**.
All `/debug/*` routes require the `X-Debug-Token` header (see [Auth](#authentication)).

---

## Public Routes

### `GET /health`
Returns service status.

```bash
curl http://localhost:8088/health
```

**Response**
```json
{
  "status": "ok",
  "service": "predsx-api",
  "project": "PredSX Trading Platform",
  "instance": "Production-VPS",
  "timestamp": 1749600000
}
```

### `GET /metrics`
Prometheus metrics scrape endpoint. Returns plain-text Prometheus exposition format.

```bash
curl http://localhost:8088/metrics
```

### `POST /graphql`
Placeholder stub — always returns `{"data": {"message": "GraphQL API placeholder"}}`.
Not a real GraphQL implementation yet.

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

```bash
# First page
curl "http://localhost:8088/v1/markets?limit=2&status=ACTIVE"

# Next page using cursor
curl "http://localhost:8088/v1/markets?limit=2&cursor=Mg=="
```

**Response**
```json
{
  "data": [
    {
      "market_id": "0x3a4b2c1d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b",
      "exchange": "polymarket",
      "slug": "will-trump-win-2024-election",
      "title": "Will Donald Trump win the 2024 US Presidential Election?",
      "question": "Will Donald Trump win the 2024 US Presidential Election?",
      "status": "RESOLVED",
      "event_id": "us-presidential-election-2024",
      "outcomes": ["Yes", "No"],
      "condition_id": "0xabc123def456abc123def456abc123def456abc1",
      "start_time": "2024-01-01T00:00:00Z",
      "end_time": "2024-11-06T00:00:00Z",
      "yes_price": 1.0,
      "no_price": 0.0,
      "price": 1.0,
      "mid_price": 1.0,
      "best_bid": 0.99,
      "best_ask": 1.0,
      "spread": 0.01,
      "volume": 485320000.0,
      "volume_24h": 485320000.0,
      "tokens": {
        "yes": "0x1234abcd...",
        "no": "0x5678efgh..."
      }
    },
    {
      "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
      "exchange": "polymarket",
      "slug": "btc-above-100k-dec-2024",
      "title": "Will Bitcoin exceed $100,000 by end of December 2024?",
      "question": "Will Bitcoin exceed $100,000 by end of December 2024?",
      "status": "ACTIVE",
      "event_id": "bitcoin-price-milestones",
      "outcomes": ["Yes", "No"],
      "condition_id": "0xdef789abc012def789abc012def789abc012def7",
      "start_time": "2024-06-01T00:00:00Z",
      "end_time": "2024-12-31T23:59:59Z",
      "yes_price": 0.63,
      "no_price": 0.37,
      "price": 0.63,
      "mid_price": 0.635,
      "best_bid": 0.63,
      "best_ask": 0.64,
      "spread": 0.01,
      "volume": 12480000.0,
      "volume_24h": 12480000.0,
      "tokens": {
        "yes": "0xabcd1234...",
        "no": "0xefgh5678..."
      }
    }
  ],
  "next_cursor": "Mg==",
  "has_more": true
}
```

> **Breaking change from original:** Response is now an object with `data`, `next_cursor`, `has_more` — not a plain array.

---

### `GET /v1/markets/{id}`
Single market detail, auto-enriched with inline 24h stats (no second request needed).

```bash
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b"
```

**Response**
```json
{
  "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
  "exchange": "polymarket",
  "slug": "btc-above-100k-dec-2024",
  "title": "Will Bitcoin exceed $100,000 by end of December 2024?",
  "question": "Will Bitcoin exceed $100,000 by end of December 2024?",
  "status": "ACTIVE",
  "event_id": "bitcoin-price-milestones",
  "outcomes": ["Yes", "No"],
  "condition_id": "0xdef789abc012def789abc012def789abc012def7",
  "start_time": "2024-06-01T00:00:00Z",
  "end_time": "2024-12-31T23:59:59Z",
  "yes_price": 0.63,
  "no_price": 0.37,
  "price": 0.63,
  "mid_price": 0.635,
  "best_bid": 0.63,
  "best_ask": 0.64,
  "spread": 0.01,
  "volume": 12480000.0,
  "volume_24h": 12480000.0,
  "tokens": {
    "yes": "0xabcd1234...",
    "no": "0xefgh5678..."
  },
  "signals": [],
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

```bash
curl "http://localhost:8088/v1/markets/top?by=volume&period=24h&limit=3"
```

**Response**
```json
[
  {
    "market_id": "0x3a4b2c1d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b",
    "title": "Will Donald Trump win the 2024 US Presidential Election?",
    "status": "RESOLVED",
    "yes_price": 1.0,
    "no_price": 0.0,
    "volume_24h": 485320000.0,
    "volume_period": 485320000.0,
    "trades_period": 198400
  },
  {
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "title": "Will Bitcoin exceed $100,000 by end of December 2024?",
    "status": "ACTIVE",
    "yes_price": 0.63,
    "no_price": 0.37,
    "volume_24h": 12480000.0,
    "volume_period": 12480000.0,
    "trades_period": 4820
  }
]
```

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

```bash
curl "http://localhost:8088/v1/markets/search?q=bitcoin&status=ACTIVE&limit=2"
```

**Response**
```json
[
  {
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "title": "Will Bitcoin exceed $100,000 by end of December 2024?",
    "status": "ACTIVE",
    "yes_price": 0.63,
    "no_price": 0.37,
    "volume_24h": 12480000.0
  },
  {
    "market_id": "0x1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c",
    "title": "Will Bitcoin ETF get SEC approval before June 2024?",
    "status": "RESOLVED",
    "yes_price": 1.0,
    "no_price": 0.0,
    "volume_24h": 8920000.0
  }
]
```

---

### `GET /v1/markets/{id}/stats`
24h OHLCV stats for a market. Cached for **30 seconds**.

```bash
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/stats"
```

**Response**
```json
{
  "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
  "volume_24h": 12480000.0,
  "trades_24h": 4820,
  "open_price": 0.52,
  "close_price": 0.63,
  "low_24h": 0.48,
  "high_24h": 0.65,
  "price_change_pct": 21.15,
  "timestamp": 1749600000000
}
```

---

### `GET /v1/markets/{id}/summary`
Market summary (current price, orderbook snapshot, recent signals). Cached for **10 seconds**.

```bash
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/summary"
```

**Response**
```json
{
  "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
  "condition_id": "0xdef789abc012def789abc012def789abc012def7",
  "title": "Will Bitcoin exceed $100,000 by end of December 2024?",
  "price": 0.63,
  "best_bid": 0.63,
  "best_ask": 0.64,
  "spread": 0.01,
  "status": "ACTIVE",
  "signals": [
    {
      "type": "predsx.signal.momentum",
      "value": 0.72,
      "severity": "high",
      "timestamp": 1749598800000
    }
  ],
  "timestamp": 1749600000000
}
```

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

```bash
# Last 24h of 1-minute bars
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/candles?resolution=1&from=1749513600&to=1749600000"

# 1-hour bars
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/candles?resolution=60"
```

**Response**
```json
{
  "s": "ok",
  "t": [1749513600, 1749513660, 1749513720],
  "o": [0.610, 0.615, 0.612],
  "h": [0.618, 0.620, 0.618],
  "l": [0.608, 0.612, 0.609],
  "c": [0.615, 0.612, 0.616],
  "v": [14820.50, 9340.00, 11200.75],
  "resolution": "1"
}
```

Returns `{"s": "error", "errmsg": "..."}` on query failure.

**Data source:** `price_history_1m` (resolution=1) or `price_history_1h` (resolution=60) in ClickHouse.

---

### `GET /v1/markets/{id}/orderbook`
Live orderbook snapshot, served verbatim from Redis (`predsx:orderbook:{id}`).
Returns `404 orderbook not found` if no snapshot is cached for this market.

```bash
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/orderbook"
```

**Response**
```json
{
  "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
  "token": "0xabcd1234...",
  "best_bid": 0.63,
  "best_ask": 0.64,
  "bids": [
    [0.63, 1500.00],
    [0.62, 3200.00],
    [0.61, 5800.00],
    [0.60, 9100.00],
    [0.59, 12400.00]
  ],
  "asks": [
    [0.64, 1200.00],
    [0.65, 2800.00],
    [0.66, 4500.00],
    [0.67, 7200.00],
    [0.68, 11000.00]
  ],
  "timestamp": 1749599998000
}
```

---

### `GET /v1/markets/{id}/price`
Current live price, served verbatim from Redis (`predsx:price:{id}`).
Returns `404 price not found` if no snapshot is cached for this market.

```bash
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/price"
```

**Response**
```json
{
  "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
  "price": 0.63,
  "mid_price": 0.635,
  "best_bid": 0.63,
  "best_ask": 0.64,
  "spread": 0.01,
  "last_trade_price": 0.632,
  "volume_24h": 12480000.0,
  "timestamp": 1749599998000
}
```

---

### `GET /v1/markets/{id}/price-history`
Historical price/volume time series from ClickHouse candle tables.

**Query params**

| Param | Default | Description |
|-------|---------|-------------|
| `resolution` | `1m` | Bucket size: `1s` (reads `market_metrics`), `1m`, `5m`, `1h` |
| `from` / `to` | — | Optional time range (Unix seconds, Unix ms, or RFC3339) |
| `limit` | `500` | Max rows (cap: 20000), most-recent first |

```bash
# Last 500 1-minute buckets
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/price-history?resolution=1m&limit=3"

# 1-hour buckets between two timestamps
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/price-history?resolution=1h&from=1749513600&to=1749600000"
```

**Response**
```json
[
  {
    "timestamp": "2026-06-11T12:00:00Z",
    "trade_count": 48,
    "volume": 14820.50,
    "avg_price": 0.631
  },
  {
    "timestamp": "2026-06-11T11:59:00Z",
    "trade_count": 31,
    "volume": 9340.00,
    "avg_price": 0.628
  },
  {
    "timestamp": "2026-06-11T11:58:00Z",
    "trade_count": 55,
    "volume": 11200.75,
    "avg_price": 0.625
  }
]
```

---

### `GET /v1/markets/{id}/trades`
Recent trade history for a market.

**Query params**

| Param | Default | Description |
|-------|---------|-------------|
| `limit` | `100` | Max results (cap: 5000) |
| `from` / `to` | — | Optional time range (Unix seconds, Unix ms, or RFC3339) |
| `wallet` | — | If set, proxies to the Polymarket Data API `/trades` filtered by `user`/`market` |
| `source` | `local` (or `data-api` if `wallet` is set) | Forces local ClickHouse query vs. Data API proxy |

```bash
# Last 3 trades from ClickHouse
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/trades?limit=3"

# Trades for a specific wallet (proxies to Polymarket Data API)
curl "http://localhost:8088/v1/markets/0x9f8e.../trades?wallet=0xUserWalletAddress"
```

**Response (local source)**
```json
[
  {
    "trade_id": "0xf1e2d3c4b5a6978869504132231415161718191a",
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "price": 0.632,
    "size": 500.00,
    "side": "BUY",
    "token": "0xabcd1234...",
    "timestamp": 1749599998000
  },
  {
    "trade_id": "0xa1b2c3d4e5f6071829304152637485960718293a",
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "price": 0.628,
    "size": 1200.00,
    "side": "SELL",
    "token": "0xabcd1234...",
    "timestamp": 1749599990000
  }
]
```

---

### `GET /v1/markets/{id}/positions`
Open positions for this specific market. Proxies to the Polymarket Data API
`/market-positions` endpoint with `market={id}` (and `user={wallet}` if the
`wallet` query param is supplied).

```bash
curl "http://localhost:8088/v1/markets/0x9f8e.../positions?wallet=0xYourWalletAddress"
```

---

### `GET /v1/markets/{id}/related`
Other markets belonging to the same event.

**Query params**

| Param | Default | Description |
|-------|---------|-------------|
| `limit` | `20` | Max results (cap: 100) |

```bash
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/related?limit=2"
```

**Response**
```json
[
  {
    "market_id": "0xaabb1122ccdd3344eeff5566aabb1122ccdd3344",
    "title": "Will Bitcoin exceed $150,000 by end of 2025?",
    "status": "ACTIVE",
    "yes_price": 0.31,
    "no_price": 0.69
  }
]
```

Returns `[]` if the market has no `event_id`.

---

### `GET /v1/markets/{id}/signals`
Latest trading signals generated for this market.

**Query params**

| Param | Default | Description |
|-------|---------|-------------|
| `type` | — | Filter to a specific signal type (e.g. `momentum`, `volume_spike`) |

```bash
curl "http://localhost:8088/v1/markets/0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b/signals"

# Filter by type
curl "http://localhost:8088/v1/markets/0x9f8e.../signals?type=momentum"
```

**Response**
```json
[
  {
    "type": "predsx.signal.momentum",
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "value": 0.72,
    "severity": "high",
    "timestamp": 1749598800000
  },
  {
    "type": "predsx.signal.volume_spike",
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "value": 3.41,
    "severity": "medium",
    "timestamp": 1749597600000
  }
]
```

---

## Prices & Events

### `GET /v1/prices`
Batch live prices for up to 100 markets in a single round trip.

**Query params**

| Param | Default | Description |
|-------|---------|-------------|
| `ids` | — | Comma-separated market IDs (max 100; extras are dropped) |

```bash
curl "http://localhost:8088/v1/prices?ids=0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b,0x3a4b2c1d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b,unknown-id"
```

**Response**
```json
{
  "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b": {
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "price": 0.63,
    "mid_price": 0.635,
    "best_bid": 0.63,
    "best_ask": 0.64,
    "spread": 0.01,
    "volume_24h": 12480000.0,
    "timestamp": 1749599998000
  },
  "0x3a4b2c1d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b": {
    "market_id": "0x3a4b2c1d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b",
    "price": 1.0,
    "mid_price": 1.0,
    "best_bid": 0.99,
    "best_ask": 1.0,
    "spread": 0.01,
    "volume_24h": 485320000.0,
    "timestamp": 1749599998000
  },
  "unknown-id": null
}
```

A value of `null` means no live price is currently cached for that ID.

---

### `GET /v1/events`
List prediction-market events. Pure passthrough proxy to the Polymarket Gamma
API `/events` endpoint. Only `exchange=polymarket` is supported.

```bash
curl "http://localhost:8088/v1/events?exchange=polymarket&limit=5"
```

---

## Positions

### `GET /v1/positions`
All open positions for a wallet. Proxies to the Polymarket Data API `/positions`.

```bash
curl "http://localhost:8088/v1/positions?wallet=0xYourWalletAddress"
```

Returns `400 wallet is required` if omitted.

### `GET /v1/positions/closed`
Closed/settled positions for a wallet. Same `wallet` (required) param.

```bash
curl "http://localhost:8088/v1/positions/closed?wallet=0xYourWalletAddress"
```

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

```bash
# List all tracked markets (requires debug token)
curl -H "X-Debug-Token: your-secret-token" http://localhost:8088/debug/markets
```

**`GET /debug/markets` response**
```json
[
  {
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "yes_price": 0.63,
    "no_price": 0.37,
    "volume": 12480000.0
  }
]
```

**`GET /debug/markets/{id}` response**
```json
{
  "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
  "yes_price": 0.63,
  "no_price": 0.37,
  "volume": 3200000,
  "liquidity": 580000
}
```

**`GET /debug/trades` response** — last 50 trades from ClickHouse
```json
[
  {
    "trade_id": "0xf1e2d3c4b5a6978869504132231415161718191a",
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "price": 0.632,
    "size": 500.00,
    "side": "BUY",
    "timestamp": 1749599998000
  }
]
```

**`GET /debug/orderbook/{id}` response**
```json
{
  "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
  "bids": [[0.63, 1500], [0.62, 3200]],
  "asks": [[0.64, 1200], [0.65, 2800]]
}
```

---

## WebSocket

### `GET /stream`
Upgrade to WebSocket. Receive real-time events for all markets by default.

```javascript
const ws = new WebSocket("ws://localhost:8088/stream");

// Subscribe to specific markets
ws.send(JSON.stringify({
  action: "subscribe",
  markets: [
    "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "0x3a4b2c1d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b"
  ]
}));

// Unsubscribe (receive all markets again)
ws.send(JSON.stringify({ action: "unsubscribe", markets: [] }));
```

**Outbound — trade event**
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

**Outbound — orderbook event**
```json
{
  "type": "orderbook",
  "data": {
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "best_bid": 0.63,
    "best_ask": 0.64,
    "bids": [[0.63, 1500.0], [0.62, 3200.0]],
    "asks": [[0.64, 1200.0], [0.65, 2800.0]]
  },
  "timestamp": "2026-06-11T12:00:00Z"
}
```

**Outbound — price event**
```json
{
  "type": "price",
  "data": {
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "price": 0.632,
    "mid_price": 0.635,
    "best_bid": 0.63,
    "best_ask": 0.64,
    "spread": 0.01,
    "volume_24h": 12480000.0
  },
  "timestamp": "2026-06-11T12:00:00Z"
}
```

**Outbound — signal event**
```json
{
  "type": "signal",
  "data": {
    "type": "predsx.signal.momentum",
    "market_id": "0x9f8e7d6c5b4a3928170605040302010f9e8d7c6b",
    "value": 0.72,
    "severity": "high"
  },
  "timestamp": "2026-06-11T12:00:00Z"
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
Set the `DEBUG_TOKEN` environment variable to enable debug endpoints.

```yaml
# docker-compose.yml environment section for hub-api
- DEBUG_TOKEN=your-secret-token
```

Pass the token via:
- Header: `X-Debug-Token: your-secret-token`
- Query: `?debug_token=your-secret-token`

```bash
curl -H "X-Debug-Token: your-secret-token" http://localhost:8088/debug/signals
```

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

```bash
# First call — cache miss
curl -I "http://localhost:8088/v1/markets/top"
# X-Cache: (absent)

# Second call within 15s — cache hit
curl -I "http://localhost:8088/v1/markets/top"
# X-Cache: HIT
```

---

## Error Reference

| HTTP Status | Body | Cause |
|------------|------|-------|
| `400 Bad Request` | `"unsupported exchange"` | Exchange other than `polymarket` passed |
| `400 Bad Request` | `"wallet is required"` | `/v1/positions` called without `wallet` param |
| `404 Not Found` | `"market not found"` | Market ID not in Redis `predsx:markets` set |
| `404 Not Found` | `"orderbook not found"` | No orderbook snapshot cached in Redis |
| `404 Not Found` | `"price not found"` | No price snapshot cached in Redis |
| `429 Too Many Requests` | `{"error":"rate limit exceeded","retry_after":"60s"}` | Over 60 req/min from same IP |
| `403 Forbidden` | `"debug endpoints disabled"` | `DEBUG_TOKEN` not set in env |
| `403 Forbidden` | `"forbidden"` | Wrong debug token supplied |
| `502 Bad Gateway` | `"upstream request failed"` | Polymarket Gamma / Data API unreachable |
