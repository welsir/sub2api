// [INPUT]: Activation worker dependencies, deterministic account evidence, and lifecycle controls.
// [OUTPUT]: Proof of activation policy, split-brain exclusion, delivery recovery, and safe shutdown.
// [POS]: Service-layer contract suite for the HVOY activation recovery worker.
//
// [PROTOCOL]:
// 1. Update this header when activation worker contracts or failure modes change.
// 2. Test policy with observed evidence rather than mock-generated conclusions.
package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestUserActivationWorkerSelectStagePriorityAndTimeRules(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	starterID := int64(11)
	recallID := int64(22)
	baseUser := &User{
		ID:           1,
		SignupSource: "email",
		CreatedAt:    now.Add(-6 * time.Hour),
	}
	baseJourney := &UserActivationJourney{
		ID:                    10,
		UserID:                baseUser.ID,
		StarterSubscriptionID: &starterID,
		StarterExpiresAt:      timePointer(now.Add(-time.Hour)),
		RecallState:           "claimable",
	}

	t.Run("paid has highest priority and bypasses cooldown", func(t *testing.T) {
		journey := *baseJourney
		journey.RecallState = "expired"
		journey.RecallSubscriptionID = &recallID
		journey.RecallExpiresAt = timePointer(now.Add(-time.Hour))
		journey.LastEmailSentAt = timePointer(now.Add(-time.Minute))
		stage, ok := selectUserActivationEmailStage(
			7,
			baseUser,
			&journey,
			&UserActivationEvidence{
				FirstCompletedPaymentAt: timePointer(now.Add(-time.Minute)),
				LastAttemptAt:           timePointer(now.Add(-time.Hour)),
				UsageCount:              1,
			},
			now,
		)
		require.True(t, ok)
		require.Equal(t, UserActivationEmailStagePaidSupport, stage)
	})

	t.Run("recall expired precedes attempted", func(t *testing.T) {
		journey := *baseJourney
		journey.RecallState = "expired"
		journey.RecallSubscriptionID = &recallID
		journey.RecallExpiresAt = timePointer(now.Add(-time.Hour))
		stage, ok := selectUserActivationEmailStage(
			7,
			baseUser,
			&journey,
			&UserActivationEvidence{
				LastAttemptAt: timePointer(now.Add(-time.Hour)),
				UsageCount:    1,
			},
			now,
		)
		require.True(t, ok)
		require.Equal(t, UserActivationEmailStageRecallExpired, stage)
	})

	t.Run("recall available precedes attempted and stays inside seven day window", func(t *testing.T) {
		stage, ok := selectUserActivationEmailStage(
			7,
			baseUser,
			baseJourney,
			&UserActivationEvidence{
				LastAttemptAt: timePointer(now.Add(-time.Hour)),
				UsageCount:    1,
			},
			now,
		)
		require.True(t, ok)
		require.Equal(t, UserActivationEmailStageRecallAvailable, stage)

		userAtWindowEnd := *baseUser
		userAtWindowEnd.CreatedAt = now.Add(-7 * 24 * time.Hour)
		stage, ok = selectUserActivationEmailStage(
			7,
			&userAtWindowEnd,
			baseJourney,
			&UserActivationEvidence{},
			now,
		)
		require.True(t, ok)
		require.Equal(t, UserActivationEmailStageNoAttempt, stage)
	})

	t.Run("attempted remains eligible after recall window", func(t *testing.T) {
		user := *baseUser
		user.CreatedAt = now.Add(-8 * 24 * time.Hour)
		journey := *baseJourney
		journey.RecallState = "locked"
		stage, ok := selectUserActivationEmailStage(
			7,
			&user,
			&journey,
			&UserActivationEvidence{
				FirstAPIKeyAt: timePointer(now.Add(-2 * time.Hour)),
				APIKeyCount:   1,
			},
			now,
		)
		require.True(t, ok)
		require.Equal(t, UserActivationEmailStageAttempted, stage)
	})

	t.Run("attempt waits thirty minutes from later key or request", func(t *testing.T) {
		journey := *baseJourney
		journey.RecallState = "locked"
		evidence := &UserActivationEvidence{
			FirstAPIKeyAt: timePointer(now.Add(-2 * time.Hour)),
			LastAttemptAt: timePointer(now.Add(-29 * time.Minute)),
			APIKeyCount:   1,
			UsageCount:    1,
		}
		_, ok := selectUserActivationEmailStage(7, baseUser, &journey, evidence, now)
		require.False(t, ok)

		evidence.LastAttemptAt = timePointer(now.Add(-30 * time.Minute))
		stage, ok := selectUserActivationEmailStage(7, baseUser, &journey, evidence, now)
		require.True(t, ok)
		require.Equal(t, UserActivationEmailStageAttempted, stage)
	})

	t.Run("no attempt waits two hours from real user creation", func(t *testing.T) {
		journey := *baseJourney
		journey.RecallState = "locked"
		journey.CreatedAt = now.Add(-10 * time.Minute)
		user := *baseUser
		user.CreatedAt = now.Add(-119 * time.Minute)
		_, ok := selectUserActivationEmailStage(7, &user, &journey, &UserActivationEvidence{}, now)
		require.False(t, ok)

		user.CreatedAt = now.Add(-2 * time.Hour)
		stage, ok := selectUserActivationEmailStage(7, &user, &journey, &UserActivationEvidence{}, now)
		require.True(t, ok)
		require.Equal(t, UserActivationEmailStageNoAttempt, stage)
	})

	t.Run("non paid stages respect twelve hour cooldown", func(t *testing.T) {
		journey := *baseJourney
		journey.LastEmailSentAt = timePointer(now.Add(-11*time.Hour - 59*time.Minute))
		_, ok := selectUserActivationEmailStage(7, baseUser, &journey, &UserActivationEvidence{}, now)
		require.False(t, ok)

		journey.LastEmailSentAt = timePointer(now.Add(-12 * time.Hour))
		stage, ok := selectUserActivationEmailStage(7, baseUser, &journey, &UserActivationEvidence{}, now)
		require.True(t, ok)
		require.Equal(t, UserActivationEmailStageRecallAvailable, stage)
	})

	t.Run("successful users never receive activation email", func(t *testing.T) {
		_, ok := selectUserActivationEmailStage(
			7,
			baseUser,
			baseJourney,
			&UserActivationEvidence{FirstSuccessfulUsageAt: timePointer(now.Add(-time.Minute))},
			now,
		)
		require.False(t, ok)
	})
}

func TestUserActivationWorkerRepairsMissingJourneysAndStarterGrantsWithStableCursor(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	users := []User{
		{ID: 1, Email: "first@example.com", SignupSource: "email", CreatedAt: now.Add(-time.Hour)},
		{ID: 2, Email: "second@example.com", SignupSource: "email", CreatedAt: now.Add(-time.Hour)},
	}
	journeys := &activationWorkerJourneyRepoStub{
		eligibleUsers: users,
		dueJourneys: []UserActivationJourney{
			{ID: 10, UserID: 2, StarterSubscriptionID: nil, RecallState: "locked"},
		},
		byUserID: map[int64]UserActivationJourney{
			2: {ID: 10, UserID: 2, StarterSubscriptionID: nil, RecallState: "locked"},
		},
	}
	lifecycle := &activationWorkerLifecycleStub{
		bootstrapErrors: map[int64]error{1: errors.New("grant unavailable")},
	}
	worker := newActivationWorkerForTest(
		activationWorkerConfig(now),
		journeys,
		&activationWorkerEvidenceRepoStub{},
		lifecycle,
		&activationWorkerUserReaderStub{users: map[int64]*User{2: &users[1]}},
		&activationWorkerEmailSenderStub{},
	)

	worker.runOnceAt(context.Background(), now)

	require.Equal(t, []int64{1, 2, 2}, lifecycle.bootstrapUserIDs)
	require.Equal(t, []int64{0, 1, 2}, journeys.eligibleAfterIDs)
	require.Equal(t, []int64{0, 10}, journeys.dueAfterIDs)
	require.Equal(t, 1, lifecycle.evaluateCalls[2])
}

func TestUserActivationWorkerPreSendSuccessRecheckStopsEmail(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	user := &User{ID: 7, Email: "user@example.com", SignupSource: "email", CreatedAt: now.Add(-3 * time.Hour)}
	journey := UserActivationJourney{
		ID:                    70,
		UserID:                user.ID,
		StarterSubscriptionID: int64Pointer(1),
		RecallState:           "locked",
	}
	journeys := &activationWorkerJourneyRepoStub{
		dueJourneys: []UserActivationJourney{journey},
		byUserID:    map[int64]UserActivationJourney{user.ID: journey},
	}
	evidence := &activationWorkerEvidenceRepoStub{
		sequence: map[int64][]*UserActivationEvidence{
			user.ID: {
				{},
				{FirstSuccessfulUsageAt: timePointer(now.Add(-time.Second))},
			},
		},
	}
	sender := &activationWorkerEmailSenderStub{}
	worker := newActivationWorkerForTest(
		activationWorkerConfig(now),
		journeys,
		evidence,
		&activationWorkerLifecycleStub{},
		&activationWorkerUserReaderStub{users: map[int64]*User{user.ID: user}},
		sender,
	)

	worker.runOnceAt(context.Background(), now)

	require.Empty(t, sender.inputs)
	require.Empty(t, journeys.marked)
}

func TestUserActivationWorkerSendsOneDeduplicatedStageAndMarksNilSendResult(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	user := &User{
		ID:           8,
		Email:        "paid@example.com",
		Username:     "Paid User",
		SignupSource: "email",
		CreatedAt:    now.Add(-4 * time.Hour),
	}
	journey := UserActivationJourney{
		ID:                    80,
		UserID:                user.ID,
		StarterSubscriptionID: int64Pointer(1),
		RecallState:           "blocked_paid",
		LastEmailSentAt:       timePointer(now.Add(-time.Minute)),
	}
	journeys := &activationWorkerJourneyRepoStub{
		dueJourneys: []UserActivationJourney{journey},
		byUserID:    map[int64]UserActivationJourney{user.ID: journey},
	}
	evidence := &activationWorkerEvidenceRepoStub{
		defaults: map[int64]*UserActivationEvidence{
			user.ID: {FirstCompletedPaymentAt: timePointer(now.Add(-time.Minute))},
		},
	}
	sender := &activationWorkerEmailSenderStub{}
	worker := newActivationWorkerForTest(
		activationWorkerConfig(now),
		journeys,
		evidence,
		&activationWorkerLifecycleStub{},
		&activationWorkerUserReaderStub{users: map[int64]*User{user.ID: user}},
		sender,
	)

	worker.runOnceAt(context.Background(), now)

	require.Len(t, sender.inputs, 1)
	input := sender.inputs[0]
	require.Equal(t, NotificationEmailEventActivationPaidZeroSuccess, input.Event)
	require.Equal(t, "user_activation_journey", input.SourceType)
	require.Equal(t, "80", input.SourceID)
	require.Equal(t, "paid_zero_success", input.ReminderKey)
	require.Equal(t, "welsir02", input.Variables["support_wechat"])
	require.NotContains(t, input.Variables, "activation_url")
	require.Equal(t, []activationWorkerMarkedEmail{
		{journeyID: 80, stage: UserActivationEmailStagePaidSupport, sentAt: now},
	}, journeys.marked)
}

func TestUserActivationWorkerMarksAfterSMTPDeliveryWhenDeliveryKeyPersistenceFails(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	baseRepo := newNotificationEmailMemorySettingRepo()
	smtpServer := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, baseRepo.SetMultiple(ctx, smtpServer.settings()))
	require.NoError(t, baseRepo.Set(ctx, notificationEmailUnsubscribeSecretKey, "fixed-test-secret"))
	settingRepo := &notificationEmailFailingSetRepo{
		notificationEmailMemorySettingRepo: baseRepo,
		failPrefix:                         notificationEmailDeliveryKeyPrefix,
	}
	emailSender := NewNotificationEmailService(settingRepo, NewEmailService(settingRepo, nil))

	user := &User{ID: 88, Email: "paid@example.com", CreatedAt: now.Add(-4 * time.Hour)}
	journey := UserActivationJourney{
		ID:                    880,
		UserID:                user.ID,
		StarterSubscriptionID: int64Pointer(1),
		RecallState:           "blocked_paid",
	}
	journeys := &activationWorkerJourneyRepoStub{
		dueJourneys: []UserActivationJourney{journey},
		byUserID:    map[int64]UserActivationJourney{user.ID: journey},
	}
	evidence := &activationWorkerEvidenceRepoStub{
		defaults: map[int64]*UserActivationEvidence{
			user.ID: {FirstCompletedPaymentAt: timePointer(now.Add(-time.Minute))},
		},
	}
	worker := newActivationWorkerForTest(
		activationWorkerConfig(now),
		journeys,
		evidence,
		&activationWorkerLifecycleStub{},
		&activationWorkerUserReaderStub{users: map[int64]*User{user.ID: user}},
		emailSender,
	)

	worker.runOnceAt(ctx, now)
	worker.runOnceAt(ctx, now.Add(time.Minute))

	require.Equal(t, int64(1), smtpServer.messageCount())
	require.Len(t, journeys.marked, 1)
	require.Equal(t, UserActivationEmailStagePaidSupport, journeys.marked[0].stage)
}

func TestUserActivationWorkerDefersURLStageWhenFrontendURLIsUnavailable(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	user := &User{ID: 9, Email: "user@example.com", CreatedAt: now.Add(-3 * time.Hour)}
	journey := UserActivationJourney{
		ID:                    90,
		UserID:                user.ID,
		StarterSubscriptionID: int64Pointer(1),
		RecallState:           "locked",
	}
	journeys := &activationWorkerJourneyRepoStub{
		dueJourneys: []UserActivationJourney{journey},
		byUserID:    map[int64]UserActivationJourney{user.ID: journey},
	}
	sender := &activationWorkerEmailSenderStub{}
	worker := NewUserActivationWorker(
		activationWorkerConfig(now),
		journeys,
		&activationWorkerEvidenceRepoStub{},
		&activationWorkerLifecycleStub{},
		&activationWorkerUserReaderStub{users: map[int64]*User{user.ID: user}},
		sender,
		&activationWorkerFrontendURLStub{},
	)

	worker.runOnceAt(context.Background(), now)

	require.Empty(t, sender.inputs)
	require.Empty(t, journeys.marked)
}

func TestUserActivationWorkerLogsSafeFailureCategoryWithoutDownstreamDetails(t *testing.T) {
	sink, cleanup := captureStructuredLog(t)
	defer cleanup()

	logUserActivationWorkerError(
		"attempted_zero_success",
		42,
		7,
		errors.New("smtp failed for full-user@example.com body=private-message token=sk-secret-value"),
	)

	require.True(t, sink.ContainsMessage("failure_category=downstream"))
	require.True(t, sink.ContainsMessage("stage=attempted_zero_success"))
	require.True(t, sink.ContainsMessage("journey_id=42"))
	require.True(t, sink.ContainsMessage("user_id=7"))
	require.False(t, sink.ContainsMessage("full-user@example.com"))
	require.False(t, sink.ContainsMessage("private-message"))
	require.False(t, sink.ContainsMessage("sk-secret-value"))
}

func TestUserActivationWorkerLeaderAllowsOnlyOneInstanceToScan(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	entered := make(chan struct{})
	release := make(chan struct{})
	journeys := &activationWorkerJourneyRepoStub{
		eligibleEntered: entered,
		eligibleRelease: release,
	}
	lock := &fakeLeaderLockCache{}
	first := newActivationWorkerForTest(
		activationWorkerConfig(now),
		journeys,
		&activationWorkerEvidenceRepoStub{},
		&activationWorkerLifecycleStub{},
		&activationWorkerUserReaderStub{},
		&activationWorkerEmailSenderStub{},
	)
	second := newActivationWorkerForTest(
		activationWorkerConfig(now),
		journeys,
		&activationWorkerEvidenceRepoStub{},
		&activationWorkerLifecycleStub{},
		&activationWorkerUserReaderStub{},
		&activationWorkerEmailSenderStub{},
	)
	first.SetLeaderLock(lock, nil)
	second.SetLeaderLock(lock, nil)

	done := make(chan struct{})
	go func() {
		first.runOnceAt(context.Background(), now)
		close(done)
	}()
	<-entered
	second.runOnceAt(context.Background(), now)
	close(release)
	<-done

	require.Equal(t, 1, journeys.eligibleCalls)
	require.Equal(t, 1, journeys.dueCalls)
}

func TestUserActivationWorkerPGMutexBlocksRecoveredRedisPeer(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	lockID := hashAdvisoryLockID(userActivationWorkerLeaderLockKey)
	mock.ExpectQuery("SELECT pg_try_advisory_lock").
		WithArgs(lockID).
		WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(true))
	mock.ExpectQuery("SELECT pg_try_advisory_lock").
		WithArgs(lockID).
		WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(false))
	mock.ExpectExec("SELECT pg_advisory_unlock").
		WithArgs(lockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	entered := make(chan struct{})
	release := make(chan struct{})
	firstRepo := &activationWorkerJourneyRepoStub{
		eligibleEntered: entered,
		eligibleRelease: release,
	}
	secondRepo := &activationWorkerJourneyRepoStub{}
	cache := &activationSplitBrainCache{failedOwners: map[string]bool{"A": true}}
	first := newActivationWorkerForTest(
		activationWorkerConfig(now),
		firstRepo,
		&activationWorkerEvidenceRepoStub{},
		&activationWorkerLifecycleStub{},
		&activationWorkerUserReaderStub{},
		&activationWorkerEmailSenderStub{},
	)
	second := newActivationWorkerForTest(
		activationWorkerConfig(now),
		secondRepo,
		&activationWorkerEvidenceRepoStub{},
		&activationWorkerLifecycleStub{},
		&activationWorkerUserReaderStub{},
		&activationWorkerEmailSenderStub{},
	)
	first.instanceID = "A"
	second.instanceID = "B"
	first.SetLeaderLock(cache, db)
	second.SetLeaderLock(cache, db)

	done := make(chan struct{})
	go func() {
		first.runOnceAt(context.Background(), now)
		close(done)
	}()
	<-entered
	second.runOnceAt(context.Background(), now)

	close(release)
	<-done
	require.Zero(t, secondRepo.eligibleCalls)
	require.Zero(t, secondRepo.dueCalls)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserActivationWorkerStopWaitsForRunningScan(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	entered := make(chan struct{})
	release := make(chan struct{})
	journeys := &activationWorkerJourneyRepoStub{
		eligibleEntered: entered,
		eligibleRelease: release,
	}
	worker := newActivationWorkerForTest(
		activationWorkerConfig(now),
		journeys,
		&activationWorkerEvidenceRepoStub{},
		&activationWorkerLifecycleStub{},
		&activationWorkerUserReaderStub{},
		&activationWorkerEmailSenderStub{},
	)
	worker.Start()
	<-entered

	stopped := make(chan struct{})
	go func() {
		worker.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned before the running scan exited")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not wait for and join the worker goroutine")
	}
}

func TestUserActivationWorkerStartStopConcurrent(t *testing.T) {
	for range 1000 {
		worker := newActivationWorkerForTest(
			activationWorkerConfig(time.Now().UTC()),
			&activationWorkerJourneyRepoStub{},
			&activationWorkerEvidenceRepoStub{},
			&activationWorkerLifecycleStub{},
			&activationWorkerUserReaderStub{},
			&activationWorkerEmailSenderStub{},
		)
		start := make(chan struct{})
		var calls sync.WaitGroup
		calls.Add(2)
		go func() {
			defer calls.Done()
			<-start
			worker.Start()
		}()
		go func() {
			defer calls.Done()
			<-start
			worker.Stop()
		}()
		close(start)
		calls.Wait()
		worker.Stop()
	}
}

func TestUserActivationWorkerDisabledDoesNotStartOrScan(t *testing.T) {
	cfg := activationWorkerConfig(time.Now().UTC())
	cfg.Enabled = false
	journeys := &activationWorkerJourneyRepoStub{}
	worker := newActivationWorkerForTest(
		cfg,
		journeys,
		&activationWorkerEvidenceRepoStub{},
		&activationWorkerLifecycleStub{},
		&activationWorkerUserReaderStub{},
		&activationWorkerEmailSenderStub{},
	)

	worker.Start()
	worker.Stop()

	require.Zero(t, journeys.eligibleCalls)
	require.Zero(t, journeys.dueCalls)
}

func activationWorkerConfig(now time.Time) config.UserActivationConfig {
	return config.UserActivationConfig{
		Enabled:          true,
		EligibleAfter:    now.Add(-24 * time.Hour),
		RecallWindowDays: 7,
		WorkerInterval:   10 * time.Minute,
		SupportWeChat:    "welsir02",
	}
}

func newActivationWorkerForTest(
	cfg config.UserActivationConfig,
	journeys UserActivationJourneyRepository,
	evidence UserActivationEvidenceRepository,
	lifecycle userActivationWorkerLifecycle,
	users userActivationWorkerUserReader,
	sender userActivationWorkerEmailSender,
) *UserActivationWorker {
	return NewUserActivationWorker(
		cfg,
		journeys,
		evidence,
		lifecycle,
		users,
		sender,
		&activationWorkerFrontendURLStub{url: "https://example.com"},
	)
}

type activationWorkerLifecycleStub struct {
	mu               sync.Mutex
	bootstrapErrors  map[int64]error
	bootstrapUserIDs []int64
	evaluateCalls    map[int64]int
}

func (s *activationWorkerLifecycleStub) BootstrapVerifiedRegistration(
	_ context.Context,
	user *User,
	_ string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bootstrapUserIDs = append(s.bootstrapUserIDs, user.ID)
	return s.bootstrapErrors[user.ID]
}

func (s *activationWorkerLifecycleStub) Evaluate(
	_ context.Context,
	userID int64,
	_ time.Time,
) (*UserActivationStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.evaluateCalls == nil {
		s.evaluateCalls = make(map[int64]int)
	}
	s.evaluateCalls[userID]++
	return &UserActivationStatus{Enabled: true}, nil
}

type activationWorkerJourneyRepoStub struct {
	mu sync.Mutex

	eligibleUsers    []User
	dueJourneys      []UserActivationJourney
	byUserID         map[int64]UserActivationJourney
	eligibleAfterIDs []int64
	dueAfterIDs      []int64
	eligibleCalls    int
	dueCalls         int
	marked           []activationWorkerMarkedEmail

	eligibleEntered chan struct{}
	eligibleRelease chan struct{}
	enterOnce       sync.Once
}

type activationWorkerMarkedEmail struct {
	journeyID int64
	stage     UserActivationEmailStage
	sentAt    time.Time
}

func (s *activationWorkerJourneyRepoStub) CreateIfAbsent(
	context.Context,
	int64,
	string,
) (*UserActivationJourney, bool, error) {
	return nil, false, nil
}

func (s *activationWorkerJourneyRepoStub) GetByUserID(
	_ context.Context,
	userID int64,
) (*UserActivationJourney, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	journey, ok := s.byUserID[userID]
	if !ok {
		return nil, ErrUserActivationJourneyNotFound
	}
	copy := journey
	return &copy, nil
}

func (s *activationWorkerJourneyRepoStub) GetByUserIDForUpdate(
	context.Context,
	int64,
) (*UserActivationJourney, error) {
	return nil, ErrUserActivationJourneyNotFound
}

func (s *activationWorkerJourneyRepoStub) Update(
	context.Context,
	*UserActivationJourney,
) error {
	return nil
}

func (s *activationWorkerJourneyRepoStub) MarkEmailSent(
	_ context.Context,
	journeyID int64,
	stage UserActivationEmailStage,
	sentAt time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marked = append(s.marked, activationWorkerMarkedEmail{
		journeyID: journeyID,
		stage:     stage,
		sentAt:    sentAt,
	})
	for userID, journey := range s.byUserID {
		if journey.ID != journeyID {
			continue
		}
		journey.LastEmailSentAt = timePointer(sentAt)
		switch stage {
		case UserActivationEmailStageNoAttempt:
			journey.NoAttemptEmailSentAt = timePointer(sentAt)
		case UserActivationEmailStageAttempted:
			journey.AttemptedEmailSentAt = timePointer(sentAt)
		case UserActivationEmailStagePaidSupport:
			journey.PaidSupportEmailSentAt = timePointer(sentAt)
		case UserActivationEmailStageRecallAvailable:
			journey.RecallAvailableEmailSentAt = timePointer(sentAt)
		case UserActivationEmailStageRecallExpired:
			journey.RecallExpiredEmailSentAt = timePointer(sentAt)
		}
		s.byUserID[userID] = journey
		break
	}
	return nil
}

func (s *activationWorkerJourneyRepoStub) ListDue(
	_ context.Context,
	_ time.Time,
	afterID int64,
	_ int,
) ([]UserActivationJourney, error) {
	s.mu.Lock()
	s.dueCalls++
	s.dueAfterIDs = append(s.dueAfterIDs, afterID)
	defer s.mu.Unlock()
	for _, journey := range s.dueJourneys {
		if journey.ID > afterID {
			return []UserActivationJourney{journey}, nil
		}
	}
	return []UserActivationJourney{}, nil
}

func (s *activationWorkerJourneyRepoStub) ListEligibleUsersWithoutJourney(
	_ context.Context,
	_ time.Time,
	afterID int64,
	_ int,
) ([]User, error) {
	s.mu.Lock()
	s.eligibleCalls++
	s.eligibleAfterIDs = append(s.eligibleAfterIDs, afterID)
	s.enterOnce.Do(func() {
		if s.eligibleEntered != nil {
			close(s.eligibleEntered)
		}
	})
	release := s.eligibleRelease
	s.mu.Unlock()
	if release != nil {
		<-release
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.eligibleUsers {
		if user.ID > afterID {
			return []User{user}, nil
		}
	}
	return []User{}, nil
}

type activationWorkerEvidenceRepoStub struct {
	mu       sync.Mutex
	defaults map[int64]*UserActivationEvidence
	sequence map[int64][]*UserActivationEvidence
	calls    map[int64]int
}

func (s *activationWorkerEvidenceRepoStub) Snapshot(
	_ context.Context,
	userID int64,
) (*UserActivationEvidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = make(map[int64]int)
	}
	index := s.calls[userID]
	s.calls[userID]++
	if values := s.sequence[userID]; len(values) > 0 {
		if index >= len(values) {
			index = len(values) - 1
		}
		copy := *values[index]
		return &copy, nil
	}
	if value := s.defaults[userID]; value != nil {
		copy := *value
		return &copy, nil
	}
	return &UserActivationEvidence{}, nil
}

type activationWorkerUserReaderStub struct {
	users map[int64]*User
}

func (s *activationWorkerUserReaderStub) GetByID(
	_ context.Context,
	userID int64,
) (*User, error) {
	user := s.users[userID]
	if user == nil {
		return nil, ErrUserNotFound
	}
	copy := *user
	return &copy, nil
}

type activationWorkerEmailSenderStub struct {
	mu     sync.Mutex
	inputs []NotificationEmailSendInput
	err    error
}

func (s *activationWorkerEmailSenderStub) Send(
	_ context.Context,
	input NotificationEmailSendInput,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, input)
	return s.err
}

type activationWorkerFrontendURLStub struct {
	url string
}

func (s *activationWorkerFrontendURLStub) GetFrontendURL(context.Context) string {
	return s.url
}

type activationSplitBrainCache struct {
	mu           sync.Mutex
	owners       map[string]string
	failedOwners map[string]bool
}

func (c *activationSplitBrainCache) TryAcquireLeaderLock(
	_ context.Context,
	key string,
	owner string,
	_ time.Duration,
) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failedOwners[owner] {
		return false, errors.New("redis unavailable")
	}
	if c.owners == nil {
		c.owners = make(map[string]string)
	}
	if _, exists := c.owners[key]; exists {
		return false, nil
	}
	c.owners[key] = owner
	return true, nil
}

func (c *activationSplitBrainCache) ReleaseLeaderLock(
	_ context.Context,
	key string,
	owner string,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.owners[key] == owner {
		delete(c.owners, key)
	}
	return nil
}
