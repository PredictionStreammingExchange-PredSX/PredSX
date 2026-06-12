# PredSX — VPS Deployment Guide

> Full step-by-step guide for deploying PredSX to a Hetzner (or any Ubuntu) VPS.
> Covers building locally, shipping images, starting the stack, and verifying every API endpoint.

---

## Prerequisites

| Requirement | Notes |
|---|---|
| Docker Desktop installed locally | For building images on your dev machine |
| VPS with Ubuntu 22.04+ | 4 GB RAM minimum |
| SSH key configured | `~/.ssh/your_ssh_key` |
| Git repo is on `main` | Images are tagged with the git commit hash |

**Never build images on the VPS.** The Go compiler spikes 1–2 GB of RAM per service. With a 4 GB VPS running 10 containers, there is not enough headroom. Always build locally and ship pre-built `.tar` files.

---

## Step 1 — Build Images Locally

Open PowerShell in `C:\path\to\PredSX\predsx\` and run:

```powershell
.\build-and-ship.bat
```

What it does:
1. Captures the short git commit hash (e.g. `cc27bce`) for rollback tagging
2. Runs `docker build` for all 5 services: `discovery`, `ingestion`, `processor`, `storage`, `api`
3. Tags each image as both `:latest` and `:<git-hash>` (e.g. `predsx-api:cc27bce`)
4. Saves all 5 images as `.tar` files into `deployments/package/`
5. Copies `docker-compose.vps.yml` → `deployments/package/docker-compose.yml`
6. Copies `deployments/.env` → `deployments/package/.env`

When complete, `deployments/package/` contains:
```
api.tar
discovery.tar
ingestion.tar
processor.tar
storage.tar
docker-compose.yml
.env
```

Note the git hash printed at the end — save it. You will need it if you ever need to roll back.

**Expected duration:** 5–15 minutes on first run (downloads Go base image). Subsequent runs are faster due to Docker layer cache.

---

## Step 2 — Create the Directory on VPS

The destination folder must exist before `scp` can write to it. Run this once:

```powershell
ssh -i $env:USERPROFILE\.ssh\your_ssh_key user@YOUR_VPS_IP "mkdir -p ~/predsx"
```

---

## Step 3 — Transfer the Package to VPS

From `C:\path\to\PredSX\predsx\`:

```powershell
scp -i $env:USERPROFILE\.ssh\your_ssh_key -r .\deployments\package\ user@YOUR_VPS_IP:~/predsx/
```

The 5 `.tar` files are ~200 MB each (~1 GB total). Transfer time depends on upload speed — typically 3–10 minutes on a home connection.

---

## Step 4 — SSH Into VPS

```powershell
ssh -i $env:USERPROFILE\.ssh\your_ssh_key user@YOUR_VPS_IP
```

Navigate to the package:

```bash
cd ~/predsx/package
ls
```

Expected output:
```
api.tar  discovery.tar  docker-compose.yml  ingestion.tar  processor.tar  storage.tar  .env
```

---

## Step 5 — Install Docker (First Deploy Only)

If Docker is not yet installed on the VPS:

```bash
curl -fsSL https://get.docker.com | sh
```

Add your user to the docker group so you don't need `sudo` on every command:

```bash
sudo usermod -aG docker $USER
newgrp docker
```

Verify Docker is working:

```bash
docker --version
docker compose version
```

---

## Step 6 — Load Docker Images

```bash
ls *.tar | xargs -I {} docker load -i {}
```

Each image prints `Loaded image: predsx-xxx:latest` when done. Takes 1–2 minutes.

Verify all 5 are loaded:

```bash
docker images | grep predsx
```

Expected output (5 rows):
```
predsx-api        latest    abc123   2 minutes ago   25MB
predsx-storage    latest    abc123   2 minutes ago   22MB
predsx-processor  latest    abc123   2 minutes ago   21MB
predsx-ingestion  latest    abc123   2 minutes ago   20MB
predsx-discovery  latest    abc123   2 minutes ago   19MB
```

---

## Step 7 — Configure the Firewall (First Deploy Only)

```bash
sudo ufw allow 22/tcp
sudo ufw allow 8088/tcp
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw enable
```

When prompted `Proceed with operation (y|n)?` — type `y`.

Verify:

```bash
sudo ufw status
```

Expected output:
```
Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
8088/tcp                   ALLOW       Anywhere
22/tcp (v6)                ALLOW       Anywhere (v6)
8088/tcp (v6)              ALLOW       Anywhere (v6)
```

Only port 22 (SSH) and 8088 (API) are open. All infrastructure ports (Kafka 9092, Redis 6379, Postgres 5432, ClickHouse 9000/8123) are internal to Docker and unreachable from outside.

---

## Step 8 — Start the Stack

```bash
cd ~/predsx/package
docker compose up -d
```

Docker will pull the 5 infrastructure images (Kafka, Zookeeper, Redis, Postgres, ClickHouse) on first run — this takes ~30–60 seconds. Your 5 hub images are already loaded from the `.tar` files.

### Startup order

The services start in this order due to healthcheck dependencies:

```
zookeeper (starts immediately)
    │
    └── kafka (waits for zookeeper, then healthcheck: kafka-topics --list)
            │
            └── all 5 hubs (wait for kafka: healthy + redis: healthy + postgres: healthy + clickhouse: healthy)
```

Hubs will not start until all their dependencies pass healthchecks. This eliminates crash-loop restarts.

---

## Step 9 — Verify the Stack

```bash
docker ps --format "table {{.Names}}\t{{.Status}}"
```

Expected output (all 10 rows, all showing `Up` or `healthy`):

```
NAMES                          STATUS
package-hub-api-1              Up 23 seconds (healthy)
package-hub-processor-1        Up 23 seconds (healthy)
package-hub-discovery-1        Up 23 seconds (healthy)
package-hub-ingestion-1        Up 23 seconds (healthy)
package-hub-storage-1          Up 23 seconds (healthy)
package-kafka-1                Up 36 seconds (healthy)
package-postgres-1             Up 36 seconds (healthy)
package-redis-1                Up 36 seconds (healthy)
package-clickhouse-1           Up 36 seconds (healthy)
package-zookeeper-1            Up 36 seconds
```

> **Container naming:** Docker names containers using the compose project name as a prefix. When the compose files live in `~/predsx/package/`, Docker uses `package` as the project name — so all containers are `package-*`, not `predsx-*`.

> Zookeeper has no healthcheck — `Up` without `(healthy)` is correct.

If any hub shows `Up (unhealthy)`, check its logs:

```bash
docker compose logs hub-storage --tail=50
```

---

## Step 10 — Check Data Is Flowing

```bash
# Ingestion: should show WebSocket connections to Polymarket
docker compose logs hub-discovery --tail=30

# Discovery: should show Gamma API pagination
docker compose logs hub-ingestion --tail=30

# Storage: should show schema init and ClickHouse writes
docker compose logs hub-storage --tail=30
```

Healthy ingestion output looks like:
```json
{"time":"...","level":"INFO","msg":"connected to polymarket websocket","channel":"live"}
{"time":"...","level":"INFO","msg":"subscribed to markets","count":247}
```

Healthy storage output looks like:
```json
{"time":"...","level":"INFO","msg":"checking table","name":"events_raw"}
{"time":"...","level":"INFO","msg":"clickhouse connection verified"}
{"time":"...","level":"INFO","msg":"metadata consumer started","group":"predsx-storage-dis"}
```

---

## API Endpoint Verification

All commands below use the VPS public IP. Replace `YOUR_VPS_IP` with your VPS IP if different.

Base URL: `http://YOUR_VPS_IP:8088`

---

### Health Check

```bash
curl http://YOUR_VPS_IP:8088/health
```

**Expected:**
```json
{
  "status": "ok",
  "service": "predsx-api",
  "project": "PredSX Trading Platform",
  "instance": "Production-VPS",
  "timestamp": 1749600000
}
```

This confirms the API container is up and the HTTP server is responding.

---

### Markets — List

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets?limit=5&status=ACTIVE"
```

**Expected:** JSON object with `data` array, `next_cursor`, and `has_more`. If discovery is running, `data` will contain real Polymarket markets within a few minutes of first boot.

```json
{
  "data": [ { "market_id": "0x...", "title": "...", "status": "ACTIVE", ... } ],
  "next_cursor": "NQ==",
  "has_more": true
}
```

If `data` is empty, discovery hasn't indexed markets yet — wait 2–3 minutes and retry.

---

### Markets — Search

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets/search?q=bitcoin&limit=3"
```

**Expected:** Array of markets whose title or question contains "bitcoin". Returns `[]` if no matching markets are indexed yet.

---

### Markets — Top by Volume

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets/top?by=volume&period=24h&limit=5"
```

**Expected:** Array of top 5 markets ranked by 24h volume. Cached for 15 seconds — run twice to confirm `X-Cache: HIT` on the second call:

```bash
curl -I "http://YOUR_VPS_IP:8088/v1/markets/top?by=volume&limit=5"
```

---

### Single Market Detail

Get a real market ID from the list response above, then:

```bash
# Replace MARKET_ID with a real ID from /v1/markets
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID"
```

**Expected:** Full market detail including `stats_24h` inline — no second request needed.

Returns `404 market not found` if the ID isn't indexed yet.

---

### Market Orderbook

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/orderbook"
```

**Expected:** Live orderbook with bids/asks arrays. Returns `404 orderbook not found` if ingestion hasn't cached a snapshot for this market yet — wait a few seconds and retry.

---

### Market Price

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/price"
```

**Expected:**
```json
{
  "market_id": "0x...",
  "price": 0.63,
  "mid_price": 0.635,
  "best_bid": 0.63,
  "best_ask": 0.64,
  "spread": 0.01,
  "volume_24h": 12480000.0,
  "timestamp": 1749599998000
}
```

---

### Market Trades (ClickHouse)

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/trades?limit=10"
```

**Expected:** Array of recent trades from ClickHouse. Empty until ingestion has captured some trade events for this market (~1–5 minutes after first boot on an active market).

---

### Market Candles (OHLCV)

```bash
# 1-minute candles, last 60 minutes
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/candles?resolution=1&limit=60"

# 1-hour candles
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/candles?resolution=60&limit=24"
```

**Expected:** TradingView UDF format — `s`, `t`, `o`, `h`, `l`, `c`, `v` arrays. Returns `{"s":"no_data"}` if not enough candles have accumulated yet.

---

### Market Stats (24h OHLCV summary)

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/stats"
```

**Expected:**
```json
{
  "market_id": "0x...",
  "volume_24h": 12480000.0,
  "trades_24h": 4820,
  "open_price": 0.52,
  "close_price": 0.63,
  "low_24h": 0.48,
  "high_24h": 0.65,
  "price_change_pct": 21.15,
  "timestamp": 1749600000000
}
```

---

### Market Price History

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/price-history?resolution=1m&limit=10"
```

**Expected:** Array of time-bucketed rows with `timestamp`, `trade_count`, `volume`, `avg_price`.

---

### Market Summary

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/summary"
```

**Expected:** Current price, orderbook snapshot, and recent signals in one call. Cached for 10 seconds.

---

### Market Signals

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/signals"

# Filter by type
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/signals?type=momentum"
```

**Expected:** Array of signal objects. Returns `[]` if the processor hub hasn't generated signals for this market yet.

---

### Related Markets

```bash
curl "http://YOUR_VPS_IP:8088/v1/markets/MARKET_ID/related?limit=5"
```

**Expected:** Other markets belonging to the same event. Returns `[]` if the market has no `event_id`.

---

### Batch Prices

```bash
# Get prices for multiple markets in one call (replace with real IDs)
curl "http://YOUR_VPS_IP:8088/v1/prices?ids=MARKET_ID_1,MARKET_ID_2"
```

**Expected:** Map of market ID → price object. Unknown IDs return `null`.

---

### Events (Polymarket passthrough)

```bash
curl "http://YOUR_VPS_IP:8088/v1/events?exchange=polymarket&limit=5"
```

**Expected:** Proxied response from Polymarket Gamma API. Requires outbound internet access from the VPS.

---

### Positions (Polymarket passthrough)

```bash
curl "http://YOUR_VPS_IP:8088/v1/positions?wallet=0xYourWalletAddress"
curl "http://YOUR_VPS_IP:8088/v1/positions/closed?wallet=0xYourWalletAddress"
```

**Expected:** Proxied response from Polymarket Data API. Returns `400 wallet is required` if `wallet` param is omitted.

---

### Rate Limit Test

```bash
# Run 65 requests in a loop to trigger the rate limiter (60 req/min limit)
for i in $(seq 1 65); do curl -s -o /dev/null -w "%{http_code}\n" "http://YOUR_VPS_IP:8088/v1/markets"; done
```

**Expected:** First 60 return `200`, then `429`. Response headers include `X-RateLimit-Limit: 60` and `X-RateLimit-Remaining: N`.

---

### Debug Endpoints

Debug endpoints require the `DEBUG_TOKEN` env var set in the compose file. If not set, all debug routes return `403 debug endpoints disabled`.

```bash
# If DEBUG_TOKEN is set
curl -H "X-Debug-Token: your-secret-token" http://YOUR_VPS_IP:8088/debug/markets
curl -H "X-Debug-Token: your-secret-token" http://YOUR_VPS_IP:8088/debug/trades
curl -H "X-Debug-Token: your-secret-token" http://YOUR_VPS_IP:8088/debug/signals
```

---

### WebSocket Stream (from local machine)

Install `websocat` or use any WebSocket client:

```bash
# Using websocat (install: cargo install websocat)
websocat ws://YOUR_VPS_IP:8088/stream

# Subscribe to specific markets
echo '{"action":"subscribe","markets":["MARKET_ID"]}' | websocat ws://YOUR_VPS_IP:8088/stream
```

**Expected:** Stream of JSON events — `type: trade`, `type: orderbook`, `type: price`, `type: signal` — in real time as Polymarket data flows in.

---

## Ongoing Operations

### View live logs

```bash
# All services together
docker compose logs -f --tail=30

# Single service
docker compose logs -f hub-ingestion
docker compose logs -f hub-storage
```

### Monitor RAM and CPU

```bash
docker stats --no-stream
```

### Check ClickHouse table sizes

```bash
docker exec -it package-clickhouse-1 clickhouse-client --query \
  "SELECT table, formatReadableSize(sum(bytes)) AS size FROM system.parts WHERE active GROUP BY table ORDER BY sum(bytes) DESC"
```

> **ClickHouse database name:** All tables live in the `default` database (not `predsx`). Always use `default.market_metadata`, `default.events_raw`, etc. in manual queries.

### Check market status counts in ClickHouse

```bash
docker exec -it package-clickhouse-1 clickhouse-client --query "SELECT status, count() FROM default.market_metadata GROUP BY status"
```

### Full stack health check (one command)

```bash
echo "=== CONTAINERS ===" && docker ps --format "table {{.Names}}\t{{.Status}}" && echo "" && echo "=== CLICKHOUSE MARKETS ===" && docker exec -it package-clickhouse-1 clickhouse-client --query "SELECT status, count() FROM default.market_metadata GROUP BY status" && echo "" && echo "=== API HEALTH ===" && curl -s http://YOUR_VPS_IP:8088/health && echo "" && echo "" && echo "=== MARKETS ENDPOINT ===" && curl -s "http://YOUR_VPS_IP:8088/v1/markets?limit=2&status=ACTIVE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'data count: {len(d[\"data\"])}, has_more: {d[\"has_more\"]}')"
```

### Restart a single service (without touching volumes)

> **Important:** `docker compose restart` only sends a restart signal — it does **not** pick up a newly loaded image. When you load a new image and want the container to use it, always use `up -d --no-deps`:

```bash
# Restart with new image (use this after docker load)
docker compose up -d --no-deps hub-storage

# Restart without image change (config-only change)
docker compose restart hub-storage
```

### Full restart (keeps all data)

```bash
docker compose down
docker compose up -d
```

**Never run `docker compose down -v`** — the `-v` flag deletes all named volumes, which permanently destroys Kafka offsets, Postgres backfill state, and all ClickHouse data.

---

## Deploying an Update

1. Make changes on your dev machine
2. Commit and run `.\build-and-ship.bat` — note the new git hash
3. `scp` the new `deployments/package/` to the VPS (same command as Step 3)
4. On VPS:

```bash
cd ~/predsx/package
ls *.tar | xargs -I {} docker load -i {}
docker compose down
docker compose up -d
```

### Required Environment Variables

These must be set in `deployments/.env` before deploying. The table below covers variables added during the security hardening and event discovery phases:

| Variable | Example | Required | Description |
|----------|---------|----------|-------------|
| `ALLOWED_ORIGINS` | `https://app.predsx.com` | Yes | Comma-separated allowed CORS origins. Use `*` only for fully public APIs. |
| `TRUSTED_PROXIES` | `10.0.0.1/24` | Only behind a proxy | CIDR/IP of your load balancer. When set, `X-Forwarded-For` is trusted for rate limiting. Omit if the API is directly internet-facing. |
| `DEBUG_TOKEN` | `your-secret-token` | Recommended | Enables `/debug/*` and `/metrics`. Without it, those routes return `403`. |
| `GAMMA_EVENTS_URL` | `https://gamma-api.polymarket.com/events` | No | Override Gamma events URL. Default is the Polymarket production endpoint. |

```env
# deployments/.env — add these lines
ALLOWED_ORIGINS=https://app.predsx.com
DEBUG_TOKEN=your-secret-token
# TRUSTED_PROXIES=10.0.0.1/24   # uncomment if behind nginx/caddy
```

---

### Deploying a single service (faster — no downtime on other services)

If only one service changed, rebuild and ship just that image:

```powershell
# Local — rebuild only the changed service
docker build -t predsx-discovery:latest -f services/discovery/Dockerfile .
docker save predsx-discovery:latest -o deployments\package\discovery.tar
scp -i $env:USERPROFILE\.ssh\your_ssh_key .\deployments\package\discovery.tar user@YOUR_VPS_IP:~/predsx/package/
```

```bash
# VPS — load and recreate only that container
docker load -i ~/predsx/package/discovery.tar
cd ~/predsx/package
docker compose up -d --no-deps hub-discovery
```

> **Why `--no-deps`:** Without it, `docker compose up -d hub-discovery` also restarts all services that depend on it. `--no-deps` isolates the change to just the one container.

### Rolling back to a previous version

Each `build-and-ship.bat` run tags images with both `:latest` and `:<git-hash>`. To roll back:

```bash
# On VPS — stop the running stack
docker compose down

# Edit docker-compose.yml to replace :latest with the old hash
sed -i 's/:latest/:cc27bce/g' docker-compose.yml

# Start on old images
docker compose up -d

# To undo the rollback later — restore :latest tags
sed -i 's/:cc27bce/:latest/g' docker-compose.yml
docker compose down && docker compose up -d
```

The old hash images are already loaded on the VPS — no retransfer needed.

---

## SSH Tunnels for Local DB Access

Infrastructure ports are not exposed publicly. Use SSH tunnels to connect local tools (TablePlus, DBeaver, redis-cli) to the VPS databases:

```powershell
# Open all tunnels at once (run from local machine, keep terminal open)
ssh -i $env:USERPROFILE\.ssh\your_ssh_key `
    -L 5432:localhost:5432 `
    -L 6379:localhost:6379 `
    -L 8123:localhost:8123 `
    -L 9000:localhost:9000 `
    user@YOUR_VPS_IP
```

Then connect:

| Tool | Host | Port |
|---|---|---|
| TablePlus (Postgres) | localhost | 5432 |
| redis-cli | localhost | 6379 |
| DBeaver / ClickHouse HTTP | localhost | 8123 |
| clickhouse-client | localhost | 9000 |

Close the SSH session when done — all tunnels close automatically.

---

## Related Docs

- [port-security.md](port-security.md) — Port bindings, UFW rules, TLS/Caddy setup
- [api-reference.md](api-reference.md) — Full API endpoint reference with all response schemas
- [storage-projections.md](storage-projections.md) — Disk and RAM capacity planning
- [info.md](info.md) — Full project history and development log
