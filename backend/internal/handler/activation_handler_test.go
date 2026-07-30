// [INPUT]: Gin test contexts, activation service doubles, JWT subjects, and idempotency coordination.
// [OUTPUT]: HTTP contract proof for public offers, user-scoped status, and recall claims.
// [POS]: Handler-level acceptance tests for the HVOY activation API.
package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type activationServiceStub struct {
	getStatus func(context.Context, int64) (*service.UserActivationStatus, error)
	claim     func(context.Context, int64) (*service.UserActivationStatus, error)
}

func (s *activationServiceStub) GetStatus(
	ctx context.Context,
	userID int64,
) (*service.UserActivationStatus, error) {
	return s.getStatus(ctx, userID)
}

func (s *activationServiceStub) ClaimRecall(
	ctx context.Context,
	userID int64,
) (*service.UserActivationStatus, error) {
	return s.claim(ctx, userID)
}

func activationRequest(
	t *testing.T,
	method string,
	path string,
	handler gin.HandlerFunc,
	userID *int64,
	idempotencyKey string,
) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	if userID != nil {
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: *userID})
			c.Next()
		})
	}
	routePath, _, _ := strings.Cut(path, "?")
	router.Handle(method, routePath, handler)
	request := httptest.NewRequest(method, path, nil)
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func replaceDefaultIdempotencyCoordinator(
	t *testing.T,
	coordinator *service.IdempotencyCoordinator,
) {
	t.Helper()
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(coordinator)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(previous)
	})
}

func TestActivationHandlerPublicOfferEnabledUsesFixedContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewActivationHandler(&activationServiceStub{}, config.UserActivationConfig{
		Enabled:          true,
		RecallWindowDays: 7,
		SupportWeChat:    "welsir02",
	})

	recorder := activationRequest(
		t,
		http.MethodGet,
		"/api/v1/public/activation/hvoy",
		h.GetPublicOffer,
		nil,
		"",
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{
		"code": 0,
		"message": "success",
		"data": {
			"enabled": true,
			"starter_credit_usd": 1,
			"starter_valid_hours": 24,
			"recall_credit_usd": 2,
			"recall_valid_hours": 24,
			"recall_window_days": 7,
			"minimum_recharge_cny": 10,
			"recharge_credit_rate": 1,
			"pro_rate_multiplier": 0.2,
			"support_wechat": "welsir02"
		}
	}`, recorder.Body.String())
}

func TestActivationHandlerPublicOfferDisabledDoesNotLeakGrantTerms(t *testing.T) {
	h := NewActivationHandler(&activationServiceStub{}, config.UserActivationConfig{})

	recorder := activationRequest(
		t,
		http.MethodGet,
		"/api/v1/public/activation/hvoy",
		h.GetPublicOffer,
		nil,
		"",
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{
		"code": 0,
		"message": "success",
		"data": {"enabled": false}
	}`, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "starter_credit_usd")
	require.NotContains(t, recorder.Body.String(), "support_wechat")
}

func TestActivationHandlerPublicOfferDoesNotDependOnActivationService(t *testing.T) {
	h := NewActivationHandler(nil, config.UserActivationConfig{
		Enabled:          true,
		RecallWindowDays: 7,
		SupportWeChat:    "welsir02",
	})

	recorder := activationRequest(
		t,
		http.MethodGet,
		"/api/v1/public/activation/hvoy",
		h.GetPublicOffer,
		nil,
		"",
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{
		"code": 0,
		"message": "success",
		"data": {
			"enabled": true,
			"starter_credit_usd": 1,
			"starter_valid_hours": 24,
			"recall_credit_usd": 2,
			"recall_valid_hours": 24,
			"recall_window_days": 7,
			"minimum_recharge_cny": 10,
			"recharge_credit_rate": 1,
			"pro_rate_multiplier": 0.2,
			"support_wechat": "welsir02"
		}
	}`, recorder.Body.String())
}

func TestActivationHandlerStatusUsesAuthenticatedSubjectOnly(t *testing.T) {
	var gotUserID int64
	expected := &service.UserActivationStatus{
		Enabled:       true,
		Segment:       service.UserActivationSegmentRegistered,
		Starter:       service.ActivationGrantStatus{State: "granted"},
		Recall:        service.ActivationRecallStatus{State: "locked"},
		SupportWeChat: "welsir02",
		NextAction:    "use_trial",
	}
	h := NewActivationHandler(&activationServiceStub{
		getStatus: func(_ context.Context, userID int64) (*service.UserActivationStatus, error) {
			gotUserID = userID
			return expected, nil
		},
	}, config.UserActivationConfig{Enabled: true})
	userID := int64(42)

	recorder := activationRequest(
		t,
		http.MethodGet,
		"/api/v1/user/activation?user_id=999",
		h.GetStatus,
		&userID,
		"",
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, userID, gotUserID)
	require.Contains(t, recorder.Body.String(), `"segment":"REGISTERED_NO_ATTEMPT"`)
	require.Contains(t, recorder.Body.String(), `"next_action":"use_trial"`)
}

func TestActivationHandlerStatusRequiresAuthenticatedSubject(t *testing.T) {
	var called atomic.Bool
	h := NewActivationHandler(&activationServiceStub{
		getStatus: func(context.Context, int64) (*service.UserActivationStatus, error) {
			called.Store(true)
			return nil, nil
		},
	}, config.UserActivationConfig{Enabled: true})

	recorder := activationRequest(
		t,
		http.MethodGet,
		"/api/v1/user/activation",
		h.GetStatus,
		nil,
		"",
	)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.False(t, called.Load())
}

func TestActivationHandlerRecallClaimRequiresIdempotencyKey(t *testing.T) {
	replaceDefaultIdempotencyCoordinator(t, nil)
	var called atomic.Bool
	h := NewActivationHandler(&activationServiceStub{
		claim: func(context.Context, int64) (*service.UserActivationStatus, error) {
			called.Store(true)
			return nil, nil
		},
	}, config.UserActivationConfig{Enabled: true})
	userID := int64(43)

	recorder := activationRequest(
		t,
		http.MethodPost,
		"/api/v1/user/activation/recall/claim",
		h.ClaimRecall,
		&userID,
		"",
	)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"reason":"IDEMPOTENCY_KEY_REQUIRED"`)
	require.False(t, called.Load())
}

func TestActivationHandlerRecallClaimReplaysSameKey(t *testing.T) {
	repo := newUserMemoryIdempotencyRepoStub()
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	replaceDefaultIdempotencyCoordinator(
		t,
		service.NewIdempotencyCoordinator(repo, cfg),
	)

	var claims atomic.Int32
	h := NewActivationHandler(&activationServiceStub{
		claim: func(context.Context, int64) (*service.UserActivationStatus, error) {
			claims.Add(1)
			return &service.UserActivationStatus{
				Enabled: true,
				Recall:  service.ActivationRecallStatus{State: "claimed"},
			}, nil
		},
	}, config.UserActivationConfig{Enabled: true})
	userID := int64(44)

	first := activationRequest(
		t,
		http.MethodPost,
		"/api/v1/user/activation/recall/claim",
		h.ClaimRecall,
		&userID,
		"activation-replay",
	)
	replayed := activationRequest(
		t,
		http.MethodPost,
		"/api/v1/user/activation/recall/claim",
		h.ClaimRecall,
		&userID,
		"activation-replay",
	)

	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, http.StatusOK, replayed.Code)
	require.Equal(t, "true", replayed.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, int32(1), claims.Load())
}

func TestActivationHandlerRecallClaimDifferentKeysReachServiceIndependently(t *testing.T) {
	repo := newUserMemoryIdempotencyRepoStub()
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	replaceDefaultIdempotencyCoordinator(
		t,
		service.NewIdempotencyCoordinator(repo, cfg),
	)

	var calls atomic.Int32
	h := NewActivationHandler(&activationServiceStub{
		claim: func(context.Context, int64) (*service.UserActivationStatus, error) {
			calls.Add(1)
			return &service.UserActivationStatus{
				Enabled: true,
				Recall:  service.ActivationRecallStatus{State: "claimed"},
			}, nil
		},
	}, config.UserActivationConfig{Enabled: true})
	userID := int64(45)

	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for _, key := range []string{"activation-concurrent-a", "activation-concurrent-b"} {
		wait.Add(1)
		go func(idempotencyKey string) {
			defer wait.Done()
			<-start
			results <- activationRequest(
				t,
				http.MethodPost,
				"/api/v1/user/activation/recall/claim",
				h.ClaimRecall,
				&userID,
				idempotencyKey,
			)
		}(key)
	}
	close(start)
	wait.Wait()
	close(results)

	for result := range results {
		require.Equal(t, http.StatusOK, result.Code)
		require.Contains(t, result.Body.String(), `"state":"claimed"`)
	}
	// The service/PostgreSQL concurrency tests own the single-subscription guarantee.
	require.Equal(t, int32(2), calls.Load())
}

func TestActivationHandlerRecallClaimPaidUserReturnsBusinessConflict(t *testing.T) {
	// Subscription non-creation is covered by the activation service tests.
	replaceDefaultIdempotencyCoordinator(t, nil)
	h := NewActivationHandler(&activationServiceStub{
		claim: func(context.Context, int64) (*service.UserActivationStatus, error) {
			return &service.UserActivationStatus{
					Enabled: true,
					Segment: service.UserActivationSegmentPaidZeroSuccess,
					Recall:  service.ActivationRecallStatus{State: "blocked_paid"},
				}, infraerrors.Conflict(
					"USER_ACTIVATION_RECALL_UNAVAILABLE",
					"recall subscription is not claimable",
				).WithMetadata(map[string]string{"recall_state": "blocked_paid"})
		},
	}, config.UserActivationConfig{Enabled: true})
	userID := int64(46)

	recorder := activationRequest(
		t,
		http.MethodPost,
		"/api/v1/user/activation/recall/claim",
		h.ClaimRecall,
		&userID,
		"activation-paid",
	)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"reason":"USER_ACTIVATION_RECALL_UNAVAILABLE"`)
	require.Contains(t, recorder.Body.String(), `"recall_state":"blocked_paid"`)
}
