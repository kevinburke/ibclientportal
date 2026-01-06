package ibclientportal

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitRule describes a per-endpoint rate/concurrency limit.
type RateLimitRule struct {
	Method        string
	PathPrefix    string
	MinInterval   time.Duration
	MaxConcurrent int
	PerAccount    bool
}

// RateLimiter throttles requests based on global and per-endpoint limits.
type RateLimiter struct {
	mu            sync.Mutex
	sem           map[string]chan struct{}
	rules         []RateLimitRule
	globalLimiter *rate.Limiter
	ruleLimiters  map[string]*rate.Limiter
}

// DefaultGlobalRateLimitInterval is the default global throttling interval.
// The IB Client Portal docs list a 10 req/sec global limit.
const DefaultGlobalRateLimitInterval = 100 * time.Millisecond

// DefaultRateLimitRules returns per-endpoint limits from the IB docs.
// These are not enabled by default; call EnableRateLimits to apply.
func DefaultRateLimitRules() []RateLimitRule {
	return []RateLimitRule{
		{Method: "GET", PathPrefix: "/fyi/unreadnumber", MinInterval: 1 * time.Second},
		{Method: "GET", PathPrefix: "/fyi/settings", MinInterval: 1 * time.Second},
		{Method: "POST", PathPrefix: "/fyi/settings", MinInterval: 1 * time.Second},
		{Method: "GET", PathPrefix: "/fyi/disclaimer", MinInterval: 1 * time.Second},
		{Method: "PUT", PathPrefix: "/fyi/disclaimer", MinInterval: 1 * time.Second},
		{Method: "GET", PathPrefix: "/fyi/deliveryoptions", MinInterval: 1 * time.Second},
		{Method: "PUT", PathPrefix: "/fyi/deliveryoptions/email", MinInterval: 1 * time.Second},
		{Method: "POST", PathPrefix: "/fyi/deliveryoptions/device", MinInterval: 1 * time.Second},
		{Method: "DELETE", PathPrefix: "/fyi/deliveryoptions", MinInterval: 1 * time.Second},
		{Method: "GET", PathPrefix: "/fyi/notifications", MinInterval: 1 * time.Second},
		{Method: "GET", PathPrefix: "/fyi/notifications/more", MinInterval: 1 * time.Second},
		{Method: "PUT", PathPrefix: "/fyi/notifications", MinInterval: 1 * time.Second},
		{Method: "GET", PathPrefix: "/iserver/account/orders", MinInterval: 5 * time.Second, PerAccount: true},
		{Method: "GET", PathPrefix: "/iserver/account/pnl/partitioned", MinInterval: 5 * time.Second, PerAccount: true},
		{Method: "GET", PathPrefix: "/iserver/account/trades", MinInterval: 5 * time.Second, PerAccount: true},
		{Method: "GET", PathPrefix: "/iserver/marketdata/history", MaxConcurrent: 5},
		{Method: "GET", PathPrefix: "/iserver/marketdata/snapshot", MinInterval: 100 * time.Millisecond},
		{Method: "GET", PathPrefix: "/iserver/scanner/params", MinInterval: 15 * time.Minute},
		{Method: "POST", PathPrefix: "/iserver/scanner/run", MinInterval: 1 * time.Second},
		{Method: "POST", PathPrefix: "/pa/performance", MinInterval: 15 * time.Minute, PerAccount: true},
		{Method: "POST", PathPrefix: "/pa/summary", MinInterval: 15 * time.Minute, PerAccount: true},
		{Method: "POST", PathPrefix: "/pa/transactions", MinInterval: 15 * time.Minute, PerAccount: true},
		{Method: "GET", PathPrefix: "/portfolio/accounts", MinInterval: 5 * time.Second},
		{Method: "GET", PathPrefix: "/portfolio/subaccounts", MinInterval: 5 * time.Second},
		{Method: "GET", PathPrefix: "/sso/validate", MinInterval: 1 * time.Minute},
		{Method: "GET", PathPrefix: "/tickle", MinInterval: 1 * time.Second},
	}
}

// NewRateLimiter returns a new RateLimiter with the provided rules.
func NewRateLimiter(rules []RateLimitRule, globalMinInterval time.Duration) *RateLimiter {
	normalized := normalizeRules(rules)
	limiter := (*rate.Limiter)(nil)
	if globalMinInterval > 0 {
		limiter = rate.NewLimiter(rate.Every(globalMinInterval), 1)
	}
	return &RateLimiter{
		sem:           make(map[string]chan struct{}),
		rules:         normalized,
		globalLimiter: limiter,
		ruleLimiters:  make(map[string]*rate.Limiter),
	}
}

// EnableRateLimits enables the default IB rate limits for this client.
func (c *Client) EnableRateLimits() {
	c.rateLimiter = NewRateLimiter(DefaultRateLimitRules(), DefaultGlobalRateLimitInterval)
}

// SetRateLimiter sets a custom rate limiter for this client (nil disables).
func (c *Client) SetRateLimiter(limiter *RateLimiter) {
	c.rateLimiter = limiter
}

// DisableRateLimits disables rate limiting for this client.
func (c *Client) DisableRateLimits() {
	c.rateLimiter = nil
}

// Wait enforces global and per-endpoint limits; it returns a release func for concurrency rules.
func (r *RateLimiter) Wait(ctx context.Context, method, path, accountID string) (func(), error) {
	if r == nil {
		return nil, nil
	}
	path = stripQuery(path)
	if r.globalLimiter != nil {
		if err := r.globalLimiter.Wait(ctx); err != nil {
			return nil, err
		}
	}
	rule, ok := r.matchRule(method, path)
	if !ok {
		return nil, nil
	}
	if rule.MinInterval > 0 {
		if limiter := r.limiterForRule(rule, accountID); limiter != nil {
			if err := limiter.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	if rule.MaxConcurrent > 0 {
		return r.acquire(ctx, ruleKeyWithAccount(rule, accountID), rule.MaxConcurrent)
	}
	return nil, nil
}

func (r *RateLimiter) acquire(ctx context.Context, key string, maxConcurrent int) (func(), error) {
	if maxConcurrent <= 0 {
		return nil, nil
	}
	r.mu.Lock()
	ch := r.sem[key]
	if ch == nil {
		ch = make(chan struct{}, maxConcurrent)
		r.sem[key] = ch
	}
	r.mu.Unlock()

	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *RateLimiter) matchRule(method, path string) (RateLimitRule, bool) {
	method = strings.ToUpper(method)
	path = stripQuery(path)
	bestLen := -1
	var best RateLimitRule
	for _, rule := range r.rules {
		if rule.Method != "" && rule.Method != method {
			continue
		}
		if !strings.HasPrefix(path, rule.PathPrefix) {
			continue
		}
		if len(rule.PathPrefix) > bestLen {
			bestLen = len(rule.PathPrefix)
			best = rule
		}
	}
	return best, bestLen >= 0
}

func normalizeRules(rules []RateLimitRule) []RateLimitRule {
	out := make([]RateLimitRule, 0, len(rules))
	for _, rule := range rules {
		if rule.PathPrefix == "" {
			continue
		}
		rule.Method = strings.ToUpper(rule.Method)
		out = append(out, rule)
	}
	return out
}

func ruleKey(rule RateLimitRule) string {
	return fmt.Sprintf("%s %s", strings.ToUpper(rule.Method), rule.PathPrefix)
}

func ruleKeyWithAccount(rule RateLimitRule, accountID string) string {
	if rule.PerAccount && accountID != "" {
		return fmt.Sprintf("%s %s %s", strings.ToUpper(rule.Method), rule.PathPrefix, accountID)
	}
	return ruleKey(rule)
}

func (r *RateLimiter) limiterForRule(rule RateLimitRule, accountID string) *rate.Limiter {
	if rule.MinInterval <= 0 {
		return nil
	}
	key := ruleKeyWithAccount(rule, accountID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if limiter, ok := r.ruleLimiters[key]; ok {
		return limiter
	}
	limiter := rate.NewLimiter(rate.Every(rule.MinInterval), 1)
	r.ruleLimiters[key] = limiter
	return limiter
}

func stripQuery(path string) string {
	if idx := strings.Index(path, "?"); idx >= 0 {
		return path[:idx]
	}
	return path
}
