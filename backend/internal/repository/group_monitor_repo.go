package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type groupMonitorRepository struct {
	db *sql.DB
}

func NewGroupMonitorRepository(db *sql.DB) service.GroupMonitorRepository {
	return &groupMonitorRepository{db: db}
}

const groupMonitorColumns = `gm.id, gm.group_id, g.name, gm.enabled, g.user_visible, g.is_exclusive, g.subscription_type, gm.interval_minutes, gm.model_id,
	gm.auto_recover, gm.max_output_tokens, gm.last_run_at, gm.next_run_at, gm.created_at, gm.updated_at`

func (r *groupMonitorRepository) scanMonitor(row scannable) (*service.GroupMonitor, error) {
	m := &service.GroupMonitor{}
	if err := row.Scan(
		&m.ID, &m.GroupID, &m.GroupName, &m.Enabled, &m.UserVisible, &m.IsExclusive, &m.SubscriptionType, &m.IntervalMinutes, &m.ModelID,
		&m.AutoRecover, &m.MaxOutputTokens, &m.LastRunAt, &m.NextRunAt, &m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return nil, err
	}
	m.UserVisibleSet = true
	return m, nil
}

func (r *groupMonitorRepository) Create(ctx context.Context, m *service.GroupMonitor) error {
	var id int64
	if err := r.db.QueryRowContext(ctx, `
		INSERT INTO group_monitors (group_id, enabled, interval_minutes, model_id, auto_recover, max_output_tokens, next_run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7, NOW()), NOW(), NOW())
		RETURNING id
	`, m.GroupID, m.Enabled, m.IntervalMinutes, m.ModelID, m.AutoRecover, m.MaxOutputTokens, m.NextRunAt).Scan(&id); err != nil {
		return err
	}
	created, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	*m = *created
	return nil
}

func (r *groupMonitorRepository) GetByID(ctx context.Context, id int64) (*service.GroupMonitor, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+groupMonitorColumns+`
		FROM group_monitors gm
		JOIN groups g ON g.id = gm.group_id
		WHERE gm.id = $1
	`, id)
	m, err := r.scanMonitor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrGroupMonitorNotFound
	}
	return m, err
}

func (r *groupMonitorRepository) GetByGroupID(ctx context.Context, groupID int64) (*service.GroupMonitor, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+groupMonitorColumns+`
		FROM group_monitors gm
		JOIN groups g ON g.id = gm.group_id
		WHERE gm.group_id = $1
	`, groupID)
	m, err := r.scanMonitor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrGroupMonitorNotFound
	}
	return m, err
}

func (r *groupMonitorRepository) Update(ctx context.Context, m *service.GroupMonitor) error {
	if _, err := r.db.ExecContext(ctx, `
		UPDATE group_monitors
		SET enabled = $2, interval_minutes = $3, model_id = $4, auto_recover = $5, max_output_tokens = $6, updated_at = NOW()
		WHERE id = $1
	`, m.ID, m.Enabled, m.IntervalMinutes, m.ModelID, m.AutoRecover, m.MaxOutputTokens); err != nil {
		return err
	}
	updated, err := r.GetByID(ctx, m.ID)
	if err != nil {
		return err
	}
	*m = *updated
	return nil
}

func (r *groupMonitorRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM group_monitors WHERE id = $1`, id)
	return err
}

func (r *groupMonitorRepository) List(ctx context.Context, params service.GroupMonitorListParams) ([]*service.GroupMonitor, int64, error) {
	var conds []string
	var args []any
	argIdx := 1
	if params.Enabled != nil {
		args = append(args, *params.Enabled)
		conds = append(conds, "gm.enabled = $"+itoa(argIdx))
		argIdx++
	}
	if search := strings.TrimSpace(params.Search); search != "" {
		args = append(args, "%"+search+"%")
		conds = append(conds, "(g.name ILIKE $"+itoa(argIdx)+" OR gm.model_id ILIKE $"+itoa(argIdx)+")")
		argIdx++
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM group_monitors gm JOIN groups g ON g.id = gm.group_id"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := params.PageSize
	offset := (params.Page - 1) * params.PageSize
	listArgs := append(append([]any{}, args...), limit, offset)
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+groupMonitorColumns+`,
			(SELECT COUNT(*) FROM account_groups ag JOIN accounts a2 ON a2.id = ag.account_id WHERE ag.group_id = gm.group_id AND a2.deleted_at IS NULL AND a2.status <> 'disabled' AND COALESCE(a2.schedulable, true)) AS account_count,
			(SELECT COUNT(*) FROM group_monitor_results gr WHERE gr.monitor_id = gm.id AND gr.status = 'success') AS healthy_count,
			(SELECT COUNT(*) FROM group_monitor_results gr WHERE gr.monitor_id = gm.id AND gr.status = 'failed') AS failed_count,
			(SELECT COUNT(*) FROM group_monitor_results gr WHERE gr.monitor_id = gm.id AND gr.status = 'unknown') AS unknown_count
		FROM group_monitors gm
		JOIN groups g ON g.id = gm.group_id
		`+where+`
		ORDER BY gm.id DESC
		LIMIT $`+itoa(argIdx)+` OFFSET $`+itoa(argIdx+1)+`
	`, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var items []*service.GroupMonitor
	for rows.Next() {
		m := &service.GroupMonitor{}
		if err := rows.Scan(
			&m.ID, &m.GroupID, &m.GroupName, &m.Enabled, &m.UserVisible, &m.IsExclusive, &m.SubscriptionType, &m.IntervalMinutes, &m.ModelID,
			&m.AutoRecover, &m.MaxOutputTokens, &m.LastRunAt, &m.NextRunAt, &m.CreatedAt, &m.UpdatedAt,
			&m.AccountCount, &m.HealthyCount, &m.FailedCount, &m.UnknownCount,
		); err != nil {
			return nil, 0, err
		}
		m.UserVisibleSet = true
		items = append(items, m)
	}
	return items, total, rows.Err()
}

func (r *groupMonitorRepository) ListDue(ctx context.Context, now time.Time) ([]*service.GroupMonitor, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+groupMonitorColumns+`
		FROM group_monitors gm
		JOIN groups g ON g.id = gm.group_id
		WHERE gm.enabled = true AND gm.next_run_at <= $1
		ORDER BY gm.next_run_at ASC
	`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var items []*service.GroupMonitor
	for rows.Next() {
		m, err := r.scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (r *groupMonitorRepository) UpdateAfterRun(ctx context.Context, id int64, lastRunAt, nextRunAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE group_monitors SET last_run_at = $2, next_run_at = $3, updated_at = NOW() WHERE id = $1
	`, id, lastRunAt, nextRunAt)
	return err
}

func (r *groupMonitorRepository) ListGroupAccounts(ctx context.Context, groupID int64) ([]*service.GroupMonitorAccount, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.name, a.platform
		FROM accounts a
		JOIN account_groups ag ON ag.account_id = a.id
		WHERE ag.group_id = $1 AND a.deleted_at IS NULL AND a.status <> 'disabled' AND COALESCE(a.schedulable, true)
		ORDER BY ag.priority ASC, a.id ASC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var items []*service.GroupMonitorAccount
	for rows.Next() {
		a := &service.GroupMonitorAccount{}
		if err := rows.Scan(&a.ID, &a.Name, &a.Platform); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

// DeleteResultsForUnschedulableAccounts 清理手动暂停（schedulable=false 或 disabled）账号在本监控下的旧结果，
// 避免暂停账号继续以历史状态出现在监控展示里。
func (r *groupMonitorRepository) DeleteResultsForUnschedulableAccounts(ctx context.Context, monitorID int64) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM group_monitor_results
		WHERE monitor_id = $1
		  AND account_id IN (SELECT id FROM accounts WHERE COALESCE(schedulable, false) = false OR status = 'disabled')
	`, monitorID)
	return err
}

func (r *groupMonitorRepository) UpsertResult(ctx context.Context, res *service.GroupMonitorResult) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO group_monitor_results (monitor_id, account_id, status, model_id, latency_ms, error_message, checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (monitor_id, account_id) DO UPDATE SET
			status = EXCLUDED.status,
			model_id = EXCLUDED.model_id,
			latency_ms = EXCLUDED.latency_ms,
			error_message = EXCLUDED.error_message,
			checked_at = EXCLUDED.checked_at
	`, res.MonitorID, res.AccountID, res.Status, res.ModelID, res.LatencyMs, res.ErrorMessage, res.CheckedAt)
	return err
}

func (r *groupMonitorRepository) AppendHistory(ctx context.Context, res *service.GroupMonitorResult) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO group_monitor_result_history
			(monitor_id, account_id, status, latency_ms, ttft_ms, input_tokens, cache_read_tokens, cache_creation_tokens, checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, res.MonitorID, res.AccountID, res.Status, res.LatencyMs, res.TTFTMs, res.InputTokens, res.CacheRead, res.CacheCreate, res.CheckedAt)
	return err
}

func (r *groupMonitorRepository) QueryHistoryStats(ctx context.Context, monitorID int64, since time.Time) (*service.GroupMonitorHistoryStats, error) {
	var probes, successes int64
	var latencySum, ttftSum sql.NullFloat64
	var inputSum, cacheReadSum sql.NullFloat64
	if err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'success'),
			SUM(latency_ms::float8) FILTER (WHERE status = 'success'),
			SUM(ttft_ms::float8) FILTER (WHERE status = 'success'),
			SUM(input_tokens::float8),
			SUM(cache_read_tokens::float8)
		FROM group_monitor_result_history
		WHERE monitor_id = $1 AND checked_at >= $2
	`, monitorID, since).Scan(&probes, &successes, &latencySum, &ttftSum, &inputSum, &cacheReadSum); err != nil {
		return nil, err
	}
	s := &service.GroupMonitorHistoryStats{Probes: int(probes), Successes: int(successes)}
	if probes > 0 {
		s.Availability = float64(successes) / float64(probes) * 100
	}
	if successes > 0 && latencySum.Valid {
		s.AvgLatencyMs = latencySum.Float64 / float64(successes)
	}
	if successes > 0 && ttftSum.Valid {
		s.AvgTTFTMs = ttftSum.Float64 / float64(successes)
	}
	if inputSum.Valid && cacheReadSum.Valid && inputSum.Float64+cacheReadSum.Float64 > 0 {
		s.CacheRate = cacheReadSum.Float64 / (inputSum.Float64 + cacheReadSum.Float64) * 100
	}
	return s, nil
}

func (r *groupMonitorRepository) QueryHistorySeries(ctx context.Context, monitorID int64, since time.Time, bucketCount int) ([]service.GroupMonitorSeriesPoint, error) {
	if bucketCount <= 0 {
		bucketCount = 24
	}
	startBucket := since.Truncate(time.Minute)
	span := time.Now().Sub(startBucket)
	if span < time.Minute {
		span = time.Minute
	}
	step := span / time.Duration(bucketCount)
	if step < time.Minute {
		step = time.Minute
	}
	step = step.Truncate(time.Minute)
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			date_trunc('minute', checked_at) AS bucket,
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'success'),
			AVG(latency_ms::float8) FILTER (WHERE status = 'success'),
			AVG(ttft_ms::float8) FILTER (WHERE status = 'success'),
			SUM(input_tokens::float8),
			SUM(cache_read_tokens::float8)
		FROM group_monitor_result_history
		WHERE monitor_id = $1 AND checked_at >= $2
		GROUP BY 1
		ORDER BY 1 ASC
	`, monitorID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	byBucket := map[time.Time]*service.GroupMonitorSeriesPoint{}
	var order []time.Time
	for rows.Next() {
		var b time.Time
		var probes, successes int64
		var latAvg, ttftAvg sql.NullFloat64
		var inputSum, cacheReadSum sql.NullFloat64
		if err := rows.Scan(&b, &probes, &successes, &latAvg, &ttftAvg, &inputSum, &cacheReadSum); err != nil {
			return nil, err
		}
		p := &service.GroupMonitorSeriesPoint{
			Probes:       int(probes),
			Successes:    int(successes),
			Availability: float64(successes) / float64(probes) * 100,
		}
		if successes > 0 {
			if latAvg.Valid {
				p.AvgLatencyMs = latAvg.Float64
			}
			if ttftAvg.Valid {
				p.AvgTTFTMs = ttftAvg.Float64
			}
		}
		if inputSum.Valid && cacheReadSum.Valid && inputSum.Float64+cacheReadSum.Float64 > 0 {
			p.CacheRate = cacheReadSum.Float64 / (inputSum.Float64 + cacheReadSum.Float64) * 100
		}
		byBucket[b] = p
		order = append(order, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 初始化等间距空桶，再把实际数据桶映射到所属代码桶（解决 date_trunc 分钟桶与等间距桶不对齐的问题）。
	points := make([]service.GroupMonitorSeriesPoint, 0, bucketCount)
	for i := 0; i < bucketCount; i++ {
		bt := startBucket.Add(step * time.Duration(i))
		points = append(points, service.GroupMonitorSeriesPoint{Bucket: bt.UTC().Format(time.RFC3339)})
	}
	for _, b := range order {
		idx := int(b.Sub(startBucket) / step)
		if idx >= 0 && idx < bucketCount {
			p := *byBucket[b]
			p.Bucket = points[idx].Bucket
			points[idx] = p
		}
	}
	return points, nil
}

func (r *groupMonitorRepository) ListResults(ctx context.Context, monitorID int64) ([]*service.GroupMonitorAccountStatus, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT gr.account_id, a.name, a.platform, gr.status, gr.model_id, gr.latency_ms, gr.error_message, gr.checked_at
		FROM group_monitor_results gr
		JOIN accounts a ON a.id = gr.account_id
		JOIN group_monitors gm ON gm.id = gr.monitor_id
		LEFT JOIN account_groups ag ON ag.account_id = gr.account_id AND ag.group_id = gm.group_id
		WHERE gr.monitor_id = $1 AND COALESCE(a.schedulable, true)
		ORDER BY ag.priority ASC NULLS LAST, a.id ASC
	`, monitorID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var items []*service.GroupMonitorAccountStatus
	for rows.Next() {
		s := &service.GroupMonitorAccountStatus{}
		if err := rows.Scan(&s.AccountID, &s.AccountName, &s.Platform, &s.Status, &s.ModelID, &s.LatencyMs, &s.ErrorMessage, &s.CheckedAt); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

func (r *groupMonitorRepository) ListRecentRecords(ctx context.Context, monitorID int64, limit int) ([]service.GroupMonitorHistoryRecord, error) {
	if limit <= 0 {
		limit = 60
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			date_trunc('minute', checked_at) AS bucket,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = 'success') AS ok,
			AVG(latency_ms::float8) FILTER (WHERE status = 'success') AS avg_lat
		FROM group_monitor_result_history
		WHERE monitor_id = $1
		GROUP BY 1
		ORDER BY 1 DESC
		LIMIT $2
	`, monitorID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var recs []service.GroupMonitorHistoryRecord
	for rows.Next() {
		var bucket time.Time
		var total, ok int64
		var avgLat sql.NullFloat64
		if err := rows.Scan(&bucket, &total, &ok, &avgLat); err != nil {
			return nil, err
		}
		status := "success"
		if ok < total {
			status = "failed"
		}
		lat := int64(0)
		if avgLat.Valid {
			lat = int64(avgLat.Float64)
		}
		recs = append(recs, service.GroupMonitorHistoryRecord{Status: status, LatencyMs: lat, CheckedAt: bucket})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 反转为时间升序（旧→新，PAST→NOW）。
	for i, j := 0, len(recs)-1; i < j; i, j = i+1, j-1 {
		recs[i], recs[j] = recs[j], recs[i]
	}
	return recs, nil
}

func (r *groupMonitorRepository) ResetResults(ctx context.Context, monitorID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM group_monitor_results WHERE monitor_id = $1`, monitorID)
	return err
}

// PruneHistory 删除早于 cutoff 的历史记录（数据治理，防表膨胀）。
func (r *groupMonitorRepository) PruneHistory(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM group_monitor_result_history WHERE checked_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// QueryHistoryStatsBatch 一次性聚合所有监控在时间窗内的统计（消除 N+1）。
func (r *groupMonitorRepository) QueryHistoryStatsBatch(ctx context.Context, since time.Time) (map[int64]*service.GroupMonitorHistoryStats, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			monitor_id,
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'success'),
			SUM(latency_ms::float8) FILTER (WHERE status = 'success'),
			SUM(ttft_ms::float8) FILTER (WHERE status = 'success'),
			SUM(input_tokens::float8),
			SUM(cache_read_tokens::float8)
		FROM group_monitor_result_history
		WHERE checked_at >= $1
		GROUP BY monitor_id
	`, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[int64]*service.GroupMonitorHistoryStats{}
	for rows.Next() {
		var monitorID, probes, successes int64
		var latencySum, ttftSum, inputSum, cacheReadSum sql.NullFloat64
		if err := rows.Scan(&monitorID, &probes, &successes, &latencySum, &ttftSum, &inputSum, &cacheReadSum); err != nil {
			return nil, err
		}
		s := &service.GroupMonitorHistoryStats{Probes: int(probes), Successes: int(successes)}
		if probes > 0 {
			s.Availability = float64(successes) / float64(probes) * 100
		}
		if successes > 0 && latencySum.Valid {
			s.AvgLatencyMs = latencySum.Float64 / float64(successes)
		}
		if successes > 0 && ttftSum.Valid {
			s.AvgTTFTMs = ttftSum.Float64 / float64(successes)
		}
		if inputSum.Valid && cacheReadSum.Valid && inputSum.Float64+cacheReadSum.Float64 > 0 {
			s.CacheRate = cacheReadSum.Float64 / (inputSum.Float64 + cacheReadSum.Float64) * 100
		}
		out[monitorID] = s
	}
	return out, rows.Err()
}

// ListRecentRecordsBatch 一次性返回所有监控的最近 limit 条记录（升序）。
func (r *groupMonitorRepository) ListRecentRecordsBatch(ctx context.Context, limitPerMonitor int) (map[int64][]service.GroupMonitorHistoryRecord, error) {
	if limitPerMonitor <= 0 {
		limitPerMonitor = 60
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT monitor_id, bucket, total, ok, avg_lat FROM (
			SELECT
				monitor_id,
				date_trunc('minute', checked_at) AS bucket,
				COUNT(*) AS total,
				COUNT(*) FILTER (WHERE status = 'success') AS ok,
				AVG(latency_ms::float8) FILTER (WHERE status = 'success') AS avg_lat,
				ROW_NUMBER() OVER (PARTITION BY monitor_id ORDER BY date_trunc('minute', checked_at) DESC) AS rn
			FROM group_monitor_result_history
			GROUP BY monitor_id, 2
		) t
		WHERE rn <= $1
		ORDER BY monitor_id ASC, bucket ASC
	`, limitPerMonitor)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[int64][]service.GroupMonitorHistoryRecord{}
	for rows.Next() {
		var monitorID, total, ok int64
		var bucket time.Time
		var avgLat sql.NullFloat64
		if err := rows.Scan(&monitorID, &bucket, &total, &ok, &avgLat); err != nil {
			return nil, err
		}
		status := "success"
		if ok < total {
			status = "failed"
		}
		lat := int64(0)
		if avgLat.Valid {
			lat = int64(avgLat.Float64)
		}
		out[monitorID] = append(out[monitorID], service.GroupMonitorHistoryRecord{Status: status, LatencyMs: lat, CheckedAt: bucket})
	}
	return out, rows.Err()
}

// GroupNamesBatch 按 group_id 批量返回分组名。
func (r *groupMonitorRepository) GroupNamesBatch(ctx context.Context, groupIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(groupIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name FROM groups WHERE id = ANY($1)
	`, pq.Array(groupIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// QueryGroupPassiveStatsBatch 按 group_id 聚合真实用户请求：
// 成功来自 usage_logs，失败来自 ops_error_logs；并产出分桶序列（状态条）。
func (r *groupMonitorRepository) QueryGroupPassiveStatsBatch(ctx context.Context, since time.Time, bucketCount int) (map[int64]*service.GroupPassiveStats, error) {
	if bucketCount <= 0 {
		bucketCount = 60
	}
	out := map[int64]*service.GroupPassiveStats{}

	// 成功（usage_logs）按组聚合 + 分桶。
	succRows, err := r.db.QueryContext(ctx, `
		SELECT group_id,
			floor(extract(epoch FROM (created_at - $1)) / GREATEST(1, floor(extract(epoch FROM (NOW() - $1)) / $2))) AS bucket_idx,
			COUNT(*),
			AVG(first_token_ms::float8) FILTER (WHERE first_token_ms IS NOT NULL AND first_token_ms > 0),
			SUM(input_tokens::float8),
			SUM(cache_read_tokens::float8)
		FROM usage_logs
		WHERE created_at >= $1 AND group_id IS NOT NULL
		GROUP BY group_id, bucket_idx
	`, since, bucketCount)
	if err != nil {
		return nil, err
	}
	bucketDur := time.Since(since) / time.Duration(bucketCount)
	type bucketAgg struct {
		succ, failed       int64
		ttftSum            float64
		ttftN              int64
		inputSum, cacheSum float64
	}
	tmp := map[int64]map[int]*bucketAgg{}
	for succRows.Next() {
		var gid, cnt int64
		var bidx float64
		var ttft, inputSum, cacheSum sql.NullFloat64
		if err := succRows.Scan(&gid, &bidx, &cnt, &ttft, &inputSum, &cacheSum); err != nil {
			succRows.Close()
			return nil, err
		}
		if tmp[gid] == nil {
			tmp[gid] = map[int]*bucketAgg{}
		}
		idx := int(bidx)
		if tmp[gid][idx] == nil {
			tmp[gid][idx] = &bucketAgg{}
		}
		tmp[gid][idx].succ += cnt
		if ttft.Valid {
			tmp[gid][idx].ttftSum += ttft.Float64 * float64(cnt)
			tmp[gid][idx].ttftN += cnt
		}
		if inputSum.Valid {
			tmp[gid][idx].inputSum += inputSum.Float64
		}
		if cacheSum.Valid {
			tmp[gid][idx].cacheSum += cacheSum.Float64
		}
	}
	succRows.Close()

	// 失败（ops_error_logs）按组+桶聚合。
	failRows, err := r.db.QueryContext(ctx, `
		SELECT group_id,
			floor(extract(epoch FROM (created_at - $1)) / GREATEST(1, floor(extract(epoch FROM (NOW() - $1)) / $2))) AS bucket_idx,
			COUNT(*)
		FROM ops_error_logs
		WHERE created_at >= $1 AND group_id IS NOT NULL
		GROUP BY group_id, bucket_idx
	`, since, bucketCount)
	if err != nil {
		return nil, err
	}
	for failRows.Next() {
		var gid, cnt int64
		var bidx float64
		if err := failRows.Scan(&gid, &bidx, &cnt); err != nil {
			failRows.Close()
			return nil, err
		}
		if tmp[gid] == nil {
			tmp[gid] = map[int]*bucketAgg{}
		}
		idx := int(bidx)
		if tmp[gid][idx] == nil {
			tmp[gid][idx] = &bucketAgg{}
		}
		tmp[gid][idx].failed += cnt
	}
	failRows.Close()

	for gid, buckets := range tmp {
		st := &service.GroupPassiveStats{Buckets: make([]service.GroupPassiveBucket, bucketCount)}
		var ttftSum float64
		var ttftN int64
		for idx, agg := range buckets {
			if idx < 0 || idx >= bucketCount {
				continue
			}
			b := &st.Buckets[idx]
			b.Success = agg.succ
			b.Failed = agg.failed
			if agg.ttftN > 0 {
				b.AvgTTFTMs = agg.ttftSum / float64(agg.ttftN)
			}
			if agg.inputSum+agg.cacheSum > 0 {
				b.CacheRate = agg.cacheSum / (agg.inputSum + agg.cacheSum) * 100
			}
			b.BucketStart = since.Add(bucketDur * time.Duration(idx))
			st.Success += agg.succ
			st.Failed += agg.failed
			ttftSum += agg.ttftSum
			ttftN += agg.ttftN
		}
		if ttftN > 0 {
			st.AvgTTFTMs = ttftSum / float64(ttftN)
		}
		out[gid] = st
	}
	return out, nil
}

// QueryGroupUsageStatsBatch 按 group_id 聚合 usage_logs（被动用量）。
// usage_logs 只记录成功请求，故请求数即成功数；TTFT/缓存率来自真实流量。
func (r *groupMonitorRepository) QueryGroupUsageStatsBatch(ctx context.Context, since time.Time) (map[int64]*service.GroupUsageStats, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			group_id,
			COUNT(*),
			AVG(first_token_ms::float8) FILTER (WHERE first_token_ms IS NOT NULL AND first_token_ms > 0),
			SUM(input_tokens::float8),
			SUM(cache_read_tokens::float8)
		FROM usage_logs
		WHERE created_at >= $1 AND group_id IS NOT NULL
		GROUP BY group_id
	`, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[int64]*service.GroupUsageStats{}
	for rows.Next() {
		var groupID, requests int64
		var ttftAvg, inputSum, cacheReadSum sql.NullFloat64
		if err := rows.Scan(&groupID, &requests, &ttftAvg, &inputSum, &cacheReadSum); err != nil {
			return nil, err
		}
		s := &service.GroupUsageStats{Requests: requests}
		if ttftAvg.Valid {
			s.AvgTTFTMs = ttftAvg.Float64
		}
		if inputSum.Valid && cacheReadSum.Valid && inputSum.Float64+cacheReadSum.Float64 > 0 {
			s.CacheRate = cacheReadSum.Float64 / (inputSum.Float64 + cacheReadSum.Float64) * 100
		}
		out[groupID] = s
	}
	return out, rows.Err()
}

// RecentGroupModels 返回某分组最近成功请求的模型（按使用量降序）。
func (r *groupMonitorRepository) RecentGroupModels(ctx context.Context, groupID int64, since time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT requested_model, COUNT(*) AS cnt
		FROM usage_logs
		WHERE group_id = $1 AND created_at >= $2 AND requested_model <> ''
		GROUP BY requested_model
		ORDER BY cnt DESC
		LIMIT $3
	`, groupID, since, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var models []string
	for rows.Next() {
		var model string
		var cnt int64
		if err := rows.Scan(&model, &cnt); err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	return models, rows.Err()
}

// ListRecentUsageEvents 按 group_id 各取最近 limit 条真实请求事件：
// 成功请求来自 usage_logs，失败请求来自 ops_error_logs，两路各取 limit 后合并。
// 全部走 (group_id, created_at) 索引，成本与分组数成正比、与表大小无关。
func (r *groupMonitorRepository) ListRecentUsageEvents(
	ctx context.Context, groupIDs []int64, limit int,
) (map[int64][]service.GroupUsageEvent, error) {
	out := map[int64][]service.GroupUsageEvent{}
	if len(groupIDs) == 0 || limit <= 0 {
		return out, nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT group_id, created_at, true, COALESCE(first_token_ms, 0),
			COALESCE(input_tokens, 0), COALESCE(cache_read_tokens, 0)
		FROM (
			SELECT group_id, created_at, first_token_ms, input_tokens, cache_read_tokens,
				ROW_NUMBER() OVER (PARTITION BY group_id ORDER BY created_at DESC) AS rn
			FROM usage_logs
			WHERE group_id = ANY($1)
		) t
		WHERE rn <= $2
	`, pq.Array(groupIDs), limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e service.GroupUsageEvent
		if err := rows.Scan(&e.GroupID, &e.CreatedAt, &e.Success, &e.TTFTMs, &e.InputTokens, &e.CacheRead); err != nil {
			rows.Close()
			return nil, err
		}
		out[e.GroupID] = append(out[e.GroupID], e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.db.QueryContext(ctx, `
		SELECT group_id, created_at, false, 0, 0, 0
		FROM (
			SELECT group_id, created_at,
				ROW_NUMBER() OVER (PARTITION BY group_id ORDER BY created_at DESC) AS rn
			FROM ops_error_logs
			WHERE group_id = ANY($1)
		) t
		WHERE rn <= $2
	`, pq.Array(groupIDs), limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e service.GroupUsageEvent
		if err := rows.Scan(&e.GroupID, &e.CreatedAt, &e.Success, &e.TTFTMs, &e.InputTokens, &e.CacheRead); err != nil {
			rows.Close()
			return nil, err
		}
		out[e.GroupID] = append(out[e.GroupID], e)
	}
	rows.Close()
	return out, rows.Err()
}

// ListUsageEventsSince 返回 since 之后各分组的真实请求事件（成功+失败）。
// 走 (group_id, created_at) 索引；窗口内行数有界（状态条桶数×分组数级别）。
func (r *groupMonitorRepository) ListUsageEventsSince(
	ctx context.Context, groupIDs []int64, since time.Time,
) (map[int64][]service.GroupUsageEvent, error) {
	out := map[int64][]service.GroupUsageEvent{}
	if len(groupIDs) == 0 {
		return out, nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT group_id, created_at, true, COALESCE(first_token_ms, 0),
			COALESCE(input_tokens, 0), COALESCE(cache_read_tokens, 0)
		FROM usage_logs
		WHERE group_id = ANY($1) AND created_at >= $2
	`, pq.Array(groupIDs), since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e service.GroupUsageEvent
		if err := rows.Scan(&e.GroupID, &e.CreatedAt, &e.Success, &e.TTFTMs, &e.InputTokens, &e.CacheRead); err != nil {
			rows.Close()
			return nil, err
		}
		out[e.GroupID] = append(out[e.GroupID], e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.db.QueryContext(ctx, `
		SELECT group_id, created_at, false, 0, 0, 0
		FROM ops_error_logs
		WHERE group_id = ANY($1) AND created_at >= $2
	`, pq.Array(groupIDs), since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e service.GroupUsageEvent
		if err := rows.Scan(&e.GroupID, &e.CreatedAt, &e.Success, &e.TTFTMs, &e.InputTokens, &e.CacheRead); err != nil {
			rows.Close()
			return nil, err
		}
		out[e.GroupID] = append(out[e.GroupID], e)
	}
	rows.Close()
	return out, rows.Err()
}

// ListProbeRecordsSince 返回 since 之后各监控的探测记录（状态条时间桶聚合用）。
func (r *groupMonitorRepository) ListProbeRecordsSince(
	ctx context.Context, monitorIDs []int64, since time.Time,
) (map[int64][]service.GroupProbeRecord, error) {
	out := map[int64][]service.GroupProbeRecord{}
	if len(monitorIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT monitor_id, checked_at, status, latency_ms
		FROM group_monitor_result_history
		WHERE monitor_id = ANY($1) AND checked_at >= $2
		ORDER BY checked_at ASC
	`, pq.Array(monitorIDs), since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p service.GroupProbeRecord
		if err := rows.Scan(&p.MonitorID, &p.CheckedAt, &p.Status, &p.LatencyMs); err != nil {
			rows.Close()
			return nil, err
		}
		out[p.MonitorID] = append(out[p.MonitorID], p)
	}
	rows.Close()
	return out, rows.Err()
}

// AggregateUsageSince 按 group_id 精确聚合 since 之后的真实请求：
// 成功（usage_logs）+ 失败（ops_error_logs）全窗 COUNT，TTFT 均值与缓存率。
// 不做最近 N 条截断，可用率 = success/(success+failed) 真实反映整窗。
func (r *groupMonitorRepository) AggregateUsageSince(
	ctx context.Context, groupIDs []int64, since time.Time,
) (map[int64]*service.GroupWindowAgg, error) {
	out := map[int64]*service.GroupWindowAgg{}
	if len(groupIDs) == 0 {
		return out, nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT group_id, COUNT(*),
			AVG(first_token_ms::float8) FILTER (WHERE first_token_ms IS NOT NULL AND first_token_ms > 0),
			SUM(input_tokens::float8), SUM(cache_read_tokens::float8)
		FROM usage_logs
		WHERE group_id = ANY($1) AND created_at >= $2
		GROUP BY group_id
	`, pq.Array(groupIDs), since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var gid, cnt int64
		var ttft, inSum, cacheSum sql.NullFloat64
		if err := rows.Scan(&gid, &cnt, &ttft, &inSum, &cacheSum); err != nil {
			rows.Close()
			return nil, err
		}
		a := out[gid]
		if a == nil {
			a = &service.GroupWindowAgg{}
			out[gid] = a
		}
		a.Success += cnt
		if ttft.Valid {
			a.AvgTTFTMs = ttft.Float64
		}
		if inSum.Valid && cacheSum.Valid && inSum.Float64+cacheSum.Float64 > 0 {
			a.CacheRate = cacheSum.Float64 / (inSum.Float64 + cacheSum.Float64) * 100
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.db.QueryContext(ctx, `
		SELECT group_id, COUNT(*)
		FROM ops_error_logs
		WHERE group_id = ANY($1) AND created_at >= $2
		GROUP BY group_id
	`, pq.Array(groupIDs), since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var gid, cnt int64
		if err := rows.Scan(&gid, &cnt); err != nil {
			rows.Close()
			return nil, err
		}
		a := out[gid]
		if a == nil {
			a = &service.GroupWindowAgg{}
			out[gid] = a
		}
		a.Failed += cnt
	}
	rows.Close()
	return out, rows.Err()
}

// AggregateProbesSince 按 monitor_id 精确聚合 since 之后的探测记录。
func (r *groupMonitorRepository) AggregateProbesSince(
	ctx context.Context, monitorIDs []int64, since time.Time,
) (map[int64]*service.GroupWindowAgg, error) {
	out := map[int64]*service.GroupWindowAgg{}
	if len(monitorIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT monitor_id,
			COUNT(*) FILTER (WHERE status = 'success'),
			COUNT(*) FILTER (WHERE status <> 'success'),
			AVG(latency_ms::float8) FILTER (WHERE status = 'success')
		FROM group_monitor_result_history
		WHERE monitor_id = ANY($1) AND checked_at >= $2
		GROUP BY monitor_id
	`, pq.Array(monitorIDs), since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var mid, succ, fail int64
		var lat sql.NullFloat64
		if err := rows.Scan(&mid, &succ, &fail, &lat); err != nil {
			rows.Close()
			return nil, err
		}
		a := &service.GroupWindowAgg{Success: succ, Failed: fail}
		if lat.Valid {
			a.AvgLatencyMs = lat.Float64
		}
		out[mid] = a
	}
	rows.Close()
	return out, rows.Err()
}

// ListAccountStatesBatch 一次性返回所有监控的账号最新状态（按优先级排序）。
func (r *groupMonitorRepository) ListAccountStatesBatch(ctx context.Context) (map[int64][]*service.GroupMonitorAccountStatus, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT gr.monitor_id, gr.account_id, a.name, a.platform, gr.status, gr.model_id, gr.latency_ms, gr.error_message, gr.checked_at
		FROM group_monitor_results gr
		JOIN accounts a ON a.id = gr.account_id
		JOIN group_monitors gm ON gm.id = gr.monitor_id
		LEFT JOIN account_groups ag ON ag.account_id = gr.account_id AND ag.group_id = gm.group_id
		WHERE COALESCE(a.schedulable, true) AND a.deleted_at IS NULL
		ORDER BY gr.monitor_id ASC, ag.priority ASC NULLS LAST, a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[int64][]*service.GroupMonitorAccountStatus{}
	for rows.Next() {
		var monitorID int64
		s := &service.GroupMonitorAccountStatus{}
		if err := rows.Scan(&monitorID, &s.AccountID, &s.AccountName, &s.Platform, &s.Status, &s.ModelID, &s.LatencyMs, &s.ErrorMessage, &s.CheckedAt); err != nil {
			return nil, err
		}
		out[monitorID] = append(out[monitorID], s)
	}
	return out, rows.Err()
}
