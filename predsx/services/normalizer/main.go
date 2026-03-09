package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/redis-client"
	"github.com/predsx/predsx/libs/retry-utils"
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
}

func main() {
	svc := service.NewBaseService("normalizer")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Config
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
		chAddr := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		batchSize := config.GetEnvInt("BATCH_SIZE", 500)
		flushInterval := time.Duration(config.GetEnvInt("FLUSH_INTERVAL_MS", 100)) * time.Millisecond

		// Clients
		rdb := redisclient.NewClient(redisclient.Options{Addr: redisAddr}, svc.Logger)
		ch, err := clickhouse.NewClient(clickhouse.Options{Addr: chAddr}, svc.Logger)
		if err != nil {
			return fmt.Errorf("failed to connect to clickhouse: %w", err)
		}

		n := &Normalizer{
			svc:           svc,
			redis:         rdb,
			clickhouse:    ch,
			batchSize:     batchSize,
			flushInterval: flushInterval,
			buffer:        make([]schemas.NormalizedEvent, 0, batchSize),
		}

		go n.flushLoop(ctx)

		// Topics to consume
		topics := []string{
			"predsx.trades",
			"predsx.orderbook",
			"predsx.prices",
			"predsx.historical",
		}

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

func (n *Normalizer) consumeTopic(ctx context.Context, brokers, topic string) {
	consumer := kafkaclient.NewTypedConsumer[map[string]interface{}]([]string{brokers}, topic, "normalizer-group", n.svc.Logger)
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

	// 1. Enrich with metadata from Redis
	metadata := make(map[string]string)
	val, err := n.redis.Get(ctx, fmt.Sprintf("market:%s:metadata", marketID)).Result()
	if err == nil {
		json.Unmarshal([]byte(val), &metadata)
	}

	// 2. Normalize
	data, _ := json.Marshal(raw)
	event := schemas.NormalizedEvent{
		EventType: topic,
		MarketID:  marketID,
		Data:      data,
		Metadata:  metadata,
		Timestamp: time.Now(),
		Version:   schemas.VersionV1,
	}

	// 3. Update Redis Live State (e.g., last price)
	if topic == "predsx.prices" {
		n.redis.Set(ctx, fmt.Sprintf("live:price:%s", marketID), data, 0)
	}

	// 4. Buffer for ClickHouse
	n.mu.Lock()
	n.buffer = append(n.buffer, event)
	if len(n.buffer) >= n.batchSize {
		n.flush(ctx)
	}
	n.mu.Unlock()
}

func (n *Normalizer) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(n.flushInterval)
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

func (n *Normalizer) flush(ctx context.Context) {
	if len(n.buffer) == 0 {
		return
	}

	n.svc.Logger.Info("flushing events to ClickHouse", "count", len(n.buffer))

	err := retry.Do(ctx, func() error {
		// Simulation of batch ClickHouse insert
		// In reality, we'd use clickhouse.Conn.PrepareBatch or similar
		return n.clickhouse.Exec(ctx, "INSERT INTO events (type, market_id, data, metadata, timestamp) VALUES ...")
	}, retry.DefaultOptions())

	if err != nil {
		n.svc.Logger.Error("failed to write to ClickHouse after retries", "error", err)
	}

	n.buffer = n.buffer[:0]
}
