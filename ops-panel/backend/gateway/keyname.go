// 上游 Ensure Key 的稳定命名（slug / 哈希）。
package gateway

import (
	"fmt"
	"strings"
)

func (svc *Service) stableUpstreamKeyName(channelID uint, sourceGroupID *int64, sourceGroupName string) string {
	if sourceGroupID != nil && *sourceGroupID > 0 {
		return fmt.Sprintf("uops-ch%d-sg%d", channelID, *sourceGroupID)
	}
	name := strings.TrimSpace(sourceGroupName)
	if name != "" {
		return fmt.Sprintf("uops-ch%d-sgn-%s", channelID, svc.slugForKeyName(name))
	}
	return fmt.Sprintf("uops-ch%d-default", channelID)
}

// slugForKeyName 把源分组名压成适合作为 Key 名后缀的短串。
func (svc *Service) slugForKeyName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "x"
	}
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '_' || r == '-' || r == '.' || r == ' ':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		default:
			// 中文等：用固定短哈希，避免上游 Key 名非法字符
			// 整段非 ASCII 时落 hash
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		// 全是非 ASCII：用 FNV 短哈希，保证同名稳定、不同名尽量不撞
		h := svc.fnv32a(s)
		return fmt.Sprintf("h%x", h)
	}
	if len(out) > 48 {
		out = out[:48]
	}
	// 混有非 ASCII 时附加短哈希，降低「不同中文名压成相同 slug」的概率
	if svc.containsNonASCII(s) {
		return out + "-" + fmt.Sprintf("%x", svc.fnv32a(s)&0xffff)
	}
	return out
}

func (svc *Service) containsNonASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}

func (svc *Service) fnv32a(s string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime32
	}
	return h
}
