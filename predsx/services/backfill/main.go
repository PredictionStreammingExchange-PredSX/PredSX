package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	postgres "github.com/predsx/predsx/libs/postgres-client"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

type BackfillService struct {
	svc       *service.BaseService
	pg        postgres.Interface
	producer  *kafkaclient.TypedProducer[schemas.HistoricalEvent]
	rateLimit time.Duration
}

func main() {
	svc := service.NewBaseService("backfill")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Config
		kafkaBrokers  := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		pgConn        := config.GetEnv("POSTGRES_URL", "postgres://postgres:postgres@localhost:5432/predsx")
		topic         := config.GetEnv("TRADES_BACKFILL_TOPIC", "predsx.trades.backfill")
		rateLimitMs   := config.GetEnvInt("RATE_LIMIT_MS", 100)
		// Optional: allow overriding market list via env var (comma-separated)
		// e.g. BACKFILL_MARKETS=market-1,market-2
		marketsOverride := config.GetEnv("BACKFILL_MARKETS", "")

		// Clients
		pg, err := postgres.NewClient(ctx, pgConn, 5, svc.Logger)
		if err != nil {
			return fmt.Errorf("postgres connect failed: %w", err)
		}
		defer pg.Close()

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			topic: 3,
		}, svc.Logger)

		producer := kafkaclient.NewTypedProducer[schemas.HistoricalEvent]([]string{kafkaBrokers}, topic, svc.Logger)
		defer producer.Close()

		b := &BackfillService{
			svc:       svc,
			pg:        pg,
			producer:  producer,
			rateLimit: time.Duration(rateLimitMs) * time.Millisecond,
		}

		// Initialize progress table
		if err := b.initDB(ctx); err != nil {
			return fmt.Errorf("failed to init backfill_progress table: %w", err)
		}

		// Resolve market list: env override → Postgres query → empty (log warning)
		var markets []string
		if marketsOverride != "" {
			markets = strings.Split(marketsOverride, ",")
			svc.Logger.Info("using market list from BACKFILL_MARKETS env",
				"count", len(markets))
		} else {
			markets, err = b.loadMarketsFromDB(ctx)
			if err != nil {
				svc.Logger.Warn("could not load markets from postgres, backfill skipped",
					"error", err)
				return nil
			}
			svc.Logger.Info("loaded markets from postgres", "count", len(markets))
		}

		if len(markets) == 0 {
			svc.Logger.Warn("no markets found, backfill complete (nothing to do)")
			return nil
		}

		var wg sync.WaitGroup
		for _, mID := range markets {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				b.backfillMarket(ctx, id)
			}(mID)
		}
		wg.Wait()
		return nil
	})
}

// loadMarketsFromDB reads market IDs from the markets table populated by
// the market-discovery service.
func (b *BackfillService) loadMarketsFromDB(ctx context.Context) ([]string, error) {
	rows, err := b.pg.Query(ctx, "SELECT id FROM markets WHERE status='active'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (b *BackfillService) initDB(ctx context.Context) error {
	_, err := b.pg.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS backfill_progress (
			market_id  TEXT PRIMARY KEY,
			last_offset INT DEFAULT 0,
			status     TEXT,
			updated_at TIMESTAMP DEFAULT NOW()
		)
	`)
	return err
}

func (b *BackfillService) backfillMarket(ctx context.Context, marketID string) {
	b.svc.Logger.Info("starting backfill", "market_id", marketID)

	var offset int
	err := b.pg.QueryRow(ctx,
		"SELECT last_offset FROM backfill_progress WHERE market_id=$1", marketID).Scan(&offset)
	if err != nil {
		b.pg.Exec(ctx,
			"INSERT INTO backfill_progress (market_id, status) VALUES ($1, 'running') ON CONFLICT DO NOTHING",
			marketID)
	}

	limit := 100
	for {
		select {
		case <-ctx.Done():
			return
		default:
			events, nextOffset, err := b.fetchFromSource(ctx, marketID, offset, limit)
			if err != nil {
				b.svc.Logger.Error("fetch failed, retrying",
					"market_id", marketID, "error", err)
				time.Sleep(5 * time.Second)
				continue
			}

			if len(events) == 0 {
				b.svc.Logger.Info("backfill complete", "market_id", marketID, "total_offset", offset)
				b.pg.Exec(ctx,
					"UPDATE backfill_progress SET status='completed', updated_at=NOW() WHERE market_id=$1",
					marketID)
				return
			}

			for _, e := range events {
				if err := b.producer.Publish(ctx, marketID, e); err != nil {
					b.svc.Logger.Error("publish failed",
						"market_id", marketID, "error", err)
				}
			}

			offset = nextOffset
			b.pg.Exec(ctx,
				"UPDATE backfill_progress SET last_offset=$1, updated_at=NOW() WHERE market_id=$2",
				offset, marketID)

			b.svc.Logger.Info("backfill progress",
				"market_id", marketID, "offset", offset, "batch", len(events))
			time.Sleep(b.rateLimit)
		}
	}
}

// fetchFromSource simulates fetching historical data.
// In production, replace with real Gamma API or Subgraph calls.
// All events are tagged with metadata.source="backfill" so downstream
// processors can apply idempotency checks and avoid duplicating live events.
func (b *BackfillService) fetchFromSource(ctx context.Context, marketID string, offset, limit int) ([]schemas.HistoricalEvent, int, error) {
	events := make([]schemas.HistoricalEvent, 0)
	if offset < 500 { // Simulate end of data after 500 records
		for i := 0; i < limit; i++ {
			data, _ := json.Marshal(map[string]interface{}{
				"trade_id": fmt.Sprintf("hist_%d", offset+i),
				"price":    0.5,
				// source tag enables downstream idempotency:
				// dedup_key = market_id + timestamp + price + size
				"source": "backfill",
			})
			events = append(events, schemas.HistoricalEvent{
				MarketID:  marketID,
				Source:    "gamma",
				EventType: "trade",
				Data:      data,
				Timestamp: time.Now().Add(-time.Duration(offset+i) * time.Hour),
				Version:   schemas.VersionV1,
			})
		}
	}
	return events, offset + len(events), nil
}
