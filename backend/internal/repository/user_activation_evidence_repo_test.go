// [INPUT]: SQL fixtures, transactional Ent clients, and activation evidence repository.
// [OUTPUT]: Proof of one-roundtrip, parameterized activation evidence semantics.
// [POS]: Persistence contract suite for the HVOY activation evidence snapshot.
//
// [PROTOCOL]:
// 1. Update this header when the evidence repository test scope changes.
// 2. Keep tests focused on auditable evidence aggregation.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestUserActivationEvidenceRepositoryConstructor(t *testing.T) {
	var _ service.UserActivationEvidenceRepository = NewUserActivationEvidenceRepository(nil, nil)
}

func TestUserActivationEvidenceSnapshotUsesOneParameterizedAggregateQuery(t *testing.T) {
	db, mock := newActivationEvidenceSQLMock(t)
	repo := NewUserActivationEvidenceRepository(nil, db)
	userID := int64(42)
	firstSuccess := time.Date(2026, 7, 28, 1, 0, 0, 0, time.UTC)
	firstPayment := firstSuccess.Add(time.Hour)
	lastAttempt := firstPayment.Add(time.Hour)
	firstAPIKey := lastAttempt.Add(time.Hour)

	mock.ExpectQuery("activation evidence snapshot").
		WithArgs(userID).
		WillReturnRows(activationEvidenceRows().
			AddRow(firstSuccess, firstPayment, lastAttempt, firstAPIKey, int64(3), int64(2)))

	snapshot, err := repo.Snapshot(context.Background(), userID)
	require.NoError(t, err)
	require.Equal(t, int64(3), snapshot.UsageCount)
	require.Equal(t, int64(2), snapshot.APIKeyCount)
	require.Equal(t, firstSuccess, *snapshot.FirstSuccessfulUsageAt)
	require.Equal(t, firstPayment, *snapshot.FirstCompletedPaymentAt)
	require.Equal(t, lastAttempt, *snapshot.LastAttemptAt)
	require.Equal(t, firstAPIKey, *snapshot.FirstAPIKeyAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserActivationEvidenceSnapshotPreservesNullEvidence(t *testing.T) {
	db, mock := newActivationEvidenceSQLMock(t)
	repo := NewUserActivationEvidenceRepository(nil, db)

	mock.ExpectQuery("activation evidence snapshot").
		WithArgs(int64(7)).
		WillReturnRows(activationEvidenceRows().
			AddRow(nil, nil, nil, nil, int64(0), int64(0)))

	snapshot, err := repo.Snapshot(context.Background(), 7)
	require.NoError(t, err)
	require.Nil(t, snapshot.FirstSuccessfulUsageAt)
	require.Nil(t, snapshot.FirstCompletedPaymentAt)
	require.Nil(t, snapshot.LastAttemptAt)
	require.Nil(t, snapshot.FirstAPIKeyAt)
	require.Zero(t, snapshot.UsageCount)
	require.Zero(t, snapshot.APIKeyCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserActivationEvidenceSnapshotAppliesBusinessEvidenceSemantics(t *testing.T) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		CREATE TABLE usage_logs (
			user_id INTEGER NOT NULL,
			actual_cost REAL NOT NULL,
			created_at TIMESTAMP NOT NULL
		);
		CREATE TABLE api_keys (
			user_id INTEGER NOT NULL,
			deleted_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL
		);
		CREATE TABLE payment_orders (
			user_id INTEGER NOT NULL,
			order_type TEXT NOT NULL,
			completed_at TIMESTAMP NULL,
			pay_amount REAL NOT NULL
		);
	`)
	require.NoError(t, err)

	userID := int64(55)
	firstAttempt := time.Date(2026, 7, 28, 1, 0, 0, 0, time.UTC)
	firstSuccess := firstAttempt.Add(2 * time.Hour)
	laterSuccess := firstSuccess.Add(30 * time.Minute)
	lastAttempt := firstAttempt.Add(4 * time.Hour)
	firstPayment := firstAttempt.Add(5 * time.Hour)
	laterPayment := firstPayment.Add(time.Hour)
	firstAPIKey := firstAttempt.Add(15 * time.Minute)

	_, err = db.Exec(`
		INSERT INTO usage_logs (user_id, actual_cost, created_at) VALUES
			(55, 0, ?),
			(55, 0.50, ?),
			(55, 0.25, ?),
			(55, 0, ?),
			(55, 0, ?),
			(56, 1, ?)
	`,
		firstAttempt,
		laterSuccess,
		firstSuccess,
		lastAttempt,
		firstAttempt.Add(time.Hour),
		firstAttempt,
	)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO api_keys (user_id, deleted_at, created_at) VALUES
			(55, NULL, ?),
			(55, NULL, ?),
			(55, ?, ?),
			(56, NULL, ?)
	`, firstAPIKey.Add(time.Hour), firstAPIKey, firstAttempt, firstAttempt.Add(-time.Hour), firstAttempt)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO payment_orders (user_id, order_type, completed_at, pay_amount) VALUES
			(55, 'balance', ?, 0),
			(55, 'subscription', ?, 10),
			(55, 'balance', NULL, 10),
			(55, 'balance', ?, 8),
			(55, 'balance', ?, 5),
			(56, 'balance', ?, 10)
	`,
		firstAttempt,
		firstAttempt,
		laterPayment,
		firstPayment,
		firstAttempt,
	)
	require.NoError(t, err)

	var firstSuccessRaw any
	var firstPaymentRaw any
	var lastAttemptRaw any
	var firstAPIKeyRaw any
	var usageCount int64
	var apiKeyCount int64
	err = db.QueryRowContext(
		context.Background(),
		userActivationEvidenceSnapshotSQL,
		userID,
	).Scan(
		&firstSuccessRaw,
		&firstPaymentRaw,
		&lastAttemptRaw,
		&firstAPIKeyRaw,
		&usageCount,
		&apiKeyCount,
	)
	require.NoError(t, err)
	require.Equal(t, int64(5), usageCount)
	require.Equal(t, int64(2), apiKeyCount)
	require.Equal(t, firstSuccess, parseSQLiteAggregateTime(t, firstSuccessRaw))
	require.Equal(t, firstPayment, parseSQLiteAggregateTime(t, firstPaymentRaw))
	require.Equal(t, lastAttempt, parseSQLiteAggregateTime(t, lastAttemptRaw))
	require.Equal(t, firstAPIKey, parseSQLiteAggregateTime(t, firstAPIKeyRaw))
}

func TestUserActivationEvidenceSnapshotPrefersTransactionExecutor(t *testing.T) {
	fallbackDB, fallbackMock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = fallbackDB.Close() })

	txDB, txMock := newActivationEvidenceSQLMock(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, txDB)))
	t.Cleanup(func() { _ = client.Close() })

	txMock.ExpectBegin()
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	txCtx := dbent.NewTxContext(context.Background(), tx)

	txMock.ExpectQuery("activation evidence snapshot").
		WithArgs(int64(99)).
		WillReturnRows(activationEvidenceRows().
			AddRow(nil, nil, nil, nil, int64(1), int64(0)))

	repo := NewUserActivationEvidenceRepository(client, fallbackDB)
	snapshot, err := repo.Snapshot(txCtx, 99)
	require.NoError(t, err)
	require.Equal(t, int64(1), snapshot.UsageCount)

	txMock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, txMock.ExpectationsWereMet())
	require.NoError(t, fallbackMock.ExpectationsWereMet())
}

func newActivationEvidenceSQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(
		sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(matchActivationEvidenceQuery)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func matchActivationEvidenceQuery(_ string, actual string) error {
	query := strings.ToLower(strings.Join(strings.Fields(actual), " "))
	required := []string{
		"usage_logs",
		"api_keys",
		"payment_orders",
		"actual_cost > 0",
		"min(created_at)",
		"min(completed_at)",
		"max(created_at)",
		"count(*)",
		"first_api_key_at",
		"deleted_at is null",
		"order_type = 'balance'",
		"completed_at is not null",
		"pay_amount > 0",
		"$1",
	}
	for _, fragment := range required {
		if !strings.Contains(query, fragment) {
			return fmt.Errorf("activation evidence query missing %q: %s", fragment, actual)
		}
	}
	if strings.Contains(query, "total_recharged") {
		return fmt.Errorf("activation evidence query must not use users.total_recharged: %s", actual)
	}
	return nil
}

func activationEvidenceRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"first_successful_usage_at",
		"first_completed_payment_at",
		"last_attempt_at",
		"first_api_key_at",
		"usage_count",
		"api_key_count",
	})
}

func parseSQLiteAggregateTime(t *testing.T, value any) time.Time {
	t.Helper()
	if parsed, ok := value.(time.Time); ok {
		return parsed
	}
	text, ok := value.(string)
	require.True(t, ok, "unexpected SQLite aggregate time type %T", value)
	parsed, err := time.Parse("2006-01-02 15:04:05 -0700 MST", text)
	require.NoError(t, err)
	return parsed
}
