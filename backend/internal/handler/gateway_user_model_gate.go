package handler

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func userModelDenied(apiKey *service.APIKey, model string) bool {
	return apiKey != nil && apiKey.User != nil && !apiKey.User.AllowsModel(model)
}

func userModelDenialMessage(model string) string {
	if model == "" {
		return "The requested model is not permitted for your account"
	}
	return fmt.Sprintf("Model %q is not permitted for your account", model)
}

func filterModelIDsByUser(apiKey *service.APIKey, modelIDs []string) []string {
	if apiKey == nil || apiKey.User == nil || len(apiKey.User.AllowedModels) == 0 {
		return modelIDs
	}
	out := make([]string, 0, len(modelIDs))
	for _, id := range modelIDs {
		if apiKey.User.AllowsModel(id) {
			out = append(out, id)
		}
	}
	return out
}

func filterModelObjectsByUser[T any](apiKey *service.APIKey, models []T, idOf func(T) string) []T {
	if apiKey == nil || apiKey.User == nil || len(apiKey.User.AllowedModels) == 0 {
		return models
	}
	out := make([]T, 0, len(models))
	for _, model := range models {
		if apiKey.User.AllowsModel(idOf(model)) {
			out = append(out, model)
		}
	}
	return out
}
