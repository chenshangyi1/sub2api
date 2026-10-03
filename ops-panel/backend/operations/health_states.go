package operations

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/health"
)

// AccountHealthState 账号的六状态健康快照（由 upstream_monitor_results 回放得出）。
type AccountHealthState struct {
	State               health.State `json:"state"`
	WeightPercent       int          `json:"weight_percent"`
	ConsecutiveFailures int          `json:"consecutive_failures"`
	Score               float64      `json:"score,omitempty"`
	ChangedAt           time.Time    `json:"changed_at,omitempty"`
}

type monitorResultRow struct {
	AccountID int64     `gorm:"column:account_id"`
	Status    string    `gorm:"column:status"`
	Message   string    `gorm:"column:message"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

var statusCodePattern = regexp.MustCompile(`\b([45]\d{2})\b`)

// classifyMonitorResult 把上游监控结果行映射为健康机探测输入。
func classifyMonitorResult(row monitorResultRow) health.ProbeResult {
	status := strings.ToLower(strings.TrimSpace(row.Status))
	if status == "success" || status == "healthy" || status == "operational" || status == "ok" {
		return health.ProbeResult{Success: true, At: row.CreatedAt}
	}
	message := strings.ToLower(row.Message)
	code := 0
	if match := statusCodePattern.FindStringSubmatch(message); match != nil {
		code, _ = strconv.Atoi(match[1])
	}
	switch {
	case strings.Contains(message, "429") || strings.Contains(message, "too many requests") ||
		strings.Contains(message, "rate limit") || strings.Contains(message, "timeout") ||
		strings.Contains(message, "timed out") || strings.Contains(message, "connection") ||
		strings.Contains(message, "network"):
		// 429 / 网络错误 / 超时 → 软失败
	case strings.Contains(message, "401") || strings.Contains(message, "403") ||
		strings.Contains(message, "404") || strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "forbidden") || strings.Contains(message, "permission denied") ||
		strings.Contains(message, "invalid api key"):
		code = 403
	}
	return health.ProbeResult{Success: false, StatusCode: code, At: row.CreatedAt}
}

// AccountHealthStates 按账号回放最近 7 天的监控结果，返回六状态快照。
// 无结果或回放失败的账号不返回（由调用方按 healthy 处理）。
func (s *Service) AccountHealthStates(ctx context.Context, targetID uint) (map[int64]AccountHealthState, error) {
	out := map[int64]AccountHealthState{}
	if s.db == nil || !s.db.Migrator().HasTable("upstream_monitor_results") {
		return out, nil
	}
	var rows []monitorResultRow
	if err := s.db.WithContext(ctx).Raw(`
		SELECT account_id, status, COALESCE(message, '') AS message, created_at
		FROM upstream_monitor_results
		WHERE connection_id = ? AND created_at >= ?
		ORDER BY account_id, created_at ASC`, targetID, s.now().AddDate(0, 0, -7)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	snaps := map[int64]health.Snapshot{}
	lastChange := map[int64]time.Time{}
	successCount := map[int64]int64{}
	resultCount := map[int64]int64{}
	for _, row := range rows {
		resultCount[row.AccountID]++
		if strings.EqualFold(strings.TrimSpace(row.Status), "success") {
			successCount[row.AccountID]++
		}
		before := snaps[row.AccountID]
		after, tr := health.Step(health.Config{}, before, classifyMonitorResult(row))
		snaps[row.AccountID] = after
		if tr.Changed {
			lastChange[row.AccountID] = row.CreatedAt
		}
	}
	for accountID, snap := range snaps {
		score := 0.0
		if resultCount[accountID] > 0 {
			score = float64(successCount[accountID]) * 100 / float64(resultCount[accountID])
		}
		out[accountID] = AccountHealthState{
			State:               snap.State,
			WeightPercent:       snap.WeightPercent,
			ConsecutiveFailures: snap.ConsecutiveFailures,
			Score:               score,
			ChangedAt:           lastChange[accountID],
		}
	}
	return out, nil
}
