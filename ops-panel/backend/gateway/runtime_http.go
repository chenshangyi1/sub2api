// 数据面：代理、超时与按目标构建 HTTP Client。
package gateway

import (
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/storage"
)

func (rt *Runtime) gatewayRuntime() config.GatewayConfig {
	rt.mu.RLock()
	cfg := rt.gatewayCfg
	rt.mu.RUnlock()
	return cfg.WithDefaults()
}

func (rt *Runtime) proxyURLForChannel(ch *storage.Channel) string {
	return rt.proxyURLForTarget(ch, nil)
}

// proxyURLForTarget 监控渠道或直连 Provider 任一方开启 proxy_enabled，且全局代理启用时返回代理 URL。

func (rt *Runtime) proxyURLForTarget(ch *storage.Channel, provider *storage.GatewayProvider) string {
	rt.mu.RLock()
	pc := rt.proxyConfig
	rt.mu.RUnlock()
	if !pc.Enabled {
		return ""
	}
	useProxy := false
	if ch != nil && ch.ProxyEnabled {
		useProxy = true
	}
	if provider != nil && provider.ProxyEnabled {
		useProxy = true
	}
	if !useProxy {
		return ""
	}
	u, err := pc.ActiveURL()
	if err != nil {
		return ""
	}
	return u
}

func (rt *Runtime) httpClientForChannel(ch *storage.Channel) *http.Client {
	return rt.httpClientForTarget(ch, nil)
}

func (rt *Runtime) httpClientForTarget(ch *storage.Channel, provider *storage.GatewayProvider) *http.Client {
	// 网关转发超时由 gateway.forwardTimeoutSeconds 控制（与监控上游 timeout 分离）。
	timeout := rt.gatewayRuntime().ForwardTimeout()

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	if proxy := rt.proxyURLForTarget(ch, provider); proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

// ---------- key helpers ----------
