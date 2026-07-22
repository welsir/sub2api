package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

const (
	xunhuPayDefaultAPIBase = "https://api.xunhupay.com"
	xunhuPayHTTPTimeout    = 10 * time.Second
)

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
