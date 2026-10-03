package migration

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrSourceFingerprintChanged = errors.New("legacy source fingerprint changed")
	ErrCanonicalMappingChanged  = errors.New("legacy canonical mapping changed")
	ErrRollbackDrift            = errors.New("canonical data changed after migration")
)

type ImportRecord struct {
	MigrationVersion     string
	LegacyTable          string
	LegacyID             string
	CanonicalTable       string
	CanonicalID          string
	SourceFingerprint    string
	CanonicalFingerprint string
	ImportedAt           time.Time
}

func RecordImport(db *gorm.DB, record ImportRecord) (*LegacyImportMap, bool, error) {
	if db == nil {
		return nil, false, errors.New("record import database is nil")
	}
	record.MigrationVersion = strings.TrimSpace(record.MigrationVersion)
	record.LegacyTable = strings.TrimSpace(record.LegacyTable)
	record.LegacyID = strings.TrimSpace(record.LegacyID)
	record.CanonicalTable = strings.TrimSpace(record.CanonicalTable)
	record.CanonicalID = strings.TrimSpace(record.CanonicalID)
	record.SourceFingerprint = strings.TrimSpace(record.SourceFingerprint)
	record.CanonicalFingerprint = strings.TrimSpace(record.CanonicalFingerprint)
	if record.MigrationVersion == "" || record.LegacyTable == "" || record.LegacyID == "" ||
		record.CanonicalTable == "" || record.CanonicalID == "" {
		return nil, false, errors.New("record import requires non-empty source and canonical identity")
	}
	if err := validateFingerprint(record.SourceFingerprint); err != nil {
		return nil, false, err
	}
	if record.CanonicalFingerprint != "" {
		if err := validateFingerprint(record.CanonicalFingerprint); err != nil {
			return nil, false, fmt.Errorf("canonical fingerprint: %w", err)
		}
	}
	if record.ImportedAt.IsZero() {
		record.ImportedAt = time.Now().UTC()
	}

	var result LegacyImportMap
	created := false
	err := db.Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("migration_version = ? AND legacy_table = ? AND legacy_id = ? AND canonical_table = ?",
				record.MigrationVersion, record.LegacyTable, record.LegacyID, record.CanonicalTable)
		err := query.Take(&result).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			result = LegacyImportMap{
				MigrationVersion:     record.MigrationVersion,
				LegacyTable:          record.LegacyTable,
				LegacyID:             record.LegacyID,
				CanonicalTable:       record.CanonicalTable,
				CanonicalID:          record.CanonicalID,
				SourceFingerprint:    record.SourceFingerprint,
				CanonicalFingerprint: record.CanonicalFingerprint,
				ImportedAt:           record.ImportedAt,
			}
			if err := tx.Create(&result).Error; err != nil {
				return fmt.Errorf("create legacy import ledger row: %w", err)
			}
			created = true
			return nil
		}
		if err != nil {
			return fmt.Errorf("read legacy import ledger row: %w", err)
		}

		if result.RolledBackAt != nil {
			updates := map[string]any{
				"canonical_id":          record.CanonicalID,
				"source_fingerprint":    record.SourceFingerprint,
				"canonical_fingerprint": record.CanonicalFingerprint,
				"imported_at":           record.ImportedAt,
				"rolled_back_at":        nil,
			}
			if err := tx.Model(&result).Updates(updates).Error; err != nil {
				return fmt.Errorf("reactivate legacy import ledger row: %w", err)
			}
			result.CanonicalID = record.CanonicalID
			result.SourceFingerprint = record.SourceFingerprint
			result.CanonicalFingerprint = record.CanonicalFingerprint
			result.ImportedAt = record.ImportedAt
			result.RolledBackAt = nil
			created = true
			return nil
		}
		if result.SourceFingerprint != record.SourceFingerprint {
			return fmt.Errorf("%w: %s[%s]", ErrSourceFingerprintChanged, record.LegacyTable, record.LegacyID)
		}
		if result.CanonicalID != record.CanonicalID {
			return fmt.Errorf("%w: %s[%s] maps to %s[%s], not %s",
				ErrCanonicalMappingChanged, record.LegacyTable, record.LegacyID,
				record.CanonicalTable, result.CanonicalID, record.CanonicalID)
		}
		if result.CanonicalFingerprint == "" && record.CanonicalFingerprint != "" {
			if err := tx.Model(&result).Update("canonical_fingerprint", record.CanonicalFingerprint).Error; err != nil {
				return fmt.Errorf("backfill canonical import fingerprint: %w", err)
			}
			result.CanonicalFingerprint = record.CanonicalFingerprint
		} else if record.CanonicalFingerprint != "" && result.CanonicalFingerprint != record.CanonicalFingerprint {
			return fmt.Errorf("%w: %s[%s] canonical row fingerprint changed", ErrCanonicalMappingChanged, record.CanonicalTable, record.CanonicalID)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &result, created, nil
}

func ActiveImports(db *gorm.DB, version string) ([]LegacyImportMap, error) {
	if db == nil {
		return nil, errors.New("list imports database is nil")
	}
	var rows []LegacyImportMap
	err := db.Where("migration_version = ? AND rolled_back_at IS NULL", strings.TrimSpace(version)).
		Order("id ASC").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list active legacy imports: %w", err)
	}
	return rows, nil
}

func MarkImportRolledBack(db *gorm.DB, id uint, at time.Time) error {
	if db == nil {
		return errors.New("mark import rollback database is nil")
	}
	if id == 0 {
		return errors.New("mark import rollback requires a ledger id")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result := db.Model(&LegacyImportMap{}).
		Where("id = ? AND rolled_back_at IS NULL", id).
		Update("rolled_back_at", at)
	if result.Error != nil {
		return fmt.Errorf("mark legacy import rolled back: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("mark legacy import rolled back: active ledger row %d not found", id)
	}
	return nil
}

func validateFingerprint(value string) error {
	if len(value) != sha256HexLength {
		return fmt.Errorf("source fingerprint must be %d lowercase hex characters", sha256HexLength)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256HexLength/2 || strings.ToLower(value) != value {
		return fmt.Errorf("source fingerprint must be %d lowercase hex characters", sha256HexLength)
	}
	return nil
}
