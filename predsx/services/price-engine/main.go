package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	clickhouse "github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	redisclient "github.com/predsx/predsx/libs/redis-client"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

type MarketPriceState struct {
	ID             string
	Token          string
	BestBid        float64
	BestAsk        float64
	LastTradePrice float64
	Volume24h      float64
	trades         []tradePoint
	lastSeedAt     time.Time
	mu             sync.RWMutex
}

type tradePoint struct {
	ts       time.Time
	notional float64
}

var (
	states   = make(map[string]*MarketPriceState)
	statesMu sync.RWMutex
)

func main() {
	svc := service.NewBaseService("price-engine")

	svc.Run(context.Background(), func(ctx context.Context) error {
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		tradesTopic := config.GetEnv("TRADES_LIVE_TOPIC", "predsx.trades.live")
		orderbookTopic := config.GetEnv("ORDERBOOK_TOPIC", "predsx.orderbook.updates")
		outputTopic := config.GetEnv("PRICES_TOPIC", "predsx.prices")
		groupID := config.GetEnv("CONSUMER_GROUP", "price-engine-group")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
		chAddr := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase := config.GetEnv("CLICKHOUSE_DB", "default")

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			tradesTopic:    6,
			outputTopic:    3,
		}, svc.Logger)

		// Kafka Clients
		tradeConsumer := kafkaclient.NewTypedConsumer[schemas.TradeEvent]([]string{kafkaBrokers}, tradesTopic, groupID, svc.Logger)
		defer tradeConsumer.Close()

		obConsumer := kafkaclient.NewTypedConsumer[schemas.OrderbookUpdate]([]string{kafkaBrokers}, orderbookTopic, groupID, svc.Logger)
		defer obConsumer.Close()

		producer := kafkaclient.NewTypedProducer[schemas.PriceUpdate]([]string{kafkaBrokers}, outputTopic, svc.Logger)
		defer producer.Close()

		rdb := redisclient.NewClient(redisclient.Options{Addr: redisAddr}, svc.Logger)
		defer rdb.Close()

		ch, err := clickhouse.NewClient(clickhouse.Options{
			Addr:     chAddr,
			User:     chUser,
			Password: chPassword,
			Database: chDatabase,
			MaxConns: 5,
		}, svc.Logger)
		if err != nil {
			svc.Logger.Warn("clickhouse unavailable, volume seeding disabled", "error", err)
		}

		svc.Logger.Info("price engine started", "trades", tradesTopic, "orderbook", orderbookTopic, "output", outputTopic)

		if ch != nil {
			go seedVolumesLoop(ctx, svc, ch, rdb)
			go func() {
				// Seed once shortly after startup
				time.Sleep(5 * time.Second)
				seedVolumes(ctx, svc, ch, rdb)
			}()
		}

		// Concurrent loops for trades and orderbooks
		go func() {
			for {
				trade, err := tradeConsumer.Fetch(ctx)
				if err != nil {
					svc.Logger.Error("trade fetch error", "error", err)
					continue
				}
				processTrade(ctx, svc, trade, producer, rdb)
			}
		}()

		for {
			ob, err := obConsumer.Fetch(ctx)
			if err != nil {
				svc.Logger.Error("ob fetch error", "error", err)
				continue
			}
			processOrderbook(ctx, svc, ob, producer, rdb)
		}
	})
}

func getState(marketID, token string) *MarketPriceState {
	statesMu.Lock()
	defer statesMu.Unlock()

	key := marketID + ":" + token
	if s, ok := states[key]; ok {
		return s
	}

	s := &MarketPriceState{
		ID:    marketID,
		Token: token,
	}
	states[key] = s
	return s
}

func processTrade(ctx context.Context, svc *service.BaseService, trade schemas.TradeEvent, producer *kafkaclient.TypedProducer[schemas.PriceUpdate], rdb redisclient.Interface) {
	s := getState(trade.MarketID, trade.Token)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.LastTradePrice = trade.Price
	now := trade.Timestamp
	if now.IsZero() {
		now = time.Now()
	}
	notional := trade.Price * trade.Size
	s.trades = append(s.trades, tradePoint{ts: now, notional: notional})
	s.Volume24h += notional
	cutoff := now.Add(-24 * time.Hour)
	for len(s.trades) > 0 && s.trades[0].ts.Before(cutoff) {
		s.Volume24h -= s.trades[0].notional
		s.trades = s.trades[1:]
	}

	emitPrice(ctx, s, producer, rdb)
}

func processOrderbook(ctx context.Context, svc *service.BaseService, ob schemas.OrderbookUpdate, producer *kafkaclient.TypedProducer[schemas.PriceUpdate], rdb redisclient.Interface) {
	s := getState(ob.MarketID, ob.Token)
	s.mu.Lock()
	defer s.mu.Unlock()

	if bid, err := strconv.ParseFloat(ob.BestBid, 64); err == nil {
		s.BestBid = bid
	}
	if ask, err := strconv.ParseFloat(ob.BestAsk, 64); err == nil {
		s.BestAsk = ask
	}

	emitPrice(ctx, s, producer, rdb)
}

func emitPrice(ctx context.Context, s *MarketPriceState, producer *kafkaclient.TypedProducer[schemas.PriceUpdate], rdb redisclient.Interface) {
	mid := 0.0
	spread := 0.0
	if s.BestBid > 0 && s.BestAsk > 0 {
		mid = (s.BestBid + s.BestAsk) / 2
		spread = s.BestAsk - s.BestBid
	}

	update := schemas.PriceUpdate{
		MarketID:       s.ID,
		Token:          s.Token,
		Price:          mid,
		MidPrice:       mid,
		BestBid:        s.BestBid,
		BestAsk:        s.BestAsk,
		Spread:         spread,
		LastTradePrice: s.LastTradePrice,
		Volume24h:      s.Volume24h,
		Timestamp:      time.Now(),
		Version:        schemas.VersionV1,
	}

	producer.Publish(ctx, s.ID, update)

	if rdb != nil {
		if payload, err := json.Marshal(update); err == nil {
			rdb.Set(ctx, "live:price:"+s.ID, payload, 2*time.Hour)
			rdb.Set(ctx, "predsx:price:"+s.ID, payload, 2*time.Hour)
		}
	}
}

func seedVolumesLoop(ctx context.Context, svc *service.BaseService, ch clickhouse.Interface, rdb redisclient.Interface) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			seedVolumes(ctx, svc, ch, rdb)
		}
	}
}

func seedVolumes(ctx context.Context, svc *service.BaseService, ch clickhouse.Interface, rdb redisclient.Interface) {
	// Scan Redis for market tokens to seed a broad set of markets
	var cursor uint64
	marketIDs := make([]string, 0, 500)
	for {
		keys, nextCursor, err := rdb.Scan(ctx, cursor, "market:*:tokens", 500).Result()
		if err != nil {
			break
		}
		for _, key := range keys {
			parts := strings.Split(key, ":")
			if len(parts) == 3 {
				marketIDs = append(marketIDs, parts[1])
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	for _, marketID := range marketIDs {
		// Only seed if missing or zero volume in Redis
		needsSeed := true
		if raw, err := rdb.Get(ctx, "live:price:"+marketID).Result(); err == nil && raw != "" {
			var existing schemas.PriceUpdate
			if json.Unmarshal([]byte(raw), &existing) == nil {
				if existing.Volume24h > 0 {
					needsSeed = false
				}
			}
		}
		if !needsSeed {
			continue
		}

		vol, lastPrice, err := queryVolumeAndLastPrice(ctx, ch, marketID)
		if err != nil {
			continue
		}
		if vol == 0 && lastPrice == 0 {
			continue
		}

		update := schemas.PriceUpdate{
			MarketID:       marketID,
			Price:          lastPrice,
			MidPrice:       lastPrice,
			LastTradePrice: lastPrice,
			Volume24h:      vol,
			Timestamp:      time.Now(),
			Version:        schemas.VersionV1,
		}
		if payload, err := json.Marshal(update); err == nil {
			rdb.Set(ctx, "live:price:"+marketID, payload, 2*time.Hour)
			rdb.Set(ctx, "predsx:price:"+marketID, payload, 2*time.Hour)
		}
	}
}

func queryVolumeAndLastPrice(ctx context.Context, ch clickhouse.Interface, marketID string) (float64, float64, error) {
	query := `
		SELECT
			sum(toFloat64OrZero(JSONExtractString(data, 'price')) * toFloat64OrZero(JSONExtractString(data, 'size'))) AS vol,
			argMax(toFloat64OrZero(JSONExtractString(data, 'price')), timestamp) AS last_price
		FROM events_raw
		WHERE market_id = ?
		  AND type IN ('predsx.trades', 'trade')
		  AND timestamp > now() - INTERVAL 24 HOUR
	`
	rows, err := ch.Query(ctx, query, marketID)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if closer, ok := rows.(interface{ Close() error }); ok {
			closer.Close()
		}
	}()
	if sqlRows, ok := rows.(interface {
		Next() bool
		Scan(dest ...interface{}) error
	}); ok {
		if sqlRows.Next() {
			var vol float64
			var last float64
			if err := sqlRows.Scan(&vol, &last); err == nil {
				return vol, last, nil
			}
		}
	}
	return 0, 0, nil
}
