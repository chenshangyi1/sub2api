package storage

import (
	"time"

	"gorm.io/gorm"
)

type Captchas struct{ db *gorm.DB }

func NewCaptchas(db *gorm.DB) *Captchas { return &Captchas{db: db} }

func (r *Captchas) List() ([]CaptchaConfig, error) {
	var list []CaptchaConfig
	if err := r.db.Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Captchas) FindByID(id uint) (*CaptchaConfig, error) {
	var c CaptchaConfig
	if err := r.db.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Captchas) Create(c *CaptchaConfig) error { return r.db.Create(c).Error }
func (r *Captchas) Update(c *CaptchaConfig) error { return r.db.Save(c).Error }
func (r *Captchas) Delete(id uint) error          { return r.db.Delete(&CaptchaConfig{}, id).Error }

func (r *Captchas) UpdateBalance(id uint, balance *float64, unit string, errText string, at time.Time) error {
	return r.db.Model(&CaptchaConfig{}).Where("id = ?", id).Updates(map[string]any{
		"last_balance":  balance,
		"balance_unit":  unit,
		"balance_at":    at,
		"balance_error": errText,
	}).Error
}
