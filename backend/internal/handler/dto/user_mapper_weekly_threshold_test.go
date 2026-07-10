package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserFromServiceShallowMapsWeeklyThreshold(t *testing.T) {
	threshold := 50.0
	out := UserFromServiceShallow(&service.User{ID: 1, WeeklyCostThreshold: &threshold})

	require.NotNil(t, out.WeeklyCostThreshold)
	require.Equal(t, 50.0, *out.WeeklyCostThreshold)
}
