# PredSX — Storage & Ingestion Projections

> Capacity planning reference for a 4 GB RAM / 30 GB disk VPS running the full PredSX stack.

---

## How Data Flows Into Storage

```
Polymarket WebSocket (4 connections)
         │
         ▼
   Ingestion Hub  ──► Kafka (2-hour transit pipe)
         │
         ▼
   Storage Hub
         │
         ├──► events_raw          (raw WS payloads — 3 day TTL)
         ├──► trades              (normalized trades — 7 day TTL)
         ├──► orderbook_history   (L2 snapshots — 2 day TTL)
         ├──► market_metrics      (per-market aggregates — 2 day TTL)
         ├──► market_metadata     (market registry — permanent)
         ├──► price_history_1m    (1-min OHLCV candles — permanent)
         └──► price_history_1h    (1-hour OHLCV candles — permanent)

Polygon RPC (on-chain)
         │
         ▼
   Ingestion Hub ──► onchain_trades (14 day TTL)
```

---

## ClickHouse Table Reference

### TTL-Capped Tables (reach steady state, never grow past it)

#### `events_raw` — 3 day TTL

Raw WebSocket payloads before normalization. Every event type lands here first.

| Metric | Estimate |
|---|---|
| Write rate | ~200 events/sec across all markets |
| Row size (compressed) | ~100–150 bytes |
| Daily ingest | ~1.7 GB raw → ~200–400 MB compressed |
| **Steady state** | **~600 MB – 1.2 GB** |

> Highest write frequency table. Useful for debugging and replaying events but not for analytics.

---

#### `orderbook_history` — 2 day TTL

L2 orderbook snapshots captured on every orderbook update. Each row stores the full bids/asks JSON arrays.

| Metric | Estimate |
|---|---|
| Write rate | ~50 snapshots/sec (active markets) |
| Row size (compressed) | ~300–500 bytes (bids + asks JSON) |
| Daily ingest | ~1.3–2.2 GB raw → ~500–800 MB compressed |
| **Steady state** | **~1.0 – 1.6 GB** |

> Second-highest volume table. JSON columns compress poorly compared to numeric columns — this is the main TTL-capped contributor to disk use.

---

#### `trades` — 7 day TTL

Normalized, deduplicated trade events. Source for all price calculations and signals.

| Metric | Estimate |
|---|---|
| Write rate | ~5–20 trades/sec (depends on market activity) |
| Row size (compressed) | ~40–60 bytes |
| Daily ingest | ~25–100 MB raw → ~10–30 MB compressed |
| **Steady state** | **~70 – 210 MB** |

---

#### `market_metrics` — 2 day TTL

Aggregated per-market stats (trade count, volume, avg price) written by the processor hub.

| Metric | Estimate |
|---|---|
| Write rate | low — triggered by trade events |
| Row size (compressed) | ~30 bytes |
| Daily ingest | ~50–100 MB |
| **Steady state** | **~100 – 200 MB** |

---

#### `onchain_trades` — 14 day TTL

On-chain Polygon trade events fetched via RPC. Lower frequency than WebSocket trades.

| Metric | Estimate |
|---|---|
| Write rate | ~1–5 events/sec |
| Row size (compressed) | ~80 bytes |
| **Steady state** | **~50 – 150 MB** |

---

### Permanent Tables (grow indefinitely)

#### `price_history_1m` ⚠️ Primary disk growth driver

1-minute OHLCV candles built automatically by a materialized view on `trades`.

| Metric | Estimate |
|---|---|
| Active markets generating candles | ~100–500 at any time |
| Rows per day | 100–500 markets × 1440 min = 144k–720k rows/day |
| Row size (compressed) | ~40 bytes |
| **Daily growth** | **~6 – 30 MB/day** |
| After 6 months | ~1 – 5 GB |
| After 12 months | ~2 – 11 GB |
| After 24 months | ~4 – 22 GB |

---

#### `price_history_1h`

1-hour OHLCV candles. 60× fewer rows than `price_history_1m`.

| Metric | Estimate |
|---|---|
| Rows per day | ~2.4k – 12k rows/day |
| **Daily growth** | **~0.1 – 0.5 MB/day** |
| After 24 months | ~70 – 350 MB |

---

#### `market_metadata`

One row per Polymarket market. Grows only as new markets open.

| Metric | Estimate |
|---|---|
| Total Polymarket markets | ~10,000–50,000 lifetime |
| Row size | ~500 bytes |
| **Total size** | **~5 – 25 MB** (effectively flat) |

---

## Full Stack Disk Budget (30 GB VPS)

```
30 GB total
├──  5 GB   OS + Docker engine + images (5 images × ~200 MB each)
├──  2 GB   Kafka data + Zookeeper (capped by 2-hour retention + 1 GB limit)
├──  0.5 GB Postgres (backfill offsets — tiny)
├──  0.3 GB Docker container logs (capped at 10 MB × 3 files × 10 services)
│
└── ~22 GB  ClickHouse
    ├──  2–4 GB   TTL-capped tables (steady state, never exceeds this)
    └── ~18 GB    Available for permanent tables (price_history_1m/1h)
```

### When does 30 GB fill up?

| Activity level | `price_history_1m` growth | 18 GB fills in |
|---|---|---|
| Quiet (100 active markets) | ~6 MB/day | ~8 years |
| Normal (250 active markets) | ~15 MB/day | ~3 years |
| Active (500 active markets) | ~30 MB/day | ~18 months |
| Peak (1000+ markets, high volume) | ~60 MB/day | ~9 months |

**Expected for a live Polymarket setup: 18–36 months before disk action is needed.**

---

## RAM Budget (4 GB VPS)

| Service | Memory limit | Notes |
|---|---|---|
| ClickHouse | 1 GB | Largest consumer — query cache + merge buffers |
| Kafka | 512 MB | JVM heap fixed at 256M + overhead |
| Postgres | 256 MB | Mostly idle (backfill offsets only) |
| Redis | 100 MB | 64 MB data cap + overhead |
| Zookeeper | 128 MB | |
| hub-processor | 400 MB | Holds live orderbook state in memory |
| hub-ingestion | 256 MB | 4 WS connections + RPC pool |
| hub-storage | 256 MB | Batch flush buffers |
| hub-discovery | 200 MB | |
| hub-api | 256 MB | Redis-backed response cache |
| OS + Docker | ~400 MB | Kernel, networking, Docker daemon |
| **Total** | **~3.8 GB** | Tight but within limits |

> RAM is the tighter constraint on a 4 GB VPS. If any service hits its limit, Docker kills it and `restart: always` brings it back. Monitor with `docker stats`.

---

## Monitoring Commands

```bash
# ClickHouse table sizes
docker exec -it <clickhouse-container> clickhouse-client --query \
  "SELECT table, formatReadableSize(sum(bytes)) AS size, count() AS parts
   FROM system.parts WHERE active GROUP BY table ORDER BY sum(bytes) DESC"

# Overall disk usage
df -h /var/lib/docker

# Docker volume sizes
du -sh /var/lib/docker/volumes/*

# Live RAM per container
docker stats --no-stream
```

---

## If Disk Starts Running Low

**Option 1 — Add TTL to `price_history_1m` (keep last 90 days of minute candles)**
```sql
ALTER TABLE price_history_1m MODIFY TTL timestamp + INTERVAL 90 DAY;
```
The 1-hour candles (`price_history_1h`) still cover everything older for backtesting. This single change gives years of additional headroom.

**Option 2 — Shorten `events_raw` TTL (from 3 days to 1 day)**
```sql
ALTER TABLE events_raw MODIFY TTL timestamp + INTERVAL 1 DAY;
```
Saves ~400–800 MB immediately. Fine to do — raw events are only useful for short-term debugging.

**Option 3 — Shorten `orderbook_history` TTL (from 2 days to 12 hours)**
```sql
ALTER TABLE orderbook_history MODIFY TTL timestamp + INTERVAL 12 HOUR;
```
Saves ~500–800 MB immediately. Real-time signals only need recent orderbook snapshots.

---

## Related Docs

- [info.md](info.md) — Full project reference and development history
- [port-security.md](port-security.md) — Port bindings, networking, and TLS setup
- [api-reference.md](api-reference.md) — API endpoint reference
