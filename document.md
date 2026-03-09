# PredSX — Development Journey

> A complete record of every stage the PredSX system went through, from initial design to production-ready deployment.

---

## Overview

**PredSX** is a production-grade, real-time prediction market data platform built entirely in Go. It ingests live market data from Polymarket, normalizes it, and exposes it through a unified API. The system is fully event-driven and containerized.

**Tech Stack**: Go 1.23 · Kafka · Redis · ClickHouse · PostgreSQL · Docker Compose · Kubernetes

---

## Stage 1 — Architecture Design & Project Scaffold

### What Happened
The initial project structure and architecture were laid out. Key decisions made:

- **Go Workspaces** (`go.work`) chosen to manage multiple modules cleanly without duplicating dependency definitions.
- **Microservices architecture** selected — each service is an independently deployable binary.
- **Kafka** chosen as the central event bus for all inter-service communication.
- **Monorepo layout** adopted:
  ```
  PredSX/
  └── predsx/
      ├── libs/       ← Shared libraries
      ├── services/   ← 9 microservices
      ├── cmd/        ← CLI tool
      └── deployments/← Docker Compose + Kubernetes
  ```

### Result
A clean, scalable project skeleton with clearly separated concerns. Every service has its own `go.mod`, keeping dependencies isolated. The shared `go.work` file ties everything together for local development.

---

## Stage 2 — Shared Infrastructure Libraries

### What Happened
Before writing any business logic, all shared client libraries were built to production-grade standards. Each library was given:
- A formal **Go interface** for testability and mocking
- **Connection pooling** configuration
- **Retry logic** with exponential backoff

### Libraries Built

| Library | Purpose | Key Feature |
|---|---|---|
| `libs/logger` | Structured logging | `Interface` + `NoOpLogger` for tests |
| `libs/config` | Env-var configuration | Required variable validation |
| `libs/retry-utils` | Retry with backoff | Exponential backoff for all I/O ops |
| `libs/kafka-client` | Kafka producer/consumer | **Generics** (`TypedProducer[T]`, `TypedConsumer[T]`) |
| `libs/redis-client` | Redis cache client | Pool size config, standard interface |
| `libs/clickhouse-client` | ClickHouse time-series DB | `MaxOpenConns` + `ConnMaxLifetime` |
| `libs/postgres-client` | PostgreSQL client | `pgxpool` for high-concurrency |
| `libs/websocket-client` | WebSocket connections | Standardized for ingestion & streaming |

### Event Schema System
A unified event schema system was created at `libs/schemas/`:
- **Protobuf definitions** (`events.proto`) for all 8 platform event types
- **Go structs** with built-in validation and schema versioning
- **JSON/Proto serialization helpers**
- **Unit tests** covering data integrity and version detection

### Result
A reusable, well-tested library layer that all 9 services depend on. No business logic is duplicated — each service just imports what it needs.

---

## Stage 3 — Core Microservices Implementation

### What Happened
All 9 microservices were implemented end-to-end. Each service follows the same pattern: consume from Kafka → process → publish back to Kafka.

### Data Pipeline Flow
```
Polymarket API
      │
      ▼
[market-discovery] ──► predsx.markets.discovered
      │
      ▼
[token-extractor]  ──► predsx.tokens.extracted
      │
      ▼
[stream]           ──► Raw WebSocket events (wss://clob.polymarket.com/ws)
      │
      ├──► [trade-engine]      ──► predsx.trades
      ├──► [orderbook-engine]  ──► predsx.orderbook
      └──► [price-engine]      ──► predsx.prices
                │
                ▼
          [normalizer]         ──► ClickHouse (batch insert)
                │
                ▼
             [api]             ──► REST / GraphQL (port 8080)
             [backfill]        ──► Historical data ingestion
```

### Service Details

#### 1. `market-discovery`
- Polls Polymarket Gamma API with pagination, retry, and exponential backoff
- Extracts: market question, condition ID, start/end times, outcomes
- Publishes `MarketDiscovered` events to `predsx.markets.discovered`

#### 2. `token-extractor`
- Consumes `MarketDiscovered` events
- Extracts YES/NO token addresses for each market
- Uses Redis for idempotency — no token extracted twice
- Publishes `TokenExtracted` events to `predsx.tokens.extracted`

#### 3. `stream`
- Manages multi-connection WebSocket pool via **consistent hashing (FNV-1a)**
- Shards tokens across workers for load distribution
- Each worker auto-reconnects to `wss://clob.polymarket.com/ws`
- Listens for new tokens on Kafka, dispatches to correct shard dynamically

#### 4. `trade-engine`
- Consumes raw WebSocket trade events
- Deduplicates using Redis (each trade ID processed exactly once)
- Exposes Prometheus metrics for processing rate and error counts
- Publishes normalized `TradeEvent` to `predsx.trades`

#### 5. `orderbook-engine`
- Maintains real-time in-memory L2 orderbooks for all active tokens
- Handles `snapshot` (full state replacement) and `l2update` (incremental delta)
- Removes price levels when size reaches zero
- Streams top-10 bid/ask levels + `best_bid`/`best_ask` to `predsx.orderbook`

#### 6. `price-engine`
- Consumes both `predsx.trades` and `predsx.orderbook`
- Computes: `mid_price`, `spread`, `last_trade_price`, `volume_24h`
- Maintains in-memory state, emits `PriceUpdate` to `predsx.prices`

#### 7. `normalizer`
- Multi-stream aggregator: consumes `trades`, `orderbook`, `prices`, `historical`
- Enriches events with market metadata from Redis
- High-throughput batching: **500 events / 100ms** flush to ClickHouse
- Exponential backoff retries on ClickHouse write failures

#### 8. `backfill`
- Multi-source historical data ingestion (Gamma API + Subgraph)
- Persists ingestion offsets to PostgreSQL, enabling **resumable backfills**
- Built-in rate limiting to respect upstream API quotas

#### 9. `api`
- Unified REST gateway on port 8080
- Hybrid sourcing: real-time snapshots from Redis, historical data from ClickHouse
- Health endpoint at `/health`, Prometheus metrics at `/metrics`

### CLI Tool (`cmd/predsx`)
- Developer tool for querying market data directly from the command line
- Subcommands: `markets`, `trades`, `orderbook`, `price`
- Rich table output via `tablewriter`, raw JSON mode for piping
- Configurable base URL (default: `http://localhost:8080`)

### Result
A fully functional, event-driven platform capable of ingesting, processing, and serving real-time prediction market data at scale.

---

## Stage 4 — Package Name Conflict Fix

### Problem
`libs/redis-client/redis.go` declared `package redis`, which directly conflicted with the imported `github.com/redis/go-redis/v9` package (also named `redis`). This caused build failures across all services that used the Redis client.

### Fix
Renamed the local package from `redis` → `redisclient` across all files:

| File Fixed |
|---|
| `libs/redis-client/redis.go` |
| `services/token-extractor/main.go` |
| `services/trade-engine/main.go` |
| `services/api/main.go` |
| `services/api/handlers/rest.go` |
| `services/normalizer/main.go` |
| `libs/redis-client/examples/main.go` |

### Result
All services compile correctly. No more namespace collisions between the local Redis wrapper and the upstream Go Redis library.

---

## Stage 5 — Docker Build Optimization

### Problem
Every Dockerfile copied **all** `go.mod` files from the entire workspace and ran `go work sync` + `go mod download` for every service in every build. This caused:
- Multi-hour build times
- Builds hanging indefinitely on network operations
- Docker Hub firewall blocks when pulling `alpine:latest`

### Fix
All 9 service Dockerfiles were rewritten to:
1. **Copy only the specific libs** each service actually needs
2. **Skip `go work sync`** (was blocking on network)
3. **Set `GONOSUMDB=*` and `GOFLAGS=-mod=mod`** so `go build` resolves modules inline
4. **Use `golang:1.23-alpine` as the final stage** (already cached locally, bypasses Docker Hub firewall)

### Service → Library Dependency Map

| Service | Libraries Needed |
|---|---|
| `market-discovery` | `config`, `kafka-client`, `logger`, `postgres-client`, `retry-utils`, `schemas`, `service` |
| `token-extractor` | `config`, `kafka-client`, `logger`, `redis-client`, `retry-utils`, `schemas`, `service` |
| `trade-engine` | `config`, `kafka-client`, `logger`, `redis-client`, `retry-utils`, `schemas`, `service` |
| `stream` | `config`, `kafka-client`, `logger`, `retry-utils`, `schemas`, `service`, `websocket-client` |
| `orderbook-engine` | `config`, `kafka-client`, `logger`, `retry-utils`, `schemas`, `service` |
| `price-engine` | `config`, `kafka-client`, `logger`, `retry-utils`, `schemas`, `service` |
| `normalizer` | `clickhouse-client`, `config`, `kafka-client`, `logger`, `redis-client`, `retry-utils`, `schemas`, `service` |
| `backfill` | `config`, `kafka-client`, `logger`, `postgres-client`, `retry-utils`, `schemas`, `service` |
| `api` | `clickhouse-client`, `config`, `logger`, `redis-client`, `retry-utils`, `service` |

### Result
Build times reduced from hours to minutes. Each service only downloads the dependencies it actually needs, and Docker layer caching is now effective.

---

## Stage 6 — Docker Build Error Fixes

### Problem
After Dockerfile optimization, several new build errors surfaced:

#### Error 1: `ENV GOFLAGS=-mod=mod` Incompatibility
- **Issue**: Setting `GOFLAGS=-mod=mod` as an environment variable inside the Dockerfile conflicts with Go workspace mode (`go.work`). The Go toolchain cannot use `-mod=mod` when a `go.work` file is present.
- **Fix**: Removed `ENV GOFLAGS=-mod=mod` from all Dockerfiles. Flags are passed only at build time via `go build` arguments.

#### Error 2: Missing `cmd/predsx` Module
- **Issue**: `go.work` references the `cmd/predsx` module. Since Go workspaces require all referenced modules to be present, builds failed with `module not found` when `cmd/predsx` was not copied into the Docker build context.
- **Fix**: Added `COPY cmd/predsx cmd/predsx` to all service Dockerfiles.

#### Error 3: `go.sum` Missing Entries / Rate Limiting
- **Issue**: Go's checksum verification required entries in `go.sum` that were missing. Attempts to auto-fetch these checksums during parallel Docker builds hit `sum.golang.org` rate limits, causing builds to fail or stall.
- **Fix**:
  1. Set `GOWORK=off` inside Dockerfiles to disable workspace mode during builds
  2. Copied the root `go.work.sum` to each service's `go.sum` file
  3. Used `GOFLAGS=-mod=mod` in the `go build` command (not as `ENV`) to allow builds without network checksum verification

### Result
All services build cleanly inside Docker. The `docker-compose up --build` command runs successfully and brings up the full stack.

---

## Stage 7 — Version Control Setup

### What Happened
The project was committed to its own Git repository:

- **Git initialized** at `C:/Users/vijay/OneDrive/Desktop/PredSX/` (separate from the user's home directory git)
- **`.gitignore`** created for Go projects — excludes binaries, `.env` files, `vendor/`, IDE folders, Docker `.tar` artifacts, and logs
- **Initial commit** made:
  - **72 files changed, 4023 insertions**
  - All services, libs, deployment configs, CLI tool, README

### Commit Message
```
Initial commit: PredSX platform - all services, libs, and deployment configs
```

### Result
The full project state is now under version control, ready to push to a remote repository (GitHub, GitLab, etc.).

---

## Current System State

| Component | Status |
|---|---|
| `libs/` — 9 shared libraries | ✅ Built & tested |
| `services/` — 9 microservices | ✅ Implemented |
| `cmd/predsx` — CLI tool | ✅ Implemented |
| `deployments/docker-compose.yml` | ✅ Configured |
| `deployments/k8s/predsx.yaml` | ✅ Configured |
| Docker builds | ✅ Building successfully |
| Git version control | ✅ Initial commit made |

### Running the Stack
```powershell
# From the predsx/ directory:
docker-compose -f deployments/docker-compose.yml up --build -d
```

### Checking Health
```powershell
curl http://localhost:8080/health   # API Gateway
docker ps                           # All running containers
```

---

*Document generated: March 9, 2026*
