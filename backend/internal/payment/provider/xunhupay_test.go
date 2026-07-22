package provider

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

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
