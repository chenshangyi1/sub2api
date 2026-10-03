// 向客户端写入协议兼容的错误响应。
package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/bejix/upstream-ops/backend/gateway/protocol"
	"github.com/gin-gonic/gin"
)

func (svc *Service) estimateTokenCount(body []byte) int {
	var obj struct {
		Messages []struct {
			Content any `json:"content"`
		} `json:"messages"`
		System any `json:"system"`
	}
	_ = json.Unmarshal(body, &obj)
	var text strings.Builder
	appendContent := func(v any) {
		switch t := v.(type) {
		case string:
			text.WriteString(t)
		case []any:
			for _, p := range t {
				if m, ok := p.(map[string]any); ok {
					if s, ok := m["text"].(string); ok {
						text.WriteString(s)
					}
				}
			}
		}
	}
	appendContent(obj.System)
	for _, m := range obj.Messages {
		appendContent(m.Content)
	}
	n := len([]rune(text.String()))
	if n == 0 {
		n = len(body)
	}
	// 粗估：约 4 字符/token
	tok := n / 4
	if tok < 1 {
		tok = 1
	}
	return tok
}

// HandleUsage GET /v1/usage — 当前密钥近期用量摘要。
func (svc *Service) writeAuthError(c *gin.Context, kind protocolKind, msg string) {
	svc.writeGatewayError(c, kind, http.StatusUnauthorized, "authentication_error", msg)
}

func (svc *Service) writeGatewayError(c *gin.Context, kind protocolKind, status int, typ, msg string) {
	reqID := svc.ensureGatewayRequestID(c)
	if protocol.NormalizeKind(kind) == protocol.KindAnthropic {
		c.JSON(status, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    typ,
				"message": msg,
			},
			jsonKeyUpstreamOpsRequestID: reqID,
		})
		return
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": msg,
			"type":    typ,
		},
		jsonKeyUpstreamOpsRequestID: reqID,
	})
}
