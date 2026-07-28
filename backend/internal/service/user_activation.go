// [INPUT]: Context, user activation lifecycle state, and observed account evidence.
// [OUTPUT]: Domain records and repository ports for the HVOY activation workflow.
// [POS]: Service-layer contract separating activation policy from persistence.
//
// [PROTOCOL]:
// 1. Update this header when activation domain fields or repository contracts change.
// 2. Keep persistence details and benefit-granting behavior outside this file.
package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrUserActivationJourneyNotFound = infraerrors.NotFound(
	"USER_ACTIVATION_JOURNEY_NOT_FOUND",
	"user activation journey not found",
)

type UserActivationJourney struct {
	ID                         int64
	UserID                     int64
	CampaignSource             string
	StarterState               string
	StarterSubscriptionID      *int64
	StarterGrantedAt           *time.Time
	StarterExpiresAt           *time.Time
	RecallState                string
	RecallSubscriptionID       *int64
	RecallClaimedAt            *time.Time
	RecallExpiresAt            *time.Time
	FirstSuccessAt             *time.Time
	NoAttemptEmailSentAt       *time.Time
	AttemptedEmailSentAt       *time.Time
	PaidSupportEmailSentAt     *time.Time
	RecallAvailableEmailSentAt *time.Time
	RecallExpiredEmailSentAt   *time.Time
	LastEmailSentAt            *time.Time
	LastEvaluatedAt            *time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type UserActivationEvidence struct {
	FirstSuccessfulUsageAt  *time.Time
	FirstCompletedPaymentAt *time.Time
	LastAttemptAt           *time.Time
	UsageCount              int64
	APIKeyCount             int64
}

type UserActivationJourneyRepository interface {
	CreateIfAbsent(ctx context.Context, userID int64, source string) (*UserActivationJourney, bool, error)
	GetByUserID(ctx context.Context, userID int64) (*UserActivationJourney, error)
	GetByUserIDForUpdate(ctx context.Context, userID int64) (*UserActivationJourney, error)
	Update(ctx context.Context, journey *UserActivationJourney) error
	ListDue(ctx context.Context, now time.Time, limit int) ([]UserActivationJourney, error)
	ListEligibleUsersWithoutJourney(ctx context.Context, eligibleAfter time.Time, limit int) ([]User, error)
}

type UserActivationEvidenceRepository interface {
	Snapshot(ctx context.Context, userID int64) (*UserActivationEvidence, error)
}
