package operations

import (
	"context"
	"time"
)

// GroupMonitorOverview 分组监控聚合视图（运营面板）。
type GroupMonitorOverview struct {
	TotalMonitors   int64                    `json:"total_monitors"`
	HealthyMonitors int64                    `json:"healthy_monitors"`
	FailedMonitors  int64                    `json:"failed_monitors"`
	TotalAccounts   int64                    `json:"total_accounts"`
	HealthyAccounts int64                    `json:"healthy_accounts"`
	FailedAccounts  int64                    `json:"failed_accounts"`
	AvgAvailability float64                  `json:"avg_availability"`
	Monitors        []GroupMonitorSummaryRow `json:"monitors"`
}

// GroupMonitorSummaryRow 单个分组监控的聚合行。
type GroupMonitorSummaryRow struct {
	MonitorID    int64   `json:"monitor_id"`
	GroupID      int64   `json:"group_id"`
	GroupName    string  `json:"group_name"`
	Enabled      bool    `json:"enabled"`
	IntervalMin  int     `json:"interval_minutes"`
	LastRunAt    *string `json:"last_run_at,omitempty"`
	AccountCount int64   `json:"account_count"`
	HealthyCount int64   `json:"healthy_count"`
	FailedCount  int64   `json:"failed_count"`
	UnknownCount int64   `json:"unknown_count"`
	Probes7d     int64   `json:"probes_7d"`
	Availability float64 `json:"availability_7d"`
	AvgTTFTMs    float64 `json:"avg_ttft_ms_7d"`
	CacheRate    float64 `json:"cache_rate_7d"`
}

// GetGroupMonitorOverview 聚合 sub2api 主库的分组监控数据（只读直连）。
func (s *Service) GetGroupMonitorOverview(ctx context.Context) (*GroupMonitorOverview, error) {
	if s.mainDB == nil {
		return nil, ErrInvalid
	}

	since7d := time.Now().Add(-7 * 24 * time.Hour)

	rows, err := s.mainDB.WithContext(ctx).Raw(`
		SELECT
			gm.id, gm.group_id, g.name, gm.enabled, gm.interval_minutes, gm.last_run_at,
			COALESCE(acct.cnt, 0), COALESCE(acct.healthy, 0), COALESCE(acct.failed, 0), COALESCE(acct.unknown, 0),
			COALESCE(hist.probes, 0), COALESCE(hist.successes, 0),
			COALESCE(hist.ttft_sum, 0), COALESCE(hist.input_sum, 0), COALESCE(hist.cache_read_sum, 0)
		FROM group_monitors gm
		JOIN groups g ON g.id = gm.group_id
		LEFT JOIN (
			SELECT monitor_id,
				COUNT(*) AS cnt,
				COUNT(*) FILTER (WHERE status = 'success') AS healthy,
				COUNT(*) FILTER (WHERE status = 'failed') AS failed,
				COUNT(*) FILTER (WHERE status = 'unknown') AS unknown
			FROM group_monitor_results
			GROUP BY monitor_id
		) acct ON acct.monitor_id = gm.id
		LEFT JOIN (
			SELECT monitor_id,
				COUNT(*) AS probes,
				COUNT(*) FILTER (WHERE status = 'success') AS successes,
				SUM(ttft_ms::float8) FILTER (WHERE status = 'success') AS ttft_sum,
				SUM(input_tokens::float8) AS input_sum,
				SUM(cache_read_tokens::float8) AS cache_read_sum
			FROM group_monitor_result_history
			WHERE checked_at >= ?
			GROUP BY monitor_id
		) hist ON hist.monitor_id = gm.id
		ORDER BY gm.id ASC
	`, since7d).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	overview := &GroupMonitorOverview{Monitors: []GroupMonitorSummaryRow{}}
	var availSum float64
	var availCount int64

	for rows.Next() {
		var r GroupMonitorSummaryRow
		var lastRun *time.Time
		var successes, ttftSum, inputSum, cacheReadSum float64
		if err := rows.Scan(&r.MonitorID, &r.GroupID, &r.GroupName, &r.Enabled, &r.IntervalMin, &lastRun,
			&r.AccountCount, &r.HealthyCount, &r.FailedCount, &r.UnknownCount,
			&r.Probes7d, &successes, &ttftSum, &inputSum, &cacheReadSum); err != nil {
			return nil, err
		}
		if lastRun != nil {
			ts := lastRun.UTC().Format("2006-01-02T15:04:05Z")
			r.LastRunAt = &ts
		}
		if r.Probes7d > 0 {
			r.Availability = successes / float64(r.Probes7d) * 100
			availSum += r.Availability
			availCount++
		}
		if successes > 0 {
			r.AvgTTFTMs = ttftSum / successes
		}
		if inputSum+cacheReadSum > 0 {
			r.CacheRate = cacheReadSum / (inputSum + cacheReadSum) * 100
		}

		overview.TotalMonitors++
		if r.FailedCount == 0 && r.UnknownCount == 0 {
			overview.HealthyMonitors++
		} else {
			overview.FailedMonitors++
		}
		overview.TotalAccounts += r.AccountCount
		overview.HealthyAccounts += r.HealthyCount
		overview.FailedAccounts += r.FailedCount
		overview.Monitors = append(overview.Monitors, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if availCount > 0 {
		overview.AvgAvailability = availSum / float64(availCount)
	}
	return overview, nil
}
