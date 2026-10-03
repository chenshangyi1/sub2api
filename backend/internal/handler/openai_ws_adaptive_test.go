package handler

import (
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSAdaptiveLeafExecutableSkipsAnthropicAndGemini(t *testing.T) {
	t.Parallel()

	require.True(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{}))
	require.True(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: ""}}))
	require.True(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: service.PlatformOpenAI}}))
	require.True(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: service.PlatformGrok}}))
	require.True(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: service.PlatformCN}}))
	require.True(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: service.PlatformVideo}}))
	require.True(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: service.PlatformKimi}}))
	require.True(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: service.PlatformZhipu}}))
	require.True(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: service.PlatformDeepseek}}))
	require.False(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: service.PlatformAnthropic}}))
	require.False(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: service.PlatformGemini}}))
	require.False(t, openAIWSAdaptiveLeafExecutable(&service.APIKey{Group: &service.Group{Platform: "anthropic"}}))
}

func TestOpenAIWSAdaptiveImageAllowedUsesLeavesWhenSessionExists(t *testing.T) {
	t.Parallel()

	parent := &service.APIKey{Group: &service.Group{Platform: service.PlatformOpenAI, AllowImageGeneration: false}}
	blockedLeaf := openAIFallbackGroupAttempt{Group: &service.Group{ID: 11, AllowImageGeneration: false}}
	imageLeaf := openAIFallbackGroupAttempt{Group: &service.Group{ID: 12, AllowImageGeneration: true}}
	session := &openAIAdaptiveSession{ParentGroupID: 99}

	require.False(t, openAIWSAdaptiveImageAllowed(parent, nil, nil))
	require.False(t, openAIWSAdaptiveImageAllowed(parent, []openAIFallbackGroupAttempt{imageLeaf}, nil),
		"non-Adaptive must keep using the primary group, not planned leaves")
	require.False(t, openAIWSAdaptiveImageAllowed(parent, []openAIFallbackGroupAttempt{blockedLeaf}, session))
	require.True(t, openAIWSAdaptiveImageAllowed(parent, []openAIFallbackGroupAttempt{blockedLeaf, imageLeaf}, session))
}

func TestResponsesWebSocketWiresAdaptiveWebSocketProtocol(t *testing.T) {
	t.Parallel()

	wsSource := responsesWebSocketHandlerSource(t)
	require.Contains(t, wsSource, "AdaptiveRouteProtocolOpenAIWebSocket")
	require.Contains(t, wsSource, "finishAdaptiveOpenAIRequest")
	require.Contains(t, wsSource, "openAIWSAdaptiveLeafExecutable")
	require.Contains(t, wsSource, "openAIWSAdaptiveImageAllowed")
	require.Contains(t, wsSource, "markAdaptiveLeafInFlight")
	require.Contains(t, wsSource, "prepareAdaptiveSessionSettlement")
	require.Contains(t, wsSource, "AdaptiveBilling")
	require.NotContains(t, wsSource, "NormalizeOpenAICompatiblePlatform(service.PlatformAnthropic)")
	require.NotContains(t, wsSource, "tryAdaptiveNativeLeaf")
}

func responsesWebSocketHandlerSource(t *testing.T) string {
	t.Helper()
	handlerSource, err := os.ReadFile("openai_gateway_handler.go")
	require.NoError(t, err)
	src := string(handlerSource)
	start := strings.Index(src, "func (h *OpenAIGatewayHandler) ResponsesWebSocket")
	require.NotEqual(t, -1, start)
	rest := src[start:]
	if next := strings.Index(rest[1:], "\nfunc "); next >= 0 {
		rest = rest[:next+1]
	}
	return rest
}
