//go:build unit

package service

import "testing"

// Verifies the per-user model whitelist survives the auth cache snapshot path:
// snapshot.User.AllowedModels must be reconstructed onto apiKey.User so the
// gateway hot-path admission gate can enforce it without a DB round-trip.
func TestApplyAuthCacheEntry_CarriesAllowedModels(t *testing.T) {
	groupID := int64(7)
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-models", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  apiKeyAuthSnapshotVersion,
			APIKeyID: 1,
			UserID:   2,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:            2,
				Status:        StatusActive,
				Role:          RoleUser,
				Balance:       10,
				Concurrency:   3,
				AllowedModels: []string{"claude-*", "gpt-5"},
			},
			Group: &APIKeyAuthGroupSnapshot{
				ID:               groupID,
				Name:             "openai",
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   1,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || apiKey == nil || apiKey.User == nil {
		t.Fatalf("expected snapshot to be accepted with a populated user, got ok=%v apiKey=%#v", ok, apiKey)
	}

	got := apiKey.User.AllowedModels
	if len(got) != 2 || got[0] != "claude-*" || got[1] != "gpt-5" {
		t.Fatalf("AllowedModels not carried through snapshot: %v", got)
	}
	if !apiKey.User.AllowsModel("claude-opus-4-6") {
		t.Fatal("expected wildcard-allowed model to pass")
	}
	if apiKey.User.AllowsModel("gemini-2.5-pro") {
		t.Fatal("expected non-whitelisted model to be blocked")
	}
}
