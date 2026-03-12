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
