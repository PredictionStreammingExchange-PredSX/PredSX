package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	"github.com/predsx/predsx/libs/service"
)

type BacktestRequest struct {
	MarketID  string  `json:"market_id"`
	Threshold float64 `json:"threshold"` // Volume spike threshold
	Capital   float64 `json:"capital"`
	Days      int     `json:"days"`
}

type BacktestResult struct {
	MarketID    string  `json:"market_id"`
	TradesTaken int     `json:"trades_taken"`
	FinalCapital float64 `json:"final_capital"`
	PnL         float64 `json:"pnl"`
	PnLPercent  float64 `json:"pnl_percent"`
	Error       string  `json:"error,omitempty"`
}

type Backtester struct {
	svc *service.BaseService
	ch  clickhouse.Interface
}

func main() {
	svc := service.NewBaseService("backtester")

	svc.Run(context.Background(), func(ctx context.Context) error {
		port := config.GetEnv("BACKTEST_PORT", "8082")
		chAddr := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase := config.GetEnv("CLICKHOUSE_DB", "default")

		ch, err := clickhouse.NewClient(clickhouse.Options{
			Addr:     chAddr,
			User:     chUser,
			Password: chPassword,
			Database: chDatabase,
		}, svc.Logger)
		if err != nil {
			return fmt.Errorf("failed to connect to clickhouse: %w", err)
		}

		b := &Backtester{svc: svc, ch: ch}

		r := mux.NewRouter()
		r.HandleFunc("/api/v1/backtest/run", b.RunBacktest).Methods("POST", "OPTIONS")

		// CORS Middleware
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
			})
		})

		srv := &http.Server{
			Addr:    ":" + port,
			Handler: r,
		}

		svc.Logger.Info("Backtest engine starting", "port", port)

		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				svc.Logger.Error("listen error", "error", err)
			}
		}()

		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})
}

func (b *Backtester) RunBacktest(w http.ResponseWriter, r *http.Request) {
	var req BacktestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Capital == 0 {
		req.Capital = 1000.0 // Default capital
	}
	if req.Days == 0 {
		req.Days = 7
	}
	if req.Threshold == 0 {
		req.Threshold = 5.0
	}

	res := BacktestResult{
		MarketID: req.MarketID,
		FinalCapital: req.Capital,
	}

	// Calculate timestamp range
	endTime := time.Now().UTC()
	startTime := endTime.AddDate(0, 0, -req.Days)

	// Fetch raw events (trades) from ClickHouse
	query := `
		SELECT timestamp, data 
		FROM events_raw 
		WHERE market_id = ? AND timestamp >= ? AND timestamp <= ? AND type = 'predsx.trades'
		ORDER BY timestamp ASC
	`
	rows, err := b.ch.Query(r.Context(), query, req.MarketID, startTime, endTime)
	if err != nil {
		b.svc.Logger.Error("clickhouse query error", "error", err)
		res.Error = "Database error"
		json.NewEncoder(w).Encode(res)
		return
	}
	defer rows.Close()

	var emaVolume float64
	var emaPrice float64
	alpha := 0.1

	capital := req.Capital
	tradesTaken := 0

	for rows.Next() {
		var ts time.Time
		var data string
		if err := rows.Scan(&ts, &data); err != nil {
			continue
		}

		var tradeData map[string]interface{}
		if err := json.Unmarshal([]byte(data), &tradeData); err != nil {
			continue
		}

		price, _ := tradeData["price"].(float64)
		size, _ := tradeData["size"].(float64)

		if price <= 0 || size <= 0 {
			continue
		}

		// Simple volume spike signal logic
		if emaVolume == 0 {
			emaVolume = size
			emaPrice = price
			continue
		}

		if size > emaVolume * req.Threshold {
			// Signal triggered! Simulate taking a position
			// Let's assume we buy $100 worth of shares at the current price
			betAmount := 100.0
			if capital >= betAmount {
				// We hold the position until the price increases by 5% or decreases by 5%
				// For a simplified backtest without full orderbook simulation, 
				// we just calculate a simulated win/loss ratio based on historical bias.
				// In a real backtest, we'd add it to an open positions array and close it later.
				// Here, we just do a mock calculation based on the momentum (price > emaPrice)
				
				if price > emaPrice {
					// Momentum is up, we win +10%
					capital += betAmount * 0.10
				} else {
					// Momentum is down, we lose -10%
					capital -= betAmount * 0.10
				}
				tradesTaken++
			}
		}

		// Update EMA
		emaVolume = (size * alpha) + (emaVolume * (1 - alpha))
		emaPrice = (price * alpha) + (emaPrice * (1 - alpha))
	}

	res.TradesTaken = tradesTaken
	res.FinalCapital = capital
	res.PnL = capital - req.Capital
	res.PnLPercent = (res.PnL / req.Capital) * 100

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
