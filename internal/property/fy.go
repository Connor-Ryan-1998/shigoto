package property

import "time"

// FYRange returns the financial year containing today (a calendar date) for
// an org whose financial year starts on the first of fyStartMonth: the
// inclusive first and last days, at UTC midnight. A month outside 1-12
// falls back to July, the Australian financial year.
func FYRange(fyStartMonth int, today time.Time) (from, to time.Time) {
	if fyStartMonth < 1 || fyStartMonth > 12 {
		fyStartMonth = 7
	}
	year := today.Year()
	if int(today.Month()) < fyStartMonth {
		year--
	}
	from = time.Date(year, time.Month(fyStartMonth), 1, 0, 0, 0, 0, time.UTC)
	to = from.AddDate(1, 0, -1)
	return from, to
}
