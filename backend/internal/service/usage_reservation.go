package service

import (
	"context"
	"errors"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	UsageReservationEndpointResponses         = "responses"
	UsageReservationEndpointChatCompletions   = "chat_completions"
	UsageReservationEndpointAnthropicMessages = "anthropic_messages"
)

var (
	ErrUsageReservationMissingBillingService = errors.New("usage reservation billing service is required")
	ErrUsageReservationInvalidRequestBody    = errors.New("usage reservation request body must be valid JSON")
)

type UsageReservationQuoteInput struct {
	BillingService        *BillingService
	Resolver              *ModelPricingResolver
	GroupID               *int64
	Model                 string
	Body                  []byte
	Endpoint              string
	InputTokenUpperBound  int
	OutputTokenUpperBound int
	RateMultiplier        float64
	ServiceTier           string
}

type UsageReservationQuote struct {
	Body           []byte
	BodyModified   bool
	InputTokenCap  int
	OutputTokenCap int
	RequiredUSD    float64
}

func BuildUsageReservationQuote(ctx context.Context, input UsageReservationQuoteInput) (*UsageReservationQuote, error) {
	if input.BillingService == nil {
		return nil, ErrUsageReservationMissingBillingService
	}
	if !gjson.ValidBytes(input.Body) {
		return nil, ErrUsageReservationInvalidRequestBody
	}

	outputCap := normalizeReservationOutputCap(input.OutputTokenUpperBound)
	body, modified, err := clampReservationOutputTokens(input.Body, input.Endpoint, outputCap)
	if err != nil {
		return nil, err
	}

	tokens := UsageTokens{
		InputTokens:  normalizeReservationInputCap(input.InputTokenUpperBound),
		OutputTokens: outputCap,
	}
	rate := input.RateMultiplier
	if rate == 0 {
		rate = 1
	}

	var cost *CostBreakdown
	if input.Resolver != nil && input.GroupID != nil {
		cost, err = input.BillingService.CalculateCostUnified(CostInput{
			Ctx:            ctx,
			Model:          input.Model,
			GroupID:        input.GroupID,
			Tokens:         tokens,
			RequestCount:   1,
			RateMultiplier: rate,
			ServiceTier:    input.ServiceTier,
			Resolver:       input.Resolver,
		})
	} else {
		cost, err = input.BillingService.CalculateCostWithServiceTier(input.Model, tokens, rate, input.ServiceTier)
	}
	if err != nil {
		return nil, err
	}
	return &UsageReservationQuote{
		Body:           body,
		BodyModified:   modified,
		InputTokenCap:  tokens.InputTokens,
		OutputTokenCap: outputCap,
		RequiredUSD:    cost.ActualCost,
	}, nil
}

func normalizeReservationInputCap(v int) int {
	if v > 0 {
		return v
	}
	return 128000
}

func normalizeReservationOutputCap(v int) int {
	if v > 0 {
		return v
	}
	return 8192
}

func clampReservationOutputTokens(body []byte, endpoint string, cap int) ([]byte, bool, error) {
	field := reservationOutputTokenField(endpoint, body)
	current := gjson.GetBytes(body, field)
	if current.Exists() && current.Int() > 0 && int(current.Int()) <= cap {
		return body, false, nil
	}
	modified, err := sjson.SetBytes(body, field, cap)
	if err != nil {
		return nil, false, err
	}
	return modified, true, nil
}

func reservationOutputTokenField(endpoint string, body []byte) string {
	switch strings.TrimSpace(endpoint) {
	case UsageReservationEndpointAnthropicMessages:
		return "max_tokens"
	case UsageReservationEndpointChatCompletions:
		if gjson.GetBytes(body, "max_completion_tokens").Exists() {
			return "max_completion_tokens"
		}
		return "max_tokens"
	default:
		return "max_output_tokens"
	}
}
