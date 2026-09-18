package migrations

import (
	"strings"
	"testing"
)

func TestAdaptiveGroupPoolsMigrationDefinesModelFirstTopology(t *testing.T) {
	content, err := FS.ReadFile("191_adaptive_group_pools.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS adaptive_group_configs",
		"CREATE TABLE IF NOT EXISTS adaptive_group_memberships",
		"UNIQUE (parent_group_id)",
		"UNIQUE (config_id, leaf_group_id)",
		"adaptive parent and leaf platforms must match",
		"next_adaptive_group_generation(config_generation)",
		"adaptive config generation exhausted",
		"g.status = 'active'",
		"REFERENCES groups(id) ON DELETE CASCADE",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration is missing %q", required)
		}
	}
	for _, forbidden := range []string{"models_list_config", "fallback_group_ids"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration must not use %q as Adaptive topology", forbidden)
		}
	}
}
