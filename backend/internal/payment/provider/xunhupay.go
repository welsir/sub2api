package provider

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

const (
	xunhuPayDefaultAPIBase  = "https://api.xunhupay.com"
	xunhuPayHTTPTimeout     = 10 * time.Second
	xunhuPayMaxResponseSize = 1 << 20
)

type xunhuPayResponse map[string]json.RawMessage

func (r xunhuPayResponse) String(key string) string {
	raw, ok := r[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return strings.TrimSpace(string(raw))
}

// XunhuPay implements payment.Provider for XunhuPay's native payment protocol.
type XunhuPay struct {
	instanceID string
	config     map[string]string
	httpClient *http.Client
}

// NewXunhuPay creates an XunhuPay provider instance.
func NewXunhuPay(instanceID string, config map[string]string) (*XunhuPay, error) {
	for _, key := range []string{"appId", "appSecret", "notifyUrl"} {
		if strings.TrimSpace(config[key]) == "" {
			return nil, fmt.Errorf("xunhupay config missing required key: %s", key)
		}
	}
	cfg := make(map[string]string, len(config)+1)
	for key, value := range config {
		cfg[key] = strings.TrimSpace(value)
	}
	if cfg["apiBase"] == "" {
		cfg["apiBase"] = xunhuPayDefaultAPIBase
	}
	return &XunhuPay{
		instanceID: instanceID,
		config:     cfg,
		httpClient: &http.Client{Timeout: xunhuPayHTTPTimeout},
	}, nil
}

func (x *XunhuPay) Name() string        { return "XunhuPay" }
func (x *XunhuPay) ProviderKey() string { return payment.TypeXunhuPay }
func (x *XunhuPay) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeWxpay}
}

func (x *XunhuPay) MerchantIdentityMetadata() map[string]string {
	if x == nil || strings.TrimSpace(x.config["appId"]) == "" {
		return nil
	}
	return map[string]string{"appId": strings.TrimSpace(x.config["appId"])}
}

func xunhuPaySign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key == "hash" || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + secret))
	return hex.EncodeToString(sum[:])
}

func xunhuPayVerifySign(params map[string]string, secret, signature string) bool {
	expected := xunhuPaySign(params, secret)
	if len(expected) != len(signature) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(signature))) == 1
}

func xunhuPayNonce() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func (x *XunhuPay) postSigned(ctx context.Context, endpoint string, params map[string]string) (xunhuPayResponse, error) {
	requestParams := make(map[string]string, len(params)+4)
	for key, value := range params {
		requestParams[key] = value
	}
	nonce, err := xunhuPayNonce()
	if err != nil {
		return nil, err
	}
	requestParams["appid"] = x.config["appId"]
	requestParams["time"] = strconv.FormatInt(time.Now().Unix(), 10)
	requestParams["nonce_str"] = nonce
	requestParams["hash"] = xunhuPaySign(requestParams, x.config["appSecret"])

	form := url.Values{}
	for key, value := range requestParams {
		form.Set(key, value)
	}
	requestURL := strings.TrimRight(x.config["apiBase"], "/") + "/" + strings.TrimLeft(endpoint, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := x.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xunhupay request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, xunhuPayMaxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(body) > xunhuPayMaxResponseSize {
		return nil, fmt.Errorf("xunhupay response too large")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("xunhupay status %d", resp.StatusCode)
	}

	var result xunhuPayResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	signature := result.String("hash")
	if signature == "" {
		return nil, fmt.Errorf("xunhupay response missing hash")
	}
	if !xunhuPayVerifySign(xunhuPayResponseSigningParams(result), x.config["appSecret"], signature) {
		return nil, fmt.Errorf("xunhupay invalid response hash")
	}
	return result, nil
}

func xunhuPayResponseSigningParams(response xunhuPayResponse) map[string]string {
	params := make(map[string]string, len(response))
	for key, raw := range response {
		if key == "hash" {
			continue
		}
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" || trimmed == "null" {
			continue
		}
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			params[key] = "Array"
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			params[key] = value
			continue
		}
		params[key] = trimmed
	}
	return params
}

func (x *XunhuPay) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	return nil, fmt.Errorf("xunhupay create payment not implemented")
}

func (x *XunhuPay) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	return nil, fmt.Errorf("xunhupay query order not implemented")
}

func (x *XunhuPay) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	return nil, fmt.Errorf("xunhupay notification verification not implemented")
}

func (x *XunhuPay) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, fmt.Errorf("xunhupay refunds are not supported")
}
