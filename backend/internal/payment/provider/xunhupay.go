package provider

import (
	"bytes"
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
	xunhuPayMaxQRImageSize  = 512 << 10
	xunhuPayMaxQRRedirects  = 3
)

var xunhuPayPNGSignature = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

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

func (x *XunhuPay) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	if req.PaymentType != "" && req.PaymentType != payment.TypeWxpay {
		return nil, fmt.Errorf("xunhupay unsupported payment type: %s", req.PaymentType)
	}
	notifyURL := strings.TrimSpace(req.NotifyURL)
	if notifyURL == "" {
		notifyURL = x.config["notifyUrl"]
	}
	returnURL := strings.TrimSpace(req.ReturnURL)
	if returnURL == "" {
		returnURL = x.config["returnUrl"]
	}
	result, err := x.postSigned(ctx, "/payment/do.html", map[string]string{
		"version":        "1.1",
		"trade_order_id": req.OrderID,
		"total_fee":      req.Amount,
		"title":          req.Subject,
		"notify_url":     notifyURL,
		"return_url":     returnURL,
		"plugins":        "sub2api",
	})
	if err != nil {
		return nil, fmt.Errorf("xunhupay create: %w", err)
	}
	if err := xunhuPayResponseError(result); err != nil {
		return nil, err
	}
	qrCode := result.String("url_qrcode")
	payURL := result.String("url")
	if qrCode == "" && payURL == "" {
		return nil, fmt.Errorf("xunhupay create response missing payment URL")
	}
	return &payment.CreatePaymentResponse{
		TradeNo:        req.OrderID,
		QRCodeImageURL: qrCode,
		PayURL:         payURL,
	}, nil
}

func (x *XunhuPay) FetchQRCodeImage(ctx context.Context, targetURL string) ([]byte, string, error) {
	target, err := x.trustedQRCodeImageURL(targetURL)
	if err != nil {
		return nil, "", err
	}
	client := *x.httpClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > xunhuPayMaxQRRedirects {
			return fmt.Errorf("xunhupay qr image redirect limit exceeded")
		}
		_, err := x.trustedQRCodeImageURL(req.URL.String())
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("xunhupay qr image request: %w", err)
	}
	req.Header.Set("Accept", "image/png")
	req.Header.Set("User-Agent", "Sub2API-Payment-QR/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("xunhupay qr image fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("xunhupay qr image status %d", resp.StatusCode)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if contentType != "image/png" {
		return nil, "", fmt.Errorf("xunhupay qr image invalid content type")
	}
	if resp.ContentLength > xunhuPayMaxQRImageSize {
		return nil, "", fmt.Errorf("xunhupay qr image too large")
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, xunhuPayMaxQRImageSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("xunhupay qr image read: %w", err)
	}
	if len(content) > xunhuPayMaxQRImageSize || !bytes.HasPrefix(content, xunhuPayPNGSignature) {
		return nil, "", fmt.Errorf("xunhupay qr image invalid content")
	}
	return content, "image/png", nil
}

func (x *XunhuPay) trustedQRCodeImageURL(value string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(x.config["apiBase"]))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("xunhupay qr image base URL is invalid")
	}
	target, err := url.Parse(strings.TrimSpace(value))
	if err != nil || target.Scheme == "" || target.Host == "" || target.User != nil ||
		!strings.EqualFold(target.Scheme, base.Scheme) || !strings.EqualFold(target.Host, base.Host) {
		return nil, fmt.Errorf("xunhupay qr image URL is untrusted")
	}
	return target, nil
}

func (x *XunhuPay) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	result, err := x.postSigned(ctx, "/payment/query.html", map[string]string{
		"out_trade_order": tradeNo,
	})
	if err != nil {
		return nil, fmt.Errorf("xunhupay query: %w", err)
	}
	if err := xunhuPayResponseError(result); err != nil {
		return nil, err
	}
	var data struct {
		Status        string `json:"status"`
		TransactionID string `json:"transaction_id"`
		TotalFee      string `json:"total_fee"`
		PaidAt        string `json:"pay_time"`
	}
	if raw := result["data"]; len(raw) == 0 {
		return nil, fmt.Errorf("xunhupay query response missing data")
	} else if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("xunhupay decode query data: %w", err)
	}
	status := payment.ProviderStatusPending
	switch strings.ToUpper(strings.TrimSpace(data.Status)) {
	case "OD":
		status = payment.ProviderStatusPaid
	case "CD", "UD":
		status = payment.ProviderStatusFailed
	case "RD":
		status = payment.ProviderStatusPending
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(data.TotalFee), 64)
	if err != nil && strings.TrimSpace(data.TotalFee) != "" {
		return nil, fmt.Errorf("xunhupay invalid query amount: %w", err)
	}
	responseTradeNo := strings.TrimSpace(data.TransactionID)
	if responseTradeNo == "" {
		responseTradeNo = tradeNo
	}
	return &payment.QueryOrderResponse{
		TradeNo:  responseTradeNo,
		Status:   status,
		Amount:   amount,
		PaidAt:   strings.TrimSpace(data.PaidAt),
		Metadata: x.MerchantIdentityMetadata(),
	}, nil
}

func (x *XunhuPay) VerifyNotification(_ context.Context, rawBody string, _ map[string]string) (*payment.PaymentNotification, error) {
	values, err := url.ParseQuery(rawBody)
	if err != nil {
		return nil, fmt.Errorf("xunhupay parse notification: %w", err)
	}
	params := make(map[string]string, len(values))
	for key := range values {
		params[key] = values.Get(key)
	}
	signature := strings.TrimSpace(params["hash"])
	if signature == "" {
		return nil, fmt.Errorf("xunhupay notification missing hash")
	}
	if !xunhuPayVerifySign(params, x.config["appSecret"], signature) {
		return nil, fmt.Errorf("xunhupay invalid notification hash")
	}
	if strings.TrimSpace(params["appid"]) != x.config["appId"] {
		return nil, fmt.Errorf("xunhupay notification appid mismatch")
	}
	if plugin := strings.TrimSpace(params["plugins"]); plugin != "" && plugin != "sub2api" {
		return nil, fmt.Errorf("xunhupay notification plugin mismatch")
	}
	orderID := strings.TrimSpace(params["trade_order_id"])
	if orderID == "" {
		return nil, fmt.Errorf("xunhupay notification missing trade_order_id")
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(params["total_fee"]), 64)
	if err != nil {
		return nil, fmt.Errorf("xunhupay invalid notification amount: %w", err)
	}
	tradeNo := strings.TrimSpace(params["transaction_id"])
	if tradeNo == "" {
		tradeNo = strings.TrimSpace(params["open_order_id"])
	}
	status := payment.ProviderStatusFailed
	if strings.EqualFold(strings.TrimSpace(params["status"]), "OD") {
		status = payment.NotificationStatusSuccess
	}
	return &payment.PaymentNotification{
		TradeNo:  tradeNo,
		OrderID:  orderID,
		Amount:   amount,
		Status:   status,
		RawData:  rawBody,
		Metadata: x.MerchantIdentityMetadata(),
	}, nil
}

func (x *XunhuPay) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, fmt.Errorf("xunhupay refunds are not supported")
}

func xunhuPayResponseError(result xunhuPayResponse) error {
	errCode, err := strconv.Atoi(result.String("errcode"))
	if err != nil {
		return fmt.Errorf("xunhupay invalid errcode: %w", err)
	}
	if errCode != 0 {
		message := strings.TrimSpace(result.String("errmsg"))
		if message == "" {
			message = "unknown upstream error"
		}
		return fmt.Errorf("xunhupay error %d: %s", errCode, message)
	}
	return nil
}
