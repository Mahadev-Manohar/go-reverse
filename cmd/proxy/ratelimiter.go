
package main

import (
	"net"
	"net/http"
	"sync"
	"time"
	"context"
	"math"
	"strconv"
)

type ClientBucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	clients  map[string]*ClientBucket
	rate     float64
	capacity float64
}

func NewRateLimiter(rate, capacity float64) *RateLimiter {
	return &RateLimiter{
		clients:  make(map[string]*ClientBucket),
		rate:     rate,
		capacity: capacity,
	}
}


func (rl *RateLimiter) Allow(ip string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	bucket, exists := rl.clients[ip]

	if !exists {
		bucket = &ClientBucket{
			tokens:     rl.capacity,
			lastRefill: now,
			lastSeen:   now,
		}
		rl.clients[ip] = bucket
	}

	bucket.lastSeen = now

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens += elapsed * rl.rate

	if bucket.tokens > rl.capacity {
		bucket.tokens = rl.capacity
	}

	bucket.lastRefill = now

	// Reject if fewer than one token is available.
	if bucket.tokens < 1 {
		missingTokens := 1 - bucket.tokens
		waitSeconds := missingTokens / rl.rate

		return false, time.Duration(waitSeconds * float64(time.Second))
	}

	bucket.tokens--

	return true, 0
}


func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}


func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)

		allowed, wait := rl.Allow(ip)

		if !allowed {
			// Retry-After requires whole seconds.
			retrySeconds := int(math.Ceil(wait.Seconds()))

			if retrySeconds < 1 {
				retrySeconds = 1
			}

			w.Header().Set(
				"Retry-After",
				strconv.Itoa(retrySeconds),
			)

			http.Error(
				w,
				"Rate limit exceeded",
				http.StatusTooManyRequests,
			)
			return
		}

		next.ServeHTTP(w, r)
	})
}


func (rl *RateLimiter) CleanupInactiveClients(ctx context.Context, idleTimeout time.Duration, cleanupInterval time.Duration) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()

			rl.mu.Lock()

			for ip, bucket := range rl.clients {
				if now.Sub(bucket.lastSeen) >= idleTimeout {
					delete(rl.clients, ip)
				}
			}

			rl.mu.Unlock()

		case <-ctx.Done():
			return
		}
	}
}