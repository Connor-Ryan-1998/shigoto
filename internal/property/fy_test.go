package property

import (
	"testing"
	"time"
)

func TestFYRange(t *testing.T) {
	d := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		month          int
		today          string
		wantFrom, want string
	}{
		{7, "2026-11-19", "2026-07-01", "2027-06-30"},
		{7, "2026-06-30", "2025-07-01", "2026-06-30"},
		{7, "2026-07-01", "2026-07-01", "2027-06-30"},
		{1, "2026-11-19", "2026-01-01", "2026-12-31"},
		{4, "2026-03-31", "2025-04-01", "2026-03-31"},
		{0, "2026-11-19", "2026-07-01", "2027-06-30"}, // invalid month falls back to July
	}
	for _, c := range cases {
		from, to := FYRange(c.month, d(c.today))
		if from.Format("2006-01-02") != c.wantFrom || to.Format("2006-01-02") != c.want {
			t.Errorf("FYRange(%d, %s) = %s..%s, want %s..%s", c.month, c.today,
				from.Format("2006-01-02"), to.Format("2006-01-02"), c.wantFrom, c.want)
		}
	}
}
