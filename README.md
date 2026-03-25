<p align="center">
  <img src="https://img.shields.io/badge/Language-Go%201.23-00ADD8?logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Streaming-Kafka-231F20?logo=apachekafka" alt="Kafka">
  <img src="https://img.shields.io/badge/State-Redis-DC382D?logo=redis" alt="Redis">
  <img src="https://img.shields.io/badge/OLAP-ClickHouse-FFCC01?logo=clickhouse" alt="ClickHouse">
</p>

# PredSX — Real-Time Prediction Market Data Engine

**PredSX** is a production-grade, event-driven data platform built in Go. It ingests live market data from Polymarket, normalizes complex trade events, maintains real-time orderbook states, and broadcasts live data to clients via WebSockets.

The official React frontend UI for this project is hosted at: **[PredSX-Stat](https://github.com/PredictionStreammingExchange-PredSX/PredSX-Stat)**

---

## 🚀 Features

- **Live Ingestion**: Connects directly to Polymarket WebSockets with robust connection pooling, automatic sharding, and consistent hashing.
- **Microservices Architecture**: 9 specialized Go microservices decoupled by Apache Kafka event streams.
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
