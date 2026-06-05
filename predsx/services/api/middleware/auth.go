package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/predsx/predsx/libs/logger"
)

// Auth is a simple JWT placeholder middleware
func Auth(log logger.Interface) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				log.Warn("missing or invalid auth header", "path", r.URL.Path)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			// In production, validate JWT here
			next.ServeHTTP(w, r)
		})
	}
}

// DebugAuth gates /debug/* routes behind a static token set via DEBUG_TOKEN env var.
// If DEBUG_TOKEN is not set, all debug access is blocked.
// Pass the token via X-Debug-Token header or ?debug_token= query param.
func DebugAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := os.Getenv("DEBUG_TOKEN")
		if token == "" {
			http.Error(w, "debug endpoints disabled", http.StatusForbidden)
			return
		}
		provided := r.Header.Get("X-Debug-Token")
		if provided == "" {
			provided = r.URL.Query().Get("debug_token")
		}
		if provided != token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
