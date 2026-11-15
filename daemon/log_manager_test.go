package daemon

import (
	"testing"
	"time"
)

func TestNextCleanup(t *testing.T) {
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 3, 31, hour, minute, 0, 0, time.Local)
	}
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{at(9, 59), at(10, 0)},
		{at(10, 0), time.Date(2026, 4, 1, 10, 0, 0, 0, time.Local)},
		{at(23, 0), time.Date(2026, 4, 1, 10, 0, 0, 0, time.Local)},
	}
	for _, c := range cases {
		if got := nextCleanup(c.now); !got.Equal(c.want) {
			t.Errorf("nextCleanup(%v) = %v, want %v", c.now, got, c.want)
		}
	}
}
