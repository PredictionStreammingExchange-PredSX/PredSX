package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	clickhouse "github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	"github.com/predsx/predsx/libs/crypto"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	redisclient "github.com/predsx/predsx/libs/redis-client"
	retry "github.com/predsx/predsx/libs/retry-utils"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

type Normalizer struct {
	svc           *service.BaseService
	redis         redisclient.Interface
	clickhouse    clickhouse.Interface
	batchSize     int
	flushInterval time.Duration
	buffer        []schemas.NormalizedEvent
	mu            sync.Mutex
	producer      *kafkaclient.TypedProducer[schemas.NormalizedEvent]
}

func main() {
	svc := service.NewBaseService("normalizer")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Config
		kafkaBrokers  := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		redisAddr     := config.GetEnv("REDIS_ADDR", "localhost:6379")
		chAddr        := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser        := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword    := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase    := config.GetEnv("CLICKHOUSE_DB", "default")
		batchSize     := config.GetEnvInt("BATCH_SIZE", 500)
		flushInterval := time.Duration(config.GetEnvInt("FLUSH_INTERVAL_MS", 100)) * time.Millisecond

		// Clients
		rdb := redisclient.NewClient(redisclient.Options{Addr: redisAddr}, svc.Logger)

		ch, err := clickhouse.NewClient(clickhouse.Options{
			Addr:     chAddr,
			User:     chUser,
			Password: chPassword,
			Database: chDatabase,
			MaxConns: 10,
		}, svc.Logger)
		if err != nil {
			return fmt.Errorf("failed to connect to clickhouse: %w", err)
		}

		// Auto-migrate: create events table with optimized schema
		if err := ensureSchema(ctx, ch); err != nil {
			return fmt.Errorf("failed to ensure clickhouse schema: %w", err)
		}
		svc.Logger.Info("clickhouse schema verified")

		n := &Normalizer{
			svc:           svc,
			redis:         rdb,
			clickhouse:    ch,
			batchSize:     batchSize,
			flushInterval: flushInterval,
			buffer:        make([]schemas.NormalizedEvent, 0, batchSize),
			producer:      kafkaclient.NewTypedProducer[schemas.NormalizedEvent]([]string{kafkaBrokers}, "predsx.trades.live", svc.Logger),
		}
		defer n.producer.Close()

		go n.flushLoop(ctx)

		// Topics to consume
		topics := []string{
			"predsx.ws.raw",
			"predsx.trades.backfill",
		}

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			"predsx.ws.raw":          6,
			"predsx.trades.backfill": 3,
			"predsx.trades.live":     6,
		}, svc.Logger)

		var wg sync.WaitGroup
		for _, topic := range topics {
			wg.Add(1)
			go func(t string) {
				defer wg.Done()
				n.consumeTopic(ctx, kafkaBrokers, t)
			}(topic)
		}

		wg.Wait()
		return nil
	})
}

// ensureSchema creates the events_raw table if it does not exist, using the
// Level-10 architecture schema.
func ensureSchema(ctx context.Context, ch clickhouse.Interface) error {
	err := ch.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events_raw (
			event_id    String,
			type        LowCardinality(String),
			market_id   String,
			timestamp   DateTime64(3),
			data        String,
			metadata    String
		) ENGINE = MergeTree()
		PARTITION BY toDate(timestamp)
		ORDER BY (market_id, timestamp)
		TTL timestamp + INTERVAL 3 DAY;
	`)
	
	if err != nil {
		return err
	}
	
	// Ensure TTL is applied for existing tables
	return ch.Exec(ctx, `
		ALTER TABLE events_raw MODIFY TTL timestamp + INTERVAL 3 DAY;
	`)
}

func (n *Normalizer) consumeTopic(ctx context.Context, brokers, topic string) {
	groupID := config.GetEnv("KAFKA_GROUP_ID", "predsx-normalizer")
	consumer := kafkaclient.NewTypedConsumer[map[string]interface{}]([]string{brokers}, topic, groupID, n.svc.Logger)
	defer consumer.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := consumer.Fetch(ctx)
			if err != nil {
				n.svc.Logger.Error("fetch error", "topic", topic, "error", err)
				continue
			}
			n.processEvent(ctx, topic, msg)
		}
	}
}

func (n *Normalizer) processEvent(ctx context.Context, topic string, raw map[string]interface{}) {
	marketID, _ := raw["market_id"].(string)
	if marketID == "" {
		return
	}

	// 1. Determine Event Identity
	// If the raw event already has an event_id (e.g., from an upstream component) we could use it, 
	// but we strictly compute the SHA1 hash based on Level-10 spec:
	// event_id = SHA1(market_id + timestamp + price + size + side)
	
	// Safely extract fields for hash generation
	ts := time.Now().UTC()
	if rawTsStr, ok := raw["timestamp"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, rawTsStr); err == nil {
			ts = parsed
		}
	} else if rawTsFloat, ok := raw["timestamp"].(float64); ok { // sometimes JSON parses as float Unix
		ts = time.Unix(int64(rawTsFloat/1000), 0)
	}
	tsStr := fmt.Sprintf("%d", ts.UnixNano())
	
	priceStr := fmt.Sprintf("%v", raw["price"])
	sizeStr := fmt.Sprintf("%v", raw["size"])
	sideStr := fmt.Sprintf("%v", raw["side"])

	eventID := crypto.GenerateEventID(marketID, tsStr, priceStr, sizeStr, sideStr)

	// 2. Redis Deduplication 
	// SETNX predsx:dedup:{event_id} "1" EX 48h
	dedupKey := fmt.Sprintf("predsx:dedup:%s", eventID)
	set, err := n.redis.SetNX(ctx, dedupKey, "1", 48*time.Hour).Result()
	if err != nil {
		n.svc.Logger.Warn("deduplication check error", "error", err, "event_id", eventID)
		// Continue processing safely even if Redis goes down briefly
	} else if !set {
		// Event ID exists => duplicate, skip processing entirely
		return
	}

	// 3. Enrich with metadata from Redis
	metadata := make(map[string]string)
	val, err := n.redis.Get(ctx, fmt.Sprintf("market:%s:metadata", marketID)).Result()
	if err == nil {
		json.Unmarshal([]byte(val), &metadata)
	}

	// Propagate source tag (e.g. "backfill") if present in the event
	if src, ok := raw["source"].(string); ok && src != "" {
		metadata["source"] = src
	}

	// 4. Normalize
	data, _ := json.Marshal(raw)
	event := schemas.NormalizedEvent{
		EventID:   eventID,
		EventType: topic,
		MarketID:  marketID,
		Data:      data,
		Metadata:  metadata,
		Timestamp: ts,
		Version:   schemas.VersionV1,
	}

	// 5. Publish strictly verified, uniquely processed trades into the Kafka topic predsx.trades.live
	if err := n.producer.Publish(ctx, event.MarketID, event); err != nil {
		n.svc.Logger.Error("failed to publish verified live trade", "event_id", eventID, "error", err)
	}

	// 6. Update Redis Live State (e.g., last price)
	if topic == "predsx.prices" {
		n.redis.Set(ctx, fmt.Sprintf("live:price:%s", marketID), data, 0)
	}

	// 7. Buffer for ClickHouse
	n.mu.Lock()
	n.buffer = append(n.buffer, event)
	if len(n.buffer) >= n.batchSize {
		n.flush(ctx)
	}
	n.mu.Unlock()
}

func (n *Normalizer) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(n.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.mu.Lock()
			n.flush(ctx)
			n.mu.Unlock()
		}
	}
}

// flush writes the buffered events to ClickHouse using PrepareBatch for
// efficient bulk ingestion. ClickHouse is optimised for large batch inserts;
// per-row inserts would cause merge storms under any real load.
func (n *Normalizer) flush(ctx context.Context) {
	if len(n.buffer) == 0 {
		return
	}

	batch := n.buffer
	n.buffer = make([]schemas.NormalizedEvent, 0, n.batchSize)

	start := time.Now()

	err := retry.Do(ctx, func() error {
		b, err := n.clickhouse.PrepareBatch(ctx,
			"INSERT INTO events_raw (event_id, type, market_id, data, metadata, timestamp)")
		if err != nil {
			return fmt.Errorf("prepare batch: %w", err)
		}

		for _, e := range batch {
			metaBytes, _ := json.Marshal(e.Metadata)
			if err := b.Append(
				e.EventID,
				e.EventType,
				e.MarketID,
				string(e.Data),
				string(metaBytes),
				e.Timestamp,
			); err != nil {
				n.svc.Logger.Warn("batch append failed", "error", err,
					"market_id", e.MarketID, "event_type", e.EventType)
			}
		}

		return b.Send()
	}, retry.DefaultOptions())

	if err != nil {
		n.svc.Logger.Error("clickhouse batch send failed after retries",
			"error", err,
			"batch_size", len(batch))
		return
	}

	n.svc.Logger.Info("flushed events to clickhouse",
		"batch_size", len(batch),
		"latency_ms", time.Since(start).Milliseconds())
}
