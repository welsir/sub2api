package service

import "time"

// 自然周定义：周六为一周的第一天、周五为最后一天（公司周用量口径）。
// 周边界按指定时区计算，默认 Asia/Shanghai。

// naturalWeekLoc 是计算自然周边界使用的时区，默认 Asia/Shanghai。
// 加载失败时回退到固定 UTC+8。
var naturalWeekLoc = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*60*60)
}()

// NaturalWeekRange 返回包含 t 的自然周的 [start, end) 边界（左闭右开），
// 周起点为最近的周六 00:00:00，end 为下一个周六 00:00:00，按 loc 计算。
// loc 为 nil 时使用默认自然周时区。
func NaturalWeekRange(t time.Time, loc *time.Location) (start, end time.Time) {
	if loc == nil {
		loc = naturalWeekLoc
	}
	lt := t.In(loc)
	// time.Weekday: Sunday=0 ... Saturday=6。
	// 距最近周六（含当天）的天数：Sat=0, Sun=1, Mon=2, ..., Fri=6。
	offset := (int(lt.Weekday()) + 1) % 7
	y, m, d := lt.Date()
	start = time.Date(y, m, d, 0, 0, 0, 0, loc).AddDate(0, 0, -offset)
	end = start.AddDate(0, 0, 7)
	return start, end
}

// CurrentNaturalWeekRange 返回当前时刻所在自然周的 [start, end) 边界。
func CurrentNaturalWeekRange() (start, end time.Time) {
	return NaturalWeekRange(time.Now(), naturalWeekLoc)
}

// NaturalWeekKey 返回自然周起始日的 YYYY-MM-DD 标识，用于周维度去重（如告警一周一次）。
func NaturalWeekKey(t time.Time, loc *time.Location) string {
	start, _ := NaturalWeekRange(t, loc)
	return start.Format("2006-01-02")
}
