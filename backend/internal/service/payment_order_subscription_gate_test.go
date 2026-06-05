package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type paymentOrderSubscriptionGateRepo struct {
	active []UserSubscription
	err    error
	calls  int
}

func (r *paymentOrderSubscriptionGateRepo) Create(context.Context, *UserSubscription) error {
	panic("unexpected Create call")
}
func (r *paymentOrderSubscriptionGateRepo) GetByID(context.Context, int64) (*UserSubscription, error) {
	panic("unexpected GetByID call")
}
func (r *paymentOrderSubscriptionGateRepo) GetByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	panic("unexpected GetByUserIDAndGroupID call")
}
func (r *paymentOrderSubscriptionGateRepo) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	panic("unexpected GetActiveByUserIDAndGroupID call")
}
func (r *paymentOrderSubscriptionGateRepo) Update(context.Context, *UserSubscription) error {
	panic("unexpected Update call")
}
func (r *paymentOrderSubscriptionGateRepo) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (r *paymentOrderSubscriptionGateRepo) ListByUserID(context.Context, int64) ([]UserSubscription, error) {
	panic("unexpected ListByUserID call")
}
func (r *paymentOrderSubscriptionGateRepo) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return r.active, nil
}
func (r *paymentOrderSubscriptionGateRepo) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}
func (r *paymentOrderSubscriptionGateRepo) List(context.Context, pagination.PaginationParams, *int64, *int64, string, string, string, string) ([]UserSubscription, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (r *paymentOrderSubscriptionGateRepo) ExistsByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	panic("unexpected ExistsByUserIDAndGroupID call")
}
func (r *paymentOrderSubscriptionGateRepo) ExtendExpiry(context.Context, int64, time.Time) error {
	panic("unexpected ExtendExpiry call")
}
func (r *paymentOrderSubscriptionGateRepo) UpdateStatus(context.Context, int64, string) error {
	panic("unexpected UpdateStatus call")
}
func (r *paymentOrderSubscriptionGateRepo) UpdateNotes(context.Context, int64, string) error {
	panic("unexpected UpdateNotes call")
}
func (r *paymentOrderSubscriptionGateRepo) ActivateWindows(context.Context, int64, time.Time) error {
	panic("unexpected ActivateWindows call")
}
func (r *paymentOrderSubscriptionGateRepo) ResetDailyUsage(context.Context, int64, time.Time) error {
	panic("unexpected ResetDailyUsage call")
}
func (r *paymentOrderSubscriptionGateRepo) ResetWeeklyUsage(context.Context, int64, time.Time) error {
	panic("unexpected ResetWeeklyUsage call")
}
func (r *paymentOrderSubscriptionGateRepo) ResetMonthlyUsage(context.Context, int64, time.Time) error {
	panic("unexpected ResetMonthlyUsage call")
}
func (r *paymentOrderSubscriptionGateRepo) IncrementUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementUsage call")
}
func (r *paymentOrderSubscriptionGateRepo) BatchUpdateExpiredStatus(context.Context) (int64, error) {
	panic("unexpected BatchUpdateExpiredStatus call")
}

func TestPaymentOrderBalanceRequiresActiveSubscriptionWhenConfigured(t *testing.T) {
	t.Parallel()

	repo := &paymentOrderSubscriptionGateRepo{}
	svc := &PaymentService{
		subscriptionSvc: NewSubscriptionService(nil, repo, nil, nil, nil),
	}

	_, err := svc.validateOrderInput(context.Background(), CreateOrderRequest{
		UserID:    7,
		OrderType: payment.OrderTypeBalance,
		Amount:    30,
	}, &PaymentConfig{
		Enabled:                           true,
		BalanceRequiresActiveSubscription: true,
	})

	if err == nil {
		t.Fatal("expected balance order without active subscription to fail")
	}
	if repo.calls != 1 {
		t.Fatalf("ListActiveByUserID calls = %d, want 1", repo.calls)
	}
}

func TestPaymentOrderBalanceAllowedForActiveSubscriber(t *testing.T) {
	t.Parallel()

	repo := &paymentOrderSubscriptionGateRepo{
		active: []UserSubscription{{ID: 1, UserID: 7, GroupID: 2, Status: SubscriptionStatusActive}},
	}
	svc := &PaymentService{
		subscriptionSvc: NewSubscriptionService(nil, repo, nil, nil, nil),
	}

	_, err := svc.validateOrderInput(context.Background(), CreateOrderRequest{
		UserID:    7,
		OrderType: payment.OrderTypeBalance,
		Amount:    30,
	}, &PaymentConfig{
		Enabled:                           true,
		BalanceRequiresActiveSubscription: true,
	})
	if err != nil {
		t.Fatalf("expected active subscriber to create balance order, got %v", err)
	}
}
