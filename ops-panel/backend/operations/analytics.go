package operations

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const analyticsTimezone = "Asia/Shanghai"

type analyticsAggregateRow struct {
	Requests            int64   `gorm:"column:requests"`
	ActiveUsers         int64   `gorm:"column:active_users"`
	StreamRequests      int64   `gorm:"column:stream_requests"`
	InputTokens         int64   `gorm:"column:input_tokens"`
	OutputTokens        int64   `gorm:"column:output_tokens"`
	CacheReadTokens     int64   `gorm:"column:cache_read_tokens"`
	CacheCreationTokens int64   `gorm:"column:cache_creation_tokens"`
	UserCost            float64 `gorm:"column:user_cost"`
	UpstreamCost        float64 `gorm:"column:upstream_cost"`
	AdministratorCost   float64 `gorm:"column:administrator_cost"`
	AverageFirstTokenMS float64 `gorm:"column:average_first_token_ms"`
	P95FirstTokenMS     float64 `gorm:"column:p95_first_token_ms"`
	FirstTokenSamples   int64   `gorm:"column:first_token_samples"`
	SlowFirstTokens     int64   `gorm:"column:slow_first_tokens"`
}

type analyticsDayRow struct {
	Date      time.Time             `gorm:"column:bucket_date"`
	Aggregate analyticsAggregateRow `gorm:"embedded"`
}

type actualUpstreamCostDayRow struct {
	Date time.Time `gorm:"column:bucket_date"`
	Cost float64   `gorm:"column:upstream_cost"`
}

type analyticsHeatmapRow struct {
	Date                time.Time `gorm:"column:bucket_date"`
	Hour                int       `gorm:"column:bucket_hour"`
	Requests            int64     `gorm:"column:requests"`
	Failures            int64     `gorm:"column:failures"`
	AverageFirstTokenMS float64   `gorm:"column:average_first_token_ms"`
}

type slowRequestRow struct {
	ID           int64     `gorm:"column:id"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UserID       int64     `gorm:"column:user_id"`
	UserName     string    `gorm:"column:user_name"`
	AccountID    int64     `gorm:"column:account_id"`
	Model        string    `gorm:"column:model"`
	Stream       bool      `gorm:"column:stream"`
	DurationMS   int       `gorm:"column:duration_ms"`
	FirstTokenMS *int      `gorm:"column:first_token_ms"`
	StatusCode   int       `gorm:"column:status_code"`
	Error        string    `gorm:"column:error"`
}

func (s *Service) GetAnalytics(ctx context.Context, requestedRange string) (*Analytics, error) {
	if s.mainDB == nil {
		return nil, fmt.Errorf("%w: SUB2API_DATABASE_URL is not configured", ErrInvalid)
	}
	if !s.mainDB.Migrator().HasTable("usage_logs") || !s.mainDB.Migrator().HasTable("users") {
		return nil, fmt.Errorf("%w: target usage tables are not available", ErrInvalid)
	}

	rangeKey := strings.ToLower(strings.TrimSpace(requestedRange))
	location, err := time.LoadLocation(analyticsTimezone)
	if err != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	now := s.now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	start := today
	label := "Today"
	switch rangeKey {
	case "day", "":
		rangeKey = "day"
	case "week":
		offset := (int(today.Weekday()) + 6) % 7
		start = today.AddDate(0, 0, -offset)
		label = "This week"
	case "month":
		start = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, location)
		label = "This month"
	default:
		return nil, fmt.Errorf("%w: range must be day, week or month", ErrInvalid)
	}
	end := today.AddDate(0, 0, 1)
	costExpression := s.analyticsUpstreamCostExpression()

	summaryRow, err := s.analyticsAggregate(ctx, start, end, costExpression)
	if err != nil {
		return nil, err
	}
	actualUpstreamCosts, hasActualUpstreamCosts, err := s.analyticsActualUpstreamCosts(ctx, start, end, today, location)
	if err != nil {
		return nil, err
	}
	if hasActualUpstreamCosts {
		summaryRow.UpstreamCost = sumActualUpstreamCosts(actualUpstreamCosts, start, today, location)
	}
	summary := mapAnalyticsPeriod(summaryRow, rangeKey, label, start, end)
	operatingCosts := s.analyticsOperatingCostsByDay(ctx, start, today, location)
	summary.OperatingCost = sumOperatingCosts(operatingCosts, start, today, location)
	summary.Profit = summary.UserCost - summary.UpstreamCost - summary.OperatingCost
	if summary.UserCost > 0 {
		summary.ProfitMargin = summary.Profit / summary.UserCost
	} else {
		summary.ProfitMargin = 0
	}
	daily, err := s.analyticsDaily(ctx, start, end, today, location, costExpression)
	if err != nil {
		return nil, err
	}
	if hasActualUpstreamCosts {
		for index := range daily {
			if cost, ok := actualUpstreamCosts[daily[index].Date]; ok {
				daily[index].UpstreamCost = cost
				operating := operatingCosts[daily[index].Date]
				daily[index].OperatingCost = operating
				daily[index].Profit = daily[index].UserCost - cost - operating
				if daily[index].UserCost > 0 {
					daily[index].ProfitMargin = daily[index].Profit / daily[index].UserCost
				} else {
					daily[index].ProfitMargin = 0
				}
			}
		}
	}
	for index := range daily {
		if _, applied := actualUpstreamCosts[daily[index].Date]; applied || !hasActualUpstreamCosts {
			if cost, ok := operatingCosts[daily[index].Date]; ok {
				daily[index].OperatingCost = cost
				daily[index].Profit = daily[index].UserCost - daily[index].UpstreamCost - cost
				if daily[index].UserCost > 0 {
					daily[index].ProfitMargin = daily[index].Profit / daily[index].UserCost
				} else {
					daily[index].ProfitMargin = 0
				}
			}
		}
	}
	heatmap, err := s.analyticsHeatmap(ctx, today, location)
	if err != nil {
		return nil, err
	}
	slow, err := s.analyticsSlowRequests(ctx, start, end)
	if err != nil {
		return nil, err
	}
	return &Analytics{Range: rangeKey, Summary: summary, Daily: daily, Heatmap: heatmap, SlowRequests: slow}, nil
}

func (s *Service) analyticsUpstreamCostExpression() string {
	base := "COALESCE(l.total_cost, 0)"
	if s.mainDB.Migrator().HasColumn("usage_logs", "account_rate_multiplier") {
		return base + " * COALESCE(l.account_rate_multiplier, 1)"
	}
	return base
}

// analyticsActualUpstreamCosts reads the authoritative channel cost snapshots.
// usage_logs can estimate account cost, but it cannot represent provider-side
// billing adjustments or cache behavior that the channel monitor observes.
func (s *Service) analyticsActualUpstreamCosts(
	ctx context.Context,
	start, end, today time.Time,
	location *time.Location,
) (map[string]float64, bool, error) {
	if s.db == nil || !s.db.Migrator().HasTable("cost_snapshots") {
		return nil, false, nil
	}

	var rows []actualUpstreamCostDayRow
	joinClause := ""
	selfOperatedFilter := ""
	if s.db.Migrator().HasColumn("channels", "self_operated") {
		joinClause = "LEFT JOIN channels ch ON ch.id = cs.channel_id"
		selfOperatedFilter = "AND COALESCE(ch.self_operated, false) = false"
	}
	query := fmt.Sprintf(`
		SELECT bucket_date, COALESCE(SUM(channel_cost), 0)::double precision AS upstream_cost
		FROM (
			SELECT
				(cs.sampled_at AT TIME ZONE ?)::date AS bucket_date,
				cs.channel_id,
				MAX(COALESCE(cs.today_cost, 0)) AS channel_cost
			FROM cost_snapshots cs %s
			WHERE cs.sampled_at >= ? AND cs.sampled_at < ? %s
			GROUP BY bucket_date, cs.channel_id
		) daily_channels
		GROUP BY bucket_date
		ORDER BY bucket_date`, joinClause, selfOperatedFilter)
	if err := s.db.WithContext(ctx).Raw(query, analyticsTimezone, start.UTC(), end.UTC()).Scan(&rows).Error; err != nil {
		return nil, true, err
	}

	byDate := make(map[string]float64, len(rows)+1)
	for _, row := range rows {
		byDate[row.Date.In(location).Format("2006-01-02")] = row.Cost
	}

	// The live channel row is newer than the last periodic snapshot. Use it
	// only for today; historical dates must remain snapshot-based.
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)
	tomorrow := todayStart.AddDate(0, 0, 1)
	if todayStart.Before(end) && tomorrow.After(start) && s.db.Migrator().HasTable("channels") {
		var currentCost float64
		liveQuery := s.db.WithContext(ctx).Table("channels")
		if s.db.Migrator().HasColumn("channels", "self_operated") {
			liveQuery = liveQuery.Where("COALESCE(self_operated, false) = false")
		}
		if err := liveQuery.Select("COALESCE(SUM(today_cost), 0)").Scan(&currentCost).Error; err != nil {
			return nil, true, err
		}
		key := todayStart.Format("2006-01-02")
		if currentCost > byDate[key] {
			byDate[key] = currentCost
		}
	}

	return byDate, true, nil
}

type operatingCostDayRow struct {
	Date time.Time `gorm:"column:bucket_date"`
	Cost float64   `gorm:"column:operating_cost"`
}

// analyticsOperatingCostsByDay 读取自营站的运营成本，按发生日归并（UTC 落库，按 Asia/Shanghai 切日）。
func (s *Service) analyticsOperatingCostsByDay(ctx context.Context, start, today time.Time, location *time.Location) map[string]float64 {
	byDate := map[string]float64{}
	if s.db == nil || !s.db.Migrator().HasTable("operating_costs") {
		return byDate
	}
	end := today.AddDate(0, 0, 1)
	var rows []operatingCostDayRow
	query := `
		SELECT (oc.occurred_at AT TIME ZONE ?)::date AS bucket_date,
		       COALESCE(SUM(oc.amount), 0)::double precision AS operating_cost
		FROM operating_costs oc
		WHERE oc.occurred_at >= ? AND oc.occurred_at < ?
		GROUP BY bucket_date`
	if err := s.db.WithContext(ctx).Raw(query, analyticsTimezone, start.UTC(), end.UTC()).Scan(&rows).Error; err != nil {
		s.log.Warn("load operating costs failed", "err", err)
		return byDate
	}
	for _, row := range rows {
		byDate[row.Date.In(location).Format("2006-01-02")] = row.Cost
	}
	return byDate
}

func sumOperatingCosts(byDate map[string]float64, start, today time.Time, location *time.Location) float64 {
	total := 0.0
	for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
		total += byDate[day.In(location).Format("2006-01-02")]
	}
	return total
}

func sumActualUpstreamCosts(byDate map[string]float64, start, today time.Time, location *time.Location) float64 {
	total := 0.0
	for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
		total += byDate[day.In(location).Format("2006-01-02")]
	}
	return total
}

func (s *Service) analyticsAggregate(ctx context.Context, start, end time.Time, costExpression string) (analyticsAggregateRow, error) {
	var row analyticsAggregateRow
	query := fmt.Sprintf(`
		SELECT
			COUNT(l.id)::bigint AS requests,
			COUNT(DISTINCT l.user_id)::bigint AS active_users,
			COUNT(l.id) FILTER (WHERE l.stream = true)::bigint AS stream_requests,
			COALESCE(SUM(COALESCE(l.input_tokens, 0)), 0)::bigint AS input_tokens,
			COALESCE(SUM(COALESCE(l.output_tokens, 0)), 0)::bigint AS output_tokens,
			COALESCE(SUM(COALESCE(l.cache_read_tokens, 0)), 0)::bigint AS cache_read_tokens,
			COALESCE(SUM(COALESCE(l.cache_creation_tokens, 0)), 0)::bigint AS cache_creation_tokens,
			COALESCE(SUM(COALESCE(l.actual_cost, 0)) FILTER (WHERE COALESCE(u.role, '') <> 'admin'), 0)::double precision AS user_cost,
			COALESCE(SUM(%[1]s), 0)::double precision AS upstream_cost,
			COALESCE(SUM(%[1]s) FILTER (WHERE u.role = 'admin'), 0)::double precision AS administrator_cost,
			COALESCE(AVG(l.first_token_ms) FILTER (WHERE l.first_token_ms IS NOT NULL), 0)::double precision AS average_first_token_ms,
			COALESCE(percentile_disc(0.95) WITHIN GROUP (ORDER BY l.first_token_ms) FILTER (WHERE l.first_token_ms IS NOT NULL), 0)::double precision AS p95_first_token_ms,
			COUNT(l.id) FILTER (WHERE l.first_token_ms IS NOT NULL)::bigint AS first_token_samples,
			COUNT(l.id) FILTER (WHERE l.first_token_ms > 10000)::bigint AS slow_first_tokens
		FROM usage_logs l
		LEFT JOIN users u ON u.id = l.user_id
		WHERE l.created_at >= ? AND l.created_at < ?`, costExpression)
	if err := s.mainDB.WithContext(ctx).Raw(query, start.UTC(), end.UTC()).Scan(&row).Error; err != nil {
		return row, err
	}
	return row, nil
}

func mapAnalyticsPeriod(row analyticsAggregateRow, key, label string, start, end time.Time) AnalyticsPeriod {
	promptTokens := row.InputTokens + row.CacheReadTokens + row.CacheCreationTokens
	cacheHitRate := 0.0
	if promptTokens > 0 {
		cacheHitRate = float64(row.CacheReadTokens) / float64(promptTokens)
	}
	profit := row.UserCost - row.UpstreamCost
	profitMargin := 0.0
	if row.UserCost > 0 {
		profitMargin = profit / row.UserCost
	}
	slowRate := 0.0
	if row.FirstTokenSamples > 0 {
		slowRate = float64(row.SlowFirstTokens) / float64(row.FirstTokenSamples)
	}
	return AnalyticsPeriod{
		Key: key, Label: label, StartAt: start.UTC(), EndAt: end.Add(-time.Nanosecond).UTC(),
		UserCost: row.UserCost, UpstreamCost: row.UpstreamCost,
		AdministratorCost: row.AdministratorCost, Profit: profit, ProfitMargin: profitMargin,
		Requests: row.Requests, ActiveUsers: row.ActiveUsers, StreamRequests: row.StreamRequests,
		TotalTokens: row.InputTokens + row.OutputTokens + row.CacheReadTokens + row.CacheCreationTokens,
		InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
		CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens,
		CacheHitRate: cacheHitRate, AverageFirstTokenMS: row.AverageFirstTokenMS,
		P95FirstTokenMS: row.P95FirstTokenMS, SlowFirstTokenRate: slowRate,
	}
}

func (s *Service) analyticsDaily(ctx context.Context, start, end, today time.Time, location *time.Location, costExpression string) ([]AnalyticsDay, error) {
	var rows []analyticsDayRow
	query := fmt.Sprintf(`
		SELECT (l.created_at AT TIME ZONE 'Asia/Shanghai')::date AS bucket_date,
			COUNT(l.id)::bigint AS requests,
			COUNT(DISTINCT l.user_id)::bigint AS active_users,
			COUNT(l.id) FILTER (WHERE l.stream = true)::bigint AS stream_requests,
			COALESCE(SUM(COALESCE(l.input_tokens, 0)), 0)::bigint AS input_tokens,
			COALESCE(SUM(COALESCE(l.output_tokens, 0)), 0)::bigint AS output_tokens,
			COALESCE(SUM(COALESCE(l.cache_read_tokens, 0)), 0)::bigint AS cache_read_tokens,
			COALESCE(SUM(COALESCE(l.cache_creation_tokens, 0)), 0)::bigint AS cache_creation_tokens,
			COALESCE(SUM(COALESCE(l.actual_cost, 0)) FILTER (WHERE COALESCE(u.role, '') <> 'admin'), 0)::double precision AS user_cost,
			COALESCE(SUM(%[1]s), 0)::double precision AS upstream_cost,
			COALESCE(SUM(%[1]s) FILTER (WHERE u.role = 'admin'), 0)::double precision AS administrator_cost,
			COALESCE(AVG(l.first_token_ms) FILTER (WHERE l.first_token_ms IS NOT NULL), 0)::double precision AS average_first_token_ms,
			COALESCE(percentile_disc(0.95) WITHIN GROUP (ORDER BY l.first_token_ms) FILTER (WHERE l.first_token_ms IS NOT NULL), 0)::double precision AS p95_first_token_ms,
			COUNT(l.id) FILTER (WHERE l.first_token_ms IS NOT NULL)::bigint AS first_token_samples,
			COUNT(l.id) FILTER (WHERE l.first_token_ms > 10000)::bigint AS slow_first_tokens
		FROM usage_logs l
		LEFT JOIN users u ON u.id = l.user_id
		WHERE l.created_at >= ? AND l.created_at < ?
		GROUP BY bucket_date ORDER BY bucket_date ASC`, costExpression)
	if err := s.mainDB.WithContext(ctx).Raw(query, start.UTC(), end.UTC()).Scan(&rows).Error; err != nil {
		return nil, err
	}
	byDate := make(map[string]analyticsAggregateRow, len(rows))
	for _, row := range rows {
		byDate[row.Date.In(location).Format("2006-01-02")] = row.Aggregate
	}
	items := make([]AnalyticsDay, 0, int(today.Sub(start).Hours()/24)+1)
	for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		period := mapAnalyticsPeriod(byDate[key], key, key, day, day.AddDate(0, 0, 1))
		items = append(items, AnalyticsDay{AnalyticsPeriod: period, Date: key})
	}
	return items, nil
}

func (s *Service) analyticsHeatmap(ctx context.Context, today time.Time, location *time.Location) ([]AnalyticsHeatmapCell, error) {
	start := today.AddDate(0, 0, -6)
	end := today.AddDate(0, 0, 1)
	failureExpression := "0"
	if s.mainDB.Migrator().HasColumn("usage_logs", "status_code") {
		failureExpression = "COUNT(l.id) FILTER (WHERE l.status_code < 200 OR l.status_code >= 400)"
	}
	var rows []analyticsHeatmapRow
	query := fmt.Sprintf(`
		SELECT (l.created_at AT TIME ZONE 'Asia/Shanghai')::date AS bucket_date,
		       EXTRACT(hour FROM l.created_at AT TIME ZONE 'Asia/Shanghai')::int AS bucket_hour,
		       COUNT(l.id)::bigint AS requests,
		       (%s)::bigint AS failures,
		       COALESCE(AVG(l.first_token_ms) FILTER (WHERE l.first_token_ms IS NOT NULL), 0)::double precision AS average_first_token_ms
		FROM usage_logs l
		JOIN users u ON u.id = l.user_id
		WHERE u.role <> 'admin' AND l.created_at >= ? AND l.created_at < ?
		GROUP BY bucket_date, bucket_hour
		ORDER BY bucket_date, bucket_hour`, failureExpression)
	if err := s.mainDB.WithContext(ctx).Raw(query, start.UTC(), end.UTC()).Scan(&rows).Error; err != nil {
		return nil, err
	}
	byCell := map[string]analyticsHeatmapRow{}
	for _, row := range rows {
		key := fmt.Sprintf("%s:%d", row.Date.In(location).Format("2006-01-02"), row.Hour)
		byCell[key] = row
	}
	items := make([]AnalyticsHeatmapCell, 0, 7*24)
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		for hour := 0; hour < 24; hour++ {
			row := byCell[fmt.Sprintf("%s:%d", date, hour)]
			items = append(items, AnalyticsHeatmapCell{Date: date, Hour: hour, Requests: row.Requests, Failures: row.Failures, AverageFirstTokenMS: row.AverageFirstTokenMS})
		}
	}
	return items, nil
}

func (s *Service) analyticsSlowRequests(ctx context.Context, start, end time.Time) ([]SlowRequest, error) {
	statusExpression := "200"
	if s.mainDB.Migrator().HasColumn("usage_logs", "status_code") {
		statusExpression = "COALESCE(l.status_code, 200)"
	}
	errorExpression := "''"
	if s.mainDB.Migrator().HasColumn("usage_logs", "error") {
		errorExpression = "COALESCE(l.error, '')"
	} else if s.mainDB.Migrator().HasColumn("usage_logs", "error_message") {
		errorExpression = "COALESCE(l.error_message, '')"
	}
	var rows []slowRequestRow
	query := fmt.Sprintf(`
		SELECT l.id, l.created_at, l.user_id,
		       COALESCE(NULLIF(u.username, ''), u.email, '') AS user_name,
		       l.account_id, l.model, l.stream, COALESCE(l.duration_ms, 0) AS duration_ms,
		       l.first_token_ms, %s AS status_code, %s AS error
		FROM usage_logs l
		JOIN users u ON u.id = l.user_id
		WHERE u.role <> 'admin' AND l.created_at >= ? AND l.created_at < ?
		  AND (COALESCE(l.first_token_ms, 0) >= 10000 OR COALESCE(l.duration_ms, 0) >= 60000)
		ORDER BY GREATEST(COALESCE(l.first_token_ms, 0), COALESCE(l.duration_ms, 0)) DESC, l.created_at DESC
		LIMIT 30`, statusExpression, errorExpression)
	if err := s.mainDB.WithContext(ctx).Raw(query, start.UTC(), end.UTC()).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]SlowRequest, 0, len(rows))
	for _, row := range rows {
		items = append(items, SlowRequest{
			ID: row.ID, CreatedAt: row.CreatedAt, UserID: row.UserID, UserName: row.UserName,
			AccountID: row.AccountID, Model: row.Model, Stream: row.Stream,
			DurationMS: row.DurationMS, FirstTokenMS: row.FirstTokenMS,
			StatusCode: row.StatusCode, Error: row.Error,
		})
	}
	return items, nil
}
