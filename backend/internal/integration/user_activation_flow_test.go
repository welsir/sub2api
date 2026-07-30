//go:build integration

// [INPUT]: Real PostgreSQL migrations, activation services, repositories, and worker orchestration.
// [OUTPUT]: End-to-end proof for direct-registration starter, recall, and paid-user recovery.
// [POS]: Cross-layer integration contract for the public new-user activation journey.
//
// [PROTOCOL]:
// 1. Update this header when the activation journey contract or integration harness changes.
// 2. Keep external email delivery and production infrastructure outside this test.
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	dbsubscription "github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const activationFlowPostgresImage = "postgres:18.1-alpine3.23"

func TestUserActivationFlowStarterRecallSuccessPostgres(t *testing.T) {
	harness := newUserActivationFlowHarness(t)
	harness.verifyStarterRecallSuccess(t)
}

func TestUserActivationFlowPaidWithoutSuccessPostgres(t *testing.T) {
	harness := newUserActivationFlowHarness(t)
	harness.verifyPaidWithoutSuccess(t)
}

type userActivationFlowHarness struct {
	ctx             context.Context
	db              *sql.DB
	client          *dbent.Client
	cfg             config.UserActivationConfig
	journeys        service.UserActivationJourneyRepository
	evidence        service.UserActivationEvidenceRepository
	subscriptions   *service.SubscriptionService
	activation      *service.UserActivationService
	auth            *service.AuthService
	users           service.UserRepository
	emailCache      *activationFlowEmailCache
	starterGroupID  int64
	recallGroupID   int64
	evidenceAccount *dbent.Account
}

func newUserActivationFlowHarness(t *testing.T) *userActivationFlowHarness {
	t.Helper()
	require.NoError(t, timezone.Init("UTC"))

	ctx := context.Background()
	container, err := tcpostgres.Run(
		ctx,
		activationFlowPostgresImage,
		tcpostgres.WithDatabase("sub2api_activation_flow"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, container.Terminate(context.Background()))
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pingCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		return db.PingContext(pingCtx) == nil
	}, 10*time.Second, 100*time.Millisecond)
	t.Cleanup(func() {
		_ = db.Close()
	})

	require.NoError(t, repository.ApplyMigrations(ctx, db))
	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() {
		_ = client.Close()
	})

	suffix := time.Now().UnixNano()
	starterGroup, err := client.Group.Create().
		SetName(fmt.Sprintf("activation-flow-starter-%d", suffix)).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetRateMultiplier(0.2).
		SetDailyLimitUsd(1).
		Save(ctx)
	require.NoError(t, err)
	recallGroup, err := client.Group.Create().
		SetName(fmt.Sprintf("activation-flow-recall-%d", suffix)).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetRateMultiplier(0.2).
		SetDailyLimitUsd(2).
		Save(ctx)
	require.NoError(t, err)
	account, err := client.Account.Create().
		SetName(fmt.Sprintf("activation-flow-account-%d", suffix)).
		SetPlatform("openai").
		SetType("oauth").
		Save(ctx)
	require.NoError(t, err)

	appCfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:     "activation-flow-integration-secret",
			ExpireHour: 1,
		},
	}
	settingsRepo := &activationFlowSettingRepo{
		values: map[string]string{
			service.SettingKeyRegistrationEnabled:              "true",
			service.SettingKeyEmailVerifyEnabled:               "true",
			service.SettingKeyRegistrationEmailSuffixWhitelist: "example.com",
		},
	}
	settings := service.NewSettingService(settingsRepo, appCfg)
	journeys := repository.NewUserActivationJourneyRepository(client)
	evidence := repository.NewUserActivationEvidenceRepository(client, db)
	subscriptions := service.NewSubscriptionService(
		repository.NewGroupRepository(client, db),
		repository.NewUserSubscriptionRepository(client),
		nil,
		client,
		appCfg,
	)
	t.Cleanup(subscriptions.Stop)
	activationCfg := config.UserActivationConfig{
		Enabled:          true,
		EligibleAfter:    time.Now().UTC().Add(-time.Hour),
		StarterGroupID:   starterGroup.ID,
		RecallGroupID:    recallGroup.ID,
		RecallWindowDays: 7,
		WorkerInterval:   time.Hour,
		SupportWeChat:    "welsir02",
	}
	activation := service.NewUserActivationService(
		activationCfg,
		journeys,
		evidence,
		subscriptions,
		settings,
		client,
	)
	users := repository.NewUserRepository(client, db)
	emailCache := newActivationFlowEmailCache()
	emailService := service.NewEmailService(settingsRepo, emailCache)
	auth := service.NewAuthService(
		client,
		users,
		nil,
		nil,
		appCfg,
		settings,
		emailService,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	auth.SetUserActivationBootstrapper(activation)

	return &userActivationFlowHarness{
		ctx:             ctx,
		db:              db,
		client:          client,
		cfg:             activationCfg,
		journeys:        journeys,
		evidence:        evidence,
		subscriptions:   subscriptions,
		activation:      activation,
		auth:            auth,
		users:           users,
		emailCache:      emailCache,
		starterGroupID:  starterGroup.ID,
		recallGroupID:   recallGroup.ID,
		evidenceAccount: account,
	}
}

func (h *userActivationFlowHarness) verifyStarterRecallSuccess(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	user := h.registerVerifiedDirectUser(t, "starter-recall")

	journey, err := h.journeys.GetByUserID(h.ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, "direct", journey.CampaignSource)
	require.Equal(t, "granted", journey.StarterState)
	require.NotNil(t, journey.StarterSubscriptionID)
	require.NotNil(t, journey.StarterGrantedAt)
	require.NotNil(t, journey.StarterExpiresAt)
	require.WithinDuration(
		t,
		journey.StarterGrantedAt.Add(24*time.Hour),
		*journey.StarterExpiresAt,
		time.Second,
	)
	require.Equal(t, 1, h.subscriptionCount(t, user.ID, h.starterGroupID))

	apiKey, err := h.client.APIKey.Create().
		SetUserID(user.ID).
		SetGroupID(h.starterGroupID).
		SetKey(fmt.Sprintf("sk-activation-flow-%d", time.Now().UnixNano())).
		SetName("Omni Trial").
		Save(h.ctx)
	require.NoError(t, err)
	h.createUsage(t, user.ID, apiKey.ID, h.starterGroupID, *journey.StarterSubscriptionID, 0, now)

	status, err := h.activation.Evaluate(h.ctx, user.ID, now)
	require.NoError(t, err)
	require.Equal(t, service.UserActivationSegmentAttemptedNoSuccess, status.Segment)
	require.Equal(t, "locked", status.Recall.State)

	require.NoError(t, h.expireStarter(t, user.ID, now))
	status, err = h.activation.Evaluate(h.ctx, user.ID, now)
	require.NoError(t, err)
	require.Equal(t, service.UserActivationSegmentAttemptedNoSuccess, status.Segment)
	require.Equal(t, "claimable", status.Recall.State)
	require.True(t, status.Recall.Claimable)

	firstClaim, err := h.activation.ClaimRecall(h.ctx, user.ID)
	require.NoError(t, err)
	replayedClaim, err := h.activation.ClaimRecall(h.ctx, user.ID)
	require.NoError(t, err)
	require.NotNil(t, firstClaim.ActiveGroup)
	require.NotNil(t, replayedClaim.ActiveGroup)
	require.Equal(t, firstClaim.ActiveGroup.SubscriptionID, replayedClaim.ActiveGroup.SubscriptionID)
	require.Equal(t, h.recallGroupID, firstClaim.ActiveGroup.GroupID)
	require.Equal(t, 1, h.subscriptionCount(t, user.ID, h.recallGroupID))

	h.createUsage(
		t,
		user.ID,
		apiKey.ID,
		h.recallGroupID,
		firstClaim.ActiveGroup.SubscriptionID,
		0.02,
		now.Add(time.Minute),
	)
	status, err = h.activation.Evaluate(h.ctx, user.ID, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.Equal(t, service.UserActivationSegmentSuccess, status.Segment)
	require.Equal(t, "closed_success", status.Recall.State)
	require.Equal(t, "purchase", status.NextAction)
	require.NotNil(t, status.FirstSuccessAt)

	h.markJourneyDue(t, user.ID, now.Add(-2*time.Hour))
	sender := newActivationFlowEmailCapture()
	worker := h.newWorker(sender)
	worker.Start()
	select {
	case input := <-sender.sent:
		worker.Stop()
		t.Fatalf("successful user received unexpected activation email: %s", input.Event)
	case <-time.After(300 * time.Millisecond):
	}
	worker.Stop()
	require.Empty(t, sender.Inputs())
}

func (h *userActivationFlowHarness) verifyPaidWithoutSuccess(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	user := h.registerVerifiedDirectUser(t, "paid-no-success")
	h.createCompletedBalanceOrder(t, user, now.Add(-time.Minute))

	status, err := h.activation.Evaluate(h.ctx, user.ID, now)
	require.NoError(t, err)
	require.Equal(t, service.UserActivationSegmentPaidZeroSuccess, status.Segment)
	require.Equal(t, "blocked_paid", status.Recall.State)
	require.False(t, status.Recall.Claimable)
	require.Equal(t, "support", status.NextAction)

	blockedStatus, err := h.activation.ClaimRecall(h.ctx, user.ID)
	require.ErrorIs(t, err, service.ErrUserActivationRecallUnavailable)
	require.NotNil(t, blockedStatus)
	require.Equal(t, service.UserActivationSegmentPaidZeroSuccess, blockedStatus.Segment)
	require.Equal(t, 0, h.subscriptionCount(t, user.ID, h.recallGroupID))

	h.markJourneyDue(t, user.ID, now.Add(-2*time.Hour))
	sender := newActivationFlowEmailCapture()
	worker := h.newWorker(sender)
	worker.Start()
	select {
	case <-sender.sent:
	case <-time.After(5 * time.Second):
		worker.Stop()
		t.Fatal("paid-without-success worker did not emit the support email")
	}
	require.Eventually(t, func() bool {
		journey, err := h.journeys.GetByUserID(h.ctx, user.ID)
		return err == nil && journey.PaidSupportEmailSentAt != nil
	}, 5*time.Second, 25*time.Millisecond)
	worker.Stop()

	inputs := sender.Inputs()
	require.Len(t, inputs, 1)
	require.Equal(t, service.NotificationEmailEventActivationPaidZeroSuccess, inputs[0].Event)
	require.Equal(t, "welsir02", inputs[0].Variables["support_wechat"])
	require.NotContains(t, inputs[0].Variables, "activation_url")
	require.Equal(t, 0, h.subscriptionCount(t, user.ID, h.recallGroupID))
}

func (h *userActivationFlowHarness) registerVerifiedDirectUser(
	t *testing.T,
	prefix string,
) *service.User {
	t.Helper()
	email := fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
	code := "123456"
	require.NoError(t, h.emailCache.SetVerificationCode(
		h.ctx,
		email,
		&service.VerificationCodeData{
			Code:      code,
			CreatedAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		},
		15*time.Minute,
	))
	token, user, err := h.auth.RegisterWithVerificationContext(
		h.ctx,
		email,
		"ActivationFlowPassword123!",
		code,
		"",
		"",
		"",
		service.RegistrationContext{},
	)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.NotNil(t, user)
	return user
}

func (h *userActivationFlowHarness) createUsage(
	t *testing.T,
	userID, apiKeyID, groupID, subscriptionID int64,
	actualCost float64,
	createdAt time.Time,
) {
	t.Helper()
	_, err := h.client.UsageLog.Create().
		SetUserID(userID).
		SetAPIKeyID(apiKeyID).
		SetAccountID(h.evidenceAccount.ID).
		SetRequestID(fmt.Sprintf("activation-flow-%d", time.Now().UnixNano())).
		SetModel("gpt-5.6").
		SetGroupID(groupID).
		SetSubscriptionID(subscriptionID).
		SetActualCost(actualCost).
		SetTotalCost(actualCost).
		SetCreatedAt(createdAt).
		Save(h.ctx)
	require.NoError(t, err)
}

func (h *userActivationFlowHarness) createCompletedBalanceOrder(
	t *testing.T,
	user *service.User,
	completedAt time.Time,
) {
	t.Helper()
	suffix := time.Now().UnixNano()
	_, err := h.client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetRechargeCode(fmt.Sprintf("ACTIVATION-%d", suffix)).
		SetOutTradeNo(fmt.Sprintf("activation_flow_%d", suffix)).
		SetPaymentType("alipay").
		SetPaymentTradeNo(fmt.Sprintf("activation-trade-%d", suffix)).
		SetOrderType("balance").
		SetStatus("COMPLETED").
		SetPaidAt(completedAt).
		SetCompletedAt(completedAt).
		SetExpiresAt(completedAt.Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("activation.integration.test").
		Save(h.ctx)
	require.NoError(t, err)
}

func (h *userActivationFlowHarness) expireStarter(
	t *testing.T,
	userID int64,
	now time.Time,
) error {
	t.Helper()
	journey, err := h.journeys.GetByUserID(h.ctx, userID)
	if err != nil {
		return err
	}
	if journey.StarterSubscriptionID == nil {
		return fmt.Errorf("starter subscription is missing")
	}
	startsAt := now.Add(-25 * time.Hour)
	expiresAt := now.Add(-time.Hour)
	_, err = h.client.UserSubscription.UpdateOneID(*journey.StarterSubscriptionID).
		SetStartsAt(startsAt).
		SetExpiresAt(expiresAt).
		Save(h.ctx)
	if err != nil {
		return err
	}
	journey.StarterState = "expired"
	journey.StarterGrantedAt = &startsAt
	journey.StarterExpiresAt = &expiresAt
	return h.journeys.Update(h.ctx, journey)
}

func (h *userActivationFlowHarness) markJourneyDue(
	t *testing.T,
	userID int64,
	evaluatedAt time.Time,
) {
	t.Helper()
	journey, err := h.journeys.GetByUserID(h.ctx, userID)
	require.NoError(t, err)
	journey.LastEvaluatedAt = &evaluatedAt
	require.NoError(t, h.journeys.Update(h.ctx, journey))
}

func (h *userActivationFlowHarness) subscriptionCount(
	t *testing.T,
	userID, groupID int64,
) int {
	t.Helper()
	count, err := h.client.UserSubscription.Query().
		Where(
			dbsubscription.UserIDEQ(userID),
			dbsubscription.GroupIDEQ(groupID),
		).
		Count(h.ctx)
	require.NoError(t, err)
	return count
}

func (h *userActivationFlowHarness) newWorker(
	sender *activationFlowEmailCapture,
) *service.UserActivationWorker {
	worker := service.NewUserActivationWorker(
		h.cfg,
		h.journeys,
		h.evidence,
		h.activation,
		h.users,
		sender,
		activationFlowFrontendURL("https://ai.welsir.com"),
	)
	worker.SetLeaderLock(nil, h.db)
	return worker
}

type activationFlowSettingRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *activationFlowSettingRepo) GetValue(
	_ context.Context,
	key string,
) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *activationFlowSettingRepo) GetMultiple(
	_ context.Context,
	keys []string,
) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

type activationFlowEmailCache struct {
	service.EmailCache

	mu    sync.Mutex
	codes map[string]*service.VerificationCodeData
}

func newActivationFlowEmailCache() *activationFlowEmailCache {
	return &activationFlowEmailCache{codes: make(map[string]*service.VerificationCodeData)}
}

func (c *activationFlowEmailCache) GetVerificationCode(
	_ context.Context,
	email string,
) (*service.VerificationCodeData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data := c.codes[email]
	if data == nil {
		return nil, nil
	}
	copy := *data
	return &copy, nil
}

func (c *activationFlowEmailCache) SetVerificationCode(
	_ context.Context,
	email string,
	data *service.VerificationCodeData,
	_ time.Duration,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	copy := *data
	c.codes[email] = &copy
	return nil
}

func (c *activationFlowEmailCache) DeleteVerificationCode(
	_ context.Context,
	email string,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.codes, email)
	return nil
}

type activationFlowEmailCapture struct {
	mu     sync.Mutex
	inputs []service.NotificationEmailSendInput
	sent   chan service.NotificationEmailSendInput
}

func newActivationFlowEmailCapture() *activationFlowEmailCapture {
	return &activationFlowEmailCapture{
		sent: make(chan service.NotificationEmailSendInput, 4),
	}
}

func (c *activationFlowEmailCapture) Send(
	_ context.Context,
	input service.NotificationEmailSendInput,
) error {
	c.mu.Lock()
	c.inputs = append(c.inputs, input)
	c.mu.Unlock()
	c.sent <- input
	return nil
}

func (c *activationFlowEmailCapture) Inputs() []service.NotificationEmailSendInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]service.NotificationEmailSendInput(nil), c.inputs...)
}

type activationFlowFrontendURL string

func (u activationFlowFrontendURL) GetFrontendURL(context.Context) string {
	return string(u)
}
