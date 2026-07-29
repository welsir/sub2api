// [INPUT]: Ent client, SQL database, and activation evidence service contract.
// [OUTPUT]: Activation evidence repository constructor.
// [POS]: Read-only persistence adapter for HVOY activation evidence snapshots.
//
// [PROTOCOL]:
// 1. Update this header when evidence sources or aggregation semantics change.
// 2. Keep activation policy and user balance fields outside this adapter.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const userActivationEvidenceSnapshotSQL = `
SELECT
	(
		SELECT MIN(created_at)
		FROM usage_logs
		WHERE user_id = $1 AND actual_cost > 0
	) AS first_successful_usage_at,
	(
		SELECT MIN(completed_at)
		FROM payment_orders
		WHERE user_id = $1
		  AND order_type = 'balance'
		  AND completed_at IS NOT NULL
		  AND pay_amount > 0
	) AS first_completed_payment_at,
	(
		SELECT MAX(created_at)
		FROM usage_logs
		WHERE user_id = $1
	) AS last_attempt_at,
	(
		SELECT MIN(created_at)
		FROM api_keys
		WHERE user_id = $1 AND deleted_at IS NULL
	) AS first_api_key_at,
	(
		SELECT COUNT(*)
		FROM usage_logs
		WHERE user_id = $1
	) AS usage_count,
	(
		SELECT COUNT(*)
		FROM api_keys
		WHERE user_id = $1 AND deleted_at IS NULL
	) AS api_key_count
`

type userActivationEvidenceRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

func NewUserActivationEvidenceRepository(
	client *dbent.Client,
	sqlDB *sql.DB,
) service.UserActivationEvidenceRepository {
	return &userActivationEvidenceRepository{client: client, sql: sqlDB}
}

func (r *userActivationEvidenceRepository) Snapshot(
	ctx context.Context,
	userID int64,
) (*service.UserActivationEvidence, error) {
	executor := txAwareSQLExecutor(ctx, r.sql, r.client)
	if executor == nil {
		return nil, errors.New("activation evidence SQL executor is not configured")
	}

	rows, err := executor.QueryContext(ctx, userActivationEvidenceSnapshotSQL, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}

	var firstSuccessfulUsageAt sql.NullTime
	var firstCompletedPaymentAt sql.NullTime
	var lastAttemptAt sql.NullTime
	var firstAPIKeyAt sql.NullTime
	evidence := &service.UserActivationEvidence{}
	if err := rows.Scan(
		&firstSuccessfulUsageAt,
		&firstCompletedPaymentAt,
		&lastAttemptAt,
		&firstAPIKeyAt,
		&evidence.UsageCount,
		&evidence.APIKeyCount,
	); err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, fmt.Errorf("activation evidence query returned multiple rows for user %d", userID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	evidence.FirstSuccessfulUsageAt = nullTimePointer(firstSuccessfulUsageAt)
	evidence.FirstCompletedPaymentAt = nullTimePointer(firstCompletedPaymentAt)
	evidence.LastAttemptAt = nullTimePointer(lastAttemptAt)
	evidence.FirstAPIKeyAt = nullTimePointer(firstAPIKeyAt)
	return evidence, nil
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
