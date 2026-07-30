// [INPUT]: Activation configuration, verified users, and activation service dependencies.
// [OUTPUT]: Unit proof for activation safety gates, durable outcomes, and active entitlements.
// [POS]: Service-layer TDD contract for the HVOY new-user activation workflow.
//
// [PROTOCOL]:
// 1. Update this header when activation service behavior under test changes.
// 2. Keep PostgreSQL locking guarantees in the repository integration test.
package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestUserActivationDisabledDoesNotBootstrap(t *testing.T) {
	journeys := newActivationJourneyRepoStub()
	subscriptions, subs := newActivationSubscriptionService()
	svc := NewUserActivationService(
		config.UserActivationConfig{},
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		nil,
	)

	err := svc.BootstrapVerifiedRegistration(context.Background(), &User{ID: 1}, "hvoy_partner")

	require.NoError(t, err)
	require.Zero(t, journeys.createCalls)
	require.Zero(t, subs.createCalls)
}

func TestUserActivationBeforeEligibleAfterDoesNotBootstrap(t *testing.T) {
	eligibleAfter := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	journeys := newActivationJourneyRepoStub()
	subscriptions, subs := newActivationSubscriptionService()
	svc := NewUserActivationService(
		config.UserActivationConfig{
			Enabled:       true,
			EligibleAfter: eligibleAfter,
		},
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		nil,
	)

	err := svc.BootstrapVerifiedRegistration(context.Background(), &User{
		ID:           1,
		SignupSource: "email",
		CreatedAt:    eligibleAfter.Add(-time.Nanosecond),
	}, "hvoy_partner")

	require.NoError(t, err)
	require.Zero(t, journeys.createCalls)
	require.Zero(t, subs.createCalls)
}

func TestUserActivationEligibleRegistrationGrantsStarter(t *testing.T) {
	eligibleAfter := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	user := &User{
		ID:           1,
		Email:        "starter@example.com",
		SignupSource: "email",
		CreatedAt:    eligibleAfter,
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	subscriptions, subs := newActivationSubscriptionService()
	svc := NewUserActivationService(
		config.UserActivationConfig{
			Enabled:        true,
			EligibleAfter:  eligibleAfter,
			StarterGroupID: 101,
		},
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		client,
	)
	expectActivationTransaction(sqlMock, user, true)

	err := svc.BootstrapVerifiedRegistration(context.Background(), user, "hvoy_partner")

	require.NoError(t, err)
	require.Equal(t, 1, journeys.createCalls)
	require.Equal(t, 1, journeys.updateCalls)
	require.Equal(t, 1, subs.createCalls)
	granted := subs.subscription(user.ID, 101)
	require.NotNil(t, granted)
	require.Equal(t, "user_activation:starter", granted.Notes)
	require.Equal(t, 24*time.Hour, granted.ExpiresAt.Sub(granted.StartsAt))
	stored := journeys.journey(user.ID)
	require.Equal(t, "granted", stored.StarterState)
	require.NotNil(t, stored.StarterSubscriptionID)
	require.Equal(t, granted.ID, *stored.StarterSubscriptionID)
	require.Equal(t, granted.StartsAt, *stored.StarterGrantedAt)
	require.Equal(t, granted.ExpiresAt, *stored.StarterExpiresAt)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationEligibilityAndEmailVerificationGates(t *testing.T) {
	eligibleAfter := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	deletedAt := eligibleAfter.Add(time.Hour)
	tests := []struct {
		name  string
		user  *User
		email bool
	}{
		{name: "nil user", user: nil, email: true},
		{
			name: "deleted user",
			user: &User{
				ID:           1,
				SignupSource: "email",
				CreatedAt:    eligibleAfter,
				DeletedAt:    &deletedAt,
			},
			email: true,
		},
		{
			name: "oauth user",
			user: &User{
				ID:           2,
				SignupSource: "google",
				CreatedAt:    eligibleAfter,
			},
			email: true,
		},
		{
			name: "email verification disabled",
			user: &User{
				ID:           3,
				SignupSource: "email",
				CreatedAt:    eligibleAfter,
			},
			email: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			journeys := newActivationJourneyRepoStub()
			subscriptions, subs := newActivationSubscriptionService()
			svc := NewUserActivationService(
				config.UserActivationConfig{
					Enabled:        true,
					EligibleAfter:  eligibleAfter,
					StarterGroupID: 101,
				},
				journeys,
				&activationEvidenceRepoStub{},
				subscriptions,
				activationSettingService(test.email),
				nil,
			)

			err := svc.BootstrapVerifiedRegistration(context.Background(), test.user, "direct")

			if test.name == "email verification disabled" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Zero(t, journeys.createCalls)
			require.Zero(t, subs.createCalls)
		})
	}
}

func TestUserActivationStarterRetryDoesNotExtendOrDuplicate(t *testing.T) {
	now := time.Now().UTC()
	user := &User{
		ID:           11,
		Email:        "starter-retry@example.com",
		SignupSource: "email",
		CreatedAt:    now,
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	subscriptions, subs := newActivationSubscriptionService()
	svc := NewUserActivationService(
		config.UserActivationConfig{
			Enabled:        true,
			EligibleAfter:  now.Add(-time.Hour),
			StarterGroupID: 101,
		},
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		client,
	)

	expectActivationTransaction(sqlMock, user, true)
	require.NoError(t, svc.BootstrapVerifiedRegistration(context.Background(), user, "hvoy_partner"))
	first := subs.subscription(user.ID, 101)
	require.NotNil(t, first)

	expectActivationTransaction(sqlMock, user, true)
	require.NoError(t, svc.BootstrapVerifiedRegistration(context.Background(), user, "ignored_retry_source"))
	retried := subs.subscription(user.ID, 101)
	require.NotNil(t, retried)

	require.Equal(t, 1, subs.createCalls)
	require.Equal(t, first.ID, retried.ID)
	require.Equal(t, first.StartsAt, retried.StartsAt)
	require.Equal(t, first.ExpiresAt, retried.ExpiresAt)
	require.Equal(t, "hvoy_partner", journeys.journey(user.ID).CampaignSource)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationEvaluateUsesLockedSegmentPriority(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	successAt := now.Add(-3 * time.Hour)
	paidAt := now.Add(-2 * time.Hour)
	tests := []struct {
		name     string
		evidence *UserActivationEvidence
		segment  string
	}{
		{
			name: "success wins over paid and attempted",
			evidence: &UserActivationEvidence{
				FirstSuccessfulUsageAt:  &successAt,
				FirstCompletedPaymentAt: &paidAt,
				UsageCount:              2,
				APIKeyCount:             1,
			},
			segment: "SUCCESS",
		},
		{
			name: "paid wins over attempted",
			evidence: &UserActivationEvidence{
				FirstCompletedPaymentAt: &paidAt,
				UsageCount:              2,
				APIKeyCount:             1,
			},
			segment: "PAID_ZERO_SUCCESS",
		},
		{
			name: "usage attempt",
			evidence: &UserActivationEvidence{
				UsageCount: 1,
			},
			segment: "ATTEMPTED_ZERO_SUCCESS",
		},
		{
			name: "api key attempt",
			evidence: &UserActivationEvidence{
				APIKeyCount: 1,
			},
			segment: "ATTEMPTED_ZERO_SUCCESS",
		},
		{
			name:     "registered only",
			evidence: &UserActivationEvidence{},
			segment:  "REGISTERED_NO_ATTEMPT",
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user := &User{
				ID:           int64(100 + index),
				Email:        fmt.Sprintf("segment-%d@example.com", index),
				SignupSource: "email",
				CreatedAt:    now.Add(-2 * 24 * time.Hour),
			}
			client, sqlMock := newActivationServiceSQLMockClient(t)
			journeys := newActivationJourneyRepoStub()
			seedActivationJourney(t, journeys, user.ID, func(journey *UserActivationJourney) {
				starterExpiry := now.Add(-time.Hour)
				journey.StarterState = "expired"
				journey.StarterExpiresAt = &starterExpiry
			})
			evidence := &activationEvidenceRepoStub{
				byUser: map[int64]*UserActivationEvidence{user.ID: test.evidence},
			}
			subscriptions, _ := newActivationSubscriptionService()
			svc := NewUserActivationService(
				testActivationConfig(now),
				journeys,
				evidence,
				subscriptions,
				activationSettingService(true),
				client,
			)
			expectActivationTransaction(sqlMock, user, true)

			status, err := svc.Evaluate(context.Background(), user.ID, now)

			require.NoError(t, err)
			require.Equal(t, test.segment, status.Segment)
			require.Equal(t, now, *journeys.journey(user.ID).LastEvaluatedAt)
			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}
}

func TestUserActivationEvaluateWritesFirstSuccessOnlyOnce(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	firstSuccess := now.Add(-4 * time.Hour)
	laterEvidence := now.Add(-time.Hour)
	user := &User{
		ID:           201,
		Email:        "success-once@example.com",
		SignupSource: "email",
		CreatedAt:    now.Add(-24 * time.Hour),
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	seedActivationJourney(t, journeys, user.ID, nil)
	evidence := &activationEvidenceRepoStub{
		byUser: map[int64]*UserActivationEvidence{
			user.ID: {FirstSuccessfulUsageAt: &firstSuccess},
		},
	}
	subscriptions, _ := newActivationSubscriptionService()
	svc := NewUserActivationService(
		testActivationConfig(now),
		journeys,
		evidence,
		subscriptions,
		activationSettingService(true),
		client,
	)

	expectActivationTransaction(sqlMock, user, true)
	first, err := svc.Evaluate(context.Background(), user.ID, now)
	require.NoError(t, err)
	require.Equal(t, firstSuccess, *first.FirstSuccessAt)

	evidence.mu.Lock()
	evidence.byUser[user.ID] = &UserActivationEvidence{FirstSuccessfulUsageAt: &laterEvidence}
	evidence.mu.Unlock()
	expectActivationTransaction(sqlMock, user, true)
	second, err := svc.Evaluate(context.Background(), user.ID, now.Add(time.Hour))
	require.NoError(t, err)

	require.Equal(t, firstSuccess, *second.FirstSuccessAt)
	require.Equal(t, firstSuccess, *journeys.journey(user.ID).FirstSuccessAt)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationDurableSuccessSurvivesMissingSnapshotAndBlocksRecall(t *testing.T) {
	now := time.Now().UTC()
	firstSuccess := now.Add(-4 * time.Hour)
	user := &User{
		ID:           251,
		Email:        "durable-success@example.com",
		SignupSource: "email",
		CreatedAt:    now.Add(-2 * 24 * time.Hour),
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	seedActivationJourney(t, journeys, user.ID, func(journey *UserActivationJourney) {
		expired := now.Add(-time.Hour)
		starterID := int64(851)
		journey.StarterState = "expired"
		journey.StarterSubscriptionID = &starterID
		journey.StarterExpiresAt = &expired
	})
	evidence := &activationEvidenceRepoStub{
		byUser: map[int64]*UserActivationEvidence{
			user.ID: {FirstSuccessfulUsageAt: &firstSuccess},
		},
	}
	subscriptions, subs := newActivationSubscriptionService()
	svc := NewUserActivationService(
		testActivationConfig(now),
		journeys,
		evidence,
		subscriptions,
		activationSettingService(true),
		client,
	)

	expectActivationTransaction(sqlMock, user, true)
	first, err := svc.Evaluate(context.Background(), user.ID, now)
	require.NoError(t, err)
	require.Equal(t, UserActivationSegmentSuccess, first.Segment)
	require.Equal(t, "closed_success", first.Recall.State)
	require.Equal(t, firstSuccess, *first.FirstSuccessAt)

	evidence.mu.Lock()
	evidence.byUser[user.ID] = nil
	evidence.mu.Unlock()
	expectActivationTransaction(sqlMock, user, true)
	second, err := svc.Evaluate(context.Background(), user.ID, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, UserActivationSegmentSuccess, second.Segment)
	assert.Equal(t, "closed_success", second.Recall.State)
	assert.Equal(t, firstSuccess, *second.FirstSuccessAt)

	expectActivationTransaction(sqlMock, user, true)
	status, err := svc.ClaimRecall(context.Background(), user.ID)
	assert.ErrorIs(t, err, ErrUserActivationRecallUnavailable)
	require.NotNil(t, status)
	assert.Equal(t, UserActivationSegmentSuccess, status.Segment)
	assert.Equal(t, "closed_success", status.Recall.State)
	assert.Zero(t, subs.createCalls)
	assert.Nil(t, subs.subscription(user.ID, 202))
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationActiveRecallRemainsEntitlementAfterSuccess(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	successAt := now.Add(-15 * time.Minute)
	claimedAt := now.Add(-30 * time.Minute)
	expiresAt := now.Add(30 * time.Minute)
	recallSubscriptionID := int64(961)
	user := &User{
		ID:           261,
		Email:        "active-recall-success@example.com",
		SignupSource: "email",
		CreatedAt:    now.Add(-2 * 24 * time.Hour),
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	seedActivationJourney(t, journeys, user.ID, func(journey *UserActivationJourney) {
		journey.RecallState = "claimed"
		journey.RecallSubscriptionID = &recallSubscriptionID
		journey.RecallClaimedAt = &claimedAt
		journey.RecallExpiresAt = &expiresAt
	})
	evidence := &activationEvidenceRepoStub{
		byUser: map[int64]*UserActivationEvidence{
			user.ID: {FirstSuccessfulUsageAt: &successAt},
		},
	}
	subscriptions, _ := newActivationSubscriptionService()
	svc := NewUserActivationService(
		testActivationConfig(now),
		journeys,
		evidence,
		subscriptions,
		activationSettingService(true),
		client,
	)
	expectActivationTransaction(sqlMock, user, true)

	status, err := svc.Evaluate(context.Background(), user.ID, now)

	require.NoError(t, err)
	require.Equal(t, UserActivationSegmentSuccess, status.Segment)
	require.Equal(t, "closed_success", status.Recall.State)
	require.NotNil(t, status.ActiveGroup)
	require.Equal(t, int64(202), status.ActiveGroup.GroupID)
	require.Equal(t, recallSubscriptionID, status.ActiveGroup.SubscriptionID)
	require.Equal(t, claimedAt, status.ActiveGroup.StartsAt)
	require.Equal(t, expiresAt, status.ActiveGroup.ExpiresAt)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationActiveRecallRemainsEntitlementAfterPayment(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	paidAt := now.Add(-15 * time.Minute)
	claimedAt := now.Add(-30 * time.Minute)
	expiresAt := now.Add(30 * time.Minute)
	recallSubscriptionID := int64(962)
	user := &User{
		ID:           262,
		Email:        "active-recall-paid@example.com",
		SignupSource: "email",
		CreatedAt:    now.Add(-2 * 24 * time.Hour),
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	seedActivationJourney(t, journeys, user.ID, func(journey *UserActivationJourney) {
		journey.RecallState = "claimed"
		journey.RecallSubscriptionID = &recallSubscriptionID
		journey.RecallClaimedAt = &claimedAt
		journey.RecallExpiresAt = &expiresAt
	})
	evidence := &activationEvidenceRepoStub{
		byUser: map[int64]*UserActivationEvidence{
			user.ID: {FirstCompletedPaymentAt: &paidAt},
		},
	}
	subscriptions, _ := newActivationSubscriptionService()
	svc := NewUserActivationService(
		testActivationConfig(now),
		journeys,
		evidence,
		subscriptions,
		activationSettingService(true),
		client,
	)
	expectActivationTransaction(sqlMock, user, true)

	status, err := svc.Evaluate(context.Background(), user.ID, now)

	require.NoError(t, err)
	require.Equal(t, UserActivationSegmentPaidZeroSuccess, status.Segment)
	require.Equal(t, "blocked_paid", status.Recall.State)
	require.NotNil(t, status.ActiveGroup)
	require.Equal(t, int64(202), status.ActiveGroup.GroupID)
	require.Equal(t, recallSubscriptionID, status.ActiveGroup.SubscriptionID)
	require.Equal(t, claimedAt, status.ActiveGroup.StartsAt)
	require.Equal(t, expiresAt, status.ActiveGroup.ExpiresAt)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationRecallEntitlementRequiresClaimedAtByEvaluationTime(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	claimedAt := now.Add(time.Minute)
	expiresAt := now.Add(30 * time.Minute)
	recallSubscriptionID := int64(963)
	user := &User{
		ID:           263,
		Email:        "future-recall@example.com",
		SignupSource: "email",
		CreatedAt:    now.Add(-2 * 24 * time.Hour),
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	seedActivationJourney(t, journeys, user.ID, func(journey *UserActivationJourney) {
		journey.RecallState = "claimed"
		journey.RecallSubscriptionID = &recallSubscriptionID
		journey.RecallClaimedAt = &claimedAt
		journey.RecallExpiresAt = &expiresAt
	})
	subscriptions, _ := newActivationSubscriptionService()
	svc := NewUserActivationService(
		testActivationConfig(now),
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		client,
	)
	expectActivationTransaction(sqlMock, user, true)

	status, err := svc.Evaluate(context.Background(), user.ID, now)

	require.NoError(t, err)
	require.Nil(t, status.ActiveGroup)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationRecallEntitlementFieldAndTimeBoundaries(t *testing.T) {
	evaluationNow := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	expiresAfterEvaluation := evaluationNow.Add(time.Minute)
	recallSubscriptionID := int64(964)
	tests := []struct {
		name       string
		mutate     func(*UserActivationJourney)
		wantActive bool
	}{
		{
			name:       "claimed at evaluation with later expiry is active",
			wantActive: true,
		},
		{
			name: "expiry equal to evaluation is inactive",
			mutate: func(journey *UserActivationJourney) {
				journey.RecallExpiresAt = &evaluationNow
			},
		},
		{
			name: "missing subscription id is inactive",
			mutate: func(journey *UserActivationJourney) {
				journey.RecallSubscriptionID = nil
			},
		},
		{
			name: "missing claimed at is inactive",
			mutate: func(journey *UserActivationJourney) {
				journey.RecallClaimedAt = nil
			},
		},
		{
			name: "missing expires at is inactive",
			mutate: func(journey *UserActivationJourney) {
				journey.RecallExpiresAt = nil
			},
		},
		{
			name: "missing last evaluated at is inactive",
			mutate: func(journey *UserActivationJourney) {
				journey.LastEvaluatedAt = nil
			},
		},
	}
	svc := &UserActivationService{
		cfg: config.UserActivationConfig{RecallGroupID: 202},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			journey := &UserActivationJourney{
				RecallState:          "claimed",
				RecallSubscriptionID: &recallSubscriptionID,
				RecallClaimedAt:      &evaluationNow,
				RecallExpiresAt:      &expiresAfterEvaluation,
				LastEvaluatedAt:      &evaluationNow,
			}
			if test.mutate != nil {
				test.mutate(journey)
			}

			status := svc.statusFromJourney(journey, &UserActivationEvidence{})

			if !test.wantActive {
				require.Nil(t, status.ActiveGroup)
				return
			}
			require.NotNil(t, status.ActiveGroup)
			require.Equal(t, int64(202), status.ActiveGroup.GroupID)
			require.Equal(t, recallSubscriptionID, status.ActiveGroup.SubscriptionID)
			require.Equal(t, evaluationNow, status.ActiveGroup.StartsAt)
			require.Equal(t, expiresAfterEvaluation, status.ActiveGroup.ExpiresAt)
		})
	}
}

func TestUserActivationEvaluateRecallStateTable(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	evidenceAt := now.Add(-time.Hour)
	claimedAt := now.Add(-2 * time.Hour)
	activeRecallExpiry := now.Add(time.Hour)
	expiredRecallExpiry := now
	activeStarterExpiry := now.Add(time.Nanosecond)
	expiredStarterExpiry := now
	starterID := int64(801)
	recallID := int64(901)
	tests := []struct {
		name      string
		createdAt time.Time
		evidence  *UserActivationEvidence
		mutate    func(*UserActivationJourney)
		recall    string
		claimable bool
	}{
		{
			name:      "success closes recall",
			createdAt: now.Add(-2 * 24 * time.Hour),
			evidence:  &UserActivationEvidence{FirstSuccessfulUsageAt: &evidenceAt},
			mutate: func(journey *UserActivationJourney) {
				journey.StarterSubscriptionID = &starterID
				journey.StarterExpiresAt = &expiredStarterExpiry
			},
			recall: "closed_success",
		},
		{
			name:      "paid blocks recall",
			createdAt: now.Add(-2 * 24 * time.Hour),
			evidence:  &UserActivationEvidence{FirstCompletedPaymentAt: &evidenceAt},
			mutate: func(journey *UserActivationJourney) {
				journey.StarterSubscriptionID = &starterID
				journey.StarterExpiresAt = &expiredStarterExpiry
			},
			recall: "blocked_paid",
		},
		{
			name:      "active claim remains claimed",
			createdAt: now.Add(-2 * 24 * time.Hour),
			evidence:  &UserActivationEvidence{},
			mutate: func(journey *UserActivationJourney) {
				journey.RecallSubscriptionID = &recallID
				journey.RecallClaimedAt = &claimedAt
				journey.RecallExpiresAt = &activeRecallExpiry
			},
			recall: "claimed",
		},
		{
			name:      "expired claim is never reissued",
			createdAt: now.Add(-2 * 24 * time.Hour),
			evidence:  &UserActivationEvidence{},
			mutate: func(journey *UserActivationJourney) {
				journey.RecallSubscriptionID = &recallID
				journey.RecallClaimedAt = &claimedAt
				journey.RecallExpiresAt = &expiredRecallExpiry
			},
			recall: "expired",
		},
		{
			name:      "active starter locks recall",
			createdAt: now.Add(-2 * 24 * time.Hour),
			evidence:  &UserActivationEvidence{},
			mutate: func(journey *UserActivationJourney) {
				journey.StarterState = "granted"
				journey.StarterSubscriptionID = &starterID
				journey.StarterExpiresAt = &activeStarterExpiry
			},
			recall: "locked",
		},
		{
			name:      "window equality is expired",
			createdAt: now.Add(-7 * 24 * time.Hour),
			evidence:  &UserActivationEvidence{},
			mutate: func(journey *UserActivationJourney) {
				journey.StarterState = "expired"
				journey.StarterSubscriptionID = &starterID
				journey.StarterExpiresAt = &expiredStarterExpiry
			},
			recall: "expired",
		},
		{
			name:      "expired starter inside window is claimable",
			createdAt: now.Add(-2 * 24 * time.Hour),
			evidence:  &UserActivationEvidence{},
			mutate: func(journey *UserActivationJourney) {
				journey.StarterState = "expired"
				journey.StarterSubscriptionID = &starterID
				journey.StarterExpiresAt = &expiredStarterExpiry
			},
			recall:    "claimable",
			claimable: true,
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user := &User{
				ID:           int64(300 + index),
				Email:        fmt.Sprintf("recall-state-%d@example.com", index),
				SignupSource: "email",
				CreatedAt:    test.createdAt,
			}
			client, sqlMock := newActivationServiceSQLMockClient(t)
			journeys := newActivationJourneyRepoStub()
			seedActivationJourney(t, journeys, user.ID, test.mutate)
			evidence := &activationEvidenceRepoStub{
				byUser: map[int64]*UserActivationEvidence{user.ID: test.evidence},
			}
			subscriptions, _ := newActivationSubscriptionService()
			svc := NewUserActivationService(
				testActivationConfig(now),
				journeys,
				evidence,
				subscriptions,
				activationSettingService(true),
				client,
			)
			expectActivationTransaction(sqlMock, user, true)

			status, err := svc.Evaluate(context.Background(), user.ID, now)

			require.NoError(t, err)
			require.Equal(t, test.recall, status.Recall.State)
			require.Equal(t, test.claimable, status.Recall.Claimable)
			require.Equal(t, test.recall, journeys.journey(user.ID).RecallState)
			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}
}

func TestUserActivationRecallWindowUsesConfiguredDays(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	expiredStarterAt := now.Add(-48 * time.Hour)
	starterSubscriptionID := int64(881)
	user := &User{
		ID:           381,
		Email:        "configured-recall-window@example.com",
		SignupSource: "email",
		CreatedAt:    now.Add(-3 * 24 * time.Hour),
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	seedActivationJourney(t, journeys, user.ID, func(journey *UserActivationJourney) {
		journey.StarterState = "expired"
		journey.StarterSubscriptionID = &starterSubscriptionID
		journey.StarterExpiresAt = &expiredStarterAt
	})
	cfg := testActivationConfig(now)
	cfg.RecallWindowDays = 2
	subscriptions, _ := newActivationSubscriptionService()
	svc := NewUserActivationService(
		cfg,
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		client,
	)
	expectActivationTransaction(sqlMock, user, true)

	status, err := svc.Evaluate(context.Background(), user.ID, now)

	require.NoError(t, err)
	require.Equal(t, "expired", status.Recall.State)
	require.False(t, status.Recall.Claimable)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationWithLockedActivationRollsBackOnPanic(t *testing.T) {
	now := time.Now().UTC()
	user := &User{
		ID:           391,
		Email:        "panic-rollback@example.com",
		SignupSource: "email",
		CreatedAt:    now,
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	seedActivationJourney(t, journeys, user.ID, nil)
	subscriptions, _ := newActivationSubscriptionService()
	svc := NewUserActivationService(
		testActivationConfig(now),
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		client,
	)
	sentinel := errors.New("panic inside activation mutation")
	expectActivationTransaction(sqlMock, user, false)

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		_, _ = svc.withLockedActivation(
			context.Background(),
			user.ID,
			func(
				context.Context,
				*User,
				*UserActivationJourney,
				*UserActivationEvidence,
				time.Time,
			) error {
				panic(sentinel)
			},
		)
	}()

	require.Same(t, sentinel, recovered)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationClaimRecallGrantsOnceWithoutExtension(t *testing.T) {
	now := time.Now().UTC()
	user := &User{
		ID:           401,
		Email:        "claim@example.com",
		SignupSource: "email",
		CreatedAt:    now.Add(-2 * 24 * time.Hour),
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	seedActivationJourney(t, journeys, user.ID, func(journey *UserActivationJourney) {
		expired := now.Add(-time.Hour)
		starterID := int64(811)
		journey.StarterState = "expired"
		journey.StarterSubscriptionID = &starterID
		journey.StarterExpiresAt = &expired
	})
	subscriptions, subs := newActivationSubscriptionService()
	svc := NewUserActivationService(
		testActivationConfig(now),
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		client,
	)

	expectActivationTransaction(sqlMock, user, true)
	first, err := svc.ClaimRecall(context.Background(), user.ID)
	require.NoError(t, err)
	require.Equal(t, "claimed", first.Recall.State)
	granted := subs.subscription(user.ID, 202)
	require.NotNil(t, granted)
	require.Equal(t, "user_activation:recall", granted.Notes)
	require.Equal(t, 24*time.Hour, granted.ExpiresAt.Sub(granted.StartsAt))
	require.NotNil(t, first.ActiveGroup)
	require.Equal(t, int64(202), first.ActiveGroup.GroupID)
	require.Equal(t, granted.ID, first.ActiveGroup.SubscriptionID)
	require.Equal(t, granted.StartsAt, first.ActiveGroup.StartsAt)
	require.Equal(t, granted.ExpiresAt, first.ActiveGroup.ExpiresAt)

	expectActivationTransaction(sqlMock, user, true)
	replayed, err := svc.ClaimRecall(context.Background(), user.ID)
	require.NoError(t, err)
	require.Equal(t, "claimed", replayed.Recall.State)
	replayedGrant := subs.subscription(user.ID, 202)

	require.Equal(t, 1, subs.createCalls)
	require.Equal(t, granted.ID, replayedGrant.ID)
	require.Equal(t, granted.StartsAt, replayedGrant.StartsAt)
	require.Equal(t, granted.ExpiresAt, replayedGrant.ExpiresAt)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationClaimRecallBlocksIneligibleEvidenceAndTime(t *testing.T) {
	baseNow := time.Now().UTC()
	evidenceAt := baseNow.Add(-time.Hour)
	tests := []struct {
		name      string
		createdAt time.Time
		evidence  *UserActivationEvidence
		starterAt time.Time
	}{
		{
			name:      "starter active",
			createdAt: baseNow.Add(-2 * 24 * time.Hour),
			evidence:  &UserActivationEvidence{},
			starterAt: baseNow.Add(time.Hour),
		},
		{
			name:      "paid",
			createdAt: baseNow.Add(-2 * 24 * time.Hour),
			evidence:  &UserActivationEvidence{FirstCompletedPaymentAt: &evidenceAt},
			starterAt: baseNow.Add(-time.Hour),
		},
		{
			name:      "successful",
			createdAt: baseNow.Add(-2 * 24 * time.Hour),
			evidence:  &UserActivationEvidence{FirstSuccessfulUsageAt: &evidenceAt},
			starterAt: baseNow.Add(-time.Hour),
		},
		{
			name:      "window expired",
			createdAt: baseNow.Add(-7*24*time.Hour - time.Minute),
			evidence:  &UserActivationEvidence{},
			starterAt: baseNow.Add(-time.Hour),
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user := &User{
				ID:           int64(500 + index),
				Email:        fmt.Sprintf("claim-block-%d@example.com", index),
				SignupSource: "email",
				CreatedAt:    test.createdAt,
			}
			client, sqlMock := newActivationServiceSQLMockClient(t)
			journeys := newActivationJourneyRepoStub()
			seedActivationJourney(t, journeys, user.ID, func(journey *UserActivationJourney) {
				journey.StarterState = "granted"
				journey.StarterExpiresAt = &test.starterAt
			})
			evidence := &activationEvidenceRepoStub{
				byUser: map[int64]*UserActivationEvidence{user.ID: test.evidence},
			}
			subscriptions, subs := newActivationSubscriptionService()
			svc := NewUserActivationService(
				testActivationConfig(baseNow),
				journeys,
				evidence,
				subscriptions,
				activationSettingService(true),
				client,
			)
			expectActivationTransaction(sqlMock, user, true)

			status, err := svc.ClaimRecall(context.Background(), user.ID)

			require.Error(t, err)
			require.NotNil(t, status)
			require.Zero(t, subs.createCalls)
			require.Nil(t, subs.subscription(user.ID, 202))
			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}
}

func TestUserActivationAssignFailureLeavesPendingJourneyRetriable(t *testing.T) {
	now := time.Now().UTC()
	user := &User{
		ID:           601,
		Email:        "assign-retry@example.com",
		SignupSource: "email",
		CreatedAt:    now,
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	subscriptions, subs := newActivationSubscriptionService()
	subs.createErr = errors.New("assign unavailable")
	svc := NewUserActivationService(
		testActivationConfig(now),
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		client,
	)

	expectActivationTransaction(sqlMock, user, false)
	err := svc.BootstrapVerifiedRegistration(context.Background(), user, "direct")
	require.ErrorContains(t, err, "assign unavailable")
	pending := journeys.journey(user.ID)
	require.NotNil(t, pending)
	require.Equal(t, "pending", pending.StarterState)
	require.Nil(t, pending.StarterSubscriptionID)

	expectActivationTransaction(sqlMock, user, true)
	require.NoError(t, svc.BootstrapVerifiedRegistration(context.Background(), user, "direct"))
	require.Equal(t, 2, subs.createCalls)
	require.NotNil(t, journeys.journey(user.ID).StarterSubscriptionID)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationCommittedGrantRetriesSynchronousCacheInvalidation(t *testing.T) {
	now := time.Now().UTC()
	user := &User{
		ID:           651,
		Email:        "cache-retry@example.com",
		SignupSource: "email",
		CreatedAt:    now,
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	subscriptions, subs := newActivationSubscriptionService()
	cache := &activationBillingCacheStub{
		invalidateErr: errors.New("redis unavailable"),
		onInvalidate: func() {
			require.NoError(t, sqlMock.ExpectationsWereMet(), "cache invalidation must run after commit")
		},
	}
	subscriptions.billingCacheService = &BillingCacheService{cache: cache}
	svc := NewUserActivationService(
		testActivationConfig(now),
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		client,
	)

	expectActivationTransaction(sqlMock, user, true)
	err := svc.BootstrapVerifiedRegistration(context.Background(), user, "direct")
	var committedErr *UserActivationGrantCommittedError
	require.ErrorAs(t, err, &committedErr)
	require.Equal(t, user.ID, committedErr.UserID)
	require.Equal(t, int64(101), committedErr.GroupID)
	require.NotNil(t, journeys.journey(user.ID).StarterSubscriptionID)
	require.Equal(t, 1, subs.createCalls)
	require.Equal(t, 1, cache.invalidationCalls())

	cache.setInvalidateError(nil)
	expectActivationTransaction(sqlMock, user, true)
	require.NoError(t, svc.BootstrapVerifiedRegistration(context.Background(), user, "direct"))
	require.Equal(t, 1, subs.createCalls, "retry after commit must not issue another grant")
	require.Equal(t, 2, cache.invalidationCalls(), "retry must synchronously reattempt cache invalidation")
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationRecallGrantRetriesCacheInvalidationWithoutDuplicateOrExtension(t *testing.T) {
	now := time.Now().UTC()
	user := &User{
		ID:           681,
		Email:        "recall-cache-retry@example.com",
		SignupSource: "email",
		CreatedAt:    now.Add(-2 * 24 * time.Hour),
	}
	client, sqlMock := newActivationServiceSQLMockClient(t)
	journeys := newActivationJourneyRepoStub()
	seedActivationJourney(t, journeys, user.ID, func(journey *UserActivationJourney) {
		expired := now.Add(-time.Hour)
		starterSubscriptionID := int64(891)
		journey.StarterState = "expired"
		journey.StarterSubscriptionID = &starterSubscriptionID
		journey.StarterExpiresAt = &expired
	})
	subscriptions, subs := newActivationSubscriptionService()
	cache := &activationBillingCacheStub{
		invalidateErr: errors.New("redis unavailable"),
		onInvalidate: func() {
			require.NoError(t, sqlMock.ExpectationsWereMet(), "cache invalidation must run after commit")
		},
	}
	subscriptions.billingCacheService = &BillingCacheService{cache: cache}
	svc := NewUserActivationService(
		testActivationConfig(now),
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		client,
	)

	expectActivationTransaction(sqlMock, user, true)
	firstStatus, err := svc.ClaimRecall(context.Background(), user.ID)
	var committedErr *UserActivationGrantCommittedError
	require.ErrorAs(t, err, &committedErr)
	require.NotNil(t, firstStatus)
	require.Equal(t, "claimed", firstStatus.Recall.State)
	require.Equal(t, user.ID, committedErr.UserID)
	require.Equal(t, int64(202), committedErr.GroupID)
	firstGrant := subs.subscription(user.ID, 202)
	require.NotNil(t, firstGrant)
	require.Equal(t, "user_activation:recall", firstGrant.Notes)
	require.NotNil(t, firstStatus.ActiveGroup)
	require.Equal(t, int64(202), firstStatus.ActiveGroup.GroupID)
	require.Equal(t, firstGrant.ID, firstStatus.ActiveGroup.SubscriptionID)
	require.Equal(t, firstGrant.StartsAt, firstStatus.ActiveGroup.StartsAt)
	require.Equal(t, firstGrant.ExpiresAt, firstStatus.ActiveGroup.ExpiresAt)
	require.Equal(t, 1, subs.createCalls)
	require.Equal(t, 1, cache.invalidationCalls())

	cache.setInvalidateError(nil)
	expectActivationTransaction(sqlMock, user, true)
	secondStatus, err := svc.ClaimRecall(context.Background(), user.ID)
	require.NoError(t, err)
	require.NotNil(t, secondStatus)
	require.Equal(t, "claimed", secondStatus.Recall.State)
	secondGrant := subs.subscription(user.ID, 202)
	require.NotNil(t, secondGrant)
	require.Equal(t, firstGrant.ID, secondGrant.ID)
	require.Equal(t, firstGrant.StartsAt, secondGrant.StartsAt)
	require.Equal(t, firstGrant.ExpiresAt, secondGrant.ExpiresAt)
	require.Equal(t, 1, subs.createCalls, "cache retry must not issue another recall grant")
	require.Equal(t, 2, cache.invalidationCalls(), "cache retry must synchronously invalidate again")
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestUserActivationDisabledStatusAndClaimAreSafe(t *testing.T) {
	journeys := newActivationJourneyRepoStub()
	subscriptions, subs := newActivationSubscriptionService()
	svc := NewUserActivationService(
		config.UserActivationConfig{},
		journeys,
		&activationEvidenceRepoStub{},
		subscriptions,
		activationSettingService(true),
		nil,
	)

	status, err := svc.GetStatus(context.Background(), 701)
	require.NoError(t, err)
	require.False(t, status.Enabled)

	status, err = svc.ClaimRecall(context.Background(), 701)
	require.NoError(t, err)
	require.False(t, status.Enabled)
	require.Zero(t, journeys.createCalls)
	require.Zero(t, subs.createCalls)
}

func TestUserActivationEnabledStatusPreservesNotFoundSemantics(t *testing.T) {
	now := time.Now().UTC()
	t.Run("missing journey", func(t *testing.T) {
		user := &User{
			ID:           751,
			Email:        "missing-journey@example.com",
			SignupSource: "email",
			CreatedAt:    now,
		}
		client, sqlMock := newActivationServiceSQLMockClient(t)
		journeys := newActivationJourneyRepoStub()
		subscriptions, _ := newActivationSubscriptionService()
		svc := NewUserActivationService(
			testActivationConfig(now),
			journeys,
			&activationEvidenceRepoStub{},
			subscriptions,
			activationSettingService(true),
			client,
		)
		expectActivationTransaction(sqlMock, user, false)

		status, err := svc.GetStatus(context.Background(), user.ID)

		require.Nil(t, status)
		require.ErrorIs(t, err, ErrUserActivationJourneyNotFound)
		require.NoError(t, sqlMock.ExpectationsWereMet())
	})

	t.Run("deleted user", func(t *testing.T) {
		client, sqlMock := newActivationServiceSQLMockClient(t)
		journeys := newActivationJourneyRepoStub()
		seedActivationJourney(t, journeys, 752, nil)
		subscriptions, _ := newActivationSubscriptionService()
		svc := NewUserActivationService(
			testActivationConfig(now),
			journeys,
			&activationEvidenceRepoStub{},
			subscriptions,
			activationSettingService(true),
			client,
		)
		expectActivationMissingUserTransaction(sqlMock, 752)

		status, err := svc.GetStatus(context.Background(), 752)

		require.Nil(t, status)
		require.ErrorIs(t, err, ErrUserNotFound)
		require.NoError(t, sqlMock.ExpectationsWereMet())
	})
}

func testActivationConfig(now time.Time) config.UserActivationConfig {
	return config.UserActivationConfig{
		Enabled:          true,
		EligibleAfter:    now.Add(-30 * 24 * time.Hour),
		StarterGroupID:   101,
		RecallGroupID:    202,
		RecallWindowDays: 7,
		SupportWeChat:    "welsir02",
	}
}

func seedActivationJourney(
	t *testing.T,
	repo *activationJourneyRepoStub,
	userID int64,
	mutate func(*UserActivationJourney),
) {
	t.Helper()
	journey, _, err := repo.CreateIfAbsent(context.Background(), userID, "direct")
	require.NoError(t, err)
	if mutate != nil {
		mutate(journey)
	}
	require.NoError(t, repo.Update(context.Background(), journey))
}

type activationJourneyRepoStub struct {
	UserActivationJourneyRepository

	mu          sync.Mutex
	nextID      int64
	byUserID    map[int64]*UserActivationJourney
	createCalls int
	updateCalls int
}

func newActivationJourneyRepoStub() *activationJourneyRepoStub {
	return &activationJourneyRepoStub{
		nextID:   1,
		byUserID: make(map[int64]*UserActivationJourney),
	}
}

func (r *activationJourneyRepoStub) CreateIfAbsent(
	_ context.Context,
	userID int64,
	source string,
) (*UserActivationJourney, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls++
	if existing := r.byUserID[userID]; existing != nil {
		return cloneActivationJourney(existing), false, nil
	}
	now := time.Now().UTC()
	journey := &UserActivationJourney{
		ID:             r.nextID,
		UserID:         userID,
		CampaignSource: source,
		StarterState:   "pending",
		RecallState:    "locked",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.nextID++
	r.byUserID[userID] = cloneActivationJourney(journey)
	return cloneActivationJourney(journey), true, nil
}

func (r *activationJourneyRepoStub) GetByUserID(
	_ context.Context,
	userID int64,
) (*UserActivationJourney, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	journey := r.byUserID[userID]
	if journey == nil {
		return nil, ErrUserActivationJourneyNotFound
	}
	return cloneActivationJourney(journey), nil
}

func (r *activationJourneyRepoStub) GetByUserIDForUpdate(
	ctx context.Context,
	userID int64,
) (*UserActivationJourney, error) {
	if dbent.TxFromContext(ctx) == nil {
		return nil, ErrUserActivationJourneyTransactionRequired
	}
	return r.GetByUserID(ctx, userID)
}

func (r *activationJourneyRepoStub) Update(_ context.Context, journey *UserActivationJourney) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if journey == nil || r.byUserID[journey.UserID] == nil {
		return ErrUserActivationJourneyNotFound
	}
	r.updateCalls++
	r.byUserID[journey.UserID] = cloneActivationJourney(journey)
	return nil
}

func (r *activationJourneyRepoStub) journey(userID int64) *UserActivationJourney {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneActivationJourney(r.byUserID[userID])
}

func cloneActivationJourney(journey *UserActivationJourney) *UserActivationJourney {
	if journey == nil {
		return nil
	}
	cloned := *journey
	return &cloned
}

type activationEvidenceRepoStub struct {
	UserActivationEvidenceRepository

	mu     sync.Mutex
	byUser map[int64]*UserActivationEvidence
	err    error
}

func (r *activationEvidenceRepoStub) Snapshot(
	_ context.Context,
	userID int64,
) (*UserActivationEvidence, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	evidence := r.byUser[userID]
	if evidence == nil {
		return &UserActivationEvidence{}, nil
	}
	cloned := *evidence
	return &cloned, nil
}

type activationSettingRepoStub struct {
	SettingRepository
	emailVerify bool
}

func (r *activationSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if key == SettingKeyEmailVerifyEnabled && r.emailVerify {
		return "true", nil
	}
	return "false", nil
}

func activationSettingService(emailVerify bool) *SettingService {
	return NewSettingService(&activationSettingRepoStub{emailVerify: emailVerify}, &config.Config{})
}

type activationBillingCacheStub struct {
	BillingCache

	mu            sync.Mutex
	invalidateErr error
	onInvalidate  func()
	invalidations int
	publications  int
}

func (c *activationBillingCacheStub) InvalidateSubscriptionCache(
	_ context.Context,
	_ int64,
	_ int64,
) error {
	c.mu.Lock()
	c.invalidations++
	err := c.invalidateErr
	onInvalidate := c.onInvalidate
	c.mu.Unlock()
	if onInvalidate != nil {
		onInvalidate()
	}
	return err
}

func (c *activationBillingCacheStub) PublishSubscriptionCacheInvalidation(
	_ context.Context,
	_ string,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publications++
	return nil
}

func (c *activationBillingCacheStub) SubscribeSubscriptionCacheInvalidation(
	_ context.Context,
	_ func(string),
) error {
	return nil
}

func (c *activationBillingCacheStub) setInvalidateError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidateErr = err
}

func (c *activationBillingCacheStub) invalidationCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.invalidations
}

type activationGroupRepoStub struct {
	GroupRepository
}

func (r *activationGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	return &Group{
		ID:               id,
		SubscriptionType: SubscriptionTypeSubscription,
	}, nil
}

type activationUserSubRepoStub struct {
	UserSubscriptionRepository

	mu          sync.Mutex
	nextID      int64
	byUserGroup map[string]*UserSubscription
	createCalls int
	createErr   error
}

func newActivationSubscriptionService() (*SubscriptionService, *activationUserSubRepoStub) {
	repo := &activationUserSubRepoStub{
		nextID:      1,
		byUserGroup: make(map[string]*UserSubscription),
	}
	return &SubscriptionService{
		groupRepo:   &activationGroupRepoStub{},
		userSubRepo: repo,
	}, repo
}

func (r *activationUserSubRepoStub) key(userID, groupID int64) string {
	return fmt.Sprintf("%d:%d", userID, groupID)
}

func (r *activationUserSubRepoStub) ExistsByUserIDAndGroupID(
	_ context.Context,
	userID int64,
	groupID int64,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, exists := r.byUserGroup[r.key(userID, groupID)]
	return exists, nil
}

func (r *activationUserSubRepoStub) GetByUserIDAndGroupID(
	_ context.Context,
	userID int64,
	groupID int64,
) (*UserSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	subscription := r.byUserGroup[r.key(userID, groupID)]
	if subscription == nil {
		return nil, ErrSubscriptionNotFound
	}
	cloned := *subscription
	return &cloned, nil
}

func (r *activationUserSubRepoStub) Create(_ context.Context, subscription *UserSubscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls++
	if r.createErr != nil {
		err := r.createErr
		r.createErr = nil
		return err
	}
	key := r.key(subscription.UserID, subscription.GroupID)
	if r.byUserGroup[key] != nil {
		return ErrSubscriptionAlreadyExists
	}
	cloned := *subscription
	cloned.ID = r.nextID
	r.nextID++
	subscription.ID = cloned.ID
	r.byUserGroup[key] = &cloned
	return nil
}

func (r *activationUserSubRepoStub) GetByID(
	_ context.Context,
	id int64,
) (*UserSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, subscription := range r.byUserGroup {
		if subscription.ID == id {
			cloned := *subscription
			return &cloned, nil
		}
	}
	return nil, ErrSubscriptionNotFound
}

func (r *activationUserSubRepoStub) subscription(userID, groupID int64) *UserSubscription {
	r.mu.Lock()
	defer r.mu.Unlock()
	subscription := r.byUserGroup[r.key(userID, groupID)]
	if subscription == nil {
		return nil
	}
	cloned := *subscription
	return &cloned
}

func newActivationServiceSQLMockClient(t *testing.T) (*dbent.Client, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, sqlMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	sqlMock.MatchExpectationsInOrder(false)
	t.Cleanup(func() { _ = sqlDB.Close() })

	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, sqlDB)))
	t.Cleanup(func() { _ = client.Close() })
	return client, sqlMock
}

func expectActivationTransaction(sqlMock sqlmock.Sqlmock, user *User, commit bool) {
	sqlMock.ExpectBegin()
	sqlMock.ExpectQuery(`SELECT .* FROM "users".* FOR UPDATE`).
		WithArgs(user.ID).
		WillReturnRows(sqlmock.NewRows(dbuser.Columns).AddRow(activationUserRow(user)...))
	if commit {
		sqlMock.ExpectCommit()
	} else {
		sqlMock.ExpectRollback()
	}
}

func expectActivationMissingUserTransaction(sqlMock sqlmock.Sqlmock, userID int64) {
	sqlMock.ExpectBegin()
	sqlMock.ExpectQuery(`SELECT .* FROM "users".* FOR UPDATE`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows(dbuser.Columns))
	sqlMock.ExpectRollback()
}

func activationUserRow(user *User) []driver.Value {
	values := make([]driver.Value, 0, len(dbuser.Columns))
	for _, column := range dbuser.Columns {
		switch column {
		case dbuser.FieldID:
			values = append(values, user.ID)
		case dbuser.FieldCreatedAt:
			values = append(values, user.CreatedAt)
		case dbuser.FieldUpdatedAt:
			values = append(values, user.CreatedAt)
		case dbuser.FieldDeletedAt:
			if user.DeletedAt == nil {
				values = append(values, nil)
			} else {
				values = append(values, *user.DeletedAt)
			}
		case dbuser.FieldEmail:
			values = append(values, user.Email)
		case dbuser.FieldPasswordHash:
			values = append(values, "test-password-hash")
		case dbuser.FieldRole:
			values = append(values, RoleUser)
		case dbuser.FieldBalance, dbuser.FieldFrozenBalance, dbuser.FieldTotalRecharged:
			values = append(values, float64(0))
		case dbuser.FieldConcurrency:
			values = append(values, int64(5))
		case dbuser.FieldStatus:
			values = append(values, StatusActive)
		case dbuser.FieldUsername, dbuser.FieldNotes:
			values = append(values, "")
		case dbuser.FieldTotpSecretEncrypted, dbuser.FieldTotpEnabledAt,
			dbuser.FieldLastLoginAt, dbuser.FieldLastActiveAt,
			dbuser.FieldBalanceNotifyThreshold:
			values = append(values, nil)
		case dbuser.FieldTotpEnabled:
			values = append(values, false)
		case dbuser.FieldSignupSource:
			values = append(values, user.SignupSource)
		case dbuser.FieldBalanceNotifyEnabled:
			values = append(values, true)
		case dbuser.FieldBalanceNotifyThresholdType:
			values = append(values, "fixed")
		case dbuser.FieldBalanceNotifyExtraEmails:
			values = append(values, "[]")
		case dbuser.FieldRpmLimit:
			values = append(values, int64(0))
		default:
			panic(fmt.Sprintf("unhandled user column %q", column))
		}
	}
	return values
}
