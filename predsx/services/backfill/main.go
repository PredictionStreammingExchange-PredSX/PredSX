package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/postgres-client"
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
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		pgConn := config.GetEnv("POSTGRES_URL", "postgres://postgres:postgres@localhost:5432/predsx")
		topic := config.GetEnv("HISTORICAL_TOPIC", "predsx.historical")
		rateLimitMs := config.GetEnvInt("RATE_LIMIT_MS", 100)

		// Clients
		pg, err := postgres.NewClient(ctx, pgConn, 5, svc.Logger)
		if err != nil {
			return err
		}
		defer pg.Close()

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
			return err
		}

		// Start parallel workers for different markets or time ranges
		markets := []string{"m1", "m2", "m3"} // In reality, fetch from market-discovery
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

func (b *BackfillService) initDB(ctx context.Context) error {
	_, err := b.pg.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS backfill_progress (
			market_id TEXT PRIMARY KEY,
			last_offset INT DEFAULT 0,
			status TEXT,
			updated_at TIMESTAMP DEFAULT NOW()
		)
	`)
	return err
}

func (b *BackfillService) backfillMarket(ctx context.Context, marketID string) {
	b.svc.Logger.Info("starting backfill for market", "market_id", marketID)

	// 1. Load progress
	var offset int
	err := b.pg.QueryRow(ctx, "SELECT last_offset FROM backfill_progress WHERE market_id=$1", marketID).Scan(&offset)
	if err != nil {
		// Insert initial record if not found
		b.pg.Exec(ctx, "INSERT INTO backfill_progress (market_id, status) VALUES ($1, 'running') ON CONFLICT DO NOTHING", marketID)
	}

	// 2. Paginated Fetch Loop
	limit := 100
	for {
		select {
		case <-ctx.Done():
			return
		default:
			events, nextOffset, err := b.fetchFromSource(ctx, marketID, offset, limit)
			if err != nil {
				b.svc.Logger.Error("failed to fetch historical data", "market_id", marketID, "error", err)
				time.Sleep(5 * time.Second)
				continue
			}

			if len(events) == 0 {
				b.svc.Logger.Info("backfill complete for market", "market_id", marketID)
				b.pg.Exec(ctx, "UPDATE backfill_progress SET status='completed', updated_at=NOW() WHERE market_id=$1", marketID)
				return
			}

			// 3. Publish to Kafka
			for _, e := range events {
				if err := b.producer.Publish(ctx, marketID, e); err != nil {
					b.svc.Logger.Error("failed to publish historical event", "market_id", marketID, "error", err)
				}
			}

			// 4. Persistence
			offset = nextOffset
			b.pg.Exec(ctx, "UPDATE backfill_progress SET last_offset=$1, updated_at=NOW() WHERE market_id=$2", offset, marketID)

			b.svc.Logger.Info("progress updated", "market_id", marketID, "offset", offset)
			time.Sleep(b.rateLimit) // Rate limiting
		}
	}
}

func (b *BackfillService) fetchFromSource(ctx context.Context, marketID string, offset, limit int) ([]schemas.HistoricalEvent, int, error) {
	// Simulated fetch from Polymarket Gamma API or Subgraph
	// In production, this would use http.Client and parse JSON response

	// Simulation:
	events := make([]schemas.HistoricalEvent, 0)
	if offset < 500 { // Simulate end of data after 500 records
		for i := 0; i < limit; i++ {
			data, _ := json.Marshal(map[string]interface{}{"trade_id": fmt.Sprintf("hist_%d", offset+i), "price": 0.5})
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
