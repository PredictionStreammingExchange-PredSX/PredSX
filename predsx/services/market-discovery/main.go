package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	redisclient "github.com/predsx/predsx/libs/redis-client"
	"github.com/predsx/predsx/libs/retry-utils"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

type PolymarketMarket struct {
	ID           string          `json:"id"`
	Slug         string          `json:"slug"`
	Title        string          `json:"title"`
	Question     string          `json:"question"`
	ConditionID  string          `json:"conditionID"`
	EventID      string          `json:"eventId"`
	StartDate    string          `json:"startDate"`
	EndDate      string          `json:"endDate"`
	Outcomes     string          `json:"outcomes"` // JSON string in Gamma API sometimes? Let's assume slice for now or parse it
	Status       string          `json:"status"`
	ClobTokenIds json.RawMessage `json:"clobTokenIds"`
}

type GammaResponse struct {
	Value []PolymarketMarket `json:"value"`
	Count int                `json:"Count"`
}

func main() {
	svc := service.NewBaseService("market-discovery")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Configuration
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		topic := config.GetEnv("MARKET_DISCOVERY_TOPIC", "predsx.markets.discovered")
		pollInterval := time.Duration(config.GetEnvInt("POLL_INTERVAL_SECONDS", 60)) * time.Second
		gammaURL := config.GetEnv("GAMMA_API_URL", "https://gamma-api.polymarket.com/markets")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")

		// Kafka Producer
		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			topic: 1, // predsx.markets.discovered
		}, svc.Logger)

		producer := kafkaclient.NewTypedProducer[schemas.MarketDiscovered]([]string{kafkaBrokers}, topic, svc.Logger)
		defer producer.Close()

		rdb := redisclient.NewClient(redisclient.Options{
			Addr:     redisAddr,
			PoolSize: 10,
		}, svc.Logger)
		defer rdb.Close()

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
				if err := discoverMarkets(ctx, svc, gammaURL, rdb, producer); err != nil {
					svc.Logger.Error("discovery cycle failed", "error", err)
				}
			}
		}
	})
}

func discoverMarkets(ctx context.Context, svc *service.BaseService, url string, rdb redisclient.Interface, producer *kafkaclient.TypedProducer[schemas.MarketDiscovered]) error {
	svc.Logger.Info("polling Polymarket Gamma API...")

	// Pagination implementation
	limit := 100
	offset := 0
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}

	for {
		pagedURL := fmt.Sprintf("%s%slimit=%d&offset=%d&active=true&closed=false", url, sep, limit, offset)

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

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			var result GammaResponse
			if err := json.Unmarshal(body, &result); err == nil && len(result.Value) > 0 {
				markets = result.Value
				return nil
			}

			var arr []PolymarketMarket
			if err := json.Unmarshal(body, &arr); err != nil {
				return err
			}
			markets = arr
			return nil
		}, retry.DefaultOptions())

		if err != nil {
			return fmt.Errorf("failed to fetch markets: %w", err)
		}

		if len(markets) == 0 {
			break
		}

		for _, m := range markets {
			// Extract metadata and map to internal schema
			clobTokenIDs := parseClobTokenIDs(m.ClobTokenIds)

			startTime := parseTime(m.StartDate)
			endTime := parseTime(m.EndDate)
			rawBytes, _ := json.Marshal(m)
			rawStr := string(rawBytes)

			event := schemas.MarketDiscovered{
				ID:           m.ID,
				Slug:         m.Slug,
				Title:        m.Title,
				Question:     m.Question,
				ConditionID:  m.ConditionID,
				ClobTokenIDs: clobTokenIDs,
				Exchange:     "polymarket",
				EventID:      m.EventID,
				Raw:          rawStr,
				StartTime:    startTime,
				EndTime:      endTime,
				Status:       m.Status,
				CreatedAt:    time.Now(),
				Version:      schemas.VersionV1,
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

			storeMarketMetadata(ctx, rdb, event, m)
		}

		svc.Logger.Info("processed page", "offset", offset, "count", len(markets))
		offset += limit
	}

	return nil
}

func storeMarketMetadata(ctx context.Context, rdb redisclient.Interface, event schemas.MarketDiscovered, raw PolymarketMarket) {
	meta := map[string]string{
		"id":           event.ID,
		"slug":         event.Slug,
		"title":        event.Title,
		"question":     event.Question,
		"condition_id": event.ConditionID,
		"status":       event.Status,
		"exchange":     event.Exchange,
	}
	if event.EventID != "" {
		meta["event_id"] = event.EventID
	}
	if len(event.Outcomes) > 0 {
		if b, err := json.Marshal(event.Outcomes); err == nil {
			meta["outcomes"] = string(b)
		}
	}
	if !event.StartTime.IsZero() {
		meta["start_time"] = event.StartTime.Format(time.RFC3339Nano)
	}
	if !event.EndTime.IsZero() {
		meta["end_time"] = event.EndTime.Format(time.RFC3339Nano)
	}

	if metaBytes, err := json.Marshal(meta); err == nil {
		rdb.Set(ctx, fmt.Sprintf("market:%s:metadata", event.ID), metaBytes, 0)
	}
	if rawBytes, err := json.Marshal(raw); err == nil {
		rdb.Set(ctx, fmt.Sprintf("market:%s:metadata_raw", event.ID), rawBytes, 0)
	}
	rdb.SAdd(ctx, "predsx:markets", event.ID)
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts
	}
	return time.Time{}
}

func parseClobTokenIDs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	var ids []string
	if err := json.Unmarshal(raw, &ids); err == nil && len(ids) > 0 {
		return ids
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && asString != "" {
		if parsed := parseTokenIDsBytes([]byte(asString)); len(parsed) > 0 {
			return parsed
		}
	}

	return parseTokenIDsBytes(raw)
}

func parseTokenIDsBytes(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}

	var ids []string
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var generic interface{}
	if err := dec.Decode(&generic); err != nil {
		return nil
	}

	switch v := generic.(type) {
	case []interface{}:
		for _, item := range v {
			switch t := item.(type) {
			case string:
				ids = append(ids, t)
			case json.Number:
				ids = append(ids, t.String())
			}
		}
	}
	return ids
}
