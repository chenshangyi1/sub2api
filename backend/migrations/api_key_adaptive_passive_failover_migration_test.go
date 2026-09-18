package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAdaptivePassiveFailoverMigration(t *testing.T) {
	content, err := FS.ReadFile("197_api_key_adaptive_passive_failover.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS adaptive_passive_failover_enabled BOOLEAN NOT NULL DEFAULT FALSE")
}
