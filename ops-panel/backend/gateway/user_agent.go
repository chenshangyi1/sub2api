// 上游 User-Agent 策略解析（纯函数 + Runtime 写入 target）。
package gateway

import (
	"strings"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/storage"
)

// NormalizeUserAgentMode 归一化路由 UA 策略；非法/空 → passthrough。
func NormalizeUserAgentMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case storage.GatewayUserAgentModeGroup:
		return storage.GatewayUserAgentModeGroup
	case storage.GatewayUserAgentModeCustom:
		return storage.GatewayUserAgentModeCustom
	default:
		return storage.GatewayUserAgentModePassthrough
	}
}

// ResolveRouteUserAgent 按路由策略解析最终发往上游的 User-Agent 重写值。
// 返回空串表示不重写（透传客户端）。
//
//	passthrough — 始终不重写
//	group       — 使用组 UA；组为空则不重写
//	custom      — 使用路由自定义；自定义为空则不重写
//
// 注意：客户端转发路径空串 = 透传客户端 UA；
// 模型测试 / 拉模型列表无客户端时，应再经 withDefaultUserAgent 填默认 UA。
func ResolveRouteUserAgent(mode, custom, groupUA string) string {
	switch NormalizeUserAgentMode(mode) {
	case storage.GatewayUserAgentModeGroup:
		return strings.TrimSpace(groupUA)
	case storage.GatewayUserAgentModeCustom:
		return strings.TrimSpace(custom)
	default:
		return ""
	}
}

// ResolveRouteUserAgentFrom 从组 + 路由结构体解析 UA 重写值。
func ResolveRouteUserAgentFrom(group *storage.GatewayGroup, route *storage.GatewayRoute) string {
	mode, custom, groupUA := "", "", ""
	if route != nil {
		mode = route.UserAgentMode
		custom = route.UserAgentCustom
	}
	if group != nil {
		groupUA = group.UserAgent
	}
	return ResolveRouteUserAgent(mode, custom, groupUA)
}

// withDefaultUserAgent 解析结果为空时使用默认 UA（设置页 upstream.userAgent / 内置默认）。
// 用于无客户端上下文的路径：模型测试、拉取模型列表。
func withDefaultUserAgent(resolved, defaultUA string) string {
	if ua := strings.TrimSpace(resolved); ua != "" {
		return ua
	}
	if d := strings.TrimSpace(defaultUA); d != "" {
		return d
	}
	return config.DefaultUpstreamUserAgent
}

// applyRouteUserAgent 把解析后的 UA 写入 upstreamTarget（转发：空=透传客户端）。
func (rt *Runtime) applyRouteUserAgent(target *upstreamTarget, group *storage.GatewayGroup, route *storage.GatewayRoute) {
	if target == nil {
		return
	}
	target.UserAgentOverride = ResolveRouteUserAgentFrom(group, route)
}
