package main

import (
	"context"

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
		inputTopic := config.GetEnv("TRADES_LIVE_TOPIC", "predsx.trades.live")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
		groupID := config.GetEnv("CONSUMER_GROUP", "trade-engine-group")

		// Kafka Clients
		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			inputTopic:  6,
		}, svc.Logger)

		consumer := kafkaclient.NewTypedConsumer[schemas.TradeEvent]([]string{kafkaBrokers}, inputTopic, groupID, svc.Logger)
		defer consumer.Close()



		// Redis for deduplication
		rdb := redisclient.NewClient(redisclient.Options{
			Addr:     redisAddr,
			PoolSize: 10,
		}, svc.Logger)
		defer rdb.Close()

		svc.Logger.Info("trade engine started", 
			"input_topic", inputTopic)

		for {
			rawMsg, err := consumer.Fetch(ctx)
			if err != nil {
				svc.Logger.Error("failed to fetch raw event", "error", err)
				continue
			}

			if err := processEvent(ctx, svc, rawMsg, rdb); err != nil {
				svc.Logger.Error("failed to process event", "error", err)
				tradeErrors.Inc()
			}
		}
	})
}

func processEvent(ctx context.Context, svc *service.BaseService, event schemas.TradeEvent, rdb redisclient.Interface) error {
	// 2. Analytics / Deduplication logic using Redis
	// Trade engine receives normalized trades directly. If we need to deduplicate 
	// based on analytic windows, we do it here.
	// dedupKey := fmt.Sprintf("trade:analytics:%s", event.TradeID)
	// rdb.Set(ctx, dedupKey, "1", 24*time.Hour)

	// 3. Analytics
	// Calculate moving averages, volatility, etc based on `event`
	// ...

	tradesProcessed.Inc()
	svc.Logger.Info("processed trade", "id", event.TradeID, "price", event.Price, "size", event.Size)
	return nil
}
