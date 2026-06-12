package middleware

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/predsx/predsx/libs/logger"
	redisclient "github.com/predsx/predsx/libs/redis-client"
)

const defaultRPM = 60

// RateLimit is a Redis-backed sliding-window rate limiter (60 req/min per IP by default).
// On Redis failure it fails open so the service stays available.
func RateLimit(rdb redisclient.Interface, log logger.Interface) func(http.Handler) http.Handler {
	trustedProxies := parseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractClientIP(r, trustedProxies)
			key := fmt.Sprintf("rl:%s", ip)
			ctx := r.Context()

			count, err := rdb.Incr(ctx, key).Result()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if count == 1 {
				rdb.Expire(ctx, key, time.Minute)
			}

			remaining := defaultRPM - int(count)
			if remaining < 0 {
				remaining = 0
			}
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(defaultRPM))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			if int(count) > defaultRPM {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				http.Error(w, `{"error":"rate limit exceeded","retry_after":"60s"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// parseTrustedProxies parses a comma-separated list of CIDRs/IPs from env.
// If empty, X-Forwarded-For is never trusted.
func parseTrustedProxies(raw string) []*net.IPNet {
	if raw == "" {
		return nil
	}
	var nets []*net.IPNet
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			s += "/32"
		}
		_, ipNet, err := net.ParseCIDR(s)
		if err == nil {
			nets = append(nets, ipNet)
		}
	}
	return nets
}

// extractClientIP returns the real client IP. X-Forwarded-For is only trusted
// when the direct connection IP is in the TRUSTED_PROXIES list.
func extractClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}

	if len(trustedProxies) > 0 && isTrusted(remoteIP, trustedProxies) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.SplitN(xff, ",", 2)
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	return remoteIP
}

func isTrusted(ipStr string, nets []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
