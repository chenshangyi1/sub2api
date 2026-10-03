package operations

import (
	"context"
	"fmt"
	"strings"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
)

// defaultImportedAccountPriority keeps new panel-imported accounts at the bottom
// of the Sub2API 1..30 scheduling band. The sub2api backend rejects priority 0
// after the custom priority-bounds change, and the extension worker re-ranks the
// account through the account-pool policy on its next cycle.
const defaultImportedAccountPriority = 30

type ImportAPIKeyInput struct {
	Name            string           `json:"name"`
	Platform        string           `json:"platform"`
	Type            string           `json:"type"`
	BaseURL         string           `json:"base_url"`
	APIKey          string           `json:"api_key"`
	SourceChannelID uint             `json:"source_channel_id"`
	SourceGroupID   string           `json:"source_group_id"`
	SourceGroupName string           `json:"source_group_name"`
	TargetGroupIDs  []int64          `json:"target_group_ids"`
	RatePolicy      *RatePolicyInput `json:"rate_policy"`
}

type ImportAPIKeyResult struct {
	Account    sub2api.AdminAccount `json:"account"`
	Created    bool                 `json:"created"`
	RatePolicy *RatePolicyTarget    `json:"rate_policy,omitempty"`
}

func (s *Service) ImportAPIKey(ctx context.Context, targetID uint, input ImportAPIKeyInput) (*ImportAPIKeyResult, error) {
	_, target, err := s.target(ctx, targetID)
	if err != nil {
		return nil, err
	}
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.Name = strings.TrimSpace(input.Name)
	input.Platform = strings.TrimSpace(input.Platform)
	input.Type = strings.TrimSpace(input.Type)
	if input.APIKey == "" || input.BaseURL == "" {
		return nil, fmt.Errorf("%w: base_url and api_key are required", ErrInvalid)
	}
	if input.Name == "" {
		return nil, fmt.Errorf("%w: account name is required", ErrInvalid)
	}
	if input.Platform == "" {
		input.Platform = "openai"
	}
	if input.Type == "" {
		input.Type = "apikey"
	}

	groups, err := s.admin.ListGroups(ctx, target, true)
	if err != nil {
		return nil, fmt.Errorf("list target groups: %w", err)
	}
	targetGroupIDs := uniquePositiveIDs(input.TargetGroupIDs)
	if len(targetGroupIDs) == 0 {
		return nil, fmt.Errorf("%w: select at least one target group", ErrInvalid)
	}
	availableGroupIDs := make(map[int64]struct{}, len(groups))
	for _, group := range groups {
		availableGroupIDs[group.ID] = struct{}{}
	}
	for _, groupID := range targetGroupIDs {
		if _, ok := availableGroupIDs[groupID]; !ok {
			return nil, fmt.Errorf("%w: target group %d does not exist", ErrInvalid, groupID)
		}
	}
	platform, err := resolveImportPlatform(groups, targetGroupIDs, input.Platform)
	if err != nil {
		return nil, err
	}
	input.Platform = platform

	if input.RatePolicy != nil && input.RatePolicy.Enabled {
		if len(input.RatePolicy.Bindings) == 0 {
			return nil, fmt.Errorf("%w: an enabled account rule needs a source binding", ErrInvalid)
		}
		workspace, workspaceErr := s.GetRatePolicies(ctx, targetID)
		if workspaceErr != nil {
			return nil, workspaceErr
		}
		for i := range input.RatePolicy.Bindings {
			binding := &input.RatePolicy.Bindings[i]
			if binding.ChannelID == 0 {
				binding.ChannelID = input.SourceChannelID
			}
			if strings.TrimSpace(binding.GroupID) == "" {
				binding.GroupID = strings.TrimSpace(input.SourceGroupID)
				if binding.GroupID == "" {
					binding.GroupID = strings.TrimSpace(input.SourceGroupName)
				}
			}
			if !hasRatePolicySource(workspace.Sources, binding.ChannelID, binding.GroupID) {
				return nil, fmt.Errorf("%w: source rate %d/%s is not collected yet", ErrInvalid, binding.ChannelID, binding.GroupID)
			}
		}
	}

	accounts, err := listAllTargetAccounts(func(page, pageSize int) ([]sub2api.AdminAccount, error) {
		return s.admin.ListAccounts(ctx, target, page, pageSize)
	})
	if err != nil {
		return nil, fmt.Errorf("list target accounts: %w", err)
	}
	var existing *sub2api.AdminAccount
	for i := range accounts {
		if strings.EqualFold(strings.TrimSpace(accounts[i].Name), input.Name) {
			copy := accounts[i]
			existing = &copy
			break
		}
	}
	credentials := map[string]any{}
	if existing != nil {
		for key, value := range existing.Credentials {
			credentials[key] = value
		}
	}
	credentials["api_key"] = input.APIKey
	credentials["base_url"] = input.BaseURL
	request := sub2api.AdminAccount{
		Name: input.Name, Platform: input.Platform, Type: input.Type, Status: "active",
		Credentials: credentials, RateMultiplier: 1, Concurrency: 10, LoadFactor: 1,
		Priority: defaultImportedAccountPriority,
		GroupIDs: targetGroupIDs,
	}
	created := existing == nil
	var account *sub2api.AdminAccount
	if existing != nil {
		request = *existing
		request.Name = input.Name
		if input.Platform != "" {
			request.Platform = input.Platform
		}
		if input.Type != "" {
			request.Type = input.Type
		}
		request.Credentials = credentials
		if len(targetGroupIDs) > 0 {
			request.GroupIDs = targetGroupIDs
		}
		account, err = s.admin.UpdateAccount(ctx, target, existing.ID, request)
	} else {
		account, err = s.admin.CreateAccount(ctx, target, request)
	}
	if err != nil {
		return nil, fmt.Errorf("write target account: %w", err)
	}

	result := &ImportAPIKeyResult{Account: *account, Created: created}
	if input.RatePolicy != nil {
		policy, policyErr := s.SaveRatePolicy(ctx, targetID, RatePolicyTargetAccount, account.ID, *input.RatePolicy)
		if policyErr != nil {
			return nil, fmt.Errorf("account written but rate policy was not saved: %w", policyErr)
		}
		result.RatePolicy = policy
		if policyErr := s.bindImportedGroups(ctx, targetID, targetGroupIDs, input.RatePolicy.Bindings); policyErr != nil {
			return nil, policyErr
		}
	}
	s.recordAction(ctx, "import_api_key_account", fmt.Sprintf("target:%d/account:%d", targetID, account.ID), input.Name, true)
	return result, nil
}

func (s *Service) bindImportedGroups(ctx context.Context, targetID uint, groupIDs []int64, sourceBindings []RatePolicyBindingInput) error {
	if len(groupIDs) == 0 || len(sourceBindings) == 0 {
		return nil
	}
	workspace, err := s.GetRatePolicies(ctx, targetID)
	if err != nil {
		return fmt.Errorf("load rate policies for group bindings: %w", err)
	}
	for _, groupID := range groupIDs {
		var existing *RatePolicyTarget
		for i := range workspace.Groups {
			if workspace.Groups[i].ID == groupID {
				existing = &workspace.Groups[i]
				break
			}
		}
		groupInput := RatePolicyInput{Enabled: true, Mode: "first", Offset: 0}
		if existing != nil {
			groupInput.Enabled = true
			groupInput.Mode = existing.Rule.Mode
			groupInput.Offset = existing.Rule.Offset
			groupInput.Expression = existing.Rule.Expression
			for _, binding := range existing.Bindings {
				groupInput.Bindings = append(groupInput.Bindings, RatePolicyBindingInput{ChannelID: binding.ChannelID, GroupID: binding.GroupID})
			}
		}
		for _, binding := range sourceBindings {
			groupInput.Bindings = append(groupInput.Bindings, RatePolicyBindingInput{ChannelID: binding.ChannelID, GroupID: binding.GroupID})
		}
		if _, groupErr := s.SaveRatePolicy(ctx, targetID, RatePolicyTargetGroup, groupID, groupInput); groupErr != nil {
			return fmt.Errorf("account written but group %d rate policy was not saved: %w", groupID, groupErr)
		}
	}
	return nil
}

func uniquePositiveIDs(values []int64) []int64 {
	seen := map[int64]struct{}{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func hasRatePolicySource(sources []RatePolicySource, channelID uint, groupID string) bool {
	groupID = strings.TrimSpace(groupID)
	for _, source := range sources {
		if source.ChannelID == channelID && strings.TrimSpace(source.GroupID) == groupID {
			return true
		}
	}
	return false
}

func normalizeImportPlatform(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "claude":
		return "anthropic"
	case "anthropic", "openai", "gemini", "antigravity", "kiro", "grok", "composite":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.TrimSpace(value)
	}
}

func resolveImportPlatform(groups []sub2api.AdminGroup, targetGroupIDs []int64, requested string) (string, error) {
	byID := make(map[int64]sub2api.AdminGroup, len(groups))
	for _, group := range groups {
		byID[group.ID] = group
	}

	resolved := ""
	for _, groupID := range targetGroupIDs {
		platform := normalizeImportPlatform(byID[groupID].Platform)
		if platform == "" {
			continue
		}
		if resolved != "" && !strings.EqualFold(resolved, platform) {
			return "", fmt.Errorf("%w: target groups use different platforms", ErrInvalid)
		}
		resolved = platform
	}
	if resolved != "" {
		return resolved, nil
	}

	requested = normalizeImportPlatform(requested)
	if requested == "" {
		return "openai", nil
	}
	return requested, nil
}
