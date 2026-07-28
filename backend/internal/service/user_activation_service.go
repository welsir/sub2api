// [INPUT]: Activation configuration, journey/evidence ports, subscriptions, settings, and Ent.
// [OUTPUT]: Starter grants, evidence-derived status, and transactional recall claims.
// [POS]: Service-layer policy owner for the HVOY new-user activation lifecycle.
//
// [PROTOCOL]:
// 1. Update this header when activation eligibility, locking, or grant semantics change.
// 2. Keep email delivery, worker scheduling, HTTP handling, and auth integration outside this file.
package service

import (
	"context"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	UserActivationSegmentSuccess            = "SUCCESS"
	UserActivationSegmentPaidZeroSuccess    = "PAID_ZERO_SUCCESS"
	UserActivationSegmentAttemptedNoSuccess = "ATTEMPTED_ZERO_SUCCESS"
	UserActivationSegmentRegistered         = "REGISTERED_NO_ATTEMPT"

	activationStarterNotes = "user_activation:starter"
	activationRecallNotes  = "user_activation:recall"
	activationValidityDays = 1
)

var (
	ErrUserActivationEmailVerificationRequired = infraerrors.Forbidden(
		"USER_ACTIVATION_EMAIL_VERIFICATION_REQUIRED",
		"email verification must be enabled for user activation grants",
	)
	ErrUserActivationRecallUnavailable = infraerrors.Conflict(
		"USER_ACTIVATION_RECALL_UNAVAILABLE",
		"recall subscription is not claimable",
	)
)

type ActivationGrantStatus struct {
	State          string     `json:"state"`
	SubscriptionID *int64     `json:"subscription_id,omitempty"`
	GrantedAt      *time.Time `json:"granted_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
}

type ActivationRecallStatus struct {
	State          string     `json:"state"`
	SubscriptionID *int64     `json:"subscription_id,omitempty"`
	ClaimedAt      *time.Time `json:"claimed_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Claimable      bool       `json:"claimable"`
}

type ActivationGroupStatus struct {
	GroupID        int64     `json:"group_id"`
	SubscriptionID int64     `json:"subscription_id"`
	StartsAt       time.Time `json:"starts_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type UserActivationStatus struct {
	Enabled        bool                   `json:"enabled"`
	Segment        string                 `json:"segment,omitempty"`
	FirstSuccessAt *time.Time             `json:"first_success_at,omitempty"`
	Starter        ActivationGrantStatus  `json:"starter"`
	Recall         ActivationRecallStatus `json:"recall"`
	ActiveGroup    *ActivationGroupStatus `json:"active_group,omitempty"`
	SupportWeChat  string                 `json:"support_wechat,omitempty"`
	NextAction     string                 `json:"next_action,omitempty"`
}

type UserActivationBootstrapper interface {
	BootstrapVerifiedRegistration(ctx context.Context, user *User, campaignSource string) error
}

type UserActivationGrantCommittedError struct {
	UserID  int64
	GroupID int64
	Cause   error
}

func (e *UserActivationGrantCommittedError) Error() string {
	return fmt.Sprintf(
		"activation subscription grant committed for user %d group %d, but cache invalidation failed: %v",
		e.UserID,
		e.GroupID,
		e.Cause,
	)
}

func (e *UserActivationGrantCommittedError) Unwrap() error {
	return e.Cause
}

type UserActivationService struct {
	cfg            config.UserActivationConfig
	journeys       UserActivationJourneyRepository
	evidence       UserActivationEvidenceRepository
	subscriptions  *SubscriptionService
	settingService *SettingService
	entClient      *dbent.Client
}

func NewUserActivationService(
	cfg config.UserActivationConfig,
	journeys UserActivationJourneyRepository,
	evidence UserActivationEvidenceRepository,
	subscriptions *SubscriptionService,
	settingService *SettingService,
	entClient *dbent.Client,
) *UserActivationService {
	return &UserActivationService{
		cfg:            cfg,
		journeys:       journeys,
		evidence:       evidence,
		subscriptions:  subscriptions,
		settingService: settingService,
		entClient:      entClient,
	}
}

func (s *UserActivationService) BootstrapVerifiedRegistration(
	ctx context.Context,
	user *User,
	campaignSource string,
) error {
	if !s.cfg.Enabled {
		return nil
	}
	if !s.isEligibleVerifiedEmailUser(user) {
		return nil
	}
	if s.settingService == nil || !s.settingService.IsEmailVerifyEnabled(ctx) {
		return ErrUserActivationEmailVerificationRequired
	}
	if s.journeys == nil {
		return fmt.Errorf("user activation journey repository is not configured")
	}
	if s.subscriptions == nil {
		return fmt.Errorf("user activation subscription service is not configured")
	}
	if _, _, err := s.journeys.CreateIfAbsent(ctx, user.ID, campaignSource); err != nil {
		return fmt.Errorf("create activation journey: %w", err)
	}

	cacheGroupID := int64(0)
	_, err := s.withLockedActivation(ctx, user.ID, func(
		txCtx context.Context,
		lockedUser *User,
		journey *UserActivationJourney,
		evidence *UserActivationEvidence,
		now time.Time,
	) error {
		if !s.isEligibleVerifiedEmailUser(lockedUser) {
			return ErrUserNotFound
		}
		if journey.StarterSubscriptionID == nil {
			subscription, _, err := s.subscriptions.assignSubscriptionWithReuse(
				txCtx,
				&AssignSubscriptionInput{
					UserID:       lockedUser.ID,
					GroupID:      s.cfg.StarterGroupID,
					ValidityDays: activationValidityDays,
					Notes:        activationStarterNotes,
				},
				true,
			)
			if err != nil {
				return fmt.Errorf("assign starter subscription: %w", err)
			}
			journey.StarterState = "granted"
			journey.StarterSubscriptionID = int64Pointer(subscription.ID)
			journey.StarterGrantedAt = timePointer(subscription.StartsAt)
			journey.StarterExpiresAt = timePointer(subscription.ExpiresAt)
		}
		s.applyEvidenceState(lockedUser, journey, evidence, now)
		cacheGroupID = s.cfg.StarterGroupID
		return nil
	})
	if err != nil {
		return err
	}
	return s.invalidateCommittedGrant(user.ID, cacheGroupID)
}

func (s *UserActivationService) GetStatus(
	ctx context.Context,
	userID int64,
) (*UserActivationStatus, error) {
	if !s.cfg.Enabled {
		return s.disabledStatus(), nil
	}
	return s.Evaluate(ctx, userID, time.Now().UTC())
}

func (s *UserActivationService) Evaluate(
	ctx context.Context,
	userID int64,
	now time.Time,
) (*UserActivationStatus, error) {
	if !s.cfg.Enabled {
		return s.disabledStatus(), nil
	}
	return s.withLockedActivation(ctx, userID, func(
		_ context.Context,
		lockedUser *User,
		journey *UserActivationJourney,
		evidence *UserActivationEvidence,
		_ time.Time,
	) error {
		s.applyEvidenceState(lockedUser, journey, evidence, now)
		return nil
	})
}

func (s *UserActivationService) ClaimRecall(
	ctx context.Context,
	userID int64,
) (*UserActivationStatus, error) {
	if !s.cfg.Enabled {
		return s.disabledStatus(), nil
	}
	if s.subscriptions == nil {
		return nil, fmt.Errorf("user activation subscription service is not configured")
	}

	var blockedState string
	cacheGroupID := int64(0)
	status, err := s.withLockedActivation(ctx, userID, func(
		txCtx context.Context,
		lockedUser *User,
		journey *UserActivationJourney,
		evidence *UserActivationEvidence,
		now time.Time,
	) error {
		s.applyEvidenceState(lockedUser, journey, evidence, now)
		if journey.RecallSubscriptionID != nil || journey.RecallClaimedAt != nil {
			cacheGroupID = s.cfg.RecallGroupID
			return nil
		}
		if journey.RecallState != "claimable" {
			blockedState = journey.RecallState
			return nil
		}

		subscription, _, err := s.subscriptions.assignSubscriptionWithReuse(
			txCtx,
			&AssignSubscriptionInput{
				UserID:       lockedUser.ID,
				GroupID:      s.cfg.RecallGroupID,
				ValidityDays: activationValidityDays,
				Notes:        activationRecallNotes,
			},
			true,
		)
		if err != nil {
			return fmt.Errorf("assign recall subscription: %w", err)
		}
		journey.RecallState = "claimed"
		journey.RecallSubscriptionID = int64Pointer(subscription.ID)
		journey.RecallClaimedAt = timePointer(subscription.StartsAt)
		journey.RecallExpiresAt = timePointer(subscription.ExpiresAt)
		cacheGroupID = s.cfg.RecallGroupID
		return nil
	})
	if err != nil {
		return nil, err
	}
	if blockedState != "" {
		return status, ErrUserActivationRecallUnavailable.WithMetadata(map[string]string{
			"recall_state": blockedState,
		})
	}
	if err := s.invalidateCommittedGrant(userID, cacheGroupID); err != nil {
		return status, err
	}
	return status, nil
}

type activationMutation func(
	ctx context.Context,
	user *User,
	journey *UserActivationJourney,
	evidence *UserActivationEvidence,
	now time.Time,
) error

func (s *UserActivationService) withLockedActivation(
	ctx context.Context,
	userID int64,
	mutate activationMutation,
) (*UserActivationStatus, error) {
	if s.entClient == nil {
		return nil, fmt.Errorf("user activation Ent client is not configured")
	}
	if s.journeys == nil {
		return nil, fmt.Errorf("user activation journey repository is not configured")
	}
	if s.evidence == nil {
		return nil, fmt.Errorf("user activation evidence repository is not configured")
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin user activation transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()
	txCtx := dbent.NewTxContext(ctx, tx)

	lockedEntity, err := tx.Client().User.Query().
		Where(
			dbuser.IDEQ(userID),
			dbuser.DeletedAtIsNil(),
		).
		ForUpdate().
		Only(txCtx)
	if err != nil {
		_ = tx.Rollback()
		if dbent.IsNotFound(err) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("lock activation user: %w", err)
	}
	lockedUser := &User{
		ID:           lockedEntity.ID,
		Email:        lockedEntity.Email,
		SignupSource: lockedEntity.SignupSource,
		CreatedAt:    lockedEntity.CreatedAt,
		UpdatedAt:    lockedEntity.UpdatedAt,
		DeletedAt:    lockedEntity.DeletedAt,
	}

	journey, err := s.journeys.GetByUserIDForUpdate(txCtx, userID)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	snapshot, err := s.evidence.Snapshot(txCtx, userID)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("read activation evidence: %w", err)
	}
	now := time.Now().UTC()
	if err := mutate(txCtx, lockedUser, journey, snapshot, now); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := s.journeys.Update(txCtx, journey); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("update activation journey: %w", err)
	}
	status := s.statusFromJourney(journey, snapshot)
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit user activation transaction: %w", err)
	}
	return status, nil
}

func (s *UserActivationService) applyEvidenceState(
	user *User,
	journey *UserActivationJourney,
	evidence *UserActivationEvidence,
	now time.Time,
) {
	if evidence == nil {
		evidence = &UserActivationEvidence{}
	}
	if journey.FirstSuccessAt == nil && evidence.FirstSuccessfulUsageAt != nil {
		journey.FirstSuccessAt = timePointer(*evidence.FirstSuccessfulUsageAt)
	}
	if journey.StarterSubscriptionID != nil && journey.StarterExpiresAt != nil {
		if journey.StarterExpiresAt.After(now) {
			journey.StarterState = "granted"
		} else {
			journey.StarterState = "expired"
		}
	}

	switch {
	case journey.FirstSuccessAt != nil || evidence.FirstSuccessfulUsageAt != nil:
		journey.RecallState = "closed_success"
	case evidence.FirstCompletedPaymentAt != nil:
		journey.RecallState = "blocked_paid"
	case journey.RecallSubscriptionID != nil || journey.RecallClaimedAt != nil:
		if journey.RecallExpiresAt != nil && journey.RecallExpiresAt.After(now) {
			journey.RecallState = "claimed"
		} else {
			journey.RecallState = "expired"
		}
	case journey.StarterSubscriptionID == nil ||
		journey.StarterExpiresAt == nil ||
		journey.StarterExpiresAt.After(now):
		journey.RecallState = "locked"
	case user == nil || !now.Before(user.CreatedAt.Add(
		time.Duration(s.cfg.RecallWindowDays)*24*time.Hour,
	)):
		journey.RecallState = "expired"
	default:
		journey.RecallState = "claimable"
	}
	journey.LastEvaluatedAt = timePointer(now)
}

func (s *UserActivationService) statusFromJourney(
	journey *UserActivationJourney,
	evidence *UserActivationEvidence,
) *UserActivationStatus {
	status := &UserActivationStatus{
		Enabled:        true,
		Segment:        activationSegmentFromState(journey, evidence),
		FirstSuccessAt: journey.FirstSuccessAt,
		Starter: ActivationGrantStatus{
			State:          journey.StarterState,
			SubscriptionID: journey.StarterSubscriptionID,
			GrantedAt:      journey.StarterGrantedAt,
			ExpiresAt:      journey.StarterExpiresAt,
		},
		Recall: ActivationRecallStatus{
			State:          journey.RecallState,
			SubscriptionID: journey.RecallSubscriptionID,
			ClaimedAt:      journey.RecallClaimedAt,
			ExpiresAt:      journey.RecallExpiresAt,
			Claimable:      journey.RecallState == "claimable",
		},
		SupportWeChat: s.cfg.SupportWeChat,
	}
	if journey.RecallSubscriptionID != nil &&
		journey.RecallClaimedAt != nil &&
		journey.RecallExpiresAt != nil &&
		journey.LastEvaluatedAt != nil &&
		!journey.RecallClaimedAt.After(*journey.LastEvaluatedAt) &&
		journey.RecallExpiresAt.After(*journey.LastEvaluatedAt) {
		status.ActiveGroup = &ActivationGroupStatus{
			GroupID:        s.cfg.RecallGroupID,
			SubscriptionID: *journey.RecallSubscriptionID,
			StartsAt:       *journey.RecallClaimedAt,
			ExpiresAt:      *journey.RecallExpiresAt,
		}
	} else if journey.StarterState == "granted" &&
		journey.StarterSubscriptionID != nil &&
		journey.StarterGrantedAt != nil &&
		journey.StarterExpiresAt != nil {
		status.ActiveGroup = &ActivationGroupStatus{
			GroupID:        s.cfg.StarterGroupID,
			SubscriptionID: *journey.StarterSubscriptionID,
			StartsAt:       *journey.StarterGrantedAt,
			ExpiresAt:      *journey.StarterExpiresAt,
		}
	}
	status.NextAction = activationNextAction(status)
	return status
}

func activationSegmentFromState(
	journey *UserActivationJourney,
	evidence *UserActivationEvidence,
) string {
	switch {
	case (journey != nil && journey.FirstSuccessAt != nil) ||
		(evidence != nil && evidence.FirstSuccessfulUsageAt != nil):
		return UserActivationSegmentSuccess
	case evidence != nil && evidence.FirstCompletedPaymentAt != nil:
		return UserActivationSegmentPaidZeroSuccess
	case evidence != nil && (evidence.UsageCount > 0 || evidence.APIKeyCount > 0):
		return UserActivationSegmentAttemptedNoSuccess
	default:
		return UserActivationSegmentRegistered
	}
}

func activationNextAction(status *UserActivationStatus) string {
	switch {
	case status == nil || !status.Enabled:
		return ""
	case status.Segment == UserActivationSegmentSuccess:
		return "purchase"
	case status.Segment == UserActivationSegmentPaidZeroSuccess:
		return "support"
	case status.Recall.Claimable:
		return "claim_recall"
	case status.ActiveGroup != nil:
		return "use_trial"
	default:
		return "troubleshoot"
	}
}

func (s *UserActivationService) isEligibleVerifiedEmailUser(user *User) bool {
	return user != nil &&
		user.DeletedAt == nil &&
		user.SignupSource == "email" &&
		!user.CreatedAt.Before(s.cfg.EligibleAfter)
}

func (s *UserActivationService) disabledStatus() *UserActivationStatus {
	return &UserActivationStatus{Enabled: false}
}

func (s *UserActivationService) invalidateCommittedGrant(userID, groupID int64) error {
	if groupID <= 0 || s.subscriptions == nil {
		return nil
	}
	if err := s.subscriptions.invalidateSubscriptionCaches(userID, groupID); err != nil {
		return &UserActivationGrantCommittedError{
			UserID:  userID,
			GroupID: groupID,
			Cause:   err,
		}
	}
	return nil
}

func int64Pointer(value int64) *int64 {
	return &value
}

func timePointer(value time.Time) *time.Time {
	return &value
}
