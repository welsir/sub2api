package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type providerPricingReaderStub struct {
	response *service.ProviderPricingResponse
	err      error
}

func (s *providerPricingReaderStub) Get(context.Context) (*service.ProviderPricingResponse, error) {
	return s.response, s.err
}

func signProviderPricingTestRequest(secret, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestProviderPricingHandlerRequiresValidHMAC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(1_750_000_000, 0)
	secret := "0123456789abcdef0123456789abcdef"
	reader := &providerPricingReaderStub{response: &service.ProviderPricingResponse{SchemaVersion: "1.1", Success: true}}
	h := NewProviderPricingHandler(reader, config.ProviderPricingConfig{
		Enabled:        true,
		HMACSecret:     secret,
		MaxSkewSeconds: 60,
	})
	h.now = func() time.Time { return now }

	tests := []struct {
		name      string
		timestamp string
		signature string
		wantCode  int
	}{
		{name: "missing headers", wantCode: http.StatusUnauthorized},
		{name: "malformed timestamp", timestamp: "bad", signature: "00", wantCode: http.StatusUnauthorized},
		{name: "stale timestamp", timestamp: strconv.FormatInt(now.Add(-61*time.Second).Unix(), 10), signature: signProviderPricingTestRequest(secret, strconv.FormatInt(now.Add(-61*time.Second).Unix(), 10)), wantCode: http.StatusUnauthorized},
		{name: "wrong signature", timestamp: strconv.FormatInt(now.Unix(), 10), signature: hex.EncodeToString(make([]byte, sha256.Size)), wantCode: http.StatusUnauthorized},
		{name: "valid signature", timestamp: strconv.FormatInt(now.Unix(), 10), signature: signProviderPricingTestRequest(secret, strconv.FormatInt(now.Unix(), 10)), wantCode: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/api/provider/pricing", h.Get)
			req := httptest.NewRequest(http.MethodGet, "/api/provider/pricing", nil)
			if tt.timestamp != "" {
				req.Header.Set("X-Hvoy-Ts", tt.timestamp)
			}
			if tt.signature != "" {
				req.Header.Set("X-Hvoy-Sign", tt.signature)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tt.wantCode, w.Code)
			if tt.wantCode == http.StatusOK {
				require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
				require.JSONEq(t, `{"schema_version":"1.1","success":true,"message":"","data":{"currency":"","price_unit":"","updated_at":"","models":null}}`, w.Body.String())
			}
		})
	}
}

func TestProviderPricingHandlerDisabledReturnsNotFound(t *testing.T) {
	h := NewProviderPricingHandler(&providerPricingReaderStub{}, config.ProviderPricingConfig{})
	r := gin.New()
	r.GET("/api/provider/pricing", h.Get)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/provider/pricing", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
}
