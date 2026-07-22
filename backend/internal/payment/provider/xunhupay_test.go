package provider

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestXunhuPaySignSortsAndExcludesEmptyAndHashWithoutMutation(t *testing.T) {
	t.Parallel()

	params := map[string]string{
		"z": "last", "a": "first", "empty": "", "hash": "ignored",
	}
	got := xunhuPaySign(params, "secret")
	require.Equal(t, testMD5Hex("a=first&z=lastsecret"), got)
	require.Equal(t, "ignored", params["hash"])
	require.Contains(t, params, "empty")
	require.True(t, xunhuPayVerifySign(params, "secret", got))
	require.False(t, xunhuPayVerifySign(params, "wrong", got))
}

func TestXunhuPayPostSignedAddsProtocolFieldsAndVerifiesResponse(t *testing.T) {
	t.Parallel()

	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		received = r.PostForm
		require.Equal(t, "app-1", received.Get("appid"))
		require.NotEmpty(t, received.Get("time"))
		require.Len(t, received.Get("nonce_str"), 32)
		require.True(t, xunhuPayVerifySign(firstFormValues(received), "secret-1", received.Get("hash")))

		response := map[string]string{"errcode": "0", "errmsg": "success!", "url_qrcode": "weixin://qr/1"}
		response["hash"] = xunhuPaySign(response, "secret-1")
		_, _ = fmt.Fprintf(w, `{"errcode":0,"errmsg":"success!","url_qrcode":"weixin://qr/1","hash":%q}`, response["hash"])
	}))
	defer server.Close()

	provider := mustTestXunhuPay(t, server)
	result, err := provider.postSigned(context.Background(), "/payment/do.html", map[string]string{"trade_order_id": "order-1"})
	require.NoError(t, err)
	require.Equal(t, "weixin://qr/1", result.String("url_qrcode"))
	require.Equal(t, "order-1", received.Get("trade_order_id"))
}

func TestXunhuPayPostSignedRejectsUnsafeResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    string
	}{
		{name: "non 2xx", statusCode: http.StatusBadGateway, body: `bad gateway`, wantErr: "status 502"},
		{name: "invalid json", statusCode: http.StatusOK, body: `{`, wantErr: "decode response"},
		{name: "missing hash", statusCode: http.StatusOK, body: `{"errcode":0,"errmsg":"success!"}`, wantErr: "missing hash"},
		{name: "invalid hash", statusCode: http.StatusOK, body: `{"errcode":0,"errmsg":"success!","hash":"00000000000000000000000000000000"}`, wantErr: "invalid response hash"},
		{name: "oversized", statusCode: http.StatusOK, body: strings.Repeat("x", xunhuPayMaxResponseSize+1), wantErr: "response too large"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			provider := mustTestXunhuPay(t, server)
			_, err := provider.postSigned(context.Background(), "/payment/do.html", map[string]string{"trade_order_id": "order-1"})
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestXunhuPayPostSignedHonorsCancelledContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	provider := mustTestXunhuPay(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := provider.postSigned(ctx, "/payment/do.html", map[string]string{"trade_order_id": "order-1"})
	require.ErrorIs(t, err, context.Canceled)
}

func testMD5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func firstFormValues(values url.Values) map[string]string {
	result := make(map[string]string, len(values))
	for key := range values {
		result[key] = values.Get(key)
	}
	return result
}

func mustTestXunhuPay(t *testing.T, server *httptest.Server) *XunhuPay {
	t.Helper()
	provider, err := NewXunhuPay("instance-1", map[string]string{
		"appId":     "app-1",
		"appSecret": "secret-1",
		"apiBase":   server.URL,
		"notifyUrl": "https://merchant.example.com/api/v1/payment/webhook/xunhupay",
	})
	require.NoError(t, err)
	provider.httpClient = server.Client()
	return provider
}

func TestNewXunhuPayRequiresCredentials(t *testing.T) {
	t.Parallel()

	_, err := NewXunhuPay("instance-1", map[string]string{})
	require.ErrorContains(t, err, "appId")

	_, err = NewXunhuPay("instance-1", map[string]string{"appId": "app-1"})
	require.ErrorContains(t, err, "appSecret")
}

func TestNewXunhuPayDeclaresOnlyWeChat(t *testing.T) {
	t.Parallel()

	provider, err := NewXunhuPay("instance-1", map[string]string{
		"appId":     "app-1",
		"appSecret": "secret-1",
		"notifyUrl": "https://merchant.example.com/api/v1/payment/webhook/xunhupay",
	})
	require.NoError(t, err)
	require.Equal(t, payment.TypeXunhuPay, provider.ProviderKey())
	require.Equal(t, []payment.PaymentType{payment.TypeWxpay}, provider.SupportedTypes())
}

func TestFactoryCreatesXunhuPay(t *testing.T) {
	t.Parallel()

	provider, err := CreateProvider(payment.TypeXunhuPay, "instance-1", map[string]string{
		"appId":     "app-1",
		"appSecret": "secret-1",
		"notifyUrl": "https://merchant.example.com/api/v1/payment/webhook/xunhupay",
	})
	require.NoError(t, err)
	require.Equal(t, payment.TypeXunhuPay, provider.ProviderKey())
}
