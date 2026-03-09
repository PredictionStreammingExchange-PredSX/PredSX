package middleware

import (
	"net/http"
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
