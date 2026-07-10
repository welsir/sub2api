//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyAuthCacheEntryCarriesWeeklyThreshold(t *testing.T) {
	threshold := 75.0
	week := "2026-07-04"
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-weekly", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  apiKeyAuthSnapshotVersion,
			APIKeyID: 1,
			UserID:   2,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:                          2,
				Status:                      StatusActive,
				Role:                        RoleUser,
				WeeklyCostThreshold:         &threshold,
				WeeklyThresholdNotifiedWeek: &week,
			},
		},
	})

	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, apiKey)
	require.NotNil(t, apiKey.User)
	require.Equal(t, 75.0, *apiKey.User.WeeklyCostThreshold)
	require.Equal(t, week, *apiKey.User.WeeklyThresholdNotifiedWeek)
}
