package syncer

import (
	"context"
	"testing"

	"github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/storage"
)

func TestEnsureDefaultTargetFromEnvironmentCreatesOnce(t *testing.T) {
	cfg := DefaultTargetConfig{Name: "Primary Site", BaseURL: "http://sub2api:8080///", AdminAPIKey: "canonical-key"}
	db := openSyncerTestDB(t)
	svc := newTestService(t, db, &fakeChannelService{})

	created, err := svc.EnsureDefaultTarget(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ensure default target: %v", err)
	}
	if !created {
		t.Fatal("default target was not created")
	}
	created, err = svc.EnsureDefaultTarget(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ensure default target again: %v", err)
	}
	if created {
		t.Fatal("second ensure created another target")
	}

	targets, err := storage.NewUpstreamSyncTargets(db).List()
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("target count = %d, want 1", len(targets))
	}
	target := targets[0]
	if target.Name != "Primary Site" || target.BaseURL != "http://sub2api:8080" || !target.Enabled {
		t.Fatalf("default target = %#v", target)
	}
	cipher, err := crypto.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	adminAPIKey, err := cipher.Decrypt(target.AdminAPIKeyCipher)
	if err != nil {
		t.Fatalf("decrypt target key: %v", err)
	}
	if adminAPIKey != "canonical-key" {
		t.Fatalf("target key = %q, want primary environment key", adminAPIKey)
	}

	var shadow storage.Connection
	if err := db.First(&shadow, target.ID).Error; err != nil {
		t.Fatalf("find canonical shadow: %v", err)
	}
	if shadow.SyncMode != storage.ConnectionSyncModeCanonicalTarget || shadow.AdminAPIKey != "" {
		t.Fatalf("canonical shadow = %#v", shadow)
	}
}

func TestEnsureDefaultTargetFromEnvironmentUsesCompatibilityFallbacks(t *testing.T) {
	db := openSyncerTestDB(t)
	svc := newTestService(t, db, &fakeChannelService{})

	created, err := svc.EnsureDefaultTarget(context.Background(), DefaultTargetConfig{
		BaseURL:     "https://public.example.test",
		AdminAPIKey: "legacy-key",
	})
	if err != nil {
		t.Fatalf("ensure default target: %v", err)
	}
	if !created {
		t.Fatal("default target was not created")
	}
	targets, err := storage.NewUpstreamSyncTargets(db).List()
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	if len(targets) != 1 || targets[0].BaseURL != "https://public.example.test" {
		t.Fatalf("targets = %#v", targets)
	}
	cipher, err := crypto.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	adminAPIKey, err := cipher.Decrypt(targets[0].AdminAPIKeyCipher)
	if err != nil {
		t.Fatalf("decrypt target key: %v", err)
	}
	if adminAPIKey != "legacy-key" {
		t.Fatalf("target key = %q, want legacy environment key", adminAPIKey)
	}
}

func TestEnsureDefaultTargetFromEnvironmentPreservesExistingTarget(t *testing.T) {
	db := openSyncerTestDB(t)
	svc := newTestService(t, db, &fakeChannelService{})
	existing, err := svc.CreateTarget(context.Background(), TargetInput{
		Name:        "Existing",
		BaseURL:     "https://existing.example.test",
		AdminAPIKey: "existing-key",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("create existing target: %v", err)
	}

	created, err := svc.EnsureDefaultTarget(context.Background(), DefaultTargetConfig{
		BaseURL:     "https://replacement.example.test",
		AdminAPIKey: "replacement-key",
	})
	if err != nil {
		t.Fatalf("ensure default target: %v", err)
	}
	if created {
		t.Fatal("ensure replaced an existing target")
	}
	stored, err := storage.NewUpstreamSyncTargets(db).FindByID(existing.ID)
	if err != nil {
		t.Fatalf("find existing target: %v", err)
	}
	if stored.Name != "Existing" || stored.BaseURL != "https://existing.example.test" || !stored.Enabled {
		t.Fatalf("existing target changed: %#v", stored)
	}
	cipher, err := crypto.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	adminAPIKey, err := cipher.Decrypt(stored.AdminAPIKeyCipher)
	if err != nil {
		t.Fatalf("decrypt existing key: %v", err)
	}
	if adminAPIKey != "existing-key" {
		t.Fatalf("existing key changed: %q", adminAPIKey)
	}
}

func TestEnsureDefaultTargetFromEnvironmentSkipsMissingKeyAndDefaultsMissingURL(t *testing.T) {
	tests := []struct {
		name        string
		cfg         DefaultTargetConfig
		wantCreated bool
	}{
		{name: "missing base URL", cfg: DefaultTargetConfig{AdminAPIKey: "key"}, wantCreated: true},
		{name: "missing admin key", cfg: DefaultTargetConfig{BaseURL: "https://target.example.test"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openSyncerTestDB(t)
			svc := newTestService(t, db, &fakeChannelService{})
			created, err := svc.EnsureDefaultTarget(context.Background(), tt.cfg)
			if err != nil {
				t.Fatalf("ensure default target: %v", err)
			}
			if created != tt.wantCreated {
				t.Fatalf("created = %v, want %v", created, tt.wantCreated)
			}
			targets, err := storage.NewUpstreamSyncTargets(db).List()
			if err != nil {
				t.Fatalf("list targets: %v", err)
			}
			wantCount := 0
			if tt.wantCreated {
				wantCount = 1
			}
			if len(targets) != wantCount {
				t.Fatalf("target count = %d, want %d", len(targets), wantCount)
			}
			if tt.wantCreated && targets[0].BaseURL != defaultEnvironmentTargetBaseURL {
				t.Fatalf("default target base URL = %q", targets[0].BaseURL)
			}
		})
	}
}
