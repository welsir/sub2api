//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNaturalWeekRangeUsesSaturdayAsFirstDay(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	friday := time.Date(2026, 5, 29, 23, 59, 59, 0, loc)

	start, end := NaturalWeekRange(friday, loc)

	require.Equal(t, "2026-05-23", start.Format("2006-01-02"))
	require.Equal(t, "2026-05-30", end.Format("2006-01-02"))
	require.Equal(t, time.Saturday, start.Weekday())
}

func TestNaturalWeekRangeStartsOnCurrentSaturday(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	saturday := time.Date(2026, 5, 23, 12, 0, 0, 0, loc)

	start, end := NaturalWeekRange(saturday, loc)

	require.Equal(t, "2026-05-23", start.Format("2006-01-02"))
	require.Equal(t, "2026-05-30", end.Format("2006-01-02"))
}
