package storage

import (
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UpstreamSyncTargets struct{ db *gorm.DB }
type UpstreamSyncTargetGroups struct{ db *gorm.DB }
type UpstreamSyncGroups struct{ db *gorm.DB }
type UpstreamSyncAccounts struct{ db *gorm.DB }
type UpstreamSyncManagedAccounts struct{ db *gorm.DB }
type UpstreamSyncLogs struct{ db *gorm.DB }

func NewUpstreamSyncTargets(db *gorm.DB) *UpstreamSyncTargets { return &UpstreamSyncTargets{db: db} }
func NewUpstreamSyncTargetGroups(db *gorm.DB) *UpstreamSyncTargetGroups {
	return &UpstreamSyncTargetGroups{db: db}
}
func NewUpstreamSyncGroups(db *gorm.DB) *UpstreamSyncGroups { return &UpstreamSyncGroups{db: db} }
func NewUpstreamSyncAccounts(db *gorm.DB) *UpstreamSyncAccounts {
	return &UpstreamSyncAccounts{db: db}
}
func NewUpstreamSyncManagedAccounts(db *gorm.DB) *UpstreamSyncManagedAccounts {
	return &UpstreamSyncManagedAccounts{db: db}
}
func NewUpstreamSyncLogs(db *gorm.DB) *UpstreamSyncLogs { return &UpstreamSyncLogs{db: db} }

func (r *UpstreamSyncTargets) List() ([]UpstreamSyncTarget, error) {
	var list []UpstreamSyncTarget
	if err := r.db.Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *UpstreamSyncTargets) FindByID(id uint) (*UpstreamSyncTarget, error) {
	var item UpstreamSyncTarget
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *UpstreamSyncTargets) Create(item *UpstreamSyncTarget) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		shadow := &Connection{
			ID:          item.ID,
			Name:        item.Name,
			BaseURL:     item.BaseURL,
			AdminAPIKey: "",
			Enabled:     item.Enabled,
			SyncMode:    ConnectionSyncModeCanonicalTarget,
			LastCheckAt: item.LastCheckAt,
			CreatedAt:   item.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
		}
		return tx.Create(shadow).Error
	})
}

func (r *UpstreamSyncTargets) Update(item *UpstreamSyncTarget) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(item).Error; err != nil {
			return err
		}
		return tx.Model(&Connection{}).
			Where("id = ? AND sync_mode = ?", item.ID, ConnectionSyncModeCanonicalTarget).
			Updates(map[string]any{
				"name":          item.Name,
				"base_url":      item.BaseURL,
				"enabled":       item.Enabled,
				"last_check_at": item.LastCheckAt,
				"updated_at":    item.UpdatedAt,
			}).Error
	})
}

func (r *UpstreamSyncTargets) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var syncGroupIDs []uint
		if err := tx.Model(&UpstreamSyncGroup{}).Where("target_id = ?", id).Pluck("id", &syncGroupIDs).Error; err != nil {
			return err
		}
		if len(syncGroupIDs) > 0 {
			if err := tx.Where("sync_group_id IN ?", syncGroupIDs).Delete(&UpstreamSyncAccount{}).Error; err != nil {
				return err
			}
			if err := tx.Where("sync_group_id IN ?", syncGroupIDs).Delete(&UpstreamSyncManagedAccount{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("target_id = ?", id).Delete(&UpstreamSyncTargetGroup{}).Error; err != nil {
			return err
		}
		if err := tx.Where("target_id = ?", id).Delete(&UpstreamSyncLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("target_id = ?", id).Delete(&UpstreamSyncGroup{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&UpstreamSyncTarget{}, id).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND sync_mode = ?", id, ConnectionSyncModeCanonicalTarget).
			Delete(&Connection{}).Error
	})
}

func (r *UpstreamSyncTargets) UpdateCheck(id uint, status string, checkedAt *time.Time, errText string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&UpstreamSyncTarget{}).Where("id = ?", id).Updates(map[string]any{
			"last_check_status": status,
			"last_check_at":     checkedAt,
			"last_check_error":  errText,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&Connection{}).
			Where("id = ? AND sync_mode = ?", id, ConnectionSyncModeCanonicalTarget).
			Updates(map[string]any{
				"last_check_at": checkedAt,
				"updated_at":    time.Now(),
			}).Error
	})
}

func (r *UpstreamSyncTargets) UpdateAdminAPIKey(id uint, cipher string) error {
	return r.db.Model(&UpstreamSyncTarget{}).Where("id = ?", id).Update("admin_api_key_cipher", cipher).Error
}

func (r *UpstreamSyncTargetGroups) ListByTarget(targetID uint, includeMissing bool) ([]UpstreamSyncTargetGroup, error) {
	q := r.db.Where("target_id = ?", targetID)
	if !includeMissing {
		q = q.Where("status = ? OR status = ?", "", "active")
	}
	var list []UpstreamSyncTargetGroup
	if err := q.Order("sort ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *UpstreamSyncTargetGroups) Upsert(item *UpstreamSyncTargetGroup) error {
	now := time.Now()
	if item.LastSyncAt == nil {
		item.LastSyncAt = &now
	}
	item.UpdatedAt = now
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "target_id"}, {Name: "remote_group_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name",
			"platform",
			"ratio",
			"status",
			"sort",
			"description",
			"last_sync_at",
			"updated_at",
		}),
	}).Create(item).Error
}

func (r *UpstreamSyncTargetGroups) DeleteMissing(targetID uint, remoteIDs []int64) error {
	if len(remoteIDs) == 0 {
		return r.db.
			Where("target_id = ?", targetID).
			Delete(&UpstreamSyncTargetGroup{}).Error
	}
	return r.db.
		Where("target_id = ? AND remote_group_id NOT IN ?", targetID, remoteIDs).
		Delete(&UpstreamSyncTargetGroup{}).Error
}

func (r *UpstreamSyncTargetGroups) FindByID(id uint) (*UpstreamSyncTargetGroup, error) {
	var item UpstreamSyncTargetGroup
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *UpstreamSyncTargetGroups) FindByTargetAndRemote(targetID uint, remoteGroupID int64) (*UpstreamSyncTargetGroup, error) {
	var item UpstreamSyncTargetGroup
	if err := r.db.First(&item, "target_id = ? AND remote_group_id = ?", targetID, remoteGroupID).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *UpstreamSyncGroups) List() ([]UpstreamSyncGroup, error) {
	var list []UpstreamSyncGroup
	if err := r.db.Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *UpstreamSyncGroups) FindByID(id uint) (*UpstreamSyncGroup, error) {
	var item UpstreamSyncGroup
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *UpstreamSyncGroups) FindByName(name string) (*UpstreamSyncGroup, error) {
	var item UpstreamSyncGroup
	if err := r.db.First(&item, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *UpstreamSyncGroups) Create(item *UpstreamSyncGroup) error { return r.db.Create(item).Error }
func (r *UpstreamSyncGroups) Update(item *UpstreamSyncGroup) error { return r.db.Save(item).Error }
func (r *UpstreamSyncGroups) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("sync_group_id = ?", id).Delete(&UpstreamSyncManagedAccount{}).Error; err != nil {
			return err
		}
		if err := tx.Where("sync_group_id = ?", id).Delete(&UpstreamSyncAccount{}).Error; err != nil {
			return err
		}
		return tx.Delete(&UpstreamSyncGroup{}, id).Error
	})
}

func (r *UpstreamSyncGroups) UpdateStatus(id uint, status, errText string, at *time.Time) error {
	return r.db.Model(&UpstreamSyncGroup{}).Where("id = ?", id).Updates(map[string]any{
		"apply_status":    status,
		"apply_error":     errText,
		"last_applied_at": at,
	}).Error
}

func (r *UpstreamSyncGroups) ParseTargetGroupIDs(syncGroup *UpstreamSyncGroup) ([]uint, error) {
	return parseUintArray(syncGroup.TargetGroupIDsJSON)
}

func (r *UpstreamSyncGroups) SetTargetGroupIDs(id uint, ids []uint) error {
	return r.db.Model(&UpstreamSyncGroup{}).Where("id = ?", id).Update("target_group_ids_json", mustUintArray(ids)).Error
}

func (r *UpstreamSyncGroups) SetTargetID(id uint, targetID uint) error {
	return r.db.Model(&UpstreamSyncGroup{}).Where("id = ?", id).Update("target_id", targetID).Error
}

func (r *UpstreamSyncGroups) SetNameFields(id uint, nameTemplate, name string) error {
	return r.db.Model(&UpstreamSyncGroup{}).Where("id = ?", id).Updates(map[string]any{
		"name_template": nameTemplate,
		"name":          name,
	}).Error
}

func (r *UpstreamSyncGroups) SetMutableFields(id uint, fields map[string]any) error {
	return r.db.Model(&UpstreamSyncGroup{}).Where("id = ?", id).Updates(fields).Error
}

func (r *UpstreamSyncAccounts) ListBySyncGroupID(syncGroupID uint) ([]UpstreamSyncAccount, error) {
	var list []UpstreamSyncAccount
	if err := r.db.Where("sync_group_id = ?", syncGroupID).Order("position ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *UpstreamSyncAccounts) SaveForGroup(syncGroupID uint, list []UpstreamSyncAccount) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		keep := make([]uint, 0, len(list))
		for i := range list {
			list[i].SyncGroupID = syncGroupID
			list[i].Position = i
			if list[i].Concurrency <= 0 {
				list[i].Concurrency = 10
			}
			if list[i].Weight <= 0 {
				list[i].Weight = 1
			}
			if strings.TrimSpace(list[i].RateConvertMode) == "" {
				list[i].RateConvertMode = "raw"
			}
			if strings.TrimSpace(list[i].RateConvertMode) != "custom" && list[i].RateConvertValue == 0 {
				list[i].RateConvertValue = 1
			}
			list[i].SourceGroupName = strings.TrimSpace(list[i].SourceGroupName)
			list[i].TestModel = strings.TrimSpace(list[i].TestModel)
			if list[i].ID == 0 {
				if err := tx.Create(&list[i]).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Model(&UpstreamSyncAccount{}).Where("id = ?", list[i].ID).Updates(map[string]any{
					"sync_group_id":      list[i].SyncGroupID,
					"position":           list[i].Position,
					"source_channel_id":  list[i].SourceChannelID,
					"source_group_id":    list[i].SourceGroupID,
					"source_group_name":  list[i].SourceGroupName,
					"proxy_id":           list[i].ProxyID,
					"concurrency":        list[i].Concurrency,
					"weight":             list[i].Weight,
					"rate_convert_mode":  list[i].RateConvertMode,
					"rate_convert_value": list[i].RateConvertValue,
					"enabled":            list[i].Enabled,
					"test_enabled":       list[i].TestEnabled,
					"test_model":         list[i].TestModel,
				}).Error; err != nil {
					return err
				}
			}
			keep = append(keep, list[i].ID)
		}
		q := tx.Where("sync_group_id = ?", syncGroupID)
		if len(keep) > 0 {
			q = q.Where("id NOT IN ?", keep)
		}
		if err := q.Delete(&UpstreamSyncAccount{}).Error; err != nil {
			return err
		}
		return nil
	})
}

func (r *UpstreamSyncAccounts) DeleteBySyncGroupID(syncGroupID uint) error {
	return r.db.Where("sync_group_id = ?", syncGroupID).Delete(&UpstreamSyncAccount{}).Error
}

func (r *UpstreamSyncManagedAccounts) FindByAccountID(accountID uint) (*UpstreamSyncManagedAccount, error) {
	var item UpstreamSyncManagedAccount
	if err := r.db.First(&item, "sync_account_id = ?", accountID).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *UpstreamSyncManagedAccounts) Upsert(item *UpstreamSyncManagedAccount) error {
	item.UpdatedAt = time.Now()
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "sync_account_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"sync_group_id",
			"source_api_key_id",
			"source_api_key_name",
			"target_account_id",
			"target_account_name",
			"target_group_ids_json",
			"last_applied_at",
			"updated_at",
		}),
	}).Create(item).Error
}

func (r *UpstreamSyncManagedAccounts) DeleteBySyncGroupID(syncGroupID uint) error {
	return r.db.Where("sync_group_id = ?", syncGroupID).Delete(&UpstreamSyncManagedAccount{}).Error
}

func (r *UpstreamSyncManagedAccounts) Delete(id uint) error {
	return r.db.Delete(&UpstreamSyncManagedAccount{}, id).Error
}

func (r *UpstreamSyncManagedAccounts) ListBySyncGroupID(syncGroupID uint) ([]UpstreamSyncManagedAccount, error) {
	var list []UpstreamSyncManagedAccount
	if err := r.db.Where("sync_group_id = ?", syncGroupID).Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *UpstreamSyncManagedAccounts) List() ([]UpstreamSyncManagedAccount, error) {
	var list []UpstreamSyncManagedAccount
	if err := r.db.Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *UpstreamSyncLogs) Append(item *UpstreamSyncLog) error {
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	return r.db.Create(item).Error
}

func (r *UpstreamSyncLogs) DeleteBefore(cutoff time.Time) (int64, error) {
	res := r.db.Where("created_at < ?", cutoff).Delete(&UpstreamSyncLog{})
	return res.RowsAffected, res.Error
}

func (r *UpstreamSyncLogs) ListBySyncGroupID(syncGroupID uint, limit int) ([]UpstreamSyncLog, error) {
	if limit <= 0 {
		limit = 50
	}
	var list []UpstreamSyncLog
	if err := r.db.Where("sync_group_id = ?", syncGroupID).Order("created_at DESC").Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *UpstreamSyncLogs) ListPage(page, pageSize int) ([]UpstreamSyncLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	var total int64
	if err := r.db.Model(&UpstreamSyncLog{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []UpstreamSyncLog
	if err := r.db.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *UpstreamSyncLogs) ListPageBySyncGroupID(syncGroupID uint, page, pageSize int) ([]UpstreamSyncLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	q := r.db.Model(&UpstreamSyncLog{}).Where("sync_group_id = ?", syncGroupID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []UpstreamSyncLog
	if err := q.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func parseUintArray(raw string) ([]uint, error) {
	if strings.TrimSpace(raw) == "" {
		return []uint{}, nil
	}
	var list []uint
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	return list, nil
}

func mustUintArray(list []uint) string {
	if len(list) == 0 {
		return "[]"
	}
	body, _ := json.Marshal(list)
	return string(body)
}
