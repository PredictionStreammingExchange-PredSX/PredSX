# Running PredSX

If you do NOT have `make` or `go` in your system path, you can still develop and run PredSX using Docker.

## Running the Platform

To start everything:
```powershell
docker-compose -f deployments/docker-compose.yml up --build
```

## Developing with Docker

If you need to build a single service:
```powershell
docker build -t predsx-api -f services/api/Dockerfile .
```

## Platform Architecture

1.  **Ingestion Layer**: `market-discovery` & `stream`
2.  **Processing Layer**: `trade-engine`, `orderbook-engine`, `price-engine`
3.  **Storage Layer**: Redis (real-time), ClickHouse (analytics), Postgres (metadata)
4.  **API Layer**: `api` service

Each service is stateless and communicates via Kafka.
Metrics are available at `http://localhost:<service-port>/metrics`.
Health checks at `http://localhost:<service-port>/health`.
