package service

import "strings"

// ProxyModeExtraKey stores the account proxy selection mode in the existing
// JSONB extra column. Keeping this in extra preserves compatibility with
// existing installations without requiring an account table migration.
const ProxyModeExtraKey = "proxy_mode"

const ProxyModeRandom = "random"

// NormalizeProxyModeExtra enforces the proxy_mode whitelist on an account
// extra map: "random" (case/whitespace-insensitive) is canonicalized to
// ProxyModeRandom, any other value is dropped so it can never persist.
func NormalizeProxyModeExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	raw, ok := extra[ProxyModeExtraKey]
	if !ok {
		return extra
	}
	if mode, ok := raw.(string); ok && strings.EqualFold(strings.TrimSpace(mode), ProxyModeRandom) {
		extra[ProxyModeExtraKey] = ProxyModeRandom
		return extra
	}
	delete(extra, ProxyModeExtraKey)
	return extra
}

// IsRandomProxy reports whether the account should use a randomly selected
// active proxy for each newly selected request. The value is deliberately
// strict: malformed user data never enables an unexpected mode.
func (a *Account) IsRandomProxy() bool {
	if a == nil || a.Extra == nil {
		return false
	}
	mode, ok := a.Extra[ProxyModeExtraKey].(string)
	return ok && strings.EqualFold(strings.TrimSpace(mode), ProxyModeRandom)
}
