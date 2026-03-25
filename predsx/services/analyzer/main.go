package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	clickhouse "github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	"github.com/predsx/predsx/libs/crypto"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	redisclient "github.com/predsx/predsx/libs/redis-client"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

type Analyzer struct {
	svc            *service.BaseService
	redis          redisclient.Interface
	clickhouse     clickhouse.Interface
	signalProducer *kafkaclient.TypedProducer[schemas.SignalEvent]

	emaAlpha            float64
	imbalanceDepth      int
	spreadThreshold     float64
	imbalanceThreshold  float64
	liquidityGapThresh  float64
	arbitrageThreshold  float64
	emitAll             bool

	histTTL  time.Duration
	histMu   sync.Mutex
	histCache map[string]historicalStats

	tokenMu    sync.Mutex
	tokenCache map[string]tokenCacheEntry

	midMu   sync.Mutex
	lastMid map[string]float64
}

type historicalStats struct {
	avgPrice   float64
	stdPrice   float64
	tradeCount uint64
	fetchedAt  time.Time
}

type tokenCacheEntry struct {
	yes string
	no  string
	at  time.Time
}

func main() {
	svc := service.NewBaseService("analyzer")

	svc.Run(context.Background(), func(ctx context.Context) error {
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		tradesTopic := config.GetEnv("TRADES_LIVE_TOPIC", "predsx.trades.live")
		orderbookTopic := config.GetEnv("ORDERBOOK_UPDATES_TOPIC", "predsx.orderbook.updates")
		signalsTopic := config.GetEnv("SIGNALS_TOPIC", "predsx.signals.live")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
		chAddr := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase := config.GetEnv("CLICKHOUSE_DB", "default")

		emaAlpha := getEnvFloat("EMA_ALPHA", 0.2)
		imbalanceDepth := config.GetEnvInt("IMBALANCE_DEPTH", 10)
		spreadThreshold := getEnvFloat("SPREAD_THRESHOLD", 0.03)
		imbalanceThreshold := getEnvFloat("IMBALANCE_THRESHOLD", 0.35)
		liquidityGapThresh := getEnvFloat("LIQUIDITY_GAP_THRESHOLD", 0.05)
		arbitrageThreshold := getEnvFloat("ARBITRAGE_THRESHOLD", 0.02)
		emitAll := config.GetEnv("SIGNAL_EMIT_ALL", "") == "1"
		histTTL := time.Duration(config.GetEnvInt("HIST_CONTEXT_TTL_SEC", 60)) * time.Second

		rdb := redisclient.NewClient(redisclient.Options{Addr: redisAddr}, svc.Logger)

		ch, err := clickhouse.NewClient(clickhouse.Options{
			Addr:     chAddr,
			User:     chUser,
			Password: chPassword,
			Database: chDatabase,
			MaxConns: 10,
		}, svc.Logger)
		if err != nil {
			svc.Logger.Warn("clickhouse unavailable, historical context disabled", "error", err)
		}

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			tradesTopic:    6,
			orderbookTopic: 6,
			signalsTopic:   6,
		}, svc.Logger)

		an := &Analyzer{
			svc:               svc,
			redis:             rdb,
			clickhouse:        ch,
			signalProducer:    kafkaclient.NewTypedProducer[schemas.SignalEvent]([]string{kafkaBrokers}, signalsTopic, svc.Logger),
			emaAlpha:          emaAlpha,
			imbalanceDepth:    imbalanceDepth,
			spreadThreshold:   spreadThreshold,
			imbalanceThreshold: imbalanceThreshold,
			liquidityGapThresh: liquidityGapThresh,
			arbitrageThreshold: arbitrageThreshold,
			emitAll:            emitAll,
			histTTL:            histTTL,
			histCache:          make(map[string]historicalStats),
			tokenCache:         make(map[string]tokenCacheEntry),
			lastMid:            make(map[string]float64),
		}
		defer an.signalProducer.Close()

		svc.Logger.Info("analyzer started",
			"trades_topic", tradesTopic,
			"orderbook_topic", orderbookTopic,
			"signals_topic", signalsTopic)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			an.consumeTrades(ctx, kafkaBrokers, tradesTopic)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			an.consumeOrderbook(ctx, kafkaBrokers, orderbookTopic)
		}()

		wg.Wait()
		return nil
	})
}

func (a *Analyzer) consumeTrades(ctx context.Context, brokers, topic string) {
	consumer := kafkaclient.NewTypedConsumer[schemas.TradeEvent]([]string{brokers}, topic, "analyzer-trades", a.svc.Logger)
	defer consumer.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		evt, err := consumer.Fetch(ctx)
		if err != nil {
			a.svc.Logger.Error("trade fetch error", "error", err)
			continue
		}
		// Store last trade price for context
		if evt.Token != "" {
			a.storeMid(evt.Token, evt.Price)
		}
	}
}

func (a *Analyzer) consumeOrderbook(ctx context.Context, brokers, topic string) {
	consumer := kafkaclient.NewTypedConsumer[schemas.OrderbookUpdate]([]string{brokers}, topic, "analyzer-orderbook", a.svc.Logger)
	defer consumer.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		evt, err := consumer.Fetch(ctx)
		if err != nil {
			a.svc.Logger.Error("orderbook fetch error", "error", err)
			continue
		}
		a.processOrderbook(ctx, evt)
	}
}

func (a *Analyzer) processOrderbook(ctx context.Context, evt schemas.OrderbookUpdate) {
	bid, ask := bestPrices(evt.Bids, evt.Asks)
	if bid <= 0 || ask <= 0 || ask < bid {
		return
	}
	mid := (bid + ask) / 2
	a.storeMid(evt.Token, mid)

	spread := ask - bid
	imbalance := orderbookImbalance(evt.Bids, evt.Asks, a.imbalanceDepth)
	liquidityGap := maxLiquidityGap(evt.Bids, evt.Asks)

	hist := a.getHistoricalStats(ctx, evt.MarketID)
	emaSpread := a.updateEMA(ctx, evt.MarketID, "spread", spread)
	emaImb := a.updateEMA(ctx, evt.MarketID, "imbalance", imbalance)
	emaGap := a.updateEMA(ctx, evt.MarketID, "liquidity_gap", liquidityGap)

	detailsBase := map[string]interface{}{
		"best_bid": bid,
		"best_ask": ask,
		"mid":      mid,
		"ema_spread": emaSpread,
		"ema_imbalance": emaImb,
		"ema_liquidity_gap": emaGap,
		"avg_price_1h": hist.avgPrice,
		"std_price_1h": hist.stdPrice,
		"trade_count_1h": hist.tradeCount,
	}

	if a.emitAll || spread >= a.spreadThreshold {
		a.emitSignal(ctx, evt.MarketID, evt.Token, "spread", spread, a.spreadThreshold, detailsBase)
	}
	if a.emitAll || math.Abs(imbalance) >= a.imbalanceThreshold {
		details := cloneDetails(detailsBase)
		details["imbalance"] = imbalance
		a.emitSignal(ctx, evt.MarketID, evt.Token, "orderbook_imbalance", imbalance, a.imbalanceThreshold, details)
	}
	if a.emitAll || liquidityGap >= a.liquidityGapThresh {
		details := cloneDetails(detailsBase)
		details["liquidity_gap"] = liquidityGap
		a.emitSignal(ctx, evt.MarketID, evt.Token, "liquidity_gap", liquidityGap, a.liquidityGapThresh, details)
	}

	a.checkArbitrage(ctx, evt.MarketID, evt.Token, detailsBase)
}

func (a *Analyzer) checkArbitrage(ctx context.Context, marketID, token string, details map[string]interface{}) {
	tokens, ok := a.getMarketTokens(ctx, marketID)
	if !ok {
		return
	}
	yesMid := a.getMid(tokens.yes)
	noMid := a.getMid(tokens.no)
	if yesMid <= 0 || noMid <= 0 {
		return
	}
	sum := yesMid + noMid
	deviation := math.Abs(1.0 - sum)
	if a.emitAll || deviation >= a.arbitrageThreshold {
		d := cloneDetails(details)
		d["yes_mid"] = yesMid
		d["no_mid"] = noMid
		d["sum"] = sum
		emaArb := a.updateEMA(ctx, marketID, "arbitrage", deviation)
		d["ema_arbitrage"] = emaArb
		a.emitSignal(ctx, marketID, token, "arbitrage", deviation, a.arbitrageThreshold, d)
	}
}

func (a *Analyzer) emitSignal(ctx context.Context, marketID, token, signalType string, value, threshold float64, details map[string]interface{}) {
	now := time.Now().UTC()
	severity := "low"
	if threshold > 0 {
		if value >= threshold*2 {
			severity = "high"
		} else if value >= threshold {
			severity = "medium"
		}
	}
	signalID := crypto.GenerateEventID(marketID, fmt.Sprintf("%d", now.UnixNano()),
		fmt.Sprintf("%f", value), fmt.Sprintf("%f", threshold), signalType)

	evt := schemas.SignalEvent{
		SignalID:   signalID,
		MarketID:   marketID,
		Token:      token,
		SignalType: signalType,
		Value:      value,
		Threshold:  threshold,
		Severity:   severity,
		Details:    details,
		Timestamp:  now,
		Version:    schemas.VersionV1,
	}

	if err := a.signalProducer.Publish(ctx, marketID, evt); err != nil {
		a.svc.Logger.Error("failed to publish signal", "error", err)
	}

	payload, _ := json.Marshal(evt)
	a.redis.Set(ctx, fmt.Sprintf("signal:latest:%s:%s", marketID, signalType), payload, 30*time.Minute)
}

func (a *Analyzer) updateEMA(ctx context.Context, marketID, key string, value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	redisKey := fmt.Sprintf("signal:ema:%s:%s", marketID, key)
	prevStr, _ := a.redis.Get(ctx, redisKey).Result()
	prev := value
	if prevStr != "" {
		if v, err := strconv.ParseFloat(prevStr, 64); err == nil {
			prev = v
		}
	}
	ema := a.emaAlpha*value + (1.0-a.emaAlpha)*prev
	a.redis.Set(ctx, redisKey, fmt.Sprintf("%f", ema), 24*time.Hour)
	return ema
}

func (a *Analyzer) getHistoricalStats(ctx context.Context, marketID string) historicalStats {
	if a.clickhouse == nil {
		return historicalStats{}
	}
	a.histMu.Lock()
	if stat, ok := a.histCache[marketID]; ok && time.Since(stat.fetchedAt) < a.histTTL {
		a.histMu.Unlock()
		return stat
	}
	a.histMu.Unlock()

	query := `
		SELECT
			avg(JSONExtractFloat(data, 'price')) AS avg_price,
			stddevPop(JSONExtractFloat(data, 'price')) AS std_price,
			count() AS trade_count
		FROM events_raw
		WHERE market_id = ?
		  AND type IN ('predsx.trades', 'trade')
		  AND timestamp > now() - INTERVAL 1 HOUR
	`
	rows, err := a.clickhouse.Query(ctx, query, marketID)
	if err != nil {
		return historicalStats{}
	}
	defer func() {
		if closer, ok := rows.(interface{ Close() error }); ok {
			closer.Close()
		}
	}()

	var avg, std float64
	var count uint64
	if sqlRows, ok := rows.(interface {
		Next() bool
		Scan(dest ...interface{}) error
	}); ok {
		if sqlRows.Next() {
			_ = sqlRows.Scan(&avg, &std, &count)
		}
	}

	stat := historicalStats{
		avgPrice:   avg,
		stdPrice:   std,
		tradeCount: count,
		fetchedAt:  time.Now(),
	}
	a.histMu.Lock()
	a.histCache[marketID] = stat
	a.histMu.Unlock()
	return stat
}

func (a *Analyzer) getMarketTokens(ctx context.Context, marketID string) (tokenCacheEntry, bool) {
	a.tokenMu.Lock()
	if entry, ok := a.tokenCache[marketID]; ok && time.Since(entry.at) < 10*time.Minute {
		a.tokenMu.Unlock()
		return entry, entry.yes != "" && entry.no != ""
	}
	a.tokenMu.Unlock()

	val, err := a.redis.Get(ctx, fmt.Sprintf("market:%s:tokens", marketID)).Result()
	if err != nil || val == "" {
		return tokenCacheEntry{}, false
	}

	var t schemas.TokenExtracted
	if err := json.Unmarshal([]byte(val), &t); err != nil {
		return tokenCacheEntry{}, false
	}

	entry := tokenCacheEntry{yes: t.TokenYes, no: t.TokenNo, at: time.Now()}
	a.tokenMu.Lock()
	a.tokenCache[marketID] = entry
	a.tokenMu.Unlock()
	return entry, true
}

func (a *Analyzer) storeMid(token string, price float64) {
	if token == "" || price <= 0 {
		return
	}
	a.midMu.Lock()
	a.lastMid[token] = price
	a.midMu.Unlock()
}

func (a *Analyzer) getMid(token string) float64 {
	if token == "" {
		return 0
	}
	a.midMu.Lock()
	defer a.midMu.Unlock()
	return a.lastMid[token]
}

func bestPrices(bids, asks []schemas.PriceLevel) (float64, float64) {
	bestBid := 0.0
	bestAsk := 0.0
	if len(bids) > 0 {
		bestBid = toFloat(bids[0].Price)
	}
	if len(asks) > 0 {
		bestAsk = toFloat(asks[0].Price)
	}
	return bestBid, bestAsk
}

func orderbookImbalance(bids, asks []schemas.PriceLevel, depth int) float64 {
	if depth <= 0 {
		return 0
	}
	bidSum := 0.0
	askSum := 0.0
	for i := 0; i < len(bids) && i < depth; i++ {
		bidSum += toFloat(bids[i].Size)
	}
	for i := 0; i < len(asks) && i < depth; i++ {
		askSum += toFloat(asks[i].Size)
	}
	denom := bidSum + askSum
	if denom == 0 {
		return 0
	}
	return (bidSum - askSum) / denom
}

func maxLiquidityGap(bids, asks []schemas.PriceLevel) float64 {
	maxGap := 0.0
	for i := 0; i+1 < len(bids); i++ {
		gap := math.Abs(toFloat(bids[i].Price) - toFloat(bids[i+1].Price))
		if gap > maxGap {
			maxGap = gap
		}
	}
	for i := 0; i+1 < len(asks); i++ {
		gap := math.Abs(toFloat(asks[i+1].Price) - toFloat(asks[i].Price))
		if gap > maxGap {
			maxGap = gap
		}
	}
	return maxGap
}

func toFloat(v string) float64 {
	if v == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return f
}

func getEnvFloat(key string, fallback float64) float64 {
	val := config.GetEnv(key, "")
	if val == "" {
		return fallback
	}
	if f, err := strconv.ParseFloat(val, 64); err == nil {
		return f
	}
	return fallback
}

func cloneDetails(src map[string]interface{}) map[string]interface{} {
	dst := make(map[string]interface{}, len(src)+4)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
