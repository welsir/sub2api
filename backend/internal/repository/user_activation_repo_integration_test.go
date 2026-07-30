//go:build integration

// [INPUT]: PostgreSQL integration harness, Ent transactions, and activation journey repository.
// [OUTPUT]: Durable proof of transaction-safe and multi-connection idempotent journey creation.
// [POS]: PostgreSQL concurrency contract for HVOY activation journey persistence.
//
// [PROTOCOL]:
// 1. Update this header when PostgreSQL activation concurrency guarantees change.
// 2. Keep fixtures isolated and clean committed rows after each test.
package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserActivationJourneyCreateIfAbsentPostgres(t *testing.T) {
	t.Run("repeated insert keeps outer transaction usable", func(t *testing.T) {
		tx := testEntTx(t)
		txCtx := dbent.NewTxContext(context.Background(), tx)
		client := tx.Client()
		user := createActivationPostgresUser(t, txCtx, client, "transaction")
		repo := NewUserActivationJourneyRepository(integrationEntClient)

		first, created, err := repo.CreateIfAbsent(txCtx, user.ID, "hvoy_partner")
		require.NoError(t, err)
		require.True(t, created)

		second, created, err := repo.CreateIfAbsent(txCtx, user.ID, "ignored_second_source")
		require.NoError(t, err)
		require.False(t, created)
		require.Equal(t, first.ID, second.ID)
		require.Equal(t, "hvoy_partner", second.CampaignSource)

		loaded, err := repo.GetByUserID(txCtx, user.ID)
		require.NoError(t, err)
		loaded.StarterState = "expired"
		require.NoError(t, repo.Update(txCtx, loaded))

		updated, err := repo.GetByUserID(txCtx, user.ID)
		require.NoError(t, err)
		require.Equal(t, "expired", updated.StarterState)
	})

	t.Run("concurrent connections create exactly one journey", func(t *testing.T) {
		ctx := context.Background()
		user := createActivationPostgresUser(t, ctx, integrationEntClient, "concurrent")
		t.Cleanup(func() {
			_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
			require.NoError(t, err)
		})
		repo := NewUserActivationJourneyRepository(integrationEntClient)

		const callers = 16
		start := make(chan struct{})
		results := make(chan activationCreateResult, callers)
		var waitGroup sync.WaitGroup
		waitGroup.Add(callers)
		for range callers {
			go func() {
				defer waitGroup.Done()
				<-start
				journey, created, err := repo.CreateIfAbsent(ctx, user.ID, "hvoy_partner")
				results <- activationCreateResult{journey: journey, created: created, err: err}
			}()
		}
		close(start)
		waitGroup.Wait()
		close(results)

		createdCount := 0
		var journeyID int64
		for result := range results {
			require.NoError(t, result.err)
			require.NotNil(t, result.journey)
			if journeyID == 0 {
				journeyID = result.journey.ID
			}
			require.Equal(t, journeyID, result.journey.ID)
			if result.created {
				createdCount++
			}
		}
		require.Equal(t, 1, createdCount)

		var userJourneyCount int
		require.NoError(t, integrationDB.QueryRowContext(
			ctx,
			"SELECT COUNT(*) FROM user_activation_journeys WHERE user_id = $1",
			user.ID,
		).Scan(&userJourneyCount))
		require.Equal(t, 1, userJourneyCount)
	})
}

type activationCreateResult struct {
	journey *service.UserActivationJourney
	created bool
	err     error
}

func createActivationPostgresUser(
	t *testing.T,
	ctx context.Context,
	client *dbent.Client,
	suffix string,
) *dbent.User {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(fmt.Sprintf("activation-%s-%d@example.com", suffix, time.Now().UnixNano())).
		SetPasswordHash("test-password-hash").
		SetSignupSource("email").
		Save(ctx)
	require.NoError(t, err)
	return user
}
