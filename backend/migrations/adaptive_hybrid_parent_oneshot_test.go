package migrations

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAdaptiveHybridParentSQLStaysOutOfEmbeddedMigrations(t *testing.T) {
	_, err := FS.ReadFile("256_adaptive_hybrid_parent_platform.sql")
	if err == nil {
		t.Fatal("256 must not be embedded; canary validate would fail")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller path unavailable")
	}
	oneshot := filepath.Join(filepath.Dir(thisFile), "..", "..", "work", "adaptive-hybrid-parent-platform.sql")
	content, err := os.ReadFile(oneshot)
	if err != nil {
		t.Fatalf("one-shot SQL missing: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"CREATE OR REPLACE FUNCTION validate_adaptive_group_membership()",
		"CREATE OR REPLACE FUNCTION validate_adaptive_group_platform_update()",
		"parent_platform IS DISTINCT FROM 'adaptive'",
		"IF NEW.platform = 'adaptive' THEN",
		"SET platform = 'adaptive'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("one-shot SQL is missing %q", required)
		}
	}
}
