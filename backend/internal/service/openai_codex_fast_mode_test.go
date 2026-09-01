package service

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestShouldForceCodexFastScope(t *testing.T) {
	enabled := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{CodexForceFastEnabled: true}}}
	disabled := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{CodexForceFastEnabled: false}}}
	oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	setupToken := &Account{Platform: PlatformOpenAI, Type: AccountTypeSetupToken}
	apiKey := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	require.True(t, enabled.shouldForceCodexFast(oauth, true, false))
	require.True(t, enabled.shouldForceCodexFast(setupToken, true, false))
	require.False(t, enabled.shouldForceCodexFast(apiKey, true, false), "API billing must never be forced")
	require.False(t, enabled.shouldForceCodexFast(oauth, false, false), "non-Codex clients must remain unchanged")
	require.False(t, enabled.shouldForceCodexFast(oauth, true, true), "compact requests must remain unchanged")
	require.False(t, disabled.shouldForceCodexFast(oauth, true, false), "the runtime switch must be an immediate code-path off switch")
}

func TestCodexForceFastInjectsPriorityIntoHTTPBody(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	forceCtx := withOpenAICodexForceFast(context.Background())

	for _, body := range [][]byte{
		[]byte(`{"model":"gpt-5.6-sol","input":"hi"}`),
		[]byte(`{"model":"gpt-5.6-sol","input":"hi","service_tier":"default"}`),
		[]byte(`{"model":"gpt-5.6-sol","input":"hi","service_tier":"flex"}`),
	} {
		updated, err := svc.applyOpenAIFastPolicyToBody(forceCtx, account, "gpt-5.6-sol", body)
		require.NoError(t, err)
		require.Equal(t, OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String())
	}

	unchanged := []byte(`{"model":"gpt-5.6-sol","input":"hi"}`)
	updated, err := svc.applyOpenAIFastPolicyToBody(context.Background(), account, "gpt-5.6-sol", unchanged)
	require.NoError(t, err)
	require.Equal(t, unchanged, updated)
}

func TestCodexForceFastInjectsPriorityIntoWebSocketResponseCreate(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	forceCtx := withOpenAICodexForceFast(context.Background())

	frame := []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"hi"}`)
	updated, blocked, err := svc.applyOpenAIFastPolicyToWSResponseCreate(forceCtx, account, "gpt-5.6-sol", frame)
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String())

	cancelFrame := []byte(`{"type":"response.cancel"}`)
	updated, blocked, err = svc.applyOpenAIFastPolicyToWSResponseCreate(forceCtx, account, "gpt-5.6-sol", cancelFrame)
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, cancelFrame, updated, "non-create websocket frames must never be modified")
}

func TestCodexForceFastStillHonorsExplicitFilterPolicy(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, openAIFastFilterPriorityPolicy())
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	forceCtx := withOpenAICodexForceFast(context.Background())

	updated, err := svc.applyOpenAIFastPolicyToBody(forceCtx, account, "gpt-5.6-sol", []byte(`{"model":"gpt-5.6-sol"}`))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists(), "admin safety policy must retain the final veto")
}

func TestCodexForceFastHTTPIntegrationScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		enabled     bool
		path        string
		userAgent   string
		accountType string
		wantFast    bool
	}{
		{name: "official Codex OAuth", enabled: true, path: "/v1/responses", userAgent: "codex_cli_rs/0.144.1", accountType: AccountTypeOAuth, wantFast: true},
		{name: "switch disabled", enabled: false, path: "/v1/responses", userAgent: "codex_cli_rs/0.144.1", accountType: AccountTypeOAuth, wantFast: false},
		{name: "non-Codex OAuth", enabled: true, path: "/v1/responses", userAgent: "curl/8.7.1", accountType: AccountTypeOAuth, wantFast: false},
		{name: "official Codex API key", enabled: true, path: "/v1/responses", userAgent: "codex_cli_rs/0.144.1", accountType: AccountTypeAPIKey, wantFast: false},
		{name: "compact excluded", enabled: true, path: "/v1/responses/compact", userAgent: "codex_cli_rs/0.144.1", accountType: AccountTypeOAuth, wantFast: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.6-sol","stream":false,"instructions":"test","input":"hello"}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("User-Agent", tt.userAgent)

			upstream := &httpUpstreamRecorder{err: errors.New("stop after capture")}
			svc := &OpenAIGatewayService{
				cfg: &config.Config{
					Gateway:  config.GatewayConfig{CodexForceFastEnabled: tt.enabled},
					Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}},
				},
				httpUpstream: upstream,
			}
			account := &Account{
				ID: 81, Name: "scope-test", Platform: PlatformOpenAI, Type: tt.accountType, Concurrency: 1,
				Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc", "api_key": "sk-test"},
				Extra:       map[string]any{"openai_responses_supported": true}, Status: StatusActive, Schedulable: true,
			}

			result, err := svc.Forward(context.Background(), c, account, body)
			require.Error(t, err)
			require.Nil(t, result)
			require.NotEmpty(t, upstream.lastBody)
			if tt.wantFast {
				require.Equal(t, OpenAIFastTierPriority, gjson.GetBytes(upstream.lastBody, "service_tier").String())
			} else {
				require.False(t, gjson.GetBytes(upstream.lastBody, "service_tier").Exists())
			}
		})
	}
}
