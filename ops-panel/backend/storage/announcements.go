package storage

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UpstreamAnnouncements struct{ db *gorm.DB }

const upstreamAnnouncementOrder = "published_at IS NULL ASC, published_at DESC, first_seen_at DESC, id DESC"

func NewUpstreamAnnouncements(db *gorm.DB) *UpstreamAnnouncements {
	return &UpstreamAnnouncements{db: db}
}

func (r *UpstreamAnnouncements) ListLatest(limit int) ([]UpstreamAnnouncement, error) {
	if limit <= 0 {
		limit = 20
	}
	var list []UpstreamAnnouncement
	if err := r.db.Order(upstreamAnnouncementOrder).Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *UpstreamAnnouncements) ListPage(page, pageSize int) ([]UpstreamAnnouncement, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	var total int64
	if err := r.db.Model(&UpstreamAnnouncement{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []UpstreamAnnouncement
	if err := r.db.Order(upstreamAnnouncementOrder).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *UpstreamAnnouncements) CountByChannel(channelID uint) (int64, error) {
	var n int64
	if err := r.db.Model(&UpstreamAnnouncement{}).Where("channel_id = ?", channelID).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *UpstreamAnnouncements) Sync(channelID uint, list []UpstreamAnnouncement) ([]UpstreamAnnouncement, error) {
	if len(list) == 0 {
		return nil, nil
	}
	now := time.Now()
	newItems := make([]UpstreamAnnouncement, 0, len(list))
	for i := range list {
		item := list[i]
		item.ChannelID = channelID
		if item.FirstSeenAt.IsZero() {
			item.FirstSeenAt = now
		}
		res := r.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "channel_id"}, {Name: "source_key"}},
			DoNothing: true,
		}).Create(&item)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected > 0 {
			newItems = append(newItems, item)
		}
	}
	return newItems, nil
}

func (r *UpstreamAnnouncements) DeleteByChannel(channelID uint) (int64, error) {
	res := r.db.Where("channel_id = ?", channelID).Delete(&UpstreamAnnouncement{})
	return res.RowsAffected, res.Error
}

func (r *UpstreamAnnouncements) DeleteBefore(cutoff time.Time) (int64, error) {
	res := r.db.Where("first_seen_at < ?", cutoff).Delete(&UpstreamAnnouncement{})
	return res.RowsAffected, res.Error
}

func (r *UpstreamAnnouncements) Exists(channelID uint, sourceKey string) (bool, error) {
	var n int64
	if err := r.db.Model(&UpstreamAnnouncement{}).
		Where("channel_id = ? AND source_key = ?", channelID, sourceKey).
		Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}
