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

// isKnownDeepSeekModel reports whether a DeepSeek model is explicitly covered
// by the billing/access policy. Unknown deepseek-* identifiers must not be
// admitted through fuzzy catalog matching or an empty account mapping.
func isKnownDeepSeekModel(model string) bool {
	m := normalizeDeepSeekModelName(model)
	return isDeepSeekFamilyOrNumericVersion(m, "deepseek-v3.2") ||
		isDeepSeekFamilyOrNumericVersion(m, "deepseek-v4-pro") ||
		isDeepSeekFamilyOrNumericVersion(m, "deepseek-v4-flash") ||
		isDeepSeekFamilyOrNumericVersion(m, "deepseek-v4.1") ||
		isDeepSeekFamilyOrNumericVersion(m, "deepseek-v4.1-flash") ||
		m == "deepseek-v4-flash-vision-exp" ||
		m == "deepseek-chat" ||
		m == "deepseek-reasoner"
}

func isDeepSeekFamilyOrNumericVersion(model, base string) bool {
	if model == base {
		return true
	}
	suffix, ok := strings.CutPrefix(model, base+"-")
	if !ok || suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
