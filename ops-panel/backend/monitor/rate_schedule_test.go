package monitor

import (
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
)

func TestRateScanDueRespectsMigratedInterval(t *testing.T) {
	last := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		channel storage.Channel
		now     time.Time
		want    bool
	}{
		{name: "upstream default", channel: storage.Channel{}, now: last, want: true},
		{name: "never scanned", channel: storage.Channel{RateIntervalMinutes: 30}, now: last, want: true},
		{name: "before interval", channel: storage.Channel{RateIntervalMinutes: 30, LastRateScanAt: &last}, now: last.Add(29 * time.Minute), want: false},
		{name: "at interval", channel: storage.Channel{RateIntervalMinutes: 30, LastRateScanAt: &last}, now: last.Add(30 * time.Minute), want: true},
		{name: "after interval", channel: storage.Channel{RateIntervalMinutes: 30, LastRateScanAt: &last}, now: last.Add(31 * time.Minute), want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := rateScanDue(test.channel, test.now); got != test.want {
				t.Fatalf("rateScanDue() = %v, want %v", got, test.want)
			}
		})
	}
}
