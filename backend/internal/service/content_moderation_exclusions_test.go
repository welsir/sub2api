package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type contentModerationExclusionGroupRepo struct {
	GroupRepository
	existing map[int64]struct{}
	failures map[int64]error
	calls    []int64
}

func (r *contentModerationExclusionGroupRepo) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	r.calls = append(r.calls, id)
	if err := r.failures[id]; err != nil {
		return nil, err
	}
	if _, ok := r.existing[id]; !ok {
		return nil, ErrGroupNotFound
	}
	return &Group{ID: id}, nil
}

func TestContentModerationGroupExclusion_AllGroupsScope(t *testing.T) {
	cfg := defaultContentModerationConfig()
	require.NoError(t, json.Unmarshal([]byte(`{
		"all_groups": true,
		"excluded_group_ids": [17]
	}`), cfg))
	cfg.normalize()

	excludedID := int64(17)
	futureID := int64(999)
	require.False(t, cfg.includesGroup(&excludedID), "an exact excluded group ID must be out of scope")
	require.True(t, cfg.includesGroup(&futureID), "a future non-excluded group ID must remain in scope")
	require.True(t, cfg.includesGroup(nil), "a nil group ID must remain in scope in all-groups mode")

	empty := defaultContentModerationConfig()
	empty.normalize()
	require.True(t, empty.includesGroup(&excludedID), "empty exclusions must preserve all-groups behavior")
}

func TestContentModerationGroupExclusion_UpdateNormalizesValidatesAndPersists(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	groupRepo := &contentModerationExclusionGroupRepo{existing: map[int64]struct{}{
		3: {},
		7: {},
	}}
	svc := NewContentModerationService(settingRepo, nil, nil, groupRepo, nil, nil, nil)

	var input UpdateContentModerationConfigInput
	require.NoError(t, json.Unmarshal([]byte(`{
		"all_groups": true,
		"excluded_group_ids": [7, 3, 7, 0, -1]
	}`), &input))

	view, err := svc.UpdateConfig(context.Background(), input)

	require.NoError(t, err)
	var viewJSON struct {
		ExcludedGroupIDs []int64 `json:"excluded_group_ids"`
	}
	rawView, err := json.Marshal(view)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rawView, &viewJSON))
	require.Equal(t, []int64{3, 7}, viewJSON.ExcludedGroupIDs)
	require.Equal(t, []int64{3, 7}, groupRepo.calls)

	var savedJSON struct {
		ExcludedGroupIDs []int64 `json:"excluded_group_ids"`
	}
	require.NoError(t, json.Unmarshal([]byte(settingRepo.values[SettingKeyContentModerationConfig]), &savedJSON))
	require.Equal(t, []int64{3, 7}, savedJSON.ExcludedGroupIDs)
}

func TestContentModerationGroupExclusion_NotFoundRejectsUpdateAsBadRequest(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	groupRepo := &contentModerationExclusionGroupRepo{existing: map[int64]struct{}{}}
	svc := NewContentModerationService(settingRepo, nil, nil, groupRepo, nil, nil, nil)

	var input UpdateContentModerationConfigInput
	require.NoError(t, json.Unmarshal([]byte(`{
		"all_groups": true,
		"excluded_group_ids": [404]
	}`), &input))

	view, err := svc.UpdateConfig(context.Background(), input)

	require.Error(t, err)
	require.Nil(t, view)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Equal(t, "INVALID_CONTENT_MODERATION_GROUP", infraerrors.Reason(err))
	require.Equal(t, []int64{404}, groupRepo.calls)
	require.NotContains(t, settingRepo.values, SettingKeyContentModerationConfig)
}

func TestContentModerationGroupExclusion_NilRepositoryRejectsNonemptyScope(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	excludedGroupIDs := []int64{17}

	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		ExcludedGroupIDs: &excludedGroupIDs,
	})

	require.Error(t, err)
	require.Nil(t, view)
	require.Equal(t, http.StatusInternalServerError, infraerrors.Code(err))
	require.NotContains(t, settingRepo.values, SettingKeyContentModerationConfig)
}

func TestContentModerationGroupExclusion_RepositoryFailureReturnsInternalError(t *testing.T) {
	repositoryErr := errors.New("group repository unavailable")
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	groupRepo := &contentModerationExclusionGroupRepo{
		existing: map[int64]struct{}{},
		failures: map[int64]error{17: repositoryErr},
	}
	svc := NewContentModerationService(settingRepo, nil, nil, groupRepo, nil, nil, nil)
	excludedGroupIDs := []int64{17}

	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		ExcludedGroupIDs: &excludedGroupIDs,
	})

	require.Error(t, err)
	require.Nil(t, view)
	require.Equal(t, http.StatusInternalServerError, infraerrors.Code(err))
	require.ErrorIs(t, err, repositoryErr)
	require.NotContains(t, settingRepo.values, SettingKeyContentModerationConfig)
}

func TestContentModerationGroupExclusion_LegacyInvalidExclusionRejectsEntireUpdate(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	groupRepo := &contentModerationExclusionGroupRepo{existing: map[int64]struct{}{
		7: {},
	}}
	svc := NewContentModerationService(settingRepo, nil, nil, groupRepo, nil, nil, nil)
	allGroups := false
	enabled := true
	groupIDs := []int64{7}
	excludedGroupIDs := []int64{404}

	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Enabled:          &enabled,
		AllGroups:        &allGroups,
		GroupIDs:         &groupIDs,
		ExcludedGroupIDs: &excludedGroupIDs,
	})

	require.Error(t, err)
	require.Nil(t, view)
	require.Equal(t, []int64{7, 404}, groupRepo.calls)
	require.NotContains(t, settingRepo.values, SettingKeyContentModerationConfig)
}

func TestContentModerationGroupExclusion_LegacyAllowlistClearsExclusions(t *testing.T) {
	cfg := defaultContentModerationConfig()
	require.NoError(t, json.Unmarshal([]byte(`{
		"all_groups": false,
		"group_ids": [7],
		"excluded_group_ids": [9]
	}`), cfg))
	cfg.normalize()

	allowedID := int64(7)
	otherID := int64(9)
	require.True(t, cfg.includesGroup(&allowedID))
	require.False(t, cfg.includesGroup(&otherID))
	require.False(t, cfg.includesGroup(nil))

	var normalizedJSON struct {
		ExcludedGroupIDs []int64 `json:"excluded_group_ids"`
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &normalizedJSON))
	require.Empty(t, normalizedJSON.ExcludedGroupIDs, "allowlist mode must clear exclusions")
}

func TestContentModerationGroupExclusion_RuntimeStatusExposesNormalizedScope(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.AllGroups = true
	cfg.ExcludedGroupIDs = []int64{7, 3, 7, 0, -1}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	status, err := svc.GetStatus(context.Background())

	require.NoError(t, err)
	var statusJSON struct {
		AllGroups        bool    `json:"all_groups"`
		ExcludedGroupIDs []int64 `json:"excluded_group_ids"`
	}
	rawStatus, err := json.Marshal(status)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rawStatus, &statusJSON))
	require.True(t, statusJSON.AllGroups)
	require.Equal(t, []int64{3, 7}, statusJSON.ExcludedGroupIDs)
}

func TestContentModerationGroupExclusion_SkipUsesExemptLogEvent(t *testing.T) {
	var slogOutput bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&slogOutput, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	var rawMap map[string]any
	require.NoError(t, json.Unmarshal(rawCfg, &rawMap))
	rawMap["excluded_group_ids"] = []int64{17}
	rawCfg, err = json.Marshal(rawMap)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)
	excludedID := int64(17)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		GroupID:  &excludedID,
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"test"}]}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Contains(t, slogOutput.String(), "content_moderation.skip_group_exempt")
	require.Contains(t, slogOutput.String(), "configured_excluded_group_ids")
	require.NotContains(t, slogOutput.String(), "content_moderation.skip_group_out_of_scope")
}
