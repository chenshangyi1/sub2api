package migration

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

// ErrCanonicalRowChanged is kept as a descriptive alias for callers that
// used the earlier name; ErrRollbackDrift is the canonical sentinel.
var ErrCanonicalRowChanged = ErrRollbackDrift

var rollbackPriority = map[string]int{
	"upstream_sync_accounts": 1,
	"upstream_sync_groups":   2,
	"auth_sessions":          3,
	"rate_change_logs":       4,
	"rate_snapshots":         5,
	"channels":               6,
	"upstream_sync_targets":  7,
}

func rollbackImports(db *gorm.DB, rows []LegacyImportMap, at time.Time) (map[string]int, error) {
	ordered := append([]LegacyImportMap(nil), rows...)
	sort.Slice(ordered, func(i, j int) bool {
		left := rollbackPriority[ordered[i].CanonicalTable]
		right := rollbackPriority[ordered[j].CanonicalTable]
		if left == right {
			return ordered[i].ID > ordered[j].ID
		}
		return left < right
	})

	deleted := make(map[string]int)
	// Validate every row before deleting any row. The surrounding transaction
	// also protects against partial changes, while this pass gives a precise
	// drift error and prevents an old ledger from deleting post-cutover data.
	for _, row := range ordered {
		if strings.TrimSpace(row.CanonicalFingerprint) == "" {
			return nil, fmt.Errorf("%w: %s[%s] has no stored canonical fingerprint", ErrRollbackDrift, row.CanonicalTable, row.CanonicalID)
		}
		current, err := canonicalRowFingerprint(db, row.CanonicalTable, row.CanonicalID)
		if err != nil {
			return nil, fmt.Errorf("rollback fingerprint %s[%s]: %w", row.CanonicalTable, row.CanonicalID, err)
		}
		if current != row.CanonicalFingerprint {
			return nil, fmt.Errorf("%w: %s[%s] fingerprint %s != imported %s", ErrRollbackDrift, row.CanonicalTable, row.CanonicalID, current, row.CanonicalFingerprint)
		}
	}
	for _, row := range ordered {
		if _, ok := rollbackPriority[row.CanonicalTable]; !ok {
			return nil, fmt.Errorf("rollback import %d: unsupported canonical table %q", row.ID, row.CanonicalTable)
		}
		canonicalID, err := canonicalUint(row.CanonicalID)
		if err != nil {
			return nil, fmt.Errorf("rollback import %d: %w", row.ID, err)
		}
		result := deleteCanonicalImport(db, row.CanonicalTable, canonicalID)
		if result.Error != nil {
			return nil, fmt.Errorf("rollback %s[%d]: %w", row.CanonicalTable, canonicalID, result.Error)
		}
		if result.RowsAffected != 1 {
			return nil, fmt.Errorf("rollback %s[%d]: expected one canonical row, deleted %d", row.CanonicalTable, canonicalID, result.RowsAffected)
		}
		if err := MarkImportRolledBack(db, row.ID, at); err != nil {
			return nil, err
		}
		deleted[row.CanonicalTable]++
	}
	if err := resetImportedSequences(db); err != nil {
		return nil, err
	}
	return deleted, nil
}

func canonicalRowFingerprint(db *gorm.DB, table, id string) (string, error) {
	selector, err := canonicalRowSelector(table)
	if err != nil {
		return "", err
	}
	var row map[string]any
	if err := db.Table(table).Where(selector, id).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("canonical row is missing")
		}
		return "", err
	}
	return Fingerprint(row)
}

func canonicalRowSelector(table string) (string, error) {
	switch table {
	case "auth_sessions":
		return "channel_id = ?", nil
	case "upstream_sync_targets", "upstream_sync_groups", "upstream_sync_accounts", "rate_change_logs", "rate_snapshots", "channels":
		return "id = ?", nil
	default:
		return "", fmt.Errorf("unsupported canonical table %q", table)
	}
}

func deleteCanonicalImport(db *gorm.DB, table string, id uint) *gorm.DB {
	switch table {
	case "upstream_sync_accounts":
		return db.Where("id = ?", id).Delete(&storage.UpstreamSyncAccount{})
	case "upstream_sync_groups":
		return db.Where("id = ?", id).Delete(&storage.UpstreamSyncGroup{})
	case "auth_sessions":
		return db.Where("channel_id = ?", id).Delete(&storage.AuthSession{})
	case "rate_change_logs":
		return db.Where("id = ?", id).Delete(&storage.RateChangeLog{})
	case "rate_snapshots":
		return db.Where("id = ?", id).Delete(&storage.RateSnapshot{})
	case "channels":
		return db.Where("id = ?", id).Delete(&storage.Channel{})
	case "upstream_sync_targets":
		return db.Where("id = ?", id).Delete(&storage.UpstreamSyncTarget{})
	default:
		return &gorm.DB{Error: fmt.Errorf("unsupported canonical table %q", table)}
	}
}
