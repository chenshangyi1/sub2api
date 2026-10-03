package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyBlockOpenAIFastMigrations(t *testing.T) {
	first, err := FS.ReadFile("185_api_key_block_openai_fast.sql")
	require.NoError(t, err)
	firstSQL := strings.Join(strings.Fields(string(first)), " ")
	require.Contains(t, firstSQL, "ADD COLUMN IF NOT EXISTS block_openai_fast BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, firstSQL, "COMMENT ON COLUMN api_keys.block_openai_fast")

	second, err := FS.ReadFile("186_api_key_block_openai_fast_default_on.sql")
	require.NoError(t, err)
	secondSQL := strings.Join(strings.Fields(string(second)), " ")
	require.Contains(t, secondSQL, "ALTER COLUMN block_openai_fast SET DEFAULT TRUE")
	require.Contains(t, secondSQL, "UPDATE api_keys SET block_openai_fast = TRUE")
	require.Contains(t, secondSQL, "AND deleted_at IS NULL")
}
