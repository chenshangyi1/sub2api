package middleware

import (
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

var corsWarningOnce sync.Once

// CORS applies the configured browser policy. A gatewayPath predicate can be
// supplied for API routes that intentionally accept calls from arbitrary web
// clients while keeping the panel/session API on the configured allowlist.
func CORS(cfg config.CORSConfig, gatewayPath ...func(*gin.Context) bool) gin.HandlerFunc {
	allowedOrigins := normalizeOrigins(cfg.AllowedOrigins)
	allowAll := false
	for _, origin := range allowedOrigins {
		if origin == "*" {
			allowAll = true
			break
		}
	}
	wildcardWithSpecific := allowAll && len(allowedOrigins) > 1
	if wildcardWithSpecific {
		allowedOrigins = []string{"*"}
	}
	allowCredentials := cfg.AllowCredentials

	corsWarningOnce.Do(func() {
		if len(allowedOrigins) == 0 {
			log.Println("Warning: CORS allowed_origins not configured; cross-origin requests will be rejected.")
		}
		if wildcardWithSpecific {
			log.Println("Warning: CORS allowed_origins includes '*'; wildcard will take precedence over explicit origins.")
		}
		if allowAll && allowCredentials {
			log.Println("Warning: CORS allowed_origins set to '*', disabling allow_credentials.")
		}
	})
	if allowAll && allowCredentials {
		allowCredentials = false
	}

	allowedSet := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin == "" || origin == "*" {
			continue
		}
		allowedSet[origin] = struct{}{}
	}
	allowHeaders := []string{
		"Content-Type", "Content-Length", "Accept-Encoding", "X-CSRF-Token", "Authorization",
		"accept", "origin", "Cache-Control", "X-Requested-With", "X-API-Key", "X-Admin-UI-Request", "X-User-UI-Request",
	}
	// OpenAI Node SDK 会发送 x-stainless-* 请求头，需在 CORS 中显式放行。
	openAIProperties := []string{
		"lang", "package-version", "os", "arch", "retry-count", "runtime",
		"runtime-version", "async", "helper-method", "poll-helper", "custom-poll-interval", "timeout",
	}
	for _, prop := range openAIProperties {
		allowHeaders = append(allowHeaders, "x-stainless-"+prop)
	}
	allowHeadersValue := strings.Join(allowHeaders, ", ")
	gatewayHeadersValue := allowHeadersValue + ", X-Goog-API-Key, Anthropic-Version, Anthropic-Beta, Anthropic-Dangerous-Direct-Browser-Access"

	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		gatewayRequest := len(gatewayPath) > 0 && gatewayPath[0] != nil && gatewayPath[0](c)
		allowAnyOrigin := allowAll || gatewayRequest
		originAllowed := allowAnyOrigin
		if origin != "" && !allowAnyOrigin {
			_, originAllowed = allowedSet[origin]
		}

		if originAllowed {
			if allowAnyOrigin {
				c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin != "" {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Add("Vary", "Origin")
			}
			if allowCredentials && !gatewayRequest {
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if gatewayRequest {
				c.Writer.Header().Set("Access-Control-Allow-Headers", gatewayHeadersValue)
			} else {
				c.Writer.Header().Set("Access-Control-Allow-Headers", allowHeadersValue)
			}
			c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
			c.Writer.Header().Set("Access-Control-Expose-Headers", "ETag, Server-Timing")
			c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		}
		// 处理预检请求
		if c.Request.Method == http.MethodOptions {
			if originAllowed {
				c.AbortWithStatus(http.StatusNoContent)
			} else {
				c.AbortWithStatus(http.StatusForbidden)
			}
			return
		}

		c.Next()
	}
}

// IsGatewayAPIRequest matches only routes registered by the API-key gateway.
// Preflight has no Gin route, so match its requested method and URL against
// the same registered route patterns. Panel and unknown paths stay restricted.
func IsGatewayAPIRequest(c *gin.Context, routes []gin.RouteInfo) bool {
	if c == nil || c.Request == nil {
		return false
	}
	method := c.Request.Method
	if method == http.MethodOptions {
		method = strings.TrimSpace(c.GetHeader("Access-Control-Request-Method"))
	}
	path := c.Request.URL.Path
	if strings.ContainsAny(path, "\\\x00") {
		return false
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == "." || part == ".." {
			return false
		}
	}
	for _, route := range routes {
		if route.Method == method && matchesGatewayRoute(route.Path, parts) {
			return true
		}
	}
	return false
}

func matchesGatewayRoute(pattern string, parts []string) bool {
	patternParts := strings.Split(pattern, "/")
	for i, part := range patternParts {
		if i >= len(parts) {
			return false
		}
		if strings.HasPrefix(part, "*") {
			return i == len(patternParts)-1 && strings.Join(parts[i:], "/") != ""
		}
		if strings.HasPrefix(part, ":") {
			if parts[i] == "" {
				return false
			}
		} else if part != parts[i] {
			return false
		}
	}
	return len(patternParts) == len(parts)
}

func normalizeOrigins(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}
