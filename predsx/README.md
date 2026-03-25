<p align="center">
  <img src="https://img.shields.io/badge/Language-Go%201.23-00ADD8?logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Streaming-Kafka-231F20?logo=apachekafka" alt="Kafka">
  <img src="https://img.shields.io/badge/State-Redis-DC382D?logo=redis" alt="Redis">
  <img src="https://img.shields.io/badge/OLAP-ClickHouse-FFCC01?logo=clickhouse" alt="ClickHouse">
</p>

# PredSX — Real-Time Prediction Market Data Engine

**PredSX** is a production-grade, event-driven data platform built in Go. It ingests live market data from Polymarket, normalizes complex trade events, maintains real-time orderbook states, and broadcasts live data to clients via WebSockets.

The official React frontend UI for this project is hosted separately at: **[PredSX-Stat](https://github.com/PredictionStreammingExchange-PredSX/PredSX-Stat)**

---

## 🚀 Features

- **Live Ingestion**: Connects directly to Polymarket WebSockets with robust connection pooling, automatic sharding, and consistent hashing.
- **Microservices Architecture**: 9 specialized Go microservices decoupled by Apache Kafka arrays.
- **In-Memory Orderbooks**: Maintains high-performance L2 orderbooks for active tokens directly in Go, persisting snapshots to Redis.
- **Real-Time Gateway**: Built-in WebSocket hub pushes live `mid_price`, `spread`, trade feeds, and orderbook updates directly to the frontend.
- **High-Throughput Storage**: Built with localized normalizer worker pools that batch-insert hundreds of events per second into ClickHouse.

## 🏗️ Architecture

The system is highly decoupled to allow horizontal scaling of any individual component. 

```text
Polymarket API 
      │
      ▼
[ stream services ] ──► Raw WS events
      │
      ├──► [ trade-engine ]     ──► predsx.trades
      ├──► [ orderbook-engine ] ──► predsx.orderbook 
      └──► [ price-engine ]     ──► predsx.prices
                │
                ▼
        [ normalizer ] ──► ClickHouse (Batch Insertion)
                │
                ▼
        [ api-gateway ] ──► REST & WebSocket Hub (port 8080)
                │
                ▼
       [ PredSX-Stat UI ]
```

## 📂 Project Structure

- `libs/`: Shared production-grade libraries (Kafka clients, ClickHouse, Redis, Websocket pools, Schemas).
- `services/`: 9 standalone microservices managing the ingestion and processing pipeline.
- `cmd/predsx/`: A built-in developer CLI tool for testing data streams directly from the terminal.
- `deployments/`: Docker Compose configurations that orchestrate the 10+ container stack.

---

## 🐳 Getting Started (Docker)

The absolute easiest way to start the entire data pipeline (including Kafka, Redis, ClickHouse, PostgreSQL, and all 9 Go microservices) is to use the provided Windows batch scripts:

```bash
# From the project root

# 1. Start all 10+ services and background workers
start-docker.bat

# 2. To completely stop and tear down the environment
stop-docker.bat
```

> **Note:** The first `start` may take a few minutes as Go downloads module dependencies inside the containers. Subsequent boots are heavily cached and very fast.

### Health Checks & Monitoring

Verify the API gateway and WebSocket hub are healthy:
```bash
curl http://localhost:8080/health
```

Prometheus Metrics are automatically exposed by all engines at:
`http://localhost:<service-port>/metrics`
