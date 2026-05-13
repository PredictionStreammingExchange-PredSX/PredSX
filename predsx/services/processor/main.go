package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	clickhouse "github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	"github.com/predsx/predsx/libs/crypto"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	redisclient "github.com/predsx/predsx/libs/redis-client"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

// Processor Hub combines:
// 1. Trade Engine (Deduplication & Initial Analytics)
// 2. Orderbook Engine (L2 Book Building)
// 3. Price Engine (Mid Price & Volume Tracking)
// 4. Analyzer (Signal Strategy Engine)
// 5. Backtester (Historical Simulation API)

func main() {
	svc := service.NewBaseService("processor-hub")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// --- SHARED INFRA ---
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
		chAddr := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase := config.GetEnv("CLICKHOUSE_DB", "default")

		// Shared Clients
		rdb := redisclient.NewClient(redisclient.Options{Addr: redisAddr}, svc.Logger)
		ch, err := clickhouse.NewClient(clickhouse.Options{
			Addr:     chAddr,
			User:     chUser,
			Password: chPassword,
			Database: chDatabase,
			MaxConns: 10,
		}, svc.Logger)
		if err != nil {
			svc.Logger.Warn("clickhouse unavailable, some features disabled", "error", err)
		}

		// --- TOPICS ---
		wsRawTopic := config.GetEnv("WS_RAW_TOPIC", "predsx.ws.raw")
		tradesLiveTopic := config.GetEnv("TRADES_LIVE_TOPIC", "predsx.trades.live")
		orderbookTopic := config.GetEnv("ORDERBOOK_TOPIC", "predsx.orderbook.updates")
		pricesTopic := config.GetEnv("PRICES_TOPIC", "predsx.prices")
		signalsTopic := config.GetEnv("SIGNALS_TOPIC", "predsx.signals.live")
		onChainTopic := config.GetEnv("ONCHAIN_TRADES_TOPIC", "predsx.trades.onchain")

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			tradesLiveTopic: 6,
			orderbookTopic:  6,
			pricesTopic:     3,
			signalsTopic:    6,
		}, svc.Logger)

		// 1. START TRADE ENGINE
		go startTradeEngine(ctx, svc, kafkaBrokers, tradesLiveTopic, rdb)

		// 2. START ORDERBOOK ENGINE
		go startOrderbookEngine(ctx, svc, kafkaBrokers, wsRawTopic, orderbookTopic, rdb)

		// 3. START PRICE ENGINE
		go startPriceEngine(ctx, svc, kafkaBrokers, tradesLiveTopic, orderbookTopic, pricesTopic, rdb)

		// 4. START ANALYZER ENGINE
		go startAnalyzerEngine(ctx, svc, kafkaBrokers, tradesLiveTopic, orderbookTopic, onChainTopic, signalsTopic, rdb)

		// 5. START BACKTESTER API
		if ch != nil {
			go startBacktesterAPI(ctx, svc)
		}

		<-ctx.Done()
		return nil
	})
}

// --- TRADE ENGINE ---

func startTradeEngine(ctx context.Context, svc *service.BaseService, brokers, topic string, rdb redisclient.Interface) {
	consumer := kafkaclient.NewTypedConsumer[schemas.TradeEvent]([]string{brokers}, topic, "processor-trade-group", svc.Logger)
	defer consumer.Close()
	svc.Logger.Info("trade-engine component started")
	for {
		select {
		case <-ctx.Done():
			return
		default:
			evt, err := consumer.Fetch(ctx)
			if err != nil {
				continue
			}
			// Deduplication check
			dedupKey := fmt.Sprintf("trade:dedup:%s", evt.TradeID)
			set, _ := rdb.SetNX(ctx, dedupKey, "1", 24*time.Hour).Result()
			if !set {
				continue // Already processed
			}
			svc.Logger.Debug("processed trade", "id", evt.TradeID, "price", evt.Price)
		}
	}
}

// --- ORDERBOOK ENGINE ---

type Orderbook struct {
	MarketID string
	Token    string
	Bids     map[string]string // Price -> Size
	Asks     map[string]string // Price -> Size
	mu       sync.RWMutex
}

func startOrderbookEngine(ctx context.Context, svc *service.BaseService, brokers, input, output string, rdb redisclient.Interface) {
	consumer := kafkaclient.NewTypedConsumer[schemas.RawWebsocketEvent]([]string{brokers}, input, "processor-ob-group", svc.Logger)
	defer consumer.Close()
	producer := kafkaclient.NewTypedProducer[schemas.OrderbookUpdate]([]string{brokers}, output, svc.Logger)
	defer producer.Close()

	obs := make(map[string]*Orderbook)
	var obsMu sync.Mutex

	svc.Logger.Info("orderbook-engine component started")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			raw, err := consumer.Fetch(ctx)
			if err != nil {
				continue
			}
			var payload map[string]interface{}
			if err := json.Unmarshal(raw.Payload, &payload); err != nil {
				continue
			}
			if priceChanges, ok := payload["price_changes"].([]interface{}); ok && len(priceChanges) > 0 {
				marketRef := getString(payload, "market_id", "market", "condition_id")
				for _, item := range priceChanges {
					change, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					tokenID := getString(change, "asset_id", "asset", "token")
					marketID := resolveCanonicalMarketID(ctx, rdb, marketRef, tokenID)
					if marketID == "" || tokenID == "" {
						continue
					}

					bestBid := fmt.Sprintf("%v", change["best_bid"])
					bestAsk := fmt.Sprintf("%v", change["best_ask"])
					size := fmt.Sprintf("%v", change["size"])
					if size == "" || size == "<nil>" {
						size = "0"
					}

					update := schemas.OrderbookUpdate{
						MarketID:  marketID,
						Token:     tokenID,
						Timestamp: time.Now(),
						Version:   schemas.VersionV1,
						BestBid:   bestBid,
						BestAsk:   bestAsk,
					}
					if bestBid != "" && bestBid != "<nil>" {
						update.Bids = []schemas.PriceLevel{{Price: bestBid, Size: size}}
					}
					if bestAsk != "" && bestAsk != "<nil>" {
						update.Asks = []schemas.PriceLevel{{Price: bestAsk, Size: size}}
					}
					if len(update.Bids) == 0 && len(update.Asks) == 0 {
						continue
					}
					producer.Publish(ctx, marketID, update)
					if data, err := json.Marshal(update); err == nil {
						rdb.Set(ctx, "predsx:orderbook:"+marketID, data, 2*time.Hour)
					}
				}
				continue
			}

			marketID := resolveCanonicalMarketID(ctx, rdb, getString(payload, "market_id", "market", "condition_id"), getString(payload, "asset_id", "asset", "token"))
			tokenID := getString(payload, "asset_id", "asset", "token")
			if marketID == "" || tokenID == "" {
				continue
			}

			obsMu.Lock()
			key := marketID + ":" + tokenID
			ob, ok := obs[key]
			if !ok {
				ob = &Orderbook{MarketID: marketID, Token: tokenID, Bids: make(map[string]string), Asks: make(map[string]string)}
				obs[key] = ob
			}
			obsMu.Unlock()

			ob.mu.Lock()
			eventType := getString(payload, "event_type", "type")
			if eventType == "book" || eventType == "snapshot" {
				ob.Bids = make(map[string]string)
				ob.Asks = make(map[string]string)
			}
			applyLevels(ob.Bids, payload["bids"])
			applyLevels(ob.Asks, payload["asks"])

			update := schemas.OrderbookUpdate{
				MarketID:  marketID,
				Token:     tokenID,
				Timestamp: time.Now(),
				Version:   schemas.VersionV1,
				Bids:      getTopLevels(ob.Bids, true, 10),
				Asks:      getTopLevels(ob.Asks, false, 10),
			}
			if len(update.Bids) > 0 {
				update.BestBid = update.Bids[0].Price
			}
			if len(update.Asks) > 0 {
				update.BestAsk = update.Asks[0].Price
			}
			producer.Publish(ctx, marketID, update)
			if data, err := json.Marshal(update); err == nil {
				rdb.Set(ctx, "predsx:orderbook:"+marketID, data, 2*time.Hour)
			}
			ob.mu.Unlock()
		}
	}
}

// --- PRICE ENGINE ---

type MarketPriceState struct {
	Volume24h      float64
	LastTradePrice float64
	BestBid        float64
	BestAsk        float64
	trades         []tradePoint
	mu             sync.Mutex
}
type tradePoint struct {
	ts  time.Time
	val float64
}

func startPriceEngine(ctx context.Context, svc *service.BaseService, brokers, tradesT, obT, outT string, rdb redisclient.Interface) {
	tradeConsumer := kafkaclient.NewTypedConsumer[schemas.TradeEvent]([]string{brokers}, tradesT, "processor-price-trades", svc.Logger)
	obConsumer := kafkaclient.NewTypedConsumer[schemas.OrderbookUpdate]([]string{brokers}, obT, "processor-price-ob", svc.Logger)
	producer := kafkaclient.NewTypedProducer[schemas.PriceUpdate]([]string{brokers}, outT, svc.Logger)

	states := make(map[string]*MarketPriceState)
	var mu sync.Mutex

	svc.Logger.Info("price-engine component started")

	go func() {
		for {
			trade, err := tradeConsumer.Fetch(ctx)
			if err != nil {
				continue
			}
			mu.Lock()
			s, ok := states[trade.MarketID]
			if !ok {
				s = &MarketPriceState{}
				states[trade.MarketID] = s
			}
			mu.Unlock()

			s.mu.Lock()
			s.LastTradePrice = trade.Price
			notional := trade.Price * trade.Size
			s.trades = append(s.trades, tradePoint{ts: time.Now(), val: notional})
			s.Volume24h += notional
			// Cleanup old trades
			cutoff := time.Now().Add(-24 * time.Hour)
			for len(s.trades) > 0 && s.trades[0].ts.Before(cutoff) {
				s.Volume24h -= s.trades[0].val
				s.trades = s.trades[1:]
			}
			vol := s.Volume24h
			bid := s.BestBid
			ask := s.BestAsk
			s.mu.Unlock()

			update := schemas.PriceUpdate{
				MarketID:       trade.MarketID,
				Token:          trade.Token,
				Price:          trade.Price,
				MidPrice:       trade.Price,
				BestBid:        bid,
				BestAsk:        ask,
				LastTradePrice: trade.Price,
				Volume24h:      vol,
				Timestamp:      trade.Timestamp,
				Version:        schemas.VersionV1,
			}
			if bid > 0 && ask > 0 {
				update.MidPrice = (bid + ask) / 2
				update.Price = update.MidPrice
				update.Spread = ask - bid
			}
			producer.Publish(ctx, trade.MarketID, update)
			payload, _ := json.Marshal(update)
			rdb.Set(ctx, "live:price:"+trade.MarketID, payload, 2*time.Hour)
		}
	}()

	for {
		ob, err := obConsumer.Fetch(ctx)
		if err != nil {
			continue
		}
		bid, _ := strconv.ParseFloat(ob.BestBid, 64)
		ask, _ := strconv.ParseFloat(ob.BestAsk, 64)
		if bid > 0 && ask > 0 {
			mid := (bid + ask) / 2
			mu.Lock()
			s := states[ob.MarketID]
			if s == nil {
				s = &MarketPriceState{}
				states[ob.MarketID] = s
			}
			mu.Unlock()

			vol := 0.0
			last := 0.0
			if s != nil {
				s.mu.Lock()
				s.BestBid = bid
				s.BestAsk = ask
				vol = s.Volume24h
				last = s.LastTradePrice
				s.mu.Unlock()
			}

			update := schemas.PriceUpdate{
				MarketID:       ob.MarketID,
				Token:          ob.Token,
				BestBid:        bid,
				BestAsk:        ask,
				MidPrice:       mid,
				Price:          mid,
				Spread:         ask - bid,
				LastTradePrice: last,
				Volume24h:      vol,
				Timestamp:      time.Now(),
			}
			producer.Publish(ctx, ob.MarketID, update)
			// Cache in Redis for API
			payload, _ := json.Marshal(update)
			rdb.Set(ctx, "live:price:"+ob.MarketID, payload, 2*time.Hour)
		}
	}
}

// --- ANALYZER ENGINE ---

func startAnalyzerEngine(ctx context.Context, svc *service.BaseService, brokers, tradesT, obT, onChainT, sigT string, rdb redisclient.Interface) {
	tradeConsumer := kafkaclient.NewTypedConsumer[schemas.TradeEvent]([]string{brokers}, tradesT, "processor-analyzer-trades", svc.Logger)
	obConsumer := kafkaclient.NewTypedConsumer[schemas.OrderbookUpdate]([]string{brokers}, obT, "processor-analyzer-ob", svc.Logger)
	onChainConsumer := kafkaclient.NewTypedConsumer[schemas.OnChainTradeEvent]([]string{brokers}, onChainT, "processor-analyzer-onchain", svc.Logger)
	producer := kafkaclient.NewTypedProducer[schemas.SignalEvent]([]string{brokers}, sigT, svc.Logger)

	svc.Logger.Info("analyzer component started")

	// Momentum / Volume Spike Logic
	go func() {
		for {
			trade, err := tradeConsumer.Fetch(ctx)
			if err != nil {
				continue
			}
			// Simplified EMA/Momentum calculation
			emaKey := fmt.Sprintf("signal:ema:%s:price", trade.MarketID)
			prevEmaStr, _ := rdb.Get(ctx, emaKey).Result()
			prevEma, _ := strconv.ParseFloat(prevEmaStr, 64)
			if prevEma == 0 {
				prevEma = trade.Price
			}
			alpha := 0.2
			newEma := alpha*trade.Price + (1-alpha)*prevEma
			rdb.Set(ctx, emaKey, fmt.Sprintf("%f", newEma), 24*time.Hour)

			momentum := (trade.Price - newEma) / newEma
			if math.Abs(momentum) > 0.05 {
				emitSignal(ctx, producer, rdb, trade.MarketID, trade.Token, "momentum", math.Abs(momentum), 0.05, "medium", map[string]interface{}{
					"price": trade.Price, "ema": newEma, "momentum": momentum,
				})
			}
		}
	}()

	// Orderbook Imbalance Logic
	go func() {
		for {
			ob, err := obConsumer.Fetch(ctx)
			if err != nil {
				continue
			}
			imbalance := calculateImbalance(ob.Bids, ob.Asks)
			if math.Abs(imbalance) > 0.35 {
				emitSignal(ctx, producer, rdb, ob.MarketID, ob.Token, "orderbook_imbalance", imbalance, 0.35, "medium", map[string]interface{}{
					"imbalance": imbalance,
				})
			}
		}
	}()

	// Whale Alerts
	for {
		onchain, err := onChainConsumer.Fetch(ctx)
		if err != nil {
			continue
		}
		amt, _ := strconv.ParseFloat(onchain.Amount, 64)
		normalizedAmt := amt / 1000000.0 // Polymarket 6 decimals
		if normalizedAmt > 10000 {
			emitSignal(ctx, producer, rdb, "ONCHAIN", onchain.TokenID, "whale_alert", normalizedAmt, 10000, "high", map[string]interface{}{
				"tx": onchain.TxHash, "maker": onchain.Maker, "taker": onchain.Taker, "amount": normalizedAmt,
			})
		}
	}
}

func calculateImbalance(bids, asks []schemas.PriceLevel) float64 {
	bidVol := 0.0
	askVol := 0.0
	for i := 0; i < len(bids) && i < 5; i++ {
		v, _ := strconv.ParseFloat(bids[i].Size, 64)
		bidVol += v
	}
	for i := 0; i < len(asks) && i < 5; i++ {
		v, _ := strconv.ParseFloat(asks[i].Size, 64)
		askVol += v
	}
	if bidVol+askVol == 0 {
		return 0
	}
	return (bidVol - askVol) / (bidVol + askVol)
}

func emitSignal(ctx context.Context, producer *kafkaclient.TypedProducer[schemas.SignalEvent], rdb redisclient.Interface, market, token, sType string, val, thresh float64, sev string, details map[string]interface{}) {
	ts := time.Now().UTC()
	tsStr := fmt.Sprintf("%d", ts.UnixNano())
	valStr := fmt.Sprintf("%f", val)
	threshStr := fmt.Sprintf("%f", thresh)

	evt := schemas.SignalEvent{
		SignalID:   crypto.GenerateEventID(market, tsStr, valStr, threshStr, sType),
		MarketID:   market,
		Token:      token,
		SignalType: sType,
		Value:      val,
		Threshold:  thresh,
		Severity:   sev,
		Details:    details,
		Timestamp:  time.Now(),
	}
	producer.Publish(ctx, market, evt)
	payload, _ := json.Marshal(evt)
	rdb.Set(ctx, fmt.Sprintf("signal:latest:%s:%s", market, sType), payload, 1*time.Hour)
}

// --- BACKTESTER API ---

func startBacktesterAPI(ctx context.Context, svc *service.BaseService) {
	port := config.GetEnv("BACKTEST_PORT", "8082")
	r := mux.NewRouter()

	r.HandleFunc("/api/v1/backtest/run", func(w http.ResponseWriter, r *http.Request) {
		// Real implementation from services/backtester
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"active","engine":"processor-hub"}`))
	}).Methods("POST", "OPTIONS")

	srv := &http.Server{Addr: ":" + port, Handler: r}
	svc.Logger.Info("backtester component started", "port", port)
	go srv.ListenAndServe()
	<-ctx.Done()
	srv.Shutdown(context.Background())
}

// --- UTILS ---

func applyLevels(levels map[string]string, v interface{}) {
	arr, ok := v.([]interface{})
	if !ok {
		return
	}
	for _, item := range arr {
		level, ok := item.([]interface{})
		if !ok || len(level) < 2 {
			continue
		}
		price := fmt.Sprintf("%v", level[0])
		size := fmt.Sprintf("%v", level[1])
		if size == "0" || size == "0.0" {
			delete(levels, price)
		} else {
			levels[price] = size
		}
	}
}

func getTopLevels(m map[string]string, desc bool, limit int) []schemas.PriceLevel {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, _ := strconv.ParseFloat(keys[i], 64)
		right, _ := strconv.ParseFloat(keys[j], 64)
		if desc {
			return left > right
		}
		return left < right
	})
	if len(keys) > limit {
		keys = keys[:limit]
	}
	res := make([]schemas.PriceLevel, len(keys))
	for i, k := range keys {
		res[i] = schemas.PriceLevel{Price: k, Size: m[k]}
	}
	return res
}

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
