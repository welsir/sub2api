package handler

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUserModelDenied(t *testing.T) {
	// nil apiKey / nil user => cannot enforce, so never denied.
	if userModelDenied(nil, "m") {
		t.Fatal("nil apiKey must not deny")
	}
	if userModelDenied(&service.APIKey{}, "m") {
		t.Fatal("nil user must not deny")
	}

	// Empty whitelist => no restriction.
	allowAll := &service.APIKey{User: &service.User{}}
	if userModelDenied(allowAll, "anything") {
		t.Fatal("empty whitelist must not deny")
	}

	// Whitelist set.
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

	// Empty whitelist returns the input unchanged.
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
