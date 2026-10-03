package gateway

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/storage"
)

func TestProxyURLForTarget_Provider(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.UpdateProxyConfig(config.ProxyConfig{
		Enabled:  true,
		Protocol: "http",
		Host:     "127.0.0.1",
		Port:     7890,
	})

	if got := svc.proxyURLForTarget(nil, nil); got != "" {
		t.Fatalf("no source should skip proxy, got %q", got)
	}
	if got := svc.proxyURLForTarget(nil, &storage.GatewayProvider{ProxyEnabled: false}); got != "" {
		t.Fatalf("provider proxy off: got %q", got)
	}
	got := svc.proxyURLForTarget(nil, &storage.GatewayProvider{ProxyEnabled: true})
	if got == "" {
		t.Fatal("provider proxy on: empty url")
	}
	u, err := url.Parse(got)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Port() != "7890" {
		t.Fatalf("proxy url = %q err=%v", got, err)
	}

	// 监控渠道仍可用
	chGot := svc.proxyURLForChannel(&storage.Channel{ProxyEnabled: true})
	if chGot == "" {
		t.Fatal("channel proxy on: empty")
	}

	// 全局关
	svc.UpdateProxyConfig(config.ProxyConfig{Enabled: false, Protocol: "http", Host: "127.0.0.1", Port: 7890})
	if got := svc.proxyURLForTarget(nil, &storage.GatewayProvider{ProxyEnabled: true}); got != "" {
		t.Fatalf("global off should skip, got %q", got)
	}
}

func TestHTTPClientForTarget_UsesProviderProxy(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.UpdateProxyConfig(config.ProxyConfig{
		Enabled:  true,
		Protocol: "http",
		Host:     "10.0.0.2",
		Port:     1080,
	})
	client := svc.httpClientForTarget(nil, &storage.GatewayProvider{ProxyEnabled: true})
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("expected transport with proxy func")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	proxyURL, err := tr.Proxy(req)
	if err != nil || proxyURL == nil || proxyURL.Hostname() != "10.0.0.2" {
		t.Fatalf("proxy = %v err=%v", proxyURL, err)
	}
}

func TestGatewayProviderProxyEnabledPersists(t *testing.T) {
	db := openGatewayTestDB(t)
	repo := storage.NewGatewayProviders(db)
	item := &storage.GatewayProvider{
		Name:         "p-proxy",
		BaseURL:      "https://api.example.com",
		APIKeyCipher: "x",
		ProxyEnabled: true,
		Enabled:      true,
	}
	if err := repo.Create(item); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.FindByID(item.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if !got.ProxyEnabled {
		t.Fatal("proxy_enabled not persisted")
	}
}
