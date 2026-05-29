//go:build unit

package service

import (
	"testing"
	"time"
)

func TestNaturalWeekRange(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	const layout = "2006-01-02 15:04:05"

	tests := []struct {
		name      string
		now       string
		wantStart string
		wantEnd   string
	}{
		// 2026-05-29 is a Friday (last day of the week) -> week started Sat 2026-05-23.
		{"friday last day", "2026-05-29 23:59:59", "2026-05-23", "2026-05-30"},
		// 2026-05-23 is a Saturday (first day) -> the week starts that very day.
		{"saturday first day start of day", "2026-05-23 00:00:00", "2026-05-23", "2026-05-30"},
		{"saturday first day end of day", "2026-05-23 23:59:59", "2026-05-23", "2026-05-30"},
		// 2026-05-24 Sunday -> still in the Sat 2026-05-23 week.
		{"sunday", "2026-05-24 12:00:00", "2026-05-23", "2026-05-30"},
		// 2026-05-22 Friday -> previous week started Sat 2026-05-16.
		{"prev friday", "2026-05-22 09:00:00", "2026-05-16", "2026-05-23"},
		// Year boundary: 2027-01-01 is a Friday -> week started Sat 2026-12-26.
		{"year boundary friday", "2027-01-01 10:00:00", "2026-12-26", "2027-01-02"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now, err := time.ParseInLocation(layout, tt.now, loc)
			if err != nil {
				t.Fatalf("parse now: %v", err)
			}
			start, end := NaturalWeekRange(now, loc)
			if got := start.Format("2006-01-02"); got != tt.wantStart {
				t.Errorf("start = %s, want %s", got, tt.wantStart)
			}
			if got := end.Format("2006-01-02"); got != tt.wantEnd {
				t.Errorf("end = %s, want %s", got, tt.wantEnd)
			}
			if start.Weekday() != time.Saturday {
				t.Errorf("start weekday = %s, want Saturday", start.Weekday())
			}
			// [start, end) must span exactly 7 days.
			if d := end.Sub(start); d != 7*24*time.Hour {
				t.Errorf("span = %v, want 168h", d)
			}
			// now must fall within [start, end).
			if now.Before(start) || !now.Before(end) {
				t.Errorf("now %s not within [%s, %s)", now, start, end)
			}
		})
	}
}
