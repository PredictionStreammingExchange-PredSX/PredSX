# PredSX — Environment Variables Reference

> Complete reference for all environment variables across all 5 hub services.
> All variables are read at startup via `libs/config`. Missing required variables cause startup failure. Optional variables fall back to documented defaults.

---

## Quick Reference — Which Service Uses What

| Variable | Discovery | Ingestion | Processor | Storage | API |
|----------|:---------:|:---------:|:---------:|:-------:|:---:|
| `KAFKA_BROKERS` | ✅ | ✅ | ✅ | ✅ | ✅ |
| `REDIS_ADDR` | ✅ | — | ✅ | ✅ | ✅ |
| `CLICKHOUSE_ADDR` | — | ✅ | ✅ | ✅ | ✅ |
| `CLICKHOUSE_USER` | — | ✅ | ✅ | ✅ | ✅ |
| `CLICKHOUSE_PASSWORD` | — | ✅ | ✅ | ✅ | ✅ |
| `CLICKHOUSE_DB` | — | ✅ | ✅ | ✅ | ✅ |
| `POSTGRES_URL` | — | — | — | ✅ | — |
| `GAMMA_API_URL` | ✅ | — | — | — | — |
| `GAMMA_EVENTS_URL` | ✅ | — | — | — | — |
| `POLL_INTERVAL_SECONDS` | ✅ | — | — | — | — |
| `POLYMARKET_WS_URL` | — | ✅ | — | — | — |
| `WS_CONNECTIONS` | — | ✅ | — | — | — |
| `POLYGON_RPC_URLS` | — | ✅ | — | — | — |
| `API_PORT` | — | — | — | — | ✅ |
| `ALLOWED_ORIGINS` | — | — | — | — | ✅ |
| `TRUSTED_PROXIES` | — | — | — | — | ✅ |
| `DEBUG_TOKEN` | — | — | — | — | ✅ |
| `GAMMA_API_BASE_URL` | — | — | — | — | ✅ |
| `DATA_API_URL` | — | — | — | — | ✅ |
| `CLOB_API_URL` | — | — | — | — | ✅ |

---

## Shared Infrastructure (used by multiple services)

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated Kafka broker addresses. In Docker Compose, set to `kafka:9092` (Docker DNS). |
| `REDIS_ADDR` | `localhost:6379` | Redis connection address. In Docker Compose, set to `redis:6379`. |
| `CLICKHOUSE_ADDR` | `localhost:9000` | ClickHouse native TCP address. In Docker Compose, set to `clickhouse:9000`. |
| `CLICKHOUSE_USER` | `default` | ClickHouse username. The default installation uses `default` with no password. |
| `CLICKHOUSE_PASSWORD` | `` (empty) | ClickHouse password. Leave empty for local/default installs. |
| `CLICKHOUSE_DB` | `default` | ClickHouse database name. All PredSX tables live in `default` — do not change this. |

---

## Kafka Topics (shared across services)

These topic names must be consistent between producer and consumer services. If you override one, override it in every service that reads or writes that topic.

| Variable | Default Topic | Producer | Consumer |
|----------|--------------|----------|----------|
| `MARKET_DISCOVERY_TOPIC` | `predsx.markets.discovered` | Discovery | Storage |
| `TOKEN_EXTRACTOR_TOPIC` | `predsx.tokens.extracted` | Discovery | Ingestion |
| `WS_RAW_TOPIC` | `predsx.ws.raw` | Ingestion | Processor, Storage |
| `TRADES_LIVE_TOPIC` | `predsx.trades.live` | Ingestion, Processor | Storage, API (WS) |
| `ORDERBOOK_UPDATES_TOPIC` | `predsx.orderbook.updates` | Processor | Storage, API (WS) |
| `PRICES_TOPIC` | `predsx.prices` | Processor | API (WS) |
| `SIGNALS_TOPIC` | `predsx.signals.live` | Processor | API (WS) |
| `ONCHAIN_TRADES_TOPIC` | `predsx.trades.onchain` | Ingestion | Processor, Storage |
| `TRADES_BACKFILL_TOPIC` | `predsx.trades.backfill` | Storage (backfill) | Storage (reader) |
| `METRICS_TRADES_TOPIC` | `predsx.metrics.trades` | Ingestion | — |

---

## Hub Discovery

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `KAFKA_BROKERS` | `localhost:9092` | Yes | Kafka broker address |
| `REDIS_ADDR` | `localhost:6379` | Yes | Redis address for token + metadata cache |
| `GAMMA_API_URL` | `https://gamma-api.polymarket.com/markets` | No | Gamma `/markets` endpoint. Override for staging or if Polymarket changes the URL. |
| `GAMMA_EVENTS_URL` | `https://gamma-api.polymarket.com/events` | No | Gamma `/events` endpoint. Used by `discoverEvents` to guarantee `event_id` on all markets. |
| `POLL_INTERVAL_SECONDS` | `60` | No | How often (in seconds) both discovery loops repeat. Lower values increase Gamma API load. |
| `MARKET_DISCOVERY_TOPIC` | `predsx.markets.discovered` | No | Kafka topic for discovered market events |
| `TOKEN_EXTRACTOR_TOPIC` | `predsx.tokens.extracted` | No | Kafka topic for extracted YES/NO token pairs |

---

## Hub Ingestion

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `KAFKA_BROKERS` | `localhost:9092` | Yes | Kafka broker address |
| `CLICKHOUSE_ADDR` | `localhost:9000` | Yes | ClickHouse address |
| `CLICKHOUSE_USER` | `default` | No | ClickHouse user |
| `CLICKHOUSE_PASSWORD` | `` | No | ClickHouse password |
| `CLICKHOUSE_DB` | `default` | No | ClickHouse database |
| `POLYMARKET_WS_URL` | `wss://ws-subscriptions-clob.polymarket.com/ws/market` | No | Polymarket WebSocket endpoint for live trade + orderbook events |
| `WS_CONNECTIONS` | `4` | No | Number of parallel WebSocket connections. Markets are distributed via FNV-1a consistent hashing. Increase if subscribing to >2000 markets. |
| `POLYGON_RPC_URLS` | *(public pool)* | No | Comma-separated Polygon RPC URLs for on-chain trade indexing. Defaults to a built-in pool of free public endpoints. Set this to override with private RPC URLs (e.g. Alchemy, Infura). |
| `TOKEN_EXTRACTOR_TOPIC` | `predsx.tokens.extracted` | No | Consumed to learn which token IDs to subscribe to |
| `WS_RAW_TOPIC` | `predsx.ws.raw` | No | Topic for raw WebSocket payloads |
| `TRADES_LIVE_TOPIC` | `predsx.trades.live` | No | Topic for normalized trade events |
| `ONCHAIN_TRADES_TOPIC` | `predsx.trades.onchain` | No | Topic for on-chain Polygon trade events |
| `METRICS_TRADES_TOPIC` | `predsx.metrics.trades` | No | Topic for trade metrics aggregation |

---

## Hub Processor

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `KAFKA_BROKERS` | `localhost:9092` | Yes | Kafka broker address |
| `REDIS_ADDR` | `localhost:6379` | Yes | Redis address — processor writes live prices, orderbooks, and signals here |
| `CLICKHOUSE_ADDR` | `localhost:9000` | Yes | ClickHouse address |
| `CLICKHOUSE_USER` | `default` | No | ClickHouse user |
| `CLICKHOUSE_PASSWORD` | `` | No | ClickHouse password |
| `CLICKHOUSE_DB` | `default` | No | ClickHouse database |
| `WS_RAW_TOPIC` | `predsx.ws.raw` | No | Raw WebSocket events to process |
| `TRADES_LIVE_TOPIC` | `predsx.trades.live` | No | Output topic for normalized trades |
| `ORDERBOOK_TOPIC` | `predsx.orderbook.updates` | No | Output topic for orderbook updates |
| `PRICES_TOPIC` | `predsx.prices` | No | Output topic for calculated prices |
| `SIGNALS_TOPIC` | `predsx.signals.live` | No | Output topic for trading signals |
| `ONCHAIN_TRADES_TOPIC` | `predsx.trades.onchain` | No | On-chain trade events to process |
| `BACKTEST_PORT` | `8082` | No | Internal port for the backtester HTTP server. Must not collide with other hub `METRICS_PORT` values. |

---

## Hub Storage

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `KAFKA_BROKERS` | `localhost:9092` | Yes | Kafka broker address |
| `CLICKHOUSE_ADDR` | `localhost:9000` | Yes | ClickHouse address — storage is the primary writer |
| `CLICKHOUSE_USER` | `default` | No | ClickHouse user |
| `CLICKHOUSE_PASSWORD` | `` | No | ClickHouse password |
| `CLICKHOUSE_DB` | `default` | No | ClickHouse database |
| `POSTGRES_URL` | `postgres://postgres:postgres@localhost:5432/predsx` | Yes | PostgreSQL connection string. Used for resumable backfill offsets. In Docker Compose, set to `postgres://postgres:postgres@postgres:5432/predsx`. |
| `REDIS_ADDR` | `localhost:6379` | Yes | Redis address |
| `WS_RAW_TOPIC` | `predsx.ws.raw` | No | Raw WebSocket events to persist |
| `TRADES_BACKFILL_TOPIC` | `predsx.trades.backfill` | No | Historical backfill topic |
| `TRADES_LIVE_TOPIC` | `predsx.trades.live` | No | Live trades to persist |
| `ORDERBOOK_UPDATES_TOPIC` | `predsx.orderbook.updates` | No | Orderbook snapshots to persist |
| `MARKET_DISCOVERY_TOPIC` | `predsx.markets.discovered` | No | Market metadata to persist |
| `ONCHAIN_TRADES_TOPIC` | `predsx.trades.onchain` | No | On-chain trades to persist |
| `MARKET_DISCOVERY_GROUP` | `storage-norm-dis-v10` | No | Kafka consumer group ID for market discovery consumer. Changing this resets the consumer offset — the storage hub will reprocess all discovery events from the beginning. |

---

## Hub API

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `KAFKA_BROKERS` | `localhost:9092` | Yes | Kafka broker address (used by WebSocket broadcaster) |
| `REDIS_ADDR` | `localhost:6379` | Yes | Redis address — API reads all live state from here |
| `CLICKHOUSE_ADDR` | `localhost:9000` | Yes | ClickHouse address — API queries historical data |
| `CLICKHOUSE_USER` | `default` | No | ClickHouse user |
| `CLICKHOUSE_PASSWORD` | `` | No | ClickHouse password |
| `CLICKHOUSE_DB` | `default` | No | ClickHouse database |
| `API_PORT` | `8088` | No | HTTP listen port |
| `ALLOWED_ORIGINS` | `http://localhost:3000` | **Yes (production)** | Comma-separated allowed CORS origins. Set to your frontend domain(s). Use `*` only for fully public APIs. |
| `TRUSTED_PROXIES` | `` (disabled) | Only behind a proxy | CIDR or IP of your reverse proxy/load balancer. When set, `X-Forwarded-For` is trusted for rate limiting. Omit if the API is directly internet-facing. |
| `DEBUG_TOKEN` | `` (disabled) | Recommended | Secret token that gates `/debug/*` and `/metrics`. Without it, those routes return `403 debug endpoints disabled`. |
| `GAMMA_API_BASE_URL` | `https://gamma-api.polymarket.com` | No | Base URL for Gamma API proxy calls (`/v1/events`) |
| `DATA_API_URL` | `https://data-api.polymarket.com` | No | Polymarket Data API for positions and wallet trades |
| `CLOB_API_URL` | `https://clob.polymarket.com` | No | Polymarket CLOB API (reserved for future order submission) |
| `TRADES_LIVE_TOPIC` | `predsx.trades.live` | No | Kafka topic the WebSocket broadcaster subscribes to for live trade events |
| `ORDERBOOK_UPDATES_TOPIC` | `predsx.orderbook.updates` | No | Kafka topic for live orderbook events |
| `PRICES_TOPIC` | `predsx.prices` | No | Kafka topic for live price events |
| `SIGNALS_TOPIC` | `predsx.signals.live` | No | Kafka topic for live signal events |

---

## Minimal Production `.env`

The following is the minimum `.env` file needed for a production VPS deploy. All other variables use safe defaults.

```env
# Infrastructure (Docker internal DNS)
KAFKA_BROKERS=kafka:9092
REDIS_ADDR=redis:6379
CLICKHOUSE_ADDR=clickhouse:9000
CLICKHOUSE_USER=default
CLICKHOUSE_PASSWORD=
CLICKHOUSE_DB=default
POSTGRES_URL=postgres://postgres:postgres@postgres:5432/predsx

# API Security (required for production)
ALLOWED_ORIGINS=https://app.predsx.com
DEBUG_TOKEN=replace-with-a-secure-random-string

# Optional — only if behind Nginx/Caddy reverse proxy
# TRUSTED_PROXIES=10.0.0.1/24

# Optional — only if Gamma changes their URLs
# GAMMA_API_URL=https://gamma-api.polymarket.com/markets
# GAMMA_EVENTS_URL=https://gamma-api.polymarket.com/events
```

> **Security note:** Never commit `.env` to git. The `.gitignore` in `predsx/` excludes `deployments/.env` and `deployments/package/.env`. Verify with `git status` before committing.
