package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// MobileUser 移动端用户查询返回的用户信息。
type MobileUser struct {
	ID       int64   `json:"id"`
	Username string  `json:"username"`
	Email    string  `json:"email"`
	Balance  float64 `json:"balance"`
}

// FindUsers 按 id / email / username 模糊查询 sub2api 用户（未删除）。
func (s *Service) FindUsers(ctx context.Context, query string, limit int) ([]MobileUser, error) {
	if s.mainDB == nil {
		return nil, fmt.Errorf("%w: SUB2API_DATABASE_URL is not configured", ErrInvalid)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: query is required", ErrInvalid)
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	pattern := "%" + query + "%"
	var rows []MobileUser
	err := s.mainDB.WithContext(ctx).Raw(
		`SELECT u.id, u.username, u.email, u.balance
		 FROM users u
		 WHERE u.deleted_at IS NULL
		   AND (u.id::text = ? OR u.email ILIKE ? OR u.username ILIKE ?)
		 ORDER BY u.id
		 LIMIT ?`,
		query, pattern, pattern, limit,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// TopupByIdentifier 支持按用户 ID（纯数字）或 email/username 充值。
func (s *Service) TopupByIdentifier(ctx context.Context, identifier string, amount float64, actor string) (*TopupResult, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("%w: user id or email is required", ErrInvalid)
	}
	if id, err := strconv.ParseInt(identifier, 10, 64); err == nil && id > 0 {
		return s.Topup(ctx, id, amount, actor)
	}
	users, err := s.FindUsers(ctx, identifier, 1)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("%w: user %q not found", ErrNotFound, identifier)
	}
	return s.Topup(ctx, users[0].ID, amount, actor)
}

// TopupResult 充值结果。
type TopupResult struct {
	UserID         int64   `json:"user_id"`
	BalanceBefore  float64 `json:"balance_before"`
	BalanceAfter   float64 `json:"balance_after"`
	Amount         float64 `json:"amount"`
	DisplayName    string  `json:"display_name"`
}

// Topup 直接给 Sub2API 主库用户余额充值并写审计日志。
// 与 Telegram 机器人充值逻辑一致（直连主库），供移动端管理使用。
func (s *Service) Topup(ctx context.Context, userID int64, amount float64, actor string) (*TopupResult, error) {
	if s.mainDB == nil {
		return nil, fmt.Errorf("%w: SUB2API_DATABASE_URL is not configured", ErrInvalid)
	}
	if userID <= 0 {
		return nil, fmt.Errorf("%w: user id is required", ErrInvalid)
	}
	if amount <= 0 {
		return nil, fmt.Errorf("%w: amount must be positive", ErrInvalid)
	}

	result := &TopupResult{UserID: userID, Amount: amount}
	err := s.mainDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct {
			ID       int64
			Username string
			Email    string
			Balance  float64
		}
		err := tx.Raw(
			"SELECT u.id, u.username, u.email, u.balance FROM users u WHERE u.id = ? AND u.deleted_at IS NULL LIMIT 1 FOR UPDATE",
			userID,
		).Scan(&row).Error
		if err != nil {
			return err
		}
		if row.ID == 0 {
			return fmt.Errorf("%w: user %d not found", ErrNotFound, userID)
		}

		newBalance := row.Balance + amount
		if err := tx.Exec(
			"UPDATE users SET balance = balance + ?, total_recharged = total_recharged + ?, updated_at = now() WHERE id = ?",
			amount, amount, userID,
		).Error; err != nil {
			return err
		}

		payload, _ := json.Marshal(map[string]any{
			"target_user_id": userID,
			"amount":         amount,
			"balance_before": row.Balance,
			"balance_after":  newBalance,
		})
		extra, _ := json.Marshal(map[string]any{
			"source":         "mobile_app",
			"actor":          actor,
			"target_user_id": userID,
		})
		if err := tx.Exec(
			`INSERT INTO audit_logs (
				created_at, actor_email, actor_role, auth_method, action,
				method, path, request_body, status_code, latency_ms, extra
			) VALUES (now(), ?, 'admin', 'panel', 'bot.users.balance.topup',
				'API', '/api/mobile/topup', ?::jsonb, 200, 0, ?::jsonb)`,
			actor, string(payload), string(extra),
		).Error; err != nil {
			return err
		}

		result.BalanceBefore = row.Balance
		result.BalanceAfter = newBalance
		display := row.Username
		if display == "" {
			display = row.Email
		}
		result.DisplayName = display
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}