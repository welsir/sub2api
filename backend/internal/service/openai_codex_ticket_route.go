package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func codexTicketRoutes(cfg config.OpenAICodexTicketConfig) ([]string, error) {
	values := append([]string{cfg.HarvestProxyURL}, cfg.HarvestProxyURLs...)
	routes := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		if ValidateOpenAICodexTicketHarvestProxyURL(value) != nil {
			return nil, errors.New("invalid_harvest_route")
		}
		seen[value] = true
		routes = append(routes, value)
		if len(routes) > 32 {
			return nil, errors.New("too_many_harvest_routes")
		}
	}
	return routes, nil
}

func (s *OpenAIGatewayService) codexTicketRouteConfig(ctx context.Context) config.OpenAICodexTicketConfig {
	cfg := s.openAICodexTicketConfig()
	cfg.HarvestProxyURL = s.openAICodexTicketHarvestProxyURLContext(ctx)
	return cfg
}

func (t *openAICodexTicket) routeFor(account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) (string, bool) {
	if !t.valid(now, codexTicketTargetLength(account)) || t.AccountID != account.ID || t.CredentialKey != codexTicketCredentialKey(account) {
		return "", false
	}
	routes, err := codexTicketRoutes(cfg)
	if err != nil {
		return "", false
	}
	for _, route := range routes {
		if codexTicketDigest(route) == t.RouteKey {
			return route, true
		}
	}
	return "", false
}

func (s *OpenAIGatewayService) codexTicketUsable(account *Account, ticket *openAICodexTicket, now time.Time) bool {
	return s.codexTicketUsableContext(context.Background(), account, ticket, now)
}

func (s *OpenAIGatewayService) codexTicketUsableContext(ctx context.Context, account *Account, ticket *openAICodexTicket, now time.Time) bool {
	if ticket == nil || account == nil {
		return false
	}
	_, ok := ticket.routeFor(account, s.codexTicketRouteConfig(ctx), now)
	return ok
}

type codexTicketPause struct{ until time.Time }

func (s *OpenAIGatewayService) codexTicketPaused(account *Account) bool {
	if account == nil {
		return true
	}
	now := time.Now()
	if account.IsRateLimited() || account.IsOverloaded() ||
		(account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil)) ||
		(account.AutoPauseOnExpired && account.ExpiresAt != nil && !now.Before(*account.ExpiresAt)) {
		return true
	}
	key := openAICodexTicketKey(account.ID, codexTicketCredentialKey(account))
	if raw, ok := s.openaiCodexTicketPauses.Load(key); ok {
		p := raw.(codexTicketPause)
		if p.until.IsZero() || now.Before(p.until) {
			return true
		}
		s.openaiCodexTicketPauses.Delete(key)
	}
	return false
}

func (s *OpenAIGatewayService) pauseCodexTicket(account *Account, status int, retry time.Duration) {
	if account == nil || (status != 401 && status != 403 && status != 429) {
		return
	}
	if !isOpenAICodexTicketAccount(account) || !s.openAICodexTicketEnabled() {
		return
	}
	p := codexTicketPause{}
	if status == 429 {
		if retry < 3*time.Minute {
			retry = 3 * time.Minute
		}
		p.until = time.Now().Add(retry)
	}
	// Authentication failures remain stopped for this credential; replacing or
	// refreshing it changes the key. Never rotate proxies to work around limits.
	key := openAICodexTicketKey(account.ID, codexTicketCredentialKey(account))
	for {
		previous, loaded := s.openaiCodexTicketPauses.LoadOrStore(key, p)
		if !loaded {
			return
		}
		old := previous.(codexTicketPause)
		if old.until.IsZero() || (!p.until.IsZero() && !p.until.After(old.until)) {
			return
		}
		if s.openaiCodexTicketPauses.CompareAndSwap(key, previous, p) {
			return
		}
	}
}

func codexTicketRetryAfter(h http.Header) time.Duration {
	value := strings.TrimSpace(h.Get("Retry-After"))
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		return time.Until(date)
	}
	return 0
}

// Resolve from the exact injected state, not whichever ticket was refreshed
// most recently. Keep old bindings until expiry for concurrent requests.
func (s *OpenAIGatewayService) codexTicketOutboundProxy(ctx context.Context, account *Account, h http.Header, fallback string) (string, error) {
	state := h.Get(openAICodexTurnStateHeader)
	if state == "" || !isOpenAICodexTicketAccount(account) {
		return fallback, nil
	}
	raw, ok := s.openaiCodexTicketBindings.Load(codexTicketDigest(state))
	if !ok {
		return fallback, nil
	} // Client-owned state retains its existing route.
	t := raw.(*openAICodexTicket)
	proxy, valid := t.routeFor(account, s.codexTicketRouteConfig(ctx), time.Now())
	if !valid || s.codexTicketPaused(account) {
		return "", ErrOpenAICodexTicketUnavailable
	}

	// Set the same identity only after ordinary builders have finished applying
	// their defaults; otherwise the Astra harvest version may be downgraded.
	applyOpenAICodexTicketHarvestIdentity(h, t.Model)
	return proxy, nil
}

// A WebSocket handshake cannot replace its state on later turns. Ticket-managed
// accounts use the existing HTTP bridge so every turn validates state and route.
func (s *OpenAIGatewayService) codexTicketUsesHTTPBridge(ctx context.Context, account *Account) bool {
	return isOpenAICodexTicketAccount(account) && s.openAICodexTicketEnabledContext(ctx)
}
