//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserWeeklyThreshold(t *testing.T) {
	require.False(t, func() bool { _, ok := (*User)(nil).WeeklyThreshold(); return ok }())
	require.False(t, func() bool { _, ok := (&User{}).WeeklyThreshold(); return ok }())

	zero := 0.0
	require.False(t, func() bool { _, ok := (&User{WeeklyCostThreshold: &zero}).WeeklyThreshold(); return ok }())

	limit := 50.0
	got, ok := (&User{WeeklyCostThreshold: &limit}).WeeklyThreshold()
	require.True(t, ok)
	require.Equal(t, 50.0, got)
}
