package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	clickhouse "github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	"github.com/predsx/predsx/libs/crypto"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	redisclient "github.com/predsx/predsx/libs/redis-client"
	retry "github.com/predsx/predsx/libs/retry-utils"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

type Normalizer struct {
	svc           *service.BaseService
	redis         redisclient.Interface
	clickhouse    clickhouse.Interface
	batchSize     int
	flushInterval time.Duration
	buffer        []schemas.NormalizedEvent
	mu            sync.Mutex
	tradeProducer *kafkaclient.TypedProducer[schemas.TradeEvent]
}

func main() {
	svc := service.NewBaseService("normalizer")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Config
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
		chAddr := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase := config.GetEnv("CLICKHOUSE_DB", "default")
		batchSize := config.GetEnvInt("BATCH_SIZE", 500)
		flushInterval := time.Duration(config.GetEnvInt("FLUSH_INTERVAL_MS", 100)) * time.Millisecond

		// Clients
		rdb := redisclient.NewClient(redisclient.Options{Addr: redisAddr}, svc.Logger)

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

		// Auto-migrate: create events table with optimized schema
		if err := ensureSchema(ctx, ch); err != nil {
			return fmt.Errorf("failed to ensure clickhouse schema: %w", err)
		}
		svc.Logger.Info("clickhouse schema verified")

		wsTopic := config.GetEnv("WS_RAW_TOPIC", "predsx.ws.raw")
		backfillTopic := config.GetEnv("TRADES_BACKFILL_TOPIC", "predsx.trades.backfill")
		tradesLiveTopic := config.GetEnv("TRADES_LIVE_TOPIC", "predsx.trades.live")
		orderbookTopic := config.GetEnv("ORDERBOOK_UPDATES_TOPIC", "predsx.orderbook.updates")
		marketDiscoveryTopic := config.GetEnv("MARKET_DISCOVERY_TOPIC", "predsx.markets.discovered")

		n := &Normalizer{
			svc:           svc,
			redis:         rdb,
			clickhouse:    ch,
			batchSize:     batchSize,
			flushInterval: flushInterval,
			buffer:        make([]schemas.NormalizedEvent, 0, batchSize),
			tradeProducer: kafkaclient.NewTypedProducer[schemas.TradeEvent]([]string{kafkaBrokers}, tradesLiveTopic, svc.Logger),
		}
		defer n.tradeProducer.Close()

		go n.flushLoop(ctx)

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			wsTopic:              6,
			backfillTopic:        3,
			tradesLiveTopic:      6,
			orderbookTopic:       6,
			marketDiscoveryTopic: 1,
		}, svc.Logger)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.consumeRawWebsocket(ctx, kafkaBrokers, wsTopic)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.consumeBackfill(ctx, kafkaBrokers, backfillTopic)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.consumeOrderbookUpdates(ctx, kafkaBrokers, orderbookTopic)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.consumeMarketDiscovery(ctx, kafkaBrokers, marketDiscoveryTopic)
		}()

		wg.Wait()
		return nil
	})
}

// ensureSchema creates the events_raw table if it does not exist, using the
// Level-10 architecture schema.
func ensureSchema(ctx context.Context, ch clickhouse.Interface) error {
	err := ch.Exec(ctx, `
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

	if err != nil {
		return err
	}

	// Ensure TTL is applied for existing tables
	if err := ch.Exec(ctx, `
		ALTER TABLE events_raw MODIFY TTL timestamp + INTERVAL 3 DAY;
	`); err != nil {
		return err
	}
	if err := ensureMarketMetadataSchema(ctx, ch); err != nil {
		return err
	}
	return ensureOrderbookHistorySchema(ctx, ch)
}

func ensureMarketMetadataSchema(ctx context.Context, ch clickhouse.Interface) error {
	return ch.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS market_metadata (
			market_id    String,
			slug         String,
			title        String,
			question     String,
			condition_id String,
			status       String,
			exchange     String,
			event_id     String,
			start_time   DateTime64(3),
			end_time     DateTime64(3),
			outcomes     String,
			created_at   DateTime64(3),
			raw          String
		) ENGINE = MergeTree()
		PARTITION BY toDate(created_at)
		ORDER BY (market_id, created_at)
	`)
}

func ensureOrderbookHistorySchema(ctx context.Context, ch clickhouse.Interface) error {
	return ch.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS orderbook_history (
			market_id String,
			token     String,
			timestamp DateTime64(3),
			best_bid  Float64,
			best_ask  Float64,
			mid       Float64,
			spread    Float64,
			bids      String,
			asks      String
		) ENGINE = MergeTree()
		PARTITION BY toDate(timestamp)
		ORDER BY (market_id, timestamp)
	`)
}

func (n *Normalizer) consumeRawWebsocket(ctx context.Context, brokers, topic string) {
	groupID := config.GetEnv("KAFKA_GROUP_ID", "predsx-normalizer")
	consumer := kafkaclient.NewTypedConsumer[schemas.RawWebsocketEvent]([]string{brokers}, topic, groupID, n.svc.Logger)
	defer consumer.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			rawMsg, err := consumer.Fetch(ctx)
			if err != nil {
				n.svc.Logger.Error("fetch error", "topic", topic, "error", err)
				continue
			}
			n.processRawWebsocketEvent(ctx, rawMsg)
		}
	}
}

func (n *Normalizer) consumeBackfill(ctx context.Context, brokers, topic string) {
	groupID := config.GetEnv("KAFKA_GROUP_ID", "predsx-normalizer")
	consumer := kafkaclient.NewTypedConsumer[schemas.HistoricalEvent]([]string{brokers}, topic, groupID, n.svc.Logger)
	defer consumer.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := consumer.Fetch(ctx)
			if err != nil {
				n.svc.Logger.Error("fetch error", "topic", topic, "error", err)
				continue
			}
			n.processBackfillEvent(ctx, topic, msg)
		}
	}
}

func (n *Normalizer) processRawWebsocketEvent(ctx context.Context, raw schemas.RawWebsocketEvent) {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw.Payload, &payload); err != nil {
		return
	}

	eventType, _ := payload["event_type"].(string)
	if eventType != "last_trade_price" && eventType != "price_change" {
		return
	}

	if eventType == "price_change" {
		if changes, ok := payload["price_changes"].([]interface{}); ok {
			for _, change := range changes {
				entry, ok := change.(map[string]interface{})
				if !ok {
					continue
				}
				rawTrade := map[string]interface{}{
					"market_id": getString(payload, "market_id", "market", "condition_id"),
					"token":     getString(entry, "asset_id", "asset", "token"),
					"price":     entry["price"],
					"size":      entry["size"],
					"side":      entry["side"],
					"timestamp": payload["timestamp"],
				}
				if rawTrade["market_id"] == "" {
					continue
				}
				trade, ok := buildTradeEvent(rawTrade)
				if ok {
					if err := n.tradeProducer.Publish(ctx, trade.MarketID, trade); err != nil {
						n.svc.Logger.Error("failed to publish live trade", "error", err)
					}
				}
				n.processEvent(ctx, "predsx.trades", rawTrade, "predsx.trades")
			}
		}
		return
	}

	rawTrade := map[string]interface{}{
		"market_id": getString(payload, "market_id", "market", "condition_id"),
		"token":     getString(payload, "asset_id", "asset", "token"),
		"price":     payload["price"],
		"size":      payload["size"],
		"side":      payload["side"],
		"timestamp": payload["timestamp"],
	}
	if rawTrade["size"] == nil {
		rawTrade["size"] = payload["amount"]
	}

	if rawTrade["market_id"] == "" {
		return
	}

	trade, ok := buildTradeEvent(rawTrade)
	if ok {
		if err := n.tradeProducer.Publish(ctx, trade.MarketID, trade); err != nil {
			n.svc.Logger.Error("failed to publish live trade", "error", err)
		}
	}

	n.processEvent(ctx, "predsx.trades", rawTrade, "predsx.trades")
}

func (n *Normalizer) processBackfillEvent(ctx context.Context, topic string, evt schemas.HistoricalEvent) {
	var payload map[string]interface{}
	if err := json.Unmarshal(evt.Data, &payload); err != nil {
		return
	}

	if payload["market_id"] == nil {
		payload["market_id"] = evt.MarketID
	}
	if payload["timestamp"] == nil {
		payload["timestamp"] = evt.Timestamp.Format(time.RFC3339Nano)
	}
	if evt.Source != "" {
		payload["source"] = evt.Source
	}

	eventType := topic
	if evt.EventType != "" {
		eventType = evt.EventType
	}
	n.processEvent(ctx, topic, payload, eventType)
}

func (n *Normalizer) consumeOrderbookUpdates(ctx context.Context, brokers, topic string) {
	groupID := config.GetEnv("NORMALIZER_ORDERBOOK_GROUP", "predsx-normalizer-orderbook")
	consumer := kafkaclient.NewTypedConsumer[schemas.OrderbookUpdate]([]string{brokers}, topic, groupID, n.svc.Logger)
	defer consumer.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		evt, err := consumer.Fetch(ctx)
		if err != nil {
			n.svc.Logger.Error("orderbook fetch error", "topic", topic, "error", err)
			continue
		}
		n.insertOrderbookSnapshot(ctx, evt)
	}
}

func (n *Normalizer) consumeMarketDiscovery(ctx context.Context, brokers, topic string) {
	groupID := config.GetEnv("NORMALIZER_MARKET_DISCOVERY_GROUP", "predsx-normalizer-market")
	consumer := kafkaclient.NewTypedConsumer[schemas.MarketDiscovered]([]string{brokers}, topic, groupID, n.svc.Logger)
	defer consumer.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		evt, err := consumer.Fetch(ctx)
		if err != nil {
			n.svc.Logger.Error("market discovery fetch error", "topic", topic, "error", err)
			continue
		}
		n.persistMarketMetadata(ctx, evt)
	}
}

func (n *Normalizer) persistMarketMetadata(ctx context.Context, evt schemas.MarketDiscovered) {
	outcomes := "[]"
	if len(evt.Outcomes) > 0 {
		if b, err := json.Marshal(evt.Outcomes); err == nil {
			outcomes = string(b)
		}
	}
	if err := n.clickhouse.Exec(ctx, `
		INSERT INTO market_metadata (
			market_id, slug, title, question, condition_id,
			status, exchange, event_id, start_time, end_time,
			outcomes, created_at, raw
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, evt.ID, evt.Slug, evt.Title, evt.Question, evt.ConditionID,
		evt.Status, evt.Exchange, evt.EventID, evt.StartTime, evt.EndTime,
		outcomes, evt.CreatedAt, evt.Raw); err != nil {
		n.svc.Logger.Error("failed to insert market metadata", "market_id", evt.ID, "error", err)
	}
}

func (n *Normalizer) insertOrderbookSnapshot(ctx context.Context, evt schemas.OrderbookUpdate) {
	now := evt.Timestamp
	if now.IsZero() {
		now = time.Now().UTC()
	}
	bids, _ := json.Marshal(evt.Bids)
	asks, _ := json.Marshal(evt.Asks)
	bestBid := toFloat(evt.BestBid)
	bestAsk := toFloat(evt.BestAsk)
	mid := 0.0
	if bestBid > 0 && bestAsk > 0 {
		mid = (bestBid + bestAsk) / 2
	}
	spread := 0.0
	if bestBid > 0 && bestAsk > 0 {
		spread = bestAsk - bestBid
	}
	if err := n.clickhouse.Exec(ctx, `
		INSERT INTO orderbook_history (
			market_id, token, timestamp,
			best_bid, best_ask, mid, spread,
			bids, asks
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, evt.MarketID, evt.Token, now, bestBid, bestAsk, mid, spread, string(bids), string(asks)); err != nil {
		n.svc.Logger.Error("failed to insert orderbook snapshot", "market_id", evt.MarketID, "error", err)
	}
}

func (n *Normalizer) processEvent(ctx context.Context, topic string, raw map[string]interface{}, eventType string) {
	marketID, _ := raw["market_id"].(string)
	if marketID == "" {
		return
	}

	// 1. Determine Event Identity
	// If the raw event already has an event_id (e.g., from an upstream component) we could use it,
	// but we strictly compute the SHA1 hash based on Level-10 spec:
	// event_id = SHA1(market_id + timestamp + price + size + side)

	// Safely extract fields for hash generation
	ts := time.Now().UTC()
	if rawTsStr, ok := raw["timestamp"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, rawTsStr); err == nil {
			ts = parsed
		}
	} else if rawTsFloat, ok := raw["timestamp"].(float64); ok { // sometimes JSON parses as float Unix
		ts = time.Unix(int64(rawTsFloat/1000), 0)
	}
	tsStr := fmt.Sprintf("%d", ts.UnixNano())

	priceStr := fmt.Sprintf("%v", raw["price"])
	sizeStr := fmt.Sprintf("%v", raw["size"])
	sideStr := fmt.Sprintf("%v", raw["side"])

	eventID := crypto.GenerateEventID(marketID, tsStr, priceStr, sizeStr, sideStr)

	// 2. Redis Deduplication
	// SETNX predsx:dedup:{event_id} "1" EX 48h
	dedupKey := fmt.Sprintf("predsx:dedup:%s", eventID)
	set, err := n.redis.SetNX(ctx, dedupKey, "1", 48*time.Hour).Result()
	if err != nil {
		n.svc.Logger.Warn("deduplication check error", "error", err, "event_id", eventID)
		// Continue processing safely even if Redis goes down briefly
	} else if !set {
		// Event ID exists => duplicate, skip processing entirely
		return
	}

	// 3. Enrich with metadata from Redis
	metadata := make(map[string]string)
	val, err := n.redis.Get(ctx, fmt.Sprintf("market:%s:metadata", marketID)).Result()
	if err == nil {
		json.Unmarshal([]byte(val), &metadata)
	}

	// Propagate source tag (e.g. "backfill") if present in the event
	if src, ok := raw["source"].(string); ok && src != "" {
		metadata["source"] = src
	}

	// 4. Normalize
	data, _ := json.Marshal(raw)
	if eventType == "" {
		eventType = topic
	}

	event := schemas.NormalizedEvent{
		EventID:   eventID,
		EventType: eventType,
		MarketID:  marketID,
		Data:      data,
		Metadata:  metadata,
		Timestamp: ts,
		Version:   schemas.VersionV1,
	}

	// 6. Update Redis Live State (e.g., last price)
	if topic == "predsx.prices" {
		n.redis.Set(ctx, fmt.Sprintf("live:price:%s", marketID), data, 0)
	}

	// 7. Buffer for ClickHouse
	n.mu.Lock()
	n.buffer = append(n.buffer, event)
	if len(n.buffer) >= n.batchSize {
		n.flush(ctx)
	}
	n.mu.Unlock()
}

func buildTradeEvent(raw map[string]interface{}) (schemas.TradeEvent, bool) {
	marketID, _ := raw["market_id"].(string)
	if marketID == "" {
		return schemas.TradeEvent{}, false
	}

	token, _ := raw["token"].(string)
	price := toFloat(raw["price"])
	if price <= 0 {
		return schemas.TradeEvent{}, false
	}
	size := toFloat(raw["size"])
	side, _ := raw["side"].(string)

	ts := time.Now().UTC()
	if rawTsStr, ok := raw["timestamp"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, rawTsStr); err == nil {
			ts = parsed
		}
	} else if rawTsFloat, ok := raw["timestamp"].(float64); ok {
		ts = time.Unix(int64(rawTsFloat/1000), 0)
	}

	tradeID := ""
	if id, ok := raw["trade_id"].(string); ok {
		tradeID = id
	} else if id, ok := raw["id"].(string); ok {
		tradeID = id
	} else {
		tsStr := fmt.Sprintf("%d", ts.UnixNano())
		tradeID = crypto.GenerateEventID(marketID, tsStr, fmt.Sprintf("%v", price), fmt.Sprintf("%v", size), fmt.Sprintf("%v", side))
	}

	return schemas.TradeEvent{
		TradeID:   tradeID,
		Token:     token,
		MarketID:  marketID,
		Price:     price,
		Size:      size,
		Side:      side,
		Timestamp: ts,
		Version:   schemas.VersionV1,
	}, true
}

func getString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch t := v.(type) {
			case string:
				return t
			case float64:
				return fmt.Sprintf("%v", t)
			}
		}
	}
	return ""
}

func toFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f
		}
	}
	return 0
}

func (n *Normalizer) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(n.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.mu.Lock()
			n.flush(ctx)
			n.mu.Unlock()
		}
	}
}

// flush writes the buffered events to ClickHouse using PrepareBatch for
// efficient bulk ingestion. ClickHouse is optimised for large batch inserts;
// per-row inserts would cause merge storms under any real load.
func (n *Normalizer) flush(ctx context.Context) {
	if len(n.buffer) == 0 {
		return
	}

	batch := n.buffer
	n.buffer = make([]schemas.NormalizedEvent, 0, n.batchSize)

	start := time.Now()

	err := retry.Do(ctx, func() error {
		b, err := n.clickhouse.PrepareBatch(ctx,
			"INSERT INTO events_raw (event_id, type, market_id, data, metadata, timestamp)")
		if err != nil {
			return fmt.Errorf("prepare batch: %w", err)
		}

		for _, e := range batch {
			metaBytes, _ := json.Marshal(e.Metadata)
			if err := b.Append(
				e.EventID,
				e.EventType,
				e.MarketID,
				string(e.Data),
				string(metaBytes),
				e.Timestamp,
			); err != nil {
				n.svc.Logger.Warn("batch append failed", "error", err,
					"market_id", e.MarketID, "event_type", e.EventType)
			}
		}

		return b.Send()
	}, retry.DefaultOptions())

	if err != nil {
		n.svc.Logger.Error("clickhouse batch send failed after retries",
			"error", err,
			"batch_size", len(batch))
		return
	}

	n.svc.Logger.Info("flushed events to clickhouse",
		"batch_size", len(batch),
		"latency_ms", time.Since(start).Milliseconds())
}
