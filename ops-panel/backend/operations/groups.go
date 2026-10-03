package operations

import (
	"context"
	"fmt"
	"strings"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
)

type CreateTargetGroupInput struct {
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Platform       string  `json:"platform"`
	RateMultiplier float64 `json:"rate_multiplier"`
}

func (s *Service) ListTargetGroups(ctx context.Context, targetID uint) ([]sub2api.AdminGroup, error) {
	_, target, err := s.target(ctx, targetID)
	if err != nil {
		return nil, err
	}
	return s.admin.ListGroups(ctx, target, true)
}

func (s *Service) CreateTargetGroup(ctx context.Context, targetID uint, input CreateTargetGroupInput) (*sub2api.AdminGroup, error) {
	_, target, err := s.target(ctx, targetID)
	if err != nil {
		return nil, err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	if input.Name == "" {
		return nil, fmt.Errorf("%w: group name is required", ErrInvalid)
	}
	switch input.Platform {
	case "anthropic", "openai", "gemini", "antigravity", "grok", "composite":
	default:
		return nil, fmt.Errorf("%w: unsupported group platform", ErrInvalid)
	}
	if input.RateMultiplier <= 0 {
		return nil, fmt.Errorf("%w: group rate multiplier must be positive", ErrInvalid)
	}
	created, err := s.admin.CreateGroup(ctx, target, sub2api.AdminGroupCreateInput{
		Name: input.Name, Description: strings.TrimSpace(input.Description), Platform: input.Platform,
		RateMultiplier: input.RateMultiplier, UserVisible: true, Subscription: "standard",
	})
	if err != nil {
		return nil, err
	}
	s.recordAction(ctx, "create_target_group", fmt.Sprintf("target:%d/group:%d", targetID, created.ID), input.Name, true)
	return created, nil
}
