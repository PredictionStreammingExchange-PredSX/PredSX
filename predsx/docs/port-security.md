# PredSX Port Security & Docker Networking

## Why Ports Are Locked Down

PredSX runs 10 containers inside a private Docker network. Services like Redis,
Postgres, ClickHouse, and Kafka only need to talk to each other — they do not
need to be reachable from the host machine or the internet.

Docker port mappings (`"HOST:CONTAINER"`) punch a hole between the host and a
container. If you bind to `0.0.0.0` (the default), that hole is open on every
network interface — meaning anyone on the same network (or the whole internet,
on a VPS) can reach it directly. None of these infrastructure services have
authentication enabled by default, so exposure = immediate compromise.

---

## How Docker Networking Works Here

```
Your machine / VPS host
        │
        │  Port mappings = "holes" into Docker
        │
  ┌─────────────────────────────────────────────┐
  │           Docker internal network            │
  │                                              │
  │  kafka:9092      redis:6379                  │
  │  postgres:5432   clickhouse:9000             │
  │  zookeeper:2181  clickhouse:8123             │
  │                                              │
  │   All hub services connect using these       │
  │   hostnames — no port mapping required.      │
  │                                              │
  │          hub-api → port 8088                 │
  └─────────────────────────────────────────────┘
        │
        │  8088 exposed → browser / API clients
```

Container-to-container traffic (e.g. `hub-ingestion` → `clickhouse:9000`) flows
entirely inside Docker and never touches the host's network stack. Port mappings
only matter for access **from outside Docker**.

---

## Port Binding Strategy

### Dev (`docker-compose.yml`)

Development exposes some ports on the host so you can connect with local tools
(TablePlus, redis-cli, kafka-ui, etc.). All infrastructure ports are bound to
`127.0.0.1` so only your own machine can reach them — not other machines on the
same Wi-Fi or LAN.

| Port | Service | Binding | Accessible From |
|------|---------|---------|-----------------|
| 9092 | Kafka | `127.0.0.1:9092` | This machine only |
| 6379 | Redis | `127.0.0.1:6379` | This machine only |
| 5432 | Postgres | `127.0.0.1:5432` | This machine only |
| 8123 | ClickHouse HTTP | `127.0.0.1:8123` | This machine only |
| 9000 | ClickHouse native | `127.0.0.1:9000` | This machine only |
| 8088 | API Gateway | `0.0.0.0:8088` | All interfaces (intentional) |

Port 9094 (Kafka external listener) has been removed — it was only used for
connecting to Kafka from outside Docker, which is not needed in normal dev
workflow.

### VPS (`docker-compose.vps.yml`)

On a VPS, infrastructure services have **no port mappings at all**. They are
completely invisible to the host network. Only the API is publicly exposed.

| Port | Service | Binding | Accessible From |
|------|---------|---------|-----------------|
| _(none)_ | Kafka | internal only | Docker network only |
| _(none)_ | Redis | internal only | Docker network only |
| _(none)_ | Postgres | internal only | Docker network only |
| _(none)_ | ClickHouse | internal only | Docker network only |
| 8088 | API Gateway | `0.0.0.0:8088` | Public internet |

This is the most secure configuration — the only entry point into the system
is the Go API, which has rate limiting, auth middleware, and controlled endpoints.

---

## Accessing Infrastructure on VPS (SSH Tunnelling)

Even though infrastructure ports are not exposed publicly on the VPS, you can
still connect to them from your local machine using **SSH tunnels**. This routes
traffic securely through your encrypted SSH connection without opening any
firewall rules.

### How an SSH Tunnel Works

```
Your laptop                    VPS
    │                           │
    │  ssh -L LOCAL:HOST:REMOTE │
    │ ─────────────────────────►│
    │                           │──► container port (internal)
    │◄──────────────────────────│
    │                           │
Connect to localhost:LOCAL      Data travels encrypted over SSH
```

### Commands

Open a terminal and run the tunnel command. Then connect your tool to
`localhost` at the local port — it will route through to the VPS container.

**Postgres**
```bash
ssh -L 5432:localhost:5432 user@your-vps-ip
# Then connect TablePlus / psql to: localhost:5432
```

**Redis**
```bash
ssh -L 6379:localhost:6379 user@your-vps-ip
# Then run: redis-cli -h localhost -p 6379
```

**ClickHouse HTTP (for DBeaver, HTTP queries)**
```bash
ssh -L 8123:localhost:8123 user@your-vps-ip
# Then open: http://localhost:8123/play
```

**ClickHouse native (for clickhouse-client)**
```bash
ssh -L 9000:localhost:9000 user@your-vps-ip
# Then run: clickhouse-client --host localhost
```

**Kafka**
```bash
ssh -L 9092:localhost:9092 user@your-vps-ip
# Then point any Kafka tool to: localhost:9092
```

**Multiple tunnels at once (open everything in one command)**
```bash
ssh -L 5432:localhost:5432 \
    -L 6379:localhost:6379 \
    -L 8123:localhost:8123 \
    -L 9000:localhost:9000 \
    user@your-vps-ip
```

Keep the SSH session open while you work. Close it when done — the tunnels
close automatically with the session.

> **Tip:** If port 5432 is already in use locally (e.g. a local Postgres
> instance), map to a different local port:
> `ssh -L 15432:localhost:5432 user@your-vps-ip`
> Then connect to `localhost:15432`.

---

## VPS Firewall Rules (UFW)

### Without TLS (direct API access on port 8088)

```bash
ufw allow 22/tcp
ufw allow 8088/tcp
ufw default deny incoming
ufw default allow outgoing
ufw enable
ufw status
```

Expected output:
```
22/tcp                     ALLOW       Anywhere
8088/tcp                   ALLOW       Anywhere
```

### With TLS via Caddy (recommended for production)

When using the Caddy overlay, port 8088 is removed from public exposure.
Caddy becomes the only entry point and terminates TLS.

```bash
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw default deny incoming
ufw default allow outgoing
ufw enable
ufw status
```

Expected output:
```
22/tcp                     ALLOW       Anywhere
80/tcp                     ALLOW       Anywhere
443/tcp                    ALLOW       Anywhere
```

Even if Docker were to expose a port (which it currently does not for
infrastructure), UFW would block external access to it.

---

## HTTPS / TLS with Caddy

Caddy is an optional reverse proxy that handles HTTPS automatically via
Let's Encrypt. It is configured as a Docker Compose **overlay** — you apply
it on top of the base VPS compose.

### Prerequisites

1. A domain pointing at your VPS IP (DNS A record: `yourdomain.com → VPS_IP`)
2. Ports 80 and 443 open in UFW (see above)
3. `DOMAIN=yourdomain.com` set in `deployments/.env`

### How it works

```
Browser (HTTPS)
      │
      │  443/tcp  ─────────► Caddy container
      │                           │
      │                           │ Docker internal network
      │                           ▼
      │                      hub-api:8088
      │
  Caddy handles:
  - TLS cert from Let's Encrypt (auto-renewed)
  - HTTP → HTTPS redirect (port 80)
  - WebSocket upgrade (/stream)
  - Gzip compression
```

Port 8088 is **not** exposed publicly when using Caddy — only Caddy's 80/443 are open.

### Start command with TLS

```bash
docker compose \
  -f docker-compose.vps.yml \
  -f docker-compose.caddy.yml \
  up -d
```

### Start command without TLS (direct 8088)

```bash
docker compose -f docker-compose.vps.yml up -d
```

### Config file

Caddy is configured via `deployments/Caddyfile`. The `{$DOMAIN}` placeholder
is replaced at runtime with the `DOMAIN` environment variable.

To use a custom TLS cert instead of Let's Encrypt (e.g. for testing), replace
the Caddyfile domain block with:

```
yourdomain.com {
    tls /path/to/cert.pem /path/to/key.pem
    reverse_proxy hub-api:8088
}
```

---

## What Was Changed (Phase 1 — Security Hardening)

| File | Change |
|------|--------|
| `deployments/docker-compose.yml` | Bound Kafka, Redis, Postgres, ClickHouse to `127.0.0.1`; removed Kafka external port 9094 |
| `deployments/docker-compose.vps.yml` | No change needed — infra ports were already absent; added `POLYGON_RPC_URLS` to ingestion env |
| `.env` (root) | Removed hardcoded Alchemy WSS key; replaced with `POLYGON_RPC_URLS` pointing to free public endpoints |
| `deployments/.env` | Replaced dead `1rpc.io/matic` endpoint with `POLYGON_RPC_URLS` free public pool |
| `services/ingestion/main.go` | Removed dead RPC endpoints (`polygon-rpc.com`, `rpc.ankr.com`) from `defaultRPCPool` |
| `services/discovery/main.go` | Added 200ms inter-page pause to Gamma API pagination to stay within rate limits |

---

## What Was Changed (Phase 2 — Stability Hardening)

### Root Cause: VPS Kafka/Storage Crash Loop

On the original VPS compose, all 5 hub services had `kafka: condition: service_started`. Docker interprets this as "start the hub as soon as Kafka's container process starts" — but Kafka takes 20–30 seconds to actually accept connections. Every hub immediately crashed trying to connect, logged errors, and restarted via `restart: always`. This created a crash-loop flood in the logs until Kafka was ready.

The fix is two parts: (1) add a proper Kafka healthcheck so Docker knows when Kafka is actually ready, and (2) change all hub dependencies to `condition: service_healthy` so they don't start until the healthcheck passes.

### Changes

| File | Change |
|------|--------|
| `deployments/docker-compose.vps.yml` | Added Kafka healthcheck (`kafka-topics --list`) with 30s start period |
| `deployments/docker-compose.vps.yml` | Changed all 5 hub services: `kafka: service_started` → `kafka: service_healthy` |
| `deployments/docker-compose.vps.yml` | Added named volumes `kafka_data`, `zookeeper_data`, `zookeeper_log` — Kafka topic data and consumer offsets now survive container restarts |
| `deployments/docker-compose.vps.yml` | Added `logging` block to Redis (was missing — logs were unbounded) |
| `deployments/docker-compose.vps.yml` | Added `--save "" --appendonly no` to Redis command (disables disk persistence — Redis is pure cache on VPS) |
| `deployments/docker-compose.vps.yml` | Added Zookeeper autopurge settings to prevent snapshot accumulation |
| `deployments/docker-compose.yml` | Added named volumes `kafka_data`, `zookeeper_data`, `zookeeper_log` to dev compose (same fix — local restarts no longer lose Kafka data) |

### Startup Order After Phase 2

```
zookeeper (starts immediately)
    │
    └── kafka (waits for zookeeper, then runs healthcheck)
            │
            └── all 5 hubs (wait for kafka: service_healthy)
                    │
                    also wait for: redis: healthy, postgres: healthy, clickhouse: healthy
```

No hub starts until every dependency it needs is confirmed ready. This eliminates the crash-loop entirely.

---

## What Was Changed (Phase 3 — Pre-VPS Deploy)

| File | Change |
|------|--------|
| `predsx/.dockerignore` | Created — excludes `docs/`, `deployments/`, `*.md`, `.git`, secrets from Docker build context |
| `deployments/Caddyfile` | Created — Caddy reverse proxy config with auto-TLS, gzip, WebSocket support |
| `deployments/docker-compose.caddy.yml` | Created — optional TLS overlay; apply on top of VPS compose when domain is ready |
| `deployments/docker-compose.vps.yml` | Added `METRICS_PORT` to all 4 hub services (8081–8084) so VPS healthchecks resolve correctly |
| `deployments/docker-compose.vps.yml` | Changed processor `BACKTEST_PORT` to 8085 (avoids collision with ingestion METRICS_PORT=8082) |
| `deployments/docker-compose.yml` | Same `METRICS_PORT` fixes in dev compose for parity |
| `docs/port-security.md` | Updated UFW rules for TLS setup; added full Caddy section |

### Why METRICS_PORT mattered

`service.BaseService` (in `libs/service/service.go`) starts a lightweight HTTP
server with `/health` and `/metrics` on `METRICS_PORT`, defaulting to **8080**.
The VPS compose healthchecks were calling ports 8081–8084 — which matched
nothing, so all 4 hub healthchecks were silently failing. Docker still started
the services (it doesn't block on hub healthchecks for dependencies), but any
monitoring or `docker ps` health status would show `unhealthy` for every hub.
Adding `METRICS_PORT=808x` to each service's env section fixes this without
any code changes.

### Port assignment after Phase 3

| Hub | METRICS_PORT | Purpose |
|-----|-------------|---------|
| hub-discovery | 8081 | `/health` + `/metrics` |
| hub-ingestion | 8082 | `/health` + `/metrics` |
| hub-processor | 8083 | `/health` + `/metrics` |
| hub-storage | 8084 | `/health` + `/metrics` |
| hub-processor | 8085 | `BACKTEST_PORT` — backtester HTTP API |
| hub-api | 8088 | Public API gateway (or Caddy-internal when TLS overlay is active) |

None of ports 8081–8085 are exposed outside Docker on VPS — they are container-internal only. They exist so `docker ps` health status and Prometheus scraping work correctly within the Docker network.

---

## What Was Changed (Phase 4 — Cleanup)

Phase 4 was internal code and build tooling cleanup with no networking changes.

| File | Change |
|------|--------|
| `services/storage/main.go` | Replaced all `fmt.Println`/`fmt.Printf` with `svc.Logger.Info`/`svc.Logger.Error` (structured slog JSON) |
| `services/storage/main.go` | Added `log logger.Interface` parameter to `ensureSchemas()` and `persistMarketMetadata()` |
| `build-and-ship.bat` | Each `docker build` now tags both `:latest` and `:<git-short-hash>` for rollback capability |

### Why storage hub needed structured logging

The storage hub is the highest-volume writer in the system — it handles schema
init, 500-event/100ms batch flushes to ClickHouse, and market metadata saves.
All its startup and error output was previously `fmt.Println` plain text:

```
initializing clickhouse schemas...
checking events_raw table...
Saving market to ClickHouse: 0x123 (some-slug)
```

Problems with this:
- No timestamp, no severity level, no service name
- Invisible to `docker logs --since` filters that rely on structured fields
- `grep ERROR` matches nothing — errors look identical to info lines
- Inconsistent with all other hubs, which already used `svc.Logger`

After Phase 4, every line is JSON:
```json
{"time":"2026-06-11T14:23:01Z","level":"INFO","msg":"checking table","name":"events_raw"}
{"time":"2026-06-11T14:23:05Z","level":"ERROR","msg":"failed to save market","id":"0x123","err":"timeout"}
```

This makes `docker logs hub-storage | grep '"level":"ERROR"'` work correctly and
keeps all hubs consistent for any future log shipping setup.

### How to roll back a deploy

Each `build-and-ship.bat` run prints the git hash it tagged. To roll back:

```bash
# On VPS — stop the running stack
docker compose -f docker-compose.yml down

# Edit docker-compose.yml: change image tags from :latest to :<old-hash>
# e.g.  image: predsx-api:latest  →  image: predsx-api:fc0dd1c
sed -i 's/:latest/:fc0dd1c/g' docker-compose.yml

# Bring the stack back up on the old images
docker compose -f docker-compose.yml up -d
```

The old hash images are already loaded on the VPS from when you ran `docker load`
during that deploy — no rebuild needed.

---

## Related Docs

- [api-reference.md](api-reference.md) — API endpoint reference and rate limits
- [rpc-failover-design.md](rpc-failover-design.md) — Polygon RPC pool rotation design
- [info.md](info.md) — Storage and logging policy
- [storage-projections.md](storage-projections.md) — Disk and RAM capacity planning
