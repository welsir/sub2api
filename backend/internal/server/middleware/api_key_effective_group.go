package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func applyEffectiveAPIKeyGroup(c *gin.Context, apiKeyService *service.APIKeyService, apiKey *service.APIKey) (*service.APIKey, error) {
	if c == nil || apiKeyService == nil || apiKey == nil {
		return apiKey, nil
	}
	targetPlatform := inferTargetPlatformForRequest(c)
	if targetPlatform == "" {
		return apiKey, nil
	}

	effectiveGroup, switched, err := apiKeyService.ResolveEffectiveGroupForPlatform(c.Request.Context(), apiKey, targetPlatform)
	if err != nil {
		return nil, err
	}
	if !switched || effectiveGroup == nil {
		return apiKey, nil
	}

	requestAPIKey := cloneAPIKeyForRequest(apiKey)
	requestAPIKey.Group = effectiveGroup
	requestAPIKey.GroupID = &effectiveGroup.ID
	if requestAPIKey.User != nil {
		// The auth snapshot override belongs to the original key group. Let
		// downstream RPM checks resolve the override for the effective group.
		requestAPIKey.User.UserGroupRPMOverride = nil
	}
	return requestAPIKey, nil
}

func cloneAPIKeyForRequest(apiKey *service.APIKey) *service.APIKey {
	if apiKey == nil {
		return nil
	}
	clone := *apiKey
	clone.GroupIDs = append([]int64(nil), apiKey.GroupIDs...)
	if apiKey.User != nil {
		userClone := *apiKey.User
		clone.User = &userClone
	}
	if apiKey.Group != nil {
		groupClone := *apiKey.Group
		clone.Group = &groupClone
	}
	return &clone
}

func inferTargetPlatformForRequest(c *gin.Context) string {
	if forcePlatform, ok := c.Request.Context().Value(ctxkey.ForcePlatform).(string); ok && strings.TrimSpace(forcePlatform) != "" {
		return strings.TrimSpace(forcePlatform)
	}

	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/v1beta/") || path == "/v1beta" {
		return service.PlatformGemini
	}
	if isOpenAIOnlyPath(path) {
		return service.PlatformOpenAI
	}

	model := requestModelFromJSONBody(c)
	return inferPlatformFromModel(model)
}

func isOpenAIOnlyPath(path string) bool {
	switch {
	case strings.HasPrefix(path, "/v1/images/"):
		return true
	case strings.HasPrefix(path, "/images/"):
		return true
	default:
		return false
	}
}

func requestModelFromJSONBody(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return ""
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(bytes.TrimSpace(body)) == 0 {
		return ""
	}

	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload.Model
}

func inferPlatformFromModel(model string) string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	normalized = strings.TrimPrefix(normalized, "models/")
	if normalized == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(normalized, "gemini-"):
		return service.PlatformGemini
	case strings.HasPrefix(normalized, "claude-"):
		return service.PlatformAnthropic
	case strings.HasPrefix(normalized, "gpt-"),
		strings.HasPrefix(normalized, "chatgpt-"),
		strings.HasPrefix(normalized, "o1"),
		strings.HasPrefix(normalized, "o3"),
		strings.HasPrefix(normalized, "o4"):
		return service.PlatformOpenAI
	default:
		return ""
	}
}
