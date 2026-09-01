package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func newOpenAIRequestCompressionTestService(enabled bool, minBytes int) *OpenAIGatewayService {
	return &OpenAIGatewayService{cfg: &config.Config{
		Gateway: config.GatewayConfig{
			OpenAIRequestCompressionEnabled:  enabled,
			OpenAIRequestCompressionMinBytes: minBytes,
		},
	}}
}

func decodeZstdRequestBody(t *testing.T, req *http.Request) []byte {
	t.Helper()
	wireBody, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	t.Cleanup(decoder.Close)
	decoded, err := decoder.DecodeAll(wireBody, nil)
	require.NoError(t, err)
	return decoded
}

func TestMaybeCompressOpenAIRequestBodyCompressesCodexOAuthLikeAccounts(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6","stream":true,"input":"` + strings.Repeat("large context ", 200) + `"}`)
	svc := newOpenAIRequestCompressionTestService(true, 64)

	accounts := []*Account{
		{ID: 1, Type: AccountTypeOAuth},
		{ID: 2, Type: AccountTypeSetupToken, Platform: PlatformOpenAI},
	}
	for _, account := range accounts {
		compressed, ok := svc.maybeCompressOpenAIRequestBody(context.Background(), account, chatgptCodexURL, body, true)
		require.True(t, ok, "account type %s should use the ChatGPT Codex compression contract", account.Type)
		require.Less(t, len(compressed), len(body))
		decoder, err := zstd.NewReader(nil)
		require.NoError(t, err)
		decoded, err := decoder.DecodeAll(compressed, nil)
		decoder.Close()
		require.NoError(t, err)
		require.Equal(t, body, decoded)
	}
}

func TestShouldCompressOpenAIRequestBodyKeepsConservativeBoundary(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 128)
	svc := newOpenAIRequestCompressionTestService(true, 64)
	oauth := &Account{Type: AccountTypeOAuth}

	require.True(t, svc.shouldCompressOpenAIRequestBody(oauth, chatgptCodexURL, body, true))
	require.False(t, svc.shouldCompressOpenAIRequestBody(oauth, chatgptCodexURL, body[:32], true), "small HTTP bodies should stay uncompressed")
	require.False(t, svc.shouldCompressOpenAIRequestBody(oauth, chatgptCodexURL, body, false), "non-streaming requests are outside the Codex client contract")
	require.False(t, svc.shouldCompressOpenAIRequestBody(oauth, chatgptCodexURL+"/compact", body, true), "compact has a separate upstream contract")
	require.False(t, svc.shouldCompressOpenAIRequestBody(&Account{Type: AccountTypeAPIKey, Platform: PlatformOpenAI}, chatgptCodexURL, body, true), "API-key providers are not verified to accept zstd")
	require.False(t, newOpenAIRequestCompressionTestService(false, 64).shouldCompressOpenAIRequestBody(oauth, chatgptCodexURL, body, true), "the rollout switch must remain fail-closed")
}

func TestBuildUpstreamRequestUsesCompressedWireBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.6","stream":true,"input":"` + strings.Repeat("large context ", 200) + `"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	svc := newOpenAIRequestCompressionTestService(true, 64)

	req, err := svc.buildUpstreamRequest(c.Request.Context(), c, &Account{ID: 1, Type: AccountTypeOAuth}, body, "token", true, "", true)
	require.NoError(t, err)
	require.Equal(t, "zstd", req.Header.Get("Content-Encoding"))
	require.Less(t, req.ContentLength, int64(len(body)))
	require.Equal(t, body, decodeZstdRequestBody(t, req))
}

func TestBuildUpstreamRequestOpenAIPassthroughUsesCompressedWireBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.6","stream":true,"input":"` + strings.Repeat("large context ", 200) + `"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	svc := newOpenAIRequestCompressionTestService(true, 64)

	req, err := svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, &Account{ID: 1, Type: AccountTypeOAuth}, body, "token")
	require.NoError(t, err)
	require.Equal(t, "zstd", req.Header.Get("Content-Encoding"))
	require.Less(t, req.ContentLength, int64(len(body)))
	require.Equal(t, body, decodeZstdRequestBody(t, req))
}

func TestBuildUpstreamRequestSkipsCompressionForCompactAndAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.6","stream":true,"input":"` + strings.Repeat("large context ", 200) + `"}`)
	svc := newOpenAIRequestCompressionTestService(true, 64)
	svc.cfg.Security.URLAllowlist.Enabled = false

	compactRecorder := httptest.NewRecorder()
	compactContext, _ := gin.CreateTestContext(compactRecorder)
	compactContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	compactReq, err := svc.buildUpstreamRequest(compactContext.Request.Context(), compactContext, &Account{ID: 1, Type: AccountTypeOAuth}, body, "token", true, "", true)
	require.NoError(t, err)
	require.Empty(t, compactReq.Header.Get("Content-Encoding"))
	require.Equal(t, int64(len(body)), compactReq.ContentLength)

	apiKeyRecorder := httptest.NewRecorder()
	apiKeyContext, _ := gin.CreateTestContext(apiKeyRecorder)
	apiKeyContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	apiKeyAccount := &Account{
		ID:          2,
		Type:        AccountTypeAPIKey,
		Platform:    PlatformOpenAI,
		Credentials: map[string]any{"base_url": "https://example.com/v1"},
	}
	apiKeyReq, err := svc.buildUpstreamRequest(apiKeyContext.Request.Context(), apiKeyContext, apiKeyAccount, body, "token", true, "", true)
	require.NoError(t, err)
	require.Empty(t, apiKeyReq.Header.Get("Content-Encoding"))
	require.Equal(t, int64(len(body)), apiKeyReq.ContentLength)
}
