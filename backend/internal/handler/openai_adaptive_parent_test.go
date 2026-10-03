package handler

import (
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIsAdaptiveParentAPIKeyAcceptsLeavesOrPlatform(t *testing.T) {
	t.Parallel()

	require.False(t, isAdaptiveParentAPIKey(nil))
	require.False(t, isAdaptiveParentAPIKey(&service.APIKey{Group: &service.Group{Platform: service.PlatformOpenAI}}))
	require.True(t, isAdaptiveParentAPIKey(&service.APIKey{Group: &service.Group{Platform: service.PlatformAdaptive}}))
	require.True(t, isAdaptiveParentAPIKey(&service.APIKey{Group: &service.Group{
		Platform: service.PlatformOpenAI,
		AdaptiveLeaves: []service.AdaptiveLeafOption{
			{ID: 85, Name: "claude", Platform: service.PlatformAnthropic},
		},
	}}))
}

func TestResponsesHandlerWiresNativeAdaptiveLeafDispatch(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("openai_gateway_handler.go")
	require.NoError(t, err)
	require.Contains(t, string(src), "tryAdaptiveNativeLeafResponses(")

	dispatch, err := os.ReadFile("openai_adaptive_leaf_dispatch.go")
	require.NoError(t, err)
	require.Contains(t, string(dispatch), "func (h *OpenAIGatewayHandler) tryAdaptiveNativeLeafResponses(")
	require.Contains(t, string(dispatch), "ForwardAsResponses(")

	ws := responsesWebSocketHandlerSource(t)
	require.NotContains(t, ws, "tryAdaptiveNativeLeaf")
}
