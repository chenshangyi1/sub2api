package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdaptiveBillingCacheInvalidationMigration_CoversReservationSnapshots(t *testing.T) {
	content, err := FS.ReadFile("190_adaptive_billing_cache_invalidation.sql")
	require.NoError(t, err)
	sqlText := string(content)

	for _, required := range []string{
		"CREATE OR REPLACE FUNCTION enqueue_adaptive_api_key_auth_cache_invalidation()",
		"AFTER UPDATE OF reserved_quota_usd, reserved_usage_5h_usd",
		"OLD.reserved_quota_usd IS DISTINCT FROM NEW.reserved_quota_usd",
		"OLD.reserved_usage_5h_usd IS DISTINCT FROM NEW.reserved_usage_5h_usd",
		"OLD.reserved_usage_1d_usd IS DISTINCT FROM NEW.reserved_usage_1d_usd",
		"OLD.reserved_usage_7d_usd IS DISTINCT FROM NEW.reserved_usage_7d_usd",
		"CREATE OR REPLACE FUNCTION enqueue_adaptive_user_auth_cache_invalidation()",
		"AFTER UPDATE OF adaptive_reserved_balance ON users",
		"OLD.adaptive_reserved_balance IS DISTINCT FROM NEW.adaptive_reserved_balance",
		"INSERT INTO auth_cache_invalidation_outbox (cache_key)",
		"encode(sha256(convert_to(k.key, 'UTF8')), 'hex')",
	} {
		require.Contains(t, sqlText, required)
	}
	require.NotContains(t, sqlText, "OLD.balance")
	require.NotContains(t, sqlText, "OLD.quota_used")
	require.NotContains(t, sqlText, "CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation()")
	require.NotContains(t, sqlText, "CREATE OR REPLACE FUNCTION enqueue_user_auth_cache_invalidation()")
	require.NotContains(t, sqlText, "k.key) VALUES")
}
