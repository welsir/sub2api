package service

import "time"

var naturalWeekLoc = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*60*60)
}()

// NaturalWeekRange returns the Saturday-first [start, end) week containing t.
func NaturalWeekRange(t time.Time, loc *time.Location) (start, end time.Time) {
	if loc == nil {
		loc = naturalWeekLoc
	}
	local := t.In(loc)
	offset := (int(local.Weekday()) + 1) % 7
	year, month, day := local.Date()
	start = time.Date(year, month, day, 0, 0, 0, 0, loc).AddDate(0, 0, -offset)
	end = start.AddDate(0, 0, 7)
	return start, end
}

func CurrentNaturalWeekRange() (start, end time.Time) {
	return NaturalWeekRange(time.Now(), naturalWeekLoc)
}

func NaturalWeekKey(t time.Time, loc *time.Location) string {
	start, _ := NaturalWeekRange(t, loc)
	return start.Format("2006-01-02")
}

func CurrentNaturalWeekKey() string {
	return NaturalWeekKey(time.Now(), naturalWeekLoc)
}
