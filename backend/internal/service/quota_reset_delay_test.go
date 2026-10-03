package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseQuotaResetDelay(t *testing.T) {
	require.InDelta(t, (6*time.Hour + 53*time.Minute + 10*time.Second).Seconds(), parseQuotaResetDelay("Your quota will reset after 6h53m10s.").Seconds(), 1)
	require.Equal(t, time.Hour, parseQuotaResetDelay("resets in 166h"))
	require.Equal(t, 90*time.Second, parseQuotaResetDelay("90"))
	require.Equal(t, 12*time.Second, parseQuotaResetDelay("retry after 12s"))
	require.Equal(t, time.Duration(0), parseQuotaResetDelay("no delay here"))
}

func TestParseRetryAfterResetTimeNaturalLanguage(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	got := parseRetryAfterResetTime(http.Header{"Retry-After": []string{"after 45s"}}, now)
	require.NotNil(t, got)
	require.Equal(t, now.Add(45*time.Second), *got)
}
