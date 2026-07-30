package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type contentModerationHandlerSettingRepo struct {
	values map[string]string
}

func (r *contentModerationHandlerSettingRepo) Get(_ context.Context, key string) (*service.Setting, error) {
	if value, ok := r.values[key]; ok {
		return &service.Setting{Key: key, Value: value}, nil
	}
	return nil, service.ErrSettingNotFound
}

func (r *contentModerationHandlerSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (r *contentModerationHandlerSettingRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *contentModerationHandlerSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *contentModerationHandlerSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *contentModerationHandlerSettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *contentModerationHandlerSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

type contentModerationHandlerGroupRepo struct {
	service.GroupRepository
	existing map[int64]struct{}
}

func (r *contentModerationHandlerGroupRepo) GetByIDLite(_ context.Context, id int64) (*service.Group, error) {
	if _, ok := r.existing[id]; !ok {
		return nil, service.ErrGroupNotFound
	}
	return &service.Group{ID: id}, nil
}

func TestContentModerationHandlerUpdateConfigPersistsAndReturnsExcludedGroupIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settingRepo := &contentModerationHandlerSettingRepo{values: map[string]string{}}
	groupRepo := &contentModerationHandlerGroupRepo{existing: map[int64]struct{}{
		17: {},
	}}
	svc := service.NewContentModerationService(settingRepo, nil, nil, groupRepo, nil, nil, nil)
	handler := NewContentModerationHandler(svc)
	router := gin.New()
	router.PUT("/api/v1/admin/risk-control/config", handler.UpdateConfig)

	body := bytes.NewBufferString(`{
		"all_groups": true,
		"excluded_group_ids": [17, 17]
	}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/risk-control/config", body)
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			AllGroups        bool    `json:"all_groups"`
			ExcludedGroupIDs []int64 `json:"excluded_group_ids"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.True(t, envelope.Data.AllGroups)
	require.Equal(t, []int64{17}, envelope.Data.ExcludedGroupIDs)

	var saved service.ContentModerationConfig
	require.NoError(t, json.Unmarshal(
		[]byte(settingRepo.values[service.SettingKeyContentModerationConfig]),
		&saved,
	))
	require.True(t, saved.AllGroups)
	require.Equal(t, []int64{17}, saved.ExcludedGroupIDs)
}
