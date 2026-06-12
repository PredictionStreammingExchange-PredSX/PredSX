package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
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
	Outcomes     string          `json:"outcomes"`
	Status       string          `json:"status"`
	Active       bool            `json:"active"`
	Closed       bool            `json:"closed"`
	Resolved     bool            `json:"resolved"`
	ClobTokenIds json.RawMessage `json:"clobTokenIds"`
}

func (m PolymarketMarket) DerivedStatus() string {
	if m.Status != "" {
		return strings.ToUpper(m.Status)
	}
	if m.Resolved {
		return "RESOLVED"
	}
	if m.Closed {
		return "CLOSED"
	}
	if m.Active {
		return "ACTIVE"
	}
	return "UNKNOWN"
}

type GammaResponse struct {
	Value []PolymarketMarket `json:"value"`
	Count int                `json:"Count"`
}

type PolymarketEvent struct {
	ID      string             `json:"id"`
	Slug    string             `json:"slug"`
	Title   string             `json:"title"`
	Active  bool               `json:"active"`
	Closed  bool               `json:"closed"`
	Markets []PolymarketMarket `json:"markets"`
}

func main() {
	svc := service.NewBaseService("discovery-hub")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Configuration
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		discoveryTopic := config.GetEnv("MARKET_DISCOVERY_TOPIC", "predsx.markets.discovered")
		tokenTopic := config.GetEnv("TOKEN_EXTRACTOR_TOPIC", "predsx.tokens.extracted")
		pollInterval := time.Duration(config.GetEnvInt("POLL_INTERVAL_SECONDS", 60)) * time.Second
		gammaURL := config.GetEnv("GAMMA_API_URL", "https://gamma-api.polymarket.com/markets")
		eventsURL := config.GetEnv("GAMMA_EVENTS_URL", "https://gamma-api.polymarket.com/events")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")

		// Kafka Clients
		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			discoveryTopic: 1,
			tokenTopic:     1,
		}, svc.Logger)

		discoveryProducer := kafkaclient.NewTypedProducer[schemas.MarketDiscovered]([]string{kafkaBrokers}, discoveryTopic, svc.Logger)
		defer discoveryProducer.Close()

		tokenProducer := kafkaclient.NewTypedProducer[schemas.TokenExtracted]([]string{kafkaBrokers}, tokenTopic, svc.Logger)
		defer tokenProducer.Close()


		// Redis Client
		rdb := redisclient.NewClient(redisclient.Options{
			Addr:     redisAddr,
			PoolSize: 10,
		}, svc.Logger)
		defer rdb.Close()


		// Start Market Discovery Loop (Producer)
		svc.Logger.Info("starting market discovery sub-loop", "interval", pollInterval)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		runBoth := func(label string) {
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				if err := discoverMarkets(ctx, svc, gammaURL, rdb, discoveryProducer, tokenProducer); err != nil {
					svc.Logger.Error(label+" markets failed", "error", err)
				}
			}()
			go func() {
				defer wg.Done()
				if err := discoverEvents(ctx, svc, eventsURL, rdb, discoveryProducer, tokenProducer); err != nil {
					svc.Logger.Error(label+" events failed", "error", err)
				}
			}()
			wg.Wait()
		}

		// Initial Discovery
		runBoth("initial discovery")

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				runBoth("discovery cycle")
			}
		}
	})
}

// Logic from market-discovery
func discoverMarkets(ctx context.Context, svc *service.BaseService, url string, rdb redisclient.Interface, producer *kafkaclient.TypedProducer[schemas.MarketDiscovered], tokenProducer *kafkaclient.TypedProducer[schemas.TokenExtracted]) error {
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
				svc.Logger.Info("unmarshaled gamma response", "count", len(markets))
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

		svc.Logger.Info("fetched markets from gamma", "count", len(markets), "offset", offset)

		for _, m := range markets {
			clobTokenIDs := parseClobTokenIDs(m.ClobTokenIds)
			startTime := parseTime(m.StartDate)
			endTime := parseTime(m.EndDate)
			rawBytes, _ := json.Marshal(m)

			event := schemas.MarketDiscovered{
				ID:           m.ID,
				Slug:         m.Slug,
				Title:        m.Title,
				Question:     m.Question,
				ConditionID:  m.ConditionID,
				ClobTokenIDs: clobTokenIDs,
				Exchange:     "polymarket",
				EventID:      m.EventID,
				Raw:          string(rawBytes),
				StartTime:    startTime,
				EndTime:      endTime,
				Status:       m.DerivedStatus(),
				CreatedAt:    time.Now(),
				Version:      schemas.VersionV1,
			}

			if len(event.ClobTokenIDs) == 0 && len(m.ClobTokenIds) > 0 {
				svc.Logger.Warn("failed to parse tokens", "id", event.ID, "raw", string(m.ClobTokenIds))
			}

			var outcomes []string
			if err := json.Unmarshal([]byte(m.Outcomes), &outcomes); err == nil {
				event.Outcomes = outcomes
			}

			if err := producer.Publish(ctx, event.ID, event); err != nil {
				svc.Logger.Error("failed to publish market", "id", event.ID, "error", err)
			} else {
				svc.Logger.Info("published market to kafka", "id", event.ID)
			}

			// Direct Process (Avoid internal Kafka round-trip for token extraction)
			if err := processMarket(ctx, svc, event, rdb, tokenProducer); err != nil {
				svc.Logger.Error("failed to extract tokens", "id", event.ID, "error", err)
			}

			storeMarketMetadata(ctx, rdb, event, m)
		}
		offset += limit
		// Pace pagination to avoid bursting Polymarket's Gamma API with back-to-back
		// requests. 200ms gap keeps the full crawl under ~10 req/s regardless of
		// market count, well below any undocumented rate limit.
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

// Logic from token-extractor
func processMarket(ctx context.Context, svc *service.BaseService, market schemas.MarketDiscovered, rdb redisclient.Interface, producer *kafkaclient.TypedProducer[schemas.TokenExtracted]) error {
	redisKey := fmt.Sprintf("tokens:extracted:%s", market.ID)

	if len(market.ClobTokenIDs) < 2 {
		svc.Logger.Warn("skipping market: not enough tokens", "market_id", market.ID, "tokens", market.ClobTokenIDs)
		return nil
	}

	event := schemas.TokenExtracted{
		MarketID: market.ID,
		TokenYes: market.ClobTokenIDs[0],
		TokenNo:  market.ClobTokenIDs[1],
		Exchange: market.Exchange,
		Version:  schemas.VersionV1,
	}

	data, _ := json.Marshal(event)
	rdb.Set(ctx, redisKey, data, 0)
	rdb.Set(ctx, fmt.Sprintf("market:%s:tokens", market.ID), data, 0)
	rdb.Set(ctx, fmt.Sprintf("token:%s:market_id", event.TokenYes), market.ID, 0)
	rdb.Set(ctx, fmt.Sprintf("token:%s:market_id", event.TokenNo), market.ID, 0)
	if market.ConditionID != "" {
		rdb.Set(ctx, fmt.Sprintf("condition:%s:market_id", market.ConditionID), market.ID, 0)
	}

	svc.Logger.Info("extracted tokens for market", "market_id", market.ID, "yes", event.TokenYes, "no", event.TokenNo)
	return producer.Publish(ctx, event.MarketID, event)
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
	if b, err := json.Marshal(event.Outcomes); err == nil {
		meta["outcomes"] = string(b)
	}
	if metaBytes, err := json.Marshal(meta); err == nil {
		rdb.Set(ctx, fmt.Sprintf("market:%s:metadata", event.ID), metaBytes, 0)
	}
	if rawBytes, err := json.Marshal(raw); err == nil {
		rdb.Set(ctx, fmt.Sprintf("market:%s:metadata_raw", event.ID), rawBytes, 0)
	}
	if event.ConditionID != "" {
		rdb.Set(ctx, fmt.Sprintf("condition:%s:market_id", event.ConditionID), event.ID, 0)
	}
	rdb.SAdd(ctx, "predsx:markets", event.ID)
}

func discoverEvents(ctx context.Context, svc *service.BaseService, url string, rdb redisclient.Interface, producer *kafkaclient.TypedProducer[schemas.MarketDiscovered], tokenProducer *kafkaclient.TypedProducer[schemas.TokenExtracted]) error {
	limit := 100
	offset := 0
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}

	for {
		pagedURL := fmt.Sprintf("%s%slimit=%d&offset=%d&active=true&closed=false", url, sep, limit, offset)
		var events []PolymarketEvent

		err := retry.Do(ctx, func() error {
			resp, err := http.Get(pagedURL)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status: %d", resp.StatusCode)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			return json.Unmarshal(body, &events)
		}, retry.DefaultOptions())

		if err != nil {
			return fmt.Errorf("failed to fetch events: %w", err)
		}
		if len(events) == 0 {
			break
		}

		svc.Logger.Info("fetched events from gamma", "count", len(events), "offset", offset)

		for _, ev := range events {
			for _, m := range ev.Markets {
				// Guarantee event_id is set — use the parent event ID if missing on the market
				if m.EventID == "" {
					m.EventID = ev.ID
				}

				clobTokenIDs := parseClobTokenIDs(m.ClobTokenIds)
				startTime := parseTime(m.StartDate)
				endTime := parseTime(m.EndDate)
				rawBytes, _ := json.Marshal(m)

				event := schemas.MarketDiscovered{
					ID:           m.ID,
					Slug:         m.Slug,
					Title:        m.Title,
					Question:     m.Question,
					ConditionID:  m.ConditionID,
					ClobTokenIDs: clobTokenIDs,
					Exchange:     "polymarket",
					EventID:      m.EventID,
					Raw:          string(rawBytes),
					StartTime:    startTime,
					EndTime:      endTime,
					Status:       m.DerivedStatus(),
					CreatedAt:    time.Now(),
					Version:      schemas.VersionV1,
				}

				var outcomes []string
				if err := json.Unmarshal([]byte(m.Outcomes), &outcomes); err == nil {
					event.Outcomes = outcomes
				}

				if err := producer.Publish(ctx, event.ID, event); err != nil {
					svc.Logger.Error("failed to publish event market", "id", event.ID, "error", err)
				} else {
					svc.Logger.Info("published event market to kafka", "id", event.ID, "event_id", ev.ID)
				}

				if err := processMarket(ctx, svc, event, rdb, tokenProducer); err != nil {
					svc.Logger.Error("failed to extract tokens for event market", "id", event.ID, "error", err)
				}

				storeMarketMetadata(ctx, rdb, event, m)
			}
		}

		offset += limit
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

func parseTime(raw string) time.Time {
	ts, _ := time.Parse(time.RFC3339, raw)
	return ts
}

func parseClobTokenIDs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	
	// Try unmarshaling as array first
	var ids []string
	if err := json.Unmarshal(raw, &ids); err == nil && len(ids) > 0 {
		return ids
	}
	
	// Try unmarshaling as a JSON string that contains an array
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		if err := json.Unmarshal([]byte(s), &ids); err == nil && len(ids) > 0 {
			return ids
		}
		// Maybe it's just a comma separated string?
		return []string{s}
	}
	
	// Last resort: if it's a string like "[id1, id2]" but not properly quoted
	return nil
}
