package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestNormalizeProviderRefundFlagsDisablesXunhuPayRefunds(t *testing.T) {
	t.Parallel()

	refundEnabled, allowUserRefund := normalizeProviderRefundFlags(payment.TypeXunhuPay, true, true)
	require.False(t, refundEnabled)
	require.False(t, allowUserRefund)
}

func TestNormalizeProviderRefundFlagsPreservesOtherProviders(t *testing.T) {
	t.Parallel()

	refundEnabled, allowUserRefund := normalizeProviderRefundFlags(payment.TypeEasyPay, true, true)
	require.True(t, refundEnabled)
	require.True(t, allowUserRefund)
}
