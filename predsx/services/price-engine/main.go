package main

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
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
	mu             sync.RWMutex
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
		orderbookTopic := config.GetEnv("ORDERBOOK_TOPIC", "predsx.orderbook")
		outputTopic := config.GetEnv("PRICES_TOPIC", "predsx.prices")
		groupID := config.GetEnv("CONSUMER_GROUP", "price-engine-group")

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

		svc.Logger.Info("price engine started", "trades", tradesTopic, "orderbook", orderbookTopic, "output", outputTopic)

		// Concurrent loops for trades and orderbooks
		go func() {
			for {
				trade, err := tradeConsumer.Fetch(ctx)
				if err != nil {
					svc.Logger.Error("trade fetch error", "error", err)
					continue
				}
				processTrade(ctx, svc, trade, producer)
			}
		}()

		for {
			ob, err := obConsumer.Fetch(ctx)
			if err != nil {
				svc.Logger.Error("ob fetch error", "error", err)
				continue
			}
			processOrderbook(ctx, svc, ob, producer)
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

func processTrade(ctx context.Context, svc *service.BaseService, trade schemas.TradeEvent, producer *kafkaclient.TypedProducer[schemas.PriceUpdate]) {
	s := getState(trade.MarketID, trade.Token)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.LastTradePrice = trade.Price
	s.Volume24h += trade.Size // Simulation: In real apps, use a sliding window

	emitPrice(ctx, s, producer)
}

func processOrderbook(ctx context.Context, svc *service.BaseService, ob schemas.OrderbookUpdate, producer *kafkaclient.TypedProducer[schemas.PriceUpdate]) {
	s := getState(ob.MarketID, ob.Token)
	s.mu.Lock()
	defer s.mu.Unlock()

	if bid, err := strconv.ParseFloat(ob.BestBid, 64); err == nil {
		s.BestBid = bid
	}
	if ask, err := strconv.ParseFloat(ob.BestAsk, 64); err == nil {
		s.BestAsk = ask
	}

	emitPrice(ctx, s, producer)
}

func emitPrice(ctx context.Context, s *MarketPriceState, producer *kafkaclient.TypedProducer[schemas.PriceUpdate]) {
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
}
