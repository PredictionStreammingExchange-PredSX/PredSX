package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/predsx/predsx/libs/config"
	"github.com/predsx/predsx/libs/logger"
)

type Config struct {
	Name        string
	LogLevel    string
	MetricsPort int
}

type BaseService struct {
	Config *Config
	Logger logger.Interface
	Server *http.Server
}

func NewBaseService(name string) *BaseService {
	logLevel := config.GetEnv("LOG_LEVEL", "info")
	metricsPort := config.GetEnvInt("METRICS_PORT", 8080)

	log := logger.NewLogger(logLevel)
	log = log.With("service", name)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", metricsPort),
		Handler: mux,
	}

	return &BaseService{
		Config: &Config{
			Name:        name,
			LogLevel:    logLevel,
			MetricsPort: metricsPort,
		},
		Logger: log,
		Server: server,
	}
}

func (s *BaseService) Run(ctx context.Context, startFunc func(ctx context.Context) error) {
	s.Logger.Info("starting service", "metrics_port", s.Config.MetricsPort)

	// Start metrics server
	go func() {
		if err := s.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.Logger.Error("metrics server failed", "error", err)
		}
	}()

	// Watch for signals
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start service logic
	errCh := make(chan error, 1)
	go func() {
		errCh <- startFunc(sigCtx)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			s.Logger.Error("service failed", "error", err)
		}
	case <-sigCtx.Done():
		s.Logger.Info("shutting down gracefully")
	}

	// Shutdown metrics server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Server.Shutdown(shutdownCtx); err != nil {
		s.Logger.Error("metrics server shutdown failed", "error", err)
	}

	s.Logger.Info("service stopped")
}
