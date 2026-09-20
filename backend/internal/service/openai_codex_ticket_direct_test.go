package service

import (
	"context"
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

func TestCodexTicketDirectGenerationKeepsHarvestProxy(t *testing.T) {
	ctx := context.Background()
	proxy := "socks5h://residential.example:1080"
	account := ticketTestAccount(41)
	header := http.Header{}
	header.Set(openAICodexTurnStateHeader, fakeCodexTicketState(292))
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(ticketTestSSE("gpt-6-astra")))},
		codexTicketResponse(), codexTicketResponse(),
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, GenerationDirect: true, HarvestProxyURL: proxy}, upstream)
	svc.probeOnceOpenAICodexTicket(ctx, account, "gpt-6-astra")
	require.Equal(t, proxy, upstream.lastProxyURL)
	require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
	for _, passthrough := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		body := []byte(`{"model":"gpt-6-astra","stream":true,"input":"ping"}`)
		var req *http.Request
		var err error
		if passthrough {
			req, err = svc.buildUpstreamRequestOpenAIPassthrough(ctx, c, account, body, "tok")
		} else {
			req, err = svc.buildUpstreamRequest(ctx, c, account, body, "tok", true, "", true)
		}
		require.NoError(t, err)
		resp, err := svc.doOpenAIUpstream(req, "http://account-proxy.example:8080", account)
		require.NoError(t, err)
		resp.Body.Close()
		require.Empty(t, upstream.lastProxyURL)
		require.Equal(t, header.Get(openAICodexTurnStateHeader), upstream.lastReq.Header.Get(openAICodexTurnStateHeader))
	}
	require.Len(t, upstream.requests, 3)
}

func TestCodexTicketDirectGenerationPreservesAdmission(t *testing.T) {
	for _, failure := range []string{"missing", "expired", "credentials", "removed-route", "paused", "wrong-model"} {
		t.Run(failure, func(t *testing.T) {
			account := ticketTestAccount(41)
			upstream := &httpUpstreamRecorder{}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, GenerationDirect: true}, upstream)
			ticket := verifiedTestTicket(account, fakeCodexTicketState(292), ticketTestProxyURL)
			if failure == "expired" {
				ticket.ExpiresAt = time.Now().Add(-time.Second)
			}
			if failure != "missing" {
				svc.storeOpenAICodexTicket(context.Background(), account, ticket)
			}
			switch failure {
			case "credentials":
				account.Credentials["access_token"] = "changed"
			case "removed-route":
				svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://replacement.example:8080"
			case "paused":
				svc.pauseCodexTicket(account, http.StatusUnauthorized, 0)
			}
			model := "gpt-6-astra"
			if failure == "wrong-model" {
				model = "gpt-5.6-sol"
			}
			require.ErrorIs(t, svc.applyOpenAICodexTicket(context.Background(), account, model, http.Header{}), ErrOpenAICodexTicketUnavailable)
			require.Empty(t, upstream.requests)
		})
	}
}

func TestCodexTicketDirectGenerationPreservesResponseValidation(t *testing.T) {
	account := ticketTestAccount(41)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(ticketTestSSE("gpt-5.6-luna")))}}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, GenerationDirect: true}, upstream)
	svc.storeOpenAICodexTicket(context.Background(), account, verifiedTestTicket(account, fakeCodexTicketState(292), ticketTestProxyURL))
	req, _ := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", req.Header))
	resp, err := svc.doOpenAIUpstream(req, ticketTestProxyURL, account)
	require.ErrorIs(t, err, errStrictCodexResponse)
	require.Nil(t, resp)
	require.Empty(t, upstream.lastProxyURL)
}

func TestCodexTicketDirectGenerationDoesNotOverrideCompactProxy(t *testing.T) {
	proxy := "http://compact.example:8080"
	upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"output":[]}`))}}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, GenerationDirect: true, CompactProxyURL: proxy}, upstream)
	req, _ := http.NewRequest(http.MethodPost, chatgptCodexURL+"/compact", nil)
	resp, err := svc.doOpenAIUpstream(req, ticketTestProxyURL, ticketTestAccount(41))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, proxy, upstream.lastProxyURL)
}
