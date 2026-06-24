//go:build unit

package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthAppliesEffectiveOpenAIGroupFromRequestModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	anthropicGroup := service.Group{ID: 1, Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
	openAIGroup := service.Group{ID: 2, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, IsExclusive: true, SubscriptionType: service.SubscriptionTypeStandard}
	user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 10, AllowedGroups: []int64{openAIGroup.ID}}
	apiKey := &service.APIKey{
		ID:       100,
		UserID:   user.ID,
		Key:      "omni-key",
		Status:   service.StatusActive,
		User:     user,
		GroupID:  &anthropicGroup.ID,
		GroupIDs: []int64{anthropicGroup.ID, openAIGroup.ID},
		Group:    &anthropicGroup,
	}
	apiKeyService := service.NewAPIKeyService(
		fakeAPIKeyRepo{getByKey: cloneAPIKeyForEffectiveGroupTest(apiKey)},
		&effectiveAuthUserRepoStub{user: user},
		&effectiveAuthGroupRepoStub{active: []service.Group{anthropicGroup, openAIGroup}},
		nil,
		nil,
		nil,
		&config.Config{RunMode: config.RunModeSimple},
	)

	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, nil, &config.Config{RunMode: config.RunModeSimple})))
	router.POST("/v1/responses", func(c *gin.Context) {
		requestAPIKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, requestAPIKey.GroupID)
		require.Equal(t, openAIGroup.ID, *requestAPIKey.GroupID)
		require.NotNil(t, requestAPIKey.Group)
		require.Equal(t, service.PlatformOpenAI, requestAPIKey.Group.Platform)

		groupFromContext, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group)
		require.True(t, ok)
		require.Equal(t, openAIGroup.ID, groupFromContext.ID)

		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "gpt-5.4")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"hi"}`))
	req.Header.Set("x-api-key", apiKey.Key)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, anthropicGroup.ID, *apiKey.GroupID, "effective group must not mutate the cached API key")
}

func TestAPIKeyAuthWithSubscriptionGoogleAppliesEffectiveGeminiGroupFromNativePath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	anthropicGroup := service.Group{ID: 1, Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
	geminiGroup := service.Group{ID: 3, Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true, IsExclusive: true, SubscriptionType: service.SubscriptionTypeStandard}
	user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 10, AllowedGroups: []int64{geminiGroup.ID}}
	apiKey := &service.APIKey{
		ID:       100,
		UserID:   user.ID,
		Key:      "omni-key",
		Status:   service.StatusActive,
		User:     user,
		GroupID:  &anthropicGroup.ID,
		GroupIDs: []int64{anthropicGroup.ID, geminiGroup.ID},
		Group:    &anthropicGroup,
	}
	apiKeyService := service.NewAPIKeyService(
		fakeAPIKeyRepo{getByKey: cloneAPIKeyForEffectiveGroupTest(apiKey)},
		&effectiveAuthUserRepoStub{user: user},
		&effectiveAuthGroupRepoStub{active: []service.Group{anthropicGroup, geminiGroup}},
		nil,
		nil,
		nil,
		&config.Config{RunMode: config.RunModeSimple},
	)

	router := gin.New()
	router.Use(APIKeyAuthWithSubscriptionGoogle(apiKeyService, nil, &config.Config{RunMode: config.RunModeSimple}))
	router.POST("/v1beta/models/gemini-2.5-pro:generateContent", func(c *gin.Context) {
		requestAPIKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, requestAPIKey.GroupID)
		require.Equal(t, geminiGroup.ID, *requestAPIKey.GroupID)
		require.NotNil(t, requestAPIKey.Group)
		require.Equal(t, service.PlatformGemini, requestAPIKey.Group.Platform)

		groupFromContext, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group)
		require.True(t, ok)
		require.Equal(t, geminiGroup.ID, groupFromContext.ID)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", strings.NewReader(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
	req.Header.Set("x-goog-api-key", apiKey.Key)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, anthropicGroup.ID, *apiKey.GroupID, "effective group must not mutate the cached API key")
}

func TestAPIKeyAuthKeepsBoundGroupWhenPlatformGroupIsNotSelectedByKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	anthropicGroup := service.Group{ID: 1, Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
	geminiGroup := service.Group{ID: 3, Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true, IsExclusive: true, SubscriptionType: service.SubscriptionTypeStandard}
	user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 10, AllowedGroups: []int64{geminiGroup.ID}}
	apiKey := &service.APIKey{
		ID:       100,
		UserID:   user.ID,
		Key:      "omni-key",
		Status:   service.StatusActive,
		User:     user,
		GroupID:  &anthropicGroup.ID,
		GroupIDs: []int64{anthropicGroup.ID},
		Group:    &anthropicGroup,
	}
	apiKeyService := service.NewAPIKeyService(
		fakeAPIKeyRepo{getByKey: cloneAPIKeyForEffectiveGroupTest(apiKey)},
		&effectiveAuthUserRepoStub{user: user},
		&effectiveAuthGroupRepoStub{active: []service.Group{anthropicGroup, geminiGroup}},
		nil,
		nil,
		nil,
		&config.Config{RunMode: config.RunModeSimple},
	)

	router := gin.New()
	router.Use(APIKeyAuthWithSubscriptionGoogle(apiKeyService, nil, &config.Config{RunMode: config.RunModeSimple}))
	router.POST("/v1beta/models/gemini-2.5-pro:generateContent", func(c *gin.Context) {
		requestAPIKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, requestAPIKey.GroupID)
		require.Equal(t, anthropicGroup.ID, *requestAPIKey.GroupID)
		require.NotNil(t, requestAPIKey.Group)
		require.Equal(t, service.PlatformAnthropic, requestAPIKey.Group.Platform)

		groupFromContext, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group)
		require.True(t, ok)
		require.Equal(t, anthropicGroup.ID, groupFromContext.ID)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", strings.NewReader(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
	req.Header.Set("x-goog-api-key", apiKey.Key)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func cloneAPIKeyForEffectiveGroupTest(apiKey *service.APIKey) func(context.Context, string) (*service.APIKey, error) {
	return func(_ context.Context, key string) (*service.APIKey, error) {
		if key != apiKey.Key {
			return nil, service.ErrAPIKeyNotFound
		}
		clone := *apiKey
		if apiKey.User != nil {
			userClone := *apiKey.User
			clone.User = &userClone
		}
		if apiKey.Group != nil {
			groupClone := *apiKey.Group
			clone.Group = &groupClone
		}
		return &clone, nil
	}
}

type effectiveAuthUserRepoStub struct {
	user *service.User
}

func (s *effectiveAuthUserRepoStub) Create(context.Context, *service.User) error { return nil }
func (s *effectiveAuthUserRepoStub) GetByID(context.Context, int64) (*service.User, error) {
	if s.user == nil {
		return nil, service.ErrUserNotFound
	}
	clone := *s.user
	return &clone, nil
}
func (s *effectiveAuthUserRepoStub) GetByIDIncludeDeleted(ctx context.Context, id int64) (*service.User, error) {
	return s.GetByID(ctx, id)
}
func (s *effectiveAuthUserRepoStub) GetByEmail(context.Context, string) (*service.User, error) {
	return nil, service.ErrUserNotFound
}
func (s *effectiveAuthUserRepoStub) GetFirstAdmin(context.Context) (*service.User, error) {
	return nil, service.ErrUserNotFound
}
func (s *effectiveAuthUserRepoStub) Update(context.Context, *service.User) error { return nil }
func (s *effectiveAuthUserRepoStub) Delete(context.Context, int64) error         { return nil }
func (s *effectiveAuthUserRepoStub) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}
func (s *effectiveAuthUserRepoStub) UpsertUserAvatar(context.Context, int64, service.UpsertUserAvatarInput) (*service.UserAvatar, error) {
	return nil, nil
}
func (s *effectiveAuthUserRepoStub) DeleteUserAvatar(context.Context, int64) error {
	return nil
}
func (s *effectiveAuthUserRepoStub) List(context.Context, pagination.PaginationParams) ([]service.User, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *effectiveAuthUserRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, service.UserListFilters) ([]service.User, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *effectiveAuthUserRepoStub) GetLatestUsedAtByUserIDs(context.Context, []int64) (map[int64]*time.Time, error) {
	return nil, nil
}
func (s *effectiveAuthUserRepoStub) GetLatestUsedAtByUserID(context.Context, int64) (*time.Time, error) {
	return nil, nil
}
func (s *effectiveAuthUserRepoStub) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	return nil
}
func (s *effectiveAuthUserRepoStub) UpdateBalance(context.Context, int64, float64) error {
	return nil
}
func (s *effectiveAuthUserRepoStub) DeductBalance(context.Context, int64, float64) error {
	return nil
}
func (s *effectiveAuthUserRepoStub) UpdateConcurrency(context.Context, int64, int) error {
	return nil
}
func (s *effectiveAuthUserRepoStub) BatchAddConcurrency(context.Context, []int64, int) (int, error) {
	return 0, nil
}
func (s *effectiveAuthUserRepoStub) BatchSetConcurrency(context.Context, []int64, int) (int, error) {
	return 0, nil
}
func (s *effectiveAuthUserRepoStub) ExistsByEmail(context.Context, string) (bool, error) {
	return false, nil
}
func (s *effectiveAuthUserRepoStub) RemoveGroupFromAllowedGroups(context.Context, int64) (int64, error) {
	return 0, nil
}
func (s *effectiveAuthUserRepoStub) AddGroupToAllowedGroups(context.Context, int64, int64) error {
	return nil
}
func (s *effectiveAuthUserRepoStub) RemoveGroupFromUserAllowedGroups(context.Context, int64, int64) error {
	return nil
}
func (s *effectiveAuthUserRepoStub) ListUserAuthIdentities(context.Context, int64) ([]service.UserAuthIdentityRecord, error) {
	return nil, nil
}
func (s *effectiveAuthUserRepoStub) UnbindUserAuthProvider(context.Context, int64, string) error {
	return nil
}
func (s *effectiveAuthUserRepoStub) UpdateTotpSecret(context.Context, int64, *string) error {
	return nil
}
func (s *effectiveAuthUserRepoStub) EnableTotp(context.Context, int64) error {
	return nil
}
func (s *effectiveAuthUserRepoStub) DisableTotp(context.Context, int64) error {
	return nil
}

type effectiveAuthGroupRepoStub struct {
	active []service.Group
}

func (s *effectiveAuthGroupRepoStub) Create(context.Context, *service.Group) error { return nil }
func (s *effectiveAuthGroupRepoStub) GetByID(context.Context, int64) (*service.Group, error) {
	return nil, service.ErrGroupNotFound
}
func (s *effectiveAuthGroupRepoStub) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return nil, service.ErrGroupNotFound
}
func (s *effectiveAuthGroupRepoStub) Update(context.Context, *service.Group) error { return nil }
func (s *effectiveAuthGroupRepoStub) Delete(context.Context, int64) error          { return nil }
func (s *effectiveAuthGroupRepoStub) DeleteCascade(context.Context, int64) ([]int64, error) {
	return nil, nil
}
func (s *effectiveAuthGroupRepoStub) List(context.Context, pagination.PaginationParams) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *effectiveAuthGroupRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *effectiveAuthGroupRepoStub) ListActive(context.Context) ([]service.Group, error) {
	out := make([]service.Group, len(s.active))
	copy(out, s.active)
	return out, nil
}
func (s *effectiveAuthGroupRepoStub) ListActiveByPlatform(context.Context, string) ([]service.Group, error) {
	return nil, nil
}
func (s *effectiveAuthGroupRepoStub) ExistsByName(context.Context, string) (bool, error) {
	return false, nil
}
func (s *effectiveAuthGroupRepoStub) GetAccountCount(context.Context, int64) (int64, int64, error) {
	return 0, 0, nil
}
func (s *effectiveAuthGroupRepoStub) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (s *effectiveAuthGroupRepoStub) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return nil, nil
}
func (s *effectiveAuthGroupRepoStub) BindAccountsToGroup(context.Context, int64, []int64) error {
	return nil
}
func (s *effectiveAuthGroupRepoStub) UpdateSortOrders(context.Context, []service.GroupSortOrderUpdate) error {
	return nil
}
