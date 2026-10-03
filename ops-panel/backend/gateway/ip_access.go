// 密钥 IP 黑白名单校验。
package gateway

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
)

func (svc *Service) checkIPAccess(ip, whitelistJSON, blacklistJSON string) error {
	ip = strings.TrimSpace(ip)
	if bl := svc.parseStringListJSON(blacklistJSON); len(bl) > 0 {
		if svc.matchIPList(ip, bl) {
			return errors.New("ip blocked")
		}
	}
	if wl := svc.parseStringListJSON(whitelistJSON); len(wl) > 0 {
		if !svc.matchIPList(ip, wl) {
			return errors.New("ip not allowed")
		}
	}
	return nil
}

func (svc *Service) parseStringListJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (svc *Service) matchIPList(ip string, list []string) bool {
	parsed := net.ParseIP(ip)
	for _, item := range list {
		if item == ip {
			return true
		}
		if strings.Contains(item, "/") {
			if _, network, err := net.ParseCIDR(item); err == nil && parsed != nil && network.Contains(parsed) {
				return true
			}
		}
	}
	return false
}
