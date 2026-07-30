package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

type contentModerationExclusionGroupRepo struct {
	GroupRepository
	existing map[int64]struct{}
	calls    []int64
}

func (r *contentModerationExclusionGroupRepo) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	r.calls = append(r.calls, id)
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

func TestContentModerationGroupExclusion_InvalidIDRejectsUpdateWithoutActivation(t *testing.T) {
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
	require.Equal(t, []int64{404}, groupRepo.calls)
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
