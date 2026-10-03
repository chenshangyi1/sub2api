// 管理面：网关密钥 CRUD 与明文揭示。
package gateway

import (
	"errors"
	"strings"

	"github.com/bejix/upstream-ops/backend/storage"
)

// CreateKey 创建密钥。
func (a *AdminService) CreateKey(in CreateKeyInput) (*CreateKeyResult, error) {
	if in.GroupID == 0 {
		return nil, errors.New("group_id is required")
	}
	if _, err := a.Groups.FindByID(in.GroupID); err != nil {
		return nil, errors.New("group not found")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	if _, err := a.Keys.FindByName(name); err == nil {
		return nil, errors.New("name already exists")
	}
	secret := strings.TrimSpace(in.CustomKey)
	if secret == "" {
		var err error
		secret, err = GenerateAPIKey(in.KeyLen)
		if err != nil {
			return nil, err
		}
	}
	cipherText, err := a.Cipher.Encrypt(secret)
	if err != nil {
		return nil, err
	}
	item := &storage.GatewayKey{
		GroupID:         in.GroupID,
		Name:            name,
		KeyHash:         HashAPIKey(secret),
		KeyPrefix:       KeyPrefix(secret),
		KeyCipher:       cipherText,
		Status:          storage.GatewayKeyStatusActive,
		Quota:           in.Quota,
		IPWhitelistJSON: strings.TrimSpace(in.IPWhitelistJSON),
		IPBlacklistJSON: strings.TrimSpace(in.IPBlacklistJSON),
	}
	if err := a.Keys.Create(item); err != nil {
		return nil, err
	}
	return &CreateKeyResult{Key: *item, Secret: secret}, nil
}

// UpdateKey 更新密钥。
func (a *AdminService) UpdateKey(id uint, in UpdateKeyInput) (*storage.GatewayKey, error) {
	item, err := a.Keys.FindByID(id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, errors.New("name is required")
		}
		if other, err := a.Keys.FindByName(name); err == nil && other.ID != id {
			return nil, errors.New("name already exists")
		}
		item.Name = name
	}
	if in.Status != nil {
		st := strings.TrimSpace(*in.Status)
		if st != storage.GatewayKeyStatusActive && st != storage.GatewayKeyStatusDisabled {
			return nil, errors.New("invalid status")
		}
		item.Status = st
	}
	if in.Quota != nil {
		item.Quota = *in.Quota
	}
	if in.IPWhitelistJSON != nil {
		item.IPWhitelistJSON = strings.TrimSpace(*in.IPWhitelistJSON)
	}
	if in.IPBlacklistJSON != nil {
		item.IPBlacklistJSON = strings.TrimSpace(*in.IPBlacklistJSON)
	}
	if in.ResetQuotaUsed != nil && *in.ResetQuotaUsed {
		item.QuotaUsed = 0
	}
	if err := a.Keys.Update(item); err != nil {
		return nil, err
	}
	return item, nil
}

// DeleteKey 删除密钥。
func (a *AdminService) DeleteKey(id uint) error {
	return a.Keys.Delete(id)
}

// RevealKey 返回密钥明文。
func (a *AdminService) RevealKey(id uint) (string, error) {
	item, err := a.Keys.FindByID(id)
	if err != nil {
		return "", err
	}
	return a.Cipher.Decrypt(item.KeyCipher)
}

// ListKeysByGroup 按组列密钥。
func (a *AdminService) ListKeysByGroup(groupID uint) ([]storage.GatewayKey, error) {
	return a.Keys.ListByGroupID(groupID)
}

// ---------- admin: providers（直连渠道） ----------
