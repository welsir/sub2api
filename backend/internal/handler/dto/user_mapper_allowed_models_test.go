package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserFromServiceShallowMapsAllowedModels(t *testing.T) {
	out := UserFromServiceShallow(&service.User{
		ID:            1,
		AllowedModels: []string{"gpt-5*", "claude-sonnet-4-6"},
	})

	require.Equal(t, []string{"gpt-5*", "claude-sonnet-4-6"}, out.AllowedModels)
}
