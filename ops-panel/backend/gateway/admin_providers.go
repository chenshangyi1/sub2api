// 管理面：直连渠道 CRUD。
package gateway

import (
	"errors"
	"strings"

	"github.com/bejix/upstream-ops/backend/storage"
)

// ListProviders 分页列直连渠道。
func (a *AdminService) ListProviders(q storage.GatewayProviderQuery) (*storage.GatewayProviderPage, error) {
	if a.Providers == nil {
		return &storage.GatewayProviderPage{Items: []storage.GatewayProvider{}}, nil
	}
	return a.Providers.List(q)
}

// ListProviderOptions 直连渠道下拉选项。
func (a *AdminService) ListProviderOptions(query string) ([]storage.GatewayProvider, error) {
	if a.Providers == nil {
		return []storage.GatewayProvider{}, nil
	}
	return a.Providers.ListOptions(query)
}

// CreateProvider 创建直连渠道。
func (a *AdminService) CreateProvider(in CreateProviderInput) (*storage.GatewayProvider, error) {
	if a.Providers == nil {
		return nil, errors.New("providers not configured")
	}
	name := strings.TrimSpace(in.Name)
	base := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	key := strings.TrimSpace(in.APIKey)
	if name == "" {
		return nil, errors.New("name is required")
	}
	if base == "" {
		return nil, errors.New("base_url is required")
	}
	if key == "" {
		return nil, errors.New("api_key is required")
	}
	if _, err := a.Providers.FindByName(name); err == nil {
		return nil, errors.New("provider name already exists")
	}
	cipherText, err := a.Cipher.Encrypt(key)
	if err != nil {
		return nil, err
	}
	rate := in.DefaultBillingRate
	if rate <= 0 {
		rate = 1
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	proxyEnabled := false
	if in.ProxyEnabled != nil {
		proxyEnabled = *in.ProxyEnabled
	}
	item := &storage.GatewayProvider{
		Name:               name,
		BaseURL:            base,
		APIKeyCipher:       cipherText,
		APIKeyHint:         a.apiKeyHint(key),
		UpstreamProtocol:   a.normalizeProviderProtocol(in.UpstreamProtocol),
		DefaultBillingRate: rate,
		AuthStyle:          a.normalizeProviderAuthStyle(in.AuthStyle),
		Enabled:            enabled,
		ProxyEnabled:       proxyEnabled,
		ExtraHeadersJSON:   strings.TrimSpace(in.ExtraHeadersJSON),
		Notes:              strings.TrimSpace(in.Notes),
	}
	if err := a.Providers.Create(item); err != nil {
		return nil, err
	}
	return item, nil
}

// UpdateProvider 更新直连渠道。
func (a *AdminService) UpdateProvider(id uint, in UpdateProviderInput) (*storage.GatewayProvider, error) {
	if a.Providers == nil {
		return nil, errors.New("providers not configured")
	}
	item, err := a.Providers.FindByID(id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, errors.New("name is required")
		}
		if name != item.Name {
			if _, err := a.Providers.FindByName(name); err == nil {
				return nil, errors.New("provider name already exists")
			}
		}
		item.Name = name
	}
	if in.BaseURL != nil {
		base := strings.TrimRight(strings.TrimSpace(*in.BaseURL), "/")
		if base == "" {
			return nil, errors.New("base_url is required")
		}
		item.BaseURL = base
	}
	if in.APIKey != nil {
		key := strings.TrimSpace(*in.APIKey)
		if key != "" {
			cipherText, err := a.Cipher.Encrypt(key)
			if err != nil {
				return nil, err
			}
			item.APIKeyCipher = cipherText
			item.APIKeyHint = a.apiKeyHint(key)
		}
	}
	if in.UpstreamProtocol != nil {
		item.UpstreamProtocol = a.normalizeProviderProtocol(*in.UpstreamProtocol)
	}
	if in.DefaultBillingRate != nil {
		rate := *in.DefaultBillingRate
		if rate <= 0 {
			rate = 1
		}
		item.DefaultBillingRate = rate
	}
	if in.AuthStyle != nil {
		item.AuthStyle = a.normalizeProviderAuthStyle(*in.AuthStyle)
	}
	if in.Enabled != nil {
		item.Enabled = *in.Enabled
	}
	if in.ProxyEnabled != nil {
		item.ProxyEnabled = *in.ProxyEnabled
	}
	if in.ExtraHeadersJSON != nil {
		item.ExtraHeadersJSON = strings.TrimSpace(*in.ExtraHeadersJSON)
	}
	if in.Notes != nil {
		item.Notes = strings.TrimSpace(*in.Notes)
	}
	if err := a.Providers.Update(item); err != nil {
		return nil, err
	}
	return item, nil
}

// DeleteProvider 删除直连渠道。
func (a *AdminService) DeleteProvider(id uint) error {
	if a.Providers == nil {
		return errors.New("providers not configured")
	}
	return a.Providers.Delete(id)
}

// RevealProviderKey 返回直连渠道密钥明文。
func (a *AdminService) RevealProviderKey(id uint) (string, error) {
	if a.Providers == nil {
		return "", errors.New("providers not configured")
	}
	item, err := a.Providers.FindByID(id)
	if err != nil {
		return "", err
	}
	return a.Cipher.Decrypt(item.APIKeyCipher)
}

// ---------- admin: routes ----------
