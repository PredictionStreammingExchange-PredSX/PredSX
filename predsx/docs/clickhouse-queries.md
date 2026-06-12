# PredSX — ClickHouse Query Guide

> SQL reference for querying PredSX data directly from ClickHouse.
> Useful for debugging, analytics, and building ML/signal features.

All tables live in the **`default`** database. All timestamps are stored as `DateTime64(3)` (millisecond precision) or `DateTime`.

---

## Connecting

**Inside the VPS (via Docker exec):**
```bash
docker exec -it package-clickhouse-1 clickhouse-client --database default
```

**From local machine (via SSH tunnel):**
```powershell
# Open tunnel — keep this terminal open
ssh -i $env:USERPROFILE\.ssh\your_ssh_key -L 8123:localhost:8123 -L 9000:localhost:9000 user@YOUR_VPS_IP
```

Then connect with any ClickHouse client:
```bash
clickhouse-client --host localhost --port 9000 --database default
# or via HTTP API
curl "http://localhost:8123/?query=SELECT+1"
```

---

## Table Overview

```sql
SHOW TABLES FROM default;
```

| Table | Description | Retention |
|-------|-------------|-----------|
| `market_metadata` | Market registry — slug, title, status, event_id, condition_id | Permanent |
| `trades` | Normalized trade events from Polymarket WebSocket | 7 days |
| `events_raw` | Raw WebSocket payloads (unprocessed) | 3 days |
| `orderbook_history` | Orderbook snapshots | 2 days |
| `onchain_trades` | On-chain Polygon RPC trade events | 14 days |
| `market_metrics` | Per-market aggregated metrics (1s buckets) | 2 days |
| `price_history_1m` | 1-minute OHLCV candles (SummingMergeTree) | Permanent |
| `price_history_1h` | 1-hour OHLCV candles (SummingMergeTree) | Permanent |

---

## Market Metadata

### All markets with their current status
```sql
SELECT market_id, slug, status, event_id
FROM default.market_metadata
ORDER BY created_at DESC
LIMIT 20;
```

### Count markets by status
```sql
SELECT status, count() AS total
FROM default.market_metadata
GROUP BY status
ORDER BY total DESC;
```

### Markets with event_id populated (multi-outcome groups)
```sql
SELECT market_id, title, event_id
FROM default.market_metadata
WHERE event_id != ''
ORDER BY created_at DESC
LIMIT 20;
```

### Find a specific market by title keyword
```sql
SELECT market_id, title, status, event_id
FROM default.market_metadata
WHERE lower(title) LIKE '%bitcoin%'
ORDER BY created_at DESC
LIMIT 10;
```

### Get sibling markets (same event group)
```sql
-- Replace '30615' with an actual event_id
SELECT market_id, title, status
FROM default.market_metadata
WHERE event_id = '30615'
ORDER BY created_at DESC;
```

### Most recently discovered markets
```sql
SELECT market_id, title, status, created_at
FROM default.market_metadata
ORDER BY created_at DESC
LIMIT 10;
```

---

## Trades

### Recent trades across all markets
```sql
SELECT trade_id, market_id, price, size, side, timestamp
FROM default.trades
ORDER BY timestamp DESC
LIMIT 20;
```

### Recent trades for a specific market
```sql
-- Replace '558935' with a real market_id
SELECT trade_id, price, size, side, timestamp
FROM default.trades
WHERE market_id = '558935'
ORDER BY timestamp DESC
LIMIT 50;
```

### Trade volume by market (last 24 hours)
```sql
SELECT
    market_id,
    count() AS trade_count,
    sum(size) AS total_volume,
    avg(price) AS avg_price
FROM default.trades
WHERE timestamp >= now() - INTERVAL 24 HOUR
GROUP BY market_id
ORDER BY total_volume DESC
LIMIT 20;
```

### Price range for a market over last 24 hours
```sql
SELECT
    min(price) AS low,
    max(price) AS high,
    avg(price) AS avg,
    argMin(price, timestamp) AS open,
    argMax(price, timestamp) AS close
FROM default.trades
WHERE market_id = '558935'
  AND timestamp >= now() - INTERVAL 24 HOUR;
```

### Trade count per hour (activity heatmap)
```sql
SELECT
    toStartOfHour(timestamp) AS hour,
    count() AS trades,
    sum(size) AS volume
FROM default.trades
WHERE market_id = '558935'
  AND timestamp >= now() - INTERVAL 7 DAY
GROUP BY hour
ORDER BY hour DESC;
```

---

## OHLCV Candles

### Last 60 one-minute candles for a market
```sql
SELECT
    timestamp,
    open,
    high,
    low,
    close,
    volume,
    trade_count
FROM default.price_history_1m
WHERE market_id = '558935'
ORDER BY timestamp DESC
LIMIT 60;
```

### Last 24 one-hour candles for a market
```sql
SELECT
    timestamp,
    open,
    high,
    low,
    close,
    volume
FROM default.price_history_1h
WHERE market_id = '558935'
ORDER BY timestamp DESC
LIMIT 24;
```

### Price change over the last 7 days (daily OHLCV)
```sql
SELECT
    toStartOfDay(timestamp) AS day,
    argMin(open, timestamp) AS open,
    max(high) AS high,
    min(low) AS low,
    argMax(close, timestamp) AS close,
    sum(volume) AS volume,
    sum(trade_count) AS trades
FROM default.price_history_1h
WHERE market_id = '558935'
  AND timestamp >= now() - INTERVAL 7 DAY
GROUP BY day
ORDER BY day DESC;
```

### Markets with the most candles (most traded)
```sql
SELECT market_id, count() AS candle_count
FROM default.price_history_1m
GROUP BY market_id
ORDER BY candle_count DESC
LIMIT 10;
```

---

## Market Metrics (1-second buckets)

The `market_metrics` table stores real-time aggregated metrics at 1-second granularity.

### Latest metrics for a market
```sql
SELECT *
FROM default.market_metrics
WHERE market_id = '558935'
ORDER BY timestamp DESC
LIMIT 10;
```

### Volume in the last 5 minutes
```sql
SELECT
    sum(volume) AS total_volume,
    sum(trade_count) AS total_trades,
    avg(mid_price) AS avg_price
FROM default.market_metrics
WHERE market_id = '558935'
  AND timestamp >= now() - INTERVAL 5 MINUTE;
```

---

## On-Chain Trades

### Recent on-chain trade events
```sql
SELECT *
FROM default.onchain_trades
ORDER BY timestamp DESC
LIMIT 20;
```

### On-chain volume by market
```sql
SELECT
    market_id,
    count() AS tx_count,
    sum(size) AS onchain_volume
FROM default.onchain_trades
WHERE timestamp >= now() - INTERVAL 24 HOUR
GROUP BY market_id
ORDER BY onchain_volume DESC
LIMIT 10;
```

---

## Storage & System

### Table sizes on disk
```sql
SELECT
    table,
    formatReadableSize(sum(bytes)) AS size,
    count() AS parts,
    sum(rows) AS row_count
FROM system.parts
WHERE active AND database = 'default'
GROUP BY table
ORDER BY sum(bytes) DESC;
```

### Check TTL settings for each table
```sql
SELECT name, engine, ttl
FROM system.tables
WHERE database = 'default'
ORDER BY name;
```

### Check row counts for all tables
```sql
SELECT
    table,
    sum(rows) AS total_rows
FROM system.parts
WHERE active AND database = 'default'
GROUP BY table
ORDER BY total_rows DESC;
```

### Current active merges (check if background work is happening)
```sql
SELECT table, elapsed, progress, rows_read, rows_written
FROM system.merges
WHERE database = 'default';
```

---

## ML / Analytics Patterns

### Build a feature dataset: price + volume per 5-minute bucket
```sql
SELECT
    toStartOfInterval(timestamp, INTERVAL 5 MINUTE) AS bucket,
    market_id,
    sum(volume) AS volume_5m,
    sum(trade_count) AS trades_5m,
    avg(close) AS avg_close,
    max(high) - min(low) AS price_range_5m
FROM default.price_history_1m
WHERE market_id = '558935'
  AND timestamp >= now() - INTERVAL 30 DAY
GROUP BY bucket, market_id
ORDER BY bucket ASC;
```

### Price momentum — compare last 1h vs previous 1h
```sql
WITH
    recent AS (
        SELECT avg(close) AS price
        FROM default.price_history_1h
        WHERE market_id = '558935'
          AND timestamp >= now() - INTERVAL 1 HOUR
    ),
    previous AS (
        SELECT avg(close) AS price
        FROM default.price_history_1h
        WHERE market_id = '558935'
          AND timestamp BETWEEN now() - INTERVAL 2 HOUR AND now() - INTERVAL 1 HOUR
    )
SELECT
    recent.price AS recent_price,
    previous.price AS prev_price,
    round((recent.price - previous.price) / previous.price * 100, 2) AS pct_change
FROM recent, previous;
```

### Find markets with unusual volume spike (>3x their 7-day average)
```sql
WITH avg_vol AS (
    SELECT market_id, avg(volume) AS avg_1h_volume
    FROM default.price_history_1h
    WHERE timestamp BETWEEN now() - INTERVAL 7 DAY AND now() - INTERVAL 1 HOUR
    GROUP BY market_id
),
recent_vol AS (
    SELECT market_id, sum(volume) AS last_1h_volume
    FROM default.price_history_1h
    WHERE timestamp >= now() - INTERVAL 1 HOUR
    GROUP BY market_id
)
SELECT
    r.market_id,
    r.last_1h_volume,
    a.avg_1h_volume,
    round(r.last_1h_volume / a.avg_1h_volume, 2) AS volume_ratio
FROM recent_vol r
JOIN avg_vol a ON r.market_id = a.market_id
WHERE r.last_1h_volume > a.avg_1h_volume * 3
ORDER BY volume_ratio DESC
LIMIT 20;
```

### Export market candles to CSV (for Python/pandas)
```bash
docker exec -it package-clickhouse-1 clickhouse-client \
  --query "SELECT timestamp, open, high, low, close, volume FROM default.price_history_1m WHERE market_id = '558935' ORDER BY timestamp ASC FORMAT CSV" \
  > market_558935_candles.csv
```

---

## Useful ClickHouse Functions

| Function | Example | Description |
|----------|---------|-------------|
| `toStartOfHour(ts)` | `toStartOfHour(timestamp)` | Truncate to hour |
| `toStartOfDay(ts)` | `toStartOfDay(timestamp)` | Truncate to day |
| `toStartOfInterval(ts, INTERVAL N MINUTE)` | `toStartOfInterval(timestamp, INTERVAL 5 MINUTE)` | N-minute buckets |
| `argMin(col, ts)` | `argMin(price, timestamp)` | Value at earliest timestamp (open price) |
| `argMax(col, ts)` | `argMax(price, timestamp)` | Value at latest timestamp (close price) |
| `formatReadableSize(bytes)` | `formatReadableSize(sum(bytes))` | Human-readable file size |
| `now()` | `timestamp >= now() - INTERVAL 1 HOUR` | Current server time |
