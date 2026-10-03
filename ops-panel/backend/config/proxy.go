package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func (p ProxyConfig) URL() (string, error) {
	protocol := strings.ToLower(strings.TrimSpace(p.Protocol))
	if protocol == "" {
		protocol = "http"
	}
	switch protocol {
	case "http", "https", "socks5":
	default:
		return "", fmt.Errorf("unsupported proxy protocol: %s", p.Protocol)
	}

	host := strings.TrimSpace(p.Host)
	if host == "" {
		return "", errors.New("proxy host is required")
	}
	if p.Port <= 0 {
		return "", errors.New("proxy port is required")
	}

	u := url.URL{
		Scheme: protocol,
		Host:   net.JoinHostPort(host, strconv.Itoa(p.Port)),
	}
	username := strings.TrimSpace(p.Username)
	if username != "" || p.Password != "" {
		u.User = url.UserPassword(username, p.Password)
	}
	return u.String(), nil
}

func (p ProxyConfig) ActiveURL() (string, error) {
	if !p.Enabled {
		return "", nil
	}
	return p.URL()
}
