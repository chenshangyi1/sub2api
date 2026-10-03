package repository

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *apiKeyRepository) hydrateAdaptiveKeySlice(ctx context.Context, keys []service.APIKey) {
	if len(keys) == 0 {
		return
	}
	ptrs := make([]*service.APIKey, len(keys))
	for i := range keys {
		ptrs[i] = &keys[i]
	}
	r.hydrateAdaptiveKeyFields(ctx, ptrs)
}

func (r *apiKeyRepository) hydrateAdaptiveKeyFields(ctx context.Context, keys []*service.APIKey) {
	if r == nil || r.sql == nil || len(keys) == 0 {
		return
	}
	ids := make([]int64, 0, len(keys))
	index := make(map[int64]*service.APIKey, len(keys))
	for _, key := range keys {
		if key == nil || key.ID <= 0 {
			continue
		}
		ids = append(ids, key.ID)
		index[key.ID] = key
	}
	if len(ids) == 0 {
		return
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, adaptive_routing_preference, adaptive_max_rate_multiplier, adaptive_leaf_group_ids
		FROM api_keys
		WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		if isUndefinedColumnError(err) {
			r.hydrateAdaptiveKeyFieldsLegacy(ctx, ids, index)
			return
		}
		slog.Warn("hydrate adaptive api key fields failed", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id        int64
			pref      sql.NullString
			maxRate   sql.NullFloat64
			leafIDs   pq.Int64Array
		)
		if scanErr := rows.Scan(&id, &pref, &maxRate, &leafIDs); scanErr != nil {
			slog.Warn("scan adaptive api key fields failed", "error", scanErr)
			return
		}
		key := index[id]
		if key == nil {
			continue
		}
		if pref.Valid {
			key.AdaptiveRoutingPreference = pref.String
		}
		if maxRate.Valid {
			v := maxRate.Float64
			key.AdaptiveMaxRateMultiplier = &v
		}
		key.AdaptiveLeafGroupIDs = service.NormalizeAdaptiveLeafGroupIDs(leafIDs)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("hydrate adaptive api key fields rows failed", "error", err)
	}
}

func (r *apiKeyRepository) hydrateAdaptiveKeyFieldsLegacy(ctx context.Context, ids []int64, index map[int64]*service.APIKey) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, adaptive_routing_preference, adaptive_max_rate_multiplier
		FROM api_keys
		WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		slog.Warn("hydrate adaptive api key legacy fields failed", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id      int64
			pref    sql.NullString
			maxRate sql.NullFloat64
		)
		if scanErr := rows.Scan(&id, &pref, &maxRate); scanErr != nil {
			return
		}
		key := index[id]
		if key == nil {
			continue
		}
		if pref.Valid {
			key.AdaptiveRoutingPreference = pref.String
		}
		if maxRate.Valid {
			v := maxRate.Float64
			key.AdaptiveMaxRateMultiplier = &v
		}
	}
}

func (r *apiKeyRepository) persistAdaptiveKeyFields(ctx context.Context, key *service.APIKey) error {
	if r == nil || r.sql == nil || key == nil || key.ID <= 0 {
		return nil
	}
	pref := strings.TrimSpace(key.AdaptiveRoutingPreference)
	if pref != "" {
		normalized, nerr := service.NormalizeAdaptiveRoutingPreference(pref)
		if nerr != nil {
			return nerr
		}
		pref = normalized
	}
	var maxRate any
	if key.AdaptiveMaxRateMultiplier != nil {
		maxRate = *key.AdaptiveMaxRateMultiplier
	}
	ids := service.NormalizeAdaptiveLeafGroupIDs(key.AdaptiveLeafGroupIDs)
	var leafArg any
	if len(ids) > 0 {
		leafArg = pq.Array(ids)
	}
	_, err := r.sql.ExecContext(ctx, `
		UPDATE api_keys
		SET adaptive_routing_preference = COALESCE(NULLIF($2, ''), adaptive_routing_preference),
		    adaptive_max_rate_multiplier = $3,
		    adaptive_leaf_group_ids = $4
		WHERE id = $1 AND deleted_at IS NULL`,
		key.ID, pref, maxRate, leafArg)
	if err == nil {
		return nil
	}
	if !isUndefinedColumnError(err) {
		return err
	}
	_, err = r.sql.ExecContext(ctx, `
		UPDATE api_keys
		SET adaptive_routing_preference = COALESCE(NULLIF($2, ''), adaptive_routing_preference),
		    adaptive_max_rate_multiplier = $3
		WHERE id = $1 AND deleted_at IS NULL`,
		key.ID, pref, maxRate)
	return err
}

func isUndefinedColumnError(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "42703"
}
