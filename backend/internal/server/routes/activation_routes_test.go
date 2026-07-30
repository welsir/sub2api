// [INPUT]: Gin route registration, activation handlers, and JWT middleware doubles.
// [OUTPUT]: Route and authentication-boundary proof for activation endpoints.
// [POS]: Server-route contract tests for the HVOY activation API.
package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type activationRouteServiceStub struct{}

func (*activationRouteServiceStub) GetStatus(
	context.Context,
	int64,
) (*service.UserActivationStatus, error) {
	return &service.UserActivationStatus{Enabled: true}, nil
}

func (*activationRouteServiceStub) ClaimRecall(
	context.Context,
	int64,
) (*service.UserActivationStatus, error) {
	return &service.UserActivationStatus{Enabled: true}, nil
}

func TestActivationRoutesExposePublicAndAuthenticatedEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	activationHandler := handler.NewActivationHandler(
		&activationRouteServiceStub{},
		config.UserActivationConfig{
			Enabled:          true,
			RecallWindowDays: 7,
			SupportWeChat:    "welsir02",
		},
	)
	handlers := &handler.Handlers{Activation: activationHandler}
	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 51})
		c.Next()
	})

	RegisterCommonRoutes(router, activationHandler)
	RegisterUserRoutes(router.Group("/api/v1"), handlers, jwtAuth, nil)

	registered := map[string]bool{}
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	require.True(t, registered["GET /api/v1/public/activation/hvoy"])
	require.True(t, registered["GET /api/v1/user/activation"])
	require.True(t, registered["POST /api/v1/user/activation/recall/claim"])

	publicRecorder := httptest.NewRecorder()
	router.ServeHTTP(
		publicRecorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/public/activation/hvoy", nil),
	)
	require.Equal(t, http.StatusOK, publicRecorder.Code)

	statusRecorder := httptest.NewRecorder()
	router.ServeHTTP(
		statusRecorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/user/activation", nil),
	)
	require.Equal(t, http.StatusUnauthorized, statusRecorder.Code)

	claimRecorder := httptest.NewRecorder()
	router.ServeHTTP(
		claimRecorder,
		httptest.NewRequest(http.MethodPost, "/api/v1/user/activation/recall/claim", nil),
	)
	require.Equal(t, http.StatusUnauthorized, claimRecorder.Code)
}
