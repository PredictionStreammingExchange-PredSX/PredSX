<p align="center">
  <img src="https://img.shields.io/badge/Language-Go%201.23-00ADD8?logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Streaming-Kafka-231F20?logo=apachekafka" alt="Kafka">
  <img src="https://img.shields.io/badge/State-Redis-DC382D?logo=redis" alt="Redis">
  <img src="https://img.shields.io/badge/OLAP-ClickHouse-FFCC01?logo=clickhouse" alt="ClickHouse">
</p>

# PredSX — Real-Time Prediction Market Data Engine

**PredSX** is a production-grade, event-driven data platform built in Go. It ingests live market data and on-chain trade events from Polymarket, normalizes them, maintains real-time orderbook/price state, and exposes everything through a unified REST API and WebSocket gateway.

> The project was originally scaffolded as ~13 fine-grained microservices and has since been **consolidated into 5 high-performance "Core Hub" services** to keep resource usage lean on small VPS deployments (4GB RAM). The standalone React frontend (`PredSX-Stat`) has been retired — this repo is now backend-only.

---

## 🚀 Features

- **Live Ingestion**: Connects to Polymarket WebSockets and on-chain RPC (with automatic multi-endpoint failover/rotation — see [`docs/rpc-failover-design.md`](predsx/docs/rpc-failover-design.md)) with robust connection pooling and pacing.
- **Consolidated Hub Architecture**: 5 specialized Go services (`discovery`, `ingestion`, `processor`, `storage`, `api`) decoupled by Apache Kafka event streams.
- **In-Memory Orderbooks**: Maintains high-performance L2 orderbooks and live price state directly in Go, persisting snapshots to Redis.
- **Unified API & Real-Time Gateway**: REST endpoints (`/v1/*`) with Redis-backed response caching and rate limiting, plus a WebSocket hub (`/stream`) pushing live trades, orderbook updates, prices, and signals — see [`docs/api-reference.md`](predsx/docs/api-reference.md).
- **High-Throughput Storage**: Batch-inserts trades, candles, and metrics into ClickHouse, with Redis for hot state and PostgreSQL for relational/offset data.

## 🏗️ Architecture

The system is decoupled via Kafka to allow horizontal scaling of any individual hub.

```text
Polymarket (WebSocket + On-chain RPC)
      │
      ▼
[ Discovery Hub ]  ──► market & token discovery   ──► predsx.markets.*
      │
      ▼
[ Ingestion Hub ]  ──► live trades & on-chain logs ──► predsx.trades / predsx.onchain.*
      │
      ▼
[ Processor Hub ]  ──► orderbooks, prices, signals ──► predsx.orderbook / predsx.prices / predsx.signals
      │
      ▼
[ Storage Hub ]    ──► batch writes ──► ClickHouse · Redis · PostgreSQL
      │
      ▼
[ API Hub ]        ──► REST (/v1/*) & WebSocket Gateway (port 8088)
```

## 📂 Project Structure

- `predsx/libs/`: Shared production-grade libraries (Kafka clients, ClickHouse, Redis, Postgres, WebSocket pools, schemas, retry/config/logging).
- `predsx/services/`: The 5 Core Hub services — `discovery`, `ingestion`, `processor`, `storage`, `api`.
- `predsx/cmd/predsx/`: A built-in developer CLI tool for testing data streams directly from the terminal.
- `predsx/deployments/`: Docker Compose configurations (local + VPS) that orchestrate the full container stack.
- `predsx/docs/`: Design docs and API reference (RPC failover design, API reference, API improvements).

---

## 🐳 Getting Started (Docker)

The easiest way to start the entire data pipeline (Kafka, Redis, ClickHouse, PostgreSQL, and all 5 Core Hub services) is via the provided Windows batch scripts:

```bash
# From the project root

# 1. Build and start the full stack in the background
start-docker.bat

# 2. To completely stop and tear down the environment
stop-docker.bat
```

> **Note:** The first `start` may take a few minutes as Go downloads module dependencies inside the containers. Subsequent boots are heavily cached and very fast.

### Health Checks & Monitoring

Verify the API gateway and WebSocket hub are healthy:
```bash
curl http://localhost:8088/health
```

Prometheus metrics are exposed by every service at:
`http://localhost:<service-port>/metrics`
