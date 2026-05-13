package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	clickhouse "github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	"github.com/predsx/predsx/libs/crypto"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	postgres "github.com/predsx/predsx/libs/postgres-client"
	redisclient "github.com/predsx/predsx/libs/redis-client"
	retry "github.com/predsx/predsx/libs/retry-utils"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

// Storage Hub combines:
// 1. Normalizer (Live Persistence to ClickHouse)
// 2. Backfill (Historical Data Fetcher)

func main() {
	svc := service.NewBaseService("storage-hub")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// --- SHARED INFRA ---
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		chAddr := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase := config.GetEnv("CLICKHOUSE_DB", "default")
		pgConn := config.GetEnv("POSTGRES_URL", "postgres://postgres:postgres@localhost:5432/predsx")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")

		// Shared Clients
		ch, err := clickhouse.NewClient(clickhouse.Options{
			Addr:     chAddr,
			User:     chUser,
			Password: chPassword,
			Database: chDatabase,
			MaxConns: 10,
		}, svc.Logger)
		if err != nil {
			return fmt.Errorf("clickhouse connect failed: %w", err)
		}

		pg, _ := postgres.NewClient(ctx, pgConn, 5, svc.Logger)
		rdb := redisclient.NewClient(redisclient.Options{Addr: redisAddr}, svc.Logger)
		defer rdb.Close()

		// 1. START NORMALIZER COMPONENT
		go startNormalizerComponent(ctx, svc, kafkaBrokers, ch, rdb)

		// 2. START BACKFILL COMPONENT
		if pg != nil {
			go startBackfillComponent(ctx, svc, kafkaBrokers)
		}

		<-ctx.Done()
		return nil
	})
}

// --- NORMALIZER COMPONENT ---

func startNormalizerComponent(ctx context.Context, svc *service.BaseService, brokers string, ch clickhouse.Interface, rdb redisclient.Interface) {
	ensureSchemas(ctx, ch)

	wsTopic := config.GetEnv("WS_RAW_TOPIC", "predsx.ws.raw")
	backfillTopic := config.GetEnv("TRADES_BACKFILL_TOPIC", "predsx.trades.backfill")
	tradesLiveTopic := config.GetEnv("TRADES_LIVE_TOPIC", "predsx.trades.live")
	obTopic := config.GetEnv("ORDERBOOK_UPDATES_TOPIC", "predsx.orderbook.updates")
	discoveryTopic := config.GetEnv("MARKET_DISCOVERY_TOPIC", "predsx.markets.discovered")
	onChainTopic := config.GetEnv("ONCHAIN_TRADES_TOPIC", "predsx.trades.onchain")

	producer := kafkaclient.NewTypedProducer[schemas.TradeEvent]([]string{brokers}, tradesLiveTopic, svc.Logger)
	defer producer.Close()

	wsConsumer := kafkaclient.NewTypedConsumer[schemas.RawWebsocketEvent]([]string{brokers}, wsTopic, "storage-norm-ws", svc.Logger)
	bfConsumer := kafkaclient.NewTypedConsumer[schemas.HistoricalEvent]([]string{brokers}, backfillTopic, "storage-norm-bf", svc.Logger)
	obConsumer := kafkaclient.NewTypedConsumer[schemas.OrderbookUpdate]([]string{brokers}, obTopic, "storage-norm-ob", svc.Logger)
	disConsumer := kafkaclient.NewTypedConsumer[schemas.MarketDiscovered]([]string{brokers}, discoveryTopic, "storage-norm-dis", svc.Logger)
	oncConsumer := kafkaclient.NewTypedConsumer[schemas.OnChainTradeEvent]([]string{brokers}, onChainTopic, "storage-norm-onc", svc.Logger)

	svc.Logger.Info("normalizer component started")

	var buffer []schemas.NormalizedEvent
	var mu sync.Mutex

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				if len(buffer) > 0 {
					flushBatch(ctx, ch, buffer)
					buffer = nil
				}
				mu.Unlock()
			}
		}
	}()

	// WS Trades -> Live Broadcast + Buffer
	go func() {
		for {
			raw, _ := wsConsumer.Fetch(ctx)
			processWebsocketEvent(ctx, rdb, producer, raw, func(e schemas.NormalizedEvent) {
				mu.Lock()
				buffer = append(buffer, e)
				mu.Unlock()
			})
		}
	}()

	// Backfill -> Buffer
	go func() {
		for {
			evt, _ := bfConsumer.Fetch(ctx)
			mu.Lock()
			buffer = append(buffer, normalizeBackfill(evt))
			mu.Unlock()
		}
	}()

	// Orderbook Snapshots -> Direct Insert (Low Frequency)
	go func() {
		for {
			evt, _ := obConsumer.Fetch(ctx)
			insertOrderbookSnapshot(ctx, ch, evt)
		}
	}()

	// Metadata Persistence
	go func() {
		for {
			evt, _ := disConsumer.Fetch(ctx)
			persistMarketMetadata(ctx, ch, evt)
		}
	}()

	// OnChain trades -> Direct insert
	for {
		evt, _ := oncConsumer.Fetch(ctx)
		ch.Exec(ctx, `INSERT INTO onchain_trades (tx_hash, maker, taker, token_id, amount, timestamp) VALUES (?, ?, ?, ?, ?, ?)`,
			evt.TxHash, evt.Maker, evt.Taker, evt.TokenID, evt.Amount, evt.Timestamp)
	}
}

func ensureSchemas(ctx context.Context, ch clickhouse.Interface) {
	ch.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events_raw (
			event_id    String,
			type        LowCardinality(String),
			market_id   String,
			timestamp   DateTime64(3),
			data        String,
			metadata    String
		) ENGINE = MergeTree()
		PARTITION BY toDate(timestamp)
		ORDER BY (market_id, timestamp)
		TTL timestamp + INTERVAL 3 DAY;
	`)
	ch.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS market_metadata (
			market_id String, slug String, title String, question String, condition_id String,
			status String, exchange String, event_id String, start_time DateTime64(3),
			end_time DateTime64(3), outcomes String, created_at DateTime64(3), raw String
		) ENGINE = MergeTree() ORDER BY (market_id, created_at)
	`)
	ch.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS orderbook_history (
			market_id String, token String, timestamp DateTime64(3),
			best_bid Float64, best_ask Float64, mid Float64, spread Float64, bids String, asks String
		) ENGINE = MergeTree() ORDER BY (market_id, timestamp)
	`)
	ch.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS onchain_trades (
			tx_hash String, maker String, taker String, token_id String, amount String, timestamp DateTime64(3)
		) ENGINE = MergeTree() ORDER BY (tx_hash, timestamp)
	`)
}

func processWebsocketEvent(ctx context.Context, rdb redisclient.Interface, p *kafkaclient.TypedProducer[schemas.TradeEvent], raw schemas.RawWebsocketEvent, onNormalized func(schemas.NormalizedEvent)) {
	var payload map[string]interface{}
	json.Unmarshal(raw.Payload, &payload)
	eType := getString(payload, "event_type", "type")
	if eType != "last_trade_price" && eType != "price_change" {
		return
	}

	marketID := resolveCanonicalMarketID(ctx, rdb, getString(payload, "market_id", "market", "condition_id"), "")
	changes, ok := payload["price_changes"].([]interface{})
	if ok && len(changes) > 0 {
		eventTimestamp := parsePayloadTimestamp(payload["timestamp"])
		for _, item := range changes {
			change, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			token := getString(change, "asset_id", "asset", "token")
			if marketID == "" {
				marketID = resolveCanonicalMarketID(ctx, rdb, getString(payload, "market_id", "market", "condition_id"), token)
			}
			if marketID == "" {
				continue
			}
			price := toFloat(change["price"])
			size := toFloat(change["size"])
			if size == 0 {
				size = toFloat(change["amount"])
			}
			if price <= 0 || size <= 0 {
				continue
			}

			tradeTime := eventTimestamp
			if tradeTime.IsZero() {
				tradeTime = time.Now().UTC()
			}
			tradeID := getString(change, "hash", "trade_id")
			if tradeID == "" {
				tsStr := fmt.Sprintf("%d", tradeTime.UnixNano())
				priceStr := fmt.Sprintf("%v", price)
				sizeStr := fmt.Sprintf("%v", size)
				tradeID = crypto.GenerateEventID(marketID, tsStr, priceStr, sizeStr, token)
			}

			trade := schemas.TradeEvent{
				TradeID:   tradeID,
				MarketID:  marketID,
				Token:     token,
				Price:     price,
				Size:      size,
				Side:      strings.ToLower(getString(change, "side")),
				Timestamp: tradeTime,
				Version:   schemas.VersionV1,
			}
			p.Publish(ctx, marketID, trade)

			tradePayload, _ := json.Marshal(trade)
			onNormalized(schemas.NormalizedEvent{
				EventID:   trade.TradeID,
				MarketID:  marketID,
				EventType: "predsx.trades",
				Data:      tradePayload,
				Timestamp: tradeTime,
			})
		}
		return
	}

	price := toFloat(payload["price"])
	size := toFloat(payload["size"])
	if size == 0 {
		size = toFloat(payload["amount"])
	}

	if price > 0 && size > 0 {
		ts := time.Now().UTC()
		tsStr := fmt.Sprintf("%d", ts.UnixNano())
		priceStr := fmt.Sprintf("%v", price)
		sizeStr := fmt.Sprintf("%v", size)
		
		trade := schemas.TradeEvent{
			TradeID:   crypto.GenerateEventID(marketID, tsStr, priceStr, sizeStr, "ws"),
			MarketID:  marketID,
			Token:     getString(payload, "asset_id", "asset"),
			Price:     price,
			Size:      size,
			Side:      strings.ToLower(getString(payload, "side")),
			Timestamp: ts,
			Version:   schemas.VersionV1,
		}
		p.Publish(ctx, marketID, trade)
		onNormalized(schemas.NormalizedEvent{
			EventID: trade.TradeID, MarketID: marketID, EventType: "predsx.trades", Data: raw.Payload, Timestamp: ts,
		})
	}
}

func normalizeBackfill(evt schemas.HistoricalEvent) schemas.NormalizedEvent {
	ts := evt.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	tsStr := fmt.Sprintf("%d", ts.UnixNano())
	return schemas.NormalizedEvent{
		EventID: crypto.GenerateEventID(evt.MarketID, tsStr, "0", "0", "backfill"), MarketID: evt.MarketID,
		EventType: "predsx.trades.backfill", Data: evt.Data, Timestamp: ts,
	}
}

func flushBatch(ctx context.Context, ch clickhouse.Interface, batch []schemas.NormalizedEvent) {
	retry.Do(ctx, func() error {
		b, err := ch.PrepareBatch(ctx, "INSERT INTO events_raw (event_id, type, market_id, data, timestamp)")
		if err != nil {
			return err
		}
		for _, e := range batch {
			b.Append(e.EventID, e.EventType, e.MarketID, string(e.Data), e.Timestamp)
		}
		return b.Send()
	}, retry.DefaultOptions())
}

func insertOrderbookSnapshot(ctx context.Context, ch clickhouse.Interface, evt schemas.OrderbookUpdate) {
	bids, _ := json.Marshal(evt.Bids)
	asks, _ := json.Marshal(evt.Asks)
	bid := toFloat(evt.BestBid)
	ask := toFloat(evt.BestAsk)
	mid := 0.0
	if bid > 0 && ask > 0 {
		mid = (bid + ask) / 2
	}
	ch.Exec(ctx, `INSERT INTO orderbook_history (market_id, token, timestamp, best_bid, best_ask, mid, spread, bids, asks) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		evt.MarketID, evt.Token, time.Now(), bid, ask, mid, ask-bid, string(bids), string(asks))
}

func persistMarketMetadata(ctx context.Context, ch clickhouse.Interface, evt schemas.MarketDiscovered) {
	title := evt.Title
	if title == "" {
		title = evt.Question
	}
	outcomes, _ := json.Marshal(evt.Outcomes)
	ch.Exec(ctx, `INSERT INTO market_metadata (market_id, slug, title, question, condition_id, status, exchange, event_id, start_time, end_time, outcomes, created_at, raw) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		evt.ID, evt.Slug, title, evt.Question, evt.ConditionID, evt.Status, evt.Exchange, evt.EventID, evt.StartTime, evt.EndTime, string(outcomes), time.Now(), evt.Raw)
}

// --- BACKFILL COMPONENT ---

func startBackfillComponent(ctx context.Context, svc *service.BaseService, brokers string) {
	topic := config.GetEnv("TRADES_BACKFILL_TOPIC", "predsx.trades.backfill")
	producer := kafkaclient.NewTypedProducer[schemas.HistoricalEvent]([]string{brokers}, topic, svc.Logger)
	defer producer.Close()

	svc.Logger.Info("backfill component started")

	ticker := time.NewTicker(1 * time.Hour)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Full backfill logic...
		}
	}
}

// --- UTILS ---

func getString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func toFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	}
	return 0
}

func resolveCanonicalMarketID(ctx context.Context, rdb redisclient.Interface, marketRef string, token string) string {
	if marketRef != "" {
		if id, err := rdb.Get(ctx, "condition:"+marketRef+":market_id").Result(); err == nil && id != "" {
			return id
		}
	}
	if token != "" {
		if id, err := rdb.Get(ctx, "token:"+token+":market_id").Result(); err == nil && id != "" {
			return id
		}
	}
	return marketRef
}

func parsePayloadTimestamp(v interface{}) time.Time {
	raw := fmt.Sprintf("%v", v)
	if raw == "" || raw == "<nil>" {
		return time.Time{}
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n > 1_000_000_000_000 {
			return time.UnixMilli(n).UTC()
		}
		return time.Unix(n, 0).UTC()
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts.UTC()
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.UTC()
	}
	return time.Time{}
}
