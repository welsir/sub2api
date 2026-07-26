package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newCacheFloorTestBillingService() *BillingService {
	return NewBillingService(&config.Config{}, nil)
}

func TestBillingCacheFloorAppliesAtOmniStandardRate(t *testing.T) {
	svc := newCacheFloorTestBillingService()

	tokens := UsageTokens{CacheReadTokens: 100_000}
	cost, err := svc.CalculateCost("gpt-5.6-terra", tokens, 0.2)
	require.NoError(t, err)

	// Terra 缓存基础价为 0.25/MTok；0.25*0.2=0.05，低于当前规定的 0.12/MTok。
	require.InDelta(t, 0.025, cost.CacheReadCost, 1e-12)
	require.InDelta(t, 0.025, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.012, cost.ActualCost, 1e-12)
}

func TestBillingCacheFloorLeavesOtherTokenChargesAtStandardRate(t *testing.T) {
	svc := newCacheFloorTestBillingService()

	tokens := UsageTokens{
		InputTokens:     100_000,
		OutputTokens:    10_000,
		CacheReadTokens: 100_000,
	}
	cost, err := svc.CalculateCost("gpt-5.6-terra", tokens, 0.2)
	require.NoError(t, err)

	expectedNonCacheActual := (cost.InputCost + cost.OutputCost) * 0.2
	expectedCacheActual := float64(tokens.CacheReadTokens) * omniCacheReadFloorPerToken
	require.InDelta(t, expectedNonCacheActual+expectedCacheActual, cost.ActualCost, 1e-12)
}

func TestBillingCacheFloorDoesNotChangeOtherGroupRate(t *testing.T) {
	svc := newCacheFloorTestBillingService()

	cost, err := svc.CalculateCost("gpt-5.6-terra", UsageTokens{CacheReadTokens: 100_000}, 1.5)
	require.NoError(t, err)

	require.InDelta(t, cost.TotalCost*1.5, cost.ActualCost, 1e-12)
}

func TestBillingCacheFloorUsesLongContextAdjustedPrice(t *testing.T) {
	svc := newCacheFloorTestBillingService()

	tokens := UsageTokens{CacheReadTokens: 300_000}
	cost, err := svc.CalculateCost("gpt-5.6-sol", tokens, 0.2)
	require.NoError(t, err)

	// Sol 缓存基础价 0.5/MTok，超过 272k 后先乘长上下文 2x；
	// 最终 1.0*0.2=0.2/MTok，高于 0.12/MTok，不应再抬价。
	require.InDelta(t, 0.3, cost.CacheReadCost, 1e-12)
	require.InDelta(t, 0.06, cost.ActualCost, 1e-12)
}
