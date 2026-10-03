package storage

import (
	"time"

	"gorm.io/gorm"
)

type Rates struct{ db *gorm.DB }

func NewRates(db *gorm.DB) *Rates { return &Rates{db: db} }

var trendNow = time.Now
var trendLocation = loadTrendLocation()

func loadTrendLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return loc
}

// ListByChannel 返回渠道当前所有倍率快照。
func (r *Rates) ListByChannel(channelID uint) ([]RateSnapshot, error) {
	var list []RateSnapshot
	if err := r.db.Where("channel_id = ?", channelID).Order("model_name ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// Upsert 更新或插入倍率快照，返回此前的记录（若有），调用方据此判断是否变化。
func (r *Rates) Upsert(snapshot *RateSnapshot) (*RateSnapshot, error) {
	var prev RateSnapshot
	err := r.db.
		Where("channel_id = ? AND model_name = ?", snapshot.ChannelID, snapshot.ModelName).
		First(&prev).Error
	switch {
	case err == nil:
		old := prev
		prev.Ratio = snapshot.Ratio
		prev.CompletionRatio = snapshot.CompletionRatio
		prev.RemoteGroupID = snapshot.RemoteGroupID
		prev.Description = snapshot.Description
		prev.LastSeenAt = snapshot.LastSeenAt
		if err := r.db.Save(&prev).Error; err != nil {
			return nil, err
		}
		return &old, nil
	case err == gorm.ErrRecordNotFound:
		snapshot.FirstSeenAt = snapshot.LastSeenAt
		if err := r.db.Create(snapshot).Error; err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, err
	}
}

func (r *Rates) AppendChange(log *RateChangeLog) error {
	if log.ChangedAt.IsZero() {
		log.ChangedAt = time.Now()
	}
	return r.db.Create(log).Error
}

func (r *Rates) DeleteSnapshot(channelID uint, modelName string) error {
	return r.db.Where("channel_id = ? AND model_name = ?", channelID, modelName).Delete(&RateSnapshot{}).Error
}

// ListChanges 倒序拉取倍率变化日志。channelID 为 0 表示不过滤。
func (r *Rates) ListChanges(channelID uint, limit int) ([]RateChangeLog, error) {
	if limit <= 0 {
		limit = 50
	}
	q := r.db.Model(&RateChangeLog{}).Order("changed_at DESC").Limit(limit)
	if channelID != 0 {
		q = q.Where("channel_id = ?", channelID)
	}
	var list []RateChangeLog
	if err := q.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Rates) ListChangesPage(channelID uint, page, pageSize int) ([]RateChangeLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	q := r.db.Model(&RateChangeLog{})
	if channelID != 0 {
		q = q.Where("channel_id = ?", channelID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []RateChangeLog
	if err := q.Order("changed_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *Rates) AppendBalance(s *BalanceSnapshot) error {
	if s.SampledAt.IsZero() {
		s.SampledAt = time.Now()
	}
	return r.db.Create(s).Error
}

func (r *Rates) AppendCost(s *CostSnapshot) error {
	if s.SampledAt.IsZero() {
		s.SampledAt = time.Now()
	}
	return r.db.Create(s).Error
}

// DeleteBalanceSnapshotsBefore 删除 sampled_at < cutoff 的余额快照，返回删除行数。
func (r *Rates) DeleteBalanceSnapshotsBefore(cutoff time.Time) (int64, error) {
	res := r.db.Where("sampled_at < ?", cutoff).Delete(&BalanceSnapshot{})
	return res.RowsAffected, res.Error
}

// DeleteCostSnapshotsBefore 删除 sampled_at < cutoff 的消费快照，返回删除行数。
func (r *Rates) DeleteCostSnapshotsBefore(cutoff time.Time) (int64, error) {
	res := r.db.Where("sampled_at < ?", cutoff).Delete(&CostSnapshot{})
	return res.RowsAffected, res.Error
}

// BalanceHistory 倒序拉取余额历史。
func (r *Rates) BalanceHistory(channelID uint, limit int) ([]BalanceSnapshot, error) {
	if limit <= 0 {
		limit = 100
	}
	var list []BalanceSnapshot
	if err := r.db.
		Where("channel_id = ?", channelID).
		Order("sampled_at DESC").
		Limit(limit).
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// DailyAggregate 一天的聚合余额（所有渠道之和）。
type DailyAggregate struct {
	Day     time.Time `json:"day"`
	Balance float64   `json:"balance"`
}

// DailyCostAggregate 一天的聚合消费（所有渠道之和）。
type DailyCostAggregate struct {
	Day  time.Time `json:"day"`
	Cost float64   `json:"cost"`
}

// AggregateBalanceTrend 取最近 N 天的"日内最后一次余额"按渠道之和，作为总余额趋势。
//
// 实现：对每个 (channel_id, day) 取该天最后一次 BalanceSnapshot 的余额，再按 day 求和，
// 然后补齐窗口内缺失的日期。窗口内完全没有采样时返回空数组。
func (r *Rates) AggregateBalanceTrend(days int) ([]DailyAggregate, error) {
	if days <= 0 {
		days = 7
	}
	today := dayStart(trendNow())
	since := today.AddDate(0, 0, -(days - 1))

	var snapshots []BalanceSnapshot
	if err := r.db.
		Where("sampled_at >= ?", since).
		Order("sampled_at ASC").
		Find(&snapshots).Error; err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return []DailyAggregate{}, nil
	}

	type key struct {
		ChannelID uint
		Day       time.Time
	}

	latest := make(map[key]BalanceSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		day := dayStart(snapshot.SampledAt)
		latest[key{ChannelID: snapshot.ChannelID, Day: day}] = snapshot
	}

	byDay := make(map[string]float64, days)
	for _, snapshot := range latest {
		day := dayStart(snapshot.SampledAt)
		byDay[dayKey(day)] += snapshot.Balance
	}

	out := make([]DailyAggregate, 0, days)
	for day := since; !day.After(today); day = day.AddDate(0, 0, 1) {
		out = append(out, DailyAggregate{Day: day, Balance: byDay[dayKey(day)]})
	}
	return out, nil
}

// AggregateCostTrend 取最近 N 天的"日内最后一次今日消费"按渠道之和，作为总消费趋势。
func (r *Rates) AggregateCostTrend(days int) ([]DailyCostAggregate, error) {
	if days <= 0 {
		days = 7
	}
	today := dayStart(trendNow())
	since := today.AddDate(0, 0, -(days - 1))

	var snapshots []CostSnapshot
	if err := r.db.
		Where("sampled_at >= ?", since).
		Order("sampled_at ASC").
		Find(&snapshots).Error; err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return []DailyCostAggregate{}, nil
	}

	type key struct {
		ChannelID uint
		Day       time.Time
	}

	latest := make(map[key]CostSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		day := dayStart(snapshot.SampledAt)
		latest[key{ChannelID: snapshot.ChannelID, Day: day}] = snapshot
	}

	byDay := make(map[string]float64, days)
	for _, snapshot := range latest {
		day := dayStart(snapshot.SampledAt)
		byDay[dayKey(day)] += snapshot.TodayCost
	}

	out := make([]DailyCostAggregate, 0, days)
	for day := since; !day.After(today); day = day.AddDate(0, 0, 1) {
		out = append(out, DailyCostAggregate{Day: day, Cost: byDay[dayKey(day)]})
	}
	return out, nil
}

func dayStart(t time.Time) time.Time {
	local := t.In(trendLocation)
	y, m, d := local.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, trendLocation)
}

func dayKey(t time.Time) string {
	return t.Format("2006-01-02")
}
