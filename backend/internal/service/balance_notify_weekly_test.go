//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type weeklyUsageRepoStub struct {
	UsageLogRepository
	spend float64
	calls int
}

func (s *weeklyUsageRepoStub) GetUserStatsAggregated(context.Context, int64, time.Time, time.Time) (*usagestats.UsageStats, error) {
	s.calls++
	return &usagestats.UsageStats{TotalActualCost: s.spend}, nil
}

type weeklyNotificationStoreStub struct {
	UserRepository
	claimed bool
	calls   int
	weekKey string
}

func (s *weeklyNotificationStoreStub) ClaimWeeklyThresholdNotification(_ context.Context, _ int64, weekKey string) (bool, error) {
	s.calls++
	s.weekKey = weekKey
	return s.claimed, nil
}

func TestClaimWeeklyCostThreshold_CountsCurrentDeductionAndDeduplicates(t *testing.T) {
	usageRepo := &weeklyUsageRepoStub{spend: 95}
	userRepo := &weeklyNotificationStoreStub{claimed: true}
	svc := NewBalanceNotifyService(nil, nil, nil, usageRepo, userRepo)
	threshold := 100.0
	user := &User{ID: 42, WeeklyCostThreshold: &threshold}

	spend, gotThreshold, claimed := svc.claimWeeklyCostThreshold(context.Background(), user, 10)
	require.True(t, claimed)
	require.Equal(t, 105.0, spend)
	require.Equal(t, threshold, gotThreshold)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, userRepo.calls)
	require.Equal(t, CurrentNaturalWeekKey(), userRepo.weekKey)

	_, _, claimed = svc.claimWeeklyCostThreshold(context.Background(), user, 1)
	require.False(t, claimed)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, userRepo.calls)
}

func TestClaimWeeklyCostThreshold_AccumulatesCurrentCostsBetweenCachedQueries(t *testing.T) {
	usageRepo := &weeklyUsageRepoStub{spend: 80}
	userRepo := &weeklyNotificationStoreStub{claimed: true}
	svc := NewBalanceNotifyService(nil, nil, nil, usageRepo, userRepo)
	threshold := 100.0
	user := &User{ID: 43, WeeklyCostThreshold: &threshold}

	_, _, claimed := svc.claimWeeklyCostThreshold(context.Background(), user, 10)
	require.False(t, claimed)
	spend, _, claimed := svc.claimWeeklyCostThreshold(context.Background(), user, 15)
	require.True(t, claimed)
	require.Equal(t, 105.0, spend)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, userRepo.calls)
}

func TestClaimWeeklyCostThreshold_RespectsPersistedWeekMarker(t *testing.T) {
	usageRepo := &weeklyUsageRepoStub{spend: 1000}
	userRepo := &weeklyNotificationStoreStub{claimed: true}
	svc := NewBalanceNotifyService(nil, nil, nil, usageRepo, userRepo)
	threshold := 100.0
	weekKey := CurrentNaturalWeekKey()
	user := &User{
		ID:                          44,
		WeeklyCostThreshold:         &threshold,
		WeeklyThresholdNotifiedWeek: &weekKey,
	}

	_, _, claimed := svc.claimWeeklyCostThreshold(context.Background(), user, 10)
	require.False(t, claimed)
	require.Zero(t, usageRepo.calls)
	require.Zero(t, userRepo.calls)
}
