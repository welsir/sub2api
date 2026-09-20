package service

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func advanceCodexTicketBudgetForTest(s *OpenAIGatewayService, a *Account) {
	b := s.codexTicketBudget(a)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next = b.next.Add(-time.Minute)
	for model, starts := range b.models {
		for i := range starts {
			starts[i] = starts[i].Add(-time.Minute)
		}
		b.models[model] = starts
	}
	for i := range b.starts {
		b.starts[i] = b.starts[i].Add(-time.Minute)
	}
}

func TestCodexTicketAccountRateWindowAndSpacing(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, nil)
	a := ticketTestAccount(41)
	now := time.Now()
	for i := 0; i < 4; i++ {
		start := now.Add(time.Duration(i) * 15 * time.Second)
		require.True(t, svc.takeCodexTicketProbeSlot(a, "gpt-6-astra", start))
		// Even after 15 seconds, a pending probe blocks concurrent models.
		require.False(t, svc.takeCodexTicketProbeSlot(a, "gpt-6-astra", start.Add(15*time.Second)))
		svc.releaseCodexTicketProbeSlot(a)
		require.False(t, svc.takeCodexTicketProbeSlot(a, "gpt-6-astra", start.Add(14*time.Second)))
	}
	require.False(t, svc.takeCodexTicketProbeSlot(a, "gpt-6-astra", now.Add(59*time.Second)))
	// A refreshed credential cannot reset the account's recent request budget.
	a.Credentials["access_token"] = "refreshed"
	require.False(t, svc.takeCodexTicketProbeSlot(a, "gpt-6-astra", now.Add(59*time.Second)))
	require.True(t, svc.takeCodexTicketProbeSlot(a, "gpt-6-astra", now.Add(time.Minute)))
	other := ticketTestAccount(42)
	other.Credentials["chatgpt_account_id"] = "other-account"
	require.True(t, svc.takeCodexTicketProbeSlot(other, "gpt-6-astra", now))
}

func TestCodexTicketConcurrentModelsShareSingleProbeSlot(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, nil)
	a := ticketTestAccount(41)
	var wg sync.WaitGroup
	var admitted atomic.Int64
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if svc.takeCodexTicketProbeSlot(a, "gpt-6-astra", time.Now()) {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int64(1), admitted.Load())
}

func agedCodexTicket(t *testing.T, account *Account, age time.Duration) *openAICodexTicket {
	t.Helper()
	state, err := base64.URLEncoding.DecodeString(fakeCodexTicketState(292))
	require.NoError(t, err)
	issued := time.Now().Add(-age).Truncate(time.Second)
	binary.BigEndian.PutUint64(state[1:9], uint64(issued.Unix()))
	ticket := verifiedTestTicket(account, base64.URLEncoding.EncodeToString(state), ticketTestProxyURL)
	ticket.ExpiresAt = issued.Add(time.Hour)
	ticket.CapturedAt = issued
	return ticket
}

func TestCodexTicketReserveSurvivesRestartAndKeepsOriginalExpiry(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	a := ticketTestAccount(41)
	repo := &codexTicketRefreshRepo{}
	svc.accountRepo = repo
	active := agedCodexTicket(t, a, 30*time.Minute)
	reserve := agedCodexTicket(t, a, 20*time.Minute)
	svc.offerCodexTicket(context.Background(), a, active)
	svc.offerCodexTicket(context.Background(), a, reserve)
	require.Equal(t, active.State, svc.lookupOpenAICodexTicket(a, active.Model).State)
	require.Equal(t, reserve.State, svc.codexTicketReserve(a, active.Model).State)
	a.Extra = repo.updates
	restarted := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, RefreshBeforeSeconds: 1900}, nil)
	got := restarted.lookupOpenAICodexTicket(a, active.Model)
	require.Equal(t, reserve.State, got.State)
	require.True(t, reserve.ExpiresAt.Equal(got.ExpiresAt))
	require.InDelta(t, 40*60, time.Until(got.ExpiresAt).Seconds(), 2)
	require.Nil(t, restarted.codexTicketReserve(a, active.Model))
	require.Equal(t, reserve.State, restarted.lookupOpenAICodexTicket(a, active.Model).State)
	// Changing unrelated scheduling configuration preserves this still-valid ticket.
	restarted.cfg.Gateway.OpenAICodexTicket.AccountProbesPerMinute = 3
	require.Equal(t, reserve.State, restarted.lookupOpenAICodexTicket(a, active.Model).State)
}

func TestCodexTicketColdWaitSharesHarvestAndCancels(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, ColdWaitSeconds: 1}, nil)
	svc.accountRepo = &codexTicketRefreshRepo{}
	a := ticketTestAccount(41)
	require.False(t, svc.openAICodexTicketBlocksAccount(a, "gpt-6-astra"))
	require.True(t, svc.openAICodexTicketBlocksAccount(a, "unused-model"))
	var wg sync.WaitGroup
	results := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- svc.applyOpenAICodexTicket(context.Background(), a, "gpt-6-astra", http.Header{})
		}()
	}
	svc.offerCodexTicket(context.Background(), a, agedCodexTicket(t, a, time.Minute))
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	// The wait path did not create any probe budgets / upstream work.
	budgets := 0
	svc.openaiCodexTicketBudgets.Range(func(_, _ any) bool { budgets++; return true })
	require.Zero(t, budgets)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, svc.applyOpenAICodexTicket(ctx, a, "gpt-5.6-sol", http.Header{}), ErrOpenAICodexTicketUnavailable)
	require.ErrorIs(t, svc.applyOpenAICodexTicket(context.Background(), a, "gpt-5.6-sol", http.Header{}), ErrOpenAICodexTicketUnavailable)
}

func TestCodexTicketCompactUsesStableRouteWithoutGenerationState(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "compact-path", true: "native-trigger"}[native], func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"output":[{"type":"compaction","encrypted_content":"test"}]}`))}}}
			cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, CompactProxyURL: "http://stable.example:8080"}
			svc := ticketTestService(t, cfg, upstream)
			a := ticketTestAccount(41)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			path := "/v1/responses/compact"
			body := []byte(`{"model":"gpt-6-astra"}`)
			if native {
				path = "/v1/responses"
				body = []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction_trigger"}]}`)
			}
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
			req, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(string(body)))
			req.Header.Set(openAICodexTurnStateHeader, "client-state")
			require.NoError(t, svc.prepareCodexTicketRequest(context.Background(), c, a, body, req))
			require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
			resp, err := svc.doOpenAIUpstream(req, "http://harvest.example:8080", a)
			require.NoError(t, err)
			resp.Body.Close()
			require.Equal(t, cfg.CompactProxyURL, upstream.lastProxyURL)
			svc.cfg.Gateway.OpenAICodexTicket.CompactProxyURL = ""
			_, err = svc.doOpenAIUpstream(req, "", a)
			require.ErrorIs(t, err, errCodexCompactRoute)
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestCodexTicketAcceptsTwentyOneRoutes(t *testing.T) {
	cfg := config.OpenAICodexTicketConfig{}
	for i := 0; i < 21; i++ {
		cfg.HarvestProxyURLs = append(cfg.HarvestProxyURLs, "http://proxy"+strings.Repeat("a", i+1)+".example:8080")
	}
	routes, err := codexTicketRoutes(cfg)
	require.NoError(t, err)
	require.Len(t, routes, 21)
}

func TestCodexTicketTwoModelsEachFourAccountEight(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, nil)
	a := ticketTestAccount(41)
	now := time.Now()
	for i := 0; i < 8; i++ {
		model := "gpt-6-astra"
		if i%2 == 1 {
			model = "gpt-5.6-sol"
		}
		require.True(t, svc.takeCodexTicketProbeSlot(a, model, now.Add(time.Duration(i)*7500*time.Millisecond)))
		svc.releaseCodexTicketProbeSlot(a)
	}
	require.False(t, svc.takeCodexTicketProbeSlot(a, "gpt-6-astra", now.Add(59*time.Second)))
	require.False(t, svc.takeCodexTicketProbeSlot(a, "gpt-5.6-sol", now.Add(59*time.Second)))
	require.True(t, svc.takeCodexTicketProbeSlot(a, "gpt-6-astra", now.Add(time.Minute)))
}

func TestCodexTicketCompactExplicitDirect(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"output":[]}`))}}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, CompactProxyURL: "direct"}, upstream)
	req, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses/compact", nil)
	response, err := svc.doOpenAIUpstream(req, "http://ignored.example:8080", ticketTestAccount(41))
	require.NoError(t, err)
	response.Body.Close()
	require.Empty(t, upstream.lastProxyURL)
}
