package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdaptiveSettlementRejectedMigration_AllowsOrphanVoidStatus(t *testing.T) {
	content, err := FS.ReadFile("255_adaptive_settlement_status_rejected.sql")
	require.NoError(t, err)
	sqlText := string(content)

	for _, required := range []string{
		"adaptive_settlement_status",
		"'pending'",
		"'captured'",
		"'released'",
		"'failed'",
		"'rejected'",
		"DROP CONSTRAINT IF EXISTS usage_logs_adaptive_settlement_status_check",
	} {
		require.Contains(t, sqlText, required)
	}
}
