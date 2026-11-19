package module

import (
	"strings"
	"testing"
	"time"
)

func TestIdleReaderExtendsDeadlineOnEachRead(t *testing.T) {
	var deadlines []time.Time
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := &idleReader{
		r:     strings.NewReader("abcdef"),
		idle:  time.Minute,
		now:   func() time.Time { now = now.Add(time.Second); return now },
		setRD: func(d time.Time) error { deadlines = append(deadlines, d); return nil },
	}
	buf := make([]byte, 2)
	for i := 0; i < 3; i++ {
		if _, err := r.Read(buf); err != nil {
			t.Fatal(err)
		}
	}
	if len(deadlines) != 3 || !deadlines[2].After(deadlines[0]) || deadlines[0].Sub(time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)) != time.Minute {
		t.Fatalf("deadlines = %v", deadlines)
	}
}
