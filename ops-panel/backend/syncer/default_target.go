package syncer

import (
	"context"
	"fmt"
	"strings"
)

const (
	defaultEnvironmentTargetName    = "Default Sub2API"
	defaultEnvironmentTargetBaseURL = "http://sub2api:8080"
)

type DefaultTargetConfig struct {
	Name        string
	BaseURL     string
	AdminAPIKey string
}

// EnsureDefaultTarget imports legacy single-site settings only when the
// canonical target store is still empty.
func (s *Service) EnsureDefaultTarget(ctx context.Context, cfg DefaultTargetConfig) (bool, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	adminAPIKey := strings.TrimSpace(cfg.AdminAPIKey)
	if adminAPIKey == "" {
		return false, nil
	}
	if baseURL == "" {
		baseURL = defaultEnvironmentTargetBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	targetName := strings.TrimSpace(cfg.Name)
	if targetName == "" {
		targetName = defaultEnvironmentTargetName
	}

	targets, err := s.ListTargets()
	if err != nil {
		return false, fmt.Errorf("list canonical targets: %w", err)
	}
	if len(targets) > 0 {
		return false, nil
	}

	_, err = s.CreateTarget(ctx, TargetInput{
		Name:        targetName,
		BaseURL:     baseURL,
		AdminAPIKey: adminAPIKey,
		Enabled:     true,
	})
	if err == nil {
		return true, nil
	}

	// A concurrent startup may have populated the target after our empty read.
	// Treat that as an idempotent no-op and never rewrite the winner.
	targets, listErr := s.ListTargets()
	if listErr == nil && len(targets) > 0 {
		return false, nil
	}
	return false, fmt.Errorf("create default canonical target: %w", err)
}
