package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type subscriptionReservationState struct {
	dailyUsage   float64
	weeklyUsage  float64
	monthlyUsage float64
	dailyLimit   sql.NullFloat64
	weeklyLimit  sql.NullFloat64
	monthlyLimit sql.NullFloat64
}

func (r *usageBillingRepository) ReserveUsage(ctx context.Context, req *service.UsageReservationRequest) (_ *service.UsageReservation, err error) {
	if req == nil || req.AmountUSD <= 0 {
		return &service.UsageReservation{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	reservation := &service.UsageReservation{
		UserID:         req.UserID,
		GroupID:        cloneInt64Ptr(req.GroupID),
		SubscriptionID: cloneInt64Ptr(req.SubscriptionID),
		AmountUSD:      req.AmountUSD,
	}

	remaining := req.AmountUSD
	if req.SubscriptionID != nil {
		subRemaining, err := selectSubscriptionReservationRemaining(ctx, tx, *req.SubscriptionID, req.UserID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil && subRemaining > 0 {
			subAmount := math.Min(remaining, subRemaining)
			if err := incrementUsageBillingSubscription(ctx, tx, *req.SubscriptionID, subAmount); err != nil {
				return nil, err
			}
			reservation.SubscriptionAmountUSD = subAmount
			remaining = normalizeReservationAmount(remaining - subAmount)
		}
	}

	if remaining > 1e-9 {
		newBalance, err := reserveUsageBalance(ctx, tx, req.UserID, remaining)
		if err != nil {
			return nil, err
		}
		_ = newBalance
		reservation.BalanceAmountUSD = remaining
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return reservation, nil
}

func (r *usageBillingRepository) SettleUsageReservation(ctx context.Context, reservation *service.UsageReservation, actualCostUSD float64) (*service.UsageBillingApplyResult, error) {
	if reservation == nil || reservation.AmountUSD <= 0 {
		return &service.UsageBillingApplyResult{Applied: true}, nil
	}
	if actualCostUSD < 0 {
		actualCostUSD = 0
	}
	if actualCostUSD > reservation.AmountUSD+1e-9 {
		return nil, service.ErrUsageReservationExceeded
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := settleUsageReservationTx(ctx, tx, reservation, actualCostUSD)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func settleUsageReservationTx(ctx context.Context, tx *sql.Tx, reservation *service.UsageReservation, actualCostUSD float64) (*service.UsageBillingApplyResult, error) {
	result := &service.UsageBillingApplyResult{Applied: true}
	finalSub := math.Min(actualCostUSD, reservation.SubscriptionAmountUSD)
	finalBalance := actualCostUSD - finalSub
	if finalBalance < 0 {
		finalBalance = 0
	}

	if reservation.SubscriptionID != nil {
		refundSub := reservation.SubscriptionAmountUSD - finalSub
		if refundSub > 1e-9 {
			if err := incrementUsageBillingSubscription(ctx, tx, *reservation.SubscriptionID, -refundSub); err != nil {
				return nil, err
			}
		}
	}

	refundBalance := reservation.BalanceAmountUSD - finalBalance
	if refundBalance > 1e-9 {
		newBalance, err := refundUsageBalance(ctx, tx, reservation.UserID, refundBalance)
		if err != nil {
			return nil, err
		}
		result.NewBalance = &newBalance
	}
	return result, nil
}

func (r *usageBillingRepository) ReleaseUsageReservation(ctx context.Context, reservation *service.UsageReservation) error {
	_, err := r.SettleUsageReservation(ctx, reservation, 0)
	return err
}

func selectSubscriptionReservationRemaining(ctx context.Context, tx *sql.Tx, subscriptionID, userID int64) (float64, error) {
	var state subscriptionReservationState
	err := tx.QueryRowContext(ctx, `
		SELECT
			us.daily_usage_usd,
			us.weekly_usage_usd,
			us.monthly_usage_usd,
			g.daily_limit_usd,
			g.weekly_limit_usd,
			g.monthly_limit_usd
		FROM user_subscriptions us
		JOIN groups g ON g.id = us.group_id AND g.deleted_at IS NULL
		WHERE us.id = $1
			AND us.user_id = $2
			AND us.deleted_at IS NULL
			AND us.status = 'active'
			AND us.expires_at > NOW()
		FOR UPDATE
	`, subscriptionID, userID).Scan(
		&state.dailyUsage, &state.weeklyUsage, &state.monthlyUsage,
		&state.dailyLimit, &state.weeklyLimit, &state.monthlyLimit,
	)
	if err != nil {
		return 0, err
	}
	return state.remaining(), nil
}

func (s subscriptionReservationState) remaining() float64 {
	remaining := math.Inf(1)
	hasLimit := false
	for _, dim := range []struct {
		usage float64
		limit sql.NullFloat64
	}{
		{s.dailyUsage, s.dailyLimit},
		{s.weeklyUsage, s.weeklyLimit},
		{s.monthlyUsage, s.monthlyLimit},
	} {
		if dim.limit.Valid && dim.limit.Float64 > 0 {
			hasLimit = true
			available := dim.limit.Float64 - dim.usage
			if available < remaining {
				remaining = available
			}
		}
	}
	if !hasLimit {
		return math.Inf(1)
	}
	if remaining < 0 {
		return 0
	}
	return remaining
}

func reserveUsageBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2
			AND deleted_at IS NULL
			AND balance >= $1
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, service.ErrUsageReservationInsufficientFunds
	}
	return newBalance, err
}

func refundUsageBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance + $1,
			updated_at = NOW()
		WHERE id = $2
			AND deleted_at IS NULL
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	return newBalance, err
}

func cloneInt64Ptr(v *int64) *int64 {
	if v == nil {
		return nil
	}
	cloned := *v
	return &cloned
}

func normalizeReservationAmount(v float64) float64 {
	return math.Round(v*1e9) / 1e9
}
