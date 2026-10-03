package operations

import (
	"strings"
	"testing"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
)

func TestResolveImportPlatformUsesTargetGroupPlatform(t *testing.T) {
	groups := []sub2api.AdminGroup{{ID: 7, Platform: "anthropic"}, {ID: 8, Platform: "openai"}}

	got, err := resolveImportPlatform(groups, []int64{7}, "openai")
	if err != nil {
		t.Fatalf("resolveImportPlatform returned error: %v", err)
	}
	if got != "anthropic" {
		t.Fatalf("platform = %q, want anthropic", got)
	}
}

func TestResolveImportPlatformNormalizesClaudeAlias(t *testing.T) {
	got, err := resolveImportPlatform(nil, []int64{7}, "claude")
	if err != nil {
		t.Fatalf("resolveImportPlatform returned error: %v", err)
	}
	if got != "anthropic" {
		t.Fatalf("platform = %q, want anthropic", got)
	}
}

func TestResolveImportPlatformRejectsMixedTargetPlatforms(t *testing.T) {
	groups := []sub2api.AdminGroup{{ID: 7, Platform: "anthropic"}, {ID: 8, Platform: "openai"}}

	_, err := resolveImportPlatform(groups, []int64{7, 8}, "anthropic")
	if err == nil || !strings.Contains(err.Error(), "different platforms") {
		t.Fatalf("error = %v, want mixed-platform validation error", err)
	}
}
