package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

type proxyTestResult struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	IP        string `json:"ip"`
	Provider  string `json:"provider"`
	Error     string `json:"error,omitempty"`
}

type proxyIPProvider struct {
	name    string
	url     string
	parseIP func([]byte) string
	lastErr error
}

var proxyIPProviders = []proxyIPProvider{
	{
		name: "ipify",
		url:  "https://api.ipify.org?format=json",
		parseIP: func(body []byte) string {
			var out struct {
				IP string `json:"ip"`
			}
			_ = json.Unmarshal(body, &out)
			return out.IP
		},
	},
	{
		name: "ipinfo",
		url:  "https://ipinfo.io/json",
		parseIP: func(body []byte) string {
			var out struct {
				IP string `json:"ip"`
			}
			_ = json.Unmarshal(body, &out)
			return out.IP
		},
	},
}

func testProxy(c *gin.Context) {
	var in config.ProxyConfig
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	proxyURL, err := in.URL()
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}

	providers := make([]proxyIPProvider, len(proxyIPProviders))
	copy(providers, proxyIPProviders)

	client := resty.New().
		SetTimeout(15*time.Second).
		SetHeader("Accept", "application/json").
		SetProxy(proxyURL)

	for i := range providers {
		provider := providers[i]
		started := time.Now()
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		resp, err := client.R().SetContext(ctx).Get(provider.url)
		cancel()
		latency := time.Since(started).Milliseconds()
		if err != nil {
			providers[i].lastErr = err
			continue
		}
		if resp.IsError() {
			providers[i].lastErr = fmt.Errorf("HTTP %d", resp.StatusCode())
			continue
		}
		ip := provider.parseIP(resp.Body())
		if ip == "" {
			providers[i].lastErr = fmt.Errorf("%s 未返回 IP", provider.name)
			continue
		}
		c.JSON(http.StatusOK, gin.H{"data": proxyTestResult{
			OK:        true,
			LatencyMS: latency,
			IP:        ip,
			Provider:  provider.name,
		}})
		return
	}

	msg := "代理测试失败"
	for i := len(providers) - 1; i >= 0; i-- {
		if providers[i].lastErr != nil {
			msg = fmt.Sprintf("%s: %v", providers[i].name, providers[i].lastErr)
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": proxyTestResult{OK: false, Error: msg}})
}
