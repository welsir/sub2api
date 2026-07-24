package service

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const providerPricingScale = 1_000_000

type ProviderPricingGroupRepository interface {
	ListProviderPricingGroups(ctx context.Context) ([]Group, error)
}

type ProviderPricingCalculator interface {
	CalculateCost(model string, tokens UsageTokens, rateMultiplier float64) (*CostBreakdown, error)
}

type ProviderPricingResponse struct {
	SchemaVersion string              `json:"schema_version"`
	Success       bool                `json:"success"`
	Message       string              `json:"message"`
	Data          ProviderPricingData `json:"data"`
}

type ProviderPricingData struct {
	Currency   string                 `json:"currency"`
	PriceUnit  string                 `json:"price_unit"`
	SiteName   string                 `json:"site_name,omitempty"`
	SiteDomain string                 `json:"site_domain,omitempty"`
	UpdatedAt  string                 `json:"updated_at"`
	Models     []ProviderPricingModel `json:"models"`
}

type ProviderPricingModel struct {
	ModelName          string   `json:"model_name"`
	GroupName          string   `json:"group_name"`
	InputPrice         float64  `json:"input_price"`
	OutputPrice        *float64 `json:"output_price"`
	CacheInputPrice    *float64 `json:"cache_input_price"`
	CacheCreatePrice   *float64 `json:"cache_create_price"`
	CacheCreatePrice1H *float64 `json:"cache_create_price_1h"`
	Enabled            bool     `json:"enabled"`
	Note               string   `json:"note,omitempty"`
}

type ProviderPricingService struct {
	groupRepo  ProviderPricingGroupRepository
	calculator ProviderPricingCalculator
	siteName   string
	siteDomain string
	now        func() time.Time
}

func NewProviderPricingService(
	groupRepo ProviderPricingGroupRepository,
	calculator ProviderPricingCalculator,
	siteName string,
	siteDomain string,
) *ProviderPricingService {
	return &ProviderPricingService{
		groupRepo:  groupRepo,
		calculator: calculator,
		siteName:   siteName,
		siteDomain: siteDomain,
		now:        time.Now,
	}
}

func (s *ProviderPricingService) Get(ctx context.Context) (*ProviderPricingResponse, error) {
	groups, err := s.groupRepo.ListProviderPricingGroups(ctx)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].ProviderPricingGroupName == groups[j].ProviderPricingGroupName {
			return groups[i].ID < groups[j].ID
		}
		return groups[i].ProviderPricingGroupName < groups[j].ProviderPricingGroupName
	})

	rows := make([]ProviderPricingModel, 0)
	for i := range groups {
		g := &groups[i]
		for _, model := range NormalizeProviderPricingModels(g.ProviderPricingModels) {
			row, ok := s.projectModel(g, model)
			if !ok {
				logger.LegacyPrintf("service.provider_pricing", "skipping model without pricing: group_id=%d model=%s", g.ID, model)
				continue
			}
			rows = append(rows, row)
		}
	}

	return &ProviderPricingResponse{
		SchemaVersion: "1.1",
		Success:       true,
		Message:       "",
		Data: ProviderPricingData{
			Currency:   "CNY",
			PriceUnit:  "per_1m_tokens",
			SiteName:   s.siteName,
			SiteDomain: s.siteDomain,
			UpdatedAt:  s.now().UTC().Format(time.RFC3339),
			Models:     rows,
		},
	}, nil
}

func (s *ProviderPricingService) projectModel(group *Group, model string) (ProviderPricingModel, bool) {
	inputPrice, err := s.calculatePerMillion(model, UsageTokens{InputTokens: 1}, group.RateMultiplier)
	if err != nil {
		return ProviderPricingModel{}, false
	}

	return ProviderPricingModel{
		ModelName:          model,
		GroupName:          group.ProviderPricingGroupName,
		InputPrice:         inputPrice,
		OutputPrice:        s.optionalPerMillion(model, UsageTokens{OutputTokens: 1}, group.RateMultiplier),
		CacheInputPrice:    s.optionalPerMillion(model, UsageTokens{CacheReadTokens: 1}, group.RateMultiplier),
		CacheCreatePrice:   s.optionalPerMillion(model, UsageTokens{CacheCreationTokens: 1, CacheCreation5mTokens: 1}, group.RateMultiplier),
		CacheCreatePrice1H: s.optionalPerMillion(model, UsageTokens{CacheCreationTokens: 1, CacheCreation1hTokens: 1}, group.RateMultiplier),
		Enabled:            group.Status == StatusActive,
		Note:               "长上下文等条件价格按站内实时计费规则执行",
	}, true
}

func (s *ProviderPricingService) optionalPerMillion(model string, tokens UsageTokens, multiplier float64) *float64 {
	price, err := s.calculatePerMillion(model, tokens, multiplier)
	if err != nil || price <= 0 {
		return nil
	}
	return &price
}

func (s *ProviderPricingService) calculatePerMillion(model string, tokens UsageTokens, multiplier float64) (float64, error) {
	breakdown, err := s.calculator.CalculateCost(model, tokens, multiplier)
	if err != nil {
		return 0, err
	}
	return roundProviderPrice(breakdown.ActualCost * providerPricingScale), nil
}

func roundProviderPrice(value float64) float64 {
	return math.Round(value*1e12) / 1e12
}
