package handler

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type registerCampaignBootstrapperStub struct {
	campaignSources []string
}

func (s *registerCampaignBootstrapperStub) BootstrapVerifiedRegistration(
	_ context.Context,
	_ *service.User,
	campaignSource string,
) error {
	s.campaignSources = append(s.campaignSources, campaignSource)
	return nil
}

func TestAuthHandler_RegisterCampaignSourcePassesThrough(t *testing.T) {
	const (
		email = "campaign@example.com"
		code  = "246810"
	)
	handler := newRegisterCampaignTestHandler(t, email, code)
	bootstrapper := &registerCampaignBootstrapperStub{}
	handler.authService.SetUserActivationBootstrapper(bootstrapper)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/register",
		bytes.NewBufferString(`{
			"email":"campaign@example.com",
			"password":"secret-123",
			"verify_code":"246810",
			"campaign_source":"hvoy_partner"
		}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Register(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, []string{"hvoy_partner"}, bootstrapper.campaignSources)
}

func newRegisterCampaignTestHandler(t *testing.T, email, code string) *AuthHandler {
	t.Helper()

	db, err := sql.Open("sqlite", "file:auth_handler_activation?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:                   "test-secret",
			ExpireHour:               1,
			AccessTokenExpireMinutes: 60,
			RefreshTokenExpireDays:   7,
		},
		Default: config.DefaultConfig{
			UserConcurrency: 1,
		},
	}
	settingValues := map[string]string{
		service.SettingKeyRegistrationEnabled:              "true",
		service.SettingKeyEmailVerifyEnabled:               "true",
		service.SettingKeyRegistrationEmailSuffixWhitelist: "[]",
	}
	settingSvc := service.NewSettingService(
		&oauthPendingFlowSettingRepoStub{values: settingValues},
		cfg,
	)
	emailCache := &oauthPendingFlowEmailCacheStub{
		verificationCodes: map[string]*service.VerificationCodeData{
			email: {
				Code:      code,
				CreatedAt: time.Now().UTC(),
				ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
			},
		},
	}
	authSvc := service.NewAuthService(
		client,
		repository.NewUserRepository(client, db),
		nil,
		&oauthPendingFlowRefreshTokenCacheStub{},
		cfg,
		settingSvc,
		service.NewEmailService(&oauthPendingFlowSettingRepoStub{values: settingValues}, emailCache),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	return &AuthHandler{
		authService: authSvc,
		settingSvc:  settingSvc,
	}
}
