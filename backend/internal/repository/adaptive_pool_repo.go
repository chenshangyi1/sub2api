package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type adaptivePoolRepository struct {
	db *sql.DB
}

func NewAdaptivePoolSnapshotRepository(db *sql.DB) service.AdaptivePoolSnapshotRepository {
	return &adaptivePoolRepository{db: db}
}

func NewAdaptivePoolAdminRepository(db *sql.DB) service.AdaptivePoolAdminRepository {
	return &adaptivePoolRepository{db: db}
}

func (r *adaptivePoolRepository) GetAdaptivePoolSnapshot(ctx context.Context, parentGroupID int64) (*service.AdaptivePoolSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("adaptive pool repository is not initialized")
	}
	if parentGroupID <= 0 {
		return nil, errors.New("adaptive parent group id must be positive")
	}
	return getAdaptivePoolSnapshot(ctx, r.db, parentGroupID)
}

func getAdaptivePoolSnapshot(ctx context.Context, q sqlExecutor, parentGroupID int64) (*service.AdaptivePoolSnapshot, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT c.parent_group_id,
		       g.platform,
		       c.enabled,
		       c.config_generation,
		       COALESCE(c.use_manual_intelligence_order, FALSE),
		       COALESCE(c.allow_hybrid, FALSE),
		       m.leaf_group_id,
		       m.enabled,
		       m.sort_order
		  FROM adaptive_group_configs c
		  JOIN groups g
		    ON g.id = c.parent_group_id
		   AND g.deleted_at IS NULL
		  LEFT JOIN adaptive_group_memberships m
		    ON m.config_id = c.id
		 WHERE c.parent_group_id = $1
		 ORDER BY m.sort_order ASC NULLS LAST, m.leaf_group_id ASC NULLS LAST
	`, parentGroupID)
	if err != nil {
		return nil, fmt.Errorf("query adaptive pool snapshot: %w", err)
	}
	defer rows.Close()

	var snapshot *service.AdaptivePoolSnapshot
	for rows.Next() {
		var (
			rowParentID int64
			platform    string
			poolEnabled bool
			generation  int64
			manualIntel bool
			allowHybrid bool
			leafID      sql.NullInt64
			leafEnabled sql.NullBool
			sortOrder   sql.NullInt64
		)
		if err := rows.Scan(
			&rowParentID,
			&platform,
			&poolEnabled,
			&generation,
			&manualIntel,
			&allowHybrid,
			&leafID,
			&leafEnabled,
			&sortOrder,
		); err != nil {
			return nil, fmt.Errorf("scan adaptive pool snapshot: %w", err)
		}
		if snapshot == nil {
			snapshot = &service.AdaptivePoolSnapshot{
				ParentGroupID:              rowParentID,
				Platform:                   platform,
				Enabled:                    poolEnabled,
				ConfigGeneration:           generation,
				UseManualIntelligenceOrder: manualIntel,
				AllowHybrid:                allowHybrid,
				Members:                    make([]service.AdaptiveLeafRef, 0),
			}
		}
		if leafID.Valid {
			snapshot.Members = append(snapshot.Members, service.AdaptiveLeafRef{
				LeafGroupID: leafID.Int64,
				Enabled:     leafEnabled.Bool,
				SortOrder:   int(sortOrder.Int64),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate adaptive pool snapshot: %w", err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("%w: parent_group_id=%d", service.ErrAdaptivePoolNotFound, parentGroupID)
	}
	return snapshot, nil
}

func (r *adaptivePoolRepository) ListAdaptivePoolSnapshots(ctx context.Context) ([]service.AdaptivePoolSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("adaptive pool repository is not initialized")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT c.parent_group_id
		  FROM adaptive_group_configs c
		  JOIN groups g ON g.id = c.parent_group_id AND g.deleted_at IS NULL
		 ORDER BY c.parent_group_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list adaptive pool parents: %w", err)
	}
	defer rows.Close()

	parentIDs := make([]int64, 0)
	for rows.Next() {
		var parentID int64
		if err := rows.Scan(&parentID); err != nil {
			return nil, fmt.Errorf("scan adaptive pool parent: %w", err)
		}
		parentIDs = append(parentIDs, parentID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]service.AdaptivePoolSnapshot, 0, len(parentIDs))
	for _, parentID := range parentIDs {
		snapshot, err := r.GetAdaptivePoolSnapshot(ctx, parentID)
		if err != nil {
			return nil, err
		}
		if snapshot != nil {
			out = append(out, *snapshot)
		}
	}
	return out, nil
}

func (r *adaptivePoolRepository) DeleteAdaptivePool(ctx context.Context, parentGroupID int64) error {
	if r == nil || r.db == nil {
		return errors.New("adaptive pool repository is not initialized")
	}
	if parentGroupID <= 0 {
		return errors.New("adaptive parent group id must be positive")
	}
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM adaptive_group_configs WHERE parent_group_id = $1
	`, parentGroupID)
	if err != nil {
		return fmt.Errorf("delete adaptive pool: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("%w: parent_group_id=%d", service.ErrAdaptivePoolNotFound, parentGroupID)
	}
	return nil
}

func (r *adaptivePoolRepository) ReplaceAdaptivePool(ctx context.Context, input service.AdaptivePoolUpdate) (*service.AdaptivePoolSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("adaptive pool repository is not initialized")
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin adaptive pool replacement: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var configID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO adaptive_group_configs AS current_config (
			parent_group_id, enabled, allow_hybrid, config_generation, created_at, updated_at
		)
		VALUES ($1, $2, $3, 1, clock_timestamp(), clock_timestamp())
		ON CONFLICT (parent_group_id) DO UPDATE
		   SET enabled = EXCLUDED.enabled,
		       allow_hybrid = EXCLUDED.allow_hybrid,
		       config_generation = next_adaptive_group_generation(current_config.config_generation),
		       updated_at = clock_timestamp()
		RETURNING id
	`, input.ParentGroupID, input.Enabled, input.AllowHybrid).Scan(&configID); err != nil {
		return nil, fmt.Errorf("upsert adaptive pool config: %w", err)
	}
	// The row lock serializes concurrent full replacements. The upsert above
	// already owns it; this explicit read documents and verifies that contract.
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		  FROM adaptive_group_configs
		 WHERE id = $1
		 FOR UPDATE
	`, configID).Scan(&configID); err != nil {
		return nil, fmt.Errorf("lock adaptive pool config: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM adaptive_group_memberships WHERE config_id = $1
	`, configID); err != nil {
		return nil, fmt.Errorf("clear adaptive pool members: %w", err)
	}
	for _, member := range input.Members {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO adaptive_group_memberships (
				config_id, leaf_group_id, enabled, sort_order, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, clock_timestamp(), clock_timestamp())
		`, configID, member.LeafGroupID, member.Enabled, member.SortOrder); err != nil {
			return nil, fmt.Errorf("insert adaptive pool leaf group %d: %w", member.LeafGroupID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit adaptive pool replacement: %w", err)
	}
	return r.GetAdaptivePoolSnapshot(ctx, input.ParentGroupID)
}
