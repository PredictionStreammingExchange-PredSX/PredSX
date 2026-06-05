package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	clickhouse "github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/logger"
	retry "github.com/predsx/predsx/libs/retry-utils"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
	"github.com/predsx/predsx/libs/websocket-client"
)

// Ingestion Hub combines:
// 1. WebSocket Stream (Polymarket CLOB)
// 2. On-chain Indexer (Polygon CTF)
// 3. Stream Aggregator (Metrics)

func main() {
	svc := service.NewBaseService("ingestion-hub")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// --- SHARED CONFIG ---
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		chAddr := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase := config.GetEnv("CLICKHOUSE_DB", "default")

		// --- WEBSOCKET STREAM CONFIG ---
		wsURL := config.GetEnv("POLYMARKET_WS_URL", "wss://ws-subscriptions-clob.polymarket.com/ws/market")
		numConns := config.GetEnvInt("WS_CONNECTIONS", 4)
		tokenTopic := config.GetEnv("TOKEN_EXTRACTOR_TOPIC", "predsx.tokens.extracted")
		wsRawTopic := config.GetEnv("WS_RAW_TOPIC", "predsx.ws.raw")

		// --- INDEXER CONFIG ---
		onChainTopic := config.GetEnv("ONCHAIN_TRADES_TOPIC", "predsx.trades.onchain")
		// Using Public RPC with Rate-Limit protection as requested
		rpcURL := config.GetEnv("POLYGON_RPC_URL", "https://polygon-rpc.com")

		// --- AGGREGATOR CONFIG ---
		tradesTopic := config.GetEnv("TRADES_LIVE_TOPIC", "predsx.trades.live")
		metricsTopic := config.GetEnv("METRICS_TRADES_TOPIC", "predsx.metrics.trades")

		// Ensure Topics
		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			wsRawTopic:   6,
			onChainTopic: 6,
			metricsTopic: 3,
		}, svc.Logger)

		// 1. START WEBSOCKET STREAM COMPONENT
		go startStreamComponent(ctx, svc, kafkaBrokers, wsURL, numConns, tokenTopic, wsRawTopic)

		// 2. START INDEXER COMPONENT
		go startIndexerComponent(ctx, svc, kafkaBrokers, rpcURL, onChainTopic)

		// 3. START AGGREGATOR COMPONENT
		go startAggregatorComponent(ctx, svc, kafkaBrokers, chAddr, chUser, chPassword, chDatabase, tradesTopic, metricsTopic)

		<-ctx.Done()
		return nil
	})
}

// --- STREAM COMPONENT ---

type StreamManager struct {
	ctx           context.Context
	log           logger.Interface
	wsURL         string
	numConns      int
	workers       []*StreamWorker
	producer      *kafkaclient.TypedProducer[schemas.RawWebsocketEvent]
	tokenConsumer *kafkaclient.TypedConsumer[schemas.TokenExtracted]
}

type StreamWorker struct {
	id         int
	log        logger.Interface
	wsURL      string
	client     websocket.Interface
	tokens     map[string]bool
	mu         sync.Mutex
	msgChan    chan []byte
	producer   *kafkaclient.TypedProducer[schemas.RawWebsocketEvent]
	subscribed bool
	dropCount  int64
}

func startStreamComponent(ctx context.Context, svc *service.BaseService, brokers, wsURL string, numConns int, inputTopic, outputTopic string) {
	producer := kafkaclient.NewTypedProducer[schemas.RawWebsocketEvent]([]string{brokers}, outputTopic, svc.Logger)
	defer producer.Close()

	tokenConsumer := kafkaclient.NewTypedConsumer[schemas.TokenExtracted]([]string{brokers}, inputTopic, "ingestion-hub-stream", svc.Logger)
	defer tokenConsumer.Close()

	manager := &StreamManager{
		ctx:           ctx,
		log:           svc.Logger,
		wsURL:         wsURL,
		numConns:      numConns,
		workers:       make([]*StreamWorker, numConns),
		producer:      producer,
		tokenConsumer: tokenConsumer,
	}

	for i := 0; i < numConns; i++ {
		manager.workers[i] = &StreamWorker{
			id:       i,
			log:      svc.Logger.With("component", "stream", "worker_id", i),
			wsURL:    wsURL,
			tokens:   make(map[string]bool),
			msgChan:  make(chan []byte, 1000),
			producer: producer,
		}
		fmt.Println("STARTING WORKER", i)
		go manager.workers[i].Start(ctx)
	}

	svc.Logger.Info("stream component started", "num_connections", numConns)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			tokenMsg, err := tokenConsumer.Fetch(ctx)
			if err != nil {
				continue
			}
			manager.log.Info("received token from kafka", "token", tokenMsg.TokenYes)
			workerID := manager.getWorkerID(tokenMsg.TokenYes)
			manager.workers[workerID].AddToken(tokenMsg.TokenYes)
			manager.workers[workerID].AddToken(tokenMsg.TokenNo)
		}
	}
}

func (m *StreamManager) getWorkerID(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32()) % m.numConns
}

func (w *StreamWorker) Start(ctx context.Context) {
	w.log.Info("worker started")
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

		if err := w.connect(); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		backoff = time.Second
		w.readLoop(ctx)
	}
}

func (w *StreamWorker) connect() error {
	client, err := websocket.NewClient(w.wsURL, w.log)
	if err != nil {
		w.log.Error("failed to dial polymarket ws", "url", w.wsURL, "error", err)
		return err
	}
	w.client = client
	w.subscribed = false
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sendInitialSubscriptionLocked() {
		w.subscribed = true
	}
	return nil
}

func (w *StreamWorker) sendInitialSubscriptionLocked() bool {
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
	w.log.Info("sending initial subscription to polymarket", "payload", string(data))
	if err := w.client.WriteMessage(1, data); err != nil {
		return false
	}
	return true
}

func (w *StreamWorker) AddToken(token string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.tokens[token] {
		w.log.Info("adding token to worker", "token", token)
		w.tokens[token] = true
		if w.client != nil {
			if !w.subscribed {
				if w.sendInitialSubscriptionLocked() {
					w.subscribed = true
				}
			} else {
				sub := map[string]interface{}{
					"operation":              "subscribe",
					"assets_ids":             []string{token},
					"custom_feature_enabled": true,
				}
				data, _ := json.Marshal(sub)
				w.client.WriteMessage(1, data)
			}
		}
	}
}

func (w *StreamWorker) readLoop(ctx context.Context) {
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
			return
		}
		msgStr := string(data)
		if msgStr == "PONG" || msgStr == "PING" {
			continue
		}
		eventType, token := extractEventInfo(data)
		event := schemas.RawWebsocketEvent{
			EventType: eventType,
			Token:     token,
			Payload:   data,
			Timestamp: time.Now(),
		}
		_ = w.producer.Publish(ctx, "raw", event)
	}
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

// --- INDEXER COMPONENT ---

const CTFContractAddress = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045"

func startIndexerComponent(ctx context.Context, svc *service.BaseService, brokers, rpcURL, outputTopic string) {
	producer := kafkaclient.NewTypedProducer[schemas.OnChainTradeEvent]([]string{brokers}, outputTopic, svc.Logger)
	defer producer.Close()

	// Robust Reconnection Loop
	for {
		select {
		case <-ctx.Done():
			return
		default:
			err := runIndexerCycle(ctx, svc, producer, rpcURL)
			if err != nil {
				svc.Logger.Error("indexer cycle failed, retrying", "error", err)
				time.Sleep(10 * time.Second)
			}
		}
	}
}

func runIndexerCycle(ctx context.Context, svc *service.BaseService, producer *kafkaclient.TypedProducer[schemas.OnChainTradeEvent], rpcURL string) error {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	defer client.Close()

	contractAddress := common.HexToAddress(CTFContractAddress)
	query := ethereum.FilterQuery{Addresses: []common.Address{contractAddress}}

	transferSingleSig := crypto.Keccak256Hash([]byte("TransferSingle(address,address,address,uint256,uint256)"))
	svc.Logger.Info("indexer component started with rate-limit protection", "rpc", rpcURL)

	// Attempt Subscription (requires WebSocket RPC)
	logs := make(chan types.Log)
	sub, err := client.SubscribeFilterLogs(ctx, query, logs)
	if err == nil {
		defer sub.Unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return nil
			case err := <-sub.Err():
				return err
			case vLog := <-logs:
				processLog(ctx, producer, vLog, transferSingleSig)
			}
		}
	}

	// Fallback to Polling (for HTTPS Public RPCs)
	svc.Logger.Info("rpc does not support subscription, falling back to throttled polling")
	lastBlock := uint64(0)
	
	// Throttled Loop to avoid rate limits
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			header, err := client.HeaderByNumber(ctx, nil)
			if err != nil {
				svc.Logger.Warn("failed to get latest block", "error", err)
				continue
			}
			currentBlock := header.Number.Uint64()
			if lastBlock == 0 {
				lastBlock = currentBlock - 5 // Start 5 blocks back
			}
			if currentBlock <= lastBlock {
				continue
			}

			// Fetch logs in small chunks to avoid timeouts/limits
			from := lastBlock + 1
			to := currentBlock
			if to-from > 100 {
				to = from + 100
			}

			q := ethereum.FilterQuery{
				FromBlock: new(big.Int).SetUint64(from),
				ToBlock:   new(big.Int).SetUint64(to),
				Addresses: []common.Address{contractAddress},
			}
			
			// Rate limiting: wait 100ms between requests if we have a big range
			vLogs, err := client.FilterLogs(ctx, q)
			if err != nil {
				svc.Logger.Warn("filter logs failed", "error", err)
				time.Sleep(1 * time.Second)
				continue
			}

			for _, vLog := range vLogs {
				processLog(ctx, producer, vLog, transferSingleSig)
			}
			lastBlock = to
		}
	}
}

func processLog(ctx context.Context, producer *kafkaclient.TypedProducer[schemas.OnChainTradeEvent], vLog types.Log, sig common.Hash) {
	if len(vLog.Topics) > 0 && vLog.Topics[0] == sig {
		if len(vLog.Topics) < 4 {
			return
		}
		from := common.HexToAddress(vLog.Topics[2].Hex()).Hex()
		to := common.HexToAddress(vLog.Topics[3].Hex()).Hex()
		if len(vLog.Data) >= 64 {
			tokenID := new(big.Int).SetBytes(vLog.Data[:32]).String()
			value := new(big.Int).SetBytes(vLog.Data[32:]).String()
			evt := schemas.OnChainTradeEvent{
				TxHash:    vLog.TxHash.Hex(),
				Maker:     from,
				Taker:     to,
				TokenID:   tokenID,
				Amount:    value,
				Timestamp: time.Now(),
			}
			_ = producer.Publish(ctx, evt.TxHash, evt)
		}
	}
}

// --- AGGREGATOR COMPONENT ---

type MetricBucket struct {
	MarketID   string
	Window     time.Time
	TradeCount uint32
	Volume     float64
	PriceSumWt float64
}

func startAggregatorComponent(ctx context.Context, svc *service.BaseService, brokers, chAddr, chUser, chPassword, chDatabase, inputTopic, outputTopic string) {
	consumer := kafkaclient.NewTypedConsumer[schemas.TradeEvent]([]string{brokers}, inputTopic, "ingestion-hub-agg", svc.Logger)
	defer consumer.Close()

	producer := kafkaclient.NewTypedProducer[schemas.PriceUpdate]([]string{brokers}, outputTopic, svc.Logger)
	defer producer.Close()

	ch, err := clickhouse.NewClient(clickhouse.Options{
		Addr: chAddr, User: chUser, Password: chPassword, Database: chDatabase, MaxConns: 5,
	}, svc.Logger)
	if err != nil {
		svc.Logger.Warn("aggregator clickhouse unavailable", "error", err)
	}

	buckets := make(map[string]*MetricBucket)
	agg1m := make(map[string]*MetricBucket)
	var mu sync.Mutex

	// Flush Loops
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				current := buckets
				buckets = make(map[string]*MetricBucket)
				mu.Unlock()
				if len(current) > 0 && ch != nil {
					flushMetrics(ctx, svc, ch, current, "market_metrics")
				}
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				current := agg1m
				agg1m = make(map[string]*MetricBucket)
				mu.Unlock()
				if len(current) > 0 && ch != nil {
					flushMetrics(ctx, svc, ch, current, "price_history_1m")
				}
			}
		}
	}()

	svc.Logger.Info("aggregator component started")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			trade, err := consumer.Fetch(ctx)
			if err != nil {
				continue
			}
			mu.Lock()
			updateBucket(buckets, trade, time.Second)
			updateBucket(agg1m, trade, time.Minute)
			mu.Unlock()
		}
	}
}

func updateBucket(m map[string]*MetricBucket, trade schemas.TradeEvent, d time.Duration) {
	window := trade.Timestamp.Truncate(d)
	key := fmt.Sprintf("%s:%d", trade.MarketID, window.Unix())
	b, ok := m[key]
	if !ok {
		b = &MetricBucket{MarketID: trade.MarketID, Window: window}
		m[key] = b
	}
	b.TradeCount++
	b.Volume += trade.Size
	b.PriceSumWt += trade.Price * trade.Size
}

func flushMetrics(ctx context.Context, svc *service.BaseService, ch clickhouse.Interface, buckets map[string]*MetricBucket, table string) {
	err := retry.Do(ctx, func() error {
		b, err := ch.PrepareBatch(ctx, fmt.Sprintf("INSERT INTO %s (market_id, timestamp, trade_count, volume, avg_price)", table))
		if err != nil {
			return err
		}
		for _, bucket := range buckets {
			avg := 0.0
			if bucket.Volume > 0 {
				avg = bucket.PriceSumWt / bucket.Volume
			}
			b.Append(bucket.MarketID, bucket.Window, bucket.TradeCount, bucket.Volume, avg)
		}
		return b.Send()
	}, retry.DefaultOptions())
	if err != nil {
		svc.Logger.Error("failed to flush metrics", "table", table, "error", err)
	}
}
