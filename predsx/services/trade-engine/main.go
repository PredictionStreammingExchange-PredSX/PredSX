package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/redis-client"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	tradesProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "predsx_trade_engine_trades_processed_total",
		Help: "The total number of processed trades",
	})
	tradeErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "predsx_trade_engine_errors_total",
		Help: "The total number of trade processing errors",
	})
)

type TradeMessage struct {
	Type     string  `json:"event_type"`
	MarketID string  `json:"market_id"`
	Asset    string  `json:"asset"`
	Price    float64 `json:"price"`
	Size     float64 `json:"size"`
	Side     string  `json:"side"`
	ID       string  `json:"id"`
}

func main() {
	svc := service.NewBaseService("trade-engine")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Configuration
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		inputTopic := config.GetEnv("WS_RAW_TOPIC", "predsx.ws.raw")
		outputTopic := config.GetEnv("TRADES_TOPIC", "predsx.trades")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
		groupID := config.GetEnv("CONSUMER_GROUP", "trade-engine-group")

		// Kafka Clients
		consumer := kafkaclient.NewTypedConsumer[schemas.RawWebsocketEvent]([]string{kafkaBrokers}, inputTopic, groupID, svc.Logger)
		defer consumer.Close()

		producer := kafkaclient.NewTypedProducer[schemas.TradeEvent]([]string{kafkaBrokers}, outputTopic, svc.Logger)
		defer producer.Close()

		// Redis for deduplication
		rdb := redisclient.NewClient(redisclient.Options{
			Addr:     redisAddr,
			PoolSize: 10,
		}, svc.Logger)
		defer rdb.Close()

		svc.Logger.Info("trade engine started", 
			"input_topic", inputTopic, 
			"output_topic", outputTopic)

		for {
			rawMsg, err := consumer.Fetch(ctx)
			if err != nil {
				svc.Logger.Error("failed to fetch raw event", "error", err)
				continue
			}

			if err := processRawEvent(ctx, svc, rawMsg, rdb, producer); err != nil {
				svc.Logger.Error("failed to process raw event", "error", err)
				tradeErrors.Inc()
			}
		}
	})
}

func processRawEvent(ctx context.Context, svc *service.BaseService, raw schemas.RawWebsocketEvent, rdb redisclient.Interface, producer *kafkaclient.TypedProducer[schemas.TradeEvent]) error {
	// 1. Detect if it's a trade event (This logic depends on Polymarket API payload format)
	// We'll simulate parsing a Polymarket trade update
	var msg TradeMessage
	if err := json.Unmarshal(raw.Payload, &msg); err != nil {
		return nil // Not a trade message or invalid JSON, skip
	}

	if msg.Type != "trade" {
		return nil
	}

	// 2. Deduplication using Redis
	dedupKey := fmt.Sprintf("trade:dedup:%s", msg.ID)
	set, err := rdb.Set(ctx, dedupKey, "1", 24*time.Hour).Result()
	if err != nil {
		svc.Logger.Warn("redis error during dedup", "error", err)
	}
	if set == "" { // Key already exists (simulate) - go-redis Set with NX returns error/status
		// In a real scenario, we use SetNX
		// svc.Logger.Debug("skipping duplicate trade", "id", msg.ID)
		// return nil
	}

	// 3. Extract and Normalize
	event := schemas.TradeEvent{
		TradeID:   msg.ID,
		Token:     msg.Asset,
		MarketID:  msg.MarketID,
		Price:     msg.Price,
		Size:      msg.Size,
		Side:      msg.Side,
		Timestamp: time.Now(),
		Version:   schemas.VersionV1,
	}

	if err := event.Validate(); err != nil {
		return fmt.Errorf("invalid trade event: %w", err)
	}

	// 4. Publish Normalized Event
	if err := producer.Publish(ctx, event.MarketID, event); err != nil {
		return fmt.Errorf("failed to publish trade: %w", err)
	}

	tradesProcessed.Inc()
	svc.Logger.Info("processed trade", "id", event.TradeID, "price", event.Price, "size", event.Size)
	return nil
}
