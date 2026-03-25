package main

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"sync"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/logger"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
	"github.com/predsx/predsx/libs/websocket-client"
)

type StreamManager struct {
	ctx           context.Context
	log           logger.Interface
	wsURL         string
	numConns      int
	workers       []*Worker
	producer      *kafkaclient.TypedProducer[schemas.RawWebsocketEvent]
	tokenConsumer *kafkaclient.TypedConsumer[schemas.TokenExtracted]
}

type Worker struct {
	id       int
	log      logger.Interface
	wsURL    string
	client   websocket.Interface
	tokens   map[string]bool
	mu       sync.Mutex
	msgChan  chan []byte
	producer *kafkaclient.TypedProducer[schemas.RawWebsocketEvent]
	subscribed bool

	// drop counter for observable buffer overflow
	dropCount int64
}

func main() {
	svc := service.NewBaseService("stream")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Configuration
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		inputTopic   := config.GetEnv("TOKEN_EXTRACTOR_TOPIC", "predsx.tokens.extracted")
		outputTopic  := config.GetEnv("WS_RAW_TOPIC", "predsx.ws.raw")
		wsURL        := config.GetEnv("POLYMARKET_WS_URL", "wss://clob.polymarket.com/ws")
		numConns     := config.GetEnvInt("WS_CONNECTIONS", 4)

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			inputTopic:  1,
			outputTopic: 6, // predsx.ws.raw needs 6 partitions for high volume
		}, svc.Logger)

		producer := kafkaclient.NewTypedProducer[schemas.RawWebsocketEvent]([]string{kafkaBrokers}, outputTopic, svc.Logger)
		defer producer.Close()

		tokenConsumer := kafkaclient.NewTypedConsumer[schemas.TokenExtracted]([]string{kafkaBrokers}, inputTopic, "stream-group", svc.Logger)
		defer tokenConsumer.Close()

		manager := &StreamManager{
			ctx:           ctx,
			log:           svc.Logger,
			wsURL:         wsURL,
			numConns:      numConns,
			workers:       make([]*Worker, numConns),
			producer:      producer,
			tokenConsumer: tokenConsumer,
		}

		// Initialize workers
		for i := 0; i < numConns; i++ {
			manager.workers[i] = &Worker{
				id:       i,
				log:      svc.Logger.With("worker_id", i),
				wsURL:    wsURL,
				tokens:   make(map[string]bool),
				msgChan:  make(chan []byte, 1000),
				producer: producer,
			}
			go manager.workers[i].Start(ctx)
		}

		svc.Logger.Info("stream manager started", "num_connections", numConns, "ws_url", wsURL)

		for {
			tokenMsg, err := tokenConsumer.Fetch(ctx)
			if err != nil {
				svc.Logger.Error("failed to fetch token", "error", err)
				continue
			}

			workerID := manager.getWorkerID(tokenMsg.TokenYes)
			manager.workers[workerID].AddToken(tokenMsg.TokenYes)
			manager.workers[workerID].AddToken(tokenMsg.TokenNo)

			svc.Logger.Info("assigned tokens to worker",
				"market_id", tokenMsg.MarketID,
				"worker_id", workerID)
		}
	})
}

func (m *StreamManager) getWorkerID(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32()) % m.numConns
}

// Start runs the worker connection loop with exponential backoff.
// Real WebSocket feeds (including Polymarket) disconnect frequently —
// flat retries are insufficient; backoff prevents thundering herd on reconnect.
func (w *Worker) Start(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 60 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		w.mu.Lock()
		hasTokens := len(w.tokens) > 0
		w.mu.Unlock()
		if !hasTokens {
			time.Sleep(1 * time.Second)
			continue
		}

		if err := w.connect(ctx); err != nil {
			w.log.Error("ws connection failed, backing off",
				"error", err,
				"backoff_sec", backoff.Seconds())
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		// Reset backoff on successful connection
		backoff = time.Second
		w.log.Info("ws connected, starting read loop")
		w.readLoop(ctx)
		w.log.Warn("ws read loop exited, reconnecting")
	}
}

func (w *Worker) connect(ctx context.Context) error {
	client, err := websocket.NewClient(w.wsURL, w.log)
	if err != nil {
		return err
	}
	w.client = client
	w.subscribed = false
	w.log.Info("connected to websocket")

	// Restore all existing subscriptions after reconnect
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sendInitialSubscriptionLocked() {
		w.subscribed = true
	}
	return nil
}

func (w *Worker) sendInitialSubscriptionLocked() bool {
	if w.client == nil || len(w.tokens) == 0 {
		return false
	}

	assets := make([]string, 0, len(w.tokens))
	for token := range w.tokens {
		assets = append(assets, token)
	}

	sub := map[string]interface{}{
		"type":                   "market",
		"assets_ids":             assets,
		"custom_feature_enabled": true,
		"initial_dump":           true,
	}
	data, _ := json.Marshal(sub)
	if err := w.client.WriteMessage(1, data); err != nil {
		w.log.Error("failed to send initial subscription", "error", err)
		return false
	}
	w.log.Info("sent initial subscription", "asset_count", len(assets))
	return true
}

func (w *Worker) subscribeTokenLocked(token string) {
	sub := map[string]interface{}{
		"operation":              "subscribe",
		"assets_ids":             []string{token},
		"custom_feature_enabled": true,
	}
	data, _ := json.Marshal(sub)
	if err := w.client.WriteMessage(1, data); err != nil {
		w.log.Error("failed to send subscription update", "error", err, "token", token)
	}
}

func (w *Worker) AddToken(token string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.tokens[token] {
		w.tokens[token] = true
		if w.client != nil {
			if !w.subscribed {
				if w.sendInitialSubscriptionLocked() {
					w.subscribed = true
				}
			} else {
				w.subscribeTokenLocked(token)
			}
		}
	}
}

func (w *Worker) readLoop(ctx context.Context) {
	defer w.client.Close()

	pingStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-pingStop:
				return
			case <-ticker.C:
				if err := w.client.WriteMessage(1, []byte("PING")); err != nil {
					w.log.Warn("ping failed", "error", err)
					return
				}
			}
		}
	}()
	defer close(pingStop)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := w.client.ReadMessage()
		if err != nil {
			w.log.Error("ws read failed", "error", err)
			return
		}

		if isControlMessage(data) {
			if string(data) != "PONG" {
				w.log.Warn("ws control message", "message", string(data))
			}
			continue
		}

		eventType, token := extractEventInfo(data)
		event := schemas.RawWebsocketEvent{
			EventType: eventType,
			Token:     token,
			Payload:   data,
			Timestamp: time.Now(),
		}

		// Non-blocking publish: drop and count if Kafka is backpressuring
		if err := w.producer.Publish(ctx, "raw", event); err != nil {
			w.dropCount++
			w.log.Error("failed to publish raw event",
				"error", err,
				"total_drops", w.dropCount,
				"worker_id", w.id)
		}
	}
}

func isControlMessage(data []byte) bool {
	msg := string(data)
	return msg == "PONG" || msg == "PING" || msg == "INVALID OPERATION"
}

func extractEventInfo(data []byte) (string, string) {
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "polymarket_update", ""
	}

	eventType, _ := payload["event_type"].(string)
	if eventType == "" {
		if t, ok := payload["type"].(string); ok {
			eventType = t
		}
	}

	token := ""
	if asset, ok := payload["asset_id"].(string); ok {
		token = asset
	} else if asset, ok := payload["asset"].(string); ok {
		token = asset
	} else if asset, ok := payload["token"].(string); ok {
		token = asset
	}

	if eventType == "" {
		eventType = "polymarket_update"
	}
	return eventType, token
}
