package service

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func verifiedTestTicket(account *Account, state, proxy string) *openAICodexTicket {
	return &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", State: state, Length: len(state),
		CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), PolicyVersion: codexTicketPolicyVersion,
		ResponseModel: "gpt-6-astra", RouteKey: codexTicketDigest(proxy), CredentialKey: codexTicketCredentialKey(account)}
}

func TestCodexTicketAdmissionRejectsLegacyAnd312Override(t *testing.T) {
	account := ticketTestAccount(41)
	account.Credentials["plan_type"] = "pro"
	cfg := config.OpenAICodexTicketConfig{Enabled: true, TargetLength: 312, FailClosed: true}
	svc := ticketTestService(t, cfg, nil)
	for _, state := range []string{fakeCodexTicketState(312), openAICodexTicketStatePrefix + strings.Repeat("B", 286)} {
		svc.storeOpenAICodexTicket(context.Background(), account, verifiedTestTicket(account, state, ""))
		require.ErrorIs(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", http.Header{}), ErrOpenAICodexTicketUnavailable)
	}
	legacy := verifiedTestTicket(account, fakeCodexTicketState(292), "")
	legacy.PolicyVersion = 0
	svc.storeOpenAICodexTicket(context.Background(), account, legacy)
	require.True(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
	account.Extra = map[string]any{openAICodexTicketExtraKey("gpt-6-astra"): legacy}
	require.False(t, OpenAICodexTicketStatuses(account, cfg, time.Now())[0].Ready)
}

func TestCodexTicketAccountPolicyAndEnvelopeTime(t *testing.T) {
	account := ticketTestAccount(41)
	for _, plan := range []string{"pro", "plus", "", "team", "business", "enterprise"} {
		account.Credentials["plan_type"] = plan
		length := 292
		if plan == "team" || plan == "business" {
			length = 332
		}
		require.Equal(t, length, codexTicketTargetLength(account))
		ticket := verifiedTestTicket(account, fakeCodexTicketState(length), "")
		require.True(t, ticket.valid(time.Now(), length))
	}
	for _, issued := range []time.Time{time.Now().Add(-2 * time.Hour), time.Now().Add(time.Minute)} {
		raw, err := base64.URLEncoding.DecodeString(fakeCodexTicketState(292))
		require.NoError(t, err)
		binary.BigEndian.PutUint64(raw[1:9], uint64(issued.Unix()))
		ticket := verifiedTestTicket(account, base64.URLEncoding.EncodeToString(raw), "")
		require.False(t, ticket.valid(time.Now(), 292), "a fresh local expiry must not revive an old envelope")
	}
}

func TestCodexTicketProbeRequiresCompletedMatchingModel(t *testing.T) {
	for name, body := range map[string]string{
		"luna":                   ticketTestSSE("gpt-5.6-luna"),
		"header-only":            "",
		"done-only":              "data: [DONE]\n\n",
		"missing-model":          "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n",
		"conflicting-events":     "data: {\"type\":\"response.created\",\"response\":{\"model\":\"gpt-5.6-luna\"}}\n\n" + ticketTestSSE("gpt-6-astra"),
		"failure-before-success": "data: {\"type\":\"response.failed\"}\n\n" + ticketTestSSE("gpt-6-astra"),
		"failure-after-success":  ticketTestSSE("gpt-6-astra") + "data: {\"type\":\"response.failed\"}\n\n",
		"oversize":               strings.Repeat(" ", (1<<20)+1),
	} {
		t.Run(name, func(t *testing.T) {
			upstream := &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				resp := codexTicketResponse()
				resp.Body = io.NopCloser(strings.NewReader(body))
				return resp, nil
			}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://proxy.example:8080"}, upstream)
			account := ticketTestAccount(41)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
		})
	}
	require.NoError(t, validateCodexTicketStream(strings.NewReader("event: response.completed\r\ndata: {\"response\":\r\ndata: {\"model\":\"gpt-6-astra\",\"status\":\"completed\"}}\r\n\r\n"), "gpt-6-astra"))
}

func TestCodexTicketTransportUsesExactBoundRoute(t *testing.T) {
	proxy := "http://user:secret@harvest.example:8080"
	account := ticketTestAccount(41)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(ticketTestSSE("gpt-6-astra")))}}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, HarvestProxyURL: proxy}, upstream)
	ticket := verifiedTestTicket(account, fakeCodexTicketState(292), proxy)
	svc.storeOpenAICodexTicket(context.Background(), account, ticket)
	req, _ := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, ticket.Model, req.Header))
	// Refresh after building the request must not substitute another route/ticket.
	fresh := *ticket
	fresh.State = fakeCodexTicketState(332)
	svc.storeOpenAICodexTicket(context.Background(), account, &fresh)
	req.Header.Set("version", "0.146.0")
	resp, err := svc.doOpenAIUpstream(req, "http://different.example:8080", account)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, proxy, upstream.lastProxyURL)
	require.Equal(t, ticket.State, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
	require.Equal(t, openAICodexAstraMinVersion, upstream.requests[0].Header.Get("version"))
	// Removing/changing the configured route invalidates the old binding.
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://replacement.example:8080"
	_, err = svc.doOpenAIUpstream(req, "", account)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	require.Len(t, upstream.requests, 1)
}

func TestCodexTicketCredentialChangeInvalidatesTicket(t *testing.T) {
	account := ticketTestAccount(41)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	svc.storeOpenAICodexTicket(context.Background(), account, verifiedTestTicket(account, fakeCodexTicketState(292), ""))
	account.Credentials["access_token"] = "replacement"
	require.ErrorIs(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", http.Header{}), ErrOpenAICodexTicketUnavailable)
}

func TestCodexTicketRejectionStopsOtherModels(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: status, Header: http.Header{"Retry-After": []string{"900"}}, Body: io.NopCloser(strings.NewReader(""))}}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://proxy.example:8080"}, upstream)
			account := ticketTestAccount(41)
			account.Status = StatusActive
			svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
			svc.refreshOpenAICodexTickets(context.Background())
			svc.refreshOpenAICodexTickets(context.Background())
			require.Len(t, upstream.requests, 1)
			require.True(t, svc.codexTicketPaused(account))
			if status == 429 {
				raw, _ := svc.openaiCodexTicketPauses.Load(openAICodexTicketKey(account.ID, codexTicketCredentialKey(account)))
				require.Greater(t, time.Until(raw.(codexTicketPause).until), 14*time.Minute)
			}
		})
	}
}

func TestCodexTicketStreamRateLimitStopsHarvest(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://proxy.example:8080"}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		resp := codexTicketResponse()
		resp.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"usage_limit_reached\"}}}\n\n"))
		return resp, nil
	}})
	account := ticketTestAccount(41)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.True(t, svc.codexTicketPaused(account))
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func TestCodexTicketPoolFindsMatchingModelAndBindsSelectedRoute(t *testing.T) {
	bad := codexTicketResponse()
	bad.Body = io.NopCloser(strings.NewReader(ticketTestSSE("gpt-5.6-luna")))
	upstream := &httpUpstreamRecorder{responses: []*http.Response{bad, codexTicketResponse()}}
	first, second := "http://first.example:8080", "http://second.example:8080"
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, HarvestProxyURL: first, HarvestProxyURLs: []string{second}}
	svc := ticketTestService(t, cfg, upstream)
	account := ticketTestAccount(41)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Len(t, upstream.requests, 1)
	advanceCodexTicketBudgetForTest(svc, account)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Len(t, upstream.requests, 2)
	require.Equal(t, second, upstream.lastProxyURL)
	ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
	require.NotNil(t, ticket)
	require.Equal(t, codexTicketDigest(second), ticket.RouteKey)
	account.Extra = map[string]any{openAICodexTicketExtraKey(ticket.Model): ticket}
	require.True(t, OpenAICodexTicketStatuses(account, cfg, time.Now())[0].Ready)
	header := http.Header{}
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, ticket.Model, header))
	proxy, err := svc.codexTicketOutboundProxy(context.Background(), account, header, first)
	require.NoError(t, err)
	require.Equal(t, second, proxy)
}

func TestCodexTicketPoolStopsOnLimitWithoutTryingNextRoute(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, codexTicketResponse()}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://first.example:8080", HarvestProxyURLs: []string{"http://second.example:8080"}}, upstream)
	svc.probeOnceOpenAICodexTicket(context.Background(), ticketTestAccount(41), "gpt-6-astra")
	require.Len(t, upstream.requests, 1)
}

func TestCodexTicketWSBridgeInjectsEveryTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	proxy := "http://selected.example:8080"
	upstream := &httpUpstreamRecorder{responses: []*http.Response{codexTicketResponse(), codexTicketResponse()}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, HarvestProxyURL: proxy}, upstream)
	svc.cfg.Gateway.MaxLineSize = defaultMaxLineSize
	account := ticketTestAccount(41)
	ticket := verifiedTestTicket(account, fakeCodexTicketState(292), proxy)
	svc.storeOpenAICodexTicket(context.Background(), account, ticket)
	require.True(t, svc.codexTicketUsesHTTPBridge(context.Background(), account))
	for turn := 1; turn <= 2; turn++ {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		payload := []byte(`{"type":"response.create","model":"gpt-6-astra","input":"ping","stream":true}`)
		result, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "tok", payload, len(payload), "gpt-6-astra", "", "", "", "", turn, func([]byte) error { return nil })
		require.NoError(t, err)
		require.Equal(t, "gpt-6-astra", result.UpstreamResponseModel)
		require.Equal(t, proxy, upstream.lastProxyURL)
		require.Equal(t, ticket.State, upstream.lastReq.Header.Get(openAICodexTurnStateHeader))
	}
	require.Len(t, upstream.requests, 2)
	svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
	require.False(t, svc.codexTicketUsesHTTPBridge(context.Background(), account))
}

func TestStrictCodexAllModelsRequireOwnTicket(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"gpt-6-astra"}}, nil)
	account := ticketTestAccount(41)
	for _, model := range []string{"gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "future-model"} {
		t.Run(model, func(t *testing.T) {
			require.True(t, svc.openAICodexTicketBlocksAccount(account, model))
			require.ErrorIs(t, svc.applyOpenAICodexTicket(context.Background(), account, model, http.Header{}), ErrOpenAICodexTicketUnavailable)
		})
	}
}

func TestStrictCodexResponseDoesNotExposeUnverifiedSuccess(t *testing.T) {
	for name, body := range map[string]string{
		"valid-sse":     ticketTestSSE("gpt-6-astra"),
		"wrong-model":   ticketTestSSE("gpt-5.6-luna"),
		"truncated":     "data: {\"type\":\"response.created\"}\n\n",
		"late-failure":  ticketTestSSE("gpt-6-astra") + "data: {\"type\":\"response.failed\"}\n\n",
		"valid-json":    `{"model":"gpt-6-astra","status":"completed"}`,
		"wrong-json":    `{"model":"gpt-5.6-luna","status":"completed"}`,
		"missing-model": `{"status":"completed"}`,
	} {
		t.Run(name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, upstream)
			account := ticketTestAccount(41)
			ticket := verifiedTestTicket(account, fakeCodexTicketState(292), svc.openAICodexTicketHarvestProxyURL())
			svc.storeOpenAICodexTicket(context.Background(), account, ticket)
			req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
			require.NoError(t, err)
			require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, ticket.Model, req.Header))
			response, err := svc.doOpenAIUpstream(req, "", account)
			if strings.HasPrefix(name, "valid-") {
				require.NoError(t, err)
				got, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Equal(t, body, string(got))
				response.Body.Close()
			} else {
				require.ErrorIs(t, err, errStrictCodexResponse)
				require.Nil(t, response)
			}
		})
	}
}
