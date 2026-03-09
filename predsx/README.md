# PredSX

PredSX is a production-grade, real-time prediction market data platform built entirely in Go. It ingests live market data from Polymarket, normalizes it, and exposes it through a unified API.

For a complete history of the project's development, see [document.md](../document.md).

## Architecture

- **Event-Driven Microservices**: Built in Go, communicating via Kafka.
- **Data Pipeline**: 
  - `market-discovery` -> `token-extractor`
  - `stream` -> `trade-engine`, `orderbook-engine`, `price-engine`
  - `normalizer` -> ClickHouse
  - `api` -> REST interface to Redis/ClickHouse
  - `backfill` -> Historical data ingestion to PostgreSQL/ClickHouse
- **Infrastructure**: Kafka, Redis, ClickHouse, PostgreSQL.
- **CLI Tool**: A developer tool for querying market data directly from the command line.

## Project Structure

- `libs/`: 9 shared production-grade libraries (logger, config, schemas, various DB/Stream clients).
- `services/`: 9 individual microservices handling the data pipeline.
- `cmd/predsx/`: Command-line tool for interacting with the platform.
- `deployments/`: Docker Compose and Kubernetes manifests.

## Getting Started

### Prerequisites

- Docker and Docker Compose
- Go 1.23+ (for local development)

### Running the Full Stack

The entire stack (all 9 services + infrastructure) can be brought up using Docker Compose:

```bash
docker-compose -f deployments/docker-compose.yml up --build -d
```

> **Note:** First-time builds may take several minutes as Go downloads module dependencies. Subsequent builds are heavily cached and very fast.

### Health Checks

Verify the API gateway is healthy:
```bash
curl http://localhost:8080/health
```

Check running containers:
```bash
docker ps
```

### Local Development

The project uses Go workspaces. For local development outside of Docker:

```bash
go run ./services/api/main.go
# or build everything
go build ./...
```

## Monitoring

- Health: `http://localhost:<service-port>/health`
- Metrics: `http://localhost:<service-port>/metrics` (Prometheus)
