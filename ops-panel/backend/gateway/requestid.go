// 网关 request id 生成、响应头写入与 body 注入。
package gateway

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// newRequestID 生成 24 位小写 hex 的网关 request id（12 字节随机）。
func (svc *Service) newRequestID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// copyClientRequestIDHeaders 将客户端请求 ID 相关头原样复制到上游请求（不改写、不合成）。
func (svc *Service) copyClientRequestIDHeaders(dst, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	for _, h := range clientRequestIDHeaders {
		if v := strings.TrimSpace(src.Get(h)); v != "" {
			dst.Set(h, v)
		}
	}
}

// setGatewayRequestIDHeaders 仅写入网关专用 request id 头。
// 不改写 X-Request-Id / X-Client-Request-Id 等任何请求相关 ID（响应侧上游原样透传，请求侧原样转发）。
func (svc *Service) setGatewayRequestIDHeaders(c *gin.Context, id string) {
	if c == nil || strings.TrimSpace(id) == "" {
		return
	}
	c.Header(headerUpstreamOpsRequestID, id)
}

// ensureGatewayRequestID 生成本次请求的网关 request id（使用记录 / 重试链路关联键）。
//
// 只认网关自己生成的 ID：同一次 HandleForward 内通过 gin.Context 复用，避免重复生成。
// 不读取客户端 X-Request-Id / X-Client-Request-Id / 客户端伪造的 X-Upstream-Ops-Request-Id ——
// 客户端（如 Codex）会自带 UUID，若采纳会导致：
//  1. 使用记录 request_id 被改写成客户端 id；
//  2. 客户端重试/重放同一 id 时，多条无关请求被误合成「同请求链路」。
//
// 客户端请求 ID 仍会原样转发上游；响应里也只附加本头，不覆盖上游/客户端的 X-Request-Id。
func (svc *Service) ensureGatewayRequestID(c *gin.Context) string {
	if c == nil {
		return svc.newRequestID()
	}
	if v, ok := c.Get(ctxKeyUpstreamOpsRequestID); ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			svc.setGatewayRequestIDHeaders(c, s)
			return s
		}
	}
	// 仅复用本响应已由网关写入的专用头（同请求内多次 ensure）；不读 X-Request-Id
	if s := strings.TrimSpace(c.Writer.Header().Get(headerUpstreamOpsRequestID)); s != "" {
		if svc.isGatewayGeneratedRequestID(s) {
			c.Set(ctxKeyUpstreamOpsRequestID, s)
			svc.setGatewayRequestIDHeaders(c, s)
			return s
		}
	}
	id := svc.newRequestID()
	c.Set(ctxKeyUpstreamOpsRequestID, id)
	svc.setGatewayRequestIDHeaders(c, id)
	return id
}

// isGatewayGeneratedRequestID 网关 newRequestID 形态：12 字节 → 24 位小写 hex，无连字符。
func (svc *Service) isGatewayGeneratedRequestID(id string) bool {
	if len(id) != 24 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// injectUpstreamOpsRequestID 把网关 request id 写入 JSON 错误体（非 JSON 则包一层）。
func (svc *Service) injectUpstreamOpsRequestID(body []byte, reqID string) []byte {
	reqID = strings.TrimSpace(reqID)
	if reqID == "" {
		return body
	}
	trim := bytes.TrimSpace(body)
	if len(trim) == 0 {
		raw, _ := json.Marshal(gin.H{jsonKeyUpstreamOpsRequestID: reqID})
		return raw
	}
	var m map[string]any
	if err := json.Unmarshal(trim, &m); err != nil {
		// 非 JSON 错误体：包一层，保留原文
		raw, _ := json.Marshal(gin.H{
			"error": gin.H{
				"message": string(trim),
				"type":    "upstream_error",
			},
			jsonKeyUpstreamOpsRequestID: reqID,
		})
		return raw
	}
	m[jsonKeyUpstreamOpsRequestID] = reqID
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}
