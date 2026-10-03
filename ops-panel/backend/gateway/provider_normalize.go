// 直连渠道协议、鉴权样式归一化与密钥提示。
package gateway

import (
	"strings"

	"github.com/bejix/upstream-ops/backend/storage"
)

func (svc *Service) normalizeUpstreamProtocol(v string) string {
	up := strings.ToLower(strings.TrimSpace(v))
	switch up {
	case storage.GatewayUpstreamProtocolOpenAI,
		"chat", "chat_completions":
		// 历史 openai / 别名 → 明确的 chat
		return storage.GatewayUpstreamProtocolOpenAIChat
	case storage.GatewayUpstreamProtocolOpenAIChat,
		storage.GatewayUpstreamProtocolOpenAIResponses,
		storage.GatewayUpstreamProtocolAnthropic:
		return up
	case "responses":
		return storage.GatewayUpstreamProtocolOpenAIResponses
	default:
		return storage.GatewayUpstreamProtocolAuto
	}
}

func (svc *Service) normalizeProviderProtocol(v string) string {
	return svc.normalizeUpstreamProtocol(v)
}

func (svc *Service) normalizeProviderAuthStyle(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case storage.GatewayProviderAuthBearer, storage.GatewayProviderAuthXAPIKey:
		return s
	default:
		return storage.GatewayProviderAuthBoth
	}
}

func (svc *Service) apiKeyHint(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return secret[:1] + "…"
	}
	return secret[:4] + "…" + secret[len(secret)-4:]
}
