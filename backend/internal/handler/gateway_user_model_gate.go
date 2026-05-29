package handler

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// userModelDenied reports whether the per-user model whitelist blocks the
// requested model. A nil user or empty whitelist means no restriction.
// Callers should reject the request with HTTP 403 in the caller's protocol
// format when this returns true.
func userModelDenied(apiKey *service.APIKey, model string) bool {
	if apiKey == nil || apiKey.User == nil {
		return false
	}
	return !apiKey.User.AllowsModel(model)
}

// userModelDenialMessage builds the client-facing 403 message for a blocked model.
func userModelDenialMessage(model string) string {
	if model == "" {
		return "The requested model is not permitted for your account"
	}
	return fmt.Sprintf("Model %q is not permitted for your account", model)
}

// filterModelIDsByUser returns the subset of modelIDs the per-user whitelist permits.
// A nil user or empty whitelist means no restriction (the input is returned unchanged).
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

// filterModelObjectsByUser returns the subset of typed model objects the per-user
// whitelist permits, using idOf to extract each model's ID.
func filterModelObjectsByUser[T any](apiKey *service.APIKey, models []T, idOf func(T) string) []T {
	if apiKey == nil || apiKey.User == nil || len(apiKey.User.AllowedModels) == 0 {
		return models
	}
	out := make([]T, 0, len(models))
	for _, m := range models {
		if apiKey.User.AllowsModel(idOf(m)) {
			out = append(out, m)
		}
	}
	return out
}
