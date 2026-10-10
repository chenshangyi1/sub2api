package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountStatsRulesUserBillingMigration_IsBackwardCompatible(t *testing.T) {
	content, err := FS.ReadFile("256_account_stats_rules_user_billing.sql")
	require.NoError(t, err)
	require.Contains(t, string(content), "ADD COLUMN IF NOT EXISTS apply_to_user_billing BOOLEAN NOT NULL DEFAULT FALSE")
}
