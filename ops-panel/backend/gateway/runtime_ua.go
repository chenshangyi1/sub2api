// 数据面：上游 User-Agent 解析与默认值。
package gateway

import (
	"strings"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/storage"
)

func (rt *Runtime) defaultUpstreamUserAgent() string {
	if rt == nil || rt.Service == nil {
		return config.DefaultUpstreamUserAgent
	}
	rt.mu.RLock()
	ua := strings.TrimSpace(rt.upstream.UserAgent)
	rt.mu.RUnlock()
	if ua == "" {
		return config.DefaultUpstreamUserAgent
	}
	return ua
}

// resolveAdminUserAgent 模型测试 / 拉模型：组+路由策略，空则默认 UA。
func (rt *Runtime) resolveAdminUserAgent(group *storage.GatewayGroup, route *storage.GatewayRoute) string {
	return withDefaultUserAgent(ResolveRouteUserAgentFrom(group, route), rt.defaultUpstreamUserAgent())
}

// applyRouteUserAgentForAdmin 模型测试等无客户端路径：空则填默认 UA。
func (rt *Runtime) applyRouteUserAgentForAdmin(target *upstreamTarget, group *storage.GatewayGroup, route *storage.GatewayRoute) {
	if target == nil {
		return
	}
	target.UserAgentOverride = rt.resolveAdminUserAgent(group, route)
}
