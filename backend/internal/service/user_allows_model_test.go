//go:build unit

package service

import "testing"

func TestUserAllowsModel(t *testing.T) {
	cases := []struct {
		name    string
		allowed []string
		model   string
		want    bool
	}{
		{"nil whitelist allows all", nil, "claude-sonnet-4-5", true},
		{"empty whitelist allows all", []string{}, "gpt-5", true},
		{"exact match", []string{"claude-sonnet-4-5"}, "claude-sonnet-4-5", true},
		{"exact non-match", []string{"claude-sonnet-4-5"}, "gpt-5", false},
		{"trailing wildcard match", []string{"claude-*"}, "claude-opus-4-6", true},
		{"trailing wildcard non-match", []string{"claude-*"}, "gpt-5", false},
		{"multiple patterns hit second", []string{"gpt-5", "claude-*"}, "claude-haiku-4-5", true},
		{"multiple patterns miss all", []string{"gpt-5", "claude-*"}, "gemini-2.5-pro", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := &User{AllowedModels: tc.allowed}
			if got := u.AllowsModel(tc.model); got != tc.want {
				t.Fatalf("AllowsModel(%q) with whitelist %v = %v, want %v", tc.model, tc.allowed, got, tc.want)
			}
		})
	}

	var nilUser *User
	if !nilUser.AllowsModel("anything") {
		t.Fatal("nil user must allow all models")
	}
}
