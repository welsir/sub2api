package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNativeImagesTicketExemptionScheduling(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, capability := range []OpenAIImagesCapability{OpenAIImagesCapabilityNative, OpenAIImagesCapabilityBasic} {
			t.Run(fmt.Sprintf("advanced=%t/%s", advanced, capability), func(t *testing.T) {
				resetOpenAIAdvancedSchedulerSettingCacheForTest()
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
				account := directImagesTestAccount()
				account.Status, account.Schedulable, account.Concurrency = StatusActive, true, 1
				svc := newOpenAICompactionSchedulerTestService([]Account{*account}, advanced)
				svc.cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
				groupID := int64(1)
				selection, _, err := svc.SelectAccountWithSchedulerForImages(context.Background(), &groupID, "", "gpt-image-2", nil, capability)
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, account.ID, selection.Account.ID)
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}

				// The identical account/model through the Responses entry is still gated.
				_, _, err = svc.SelectAccountWithSchedulerForCapability(context.Background(), &groupID, "", "", "gpt-image-2", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityResponses, false, false, false)
				require.Error(t, err)
				// Images admission still observes real account rate limits.
				until := time.Now().Add(time.Hour)
				account.RateLimitResetAt = &until
				svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{*account}}
				_, _, err = svc.SelectAccountWithSchedulerForImages(context.Background(), &groupID, "", "gpt-image-2", nil, capability)
				require.Error(t, err)
			})
		}
	}
}

func TestNativeImagesTicketExemptionUsesMappedModel(t *testing.T) {
	ctx := withOpenAIImagesRequest(context.Background())
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	for _, tc := range []struct {
		mapped  string
		blocked bool
	}{
		{"gpt-image-2.5-flare", false},
		{"gpt-image-1", true},
		{"gpt-5.6-sol", true},
	} {
		t.Run(tc.mapped, func(t *testing.T) {
			account := directImagesTestAccount()
			account.Credentials["model_mapping"] = map[string]any{"gpt-image-2": tc.mapped}
			require.Equal(t, tc.blocked, svc.isOpenAIAccountRequestRuntimeBlocked(ctx, account, "gpt-image-2", false))
		})
	}
}

func TestNativeImagesTicketExemptionForwarding(t *testing.T) {
	for _, edits := range []bool{false, true} {
		t.Run(fmt.Sprintf("edits=%t", edits), func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2","prompt":"blue circle","size":"1024x1024"}`)
			if edits {
				body = []byte(`{"model":"gpt-image-2","prompt":"blue circle","images":[{"image_url":"data:image/png;base64,AA=="}]}`)
			}
			c, rec := newOpenAIImagesTestContext(t, body)
			endpoint := "/backend-api/codex/images/generations"
			if edits {
				c.Request.URL.Path = "/v1/images/edits"
				endpoint = "/backend-api/codex/images/edits"
			}
			c.Request.Header.Set(openAICodexTurnStateHeader, "client-stale-ticket")
			upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
			svc := newOpenAIImagesTestService(upstream)
			svc.cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, GenerationDirect: true, HarvestProxyURL: "http://harvest.example:8080"}
			account := directImagesTestAccount()
			proxyID := int64(1)
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{Protocol: "http", Host: "account.example", Port: 8080}
			// A harvesting pause belongs to conversation tickets, not native image admission.
			svc.pauseCodexTicket(account, http.StatusForbidden, 0)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, endpoint, upstream.lastReq.URL.Path)
			require.Empty(t, upstream.lastReq.Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, "Bearer test-token", upstream.lastReq.Header.Get("Authorization"))
			require.Equal(t, "http://account.example:8080", upstream.lastProxyURL)
		})
	}
}

func TestNativeImagesTicketExemptionFallbackStillRequiresTicket(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				require.Equal(t, "/backend-api/codex/images/generations", req.URL.Path)
				require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"endpoint unavailable"}}`))}, nil
			}}
			svc := newOpenAIImagesTestService(upstream)
			svc.cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
			body := []byte(`{"model":"gpt-image-2","prompt":"blue circle"}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			_, err = svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
			require.Equal(t, 1, calls, "the fallback Responses request must not reach upstream without its own ticket")
		})
	}
}

func TestNativeImagesTicketExemptionCannotBeSpoofed(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	account := directImagesTestAccount()
	// Image model and URL alone cannot authorize a ticket exemption.
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/images/generations", nil)
	require.NoError(t, err)
	_, err = svc.doOpenAIUpstream(req, "", account)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	for _, model := range []string{"gpt-image-2", "gpt-5.4", "gpt-5.6-sol"} {
		body := []byte(fmt.Sprintf(`{"model":%q}`, model))
		c, _ := newOpenAIImagesTestContext(t, body)
		c.Request.URL.Path = "/v1/responses"
		c.Request.Header.Set("X-Images-Request", "true")
		_, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "test-token", true, "", false)
		require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	}
	// An internal Images context alone cannot bypass the Responses transport gate.
	req = req.WithContext(withOpenAIImagesRequest(context.Background()))
	req.URL.Path = "/backend-api/codex/responses"
	_, err = svc.doOpenAIUpstream(req, "", account)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
}
