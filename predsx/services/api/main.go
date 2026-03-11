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
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	svc := service.NewBaseService("api")

	svc.Run(context.Background(), func(ctx context.Context) error {
		// Config
		port := config.GetEnv("API_PORT", "8081")
		redisAddr := config.GetEnv("REDIS_ADDR", "localhost:6379")
		chAddr := config.GetEnv("CLICKHOUSE_ADDR", "localhost:9000")
		chUser := config.GetEnv("CLICKHOUSE_USER", "default")
		chPassword := config.GetEnv("CLICKHOUSE_PASSWORD", "")
		chDatabase := config.GetEnv("CLICKHOUSE_DB", "default")

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
		}

		// Router
		r := mux.NewRouter()

		// Public Routes
		r.Handle("/metrics", promhttp.Handler())
		r.HandleFunc("/health", h.GetHealth).Methods("GET")

		// Debug Endpoints (No Auth, No Prefix)
		r.HandleFunc("/debug/markets", h.GetDebugMarkets).Methods("GET")
		r.HandleFunc("/debug/markets/{id}", h.GetDebugMarket).Methods("GET")
		r.HandleFunc("/debug/trades", h.GetDebugTrades).Methods("GET")
		r.HandleFunc("/debug/orderbook/{id}", h.GetDebugOrderbook).Methods("GET")

		// Protected API Routes
		api := r.PathPrefix("/v1").Subrouter()
		api.Use(middleware.Auth(svc.Logger))

		api.HandleFunc("/markets", h.GetMarkets).Methods("GET")
		api.HandleFunc("/markets/{id}", h.GetMarket).Methods("GET")
		api.HandleFunc("/markets/{id}/orderbook", h.GetOrderbook).Methods("GET")
		api.HandleFunc("/markets/{id}/price", h.GetPrice).Methods("GET")

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
