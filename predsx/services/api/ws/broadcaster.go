package ws

import (
	"context"
	"encoding/json"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/logger"
	"github.com/predsx/predsx/libs/schemas"
)

// Envelope wraps every outbound WebSocket message with a typed discriminator
// so the frontend can switch on "type" without inspecting the payload structure.
type Envelope struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp string      `json:"timestamp"`
}

func marshal(msgType string, data interface{}) []byte {
	env := Envelope{
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	b, _ := json.Marshal(env)
	return b
}

// StartKafkaBroadcaster fans in three Kafka consumers and broadcasts typed
// envelopes to all connected WebSocket clients via the hub.
//
// Topics are read from env vars (same names used by orderbook/trade/price engines):
//   - TRADES_LIVE_TOPIC       (default: predsx.trades.live)
//   - ORDERBOOK_UPDATES_TOPIC (default: predsx.orderbook.updates)
//   - PRICES_TOPIC            (default: predsx.prices)
func StartKafkaBroadcaster(ctx context.Context, hub *Hub, kafkaBrokers string, log logger.Interface) {
	brokers := []string{kafkaBrokers}

	tradesTopic := config.GetEnv("TRADES_LIVE_TOPIC", "predsx.trades.live")
	orderbookTopic := config.GetEnv("ORDERBOOK_UPDATES_TOPIC", "predsx.orderbook.updates")
	pricesTopic := config.GetEnv("PRICES_TOPIC", "predsx.prices")
	signalsTopic := config.GetEnv("SIGNALS_TOPIC", "predsx.signals.live")

	log.Info("kafka broadcaster starting",
		"trades_topic", tradesTopic,
		"orderbook_topic", orderbookTopic,
		"prices_topic", pricesTopic,
		"signals_topic", signalsTopic,
	)

	// Trades
	go func() {
		c := kafkaclient.NewTypedConsumer[schemas.TradeEvent](brokers, tradesTopic, "api-ws-trades", log)
		defer c.Close()
		log.Info("trade consumer started", "topic", tradesTopic)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			evt, err := c.Fetch(ctx)
			if err != nil {
				log.Error("trade consumer fetch error", "error", err)
				continue
			}
			hub.Broadcast(marshal("trade", evt))
		}
	}()

	// Orderbook
	go func() {
		c := kafkaclient.NewTypedConsumer[schemas.OrderbookUpdate](brokers, orderbookTopic, "api-ws-orderbook", log)
		defer c.Close()
		log.Info("orderbook consumer started", "topic", orderbookTopic)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			evt, err := c.Fetch(ctx)
			if err != nil {
				log.Error("orderbook consumer fetch error", "error", err)
				continue
			}
			hub.Broadcast(marshal("orderbook", evt))
		}
	}()

	// Prices
	go func() {
		c := kafkaclient.NewTypedConsumer[schemas.PriceUpdate](brokers, pricesTopic, "api-ws-prices", log)
		defer c.Close()
		log.Info("price consumer started", "topic", pricesTopic)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			evt, err := c.Fetch(ctx)
			if err != nil {
				log.Error("price consumer fetch error", "error", err)
				continue
			}
			hub.Broadcast(marshal("price", evt))
		}
	}()

	// Signals
	go func() {
		c := kafkaclient.NewTypedConsumer[schemas.SignalEvent](brokers, signalsTopic, "api-ws-signals", log)
		defer c.Close()
		log.Info("signals consumer started", "topic", signalsTopic)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			evt, err := c.Fetch(ctx)
			if err != nil {
				log.Error("signals consumer fetch error", "error", err)
				continue
			}
			hub.Broadcast(marshal("signal", evt))
		}
	}()
}
