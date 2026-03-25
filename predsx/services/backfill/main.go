package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	postgres "github.com/predsx/predsx/libs/postgres-client"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

type BackfillService struct {
	svc       *service.BaseService
	pg        postgres.Interface
	producer  *kafkaclient.TypedProducer[schemas.HistoricalEvent]
	rateLimit time.Duration
	dataLimiter chan struct{}
	clobLimiter chan struct{}
	gammaLimiter chan struct{}
	gammaURL  string
	dataURL   string
	clobURL   string
	http      *http.Client
}

func main() {
	svc := service.NewBaseService("backfill")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Config
		kafkaBrokers  := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		pgConn        := config.GetEnv("POSTGRES_URL", "postgres://postgres:postgres@localhost:5432/predsx")
		topic         := config.GetEnv("TRADES_BACKFILL_TOPIC", "predsx.trades.backfill")
		rateLimitMs   := config.GetEnvInt("RATE_LIMIT_MS", 250)
		dataRateMs    := config.GetEnvInt("DATA_RATE_LIMIT_MS", rateLimitMs)
		clobRateMs    := config.GetEnvInt("CLOB_RATE_LIMIT_MS", rateLimitMs)
		gammaRateMs   := config.GetEnvInt("GAMMA_RATE_LIMIT_MS", rateLimitMs)
		gammaURL      := config.GetEnv("GAMMA_API_URL", "https://gamma-api.polymarket.com")
		dataURL       := config.GetEnv("DATA_API_URL", "https://data-api.polymarket.com")
		clobURL       := config.GetEnv("CLOB_API_URL", "https://clob.polymarket.com")
		backfillMode  := config.GetEnv("BACKFILL_MODE", "trades,prices")
		workers       := config.GetEnvInt("BACKFILL_WORKERS", 4)
		marketLimit   := config.GetEnvInt("BACKFILL_MARKET_LIMIT", 0)
		// Optional: allow overriding market list via env var (comma-separated)
		// e.g. BACKFILL_MARKETS=market-1,market-2
		marketsOverride := config.GetEnv("BACKFILL_MARKETS", "")

		// Clients
		pg, err := postgres.NewClient(ctx, pgConn, 5, svc.Logger)
		if err != nil {
			return fmt.Errorf("postgres connect failed: %w", err)
		}
		defer pg.Close()

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			topic: 3,
		}, svc.Logger)

		producer := kafkaclient.NewTypedProducer[schemas.HistoricalEvent]([]string{kafkaBrokers}, topic, svc.Logger)
		defer producer.Close()

		b := &BackfillService{
			svc:       svc,
			pg:        pg,
			producer:  producer,
			rateLimit: time.Duration(rateLimitMs) * time.Millisecond,
			dataLimiter:  newLimiter(time.Duration(dataRateMs) * time.Millisecond),
			clobLimiter:  newLimiter(time.Duration(clobRateMs) * time.Millisecond),
			gammaLimiter: newLimiter(time.Duration(gammaRateMs) * time.Millisecond),
			gammaURL:  gammaURL,
			dataURL:   dataURL,
			clobURL:   clobURL,
			http: &http.Client{
				Timeout: 20 * time.Second,
			},
		}

		// Initialize progress table
		if err := b.initDB(ctx); err != nil {
			return fmt.Errorf("failed to init backfill_progress table: %w", err)
		}

		// Resolve market list: env override -> Postgres query -> Gamma API fallback
		var markets []string
		if marketsOverride != "" {
			markets = strings.Split(marketsOverride, ",")
			svc.Logger.Info("using market list from BACKFILL_MARKETS env",
				"count", len(markets))
			} else {
				markets, err = b.loadMarketsFromDB(ctx)
				if err != nil {
					svc.Logger.Warn("could not load markets from postgres, falling back to Gamma API",
						"error", err)
					markets, err = b.loadMarketsFromGamma(ctx)
					if err != nil {
						svc.Logger.Warn("could not load markets from Gamma API, backfill skipped",
							"error", err)
						return nil
					}
					svc.Logger.Info("loaded markets from Gamma API", "count", len(markets))
				} else {
					svc.Logger.Info("loaded markets from postgres", "count", len(markets))
				}
			}

			if len(markets) == 0 {
				svc.Logger.Warn("no markets found, backfill complete (nothing to do)")
				return nil
			}

			if marketLimit > 0 && marketLimit < len(markets) {
				markets = markets[:marketLimit]
			}

			jobs := make(chan string, len(markets))
			for _, mID := range markets {
				jobs <- mID
			}
			close(jobs)

			var wg sync.WaitGroup
			if workers < 1 {
				workers = 1
			}
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for id := range jobs {
						b.backfillMarket(ctx, id, backfillMode)
					}
				}()
			}
			wg.Wait()
			return nil
		})
}

// loadMarketsFromDB reads market IDs from the markets table populated by
// the market-discovery service.
func (b *BackfillService) loadMarketsFromDB(ctx context.Context) ([]string, error) {
	rows, err := b.pg.Query(ctx, "SELECT id FROM markets WHERE status='active'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (b *BackfillService) loadMarketsFromGamma(ctx context.Context) ([]string, error) {
	limit := config.GetEnvInt("BACKFILL_GAMMA_LIMIT", 200)
	offset := 0
	results := make([]string, 0, limit)
	for {
		b.wait(b.gammaLimiter)
		u := fmt.Sprintf("%s/markets?active=true&closed=false&limit=%d&offset=%d", strings.TrimRight(b.gammaURL, "/"), limit, offset)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := b.http.Do(req)
		if err != nil {
			return results, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusTooManyRequests {
				b.sleepRetry(resp)
				continue
			}
			return results, fmt.Errorf("gamma markets status %d", resp.StatusCode)
		}
		var arr []map[string]interface{}
		if err := json.Unmarshal(body, &arr); err != nil {
			var wrapper map[string]interface{}
			if err := json.Unmarshal(body, &wrapper); err != nil {
				return results, err
			}
			if v, ok := wrapper["value"].([]interface{}); ok {
				arr = make([]map[string]interface{}, 0, len(v))
				for _, item := range v {
					if m, ok := item.(map[string]interface{}); ok {
						arr = append(arr, m)
					}
				}
			}
		}
		if len(arr) == 0 {
			break
		}
		for _, m := range arr {
			if id, ok := m["id"].(string); ok && id != "" {
				results = append(results, id)
			} else if idNum, ok := m["id"].(float64); ok {
				results = append(results, strconv.FormatInt(int64(idNum), 10))
			}
		}
		offset += limit
		if offset >= config.GetEnvInt("BACKFILL_GAMMA_MAX", 2000) {
			break
		}
	}
	return results, nil
}

func (b *BackfillService) initDB(ctx context.Context) error {
	_, err := b.pg.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS backfill_progress (
			market_id  TEXT PRIMARY KEY,
			last_offset INT DEFAULT 0,
			status     TEXT,
			updated_at TIMESTAMP DEFAULT NOW()
		)
	`)
	return err
}

func (b *BackfillService) backfillMarket(ctx context.Context, marketID string, mode string) {
	b.svc.Logger.Info("starting backfill", "market_id", marketID)

	var offset int
	err := b.pg.QueryRow(ctx,
		"SELECT last_offset FROM backfill_progress WHERE market_id=$1", marketID).Scan(&offset)
	if err != nil {
		b.pg.Exec(ctx,
			"INSERT INTO backfill_progress (market_id, status) VALUES ($1, 'running') ON CONFLICT DO NOTHING",
			marketID)
	}

	limit := config.GetEnvInt("BACKFILL_TRADES_LIMIT", 200)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			events, nextOffset, err := b.fetchFromSource(ctx, marketID, offset, limit, mode)
			if err != nil {
				if isMaxOffsetError(err) {
					b.svc.Logger.Info("backfill reached max offset, marking complete",
						"market_id", marketID, "offset", offset)
					b.pg.Exec(ctx,
						"UPDATE backfill_progress SET status='completed', updated_at=NOW() WHERE market_id=$1",
						marketID)
					return
				}
				if isFatalHTTP(err) {
					b.svc.Logger.Error("fetch failed (fatal), skipping market",
						"market_id", marketID, "error", err)
					b.pg.Exec(ctx,
						"UPDATE backfill_progress SET status='failed', updated_at=NOW() WHERE market_id=$1",
						marketID)
					return
				}
				b.svc.Logger.Error("fetch failed, retrying",
					"market_id", marketID, "error", err)
				time.Sleep(5 * time.Second)
				continue
			}

			if len(events) == 0 {
				b.svc.Logger.Info("backfill complete", "market_id", marketID, "total_offset", offset)
				b.pg.Exec(ctx,
					"UPDATE backfill_progress SET status='completed', updated_at=NOW() WHERE market_id=$1",
					marketID)
				return
			}

			for _, e := range events {
				if err := b.producer.Publish(ctx, marketID, e); err != nil {
					b.svc.Logger.Error("publish failed",
						"market_id", marketID, "error", err)
				}
			}

			offset = nextOffset
			b.pg.Exec(ctx,
				"UPDATE backfill_progress SET last_offset=$1, updated_at=NOW() WHERE market_id=$2",
				offset, marketID)

			b.svc.Logger.Info("backfill progress",
				"market_id", marketID, "offset", offset, "batch", len(events))
			time.Sleep(b.rateLimit)
		}
	}
}

// fetchFromSource pulls real data from Gamma/Data/CLOB APIs.
// Events are tagged with metadata.source="backfill" for downstream idempotency.
func (b *BackfillService) fetchFromSource(ctx context.Context, marketID string, offset, limit int, mode string) ([]schemas.HistoricalEvent, int, error) {
	events := make([]schemas.HistoricalEvent, 0, limit)

	conditionID, tokenIDs, err := b.resolveMarket(ctx, marketID)
	if err != nil {
		return events, offset, err
	}
	if conditionID == "" {
		// Fallback: use marketID if conditionId is missing
		conditionID = marketID
	}

	if strings.Contains(mode, "trades") {
		trades, err := b.fetchTrades(ctx, conditionID, offset, limit)
		if err != nil {
			return events, offset, err
		}
		events = append(events, trades...)
	}

	if strings.Contains(mode, "prices") {
		priceEvents, err := b.fetchPricesHistory(ctx, conditionID, tokenIDs)
		if err != nil {
			return events, offset, err
		}
		events = append(events, priceEvents...)
	}

	if len(events) == 0 {
		return events, offset, nil
	}
	return events, offset + len(events), nil
}

func (b *BackfillService) resolveMarket(ctx context.Context, marketID string) (string, []string, error) {
	// If marketID already looks like a conditionId (0x...), use it.
	if strings.HasPrefix(strings.ToLower(marketID), "0x") {
		return marketID, nil, nil
	}

	b.wait(b.gammaLimiter)
	u := fmt.Sprintf("%s/markets/%s", strings.TrimRight(b.gammaURL, "/"), url.PathEscape(marketID))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := b.http.Do(req)
	if err != nil {
		return "", nil, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			b.sleepRetry(resp)
		}
		return "", nil, fmt.Errorf("gamma market status %d", resp.StatusCode)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return "", nil, err
	}
	conditionID := getString(m, "conditionId", "condition_id", "conditionID", "market")
	var tokenIDs []string
	if raw, ok := m["clobTokenIds"]; ok {
		tokenIDs = toStringSlice(raw)
	}
	return conditionID, tokenIDs, nil
}

func (b *BackfillService) fetchTrades(ctx context.Context, conditionID string, offset, limit int) ([]schemas.HistoricalEvent, error) {
	b.wait(b.dataLimiter)
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))
	// Official docs do not specify all params; allow override.
	if strings.TrimSpace(config.GetEnv("DATA_TRADES_QUERY", "")) == "" {
		params.Set("market", conditionID)
	}
	base := fmt.Sprintf("%s/trades", strings.TrimRight(b.dataURL, "/"))
	u := base + "?" + params.Encode()
	if extra := strings.TrimSpace(config.GetEnv("DATA_TRADES_QUERY", "")); extra != "" {
		if strings.HasPrefix(extra, "?") {
			u = base + extra
		} else if strings.HasPrefix(extra, "&") {
			u = base + "?" + params.Encode() + extra
		} else {
			u = base + "?" + params.Encode() + "&" + extra
		}
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			b.sleepRetry(resp)
		}
		return nil, &httpStatusError{service: "data", code: resp.StatusCode, body: string(body)}
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, err
	}
	events := make([]schemas.HistoricalEvent, 0, len(arr))
	for _, t := range arr {
		t["market_id"] = conditionID
		t["source"] = "backfill"
		data, _ := json.Marshal(t)
		ts := time.Now().UTC()
		if tsVal, ok := t["timestamp"].(float64); ok {
			ts = time.Unix(int64(tsVal), 0)
		}
		events = append(events, schemas.HistoricalEvent{
			MarketID:  conditionID,
			Source:    "data-api",
			EventType: "trade",
			Data:      data,
			Timestamp: ts,
			Version:   schemas.VersionV1,
		})
	}
	return events, nil
}

func (b *BackfillService) fetchPricesHistory(ctx context.Context, conditionID string, tokenIDs []string) ([]schemas.HistoricalEvent, error) {
	if len(tokenIDs) == 0 {
		return nil, nil
	}
	events := make([]schemas.HistoricalEvent, 0)
	for _, tokenID := range tokenIDs {
		b.wait(b.clobLimiter)
		params := url.Values{}
		params.Set("token_id", tokenID)
		base := fmt.Sprintf("%s/prices-history", strings.TrimRight(b.clobURL, "/"))
		u := base + "?" + params.Encode()
		if extra := strings.TrimSpace(config.GetEnv("CLOB_PRICES_HISTORY_QUERY", "")); extra != "" {
			if strings.HasPrefix(extra, "?") {
				u = base + extra
			} else if strings.HasPrefix(extra, "&") {
				u = base + "?" + params.Encode() + extra
			} else {
				u = base + "?" + params.Encode() + "&" + extra
			}
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := b.http.Do(req)
		if err != nil {
			return events, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusTooManyRequests {
				b.sleepRetry(resp)
			}
			return events, &httpStatusError{service: "clob", code: resp.StatusCode, body: string(body)}
		}
		var arr []map[string]interface{}
		if err := json.Unmarshal(body, &arr); err != nil {
			// Some responses may be wrapped; ignore if not array
			continue
		}
		for _, p := range arr {
			p["market_id"] = conditionID
			p["token_id"] = tokenID
			p["source"] = "backfill"
			data, _ := json.Marshal(p)
			ts := time.Now().UTC()
			if tsVal, ok := p["timestamp"].(float64); ok {
				ts = time.Unix(int64(tsVal), 0)
			}
			events = append(events, schemas.HistoricalEvent{
				MarketID:  conditionID,
				Source:    "clob",
				EventType: "price",
				Data:      data,
				Timestamp: ts,
				Version:   schemas.VersionV1,
			})
		}
		time.Sleep(b.rateLimit)
	}
	return events, nil
}

type httpStatusError struct {
	service string
	code    int
	body    string
}

func (e *httpStatusError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("%s status %d", e.service, e.code)
	}
	return fmt.Sprintf("%s status %d: %s", e.service, e.code, e.body)
}

func isFatalHTTP(err error) bool {
	he, ok := err.(*httpStatusError)
	if !ok {
		return false
	}
	return he.code == http.StatusBadRequest || he.code == http.StatusNotFound
}

func isMaxOffsetError(err error) bool {
	he, ok := err.(*httpStatusError)
	if !ok {
		return false
	}
	if he.code != http.StatusBadRequest {
		return false
	}
	return strings.Contains(strings.ToLower(he.body), "max historical activity offset")
}

func newLimiter(d time.Duration) chan struct{} {
	if d <= 0 {
		return nil
	}
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	go func() {
		ticker := time.NewTicker(d)
		defer ticker.Stop()
		for range ticker.C {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}()
	return ch
}

func (b *BackfillService) wait(limiter <-chan struct{}) {
	if limiter == nil {
		return
	}
	<-limiter
}

func (b *BackfillService) sleepRetry(resp *http.Response) {
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter != "" {
		if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 {
			time.Sleep(time.Duration(secs) * time.Second)
			return
		}
	}
	time.Sleep(5 * time.Second)
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

func toStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			switch x := item.(type) {
			case string:
				out = append(out, x)
			case float64:
				out = append(out, strconv.FormatInt(int64(x), 10))
			}
		}
		return out
	case []string:
		return t
	}
	return nil
}


