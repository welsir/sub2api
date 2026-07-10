package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// GetWorkingDirSpending aggregates actual cost by user and client cwd.
func (r *usageLogRepository) GetWorkingDirSpending(ctx context.Context, startTime, endTime time.Time, userID int64, limit int) (items []usagestats.WorkingDirSpendingItem, err error) {
	if limit <= 0 {
		limit = 100
	}

	conditions := []string{"u.created_at >= $1", "u.created_at < $2"}
	args := []any{startTime, endTime}
	if userID > 0 {
		conditions = append(conditions, fmt.Sprintf("u.user_id = $%d", len(args)+1))
		args = append(args, userID)
	}
	limitPosition := len(args) + 1
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT
			u.user_id,
			COALESCE(users.email, '') AS email,
			COALESCE(u.working_directory, '') AS working_directory,
			COALESCE(SUM(u.actual_cost), 0) AS actual_cost,
			COUNT(*) AS requests
		FROM usage_logs u
		LEFT JOIN users ON u.user_id = users.id
		WHERE %s
		GROUP BY u.user_id, users.email, u.working_directory
		ORDER BY actual_cost DESC, requests DESC, u.user_id ASC
		LIMIT $%d
	`, strings.Join(conditions, " AND "), limitPosition)

	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			items = nil
		}
	}()

	items = make([]usagestats.WorkingDirSpendingItem, 0)
	for rows.Next() {
		var item usagestats.WorkingDirSpendingItem
		if err = rows.Scan(&item.UserID, &item.Email, &item.WorkingDirectory, &item.ActualCost, &item.Requests); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
