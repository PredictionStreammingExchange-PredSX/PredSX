package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/config"
	redisclient "github.com/predsx/predsx/libs/redis-client"
	"github.com/predsx/predsx/libs/service"
	"github.com/predsx/predsx/services/api/handlers"
	"github.com/predsx/predsx/services/api/middleware"
	"github.com/predsx/predsx/services/api/ws"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	svc := service.NewBaseService("api")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Config
		port := config.GetEnv("API_PORT", "8088")
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
		chAddr := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase := config.GetEnv("CLICKHOUSE_DB", "default")
		gammaBaseURL := config.GetEnv("GAMMA_API_BASE_URL", "https://gamma-api.polymarket.com")
		dataAPIURL := config.GetEnv("DATA_API_URL", "https://data-api.polymarket.com")
		clobAPIURL := config.GetEnv("CLOB_API_URL", "https://clob.polymarket.com")

		// Clients
		rdb := redisclient.NewClient(redisclient.Options{Addr: redisAddr}, svc.Logger)
		ch, err := clickhouse.NewClient(clickhouse.Options{
			Addr:     chAddr,
			User:     chUser,
			Password: chPassword,
			Database: chDatabase,
		}, svc.Logger)
		if err != nil {
			return fmt.Errorf("failed to connect to clickhouse: %w", err)
		}

		h := &handlers.APIHandler{
			Redis:      rdb,
			ClickHouse: ch,
			Logger:     svc.Logger,
			GammaURL:   gammaBaseURL,
			DataURL:    dataAPIURL,
			ClobURL:    clobAPIURL,
		}

		// Router
		r := mux.NewRouter()
		r.Use(middleware.CORS)
		r.Use(middleware.SecurityHeaders)

		// Public Routes
		r.HandleFunc("/health", h.GetHealth).Methods("GET")

		// Metrics — gated behind DEBUG_TOKEN same as /debug/* routes
		r.Handle("/metrics", middleware.DebugAuth(promhttp.Handler()))

		// WebSocket Gateway
		hub := ws.NewHub(svc.Logger)
		go hub.Run(ctx)
		r.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
			ws.ServeWS(hub, w, r, svc.Logger)
		})

		// Real-time Kafka → WebSocket broadcaster
		go ws.StartKafkaBroadcaster(ctx, hub, kafkaBrokers, svc.Logger)

		// Debug Endpoints — gated by X-Debug-Token header (set DEBUG_TOKEN env var)
		debug := r.PathPrefix("/debug").Subrouter()
		debug.Use(middleware.DebugAuth)
		debug.HandleFunc("/markets", h.GetDebugMarkets).Methods("GET")
		debug.HandleFunc("/markets/{id}", h.GetDebugMarket).Methods("GET")
		debug.HandleFunc("/trades", h.GetDebugTrades).Methods("GET")
		debug.HandleFunc("/orderbook/{id}", h.GetDebugOrderbook).Methods("GET")
		debug.HandleFunc("/signals", h.GetDebugSignals).Methods("GET")
		debug.HandleFunc("/signals/{id}", h.GetDebugSignalsByMarket).Methods("GET")

		// Protected API Routes — rate-limited at 60 req/min per IP
		api := r.PathPrefix("/v1").Subrouter()
		api.Use(middleware.RateLimit(rdb, svc.Logger))
		// api.Use(middleware.Auth(svc.Logger))

		api.HandleFunc("/markets", h.GetMarkets).Methods("GET")
		api.HandleFunc("/markets/search", h.SearchMarkets).Methods("GET")
		api.HandleFunc("/markets/top", h.GetTopMarkets).Methods("GET")
		api.HandleFunc("/prices", h.GetBatchPrices).Methods("GET")
		api.HandleFunc("/events", h.GetEvents).Methods("GET")
		api.HandleFunc("/markets/{id}", h.GetMarket).Methods("GET")
		api.HandleFunc("/markets/{id}/orderbook", h.GetOrderbook).Methods("GET")
		api.HandleFunc("/markets/{id}/price", h.GetPrice).Methods("GET")
		api.HandleFunc("/markets/{id}/price-history", h.GetMarketPriceHistory).Methods("GET")
		api.HandleFunc("/markets/{id}/trades", h.GetMarketTrades).Methods("GET")
		api.HandleFunc("/markets/{id}/summary", h.GetMarketSummary).Methods("GET")
		api.HandleFunc("/markets/{id}/stats", h.GetMarketStats).Methods("GET")
		api.HandleFunc("/markets/{id}/candles", h.GetCandles).Methods("GET")
		api.HandleFunc("/markets/{id}/positions", h.GetMarketPositions).Methods("GET")
		api.HandleFunc("/markets/{id}/related", h.GetRelatedMarkets).Methods("GET")
		api.HandleFunc("/positions", h.GetPositions).Methods("GET")
		api.HandleFunc("/positions/closed", h.GetClosedPositions).Methods("GET")
		api.HandleFunc("/markets/{id}/signals", h.GetSignalsByMarket).Methods("GET")

		// GraphQL Placeholder
		r.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"data": {"message": "GraphQL API placeholder"}}`))
		}).Methods("POST")

		// Debugging router setup
		r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
			path, _ := route.GetPathTemplate()
			svc.Logger.Info("Registered route", "path", path)
			return nil
		})

		srv := &http.Server{
			Handler:      r,
			Addr:         ":" + port,
			WriteTimeout: 15 * time.Second,
			ReadTimeout:  15 * time.Second,
			IdleTimeout:  60 * time.Second,
			MaxHeaderBytes: 1 << 16, // 64 KB
		}

		svc.Logger.Info("API server starting", "port", port)

		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				svc.Logger.Error("listen error", "error", err)
			}
		}()

		// Graceful shutdown handled by BaseService
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})
}
