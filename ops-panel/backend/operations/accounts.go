package operations

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
)

func (s *Service) ListAccounts(ctx context.Context, targetID uint, filter AccountFilter) (*AccountPage, error) {
	_, target, err := s.target(ctx, targetID)
	if err != nil {
		return nil, err
	}
	remote, err := listAllTargetAccounts(func(page, pageSize int) ([]sub2api.AdminAccount, error) {
		return s.admin.ListAccounts(ctx, target, page, pageSize)
	})
	if err != nil {
		return nil, fmt.Errorf("list target accounts: %w", err)
	}
	thresholds, defaultThreshold, _ := s.balanceThresholds(ctx, targetID)
	healthStates, _ := s.AccountHealthStates(ctx, targetID)

	items := make([]Account, 0, len(remote))
	for _, raw := range remote {
		item := mapRemoteAccount(raw)
		if state, ok := healthStates[item.ID]; ok {
			item.HealthState = string(state.State)
			item.HealthWeight = state.WeightPercent
			score := state.Score
			item.HealthScore = &score
		}
		if threshold, ok := thresholds[item.ID]; ok {
			item.BalanceThreshold = &threshold
		} else if defaultThreshold > 0 {
			value := defaultThreshold
			item.BalanceThreshold = &value
		}
		if filter.Search != "" && !containsFold(item.Name+" "+item.Platform+" "+item.Type+" "+item.LastError, filter.Search) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(filter.Schedule)) {
		case "enabled":
			if !item.Schedulable {
				continue
			}
		case "disabled":
			if item.Schedulable {
				continue
			}
		case "errors":
			if item.LastError == "" && !strings.EqualFold(item.Status, "error") {
				continue
			}
		case "temporary_unavailable":
			if !item.TemporaryUnavailable {
				continue
			}
		case "", "all":
		default:
			return nil, fmt.Errorf("%w: unsupported schedule filter", ErrInvalid)
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	summary := AccountSummary{Total: len(items)}
	for _, item := range items {
		if item.Schedulable {
			summary.Schedulable++
		}
		if item.LastError != "" || strings.EqualFold(item.Status, "error") {
			summary.Errors++
		}
		if item.TemporaryUnavailable {
			summary.TemporaryUnavailable++
		}
		if item.Balance != nil && item.BalanceThreshold != nil && *item.Balance < *item.BalanceThreshold {
			summary.BalanceLow++
		}
	}

	page := clampInt(filter.Page, 1, 1_000_000)
	pageSize := clampInt(filter.PageSize, 1, 200)
	pages := 1
	if len(items) > 0 {
		pages = (len(items) + pageSize - 1) / pageSize
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return &AccountPage{
		Items: items[start:end], Summary: summary, Total: len(items),
		Page: page, PageSize: pageSize, Pages: pages,
	}, nil
}

func listAllTargetAccounts(loadPage func(int, int) ([]sub2api.AdminAccount, error)) ([]sub2api.AdminAccount, error) {
	const pageSize = 1000
	items := make([]sub2api.AdminAccount, 0, pageSize)
	seen := map[int64]bool{}
	for page := 1; page <= 1000; page++ {
		batch, err := loadPage(page, pageSize)
		if err != nil {
			return nil, fmt.Errorf("load target accounts page %d: %w", page, err)
		}
		before := len(items)
		for _, account := range batch {
			if account.ID <= 0 || seen[account.ID] {
				continue
			}
			seen[account.ID] = true
			items = append(items, account)
		}
		if len(batch) < pageSize {
			return items, nil
		}
		if len(items) == before {
			return nil, fmt.Errorf("load target accounts page %d: pagination made no progress", page)
		}
	}
	return nil, fmt.Errorf("load target accounts: pagination limit reached")
}

func mapRemoteAccount(raw sub2api.AdminAccount) Account {
	rate := raw.RateMultiplier
	groupNames := make([]string, 0, len(raw.Groups)+len(raw.AccountGroups))
	seen := map[string]struct{}{}
	for _, group := range raw.Groups {
		name := strings.TrimSpace(group.Name)
		if name != "" {
			seen[name] = struct{}{}
			groupNames = append(groupNames, name)
		}
	}
	for _, relation := range raw.AccountGroups {
		name := strings.TrimSpace(relation.Group.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		groupNames = append(groupNames, name)
	}
	var loadFactor *float64
	if raw.LoadFactor > 0 {
		value := raw.LoadFactor
		loadFactor = &value
	}
	var balance *float64
	if raw.QuotaLimit != nil && raw.QuotaUsed != nil {
		value := *raw.QuotaLimit - *raw.QuotaUsed
		balance = &value
	}
	var expiresAt *time.Time
	if raw.ExpiresAt != nil && *raw.ExpiresAt > 0 {
		value := *raw.ExpiresAt
		if value > 10_000_000_000 {
			value /= 1000
		}
		parsed := time.Unix(value, 0).UTC()
		expiresAt = &parsed
	}
	return Account{
		ID: raw.ID, Name: raw.Name, Platform: raw.Platform, Type: raw.Type,
		Status: raw.Status, Schedulable: raw.Schedulable, Concurrency: raw.Concurrency,
		Priority: raw.Priority, LoadFactor: loadFactor, RateMultiplier: rate,
		GroupNames: groupNames, Balance: balance, BalanceCurrency: "USD",
		ExpiresAt: expiresAt, LastError: raw.ErrorMessage, UpdatedAt: raw.UpdatedAt,
		TemporaryUnavailable:       raw.TempUnschedulableUntil != nil && raw.TempUnschedulableUntil.After(time.Now()),
		TemporaryUnavailableUntil:  raw.TempUnschedulableUntil,
		TemporaryUnavailableReason: raw.TempUnschedulableReason,
	}
}

func (s *Service) RunAccountAction(ctx context.Context, targetID uint, accountID int64, action string) error {
	_, target, err := s.target(ctx, targetID)
	if err != nil {
		return err
	}
	if accountID <= 0 {
		return fmt.Errorf("%w: account id is required", ErrInvalid)
	}
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "enable":
		_, err = s.admin.SetAccountSchedulable(ctx, target, accountID, true)
	case "disable":
		_, err = s.admin.SetAccountSchedulable(ctx, target, accountID, false)
	case "clear_error":
		_, err = s.admin.ClearAccountError(ctx, target, accountID)
	case "refresh":
		_, err = s.admin.RefreshAccount(ctx, target, accountID)
	case "check_balance":
		_, err = s.admin.GetAccountUsage(ctx, target, accountID)
	default:
		return fmt.Errorf("%w: unsupported account action %q", ErrInvalid, action)
	}
	targetLabel := fmt.Sprintf("target:%d/account:%d", targetID, accountID)
	if err != nil {
		s.recordAction(ctx, "account_"+action, targetLabel, err.Error(), false)
		return fmt.Errorf("run target account action: %w", err)
	}
	if action == "disable" {
		s.detachAccountRateBindings(ctx, targetID, accountID)
	}
	s.recordAction(ctx, "account_"+action, targetLabel, "", true)
	return nil
}

func (s *Service) detachAccountRateBindings(ctx context.Context, targetID uint, accountID int64) {
	workspace, err := s.GetRatePolicies(ctx, targetID)
	if err != nil {
		return
	}
	for i := range workspace.Accounts {
		if workspace.Accounts[i].ID != accountID {
			continue
		}
		if len(workspace.Accounts[i].Bindings) == 0 && !workspace.Accounts[i].Rule.Enabled {
			return
		}
		_, _ = s.SaveRatePolicy(ctx, targetID, RatePolicyTargetAccount, accountID, RatePolicyInput{
			Enabled: false, Mode: "first", Offset: 0, Bindings: []RatePolicyBindingInput{},
		})
		return
	}
}

func (s *Service) DeleteAccount(ctx context.Context, targetID uint, accountID int64) error {
	_, target, err := s.target(ctx, targetID)
	if err != nil {
		return err
	}
	if accountID <= 0 {
		return fmt.Errorf("%w: account id is required", ErrInvalid)
	}
	if err := s.admin.DeleteAccount(ctx, target, accountID); err != nil {
		s.recordAction(ctx, "delete_target_account", fmt.Sprintf("target:%d/account:%d", targetID, accountID), err.Error(), false)
		return fmt.Errorf("delete target account: %w", err)
	}
	if connectionID, cerr := s.extensionConnectionID(ctx, targetID); cerr == nil {
		if s.db.Migrator().HasTable("bl_source_bindings") {
			_ = s.db.WithContext(ctx).Table("bl_source_bindings").
				Where("connection_id = ? AND target_type = ? AND target_id = ?", connectionID, RatePolicyTargetAccount, accountID).
				Delete(nil).Error
		}
		if s.db.Migrator().HasTable("bl_account_rate_rules") {
			_ = s.db.WithContext(ctx).Table("bl_account_rate_rules").
				Where("connection_id = ? AND account_id = ?", connectionID, accountID).
				Delete(nil).Error
		}
	}
	s.recordAction(ctx, "delete_target_account", fmt.Sprintf("target:%d/account:%d", targetID, accountID), "", true)
	return nil
}
