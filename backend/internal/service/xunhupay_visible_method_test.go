package service

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestXunhuPayVisibleMethodSourceMapsToProvider(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		VisibleMethodSourceXunhuPayWechat,
		NormalizeVisibleMethodSource(payment.TypeWxpay, "xunhupay"),
	)
	providerKey, ok := VisibleMethodProviderKeyForSource(payment.TypeWxpay, VisibleMethodSourceXunhuPayWechat)
	require.True(t, ok)
	require.Equal(t, payment.TypeXunhuPay, providerKey)

	_, ok = VisibleMethodProviderKeyForSource(payment.TypeAlipay, VisibleMethodSourceXunhuPayWechat)
	require.False(t, ok)
}

func TestXunhuPayProviderExposesOnlyVisibleWeChatMethod(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		[]string{payment.TypeWxpay},
		enabledVisibleMethodsForProvider(payment.TypeXunhuPay, payment.TypeWxpay),
	)
	require.Empty(t, enabledVisibleMethodsForProvider(payment.TypeXunhuPay, payment.TypeAlipay))
}

func TestBuildVisibleMethodSourceAvailabilityIncludesXunhuPay(t *testing.T) {
	t.Parallel()

	availability := buildVisibleMethodSourceAvailability([]*dbent.PaymentProviderInstance{
		{ProviderKey: payment.TypeXunhuPay, SupportedTypes: payment.TypeWxpay},
	})
	require.True(t, availability[VisibleMethodSourceXunhuPayWechat])
}

func TestResolveVisibleWeChatMethodSelectsConfiguredXunhuPayInstance(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWxpay).
		SetName("Official WeChat").
		SetConfig("{}").
		SetSupportedTypes(payment.TypeWxpay).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	xunhuPay, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeXunhuPay).
		SetName("XunhuPay WeChat").
		SetConfig("{}").
		SetSupportedTypes(payment.TypeWxpay).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	service := &PaymentConfigService{
		entClient: client,
		settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
			SettingPaymentVisibleMethodWxpaySource: VisibleMethodSourceXunhuPayWechat,
		}},
	}
	selected, err := service.resolveEnabledVisibleMethodInstance(ctx, payment.TypeWxpay)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, xunhuPay.ID, selected.ID)
}
