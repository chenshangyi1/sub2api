package migration

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func Begin(db *gorm.DB, version, sourceFingerprint string, at time.Time) error {
	if db == nil {
		return errors.New("begin migration database is nil")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return errors.New("migration version is empty")
	}
	if err := validateFingerprint(sourceFingerprint); err != nil {
		return err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var row SchemaMigration
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("version = ?", version).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = SchemaMigration{
				Version:           version,
				State:             MigrationStateRunning,
				SourceFingerprint: sourceFingerprint,
				StartedAt:         at,
			}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("create migration state: %w", err)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("read migration state: %w", err)
		}
		if row.SourceFingerprint != sourceFingerprint && row.State != MigrationStateRolledBack {
			return fmt.Errorf("%w: migration %s", ErrSourceFingerprintChanged, version)
		}
		if row.State == MigrationStateApplied || row.State == MigrationStateVerified {
			return nil
		}
		updates := map[string]any{
			"state":              MigrationStateRunning,
			"source_fingerprint": sourceFingerprint,
			"started_at":         at,
			"applied_at":         nil,
			"verified_at":        nil,
			"rolled_back_at":     nil,
			"error_message":      "",
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return fmt.Errorf("restart migration state: %w", err)
		}
		return nil
	})
}

func MarkApplied(db *gorm.DB, version string, at time.Time) error {
	return transition(db, version, MigrationStateApplied, at, "")
}

func MarkVerified(db *gorm.DB, version string, at time.Time) error {
	return transition(db, version, MigrationStateVerified, at, "")
}

func MarkRolledBack(db *gorm.DB, version string, at time.Time) error {
	return transition(db, version, MigrationStateRolledBack, at, "")
}

func MarkFailed(db *gorm.DB, version string, at time.Time, migrationErr error) error {
	message := "migration failed"
	if migrationErr != nil {
		message = migrationErr.Error()
	}
	return transition(db, version, MigrationStateFailed, at, message)
}

func transition(db *gorm.DB, version, state string, at time.Time, message string) error {
	if db == nil {
		return errors.New("transition migration database is nil")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return errors.New("migration version is empty")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	var allowedFrom []string
	updates := map[string]any{"state": state, "error_message": message}
	switch state {
	case MigrationStateApplied:
		allowedFrom = []string{MigrationStateRunning}
		updates["applied_at"] = at
	case MigrationStateVerified:
		allowedFrom = []string{MigrationStateApplied, MigrationStateVerified}
		updates["verified_at"] = at
	case MigrationStateRolledBack:
		allowedFrom = []string{MigrationStateRunning, MigrationStateApplied, MigrationStateVerified, MigrationStateFailed}
		updates["rolled_back_at"] = at
	case MigrationStateFailed:
		allowedFrom = []string{MigrationStateRunning}
	default:
		return fmt.Errorf("unsupported migration state transition to %q", state)
	}
	result := db.Model(&SchemaMigration{}).
		Where("version = ? AND state IN ?", version, allowedFrom).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("transition migration %s to %s: %w", version, state, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("transition migration %s to %s: current state is not eligible", version, state)
	}
	return nil
}
