// 上游转发目标（baseURL / key / channel / provider）。
package gateway

import "github.com/bejix/upstream-ops/backend/storage"

type upstreamTarget struct {
	BaseURL  string
	APIKey   string
	Channel  *storage.Channel
	Provider *storage.GatewayProvider
	// UserAgentOverride 非空时覆盖发往上游的 User-Agent（组+路由策略解析结果）。
	UserAgentOverride string
}
