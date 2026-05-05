CREATE TABLE IF NOT EXISTS events_raw
(
 event_id String,
 type LowCardinality(String),
 market_id String,
 timestamp DateTime64(3),
 data String,
 metadata String
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (market_id, timestamp)
TTL timestamp + INTERVAL 3 DAY;

CREATE TABLE IF NOT EXISTS trades
(
 market_id String,
 token_id String,
 price Float64,
 size Float64,
 timestamp DateTime64(3)
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (market_id, timestamp);

CREATE TABLE IF NOT EXISTS market_metrics
(
    market_id String,
    volume Float64,
    trades UInt64,
    timestamp DateTime64
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (market_id, timestamp);

CREATE TABLE IF NOT EXISTS market_metadata
(
    market_id String,
    slug String,
    title String,
    question String,
    status String,
    exchange String,
    event_id String,
    start_time DateTime64(3),
    end_time DateTime64(3),
    outcomes String,
    raw String,
    created_at DateTime64(3) DEFAULT now64(3)
)
ENGINE = MergeTree()
ORDER BY (market_id, created_at);

CREATE TABLE IF NOT EXISTS price_history_1m
(
    market_id String,
    timestamp DateTime64,
    trade_count UInt32,
    volume Float64,
    avg_price Float64
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (market_id, timestamp);

CREATE TABLE IF NOT EXISTS price_history_5m
(
    market_id String,
    timestamp DateTime64,
    trade_count UInt32,
    volume Float64,
    avg_price Float64
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (market_id, timestamp);

CREATE TABLE IF NOT EXISTS price_history_1h
(
    market_id String,
    timestamp DateTime64,
    trade_count UInt32,
    volume Float64,
    avg_price Float64
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (market_id, timestamp);
