package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/retry-utils"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

type PolymarketMarket struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Question    string    `json:"question"`
	ConditionID string    `json:"conditionID"`
	StartDate   string    `json:"startDate"`
	EndDate     string    `json:"endDate"`
	Outcomes    string    `json:"outcomes"` // JSON string in Gamma API sometimes? Let's assume slice for now or parse it
	Status      string    `json:"status"`
}

func main() {
	svc := service.NewBaseService("market-discovery")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Configuration
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		topic := config.GetEnv("MARKET_DISCOVERY_TOPIC", "predsx.markets.discovered")
		pollInterval := time.Duration(config.GetEnvInt("POLL_INTERVAL_SECONDS", 60)) * time.Second
		gammaURL := config.GetEnv("GAMMA_API_URL", "https://gamma-api.polymarket.com/markets")

		// Kafka Producer
		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			topic: 1, // predsx.markets.discovered
		}, svc.Logger)

		producer := kafkaclient.NewTypedProducer[schemas.MarketDiscovered]([]string{kafkaBrokers}, topic, svc.Logger)
		defer producer.Close()

		svc.Logger.Info("starting market discovery loop", 
			"interval", pollInterval, 
			"topic", topic, 
			"api_url", gammaURL)

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if err := discoverMarkets(ctx, svc, gammaURL, producer); err != nil {
					svc.Logger.Error("discovery cycle failed", "error", err)
				}
			}
		}
	})
}

func discoverMarkets(ctx context.Context, svc *service.BaseService, url string, producer *kafkaclient.TypedProducer[schemas.MarketDiscovered]) error {
	svc.Logger.Info("polling Polymarket Gamma API...")

	// Pagination implementation
	limit := 100
	offset := 0

	for {
		pagedURL := fmt.Sprintf("%s?limit=%d&offset=%d", url, limit, offset)
		
		var markets []PolymarketMarket
		err := retry.Do(ctx, func() error {
			resp, err := http.Get(pagedURL)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			}

			return json.NewDecoder(resp.Body).Decode(&markets)
		}, retry.DefaultOptions())

		if err != nil {
			return fmt.Errorf("failed to fetch markets: %w", err)
		}

		if len(markets) == 0 {
			break
		}

		for _, m := range markets {
			// Extract metadata and map to internal schema
			event := schemas.MarketDiscovered{
				ID:          m.ID,
				Slug:        m.Slug,
				Title:       m.Title,
				Question:    m.Question,
				ConditionID: m.ConditionID,
				Status:      m.Status,
				CreatedAt:   time.Now(),
				Version:     schemas.VersionV1,
			}

			// Parse outcomes if it's a JSON string
			var outcomes []string
			if err := json.Unmarshal([]byte(m.Outcomes), &outcomes); err == nil {
				event.Outcomes = outcomes
			} else {
				// Fallback or handle if already slice (unlikely if decoding into struct expects string)
				// For this implementation, we assume outcomes is a JSON array string in the source API
			}

			if err := producer.Publish(ctx, event.ID, event); err != nil {
				svc.Logger.Error("failed to publish market", "id", event.ID, "error", err)
			}
		}

		svc.Logger.Info("processed page", "offset", offset, "count", len(markets))
		offset += limit
	}

	return nil
}
