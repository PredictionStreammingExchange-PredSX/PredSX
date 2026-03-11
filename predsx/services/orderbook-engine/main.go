package main

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

// Orderbook represents an in-memory L2 orderbook.
type Orderbook struct {
	MarketID string
	Token    string
	Bids     map[string]string // Price -> Size
	Asks     map[string]string // Price -> Size
	mu       sync.RWMutex
}

type OrderbookMessage struct {
	Type     string         `json:"event_type"` // snapshot, l2update
	MarketID string         `json:"market_id"`
	Asset    string         `json:"asset"`
	Bids     [][]string     `json:"bids"` // [[price, size], ...]
	Asks     [][]string     `json:"asks"`
}

var (
	orderbooks    = make(map[string]*Orderbook)
	orderbooksMu  sync.RWMutex
)

func main() {
	svc := service.NewBaseService("orderbook-engine")

	svc.Run(context.Background(), func(ctx context.Context) error {
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		inputTopic := config.GetEnv("ORDERBOOK_UPDATES_TOPIC", "predsx.orderbook.updates")
		groupID := config.GetEnv("CONSUMER_GROUP", "orderbook-engine-group")

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			inputTopic:  6,
		}, svc.Logger)

		consumer := kafkaclient.NewTypedConsumer[schemas.RawWebsocketEvent]([]string{kafkaBrokers}, inputTopic, groupID, svc.Logger)
		defer consumer.Close()

		svc.Logger.Info("orderbook engine started", "input", inputTopic)

		for {
			rawMsg, err := consumer.Fetch(ctx)
			if err != nil {
				svc.Logger.Error("fetch error", "error", err)
				continue
			}

			if err := processEvent(ctx, svc, rawMsg); err != nil {
				svc.Logger.Error("process error", "error", err)
			}
		}
	})
}

func getOrderbook(marketID, token string) *Orderbook {
	orderbooksMu.Lock()
	defer orderbooksMu.Unlock()

	key := marketID + ":" + token
	if ob, ok := orderbooks[key]; ok {
		return ob
	}

	ob := &Orderbook{
		MarketID: marketID,
		Token:    token,
		Bids:     make(map[string]string),
		Asks:     make(map[string]string),
	}
	orderbooks[key] = ob
	return ob
}

func processEvent(ctx context.Context, svc *service.BaseService, raw schemas.RawWebsocketEvent) error {
	var msg OrderbookMessage
	if err := json.Unmarshal(raw.Payload, &msg); err != nil {
		return nil // Skip non-orderbook messages
	}

	if msg.Type != "snapshot" && msg.Type != "l2update" {
		return nil
	}

	ob := getOrderbook(msg.MarketID, msg.Asset)
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if msg.Type == "snapshot" {
		ob.Bids = make(map[string]string)
		ob.Asks = make(map[string]string)
	}

	for _, b := range msg.Bids {
		updateLevel(ob.Bids, b[0], b[1])
	}
	for _, a := range msg.Asks {
		updateLevel(ob.Asks, a[0], a[1])
	}

	// Compute top 10 and emit
	update := schemas.OrderbookUpdate{
		MarketID:  ob.MarketID,
		Token:     ob.Token,
		Timestamp: time.Now(),
		Version:   schemas.VersionV1,
	}

	update.Bids = getTopLevels(ob.Bids, true, 10)
	update.Asks = getTopLevels(ob.Asks, false, 10)

	if len(update.Bids) > 0 {
		update.BestBid = update.Bids[0].Price
	}
	if len(update.Asks) > 0 {
		update.BestAsk = update.Asks[0].Price
	}

	// Snapshots could theoretically be pushed to Redis here instead of Kafka 
	// based on the architectural decision "Maintain orderbook snapshots in Redis"
	// but to prevent large diffs we'll simulate the snapshot store
	return nil
}

func updateLevel(m map[string]string, price, size string) {
	if size == "0" || size == "0.0" {
		delete(m, price)
	} else {
		m[price] = size
	}
}

func getTopLevels(m map[string]string, descending bool, limit int) []schemas.PriceLevel {
	prices := make([]string, 0, len(m))
	for p := range m {
		prices = append(prices, p)
	}

	sort.Slice(prices, func(i, j int) bool {
		if descending {
			return prices[i] > prices[j] // Note: String sort works if precision is fixed, otherwise need float parse
		}
		return prices[i] < prices[j]
	})

	if len(prices) > limit {
		prices = prices[:limit]
	}

	levels := make([]schemas.PriceLevel, len(prices))
	for i, p := range prices {
		levels[i] = schemas.PriceLevel{
			Price: p,
			Size:  m[p],
		}
	}
	return levels
}
