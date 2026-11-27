package quotes

import (
	"testing"
	"time"
)

func TestFormatNumber(t *testing.T) {
	cases := []struct {
		number, revision int
		want             string
	}{
		{7, 1, "Q-0007"},
		{7, 2, "Q-0007-R2"},
		{12345, 1, "Q-12345"},
	}
	for _, c := range cases {
		if got := FormatNumber(c.number, c.revision); got != c.want {
			t.Errorf("FormatNumber(%d, %d) = %q, want %q", c.number, c.revision, got, c.want)
		}
	}
}

func TestDisplayStatus(t *testing.T) {
	today := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		status     string
		validUntil time.Time
		want       string
	}{
		{StatusSent, today, StatusSent},
		{StatusSent, today.AddDate(0, 0, -1), DisplayExpired},
		{StatusDraft, today.AddDate(0, 0, -1), StatusDraft},
		{StatusAccepted, today.AddDate(0, 0, -1), StatusAccepted},
	}
	for _, c := range cases {
		q := Quote{Status: c.status, ValidUntil: c.validUntil}
		if got := q.DisplayStatus(today); got != c.want {
			t.Errorf("%s valid until %s: DisplayStatus = %q, want %q", c.status, c.validUntil.Format("2006-01-02"), got, c.want)
		}
	}
}
