package property

import (
	"testing"
	"time"
)

func TestParseVisitTimeUsesOrgLocation(t *testing.T) {
	bne, err := time.LoadLocation("Australia/Brisbane")
	if err != nil {
		t.Skip("no tzdata")
	}
	got, err := parseVisitTime("2026-11-24T09:00", bne)
	if err != nil || got.UTC().Format(time.RFC3339) != "2026-11-23T23:00:00Z" {
		t.Fatalf("got %v %v", got, err)
	}
	if v := VisitToResponse(Visit{StartsAt: got, EndsAt: got.Add(time.Hour)}, bne); v.StartsAt != "2026-11-24T09:00:00+10:00" {
		t.Fatalf("response starts_at = %s", v.StartsAt)
	}
	if _, err := parseVisitTime("tomorrow", bne); err == nil {
		t.Fatal("accepted garbage")
	}
}
