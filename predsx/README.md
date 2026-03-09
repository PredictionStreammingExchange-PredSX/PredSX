# PredSX

PredSX is a production-grade real-time prediction market data platform.

## Architecture

- **Event-Driven Microservices**: Built in Go, communicating via Kafka.
- **Data Pipeline**: 
  - `market-discovery` -> `token-extractor`
  - `stream` -> `trade-engine`, `orderbook-engine`, `price-engine`
  - `normalizer` -> ClickHouse
  - `api` -> REST interface to Redis/Postgres
- **Infrastructure**: Kafka, Redis, ClickHouse, PostgreSQL.

## Project Structure

- `libs/`: Shared libraries (logger, config, schemas, clients).
- `services/`: Individual microservices.
- `deployments/`: Docker Compose and Kubernetes manifests.

## Getting Started

### Prerequisites

- Docker and Docker Compose
- Go 1.23+ (for local development)

### Running the Full Stack

```bash
make run
```

This command will build all services and start the infrastructure using Docker Compose.

### Local Development

Use Go workspaces:

```bash
go work sync
go build ./...
```

## Monitoring

- Health: `http://localhost:<service-port>/health`
- Metrics: `http://localhost:<service-port>/metrics` (Prometheus)
