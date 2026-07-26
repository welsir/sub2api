package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type providerPricingGroupRepoStub struct {
	groups []Group
	err    error
}

func TestProviderPricingServiceUsesBillingSourceOfTruthAtGroupMultiplier(t *testing.T) {
	repo := &providerPricingGroupRepoStub{groups: []Group{{
		Status:                   StatusActive,
		RateMultiplier:           0.2,
		ProviderPricingEnabled:   true,
		ProviderPricingGroupName: "gpt01",
		ProviderPricingModels:    []string{"gpt-5.6"},
	}}}

	result, err := NewProviderPricingService(
		repo,
		NewBillingService(&config.Config{}, nil),
		"Omni",
		"omni.welsir.com",
		nil,
	).Get(context.Background())
	require.NoError(t, err)
	require.Len(t, result.Data.Models, 1)
	row := result.Data.Models[0]
	require.Equal(t, 1.0, row.InputPrice)
	require.Equal(t, 6.0, *row.OutputPrice)
	require.Equal(t, 0.12, *row.CacheInputPrice)
	require.Equal(t, 1.25, *row.CacheCreatePrice)
}

func (s *providerPricingGroupRepoStub) ListProviderPricingGroups(context.Context) ([]Group, error) {
	return s.groups, s.err
}

type providerPricingCalculatorStub struct {
	prices map[string]map[UsageTokens]float64
	errors map[string]error
}

func (s *providerPricingCalculatorStub) CalculateCost(model string, tokens UsageTokens, multiplier float64) (*CostBreakdown, error) {
	if err := s.errors[model]; err != nil {
		return nil, err
	}
	base := s.prices[model][tokens]
	return &CostBreakdown{ActualCost: base * multiplier}, nil
}

func TestProviderPricingServiceBuildsStableFinalPriceRows(t *testing.T) {
	repo := &providerPricingGroupRepoStub{groups: []Group{
		{
			ID:                       20,
			Name:                     "内部名称可改",
			Status:                   "inactive",
			RateMultiplier:           1.5,
			ProviderPricingEnabled:   true,
			ProviderPricingGroupName: "gpt02",
			ProviderPricingModels:    []string{"gpt-5.4"},
		},
		{
			ID:                       10,
			Name:                     "pro号池[自营官方渠道]",
			Status:                   StatusActive,
			RateMultiplier:           0.2,
			ProviderPricingEnabled:   true,
			ProviderPricingGroupName: "gpt01",
			ProviderPricingModels:    []string{"gpt-5.6"},
		},
	}}
	calc := &providerPricingCalculatorStub{prices: map[string]map[UsageTokens]float64{
		"gpt-5.6": {
			{InputTokens: 1}:     0.000001,
			{OutputTokens: 1}:    0.000004,
			{CacheReadTokens: 1}: 0.0000001,
			{CacheCreationTokens: 1, CacheCreation5mTokens: 1}: 0.00000125,
			{CacheCreationTokens: 1, CacheCreation1hTokens: 1}: 0.000002,
		},
		"gpt-5.4": {
			{InputTokens: 1}:  0.000002,
			{OutputTokens: 1}: 0.000008,
		},
	}}

	svc := NewProviderPricingService(repo, calc, "omni", "omni.welsir.com", nil)
	result, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, "1.1", result.SchemaVersion)
	require.True(t, result.Success)
	require.Equal(t, "CNY", result.Data.Currency)
	require.Equal(t, "per_1m_tokens", result.Data.PriceUnit)
	require.Len(t, result.Data.Models, 2)

	first := result.Data.Models[0]
	require.Equal(t, "gpt01", first.GroupName)
	require.Equal(t, "gpt-5.6", first.ModelName)
	require.Equal(t, 0.2, first.InputPrice)
	require.Equal(t, 0.8, *first.OutputPrice)
	require.Equal(t, 0.02, *first.CacheInputPrice)
	require.Equal(t, 0.25, *first.CacheCreatePrice)
	require.Equal(t, 0.4, *first.CacheCreatePrice1H)
	require.True(t, first.Enabled)

	second := result.Data.Models[1]
	require.Equal(t, "gpt02", second.GroupName)
	require.Equal(t, 3.0, second.InputPrice)
	require.False(t, second.Enabled)
}

func TestProviderPricingServiceMapsInternalGroupNameForExternalMatching(t *testing.T) {
	repo := &providerPricingGroupRepoStub{groups: []Group{{
		ID:                       10,
		Status:                   StatusActive,
		RateMultiplier:           0.2,
		ProviderPricingEnabled:   true,
		ProviderPricingGroupName: "gpt01",
		ProviderPricingModels:    []string{"gpt-5.6"},
	}}}
	calc := &providerPricingCalculatorStub{prices: map[string]map[UsageTokens]float64{
		"gpt-5.6": {{InputTokens: 1}: 0.000001},
	}}

	result, err := NewProviderPricingService(
		repo,
		calc,
		"Omni",
		"ai.welsir.com",
		map[string]string{"gpt01": "pro专属"},
	).Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, "ai.welsir.com", result.Data.SiteDomain)
	require.Len(t, result.Data.Models, 1)
	require.Equal(t, "pro专属", result.Data.Models[0].GroupName)
}

func TestProviderPricingServiceSkipsUnpricedModel(t *testing.T) {
	repo := &providerPricingGroupRepoStub{groups: []Group{{
		Status:                   StatusActive,
		RateMultiplier:           1,
		ProviderPricingEnabled:   true,
		ProviderPricingGroupName: "gpt01",
		ProviderPricingModels:    []string{"unknown", "gpt-5.6"},
	}}}
	calc := &providerPricingCalculatorStub{
		prices: map[string]map[UsageTokens]float64{"gpt-5.6": {{InputTokens: 1}: 0.000001}},
		errors: map[string]error{"unknown": errors.New("not priced")},
	}

	result, err := NewProviderPricingService(repo, calc, "omni", "omni.welsir.com", nil).Get(context.Background())
	require.NoError(t, err)
	require.Len(t, result.Data.Models, 1)
	require.Equal(t, "gpt-5.6", result.Data.Models[0].ModelName)
}
