package provider

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
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

func TestXunhuPayCreatePaymentMapsQRCodeAndMerchantOrderID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/payment/do.html", r.URL.Path)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "1.1", r.PostForm.Get("version"))
		require.Equal(t, "order-123", r.PostForm.Get("trade_order_id"))
		require.Equal(t, "12.34", r.PostForm.Get("total_fee"))
		require.Equal(t, "Balance recharge", r.PostForm.Get("title"))
		require.Equal(t, "https://merchant.example.com/notify", r.PostForm.Get("notify_url"))
		require.Equal(t, "https://merchant.example.com/result", r.PostForm.Get("return_url"))
		require.Equal(t, "sub2api", r.PostForm.Get("plugins"))

		writeSignedXunhuPayJSON(t, w, map[string]any{
			"errcode":    0,
			"errmsg":     "success!",
			"openid":     "xunhu-order-1",
			"url_qrcode": "weixin://wxpay/bizpayurl?pr=test",
			"url":        "https://api.xunhupay.com/pay/mobile",
		}, "secret-1")
	}))
	defer server.Close()

	provider := mustTestXunhuPay(t, server)
	response, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID:     "order-123",
		Amount:      "12.34",
		PaymentType: payment.TypeWxpay,
		Subject:     "Balance recharge",
		NotifyURL:   "https://merchant.example.com/notify",
		ReturnURL:   "https://merchant.example.com/result",
		IsMobile:    true,
	})
	require.NoError(t, err)
	require.Equal(t, "order-123", response.TradeNo)
	qrImageURL := reflect.ValueOf(response).Elem().FieldByName("QRCodeImageURL")
	require.True(t, qrImageURL.IsValid(), "create response must distinguish provider QR images from QR content")
	require.Equal(t, "weixin://wxpay/bizpayurl?pr=test", qrImageURL.String())
	require.Empty(t, response.QRCode, "provider QR image URLs must not be encoded as QR content")
	require.Equal(t, "https://api.xunhupay.com/pay/mobile", response.PayURL)
}

func TestXunhuPayCreatePaymentRejectsUpstreamError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSignedXunhuPayJSON(t, w, map[string]any{"errcode": 500, "errmsg": "invalid sign!"}, "secret-1")
	}))
	defer server.Close()

	provider := mustTestXunhuPay(t, server)
	_, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID: "order-123", Amount: "12.34", PaymentType: payment.TypeWxpay, Subject: "Recharge",
	})
	require.ErrorContains(t, err, "invalid sign")
}

func TestXunhuPayFetchQRCodeImageReturnsTrustedPNG(t *testing.T) {
	t.Parallel()

	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/qr/order-123", r.URL.Path)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer server.Close()

	provider := mustTestXunhuPay(t, server)
	type qrImageFetcher interface {
		FetchQRCodeImage(context.Context, string) ([]byte, string, error)
	}
	fetcher, ok := any(provider).(qrImageFetcher)
	require.True(t, ok, "Xunhupay must provide bounded QR image fetching")
	if !ok {
		return
	}

	content, contentType, err := fetcher.FetchQRCodeImage(context.Background(), server.URL+"/qr/order-123")
	require.NoError(t, err)
	require.Equal(t, png, content)
	require.Equal(t, "image/png", contentType)
}

func TestXunhuPayFetchQRCodeImageRejectsUntrustedHost(t *testing.T) {
	t.Parallel()

	provider, err := NewXunhuPay("instance-1", map[string]string{
		"appId": "app-1", "appSecret": "secret-1",
		"apiBase":   "https://api.xunhupay.com",
		"notifyUrl": "https://merchant.example.com/api/v1/payment/webhook/xunhupay",
	})
	require.NoError(t, err)
	_, _, err = provider.FetchQRCodeImage(context.Background(), "https://attacker.example.com/qr/order-123")
	require.ErrorContains(t, err, "untrusted")
}

func TestXunhuPayFetchQRCodeImageRejectsNonPNG(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>not a QR image</html>")
	}))
	defer server.Close()

	provider := mustTestXunhuPay(t, server)
	_, _, err := provider.FetchQRCodeImage(context.Background(), server.URL+"/qr/order-123")
	require.ErrorContains(t, err, "invalid content type")
}

func TestXunhuPayQueryOrderMapsStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		upstreamStatus string
		wantStatus     string
	}{
		{upstreamStatus: "OD", wantStatus: payment.ProviderStatusPaid},
		{upstreamStatus: "WP", wantStatus: payment.ProviderStatusPending},
		{upstreamStatus: "CD", wantStatus: payment.ProviderStatusFailed},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.upstreamStatus, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/payment/query.html", r.URL.Path)
				require.NoError(t, r.ParseForm())
				require.Equal(t, "order-123", r.PostForm.Get("out_trade_order"))
				writeSignedXunhuPayJSON(t, w, map[string]any{
					"errcode": 0,
					"errmsg":  "success!",
					"data": map[string]any{
						"status":         tt.upstreamStatus,
						"transaction_id": "wx-transaction-1",
						"total_fee":      "12.34",
					},
				}, "secret-1")
			}))
			defer server.Close()

			provider := mustTestXunhuPay(t, server)
			response, err := provider.QueryOrder(context.Background(), "order-123")
			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, response.Status)
			require.Equal(t, "wx-transaction-1", response.TradeNo)
			require.InDelta(t, 12.34, response.Amount, 0.000001)
			require.Equal(t, "app-1", response.Metadata["appId"])
		})
	}
}

func TestXunhuPayVerifyNotificationAcceptsOnlySignedPaidStatus(t *testing.T) {
	t.Parallel()

	provider, err := NewXunhuPay("instance-1", map[string]string{
		"appId": "app-1", "appSecret": "secret-1", "notifyUrl": "https://merchant.example.com/notify",
	})
	require.NoError(t, err)
	params := map[string]string{
		"trade_order_id": "order-123", "total_fee": "12.34", "transaction_id": "wx-transaction-1",
		"open_order_id": "xunhu-order-1", "status": "OD", "appid": "app-1", "time": "1784700000",
		"nonce_str": "nonce-1", "future_field": "extension-value",
	}
	notification, err := provider.VerifyNotification(context.Background(), encodeSignedXunhuPayForm(params, "secret-1"), nil)
	require.NoError(t, err)
	require.Equal(t, payment.NotificationStatusSuccess, notification.Status)
	require.Equal(t, "order-123", notification.OrderID)
	require.Equal(t, "wx-transaction-1", notification.TradeNo)
	require.InDelta(t, 12.34, notification.Amount, 0.000001)
	require.Equal(t, "app-1", notification.Metadata["appId"])

	params["status"] = "WP"
	nonPaid, err := provider.VerifyNotification(context.Background(), encodeSignedXunhuPayForm(params, "secret-1"), nil)
	require.NoError(t, err)
	require.NotEqual(t, payment.NotificationStatusSuccess, nonPaid.Status)
}

func TestXunhuPayVerifyNotificationRejectsForgeryAndWrongApp(t *testing.T) {
	t.Parallel()

	provider, err := NewXunhuPay("instance-1", map[string]string{
		"appId": "app-1", "appSecret": "secret-1", "notifyUrl": "https://merchant.example.com/notify",
	})
	require.NoError(t, err)
	base := map[string]string{
		"trade_order_id": "order-123", "total_fee": "12.34", "transaction_id": "tx-1",
		"status": "OD", "appid": "app-1", "time": "1784700000", "nonce_str": "nonce-1",
	}
	_, err = provider.VerifyNotification(context.Background(), encodeSignedXunhuPayForm(base, "wrong-secret"), nil)
	require.ErrorContains(t, err, "invalid notification hash")

	base["appid"] = "other-app"
	_, err = provider.VerifyNotification(context.Background(), encodeSignedXunhuPayForm(base, "secret-1"), nil)
	require.ErrorContains(t, err, "appid mismatch")
}

func TestXunhuPayRefundIsUnsupported(t *testing.T) {
	t.Parallel()

	provider, err := NewXunhuPay("instance-1", map[string]string{
		"appId": "app-1", "appSecret": "secret-1", "notifyUrl": "https://merchant.example.com/notify",
	})
	require.NoError(t, err)
	_, err = provider.Refund(context.Background(), payment.RefundRequest{OrderID: "order-123", Amount: "12.34"})
	require.ErrorContains(t, err, "not supported")
}

func encodeSignedXunhuPayForm(params map[string]string, secret string) string {
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	values.Set("hash", xunhuPaySign(params, secret))
	return values.Encode()
}

func writeSignedXunhuPayJSON(t *testing.T, w http.ResponseWriter, values map[string]any, secret string) {
	t.Helper()
	signing := make(map[string]string, len(values))
	for key, value := range values {
		switch value := value.(type) {
		case string:
			signing[key] = value
		case map[string]any:
			signing[key] = "Array"
		default:
			signing[key] = fmt.Sprint(value)
		}
	}
	values["hash"] = xunhuPaySign(signing, secret)
	require.NoError(t, json.NewEncoder(w).Encode(values))
}
