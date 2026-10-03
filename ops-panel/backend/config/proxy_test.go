package config

import "testing"

func TestProxyConfigURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  ProxyConfig
		want string
	}{
		{
			name: "http",
			cfg:  ProxyConfig{Protocol: "http", Host: "127.0.0.1", Port: 8080},
			want: "http://127.0.0.1:8080",
		},
		{
			name: "https",
			cfg:  ProxyConfig{Protocol: "https", Host: "proxy.example.com", Port: 443},
			want: "https://proxy.example.com:443",
		},
		{
			name: "socks5 with auth",
			cfg:  ProxyConfig{Protocol: "socks5", Host: "proxy.example.com", Port: 1080, Username: "u@x", Password: "p:/?"},
			want: "socks5://u%40x:p%3A%2F%3F@proxy.example.com:1080",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.URL()
			if err != nil {
				t.Fatalf("URL: %v", err)
			}
			if got != tt.want {
				t.Fatalf("url = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProxyConfigURLRejectsIncompleteConfig(t *testing.T) {
	for _, cfg := range []ProxyConfig{
		{Protocol: "ftp", Host: "127.0.0.1", Port: 8080},
		{Protocol: "http", Port: 8080},
		{Protocol: "http", Host: "127.0.0.1"},
	} {
		if _, err := cfg.URL(); err == nil {
			t.Fatalf("URL(%#v) error is nil", cfg)
		}
	}
}

func TestProxyConfigActiveURLRequiresEnabled(t *testing.T) {
	cfg := ProxyConfig{Protocol: "http", Host: "127.0.0.1", Port: 8080}
	got, err := cfg.ActiveURL()
	if err != nil {
		t.Fatalf("ActiveURL disabled: %v", err)
	}
	if got != "" {
		t.Fatalf("disabled active url = %q, want empty", got)
	}
	cfg.Enabled = true
	got, err = cfg.ActiveURL()
	if err != nil {
		t.Fatalf("ActiveURL enabled: %v", err)
	}
	if got != "http://127.0.0.1:8080" {
		t.Fatalf("enabled active url = %q", got)
	}
}
