//go:build integration

// [INPUT]: PostgreSQL integration harness, activation repositories, and subscription service.
// [OUTPUT]: Durable concurrency and rollback proof for activation grants and recall claims.
// [POS]: Cross-repository transaction contract for the HVOY activation service.
//
// [PROTOCOL]:
// 1. Update this header when activation lock ordering or rollback guarantees change.
// 2. Keep fixtures isolated and clean committed users, groups, subscriptions, and journeys.
package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbsubscription "github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserActivationServiceConcurrentClaimPostgres(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	fixture := newActivationServicePostgresFixture(t, now.Add(-2*24*time.Hour))

	const callers = 2
	start := make(chan struct{})
	results := make(chan error, callers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(callers)
	for range callers {
		go func() {
			defer waitGroup.Done()
			<-start
			_, err := fixture.service.ClaimRecall(ctx, fixture.user.ID)
			results <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	for err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, 1, activationSubscriptionCount(
		t,
		fixture.user.ID,
		fixture.recallGroup.ID,
	))
	journey, err := fixture.journeys.GetByUserID(ctx, fixture.user.ID)
	require.NoError(t, err)
	require.Equal(t, "claimed", journey.RecallState)
	require.NotNil(t, journey.RecallSubscriptionID)
	require.NotNil(t, journey.RecallClaimedAt)
	require.NotNil(t, journey.RecallExpiresAt)
}

func TestUserActivationServiceEvaluateAndClaimDoNotOverwritePostgres(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	fixture := newActivationServicePostgresFixture(t, now.Add(-2*24*time.Hour))

	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		<-start
		_, err := fixture.service.Evaluate(ctx, fixture.user.ID, now)
		results <- err
	}()
	go func() {
		defer waitGroup.Done()
		<-start
		_, err := fixture.service.ClaimRecall(ctx, fixture.user.ID)
		results <- err
	}()
	close(start)
	waitGroup.Wait()
	close(results)

	for err := range results {
		require.NoError(t, err)
	}
	journey, err := fixture.journeys.GetByUserID(ctx, fixture.user.ID)
	require.NoError(t, err)
	require.Equal(t, "claimed", journey.RecallState)
	require.NotNil(t, journey.RecallSubscriptionID)
	require.NotNil(t, journey.LastEvaluatedAt)
	require.Equal(t, 1, activationSubscriptionCount(
		t,
		fixture.user.ID,
		fixture.recallGroup.ID,
	))
}

func TestUserActivationServiceAssignConflictRollsBackAndRetriesPostgres(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	fixture := newActivationServicePostgresFixtureWithoutStarter(t, now)

	conflicting, err := integrationEntClient.UserSubscription.Create().
		SetUserID(fixture.user.ID).
		SetGroupID(fixture.starterGroup.ID).
		SetStartsAt(now).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetNotes("conflicting-assignment").
		Save(ctx)
	require.NoError(t, err)

	err = fixture.service.BootstrapVerifiedRegistration(
		ctx,
		activationServiceUser(fixture.user),
		"hvoy_partner",
	)
	require.ErrorIs(t, err, service.ErrSubscriptionAssignConflict)
	journey, err := fixture.journeys.GetByUserID(ctx, fixture.user.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", journey.StarterState)
	require.Nil(t, journey.StarterSubscriptionID)

	_, err = integrationEntClient.UserSubscription.Delete().
		Where(dbsubscription.IDEQ(conflicting.ID)).
		Exec(ctx)
	require.NoError(t, err)

	require.NoError(t, fixture.service.BootstrapVerifiedRegistration(
		ctx,
		activationServiceUser(fixture.user),
		"hvoy_partner",
	))
	journey, err = fixture.journeys.GetByUserID(ctx, fixture.user.ID)
	require.NoError(t, err)
	require.Equal(t, "granted", journey.StarterState)
	require.NotNil(t, journey.StarterSubscriptionID)
	require.Equal(t, 1, activationSubscriptionCount(
		t,
		fixture.user.ID,
		fixture.starterGroup.ID,
	))
}

type activationServicePostgresFixture struct {
	service      *service.UserActivationService
	journeys     service.UserActivationJourneyRepository
	user         *dbent.User
	starterGroup *dbent.Group
	recallGroup  *dbent.Group
}

func newActivationServicePostgresFixture(
	t *testing.T,
	createdAt time.Time,
) *activationServicePostgresFixture {
	t.Helper()
	fixture := newActivationServicePostgresFixtureWithoutStarter(t, createdAt)
	ctx := context.Background()
	startsAt := time.Now().UTC().Add(-25 * time.Hour)
	expiresAt := startsAt.Add(24 * time.Hour)
	starter, err := integrationEntClient.UserSubscription.Create().
		SetUserID(fixture.user.ID).
		SetGroupID(fixture.starterGroup.ID).
		SetStartsAt(startsAt).
		SetExpiresAt(expiresAt).
		SetNotes("user_activation:starter").
		Save(ctx)
	require.NoError(t, err)

	journey, _, err := fixture.journeys.CreateIfAbsent(ctx, fixture.user.ID, "direct")
	require.NoError(t, err)
	journey.StarterState = "expired"
	journey.StarterSubscriptionID = &starter.ID
	journey.StarterGrantedAt = &startsAt
	journey.StarterExpiresAt = &expiresAt
	require.NoError(t, fixture.journeys.Update(ctx, journey))
	return fixture
}

func newActivationServicePostgresFixtureWithoutStarter(
	t *testing.T,
	createdAt time.Time,
) *activationServicePostgresFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	user, err := integrationEntClient.User.Create().
		SetEmail(fmt.Sprintf("activation-service-%d@example.com", suffix)).
		SetPasswordHash("test-password-hash").
		SetSignupSource("email").
		SetCreatedAt(createdAt).
		SetUpdatedAt(createdAt).
		Save(ctx)
	require.NoError(t, err)
	starterGroup := createActivationSubscriptionGroup(t, fmt.Sprintf("activation-starter-%d", suffix))
	recallGroup := createActivationSubscriptionGroup(t, fmt.Sprintf("activation-recall-%d", suffix))
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id = $1",
			user.ID,
		)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM groups WHERE id IN ($1, $2)",
			starterGroup.ID,
			recallGroup.ID,
		)
		require.NoError(t, err)
	})

	journeys := NewUserActivationJourneyRepository(integrationEntClient)
	evidence := NewUserActivationEvidenceRepository(integrationEntClient, integrationDB)
	subscriptions := service.NewSubscriptionService(
		NewGroupRepository(integrationEntClient, integrationDB),
		NewUserSubscriptionRepository(integrationEntClient),
		nil,
		integrationEntClient,
		&config.Config{},
	)
	t.Cleanup(subscriptions.Stop)
	settings := service.NewSettingService(
		&activationIntegrationSettingRepo{emailVerify: true},
		&config.Config{},
	)
	cfg := config.UserActivationConfig{
		Enabled:          true,
		EligibleAfter:    createdAt.Add(-time.Hour),
		StarterGroupID:   starterGroup.ID,
		RecallGroupID:    recallGroup.ID,
		RecallWindowDays: 7,
		SupportWeChat:    "welsir02",
	}
	return &activationServicePostgresFixture{
		service: service.NewUserActivationService(
			cfg,
			journeys,
			evidence,
			subscriptions,
			settings,
			integrationEntClient,
		),
		journeys:     journeys,
		user:         user,
		starterGroup: starterGroup,
		recallGroup:  recallGroup,
	}
}

type activationIntegrationSettingRepo struct {
	service.SettingRepository
	emailVerify bool
}

func (r *activationIntegrationSettingRepo) GetValue(
	_ context.Context,
	key string,
) (string, error) {
	if key == service.SettingKeyEmailVerifyEnabled && r.emailVerify {
		return "true", nil
	}
	return "false", nil
}

func createActivationSubscriptionGroup(t *testing.T, name string) *dbent.Group {
	t.Helper()
	group, err := integrationEntClient.Group.Create().
		SetName(name).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		Save(context.Background())
	require.NoError(t, err)
	return group
}

func activationSubscriptionCount(t *testing.T, userID, groupID int64) int {
	t.Helper()
	count, err := integrationEntClient.UserSubscription.Query().
		Where(
			dbsubscription.UserIDEQ(userID),
			dbsubscription.GroupIDEQ(groupID),
		).
		Count(context.Background())
	require.NoError(t, err)
	return count
}

func activationServiceUser(user *dbent.User) *service.User {
	return &service.User{
		ID:           user.ID,
		Email:        user.Email,
		SignupSource: user.SignupSource,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		DeletedAt:    user.DeletedAt,
	}
}
