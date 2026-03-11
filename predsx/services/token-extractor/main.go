package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/redis-client"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

func main() {
	svc := service.NewBaseService("token-extractor")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Configuration
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		inputTopic := config.GetEnv("MARKET_DISCOVERY_TOPIC", "predsx.markets.discovered")
		outputTopic := config.GetEnv("TOKEN_EXTRACTOR_TOPIC", "predsx.tokens.extracted")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
		groupID := config.GetEnv("CONSUMER_GROUP", "token-extractor-group")

		// Kafka Clients
		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			inputTopic:  1,
			outputTopic: 1,
		}, svc.Logger)
		
		consumer := kafkaclient.NewTypedConsumer[schemas.MarketDiscovered]([]string{kafkaBrokers}, inputTopic, groupID, svc.Logger)
		defer consumer.Close()
		producer := kafkaclient.NewTypedProducer[schemas.TokenExtracted]([]string{kafkaBrokers}, outputTopic, svc.Logger)
		defer producer.Close()

		// Redis Client
		rdb := redisclient.NewClient(redisclient.Options{
			Addr:     redisAddr,
			PoolSize: 10,
		}, svc.Logger)
		defer rdb.Close()

		svc.Logger.Info("starting token extractor loop", 
			"input_topic", inputTopic, 
			"output_topic", outputTopic,
			"group_id", groupID)

		for {
			msg, err := consumer.Fetch(ctx)
			if err != nil {
				svc.Logger.Error("failed to fetch market", "error", err)
				continue
			}

			if err := processMarket(ctx, svc, msg, rdb, producer); err != nil {
				svc.Logger.Error("failed to process market", "id", msg.ID, "error", err)
			}
		}
	})
}

func processMarket(ctx context.Context, svc *service.BaseService, market schemas.MarketDiscovered, rdb redisclient.Interface, producer *kafkaclient.TypedProducer[schemas.TokenExtracted]) error {
	// Idempotency check: Have we already extracted tokens for this market?
	redisKey := fmt.Sprintf("tokens:extracted:%s", market.ID)
	exists, err := rdb.Get(ctx, redisKey).Result()
	if err == nil && exists != "" {
		svc.Logger.Debug("skipping already processed market", "id", market.ID)
		return nil
	}

	// Simulated extraction logic: In a real scenario, this would involve 
	// parsing market metadata or querying a contract.
	// For now, we simulate finding YES/NO token addresses.
	tokenYes := fmt.Sprintf("0xYES_%s", market.ID)
	tokenNo := fmt.Sprintf("0xNO_%s", market.ID)

	event := schemas.TokenExtracted{
		MarketID: market.ID,
		TokenYes: tokenYes,
		TokenNo:  tokenNo,
		Version:  schemas.VersionV1,
	}

	// Store in Redis for idempotency and caching
	data, _ := json.Marshal(event)
	if err := rdb.Set(ctx, redisKey, data, 0).Err(); err != nil {
		svc.Logger.Warn("failed to cache tokens in redis", "id", market.ID, "error", err)
	}

	// Also store a mapping for external lookups
	rdb.Set(ctx, fmt.Sprintf("market:%s:tokens", market.ID), data, 0)

	// Publish to Kafka
	if err := producer.Publish(ctx, event.MarketID, event); err != nil {
		return fmt.Errorf("failed to publish tokens: %w", err)
	}

	svc.Logger.Info("extracted and published tokens", "market_id", market.ID, "yes", tokenYes, "no", tokenNo)
	return nil
}
