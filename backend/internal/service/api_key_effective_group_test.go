//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyServiceResolveEffectiveGroupForPlatformUsesEligibleUserGroup(t *testing.T) {
	ctx := context.Background()
	anthropicGroup := Group{
		ID:               1,
		Name:             "default-anthropic",
		Platform:         PlatformAnthropic,
		Status:           StatusActive,
		Hydrated:         true,
		SubscriptionType: SubscriptionTypeStandard,
	}
	openAIGroup := Group{
		ID:               2,
		Name:             "omni-openai",
		Platform:         PlatformOpenAI,
		Status:           StatusActive,
		Hydrated:         true,
		IsExclusive:      true,
		SubscriptionType: SubscriptionTypeStandard,
	}
	geminiGroup := Group{
		ID:               3,
		Name:             "omni-gemini",
		Platform:         PlatformGemini,
		Status:           StatusActive,
		Hydrated:         true,
		IsExclusive:      true,
		SubscriptionType: SubscriptionTypeSubscription,
	}
	user := &User{
		ID:            7,
		Status:        StatusActive,
		AllowedGroups: []int64{openAIGroup.ID},
	}
	apiKey := &APIKey{
		ID:       101,
		UserID:   user.ID,
		Status:   StatusActive,
		User:     user,
		GroupID:  &anthropicGroup.ID,
		GroupIDs: []int64{anthropicGroup.ID, openAIGroup.ID},
		Group:    &anthropicGroup,
	}
	svc := NewAPIKeyService(
		nil,
		&mockUserRepo{getByIDUser: user},
		&effectiveGroupRepoStub{active: []Group{anthropicGroup, openAIGroup, geminiGroup}},
		&effectiveSubRepoStub{active: []UserSubscription{{
			ID:        33,
			UserID:    user.ID,
			GroupID:   geminiGroup.ID,
			Status:    SubscriptionStatusActive,
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}}},
		nil,
		nil,
		&config.Config{},
	)

	group, switched, err := svc.ResolveEffectiveGroupForPlatform(ctx, apiKey, PlatformOpenAI)

	require.NoError(t, err)
	require.True(t, switched)
	require.NotNil(t, group)
	require.Equal(t, openAIGroup.ID, group.ID)
	require.Equal(t, PlatformOpenAI, group.Platform)
	require.Equal(t, anthropicGroup.ID, *apiKey.GroupID, "resolver must not mutate the original API key")
}

func TestAPIKeyServiceResolveEffectiveGroupForPlatformAllowsActiveSubscriptionGroup(t *testing.T) {
	ctx := context.Background()
	anthropicGroup := Group{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, SubscriptionType: SubscriptionTypeStandard}
	geminiGroup := Group{ID: 3, Platform: PlatformGemini, Status: StatusActive, Hydrated: true, IsExclusive: true, SubscriptionType: SubscriptionTypeSubscription}
	user := &User{ID: 7, Status: StatusActive}
	apiKey := &APIKey{
		ID:       101,
		UserID:   user.ID,
		Status:   StatusActive,
		User:     user,
		GroupID:  &anthropicGroup.ID,
		GroupIDs: []int64{anthropicGroup.ID, geminiGroup.ID},
		Group:    &anthropicGroup,
	}
	svc := NewAPIKeyService(
		nil,
		&mockUserRepo{getByIDUser: user},
		&effectiveGroupRepoStub{active: []Group{anthropicGroup, geminiGroup}},
		&effectiveSubRepoStub{active: []UserSubscription{{
			ID:        33,
			UserID:    user.ID,
			GroupID:   geminiGroup.ID,
			Status:    SubscriptionStatusActive,
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}}},
		nil,
		nil,
		&config.Config{},
	)

	group, switched, err := svc.ResolveEffectiveGroupForPlatform(ctx, apiKey, PlatformGemini)

	require.NoError(t, err)
	require.True(t, switched)
	require.NotNil(t, group)
	require.Equal(t, geminiGroup.ID, group.ID)
}

func TestAPIKeyServiceResolveEffectiveGroupForPlatformFallsBackToBoundGroup(t *testing.T) {
	ctx := context.Background()
	anthropicGroup := Group{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, SubscriptionType: SubscriptionTypeStandard}
	user := &User{ID: 7, Status: StatusActive}
	apiKey := &APIKey{ID: 101, UserID: user.ID, Status: StatusActive, User: user, GroupID: &anthropicGroup.ID, Group: &anthropicGroup}
	svc := NewAPIKeyService(
		nil,
		&mockUserRepo{getByIDUser: user},
		&effectiveGroupRepoStub{active: []Group{anthropicGroup}},
		&effectiveSubRepoStub{},
		nil,
		nil,
		&config.Config{},
	)

	group, switched, err := svc.ResolveEffectiveGroupForPlatform(ctx, apiKey, PlatformOpenAI)

	require.NoError(t, err)
	require.False(t, switched)
	require.NotNil(t, group)
	require.Equal(t, anthropicGroup.ID, group.ID)
}

func TestAPIKeyServiceResolveEffectiveGroupForPlatformDoesNotUseUnselectedUserGroup(t *testing.T) {
	ctx := context.Background()
	anthropicGroup := Group{
		ID:               1,
		Name:             "default-anthropic",
		Platform:         PlatformAnthropic,
		Status:           StatusActive,
		Hydrated:         true,
		SubscriptionType: SubscriptionTypeStandard,
	}
	geminiGroup := Group{
		ID:               3,
		Name:             "omni-gemini",
		Platform:         PlatformGemini,
		Status:           StatusActive,
		Hydrated:         true,
		IsExclusive:      true,
		SubscriptionType: SubscriptionTypeStandard,
	}
	user := &User{
		ID:            7,
		Status:        StatusActive,
		AllowedGroups: []int64{geminiGroup.ID},
	}
	apiKey := &APIKey{
		ID:       101,
		UserID:   user.ID,
		Status:   StatusActive,
		User:     user,
		GroupID:  &anthropicGroup.ID,
		GroupIDs: []int64{anthropicGroup.ID},
		Group:    &anthropicGroup,
	}
	svc := NewAPIKeyService(
		nil,
		&mockUserRepo{getByIDUser: user},
		&effectiveGroupRepoStub{active: []Group{anthropicGroup, geminiGroup}},
		&effectiveSubRepoStub{},
		nil,
		nil,
		&config.Config{},
	)

	group, switched, err := svc.ResolveEffectiveGroupForPlatform(ctx, apiKey, PlatformGemini)

	require.NoError(t, err)
	require.False(t, switched)
	require.NotNil(t, group)
	require.Equal(t, anthropicGroup.ID, group.ID)
}

func TestAPIKeyServiceCreatePersistsSelectedGroupIDsAndUsesFirstAsDefault(t *testing.T) {
	ctx := context.Background()
	openAIGroup := Group{ID: 2, Name: "omni-openai", Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, SubscriptionType: SubscriptionTypeStandard}
	geminiGroup := Group{ID: 3, Name: "omni-gemini", Platform: PlatformGemini, Status: StatusActive, Hydrated: true, SubscriptionType: SubscriptionTypeStandard}
	user := &User{ID: 7, Status: StatusActive}
	apiKeyRepo := &effectiveAPIKeyGroupRepoStub{nextID: 100}
	svc := NewAPIKeyService(
		apiKeyRepo,
		&mockUserRepo{getByIDUser: user},
		&effectiveGroupRepoStub{active: []Group{openAIGroup, geminiGroup}},
		&effectiveSubRepoStub{},
		nil,
		nil,
		&config.Config{Default: config.DefaultConfig{APIKeyPrefix: "sk-test-"}},
	)

	apiKey, err := svc.Create(ctx, user.ID, CreateAPIKeyRequest{
		Name:     "omni-key",
		GroupIDs: []int64{openAIGroup.ID, geminiGroup.ID},
	})

	require.NoError(t, err)
	require.NotNil(t, apiKey.GroupID)
	require.Equal(t, openAIGroup.ID, *apiKey.GroupID)
	require.Equal(t, []int64{openAIGroup.ID, geminiGroup.ID}, apiKey.GroupIDs)
	require.Equal(t, []int64{openAIGroup.ID, geminiGroup.ID}, apiKeyRepo.groupIDsByKey[apiKey.ID])
}

func TestAPIKeyServiceUpdateMovesDefaultIntoSelectedGroupIDs(t *testing.T) {
	ctx := context.Background()
	openAIGroup := Group{ID: 2, Name: "omni-openai", Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, SubscriptionType: SubscriptionTypeStandard}
	geminiGroup := Group{ID: 3, Name: "omni-gemini", Platform: PlatformGemini, Status: StatusActive, Hydrated: true, SubscriptionType: SubscriptionTypeStandard}
	user := &User{ID: 7, Status: StatusActive}
	apiKeyRepo := &effectiveAPIKeyGroupRepoStub{
		existing: &APIKey{
			ID:       100,
			UserID:   user.ID,
			Key:      "omni-key",
			Name:     "omni",
			GroupID:  &openAIGroup.ID,
			GroupIDs: []int64{openAIGroup.ID, geminiGroup.ID},
			Status:   StatusActive,
		},
		groupIDsByKey: map[int64][]int64{},
	}
	svc := NewAPIKeyService(
		apiKeyRepo,
		&mockUserRepo{getByIDUser: user},
		&effectiveGroupRepoStub{active: []Group{openAIGroup, geminiGroup}},
		&effectiveSubRepoStub{},
		nil,
		nil,
		&config.Config{},
	)
	selected := []int64{geminiGroup.ID}

	apiKey, err := svc.Update(ctx, apiKeyRepo.existing.ID, user.ID, UpdateAPIKeyRequest{
		GroupIDs: &selected,
	})

	require.NoError(t, err)
	require.NotNil(t, apiKey.GroupID)
	require.Equal(t, geminiGroup.ID, *apiKey.GroupID)
	require.Equal(t, []int64{geminiGroup.ID}, apiKey.GroupIDs)
	require.Equal(t, []int64{geminiGroup.ID}, apiKeyRepo.groupIDsByKey[apiKey.ID])
	require.NotNil(t, apiKeyRepo.updated)
	require.Equal(t, geminiGroup.ID, *apiKeyRepo.updated.GroupID)
}

type effectiveAPIKeyGroupRepoStub struct {
	quotaBaseAPIKeyRepoStub
	nextID        int64
	existing      *APIKey
	created       *APIKey
	updated       *APIKey
	groupIDsByKey map[int64][]int64
}

func (s *effectiveAPIKeyGroupRepoStub) Create(_ context.Context, key *APIKey) error {
	clone := *key
	if clone.ID == 0 {
		clone.ID = s.nextID
		if clone.ID == 0 {
			clone.ID = 1
		}
	}
	key.ID = clone.ID
	s.created = &clone
	return nil
}

func (s *effectiveAPIKeyGroupRepoStub) GetByID(_ context.Context, _ int64) (*APIKey, error) {
	if s.existing == nil {
		return nil, ErrAPIKeyNotFound
	}
	clone := *s.existing
	clone.GroupIDs = append([]int64(nil), s.existing.GroupIDs...)
	return &clone, nil
}

func (s *effectiveAPIKeyGroupRepoStub) Update(_ context.Context, key *APIKey) error {
	clone := *key
	clone.GroupIDs = append([]int64(nil), key.GroupIDs...)
	s.updated = &clone
	return nil
}

func (s *effectiveAPIKeyGroupRepoStub) SetGroupIDs(_ context.Context, apiKeyID int64, groupIDs []int64) error {
	if s.groupIDsByKey == nil {
		s.groupIDsByKey = make(map[int64][]int64)
	}
	s.groupIDsByKey[apiKeyID] = append([]int64(nil), groupIDs...)
	return nil
}

type effectiveGroupRepoStub struct {
	active []Group
}

func (s *effectiveGroupRepoStub) Create(context.Context, *Group) error { return nil }
func (s *effectiveGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	for _, group := range s.active {
		if group.ID == id {
			clone := group
			return &clone, nil
		}
	}
	return nil, ErrGroupNotFound
}
func (s *effectiveGroupRepoStub) GetByIDLite(context.Context, int64) (*Group, error) {
	return nil, ErrGroupNotFound
}
func (s *effectiveGroupRepoStub) Update(context.Context, *Group) error { return nil }
func (s *effectiveGroupRepoStub) Delete(context.Context, int64) error  { return nil }
func (s *effectiveGroupRepoStub) DeleteCascade(context.Context, int64) ([]int64, error) {
	return nil, nil
}
func (s *effectiveGroupRepoStub) List(context.Context, pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *effectiveGroupRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *effectiveGroupRepoStub) ListActive(context.Context) ([]Group, error) {
	out := make([]Group, len(s.active))
	copy(out, s.active)
	return out, nil
}
func (s *effectiveGroupRepoStub) ListActiveByPlatform(context.Context, string) ([]Group, error) {
	return nil, nil
}
func (s *effectiveGroupRepoStub) ExistsByName(context.Context, string) (bool, error) {
	return false, nil
}
func (s *effectiveGroupRepoStub) GetAccountCount(context.Context, int64) (int64, int64, error) {
	return 0, 0, nil
}
func (s *effectiveGroupRepoStub) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (s *effectiveGroupRepoStub) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return nil, nil
}
func (s *effectiveGroupRepoStub) BindAccountsToGroup(context.Context, int64, []int64) error {
	return nil
}
func (s *effectiveGroupRepoStub) UpdateSortOrders(context.Context, []GroupSortOrderUpdate) error {
	return nil
}

type effectiveSubRepoStub struct {
	active []UserSubscription
}

func (s *effectiveSubRepoStub) Create(context.Context, *UserSubscription) error { return nil }
func (s *effectiveSubRepoStub) GetByID(context.Context, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}
func (s *effectiveSubRepoStub) GetByIDIncludeDeleted(context.Context, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}
func (s *effectiveSubRepoStub) GetByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}
func (s *effectiveSubRepoStub) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}
func (s *effectiveSubRepoStub) Update(context.Context, *UserSubscription) error { return nil }
func (s *effectiveSubRepoStub) Delete(context.Context, int64) error             { return nil }
func (s *effectiveSubRepoStub) Restore(context.Context, int64, string) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}
func (s *effectiveSubRepoStub) ListByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}
func (s *effectiveSubRepoStub) ListActiveByUserID(_ context.Context, userID int64) ([]UserSubscription, error) {
	out := make([]UserSubscription, 0, len(s.active))
	now := time.Now()
	for _, sub := range s.active {
		if sub.UserID == userID && sub.Status == SubscriptionStatusActive && sub.ExpiresAt.After(now) {
			out = append(out, sub)
		}
	}
	return out, nil
}
func (s *effectiveSubRepoStub) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *effectiveSubRepoStub) List(context.Context, pagination.PaginationParams, *int64, *int64, string, string, string, string) ([]UserSubscription, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *effectiveSubRepoStub) ExistsByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	return false, nil
}
func (s *effectiveSubRepoStub) ExistsActiveByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	return false, nil
}
func (s *effectiveSubRepoStub) ExtendExpiry(context.Context, int64, time.Time) error {
	return nil
}
func (s *effectiveSubRepoStub) UpdateStatus(context.Context, int64, string) error {
	return nil
}
func (s *effectiveSubRepoStub) UpdateNotes(context.Context, int64, string) error {
	return nil
}
func (s *effectiveSubRepoStub) ActivateWindows(context.Context, int64, time.Time) error {
	return nil
}
func (s *effectiveSubRepoStub) ResetUsageWindows(context.Context, int64, bool, bool, bool, time.Time) error {
	return nil
}
func (s *effectiveSubRepoStub) ResetDailyUsage(context.Context, int64, *time.Time, time.Time) error {
	return nil
}
func (s *effectiveSubRepoStub) ResetWeeklyUsage(context.Context, int64, *time.Time, time.Time) error {
	return nil
}
func (s *effectiveSubRepoStub) ResetMonthlyUsage(context.Context, int64, *time.Time, time.Time) error {
	return nil
}
func (s *effectiveSubRepoStub) IncrementUsage(context.Context, int64, float64) error {
	return nil
}
func (s *effectiveSubRepoStub) BatchUpdateExpiredStatus(context.Context) (int64, error) {
	return 0, nil
}
