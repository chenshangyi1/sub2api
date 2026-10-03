package migration

import "time"

const VersionV007LegacyImport = "20260729_upstream_ops_v007"

const (
	MigrationStateRunning    = "running"
	MigrationStateApplied    = "applied"
	MigrationStateVerified   = "verified"
	MigrationStateRolledBack = "rolled_back"
	MigrationStateFailed     = "failed"
)

type SchemaMigration struct {
	Version           string     `gorm:"size:128;primaryKey" json:"version"`
	State             string     `gorm:"size:32;not null;index" json:"state"`
	SourceFingerprint string     `gorm:"size:64;not null" json:"source_fingerprint"`
	StartedAt         time.Time  `gorm:"not null" json:"started_at"`
	AppliedAt         *time.Time `json:"applied_at,omitempty"`
	VerifiedAt        *time.Time `json:"verified_at,omitempty"`
	RolledBackAt      *time.Time `json:"rolled_back_at,omitempty"`
	ErrorMessage      string     `gorm:"type:text" json:"error_message,omitempty"`
}

func (SchemaMigration) TableName() string { return "schema_migrations" }

type LegacyImportMap struct {
	ID                   uint       `gorm:"primaryKey" json:"id"`
	MigrationVersion     string     `gorm:"size:128;not null;uniqueIndex:idx_legacy_import_source,priority:1" json:"migration_version"`
	LegacyTable          string     `gorm:"size:128;not null;uniqueIndex:idx_legacy_import_source,priority:2" json:"legacy_table"`
	LegacyID             string     `gorm:"size:128;not null;uniqueIndex:idx_legacy_import_source,priority:3" json:"legacy_id"`
	CanonicalTable       string     `gorm:"size:128;not null;uniqueIndex:idx_legacy_import_source,priority:4;index" json:"canonical_table"`
	CanonicalID          string     `gorm:"size:128;not null" json:"canonical_id"`
	SourceFingerprint    string     `gorm:"size:64;not null" json:"source_fingerprint"`
	CanonicalFingerprint string     `gorm:"size:64;not null;default:''" json:"canonical_fingerprint"`
	ImportedAt           time.Time  `gorm:"not null" json:"imported_at"`
	RolledBackAt         *time.Time `json:"rolled_back_at,omitempty"`
}

func (LegacyImportMap) TableName() string { return "legacy_import_maps" }
