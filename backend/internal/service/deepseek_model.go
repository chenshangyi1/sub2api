package service

import "strings"

// normalizeDeepSeekModelName canonicalizes provider prefixes, case, separators
// and common version spellings while leaving non-DeepSeek IDs unchanged.
func normalizeDeepSeekModelName(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if slash := strings.LastIndexByte(m, '/'); slash >= 0 {
		m = m[slash+1:]
	}
	m = strings.ReplaceAll(m, "_", "-")
	m = strings.ReplaceAll(m, "deepseek--", "deepseek-")
	m = strings.ReplaceAll(m, "v3-2", "v3.2")
	if m == "deepseek-v4pro" {
		m = "deepseek-v4-pro"
	}
	if m == "deepseek-v4flash" {
		m = "deepseek-v4-flash"
	}
	if m == "deepseek-v41flash" || m == "deepseek-flash" {
		m = "deepseek-v4.1-flash"
	}
	return m
}

func isDeepSeekModelAlias(model string) bool {
	return strings.HasPrefix(normalizeDeepSeekModelName(model), "deepseek-")
}
