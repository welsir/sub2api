package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestUserModelDenied(t *testing.T) {
	if userModelDenied(nil, "m") {
		t.Fatal("nil apiKey must not deny")
	}
	if userModelDenied(&service.APIKey{}, "m") {
		t.Fatal("nil user must not deny")
	}

	allowAll := &service.APIKey{User: &service.User{}}
	if userModelDenied(allowAll, "anything") {
		t.Fatal("empty whitelist must not deny")
	}

	restricted := &service.APIKey{User: &service.User{AllowedModels: []string{"claude-*"}}}
	if userModelDenied(restricted, "claude-opus-4-6") {
		t.Fatal("whitelisted model must not be denied")
	}
	if !userModelDenied(restricted, "gpt-5") {
		t.Fatal("non-whitelisted model must be denied")
	}
}

func TestUserModelDenialMessage(t *testing.T) {
	if msg := userModelDenialMessage(""); msg == "" {
		t.Fatal("empty-model message must not be blank")
	}
	if msg := userModelDenialMessage("gpt-5"); !strings.Contains(msg, "gpt-5") {
		t.Fatalf("message should mention the model, got %q", msg)
	}
}

func TestFilterModelIDsByUser(t *testing.T) {
	restricted := &service.APIKey{User: &service.User{AllowedModels: []string{"claude-*"}}}
	got := filterModelIDsByUser(restricted, []string{"claude-opus", "gpt-5", "claude-haiku"})
	if want := []string{"claude-opus", "claude-haiku"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filterModelIDsByUser = %v, want %v", got, want)
	}

	allowAll := &service.APIKey{User: &service.User{}}
	in := []string{"a", "b"}
	if got := filterModelIDsByUser(allowAll, in); !reflect.DeepEqual(got, in) {
		t.Fatalf("empty whitelist should return input unchanged, got %v", got)
	}
}

func TestFilterModelObjectsByUser(t *testing.T) {
	type model struct{ ID string }
	restricted := &service.APIKey{User: &service.User{AllowedModels: []string{"gpt-5"}}}
	got := filterModelObjectsByUser(restricted, []model{{"gpt-5"}, {"claude-opus"}}, func(m model) string { return m.ID })
	if len(got) != 1 || got[0].ID != "gpt-5" {
		t.Fatalf("filterModelObjectsByUser = %v, want [{gpt-5}]", got)
	}
}

func TestUserModelAllowlistRejectsGatewayResponsesBeforeRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hello"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	groupID := int64(7)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      11,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID},
		User:    &service.User{ID: 13, AllowedModels: []string{"claude-*"}},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 13, Concurrency: 1})

	(&GatewayHandler{gatewayService: &service.GatewayService{}}).Responses(c)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "permission_error", gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
	require.Contains(t, gjson.GetBytes(rec.Body.Bytes(), "error.message").String(), "gpt-5")
}

func TestUserModelAllowlistRejectsOpenAIResponsesBeforeRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hello"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	groupID := int64(8)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      12,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI},
		User:    &service.User{ID: 14, AllowedModels: []string{"claude-*"}},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 14, Concurrency: 1})

	newOpenAIHandlerForPreviousResponseIDValidation(t, nil).Responses(c)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "permission_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Contains(t, gjson.GetBytes(rec.Body.Bytes(), "error.message").String(), "gpt-5")
}
