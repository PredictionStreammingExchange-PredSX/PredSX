package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	clickhouse "github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	retry "github.com/predsx/predsx/libs/retry-utils"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

// MetricBucket stores aggregations for a 1-second window.
type MetricBucket struct {
	MarketID    string
	Window      time.Time
	TradeCount  uint32
	Volume      float64
	PriceSumWt  float64
}

type StreamAggregator struct {
	svc          *service.BaseService
	clickhouse   clickhouse.Interface
	producer     *kafkaclient.TypedProducer[schemas.PriceUpdate]
	buckets      map[string]*MetricBucket // key: marketID:unixsec
	bucketsMu    sync.Mutex
	tickInterval time.Duration
}

func main() {
	svc := service.NewBaseService("stream-aggregator")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Config
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		inputTopic   := config.GetEnv("TRADES_LIVE_TOPIC", "predsx.trades.live")
		outputTopic  := config.GetEnv("METRICS_TRADES_TOPIC", "predsx.metrics.trades")
		groupID      := config.GetEnv("CONSUMER_GROUP", "stream-aggregator-group")
		chAddr       := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser       := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword   := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase   := config.GetEnv("CLICKHOUSE_DB", "default")

		// Kafka Clients
		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			inputTopic:  6,
			outputTopic: 3,
		}, svc.Logger)

		consumer := kafkaclient.NewTypedConsumer[schemas.TradeEvent]([]string{kafkaBrokers}, inputTopic, groupID, svc.Logger)
		defer consumer.Close()

		producer := kafkaclient.NewTypedProducer[schemas.PriceUpdate]([]string{kafkaBrokers}, outputTopic, svc.Logger)
		defer producer.Close()

		// ClickHouse Client
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

		// Ensure schema
		if err := ensureSchema(ctx, ch); err != nil {
			return fmt.Errorf("failed to ensure market_metrics schema: %w", err)
		}
		svc.Logger.Info("clickhouse schema market_metrics verified")

		aggr := &StreamAggregator{
			svc:          svc,
			clickhouse:   ch,
			producer:     producer,
			buckets:      make(map[string]*MetricBucket),
			tickInterval: 1 * time.Second,
		}

		go aggr.flushLoop(ctx)

		svc.Logger.Info("stream-aggregator started", "input", inputTopic, "output", outputTopic)

		for {
			trade, err := consumer.Fetch(ctx)
			if err != nil {
				svc.Logger.Error("fetch error", "error", err)
				continue
			}

			aggr.processTrade(trade)
		}
	})
}

// ensureSchema creates the market_metrics table for 1s aggregated windows.
func ensureSchema(ctx context.Context, ch clickhouse.Interface) error {
	return ch.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS market_metrics (
			market_id    String,
			timestamp    DateTime64(3),
			trade_count  UInt32,
			volume       Float64,
			avg_price    Float64
		) ENGINE = MergeTree()
		PARTITION BY toDate(timestamp)
		ORDER BY (market_id, timestamp)
	`)
}

func (a *StreamAggregator) processTrade(trade schemas.TradeEvent) {
	// Truncate timestamp to the 1-second interval floor
	window := trade.Timestamp.Truncate(1 * time.Second)
	key := fmt.Sprintf("%s:%d", trade.MarketID, window.Unix())

	a.bucketsMu.Lock()
	defer a.bucketsMu.Unlock()

	bucket, ok := a.buckets[key]
	if !ok {
		bucket = &MetricBucket{
			MarketID: trade.MarketID,
			Window:   window,
		}
		a.buckets[key] = bucket
	}

	bucket.TradeCount++
	bucket.Volume += trade.Size
	// Weighted price logic: sum(price * size) / total_size
	bucket.PriceSumWt += trade.Price * trade.Size
}

func (a *StreamAggregator) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(a.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.flushAndEmit(ctx)
		}
	}
}

func (a *StreamAggregator) flushAndEmit(ctx context.Context) {
	a.bucketsMu.Lock()
	// Quick memory swap to minimize lock contention
	currentBuckets := a.buckets
	a.buckets = make(map[string]*MetricBucket)
	a.bucketsMu.Unlock()

	if len(currentBuckets) == 0 {
		return
	}

	start := time.Now()

	// 1. Flush to ClickHouse `market_metrics` table
	err := retry.Do(ctx, func() error {
		b, err := a.clickhouse.PrepareBatch(ctx,
			"INSERT INTO market_metrics (market_id, timestamp, trade_count, volume, avg_price)")
		if err != nil {
			return fmt.Errorf("prepare batch: %w", err)
		}

		for _, bucket := range currentBuckets {
			avgPrice := 0.0
			if bucket.Volume > 0 {
				avgPrice = bucket.PriceSumWt / bucket.Volume
			}

			if err := b.Append(
				bucket.MarketID,
				bucket.Window,
				bucket.TradeCount,
				bucket.Volume,
				avgPrice,
			); err != nil {
				a.svc.Logger.Warn("batch append failed", "market_id", bucket.MarketID, "error", err)
			}
		}

		return b.Send()
	}, retry.DefaultOptions())

	if err != nil {
		a.svc.Logger.Error("clickhouse metrics flush failed after retries",
			"error", err, "batch_size", len(currentBuckets))
	} else {
		a.svc.Logger.Info("flushed aggregated metrics",
			"batch_size", len(currentBuckets),
			"latency_ms", time.Since(start).Milliseconds())
	}

	// 2. (Optional based on requirements) Publish to `predsx.metrics.trades` to feed downstream stream analytics.
	// For now we persist them efficiently for the API.
}
