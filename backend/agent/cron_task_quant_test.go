package agent

import (
	"testing"
	"time"
)

func TestATradingSessionClockExcludesAuctionAndLunch(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	cases := []struct {
		clock string
		want  bool
	}{
		{"09:15", false},
		{"09:29", false},
		{"09:30", true},
		{"11:30", true},
		{"11:31", false},
		{"12:59", false},
		{"13:00", true},
		{"15:00", true},
		{"15:01", false},
	}
	for _, tc := range cases {
		at, err := time.ParseInLocation("2006-01-02 15:04", "2026-07-13 "+tc.clock, loc)
		if err != nil {
			t.Fatal(err)
		}
		if got := isATradingSessionClock(at); got != tc.want {
			t.Fatalf("clock %s: got %v want %v", tc.clock, got, tc.want)
		}
	}
}
