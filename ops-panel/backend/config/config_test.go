package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAppliesUpstreamDefaults(t *testing.T) {
	cfg, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Upstream.TimeoutSeconds != DefaultUpstreamTimeoutSeconds {
		t.Fatalf("timeout seconds = %d", cfg.Upstream.TimeoutSeconds)
	}
	if cfg.Upstream.UserAgent != DefaultUpstreamUserAgent {
		t.Fatalf("user agent = %q", cfg.Upstream.UserAgent)
	}
}

func TestUpstreamConfigWithDefaultsKeepsCustomUserAgent(t *testing.T) {
	cfg := UpstreamConfig{
		TimeoutSeconds: 0,
		UserAgent:      "custom-agent",
	}.WithDefaults()
	if cfg.TimeoutSeconds != DefaultUpstreamTimeoutSeconds {
		t.Fatalf("timeout seconds = %d", cfg.TimeoutSeconds)
	}
	if cfg.UserAgent != "custom-agent" {
		t.Fatalf("user agent = %q", cfg.UserAgent)
	}
}

func TestGatewayConfigWithDefaults(t *testing.T) {
	cfg := GatewayConfig{}.WithDefaults()
	if cfg.TempPauseSeconds != DefaultGatewayTempPauseSeconds {
		t.Fatalf("temp pause = %d", cfg.TempPauseSeconds)
	}
	if cfg.ForwardTimeoutSeconds != DefaultGatewayForwardTimeoutSeconds {
		t.Fatalf("forward timeout = %d", cfg.ForwardTimeoutSeconds)
	}
	if cfg.RouteBatchConcurrency != DefaultGatewayRouteBatchConcurrency {
		t.Fatalf("batch concurrency = %d", cfg.RouteBatchConcurrency)
	}
	custom := GatewayConfig{RouteBatchConcurrency: 16, ForwardTimeoutSeconds: 120}.WithDefaults()
	if custom.RouteBatchConcurrency != 16 || custom.ForwardTimeoutSeconds != 120 {
		t.Fatalf("custom = %#v", custom)
	}
	if custom.ModelsCacheTTLSeconds != DefaultGatewayModelsCacheTTLSeconds {
		t.Fatalf("models cache ttl = %d", custom.ModelsCacheTTLSeconds)
	}
}

func TestLoadBindsDatabaseURLAndLegacyAdminSeedVariables(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://user:secret@db.example:5432/ops?schema=public")
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("OPS_PANEL_ADMIN_USERNAME", "legacy-admin@example.com")
	t.Setenv("OPS_PANEL_ADMIN_PASSWORD", "legacy-password")
	t.Setenv("ADMIN_USERNAME", "upstream-admin")
	t.Setenv("ADMIN_PASSWORD", "upstream-password")

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.URL == "" {
		t.Fatal("database URL was not loaded")
	}
	if got := cfg.Database.ToStorageConfig().EffectiveDriver(); got != "postgres" {
		t.Fatalf("effective driver = %q", got)
	}
	if cfg.Auth.Username != "legacy-admin@example.com" || cfg.Auth.Password != "legacy-password" {
		t.Fatalf("legacy admin seed variables were not preferred: username=%q", cfg.Auth.Username)
	}
}

func TestSaveDoesNotPersistDatabaseURLCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &Config{Database: DatabaseConfig{URL: "postgresql://user:secret@db.example/ops"}}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(body), "postgresql://") || strings.Contains(string(body), "secret") {
		t.Fatalf("generated config contains DATABASE_URL credentials: %s", body)
	}
}

func TestLoadBindsLegacyDefaultTargetVariables(t *testing.T) {
	t.Setenv("SUB2API_SITE_NAME", "Primary Site")
	t.Setenv("SUB2API_BASE_URL", "http://sub2api:8080")
	t.Setenv("SUB2API_PUBLIC_URL", "https://public.example.test")
	t.Setenv("SUB2API_ADMIN_API_KEY", "canonical-key")
	t.Setenv("OPS_PANEL_ADMIN_API_KEY", "legacy-key")

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultTarget.Name != "Primary Site" || cfg.DefaultTarget.BaseURL != "http://sub2api:8080" {
		t.Fatalf("default target identity = %#v", cfg.DefaultTarget)
	}
	if cfg.DefaultTarget.AdminAPIKey != "canonical-key" {
		t.Fatalf("default target key did not prefer SUB2API_ADMIN_API_KEY")
	}
}

func TestLoadBindsDefaultTargetCompatibilityFallbacks(t *testing.T) {
	t.Setenv("SUB2API_PUBLIC_URL", "https://public.example.test")
	t.Setenv("OPS_PANEL_ADMIN_API_KEY", "legacy-key")

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultTarget.BaseURL != "https://public.example.test" || cfg.DefaultTarget.AdminAPIKey != "legacy-key" {
		t.Fatalf("default target fallbacks = %#v", cfg.DefaultTarget)
	}
}

func TestSaveDoesNotPersistDefaultTargetCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &Config{DefaultTarget: DefaultTargetConfig{
		Name:        "Primary Site",
		BaseURL:     "https://target.example.test",
		AdminAPIKey: "secret-key",
	}}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, secret := range []string{"Primary Site", "target.example.test", "secret-key"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("generated config contains default target secret %q: %s", secret, body)
		}
	}
}
