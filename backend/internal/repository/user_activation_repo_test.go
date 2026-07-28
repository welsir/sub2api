// [INPUT]: Ent test databases, activation journey repository, and service contract.
// [OUTPUT]: Behavioral proof for journey creation, locking, updates, and due-user scans.
// [POS]: Persistence contract suite for the HVOY activation journey repository.
//
// [PROTOCOL]:
// 1. Update this header when the journey repository test scope changes.
// 2. Keep tests focused on persistence behavior rather than worker policy.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	dbjourney "github.com/Wei-Shaw/sub2api/ent/useractivationjourney"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestUserActivationJourneyRepositoryConstructor(t *testing.T) {
	var _ service.UserActivationJourneyRepository = NewUserActivationJourneyRepository(nil)
}

func TestUserActivationJourneyCreateIfAbsentIsConcurrentAndIdempotent(t *testing.T) {
	client := newUserActivationEntClient(t)
	user := createActivationTestUser(t, client, "concurrent@example.com", "email", time.Now().UTC(), nil)
	repo := NewUserActivationJourneyRepository(client)

	const callers = 12
	var wg sync.WaitGroup
	wg.Add(callers)
	createdResults := make(chan bool, callers)
	errorResults := make(chan error, callers)
	for range callers {
		go func() {
			defer wg.Done()
			journey, created, err := repo.CreateIfAbsent(context.Background(), user.ID, "hvoy_partner")
			if err == nil {
				require.Equal(t, user.ID, journey.UserID)
			}
			createdResults <- created
			errorResults <- err
		}()
	}
	wg.Wait()
	close(createdResults)
	close(errorResults)

	createdCount := 0
	for created := range createdResults {
		if created {
			createdCount++
		}
	}
	for err := range errorResults {
		require.NoError(t, err)
	}
	require.Equal(t, 1, createdCount)
	require.Equal(t, 1, client.UserActivationJourney.Query().CountX(context.Background()))

	stored, err := repo.GetByUserID(context.Background(), user.ID)
	require.NoError(t, err)
	require.Equal(t, "hvoy_partner", stored.CampaignSource)
	require.Equal(t, "pending", stored.StarterState)
	require.Equal(t, "locked", stored.RecallState)
}

func TestUserActivationJourneyGetAndNotFound(t *testing.T) {
	client := newUserActivationEntClient(t)
	user := createActivationTestUser(t, client, "get@example.com", "email", time.Now().UTC(), nil)
	repo := NewUserActivationJourneyRepository(client)

	created, wasCreated, err := repo.CreateIfAbsent(context.Background(), user.ID, "direct")
	require.NoError(t, err)
	require.True(t, wasCreated)

	got, err := repo.GetByUserID(context.Background(), user.ID)
	require.NoError(t, err)
	requireActivationJourneysEqual(t, created, got)

	_, err = repo.GetByUserID(context.Background(), user.ID+100)
	require.ErrorIs(t, err, service.ErrUserActivationJourneyNotFound)
}

func TestUserActivationJourneyCreateIfAbsentDoesNotSwallowNonUniqueErrors(t *testing.T) {
	client := newUserActivationEntClient(t)
	repo := NewUserActivationJourneyRepository(client)

	journey, created, err := repo.CreateIfAbsent(context.Background(), 999, "direct")
	require.Error(t, err)
	require.Nil(t, journey)
	require.False(t, created)
}

func TestUserActivationJourneyCreateIfAbsentDoesNotTreatDuplicateTextAsJourneyConflict(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	duplicateTextError := errors.New("duplicate key value violates unique constraint unrelated_table_key")
	mock.ExpectQuery(`INSERT INTO "user_activation_journeys"`).
		WillReturnError(duplicateTextError)

	repo := NewUserActivationJourneyRepository(client)
	journey, created, err := repo.CreateIfAbsent(context.Background(), 73, "direct")
	require.ErrorIs(t, err, duplicateTextError)
	require.Nil(t, journey)
	require.False(t, created)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserActivationJourneyGetForUpdateRequiresTransactionWithoutQuerying(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	repo := NewUserActivationJourneyRepository(client)
	journey, err := repo.GetByUserIDForUpdate(context.Background(), 91)
	require.ErrorIs(t, err, service.ErrUserActivationJourneyTransactionRequired)
	require.Nil(t, journey)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserActivationJourneyGetForUpdateUsesTransactionClientAndRowLock(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	txCtx := dbent.NewTxContext(context.Background(), tx)

	mock.ExpectQuery(`SELECT .* FROM "user_activation_journeys".* FOR UPDATE`).
		WithArgs(int64(91)).
		WillReturnRows(sqlmock.NewRows(dbjourney.Columns))

	repo := NewUserActivationJourneyRepository(nil)
	_, err = repo.GetByUserIDForUpdate(txCtx, 91)
	require.ErrorIs(t, err, service.ErrUserActivationJourneyNotFound)

	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserActivationJourneyUpdateSetsAndClearsNullableFields(t *testing.T) {
	client := newUserActivationEntClient(t)
	user := createActivationTestUser(t, client, "update@example.com", "email", time.Now().UTC(), nil)
	repo := NewUserActivationJourneyRepository(client)
	journey, _, err := repo.CreateIfAbsent(context.Background(), user.ID, "direct")
	require.NoError(t, err)
	otherUser := createActivationTestUser(t, client, "update-other@example.com", "email", time.Now().UTC(), nil)
	originalUserID := journey.UserID
	originalCampaignSource := journey.CampaignSource

	base := time.Date(2026, 7, 28, 1, 2, 3, 0, time.UTC)
	group, err := client.Group.Create().SetName("activation-update-group").Save(context.Background())
	require.NoError(t, err)
	starterSubscription, err := client.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetStartsAt(base).
		SetExpiresAt(base.Add(24 * time.Hour)).
		Save(context.Background())
	require.NoError(t, err)
	recallSubscription, err := client.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetStartsAt(base.Add(time.Hour)).
		SetExpiresAt(base.Add(25 * time.Hour)).
		Save(context.Background())
	require.NoError(t, err)

	journey.UserID = otherUser.ID
	journey.CampaignSource = "hacked_source"
	journey.StarterState = "granted"
	journey.StarterSubscriptionID = &starterSubscription.ID
	journey.StarterGrantedAt = timePtr(base)
	journey.StarterExpiresAt = timePtr(base.Add(24 * time.Hour))
	journey.RecallState = "claimed"
	journey.RecallSubscriptionID = &recallSubscription.ID
	journey.RecallClaimedAt = timePtr(base.Add(time.Hour))
	journey.RecallExpiresAt = timePtr(base.Add(25 * time.Hour))
	journey.FirstSuccessAt = timePtr(base.Add(2 * time.Hour))
	journey.NoAttemptEmailSentAt = timePtr(base.Add(3 * time.Hour))
	journey.AttemptedEmailSentAt = timePtr(base.Add(4 * time.Hour))
	journey.PaidSupportEmailSentAt = timePtr(base.Add(5 * time.Hour))
	journey.RecallAvailableEmailSentAt = timePtr(base.Add(6 * time.Hour))
	journey.RecallExpiredEmailSentAt = timePtr(base.Add(7 * time.Hour))
	journey.LastEmailSentAt = timePtr(base.Add(8 * time.Hour))
	journey.LastEvaluatedAt = timePtr(base.Add(9 * time.Hour))

	require.NoError(t, repo.Update(context.Background(), journey))
	require.Equal(t, originalUserID, journey.UserID)
	require.Equal(t, originalCampaignSource, journey.CampaignSource)

	stored, err := repo.GetByUserID(context.Background(), originalUserID)
	require.NoError(t, err)
	requireActivationJourneysEqual(t, journey, stored)

	journey.StarterSubscriptionID = nil
	journey.StarterGrantedAt = nil
	journey.StarterExpiresAt = nil
	journey.RecallSubscriptionID = nil
	journey.RecallClaimedAt = nil
	journey.RecallExpiresAt = nil
	journey.FirstSuccessAt = nil
	journey.NoAttemptEmailSentAt = nil
	journey.AttemptedEmailSentAt = nil
	journey.PaidSupportEmailSentAt = nil
	journey.RecallAvailableEmailSentAt = nil
	journey.RecallExpiredEmailSentAt = nil
	journey.LastEmailSentAt = nil
	journey.LastEvaluatedAt = nil

	require.NoError(t, repo.Update(context.Background(), journey))
	stored, err = repo.GetByUserID(context.Background(), user.ID)
	require.NoError(t, err)
	requireActivationJourneysEqual(t, journey, stored)

	missing := *journey
	missing.ID += 999
	require.ErrorIs(t, repo.Update(context.Background(), &missing), service.ErrUserActivationJourneyNotFound)
}

func TestUserActivationJourneyListDueUsesNullFirstStableOrderingAndLimit(t *testing.T) {
	client := newUserActivationEntClient(t)
	repo := NewUserActivationJourneyRepository(client)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	nullJourney := createActivationJourneyForTest(t, client, repo, "due-null@example.com", nil)
	oldestJourney := createActivationJourneyForTest(t, client, repo, "due-oldest@example.com", timePtr(now.Add(-2*time.Hour)))
	sameTimeFirst := createActivationJourneyForTest(t, client, repo, "due-same-1@example.com", timePtr(now.Add(-time.Hour)))
	sameTimeSecond := createActivationJourneyForTest(t, client, repo, "due-same-2@example.com", timePtr(now.Add(-time.Hour)))
	futureJourney := createActivationJourneyForTest(t, client, repo, "due-future@example.com", timePtr(now.Add(time.Hour)))

	oldestJourney.RecallState = "closed_success"
	require.NoError(t, repo.Update(context.Background(), oldestJourney))

	due, err := repo.ListDue(context.Background(), now, 4)
	require.NoError(t, err)
	require.Equal(t, []int64{nullJourney.ID, oldestJourney.ID, sameTimeFirst.ID, sameTimeSecond.ID}, journeyIDs(due))
	require.NotContains(t, journeyIDs(due), futureJourney.ID)

	due, err = repo.ListDue(context.Background(), now, 2)
	require.NoError(t, err)
	require.Equal(t, []int64{nullJourney.ID, oldestJourney.ID}, journeyIDs(due))
}

func TestUserActivationJourneyListEligibleUsersWithoutJourneyFiltersAndOrders(t *testing.T) {
	client := newUserActivationEntClient(t)
	repo := NewUserActivationJourneyRepository(client)
	eligibleAfter := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	first := createActivationTestUser(t, client, "eligible-1@example.com", "email", eligibleAfter, nil)
	second := createActivationTestUser(t, client, "eligible-2@example.com", "email", eligibleAfter, nil)
	_ = createActivationTestUser(t, client, "too-old@example.com", "email", eligibleAfter.Add(-time.Second), nil)
	_ = createActivationTestUser(t, client, "oauth@example.com", "google", eligibleAfter, nil)
	deletedAt := eligibleAfter.Add(time.Hour)
	_ = createActivationTestUser(t, client, "deleted@example.com", "email", eligibleAfter, &deletedAt)
	withJourney := createActivationTestUser(t, client, "has-journey@example.com", "email", eligibleAfter, nil)
	_, _, err := repo.CreateIfAbsent(context.Background(), withJourney.ID, "unknown")
	require.NoError(t, err)

	users, err := repo.ListEligibleUsersWithoutJourney(context.Background(), eligibleAfter, 1)
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID}, userIDs(users))

	users, err = repo.ListEligibleUsersWithoutJourney(context.Background(), eligibleAfter, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID, second.ID}, userIDs(users))
	require.Equal(t, "eligible-1@example.com", users[0].Email)
}

func newUserActivationEntClient(t *testing.T) *dbent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1&_pragma=busy_timeout(5000)", t.Name())
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	client := enttest.NewClient(
		t,
		enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))),
	)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func createActivationTestUser(
	t *testing.T,
	client *dbent.Client,
	email string,
	source string,
	createdAt time.Time,
	deletedAt *time.Time,
) *dbent.User {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		SetSignupSource(source).
		SetCreatedAt(createdAt).
		SetUpdatedAt(createdAt).
		SetNillableDeletedAt(deletedAt).
		Save(context.Background())
	require.NoError(t, err)
	return user
}

func createActivationJourneyForTest(
	t *testing.T,
	client *dbent.Client,
	repo service.UserActivationJourneyRepository,
	email string,
	lastEvaluatedAt *time.Time,
) *service.UserActivationJourney {
	t.Helper()
	user := createActivationTestUser(t, client, email, "email", time.Now().UTC(), nil)
	journey, _, err := repo.CreateIfAbsent(context.Background(), user.ID, "direct")
	require.NoError(t, err)
	journey.LastEvaluatedAt = lastEvaluatedAt
	require.NoError(t, repo.Update(context.Background(), journey))
	return journey
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func journeyIDs(journeys []service.UserActivationJourney) []int64 {
	ids := make([]int64, 0, len(journeys))
	for _, journey := range journeys {
		ids = append(ids, journey.ID)
	}
	return ids
}

func userIDs(users []service.User) []int64 {
	ids := make([]int64, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	return ids
}

func requireActivationJourneysEqual(
	t *testing.T,
	expected *service.UserActivationJourney,
	actual *service.UserActivationJourney,
) {
	t.Helper()
	require.True(t, expected.CreatedAt.Equal(actual.CreatedAt))
	require.True(t, expected.UpdatedAt.Equal(actual.UpdatedAt))

	expectedCopy := *expected
	actualCopy := *actual
	expectedCopy.CreatedAt = time.Time{}
	expectedCopy.UpdatedAt = time.Time{}
	actualCopy.CreatedAt = time.Time{}
	actualCopy.UpdatedAt = time.Time{}
	require.Equal(t, expectedCopy, actualCopy)
}
