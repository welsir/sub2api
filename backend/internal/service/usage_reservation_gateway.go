package service

import (
	"context"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	DefaultReservationInputTokenCap  = 128000
	DefaultReservationOutputTokenCap = 8192
)

type PreparedUsageReservation struct {
	Body        []byte
	Quote       *UsageReservationQuote
	Reservation *UsageReservation
}

func (s *GatewayService) PrepareUsageReservation(ctx context.Context, apiKey *APIKey, subscription *UserSubscription, billingModel string, body []byte, endpoint string) (*PreparedUsageReservation, error) {
	if s == nil {
		return &PreparedUsageReservation{Body: body}, nil
	}
	return prepareUsageReservation(ctx, usageReservationDeps{
		billingRepo:       s.usageBillingRepo,
		billingService:    s.billingService,
		resolver:          s.resolver,
		billingCache:      s.billingCacheService,
		resolveMultiplier: func() float64 { return s.resolveReservationMultiplier(ctx, apiKey) },
	}, apiKey, subscription, billingModel, body, endpoint, "")
}

func (s *OpenAIGatewayService) PrepareUsageReservation(ctx context.Context, apiKey *APIKey, subscription *UserSubscription, billingModel string, body []byte, endpoint string) (*PreparedUsageReservation, error) {
	if s == nil {
		return &PreparedUsageReservation{Body: body}, nil
	}
	serviceTier := strings.TrimSpace(gjsonString(body, "service_tier"))
	return prepareUsageReservation(ctx, usageReservationDeps{
		billingRepo:       s.usageBillingRepo,
		billingService:    s.billingService,
		resolver:          s.resolver,
		billingCache:      s.billingCacheService,
		resolveMultiplier: func() float64 { return s.resolveReservationMultiplier(ctx, apiKey) },
	}, apiKey, subscription, billingModel, body, endpoint, serviceTier)
}

type usageReservationDeps struct {
	billingRepo       UsageBillingRepository
	billingService    *BillingService
	resolver          *ModelPricingResolver
	billingCache      *BillingCacheService
	resolveMultiplier func() float64
}

func prepareUsageReservation(ctx context.Context, deps usageReservationDeps, apiKey *APIKey, subscription *UserSubscription, billingModel string, body []byte, endpoint string, serviceTier string) (*PreparedUsageReservation, error) {
	repo, ok := deps.billingRepo.(UsageReservationRepository)
	if !ok || repo == nil || deps.billingService == nil || apiKey == nil || apiKey.User == nil {
		return &PreparedUsageReservation{Body: body}, nil
	}

	model := strings.TrimSpace(billingModel)
	if model == "" {
		model = strings.TrimSpace(gjsonString(body, "model"))
	}

	rate := 1.0
	if deps.resolveMultiplier != nil {
		rate = deps.resolveMultiplier()
	}
	var groupID *int64
	if apiKey.GroupID != nil {
		groupID = cloneInt64(apiKey.GroupID)
	}
	quote, err := BuildUsageReservationQuote(ctx, UsageReservationQuoteInput{
		BillingService:        deps.billingService,
		Resolver:              deps.resolver,
		GroupID:               groupID,
		Model:                 model,
		Body:                  body,
		Endpoint:              endpoint,
		InputTokenUpperBound:  DefaultReservationInputTokenCap,
		OutputTokenUpperBound: DefaultReservationOutputTokenCap,
		RateMultiplier:        rate,
		ServiceTier:           serviceTier,
	})
	if err != nil {
		return nil, err
	}

	var subscriptionID *int64
	if subscription != nil && apiKey.Group != nil && apiKey.Group.IsSubscriptionType() {
		subscriptionID = &subscription.ID
	}
	reservation, err := repo.ReserveUsage(ctx, &UsageReservationRequest{
		UserID:         apiKey.User.ID,
		GroupID:        groupID,
		SubscriptionID: subscriptionID,
		AmountUSD:      quote.RequiredUSD,
	})
	if err != nil {
		return nil, err
	}
	if deps.billingCache != nil {
		_ = deps.billingCache.InvalidateUserBalance(ctx, apiKey.User.ID)
		if groupID != nil {
			_ = deps.billingCache.InvalidateSubscription(ctx, apiKey.User.ID, *groupID)
		}
	}
	return &PreparedUsageReservation{Body: quote.Body, Quote: quote, Reservation: reservation}, nil
}

func (s *GatewayService) ReleaseUsageReservation(ctx context.Context, reservation *UsageReservation) {
	releaseUsageReservation(ctx, s.usageBillingRepo, s.billingCacheService, reservation)
}

func (s *OpenAIGatewayService) ReleaseUsageReservation(ctx context.Context, reservation *UsageReservation) {
	releaseUsageReservation(ctx, s.usageBillingRepo, s.billingCacheService, reservation)
}

func releaseUsageReservation(ctx context.Context, repo UsageBillingRepository, cache *BillingCacheService, reservation *UsageReservation) {
	if reservation == nil || reservation.AmountUSD <= 0 {
		return
	}
	if reservationRepo, ok := repo.(UsageReservationRepository); ok && reservationRepo != nil {
		_ = reservationRepo.ReleaseUsageReservation(ctx, reservation)
	}
	if cache != nil {
		_ = cache.InvalidateUserBalance(ctx, reservation.UserID)
		if reservation.GroupID != nil {
			_ = cache.InvalidateSubscription(ctx, reservation.UserID, *reservation.GroupID)
		}
	}
}

func (s *GatewayService) resolveReservationMultiplier(ctx context.Context, apiKey *APIKey) float64 {
	multiplier := 1.0
	if s != nil && s.cfg != nil {
		multiplier = s.cfg.Default.RateMultiplier
	}
	if s != nil && apiKey != nil && apiKey.GroupID != nil && apiKey.Group != nil {
		multiplier = s.getUserGroupRateMultiplier(ctx, apiKey.User.ID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
	}
	return multiplier
}

func (s *OpenAIGatewayService) resolveReservationMultiplier(ctx context.Context, apiKey *APIKey) float64 {
	multiplier := 1.0
	if s != nil && s.cfg != nil {
		multiplier = s.cfg.Default.RateMultiplier
	}
	if s != nil && apiKey != nil && apiKey.GroupID != nil && apiKey.Group != nil {
		resolver := s.userGroupRateResolver
		if resolver == nil {
			resolver = newUserGroupRateResolver(nil, nil, resolveUserGroupRateCacheTTL(s.cfg), nil, "service.openai_gateway")
		}
		multiplier = resolver.Resolve(ctx, apiKey.User.ID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
	}
	return multiplier
}

func gjsonString(body []byte, path string) string {
	return strings.TrimSpace(gjson.GetBytes(body, path).String())
}

func cloneInt64(v *int64) *int64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
