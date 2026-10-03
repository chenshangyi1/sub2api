// 指针构造与小型无业务 helper。
package gateway

import (
	"strings"

	"github.com/bejix/upstream-ops/backend/connector"
)

func (svc *Service) findAPIKeyByName(items []connector.APIKey, name string) *connector.APIKey {
	for i := range items {
		if strings.TrimSpace(items[i].Name) == name {
			return &items[i]
		}
	}
	return nil
}

func (svc *Service) findAPIKeyByID(items []connector.APIKey, id int64) *connector.APIKey {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func boolPtr(v bool) *bool { return &v }

func int64PtrIf(cond bool, v int64) *int64 {
	if !cond {
		return nil
	}
	return &v
}

func stringPtrOrNil(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func clipString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
