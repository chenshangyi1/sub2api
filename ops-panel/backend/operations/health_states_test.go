package operations

import (
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/health"
)

func TestClassifyMonitorResult(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		row  monitorResultRow
		want health.Kind
	}{
		{"success", monitorResultRow{Status: "success", CreatedAt: now}, health.KindSuccess},
		{"operational", monitorResultRow{Status: "operational", CreatedAt: now}, health.KindSuccess},
		{"network timeout", monitorResultRow{Status: "error", Message: "request timed out after 30s", CreatedAt: now}, health.KindSoftFailure},
		{"rate limited", monitorResultRow{Status: "error", Message: "429 too many requests", CreatedAt: now}, health.KindSoftFailure},
		{"auth failure", monitorResultRow{Status: "error", Message: "401 unauthorized", CreatedAt: now}, health.KindHardFailure},
		{"forbidden", monitorResultRow{Status: "error", Message: "permission denied", CreatedAt: now}, health.KindHardFailure},
		{"server error", monitorResultRow{Status: "error", Message: "upstream returned 502", CreatedAt: now}, health.KindHardFailure},
		{"parameter error", monitorResultRow{Status: "error", Message: "400 invalid model", CreatedAt: now}, health.KindInvalidResponse},
	}
	for _, tc := range cases {
		got := health.Classify(classifyMonitorResult(tc.row))
		if got != tc.want {
			t.Errorf("%s: classify = %s, want %s", tc.name, got, tc.want)
		}
	}
}
