package main

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	redisclient "github.com/predsx/predsx/libs/redis-client"
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
		inputTopic := config.GetEnv("WS_RAW_TOPIC", "predsx.ws.raw")
		outputTopic := config.GetEnv("ORDERBOOK_UPDATES_TOPIC", "predsx.orderbook.updates")
		groupID := config.GetEnv("CONSUMER_GROUP", "orderbook-engine-group")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			inputTopic:  6,
			outputTopic: 6,
		}, svc.Logger)

		rdb := redisclient.NewClient(redisclient.Options{Addr: redisAddr}, svc.Logger)

		consumer := kafkaclient.NewTypedConsumer[schemas.RawWebsocketEvent]([]string{kafkaBrokers}, inputTopic, groupID, svc.Logger)
		defer consumer.Close()

		producer := kafkaclient.NewTypedProducer[schemas.OrderbookUpdate]([]string{kafkaBrokers}, outputTopic, svc.Logger)
		defer producer.Close()

		svc.Logger.Info("orderbook engine started", "input", inputTopic, "output", outputTopic)

		for {
			rawMsg, err := consumer.Fetch(ctx)
			if err != nil {
				svc.Logger.Error("fetch error", "error", err)
				continue
			}

			if err := processEvent(ctx, svc, rawMsg, rdb, producer); err != nil {
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

func processEvent(ctx context.Context, svc *service.BaseService, raw schemas.RawWebsocketEvent, rdb redisclient.Interface, producer *kafkaclient.TypedProducer[schemas.OrderbookUpdate]) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw.Payload, &payload); err != nil {
		return nil // Skip non-orderbook messages
	}

	eventType := getString(payload, "event_type", "type")
	if eventType == "" {
		return nil
	}

	marketID := getString(payload, "market_id", "market", "condition_id")
	if marketID == "" {
		return nil
	}

	if eventType != "book" && eventType != "price_change" && eventType != "snapshot" && eventType != "l2update" {
		return nil
	}

	if eventType == "price_change" {
		if changes, ok := payload["price_changes"].([]interface{}); ok {
			for _, change := range changes {
				entry, ok := change.(map[string]interface{})
				if !ok {
					continue
				}
				assetID := getString(entry, "asset_id", "asset", "token")
				price := getString(entry, "price")
				size := getString(entry, "size")
				side := strings.ToLower(getString(entry, "side"))
				if assetID == "" || price == "" || size == "" {
					continue
				}
				ob := getOrderbook(marketID, assetID)
				ob.mu.Lock()
				if side == "buy" || side == "bid" {
					updateLevel(ob.Bids, price, size)
				} else if side == "sell" || side == "ask" {
					updateLevel(ob.Asks, price, size)
				}
				emitUpdate(ctx, ob, rdb, producer, svc)
				ob.mu.Unlock()
			}
			return nil
		}
	}

	assetID := getString(payload, "asset_id", "asset", "token")
	if assetID == "" {
		return nil
	}

	ob := getOrderbook(marketID, assetID)
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if eventType == "book" || eventType == "snapshot" {
		ob.Bids = make(map[string]string)
		ob.Asks = make(map[string]string)
	}

	if eventType == "book" || eventType == "snapshot" {
		applyLevelsFromValue(ob.Bids, payload["bids"])
		applyLevelsFromValue(ob.Asks, payload["asks"])
	} else {
		price := getString(payload, "price")
		size := getString(payload, "size")
		side := strings.ToLower(getString(payload, "side"))
		if price == "" || size == "" {
			return nil
		}
		if side == "buy" || side == "bid" {
			updateLevel(ob.Bids, price, size)
		} else if side == "sell" || side == "ask" {
			updateLevel(ob.Asks, price, size)
		}
	}

	emitUpdate(ctx, ob, rdb, producer, svc)
	return nil
}

func emitUpdate(ctx context.Context, ob *Orderbook, rdb redisclient.Interface, producer *kafkaclient.TypedProducer[schemas.OrderbookUpdate], svc *service.BaseService) {
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

	redisPayload := map[string]interface{}{
		"market_id": ob.MarketID,
		"bids":      update.Bids,
		"asks":      update.Asks,
		"timestamp": time.Now().Unix(),
	}
	if payloadBytes, err := json.Marshal(redisPayload); err == nil {
		rdb.Set(ctx, "predsx:orderbook:"+ob.MarketID, payloadBytes, 3600*time.Second)
	}

	if err := producer.Publish(ctx, ob.MarketID, update); err != nil {
		svc.Logger.Error("failed to publish orderbook update", "error", err)
	}
}

func getString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch t := v.(type) {
			case string:
				return t
			case float64:
				return trimFloat(t)
			}
		}
	}
	return ""
}

func trimFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', -1, 64), "0"), ".")
}

func applyLevelsFromValue(levels map[string]string, v interface{}) {
	switch arr := v.(type) {
	case []interface{}:
		for _, item := range arr {
			switch level := item.(type) {
			case []interface{}:
				if len(level) < 2 {
					continue
				}
				price := toString(level[0])
				size := toString(level[1])
				if price != "" && size != "" {
					updateLevel(levels, price, size)
				}
			case map[string]interface{}:
				price := toString(level["price"])
				size := toString(level["size"])
				if price != "" && size != "" {
					updateLevel(levels, price, size)
				}
			}
		}
	}
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return trimFloat(t)
	default:
		return ""
	}
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
