package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUsageBillingRepositoryReserveUsage_BalanceRequiresSufficientFunds(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := &usageBillingRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE users")).
		WithArgs(0.51, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(1.49))
	mock.ExpectCommit()

	reservation, err := repo.ReserveUsage(context.Background(), &service.UsageReservationRequest{
		UserID:    7,
		AmountUSD: 0.51,
	})
	if err != nil {
		t.Fatalf("ReserveUsage returned error: %v", err)
	}
	if reservation.BalanceAmountUSD != 0.51 || reservation.SubscriptionAmountUSD != 0 {
		t.Fatalf("reservation split = balance %.2f subscription %.2f, want balance 0.51 subscription 0", reservation.BalanceAmountUSD, reservation.SubscriptionAmountUSD)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUsageBillingRepositoryReserveUsage_SubscriptionThenBalanceFallback(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := &usageBillingRepository{db: db}
	subID := int64(99)
	groupID := int64(5)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs(subID, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"daily_usage_usd", "weekly_usage_usd", "monthly_usage_usd",
			"daily_limit_usd", "weekly_limit_usd", "monthly_limit_usd",
		}).AddRow(0.0, 0.0, 9.0, nil, nil, 10.0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE user_subscriptions")).
		WithArgs(1.0, subID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE users")).
		WithArgs(0.35, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(2.65))
	mock.ExpectCommit()

	reservation, err := repo.ReserveUsage(context.Background(), &service.UsageReservationRequest{
		UserID:         7,
		GroupID:        &groupID,
		SubscriptionID: &subID,
		AmountUSD:      1.35,
	})
	if err != nil {
		t.Fatalf("ReserveUsage returned error: %v", err)
	}
	if reservation.SubscriptionAmountUSD != 1.0 || reservation.BalanceAmountUSD != 0.35 {
		t.Fatalf("reservation split = subscription %.2f balance %.2f, want subscription 1.00 balance 0.35", reservation.SubscriptionAmountUSD, reservation.BalanceAmountUSD)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
