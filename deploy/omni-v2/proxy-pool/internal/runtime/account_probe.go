package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/welsir/sub2api-v2-proxy-pool/internal/pool"
	"github.com/welsir/sub2api-v2-proxy-pool/internal/sub2api"
)

type AccountProbe struct {
	admin           Admin
	proxies         []sub2api.Proxy
	rollbackProxyID int64
	mu              sync.Mutex
	cursor          int
	accountLocks    map[int64]*sync.Mutex
}

func NewAccountProbe(admin Admin, proxies []sub2api.Proxy, rollbackProxyID int64) *AccountProbe {
	return &AccountProbe{
		admin: admin, proxies: append([]sub2api.Proxy(nil), proxies...), rollbackProxyID: rollbackProxyID,
		accountLocks: make(map[int64]*sync.Mutex),
	}
}

func (p *AccountProbe) Probe(ctx context.Context, group, node string) pool.ProbeResult {
	started := time.Now()
	index, err := strconv.Atoi(strings.TrimPrefix(group, "V2-PROBE-"))
	if err != nil || index < 1 || index > len(p.proxies) {
		return p.failure(started, fmt.Errorf("invalid probe group %q", group))
	}
	accounts, err := p.admin.ListEligibleOpenAIOAuth(ctx)
	if err != nil {
		return p.failure(started, fmt.Errorf("list dynamic V2 probe accounts: %w", err))
	}
	if len(accounts) == 0 {
		return p.failure(started, fmt.Errorf("no eligible V2 OAuth account for real SSE probe"))
	}
	p.mu.Lock()
	account := accounts[p.cursor%len(accounts)]
	p.cursor = (p.cursor + 1) % len(accounts)
	accountLock := p.accountLocks[account.ID]
	if accountLock == nil {
		accountLock = &sync.Mutex{}
		p.accountLocks[account.ID] = accountLock
	}
	p.mu.Unlock()
	accountLock.Lock()
	defer accountLock.Unlock()
	refreshed, err := p.admin.ListEligibleOpenAIOAuth(ctx)
	if err != nil {
		return p.failure(started, fmt.Errorf("refresh V2 probe account %d: %w", account.ID, err))
	}
	found := false
	for _, candidate := range refreshed {
		if candidate.ID == account.ID {
			account = candidate
			found = true
			break
		}
	}
	if !found {
		return p.failure(started, fmt.Errorf("selected V2 probe account %d is no longer eligible", account.ID))
	}
	oldProxyID := p.rollbackProxyID
	if account.ProxyID != nil && *account.ProxyID > 0 {
		oldProxyID = *account.ProxyID
	}
	probeProxyID := p.proxies[index-1].ID
	if err := p.admin.UpdateAccountProxy(ctx, account.ID, probeProxyID); err != nil {
		return p.failure(started, fmt.Errorf("route V2 account %d through %s (%s): %w", account.ID, group, node, err))
	}
	testErr := p.admin.TestAccount(ctx, account.ID, "gpt-5.4-mini", "hi")
	restoreErr := p.admin.UpdateAccountProxy(ctx, account.ID, oldProxyID)
	if restoreErr != nil {
		return p.failure(started, fmt.Errorf("restore V2 probe account %d to proxy %d: %w", account.ID, oldProxyID, restoreErr))
	}
	if testErr != nil {
		return p.failure(started, fmt.Errorf("real SSE probe via %s (%s): %w", group, node, testErr))
	}
	return pool.ProbeResult{
		Success: true, Reachable: true, HTTPStatus: 200,
		Duration: time.Since(started), Failure: pool.FailureNone,
	}
}

func (p *AccountProbe) failure(started time.Time, err error) pool.ProbeResult {
	return pool.ProbeResult{
		Duration: time.Since(started), Failure: pool.FailureTransport,
		Message: err.Error(), Err: err,
	}
}
