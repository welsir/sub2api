package service

import (
	"context"
	"testing"
)

func TestBuildUsageReservationQuote_ClampsResponsesMaxOutputTokensAndComputesHold(t *testing.T) {
	billing := NewBillingService(nil, nil)
	body := []byte(`{"model":"gpt-5.4","max_output_tokens":20000,"input":"hello"}`)

	quote, err := BuildUsageReservationQuote(context.Background(), UsageReservationQuoteInput{
		BillingService:        billing,
		Model:                 "gpt-5.4",
		Body:                  body,
		Endpoint:              UsageReservationEndpointResponses,
		InputTokenUpperBound:  128000,
		OutputTokenUpperBound: 8192,
		RateMultiplier:        1,
	})
	if err != nil {
		t.Fatalf("BuildUsageReservationQuote returned error: %v", err)
	}

	if quote.OutputTokenCap != 8192 {
		t.Fatalf("OutputTokenCap = %d, want 8192", quote.OutputTokenCap)
	}
	if !quote.BodyModified {
		t.Fatalf("BodyModified = false, want true")
	}
	if got, want := string(quote.Body), `{"model":"gpt-5.4","max_output_tokens":8192,"input":"hello"}`; got != want {
		t.Fatalf("Body = %s, want %s", got, want)
	}
	if diff := quote.RequiredUSD - 0.44288; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("RequiredUSD = %.8f, want %.8f", quote.RequiredUSD, 0.44288)
	}
}

func TestBuildUsageReservationQuote_DefaultsMissingAnthropicMaxTokens(t *testing.T) {
	billing := NewBillingService(nil, nil)
	body := []byte(`{"model":"claude-sonnet-4","messages":[]}`)

	quote, err := BuildUsageReservationQuote(context.Background(), UsageReservationQuoteInput{
		BillingService:        billing,
		Model:                 "claude-sonnet-4",
		Body:                  body,
		Endpoint:              UsageReservationEndpointAnthropicMessages,
		InputTokenUpperBound:  128000,
		OutputTokenUpperBound: 8192,
		RateMultiplier:        1,
	})
	if err != nil {
		t.Fatalf("BuildUsageReservationQuote returned error: %v", err)
	}

	if got, want := string(quote.Body), `{"model":"claude-sonnet-4","messages":[],"max_tokens":8192}`; got != want {
		t.Fatalf("Body = %s, want %s", got, want)
	}
	if diff := quote.RequiredUSD - 0.50688; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("RequiredUSD = %.8f, want %.8f", quote.RequiredUSD, 0.50688)
	}
}
