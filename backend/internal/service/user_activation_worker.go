// [INPUT]: Activation configuration, lifecycle/evidence repositories, email delivery, and leader locks.
// [OUTPUT]: Recoverable activation scans with URL-gated mail and bounded failure diagnostics.
// [POS]: Application worker that owns HVOY activation timing, priority, and recovery orchestration.
//
// [PROTOCOL]:
// 1. Update this header when scan recovery, timing priority, or worker lifecycle changes.
// 2. Keep SQL factual and email transport free of activation policy.
package service

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

const (
	userActivationWorkerLeaderLockKey = "user_activation:worker:leader"
	userActivationWorkerCycleTimeout  = 30 * time.Second
	userActivationWorkerLeaderLockTTL = 2 * time.Minute
	userActivationWorkerPageSize      = 200

	userActivationAttemptDelay  = 30 * time.Minute
	userActivationNoAttemptAge  = 2 * time.Hour
	userActivationEmailCooldown = 12 * time.Hour
)

type userActivationWorkerLifecycle interface {
	BootstrapVerifiedRegistration(ctx context.Context, user *User, campaignSource string) error
	Evaluate(ctx context.Context, userID int64, now time.Time) (*UserActivationStatus, error)
}

type userActivationWorkerUserReader interface {
	GetByID(ctx context.Context, userID int64) (*User, error)
}

type userActivationWorkerEmailSender interface {
	Send(ctx context.Context, input NotificationEmailSendInput) error
}

type userActivationWorkerFrontendURLReader interface {
	GetFrontendURL(ctx context.Context) string
}

type UserActivationWorker struct {
	cfg         config.UserActivationConfig
	journeys    UserActivationJourneyRepository
	evidence    UserActivationEvidenceRepository
	lifecycle   userActivationWorkerLifecycle
	users       userActivationWorkerUserReader
	emailSender userActivationWorkerEmailSender
	frontendURL userActivationWorkerFrontendURLReader

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup

	cancelMu sync.Mutex
	cancel   context.CancelFunc
}

func NewUserActivationWorker(
	cfg config.UserActivationConfig,
	journeys UserActivationJourneyRepository,
	evidence UserActivationEvidenceRepository,
	lifecycle userActivationWorkerLifecycle,
	users userActivationWorkerUserReader,
	emailSender userActivationWorkerEmailSender,
	frontendURL userActivationWorkerFrontendURLReader,
) *UserActivationWorker {
	return &UserActivationWorker{
		cfg:         cfg,
		journeys:    journeys,
		evidence:    evidence,
		lifecycle:   lifecycle,
		users:       users,
		emailSender: emailSender,
		frontendURL: frontendURL,
		instanceID:  uuid.NewString(),
		stopCh:      make(chan struct{}),
	}
}

func (w *UserActivationWorker) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if w == nil {
		return
	}
	w.lockCache = lockCache
	w.db = db
}

func (w *UserActivationWorker) Start() {
	if w == nil || !w.cfg.Enabled || w.cfg.WorkerInterval <= 0 {
		return
	}
	select {
	case <-w.stopCh:
		return
	default:
	}

	w.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		w.cancelMu.Lock()
		w.cancel = cancel
		w.cancelMu.Unlock()

		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			ticker := time.NewTicker(w.cfg.WorkerInterval)
			defer ticker.Stop()

			w.runOnce(ctx)
			for {
				select {
				case <-ticker.C:
					w.runOnce(ctx)
				case <-ctx.Done():
					return
				case <-w.stopCh:
					return
				}
			}
		}()
	})
}

func (w *UserActivationWorker) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		close(w.stopCh)
		w.cancelMu.Lock()
		cancel := w.cancel
		w.cancelMu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
	w.wg.Wait()
}

func (w *UserActivationWorker) runOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, userActivationWorkerCycleTimeout)
	defer cancel()
	w.runOnceAt(ctx, time.Now().UTC())
}

func (w *UserActivationWorker) runOnceAt(ctx context.Context, cycleNow time.Time) {
	if w == nil || !w.cfg.Enabled || w.journeys == nil || w.lifecycle == nil {
		return
	}
	release, ok := tryAcquireSingletonLeaderLock(
		ctx,
		w.lockCache,
		w.db,
		userActivationWorkerLeaderLockKey,
		w.instanceID,
		userActivationWorkerLeaderLockTTL,
	)
	if !ok {
		return
	}
	defer release()

	if err := w.repairMissingJourneys(ctx); err != nil {
		logUserActivationWorkerError("repair_missing_journeys", 0, 0, err)
	}
	if err := w.processDueJourneys(ctx, cycleNow); err != nil {
		logUserActivationWorkerError("process_due_journeys", 0, 0, err)
	}
}

func (w *UserActivationWorker) repairMissingJourneys(ctx context.Context) error {
	var afterID int64
	for {
		users, err := w.journeys.ListEligibleUsersWithoutJourney(
			ctx,
			w.cfg.EligibleAfter,
			afterID,
			userActivationWorkerPageSize,
		)
		if err != nil {
			return err
		}
		if len(users) == 0 {
			return nil
		}
		for i := range users {
			user := &users[i]
			afterID = user.ID
			if err := w.lifecycle.BootstrapVerifiedRegistration(ctx, user, "unknown"); err != nil {
				logUserActivationWorkerError("bootstrap_missing_journey", 0, user.ID, err)
			}
		}
	}
}

func (w *UserActivationWorker) processDueJourneys(ctx context.Context, cycleNow time.Time) error {
	if w.users == nil || w.evidence == nil || w.emailSender == nil {
		return nil
	}
	cutoff := cycleNow.Add(-w.cfg.WorkerInterval)
	var afterID int64
	for {
		journeys, err := w.journeys.ListDue(
			ctx,
			cutoff,
			afterID,
			userActivationWorkerPageSize,
		)
		if err != nil {
			return err
		}
		if len(journeys) == 0 {
			return nil
		}
		for i := range journeys {
			journey := &journeys[i]
			afterID = journey.ID
			w.processJourney(ctx, journey, cycleNow)
		}
	}
}

func (w *UserActivationWorker) processJourney(
	ctx context.Context,
	listedJourney *UserActivationJourney,
	cycleNow time.Time,
) {
	user, err := w.users.GetByID(ctx, listedJourney.UserID)
	if err != nil {
		logUserActivationWorkerError("read_user", listedJourney.ID, listedJourney.UserID, err)
		return
	}
	if listedJourney.StarterSubscriptionID == nil {
		source := strings.TrimSpace(listedJourney.CampaignSource)
		if source == "" {
			source = "unknown"
		}
		if err := w.lifecycle.BootstrapVerifiedRegistration(ctx, user, source); err != nil {
			logUserActivationWorkerError("retry_starter", listedJourney.ID, listedJourney.UserID, err)
			return
		}
	}
	if _, err := w.lifecycle.Evaluate(ctx, listedJourney.UserID, cycleNow); err != nil {
		logUserActivationWorkerError("evaluate", listedJourney.ID, listedJourney.UserID, err)
		return
	}

	journey, err := w.journeys.GetByUserID(ctx, listedJourney.UserID)
	if err != nil {
		logUserActivationWorkerError("refresh_journey", listedJourney.ID, listedJourney.UserID, err)
		return
	}
	evidence, err := w.evidence.Snapshot(ctx, listedJourney.UserID)
	if err != nil {
		logUserActivationWorkerError("read_evidence", journey.ID, journey.UserID, err)
		return
	}
	stage, ok := selectUserActivationEmailStage(
		w.cfg.RecallWindowDays,
		user,
		journey,
		evidence,
		cycleNow,
	)
	if !ok {
		return
	}

	latestEvidence, err := w.evidence.Snapshot(ctx, listedJourney.UserID)
	if err != nil {
		logUserActivationWorkerError("recheck_success", journey.ID, journey.UserID, err)
		return
	}
	stage, ok = selectUserActivationEmailStage(
		w.cfg.RecallWindowDays,
		user,
		journey,
		latestEvidence,
		cycleNow,
	)
	if !ok {
		return
	}

	input, ok := w.notificationInput(ctx, user, journey, stage)
	if !ok {
		return
	}
	if err := w.emailSender.Send(ctx, input); err != nil {
		logUserActivationWorkerError(string(stage), journey.ID, journey.UserID, err)
		return
	}
	if err := w.journeys.MarkEmailSent(ctx, journey.ID, stage, cycleNow); err != nil {
		logUserActivationWorkerError("mark_email_sent", journey.ID, journey.UserID, err)
	}
}

func (w *UserActivationWorker) notificationInput(
	ctx context.Context,
	user *User,
	journey *UserActivationJourney,
	stage UserActivationEmailStage,
) (NotificationEmailSendInput, bool) {
	variables := map[string]string{
		"support_wechat": strings.TrimSpace(w.cfg.SupportWeChat),
	}
	if userActivationStageUsesActivationURL(stage) {
		frontendURL := ""
		if w.frontendURL != nil {
			frontendURL = strings.TrimSpace(w.frontendURL.GetFrontendURL(ctx))
		}
		if frontendURL == "" {
			return NotificationEmailSendInput{}, false
		}
		variables["activation_url"] = strings.TrimRight(frontendURL, "/") + "/activation"
	}
	return NotificationEmailSendInput{
		Event:          userActivationNotificationEvent(stage),
		RecipientEmail: user.Email,
		RecipientName:  firstNonEmpty(user.Username, user.Email),
		UserID:         user.ID,
		SourceType:     "user_activation_journey",
		SourceID:       strconv.FormatInt(journey.ID, 10),
		ReminderKey:    string(stage),
		Variables:      variables,
	}, true
}

func selectUserActivationEmailStage(
	recallWindowDays int,
	user *User,
	journey *UserActivationJourney,
	evidence *UserActivationEvidence,
	now time.Time,
) (UserActivationEmailStage, bool) {
	if user == nil || journey == nil {
		return "", false
	}
	if evidence == nil {
		evidence = &UserActivationEvidence{}
	}
	if journey.FirstSuccessAt != nil || evidence.FirstSuccessfulUsageAt != nil {
		return "", false
	}
	if evidence.FirstCompletedPaymentAt != nil {
		if journey.PaidSupportEmailSentAt != nil {
			return "", false
		}
		return UserActivationEmailStagePaidSupport, true
	}
	if journey.LastEmailSentAt != nil &&
		now.Before(journey.LastEmailSentAt.Add(userActivationEmailCooldown)) {
		return "", false
	}
	if journey.RecallSubscriptionID != nil &&
		journey.RecallState == "expired" {
		if journey.RecallExpiredEmailSentAt != nil {
			return "", false
		}
		return UserActivationEmailStageRecallExpired, true
	}

	recallWindow := time.Duration(recallWindowDays) * 24 * time.Hour
	insideRecallWindow := recallWindowDays > 0 &&
		now.Before(user.CreatedAt.Add(recallWindow))
	if insideRecallWindow &&
		journey.RecallState == "claimable" &&
		journey.RecallAvailableEmailSentAt == nil {
		return UserActivationEmailStageRecallAvailable, true
	}
	if evidence.UsageCount > 0 || evidence.APIKeyCount > 0 {
		if journey.AttemptedEmailSentAt != nil {
			return "", false
		}
		observedAt := laterActivationAttemptTime(
			evidence.FirstAPIKeyAt,
			evidence.LastAttemptAt,
		)
		if observedAt == nil || now.Before(observedAt.Add(userActivationAttemptDelay)) {
			return "", false
		}
		return UserActivationEmailStageAttempted, true
	}
	if journey.NoAttemptEmailSentAt == nil &&
		!now.Before(user.CreatedAt.Add(userActivationNoAttemptAge)) {
		return UserActivationEmailStageNoAttempt, true
	}
	return "", false
}

func laterActivationAttemptTime(firstAPIKeyAt, lastAttemptAt *time.Time) *time.Time {
	switch {
	case firstAPIKeyAt == nil:
		return lastAttemptAt
	case lastAttemptAt == nil:
		return firstAPIKeyAt
	case lastAttemptAt.After(*firstAPIKeyAt):
		return lastAttemptAt
	default:
		return firstAPIKeyAt
	}
}

func userActivationNotificationEvent(stage UserActivationEmailStage) string {
	switch stage {
	case UserActivationEmailStageNoAttempt:
		return NotificationEmailEventActivationNoAttempt
	case UserActivationEmailStageAttempted:
		return NotificationEmailEventActivationAttemptedZeroSuccess
	case UserActivationEmailStagePaidSupport:
		return NotificationEmailEventActivationPaidZeroSuccess
	case UserActivationEmailStageRecallAvailable:
		return NotificationEmailEventActivationRecallAvailable
	case UserActivationEmailStageRecallExpired:
		return NotificationEmailEventActivationRecallExpired
	default:
		return ""
	}
}

func userActivationStageUsesActivationURL(stage UserActivationEmailStage) bool {
	return stage == UserActivationEmailStageNoAttempt ||
		stage == UserActivationEmailStageAttempted ||
		stage == UserActivationEmailStageRecallAvailable
}

func logUserActivationWorkerError(
	stage string,
	journeyID int64,
	userID int64,
	_ error,
) {
	logger.LegacyPrintf(
		"service.user_activation_worker",
		"error: stage=%s journey_id=%d user_id=%d failure_category=downstream",
		stage,
		journeyID,
		userID,
	)
}
