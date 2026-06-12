# PredSX — Master Documentation

> Consolidated reference from: `docker-commands.md`, `document.md`, `dairy.txt`, `DEVELOPMENT.md`

---

## Table of Contents

1. [Project Overview](#1-project-overview)
2. [Architecture & Tech Stack](#2-architecture--tech-stack)
3. [Platform Services](#3-platform-services)
4. [Quick Start — Docker Commands](#4-quick-start--docker-commands)
5. [Local Development (Windows/Mac)](#5-local-development-windowsmac)
6. [VPS Deployment (Ubuntu Linux)](#6-vps-deployment-ubuntu-linux)
7. [Developing with Docker](#7-developing-with-docker)
8. [VPS Storage & Logging Policy](#8-vps-storage--logging-policy)
9. [Development Journey — Stage by Stage](#9-development-journey--stage-by-stage)

---

## 1. Project Overview

**PredSX** is a production-grade, real-time prediction market data platform. It ingests live market data from Polymarket, normalizes and analyzes it for trading signals, and exposes it through a unified REST API and WebSocket gateway.

This repo is backend-only: a highly concurrent, event-driven system ("The Brain") built entirely in Go 1.23. The standalone Next.js frontend (PredSX-Stat) has been retired and removed from this stack.

**Entry Points:**
- **API Gateway** (Port `8088`): Unified REST API (`/v1/*`) with Redis-backed caching and rate limiting.
- **WebSocket Stream** (`/stream`): Live feed of trades, orderbook updates, prices, and signals.
- **CLI Tool** (`cmd/predsx`): Developer tool for querying market data from the terminal.
- **Health checks**: `http://localhost:<port>/health`
- **Metrics**: `http://localhost:<port>/metrics`

---

## 2. Architecture & Tech Stack

The platform uses a **microservices architecture** managed via Go Workspaces (`go.work`). Originally 13 independent services, consolidated into **5 Core Hubs** to optimize for 4 GB RAM VPS constraints. All inter-service communication goes through Kafka — no direct HTTP calls between services.

| Technology | Role |
|---|---|
| Go 1.23 | Backbone language for all backend services |
| Apache Kafka | Central event bus |
| Redis | Caching, orderbook state, idempotency |
| ClickHouse | Time-series DB — 500 events/100ms batch inserts |
| PostgreSQL | Relational state (resumable backfill offsets) |
| Docker Compose | Local + production container orchestration |
| Kubernetes | Production deployment (k8s manifests included) |

**Data Flow:**
```
Polymarket WS / On-chain RPC
        │
        ▼
  Discovery Hub  →  Ingestion Hub  →  Processor Hub
                                              │
                                              ▼
                                       Storage Hub  →  ClickHouse
                                              │
                                              ▼
                                          API Hub  →  REST / WebSocket
```

---

## 3. Platform Services

The 5 Core Hubs live in `predsx/services/`:

| Hub | Merged From | Responsibility |
|---|---|---|
| **Discovery** | `market-discovery`, `token-extractor` | Polls Polymarket Gamma API; extracts YES/NO token addresses with Redis idempotency |
| **Ingestion** | `stream`, `stream-aggregator`, `indexer` | Multi-connection WebSocket pool (FNV-1a consistent hashing); on-chain Polygon RPC with multi-endpoint failover |
| **Processor** | `trade-engine`, `orderbook-engine`, `price-engine`, `analyzer` | Real-time L2 orderbooks, mid-price/spread/volume calculations, trading signals |
| **Storage** | `normalizer`, `backfill` | Batch-flushes all raw events to ClickHouse; persists ingestion offsets to Postgres; historical backfill with resumable offsets |
| **API** | `api` | Unified REST gateway + WebSocket hub on port `8088` |

Each service is stateless. See [`api-reference.md`](api-reference.md) for full REST docs and [`rpc-failover-design.md`](rpc-failover-design.md) for ingestion failover details.

---

## 4. Quick Start — Docker Commands

Run all commands from the root `PredSX` folder (`C:\path\to\PredSX`).

### Start the Stack
```bash
docker compose -f predsx/deployments/docker-compose.yml up --build -d
```

### Stop the Stack
```bash
docker compose -f predsx/deployments/docker-compose.yml down
```
> Data is persisted safely in Docker volumes — `down` does not wipe the database.

### View Logs
```bash
# All services
docker compose -f predsx/deployments/docker-compose.yml logs -f

# Single service
docker logs -f package-hub-ingestion-1
```

### Build a Single Service
```bash
docker build -t predsx-api -f predsx/services/api/Dockerfile .
```

> **Shortcut scripts**: `start-docker.bat` and `stop-docker.bat` in the project root wrap the commands above — double-click or run from any terminal.

---

## 5. Local Development (Windows/Mac)

**Prerequisites:** Docker Desktop installed and running.

```powershell
# 1. Open a terminal at the project root
# 2. Start all services
.\start-docker.bat

# 3. Verify the API is up
curl http://localhost:8088/health

# 4. Stop everything
.\stop-docker.bat
```

---

## 6. VPS Deployment (Ubuntu Linux)

**Prerequisites on VPS:** Docker Engine + Docker Compose V2, SSH access.

```bash
# 1. Transfer code (git clone or scp)
# 2. SSH in
ssh user@your-vps-ip

# 3. Navigate to project directory
cd ~/PredSX

# 4. Grant execute permissions
chmod +x deploy.sh

# 5. Start the stack
docker compose -f predsx/deployments/docker-compose.yml up --build -d

# 6. Monitor startup
docker compose -f predsx/deployments/docker-compose.yml logs -f
```

---

## 7. Developing with Docker

If you don't have `make` or `go` locally, Docker covers full development:

```powershell
# Start everything (from predsx/ subfolder)
docker-compose -f deployments/docker-compose.yml up --build

# Build a single service image
docker build -t predsx-api -f services/api/Dockerfile .
```

---

## 8. VPS Storage & Logging Policy

To prevent 100% disk usage crashes (observed in earlier 48-hour VPS runs):

| Mechanism | Setting | Purpose |
|---|---|---|
| Docker log rotation | Max 10 MB × 3 files per service | Caps container log disk use |
| Kafka retention | **2-hour TTL**, 1 GB max | Transit pipe only — not for replay |
| Redis | In-memory only (`--save "" --appendonly no`), 64 MB cap, LRU eviction | Pure cache layer, no disk writes |
| ClickHouse TTL | See table below | Auto-purges stale partitions |

### ClickHouse Table TTLs

| Table | Retention | Notes |
|---|---|---|
| `events_raw` | **3 days** | Raw WS payloads — high volume, short window |
| `trades` | **7 days** | Normalized trade events for recent analytics |
| `orderbook_history` | **2 days** | Snapshot history — highest write volume |
| `onchain_trades` | **14 days** | On-chain Polygon events |
| `market_metrics` | **2 days** | Aggregated per-market metrics |
| `market_metadata` | **permanent** | Market registry — never expires |
| `price_history_1m` | permanent (SummingMergeTree) | 1-minute OHLCV candles |
| `price_history_1h` | permanent (SummingMergeTree) | 1-hour OHLCV candles |

TTLs are enforced by ClickHouse's `MergeTree` engine and applied automatically — no manual cleanup needed. TTL clauses are defined inline in `services/storage/main.go:ensureSchemas()`, not in `deployments/clickhouse/init.sql` (that file is a stale reference copy and is not mounted by Docker).

**Analytics note:** For model training, query `trades` or `price_history_1h` directly from ClickHouse via HTTP (`localhost:8123/play` locally, or via SSH tunnel on VPS). Do not rely on container logs.

---

## 9. Development Journey — Stage by Stage

### Stage 1 — Architecture Design & Project Scaffold

Key decisions:
- **Go Workspaces** (`go.work`) to manage multiple modules without duplicating dependencies.
- **Microservices architecture** — each service is an independently deployable binary.
- **Kafka** as the central event bus.
- **Monorepo layout**:
  ```
  PredSX/
  └── predsx/
      ├── libs/       ← Shared libraries
      ├── services/   ← Microservices
      ├── cmd/        ← CLI tool
      └── deployments/← Docker Compose + Kubernetes
  ```

**Result:** Clean, scalable skeleton with isolated `go.mod` per service, unified by `go.work`.

---

### Stage 2 — Shared Infrastructure Libraries

All shared client libraries built to production grade before any business logic:

| Library | Purpose | Key Feature |
|---|---|---|
| `libs/logger` | Structured logging | Interface + NoOpLogger for tests |
| `libs/config` | Env-var configuration | Required variable validation |
| `libs/retry-utils` | Retry with backoff | Exponential backoff for all I/O |
| `libs/kafka-client` | Kafka producer/consumer | Generics (`TypedProducer[T]`, `TypedConsumer[T]`) |
| `libs/redis-client` | Redis cache client | Pool size config |
| `libs/clickhouse-client` | ClickHouse time-series DB | `MaxOpenConns` + `ConnMaxLifetime` |
| `libs/postgres-client` | PostgreSQL client | `pgxpool` for high concurrency |
| `libs/websocket-client` | WebSocket connections | Standardized for ingestion & streaming |

A unified event schema system at `libs/schemas/` provides Protobuf definitions, Go structs with validation, JSON/Proto serialization helpers, and unit tests for all 8 platform event types.

---

### Stage 3 — Core Microservices Implementation

All 9 original microservices implemented end-to-end:

```
Polymarket API
      │
      ▼
[market-discovery] ──► predsx.markets.discovered
      │
[token-extractor]  ──► predsx.tokens.extracted
      │
[stream]           ──► Raw WebSocket events
      │
      ├──► [trade-engine]      ──► predsx.trades
      ├──► [orderbook-engine]  ──► predsx.orderbook
      └──► [price-engine]      ──► predsx.prices
                │
          [normalizer]         ──► ClickHouse (500 events / 100ms)
                │
             [api]             ──► REST / WebSocket (port 8080)
             [backfill]        ──► Historical data ingestion
```

**CLI Tool** (`cmd/predsx`): subcommands `markets`, `trades`, `orderbook`, `price`; rich table output via `tablewriter`; JSON pipe mode.

---

### Stage 4 — Package Name Conflict Fix

`libs/redis-client/redis.go` declared `package redis`, conflicting with `github.com/redis/go-redis/v9`. Fixed by renaming the local package to `redisclient` across all consuming services.

---

### Stage 5 — Docker Build Optimization

**Problem:** Dockerfiles copied all `go.mod` files and ran `go work sync` for every service — multi-hour builds, network hangs, Docker Hub firewall blocks.

**Fix:** Rewrote all Dockerfiles to copy only the libs each service needs, disabled `go work sync`, set `GONOSUMDB=*`, and used the locally cached `golang:1.23-alpine` base image.

**Result:** Build times reduced from hours to minutes; Docker layer caching now effective.

---

### Stage 6 — Docker Build Error Fixes

Three classes of errors resolved:

1. **`ENV GOFLAGS=-mod=mod` incompatibility** with `go.work` — removed the `ENV` line; flags passed only at `go build` time.
2. **Missing `cmd/predsx` module** — `go.work` requires all referenced modules present; added `COPY cmd/predsx cmd/predsx` to all Dockerfiles.
3. **`go.sum` missing entries / rate limiting** — set `GOWORK=off` in Dockerfiles, copied root `go.work.sum` to each service's `go.sum`, used `GOFLAGS=-mod=mod` in the `go build` command.

---

### Stage 7 — Version Control Setup

- Git initialized at `C:/path/to/PredSX/`
- `.gitignore` covers binaries, `.env`, `vendor/`, IDE folders, Docker `.tar` artifacts, logs
- Initial commit: **72 files, 4023 insertions**

---

### Stage 8 — Production Gap Fixes & Database Integration

- **Redis Integration**: Replaced stub API data with live state fetches; persisted active orderbook to Redis.
- **ClickHouse**: Rigorous table schema init on startup + TTL strategy for raw event expiry.
- **Normalizer Scale-Out**: Worker pool for high-throughput Kafka-to-ClickHouse ingestion.

---

### Stage 9 — Real-Time WebSocket Gateway

- New `/stream` endpoint added to the `api` service.
- Internal WebSocket hub managing hundreds of concurrent client connections.
- Broadcast mechanism syncing Kafka stream payloads to all connected clients.

---

### Stage 10 — End-to-End Pipeline Hardening

- **Docker Build Stability**: Resolved `go.work` and module resolution edge cases.
- **End-to-End Data Flow**: Fixed upstream Polymarket `stream` connection bugs; verified pipeline from ingestion to WebSocket broadcast.
- **Developer Experience**: Added `start-docker.bat` / `stop-docker.bat` for frictionless local startup.

---

### Stage 11 — Hub Consolidation (5-Core Model)

After 48 hours on a 4 GB VPS, a storage crash prompted architectural consolidation from 13 services into the current 5 Core Hubs:

| Optimization | Impact |
|---|---|
| Hub consolidation | ~30% reduction in Docker RAM overhead |
| Storage bypass (Kafka + log rotation) | Eliminated 100% disk usage risk |
| Local build & ship (`.tar` transfer) | Bypassed Go compiler RAM spikes on VPS |

---

### Stage 12 — Pre-VPS Security & Stability Hardening

Before migrating to production VPS, a full audit identified and resolved two categories of issues.

**Phase 1 — Security:**

| Issue | Fix |
|---|---|
| Hardcoded Alchemy WSS key in `.env` | Removed; replaced with `POLYGON_RPC_URLS` free public pool |
| Dead RPC endpoints (`polygon-rpc.com`, `rpc.ankr.com`) in ingestion | Removed from `defaultRPCPool` |
| Infrastructure ports (`9092`, `6379`, `5432`, `8123`, `9000`) bound to `0.0.0.0` | Re-bound to `127.0.0.1` in dev compose |
| Kafka external listener port 9094 exposed unnecessarily | Port removed entirely |
| Gamma API pagination firing 30–50 requests/second | Added 200ms inter-page sleep → 5 req/s |
| `POLYGON_RPC_URL` (singular, dead) vs `POLYGON_RPC_URLS` (plural, actual) mismatch | Fixed env var name in both `.env` files and both compose files |

**Phase 2 — Stability:**

| Issue | Fix |
|---|---|
| VPS: hub services started before Kafka was ready → crash loop | Added Kafka healthcheck; changed all hubs to `condition: service_healthy` |
| Kafka + Zookeeper had no named volumes → data lost on every restart | Added `kafka_data`, `zookeeper_data`, `zookeeper_log` volumes |
| Redis log rotation missing in VPS compose | Added `json-file` logging with 10 MB × 3 file cap |
| Redis disk persistence enabled on VPS (unnecessary writes) | Added `--save "" --appendonly no` |
| Zookeeper snapshots accumulating indefinitely | Added `AUTOPURGE_SNAP_RETAIN_COUNT=3` and `PURGE_INTERVAL=24` |

**Phase 3 — Pre-VPS Deploy:**

| Issue | Fix |
|---|---|
| No `.dockerignore` — entire `predsx/` repo sent as Docker build context | Created `predsx/.dockerignore` excluding `docs/`, `deployments/`, `.git`, secrets |
| No TLS — API served over plain HTTP on port 8088 | Added `deployments/Caddyfile` + `docker-compose.caddy.yml` overlay for auto-HTTPS via Let's Encrypt |
| Hub healthchecks silently failing — `METRICS_PORT` defaulted to 8080 but checks called 8081–8084 | Added `METRICS_PORT=808x` to all 4 hub services in both compose files |
| Processor backtester port (8082) collided with ingestion metrics port (8082) | Changed processor `BACKTEST_PORT` to 8085 |

**Why `.dockerignore` matters:** Without it, the entire `predsx/` directory — including `docs/`, `deployments/`, `.git/`, and all Markdown files — is sent to the Docker daemon as the build context before each image build. This slows down every `docker build` call (network transfer to daemon) and risks accidentally including secrets or environment files inside images. The `.dockerignore` reduces the context to only what Go actually needs: `libs/`, `services/`, `cmd/`, `go.work`, `go.work.sum`.

**Why Caddy over Nginx:** Caddy handles Let's Encrypt TLS certificate issuance and renewal automatically with zero config — no certbot, no cron job. It is deployed as an optional overlay (`docker-compose.caddy.yml`) so you can run the stack without a domain (direct port 8088) and add TLS later by simply adding the overlay. When active, Caddy takes over port 80/443 and removes the public 8088 mapping from `hub-api`.

**Why METRICS_PORT was silent:** `service.BaseService` (in `libs/service/service.go`) starts an HTTP server on `METRICS_PORT` env var, defaulting to 8080. All 4 hub healthchecks in the VPS compose were calling ports 8081–8084 but the services were all listening on 8080. Every healthcheck was returning connection refused. Docker still ran the services (hub healthchecks are not used as dependency conditions), but `docker ps` showed every hub as `unhealthy`. Setting `METRICS_PORT=808x` per service costs zero code changes and fixes all four.

**Phase 4 — Cleanup:**

| Issue | Fix |
|---|---|
| `services/storage/main.go` had 15+ `fmt.Println`/`fmt.Printf` calls — no structured context, invisible to log aggregators | Replaced all with `svc.Logger.Info`/`svc.Logger.Error` (slog JSON); added `logger.Interface` parameter to `ensureSchemas` and `persistMarketMetadata` |
| `build-and-ship.bat` tagged all images `:latest` only — no way to roll back to a specific build | Now also tags each image `:<git-short-hash>`; rollback instructions printed after each build |

**Why structured logging matters on VPS:** The storage hub is the highest-write service — it handles ClickHouse schema init, batch inserts, and every market metadata save. With `fmt.Println`, all those messages were unstructured plain text with no timestamp, service name, or severity level, making them impossible to grep reliably or ship to a log aggregator. With `svc.Logger` (backed by Go's `log/slog` JSON handler), every line is a JSON object with `time`, `level`, `msg`, and any key-value fields. This makes `docker logs | grep '"level":"ERROR"'` work, and makes the storage hub's log output consistent with all other hubs which already used `svc.Logger`.

**Why git hash tagging matters:** Every `build-and-ship.bat` run previously overwrote `:latest` on all 5 images with no record of what changed. If a deploy broke something, there was no prior image to roll back to — you'd have to rebuild from source on VPS. Now each build also tags `:<short-hash>` (e.g. `predsx-api:fc0dd1c`). The `:latest` tag still advances as normal, but the hash tag is permanent. Rolling back is: stop the stack, change image tags in `docker-compose.yml` to the old hash, bring the stack back up.

See [`port-security.md`](port-security.md) for the full networking, port binding, and TLS documentation. See [`storage-projections.md`](storage-projections.md) for disk and RAM capacity planning.

---

### Stage 13 — VPS First Deploy: Production Bug Fixes

After the first real VPS deploy, two bugs surfaced that only appeared with live Polymarket data.

---

**Bug 1 — `/v1/markets?status=ACTIVE` returned `{"data":[]}`**

Root cause: `PolymarketMarket` struct in `services/discovery/main.go` had `Status string \`json:"status"\`` but the Polymarket Gamma API returns boolean fields (`active`, `closed`, `resolved`), not a string `status`. The field always deserialized as `""`. Storage wrote all markets to ClickHouse with empty `status`. The API query uses `WHERE LOWER(status) = LOWER(?)`, so filtering for `ACTIVE` returned zero rows.

Fix:
- Added `Active bool \`json:"active"\``, `Closed bool \`json:"closed"\``, `Resolved bool \`json:"resolved"\`` to `PolymarketMarket`
- Added `DerivedStatus()` method that maps booleans → `"ACTIVE"` / `"CLOSED"` / `"RESOLVED"` / `"UNKNOWN"`, with fallback to the string field if present
- Changed event construction to use `Status: m.DerivedStatus()`

After deploying the fixed discovery image (`docker compose up -d --no-deps hub-discovery`), new rows appeared in ClickHouse with correct status within one poll cycle (~60s).

---

**Bug 2 — `/v1/markets/{id}/price` returned `"price not found"` for all markets**

Root cause: key mismatch between writer and reader.
- Processor hub writes to Redis key: `live:price:<marketID>`
- API `GetPrice` handler read from: `predsx:price:<marketID>`

Fix: Changed `rest.go` line 333 from `predsx:price:` to `live:price:`.

---

**Operational lessons learned:**

| Lesson | Detail |
|--------|--------|
| `docker save >` corrupts tar files | PowerShell `>` writes UTF-16 with BOM. Always use `docker save -o file.tar` |
| `docker compose restart` does not load new images | It restarts the existing container. Use `docker compose up -d --no-deps <service>` to recreate with a newly loaded image |
| ClickHouse database is `default`, not `predsx` | All tables live in `default`. Queries must use `default.market_metadata` etc. |
| Container prefix is `package-*`, not `predsx-*` | Docker uses the compose file's directory name (`package`) as the project prefix |

---

## Final Checklist

| Component | Status |
|---|---|
| `libs/` — 9 shared libraries | ✅ Built & tested |
| `services/` — 5 Core Hubs | ✅ Consolidated & optimized |
| `cmd/predsx` — CLI tool | ✅ Implemented |
| `deployments/docker-compose.yml` | ✅ Updated for 5 Hubs, ports locked to `127.0.0.1`, Kafka volumes added |
| `deployments/docker-compose.vps.yml` | ✅ All ports internal-only, Kafka healthcheck, `service_healthy` deps, volumes |
| Redis / ClickHouse / WebSockets | ✅ Integrated |
| Docker builds | ✅ Fast & stable |
| Security hardening (Phase 1) | ✅ Done |
| Stability hardening (Phase 2) | ✅ Done |
| Pre-VPS deploy prep (Phase 3) | ✅ Done — `.dockerignore`, Caddy TLS overlay, hub `METRICS_PORT` wired |
| Nice-to-have cleanup (Phase 4) | ✅ Done — structured logger in storage hub, git-tagged image builds |
| VPS first deploy | ✅ Live on Hetzner `YOUR_VPS_IP:8088` |
| Discovery status bug fix | ✅ `DerivedStatus()` — Gamma API booleans → proper status string |
| Price endpoint Redis key fix | ✅ `live:price:` key — processor and API now aligned |

---

## Related Docs

- [vps-deploy.md](vps-deploy.md) — Full VPS deployment guide and API endpoint verification
- [port-security.md](port-security.md) — Port bindings, networking, UFW, TLS
- [api-reference.md](api-reference.md) — API endpoint reference and response schemas
- [storage-projections.md](storage-projections.md) — Disk and RAM capacity planning
- [rpc-failover-design.md](rpc-failover-design.md) — Polygon RPC pool rotation design

---

*Last updated: June 12, 2026*
