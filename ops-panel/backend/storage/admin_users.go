package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const adminPasswordMinLength = 6

// AdminUser mirrors the existing operations panel table. It deliberately
// keeps the legacy email column as the login subject for data compatibility.
type AdminUser struct {
	ID           int       `gorm:"primaryKey;autoIncrement" json:"id"`
	Email        string    `gorm:"column:email;not null;uniqueIndex:admin_users_email_key" json:"email"`
	PasswordHash string    `gorm:"column:password_hash;not null" json:"-"`
	CreatedAt    time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (AdminUser) TableName() string { return "admin_users" }

// AdminUsers implements auth.CredentialStore without coupling the storage
// package to the authentication package.
type AdminUsers struct {
	db *gorm.DB
}

func NewAdminUsers(db *gorm.DB) *AdminUsers {
	return &AdminUsers{db: db}
}

func (s *AdminUsers) Authenticate(ctx context.Context, username, password string) (string, bool, error) {
	username = strings.TrimSpace(username)
	var user AdminUser
	err := s.db.WithContext(ctx).
		Select("email", "password_hash").
		Where("email = ?", username).
		Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find admin user: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("verify admin password hash: %w", err)
	}
	return user.Email, true, nil
}

func (s *AdminUsers) SubjectExists(ctx context.Context, subject string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&AdminUser{}).
		Where("email = ?", strings.TrimSpace(subject)).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("find admin subject: %w", err)
	}
	return count > 0, nil
}

// SeedIfEmpty imports the configured administrator only when the legacy table
// has no rows. Existing database accounts always remain authoritative.
func (s *AdminUsers) SeedIfEmpty(ctx context.Context, username, password string) (bool, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return false, nil
	}
	if len(password) < adminPasswordMinLength {
		return false, fmt.Errorf("OPS_PANEL_ADMIN_PASSWORD must be at least %d characters", adminPasswordMinLength)
	}

	seeded := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&AdminUser{}).Count(&count).Error; err != nil {
			return fmt.Errorf("count admin users: %w", err)
		}
		if count > 0 {
			return nil
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
		if err != nil {
			return fmt.Errorf("hash admin password: %w", err)
		}
		user := &AdminUser{Email: username, PasswordHash: string(hash)}
		if err := tx.Create(user).Error; err != nil {
			return fmt.Errorf("seed admin user: %w", err)
		}
		seeded = true
		return nil
	})
	return seeded, err
}
