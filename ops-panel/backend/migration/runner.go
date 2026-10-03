package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appcrypto "github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

type RunnerOptions struct {
	Version             string
	LegacyEncryptionKey string
	AppSecret           string
	RequirePostgres     bool
	Now                 func() time.Time
}

type Runner struct {
	db              *gorm.DB
	version         string
	legacyCipher    *LegacyCipher
	appCipher       *appcrypto.Cipher
	requirePostgres bool
	now             func() time.Time
}

type MigrationResult struct {
	Version           string         `json:"version"`
	SourceFingerprint string         `json:"source_fingerprint"`
	State             string         `json:"state"`
	Imported          map[string]int `json:"imported"`
	Skipped           map[string]int `json:"skipped"`
}

type VerifyReport struct {
	Version           string         `json:"version"`
	SourceFingerprint string         `json:"source_fingerprint"`
	State             string         `json:"state"`
	Verified          map[string]int `json:"verified"`
	Skipped           map[string]int `json:"skipped"`
}

type RollbackReport struct {
	Version string         `json:"version"`
	State   string         `json:"state"`
	Deleted map[string]int `json:"deleted"`
}

func NewRunner(db *gorm.DB, opts RunnerOptions) (*Runner, error) {
	if db == nil {
		return nil, errors.New("migration database is nil")
	}
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = VersionV007LegacyImport
	}
	runner := &Runner{
		db:              db,
		version:         version,
		requirePostgres: opts.RequirePostgres,
		now:             opts.Now,
	}
	if runner.now == nil {
		runner.now = func() time.Time { return time.Now().UTC() }
	}
	if strings.TrimSpace(opts.LegacyEncryptionKey) != "" {
		legacyCipher, err := NewLegacyCipher(strings.TrimSpace(opts.LegacyEncryptionKey))
		if err != nil {
			return nil, err
		}
		runner.legacyCipher = legacyCipher
	}
	if opts.AppSecret != "" {
		appCipher, err := appcrypto.NewCipher(opts.AppSecret)
		if err != nil {
			return nil, err
		}
		runner.appCipher = appCipher
	}
	return runner, nil
}

func (r *Runner) Preflight(ctx context.Context) (*PreflightReport, error) {
	report, _, err := r.preflight(ctx)
	return report, err
}

func (r *Runner) preflight(ctx context.Context) (*PreflightReport, *legacySnapshot, error) {
	report, err := Preflight(ctx, r.db, PreflightOptions{
		RequirePostgres: r.requirePostgres,
		Version:         r.version,
	})
	if err != nil {
		return report, nil, err
	}
	snapshot, err := loadLegacySnapshot(ctx, r.db)
	if err != nil {
		return report, nil, err
	}
	if err := snapshot.validate(r.legacyCipher); err != nil {
		return report, snapshot, fmt.Errorf("legacy semantic preflight: %w", err)
	}
	fingerprint, err := snapshot.fingerprint()
	if err != nil {
		return report, snapshot, err
	}
	report.SourceFingerprint = fingerprint
	report.ImportableCounts = expectedImportCounts(snapshot)
	report.SkippedCounts = expectedSkippedCounts(snapshot)
	return report, snapshot, nil
}

func (r *Runner) Migrate(ctx context.Context) (*MigrationResult, error) {
	if r.appCipher == nil {
		return nil, errors.New("APP_SECRET is required for migration")
	}
	report, snapshot, err := r.preflight(ctx)
	if err != nil {
		return nil, err
	}
	result := &MigrationResult{
		Version:           r.version,
		SourceFingerprint: report.SourceFingerprint,
		State:             report.ExistingState,
		Imported:          make(map[string]int),
		Skipped:           expectedSkippedCounts(snapshot),
	}
	if report.ExistingState == MigrationStateApplied || report.ExistingState == MigrationStateVerified {
		state, err := loadMigrationState(r.db.WithContext(ctx), r.version)
		if err != nil {
			return nil, err
		}
		if state.SourceFingerprint != report.SourceFingerprint {
			return nil, fmt.Errorf("%w: migration %s", ErrSourceFingerprintChanged, r.version)
		}
		result.State = state.State
		for table, count := range report.ImportableCounts {
			result.Imported[table] = count
		}
		return result, nil
	}
	if report.ExistingImports > 0 {
		return nil, errors.New("migration has active partial imports; run rollback before retrying")
	}
	if err := EnsureMetadata(r.db.WithContext(ctx)); err != nil {
		return nil, err
	}
	if err := Begin(r.db.WithContext(ctx), r.version, report.SourceFingerprint, r.now()); err != nil {
		return nil, err
	}

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		imported, err := r.importSnapshot(tx, snapshot)
		if err != nil {
			return err
		}
		result.Imported = imported
		return nil
	})
	if err != nil {
		_ = MarkFailed(r.db.WithContext(ctx), r.version, r.now(), err)
		return nil, err
	}
	if err := MarkApplied(r.db.WithContext(ctx), r.version, r.now()); err != nil {
		return nil, err
	}
	result.State = MigrationStateApplied
	return result, nil
}

func (r *Runner) Verify(ctx context.Context) (*VerifyReport, error) {
	if r.appCipher == nil {
		return nil, errors.New("APP_SECRET is required for migration verification")
	}
	report, snapshot, err := r.preflight(ctx)
	if err != nil {
		return nil, err
	}
	state, err := loadMigrationState(r.db.WithContext(ctx), r.version)
	if err != nil {
		return nil, err
	}
	if state.State != MigrationStateApplied && state.State != MigrationStateVerified {
		return nil, fmt.Errorf("migration %s is %s, not applied", r.version, state.State)
	}
	if state.SourceFingerprint != report.SourceFingerprint {
		return nil, fmt.Errorf("%w: migration %s", ErrSourceFingerprintChanged, r.version)
	}
	verified, err := r.verifySnapshot(r.db.WithContext(ctx), snapshot)
	if err != nil {
		return nil, err
	}
	if err := MarkVerified(r.db.WithContext(ctx), r.version, r.now()); err != nil {
		return nil, err
	}
	return &VerifyReport{
		Version:           r.version,
		SourceFingerprint: report.SourceFingerprint,
		State:             MigrationStateVerified,
		Verified:          verified,
		Skipped:           expectedSkippedCounts(snapshot),
	}, nil
}

func (r *Runner) Rollback(ctx context.Context) (*RollbackReport, error) {
	if err := EnsureMetadata(r.db.WithContext(ctx)); err != nil {
		return nil, err
	}
	state, err := loadMigrationState(r.db.WithContext(ctx), r.version)
	if err != nil {
		return nil, err
	}
	if state.State == MigrationStateRolledBack {
		return &RollbackReport{Version: r.version, State: state.State, Deleted: map[string]int{}}, nil
	}
	// Applied/verified imports may have been edited by the running service after
	// migration. Verify the imported projection before deleting anything so a
	// rollback cannot erase user changes. Failed partial migrations are still
	// cleaned up by the ledger because they never reached an accepted state.
	if state.State == MigrationStateApplied || state.State == MigrationStateVerified {
		if r.appCipher == nil || r.legacyCipher == nil {
			return nil, errors.New("rollback drift protection requires APP_SECRET and ENCRYPTION_KEY")
		}
		report, snapshot, preflightErr := r.preflight(ctx)
		if preflightErr != nil {
			return nil, preflightErr
		}
		if report.SourceFingerprint != state.SourceFingerprint {
			return nil, fmt.Errorf("%w: migration %s", ErrSourceFingerprintChanged, r.version)
		}
		if _, verifyErr := r.verifySnapshot(r.db.WithContext(ctx), snapshot); verifyErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrRollbackDrift, verifyErr)
		}
	}
	rows, err := ActiveImports(r.db.WithContext(ctx), r.version)
	if err != nil {
		return nil, err
	}
	result := &RollbackReport{
		Version: r.version,
		State:   MigrationStateRolledBack,
		Deleted: make(map[string]int),
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deleted, err := rollbackImports(tx, rows, r.now())
		if err != nil {
			return err
		}
		result.Deleted = deleted
		return transition(tx, r.version, MigrationStateRolledBack, r.now(), "")
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func loadMigrationState(db *gorm.DB, version string) (*SchemaMigration, error) {
	var state SchemaMigration
	if err := db.Where("version = ?", version).Take(&state).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("migration %s has not been started", version)
		}
		return nil, fmt.Errorf("load migration state: %w", err)
	}
	return &state, nil
}

// RequireVerified prevents a legacy PostgreSQL database from reaching the
// server's normal AutoMigrate path before the explicit import has succeeded.
func RequireVerified(db *gorm.DB, version string) error {
	if db == nil {
		return errors.New("migration gate database is nil")
	}
	if !db.Migrator().HasTable("connections") {
		return nil
	}
	// Fresh canonical databases retain a credentials-free connection shadow for
	// extension-table foreign keys. Only legacy rows require the import gate.
	if db.Migrator().HasColumn("connections", "sync_mode") {
		var legacyRows int64
		if err := db.Table("connections").
			Where("sync_mode IS NULL OR sync_mode <> ?", storage.ConnectionSyncModeCanonicalTarget).
			Count(&legacyRows).Error; err != nil {
			return fmt.Errorf("inspect legacy operations connections: %w", err)
		}
		if legacyRows == 0 {
			return nil
		}
	}
	if !db.Migrator().HasTable((&SchemaMigration{}).TableName()) {
		return fmt.Errorf("legacy operations schema detected; run migration %s", version)
	}
	state, err := loadMigrationState(db, version)
	if err != nil {
		return err
	}
	if state.State != MigrationStateVerified {
		return fmt.Errorf("migration %s must be verified before server startup; current state is %s", version, state.State)
	}
	return nil
}
